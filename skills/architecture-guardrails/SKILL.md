---
name: postgram-architecture-guardrails
description: >
  Architecture guardrails for Postgram across store, server, MCP, TUI, and remote
  integrations. Trigger: Any change that affects system boundaries, ownership,
  state flow, or cross-package responsibilities.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

Use this skill when:
- Adding a new subsystem or major package
- Moving responsibilities between store, server, MCP, TUI, or remote integrations
- Changing source-of-truth rules or persistence boundaries

---

## Core Guardrails

1. PostgreSQL is the source of truth; local and remote flows must preserve the same product story.
2. Keep integration/adaptor layers thin; real behavior belongs in Go packages.
3. Prefer explicit boundaries: store, server, mcp, auth, tui.
4. New features must fit the existing local-first mental model before they fit the UI.
5. Do not hide cross-system coupling inside helpers or templates.

---

## Decision Rules

- Local-only concern -> `internal/store`
- HTTP contract or enforcement -> `internal/server`
- MCP tool contract -> `internal/mcp`
- Auth and identity integration -> `internal/auth`
- Terminal UX -> `internal/tui`

---

## Validation

- Add regression tests for every boundary change.
- Verify CLI, HTTP, MCP, and persistence behavior still tell the same product story.
- If the change crosses package boundaries, test the affected entrypoints.
