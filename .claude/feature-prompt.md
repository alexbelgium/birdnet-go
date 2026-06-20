# BirdNET-Go Feature Prompt

> Fill story → send. Rules embedded — skip CLAUDE.md/CONTRIBUTING.md.

---

## Story

**As a** [user / admin / developer]
**I want** [what]
**So that** [benefit]

**Hints** *(opt)*: [approach, suspected files, constraints]
**Scope**: [backend | frontend | both | API endpoint | config]

---

## Response format (whole session)

- Bullets only. No prose, no narration.
- Files: `path:line`
- Phase done: one-line status, stop.
- Ask before acting outside phases.

---

## Rules (no need to re-read source docs)

**Universal**
- No API v1 — endpoints in `internal/api/v2/` only
- Named constants — no magic numbers/strings
- UI settings hot-reload without server restart
- Edit existing files before creating new ones
- No new abstraction unless 3+ call sites
- No bonus refactoring, shims, or defensive stubs
- No TODO/FIXME in committed code
- No telemetry without user opt-in
- Comments: WHY only, one line max

**Go**
- `internal/errors` not stdlib `errors`; `internal/logger` for logging
- Handlers: `c.logAPIRequest()`, `c.HandleError(ctx, err, "msg", status)`, `c.getEffectiveAuthMiddleware()`
- Go 1.26: `strings.Cut()`, `errors.AsType[T]()`, `new(expr)`
- Exported symbols: one-line doc comment
- Tests: `testify` + `-race`; reuse `internal/testutil()`; no raw `t.Error`/`t.Fatal`
- No paths under `/api/v2/audio/` — reserved for numeric `:id`

**Frontend (Svelte 5 + TS)**
- Runes: `$props()`, `$state()`, `$derived()`, `$effect()` — no Svelte 4 `$:`
- No `any`; no inline styles; no daisyUI (Tailwind v4.1 only)
- User strings: `$t('key')` + add key to all locale files
- Reuse `frontend/src/lib/api/` and `frontend/src/lib/components/` first

---

## Phase 1 — Research

API endpoint → read `internal/api/v2/README.md` first (check duplicates).

Search (ast-grep, not grep/sed):
```bash
ast-grep --pattern "PATTERN" internal/
ast-grep --pattern "PATTERN" --lang svelte frontend/src/
```

Report ≤5 bullets:
- Closest analogous `file:line`
- Reusable functions (name + pkg)
- Existing endpoint covering this
- Closest config field (`internal/conf/`)

---

## Phase 2 — Plan

- Modify: `file:line` — one reason
- Create: `file` — only if nothing fits
- Reuse: function names
- New code: one justification per item
- Out of scope: what won't change

**Stop. No code until "go ahead".**

---

## Phase 3 — Implement

Min code to satisfy story. Apply rules above.

- New API endpoint → update `internal/api/v2/README.md`
- New i18n key → add to all `frontend/static/messages/` files

---

## Phase 4 — Lint (scoped)

```bash
golangci-lint run -v ./internal/<pkg>/...
go test -race -v ./internal/<pkg>/...
cd frontend && npm run check:all
```

`./...` only if cross-cutting code changed. Zero warnings.

---

## Phase 5 — Done?

Ask: `"Done. Run /preflight (~5 min) before committing?"`

Preflight only if yes.

---

## Phase 6 — Commit

```bash
git add <specific files>
git commit -m "type: short imperative"
git push -u origin <branch>
```

Types: `feat` | `fix` | `refactor` | `test` | `docs` | `chore`
