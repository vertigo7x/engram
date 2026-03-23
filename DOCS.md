# Engram

**Persistent memory for AI coding agents**

> *Engram* is a neuroscience term for the physical trace of a memory in the brain.

## What is Engram?

An agent-agnostic persistent memory system. A Go binary backed by PostgreSQL, exposed via CLI and HTTP API, including MCP over HTTP for remote and team-oriented use.

**Why Go?** Single binary, cross-platform, no runtime dependencies. Uses PostgreSQL via `github.com/lib/pq` and serves CLI, HTTP, MCP, and TUI flows from one binary.

- **Module**: `github.com/Gentleman-Programming/engram`
- **Version**: 0.1.0

---

## Architecture

The Go binary is the brain. Agents talk to it via HTTP, including MCP over HTTP.

```
Agent (Claude Code/Cursor/Gemini/Codex/VS Code/etc.)
    ↓ MCP over HTTP
Engram Go Binary / Service
    ↓
PostgreSQL (`ENGRAM_DATABASE_URL`)
```

Six interfaces:

1. **CLI** — Direct terminal usage (`engram search`, `engram save`, etc.)
2. **HTTP API** — REST API on port 7437 for integrations and automation
3. **MCP Server** — HTTP transport for any MCP-compatible agent
4. **TUI** — Interactive terminal UI for browsing memories (`engram tui`)

---

## Project Structure

```
engram/
├── cmd/engram/main.go              # CLI entrypoint — all commands
├── internal/
│   ├── store/store.go              # Core data layer: PostgreSQL + search + sync journal
│   ├── server/server.go            # HTTP REST API server (port 7437)
│   ├── mcp/mcp.go                  # MCP tool registry (13 tools)
│   ├── sync/sync.go                # Git sync: manifest + chunks (gzipped JSONL)
│   └── tui/                        # Bubbletea terminal UI
│       ├── model.go                # Screen constants, Model struct, Init(), custom messages
│       ├── styles.go               # Lipgloss styles (Catppuccin Mocha palette)
│       ├── update.go               # Update(), handleKeyPress(), per-screen handlers
│       └── view.go                 # View(), per-screen renderers
├── skills/
│   └── gentleman-bubbletea/
│       └── SKILL.md                # Bubbletea TUI patterns reference
├── DOCS.md
├── go.mod
├── go.sum
└── .gitignore
```

---

## Database Schema

### Tables

- **sessions** — `id` (UUID PK, deterministic effective session id), `client_session_id`, `project`, `directory`, `auth_issuer`, `auth_subject`, `auth_username`, `auth_email`, `started_at`, `ended_at`, `summary`, `status`
- **observations** — `id` (UUID PK), `session_id` (FK), `sync_id` (UUID), `type`, `title`, `content`, `tool_name`, `project`, `scope`, `topic_key`, `normalized_hash`, `revision_count`, `duplicate_count`, `last_seen_at`, `created_at`, `updated_at`, `deleted_at`
- **user_prompts** — `id` (UUID PK), `session_id` (FK), `sync_id` (UUID), `content`, `project`, `created_at`
- **sync_chunks** — `chunk_id` (TEXT PK), `imported_at` — tracks which chunks have been imported to prevent duplicates

### PostgreSQL Configuration

- Set `ENGRAM_DATABASE_URL=postgres://...`
- `ENGRAM_DB_DRIVER` is deprecated and ignored; configure `ENGRAM_DATABASE_URL` instead
- Search uses PostgreSQL `ILIKE` queries today
- `sessions.id` is deterministic:
  - authenticated: derived from `iss + sub + client_session_id`
  - unauthenticated: derived from raw `client_session_id`

### MCP HTTP Transport

- `engram serve` exposes MCP over HTTP by default
- Endpoint path is configured with `ENGRAM_MCP_HTTP_PATH` (default `/mcp`)
- Tool profile/filter is configured with `ENGRAM_MCP_TOOLS` (for example `agent`, `admin`, or `agent,admin`)

### MCP OIDC JWT Authentication

