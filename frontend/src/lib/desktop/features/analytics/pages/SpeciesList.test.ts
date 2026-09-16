import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { cleanup, fireEvent, waitFor } from '@testing-library/svelte';
import { createComponentTestFactory } from '../../../../../test/render-helpers';
import { navigation } from '$lib/stores/navigation.svelte';
import SpeciesList from './SpeciesList.svelte';
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock('$lib/utils/api', async importOriginal => ({
  ...(await importOriginal<typeof import('$lib/utils/api')>()),
  fetchWithCSRF: mocks.fetch,
}));
const inventory = [
  { scientific_name: 'Turdus merula', common_name: 'Merle noir', count: 12 },
  { scientific_name: 'Pipistrellus pipistrellus', common_name: 'Pipistrelle', count: 4 },
  { scientific_name: 'Vulpes vulpes', common_name: 'Renard', count: 2 },
];
const factory = createComponentTestFactory(SpeciesList);
beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  navigation.navigate('/ui/analytics/species-list');
  mocks.fetch.mockImplementation(async (url: string) => {
    if (url.includes('/recordings?'))
      return Object.fromEntries(inventory.map(row => [row.scientific_name, null]));
    if (url.includes('/tools?')) return inventory;
    if (url.includes('/review-stats')) return [];
    return { species: [] };
  });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.clear();
  navigation.navigate('/ui/analytics/species-list');
});
async function renderPage() {
  const result = factory.render({});
  await waitFor(() => expect(result.container.querySelectorAll('table tbody tr')).toHaveLength(3));
  return result;
}
describe('Species workspace overview', () => {
  it('filters by common name and taxon without network requests', async () => {
    const { container } = await renderPage();
    const calls = mocks.fetch.mock.calls.length;
    const search = container.querySelector('input[type=search]');
    const taxon = container.querySelector('.sw-filters select');
    if (!search || !taxon) throw new Error('Missing search controls');
    await fireEvent.input(search, { target: { value: 'MERLE' } });
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1);
    await fireEvent.input(search, { target: { value: '' } });
    await fireEvent.change(taxon, { target: { value: 'bat' } });
    expect(container.querySelector('tbody')?.textContent).toContain('Pipistrellus');
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1);
    await fireEvent.change(taxon, { target: { value: 'other' } });
    expect(container.querySelector('tbody')?.textContent).toContain('Vulpes');
    expect(mocks.fetch).toHaveBeenCalledTimes(calls);
  });
  it('restores columns and does not request hidden data', async () => {
    localStorage.setItem(
      'species-tools-layout-v1',
      JSON.stringify({ columns: ['included', 'count'], sort: 'count', ascending: false })
    );
    const { container } = await renderPage();
    const urls = mocks.fetch.mock.calls.map(call => String(call[0]));
    expect(urls).toContain('/api/v2/analytics/species/tools?fields=count');
    expect(urls).toContain('/api/v2/detections/included');
    expect(urls.some(url => /recordings|review-stats|range|ignored|confirmed/.test(url))).toBe(
      false
    );
    expect(container.querySelectorAll('thead th')).toHaveLength(4);
  });
  it('shows unavailable recording instead of an inactive player', async () => {
    const { container } = await renderPage();
    await waitFor(() =>
      expect(container.querySelector('tbody')?.textContent).toContain(
        'analytics.speciesTools.audioUnavailable'
      )
    );
  });
  it('navigates from the species name to the query-based page', async () => {
    const navigate = vi.spyOn(navigation, 'navigate').mockImplementation(() => undefined);
    const { container } = await renderPage();
    const link = container.querySelector('tbody a');
    if (!link) throw new Error('Missing species link');
    await fireEvent.click(link);
    expect(navigate).toHaveBeenCalledWith(
      expect.stringContaining('/ui/analytics/species-list?species=Turdus+merula')
    );
  });
});
