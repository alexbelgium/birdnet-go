import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createWorkspace, type Workspace } from './workspace.svelte';
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock('$lib/utils/api', () => ({ fetchWithCSRF: mocks.fetch }));
const rows = [
  { scientific_name: 'Turdus merula', common_name: 'Merle noir', count: 12 },
  { scientific_name: 'Corvus corax', common_name: 'Corbeau', count: 2 },
];
let workspace: Workspace;
beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
});
afterEach(() => workspace.dispose());
function preferences(columns: string[], sort = 'name') {
  localStorage.setItem(
    'species-tools-layout-v1',
    JSON.stringify({ columns, sort, ascending: true })
  );
}
describe('Species workspace loading', () => {
  it('preserves row order on metric refresh', async () => {
    preferences(['count'], 'count');
    mocks.fetch.mockResolvedValue(rows);
    workspace = createWorkspace();
    await workspace.load();
    expect(workspace.orderedRows[0].scientific_name).toBe('Corvus corax');
    mocks.fetch.mockResolvedValue([
      { ...rows[0], count: 1 },
      { ...rows[1], count: 20 },
    ]);
    await workspace.refresh();
    expect(workspace.orderedRows[0].scientific_name).toBe('Corvus corax');
    expect(workspace.orderedRows[0].count).toBe(20);
  });
  it('loads truthful deletion counts with count hidden', async () => {
    preferences([]);
    mocks.fetch
      .mockResolvedValueOnce(
        rows.map(row => ({ scientific_name: row.scientific_name, common_name: row.common_name }))
      )
      .mockResolvedValueOnce(rows);
    workspace = createWorkspace();
    await workspace.load();
    expect((await workspace.deletionTarget(workspace.rows[0])).count).toBe(12);
    expect(mocks.fetch).toHaveBeenLastCalledWith('/api/v2/analytics/species/tools?fields=count');
  });
  it('aborts hidden enrichment and rejects stale results', async () => {
    preferences(['included']);
    let resolve: (value: { species: string[] }) => void = () => {};
    let signal: AbortSignal | undefined;
    mocks.fetch.mockImplementation((url: string, options?: RequestInit) => {
      if (url.includes('/included')) {
        signal = options?.signal ?? undefined;
        return new Promise(done => {
          resolve = done;
        });
      }
      return Promise.resolve(rows);
    });
    workspace = createWorkspace();
    const loading = workspace.load();
    await vi.waitFor(() => expect(signal).toBeDefined());
    workspace.saveColumns([]);
    expect(signal?.aborted).toBe(true);
    resolve({ species: ['Turdus merula'] });
    await loading;
    expect(workspace.member('included', rows[0])).toBeNull();
  });
  it('defers saved enrichment sorting until interaction ends', async () => {
    preferences(['included'], 'included');
    let resolve: (value: { species: string[] }) => void = () => {};
    mocks.fetch.mockImplementation((url: string) =>
      url.includes('/included')
        ? new Promise(done => {
            resolve = done;
          })
        : Promise.resolve(rows)
    );
    workspace = createWorkspace();
    const loading = workspace.load();
    await vi.waitFor(() => expect(workspace.rows).toHaveLength(2));
    workspace.setInteracting(true);
    resolve({ species: ['Turdus merula'] });
    await loading;
    expect(workspace.orderedRows[0].scientific_name).toBe('Turdus merula');
    workspace.setInteracting(false);
    expect(workspace.orderedRows[0].scientific_name).toBe('Corvus corax');
  });
});
