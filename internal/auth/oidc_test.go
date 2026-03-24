package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCVerifierAndMiddleware(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	issuer := "https://issuer.example"
	aud := "postgram-mcp"

	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())

	jwksPath := "/jwks.json"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":   issuer,
				"jwks_uri": server.URL + jwksPath,
			})
		case jwksPath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{
					"kty": "RSA",
					"kid": "k1",
					"n":   n,
					"e":   e,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	verifier, err := NewOIDCVerifier(OIDCConfig{
		IssuerURL:     issuer,
		Audience:      aud,
		JWKSURL:       server.URL + jwksPath,
		RefreshAfter:  time.Millisecond,
		RequiredScope: "postgram.mcp",
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   issuer,
		"aud":   aud,
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"iat":   time.Now().Add(-1 * time.Minute).Unix(),
		"scope": "postgram.mcp other",
		"sub":   "user-1",
	})
	token.Header["kid"] = "k1"
	tokenStr, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	claims, err := verifier.VerifyToken(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims["sub"] != "user-1" {
		t.Fatalf("unexpected sub claim: %v", claims["sub"])
	}

	h := Middleware(verifier, MiddlewareConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	badReq.Header.Set("Authorization", "Bearer bad-token")
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", badRec.Code)
	}

	noScopeToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": issuer,
		"aud": aud,
		"exp": time.Now().Add(5 * time.Minute).Unix(),
		"iat": time.Now().Add(-1 * time.Minute).Unix(),
		"sub": "user-1",
	})
	noScopeToken.Header["kid"] = "k1"
	noScopeTokenStr, err := noScopeToken.SignedString(key)
	if err != nil {
		t.Fatalf("sign no-scope token: %v", err)
	}
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+noScopeTokenStr)
	forbiddenRec := httptest.NewRecorder()
	Middleware(verifier, MiddlewareConfig{RequiredScope: "postgram.mcp"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing scope, got %d", forbiddenRec.Code)
	}
}

func TestMiddlewareSetsMCPChallengeAndPRM(t *testing.T) {
	h := Middleware(&OIDCVerifier{}, MiddlewareConfig{
		Realm:               "mcp",
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
		RequiredScope:       "mcp:tools",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	www := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(www, `Bearer realm="mcp"`) || !strings.Contains(www, `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("unexpected challenge header: %q", www)
	}

	md := OAuthProtectedResourceMetadata{
		Resource:             "https://mcp.example.com/mcp",
		AuthorizationServers: []string{"https://auth.example.com"},
		ScopesSupported:      []string{"mcp:tools"},
	}
	mdReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	mdRec := httptest.NewRecorder()
	ProtectedResourceMetadataHandler(md).ServeHTTP(mdRec, mdReq)
	if mdRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", mdRec.Code)
	}
	if got := mdRec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected json content-type, got %q", got)
	}
}
