package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	postgramsrv "github.com/vertigo7x/postgram/internal/server"
	"github.com/vertigo7x/postgram/internal/store"
	"github.com/vertigo7x/postgram/internal/testutil"
	"github.com/vertigo7x/postgram/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/golang-jwt/jwt/v5"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type exitCode int

func captureOutputAndRecover(t *testing.T, fn func()) (stdout string, stderr string, recovered any) {
	t.Helper()

	oldOut := os.Stdout
	oldErr := os.Stderr

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = outW
	os.Stderr = errW

	func() {
		defer func() {
			recovered = recover()
		}()
		fn()
	}()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return string(outBytes), string(errBytes), recovered
}

func stubExitWithPanic(t *testing.T) {
	t.Helper()
	old := exitFunc
	exitFunc = func(code int) { panic(exitCode(code)) }
	t.Cleanup(func() { exitFunc = old })
}

func stubRuntimeHooks(t *testing.T) {
	t.Helper()
	oldStoreNew := storeNew
	oldNewHTTPServer := newHTTPServer
	oldStartHTTP := startHTTP
	oldNewMCPServerWithTools := newMCPServerWithTools
	oldNewMCPHTTPServer := newMCPHTTPServer
	oldNewTUIModel := newTUIModel
	oldNewTeaProgram := newTeaProgram
	oldRunTeaProgram := runTeaProgram
	oldStoreSearch := storeSearch
	oldStoreAddObservation := storeAddObservation
	oldStoreTimeline := storeTimeline
	oldStoreFormatContext := storeFormatContext
	oldStoreStats := storeStats
	oldStoreExport := storeExport
	oldJSONMarshalIndent := jsonMarshalIndent

	storeNew = store.New
	newHTTPServer = func(s *store.Store, _ int, version string) *postgramsrv.Server { return postgramsrv.New(s, 0, version) }
	startHTTP = func(_ *postgramsrv.Server) error { return nil }
	newMCPServerWithTools = func(s *store.Store, allowlist map[string]bool) *mcpserver.MCPServer {
		return mcpserver.NewMCPServer("test", "0", mcpserver.WithRecovery())
	}
	newMCPHTTPServer = mcpserver.NewStreamableHTTPServer
	newTUIModel = func(_ *store.Store) tui.Model { return tui.New(nil, "") }
	newTeaProgram = func(tea.Model, ...tea.ProgramOption) *tea.Program { return &tea.Program{} }
	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	storeSearch = func(s *store.Store, query string, opts store.SearchOptions) ([]store.SearchResult, error) {
		return s.Search(query, opts)
	}
	storeAddObservation = func(s *store.Store, p store.AddObservationParams) (string, error) {
		return s.AddObservation(p)
	}
	storeTimeline = func(s *store.Store, observationID string, before, after int) (*store.TimelineResult, error) {
		return s.Timeline(observationID, before, after)
	}
	storeFormatContext = func(s *store.Store, project, scope string) (string, error) {
		return s.FormatContext(project, scope)
	}
	storeStats = func(s *store.Store) (*store.Stats, error) { return s.Stats() }
	storeExport = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
	jsonMarshalIndent = json.MarshalIndent

	t.Cleanup(func() {
		storeNew = oldStoreNew
		newHTTPServer = oldNewHTTPServer
		startHTTP = oldStartHTTP
		newMCPServerWithTools = oldNewMCPServerWithTools
		newMCPHTTPServer = oldNewMCPHTTPServer
		newTUIModel = oldNewTUIModel
		newTeaProgram = oldNewTeaProgram
		runTeaProgram = oldRunTeaProgram
		storeSearch = oldStoreSearch
		storeAddObservation = oldStoreAddObservation
		storeTimeline = oldStoreTimeline
		storeFormatContext = oldStoreFormatContext
		storeStats = oldStoreStats
		storeExport = oldStoreExport
		jsonMarshalIndent = oldJSONMarshalIndent
	})
}