- Enable with `ENGRAM_MCP_AUTH_ENABLED=true`
- Required config:
  - `ENGRAM_OIDC_ISSUER`
  - `ENGRAM_OIDC_AUDIENCE`
- Optional config:
  - `ENGRAM_OIDC_JWKS_URL` (if omitted, discovery uses `/.well-known/openid-configuration`)
  - `ENGRAM_OIDC_REQUIRED_SCOPE`
- Middleware expects `Authorization: Bearer <token>` and validates issuer/audience/signature (+ optional scope)
- On auth failures, MCP HTTP returns OAuth-style `WWW-Authenticate: Bearer ...` including `resource_metadata`.
- Exposes OAuth Protected Resource Metadata (RFC 9728) at `/.well-known/oauth-protected-resource`.

---

## CLI Commands

```
engram serve [port]       Start HTTP API server (default: 7437)
engram serve [port]       Start HTTP API + MCP over HTTP (default: 7437)
engram tui                Launch interactive terminal UI
engram search <query>     Search memories [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]
engram save <title> <msg> Save a memory [--type TYPE] [--project PROJECT] [--scope SCOPE] [--topic TOPIC_KEY]
engram timeline <obs_id>  Show chronological context around an observation [--before N] [--after N]
engram context [project]  Show recent context from previous sessions
engram stats              Show memory system statistics
engram export [file]      Export all memories to JSON (default: engram-export.json)
engram import <file>      Import memories from a JSON export file
engram sync               Export new memories as chunk [--import] [--status] [--project NAME] [--all]
engram version            Print version
engram help               Show help
```

### Environment Variables

| Variable | Description | Default |
|---|---|---|
| `ENGRAM_DATA_DIR` | Override data directory | `~/.engram` |
| `ENGRAM_DATABASE_URL` | PostgreSQL connection URL (required) | empty |
| `ENGRAM_HOST` | HTTP bind host | `127.0.0.1` |
| `ENGRAM_PORT` | Override HTTP server port | `7437` |
| `ENGRAM_MCP_URL` | Explicit MCP URL used by generated agent configs | empty |
| `ENGRAM_MCP_HTTP_PATH` | MCP HTTP endpoint path | `/mcp` |
| `ENGRAM_MCP_TOOLS` | MCP tool profile/filter | `agent` |
| `ENGRAM_MCP_AUTH_ENABLED` | Enable OIDC JWT auth for MCP HTTP | `false` |
| `ENGRAM_OIDC_ISSUER` | OIDC issuer URL (required when auth enabled) | empty |
| `ENGRAM_OIDC_AUDIENCE` | OIDC audience (required when auth enabled) | empty |
| `ENGRAM_OIDC_JWKS_URL` | Optional JWKS URL override | empty |
| `ENGRAM_OIDC_REQUIRED_SCOPE` | Optional required scope | empty |
| `ENGRAM_BASE_URL` | Public base URL for metadata/challenges (for ingress) | empty |
| `ENGRAM_OAUTH_RESOURCE_METADATA_PATH` | OAuth protected resource metadata path | `/.well-known/oauth-protected-resource` |
| `ENGRAM_OAUTH_RESOURCE` | OAuth resource identifier (defaults to MCP URL) | empty |
| `ENGRAM_OAUTH_AUTHORIZATION_SERVERS` | Comma-separated auth server URLs for PRM | `ENGRAM_OIDC_ISSUER` |

---

## Container and Kubernetes Deployment

### Docker

```bash
docker build -t engram:local .

docker run --rm -p 7437:7437 \
  -e ENGRAM_DATABASE_URL="postgres://user:pass@postgres:5432/engram?sslmode=disable" \
  engram:local
```

### Helm

Chart location: `charts/engram`

```bash
helm install engram ./charts/engram

helm install engram ./charts/engram \
  --set database.url="postgres://user:pass@postgres:5432/engram?sslmode=disable"
```

For public deployments, enable MCP auth with OIDC and expose a stable `ENGRAM_BASE_URL` so clients receive correct `resource_metadata` URLs in `WWW-Authenticate` challenges.

### Kubernetes Secrets for Database URL

