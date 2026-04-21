[← Back to README](../README.md)

# Agent Setup

Postgram works with **any MCP-compatible agent**. Pick your agent below.

## Quick Reference

| Agent | Manual Config |
|-------|---------------|
| Claude Code | [Details](#claude-code) |
| OpenCode | [Details](#opencode) |
| Gemini CLI | [Details](#gemini-cli) |
| Codex | [Details](#codex) |
| VS Code | `code --add-mcp '{"name":"postgram","url":"https://your-postgram-host/mcp"}'` | [Details](#vs-code-copilot--claude-code-extension) |
| Antigravity | Manual JSON config | [Details](#antigravity) |
| Cursor | Manual JSON config | [Details](#cursor) |
| Windsurf | Manual JSON config | [Details](#windsurf) |
| Any MCP agent | Remote MCP URL (`https://your-postgram-host/mcp`) | [Details](#any-other-mcp-agent) |

---

## OAuth2 / OIDC Setup

If your Postgram server has `POSTGRAM_MCP_AUTH_ENABLED=true`, agents must authenticate against your OAuth2/OIDC provider before calling `/mcp`.

Server-side values that must already be configured on Postgram:

```bash
POSTGRAM_MCP_AUTH_ENABLED=true
POSTGRAM_OIDC_ISSUER=https://auth.example.com/realms/shared
POSTGRAM_OIDC_AUDIENCE=postgram-mcp
POSTGRAM_BASE_URL=https://postgram.example.com
POSTGRAM_OAUTH_RESOURCE=https://postgram.example.com/mcp
```

Common client-side values you will need:
- `url`: your MCP endpoint, for example `https://postgram.example.com/mcp`
- `clientId`: the OAuth client registered for your agent
- `scope`: usually `openid profile` plus any required MCP scope such as `mcp:tools`

When auth is enabled:
- the agent connects to the same MCP URL as before
- the client performs OAuth login with your provider
- Postgram validates issuer, audience, signature, and optional scope
- OAuth protected resource metadata is exposed at `https://your-postgram-host/.well-known/oauth-protected-resource`

If your client supports explicit OAuth settings, add them next to the MCP server config. If it does not, use the same MCP URL and let the client complete the provider login flow automatically when prompted.

For a full provider example, including Keycloak setup, see `DOCS.md:199`.

---

## OpenCode

Configure OpenCode to use Postgram as a remote MCP server:

Add to your `opencode.json` (global: `~/.config/opencode/opencode.json` or project-level; on Windows: `%APPDATA%\opencode\opencode.json`):

```json
{
  "mcp": {
    "postgram": {
      "type": "remote",
      "url": "https://your-postgram-host/mcp",
      "enabled": true,
      "oauth": {
        "clientId": "postgram-local",
        "scope": "openid profile mcp:tools"
      }
    }
  }
}
```

---

## Claude Code

Configure Claude Code to use Postgram as a remote MCP server:

Add to your `.claude/settings.json` (project) or `~/.claude/settings.json` (global):

```json
{
  "mcpServers": {
    "postgram": {
      "url": "https://your-postgram-host/mcp"
    }
  }
}
```

If your Claude Code build exposes OAuth fields for remote MCP servers, add your provider client there using the same values described in [OAuth2 / OIDC Setup](#oauth2--oidc-setup). If not, keep the MCP URL and complete the browser/device login flow when Claude prompts for authentication.

Add a [Surviving Compaction](#surviving-compaction-recommended) prompt to your `CLAUDE.md` so the agent remembers to use Postgram after context resets.

---

## Gemini CLI

Add to your `~/.gemini/settings.json` (global) or `.gemini/settings.json` (project); on Windows: `%APPDATA%\gemini\settings.json`:

```json
{
  "mcpServers": {
    "postgram": {
      "url": "https://your-postgram-host/mcp"
    }
  }
}
```

If your Gemini build exposes explicit OAuth settings for remote MCP servers, add your provider client there using the values from [OAuth2 / OIDC Setup](#oauth2--oidc-setup). Otherwise keep the MCP URL and complete the login flow when Gemini prompts for authentication.

Also add the Memory Protocol to your Gemini instructions so the agent knows when to save, search, and close sessions.

---

## Codex

Add to your `~/.codex/config.toml` (Windows: `%APPDATA%\codex\config.toml`):

```toml
[mcp_servers.postgram]
url = "https://your-postgram-host/mcp"
```

If your Codex build supports explicit OAuth configuration for MCP servers, add your provider client there using the values from [OAuth2 / OIDC Setup](#oauth2--oidc-setup). Otherwise keep the MCP URL and complete the interactive login flow when prompted.

Also add the Memory Protocol to your Codex instructions or compact prompt so the agent preserves session knowledge across context resets.

---

## VS Code (Copilot / Claude Code Extension)

VS Code supports MCP servers natively in its chat panel (Copilot agent mode). This works with **any** AI agent running inside VS Code — Copilot, Claude Code extension, or any other MCP-compatible chat provider.

**Option A: Workspace config** (recommended for teams — commit to source control):

Add to `.vscode/mcp.json` in your project:

```json
{
  "servers": {
    "postgram": {
      "url": "https://your-postgram-host/mcp"
    }
  }
}
```

**Option B: User profile** (global, available across all workspaces):

1. Open Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`)
2. Run **MCP: Open User Configuration**
3. Add the same `postgram` server entry above to VS Code User `mcp.json`:
   - macOS: `~/Library/Application Support/Code/User/mcp.json`
   - Linux: `~/.config/Code/User/mcp.json`
   - Windows: `%APPDATA%\Code\User\mcp.json`

**Option C: CLI one-liner:**

```bash
code --add-mcp "{\"name\":\"postgram\",\"url\":\"https://your-postgram-host/mcp\"}"
```

If your VS Code MCP client exposes OAuth fields, add your provider client using the values from [OAuth2 / OIDC Setup](#oauth2--oidc-setup). Otherwise keep the MCP URL and let VS Code complete the browser/device login flow when prompted.

> **Using Claude Code extension in VS Code?** The Claude Code extension runs inside VS Code but uses its own MCP config. Follow the [Claude Code](#claude-code) instructions above — the `.claude/settings.json` config works whether you use Claude Code as a CLI or as a VS Code extension.

> **Windows**: Make sure `postgram.exe` is in your `PATH`. VS Code resolves MCP commands from the system PATH.

**Adding the Memory Protocol** (recommended — teaches the agent when to save and search memories):

Without the Memory Protocol, the agent has the tools but doesn't know WHEN to use them. Add these instructions to your agent's prompt:

**For Copilot:** Create a `.instructions.md` file in the VS Code User `prompts/` folder and paste the Memory Protocol from [DOCS.md](../DOCS.md#memory-protocol-full-text).

Recommended file path:
- macOS: `~/Library/Application Support/Code/User/prompts/postgram-memory.instructions.md`
- Linux: `~/.config/Code/User/prompts/postgram-memory.instructions.md`
- Windows: `%APPDATA%\Code\User\prompts\postgram-memory.instructions.md`

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
    "postgram": {
      "url": "https://your-postgram-host/mcp"
    }
  }
}
```

If your Antigravity build exposes explicit OAuth fields for remote MCP servers, add your provider client there using the values from [OAuth2 / OIDC Setup](#oauth2--oidc-setup). Otherwise keep the MCP URL and complete the login flow when prompted.

**Adding the Memory Protocol** (recommended):

Add the Memory Protocol as a global rule in `~/.gemini/GEMINI.md`, or as a workspace rule in `.agent/rules/`. See [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full text, or use the minimal version from [Surviving Compaction](#surviving-compaction-recommended).

> **Note:** Antigravity has its own skill, rule, and MCP systems separate from VS Code. Do not use `.vscode/mcp.json`.

---

## Cursor

Add to your `.cursor/mcp.json` (same path on all platforms — it's project-relative):

```json
{
  "mcpServers": {
    "postgram": {
      "url": "https://your-postgram-host/mcp"
    }
  }
}
```

If your Cursor build exposes explicit OAuth fields, add your provider client there using the values from [OAuth2 / OIDC Setup](#oauth2--oidc-setup). Otherwise keep the MCP URL and complete the login flow when prompted.

> **Windows**: Make sure `postgram.exe` is in your `PATH`. Cursor resolves MCP commands from the system PATH.

> **Memory Protocol:** Add the Memory Protocol instructions to your `.cursorrules` file. See [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full text, or use the minimal version from [Surviving Compaction](#surviving-compaction-recommended).

---

## Windsurf

Add to your `~/.windsurf/mcp.json` (Windows: `%USERPROFILE%\.windsurf\mcp.json`):

```json
{
  "mcpServers": {
    "postgram": {
      "url": "https://your-postgram-host/mcp"
    }
  }
}
```

If your Windsurf build exposes explicit OAuth fields, add your provider client there using the values from [OAuth2 / OIDC Setup](#oauth2--oidc-setup). Otherwise keep the MCP URL and complete the login flow when prompted.

> **Memory Protocol:** Add the Memory Protocol instructions to your `.windsurfrules` file. See [DOCS.md](../DOCS.md#memory-protocol-full-text) for the full text.

---

## Any other MCP agent

The pattern is always the same — point your agent's MCP config to your Postgram MCP URL, for example `https://your-postgram-host/mcp`.

If the client supports explicit OAuth settings, provide:

```json
{
  "url": "https://your-postgram-host/mcp",
  "oauth": {
    "clientId": "postgram-local",
    "scope": "openid profile mcp:tools"
  }
}
```

If the client does not expose an `oauth` block, keep the MCP URL and follow the client's interactive OAuth/device login flow when prompted.

For local development on the same machine as the agent, `http://127.0.0.1:7437/mcp` is still a valid default.

---

## Project Identifier

When configuring your agent's memory protocol, the `project` parameter must be a **normalized identifier** derived from the git remote origin — not a local file path.

**Correct** ✅:
```
project: "github.com/vertigo7x/postgram"
project: "gitlab.com/org/my-app"
```

**Incorrect** ❌:
```
project: "/home/alice/projects/postgram"
project: "C:\Users\bob\projects\postgram"
project: "https://github.com/vertigo7x/postgram.git"
```

**How to derive the project identifier**:
1. Run `git remote get-url origin` in the working directory
2. Normalize the URL: strip `https://`, `http://`, `git@`, colon separator, and `.git` suffix
3. If no git remote exists, use the directory basename and set `scope: personal`

Add this to your agent's memory instructions so it derives the correct project identifier automatically at session start.

## Surviving Compaction (Recommended)

When your agent compacts (summarizes long conversations to free context), it starts fresh — and might forget about Postgram. To make memory truly resilient, add this to your agent's system prompt or config file:

**For Claude Code** (`CLAUDE.md`):
```markdown
## Memory
You have access to Postgram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For Gemini CLI** (`GEMINI.md`):
```markdown
## Memory
You have access to Postgram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For VS Code** (`Code/User/prompts/*.instructions.md` or custom instructions):
```markdown
## Memory
You have access to Postgram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For Antigravity** (`~/.gemini/GEMINI.md` or `.agent/rules/`):
```markdown
## Memory
You have access to Postgram persistent memory via MCP tools (mem_save, mem_search, mem_session_summary, etc.).
- Save proactively after significant work — don't wait to be asked.
- After any compaction or context reset, call `mem_context` to recover session state before continuing.
```

**For Cursor/Windsurf** (`.cursorrules` or `.windsurfrules`):
```
You have access to Postgram persistent memory (mem_save, mem_search, mem_context).
Save proactively after significant work. After context resets, call mem_context to recover state.
```

This is the **nuclear option** — system prompts survive everything, including compaction.
