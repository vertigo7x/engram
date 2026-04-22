[← Back to README](../README.md)

# Installation

- [Docker](#docker)
- [Helm](#helm)
- [Windows](#windows)
- [Install from source (macOS / Linux)](#install-from-source-macos--linux)
- [Requirements](#requirements)
- [Environment Variables](#environment-variables)
- [Windows Config Paths](#windows-config-paths)

---

## Docker

### Option A: Use the official published image

```bash
docker pull ghcr.io/vertigo7x/postgram:latest

docker run --rm -p 7437:7437 \
  -e POSTGRAM_DATABASE_URL='postgres://user:pass@host:5432/postgram?sslmode=disable' \
  ghcr.io/vertigo7x/postgram:latest serve
```

The image is also published to Docker Hub as `vertigo7x/postgram:latest`.

### Option B: Build the image from this repository

```bash
git clone https://github.com/vertigo7x/postgram.git
cd postgram
docker build -t postgram:local .

docker run --rm -p 7437:7437 \
  -e POSTGRAM_DATABASE_URL='postgres://user:pass@host:5432/postgram?sslmode=disable' \
  postgram:local serve
```

Default endpoints:
- Health: `http://127.0.0.1:7437/health`
- MCP: `http://127.0.0.1:7437/mcp`

---

## Helm

Published chart in GitHub Container Registry:

```bash
helm registry login ghcr.io

helm install postgram oci://ghcr.io/vertigo7x/charts/postgram \
  --version 0.1.4 \
  --set database.url='postgres://user:pass@host:5432/postgram?sslmode=disable'
```

Local chart from this repository:

```bash
helm install postgram ./charts/postgram \
  --set database.url='postgres://user:pass@host:5432/postgram?sslmode=disable'
```

---

## Windows

Build from source:

```powershell
git clone https://github.com/vertigo7x/postgram.git
cd postgram
go install ./cmd/postgram
# Binary goes to %GOPATH%\bin\postgram.exe (typically %USERPROFILE%\go\bin\)

# Optional: build with version stamp (otherwise `postgram version` shows "dev")
$v = git describe --tags --always
go build -ldflags="-X main.version=local-$v" -o postgram.exe ./cmd/postgram
```

> **Windows notes:**
> - Postgram requires `POSTGRAM_DATABASE_URL` to point at PostgreSQL
> - Override with `POSTGRAM_DATA_DIR` environment variable
> - All core features work natively: CLI, MCP server, TUI, HTTP API
> - No WSL required for the core binary — it's a native Windows executable

---

## Install from source (macOS / Linux)

```bash
git clone https://github.com/vertigo7x/postgram.git
cd postgram
go install ./cmd/postgram

# Optional: build with version stamp (otherwise `postgram version` shows "dev")
go build -ldflags="-X main.version=local-$(git describe --tags --always)" -o postgram ./cmd/postgram
```

---

## Requirements

- **Go 1.25+** to build from source
- **PostgreSQL** reachable via `POSTGRAM_DATABASE_URL`

The binary works natively on **macOS**, **Linux**, and **Windows** (x86_64 and ARM64).

---

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `POSTGRAM_DATA_DIR` | Data directory | `~/.postgram` (Windows: `%USERPROFILE%\.postgram`) |
| `POSTGRAM_DATABASE_URL` | PostgreSQL connection URL | empty |
| `POSTGRAM_PORT` | HTTP server port | `7437` |

---

## Windows Config Paths

Common locations for manual MCP configuration:

| Agent | macOS / Linux | Windows |
|-------|---------------|---------|
| Gemini CLI | `~/.gemini/settings.json` | `%APPDATA%\gemini\settings.json` |
| Codex | `~/.codex/config.toml` | `%APPDATA%\codex\config.toml` |
| Claude Code | `~/.claude/settings.json` | `%APPDATA%\Claude\settings.json` |
| VS Code | `.vscode/mcp.json` (workspace) or `~/Library/Application Support/Code/User/mcp.json` (user) | `.vscode\mcp.json` (workspace) or `%APPDATA%\Code\User\mcp.json` (user) |
| Antigravity | `~/.gemini/antigravity/mcp_config.json` | `%USERPROFILE%\.gemini\antigravity\mcp_config.json` |
| Data directory | `~/.postgram/` | `%USERPROFILE%\.postgram\` |
