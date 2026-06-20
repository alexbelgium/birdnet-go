# BirdNET-Go — Feature Prompt

> Fill in the story below, then send. Follow phases in order. Rules are embedded — do not read CLAUDE.md or CONTRIBUTING.md unless explicitly told to below.

---

## Story

**As a** [user / admin / developer]
**I want** [what the feature does]
**So that** [the benefit / outcome]

**Implementation hints** *(optional)*: [preferred approach, files you suspect, constraints]
**Scope**: [backend | frontend | both | new API endpoint | config setting]

---

## Response Format — Applies to the Entire Session

- Bullet lists only — no prose paragraphs, no narration ("I will now…", "Let me…")
- File references: `path/to/file:line`
- After each phase: one-line status, then stop
- Ask before acting outside these phases

---

## Embedded Rules — Do Not Re-Read Source Docs for These

### Universal
- No API v1 — all new endpoints in `internal/api/v2/`
- No magic numbers/strings — named constants only
- UI settings must hot-reload without server restart (per-request checks, not startup-time branching)
- Prefer editing existing files over creating new ones
- No new abstraction unless 3+ call sites need it
- No bonus refactoring, no backward-compat shims, no defensive stubs for hypothetical futures
- No TODO/FIXME in committed code
- No telemetry without explicit user opt-in
- Comments: non-obvious WHY only, one line max — never explain WHAT

### Go Backend
- `internal/errors` — never stdlib `errors`
- `internal/logger` — structured logging
- API handlers: `c.logAPIRequest()`, `c.HandleError(ctx, err, "msg", status)`, `c.getEffectiveAuthMiddleware()`
- Go 1.26: `strings.Cut()` over index+slice, `errors.AsType[T]()` over `errors.As()`, `new(expr)` for pointer init
- Every exported symbol: one-line doc comment
- Tests: `testify` + `-race`; reuse `internal/testutil/`; never raw `t.Error`/`t.Fatal`
- **Never** add paths under `/api/v2/audio/` — reserved for numeric `:id` only

### Frontend (Svelte 5 + TypeScript)
- Runes only: `$props()`, `$state()`, `$derived()`, `$effect()` — no Svelte 4 `$:` syntax
- No `any` — ever; no inline styles; no daisyUI classes (Tailwind v4.1 native only)
- All user-facing strings: `$t('key')` — add key to every locale file
- Reuse `frontend/src/lib/api/` and `frontend/src/lib/components/` before writing new code

---

## Phase 1 — Research (no code yet)

**If scope includes a new API endpoint**: read `internal/api/v2/README.md` to confirm no duplicate exists.

Search with ast-grep (not grep or sed):
```bash
ast-grep --pattern "PATTERN" internal/
ast-grep --pattern "PATTERN" --lang svelte frontend/src/
```

Report (5 bullets max):
- Closest analogous file:line
- Reusable functions — name + package
- Existing API endpoint covering this (if any)
- Closest config field if a new setting is needed (`internal/conf/`)

---

## Phase 2 — Plan (stop and wait for my approval)

One short list:
- Modify: `file:approx-line` — one reason per file
- Create: `file` — only if nothing existing fits
- Reuse: function names
- New code: one justification line per item
- Out of scope: what will not change

**Do not write code until I say "go ahead".**

---

## Phase 3 — Implement

Minimum code to satisfy the story. Apply embedded rules above.

Extra steps by scope:
- New API endpoint → update `internal/api/v2/README.md`
- New i18n key → add to all files under `frontend/static/messages/`

---

## Phase 4 — Lint (scoped, not repo-wide)

```bash
# Go — changed packages only
golangci-lint run -v ./internal/<pkg>/...
go test -race -v ./internal/<pkg>/...

# Frontend — always full
cd frontend && npm run check:all
```

Run `./...` only if cross-cutting code changed. Zero warnings — fix before moving on.

---

## Phase 5 — Done?

Ask exactly this:
> "Done. Run `/preflight` (6-agent quality gate, ~5 min) before committing?"

Run `/preflight` only if I say yes — it is expensive and only useful on finished work.

---

## Phase 6 — Commit

```bash
git add <specific files — never git add -A or git add .>
git commit -m "type: short imperative description"
git push -u origin <branch>
```

Types: `feat` | `fix` | `refactor` | `test` | `docs` | `chore`
