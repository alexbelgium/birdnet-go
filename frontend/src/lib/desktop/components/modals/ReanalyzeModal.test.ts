import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';

const reanalyzeDetection = vi.fn();
const correctDetectionSpecies = vi.fn();
const toastSuccess = vi.fn();

vi.mock('$lib/utils/reanalyzeDetection', () => ({
  reanalyzeDetection: (...args: unknown[]) => reanalyzeDetection(...args),
  correctDetectionSpecies: (...args: unknown[]) => correctDetectionSpecies(...args),
}));

vi.mock('$lib/stores/toast', () => ({
  toastActions: { success: (...args: unknown[]) => toastSuccess(...args) },
}));

import ReanalyzeModal from './ReanalyzeModal.svelte';

const twoModelResult = {
  detectionId: 7,
  clipDurationSec: 45,
  modelsRun: [
    { id: 'BirdNET_V2.4', name: 'BirdNET v2.4', sampleRate: 48000, windowCount: 29 },
    { id: 'Perch_V2', name: 'Perch v2', sampleRate: 32000, windowCount: 17 },
  ],
  predictions: [
    {
      scientificName: 'Ficedula hypoleuca',
      commonName: 'Pied Flycatcher',
      byModel: { 'BirdNET_V2.4': 0.998, Perch_V2: 0.869 },
      correctable: true,
    },
    {
      // Seen by one model only: the other column must render a placeholder
      // rather than 0.0%, which would read as "this model rejected it".
      scientificName: 'Aegithalos caudatus',
      commonName: 'Long-tailed Tit',
      byModel: { 'BirdNET_V2.4': 0.996 },
      correctable: true,
    },
  ],
};

describe('ReanalyzeModal', () => {
  let user: ReturnType<typeof userEvent.setup>;

  beforeEach(() => {
    user = userEvent.setup();
    reanalyzeDetection.mockReset();
    correctDetectionSpecies.mockReset();
    toastSuccess.mockReset();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('does not run reanalysis while closed', () => {
    render(ReanalyzeModal, { props: { isOpen: false, detectionId: 7, onClose: vi.fn() } });
    expect(reanalyzeDetection).not.toHaveBeenCalled();
  });

  it('runs reanalysis exactly once on open and renders the per-model grid', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);

    render(ReanalyzeModal, { props: { isOpen: true, detectionId: 7, onClose: vi.fn() } });

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    // The regression this guards: runReanalysis reads isRunning, so without the
    // untrack() around the auto-run effect, clearing isRunning re-fires the
    // effect and the modal fetches forever.
    expect(reanalyzeDetection).toHaveBeenCalledTimes(1);

    expect(screen.getByText('BirdNET v2.4')).toBeInTheDocument();
    expect(screen.getByText('Perch v2')).toBeInTheDocument();
    expect(screen.getByText('99.8%')).toBeInTheDocument();
    expect(screen.getByText('86.9%')).toBeInTheDocument();
    // Long-tailed Tit was not scored by Perch: placeholder, not a number.
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.getByText(/BirdNET v2.4 \+ Perch v2 over 45.0s of audio/)).toBeInTheDocument();
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

    render(ReanalyzeModal, { props: { isOpen: true, detectionId: 7, onClose, onCorrected } });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Use Pied Flycatcher as the species for this detection')
    );

    // Two-step: choosing a row must not write anything on its own.
    expect(correctDetectionSpecies).not.toHaveBeenCalled();
    expect(screen.getByText('Change this detection to Pied Flycatcher?')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Apply correction/ }));

    await waitFor(() => expect(correctDetectionSpecies).toHaveBeenCalledTimes(1));
    expect(correctDetectionSpecies).toHaveBeenCalledWith(7, {
      scientificName: 'Ficedula hypoleuca',
      modelId: 'BirdNET_V2.4', // 99.8% beats Perch's 86.9%
      confidence: 0.998,
    });
    await waitFor(() => expect(onCorrected).toHaveBeenCalledTimes(1));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(toastSuccess).toHaveBeenCalledTimes(1);
  });

  it('cancelling the confirmation writes nothing', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    render(ReanalyzeModal, { props: { isOpen: true, detectionId: 7, onClose: vi.fn() } });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Use Pied Flycatcher as the species for this detection')
    );
    await user.click(screen.getByRole('button', { name: 'Cancel' }));

    await waitFor(() =>
      expect(
        screen.queryByText('Change this detection to Pied Flycatcher?')
      ).not.toBeInTheDocument()
    );
    expect(correctDetectionSpecies).not.toHaveBeenCalled();
  });

  it('surfaces a reanalysis failure instead of an empty grid', async () => {
    reanalyzeDetection.mockRejectedValue(new Error('Inference failed'));

    render(ReanalyzeModal, { props: { isOpen: true, detectionId: 7, onClose: vi.fn() } });

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Inference failed'));
    // The spinner must not survive the failure.
    expect(screen.queryByText('Running inference…')).not.toBeInTheDocument();
  });

  it('shows a sound class but offers no correction for it', async () => {
    // Perch emits non-species sound classes alongside birds. The server marks
    // them correctable:false, because "power_tool" splits into a scientific/
    // common pair that looks exactly like a species named "power" — applying it
    // would relabel a bird as a sound class and mint a junk label row.
    reanalyzeDetection.mockResolvedValue({
      ...twoModelResult,
      predictions: [
        {
          scientificName: 'power',
          commonName: 'tool',
          byModel: { Perch_V2: 0.42 },
          correctable: false,
        },
      ],
    });

    render(ReanalyzeModal, { props: { isOpen: true, detectionId: 7, onClose: vi.fn() } });

    await waitFor(() => expect(screen.getByText('tool')).toBeInTheDocument());
    expect(screen.getByText('42.0%')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Use .* as the species/ })).not.toBeInTheDocument();
  });

  it('reports an empty result set rather than rendering a blank table', async () => {
    reanalyzeDetection.mockResolvedValue({ ...twoModelResult, predictions: [] });

    render(ReanalyzeModal, { props: { isOpen: true, detectionId: 7, onClose: vi.fn() } });

    await waitFor(() =>
      expect(
        screen.getByText("No species crossed any model's reporting threshold for this clip.")
      ).toBeInTheDocument()
    );
  });

  it('renders the result of a request that was already in flight when it reopened', async () => {
    // Close-and-reopen: the client hands the second caller the SAME promise as
    // the still-running first request, so the reopened modal must fill in from it
    // rather than sitting empty with no request left to populate it.
    let release!: (v: unknown) => void;
    reanalyzeDetection.mockReturnValue(
      new Promise(resolve => {
        release = resolve;
      })
    );

    const { rerender } = render(ReanalyzeModal, {
      props: { isOpen: true, detectionId: 7, onClose: vi.fn() },
    });
    await rerender({ isOpen: false, detectionId: 7, onClose: vi.fn() });
    await rerender({ isOpen: true, detectionId: 7, onClose: vi.fn() });

    release(twoModelResult);

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());
    expect(screen.queryByText('Running inference…')).not.toBeInTheDocument();
  });
});
