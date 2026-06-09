# BirdNET-Go Feature Implementation Prompt

> Paste this at the start of a new feature session. Fill in the two fields below, then follow the phases in order.

---

## Feature

**What:** [one sentence — what the feature does]
**Scope:** [backend | frontend | both | API endpoint | config setting | …]

---

## Phase 0 — Read Only Relevant Docs

Read only the files that match the scope above. Do not read others.

| Scope | File to read |
|---|---|
| Any | `CLAUDE.md` + `AGENTS.md` |
| Go / backend | `internal/CLAUDE.md` |
| Frontend / UI | `frontend/CLAUDE.md` |
| API endpoint | `internal/api/v2/CLAUDE.md` + `internal/api/v2/README.md` |
| Tests | `TESTING.md` |
| Unsure | `CONTRIBUTING.md` |

Keep responses short — no need to summarize the docs back to me.

---

## Phase 1 — Research Before Writing Anything

Search for existing code that overlaps with this feature. Use ast-grep, not grep:

```bash
ast-grep --pattern "PATTERN" internal/
ast-grep --pattern "PATTERN" --lang svelte frontend/src/
```

Report concisely:
- Closest analogous implementation (file + line)
- Reusable functions found (name + package)
- Existing API endpoint that might already cover this (check `internal/api/v2/README.md`)
- Closest existing config field if a new setting is needed (`internal/conf/`)

---

## Phase 2 — Plan (Wait for My Go-Ahead)

Propose in a short list:
- Files to modify (path + approximate line)
- Files to create (only if no existing file fits)
- Existing functions to call (name them)
- New code required (one line justifying why nothing existing covers it)
- Hard out-of-scope items (what you will not touch)

**Stop here. Do not write code until I approve.**

Non-negotiables the plan must respect:
- No new abstraction unless 3+ call sites need it
- No bonus refactoring, no backward-compat shims, no defensive stubs
- UI settings → must hot-reload without server restart
- New API endpoints → `internal/api/v2/` only; never API v1
- No magic numbers or strings — named constants only

---

## Phase 3 — Implement

Write the minimum code that makes the feature work. Rules by scope:

**Go**
- `internal/errors` not stdlib `errors`; `internal/logger` for logging
- API handlers: `c.logAPIRequest()`, `c.HandleError()`, `c.getEffectiveAuthMiddleware()`
- Go 1.26: prefer `strings.Cut()`, `errors.AsType[T]()`, `new(expr)`
- Export doc comment on every exported symbol
- Tests: `testify` + `-race`; reuse `internal/testutil/` helpers

**Frontend (Svelte 5 + TypeScript)**
- Runes only: `$props()`, `$state()`, `$derived()`, `$effect()`
- No `any` — ever; no inline styles
- All user-visible strings through `$t()`; add key to locale file
- Reuse `frontend/src/lib/api/` and `frontend/src/lib/components/` before creating new ones

**API v2**
- Public: `c.Group.GET/POST(...)`. Protected: add `c.getEffectiveAuthMiddleware()`
- Never add paths under `/api/v2/audio/` (reserved — see api/v2/CLAUDE.md)
- Validate all input at the boundary; update `internal/api/v2/README.md`

**Both**
- Prefer editing existing files over creating new ones
- Comments only for non-obvious WHY — never WHAT

---

## Phase 4 — Lint (Targeted, Not Full)

Run lint scoped to the packages you changed, not the whole repo:

```bash
# Go — target changed packages only
golangci-lint run -v ./internal/<changed-package>/...

# Frontend
cd frontend && npm run check:all

# Tests — changed package only
go test -race -v ./internal/<changed-package>/...
```

Fix every warning. Only run repo-wide lint (`./...`) if you touched cross-cutting code.

---

## Phase 5 — Commit-Ready Check (Ask Me Before Preflight)

When you believe the feature is complete, ask:

> "The feature looks done. Want me to run `/preflight` (6-agent quality gate) before committing, or commit as-is?"

Run `/preflight` only if I say yes. It is expensive and only useful on finished work.

---

## Phase 6 — Commit

```bash
git add <specific files only — never git add -A>
git commit -m "type: short imperative description"
git push -u origin <branch>
```

Types: `feat` | `fix` | `refactor` | `test` | `docs` | `chore`