Avoid hardcoding `database.url` in shared values files. The chart supports secret-based DB URL injection:

- `database.existingSecret`: existing Secret name containing DB URL
- `database.urlSecretKey`: key name inside the Secret (default `ENGRAM_DATABASE_URL`)
- `database.createSecret`: set `true` to have Helm create the Secret from `database.url`

Recommended (pre-created Secret):

```bash
kubectl create secret generic engram-db \
  --from-literal=ENGRAM_DATABASE_URL='postgres://user:pass@postgres:5432/engram?sslmode=require' \
  -n engram

helm upgrade --install engram ./charts/engram -n engram \
  --set database.existingSecret=engram-db \
  --set database.urlSecretKey=ENGRAM_DATABASE_URL
```

### Keycloak Provider Setup (Local Example)

Reference setup for remote MCP HTTP:

1. Keycloak realm (example: `Shared`).
2. MCP OAuth client (example: `engram-local`):
   - Standard Flow enabled
   - PKCE S256 enabled
   - Redirect URIs:
     - `http://127.0.0.1:*/mcp/oauth/callback`
     - `http://localhost:*/mcp/oauth/callback`
3. Client scope (example: `mcp:tools`) with Audience mapper:
   - Included Custom Audience: `engram-mcp`
4. Assign scope to client (default or requested explicitly).

Engram env alignment example:

```bash
ENGRAM_MCP_AUTH_ENABLED=true
ENGRAM_OIDC_ISSUER=http://localhost:28080/realms/Shared
ENGRAM_OIDC_AUDIENCE=engram-mcp
ENGRAM_OIDC_JWKS_URL=http://host.docker.internal:28080/realms/Shared/protocol/openid-connect/certs
ENGRAM_BASE_URL=http://localhost:7437
ENGRAM_OAUTH_RESOURCE=http://localhost:7437/mcp
ENGRAM_OAUTH_AUTHORIZATION_SERVERS=http://localhost:28080/realms/Shared
```

Example client config snippet:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "engram_remote": {
      "type": "remote",
      "url": "http://localhost:7437/mcp",
      "enabled": true,
      "oauth": {
        "clientId": "engram-local",
        "scope": "openid profile mcp:tools"
      }
    }
  }
}
```

Debug commands:

```bash
curl -i http://localhost:7437/.well-known/oauth-protected-resource
# Replace with your client's auth/debug commands if supported
```

---

## Running as a Service

### Using systemd

First you need add your engram binary to use in a global way. By example: `/usr/bin`, `/usr/local/bin` or `~/.local/bin`.
In this documentation we will use `~/.local/bin`.

1. First, move binary to `~/.local/bin` (Check if this is in your $PATH variable).
2. Create a directory for you service with user scope and engram data: `mkdir -p ~/.engram ~/.config/systemd/user`.
3. Create your service file in the following path: `~/.config/systemd/user/engram.service`.
4. Reload service list: `systemctl --user daemon-reload`.
5. Enable your service: `systemctl --user enable engram`.
6. Then start it: `systemctl --user start engram`.
7. And finally check the logs: `journalctl --user -u engram -f`.

The following code is an example of the `~/.config/systemd/user/engram.service` file:

```shell
[Unit]
Description=Engram Memory Server
After=network.target

[Service]
WorkingDirectory=%h
ExecStart=%h/.local/bin/engram serve
Restart=always
RestartSec=3
Environment=ENGRAM_DATA_DIR=%h/.engram

