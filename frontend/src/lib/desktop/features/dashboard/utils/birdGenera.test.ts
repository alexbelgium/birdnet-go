import { describe, expect, it } from 'vitest';
import { BIRD_GENERA } from './birdGenera';

// These guard the generated data file (see birdGenera.ts). They do not assert an
// exact count - that drifts with model/label updates - but they catch a botched
// regeneration: an empty set, stray casing/whitespace, missing common birds, or
// the BirdNET non-bird entries leaking back in.
describe('BIRD_GENERA', () => {
  it('contains a realistic number of genera', () => {
    // BirdNET GLOBAL 6K V2.4 has ~1.8k bird genera after excluding non-birds.
    expect(BIRD_GENERA.size).toBeGreaterThan(1500);
  });

  it('holds only lower-cased single-token genera', () => {
    for (const genus of BIRD_GENERA) {
      expect(genus).toBe(genus.toLowerCase());
      expect(genus).not.toContain(' ');
      expect(genus.length).toBeGreaterThan(0);
    }
  });

  it('includes well-known bird genera', () => {
    for (const genus of ['turdus', 'erithacus', 'falco', 'parus', 'corvus']) {
      expect(BIRD_GENERA.has(genus)).toBe(true);
    }
  });

  it('excludes the BirdNET mammal and noise classes', () => {
    // Mammals shipped in the BirdNET label set + sound/noise tokens must be gone,
    // otherwise a coyote or a "Power tools" label would classify as a bird.
    for (const nonBird of [
      'canis',
      'sciurus',
      'tamias',
      'tamiasciurus',
      'dog',
      'engine',
      'environmental',
      'fireworks',
      'gun',
      'human',
      'noise',
      'power',
      'siren',
    ]) {
      expect(BIRD_GENERA.has(nonBird)).toBe(false);
    }
  });

  it('excludes mammals that are not part of the BirdNET label set', () => {
    // Foxes/deer from multi-taxa or custom models were never bird genera.
    expect(BIRD_GENERA.has('vulpes')).toBe(false);
    expect(BIRD_GENERA.has('capreolus')).toBe(false);
  });
});
