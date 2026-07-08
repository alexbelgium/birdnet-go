import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import SpeciesHistoryModal from './SpeciesHistoryModal.svelte';

// Exact response shape produced by GET /api/v2/analytics/time/daily
// (GetDailyAnalytics in internal/api/v2/analytics): the series is wrapped in
// a `data` array, which is precisely what the first modal version failed to
// parse. These tests drive the component against that real contract.
function apiResponse(data: { date: string; count: number }[]): Response {
  const body = {
    start_date: '2000-01-01',
    end_date: '2026-07-06',
    species: 'Turdus merula',
    data,
    total: data.reduce((a, b) => a + b.count, 0),
  };
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

const SERIES = [
  { date: '2024-05-10', count: 2 },
  { date: '2026-06-20', count: 3 },
  { date: '2026-07-01', count: 5 },
];

describe('SpeciesHistoryModal', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    fetchMock.mockImplementation(() => Promise.resolve(apiResponse(SERIES)));
    vi.stubGlobal('fetch', fetchMock);
  });

  function renderModal(props?: { scientificName?: string; onClose?: () => void }) {
    return render(SpeciesHistoryModal, {
      props: {
        scientificName: props?.scientificName ?? 'Turdus merula',
        displayName: 'Common Blackbird',
        selectedDate: '2026-07-06',
        onClose: props?.onClose ?? vi.fn(),
      },
    });
  }

  it('parses the wrapped API response and shows totals for the default 30d range', async () => {
    renderModal({ scientificName: 'Parses wrapped' });

    // In-window detections for the 30 days ending 2026-07-06: 3 + 5 = 8.
    await waitFor(() => {
      expect(screen.getByText('8 detections · last 30 days')).toBeInTheDocument();
    });

    // Stats strip: total, peak and avg/day tiles.
    expect(screen.getByText('Total')).toBeInTheDocument();
    expect(screen.getByText('Peak')).toBeInTheDocument();
    expect(screen.getByText('Avg/day')).toBeInTheDocument();
    expect(screen.getByText('0.3')).toBeInTheDocument(); // 8 / 30 days

    // Fast-load strategy: one fetch for the visible 30d range, one for the
    // full history prefetch.
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const urls = fetchMock.mock.calls.map(call => String(call[0]));
    expect(urls.some(u => u.includes('start_date=2000-01-01'))).toBe(true);
    expect(urls.some(u => u.includes('start_date=2026-06-07'))).toBe(true);
  });

  it('serves every range from the prefetched full history without refetching', async () => {
    renderModal({ scientificName: 'Serves cached ranges' });
    await waitFor(() => {
      expect(screen.getByText('8 detections · last 30 days')).toBeInTheDocument();
    });
    fetchMock.mockClear();

    await fireEvent.click(screen.getByRole('button', { name: 'All' }));

    await waitFor(() => {
      expect(screen.getByText('10 detections · all time (since May 10, 2024)')).toBeInTheDocument();
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('reopens instantly from the session cache without any network request', async () => {
    const { unmount } = renderModal({ scientificName: 'Cached species' });
    await waitFor(() => {
      expect(screen.getByText('8 detections · last 30 days')).toBeInTheDocument();
    });
    unmount();
    fetchMock.mockClear();

    renderModal({ scientificName: 'Cached species' });
    await waitFor(() => {
      expect(screen.getByText('8 detections · last 30 days')).toBeInTheDocument();
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('shows the empty state with a longer-range hint when the period has no detections', async () => {
    fetchMock.mockImplementation(() => Promise.resolve(apiResponse([])));
    renderModal({ scientificName: 'Empty species' });

    await waitFor(() => {
      expect(screen.getByText('No detections in this period')).toBeInTheDocument();
    });
    expect(screen.getByText('Try a longer range')).toBeInTheDocument();
  });

  it('shows an error state with a Retry button when every request fails', async () => {
    fetchMock.mockImplementation(() => Promise.resolve(new Response('nope', { status: 500 })));
    renderModal({ scientificName: 'Failing species' });

    await waitFor(() => {
      expect(screen.getByText('Failed to load history')).toBeInTheDocument();
    });

    // Retry re-issues the requests and recovers.
    fetchMock.mockImplementation(() => Promise.resolve(apiResponse(SERIES)));
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.getByText('8 detections · last 30 days')).toBeInTheDocument();
    });
  });

  it('renders the bottom range selector with all six presets', async () => {
    renderModal({ scientificName: 'Selector species' });
    const group = screen.getByRole('group', { name: 'History range' });
    expect(group).toBeInTheDocument();
    for (const label of ['7d', '30d', '90d', '1y', '2y', 'All']) {
      expect(screen.getByRole('button', { name: label })).toBeInTheDocument();
    }
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
  });

  it('closes on Escape', async () => {
    const onClose = vi.fn();
    renderModal({ scientificName: 'Escape species', onClose });
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });
});
