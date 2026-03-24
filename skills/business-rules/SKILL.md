---
name: postgram-business-rules
description: >
  Product and business-rule guardrails for Postgram. Trigger: Any change that
  affects remote access, project controls, permissions, or memory semantics.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

Use this skill when:
- Changing auth, remote MCP behavior, admin controls, or memory behavior
- Implementing project-level or org-level policy
- Adjusting what data appears across CLI, HTTP, or MCP surfaces

---

## Product Rules

1. Local-first remains the default mental model.
2. Shared access controls belong on the server, not only in local clients.
3. Project-level policy must be enforceable server-side if it is meant for admins.
4. UI controls must map to real business rules, never fake toggles.
5. Data visibility and access permissions must be deterministic and testable.

---

## Access Rules

- Server policy controls what remote clients may read or write.
- When a policy blocks access, fail loudly rather than dropping data silently.
- Preserve auditability whenever admin policy changes behavior.
