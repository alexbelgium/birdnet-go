import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { cleanup, fireEvent, waitFor } from '@testing-library/svelte';
import { createComponentTestFactory } from '../../../../../test/render-helpers';
import { resetBasePath } from '$lib/utils/urlHelpers';
import { settingsActions } from '$lib/stores/settings';
import { navigation } from '$lib/stores/navigation.svelte';
import { api } from '$lib/utils/api';
import SpeciesList from './SpeciesList.svelte';

/* eslint-disable security/detect-object-injection -- bracket access in this file is constant numeric indexing into NodeLists in assertions. */

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

describe('SpeciesList (analytics page)', () => {
  const originalFetch = globalThis.fetch;
  const SPECIES_SORT_STORAGE_KEY = 'analytics.species.sortOrder';

  // Table body cell order: name(0), count(1), maxConf(2), lastSeen(3),
  // excluded(4), included(5), correct(6), range(7), confirmed(8), delete(9).
  const CORRECT_CELL_INDEX = 6;

  const summary = [
    {
      common_name: 'American Robin',
      scientific_name: 'Turdus migratorius',
      count: 5,
      avg_confidence: 0.8,
      max_confidence: 0.9,
      first_heard: '2026-04-01',
      last_heard: '2026-04-20',
    },
  ];

  function mockManageFetch(reviewStats: unknown) {
    globalThis.fetch = mockFetchSequence({
      '/api/v2/analytics/species/review-stats': () => reviewStats,
      '/api/v2/analytics/species/summary': () => summary,
      '/api/v2/detections/included': () => ({ species: [] }),
      '/api/v2/detections/confirmed': () => ({ species: [] }),
      '/api/v2/detections/ignored': () => ({ species: [] }),
    });
  }

  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(settingsActions, 'loadRangeFilterSpecies').mockResolvedValue({
      count: 0,
      species: [],
    });
    mockManageFetch([
      { scientificName: 'Turdus migratorius', total: 10, verified: 7, rejected: 3 },
    ]);
    window.localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    resetBasePath();
    globalThis.fetch = originalFetch;
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  const speciesListTest = createComponentTestFactory(SpeciesList);

  async function renderSpeciesList() {
    const { container } = speciesListTest.render({});
    await waitFor(
      () => {
        if (!container.querySelector('table tbody tr')) throw new Error('table not yet rendered');
      },
      { timeout: 2000 }
    );
    return { container };
  }

  it('always renders the management table without a view-mode toggle', async () => {
    const { container } = await renderSpeciesList();

    expect(container.querySelector('.join')).toBeNull();
    expect(container.querySelector('table')).not.toBeNull();
  });

  it('hides average confidence and first detected columns', async () => {
    const { container } = await renderSpeciesList();
    const headText = container.querySelector('table thead')?.textContent ?? '';
    expect(headText).not.toContain('avgConfidence');
    expect(headText).not.toContain('firstDetected');
    expect(headText).toContain('maxConfidence');
    expect(headText).toContain('lastDetected');
  });

  it('shows the correct rate as an integer percentage', async () => {
    const { container } = await renderSpeciesList();
    await waitFor(() => {
      const cell = container.querySelectorAll('table tbody tr td')[CORRECT_CELL_INDEX];
      expect(cell.textContent.trim()).toBe('70%');
    });
  });

  it('shows — in the correct column when a species has no reviews', async () => {
    mockManageFetch([{ scientificName: 'Turdus migratorius', total: 5, verified: 0, rejected: 0 }]);
    const { container } = await renderSpeciesList();
    await waitFor(() => {
      const cell = container.querySelectorAll('table tbody tr td')[CORRECT_CELL_INDEX];
      expect(cell.textContent.trim()).toBe('—');
    });
  });

  it('makes all membership columns sortable without changing the Species page sort', async () => {
    window.localStorage.setItem(SPECIES_SORT_STORAGE_KEY, JSON.stringify('count_asc'));
    const { container } = await renderSpeciesList();
    const sortButtons = container.querySelectorAll('table thead th button');
    expect(sortButtons).toHaveLength(9);

    await fireEvent.click(sortButtons[4]);
    expect(window.localStorage.getItem(SPECIES_SORT_STORAGE_KEY)).toBe(JSON.stringify('count_asc'));
  });

  it('sorts by the Included column, grouping included species first', async () => {
    globalThis.fetch = mockFetchSequence({
      '/api/v2/analytics/species/review-stats': () => [
        { scientificName: 'Turdus migratorius', total: 10, verified: 7, rejected: 3 },
        {
          scientificName: 'Corvus corax',
          commonName: 'Common Raven',
          total: 4,
          verified: 0,
          rejected: 4,
        },
      ],
      '/api/v2/analytics/species/summary': () => summary,
      '/api/v2/detections/included': () => ({ species: ['Corvus corax'] }),
      '/api/v2/detections/confirmed': () => ({ species: [] }),
      '/api/v2/detections/ignored': () => ({ species: [] }),
    });
    const { container } = await renderSpeciesList();
    await waitFor(() => {
      if (container.querySelectorAll('table tbody tr').length < 2)
        throw new Error('both rows not yet rendered');
    });

    const sortButtons = container.querySelectorAll('table thead th button');
    await fireEvent.click(sortButtons[5]);
    await waitFor(() => {
      const rows = container.querySelectorAll('table tbody tr');
      expect(rows[0].textContent).toContain('Common Raven');
    });
  });

  it('surfaces a fully-rejected species that is absent from the summary', async () => {
    mockManageFetch([
      { scientificName: 'Turdus migratorius', total: 10, verified: 7, rejected: 3 },
      {
        scientificName: 'Corvus corax',
        commonName: 'Common Raven',
        total: 4,
        verified: 0,
        rejected: 4,
      },
    ]);
    const { container } = await renderSpeciesList();
    await waitFor(() => {
      if (container.querySelectorAll('table tbody tr').length < 2)
        throw new Error('synthesized row not yet rendered');
    });

    const rows = Array.from(container.querySelectorAll('table tbody tr'));
    const ravenRow = rows.find(row => row.textContent.includes('Common Raven'));
    if (!ravenRow) throw new Error('Common Raven row not rendered');
    const cells = ravenRow.querySelectorAll('td');
    expect(cells[1].textContent.trim()).toBe('4');
    expect(cells[2].textContent.trim()).toBe('—');
    expect(cells[3].textContent.trim()).toBe('—');
    expect(cells[CORRECT_CELL_INDEX].textContent.trim()).toBe('0%');
  });

  it('checks the included toggle when the list stores the scientific-name alias', async () => {
    globalThis.fetch = mockFetchSequence({
      '/api/v2/analytics/species/review-stats': () => [
        { scientificName: 'Turdus migratorius', total: 10, verified: 7, rejected: 3 },
      ],
      '/api/v2/analytics/species/summary': () => summary,
      '/api/v2/detections/included': () => ({ species: ['Turdus migratorius'] }),
      '/api/v2/detections/confirmed': () => ({ species: [] }),
      '/api/v2/detections/ignored': () => ({ species: [] }),
    });
    const { container } = await renderSpeciesList();
    await waitFor(() => {
      const cell = container.querySelectorAll('table tbody tr td')[5];
      const checkbox = cell.querySelector<HTMLInputElement>('input[type="checkbox"]');
      expect(checkbox?.checked).toBe(true);
    });
  });

  it('navigates to all recordings from the species name while controls remain independent', async () => {
    const navigateSpy = vi.spyOn(navigation, 'navigate').mockImplementation(() => undefined);
    vi.spyOn(api, 'post').mockResolvedValue({ action: 'added' });
    const { container } = await renderSpeciesList();

    const cells = container.querySelectorAll('table tbody tr td');
    const excludedCheckbox = cells[4].querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (!excludedCheckbox) throw new Error('excluded checkbox not rendered');
    await fireEvent.click(excludedCheckbox);
    expect(navigateSpy).not.toHaveBeenCalled();

    const deleteButton = cells[9].querySelector('button');
    if (!deleteButton) throw new Error('delete button not rendered');
    await fireEvent.click(deleteButton);
    expect(navigateSpy).not.toHaveBeenCalled();

    const nameButton = cells[0].querySelector('button');
    if (!nameButton) throw new Error('species name button not rendered');
    await fireEvent.click(nameButton);
    expect(navigateSpy).toHaveBeenCalledExactlyOnceWith(
      '/ui/detections?queryType=species&species=Turdus%20migratorius&sortBy=confidence_desc'
    );
  });
});
