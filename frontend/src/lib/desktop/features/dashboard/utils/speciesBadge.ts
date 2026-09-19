// speciesBadge.ts - Shared species badge helpers for the daily summary views.
//
// One palette + one deterministic hash so the desktop heatmap and the mobile
// summary table always render the same badge color/initials for a species.

/** Species badge color palette - 12 distinct, visually appealing colors. */
export const BADGE_COLORS: readonly string[] = [
  '#10b981', // emerald
  '#f59e0b', // amber
  '#ef4444', // red
  '#8b5cf6', // violet
  '#06b6d4', // cyan
  '#ec4899', // pink
  '#84cc16', // lime
  '#f97316', // orange
  '#6366f1', // indigo
  '#14b8a6', // teal
  '#a855f7', // purple
  '#eab308', // yellow
];

// Fallback when index access is typed as possibly undefined (never hit at runtime).
const DEFAULT_BADGE_COLOR = '#10b981';

/** Generates a consistent badge color for a species based on its name. */
export function getSpeciesBadgeColor(speciesName: string): string {
  let hash = 0;
  for (let i = 0; i < speciesName.length; i++) {
    hash = speciesName.charCodeAt(i) + ((hash << 5) - hash);
  }
  return BADGE_COLORS[Math.abs(hash) % BADGE_COLORS.length] ?? DEFAULT_BADGE_COLOR;
}

/** Initials shown in the badge: first letter of the first two words. */
export function getSpeciesInitials(commonName: string): string {
  const words = commonName.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return '??';
  if (words.length === 1) return (words[0] ?? '').substring(0, 2).toUpperCase();
  return ((words[0] ?? '')[0] + (words[1] ?? '')[0]).toUpperCase();
}
