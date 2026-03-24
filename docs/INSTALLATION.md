[← Back to README](../README.md)

# Installation

- [Homebrew (macOS / Linux)](#homebrew-macos--linux)
- [Windows](#windows)
- [Install from source (macOS / Linux)](#install-from-source-macos--linux)
- [Download binary (all platforms)](#download-binary-all-platforms)
- [Requirements](#requirements)
- [Environment Variables](#environment-variables)
- [Windows Config Paths](#windows-config-paths)

---

## Homebrew (macOS / Linux)

```bash
brew install gentleman-programming/tap/postgram
```

Upgrade to latest:

```bash
brew update && brew upgrade postgram
```

> **Migrating from Cask?** If you installed postgram before v1.0.1, it was distributed as a Cask. Uninstall first, then reinstall:
> ```bash
> brew uninstall --cask postgram 2>/dev/null; brew install gentleman-programming/tap/postgram
> ```

---

## Windows

**Option A: Download the binary (recommended)**

1. Go to [GitHub Releases](https://github.com/Gentleman-Programming/postgram/releases)
2. Download `postgram_<version>_windows_amd64.zip` (or `arm64` for ARM devices)
3. Extract `postgram.exe` to a folder in your `PATH` (e.g. `C:\Users\<you>\bin\`)

```powershell
# Example: extract and add to PATH (PowerShell)
Expand-Archive postgram_*_windows_amd64.zip -DestinationPath "$env:USERPROFILE\bin"
# Add to PATH permanently (run once):
[Environment]::SetEnvironmentVariable("Path", "$env:USERPROFILE\bin;" + [Environment]::GetEnvironmentVariable("Path", "User"), "User")
```

**Option B: Install from source**

```powershell
git clone https://github.com/Gentleman-Programming/postgram.git
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
git clone https://github.com/Gentleman-Programming/postgram.git
cd postgram
go install ./cmd/postgram

# Optional: build with version stamp (otherwise `postgram version` shows "dev")
go build -ldflags="-X main.version=local-$(git describe --tags --always)" -o postgram ./cmd/postgram
```

---

## Download binary (all platforms)

Grab the latest release for your platform from [GitHub Releases](https://github.com/Gentleman-Programming/postgram/releases).

| Platform | File |
|----------|------|
| macOS (Apple Silicon) | `postgram_<version>_darwin_arm64.tar.gz` |
| macOS (Intel) | `postgram_<version>_darwin_amd64.tar.gz` |
| Linux (x86_64) | `postgram_<version>_linux_amd64.tar.gz` |
| Linux (ARM64) | `postgram_<version>_linux_arm64.tar.gz` |
| Windows (x86_64) | `postgram_<version>_windows_amd64.zip` |
| Windows (ARM64) | `postgram_<version>_windows_arm64.zip` |

---

## Requirements

- **Go 1.25+** to build from source (not needed if installing via Homebrew or downloading a binary)
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
