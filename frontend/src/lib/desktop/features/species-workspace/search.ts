/** Accent- and case-insensitive species search over localized, common and scientific names. */

/** Folds text for matching: strips diacritics and lowercases. */
export function foldText(text: string): string {
  return text.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase().trim();
}

export interface SearchableSpecies {
  scientificName: string;
  commonName: string;
  displayName: string;
}

/** True when every whitespace-separated query token appears in one of the names. */
export function matchesSearch(item: SearchableSpecies, query: string): boolean {
  const tokens = foldText(query).split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return true;
  const haystack = [item.displayName, item.commonName, item.scientificName]
    .map(foldText)
    .join('\n');
  return tokens.every(token => haystack.includes(token));
}
