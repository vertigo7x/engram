# Engram

**Persistent memory for AI coding agents**

> *Engram* is a neuroscience term for the physical trace of a memory in the brain.

## What is Engram?

An agent-agnostic persistent memory system. A Go binary with relational storage (SQLite by default, PostgreSQL optional), exposed via CLI, HTTP API, and MCP server. Thin adapter plugins connect it to specific agents (OpenCode, Claude Code, Cursor, Windsurf, etc.).

**Why Go?** Single binary, cross-platform, no runtime dependencies. Uses `modernc.org/sqlite` (pure Go, no CGO) and optional PostgreSQL via `github.com/lib/pq`.

- **Module**: `github.com/Gentleman-Programming/engram`
- **Version**: 0.1.0

---

## Architecture

The Go binary is the brain. Thin adapter plugins per-agent talk to it via HTTP or MCP stdio.

```
Agent (OpenCode/Claude Code/Cursor/etc.)
    ↓ (plugin or MCP)
Engram Go Binary / Service
    ↓
Relational DB (SQLite default / PostgreSQL optional)
```

Six interfaces:

1. **CLI** — Direct terminal usage (`engram search`, `engram save`, etc.)
2. **HTTP API** — REST API on port 7437 for plugins and integrations
3. **MCP Server** — stdio transport for any MCP-compatible agent
4. **TUI** — Interactive terminal UI for browsing memories (`engram tui`)

---

## Project Structure

```
engram/
├── cmd/engram/main.go              # CLI entrypoint — all commands
├── internal/
│   ├── store/store.go              # Core data layer: SQLite/PostgreSQL + search + sync journal
│   ├── server/server.go            # HTTP REST API server (port 7437)
│   ├── mcp/mcp.go                  # MCP stdio server (13 tools)
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

- **sessions** — `id` (TEXT PK), `project`, `directory`, `started_at`, `ended_at`, `summary`, `status`
- **observations** — `id` (INTEGER PK AUTOINCREMENT), `session_id` (FK), `type`, `title`, `content`, `tool_name`, `project`, `scope`, `topic_key`, `normalized_hash`, `revision_count`, `duplicate_count`, `last_seen_at`, `created_at`, `updated_at`, `deleted_at`
- **observations_fts** — FTS5 virtual table synced via triggers (`title`, `content`, `tool_name`, `type`, `project`)
- **user_prompts** — `id` (INTEGER PK AUTOINCREMENT), `session_id` (FK), `content`, `project`, `created_at`
- **prompts_fts** — FTS5 virtual table synced via triggers (`content`, `project`)
- **sync_chunks** — `chunk_id` (TEXT PK), `imported_at` — tracks which chunks have been imported to prevent duplicates

### SQLite Configuration

- WAL mode for concurrent reads
- Busy timeout 5000ms
- Synchronous NORMAL
- Foreign keys ON

### PostgreSQL Mode

- Set `ENGRAM_DB_DRIVER=postgres`
- Set `ENGRAM_DATABASE_URL=postgres://...`
- Uses compatibility rewrites for SQL placeholders/functions
- Uses `ILIKE` search fallback (SQLite keeps FTS5 virtual tables)

### MCP HTTP Transport

- `engram serve` can expose MCP over HTTP when `ENGRAM_MCP_TRANSPORT=http`
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
engram mcp                Start MCP server (stdio transport)
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
| `ENGRAM_DB_DRIVER` | Database driver (`sqlite` or `postgres`) | `sqlite` |
| `ENGRAM_DATABASE_URL` | PostgreSQL connection URL (required when driver is `postgres`) | empty |
| `ENGRAM_HOST` | HTTP bind host | `127.0.0.1` |
| `ENGRAM_PORT` | Override HTTP server port | `7437` |
| `ENGRAM_MCP_TRANSPORT` | MCP transport in `engram serve` (`http` to enable) | `stdio` |
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

# SQLite mode
docker run --rm -p 7437:7437 -v engram-data:/data engram:local

# PostgreSQL mode
docker run --rm -p 7437:7437 \
  -e ENGRAM_DB_DRIVER=postgres \
  -e ENGRAM_DATABASE_URL="postgres://user:pass@postgres:5432/engram?sslmode=disable" \
  engram:local
```

### Helm

Chart location: `charts/engram`

```bash
helm install engram ./charts/engram

# PostgreSQL mode
helm install engram ./charts/engram \
  --set database.driver=postgres \
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
  --set database.driver=postgres \
  --set database.existingSecret=engram-db \
  --set database.urlSecretKey=ENGRAM_DATABASE_URL
