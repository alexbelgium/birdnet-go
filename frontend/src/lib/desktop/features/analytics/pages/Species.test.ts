import { describe, it, expect, afterEach, vi } from 'vitest';
import { cleanup, waitFor, fireEvent } from '@testing-library/svelte';
import { createComponentTestFactory } from '../../../../../test/render-helpers';
import { setBasePath, resetBasePath } from '$lib/utils/urlHelpers';
import Species from './Species.svelte';

interface SpeciesSummary {
  common_name: string;
  scientific_name: string;
  count: number;
  avg_confidence: number;
  max_confidence: number;
  first_heard: string;
  last_heard: string;
  thumbnail_url?: string;
}

function mockFetchSequence(handlers: Record<string, () => unknown>) {
  return vi.fn().mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    for (const [pattern, body] of Object.entries(handlers)) {
      if (url.includes(pattern)) {
        return Promise.resolve({
          ok: true,
          status: 200,
          statusText: 'OK',
          headers: new Headers({ 'content-type': 'application/json' }),
          json: () => Promise.resolve(body()),
          text: () => Promise.resolve(JSON.stringify(body())),
        });
      }
    }
    return Promise.reject(new Error(`Unexpected fetch in test: ${url}`));
  });
}

const speciesTest = createComponentTestFactory(Species);

describe('Species (analytics page)', () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    cleanup();
    resetBasePath();
    globalThis.fetch = originalFetch;
  });

  it('prefixes thumbnail URLs with the configured base path (regression)', async () => {
    // Reproduces the bug reported on /ui/analytics/species: the backend
    // returns a relative thumbnail_url like /api/v2/media/image/<name>, and
    // when configured behind a reverse proxy (e.g. /birdnet), the frontend
    // must prepend the base path before using it as <img src>.
    setBasePath('/birdnet');

    const summary: SpeciesSummary[] = [
      {
        common_name: "Wilson's Warbler",
        scientific_name: 'Cardellina pusilla',
        count: 42,
        avg_confidence: 0.85,
        max_confidence: 0.95,
        first_heard: '2026-04-01',
        last_heard: '2026-04-27',
        thumbnail_url: '/api/v2/media/image/Cardellina%20pusilla',
      },
    ];

    globalThis.fetch = mockFetchSequence({
      '/api/v2/analytics/species/summary': () => summary,
      '/api/v2/analytics/species/thumbnails': () => ({}),
    });

    const { container } = speciesTest.render({});

    // Wait for the async fetch + render to complete and an <img> to appear
    // in either the grid card or the table list view.
    const img = await waitFor(
      () => {
        const found = container.querySelector('img');
        if (!found) throw new Error('image not yet rendered');
        return found;
      },
      { timeout: 2000 }
    );

    expect(img.getAttribute('src')).toBe('/birdnet/api/v2/media/image/Cardellina%20pusilla');
  });

  it('also prefixes URLs returned by the batched thumbnails endpoint', async () => {
    setBasePath('/birdnet');

    const summary: SpeciesSummary[] = [
      {
        common_name: 'Northern Cardinal',
        scientific_name: 'Cardinalis cardinalis',
        count: 7,
        avg_confidence: 0.91,
        max_confidence: 0.99,
        first_heard: '2026-04-10',
        last_heard: '2026-04-26',
        // No thumbnail_url here — the page's loadThumbnailsAsync() should
        // populate it from the batch endpoint.
      },
    ];

    globalThis.fetch = mockFetchSequence({
      '/api/v2/analytics/species/summary': () => summary,
      '/api/v2/analytics/species/thumbnails': () => ({
        'Cardinalis cardinalis': '/api/v2/media/image/Cardinalis%20cardinalis',
      }),
    });

    const { container } = speciesTest.render({});

    const img = await waitFor(
      () => {
        const found = container.querySelector('img');
        if (!found?.getAttribute('src')?.includes('Cardinalis')) {
          throw new Error('thumbnail not yet rendered');
        }
        return found;
      },
      { timeout: 2000 }
    );

    expect(img.getAttribute('src')).toBe('/birdnet/api/v2/media/image/Cardinalis%20cardinalis');
  });
});

const SORT_STORAGE_KEY = 'birdnet-species-sort';

