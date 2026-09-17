import { beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { DEFAULT_LAYOUT } from './columns';
import { createLayoutStore, LAYOUT_CACHE_KEY } from './layout.svelte';
import { BEST_BATCH_SIZE, createWorkspaceData, type WorkspaceApi } from './workspaceData.svelte';
import type { WorkspaceLayout } from './types';

vi.mock(
  '$lib/desktop/features/dashboard/components/daily-summary/SpeciesHistoryModal.svelte',
  () => ({ invalidateSpeciesHistory: vi.fn() })
);
vi.mock('$lib/stores/excludedSpecies.svelte', () => ({ hydrateExcludedSpecies: vi.fn() }));

const tick = () => new Promise(resolve => setTimeout(resolve, 0));

function deferred<T>() {
  let resolve: (_v: T) => void = () => {};
  let reject: (_e: unknown) => void = () => {};
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function fakeApi(overrides: Partial<WorkspaceApi> = {}): WorkspaceApi {
  return {
    fetchSpecies: vi.fn(async () => [
      {
        scientificName: 'Turdus merula',
        commonName: 'Blackbird',
        speciesCode: 'eurbla',
        total: 3,
        locked: 1,
        firstSeen: '',
        lastSeen: '',
      },
    ]),
    fetchStats: vi.fn(async () => []),
    fetchMemberships: vi.fn(async () => ({
      confirmed: ['Turdus merula'],
      included: [],
      excluded: [],
    })),
    fetchRangeScores: vi.fn(async () => new Map<string, number>()),
    fetchBestRecordings: vi.fn(async (names: string[]) =>
      Object.fromEntries(names.map(n => [n, null]))
    ),
    putMembership: vi.fn(async (kind, scientificName, present) => ({
      kind,
      scientificName,
      present,
    })),
    ...overrides,
  };
}

describe('workspace data tiers', () => {
  it('loads only the groups that visible columns need', async () => {
    const api = fakeApi();
    const data = createWorkspaceData(api);
    data.ensure(new Set(['inventory', 'memberships']));
    await tick();
    expect(api.fetchSpecies).toHaveBeenCalledOnce();
    expect(api.fetchMemberships).toHaveBeenCalledOnce();
    expect(api.fetchStats).not.toHaveBeenCalled();
    expect(api.fetchRangeScores).not.toHaveBeenCalled();
    expect(api.fetchBestRecordings).not.toHaveBeenCalled();

    // Showing a column later fetches its group once.
    data.ensure(new Set(['inventory', 'memberships', 'range']));
    data.ensure(new Set(['inventory', 'memberships', 'range']));
    await tick();
    expect(api.fetchRangeScores).toHaveBeenCalledOnce();
    expect(api.fetchSpecies).toHaveBeenCalledOnce();
  });

  it('aborts a group whose column is hidden while it loads', async () => {
    const pending = deferred<never>();
    let signal: AbortSignal | undefined;
    const api = fakeApi({
      fetchStats: vi.fn((s?: AbortSignal) => {
        signal = s;
        return pending.promise;
      }),
    });
    const data = createWorkspaceData(api);
    data.ensure(new Set(['inventory', 'stats']));
    expect(data.status.stats).toBe('loading');
    data.ensure(new Set(['inventory']));
    expect(signal?.aborted).toBe(true);
    expect(data.status.stats).toBe('idle');
  });

  it('keeps membership toggles unavailable until the lists load', async () => {
    const lists = deferred<{ confirmed: string[]; included: string[]; excluded: string[] }>();
    const api = fakeApi({ fetchMemberships: vi.fn(() => lists.promise) });
    const data = createWorkspaceData(api);
    data.ensure(new Set(['inventory', 'memberships']));
    expect(data.isMember('confirmed', 'Turdus merula')).toBeNull();
    lists.resolve({ confirmed: ['turdus merula'], included: [], excluded: [] });
    await tick();
    expect(data.isMember('confirmed', 'Turdus merula')).toBe(true);
    expect(data.isMember('included', 'Turdus merula')).toBe(false);
  });

  it('reports a load error and recovers on retry', async () => {
    const api = fakeApi({
      fetchStats: vi.fn().mockRejectedValueOnce(new Error('boom')).mockResolvedValue([]),
    });
    const data = createWorkspaceData(api);
    data.ensure(new Set(['stats']));
    await tick();
    expect(data.status.stats).toBe('error');
    data.retry('stats');
    await tick();
    expect(data.status.stats).toBe('ready');
  });

  it('applies the server state after a membership change', async () => {
    const api = fakeApi();
    const data = createWorkspaceData(api);
    data.ensure(new Set(['memberships']));
    await tick();
    await data.setMembership('confirmed', 'Turdus merula', false);
    expect(api.putMembership).toHaveBeenCalledWith('confirmed', 'Turdus merula', false);
    expect(data.isMember('confirmed', 'Turdus merula')).toBe(false);
  });

  it('loads best recordings in serialized batches without dropping any', async () => {
    const calls: string[][] = [];
    const gates: Array<{ resolve: () => void }> = [];
    const api = fakeApi({
      fetchBestRecordings: vi.fn(async (names: string[]) => {
        calls.push(names);
        const gate = deferred<undefined>();
        gates.push({ resolve: () => gate.resolve(undefined) });
        await gate.promise;
        return Object.fromEntries(names.map(n => [n, { id: 1, confidence: 0.9, locked: false }]));
      }),
    });
    const data = createWorkspaceData(api);
    const names = Array.from({ length: BEST_BATCH_SIZE + 5 }, (_, i) => `Species ${i}`);
    data.requestBest(names.slice(0, 10));
    data.requestBest(names); // overlapping request while the first batch runs
    await tick();
    expect(calls).toHaveLength(1);
    gates[0]?.resolve();
    await tick();
    await tick();
    expect(calls).toHaveLength(2);
    gates[1]?.resolve();
    await tick();
    await tick();
    expect(new Set(calls.flat())).toEqual(new Set(names));
    expect(calls.flat()).toHaveLength(names.length);
    expect(data.best.size).toBe(names.length);
  });
});

describe('layout store', () => {
  const custom: WorkspaceLayout = {
    columns: DEFAULT_LAYOUT.columns.map(c => (c.id === 'range' ? { ...c, visible: true } : c)),
    sort: { column: 'lastSeen', direction: 'asc' },
    condensed: true,
  };

  beforeEach(() => {
    localStorage.clear();
  });

  it('renders from the cache, then adopts the server layout', async () => {
    localStorage.setItem(LAYOUT_CACHE_KEY, JSON.stringify(custom));
    const server: WorkspaceLayout = {
      ...DEFAULT_LAYOUT,
      sort: { column: 'species', direction: 'asc' },
    };
    const store = createLayoutStore({ fetch: vi.fn(async () => server), save: vi.fn() });
    expect(store.layout.sort).toEqual(custom.sort);
    await store.load();
    flushSync();
    expect(store.layout.sort).toEqual(server.sort);
    expect(JSON.parse(localStorage.getItem(LAYOUT_CACHE_KEY) ?? '{}').sort).toEqual(server.sort);
  });

  it('persists a change through the dedicated endpoint', async () => {
    const save = vi.fn(async (l: WorkspaceLayout) => l);
    const store = createLayoutStore({ fetch: vi.fn(), save });
    await store.save(custom);
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ sort: custom.sort }));
    expect(store.layout.sort).toEqual(custom.sort);
    expect(store.layout.condensed).toBe(true);
    expect(store.saveError).toBe(false);
  });

  it('rolls back and reports when saving fails', async () => {
    const store = createLayoutStore({
      fetch: vi.fn(),
      save: vi.fn().mockRejectedValue(new Error('x')),
    });
    await store.save(custom);
    expect(store.layout.sort).toEqual(DEFAULT_LAYOUT.sort);
    expect(store.saveError).toBe(true);
  });

  it('applies only the newest save when saves overlap', async () => {
    const replies: Array<(_l: WorkspaceLayout) => void> = [];
    const save = vi.fn(
      (l: WorkspaceLayout) =>
        new Promise<WorkspaceLayout>(resolve => replies.push(() => resolve(l)))
    );
    const store = createLayoutStore({ fetch: vi.fn(), save });
    const first = store.save({ ...DEFAULT_LAYOUT, sort: { column: 'lastSeen', direction: 'asc' } });
    const second = store.save({ ...DEFAULT_LAYOUT, sort: { column: 'species', direction: 'asc' } });
    replies[1]?.(DEFAULT_LAYOUT);
    replies[0]?.(DEFAULT_LAYOUT);
    await Promise.all([first, second]);
    expect(store.layout.sort).toEqual({ column: 'species', direction: 'asc' });
    expect(store.saving).toBe(false);
  });

  it('reset restores the default layout', async () => {
    const save = vi.fn(async (l: WorkspaceLayout) => l);
    const store = createLayoutStore({ fetch: vi.fn(), save });
    await store.save(custom);
    await store.reset();
    expect(store.layout).toEqual(DEFAULT_LAYOUT);
  });
});
