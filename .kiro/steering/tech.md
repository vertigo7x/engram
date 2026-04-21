# Tech Stack

## Language & Runtime
- **Go 1.25** — single binary, no runtime dependencies
- `CGO_ENABLED=0` for all builds (fully static)

## Key Libraries
| Library | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go` | MCP server/tool registry |
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | TUI styling (Catppuccin Mocha theme) |
| `github.com/charmbracelet/bubbles` | TUI components |
| `github.com/a-h/templ` | HTML templating (dashboard) |
| `github.com/lib/pq` | PostgreSQL driver |
| `modernc.org/sqlite` | SQLite driver (fallback/local) |
| `github.com/golang-jwt/jwt/v5` | JWT validation for OIDC auth |
| `github.com/leanovate/gopter` | Property-based testing |
| `github.com/ory/dockertest/v3` | Spin up Postgres containers in tests |
| `github.com/google/uuid` | UUID generation |

## Database
- Primary: **PostgreSQL** (via `POSTGRAM_DATABASE_URL`)
- Fallback: **SQLite** (`modernc.org/sqlite`, pure Go, no CGO)
- Schema migrations run automatically on `store.New()`

## Build & Run

```bash
# Build
go build -o postgram ./cmd/postgram

# Production build (matches Dockerfile)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o postgram ./cmd/postgram

# Run server
POSTGRAM_DATABASE_URL='postgres://user:pass@host:5432/postgram?sslmode=disable' ./postgram serve

# Docker
docker build -t postgram .
docker run --rm -p 7437:7437 -e POSTGRAM_DATABASE_URL=... postgram serve
```

## Testing

```bash
# Unit tests (no build tags — excludes e2e)
go test ./...

# E2E tests (requires Docker for Postgres container)
go test -tags e2e ./internal/server/...

# Single package
go test ./internal/store/...

# With a real Postgres (skip Docker spin-up)
POSTGRAM_TEST_DATABASE_URL='postgres://...' go test ./...
```

Test infrastructure:
- `internal/testutil/postgres.go` — spins up a `postgres:16-alpine` Docker container once per test run (via `dockertest`), creates an isolated database per test
- Set `POSTGRAM_TEST_DATABASE_URL` to skip Docker and use an existing Postgres instance
- Property-based tests use `gopter`

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `POSTGRAM_DATABASE_URL` | PostgreSQL connection string |
| `POSTGRAM_HOST` | Bind host (default `0.0.0.0` in Docker, `127.0.0.1` locally) |
| `POSTGRAM_PORT` | HTTP port (default `7437`) |
| `POSTGRAM_DATA_DIR` | Data directory for SQLite fallback |
| `POSTGRAM_MCP_AUTH_ENABLED` | Enable OIDC auth on `/mcp` |
| `POSTGRAM_OIDC_ISSUER` | OIDC issuer URL |
| `POSTGRAM_OIDC_AUDIENCE` | Expected JWT audience |
| `POSTGRAM_BASE_URL` | Public base URL for OAuth metadata |
| `POSTGRAM_OIDC_JWKS_URL` | Override JWKS discovery URL |
| `POSTGRAM_OIDC_REQUIRED_SCOPE` | Require a specific scope (e.g. `mcp:tools`) |
| `POSTGRAM_TEST_DATABASE_URL` | Postgres URL for tests (skips Docker) |

## CI
- GitHub Actions: `Unit Tests` (`go test ./...`) and `E2E Tests` (`go test -tags e2e ./internal/server/...`)
- Both must pass before merge
