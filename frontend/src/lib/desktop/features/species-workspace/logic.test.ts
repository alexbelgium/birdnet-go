import { describe, expect, it, vi } from 'vitest';
import { COLUMNS, DEFAULT_LAYOUT, neededGroups, normalizeLayout, visibleColumns } from './columns';
import { deleteAllUnlocked } from './deleteSpecies';
import { ebirdLanguage, ebirdSpeciesUrl } from './externalLinks';
import { formatCompactCount } from './format';
import { createRequestSlot, isAbortError } from './requestSlot';
import { foldText, matchesSearch } from './search';
import { sortRows } from './sort';
import type { DeleteChunkResult } from './types';

describe('search', () => {
  const blackbird = {
    scientificName: 'Turdus merula',
    commonName: 'Common Blackbird',
    displayName: 'Merle noir',
  };

  it('folds accents and case', () => {
    expect(foldText('Pouillot  VÉLOCE ')).toBe('pouillot  veloce');
  });

  it('matches localized, common and scientific names, accent-insensitively', () => {
    expect(matchesSearch(blackbird, 'merle')).toBe(true);
    expect(matchesSearch(blackbird, 'blackbird')).toBe(true);
    expect(matchesSearch(blackbird, 'turdus mer')).toBe(true);
    expect(matchesSearch({ ...blackbird, displayName: 'Mésange' }, 'mesange')).toBe(true);
    expect(matchesSearch(blackbird, 'robin')).toBe(false);
    expect(matchesSearch(blackbird, '   ')).toBe(true);
  });
});

describe('sortRows', () => {
  const rows = [
    { scientificName: 'B b', n: 2 },
    { scientificName: 'A a', n: null },
    { scientificName: 'C c', n: 5 },
    { scientificName: 'D d', n: 2 },
  ];

  it('sorts descending with missing values last and ties by scientific name', () => {
    expect(sortRows(rows, r => r.n, 'desc', 'en').map(r => r.scientificName)).toEqual([
      'C c',
      'B b',
      'D d',
      'A a',
    ]);
  });

  it('keeps missing values last when ascending', () => {
    expect(sortRows(rows, r => r.n, 'asc', 'en').map(r => r.scientificName)).toEqual([
      'B b',
      'D d',
      'C c',
      'A a',
    ]);
  });
});

describe('column layout', () => {
  it('defaults sort by count descending', () => {
    expect(DEFAULT_LAYOUT.sort).toEqual({ column: 'count', direction: 'desc' });
    expect(DEFAULT_LAYOUT.condensed).toBe(false);
  });

  it('normalizes untrusted layouts like the server', () => {
    const layout = normalizeLayout({
      columns: [
        { id: 'lastSeen', visible: true },
        { id: 'nope', visible: true },
        { id: 'actions', visible: false },
        { id: 'lastSeen', visible: false },
      ],
      sort: { column: 'bestRecording', direction: 'sideways' },
      condensed: true,
    });
    expect(layout.columns[0]).toEqual({ id: 'lastSeen', visible: true });
    expect(layout.columns[1]).toEqual({ id: 'actions', visible: true });
    expect(layout.columns).toHaveLength(COLUMNS.length);
    expect(layout.sort).toEqual(DEFAULT_LAYOUT.sort);
    expect(layout.condensed).toBe(true);
    expect(normalizeLayout({ condensed: 'yes' }).condensed).toBe(false);
    expect(normalizeLayout('garbage')).toEqual(DEFAULT_LAYOUT);
  });

  it('requests only the data groups of visible columns', () => {
    const onlyBasics = normalizeLayout({
      columns: COLUMNS.map(c => ({ id: c.id, visible: ['species', 'count'].includes(c.id) })),
    });
    // Mandatory "actions" needs memberships; nothing else beyond the inventory.
    expect([...neededGroups(onlyBasics)].sort()).toEqual(['inventory', 'memberships']);
    expect(visibleColumns(onlyBasics).map(c => c.id)).toEqual(['species', 'count', 'actions']);

    const withRange = normalizeLayout({
      columns: [
        ...onlyBasics.columns.filter(c => c.id !== 'range'),
        { id: 'range', visible: true },
      ],
    });
    expect(neededGroups(withRange).has('range')).toBe(true);
    expect(neededGroups(withRange).has('stats')).toBe(false);
    expect(neededGroups(withRange).has('best')).toBe(false);
  });
});

