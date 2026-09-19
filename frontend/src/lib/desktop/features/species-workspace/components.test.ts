import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import WorkspaceOverview from './overview/WorkspaceOverview.svelte';
import ColumnEditor from './overview/ColumnEditor.svelte';
import SpeciesDetail from './detail/SpeciesDetail.svelte';
import * as api from './api';
import { DEFAULT_LAYOUT } from './columns';
import { LAYOUT_CACHE_KEY } from './layout.svelte';
import type { WorkspaceLayout } from './types';

vi.mock('./api', () => ({
  fetchSpecies: vi.fn(),
  fetchStats: vi.fn(),
  fetchMemberships: vi.fn(),
  fetchRangeScores: vi.fn(),
  fetchBestRecordings: vi.fn(),
  putMembership: vi.fn(),
  fetchRecordings: vi.fn(),
  fetchLayout: vi.fn(),
  saveLayout: vi.fn(),
  deleteSpeciesChunk: vi.fn(),
  batchReview: vi.fn(),
  batchLock: vi.fn(),
  batchDelete: vi.fn(),
}));
vi.mock('./externalLinks', () => ({
  ebirdSpeciesUrl: () => 'https://ebird.org/species/eurbla/BE-WAL?siteLanguage=en',
  observationsUrl: async () => 'https://observations.be/species/150/',
}));
vi.mock('$lib/utils/reanalyzeDetection', () => ({
  reanalyzeDetection: vi.fn(() => new Promise(() => {})),
  correctDetectionSpecies: vi.fn(),
}));
vi.mock('$lib/stores/excludedSpecies.svelte', () => ({
  hydrateExcludedSpecies: vi.fn(async () => {}),
  isExcluded: () => false,
  setExcluded: vi.fn(),
}));

const SPECIES = [
  {
    scientificName: 'Turdus merula',
    commonName: 'Common Blackbird',
    speciesCode: 'eurbla',
    total: 50,
    locked: 2,
    firstSeen: '2025-01-01T06:00:00Z',
    lastSeen: '2025-05-01T06:00:00Z',
  },
  {
    scientificName: 'Pipistrellus pipistrellus',
    commonName: 'Common Pipistrelle',
    speciesCode: '',
    total: 80,
    locked: 0,
    firstSeen: '2025-01-02T22:00:00Z',
    lastSeen: '2025-05-02T22:00:00Z',
  },
  {
    scientificName: 'Érithacus rubecula',
    commonName: 'European Robin',
    speciesCode: 'eurrob1',
    total: 10,
    locked: 0,
    firstSeen: '2025-01-03T06:00:00Z',
    lastSeen: '2025-05-03T06:00:00Z',
  },
];

function layoutWith(visible: string[], condensed = false): WorkspaceLayout {
  return {
    columns: DEFAULT_LAYOUT.columns.map(c => ({
      ...c,
      visible: visible.includes(c.id) || c.id === 'species' || c.id === 'actions',
    })),
    sort: { column: 'count', direction: 'desc' },
    condensed,
  };
}

const defaultIntersectionObserver = globalThis.IntersectionObserver;

afterEach(() => {
  globalThis.IntersectionObserver = defaultIntersectionObserver;
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  vi.mocked(api.fetchSpecies).mockResolvedValue(SPECIES);
  vi.mocked(api.fetchStats).mockResolvedValue([]);
  vi.mocked(api.fetchMemberships).mockResolvedValue({ confirmed: [], included: [], excluded: [] });
  vi.mocked(api.fetchRangeScores).mockResolvedValue(new Map());
  vi.mocked(api.fetchBestRecordings).mockResolvedValue({});
  vi.mocked(api.saveLayout).mockImplementation(async l => l);
});