```

### Keycloak Provider Setup (Local Example)

Reference setup for OpenCode + remote MCP HTTP:

1. Keycloak realm (example: `Shared`).
2. OpenCode OAuth client (example: `engram-local`):
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
ENGRAM_MCP_TRANSPORT=http
ENGRAM_MCP_AUTH_ENABLED=true
ENGRAM_OIDC_ISSUER=http://localhost:28080/realms/Shared
ENGRAM_OIDC_AUDIENCE=engram-mcp
ENGRAM_OIDC_JWKS_URL=http://host.docker.internal:28080/realms/Shared/protocol/openid-connect/certs
ENGRAM_BASE_URL=http://localhost:7437
ENGRAM_OAUTH_RESOURCE=http://localhost:7437/mcp
ENGRAM_OAUTH_AUTHORIZATION_SERVERS=http://localhost:28080/realms/Shared
```

OpenCode config snippet:

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
opencode mcp auth engram_remote
opencode mcp debug engram_remote
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
| **Search** | FTS5 text search with text input |
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

- `POST /sessions` — Create session. Body: `{id, project, directory}`
- `POST /sessions/{id}/end` — End session. Body: `{summary}`
- `GET /sessions/recent` — Recent sessions. Query: `?project=X&limit=N`

### Observations

- `POST /observations` — Add observation. Body: `{session_id, type, title, content, tool_name?, project?, scope?, topic_key?}`
- `GET /observations/recent` — Recent observations. Query: `?project=X&scope=project|personal&limit=N`
- `GET /observations/{id}` — Get single observation by ID
- `PATCH /observations/{id}` — Update fields. Body: `{title?, content?, type?, project?, scope?, topic_key?}`
- `DELETE /observations/{id}` — Delete observation (`?hard=true` for hard delete, soft delete by default)

### Search

- `GET /search` — FTS5 search. Query: `?q=QUERY&type=TYPE&project=PROJECT&scope=SCOPE&limit=N`

### Timeline

- `GET /timeline` — Chronological context. Query: `?observation_id=N&before=5&after=5`

### Prompts

- `POST /prompts` — Save user prompt. Body: `{session_id, content, project?}`
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

Search persistent memory across all sessions. Supports FTS5 full-text search with type/project/scope/limit filters.

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
      "type": "stdio",
      "command": "engram",
      "args": ["mcp"]
    }
  }
}
```

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
2. If not found, call `mem_search` with relevant keywords (FTS5 full-text search)
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

### 1. Full-Text Search (FTS5)

- Searches across title, content, tool_name, type, and project
- Query sanitization: wraps each word in quotes to avoid FTS5 syntax errors
- Supports type and project filters

### 2. Timeline (Progressive Disclosure)

Three-layer pattern for token-efficient memory retrieval:

1. `mem_search` — Find relevant observations
2. `mem_timeline` — Drill into chronological neighborhood of a result
3. `mem_get_observation` — Get full untruncated content

### 3. Privacy Tags

`<private>...</private>` content is stripped at TWO levels:

1. **Plugin layer** (TypeScript) — Strips before data leaves the process
2. **Store layer** (Go) — `stripPrivateTags()` runs inside `AddObservation()` and `AddPrompt()`

Example: `Set up API with <private>sk-abc123</private>` becomes `Set up API with [REDACTED]`

### 4. User Prompt Storage

Separate table captures what the USER asked (not just tool calls). Gives future sessions the "why" behind the "what". Full FTS5 search support.

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
└── engram.db              ← local working DB (gitignored)
```

**Why chunks?**
- Each `engram sync` creates a NEW chunk — old chunks are never modified
- No merge conflicts: each dev creates independent chunks, git just adds files
- Chunks are content-hashed (SHA-256 prefix) — each chunk is imported only once
- The manifest is the only file git diffs — it's small and append-only
- Compressed: a chunk with 8 sessions + 10 observations = ~2KB

**Auto-import**: The OpenCode plugin detects `.engram/manifest.json` at startup and runs `engram sync --import` to load any new chunks. Clone a repo → open OpenCode → team memories are loaded.

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

- **Session summary** (`mem_session_summary`): OpenCode-style comprehensive summary

  ```
  ## Goal
  ## Instructions
  ## Discoveries
  ## Accomplished
  ## Relevant Files
  ```

The OpenCode plugin injects the **Memory Protocol** via system prompt to teach agents both formats, plus strict rules about when to save and a mandatory session close protocol.

### 8. No Raw Auto-Capture (Agent-Only Memory)

The OpenCode plugin does NOT auto-capture raw tool calls. All memory comes from the agent itself:

- **`mem_save`** — Agent saves structured observations after significant work (decisions, bugfixes, patterns)
- **`mem_session_summary`** — Agent saves comprehensive end-of-session summaries

**Why?** Raw tool calls (`edit: {file: "foo.go"}`, `bash: {command: "go build"}`) are noisy and pollute FTS5 search results. The agent's curated summaries are higher signal, more searchable, and don't bloat the database. Shell history and git provide the raw audit trail.

