// taxonFilter.ts - Group daily-summary rows into bird / bat / other.
//
// BirdNET-Go can run the standard bird model, the BattyBirdNET bat model, and
// multi-taxa models (Perch and custom ones) that also emit mammals (foxes,
// deer, ...) and non-animal "sound classes" (engine, speech, rain, ...). The
// dashboard only receives names per row (`scientific_name`, `common_name`), not
// the backend's taxonomic class, so we reconstruct the group from the genus
// (first token of the scientific name) using two allow-lists:
//
//   - genus in the Chiroptera set (BAT_GENERA below)    -> bat
//   - genus in the BirdNET bird-genus set (BIRD_GENERA) -> bird
//   - everything else                                   -> other
//
// "Other" is the default on purpose. A binomial is only a bird when its genus is
// an actual BirdNET bird genus; a mammal such as Vulpes vulpes (red fox) shares
// the "Genus species" shape but is in neither allow-list, so it falls through to
// "other" instead of being mislabelled a bird. Single-token sound classes
// ("engine", "rain") and multi-word noise labels ("Power tools") are likewise in
// neither allow-list, so they land in "other" too.

import type { DailySpeciesSummary } from '$lib/types/detection.types';
import { BIRD_GENERA } from './birdGenera';

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

/** Row fields needed to classify a taxon. */
export interface TaxonNamed {
  scientific_name: string;
}

/**
 * Resolves the taxonomic group for a row from its scientific name's genus
 * (first token). Bat genera win first, then BirdNET bird genera; anything else
 * (mammals, sound classes, empty/single-token names) is `other`.
 */
export function classifyTaxon(item: TaxonNamed): TaxonClass {
  const sci = item.scientific_name.trim();
  if (sci === '') return 'other';

  const firstSpace = sci.indexOf(' ');
  // No space => single-token sound class ("engine", "rain"); never a species.
  if (firstSpace === -1) return 'other';

  const firstToken = sci.slice(0, firstSpace).toLowerCase();
  if (BAT_GENERA.has(firstToken)) return 'bat';
  if (BIRD_GENERA.has(firstToken)) return 'bird';
  return 'other';
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
