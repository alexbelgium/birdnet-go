import { describe, it, expect, vi, beforeEach } from 'vitest';

const fetchWithCSRF = vi.fn();

vi.mock('./api', () => ({
  fetchWithCSRF: (...args: unknown[]) => fetchWithCSRF(...args),
}));

import { reanalyzeDetection, correctDetectionSpecies } from './reanalyzeDetection';

describe('reanalyzeDetection', () => {
  beforeEach(() => {
    fetchWithCSRF.mockReset();
  });

  it('posts to the detection-scoped endpoint with a 2 minute timeout', async () => {
    fetchWithCSRF.mockResolvedValue({ predictions: [] });

    await reanalyzeDetection(42);

    expect(fetchWithCSRF).toHaveBeenCalledTimes(1);
    const [url, opts] = fetchWithCSRF.mock.calls[0] as [string, Record<string, unknown>];
    expect(url).toBe('/api/v2/detections/42/reanalyze');
    expect(opts.method).toBe('POST');
    // The 30s default aborts under contention with the realtime pipeline; the
    // backend's own decode ceiling is 60s, so anything shorter than that is a
    // client-side abort of work the server is still doing.
    expect(opts.timeout).toBe(120_000);
    expect(JSON.parse(opts.body as string)).toEqual({ modelIds: [] });
  });

  it('forwards an explicit model list', async () => {
    fetchWithCSRF.mockResolvedValue({ predictions: [] });

    await reanalyzeDetection(42, ['Perch_V2']);

    const [, opts] = fetchWithCSRF.mock.calls[0] as [string, Record<string, unknown>];
    expect(JSON.parse(opts.body as string)).toEqual({ modelIds: ['Perch_V2'] });
  });

  it('joins a duplicate request for the same detection to the one in flight', async () => {
    let release!: (v: unknown) => void;
    fetchWithCSRF.mockReturnValue(
      new Promise(resolve => {
        release = resolve;
      })
    );

    const first = reanalyzeDetection(42);
    // A double-click, or a close-and-reopen of the modal, must not spend a second
    // round of inference — and must not lose the answer either: the second caller
    // gets the SAME result, not null.
    const second = reanalyzeDetection(42);
    // A different detection is unrelated and must still go through.
    void reanalyzeDetection(43);
    expect(fetchWithCSRF).toHaveBeenCalledTimes(2);

    release({ predictions: [] });
    await expect(first).resolves.toEqual({ predictions: [] });
    await expect(second).resolves.toEqual({ predictions: [] });

    // Once settled, the same detection is requestable again.
    fetchWithCSRF.mockResolvedValue({ predictions: [] });
    await reanalyzeDetection(42);
    expect(fetchWithCSRF).toHaveBeenCalledTimes(3);
  });

  it('releases the in-flight slot when the request fails', async () => {
    fetchWithCSRF.mockRejectedValueOnce(new Error('boom'));
    await expect(reanalyzeDetection(99)).rejects.toThrow('boom');

    // A failure must not wedge the detection into a permanently "busy" state.
    fetchWithCSRF.mockResolvedValue({ predictions: [] });
    await expect(reanalyzeDetection(99)).resolves.toEqual({ predictions: [] });
  });
});

describe('correctDetectionSpecies', () => {
  beforeEach(() => {
    fetchWithCSRF.mockReset();
  });

  it('posts the correction payload to the correct-species endpoint', async () => {
    fetchWithCSRF.mockResolvedValue({ verified: 'correct' });

    await correctDetectionSpecies(7, {
      scientificName: 'Ficedula hypoleuca',
      modelId: 'BirdNET_V2.4',
      confidence: 0.9,
    });

    const [url, opts] = fetchWithCSRF.mock.calls[0] as [string, Record<string, unknown>];
    expect(url).toBe('/api/v2/detections/7/correct-species');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body as string)).toEqual({
      scientificName: 'Ficedula hypoleuca',
      modelId: 'BirdNET_V2.4',
      confidence: 0.9,
    });
  });

  it('drops a duplicate correction for the same detection while one is in flight', async () => {
    let release!: (v: unknown) => void;
    fetchWithCSRF.mockReturnValue(
      new Promise(resolve => {
        release = resolve;
      })
    );
    const payload = { scientificName: 'Parus major', modelId: 'BirdNET_V2.4', confidence: 0.5 };

    const first = correctDetectionSpecies(7, payload);
    await expect(correctDetectionSpecies(7, payload)).resolves.toBeNull();
    expect(fetchWithCSRF).toHaveBeenCalledTimes(1);

    release({ verified: 'correct' });
    await first;
  });
});
