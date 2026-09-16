/** eBird and Observations.be links for a species. */
import { isValidEbirdCode } from '$lib/desktop/features/dashboard/utils/dailySummaryStats';

/** eBird region the species page opens on. */
export const EBIRD_REGION = 'BE-WAL';

// BirdNET-Go UI locale → eBird `siteLanguage` value, verified against ebird.org's
// language selector (2026-09). eBird has no Latvian or Slovak, so those fall back to English.
const EBIRD_LANGUAGES: Record<string, string> = {
  en: 'en',
  cs: 'cs',
  da: 'da',
  de: 'de',
  es: 'es',
  fi: 'fi',
  fr: 'fr',
  hu: 'hu',
  it: 'it',
  nb: 'no',
  nl: 'nl',
  pl: 'pl',
  pt: 'pt_PT',
  sv: 'sv',
};

export function ebirdLanguage(locale: string): string {
  const base = locale.toLowerCase().split(/[-_]/)[0] ?? 'en';
  return Object.hasOwn(EBIRD_LANGUAGES, base) ? (EBIRD_LANGUAGES[base] ?? 'en') : 'en';
}

/** Regional eBird species page, or null when the species has no valid eBird code. */
export function ebirdSpeciesUrl(speciesCode: string, locale: string): string | null {
  if (!isValidEbirdCode(speciesCode)) return null;
  return `https://ebird.org/species/${encodeURIComponent(speciesCode)}/${EBIRD_REGION}?siteLanguage=${ebirdLanguage(locale)}`;
}

/** Observations.be species page from the committed id map, else its search page. */
export async function observationsUrl(scientificName: string): Promise<string> {
  const { default: ids } = (await import('./data/observationsIds.json')) as {
    default: Record<string, number>;
  };
  const id = Object.hasOwn(ids, scientificName) ? ids[scientificName] : undefined;
  return typeof id === 'number'
    ? `https://observations.be/species/${id}/`
    : `https://observations.be/species/search/?q=${encodeURIComponent(scientificName)}`;
}
