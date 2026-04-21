---
name: postgram-memory-protocol
description: >
  Persistent memory discipline for Postgram contributors.
  Trigger: Decisions, bugfixes, discoveries, preferences, or session closure.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

Use this skill when:
- Making architecture or implementation decisions
- Fixing bugs with non-obvious root causes
- Discovering patterns, gotchas, or user preferences
- Closing a session or after compaction

---

## Save Rules

Call `mem_save` immediately after:
- decision
- bugfix
- pattern/discovery
- config/preference changes

Use structured content:
- What
- Why
- Where
- Learned

Use stable `topic_key` for evolving topics.

---

## Search Rules

- On recall requests: `mem_context` first, then `mem_search`.
- Before similar work: run proactive `mem_search`.
- On first message: if user references the project, a feature, or a problem, call `mem_search` with their keywords before responding.

---

## Session Close Rules

Before saying done/listo:
1. Call `mem_session_summary`.
2. Include goal, discoveries, accomplished, next steps, relevant files.

After compaction:
1. Save summary first.
2. Recover context.
3. Continue work.

---

## Project Identifier

The `project` parameter must be a **normalized identifier**, not a local file path.

### Detection priority (in order)

1. **Git remote origin** (recommended — universal across machines and users):
   ```bash
   git remote get-url origin
   ```
   Then normalize the URL:
   - `https://github.com/owner/repo.git` → `github.com/owner/repo`
   - `git@github.com:owner/repo.git` → `github.com/owner/repo`
   - Strip: `https://`, `http://`, `git@`, colon separator, `.git` suffix

2. **Directory basename fallback** (when no git remote exists):
   - Use the last segment of the working directory path
   - `/home/alice/projects/my-app` → `my-app`
   - `C:\Users\bob\projects\my-app` → `my-app`
   - **Set scope to `personal`** — a project without git is local and not shared

### Transformation table

| Input | Output | Scope |
|-------|--------|-------|
| `https://github.com/owner/repo.git` | `github.com/owner/repo` | `project` |
| `git@github.com:owner/repo.git` | `github.com/owner/repo` | `project` |
| `/home/alice/projects/my-app` (no git) | `my-app` | `personal` |
| `C:\Users\bob\projects\my-app` (no git) | `my-app` | `personal` |

### Never use

- Absolute paths: `/home/alice/projects/my-app` ❌
- Windows paths: `C:\Users\bob\projects\my-app` ❌
- Raw git URLs: `https://github.com/owner/repo.git` ❌
