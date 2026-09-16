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
  it('keeps the rendered image and continues with the next capture', async () => {
    const signals: AbortSignal[] = [];
    let call = 0;
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.signal instanceof AbortSignal) signals.push(init.signal);
      call += 1;
      if (call > 1) return new Promise<Response>(() => {});
      return Promise.resolve({
        ok: true,
        status: 200,
        headers: new Headers({ 'X-Spectrogram-Sample-Rate': '192000' }),
        blob: () => Promise.resolve(new Blob(['png'])),
      } as unknown as Response);
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal(
      'URL',
      Object.assign(URL, { createObjectURL: () => 'blob:x', revokeObjectURL: vi.fn() })
    );

    const view = renderTyped(StaticSpectrogramView, { props: { sourceId: 'src' } });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(view.container.querySelector('img')).not.toBeNull());
    expect(view.container.textContent).toContain('96 kHz');
    expect(signals[1].aborted).toBe(false);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
