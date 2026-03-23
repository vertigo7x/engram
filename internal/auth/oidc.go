package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const ClaimsContextKey contextKey = "engram-auth-claims"

var ErrInsufficientScope = errors.New("insufficient scope")

type OIDCConfig struct {
	IssuerURL     string
	Audience      string
	JWKSURL       string
	RequiredScope string
	HTTPClient    *http.Client
	RefreshAfter  time.Duration
}

type OAuthProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
}

type MiddlewareConfig struct {
	Realm               string
	ResourceMetadataURL string
	RequiredScope       string
}

type OIDCVerifier struct {
	cfg OIDCConfig

	mu          sync.RWMutex
	keysByKID   map[string]*rsa.PublicKey
	allKeys     []*rsa.PublicKey
	lastRefresh time.Time
}

func NewOIDCVerifier(cfg OIDCConfig) (*OIDCVerifier, error) {
	if strings.TrimSpace(cfg.IssuerURL) == "" {
		return nil, errors.New("oidc: issuer url is required")
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, errors.New("oidc: audience is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.RefreshAfter <= 0 {
		cfg.RefreshAfter = 5 * time.Minute
	}

	v := &OIDCVerifier{
		cfg:       cfg,
		keysByKID: map[string]*rsa.PublicKey{},
	}
	if err := v.refresh(context.Background()); err != nil {
		return nil, err
	}

	return v, nil
}

func (v *OIDCVerifier) VerifyToken(ctx context.Context, token string) (jwt.MapClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("missing bearer token")
	}

	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method == nil {
			return nil, errors.New("missing signing method")
		}
		alg := t.Method.Alg()
		if alg != jwt.SigningMethodRS256.Alg() && alg != jwt.SigningMethodRS384.Alg() && alg != jwt.SigningMethodRS512.Alg() {
			return nil, fmt.Errorf("unsupported signing algorithm: %s", alg)
		}
		kid, _ := t.Header["kid"].(string)
		key, keyErr := v.resolveKey(ctx, kid)
		if keyErr != nil {
			return nil, keyErr
		}
		return key, nil
	},
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}),
		jwt.WithIssuer(v.cfg.IssuerURL),
		jwt.WithAudience(v.cfg.Audience),
	)
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	if err := verifyScopes(claims, v.cfg.RequiredScope); err != nil {
		return nil, err
	}

	return claims, nil
}

func verifyScopes(claims jwt.MapClaims, requiredScope string) error {
	requiredScope = strings.TrimSpace(requiredScope)
	if requiredScope == "" {
		return nil
	}

	has := func(scope string) bool {
		for _, s := range strings.Fields(scope) {
			if s == requiredScope {
				return true
			}
		}
		return false
	}

	if scope, ok := claims["scope"].(string); ok && has(scope) {
		return nil
	}
	if scp, ok := claims["scp"].(string); ok && has(scp) {
		return nil
	}
	if scp, ok := claims["scp"].([]any); ok {
		for _, item := range scp {
			if s, sok := item.(string); sok && s == requiredScope {
				return nil
			}
		}
	}

	return fmt.Errorf("%w: required scope %q not present", ErrInsufficientScope, requiredScope)
}

func (v *OIDCVerifier) resolveKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	if key, ok := v.keysByKID[kid]; ok && key != nil {
		v.mu.RUnlock()
		return key, nil
	}
	refreshNeeded := time.Since(v.lastRefresh) >= v.cfg.RefreshAfter
	all := v.allKeys
	v.mu.RUnlock()

	if kid == "" && len(all) == 1 {
		return all[0], nil
	}
	if !refreshNeeded {
		if kid == "" && len(all) > 0 {
			return all[0], nil
		}
	}

	if err := v.refresh(ctx); err != nil {
		return nil, err
	}

	v.mu.RLock()
	defer v.mu.RUnlock()
	if key, ok := v.keysByKID[kid]; ok && key != nil {
		return key, nil
	}
	if kid == "" && len(v.allKeys) > 0 {
		return v.allKeys[0], nil
	}

	return nil, fmt.Errorf("no key found for kid=%q", kid)
}

