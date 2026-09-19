import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';

const reanalyzeDetection = vi.fn();
const correctDetectionSpecies = vi.fn();
const toastSuccess = vi.fn();
const setDetectionVerification = vi.fn();
const fetchWithCSRF = vi.fn();
let localizedNames = new Map<string, string>();

vi.mock('$lib/utils/reanalyzeDetection', () => ({
  reanalyzeDetection: (...args: unknown[]) => reanalyzeDetection(...args),
  correctDetectionSpecies: (...args: unknown[]) => correctDetectionSpecies(...args),
}));

vi.mock('$lib/stores/toast', () => ({
  toastActions: { success: (...args: unknown[]) => toastSuccess(...args) },
}));

vi.mock('$lib/utils/reviewDetection', () => ({
  setDetectionVerification: (...args: unknown[]) => setDetectionVerification(...args),
}));

vi.mock('$lib/utils/speciesDisplay', () => ({
  localizeSpeciesName: (scientific?: string, fallback?: string) =>
    (scientific ? localizedNames.get(scientific) : undefined) ?? fallback ?? scientific ?? '',
}));

vi.mock('$lib/utils/api', () => ({
  fetchWithCSRF: (...args: unknown[]) => fetchWithCSRF(...args),
}));

import ReanalyzeModal from './ReanalyzeModal.svelte';
import type { ComponentProps } from 'svelte';
import type { Detection } from '$lib/types/detection.types';

function mkDetection(id: number, over: Partial<Detection> = {}): Detection {
  return {
    id,
    date: '2026-09-08',
    time: '07:14:00',
    beginTime: '',
    endTime: '',
    speciesCode: 'lottit1',
    scientificName: 'Aegithalos caudatus',
    commonName: 'Long-tailed Tit',
    confidence: 0.996,
    verified: 'unverified',
    locked: false,
    ...over,
  };
}

const predictions = [
  {
    scientificName: 'Ficedula hypoleuca',
    commonName: 'Pied Flycatcher',
    byModel: { 'BirdNET_V2.4': 0.998, Perch_V2: 0.869 },
    correctable: true,
  },
  {
    scientificName: 'Aegithalos caudatus',
    commonName: 'Long-tailed Tit',
    byModel: { 'BirdNET_V2.4': 0.996 },
    correctable: true,
  },
  {
    scientificName: 'Parus major',
    commonName: 'Great Tit',
    byModel: { 'BirdNET_V2.4': 0.88, Perch_V2: 0.71 },
    correctable: true,
  },
  {
    scientificName: 'Cyanistes caeruleus',
    commonName: 'Eurasian Blue Tit',
    byModel: { 'BirdNET_V2.4': 0.75 },
    correctable: true,
  },
  {
    scientificName: 'Periparus ater',
    commonName: 'Coal Tit',
    byModel: { Perch_V2: 0.66 },
    correctable: true,
  },
];

const twoModelResult = {
  detectionId: 7,
  clipDurationSec: 45,
  modelsRun: [
    { id: 'BirdNET_V2.4', name: 'BirdNET v2.4', sampleRate: 48000, windowCount: 29 },
    { id: 'Perch_V2', name: 'Perch v2', sampleRate: 32000, windowCount: 17 },
  ],
  predictions,
};

const VERDICT_CORRECT = 'dashboard.recentDetections.actions.markCorrect';
const VERDICT_INCORRECT = 'dashboard.recentDetections.actions.markFalsePositive';

function renderModal(over: Partial<ComponentProps<typeof ReanalyzeModal>> = {}) {
  return render(ReanalyzeModal, {
    props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn(), ...over },
  });
}

