<p align="center">
  <img  alt="image" src="assets/postgram.png" />
</p>

<p align="center">
  <strong>Teams Persistent memory for AI coding agents</strong><br>
  <em>Agent-agnostic.</em>
</p>

<p align="center">
  <a href="docs/INSTALLATION.md">Installation</a> &bull;
  <a href="docs/AGENT-SETUP.md">Agent Setup</a> &bull;
  <a href="docs/ARCHITECTURE.md">Architecture</a> &bull;
  <a href="CONTRIBUTING.md">Contributing</a> &bull;
  <a href="DOCS.md">Full Docs</a>
</p>

---

> **postgram** is a remote memory service for AI coding agents. It provides a persistent, shared memory store backed by PostgreSQL, accessible over HTTP using the Memory Context Protocol (MCP). With postgram, your agents can save decisions, bugfixes, and summaries across sessions, enabling long-term context and learning.

**postgram** is a **remote MCP memory service** for coding agents. Run one Go binary, connect it to PostgreSQL, expose `/mcp`, and point your agents at it.

**postgram** was forged from [engram](https://github.com/Gentleman-Programming/engram) and heavily inspired by its design, but tailored for team use.

```
Agent (Claude Code / OpenCode / Gemini CLI / Codex / VS Code / ...)
    ↓ MCP over HTTP
Postgram (`postgram serve`)
    ↓
PostgreSQL (`POSTGRAM_DATABASE_URL`)
```

## Quick Start

### 1. Install

From the repository:

```bash
git clone https://github.com/vertigo7x/postgram.git
cd postgram
go build -o postgram ./cmd/postgram
```

Or use the published Docker image from GitHub Container Registry:

```bash
docker pull ghcr.io/vertigo7x/postgram:latest
```

The image is also published to Docker Hub as `vertigo7x/postgram:latest`.

Current install/runtime paths → build from this repository or run the Docker image.

Helm chart installs are also available from GitHub Container Registry:

```bash
helm install postgram oci://ghcr.io/vertigo7x/charts/postgram --version 0.1.2
```

### 2. Start Postgram

Binary:

```bash
export POSTGRAM_DATABASE_URL='postgres://user:pass@host:5432/postgram?sslmode=disable'
postgram serve
```

Docker:

```bash
docker run --rm -p 7437:7437 \
  -e POSTGRAM_DATABASE_URL='postgres://user:pass@host:5432/postgram?sslmode=disable' \
  ghcr.io/vertigo7x/postgram:latest serve
```

Default endpoints:
- Health: `http://127.0.0.1:7437/health`
- MCP: `http://127.0.0.1:7437/mcp`

For team/shared setups, put Postgram behind your normal ingress or reverse proxy and publish a stable URL such as `https://postgram.example.com/mcp`.

### OAuth2 / OIDC Authentication

Postgram can protect the MCP HTTP endpoint with an external OAuth2/OIDC provider such as Keycloak, Auth0, Okta, or any provider that exposes standard issuer and JWKS metadata.

Enable it with environment variables like:

```bash
POSTGRAM_MCP_AUTH_ENABLED=true
POSTGRAM_OIDC_ISSUER=https://auth.example.com/realms/shared
POSTGRAM_OIDC_AUDIENCE=postgram-mcp
POSTGRAM_BASE_URL=https://postgram.example.com
POSTGRAM_OAUTH_RESOURCE=https://postgram.example.com/mcp
```

Optional settings:
- `POSTGRAM_OIDC_JWKS_URL` to override JWKS discovery
- `POSTGRAM_OIDC_REQUIRED_SCOPE` to require a scope such as `mcp:tools`
- `POSTGRAM_OAUTH_AUTHORIZATION_SERVERS` to publish explicit authorization server metadata

When auth is enabled:
- clients authenticate with your OAuth2 provider and send bearer tokens to `/mcp`
- Postgram validates issuer, audience, signature, and optional scope
- Postgram exposes OAuth Protected Resource Metadata at `/.well-known/oauth-protected-resource`

Full provider setup example, including Keycloak, lives in `DOCS.md:187`.

### 3. Connect Your Agent

| Agent | One-liner |
|-------|-----------|
| Claude Code | Add remote MCP config from `docs/AGENT-SETUP.md` |
| OpenCode | Add remote MCP config from `docs/AGENT-SETUP.md` |
| Gemini CLI | Add remote MCP config from `docs/AGENT-SETUP.md` |
| Codex | Add remote MCP config from `docs/AGENT-SETUP.md` |
| VS Code | `code --add-mcp '{"name":"postgram","url":"https://your-postgram-host/mcp"}'` |
| Cursor / Windsurf / Any MCP | See [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md) |

Full per-agent config, Memory Protocol, and compaction survival → [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md)

That's it. No Node.js, no Python, no sidecar services. **One binary, one PostgreSQL database, one MCP HTTP endpoint.**

## The Main Flow

```
1. Agent connects to Postgram over MCP HTTP
2. Agent saves decisions, bugfixes, and summaries with `mem_save` / `mem_session_summary`
3. Postgram stores them in PostgreSQL
4. Future sessions recover context with `mem_context`, `mem_search`, and `mem_timeline`
```

Full details on session lifecycle, topic keys, and memory hygiene → [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## Why Postgram

- Shared memory for local agents, cloud agents, and team deployments
- Postgres-backed storage instead of per-machine local files
- MCP over HTTP, so agent setup is just remote server config
- Session-aware memory with summaries, prompts, context, and timeline drill-in
- CLI and TUI included for inspection, debugging, and import/export workflows

## MCP Tools

| Tool | Purpose |
|------|---------|
| `mem_save` | Save observation |
| `mem_update` | Update by ID |
| `mem_delete` | Soft or hard delete |
| `mem_suggest_topic_key` | Stable key for evolving topics |
| `mem_search` | Search memory |
| `mem_session_summary` | End-of-session save |
| `mem_context` | Recent session context |
| `mem_timeline` | Chronological drill-in |
| `mem_get_observation` | Full content by ID |
| `mem_save_prompt` | Save user prompt |
| `mem_stats` | Memory statistics |
| `mem_session_start` | Register session start |
| `mem_session_end` | Mark session complete |

Full tool reference → [docs/ARCHITECTURE.md#mcp-tools](docs/ARCHITECTURE.md#mcp-tools)

## Operating Postgram

```bash
postgram serve              # Start HTTP API + MCP over HTTP
postgram stats              # Inspect memory totals
postgram tui                # Browse memory manually
postgram export backup.json # Export all data
postgram import backup.json # Import a backup
```

The CLI is mainly for operators, debugging, migration, and manual inspection. Agents should usually talk to Postgram through MCP over HTTP.

## Terminal UI

```bash
postgram tui
```

<p align="center">
<img src="assets/tui-dashboard.png" alt="TUI Dashboard" width="400" />
  <img width="400" alt="image" src="https://github.com/user-attachments/assets/0308991a-58bb-4ad8-9aa2-201c059f8b64" />
  <img src="assets/tui-detail.png" alt="TUI Observation Detail" width="400" />
  <img src="assets/tui-search.png" alt="TUI Search Results" width="400" />
</p>

**Navigation**: `j/k` vim keys, `Enter` to drill in, `/` to search, `Esc` back. Catppuccin Mocha theme.

## CLI Reference

| Command | Description |
|---------|-------------|
| `postgram serve [port]` | Start HTTP API + MCP over HTTP |
| `postgram tui` | Launch terminal UI |
| `postgram search <query>` | Search memories |
| `postgram save <title> <msg>` | Save a memory |
| `postgram timeline <obs_id>` | Chronological context |
| `postgram context [project]` | Recent session context |
| `postgram stats` | Memory statistics |
| `postgram export [file]` | Export to JSON |
| `postgram import <file>` | Import from JSON |
| `postgram version` | Show version |


## Documentation

| Doc | Description |
|-----|-------------|
| [Installation](docs/INSTALLATION.md) | All install methods + platform support |
| [Agent Setup](docs/AGENT-SETUP.md) | Per-agent configuration + Memory Protocol |
| [Architecture](docs/ARCHITECTURE.md) | How it works + MCP tools + project structure |
| [Comparison](docs/COMPARISON.md) | Why Postgram vs claude-mem |
| [Contributing](CONTRIBUTING.md) | Contribution workflow + standards |
| [Full Docs](DOCS.md) | Complete technical reference |

## License

MIT
