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

// Label for the infrequent badge. Plain string — avoids touching 15 locale files.
const INFREQUENT_LABEL = 'Infrequent';

// Accent for the infrequent badge (left border + icon tint). Hard-coded yellow-500
// to stay distinct from amber "lifetime" / blue "year" / green "season" accents.
const INFREQUENT_COLOR = '#eab308';

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