The plugin still counts tool calls per session (for session end summary stats) but doesn't persist them as observations.

---

## OpenCode Plugin

Install with `engram setup opencode` — this copies the plugin to `~/.config/opencode/plugins/engram.ts` AND auto-registers the MCP server in `opencode.json`.

A thin TypeScript adapter that:

1. **Auto-starts** the engram binary if not running
2. **Auto-imports** git-synced memories from `.engram/memories.json` if present in the project
3. **Captures events**: `session.created`, `session.idle`, `session.deleted`, `message.updated`
4. **Tracks tool count**: Counts tool calls per session (for session end stats), but does NOT persist raw tool observations
5. **Captures user prompts**: From `message.updated` events (>10 chars)
6. **Injects Memory Protocol**: Strict rules for when to save, when to search, and mandatory session close protocol — via `chat.system.transform`
7. **Injects context on compaction**: Auto-saves checkpoint + injects previous session context + reminds compressor
8. **Privacy**: Strips `<private>` tags before sending to HTTP API

### Session Resilience

The plugin uses `ensureSession()` — an idempotent function that creates the session in engram if it doesn't exist yet. This is called from every hook that receives a `sessionID`, not just `session.created`. This means:

- **Plugin reload**: If OpenCode restarts or the plugin is reloaded mid-session, the session is re-created on the next tool call or compaction event
- **Reconnect**: If you reconnect to an existing session, the session is created on-demand
- **No lost data**: Prompts, tool counts, and compaction context all work even if `session.created` was missed

Session IDs come from OpenCode's hook inputs (`input.sessionID` in `tool.execute.after`, `input.sessionID` in `experimental.session.compacting`) rather than from a fragile in-memory Map populated by events.

### Plugin API Types (OpenCode `@opencode-ai/plugin`)

The `tool.execute.after` hook receives:
- **`input`**: `{ tool, sessionID, callID, args }` — `input.sessionID` identifies the OpenCode session
- **`output`**: `{ title, output, metadata }` — `output.output` has the result string

### ENGRAM_TOOLS (excluded from tool count)

`mem_search`, `mem_save`, `mem_update`, `mem_delete`, `mem_suggest_topic_key`, `mem_save_prompt`, `mem_session_summary`, `mem_context`, `mem_stats`, `mem_timeline`, `mem_get_observation`, `mem_session_start`, `mem_session_end`

---

## Dependencies

### Go

- `github.com/mark3labs/mcp-go v0.44.0` — MCP protocol implementation
- `modernc.org/sqlite v1.45.0` — Pure Go SQLite driver (no CGO)
- `github.com/charmbracelet/bubbletea v1.3.10` — Terminal UI framework
- `github.com/charmbracelet/lipgloss v1.1.0` — Terminal styling
- `github.com/charmbracelet/bubbles v1.0.0` — TUI components (textinput, etc.)
- `github.com/lib/pq` — Postgres driver (for cloud server)
- `github.com/golang-jwt/jwt/v5` — JWT token generation and validation (for cloud auth)
- `golang.org/x/crypto` — bcrypt password hashing (for cloud auth)

### OpenCode Plugin

- `@opencode-ai/plugin` — OpenCode plugin types and helpers
- Runtime: Bun (built into OpenCode)

---

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

### Data location

`~/.engram/engram.db` (SQLite database, created on first run)

---

## Design Decisions

1. **Go over TypeScript** — Single binary, cross-platform, no runtime. The initial prototype was TS but was rewritten.
2. **SQLite + FTS5 over vector DB** — FTS5 covers 95% of use cases. No ChromaDB/Pinecone complexity.
3. **Agent-agnostic core** — Go binary is the brain, thin plugins per-agent. Not locked to any agent.
4. **Agent-driven compression** — The agent already has an LLM. No separate compression service.
5. **Privacy at two layers** — Strip in plugin AND store. Defense in depth.
6. **Pure Go SQLite (modernc.org/sqlite)** — No CGO means true cross-platform binary distribution.
7. **No raw auto-capture** — Raw tool calls (edit, bash, etc.) are noisy, pollute search results, and bloat the database. The agent saves curated summaries via `mem_save` and `mem_session_summary` instead. Shell history and git provide the raw audit trail.
8. **TUI with Bubbletea** — Interactive terminal UI for browsing memories without leaving the terminal. Follows Gentleman Bubbletea patterns (screen constants, single Model struct, vim keys).

---

## Inspired By

[claude-mem](https://github.com/thedotmack/claude-mem) — But agent-agnostic and with a Go core instead of TypeScript.

Key differences from claude-mem:

- Agent-agnostic (not locked to Claude Code)
- Go binary (not Node.js/TypeScript)
- FTS5 instead of ChromaDB
- Agent-driven compression instead of separate LLM calls
- Simpler architecture (single binary, embedded web dashboard)
