import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import type { Detection } from '$lib/types/detection.types';
import ReanalyzeHost from './ReanalyzeHost.svelte';
import { openReanalyze, closeReanalyze } from '$lib/stores/reanalyze.svelte';

vi.mock('$lib/utils/reanalyzeDetection', () => ({
  reanalyzeDetection: vi.fn(() => new Promise(() => {})),
  correctDetectionSpecies: vi.fn(),
}));

const detection: Detection = {
  id: 7,
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
  clipName: 'clips/2026/09/pied_flycatcher.wav',
};

describe('ReanalyzeHost', () => {
  afterEach(() => closeReanalyze());

  it('renders no dialog until a detection is opened, and removes it on close', () => {
    render(ReanalyzeHost);
    expect(screen.queryByRole('dialog')).toBeNull();

    openReanalyze(detection);
    flushSync();
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    closeReanalyze();
    flushSync();
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
