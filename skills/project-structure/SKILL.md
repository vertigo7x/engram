---
name: engram-project-structure
description: >
  Repository structure and placement rules for Engram. Trigger: Creating files,
  packages, handlers, templates, styles, or tests in this repo.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

Use this skill when:
- Creating a new package, file, or directory
- Deciding where code belongs
- Adding tests, templates, assets, or docs

---

## Placement Rules

1. Put behavior near its domain, not near the caller that happens to use it.
2. Put HTTP handlers in `internal/server`, not in `internal/store`.
3. Put MCP tool wiring in `internal/mcp`, not in CLI or store packages.
4. Put auth integration in `internal/auth`, not in handlers or store code.
5. Put persistence queries in `internal/store`, not in handlers.

---

## File Creation Rules

- New route -> handler + tests in `internal/server`
- New DB behavior -> `internal/store` code + focused tests
- New MCP behavior -> `internal/mcp` code + focused tests
- New TUI behavior -> `internal/tui` code + focused tests
- New contributor guidance -> update docs/catalog in same change

---

## Anti-Patterns

- Do not mix SQL, HTML, and transport logic in one file.
- Do not create utility packages for one-off helpers.
- Do not add package layers unless they remove real coupling.
