# Plan — add "Infrequent" category to NewSpeciesHighlightsCard

Handoff spec. Opus planned. Sonnet implements. Self-contained: all new logic in ONE
new file; the `.svelte` only gets thin wiring edits so the custom card stays mergeable.

## Goal

Add a 4th highlight category **Infrequent** = species returning after a long absence
(`days_since_last_seen > 14`). Own badge label + own color + own icon.

## Locked decisions (from user)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Precedence when row is both "new" and gap>14 | **Fallback / lowest** — infrequent only claims rows NOT already lifetime/year/season. Surfaces rare returning visitors the card currently hides. |
| 2 | Label storage | **Hardcoded English string in the new file.** No i18n keys → zero edits to 15 locale JSONs + generated types. |
| 3 | Badge color | **Yellow** `#eab308` (yellow-500). NOTE: close to amber `--color-warning` used by "lifetime"; tweak if too similar. |
| 4 | Date scope | **Today only.** `days_since_last_seen` reflects live tracker state, reliable only for today (matches existing "Last seen" stat gating). |

## Backend facts (verified)

- `DailySpeciesSummary.days_since_last_seen?: number` = absence gap BEFORE this return.
- API (`internal/api/v2/analytics.go:618`) emits it only when `> 0`; first-ever (`-1`)
  and same-day (`0`) are omitted → undefined on the row.
- So `(item.days_since_last_seen ?? 0) > 14` is correct and safe.
- `> 14` = strictly 15+ days = "more than 14 days ago".

## Existing taxonomy (do NOT touch)

`utils/noveltyCategory.ts` — shared by DailySummaryCard too. Leave as-is:
- `type NoveltyCategory = 'lifetime' | 'year' | 'season'`
- `resolveNoveltyCategory(item) -> NoveltyCategory | null` (lifetime>year>season, null if none)
- `noveltyCategoryColorVar(cat)` -> `var(--color-warning|info|success)`

New file WRAPS these. DailySummaryCard untouched.

---

## STEP 1 — create new file

`frontend/src/lib/desktop/features/dashboard/utils/highlightCategory.ts`

```ts
// highlightCategory.ts - Self-contained extension of the dashboard novelty taxonomy.
//
// Wraps the shared noveltyCategory helpers (lifetime / year / season) and adds a
// local "infrequent" category for species returning after a long absence. Kept in
// a standalone file so the customised NewSpeciesHighlightsCard stays easy to merge:
// the category type, threshold, precedence, accent color, label and icon all live
// here; the card only wires generic calls to them.

import { CalendarDays, History, Leaf, Star, type IconProps } from '@lucide/svelte';
import type { Component } from 'svelte';
import { t } from '$lib/i18n';
import type { DailySpeciesSummary } from '$lib/types/detection.types';
import {
  noveltyCategoryColorVar,
  resolveNoveltyCategory,
  type NoveltyCategory,
} from './noveltyCategory';

/** Novelty categories plus the local "infrequent" (returned after a long absence). */
export type HighlightCategory = NoveltyCategory | 'infrequent';

/** A return counts as "infrequent" when the absence gap exceeds this many days. */
export const INFREQUENT_THRESHOLD_DAYS = 14;

/**
 * Label for the infrequent badge. Intentionally a plain string rather than an i18n
 * key so this custom card needs no changes to the 15 locale files / generated types.
 */
const INFREQUENT_LABEL = 'Infrequent';

/**
 * Accent for the infrequent badge (left border + icon tint). Hard-coded yellow so it
 * stays distinct from the amber "lifetime" / blue "year" / green "season" accents.
 */
const INFREQUENT_COLOR = '#eab308'; // yellow-500

/** Sort precedence; infrequent is last so it never outranks a genuine novelty. */
export const highlightCategoryRank: Record<HighlightCategory, number> = {
  lifetime: 0,
  year: 1,
  season: 2,
  infrequent: 3,
};

/**
 * Highest-precedence highlight category for a row, or null when it should not be
 * highlighted. A real novelty (lifetime/year/season) always wins; only when none
 * applies — and only for "today", since the absence gap reflects live tracker
 * state — does a return gap over the threshold yield "infrequent".
 */
export function resolveHighlightCategory(
  item: DailySpeciesSummary,
  isToday: boolean
): HighlightCategory | null {
  const novelty = resolveNoveltyCategory(item);
  if (novelty !== null) return novelty;
  if (isToday && (item.days_since_last_seen ?? 0) > INFREQUENT_THRESHOLD_DAYS) {
    return 'infrequent';
  }
  return null;
}

/** Accent color (left border / icon tint) for a highlight category. */
export function highlightCategoryColorVar(category: HighlightCategory): string {
  return category === 'infrequent' ? INFREQUENT_COLOR : noveltyCategoryColorVar(category);
}

/** Human-readable label; "season" may carry a localized season name. */
export function highlightCategoryLabel(category: HighlightCategory, season?: string): string {
  switch (category) {
    case 'lifetime':
      return t('dashboard.newSpeciesHighlights.categoryLifetime');
    case 'year':
      return t('dashboard.newSpeciesHighlights.categoryYear');
    case 'season':
      return season
        ? t('dashboard.newSpeciesHighlights.categorySeasonNamed', { season })
        : t('dashboard.newSpeciesHighlights.categorySeason');
    case 'infrequent':
      return INFREQUENT_LABEL;
  }
}

/** Lucide icon component for a category (rendered via {@const} in the card). */
export function highlightCategoryIcon(category: HighlightCategory): Component<IconProps> {
  switch (category) {
    case 'lifetime':
      return Star;
    case 'year':
      return CalendarDays;
    case 'season':
      return Leaf;
    case 'infrequent':
      return History;
  }
}
```