describe('WorkspaceOverview', () => {
  function rowNames(): string[] {
    const table = screen.getByRole('table');
    return within(table)
      .getAllByRole('row')
      .slice(1)
      .map(row => row.querySelector('.italic')?.textContent ?? '');
  }

  it('sorts by count descending by default and filters by search', async () => {
    const layout = layoutWith(['count']);
    vi.mocked(api.fetchLayout).mockResolvedValue(layout);
    localStorage.setItem(LAYOUT_CACHE_KEY, JSON.stringify(layout));
    render(WorkspaceOverview, { props: { onOpenSpecies: vi.fn() } });

    await waitFor(() => expect(rowNames()).toHaveLength(3));
    expect(rowNames()).toEqual([
      'Pipistrellus pipistrellus',
      'Turdus merula',
      'Érithacus rubecula',
    ]);

    await fireEvent.input(screen.getByPlaceholderText('speciesWorkspace.search.placeholder'), {
      target: { value: 'erithacus' },
    });
    await waitFor(() => expect(rowNames()).toEqual(['Érithacus rubecula']));
  });

  it('never requests data for hidden columns', async () => {
    const layout = layoutWith(['count', 'lastSeen']);
    vi.mocked(api.fetchLayout).mockResolvedValue(layout);
    localStorage.setItem(LAYOUT_CACHE_KEY, JSON.stringify(layout));
    render(WorkspaceOverview, { props: { onOpenSpecies: vi.fn() } });

    await waitFor(() => expect(api.fetchSpecies).toHaveBeenCalled());
    await waitFor(() => expect(api.fetchMemberships).toHaveBeenCalled());
    expect(api.fetchStats).not.toHaveBeenCalled();
    expect(api.fetchRangeScores).not.toHaveBeenCalled();
    expect(api.fetchBestRecordings).not.toHaveBeenCalled();
  });

  it('renders condensed mobile rows with ordered status icons and no hidden range fetch', async () => {
    class ImmediateIntersectionObserver {
      readonly root: Element | null = null;
      readonly rootMargin = '';
      readonly thresholds: ReadonlyArray<number> = [];

      constructor(private readonly callback: IntersectionObserverCallback) {}

      observe = (target: Element) => {
        this.callback(
          [{ isIntersecting: true, target } as IntersectionObserverEntry],
          this as unknown as IntersectionObserver
        );
      };
      unobserve = vi.fn();
      disconnect = vi.fn();
      takeRecords = vi.fn().mockReturnValue([]);
    }
    globalThis.IntersectionObserver =
      ImmediateIntersectionObserver as unknown as typeof IntersectionObserver;

    const layout = layoutWith(
      [
        'count',
        'maxConfidence',
        'lastSeen',
        'verification',
        'excluded',
        'included',
        'confirmed',
        'bestRecording',
      ],
      true
    );
    vi.mocked(api.fetchLayout).mockResolvedValue(layout);
    localStorage.setItem(LAYOUT_CACHE_KEY, JSON.stringify(layout));
    vi.mocked(api.fetchSpecies).mockResolvedValue(
      SPECIES.map(species =>
        species.scientificName === 'Pipistrellus pipistrellus'
          ? { ...species, total: 1_200_000 }
          : species
      )
    );
    vi.mocked(api.fetchStats).mockResolvedValue([
      {
        scientificName: 'Pipistrellus pipistrellus',
        correct: 3,
        falsePositive: 1,
        maxConfidence: 0.93,
      },
    ]);
    vi.mocked(api.fetchMemberships).mockResolvedValue({
      confirmed: ['Pipistrellus pipistrellus'],
      included: [],
      excluded: ['Turdus merula'],
    });
    vi.mocked(api.fetchBestRecordings).mockImplementation(async names =>
      Object.fromEntries(names.map(name => [name, { id: 1, confidence: 0.93, locked: false }]))
    );

    render(WorkspaceOverview, { props: { onOpenSpecies: vi.fn() } });

    const condensedRows = await screen.findAllByTestId('condensed-species-row');
    expect(condensedRows).toHaveLength(3);
    const row = condensedRows.find(item => within(item).queryByText('Common Pipistrelle'));
    expect(row).toBeDefined();
    if (!row) return;

    expect(within(row).getByTestId('condensed-primary')).toHaveTextContent('1.2M');
    expect(within(row).getByTestId('condensed-secondary')).toBeInTheDocument();
    await waitFor(() =>
      expect(
        within(row).getByRole('button', { name: 'speciesWorkspace.best.play' })
      ).toBeInTheDocument()
    );
    expect(
      within(row).getByRole('button', { name: 'speciesWorkspace.actions.menu' })
    ).toBeInTheDocument();
    expect(row.querySelector('[data-column="confirmed"]')).toHaveAttribute(
      'data-membership-state',
      'active'
    );
    expect(row.querySelector('[data-column="included"]')).toHaveAttribute(
      'data-membership-state',
      'inactive'
    );
    expect(row.querySelector('[data-column="range"]')).not.toBeInTheDocument();
    expect(api.fetchRangeScores).not.toHaveBeenCalled();
  });

  it('shows a load error with a working retry', async () => {
    const layout = layoutWith(['count']);
    vi.mocked(api.fetchLayout).mockResolvedValue(layout);
    vi.mocked(api.fetchSpecies).mockRejectedValueOnce(new Error('offline'));
    render(WorkspaceOverview, { props: { onOpenSpecies: vi.fn() } });

    const retry = await screen.findByRole('button', { name: 'speciesWorkspace.states.retry' });
    await fireEvent.click(retry);
    await waitFor(() => expect(rowNames()).toHaveLength(3));
  });

  it('keeps membership toggles disabled until the lists load', async () => {
    const layout = layoutWith(['count']);
    vi.mocked(api.fetchLayout).mockResolvedValue(layout);
    let resolveLists: (_v: {
      confirmed: string[];
      included: string[];
      excluded: string[];
    }) => void = () => {};
    vi.mocked(api.fetchMemberships).mockReturnValue(new Promise(r => (resolveLists = r)));
    render(WorkspaceOverview, { props: { onOpenSpecies: vi.fn() } });

    await waitFor(() => expect(rowNames()).toHaveLength(3));
    const menus = screen.getAllByRole('button', { name: 'speciesWorkspace.actions.menu' });
    await fireEvent.click(menus[0] as HTMLElement);
    const toggle = screen.getAllByRole('menuitemcheckbox')[0] as HTMLElement;
    expect(toggle).toHaveAttribute('aria-disabled', 'true');
    await fireEvent.click(toggle);
    expect(api.putMembership).not.toHaveBeenCalled();

    resolveLists({ confirmed: [], included: [], excluded: [] });
    await waitFor(() =>
      expect(screen.getAllByRole('menuitemcheckbox')[0]).toHaveAttribute('aria-disabled', 'false')
    );
  });

  it('opens a species page from the name link', async () => {
    const layout = layoutWith(['count']);
    vi.mocked(api.fetchLayout).mockResolvedValue(layout);
    const onOpenSpecies = vi.fn();
    render(WorkspaceOverview, { props: { onOpenSpecies } });
    await waitFor(() => expect(rowNames()).toHaveLength(3));
    const table = screen.getByRole('table');
    await fireEvent.click(within(table).getAllByRole('link')[0] as HTMLElement);
    expect(onOpenSpecies).toHaveBeenCalledWith('Pipistrellus pipistrellus');
  });
});