func TestFatal(t *testing.T) {
	stubExitWithPanic(t)
	_, stderr, recovered := captureOutputAndRecover(t, func() {
		fatal(errors.New("boom"))
	})

	code, ok := recovered.(exitCode)
	if !ok || int(code) != 1 {
		t.Fatalf("expected exit code 1 panic, got %v", recovered)
	}
	if !strings.Contains(stderr, "postgram: boom") {
		t.Fatalf("fatal stderr mismatch: %q", stderr)
	}
}

func TestCmdServeParsesPortAndErrors(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	tests := []struct {
		name      string
		envPort   string
		argPort   string
		wantPort  int
		startErr  error
		wantFatal bool
	}{
		{name: "default port", wantPort: 7437},
		{name: "env port", envPort: "8123", wantPort: 8123},
		{name: "arg overrides env", envPort: "8123", argPort: "9001", wantPort: 9001},
		{name: "invalid env keeps default", envPort: "nope", wantPort: 7437},
		{name: "invalid arg keeps env", envPort: "8123", argPort: "bad", wantPort: 8123},
		{name: "start failure", wantPort: 7437, startErr: errors.New("listen failed"), wantFatal: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubExitWithPanic(t)
			if tc.envPort != "" {
				t.Setenv("POSTGRAM_PORT", tc.envPort)
			} else {
				t.Setenv("POSTGRAM_PORT", "")
			}

			args := []string{"postgram", "serve"}
			if tc.argPort != "" {
				args = append(args, tc.argPort)
			}
			withArgs(t, args...)

			seenPort := -1
			newHTTPServer = func(s *store.Store, port int, version string) *postgramsrv.Server {
				seenPort = port
				return postgramsrv.New(s, 0, version)
			}
			startHTTP = func(_ *postgramsrv.Server) error {
				return tc.startErr
			}

			_, stderr, recovered := captureOutputAndRecover(t, func() {
				cmdServe(cfg)
			})

			if seenPort != tc.wantPort {
				t.Fatalf("port=%d want=%d", seenPort, tc.wantPort)
			}
			if tc.wantFatal {
				if _, ok := recovered.(exitCode); !ok {
					t.Fatalf("expected fatal exit, got %v", recovered)
				}
				if !strings.Contains(stderr, "listen failed") {
					t.Fatalf("stderr missing start error: %q", stderr)
				}
			} else if recovered != nil {
				t.Fatalf("expected no panic, got %v", recovered)
			}
		})
	}
}

func TestCmdTUIBranches(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, errors.New("tui failed") }
	_, tuiErr, recovered := captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(tuiErr, "tui failed") {
		t.Fatalf("expected tui fatal, got panic=%v stderr=%q", recovered, tuiErr)
	}

	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	_, _, recovered = captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if recovered != nil {
		t.Fatalf("unexpected panic on successful tui: %v", recovered)
	}
}

func TestCmdExportDefaultAndCmdImportErrors(t *testing.T) {
	workDir := t.TempDir()
	withCwd(t, workDir)

	cfg := testConfig(t)
	stubExitWithPanic(t)

	mustSeedObservation(t, cfg, "s-exp-default", "proj", "note", "title", "content", "project")

	withArgs(t, "postgram", "export")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("export default should succeed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Exported to postgram-export.json") {
		t.Fatalf("unexpected default export output: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(workDir, "postgram-export.json")); err != nil {
		t.Fatalf("expected default export file: %v", err)
	}

	badPath := filepath.Join(workDir, "missing", "out.json")
	withArgs(t, "postgram", "export", badPath)
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdExport(cfg) })
	if _, ok := recovered.(exitCode); !ok || (!strings.Contains(strings.ToLower(stderr), "no such file") && !strings.Contains(strings.ToLower(stderr), "cannot find") && !strings.Contains(strings.ToLower(stderr), "no puede encontrar")) {
		t.Fatalf("expected export write fatal, panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "postgram", "import")
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "usage: postgram import") {
		t.Fatalf("expected import usage exit, panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "postgram", "import", filepath.Join(workDir, "nope.json"))
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "read") {
		t.Fatalf("expected import read fatal, panic=%v stderr=%q", recovered, stderr)
	}

	invalidJSON := filepath.Join(workDir, "invalid.json")
	if err := os.WriteFile(invalidJSON, []byte("{invalid"), 0644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}
	withArgs(t, "postgram", "import", invalidJSON)
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "parse") {
		t.Fatalf("expected import parse fatal, panic=%v stderr=%q", recovered, stderr)
	}
}

