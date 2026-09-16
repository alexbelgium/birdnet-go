import { cleanup, fireEvent, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderTyped } from '../../../../../test/render-helpers';
import MiniSpectrogram from './MiniSpectrogram.svelte';

const fetchWithCSRF = vi.fn(() => Promise.resolve({}));

vi.mock('$lib/utils/api', () => ({
  fetchWithCSRF: (...args: unknown[]) => fetchWithCSRF(...(args as [])),
}));

vi.mock('hls.js', () => ({ default: class Hls {} }));

vi.mock('$lib/stores/appState.svelte', () => ({
  appState: { liveSpectrogram: false },
  hasLiveAudioAccess: () => true,
}));

vi.mock('$lib/utils/useSpectrogramAnalyser.svelte', () => ({
  useSpectrogramAnalyser: () => ({
    isActive: false,
    connect: vi.fn(),
    disconnect: vi.fn(),
    setAudioOutput: vi.fn(),
    setGain: vi.fn(),
  }),
}));

// Source discovery: announce one source as soon as the handler is attached.
vi.mock('$lib/utils/ReconnectingEventSource', () => ({
  ReconnectingEventSource: class ReconnectingEventSource {
    private handler: ((event: MessageEvent) => void) | null = null;
    set onmessage(handler: (event: MessageEvent) => void) {
      this.handler = handler;
      queueMicrotask(() =>
        this.handler?.({
          data: JSON.stringify({ type: 'audio-level', levels: { 'mic-1': {} } }),
        } as MessageEvent)
      );
    }
    set onerror(_handler: unknown) {}
    close(): void {
      this.handler = null;
    }
  },
}));

const STATIC_KEY = 'birdnet-spectrogram-static';

describe('MiniSpectrogram static mode', () => {
  let signals: AbortSignal[];
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    signals = [];
    fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.signal instanceof AbortSignal) signals.push(init.signal);
      return new Promise<Response>(() => {});
    });
    vi.stubGlobal('fetch', fetchMock);
    fetchWithCSRF.mockClear();
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    globalThis.localStorage.removeItem(STATIC_KEY);
  });

  it('captures a static spectrogram instead of starting the HLS stream, and aborts on unmount', async () => {
    globalThis.localStorage.setItem(STATIC_KEY, 'true');

    const view = renderTyped(MiniSpectrogram, { props: {} });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/v2/streams/spectrogram/mic-1');
    expect(fetchWithCSRF).not.toHaveBeenCalled();
    expect(signals[0].aborted).toBe(false);

    view.unmount();

    expect(signals[0].aborted).toBe(true);
  });

  it('turning the toggle off aborts the capture without starting the live stream', async () => {
    globalThis.localStorage.setItem(STATIC_KEY, 'true');

    const view = renderTyped(MiniSpectrogram, { props: {} });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());

    await fireEvent.click(view.getByRole('button', { name: 'spectrogram.static.toggle' }));

    expect(signals[0].aborted).toBe(true);
    expect(globalThis.localStorage.getItem(STATIC_KEY)).toBeNull();
    expect(fetchWithCSRF).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
