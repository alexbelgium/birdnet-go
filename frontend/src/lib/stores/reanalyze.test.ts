import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { Detection } from '$lib/types/detection.types';
import { openReanalyze, closeReanalyze, reanalyzeState } from './reanalyze.svelte';

function mkDetection(id: number): Detection {
  return {
    id,
    date: '2026-09-08',
    time: '07:14:00',
    beginTime: '07:14:00',
    endTime: '07:14:03',
    speciesCode: 'eupfly1',
    scientificName: 'Ficedula hypoleuca',
    commonName: 'Pied Flycatcher',
    confidence: 0.87,
    verified: 'unverified',
    locked: false,
  };
}

describe('reanalyze store', () => {
  beforeEach(() => closeReanalyze());

  it('opens for a detection and closes again', () => {
    const detection = mkDetection(7);
    expect(reanalyzeState.detection).toBeNull();

    openReanalyze(detection);
    expect(reanalyzeState.detection).toStrictEqual(detection);

    closeReanalyze();
    expect(reanalyzeState.detection).toBeNull();
  });

  it('keeps the callbacks after close, because the modal closes before it reports', () => {
    const onChanged = vi.fn();
    openReanalyze(mkDetection(7), { onChanged });
    closeReanalyze();

    reanalyzeState.callbacks.onChanged?.();
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it('replaces the callbacks of a previous opener', () => {
    const first = vi.fn();
    openReanalyze(mkDetection(1), { onChanged: first });
    openReanalyze(mkDetection(2));

    expect(reanalyzeState.callbacks.onChanged).toBeUndefined();
  });
});
