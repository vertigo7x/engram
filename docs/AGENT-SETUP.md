[← Back to README](../README.md)

# Agent Setup

Engram works with **any MCP-compatible agent**. Pick your agent below.

## Quick Reference

| Agent | Manual Config |
|-------|---------------|
| Claude Code | [Details](#claude-code) |
| OpenCode | [Details](#opencode) |
| Gemini CLI | [Details](#gemini-cli) |
| Codex | [Details](#codex) |
| VS Code | `code --add-mcp '{"name":"engram","url":"https://your-engram-host/mcp"}'` | [Details](#vs-code-copilot--claude-code-extension) |
| Antigravity | Manual JSON config | [Details](#antigravity) |
| Cursor | Manual JSON config | [Details](#cursor) |
| Windsurf | Manual JSON config | [Details](#windsurf) |
| Any MCP agent | Remote MCP URL (`https://your-engram-host/mcp`) | [Details](#any-other-mcp-agent) |

---

## OpenCode

Configure OpenCode to use Engram as a remote MCP server:

Add to your `opencode.json` (global: `~/.config/opencode/opencode.json` or project-level; on Windows: `%APPDATA%\opencode\opencode.json`):

```json
{
  "mcp": {
    "engram": {
      "type": "remote",
      "url": "https://your-engram-host/mcp",
      "enabled": true
    }
  }
}
```

---

## Claude Code

Configure Claude Code to use Engram as a remote MCP server:

Add to your `.claude/settings.json` (project) or `~/.claude/settings.json` (global):

```json
{
  "mcpServers": {
    "engram": {
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

Add a [Surviving Compaction](#surviving-compaction-recommended) prompt to your `CLAUDE.md` so the agent remembers to use Engram after context resets.

---

## Gemini CLI

Add to your `~/.gemini/settings.json` (global) or `.gemini/settings.json` (project); on Windows: `%APPDATA%\gemini\settings.json`:

```json
{
  "mcpServers": {
    "engram": {
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

Also add the Memory Protocol to your Gemini instructions so the agent knows when to save, search, and close sessions.

---

## Codex

Add to your `~/.codex/config.toml` (Windows: `%APPDATA%\codex\config.toml`):

```toml
[mcp_servers.engram]
url = "https://your-engram-host/mcp"
```

Also add the Memory Protocol to your Codex instructions or compact prompt so the agent preserves session knowledge across context resets.

---

## VS Code (Copilot / Claude Code Extension)

VS Code supports MCP servers natively in its chat panel (Copilot agent mode). This works with **any** AI agent running inside VS Code — Copilot, Claude Code extension, or any other MCP-compatible chat provider.

**Option A: Workspace config** (recommended for teams — commit to source control):

Add to `.vscode/mcp.json` in your project:

```json
{
  "servers": {
    "engram": {
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

**Option B: User profile** (global, available across all workspaces):

1. Open Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`)
2. Run **MCP: Open User Configuration**
3. Add the same `engram` server entry above to VS Code User `mcp.json`:
   - macOS: `~/Library/Application Support/Code/User/mcp.json`
   - Linux: `~/.config/Code/User/mcp.json`
   - Windows: `%APPDATA%\Code\User\mcp.json`

**Option C: CLI one-liner:**

```bash
code --add-mcp "{\"name\":\"engram\",\"url\":\"https://your-engram-host/mcp\"}"
```

> **Using Claude Code extension in VS Code?** The Claude Code extension runs inside VS Code but uses its own MCP config. Follow the [Claude Code](#claude-code) instructions above — the `.claude/settings.json` config works whether you use Claude Code as a CLI or as a VS Code extension.

> **Windows**: Make sure `engram.exe` is in your `PATH`. VS Code resolves MCP commands from the system PATH.

**Adding the Memory Protocol** (recommended — teaches the agent when to save and search memories):

Without the Memory Protocol, the agent has the tools but doesn't know WHEN to use them. Add these instructions to your agent's prompt:

**For Copilot:** Create a `.instructions.md` file in the VS Code User `prompts/` folder and paste the Memory Protocol from [DOCS.md](../DOCS.md#memory-protocol-full-text).

Recommended file path:
- macOS: `~/Library/Application Support/Code/User/prompts/engram-memory.instructions.md`
- Linux: `~/.config/Code/User/prompts/engram-memory.instructions.md`
- Windows: `%APPDATA%\Code\User\prompts\engram-memory.instructions.md`

**For any VS Code chat extension:** Add the Memory Protocol text to your extension's custom instructions or system prompt configuration.

The Memory Protocol tells the agent:
- **When to save** — after bugfixes, decisions, discoveries, config changes, patterns
- **When to search** — reactive ("remember", "recall") + proactive (overlapping past work)
- **Session close** — mandatory `mem_session_summary` before ending
- **After compaction** — recover state with `mem_context`

See [Surviving Compaction](#surviving-compaction-recommended) for the minimal version, or [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full Memory Protocol text you can copy-paste.

---

## Antigravity

[Antigravity](https://antigravity.google) is Google's AI-first IDE with native MCP and skill support.

**Add the MCP server** — open the MCP Store (`...` dropdown in the agent panel) → **Manage MCP Servers** → **View raw config**, and add to `~/.gemini/antigravity/mcp_config.json`:

```json
{
  "mcpServers": {
    "engram": {
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

**Adding the Memory Protocol** (recommended):

Add the Memory Protocol as a global rule in `~/.gemini/GEMINI.md`, or as a workspace rule in `.agent/rules/`. See [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full text, or use the minimal version from [Surviving Compaction](#surviving-compaction-recommended).

> **Note:** Antigravity has its own skill, rule, and MCP systems separate from VS Code. Do not use `.vscode/mcp.json`.

---

## Cursor

Add to your `.cursor/mcp.json` (same path on all platforms — it's project-relative):

```json
{
  "mcpServers": {
    "engram": {
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

> **Windows**: Make sure `engram.exe` is in your `PATH`. Cursor resolves MCP commands from the system PATH.

> **Memory Protocol:** Add the Memory Protocol instructions to your `.cursorrules` file. See [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full text, or use the minimal version from [Surviving Compaction](#surviving-compaction-recommended).

---

## Windsurf

Add to your `~/.windsurf/mcp.json` (Windows: `%USERPROFILE%\.windsurf\mcp.json`):

```json
{
  "mcpServers": {
    "engram": {
      "url": "https://your-engram-host/mcp"
    }
  }
}
```

> **Memory Protocol:** Add the Memory Protocol instructions to your `.windsurfrules` file. See [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full text.

---

## Any other MCP agent

The pattern is always the same — point your agent's MCP config to your Engram MCP URL, for example `https://your-engram-host/mcp`.

For local development on the same machine as the agent, `http://127.0.0.1:7437/mcp` is still a valid default.

---

## Surviving Compaction (Recommended)

When your agent compacts (summarizes long conversations to free context), it starts fresh — and might forget about Engram. To make memory truly resilient, add this to your agent's system prompt or config file:

**For Claude Code** (`CLAUDE.md`):
```markdown
## Memory
You have access to Engram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For Gemini CLI** (`GEMINI.md`):
```markdown
## Memory
You have access to Engram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For VS Code** (`Code/User/prompts/*.instructions.md` or custom instructions):
```markdown
## Memory
You have access to Engram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For Antigravity** (`~/.gemini/GEMINI.md` or `.agent/rules/`):
```markdown
## Memory
You have access to Engram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For Cursor/Windsurf** (`.cursorrules` or `.windsurfrules`):
```
You have access to Engram persistent memory (mem_save, mem_search, mem_context).
Save proactively after significant work. After context resets, call mem_context to recover state.
```

This is the **nuclear option** — system prompts survive everything, including compaction.
