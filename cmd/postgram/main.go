// Postgram — Persistent memory for AI coding agents.
//
// Usage:
//
//	postgram serve          Start HTTP API + MCP over HTTP
//	postgram search <query> Search memories from CLI
//	postgram save           Save a memory from CLI
//	postgram context        Show recent context
//	postgram stats          Show memory stats
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Gentleman-Programming/postgram/internal/auth"
	"github.com/Gentleman-Programming/postgram/internal/mcp"
	"github.com/Gentleman-Programming/postgram/internal/server"
	"github.com/Gentleman-Programming/postgram/internal/store"
	"github.com/Gentleman-Programming/postgram/internal/tui"
	versioncheck "github.com/Gentleman-Programming/postgram/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// version is set via ldflags at build time by goreleaser.
// Falls back to "dev" for local builds.
var version = "dev"

var (
	storeNew      = store.New
	newHTTPServer = server.New
	startHTTP     = (*server.Server).Start

	newMCPServerWithTools = mcp.NewServerWithTools
	resolveMCPTools       = mcp.ResolveTools
	newMCPHTTPServer      = mcpserver.NewStreamableHTTPServer

	newTUIModel   = func(s *store.Store) tui.Model { return tui.New(s, version) }
	newTeaProgram = tea.NewProgram
	runTeaProgram = (*tea.Program).Run

	checkForUpdates = versioncheck.CheckLatest

	storeSearch = func(s *store.Store, query string, opts store.SearchOptions) ([]store.SearchResult, error) {
		return s.Search(query, opts)
	}
	storeAddObservation = func(s *store.Store, p store.AddObservationParams) (string, error) { return s.AddObservation(p) }
	storeTimeline       = func(s *store.Store, observationID string, before, after int) (*store.TimelineResult, error) {
		return s.Timeline(observationID, before, after)
	}
	storeFormatContext = func(s *store.Store, project, scope string) (string, error) { return s.FormatContext(project, scope) }
	storeStats         = func(s *store.Store) (*store.Stats, error) { return s.Stats() }
	storeExport        = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
	jsonMarshalIndent  = json.MarshalIndent

	exitFunc = os.Exit

	stdinScanner = func() *bufio.Scanner { return bufio.NewScanner(os.Stdin) }
	userHomeDir  = os.UserHomeDir
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		exitFunc(1)
	}

	// Check for updates on every invocation (2s timeout, silent on failure).
	if msg := checkForUpdates(version); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		fmt.Fprintln(os.Stderr)
	}

	cfg, cfgErr := store.DefaultConfig()
	if cfgErr != nil {
		// Fallback: try to resolve home directory from environment variables
		// that os.UserHomeDir() might have missed (e.g. MCP subprocesses on
		// Windows where %USERPROFILE% is not propagated).
		if home := resolveHomeFallback(); home != "" {
			log.Printf("[postgram] UserHomeDir failed, using fallback: %s", home)
			cfg = store.FallbackConfig(filepath.Join(home, ".postgram"))
		} else {
			fatal(cfgErr)
		}
	}

	// Allow overriding data dir via env
	if dir := os.Getenv("POSTGRAM_DATA_DIR"); dir != "" {
		cfg.DataDir = dir
	}

	if dbURL := os.Getenv("POSTGRAM_DATABASE_URL"); dbURL != "" {
		cfg.DatabaseURL = dbURL
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(cfg)
	case "tui":
		cmdTUI(cfg)
	case "search":
		cmdSearch(cfg)
	case "save":
		cmdSave(cfg)
	case "timeline":
		cmdTimeline(cfg)
	case "context":
		cmdContext(cfg)
	case "stats":
		cmdStats(cfg)
	case "export":
		cmdExport(cfg)
	case "import":
		cmdImport(cfg)
	case "version", "--version", "-v":
		fmt.Printf("postgram %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		exitFunc(1)
	}
}

// ─── Commands ────────────────────────────────────────────────────────────────