// Two species whose alphabetical order is the inverse of their detection count,
// so each sort field/direction produces a distinguishable row order.
const sortFixture: SpeciesSummary[] = [
  {
    common_name: 'Zebra Finch',
    scientific_name: 'Taeniopygia guttata',
    count: 50,
    avg_confidence: 0.6,
    max_confidence: 0.7,
    first_heard: '2026-01-01',
    last_heard: '2026-01-10',
  },
  {
    common_name: 'American Robin',
    scientific_name: 'Turdus migratorius',
    count: 5,
    avg_confidence: 0.9,
    max_confidence: 0.95,
    first_heard: '2026-03-01',
    last_heard: '2026-05-20',
  },
];

/** Read the common-name column from the rendered list-view table, in row order. */
function rowNames(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('tbody tr')).map(tr => {
    const cell = tr.querySelector('.font-bold');
    if (!cell) return '';
    return cell.textContent.trim();
  });
}

/** Locate a sortable header button by field, failing loudly if it is missing. */
function sortHeader(container: HTMLElement, field: string): HTMLElement {
  const el = container.querySelector<HTMLElement>(`[data-testid="sort-${field}"]`);
  if (!el) throw new Error(`sort header for "${field}" not found`);
  return el;
}

/** Render, switch to list view, and wait until the table has rendered both rows. */
async function renderSortableList() {
  globalThis.fetch = mockFetchSequence({
    '/api/v2/analytics/species/summary': () => sortFixture,
    '/api/v2/analytics/species/thumbnails': () => ({}),
  });

  const result = speciesTest.render({});
  const { container } = result;

  // The sortable table only renders in list view; switch away from the grid.
  // The test i18n stub returns translation keys verbatim, so match on the key.
  const toListButton = await waitFor(() => {
    const btn = container.querySelector<HTMLButtonElement>(
      'button[aria-label="analytics.species.switchToList"]'
    );
    if (!btn) throw new Error('list-view toggle not yet rendered');
    return btn;
  });
  await fireEvent.click(toListButton);

  await waitFor(() => {
    if (rowNames(container).length < 2) throw new Error('rows not yet rendered');
  });

  return result;
}

describe('Species (analytics page) — sortable headers', () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    cleanup();
    resetBasePath();
    globalThis.fetch = originalFetch;
    localStorage.clear();
  });

  it('defaults to detection count descending', async () => {
    const { container } = await renderSortableList();
    // Zebra Finch (50) before American Robin (5).
    expect(rowNames(container)).toEqual(['Zebra Finch', 'American Robin']);
  });

  it('sorts ascending A→Z on first click of the species header', async () => {
    const { container } = await renderSortableList();

    await fireEvent.click(sortHeader(container, 'species'));
    await waitFor(() => {
      expect(rowNames(container)).toEqual(['American Robin', 'Zebra Finch']);
    });
  });

  it('toggles direction when the active header is clicked again', async () => {
    const { container } = await renderSortableList();
    const speciesHeader = sortHeader(container, 'species');

    await fireEvent.click(speciesHeader); // asc
    await waitFor(() => expect(rowNames(container)[0]).toBe('American Robin'));

    await fireEvent.click(speciesHeader); // desc
    await waitFor(() => expect(rowNames(container)[0]).toBe('Zebra Finch'));
  });

  it('defaults other columns to descending on first click', async () => {
    const { container } = await renderSortableList();

    // Avg confidence descending → American Robin (0.9) first.
    await fireEvent.click(sortHeader(container, 'avg_confidence'));
    await waitFor(() => {
      expect(rowNames(container)).toEqual(['American Robin', 'Zebra Finch']);
    });
  });

  it('persists the selected sort to localStorage', async () => {
    const { container } = await renderSortableList();

    await fireEvent.click(sortHeader(container, 'max_confidence'));
    await waitFor(() => {
      const stored = localStorage.getItem(SORT_STORAGE_KEY);
      expect(stored).not.toBeNull();
      expect(JSON.parse(stored as string)).toEqual({
        field: 'max_confidence',
        direction: 'desc',
      });
    });
  });

  it('restores the persisted sort on load', async () => {
    localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify({ field: 'species', direction: 'asc' }));

    const { container } = await renderSortableList();
    // Persisted species-ascending should win over the count-desc default.
    expect(rowNames(container)).toEqual(['American Robin', 'Zebra Finch']);
  });
});
