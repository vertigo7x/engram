# Project Structure

```
postgram/
├── cmd/postgram/
│   ├── main.go                  # CLI entrypoint — all subcommands (serve, tui, search, save, etc.)
│   ├── main_test.go             # CLI integration tests
│   └── main_extra_test.go       # Output capture + exit/panic test helpers
│
├── internal/
│   ├── store/
│   │   ├── store.go             # Core data layer — PostgreSQL + SQLite, all DB ops, migrations
│   │   ├── store_test.go        # Store unit tests (requires Postgres via dockertest)
│   │   └── normalize_test.go    # Tests for NormalizeRemoteURL / normalizeProject
│   │
│   ├── server/
│   │   ├── server.go            # HTTP REST API (port 7437) — all route handlers
│   │   ├── server_test.go       # Server unit tests
│   │   └── server_e2e_test.go   # E2E tests (build tag: e2e)
│   │
│   ├── mcp/
│   │   ├── mcp.go               # MCP tool registry — all 13 tools + tool profiles
│   │   └── mcp_test.go          # MCP tests
│   │
│   ├── auth/
│   │   ├── oidc.go              # OIDC/JWT validation, claims extraction
│   │   └── oidc_test.go
│   │
│   ├── tui/
│   │   ├── model.go             # Screen constants, Model struct, Init(), data-loading Cmds
│   │   ├── update.go            # Update() — input handling, per-screen key handlers
│   │   ├── view.go              # View() — rendering, per-screen views
│   │   └── styles.go            # Lipgloss styles (Catppuccin Mocha theme)
│   │
│   ├── version/
│   │   ├── check.go             # Version check against GitHub releases
│   │   └── check_test.go
│   │
│   ├── sync/                    # (reserved — sync package, currently empty)
│   │
│   └── testutil/
│       └── postgres.go          # Shared test helper — spins up postgres:16-alpine via dockertest
│
├── skills/                      # AI agent skill files (contributor guardrails)
│   ├── catalog.md               # Index of all skills
│   └── <skill-name>/SKILL.md    # One SKILL.md per skill
│
├── charts/postgram/             # Helm chart for Kubernetes deployment
├── assets/                      # Screenshots and images for docs
├── docs/                        # User-facing documentation
├── plugin/                      # Agent plugin configs (claude-code, opencode)
│
├── AGENTS.md                    # Agent skill index — load relevant skills before coding
├── go.mod / go.sum
├── Dockerfile                   # Multi-stage, distroless, CGO_ENABLED=0
└── setup.sh
```

## Key Conventions

### Package Responsibilities
- `internal/store` — owns ALL database access; no other package queries the DB directly
- `internal/server` — HTTP handlers only; delegates all data ops to `store`
- `internal/mcp` — MCP tool definitions and handlers; delegates to `store`
- `internal/tui` — Bubbletea UI only; reads from `store`, never writes HTTP
- `cmd/postgram` — wires everything together; no business logic

### TUI Pattern (Bubbletea)
- Screen constants as `iota` in `model.go`
- Single `Model` struct holds ALL state
- `Update()` in `update.go` with type switch on messages
- Per-screen key handlers returning `(tea.Model, tea.Cmd)`
- Data loading via `tea.Cmd` functions returning typed messages
- `PrevScreen` field for back navigation
- Vim keys (`j`/`k`) for navigation

### Testing
- Unit tests: same package (e.g. `package store`) — no build tag needed
- E2E tests: `//go:build e2e` tag, live in `internal/server/`
- All store tests use `internal/testutil.NewPostgresURL(t)` — never SQLite in tests
- Each test gets its own isolated Postgres database (created/dropped per test)

### MCP Tool Profiles
Tools are grouped into profiles in `internal/mcp/mcp.go`:
- `agent` — tools AI agents use during sessions (always-in-context + deferred)
- `admin` — tools for TUI/CLI/manual curation (`mem_delete`, `mem_stats`, `mem_timeline`)
- `all` (default) — every tool
- Controlled via `POSTGRAM_MCP_TOOLS` env var

### Project Identifier Normalization
The `project` field across all tools and the store is normalized to `host/owner/repo` format (e.g. `github.com/vertigo7x/postgram`). Both HTTPS and SSH remote URLs converge to the same identifier. Local paths fall back to the directory basename. See `internal/store/normalize_test.go` for the full contract.

### Skills
Skills in `skills/` define coding standards for contributors and AI agents. `AGENTS.md` maps task types to skill files. Load the relevant skill(s) before making changes — especially `architecture-guardrails`, `project-structure`, and `testing-coverage`.
