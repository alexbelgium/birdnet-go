<script lang="ts">
  /**
   * ReanalyzeModal — second-opinion inference plus correction loop on a saved
   * detection clip.
   *
   * On open it runs every currently-loaded compatible classifier against the clip
   * and renders a species x model confidence grid. Per-row "Use this" applies the
   * chosen species as a correction (species, attributed model and confidence are
   * replaced in place and the detection is marked verified) and fires onCorrected
   * so the parent can re-fetch.
   *
   * Nothing is written until the user clicks "Use this" and confirms; the
   * reanalysis itself is read-only.
   *
   * i18n: strings are hardcoded English on purpose. Adding keys would touch the
   * eight shared locale files plus the generated types, which are the highest-churn
   * files upstream owns — the whole point of this feature living in fork-owned
   * files. Translate here if this is ever upstreamed.
   */
  import { untrack } from 'svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import {
    reanalyzeDetection,
    correctDetectionSpecies,
    type ReanalyzeResult,
    type ReanalyzePrediction,
  } from '$lib/utils/reanalyzeDetection';
  import { t } from '$lib/i18n';
  import { normalizeForLookup } from '$lib/utils/speciesNames';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { toastActions } from '$lib/stores/toast';
  import { fetchWithCSRF } from '$lib/utils/api';
  import { setDetectionVerification } from '$lib/utils/reviewDetection';
  import { loggers } from '$lib/utils/logger';
  import { Sparkles, AlertCircle, Check, ThumbsDown, Trash2, Lock } from '@lucide/svelte';
  import type { Detection } from '$lib/types/detection.types';

  interface Props {
    isOpen: boolean;
    /**
     * The detection under review. The modal needs more than its id: the current
     * species is what every prediction is being compared AGAINST, and without it
     * the grid cannot say whether the models agree with the original call — the
     * question the whole feature exists to answer. The lock and verification
     * state come along for free and let the actions reflect reality instead of
     * failing server-side.
     */
    detection: Detection | null;
    onClose: () => void;
    /**
     * Called after a successful correction. The parent typically re-fetches the
     * detection (and its derived widgets: rarity, history, taxonomy) so the view
     * reflects the new label without a page reload. Optional.
     */
    onCorrected?: () => void;
    /**
     * Called after the detection is deleted. The parent must navigate away — the
     * detection the modal was opened from no longer exists. Optional; when
     * omitted the delete button is not offered, because leaving the user on a
     * detail page for a deleted record is worse than not offering the action.
     */
    onDeleted?: () => void;
  }

  let { isOpen = false, detection = null, onClose, onCorrected, onDeleted }: Props = $props();

  const detectionId = $derived(detection?.id ?? null);
  const isLocked = $derived(detection?.locked === true);

  /**
   * The species every prediction is compared against, normalized once rather
   * than per row. normalizeForLookup is the codebase's canonical species-name
   * normalization and mirrors the backend's: it folds to NFC before lowercasing,
   * so a name submitted in NFD (composing keyboards on macOS) still matches
   * instead of silently leaving the current row unmarked.
   */
  const currentSpeciesKey = $derived(
    detection?.scientificName ? normalizeForLookup(detection.scientificName) : ''
  );

  const logger = loggers.ui;

  let isRunning = $state(false);
  let result = $state<ReanalyzeResult | null>(null);
  let errorMessage = $state<string | null>(null);

  /**
   * Monotonic sequence id guarding against stale responses. With a 120 s request
   * timeout a user can open the modal, close it, and reopen it for a different
   * detection while the first fetch is still in flight; without this guard that
   * older fetch eventually resolves and overwrites `result` with the previous
   * detection's predictions. Every call claims a new seq; responses whose seq is
   * no longer current are discarded.
   */
  let requestSeq = 0;

  // Correction-confirm state. Deliberately an inline row rather than a nested
  // modal, so the prediction being applied stays on screen while confirming.
  let pendingCorrection = $state<ReanalyzePrediction | null>(null);
  let isCorrecting = $state(false);

  // Verdict shortcuts shown when no correction row is selected: the same three
  // actions the Review tab offers, so an operator who looked at the grid and
  // decided the original call was right (or wrong, or junk) does not have to
  // close the modal and go find them. They call the same endpoints the rest of
  // the app uses — setDetectionVerification for the two review verdicts, and
  // DELETE /api/v2/detections/:id for the delete.
  let isVerdictPending = $state(false);
  // Delete is two-step for the same reason "Use this" is: it is irreversible.
  let confirmingDelete = $state(false);

  /**
   * Auto-run on every open, so results reflect any model the user enabled while
   * the modal was closed.
   *
   * The body is wrapped in untrack() because runReanalysis transitively reads
   * isRunning/result/errorMessage. Without it those become dependencies of this
   * effect, so flipping isRunning back to false at the end of the fetch re-fires
   * the effect, which starts another fetch, forever.
   */
  $effect(() => {
    if (!isOpen || detectionId === null) return;
    untrack(() => {
      result = null;
      errorMessage = null;
      pendingCorrection = null;
      isCorrecting = false;
      isVerdictPending = false;
      confirmingDelete = false;
      runReanalysis();
    });
  });

  async function runReanalysis() {
    if (!detectionId) return;
    // Claim the sequence id BEFORE awaiting, so any earlier in-flight call is
    // already stale by the time its response lands.
    const mySeq = ++requestSeq;
    isRunning = true;
    errorMessage = null;
    try {
      // If a request for this detection is already running (the user closed and
      // reopened the modal), the client hands back that same promise, so this
      // call resolves with its result rather than with nothing.
      const res = await reanalyzeDetection(detectionId);
      if (mySeq !== requestSeq) return; // superseded; discard
      result = res;
    } catch (err) {
      if (mySeq !== requestSeq) return;
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      // Only clear the spinner if this call is still the current one; otherwise
      // the newer in-flight call should keep it up until it resolves.
      if (mySeq === requestSeq) isRunning = false;
    }
  }

  function startCorrection(pred: ReanalyzePrediction) {
    pendingCorrection = pred;
  }

  function cancelCorrection() {
    pendingCorrection = null;
  }

  /**
   * Choose which model's read to attribute the correction to: the highest-confidence
   * one. That model "sees" the species most clearly, and its label vocabulary is the
   * one most likely to contain it.
   */
  function chooseModelForCorrection(
    pred: ReanalyzePrediction
  ): { modelId: string; confidence: number } | null {
    let bestModel = '';
    let bestConf = -1;
    for (const [modelId, conf] of Object.entries(pred.byModel)) {
      if (conf > bestConf) {
        bestConf = conf;
        bestModel = modelId;
      }
    }
    if (bestModel === '') return null;
    return { modelId: bestModel, confidence: bestConf };
  }

  async function confirmCorrection() {
    if (!detectionId || !pendingCorrection || isCorrecting) return;
    const choice = chooseModelForCorrection(pendingCorrection);
    if (!choice) {
      errorMessage =
        'No model scored this species, so there is nothing to attribute the correction to.';
      return;
    }
    // Snapshot everything this call depends on BEFORE awaiting, and claim a
    // sequence id. The open/close effect clears pendingCorrection and swaps
    // detectionId, so a close-and-reopen mid-flight would otherwise leave the
    // catch block dereferencing a null pendingCorrection — masking the real
    // error — and fire onClose/onCorrected against a different detection.
    const mySeq = ++requestSeq;
    const target = detectionId;
    const scientificName = pendingCorrection.scientificName;
    isCorrecting = true;
    try {
      const applied = await correctDetectionSpecies(target, {
        scientificName,
        modelId: choice.modelId,
        confidence: choice.confidence,
      });
      if (mySeq !== requestSeq) return; // modal moved on; the correction still applied server-side
      if (applied === null) {
        onClose(); // duplicate in flight; the original call owns the outcome
        return;
      }
      toastActions.success(
        `Detection corrected to ${localizeSpeciesName(applied.scientificName, applied.commonName)}.`
      );
      onClose();
      onCorrected?.();
    } catch (err) {
      logger.error('Correction failed', err, {
        component: 'ReanalyzeModal',
        detectionId: target,
        scientific: scientificName,
      });
      if (mySeq !== requestSeq) return;
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      if (mySeq === requestSeq) isCorrecting = false;
    }
  }

  /**
   * Apply a review verdict to the detection as-is (no species change). Delegates
   * to the shared helper the detections list and search views use, so the request
   * shape, the in-flight dedupe and the toasts stay identical across the app.
   */
  async function applyVerdict(verified: 'correct' | 'false_positive') {
    if (!detectionId || isVerdictPending) return;
    const target = detectionId;
    const mySeq = ++requestSeq;
    isVerdictPending = true;
    try {
      if (await setDetectionVerification(target, verified)) {
        if (mySeq !== requestSeq) return; // modal moved on; the verdict still applied
        onClose();
        onCorrected?.();
      }
    } finally {
      if (mySeq === requestSeq) isVerdictPending = false;
    }
  }

  async function confirmDelete() {
    if (!detectionId || isVerdictPending) return;
    // Same sequence guard the reanalysis and correction paths use, and it matters
    // most here: onDeleted navigates away. Without it, closing and reopening the
    // modal for another detection while a delete is in flight would navigate the
    // user off the detection they are now looking at.
    const mySeq = ++requestSeq;
    const target = detectionId;
    isVerdictPending = true;
    try {
      await fetchWithCSRF(`/api/v2/detections/${target}`, { method: 'DELETE' });
      toastActions.success('Detection deleted.');
      if (mySeq !== requestSeq) return; // superseded; the delete still happened
      onClose();
      onDeleted?.();
    } catch (err) {
      logger.error('Delete failed', err, { component: 'ReanalyzeModal', detectionId: target });
      if (mySeq !== requestSeq) return;
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      if (mySeq === requestSeq) {
        isVerdictPending = false;
        confirmingDelete = false;
      }
    }
  }

  /**
   * The grid, decorated once when a result arrives instead of recomputed per
   * cell while rendering. Each row carries everything the template needs, so the
   * markup contains no function calls at all: one O(rows x models) pass replaces
   * roughly thirty per-cell computations and the same number of cached signals.
   *
   * `byModel` is enumerated once per row to find the winning model — the one a
   * correction would be attributed to, and so the one worth emphasising.
   */
  interface DecoratedCell {
    modelId: string;
    /** undefined when this model did not predict the species in any window. */
    confidence: number | undefined;
    /** Global confidence-high/-medium/-low class, or '' when unscored. */
    colorClass: string;
    isBest: boolean;
    /** Signed points vs the detection's own confidence; only set on its row. */
    delta: string | null;
  }

  interface DecoratedRow {
    pred: ReanalyzePrediction;
    key: string;
    /**
     * The name to show. Models bake their own common name into the label, and
     * BirdNET's is always English ("Ficedula hypoleuca_Pied Flycatcher") while
     * Perch emits none at all — so displaying what the model returned gives a
     * grid that is half English and half the configured language. Resolving from
     * the scientific name through the app's own localizeSpeciesName (the same
     * helper DetectionRow and DetectionDetail use) makes every row consistent,
     * with the model's name kept only as the fallback for a species the locale
     * map does not know.
     */
    displayName: string;
    isCurrent: boolean;
    cells: DecoratedCell[];
  }

  /**
   * Same thresholds and class names ConfidenceCircle uses (>=70 high, >=40
   * medium), reusing the global .confidence-* rules in styles/custom.css rather
   * than restating their colours here. Note this is deliberately NOT the only
   * scheme in the app — features/dashboard uses 90/70/50/30 for its badges — so
   * this matches ConfidenceCircle specifically, not some single app-wide rule.
   */
  function confidenceClass(c: number): string {
    const pct = c * 100;
    if (pct >= 70) return 'confidence-high';
    if (pct >= 40) return 'confidence-medium';
    return 'confidence-low';
  }

  const decoratedRows = $derived.by<DecoratedRow[]>(() => {
    if (!result) return [];
    const models = result.modelsRun;
    const baseConfidence = detection?.confidence ?? null;

    return result.predictions.map(pred => {
      let bestModel = '';
      let bestConfidence = -1;
      for (const [modelId, conf] of Object.entries(pred.byModel)) {
        if (conf > bestConfidence) {
          bestConfidence = conf;
          bestModel = modelId;
        }
      }

      const isCurrent =
        !!currentSpeciesKey && normalizeForLookup(pred.scientificName) === currentSpeciesKey;

      return {
        pred,
        key: pred.scientificName || pred.commonName || '',
        displayName: localizeSpeciesName(pred.scientificName, pred.commonName),
        isCurrent,
        cells: models.map(m => {
          const confidence = pred.byModel[m.id];
          let delta: string | null = null;
          if (isCurrent && confidence !== undefined && baseConfidence !== null) {
            const points = (confidence - baseConfidence) * 100;
            if (Math.abs(points) >= 0.05) {
              delta = `${points > 0 ? '+' : '\u2212'}${Math.abs(points).toFixed(1)} pts`;
            }
          }
          return {
            modelId: m.id,
            confidence,
            colorClass: confidence === undefined ? '' : confidenceClass(confidence),
            isBest: m.id === bestModel,
            delta,
          };
        }),
      };
    });
  });

  function formatConfidencePercent(c: number): string {
    return `${(c * 100).toFixed(1)}%`;
  }

  function formatDuration(sec: number): string {
    if (sec >= 60) {
      const m = Math.floor(sec / 60);
      return `${m}m ${(sec % 60).toFixed(1)}s`;
    }
    return `${sec.toFixed(1)}s`;
  }

  // e.g. "BirdNET v2.4 + Google Perch v2 over 45.0s of audio"
  let summaryLine = $derived.by(() => {
    if (!result) return '';
    const modelNames = result.modelsRun.map(m => m.name).join(' + ');
    return `${modelNames} over ${formatDuration(result.clipDurationSec)} of audio`;
  });