func cmdServe(cfg store.Config) {
	port := 7437 // "ENGR" on phone keypad vibes
	host := "127.0.0.1"
	if h := os.Getenv("POSTGRAM_HOST"); h != "" {
		host = h
	}
	if p := os.Getenv("POSTGRAM_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	mcpPath := strings.TrimSpace(os.Getenv("POSTGRAM_MCP_HTTP_PATH"))
	if mcpPath == "" {
		mcpPath = "/mcp"
	}
	toolsFilter := strings.TrimSpace(os.Getenv("POSTGRAM_MCP_TOOLS"))
	// Allow: postgram serve 8080
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			port = n
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	srv := newHTTPServer(s, port, version)
	srv.SetHost(host)

	var mcpSrv *mcpserver.MCPServer
	if toolsFilter != "" {
		mcpSrv = newMCPServerWithTools(s, resolveMCPTools(toolsFilter))
	} else {
		mcpSrv = newMCPServerWithTools(s, resolveMCPTools("agent"))
	}

	mcpHandler := http.Handler(newMCPHTTPServer(mcpSrv, mcpserver.WithEndpointPath(mcpPath)))
	verifier, authCfg, err := buildOIDCVerifierFromEnv(host, port, mcpPath)
	if err != nil {
		fatal(err)
	} else if verifier != nil {
		srv.SetMCPHandler(authCfg.ResourceMetadataPath, auth.ProtectedResourceMetadataHandler(auth.OAuthProtectedResourceMetadata{
			Resource:             authCfg.MCPResource,
			AuthorizationServers: authCfg.AuthorizationServers,
			ScopesSupported:      authCfg.ScopesSupported,
		}))
		mcpHandler = auth.Middleware(verifier, auth.MiddlewareConfig{
			Realm:               "mcp",
			ResourceMetadataURL: authCfg.ResourceMetadataURL,
			RequiredScope:       authCfg.RequiredScope,
		})(mcpHandler)
	}

	srv.SetMCPHandler(mcpPath, mcpHandler)
	log.Printf("[postgram] MCP HTTP transport enabled on %s", mcpPath)

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[postgram] shutting down...")
		exitFunc(0)
	}()

	if err := startHTTP(srv); err != nil {
		fatal(err)
	}
}

type mcpAuthConfig struct {
	RequiredScope        string
	ResourceMetadataPath string
	ResourceMetadataURL  string
	MCPResource          string
	AuthorizationServers []string
	ScopesSupported      []string
}

func buildOIDCVerifierFromEnv(host string, port int, mcpPath string) (*auth.OIDCVerifier, *mcpAuthConfig, error) {
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("POSTGRAM_MCP_AUTH_ENABLED")), "true")
	if !enabled {
		return nil, nil, nil
	}

	issuer := strings.TrimSpace(os.Getenv("POSTGRAM_OIDC_ISSUER"))
	audience := strings.TrimSpace(os.Getenv("POSTGRAM_OIDC_AUDIENCE"))
	jwksURL := strings.TrimSpace(os.Getenv("POSTGRAM_OIDC_JWKS_URL"))
	requiredScope := strings.TrimSpace(os.Getenv("POSTGRAM_OIDC_REQUIRED_SCOPE"))

	verifier, err := auth.NewOIDCVerifier(auth.OIDCConfig{
		IssuerURL:     issuer,
		Audience:      audience,
		JWKSURL:       jwksURL,
		RequiredScope: requiredScope,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure OIDC verifier: %w", err)
	}

	resourcePath := strings.TrimSpace(os.Getenv("POSTGRAM_OAUTH_RESOURCE_METADATA_PATH"))
	if resourcePath == "" {
		resourcePath = "/.well-known/oauth-protected-resource"
	}
	if !strings.HasPrefix(resourcePath, "/") {
		resourcePath = "/" + resourcePath
	}

	mcpResource := strings.TrimSpace(os.Getenv("POSTGRAM_OAUTH_RESOURCE"))
	if mcpResource == "" {
		mcpResource = buildPublicURL(host, port, mcpPath)
	}

	authServerRaw := strings.TrimSpace(os.Getenv("POSTGRAM_OAUTH_AUTHORIZATION_SERVERS"))
	authServers := []string{}
	if authServerRaw != "" {
		for _, item := range strings.Split(authServerRaw, ",") {
			v := strings.TrimSpace(item)
			if v != "" {
				authServers = append(authServers, v)
			}
		}
	}
	if len(authServers) == 0 {
		authServers = []string{issuer}
	}

	metadataBase := strings.TrimSpace(os.Getenv("POSTGRAM_BASE_URL"))
	if metadataBase == "" {
		metadataBase = buildPublicURL(host, port, "")
	}
	metadataURL := strings.TrimRight(metadataBase, "/") + resourcePath

	var scopes []string
	if requiredScope != "" {
		scopes = []string{requiredScope}
	}
	log.Printf("[postgram] MCP OIDC auth enabled (issuer=%s, audience=%s)", issuer, audience)
	return verifier, &mcpAuthConfig{
		RequiredScope:        requiredScope,
		ResourceMetadataPath: resourcePath,
		ResourceMetadataURL:  metadataURL,
		MCPResource:          mcpResource,
		AuthorizationServers: authServers,
		ScopesSupported:      scopes,
	}, nil
}

