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
  import { toastActions } from '$lib/stores/toast';
  import { fetchWithCSRF } from '$lib/utils/api';
  import { setDetectionVerification } from '$lib/utils/reviewDetection';
  import { loggers } from '$lib/utils/logger';
  import { Sparkles, AlertCircle, Check, ThumbsDown, Trash2 } from '@lucide/svelte';

  interface Props {
    isOpen: boolean;
    detectionId: number | null;
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

  let { isOpen = false, detectionId = null, onClose, onCorrected, onDeleted }: Props = $props();

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
        `Detection corrected to ${applied.commonName || applied.scientificName}.`
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
    const mySeq = requestSeq;
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
    const target = detectionId;
    isVerdictPending = true;
    try {
      await fetchWithCSRF(`/api/v2/detections/${target}`, { method: 'DELETE' });
      toastActions.success('Detection deleted.');
      onClose();
      onDeleted?.();
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
      logger.error('Delete failed', err, { component: 'ReanalyzeModal', detectionId: target });
    } finally {
      isVerdictPending = false;
      confirmingDelete = false;
    }
  }

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
        <div class="text-xs text-base-content/60">{summaryLine}</div>

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
                  <!-- Model names wrap onto a second line rather than running
                       into the neighbouring column. whitespace-nowrap made a long
                       name overflow its cell and overlap the next header; a wrap
                       plus a floor on the column width keeps each name inside its
                       own column and the header row readable. -->
                  {#each result.modelsRun as m (m.id)}
                    <th class="model-col text-right align-bottom" title={m.name}>{m.name}</th>
                  {/each}
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each result.predictions as pred (pred.scientificName || pred.commonName)}
                  <tr>
                    <td class="text-sm">
                      <!-- Either half can be absent: a bare-scientific model row
                           has no common name, and a non-binomial sound class has
                           no scientific name. Render only what is actually there
                           rather than an empty second line. -->
                      {#if pred.commonName}
                        <div class="font-medium">{pred.commonName}</div>
                        {#if pred.scientificName}
                          <div class="font-mono text-xs italic text-base-content/60">
                            {pred.scientificName}
                          </div>
                        {/if}
                      {:else}
                        <div class="font-mono">{pred.scientificName}</div>
                      {/if}
                    </td>
                    {#each result.modelsRun as m (m.id)}
                      {@const conf = pred.byModel[m.id]}
                      <td class="text-right tabular-nums whitespace-nowrap">
                        {#if conf !== undefined}
                          {formatConfidencePercent(conf)}
                        {:else}
                          <span class="text-base-content/30">—</span>
                        {/if}
                      </td>
                    {/each}
                    <td class="text-right">
                      <!-- Only rows the server marked correctable can be applied:
                           a sound class is not a species, and a row with no
                           scientific name has nothing to key a correction on.
                           Omit the button rather than offering one that can only
                           fail. -->
                      {#if pred.correctable && pred.scientificName}
                        <button
                          type="button"
                          class="btn btn-xs btn-ghost"
                          onclick={() => startCorrection(pred)}
                          disabled={isCorrecting}
                          aria-label={`Use ${pred.commonName || pred.scientificName} as the species for this detection`}
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

    <!-- Verdict shortcuts. Shown only while no correction row is selected: once
         the user has picked a species the pending correction is the action on
         screen, and offering three competing verdicts beside it invites a
         mis-click. Mirrors the Review tab's actions. -->
    {#if result && !isRunning && !pendingCorrection}
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
              Cancel
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
          <button
            type="button"
            class="btn btn-xs btn-ghost"
            onclick={() => applyVerdict('correct')}
            disabled={isVerdictPending}
            aria-label="Mark this detection as confirmed"
          >
            <Check class="h-3.5 w-3.5" />
            Confirmed
          </button>
          <button
            type="button"
            class="btn btn-xs btn-ghost"
            onclick={() => applyVerdict('false_positive')}
            disabled={isVerdictPending}
            aria-label="Mark this detection as a false positive"
          >
            <ThumbsDown class="h-3.5 w-3.5" />
            False positive
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
              Delete
            </button>
          {/if}
        </div>
      {/if}
    {/if}

    <!-- Inline confirmation. Appears below the table once a row is chosen; the
         second click is what makes an accidental correction hard. -->
    {#if pendingCorrection}
      <div class="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
        <div class="mb-2 font-medium">
          Change this detection to {pendingCorrection.commonName ||
            pendingCorrection.scientificName}?
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
            Cancel
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
  </div>
</Modal>

<style>
  /* Model header cells wrap instead of overflowing into the next column. The
     min-width stops a long single word from squeezing the column to nothing and
     the max-width stops one long name from starving the species column. */
  .model-col {
    white-space: normal;
    overflow-wrap: anywhere;
    min-width: 5.5rem;
    max-width: 9rem;
  }
</style>
