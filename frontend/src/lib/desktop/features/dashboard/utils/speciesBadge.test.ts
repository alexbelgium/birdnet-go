import { describe, expect, it } from 'vitest';
import { BADGE_COLORS, getSpeciesBadgeColor, getSpeciesInitials } from './speciesBadge';

describe('getSpeciesBadgeColor', () => {
  it('returns a palette color', () => {
    expect(BADGE_COLORS).toContain(getSpeciesBadgeColor('Turdus merula'));
  });

  it('is deterministic for the same name', () => {
    expect(getSpeciesBadgeColor('Parus major')).toBe(getSpeciesBadgeColor('Parus major'));
  });

  it('handles the empty string', () => {
    expect(BADGE_COLORS).toContain(getSpeciesBadgeColor(''));
  });
});

describe('getSpeciesInitials', () => {
  it('uses the first letter of the first two words', () => {
    expect(getSpeciesInitials('Eurasian Blackbird')).toBe('EB');
  });

  it('uses the first two letters of a single word', () => {
    expect(getSpeciesInitials('Dunnock')).toBe('DU');
  });

  it('ignores extra whitespace', () => {
    expect(getSpeciesInitials('  Great   Tit  ')).toBe('GT');
  });

  it('returns ?? for empty input', () => {
    expect(getSpeciesInitials('   ')).toBe('??');
  });
});
