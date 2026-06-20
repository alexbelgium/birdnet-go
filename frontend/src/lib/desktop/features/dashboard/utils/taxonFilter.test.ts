import { describe, expect, it } from 'vitest';
import type { DailySpeciesSummary } from '$lib/types/detection.types';
import {
  classifyTaxon,
  filterByTaxon,
  matchesTaxonFilter,
  taxonCounts,
  TAXON_FILTERS,
} from './taxonFilter';

const makeItem = (scientific_name: string, count = 1): DailySpeciesSummary => ({
  scientific_name,
  common_name: scientific_name,
  species_code: 'xxxxxx',
  count,
  hourly_counts: Array(24).fill(0) as number[],
  high_confidence: false,
  first_heard: '',
  latest_heard: '',
  thumbnail_url: '',
});

describe('classifyTaxon', () => {
  it('classifies a binomial bird name as bird', () => {
    expect(classifyTaxon({ scientific_name: 'Turdus merula' })).toBe('bird');
  });

  it('classifies a bat genus binomial as bat', () => {
    expect(classifyTaxon({ scientific_name: 'Pipistrellus pipistrellus' })).toBe('bat');
    expect(classifyTaxon({ scientific_name: 'Myotis daubentonii' })).toBe('bat');
    expect(classifyTaxon({ scientific_name: 'Nyctalus noctula' })).toBe('bat');
  });

  it('is case-insensitive on the bat genus', () => {
    expect(classifyTaxon({ scientific_name: 'PIPISTRELLUS pipistrellus' })).toBe('bat');
  });

  it('classifies a single-token sound class as other', () => {
    expect(classifyTaxon({ scientific_name: 'power' })).toBe('other');
    expect(classifyTaxon({ scientific_name: 'speech' })).toBe('other');
    expect(classifyTaxon({ scientific_name: 'rain' })).toBe('other');
    expect(classifyTaxon({ scientific_name: 'dog' })).toBe('other');
  });

  it('classifies multi-word non-bird labels as other', () => {
    expect(classifyTaxon({ scientific_name: 'Human vocal' })).toBe('other');
    expect(classifyTaxon({ scientific_name: 'Power tools' })).toBe('other');
  });

  it('does not misclassify a bird genus that resembles nothing in the bat set', () => {
    expect(classifyTaxon({ scientific_name: 'Falco rufigularis' })).toBe('bird'); // Bat Falcon
  });

  it('falls back to bird for empty names', () => {
    expect(classifyTaxon({ scientific_name: '' })).toBe('bird');
    expect(classifyTaxon({ scientific_name: '   ' })).toBe('bird');
  });
});

describe('matchesTaxonFilter', () => {
  it('matches everything under the all filter', () => {
    expect(matchesTaxonFilter({ scientific_name: 'power' }, 'all')).toBe(true);
    expect(matchesTaxonFilter({ scientific_name: 'Turdus merula' }, 'all')).toBe(true);
  });

  it('matches only the selected group', () => {
    expect(matchesTaxonFilter({ scientific_name: 'Myotis daubentonii' }, 'bat')).toBe(true);
    expect(matchesTaxonFilter({ scientific_name: 'Turdus merula' }, 'bat')).toBe(false);
  });
});

describe('filterByTaxon', () => {
  const data = [
    makeItem('Turdus merula'),
    makeItem('Erithacus rubecula'),
    makeItem('Pipistrellus pipistrellus'),
    makeItem('rain'),
  ];

  it('returns the same array reference for all', () => {
    expect(filterByTaxon(data, 'all')).toBe(data);
  });

  it('keeps only birds', () => {
    expect(filterByTaxon(data, 'bird').map(d => d.scientific_name)).toEqual([
      'Turdus merula',
      'Erithacus rubecula',
    ]);
  });

  it('keeps only bats', () => {
    expect(filterByTaxon(data, 'bat').map(d => d.scientific_name)).toEqual([
      'Pipistrellus pipistrellus',
    ]);
  });

  it('keeps only others', () => {
    expect(filterByTaxon(data, 'other').map(d => d.scientific_name)).toEqual(['rain']);
  });
});

describe('taxonCounts', () => {
  it('counts each group and the total', () => {
    const data = [
      makeItem('Turdus merula'),
      makeItem('Erithacus rubecula'),
      makeItem('Pipistrellus pipistrellus'),
      makeItem('rain'),
    ];
    expect(taxonCounts(data)).toEqual({ all: 4, bird: 2, bat: 1, other: 1 });
  });

  it('handles empty data', () => {
    expect(taxonCounts([])).toEqual({ all: 0, bird: 0, bat: 0, other: 0 });
  });
});

describe('TAXON_FILTERS', () => {
  it('lists all four options in display order', () => {
    expect(TAXON_FILTERS).toEqual(['all', 'bird', 'bat', 'other']);
  });
});
