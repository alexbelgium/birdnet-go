import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/svelte';
import DetectionsList from './DetectionsList.svelte';
import type { DetectionsListData } from '$lib/types/detection.types';

// The "Locked only" toggle only makes sense on the all-dates species view
// (every recording of one species, across all dates) reached from Analytics >
// Species. It must not appear on the hourly/search/date-scoped/default views,
// and it must report its new state to the parent via onLockedFilterChange
// rather than mutating anything itself (the parent owns the URL/fetch).

vi.mock('$lib/stores/navigation.svelte', () => ({
  navigation: {
    currentPath: '/ui/detections',
    navigate: vi.fn(),
    handlePopState: vi.fn(),
  },
}));

function baseData(overrides: Partial<DetectionsListData> = {}): DetectionsListData {
  return {
    notes: [],
    queryType: 'all',
    date: '2024-01-15',
    numResults: 25,
    offset: 0,
    totalResults: 0,
    itemsPerPage: 25,
    currentPage: 1,
    totalPages: 1,
    showingFrom: 0,
    showingTo: 0,
    ...overrides,
  };
}

describe('DetectionsList — locked-only filter', () => {
  afterEach(() => {
    cleanup();
  });

  it('does not render on the default all-detections view', () => {
    const { queryByRole } = render(DetectionsList, {
      props: { data: baseData({ queryType: 'all' }) },
    });

    expect(queryByRole('button', { name: 'Locked only' })).toBeNull();
  });

  it('does not render on a date-scoped species view', () => {
    const { queryByRole } = render(DetectionsList, {
      props: {
        data: baseData({ queryType: 'species', species: 'Turdus merula', date: '2024-01-15' }),
      },
    });

    expect(queryByRole('button', { name: 'Locked only' })).toBeNull();
  });

  it('renders on the all-dates species view and reports the toggled state', async () => {
    const onLockedFilterChange = vi.fn();
    const { getByRole } = render(DetectionsList, {
      props: {
        data: baseData({ queryType: 'species', species: 'Turdus merula', date: '', locked: false }),
        onLockedFilterChange,
      },
    });

    const toggle = getByRole('button', { name: 'Locked only' });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');

    await toggle.click();

    expect(onLockedFilterChange).toHaveBeenCalledExactlyOnceWith(true);
  });

  it('reflects an active locked filter and toggles it back off', async () => {
    const onLockedFilterChange = vi.fn();
    const { getByRole } = render(DetectionsList, {
      props: {
        data: baseData({ queryType: 'species', species: 'Turdus merula', date: '', locked: true }),
        onLockedFilterChange,
      },
    });

    const toggle = getByRole('button', { name: 'Locked only' });
    expect(toggle).toHaveAttribute('aria-pressed', 'true');

    await toggle.click();

    expect(onLockedFilterChange).toHaveBeenCalledExactlyOnceWith(false);
  });
});