describe('formatCompactCount', () => {
  it('uses a locale-aware compact value with one fractional digit', () => {
    expect(formatCompactCount(64_000)).toBe('64K');
    expect(formatCompactCount(1_200_000)).toBe('1.2M');
  });
});

describe('request slot', () => {
  it('a newer request aborts and replaces the one in flight', async () => {
    const slot = createRequestSlot();
    const signals: AbortSignal[] = [];
    let resolveFirst: (_v: string) => void = () => {};
    const first = slot.run(
      signal =>
        new Promise<string>(resolve => {
          signals.push(signal);
          resolveFirst = resolve;
        })
    );
    const second = slot.run(async signal => {
      signals.push(signal);
      return 'second';
    });
    resolveFirst('first');
    await expect(first).rejects.toSatisfy(isAbortError);
    await expect(second).resolves.toBe('second');
    expect(signals[0]?.aborted).toBe(true);
    expect(signals[1]?.aborted).toBe(false);
  });

  it('abort() cancels the request in flight', async () => {
    const slot = createRequestSlot();
    const pending = slot.run(
      signal =>
        new Promise((_resolve, reject) => {
          signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
        })
    );
    slot.abort();
    await expect(pending).rejects.toSatisfy(isAbortError);
  });
});

describe('deleteAllUnlocked', () => {
  const chunk = (r: Partial<DeleteChunkResult>): DeleteChunkResult => ({
    deleted: 0,
    locked: 0,
    reassigned: 0,
    remaining: 0,
    ...r,
  });

  it('loops until nothing remains, with no pass cap', async () => {
    const results = Array.from({ length: 1200 }, (_, i) =>
      chunk({ deleted: 200, remaining: 1199 - i })
    );
    const fn = vi.fn(async () => results.shift() ?? chunk({}));
    const onProgress = vi.fn();
    const progress = await deleteAllUnlocked('Turdus merula', { chunk: fn, onProgress });
    expect(fn).toHaveBeenCalledTimes(1200);
    expect(progress.deleted).toBe(240_000);
    expect(onProgress).toHaveBeenLastCalledWith(expect.objectContaining({ remaining: 0 }));
  });

  it('rejects when a chunk request fails, keeping the progress so far', async () => {
    const onProgress = vi.fn();
    const fn = vi
      .fn()
      .mockResolvedValueOnce(chunk({ deleted: 10, remaining: 5 }))
      .mockRejectedValueOnce(new Error('HTTP 500'));
    await expect(deleteAllUnlocked('X', { chunk: fn, onProgress })).rejects.toThrow('HTTP 500');
    expect(onProgress).toHaveBeenLastCalledWith(expect.objectContaining({ deleted: 10 }));
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it('stops instead of looping forever when a chunk makes no progress', async () => {
    const fn = vi.fn().mockResolvedValue(chunk({ remaining: 4 }));
    const error = await deleteAllUnlocked('X', { chunk: fn }).catch(e => e);
    expect(error.reason).toBe('stalled');
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('can be cancelled between chunks', async () => {
    const controller = new AbortController();
    const fn = vi.fn(async () => {
      controller.abort();
      return chunk({ deleted: 1, remaining: 10 });
    });
    await expect(
      deleteAllUnlocked('X', { chunk: fn, signal: controller.signal })
    ).rejects.toSatisfy(isAbortError);
    expect(fn).toHaveBeenCalledTimes(1);
  });
});

describe('eBird link', () => {
  it('builds the BE-WAL species URL with the mapped site language', () => {
    expect(ebirdSpeciesUrl('eurbla', 'fr')).toBe(
      'https://ebird.org/species/eurbla/BE-WAL?siteLanguage=fr'
    );
    expect(ebirdLanguage('nb')).toBe('no');
    expect(ebirdLanguage('pt')).toBe('pt_PT');
    expect(ebirdLanguage('sk')).toBe('en');
    expect(ebirdLanguage('xx')).toBe('en');
    expect(ebirdSpeciesUrl('', 'en')).toBeNull();
  });
});
