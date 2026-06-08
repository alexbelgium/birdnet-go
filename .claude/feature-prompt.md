# BirdNET-Go Feature Implementation Prompt

> Paste this (or the feature-specific section) at the start of any new feature session.
> Replace the `[FEATURE]` placeholder with your feature description.

---

## Feature Request

I want to implement: **[FEATURE — describe clearly what it should do and where: backend / frontend / both]**

---

## Phase 0 — Read Guidance Before Anything Else

Before writing a single line of code, read every applicable guidance file:

- Always read **`AGENTS.md`** (root)
- Always read **`CLAUDE.md`** (root)
- Go code → also read **`internal/CLAUDE.md`**
- Frontend code → also read **`frontend/CLAUDE.md`**
- New API endpoint → also read **`internal/api/v2/CLAUDE.md`** AND **`internal/api/v2/README.md`**
- Tests → also read **`TESTING.md`**
- General contribution rules → also read **`CONTRIBUTING.md`**

Do not skip this step. These files override any default behavior.

---

## Phase 1 — Research Existing Code (Do This Before Planning)

Search the codebase for existing patterns that relate to this feature:

1. **Find similar features already implemented.** Look for the closest analogous handler, component, service, or config field. Read it fully.
2. **Find reusable functions.** Search for helpers in `internal/`, `pkg/`, and `frontend/src/lib/` that overlap with what's needed. List them explicitly.
3. **Check API v2 endpoints.** Read `internal/api/v2/README.md` to confirm no existing endpoint already covers this. Never duplicate.
4. **Check config/settings structs.** If adding a new setting, find where the closest existing setting lives in `internal/conf/` and follow the exact same pattern.
5. **Use ast-grep for structural searches** — not grep or sed:
   ```bash
   ast-grep --pattern "YOUR_PATTERN" internal/
   ast-grep --pattern "YOUR_PATTERN" --lang svelte frontend/src/
   ```

Report what you found: which files, which functions, which patterns are reusable.

---

## Phase 2 — Minimal Change Plan (Get Approval Before Coding)

After researching, propose a plan with:

- **Exact files to create or modify** (paths + line numbers where insertions go)
- **Existing functions that will be reused** (call out each one by name)
- **New code that must be written** (justify why nothing existing covers it)
- **What will NOT be changed** (scope boundary — no bonus refactoring)

The plan must satisfy these non-negotiables:
- No new abstraction unless 3+ call sites need it
- No new helper function if an existing one can be called directly
- No backward-compat shims, no feature flags, no defensive stubs for hypothetical futures
- Settings changed via UI **must hot-reload** (no server restart required)
- New API endpoints go in `internal/api/v2/` — never touch API v1
- Named constants for every non-trivial value — no magic numbers/strings

Wait for explicit approval before implementing. A short "looks good" is enough.

---

## Phase 3 — Implementation Rules

Follow these rules exactly while coding:

### Go Backend
- Import `internal/errors` — never the stdlib `errors` package
- Import `internal/logger` for structured logging; use `c.logAPIRequest()` in handlers
- Use `c.HandleError(ctx, err, "message", statusCode)` for API error responses
- Go 1.26 idioms: `strings.Cut()` over index+slice, `errors.AsType[T]()` over `errors.As()`, `new(expr)` for pointer init
- Every exported symbol gets a one-line doc comment
- No `any` type — use concrete types or generics
- Test files use `-race` flag and `testify` (never raw `testing.T` asserts)
- Use shared test helpers from `internal/testutil/` before writing new ones

### Frontend (Svelte 5 + TypeScript)
- Use `$props()`, `$state()`, `$derived()`, `$effect()` — no legacy Svelte 4 syntax
- No `any` in TypeScript — ever
- Every user-visible string goes through `$t()` (i18n); add the key to the locale file
- No inline styles; use existing CSS custom properties from the design system
- Use existing API client functions in `frontend/src/lib/api/` before writing new fetch calls
- Follow existing component patterns — check `frontend/src/lib/components/` for the closest match

### API v2 Endpoints
- Register public endpoints on `c.Group`, protected ones with `c.getEffectiveAuthMiddleware()`
- **Never** add paths under `/api/v2/audio/` (reserved for numeric `:id` — see api/v2/CLAUDE.md warning)
- Input validation is mandatory — sanitize all user input at the boundary
- Update `internal/api/v2/README.md` immediately when adding an endpoint

### Both
- No new files unless strictly necessary — prefer editing existing ones
- No comments that describe WHAT the code does — only WHY if it's non-obvious
- No TODO/FIXME left in committed code

---

## Phase 4 — Lint and Test (Zero Tolerance)

After every change, run:

```bash
# Go
golangci-lint run -v ./...

# Frontend
cd frontend && npm run check:all

# Go tests (always with race detector)
go test -race ./...

# Targeted package test
go test -race -v ./internal/<package>/...
```

Fix **every** linter warning before moving on. Do not suppress linters without explaining why in a comment.

---

## Phase 5 — Pre-Commit Quality Gate

Before committing, run the preflight skill:

```
/preflight
```

This runs 6 parallel reviews (reuse, correctness, quality, i18n, integration wiring, regression/backward-compat). Address all findings before committing.

---

## Phase 6 — Commit

```bash
git add <specific files — never git add -A>
git commit -m "feat: <concise description of what and why>"
git push -u origin <branch-name>
```

Commit message format: `type: short imperative description` where type is one of:
`feat` | `fix` | `refactor` | `test` | `docs` | `chore`

---

## Guardrails Summary (Quick Reference)

| Rule | Detail |
|---|---|
| Read docs first | CLAUDE.md + domain CLAUDE.md before any code |
| Research before writing | Find reusable code; list it explicitly |
| Minimal code | No extra abstractions, no bonus refactoring |
| No API v1 | All endpoints in `internal/api/v2/` |
| Hot-reload | All UI settings must work without restart |
| Named constants | No magic numbers or strings |
| Zero linter warnings | `golangci-lint` + `npm run check:all` must be clean |
| Tests use testify + -race | No raw `t.Error`/`t.Fatal` |
| i18n | Every UI string through `$t()` |
| No `any` in TS | Concrete types or generics only |
| Preflight before commit | `/preflight` — mandatory |
