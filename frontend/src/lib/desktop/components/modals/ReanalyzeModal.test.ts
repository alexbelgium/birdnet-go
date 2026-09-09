import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';

const reanalyzeDetection = vi.fn();
const correctDetectionSpecies = vi.fn();
const toastSuccess = vi.fn();
const setDetectionVerification = vi.fn();
/** Stands in for the UI-locale species map the real helper consults. */
let localizedNames = new Map<string, string>();
const fetchWithCSRF = vi.fn();

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

/**
 * The modal takes the whole detection, not just an id: the grid marks the row
 * matching the current species, and the lock/verified state drive which actions
 * are offered.
 */
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

/** Every test renders the same shape; only the overrides differ. */
// The i18n mock in src/test/setup.ts echoes unknown keys, so the accessible name
// of a localized button IS its key here. Asserting the key verifies the component
// asks for the right string; i18n:check-usage separately proves the key resolves.
const VERDICT_CORRECT = 'common.review.status.verifiedCorrect';
const VERDICT_FALSE_POSITIVE = 'common.review.status.falsePositive';

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

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('does not run reanalysis while closed', () => {
    render(ReanalyzeModal, {
      props: { isOpen: false, detection: mkDetection(7), onClose: vi.fn() },
    });
    expect(reanalyzeDetection).not.toHaveBeenCalled();
  });

  it('runs reanalysis exactly once on open and renders the per-model grid', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });

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

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose, onCorrected },
    });
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
    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });
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

  it('survives a close and reopen while a correction is still in flight', async () => {
    // The open/close effect clears pendingCorrection and can swap detectionId.
    // Without snapshotting the payload and guarding on a sequence id, the catch
    // block dereferences a null pendingCorrection — masking the real error — and
    // onClose/onCorrected fire against whatever detection is showing now.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    let failCorrection!: (e: unknown) => void;
    correctDetectionSpecies.mockReturnValue(
      new Promise((_, reject) => {
        failCorrection = reject;
      })
    );
    const onClose = vi.fn();
    const onCorrected = vi.fn();

    const { rerender } = render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose, onCorrected },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Use Pied Flycatcher as the species for this detection')
    );
    await user.click(screen.getByRole('button', { name: /Apply correction/ }));
    await waitFor(() => expect(correctDetectionSpecies).toHaveBeenCalledTimes(1));

    // Close and reopen for a DIFFERENT detection while the correction is pending.
    await rerender({ isOpen: false, detection: mkDetection(7), onClose, onCorrected });
    await rerender({ isOpen: true, detection: mkDetection(99), onClose, onCorrected });

    failCorrection(new Error('boom'));
    await waitFor(() => expect(reanalyzeDetection).toHaveBeenCalledWith(99));

    // The superseded failure must not leak into the reopened modal.
    expect(onCorrected).not.toHaveBeenCalled();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('surfaces a reanalysis failure instead of an empty grid', async () => {
    reanalyzeDetection.mockRejectedValue(new Error('Inference failed'));

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });

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

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });

    await waitFor(() => expect(screen.getByText('tool')).toBeInTheDocument());
    expect(screen.getByText('42.0%')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Use .* as the species/ })).not.toBeInTheDocument();
  });

  it('reports an empty result set rather than rendering a blank table', async () => {
    reanalyzeDetection.mockResolvedValue({ ...twoModelResult, predictions: [] });

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });

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
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });
    await rerender({ isOpen: false, detection: mkDetection(7), onClose: vi.fn() });
    await rerender({ isOpen: true, detection: mkDetection(7), onClose: vi.fn() });

    release(twoModelResult);

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());
    expect(screen.queryByText('Running inference…')).not.toBeInTheDocument();
  });

  it('offers the three review verdicts once results are in', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn(), onDeleted: vi.fn() },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    expect(screen.getByRole('button', { name: VERDICT_CORRECT })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: VERDICT_FALSE_POSITIVE })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete this detection' })).toBeInTheDocument();
  });

  it('hides the verdicts once a correction row is selected', async () => {
    // The pending correction is the action on screen; three competing verdicts
    // beside it invite a mis-click on an irreversible button.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn(), onDeleted: vi.fn() },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Use Pied Flycatcher as the species for this detection')
    );

    await waitFor(() =>
      expect(screen.queryByRole('button', { name: VERDICT_CORRECT })).not.toBeInTheDocument()
    );
    expect(screen.queryByRole('button', { name: 'Delete this detection' })).not.toBeInTheDocument();
    // ...and the correction confirmation is what is on screen instead.
    expect(screen.getByText('Change this detection to Pied Flycatcher?')).toBeInTheDocument();
  });

  it('applies a review verdict through the shared helper and closes', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    setDetectionVerification.mockResolvedValue(true);
    const onClose = vi.fn();
    const onCorrected = vi.fn();

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose, onCorrected },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: VERDICT_FALSE_POSITIVE }));

    // The same helper the detections list and search views use, so the request
    // shape and dedupe behaviour stay identical across the app.
    await waitFor(() => expect(setDetectionVerification).toHaveBeenCalledWith(7, 'false_positive'));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCorrected).toHaveBeenCalledTimes(1);
  });

  it('requires a second click before deleting, then calls onDeleted', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    fetchWithCSRF.mockResolvedValue({});
    const onDeleted = vi.fn();

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn(), onDeleted },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Delete this detection' }));
    // Irreversible: the first click must only arm the confirmation.
    expect(fetchWithCSRF).not.toHaveBeenCalled();
    expect(screen.getByText('Delete this detection?')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Delete permanently/ }));

    await waitFor(() => expect(fetchWithCSRF).toHaveBeenCalledTimes(1));
    expect(fetchWithCSRF).toHaveBeenCalledWith('/api/v2/detections/7', { method: 'DELETE' });
    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
  });

  it('omits Delete when the parent cannot handle the aftermath', async () => {
    // Deleting leaves the parent showing a detail page for a record that no
    // longer exists, so the action is only offered when the parent can navigate
    // away from it.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    expect(screen.queryByRole('button', { name: 'Delete this detection' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: VERDICT_CORRECT })).toBeInTheDocument();
  });

  it('does not navigate away when a delete is superseded by a reopen', async () => {
    // onDeleted navigates. Without a sequence guard, a delete that resolves after
    // the modal has been reopened for a DIFFERENT detection would navigate the
    // user off the detection they are now looking at.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    let finishDelete!: (v: unknown) => void;
    fetchWithCSRF.mockReturnValue(
      new Promise(resolve => {
        finishDelete = resolve;
      })
    );
    const onClose = vi.fn();
    const onDeleted = vi.fn();

    const { rerender } = render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose, onDeleted },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Delete this detection' }));
    await user.click(screen.getByRole('button', { name: /Delete permanently/ }));
    await waitFor(() => expect(fetchWithCSRF).toHaveBeenCalledTimes(1));

    await rerender({ isOpen: false, detection: mkDetection(7), onClose, onDeleted });
    await rerender({ isOpen: true, detection: mkDetection(99), onClose, onDeleted });

    finishDelete({});
    await waitFor(() => expect(reanalyzeDetection).toHaveBeenCalledWith(99));

    expect(onDeleted).not.toHaveBeenCalled();
  });

  it('does not close the reopened modal when a verdict is superseded', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    let finishVerdict!: (v: unknown) => void;
    setDetectionVerification.mockReturnValue(
      new Promise(resolve => {
        finishVerdict = resolve;
      })
    );
    const onClose = vi.fn();
    const onCorrected = vi.fn();

    const { rerender } = render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose, onCorrected },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: VERDICT_CORRECT }));
    await waitFor(() => expect(setDetectionVerification).toHaveBeenCalledTimes(1));

    await rerender({ isOpen: false, detection: mkDetection(7), onClose, onCorrected });
    await rerender({ isOpen: true, detection: mkDetection(99), onClose, onCorrected });

    finishVerdict(true);
    await waitFor(() => expect(reanalyzeDetection).toHaveBeenCalledWith(99));

    expect(onCorrected).not.toHaveBeenCalled();
  });

  it("marks the row matching the detection's current species", async () => {
    // The comparison baseline. The detail page is behind the modal, so without
    // this the operator has to remember what they came in with to judge whether
    // the models agree with the original call. The fixture's second prediction is
    // the species mkDetection carries, so it is the row that must be marked.
    reanalyzeDetection.mockResolvedValue(twoModelResult);

    render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    // Exactly one row is marked, and it is the matching species — not the
    // top-scoring one, which is a different bird.
    const badges = screen.getAllByText('current');
    expect(badges).toHaveLength(1);
    expect(badges[0].closest('tr')?.textContent).toContain('Long-tailed Tit');
  });

  it('hides every write action on a locked detection and says why', async () => {
    // All four actions are writes and the server refuses each on a locked
    // detection (review 409, delete 403), so offering them is offering failure.
    // The app hides rather than disables these (ui/ActionMenu.svelte), and the
    // reason is stated so a missing control is not left to be guessed at.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal({ detection: mkDetection(7, { locked: true }), onDeleted: vi.fn() });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    expect(
      screen.queryByLabelText('Use Pied Flycatcher as the species for this detection')
    ).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: VERDICT_CORRECT })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Delete this detection' })).not.toBeInTheDocument();
    expect(screen.getByText('common.review.form.detectionLocked')).toBeInTheDocument();

    // The grid itself stays readable — reanalysis is read-only.
    expect(screen.getByText('99.8%')).toBeInTheDocument();
  });

  it('marks the verdict the detection already carries', async () => {
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    render(ReanalyzeModal, {
      props: {
        isOpen: true,
        detection: mkDetection(7, { verified: 'false_positive' }),
        onClose: vi.fn(),
      },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    // Without this the three shortcuts are context-free: nothing distinguishes an
    // unverified detection from one already marked.
    expect(screen.getByRole('button', { name: VERDICT_FALSE_POSITIVE })).toHaveAttribute(
      'aria-pressed',
      'true'
    );
    expect(screen.getByRole('button', { name: VERDICT_CORRECT })).toHaveAttribute(
      'aria-pressed',
      'false'
    );
  });

  it('shows the configured language, not the English baked into model labels', async () => {
    // BirdNET ships an English common name inside its label
    // ("Ficedula hypoleuca_Pied Flycatcher") while Perch ships none, so rendering
    // what the model returned gives a grid that is half English and half the
    // configured language. Every name resolves through the app's own
    // localizeSpeciesName instead.
    localizedNames = new Map([
      ['Ficedula hypoleuca', 'Gobemouche noir'],
      ['Aegithalos caudatus', 'Mésange à longue queue'],
    ]);
    reanalyzeDetection.mockResolvedValue(twoModelResult);

    renderModal();

    await waitFor(() => expect(screen.getByText('Gobemouche noir')).toBeInTheDocument());
    expect(screen.getByText('Mésange à longue queue')).toBeInTheDocument();
    expect(screen.queryByText('Pied Flycatcher')).not.toBeInTheDocument();
    expect(screen.queryByText('Long-tailed Tit')).not.toBeInTheDocument();
    // The scientific name stays as the secondary line.
    expect(screen.getByText('Ficedula hypoleuca')).toBeInTheDocument();
  });

  it('falls back to the model name for a species the locale map does not know', async () => {
    // An exotic or newly added species must still render, not go blank.
    localizedNames = new Map();
    reanalyzeDetection.mockResolvedValue(twoModelResult);

    renderModal();

    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());
  });

  it('withdraws a pending correction if the detection becomes locked', async () => {
    // The confirmation panel is its own write path: hiding the row buttons and
    // the verdict group leaves it on screen with a live Apply button, which the
    // server would then refuse with a 409.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    const { rerender } = render(ReanalyzeModal, {
      props: { isOpen: true, detection: mkDetection(7), onClose: vi.fn() },
    });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    await user.click(
      screen.getByLabelText('Use Pied Flycatcher as the species for this detection')
    );
    expect(screen.getByRole('button', { name: /Apply correction/ })).toBeInTheDocument();

    await rerender({
      isOpen: true,
      detection: mkDetection(7, { locked: true }),
      onClose: vi.fn(),
    });

    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /Apply correction/ })).not.toBeInTheDocument()
    );
    expect(correctDetectionSpecies).not.toHaveBeenCalled();
  });

  it('puts the decision block above the grid', async () => {
    // On a phone the grid is tall enough to push the actions off screen, so the
    // decision the modal exists to support would need a scroll to reach.
    reanalyzeDetection.mockResolvedValue(twoModelResult);
    renderModal({ onDeleted: vi.fn() });
    await waitFor(() => expect(screen.getByText('Pied Flycatcher')).toBeInTheDocument());

    const verdict = screen.getByRole('button', { name: VERDICT_CORRECT });
    const table = screen.getByRole('table');
    // Node.DOCUMENT_POSITION_FOLLOWING === 4: the table comes after the buttons.
    expect(verdict.compareDocumentPosition(table) & 4).toBeTruthy();
  });
});
