import { describe, expect, it, vi } from 'vitest';
import { COLUMNS, DEFAULT_LAYOUT, neededGroups, normalizeLayout, visibleColumns } from './columns';
import { deleteAllUnlocked, SpeciesDeleteError } from './deleteSpecies';
import { ebirdLanguage, ebirdSpeciesUrl } from './externalLinks';
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
    });
    expect(layout.columns[0]).toEqual({ id: 'lastSeen', visible: true });
    expect(layout.columns[1]).toEqual({ id: 'actions', visible: true });
    expect(layout.columns).toHaveLength(COLUMNS.length);
    expect(layout.sort).toEqual(DEFAULT_LAYOUT.sort);
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
    failedIds: [],
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

  it('stops on failed rows and reports them for a retry', async () => {
    const fn = vi
      .fn()
      .mockResolvedValueOnce(chunk({ deleted: 10, remaining: 5 }))
      .mockResolvedValueOnce(chunk({ deleted: 3, failedIds: ['7', '8'], remaining: 2 }));
    const error = await deleteAllUnlocked('X', { chunk: fn }).catch(e => e);
    expect(error).toBeInstanceOf(SpeciesDeleteError);
    expect(error.reason).toBe('failed');
    expect(error.failedIds).toEqual(['7', '8']);
    expect(error.progress.deleted).toBe(13);
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