func TestMainDispatchServeAndTUI(t *testing.T) {
	stubRuntimeHooks(t)
	t.Setenv("POSTGRAM_DATABASE_URL", testutil.NewPostgresURL(t))

	t.Setenv("POSTGRAM_DATA_DIR", t.TempDir())
	withArgs(t, "postgram", "serve", "8088")
	_, stderr, recovered := captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("serve dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "postgram", "tui")
	_, stderr, recovered = captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("tui dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}
}

func TestStoreInitFailurePaths(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	cfg := testConfig(t)
	importFile := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(importFile, []byte(`{"version":"0.1.0","exported_at":"2026-01-01T00:00:00Z","sessions":[],"observations":[],"prompts":[]}`), 0644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	storeNew = func(store.Config) (*store.Store, error) {
		return nil, errors.New("store init failed")
	}

	cmds := []func(store.Config){
		cmdServe,
		cmdTUI,
		cmdSearch,
		cmdSave,
		cmdTimeline,
		cmdContext,
		cmdStats,
		cmdExport,
		cmdImport,
	}

	argsByCmd := [][]string{
		{"postgram", "serve"},
		{"postgram", "tui"},
		{"postgram", "search", "q"},
		{"postgram", "save", "t", "c"},
		{"postgram", "timeline", "11111111-1111-1111-1111-111111111111"},
		{"postgram", "context"},
		{"postgram", "stats"},
		{"postgram", "export"},
		{"postgram", "import", importFile},
	}

	for i, fn := range cmds {
		withArgs(t, argsByCmd[i]...)
		_, stderr, recovered := captureOutputAndRecover(t, func() { fn(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("command %d: expected exit panic, got %v", i, recovered)
		}
		if !strings.Contains(stderr, "store init failed") {
			t.Fatalf("command %d: expected store failure stderr, got %q", i, stderr)
		}
	}
}

func TestUsageAndValidationExits(t *testing.T) {
	cfg := testConfig(t)
	stubExitWithPanic(t)

	tests := []struct {
		name       string
		args       []string
		run        func(store.Config)
		errSubstr  string
		stderrOnly bool
	}{
		{name: "search usage", args: []string{"postgram", "search"}, run: cmdSearch, errSubstr: "usage: postgram search"},
		{name: "search missing query", args: []string{"postgram", "search", "--limit", "3"}, run: cmdSearch, errSubstr: "search query is required"},
		{name: "save usage", args: []string{"postgram", "save", "title"}, run: cmdSave, errSubstr: "usage: postgram save"},
		{name: "timeline usage", args: []string{"postgram", "timeline"}, run: cmdTimeline, errSubstr: "usage: postgram timeline"},
		{name: "timeline invalid id", args: []string{"postgram", "timeline", "!"}, run: cmdTimeline, errSubstr: "invalid observation id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, tc.args...)
			_, stderr, recovered := captureOutputAndRecover(t, func() { tc.run(cfg) })
			if _, ok := recovered.(exitCode); !ok {
				t.Fatalf("expected exit panic, got %v", recovered)
			}
			if !strings.Contains(stderr, tc.errSubstr) {
				t.Fatalf("stderr missing %q: %q", tc.errSubstr, stderr)
			}
		})
	}
}

func TestMainDispatchRemainingCommands(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	withCwd(t, t.TempDir())

	dataDir := t.TempDir()
	t.Setenv("POSTGRAM_DATA_DIR", dataDir)
	databaseURL := testutil.NewPostgresURL(t)
	t.Setenv("POSTGRAM_DATABASE_URL", databaseURL)

	seedCfg, scErr := store.DefaultConfig()
	if scErr != nil {
		t.Fatalf("DefaultConfig: %v", scErr)
	}
	seedCfg.DatabaseURL = databaseURL
	mustSeedObservation(t, seedCfg, "s-main", "main-proj", "note", "focus", "focus content", "project")

	importFile := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(importFile, []byte(`{"version":"0.1.0","exported_at":"2026-01-01T00:00:00Z","sessions":[],"observations":[],"prompts":[]}`), 0644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "search", args: []string{"postgram", "search", "focus"}},
		{name: "save", args: []string{"postgram", "save", "t", "c"}},
		{name: "timeline", args: []string{"postgram", "timeline", mustSeedObservation(t, seedCfg, "s-main-tl", "main-proj", "note", "focus-timeline", "focus timeline content", "project")}},
		{name: "context", args: []string{"postgram", "context", "main-proj"}},
		{name: "stats", args: []string{"postgram", "stats"}},
		{name: "export", args: []string{"postgram", "export", filepath.Join(t.TempDir(), "exp.json")}},
		{name: "import", args: []string{"postgram", "import", importFile}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, tc.args...)
			_, stderr, recovered := captureOutputAndRecover(t, func() { main() })
			if recovered != nil {
				t.Fatalf("main panic for %s: %v stderr=%q", tc.name, recovered, stderr)
			}
		})
	}
}

func TestCmdImportStoreImportFailure(t *testing.T) {
	stubExitWithPanic(t)
	cfg := testConfig(t)

	badImport := filepath.Join(t.TempDir(), "bad-import.json")
	badJSON := `{
		"version":"0.1.0",
		"exported_at":"2026-01-01T00:00:00Z",
		"sessions":[],
		"observations":[{"id":"obsrow-bad","session_id":"missing-session","type":"note","title":"x","content":"y","scope":"project","revision_count":1,"duplicate_count":1,"created_at":"2026-01-01 00:00:00","updated_at":"2026-01-01 00:00:00"}],
		"prompts":[]
	}`
	if err := os.WriteFile(badImport, []byte(badJSON), 0644); err != nil {
		t.Fatalf("write bad import: %v", err)
	}

	withArgs(t, "postgram", "import", badImport)
	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "import observation") {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestCmdSearchAndSaveDanglingFlags(t *testing.T) {
	cfg := testConfig(t)

	withArgs(t, "postgram", "save", "dangling-title", "dangling-content", "--type")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSave(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("save with dangling flag failed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Memory saved:") {
		t.Fatalf("unexpected save output: %q", stdout)
	}

	withArgs(t, "postgram", "search", "dangling-content", "--limit", "not-a-number", "--project")
	stdout, stderr, recovered = captureOutputAndRecover(t, func() { cmdSearch(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("search with dangling flags failed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Found") {
		t.Fatalf("unexpected search output: %q", stdout)
	}
}

func TestCmdTimelineNoBeforeAfterSections(t *testing.T) {
	cfg := testConfig(t)
	focusID := mustSeedObservation(t, cfg, "solo-session", "solo", "note", "focus", "only content", "project")

	withArgs(t, "postgram", "timeline", focusID, "--before", "0", "--after", "0")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("timeline failed: panic=%v stderr=%q", recovered, stderr)
	}
	if strings.Contains(stdout, "─── Before ───") || strings.Contains(stdout, "─── After ───") {
		t.Fatalf("unexpected before/after sections in output: %q", stdout)
	}
}

func TestCmdStatsNoProjectsYet(t *testing.T) {
	cfg := testConfig(t)
	withArgs(t, "postgram", "stats")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdStats(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("stats failed: panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Projects:     none yet") {
		t.Fatalf("expected empty projects output, got: %q", stdout)
	}
}

func TestCommandErrorSeamsAndUncoveredBranches(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	cfg := testConfig(t)

	assertFatal := func(t *testing.T, stderr string, recovered any, want string) {
		t.Helper()
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, stderr)
		}
	}

	t.Run("search seam error", func(t *testing.T) {
		withArgs(t, "postgram", "search", "needle")
		storeSearch = func(*store.Store, string, store.SearchOptions) ([]store.SearchResult, error) {
			return nil, errors.New("forced search error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSearch(cfg) })
		assertFatal(t, stderr, recovered, "forced search error")
	})

	t.Run("save seam error", func(t *testing.T) {
		withArgs(t, "postgram", "save", "title", "content")
		storeAddObservation = func(*store.Store, store.AddObservationParams) (string, error) {
			return "", errors.New("forced save error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSave(cfg) })
		assertFatal(t, stderr, recovered, "forced save error")
	})

	t.Run("timeline seam error", func(t *testing.T) {
		obsID := "11111111-1111-1111-1111-111111111111"
		withArgs(t, "postgram", "timeline", obsID)
		storeTimeline = func(*store.Store, string, int, int) (*store.TimelineResult, error) {
			return nil, errors.New("forced timeline error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
		assertFatal(t, stderr, recovered, "forced timeline error")
	})

	t.Run("timeline prints session summary", func(t *testing.T) {
		summary := "this session has a non-empty summary"
		obsID := "11111111-1111-1111-1111-111111111111"
		withArgs(t, "postgram", "timeline", obsID)
		storeTimeline = func(*store.Store, string, int, int) (*store.TimelineResult, error) {
			return &store.TimelineResult{
				Focus:        store.Observation{ID: obsID, Type: "note", Title: "focus", Content: "content", CreatedAt: "2026-01-01"},
				SessionInfo:  &store.Session{Project: "proj", StartedAt: "2026-01-01", Summary: &summary},
				TotalInRange: 1,
			}, nil
		}
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected successful timeline render, panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, "Session: proj") || !strings.Contains(stdout, "non-empty summary") {
			t.Fatalf("expected summary in timeline output, got: %q", stdout)
		}
	})

	t.Run("context seam error", func(t *testing.T) {
		withArgs(t, "postgram", "context")
		storeFormatContext = func(*store.Store, string, string) (string, error) {
			return "", errors.New("forced context error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdContext(cfg) })
		assertFatal(t, stderr, recovered, "forced context error")
	})

	t.Run("stats seam error", func(t *testing.T) {
		withArgs(t, "postgram", "stats")
		storeStats = func(*store.Store) (*store.Stats, error) {
			return nil, errors.New("forced stats error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdStats(cfg) })
		assertFatal(t, stderr, recovered, "forced stats error")
	})

	t.Run("export seam error", func(t *testing.T) {
		withArgs(t, "postgram", "export")
		storeExport = func(*store.Store) (*store.ExportData, error) {
			return nil, errors.New("forced export error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
		assertFatal(t, stderr, recovered, "forced export error")
	})

	t.Run("export marshal seam error", func(t *testing.T) {
		withArgs(t, "postgram", "export")
		storeExport = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
		jsonMarshalIndent = func(any, string, string) ([]byte, error) {
			return nil, errors.New("forced marshal error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
		assertFatal(t, stderr, recovered, "forced marshal error")
	})

}

func TestCmdServeMCPToolsFilter(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Run("tools filter from env uses newMCPServerWithTools", func(t *testing.T) {
		var gotAllowlist map[string]bool
		newMCPServerWithTools = func(s *store.Store, allowlist map[string]bool) *mcpserver.MCPServer {
			gotAllowlist = allowlist
			return mcpserver.NewMCPServer("test", "0")
		}
		t.Setenv("POSTGRAM_MCP_TOOLS", "agent")
		withArgs(t, "postgram", "serve")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if gotAllowlist == nil {
			t.Fatal("expected newMCPServerWithTools to be called with non-nil allowlist")
		}
	})
}

func TestCmdServeMCPHTTPTransport(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Run("http transport mounts MCP handler", func(t *testing.T) {
		t.Setenv("POSTGRAM_MCP_HTTP_PATH", "/mcp")
		t.Setenv("POSTGRAM_MCP_AUTH_ENABLED", "false")

		seenMCPHTTP := false
		newMCPHTTPServer = func(_ *mcpserver.MCPServer, _ ...mcpserver.StreamableHTTPOption) *mcpserver.StreamableHTTPServer {
			seenMCPHTTP = true
			return mcpserver.NewStreamableHTTPServer(mcpserver.NewMCPServer("test", "0"))
		}

		withArgs(t, "postgram", "serve")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
		if recovered != nil {
			t.Fatalf("expected no panic, got %v stderr=%q", recovered, stderr)
		}
		if !seenMCPHTTP {
			t.Fatal("expected HTTP MCP server to be created")
		}
	})

	t.Run("oidc enabled with missing issuer fatals", func(t *testing.T) {
		t.Setenv("POSTGRAM_MCP_AUTH_ENABLED", "true")
		t.Setenv("POSTGRAM_OIDC_ISSUER", "")
		t.Setenv("POSTGRAM_OIDC_AUDIENCE", "postgram-mcp")

		withArgs(t, "postgram", "serve")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, "configure OIDC verifier") {
			t.Fatalf("expected oidc config error, got %q", stderr)
		}
	})
}

func TestCmdServeMCPHTTPAuthMetadataAndChallenge(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())

	issuer := "https://issuer.example"
	audience := "postgram-mcp"
	jwksPath := "/jwks.json"
	var jwksServer *httptest.Server
	jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
	defer jwksServer.Close()

	t.Setenv("POSTGRAM_MCP_HTTP_PATH", "/mcp")
	t.Setenv("POSTGRAM_MCP_AUTH_ENABLED", "true")
	t.Setenv("POSTGRAM_OIDC_ISSUER", issuer)
	t.Setenv("POSTGRAM_OIDC_AUDIENCE", audience)
	t.Setenv("POSTGRAM_OIDC_JWKS_URL", jwksServer.URL+jwksPath)
	t.Setenv("POSTGRAM_OIDC_REQUIRED_SCOPE", "postgram.mcp")
	t.Setenv("POSTGRAM_BASE_URL", "https://mcp.example.com")
	t.Setenv("POSTGRAM_OAUTH_AUTHORIZATION_SERVERS", "https://auth.example.com")

	withArgs(t, "postgram", "serve")

	var handler http.Handler
	newHTTPServer = func(s *store.Store, port int, version string) *postgramsrv.Server {
		srv := postgramsrv.New(s, port, version)
		handler = srv.Handler()
		return srv
	}
	startHTTP = func(_ *postgramsrv.Server) error { return nil }

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected no panic, got %v stderr=%q", recovered, stderr)
	}
	if handler == nil {
		t.Fatal("expected server handler to be captured")
	}

	mdReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	mdRec := httptest.NewRecorder()
	handler.ServeHTTP(mdRec, mdReq)
	if mdRec.Code != http.StatusOK {
		t.Fatalf("expected metadata 200, got %d body=%s", mdRec.Code, mdRec.Body.String())
	}
	var md map[string]any
	if err := json.NewDecoder(mdRec.Body).Decode(&md); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if md["resource"] != "http://localhost:7437/mcp" && md["resource"] != "http://127.0.0.1:7437/mcp" {
		t.Fatalf("unexpected resource: %v", md["resource"])
	}

	unauthReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthRec.Code)
	}
	www := unauthRec.Header().Get("WWW-Authenticate")
	if !strings.Contains(www, `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("expected resource_metadata challenge, got %q", www)
	}

	noScope := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"exp": time.Now().Add(5 * time.Minute).Unix(),
		"iat": time.Now().Add(-1 * time.Minute).Unix(),
	})
	noScope.Header["kid"] = "k1"
	noScopeToken, err := noScope.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	forbiddenReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	forbiddenReq.Header.Set("Content-Type", "application/json")
	forbiddenReq.Header.Set("Authorization", "Bearer "+noScopeToken)
	forbiddenRec := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for insufficient scope, got %d", forbiddenRec.Code)
	}
}