Icon-as-value pattern is idiomatic here (see `EmptyState.svelte`, `SelectDropdown.svelte`).

---

## STEP 2 — edit NewSpeciesHighlightsCard.svelte (8 surgical edits)

### Edit A — imports (current lines 19-24)

REMOVE the `noveltyCategory` import block AND trim the lucide import.

Before:
```ts
  import {
    resolveNoveltyCategory,
    noveltyCategoryColorVar,
    type NoveltyCategory,
  } from '$lib/desktop/features/dashboard/utils/noveltyCategory';
  import { AudioLines, CalendarDays, Leaf, Star } from '@lucide/svelte';
```
After:
```ts
  import {
    resolveHighlightCategory,
    highlightCategoryColorVar,
    highlightCategoryLabel,
    highlightCategoryIcon,
    highlightCategoryRank,
    type HighlightCategory,
  } from '$lib/desktop/features/dashboard/utils/highlightCategory';
  import { AudioLines } from '@lucide/svelte';
```

### Edit B — Highlight interface (line ~48)
`category: NoveltyCategory;` → `category: HighlightCategory;`

### Edit C — delete local rank (line 51)
DELETE: `const categoryRank: Record<NoveltyCategory, number> = { lifetime: 0, year: 1, season: 2 };`
(replaced by imported `highlightCategoryRank`)

### Edit D — highlights derived (lines ~59 and ~63)
- `const category = resolveNoveltyCategory(species);` → `const category = resolveHighlightCategory(species, isToday);`
- `const rankDiff = categoryRank[a.category] - categoryRank[b.category];`
  → `const rankDiff = highlightCategoryRank[a.category] - highlightCategoryRank[b.category];`

### Edit E — delete local categoryLabel function (lines 71-82)
DELETE the whole `function categoryLabel(...) { switch ... }`. All call sites move to
the imported `highlightCategoryLabel`.

### Edit F — categoryIcon snippet (lines 100-115)
Before:
```svelte
{#snippet categoryIcon(category: NoveltyCategory, season: string | undefined)}
  <span
    class="shrink-0"
    style:color={noveltyCategoryColorVar(category)}
    title={categoryLabel(category, season)}
    aria-label={categoryLabel(category, season)}
  >
    {#if category === 'lifetime'}
      <Star class="size-3.5 fill-current" />
    {:else if category === 'year'}
      <CalendarDays class="size-3.5" />
    {:else}
      <Leaf class="size-3.5" />
    {/if}
  </span>
{/snippet}
```
After:
```svelte
{#snippet categoryIcon(category: HighlightCategory, season: string | undefined)}
  {@const IconComponent = highlightCategoryIcon(category)}
  <span
    class="shrink-0"
    style:color={highlightCategoryColorVar(category)}
    title={highlightCategoryLabel(category, season)}
    aria-label={highlightCategoryLabel(category, season)}
  >
    <IconComponent class="size-3.5 {category === 'lifetime' ? 'fill-current' : ''}" />
  </span>
{/snippet}
```

### Edit G — anchor title (line ~143)
`title={categoryLabel(category, species.current_season)}`
→ `title={highlightCategoryLabel(category, species.current_season)}`

### Edit H — left border color (line ~142)
`style:border-left-color={noveltyCategoryColorVar(category)}`
→ `style:border-left-color={highlightCategoryColorVar(category)}`

After edits, confirm NO leftover references to: `resolveNoveltyCategory`,
`noveltyCategoryColorVar`, `NoveltyCategory`, `categoryRank`, `categoryLabel`,
`Star`, `CalendarDays`, `Leaf`.

---

## STEP 3 — validate

```bash
cd frontend
npm run check:all     # typecheck + eslint + ast-grep (MUST pass)
npm test              # optional
```
- Run `mcp__svelte__svelte-autofixer` on the edited `.svelte` (frontend rule).
- NO i18n changes needed (label hardcoded) → skip i18n:sync / generate:i18n-types.
- Optional unit test: `highlightCategory.test.ts` (vitest) covering
  `resolveHighlightCategory` (novelty wins; infrequent only today & gap>14; undefined gap → null)
  and `highlightCategoryColorVar('infrequent') === '#eab308'`.

## Caveats / behaviour notes

- Infrequent shows on TODAY only. Past dates: no infrequent badge (by design).
- Fallback precedence: a "new this season/year/lifetime" row never becomes infrequent.
- Yellow `#eab308` is near the amber "lifetime" accent — change `INFREQUENT_COLOR` if confusing.
- Whole card already hidden when species tracking disabled → infrequent inherits that.
- This plan file is a scratch artifact; not committed unless you choose to.