</script>

<Modal {isOpen} title="Reanalyze this clip" size="3xl" {onClose}>
  <div class="space-y-4">
    <p class="text-sm text-base-content/70">
      Runs every loaded classifier over the saved audio and shows what each one thinks. Nothing is
      saved unless you apply a correction below.
    </p>

    <!-- Verdict shortcuts. Shown only while no correction row is selected: once
         the user has picked a species the pending correction is the action on
         screen, and offering three competing verdicts beside it invites a
         mis-click. Mirrors the Review tab's actions. -->
    <!-- Every action below WRITES. A locked detection refuses all of them
         server-side (review 409, delete 403), so offering them is offering
         failure — and the app's own convention is to hide, not disable, review
         and delete on a locked detection (see ui/ActionMenu.svelte). The reason
         is stated rather than left to be guessed at, since a silently missing
         control is as confusing as a silently disabled one. -->
    {#if result && !isRunning && isLocked}
      <div class="border-t border-base-300 pt-3 text-sm text-base-content/70">
        {t('common.review.form.detectionLocked')}
      </div>
    {/if}

    {#if result && !isRunning && !pendingCorrection && !isLocked}
      {#if confirmingDelete}
        <div class="rounded-md border border-error/40 bg-error/10 p-3 text-sm">
          <div class="mb-2 font-medium">Delete this detection?</div>
          <p class="mb-3 text-base-content/70">
            The detection and its audio clip are removed permanently. This cannot be undone.
          </p>
          <div class="flex justify-end gap-2">
            <button
              type="button"
              class="btn btn-sm btn-ghost"
              onclick={() => (confirmingDelete = false)}
              disabled={isVerdictPending}
            >
              {t('common.buttons.cancel')}
            </button>
            <button
              type="button"
              class="btn btn-sm btn-error"
              onclick={confirmDelete}
              disabled={isVerdictPending}
            >
              <Trash2 class="h-3.5 w-3.5" />
              {isVerdictPending ? 'Deleting…' : 'Delete permanently'}
            </button>
          </div>
        </div>
      {:else}
        <div class="flex flex-wrap justify-end gap-2 border-t border-base-300 pt-3">
          <!-- Labels reuse review-status keys the app already ships, so they read
               in the operator's language at no i18n cost AND say what they mean:
               "Correct" from the actions namespace would be ambiguous beside a
               species-correction flow. No aria-label — the visible text is the
               accessible name, so the two cannot disagree. The active verdict is
               marked, since three context-free buttons give no way to tell an
               unverified detection from one already reviewed. -->
          <button
            type="button"
            class="btn btn-xs"
            class:btn-success={detection?.verified === 'correct'}
            class:btn-ghost={detection?.verified !== 'correct'}
            onclick={() => applyVerdict('correct')}
            disabled={isVerdictPending}
            aria-pressed={detection?.verified === 'correct'}
          >
            <Check class="h-3.5 w-3.5" />
            {t('common.review.status.verifiedCorrect')}
          </button>
          <button
            type="button"
            class="btn btn-xs"
            class:btn-error={detection?.verified === 'false_positive'}
            class:btn-ghost={detection?.verified !== 'false_positive'}
            onclick={() => applyVerdict('false_positive')}
            disabled={isVerdictPending}
            aria-pressed={detection?.verified === 'false_positive'}
          >
            <ThumbsDown class="h-3.5 w-3.5" />
            {t('common.review.status.falsePositive')}
          </button>
          {#if onDeleted}
            <button
              type="button"
              class="btn btn-xs btn-ghost text-error"
              onclick={() => (confirmingDelete = true)}
              disabled={isVerdictPending}
              aria-label="Delete this detection"
            >
              <Trash2 class="h-3.5 w-3.5" />
              {t('common.buttons.delete')}
            </button>
          {/if}
        </div>
      {/if}
    {/if}

    <!-- Inline confirmation. Appears below the table once a row is chosen; the
         second click is what makes an accidental correction hard. -->
    {#if pendingCorrection && !isLocked}
      <div class="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
        <div class="mb-2 font-medium">
          Change this detection to {localizeSpeciesName(
            pendingCorrection.scientificName,
            pendingCorrection.commonName
          )}?
        </div>
        <p class="mb-3 text-base-content/70">
          The detection's species, attributed model and confidence are replaced, and it is marked as
          verified. The audio clip is untouched.
        </p>
        <div class="flex justify-end gap-2">
          <button
            type="button"
            class="btn btn-sm btn-ghost"
            onclick={cancelCorrection}
            disabled={isCorrecting}
          >
            {t('common.buttons.cancel')}
          </button>
          <button
            type="button"
            class="btn btn-sm btn-primary"
            onclick={confirmCorrection}
            disabled={isCorrecting}
          >
            <Sparkles class="h-3.5 w-3.5" />
            {isCorrecting ? 'Applying…' : 'Apply correction'}
          </button>
        </div>
      </div>
    {/if}

    {#if isRunning}
      <div class="flex items-center gap-2 text-sm text-base-content/70">
        <span class="loading loading-spinner loading-sm"></span>
        <span>Running inference…</span>
      </div>
    {/if}

    {#if errorMessage}
      <div role="alert" class="alert alert-error text-sm">
        <AlertCircle class="h-4 w-4" />
        <span>{errorMessage}</span>
      </div>
    {/if}

    {#if result && !isRunning}
      <div class="space-y-2">
        <div class="flex flex-wrap items-center gap-2 text-xs text-base-content/60">
          <span>{summaryLine}</span>
          {#if isLocked}
            <span class="badge badge-status-warning gap-1">
              <Lock class="h-3 w-3" />
              {t('common.review.status.locked')}
            </span>
          {/if}
        </div>

        {#if result.predictions.length === 0}
          <div class="text-sm italic text-base-content/70">
            No species crossed any model's reporting threshold for this clip.
          </div>
        {:else}
          <div class="overflow-x-auto">
            <table class="table table-sm w-full">
              <thead>
                <tr>
                  <th class="text-left">Species</th>
                  <!-- Model names wrap. They previously carried whitespace-nowrap,
                       which forced the header row wider than the modal: measured at
                       the modal's 768px width with three models, the table came out
                       780px and the wrapper scrolled horizontally while the species
                       column was squeezed to 92px. Letting the names wrap keeps the
                       table at 768px with no scroll and gives the species column
                       137px, for one extra line of header height.
                       align-bottom keeps a one-line name on the same baseline as a
                       wrapped one. No width clamps: min-width/max-width were tried
                       and measured no better here, only 20px taller. -->
                  {#each result.modelsRun as m (m.id)}
                    <th class="text-right align-bottom">
                      <div>{m.name}</div>
                    </th>
                  {/each}
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each decoratedRows as row (row.key)}
                  <tr class:current-row={row.isCurrent}>
                    <td class="text-sm">
                      <!-- Either half can be absent: a bare-scientific model row
                           has no common name, and a non-binomial sound class has
                           no scientific name. Render only what is actually there
                           rather than an empty second line. -->
                      <div
                        class={row.displayName !== row.pred.scientificName
                          ? 'font-medium'
                          : 'font-mono'}
                      >
                        {row.displayName}
                        {#if row.isCurrent}
                          <!-- The detection's existing call. Without this the
                               operator has to remember what they came in with,
                               since the detail page is behind the modal. -->
                          <span class="badge badge-sm badge-status-info">current</span>
                        {/if}
                      </div>
                      {#if row.pred.scientificName && row.displayName !== row.pred.scientificName}
                        <div class="font-mono text-xs italic text-base-content/60">
                          {row.pred.scientificName}
                        </div>
                      {/if}
                    </td>
                    {#each row.cells as cell (cell.modelId)}
                      <td class="text-right tabular-nums whitespace-nowrap">
                        {#if cell.confidence !== undefined}
                          <!-- The winning model is emphasised because it is the one
                               a correction would be attributed to, and because
                               "which model is most sure" is what the grid is read
                               for. -->
                          <span class={cell.colorClass} class:font-bold={cell.isBest}>
                            {formatConfidencePercent(cell.confidence)}
                          </span>
                          {#if cell.delta}
                            <div class="text-xs text-base-content/50">{cell.delta}</div>
                          {/if}
                        {:else}
                          <span class="text-base-content/30" title="not predicted by this model"
                            >—</span
                          >
                        {/if}
                      </td>
                    {/each}
                    <td class="text-right">
                      <!-- Only rows the server marked correctable can be applied:
                           a sound class is not a species, and a row with no
                           scientific name has nothing to key a correction on.
                           Omit the button rather than offering one that can only
                           fail. -->
                      {#if row.pred.correctable && row.pred.scientificName && !isLocked}
                        <button
                          type="button"
                          class="btn btn-xs btn-ghost"
                          onclick={() => startCorrection(row.pred)}
                          disabled={isCorrecting}
                          aria-label={`Use ${row.displayName} as the species for this detection`}
                        >
                          <Check class="h-3.5 w-3.5" />
                          Use this
                        </button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</Modal>

<style>
  /* The only rule this component still needs: the detection's existing species.
     Tinting the whole row rather than just the badge makes the comparison
     baseline findable without reading every name. Everything else reuses classes
     the app already ships — .confidence-* from styles/custom.css and
     badge-status-* from styles/tailwind.css. */
  .current-row {
    background-color: color-mix(in srgb, var(--color-info) 8%, transparent);
  }
</style>