describe('ReanalyzeModal', () => {
  let user: ReturnType<typeof userEvent.setup>;

  beforeEach(() => {
    user = userEvent.setup();
    reanalyzeDetection.mockReset();
    correctDetectionSpecies.mockReset();
    toastSuccess.mockReset();
    setDetectionVerification.mockReset();
    fetchWithCSRF.mockReset();
    localizedNames = new Map();
  });

  afterEach(() => vi.clearAllMocks());

  it('does not run reanalysis while closed', () => {
    render(ReanalyzeModal, {
      props: { isOpen: false, detection: mkDetection(7), onClose: vi.fn() },
    });
    expect(reanalyzeDetection).not.toHaveBeenCalled();
  });

  it('keeps Correct, Incorrect and Delete in the top modal chrome even while loading', async () => {
    reanalyzeDetection.mockReturnValue(new Promise(() => {}));
    renderModal({ onDeleted: vi.fn() });

    const correct = screen.getByRole('button', { name: VERDICT_CORRECT });
    const incorrect = screen.getByRole('button', { name: VERDICT_INCORRECT });
    const del = screen.getByRole('button', { name: 'Delete this detection' });

    expect(correct).toBeInTheDocument();
    expect(incorrect).toBeInTheDocument();
    expect(del).toBeInTheDocument();
    expect(correct.parentElement).toHaveClass('absolute', 'top-2');
    expect(screen.getByText('Running inference…')).toBeInTheDocument();
  });

  it('runs reanalysis exactly once and renders a compact per-model grid', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal();

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());
    expect(reanalyzeDetection).toHaveBeenCalledTimes(1);
    expect(screen.getByText('BirdNET v2.4')).toBeInTheDocument();
    expect(screen.getByText('Perch v2')).toBeInTheDocument();
    expect(screen.getByText('99.8%')).toBeInTheDocument();
    expect(screen.getByRole('table')).toHaveClass('table-xs', 'text-xs');
  });

  it('shows only the first three rows until See more is clicked', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal();

    await waitFor(() => expect(screen.getByText('Great Tit')).toBeInTheDocument());
    expect(screen.queryByText('Eurasian Blue Tit')).not.toBeInTheDocument();
    expect(screen.queryByText('Coal Tit')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'See more' }));
    expect(screen.getByText('Eurasian Blue Tit')).toBeInTheDocument();
    expect(screen.getByText('Coal Tit')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'See less' })).toHaveAttribute('aria-expanded', 'true');
  });

  it('uses a text-only Select control for species correction', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal();

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());
    const selectText = screen.getAllByText('Select')[0];
    const selectButton = selectText.closest('button');
    expect(selectButton).not.toBeNull();
    expect(selectButton?.querySelector('svg')).toBeNull();
    expect(selectButton).toHaveAttribute(
      'aria-label',
      'Select Pied Flycatcher as the species for this detection'
    );
  });

  it('keeps the top decisions visible while a species confirmation is open', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal({ onDeleted: vi.fn() });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Select Pied Flycatcher as the species for this detection')
    );

    expect(screen.getByText('Change this detection to Pied Flycatcher?')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: VERDICT_CORRECT })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: VERDICT_INCORRECT })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete this detection' })).toBeInTheDocument();
  });

  it('applies a correction attributed to the highest-confidence model', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    correctDetectionSpecies.mockResolvedValue({
      detectionId: 7,
      scientificName: 'Ficedula hypoleuca',
      commonName: 'Pied Flycatcher',
      modelId: 'BirdNET_V2.4',
      modelName: 'BirdNET v2.4',
      confidence: 0.998,
      verified: 'correct',
    });
    const onClose = vi.fn();
    const onCorrected = vi.fn();
    renderModal({ onClose, onCorrected });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Select Pied Flycatcher as the species for this detection')
    );
    expect(correctDetectionSpecies).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: /Apply correction/ }));

    await waitFor(() => expect(correctDetectionSpecies).toHaveBeenCalledTimes(1));
    expect(correctDetectionSpecies).toHaveBeenCalledWith(7, {
      scientificName: 'Ficedula hypoleuca',
      modelId: 'BirdNET_V2.4',
      confidence: 0.998,
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCorrected).toHaveBeenCalledTimes(1);
  });

  it('keeps Delete visible while its confirmation panel is shown', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    fetchWithCSRF.mockResolvedValue({});
    const onDeleted = vi.fn();
    renderModal({ onDeleted });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Delete this detection' }));
    expect(fetchWithCSRF).not.toHaveBeenCalled();
    expect(screen.getByText('Delete this detection?')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete this detection' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Delete permanently/ }));
    await waitFor(() => expect(fetchWithCSRF).toHaveBeenCalledTimes(1));
    expect(fetchWithCSRF).toHaveBeenCalledWith('/api/v2/detections/7', { method: 'DELETE' });
    expect(onDeleted).toHaveBeenCalledTimes(1);
  });

  it('applies Incorrect through the shared verification helper', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    setDetectionVerification.mockResolvedValue(true);
    const onClose = vi.fn();
    const onCorrected = vi.fn();
    renderModal({ onClose, onCorrected });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: VERDICT_INCORRECT }));
    await waitFor(() => expect(setDetectionVerification).toHaveBeenCalledWith(7, 'false_positive'));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCorrected).toHaveBeenCalledTimes(1);
  });

  it('hides write actions for a locked detection but keeps the grid readable', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal({ detection: mkDetection(7, { locked: true }), onDeleted: vi.fn() });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    expect(screen.queryByRole('button', { name: VERDICT_CORRECT })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Delete this detection' })).not.toBeInTheDocument();
    expect(screen.queryByText('Select')).not.toBeInTheDocument();
    expect(screen.getByText('common.review.form.detectionLocked')).toBeInTheDocument();
    expect(screen.getByText('99.8%')).toBeInTheDocument();
  });

  it('marks the current species row', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal();
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    const badges = screen.getAllByText('current');
    expect(badges).toHaveLength(1);
    expect(badges[0].closest('tr')?.textContent).toContain('Long-tailed Tit');
  });

  it('uses a localized name when one is available and keeps scientific name underneath', async () => {
    localizedNames = new Map([
      ['Ficedula hypoleuca', 'Gobemouche noir'],
      ['Aegithalos caudatus', 'Mésange à longue queue'],
      ['Parus major', 'Mésange charbonnière'],
    ]);
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal();

    await waitFor(() => expect(screen.getByText('Gobemouche noir')).toBeInTheDocument());
    expect(screen.getByText('Mésange à longue queue')).toBeInTheDocument();
    expect(screen.getByText('Mésange charbonnière')).toBeInTheDocument();
    expect(screen.queryByText('Pied Flycatcher')).not.toBeInTheDocument();
    expect(screen.getByText('Ficedula hypoleuca')).toBeInTheDocument();
  });

  it('survives close/reopen while a reanalysis request is already in flight', async () => {
    let release!: (v: unknown) => void;
    reanalyzeDetection.mockReturnValue(
      new Promise(resolve => {
        release = resolve;
      })
    );
    const onClose = vi.fn();
    const { rerender } = renderModal({ onClose });

    await rerender({ isOpen: false, detection: mkDetection(7), onClose });
    await rerender({ isOpen: true, detection: mkDetection(7), onClose });
    release(twoModelResult);

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());
    expect(screen.queryByText('Running inference…')).not.toBeInTheDocument();
  });

  it('reports a reanalysis failure instead of a blank grid', async () => {
    reanalyzeDetection.mockRejectedValue(new Error('Inference failed'));
    renderModal();
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Inference failed'));
    expect(screen.queryByText('Running inference…')).not.toBeInTheDocument();
  });
});