describe('ColumnEditor', () => {
  it('enables condensed phone rows through the layout callback', async () => {
    const onChange = vi.fn();
    const layout = layoutWith(['count']);
    render(ColumnEditor, { props: { layout, onChange } });

    const editor = screen.getByRole('region', { name: 'speciesWorkspace.edit.panelTitle' });
    await fireEvent.click(within(editor).getAllByRole('checkbox')[0] as HTMLElement);

    expect(onChange).toHaveBeenCalledWith({ ...layout, condensed: true });
  });
});

describe('SpeciesDetail', () => {
  const recording = (id: number) => ({
    id,
    date: '2025-05-01',
    time: '06:00:00',
    timestamp: '2025-05-01T06:00:00Z',
    beginTime: '',
    endTime: '',
    speciesCode: 'eurbla',
    scientificName: 'Turdus merula',
    commonName: 'Common Blackbird',
    confidence: 0.9,
    verified: 'unverified' as const,
    locked: false,
    clipName: '',
    modelName: id === 1 ? 'BirdNET' : null,
  });

  beforeEach(() => {
    vi.mocked(api.fetchSpecies).mockResolvedValue([SPECIES[0] as (typeof SPECIES)[number]]);
    vi.mocked(api.fetchRecordings).mockResolvedValue({
      data: [recording(1), recording(2)],
      total: 2,
      page: 1,
      perPage: 25,
      totalPages: 1,
    });
  });

  it('requests confidence-descending by default and keeps an explicit sort', async () => {
    const onQueryChange = vi.fn();
    render(SpeciesDetail, {
      props: {
        scientificName: 'Turdus merula',
        query: '?species=Turdus+merula',
        onBack: vi.fn(),
        onQueryChange,
      },
    });
    await waitFor(() => expect(api.fetchRecordings).toHaveBeenCalled());
    expect(vi.mocked(api.fetchRecordings).mock.calls[0]?.[0]).toMatchObject({
      sort: 'confidence_desc',
      page: 1,
      locked: false,
    });

    const table = await screen.findByRole('table');
    await fireEvent.click(
      within(table).getByRole('button', { name: /speciesWorkspace.table.dateTime/ })
    );
    expect(onQueryChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: 'date_desc' }),
      false
    );

    render(SpeciesDetail, {
      props: {
        scientificName: 'Turdus merula',
        query: '?species=Turdus+merula&sort=date_asc',
        onBack: vi.fn(),
        onQueryChange,
      },
    });
    await waitFor(() =>
      expect(vi.mocked(api.fetchRecordings).mock.calls.some(c => c[0].sort === 'date_asc')).toBe(
        true
      )
    );
  });

  it('shows the model name or a localized unknown', async () => {
    render(SpeciesDetail, {
      props: {
        scientificName: 'Turdus merula',
        query: '',
        onBack: vi.fn(),
        onQueryChange: vi.fn(),
      },
    });
    const table = await screen.findByRole('table');
    await waitFor(() => expect(within(table).getByText('BirdNET')).toBeInTheDocument());
    expect(within(table).getByText('speciesWorkspace.states.unknownModel')).toBeInTheDocument();
  });

  it('opens the reanalysis dialog in place from a recording row action', async () => {
    const { reanalyzeDetection } = await import('$lib/utils/reanalyzeDetection');
    render(SpeciesDetail, {
      props: {
        scientificName: 'Turdus merula',
        query: '',
        onBack: vi.fn(),
        onQueryChange: vi.fn(),
      },
    });
    const table = await screen.findByRole('table');
    await fireEvent.click(
      within(table).getAllByRole('button', { name: /Actions/i })[0] as HTMLElement
    );
    await fireEvent.click(
      await screen.findByRole('menuitem', { name: 'dashboard.recentDetections.actions.reanalyze' })
    );
    expect(await screen.findByRole('dialog', { name: 'Reanalyze this clip' })).toBeInTheDocument();
    await waitFor(() => expect(reanalyzeDetection).toHaveBeenCalled());
    expect(vi.mocked(reanalyzeDetection).mock.calls[0]?.[0]).toBe(1);
  });

  it('clears the selection when Locked only is toggled', async () => {
    const onQueryChange = vi.fn();
    const { rerender } = render(SpeciesDetail, {
      props: { scientificName: 'Turdus merula', query: '', onBack: vi.fn(), onQueryChange },
    });
    const table = await screen.findByRole('table');
    await fireEvent.click(screen.getByRole('button', { name: 'speciesWorkspace.detail.select' }));
    const boxes = within(table).getAllByRole('checkbox');
    await fireEvent.click(boxes[1] as HTMLElement);
    await waitFor(() =>
      expect(screen.getByText('speciesWorkspace.table.selected')).toBeInTheDocument()
    );
    expect(screen.getByRole('button', { name: 'speciesWorkspace.table.lock' })).toBeEnabled();

    await fireEvent.click(
      screen.getByRole('button', { name: 'speciesWorkspace.detail.lockedOnly' })
    );
    expect(onQueryChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ locked: 'true' }),
      false
    );
    await rerender({
      scientificName: 'Turdus merula',
      query: '?locked=true',
      onBack: vi.fn(),
      onQueryChange,
    });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'speciesWorkspace.table.lock' })).toBeDisabled()
    );
  });
});