func (v *OIDCVerifier) refresh(ctx context.Context) error {
	jwksURL := strings.TrimSpace(v.cfg.JWKSURL)
	if jwksURL == "" {
		doc, err := discover(ctx, v.cfg.HTTPClient, v.cfg.IssuerURL)
		if err != nil {
			return err
		}
		jwksURL = doc.JWKSURI
	}

	keysByKID, allKeys, err := fetchJWKS(ctx, v.cfg.HTTPClient, jwksURL)
	if err != nil {
		return err
	}
	if len(allKeys) == 0 {
		return errors.New("oidc: no RSA keys found in JWKS")
	}

	v.mu.Lock()
	v.keysByKID = keysByKID
	v.allKeys = allKeys
	v.lastRefresh = time.Now()
	v.mu.Unlock()
	return nil
}

type openIDConfigDocument struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

func discover(ctx context.Context, c *http.Client, issuer string) (*openIDConfigDocument, error) {
	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery status %d", resp.StatusCode)
	}
	var doc openIDConfigDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("oidc: discovery parse: %w", err)
	}
	if strings.TrimSpace(doc.JWKSURI) == "" {
		return nil, errors.New("oidc: jwks_uri missing in discovery document")
	}
	return &doc, nil
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func fetchJWKS(ctx context.Context, c *http.Client, jwksURL string) (map[string]*rsa.PublicKey, []*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc: jwks request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc: jwks fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("oidc: jwks status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("oidc: jwks parse: %w", err)
	}

	keysByKID := map[string]*rsa.PublicKey{}
	allKeys := make([]*rsa.PublicKey, 0, len(doc.Keys))
	for _, k := range doc.Keys {
		if strings.ToUpper(k.KTY) != "RSA" {
			continue
		}
		pk, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		if k.KID != "" {
			keysByKID[k.KID] = pk
		}
		allKeys = append(allKeys, pk)
	}

	return keysByKID, allKeys, nil
}

func parseRSAPublicKey(nBase64, eBase64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nBase64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eBase64)
	if err != nil {
		return nil, err
	}
	if len(eBytes) == 0 {
		return nil, errors.New("empty exponent")
	}

	e := 0
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}
	if e <= 1 {
		return nil, errors.New("invalid exponent")
	}

	pk := &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}
	if pk.N.Sign() <= 0 {
		return nil, errors.New("invalid modulus")
	}
	return pk, nil
}

func ProtectedResourceMetadataHandler(metadata OAuthProtectedResourceMetadata) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metadata)
	})
}

func Middleware(verifier *OIDCVerifier, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	realm := strings.TrimSpace(cfg.Realm)
	if realm == "" {
		realm = "mcp"
	}
	resourceMetadataURL := strings.TrimSpace(cfg.ResourceMetadataURL)
	requiredScope := strings.TrimSpace(cfg.RequiredScope)

	challenge := func(extra ...string) string {
		parts := []string{fmt.Sprintf("Bearer realm=%q", realm)}
		if resourceMetadataURL != "" {
			parts = append(parts, fmt.Sprintf("resource_metadata=%q", resourceMetadataURL))
		}
		parts = append(parts, extra...)
		return strings.Join(parts, ", ")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
				w.Header().Set("WWW-Authenticate", challenge())
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimSpace(authz[len("Bearer "):])
			claims, err := verifier.VerifyToken(r.Context(), token)
			if err != nil {
				if errors.Is(err, ErrInsufficientScope) {
					extra := []string{fmt.Sprintf("error=%q", "insufficient_scope")}
					if requiredScope != "" {
						extra = append(extra, fmt.Sprintf("scope=%q", requiredScope))
					}
					w.Header().Set("WWW-Authenticate", challenge(extra...))
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("WWW-Authenticate", challenge(fmt.Sprintf("error=%q", "invalid_token")))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClaimsFromContext(ctx context.Context) jwt.MapClaims {
	if ctx == nil {
		return nil
	}
	claims, _ := ctx.Value(ClaimsContextKey).(jwt.MapClaims)
	return claims
}

func StringClaim(claims jwt.MapClaims, key string) string {
	if claims == nil {
		return ""
	}
	v, _ := claims[key].(string)
	return strings.TrimSpace(v)
}
