// taxonFilter.ts - Group daily-summary rows into bird / bat / other.
//
// BirdNET-Go can run the standard bird model, the BattyBirdNET bat model, and
// multi-taxa models (Perch) that also emit non-animal "sound classes" (engine,
// speech, rain, ...). The dashboard only receives names per row
// (`scientific_name`, `common_name`), not the backend's taxonomic class, so we
// reconstruct the group from the shape of the scientific name:
//
//   - Real species (birds, bats) are stored as a binomial: "Genus species".
//   - Non-bird sound classes are stored as a single lowercase token
//     ("power", "speech", "rain") or a short multi-word phrase whose first
//     token is a known non-species word ("Power tools", "Human vocal").
//   - Bats are the binomials whose genus is in the Chiroptera genus set below.
//
// Everything that is a binomial and not a bat genus is treated as a bird, which
// is the correct default for this project.

import type { DailySpeciesSummary } from '$lib/types/detection.types';

/** Taxonomic group a daily-summary row belongs to. */
export type TaxonClass = 'bird' | 'bat' | 'other';

/** Filter selection in the UI. `all` shows every row. */
export type TaxonFilter = 'all' | TaxonClass;

/** All taxon filter values in display order. */
export const TAXON_FILTERS: readonly TaxonFilter[] = ['all', 'bird', 'bat', 'other'];

// Genus names (the first word of the scientific name, lower-cased) that belong
// to bats (order Chiroptera). Covers the European and North American genera the
// BattyBirdNET models classify, plus common global genera so the filter still
// works with custom bat classifiers. Bat genera do not collide with any bird
// genus, so a genus match is an unambiguous signal.
const BAT_GENERA: ReadonlySet<string> = new Set([
  // Europe (BattyBirdNET-Pi)
  'rhinolophus',
  'myotis',
  'pipistrellus',
  'hypsugo',
  'nyctalus',
  'eptesicus',
  'vespertilio',
  'plecotus',
  'barbastella',
  'miniopterus',
  'tadarida',
  // North America
  'lasiurus',
  'aeorestes',
  'dasypterus',
  'lasionycteris',
  'perimyotis',
  'parastrellus',
  'corynorhinus',
  'antrozous',
  'nycticeius',
  'euderma',
  'idionycteris',
  'eumops',
  'nyctinomops',
  'molossus',
  // Other global genera (defensive coverage for custom models)
  'rhinopoma',
  'hipposideros',
  'rousettus',
  'pteropus',
  'saccopteryx',
  'noctilio',
  'pteronotus',
  'mormoops',
  'macrotus',
  'desmodus',
  'carollia',
  'artibeus',
  'sturnira',
  'scotophilus',
  'chalinolobus',
  'mormopterus',
  'chaerephon',
  'murina',
  'kerivoula',
  'otomops',
]);

// First tokens of multi-word non-bird sound classes that the single-token rule
// below would otherwise miss (e.g. BirdNET's "Human vocal", "Power tools").
// Single-token classes ("dog", "engine", "siren") are already caught by shape.
const NONBIRD_FIRST_TOKENS: ReadonlySet<string> = new Set(['human', 'power']);

/** Row fields needed to classify a taxon. */
export interface TaxonNamed {
  scientific_name: string;
}

/**
 * Resolves the taxonomic group for a row from its scientific name.
 * Empty names fall back to `bird` (the project default).
 */
export function classifyTaxon(item: TaxonNamed): TaxonClass {
  const sci = item.scientific_name.trim();
  if (sci === '') return 'bird';

  const firstSpace = sci.indexOf(' ');
  // No space => single-token sound class ("power", "speech", "rain").
  if (firstSpace === -1) return 'other';

  const firstToken = sci.slice(0, firstSpace).toLowerCase();
  if (BAT_GENERA.has(firstToken)) return 'bat';
  if (NONBIRD_FIRST_TOKENS.has(firstToken)) return 'other';
  return 'bird';
}

/** Returns true when a row should be shown under the given filter. */
export function matchesTaxonFilter(item: TaxonNamed, filter: TaxonFilter): boolean {
  return filter === 'all' || classifyTaxon(item) === filter;
}

/** Filters a daily-summary list by taxon group, preserving order. */
export function filterByTaxon(
  data: DailySpeciesSummary[],
  filter: TaxonFilter
): DailySpeciesSummary[] {
  if (filter === 'all') return data;
  return data.filter(item => classifyTaxon(item) === filter);
}

/** Row counts per group, used to label and gate the filter options. */
export interface TaxonCounts {
  all: number;
  bird: number;
  bat: number;
  other: number;
}

/** Counts how many rows fall into each taxon group. */
export function taxonCounts(data: DailySpeciesSummary[]): TaxonCounts {
  const counts: TaxonCounts = { all: data.length, bird: 0, bat: 0, other: 0 };
  for (const item of data) {
    counts[classifyTaxon(item)] += 1;
  }
  return counts;
}
