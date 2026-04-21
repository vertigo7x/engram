# Postgram — Product Summary

Postgram is a **remote persistent memory service for AI coding agents**. It provides a shared, team-accessible memory store backed by PostgreSQL, exposed over HTTP via the **Memory Context Protocol (MCP)**.

Agents (Claude Code, OpenCode, Gemini CLI, Codex, VS Code, Cursor, etc.) connect to a single Postgram instance and use MCP tools to save decisions, bugfixes, and session summaries — then retrieve that context in future sessions.

## Core Value

- One binary + one PostgreSQL database = shared agent memory for a whole team
- No Node.js, no Python, no sidecar services
- MCP over HTTP — agent setup is just a remote server URL
- Session-aware: summaries, context injection, timeline drill-in, topic upserts

## Primary Interfaces

- **MCP endpoint** (`/mcp`) — agents use this exclusively
- **HTTP REST API** (port 7437) — operator/tooling use
- **CLI** (`postgram serve`, `stats`, `export`, `import`, etc.)
- **TUI** (`postgram tui`) — interactive memory browser (Bubbletea, Catppuccin Mocha theme)

## Key MCP Tools

`mem_save`, `mem_update`, `mem_delete`, `mem_search`, `mem_context`, `mem_timeline`, `mem_get_observation`, `mem_session_summary`, `mem_session_start`, `mem_session_end`, `mem_save_prompt`, `mem_suggest_topic_key`, `mem_stats`

## Auth

Optional OAuth2/OIDC protection on `/mcp` via environment variables (`POSTGRAM_MCP_AUTH_ENABLED`, `POSTGRAM_OIDC_ISSUER`, etc.).
