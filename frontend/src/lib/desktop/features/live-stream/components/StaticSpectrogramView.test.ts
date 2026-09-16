import { cleanup, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderTyped } from '../../../../../test/render-helpers';
import StaticSpectrogramView from './StaticSpectrogramView.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('StaticSpectrogramView', () => {
  it('aborts the active native-rate capture when unmounted', async () => {
    let requestSignal: AbortSignal | undefined;
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      requestSignal = init?.signal instanceof AbortSignal ? init.signal : undefined;
      return new Promise<Response>(() => {});
    });
    vi.stubGlobal('fetch', fetchMock);

    const view = renderTyped(StaticSpectrogramView, {
      props: { sourceId: 'ultrasonic/source', sourceName: 'Ultrasonic source' },
    });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      '/api/v2/streams/spectrogram/ultrasonic%2Fsource?duration=10'
    );
    expect(requestSignal?.aborted).toBe(false);

    view.unmount();

    expect(requestSignal?.aborted).toBe(true);
  });
});
