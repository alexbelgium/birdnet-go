---
description: Minimal, approval-gated feature implementation. Sizes the task, researches, plans, waits for approval, then implements the smallest correct diff.
argument-hint: [one-sentence feature description + scope]
---

# Feature Implementation — Minimal, Approval-Gated

Feature: $ARGUMENTS

Fill in the blanks below before starting. Anything left empty is treated as "no constraint", not "guess freely".

## Source of Truth

**Requirements** (user-visible, then backend/API, then frontend — list only what applies):
- …

**Explicit user decisions** (override all assumptions and any existing attempt):
- …

**Out of scope** (do NOT implement, incl. tempting-but-rejected improvements):
- …

This feature may already have been attempted. Do not preserve or justify an existing messy diff — implement the smallest correct version from the requirements above.

## Priority Rules (when instructions conflict)

1. Explicit user decisions above
2. Repo conventions / nearby code patterns
3. Repo instruction files (`CLAUDE.md` etc. — already loaded; apply silently)
4. Existing implementation attempts
5. General best practices

## Hard Constraints (stated once; they apply throughout)

- **Strict minimum diff.** No unrelated refactors, renames, formatting, cleanup, or drive-by fixes.
- Prefer editing existing files; reuse existing helpers, stores, components, endpoint patterns, and test utilities.
- No new abstraction unless there are 3+ real call sites. No speculative future-proofing. No defensive stubs unless existing architecture/tests require them.
- Validate input at boundaries. Comments only for non-obvious WHY, never WHAT.
- New config fields: backward-compatible with missing values; deep-copy new slice/map fields in clone/copy logic.
- Generated files change only via the repo's generator, never by hand.
- Do NOT commit, push, or run `/preflight` unless I explicitly ask.

(ast-grep usage, lint/test commands, API-v1 freeze, i18n, hot-reload, and doc-routing are already mandated in `CLAUDE.md` — follow them; they are not repeated here.)

## Phase 0 — Triage

Size the task in one line, then branch:

- **Trivial** (≤2 files, no architectural ambiguity, no new persistence/endpoint): skip Phases 1–2, implement directly, then do the Phase 4 diff audit.
- **Non-trivial**: run the full flow below, including the Phase 2 approval gate.

State which path you chose and why.

## Phase 1 — Research (read-only; non-trivial only)

Read only the scope-relevant doc(s) per `CLAUDE.md`'s routing. Then search for overlapping code with ast-grep (not grep). Report **only**:

1. Closest reusable implementation(s): file + approx line
2. Existing functions/components/stores to reuse
3. Files strictly necessary to modify
4. Files needing generated updates
5. Requirements already satisfied by existing code
6. Any uncertainty affecting the plan

Stop after this report. Do not write code.

## Phase 2 — Plan & Wait (non-trivial only)

Propose a short plan: files to modify; files to create (only if unavoidable); reused symbols; new code with one-line justification each; tests to add/update; targeted validation commands; out-of-scope items.

**Stop and wait for my approval before coding.**

## Phase 3 — Implement the approved plan

Smallest working change that satisfies the requirements. Follow nearby package/handler/component conventions and repo-local error/logging/response patterns. Exported Go symbols get doc comments. Svelte 5 uses runes; no `any`; all user-visible strings via i18n.

## Phase 4 — Targeted checks + diff audit

Run only checks scoped to changed packages/files (use the repo's actual commands). Fix every warning/failure this feature caused.

Then audit the diff file-by-file: every change is strictly required; no unrelated formatting/refactor/rename; no behavior changed outside requirements; no unexpected generated-file changes; no abstraction without 3+ call sites; tests cover changed behavior where practical. Revert anything unrelated.

## Final Response

Concise: backend changes · frontend changes · config/datastore/API changes · checks run + results · files changed · tests not run + why · remaining risks. Confirm no commit/push/preflight was performed.