func buildPublicURL(host string, port int, path string) string {
	resolvedHost := strings.TrimSpace(host)
	if resolvedHost == "" || resolvedHost == "0.0.0.0" || resolvedHost == "::" {
		resolvedHost = "localhost"
	}
	if !strings.HasPrefix(path, "/") && path != "" {
		path = "/" + path
	}
	return fmt.Sprintf("http://%s:%d%s", resolvedHost, port, path)
}

func cmdTUI(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	model := newTUIModel(s)
	p := newTeaProgram(model)
	if _, err := runTeaProgram(p); err != nil {
		fatal(err)
	}
}

func cmdSearch(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: postgram search <query> [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]")
		exitFunc(1)
	}

	// Collect the query (everything that's not a flag)
	var queryParts []string
	opts := store.SearchOptions{Limit: 10}

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--type":
			if i+1 < len(os.Args) {
				opts.Type = os.Args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(os.Args) {
				opts.Project = os.Args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					opts.Limit = n
				}
				i++
			}
		case "--scope":
			if i+1 < len(os.Args) {
				opts.Scope = os.Args[i+1]
				i++
			}
		default:
			queryParts = append(queryParts, os.Args[i])
		}
	}

	query := strings.Join(queryParts, " ")
	if query == "" {
		fmt.Fprintln(os.Stderr, "error: search query is required")
		exitFunc(1)
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	results, err := storeSearch(s, query, opts)
	if err != nil {
		fatal(err)
		return
	}

	if len(results) == 0 {
		fmt.Printf("No memories found for: %q\n", query)
		return
	}

	fmt.Printf("Found %d memories:\n\n", len(results))
	for i, r := range results {
		project := ""
		if r.Project != nil {
			project = fmt.Sprintf(" | project: %s", *r.Project)
		}
		fmt.Printf("[%d] %s (%s) — %s\n    %s\n    %s%s | scope: %s\n\n",
			i+1, r.ID, r.Type, r.Title,
			truncate(r.Content, 300),
			r.CreatedAt, project, r.Scope)
	}
}