[Install]
WantedBy=default.target
```

---

## Terminal UI (TUI)

Interactive Bubbletea-based terminal UI. Launch with `engram tui`.

Built with [Bubbletea](https://github.com/charmbracelet/bubbletea) v1, [Lipgloss](https://github.com/charmbracelet/lipgloss), and [Bubbles](https://github.com/charmbracelet/bubbles) components. Follows the Gentleman Bubbletea skill patterns.

### Screens

| Screen | Description |
|---|---|
| **Dashboard** | Stats overview (sessions, observations, prompts, projects) + menu |
| **Search** | Case-insensitive text search with text input |
| **Search Results** | Browsable results list from search |
| **Recent Observations** | Browse all observations, newest first |
| **Observation Detail** | Full content of a single observation, scrollable |
| **Timeline** | Chronological context around an observation (before/after) |
| **Sessions** | Browse all sessions |
| **Session Detail** | Observations within a specific session |

### Navigation

- `j/k` or `↑/↓` — Navigate lists
- `Enter` — Select / drill into detail
- `t` — View timeline for selected observation
- `s` or `/` — Quick search from any screen
- `Esc` or `q` — Go back / quit
- `Ctrl+C` — Force quit

### Visual Features

- **Catppuccin Mocha** color palette
- **`(active)` badge** — shown next to sessions and observations from active (non-completed) sessions, sorted to the top of every list
- **Scroll indicators** — shows position in long lists (e.g. "showing 1-20 of 50")
- **2-line items** — each observation shows title + content preview

### Architecture (Gentleman Bubbletea patterns)

- `model.go` — Screen constants as `Screen int` iota, single `Model` struct holds ALL state
- `styles.go` — Lipgloss styles organized by concern (layout, dashboard, list, detail, timeline, search)
- `update.go` — `Update()` with type switch, `handleKeyPress()` routes to per-screen handlers, each returns `(tea.Model, tea.Cmd)`
- `view.go` — `View()` routes to per-screen renderers, shared `renderObservationListItem()` for consistent list formatting

### Store Methods (TUI-specific)

The TUI uses dedicated store methods that don't filter by session status (unlike `RecentSessions`/`RecentObservations` which only show completed sessions for MCP context injection):

- `AllSessions()` — All sessions regardless of status, active sorted first
- `AllObservations()` — All observations regardless of session status, active sorted first
- `SessionObservations(sessionID)` — All observations for a specific session, chronological order

---

## HTTP API Endpoints

All endpoints return JSON. Server listens on `127.0.0.1:7437`.

### Health

- `GET /health` — Returns `{"status": "ok", "service": "engram", "version": "0.1.0"}`

### Sessions

- `POST /sessions` — Create session. Body: `{id, project, directory}` where `id` is the client-provided session id; the response returns the effective stored session id.
- `POST /sessions/{id}/end` — End session. Body: `{summary}`
- `GET /sessions/recent` — Recent sessions. Query: `?project=X&limit=N`

### Observations

- `POST /observations` — Add observation. Body: `{session_id, type, title, content, tool_name?, project?, scope?, topic_key?}` where `session_id` is the effective stored session id returned by session creation.
- `GET /observations/recent` — Recent observations. Query: `?project=X&scope=project|personal&limit=N`
- `GET /observations/{id}` — Get single observation by ID
- `PATCH /observations/{id}` — Update fields. Body: `{title?, content?, type?, project?, scope?, topic_key?}`
- `DELETE /observations/{id}` — Delete observation (`?hard=true` for hard delete, soft delete by default)

### Search

- `GET /search` — Text search. Query: `?q=QUERY&type=TYPE&project=PROJECT&scope=SCOPE&limit=N`

### Timeline

- `GET /timeline` — Chronological context. Query: `?observation_id=N&before=5&after=5`

### Prompts

- `POST /prompts` — Save user prompt. Body: `{session_id, content, project?}` where `session_id` is the effective stored session id returned by session creation.
- `GET /prompts/recent` — Recent prompts. Query: `?project=X&limit=N`
- `GET /prompts/search` — Search prompts. Query: `?q=QUERY&project=X&limit=N`

### Context

- `GET /context` — Formatted context. Query: `?project=X&scope=project|personal`

### Export / Import

- `GET /export` — Export all data as JSON
- `POST /import` — Import data from JSON. Body: ExportData JSON

### Stats

- `GET /stats` — Memory statistics

### Sync Status


---

## MCP Tools (13 tools)

### mem_search

Search persistent memory across all sessions. Supports text search with type/project/scope/limit filters.

### mem_save

Save structured observations. The tool description teaches agents the format:

- **title**: Short, searchable (e.g. "JWT auth middleware")
- **type**: `decision` | `architecture` | `bugfix` | `pattern` | `config` | `discovery` | `learning`
- **scope**: `project` (default) | `personal`
- **topic_key**: optional canonical topic id (e.g. `architecture/auth-model`) used to upsert evolving memories
- **content**: Structured with `**What**`, `**Why**`, `**Where**`, `**Learned**`

Exact duplicate saves are deduplicated in a rolling time window using a normalized content hash + project + scope + type + title.
When `topic_key` is provided, `mem_save` upserts the latest observation in the same `project + scope + topic_key`, incrementing `revision_count`.

### mem_update

Update an observation by ID. Supports partial updates for `title`, `content`, `type`, `project`, `scope`, and `topic_key`.

### mem_suggest_topic_key

Suggest a stable `topic_key` from `type + title` (or content fallback). Uses family heuristics like `architecture/*`, `bug/*`, `decision/*`, etc. Use before `mem_save` when you want evolving topics to upsert into a single observation.

### mem_delete

Delete an observation by ID. Uses soft-delete by default (`deleted_at`); optional hard-delete for permanent removal.

### mem_save_prompt

Save user prompts — records what the user asked so future sessions have context about user goals.

### mem_context

Get recent memory context from previous sessions — shows sessions, prompts, and observations, with optional scope filtering for observations.

### mem_stats

Show memory system statistics — sessions, observations, prompts, projects.

### mem_timeline

Progressive disclosure: after searching, drill into chronological context around a specific observation. Shows N observations before and after within the same session.

### mem_get_observation

Get full untruncated content of a specific observation by ID.

### mem_session_summary

Save comprehensive end-of-session summary using OpenCode-style format:

```
## Goal
## Instructions
## Discoveries
## Accomplished (✅ done, 🔲 pending)
## Relevant Files
```

### mem_session_start

Register the start of a new coding session.

### mem_session_end

Mark a session as completed with optional summary.

---

## MCP Configuration

Add to any agent's config:

```json
{
  "mcp": {
    "engram": {
      "type": "remote",
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

For local-only setups, `http://127.0.0.1:7437/mcp` is still valid. For team/shared deployments, use your public Engram URL.



---

## Memory Protocol Full Text

The Memory Protocol teaches agents **when** and **how** to use Engram's MCP tools. Without it, the agent has the tools but no behavioral guidance. Add this to your agent's prompt file (see README for per-agent locations).

### WHEN TO SAVE (mandatory — not optional)

Call `mem_save` IMMEDIATELY after any of these:
- Bug fix completed
- Architecture or design decision made
- Non-obvious discovery about the codebase
- Configuration change or environment setup
- Pattern established (naming, structure, convention)
- User preference or constraint learned

Format for `mem_save`:
- **title**: Verb + what — short, searchable (e.g. "Fixed N+1 query in UserList", "Chose Zustand over Redux")
- **type**: `bugfix` | `decision` | `architecture` | `discovery` | `pattern` | `config` | `preference`
- **scope**: `project` (default) | `personal`
- **topic_key** (optional, recommended for evolving decisions): stable key like `architecture/auth-model`
- **content**:
  ```
  **What**: One sentence — what was done
  **Why**: What motivated it (user request, bug, performance, etc.)
  **Where**: Files or paths affected
  **Learned**: Gotchas, edge cases, things that surprised you (omit if none)
  ```

### Topic update rules (mandatory)

- Different topics must not overwrite each other (e.g. architecture vs bugfix)
- Reuse the same `topic_key` to update an evolving topic instead of creating new observations
- If unsure about the key, call `mem_suggest_topic_key` first and then reuse it
- Use `mem_update` when you have an exact observation ID to correct

### WHEN TO SEARCH MEMORY

When the user asks to recall something — any variation of "remember", "recall", "what did we do", "how did we solve", "recordar", "acordate", "qué hicimos", or references to past work:
1. First call `mem_context` — checks recent session history (fast, cheap)
2. If not found, call `mem_search` with relevant keywords
3. If you find a match, use `mem_get_observation` for full untruncated content

Also search memory PROACTIVELY when:
- Starting work on something that might have been done before
- The user mentions a topic you have no context on — check if past sessions covered it

### SESSION CLOSE PROTOCOL (mandatory)

Before ending a session or saying "done" / "listo" / "that's it", you MUST call `mem_session_summary` with this structure:

```
## Goal
[What we were working on this session]

## Instructions
[User preferences or constraints discovered — skip if none]

## Discoveries
- [Technical findings, gotchas, non-obvious learnings]

## Accomplished
- [Completed items with key details]

## Next Steps
- [What remains to be done — for the next session]

## Relevant Files
- path/to/file — [what it does or what changed]
```

This is NOT optional. If you skip this, the next session starts blind.

### PASSIVE CAPTURE — automatic learning extraction

When completing a task or subtask, include a `## Key Learnings:` section at the end of your response with numbered items. Engram will automatically extract and save these as observations.

Example:
```
## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
```

You can also call `mem_capture_passive(content)` directly with any text that contains a learning section. This is a safety net — it captures knowledge even if you forget to call `mem_save` explicitly.

### AFTER COMPACTION

If you see a message about compaction or context reset, or if you see "FIRST ACTION REQUIRED" in your context:
1. IMMEDIATELY call `mem_session_summary` with the compacted summary content — this persists what was done before compaction
2. Then call `mem_context` to recover any additional context from previous sessions
3. Only THEN continue working

Do not skip step 1. Without it, everything done before compaction is lost from memory.

---

## Features

### 1. Search

- Searches across title, content, tool_name, type, and project
- Supports type and project filters

### 2. Timeline (Progressive Disclosure)

Three-layer pattern for token-efficient memory retrieval:

1. `mem_search` — Find relevant observations
2. `mem_timeline` — Drill into chronological neighborhood of a result
3. `mem_get_observation` — Get full untruncated content

### 3. Privacy Tags

`<private>...</private>` content is stripped at TWO levels:

1. **Agent/input layer** — clients should avoid sending secrets in the first place
2. **Store layer** (Go) — `stripPrivateTags()` runs inside `AddObservation()` and `AddPrompt()`

Example: `Set up API with <private>sk-abc123</private>` becomes `Set up API with [REDACTED]`

### 4. User Prompt Storage

Separate table captures what the USER asked (not just tool calls). Gives future sessions the "why" behind the "what" and participates in prompt search.

### 5. Export / Import

Share memories across machines, backup, or migrate:

- `engram export` — JSON dump of all sessions, observations, prompts
- `engram import <file>` — Load from JSON, sessions use INSERT OR IGNORE (skip duplicates), atomic transaction

### 6. Git Sync (Chunked)

Share memories through git repositories using compressed chunks with a manifest index.

- `engram sync` — Exports new memories as a gzipped JSONL chunk to `.engram/chunks/`
- `engram sync --all` — Exports ALL memories from every project (ignores directory-based filter)
- `engram sync --import` — Imports chunks listed in the manifest that haven't been imported yet
- `engram sync --status` — Shows how many chunks exist locally vs remotely, and how many are pending import
- `engram sync --project NAME` — Filters export to a specific project

**Architecture**:
```
.engram/
├── manifest.json          ← index of all chunks (small, git-mergeable)
├── chunks/
│   ├── a3f8c1d2.jsonl.gz ← chunk 1 (gzipped JSONL)
│   ├── b7d2e4f1.jsonl.gz ← chunk 2
│   └── ...
└── chunks/metadata tracked in PostgreSQL import state
```

**Why chunks?**
- Each `engram sync` creates a NEW chunk — old chunks are never modified
- No merge conflicts: each dev creates independent chunks, git just adds files
- Chunks are content-hashed (SHA-256 prefix) — each chunk is imported only once
- The manifest is the only file git diffs — it's small and append-only
- Compressed: a chunk with 8 sessions + 10 observations = ~2KB

**Import flow**: Any client or team workflow can run `engram sync --import` to load new chunks from `.engram/manifest.json` into the local database.

**Tracking**: The local DB stores a `sync_chunks` table with chunk IDs that have been imported. This prevents re-importing the same data if `sync --import` runs multiple times.

### 7. AI Compression (Agent-Driven)

Instead of a separate LLM service, the agent itself compresses observations. The agent already has the model, context, and API key.

**Two levels:**

- **Per-action** (`mem_save`): Structured summaries after each significant action

  ```
  **What**: [what was done]
  **Why**: [reasoning]
  **Where**: [files affected]
  **Learned**: [gotchas, decisions]
  ```

- **Session summary** (`mem_session_summary`): Comprehensive structured summary

  ```
  ## Goal
  ## Instructions
  ## Discoveries
  ## Accomplished
  ## Relevant Files
  ```

The recommended setup is to add the **Memory Protocol** to your agent instructions so it learns both formats, plus strict rules about when to save and a mandatory session close protocol.

### 8. No Raw Auto-Capture (Agent-Only Memory)

Engram does NOT auto-capture raw tool calls. All memory comes from the agent itself:

- **`mem_save`** — Agent saves structured observations after significant work (decisions, bugfixes, patterns)
- **`mem_session_summary`** — Agent saves comprehensive end-of-session summaries

**Why?** Raw tool calls (`edit: {file: "foo.go"}`, `bash: {command: "go build"}`) are noisy and pollute search results. The agent's curated summaries are higher signal, more searchable, and don't bloat the database. Shell history and git provide the raw audit trail.


## Dependencies

### Go

- `github.com/mark3labs/mcp-go v0.44.0` — MCP protocol implementation
- `github.com/charmbracelet/bubbletea v1.3.10` — Terminal UI framework
- `github.com/charmbracelet/lipgloss v1.1.0` — Terminal styling
- `github.com/charmbracelet/bubbles v1.0.0` — TUI components (textinput, etc.)
- `github.com/lib/pq` — PostgreSQL driver
- `github.com/golang-jwt/jwt/v5` — JWT token generation and validation (for cloud auth)
- `golang.org/x/crypto` — bcrypt password hashing (for cloud auth)


## Installation

### From source

```bash
git clone https://github.com/alanbuscaglia/engram.git
cd engram
go build -o engram ./cmd/engram
go install ./cmd/engram
```

### Binary location

After `go install`: `$GOPATH/bin/engram` (typically `~/go/bin/engram`)

### Database requirement

Set `ENGRAM_DATABASE_URL` before running Engram.

---

## Design Decisions

1. **Go over TypeScript** — Single binary, cross-platform, no runtime. The initial prototype was TS but was rewritten.
2. **PostgreSQL over multi-store complexity** — one operational database keeps CLI, HTTP, MCP, and sync flows on the same source of truth.
3. **Agent-agnostic core** — Go binary is the brain, exposed via standard HTTP and MCP HTTP. Not locked to any agent.
4. **Agent-driven compression** — The agent already has an LLM. No separate compression service.
5. **Privacy at the store boundary** — Strip private tags before persistence.
6. **Remote-first MCP over HTTP** — one transport works for local development and shared team deployments.
7. **No raw auto-capture** — Raw tool calls (edit, bash, etc.) are noisy, pollute search results, and bloat the database. The agent saves curated summaries via `mem_save` and `mem_session_summary` instead. Shell history and git provide the raw audit trail.
8. **TUI with Bubbletea** — Interactive terminal UI for browsing memories without leaving the terminal. Follows Gentleman Bubbletea patterns (screen constants, single Model struct, vim keys).

---

## Inspired By

[claude-mem](https://github.com/thedotmack/claude-mem) — But agent-agnostic and with a Go core instead of TypeScript.

Key differences from claude-mem:

- Agent-agnostic (not locked to Claude Code)
- Go binary (not Node.js/TypeScript)
- PostgreSQL-backed search instead of a separate vector store
- Agent-driven compression instead of separate LLM calls
- Simpler architecture (single binary, embedded web dashboard)
### Session Identity

- Clients send a `client_session_id` when creating or using a session.
- Engram derives the stored session primary key from `issuer + subject + client_session_id` when authenticated.
- This prevents different users from colliding on weak client ids like `manual-save`.
- In unauthenticated flows, the effective session id is deterministically derived from the client-provided session id.