func cmdSave(cfg store.Config) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: postgram save <title> <content> [--type TYPE] [--project PROJECT] [--scope SCOPE] [--topic TOPIC_KEY]")
		exitFunc(1)
	}

	title := os.Args[2]
	content := os.Args[3]
	typ := "manual"
	project := ""
	scope := "project"
	topicKey := ""

	for i := 4; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--type":
			if i+1 < len(os.Args) {
				typ = os.Args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(os.Args) {
				project = os.Args[i+1]
				i++
			}
		case "--scope":
			if i+1 < len(os.Args) {
				scope = os.Args[i+1]
				i++
			}
		case "--topic":
			if i+1 < len(os.Args) {
				topicKey = os.Args[i+1]
				i++
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	sessionID := "manual-save"
	if project != "" {
		sessionID = "manual-save-" + project
	}
	_ = s.CreateSession(store.CreateSessionParams{ClientSessionID: sessionID, Project: project, Directory: ""})
	id, err := storeAddObservation(s, store.AddObservationParams{
		SessionID: store.CreateSessionParams{ClientSessionID: sessionID, Project: project}.EffectiveID(),
		Type:      typ,
		Title:     title,
		Content:   content,
		Project:   project,
		Scope:     scope,
		TopicKey:  topicKey,
	})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Memory saved: %s %q (%s)\n", id, title, typ)
}

func cmdTimeline(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: postgram timeline <observation_id> [--before N] [--after N]")
		exitFunc(1)
	}

	obsID := strings.TrimSpace(os.Args[2])
	if obsID == "" || !validObservationID(strings.TrimSpace(os.Args[2])) {
		fmt.Fprintf(os.Stderr, "error: invalid observation id %q\n", os.Args[2])
		exitFunc(1)
	}

	before, after := 5, 5
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--before":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					before = n
				}
				i++
			}
		case "--after":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					after = n
				}
				i++
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	result, err := storeTimeline(s, obsID, before, after)
	if err != nil {
		fatal(err)
	}

	// Session header
	if result.SessionInfo != nil {
		summary := ""
		if result.SessionInfo.Summary != nil {
			summary = fmt.Sprintf(" — %s", truncate(*result.SessionInfo.Summary, 100))
		}
		fmt.Printf("Session: %s (%s)%s\n", result.SessionInfo.Project, result.SessionInfo.StartedAt, summary)
		fmt.Printf("Total observations in session: %d\n\n", result.TotalInRange)
	}

	// Before
	if len(result.Before) > 0 {
		fmt.Println("─── Before ───")
		for _, e := range result.Before {
			fmt.Printf("  %s [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
		}
		fmt.Println()
	}

	// Focus
	fmt.Printf(">>> %s [%s] %s <<<\n", result.Focus.ID, result.Focus.Type, result.Focus.Title)
	fmt.Printf("    %s\n", truncate(result.Focus.Content, 500))
	fmt.Printf("    %s\n\n", result.Focus.CreatedAt)

	// After
	if len(result.After) > 0 {
		fmt.Println("─── After ───")
		for _, e := range result.After {
			fmt.Printf("  %s [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
		}
	}
}

func cmdContext(cfg store.Config) {
	project := ""
	scope := ""

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--scope":
			if i+1 < len(os.Args) {
				scope = os.Args[i+1]
				i++
			}
		default:
			if project == "" {
				project = os.Args[i]
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	ctx, err := storeFormatContext(s, project, scope)
	if err != nil {
		fatal(err)
	}

	if ctx == "" {
		fmt.Println("No previous session memories found.")
		return
	}

	fmt.Print(ctx)
}

func cmdStats(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	stats, err := storeStats(s)
	if err != nil {
		fatal(err)
	}

	projects := "none yet"
	if len(stats.Projects) > 0 {
		projects = strings.Join(stats.Projects, ", ")
	}

	fmt.Printf("Postgram Memory Stats\n")
	fmt.Printf("  Sessions:     %d\n", stats.TotalSessions)
	fmt.Printf("  Observations: %d\n", stats.TotalObservations)
	fmt.Printf("  Prompts:      %d\n", stats.TotalPrompts)
	fmt.Printf("  Projects:     %s\n", projects)
	fmt.Printf("  Database:     postgres (POSTGRAM_DATABASE_URL)\n")
}

func cmdExport(cfg store.Config) {
	outFile := "postgram-export.json"
	if len(os.Args) > 2 {
		outFile = os.Args[2]
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	data, err := storeExport(s)
	if err != nil {
		fatal(err)
	}

	out, err := jsonMarshalIndent(data, "", "  ")
	if err != nil {
		fatal(err)
	}

	if err := os.WriteFile(outFile, out, 0644); err != nil {
		fatal(err)
	}

	fmt.Printf("Exported to %s\n", outFile)
	fmt.Printf("  Sessions:     %d\n", len(data.Sessions))
	fmt.Printf("  Observations: %d\n", len(data.Observations))
	fmt.Printf("  Prompts:      %d\n", len(data.Prompts))
}

func cmdImport(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: postgram import <file.json>")
		exitFunc(1)
	}

	inFile := os.Args[2]
	raw, err := os.ReadFile(inFile)
	if err != nil {
		fatal(fmt.Errorf("read %s: %w", inFile, err))
	}

	var data store.ExportData
	if err := json.Unmarshal(raw, &data); err != nil {
		fatal(fmt.Errorf("parse %s: %w", inFile, err))
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	result, err := s.Import(&data)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Imported from %s\n", inFile)
	fmt.Printf("  Sessions:     %d\n", result.SessionsImported)
	fmt.Printf("  Observations: %d\n", result.ObservationsImported)
	fmt.Printf("  Prompts:      %d\n", result.PromptsImported)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func printUsage() {
	fmt.Printf(`postgram v%s — Persistent memory for AI coding agents

Usage:
  postgram <command> [arguments]

Commands:
  serve [port]       Start HTTP API server + MCP over HTTP (default: 7437)
                       Configure MCP with POSTGRAM_MCP_HTTP_PATH and POSTGRAM_MCP_TOOLS
                       Profiles: agent (11 tools), admin (3 tools), all (default, 14)
                       Combine: POSTGRAM_MCP_TOOLS=agent,admin or pick individual tools
  tui                Launch interactive terminal UI
  search <query>     Search memories [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]
  save <title> <msg> Save a memory  [--type TYPE] [--project PROJECT] [--scope SCOPE]
  timeline <obs_id>  Show chronological context around an observation [--before N] [--after N]
	context [project]  Show recent context from previous sessions
	stats              Show memory system statistics
	export [file]      Export all memories to JSON (default: postgram-export.json)
	import <file>      Import memories from a JSON export file

	version            Print version
	help               Show this help

Environment:
  POSTGRAM_DATA_DIR    Override data directory (default: ~/.postgram)
  POSTGRAM_DATABASE_URL PostgreSQL connection URL (required)
  POSTGRAM_PORT        Override HTTP server port (default: 7437)
  POSTGRAM_HOST        HTTP bind host (default: 127.0.0.1)
  POSTGRAM_BASE_URL    Public base URL for metadata (e.g. https://mcp.company.com)
  POSTGRAM_MCP_URL     Explicit MCP URL for generated agent configs/examples
  POSTGRAM_MCP_HTTP_PATH MCP HTTP endpoint path (default: /mcp)
  POSTGRAM_MCP_TOOLS   MCP tool profile/filter (e.g. agent, admin, agent,admin)
  POSTGRAM_MCP_AUTH_ENABLED Enable JWT OIDC auth for MCP HTTP (true/false)
  POSTGRAM_OIDC_ISSUER OIDC issuer URL (required when auth enabled)
  POSTGRAM_OIDC_AUDIENCE OIDC audience (required when auth enabled)
  POSTGRAM_OIDC_JWKS_URL Optional JWKS URL override (otherwise discovered)
  POSTGRAM_OIDC_REQUIRED_SCOPE Optional required scope (e.g. postgram.mcp)
  POSTGRAM_OAUTH_RESOURCE_METADATA_PATH OAuth protected resource metadata path
  POSTGRAM_OAUTH_RESOURCE  OAuth resource identifier (defaults to MCP URL)
  POSTGRAM_OAUTH_AUTHORIZATION_SERVERS Comma-separated auth server URLs for PRM

MCP Configuration (add to your agent's config):
  {
    "mcp": {
      "postgram": {
        "type": "remote",
        "url": "https://your-postgram-host/mcp"
      }
    }
  }
`, version)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "postgram: %s\n", err)
	exitFunc(1)
}

// resolveHomeFallback tries platform-specific environment variables to find
// a home directory when os.UserHomeDir() fails. This commonly happens on
// Windows when postgram is launched as an MCP subprocess without full env
// propagation.
func resolveHomeFallback() string {
	// Windows: try common env vars that might be set even when
	// %USERPROFILE% is missing.
	for _, env := range []string{"USERPROFILE", "HOME", "LOCALAPPDATA"} {
		if v := os.Getenv(env); v != "" {
			if env == "LOCALAPPDATA" {
				// LOCALAPPDATA is C:\Users\<user>\AppData\Local — go up two levels.
				parent := filepath.Dir(filepath.Dir(v))
				if parent != "." && parent != v {
					return parent
				}
			}
			return v
		}
	}

	// Unix: $HOME should always work, but try passwd-style fallback.
	if v := os.Getenv("HOME"); v != "" {
		return v
	}

	return ""
}
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func validObservationID(id string) bool {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || trimmed != id {
		return false
	}
	_, err := uuid.Parse(trimmed)
	return err == nil
}
