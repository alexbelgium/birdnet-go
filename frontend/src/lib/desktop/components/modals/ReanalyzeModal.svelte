<script lang="ts">
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
  import {
    Sparkles,
    AlertCircle,
    CircleCheck,
    CircleX,
    Trash2,
    Lock,
  } from '@lucide/svelte';
  import type { Detection } from '$lib/types/detection.types';

  interface Props {
    isOpen: boolean;
    detection: Detection | null;
    onClose: () => void;
    onCorrected?: () => void;
    onDeleted?: () => void;
  }

  let { isOpen = false, detection = null, onClose, onCorrected, onDeleted }: Props = $props();

  const detectionId = $derived(detection?.id ?? null);
  const isLocked = $derived(detection?.locked === true);
  const currentSpeciesKey = $derived(
    detection?.scientificName ? normalizeForLookup(detection.scientificName) : ''
  );
  const logger = loggers.ui;

  let isRunning = $state(false);
  let result = $state<ReanalyzeResult | null>(null);
  let errorMessage = $state<string | null>(null);
  let requestSeq = 0;

  let pendingCorrection = $state<ReanalyzePrediction | null>(null);
  let isCorrecting = $state(false);
  let isVerdictPending = $state(false);
  let confirmingDelete = $state(false);
  let showAllRows = $state(false);

  $effect(() => {
    if (!isOpen || detectionId === null) return;
    untrack(() => {
      result = null;
      errorMessage = null;
      pendingCorrection = null;
      isCorrecting = false;
      isVerdictPending = false;
      confirmingDelete = false;
      showAllRows = false;
      runReanalysis();
    });
  });

  // If another view locks the detection while this modal is open, withdraw any
  // armed write action immediately. The read-only reanalysis remains visible.
  $effect(() => {
    if (isLocked) {
      pendingCorrection = null;
      confirmingDelete = false;
    }
  });

  async function runReanalysis() {
    if (!detectionId) return;
    const mySeq = ++requestSeq;
    isRunning = true;
    errorMessage = null;
    try {
      const res = await reanalyzeDetection(detectionId);
      if (mySeq !== requestSeq) return;
      result = res;
    } catch (err) {
      if (mySeq !== requestSeq) return;
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      if (mySeq === requestSeq) isRunning = false;
    }
  }

  function startCorrection(pred: ReanalyzePrediction) {
    confirmingDelete = false;
    pendingCorrection = pred;
  }

  function cancelCorrection() {
    pendingCorrection = null;
  }

  function startDeleteConfirmation() {
    pendingCorrection = null;
    confirmingDelete = true;
  }

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
      if (mySeq !== requestSeq) return;
      if (applied === null) {
        onClose();
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

  async function applyVerdict(verified: 'correct' | 'false_positive') {
    if (!detectionId || isVerdictPending) return;
    const target = detectionId;
    const mySeq = ++requestSeq;
    isVerdictPending = true;
    try {
      if (await setDetectionVerification(target, verified)) {
        if (mySeq !== requestSeq) return;
        onClose();
        onCorrected?.();
      }
    } finally {
      if (mySeq === requestSeq) isVerdictPending = false;
    }
  }

  async function confirmDelete() {
    if (!detectionId || isVerdictPending) return;
    const mySeq = ++requestSeq;
    const target = detectionId;
    isVerdictPending = true;
    try {
      await fetchWithCSRF(`/api/v2/detections/${target}`, { method: 'DELETE' });
      toastActions.success('Detection deleted.');
      if (mySeq !== requestSeq) return;
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

  interface DecoratedCell {
    modelId: string;
    confidence: number | undefined;
    colorClass: string;
    isBest: boolean;
    delta: string | null;
  }

  interface DecoratedRow {
    pred: ReanalyzePrediction;
    key: string;
    displayName: string;
    isCurrent: boolean;
    cells: DecoratedCell[];
  }

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
        // The API should already return the configured species-language name.
        // Keep the shared display helper as the client-side fallback path.
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

  const visibleRows = $derived(showAllRows ? decoratedRows : decoratedRows.slice(0, 3));

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

  let summaryLine = $derived.by(() => {
    if (!result) return '';
    const modelNames = result.modelsRun.map(m => m.name).join(' + ');
    return `${modelNames} over ${formatDuration(result.clipDurationSec)} of audio`;
  });
</script>

<Modal
  {isOpen}
  title="Reanalyze this clip"
  size="3xl"
  className="pt-14 sm:pt-6"
  {onClose}
>
  <!-- Keep the three main decisions in the modal chrome, level with the close X.
       They remain present while loading and while a confirmation panel is open. -->
  {#if !isLocked}
    <div class="absolute right-12 top-2 z-10 flex h-8 items-center gap-0.5">
      <button
        type="button"
        class="inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs transition-colors hover:bg-[var(--color-base-300)] disabled:opacity-50"
        class:bg-success/10={detection?.verified === 'correct'}
        onclick={() => applyVerdict('correct')}
        disabled={isVerdictPending}
        aria-pressed={detection?.verified === 'correct'}
      >
        <CircleCheck class="size-4 text-[var(--color-success)]" />
        <span>{t('dashboard.recentDetections.actions.markCorrect')}</span>
      </button>
      <button
        type="button"
        class="inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs transition-colors hover:bg-[var(--color-base-300)] disabled:opacity-50"
        class:bg-error/10={detection?.verified === 'false_positive'}
        onclick={() => applyVerdict('false_positive')}
        disabled={isVerdictPending}
        aria-pressed={detection?.verified === 'false_positive'}
      >
        <CircleX class="size-4 text-[var(--color-error)]" />
        <span>{t('dashboard.recentDetections.actions.markFalsePositive')}</span>
      </button>
      {#if onDeleted}
        <button
          type="button"
          class="inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs text-[var(--color-error)] transition-colors hover:bg-[var(--color-error)]/10 disabled:opacity-50"
          onclick={startDeleteConfirmation}
          disabled={isVerdictPending}
          aria-label="Delete this detection"
        >
          <Trash2 class="size-4" />
          <span>Delete</span>
        </button>
      {/if}
    </div>
  {/if}

  <div class="space-y-4">
    <p class="text-sm text-base-content/70">
      Runs every loaded classifier over the saved audio and shows what each one thinks. Nothing is
      saved unless you apply a correction below.
    </p>

    {#if isLocked}
      <div class="border-t border-base-300 pt-3 text-sm text-base-content/70">
        {t('common.review.form.detectionLocked')}
      </div>
    {/if}

    <!-- Confirmation-only actions stay in the body, directly above the results table. -->
    {#if confirmingDelete && !isLocked}
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
    {/if}

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
            <table class="table table-xs w-full text-xs">
              <thead>
                <tr>
                  <th class="text-left text-xs">Species</th>
                  {#each result.modelsRun as m (m.id)}
                    <th class="text-right align-bottom text-xs">
                      <div>{m.name}</div>
                    </th>
                  {/each}
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each visibleRows as row (row.key)}
                  <tr class:current-row={row.isCurrent}>
                    <td class="text-xs">
                      <div
                        class={row.displayName !== row.pred.scientificName
                          ? 'font-medium'
                          : 'font-mono'}
                      >
                        {row.displayName}
                        {#if row.isCurrent}
                          <span class="badge badge-xs badge-status-info">current</span>
                        {/if}
                      </div>
                      {#if row.pred.scientificName && row.displayName !== row.pred.scientificName}
                        <div class="font-mono text-[0.6875rem] italic text-base-content/60">
                          {row.pred.scientificName}
                        </div>
                      {/if}
                    </td>
                    {#each row.cells as cell (cell.modelId)}
                      <td class="text-right text-xs tabular-nums whitespace-nowrap">
                        {#if cell.confidence !== undefined}
                          <span class={cell.colorClass} class:font-bold={cell.isBest}>
                            {formatConfidencePercent(cell.confidence)}
                          </span>
                          {#if cell.delta}
                            <div class="text-[0.6875rem] text-base-content/50">{cell.delta}</div>
                          {/if}
                        {:else}
                          <span class="text-base-content/30" title="not predicted by this model"
                            >—</span
                          >
                        {/if}
                      </td>
                    {/each}
                    <td class="text-right">
                      {#if row.pred.correctable && row.pred.scientificName && !isLocked}
                        <button
                          type="button"
                          class="btn btn-xs btn-ghost px-2 text-xs"
                          onclick={() => startCorrection(row.pred)}
                          disabled={isCorrecting}
                          aria-label={`Select ${row.displayName} as the species for this detection`}
                        >
                          Select
                        </button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          {#if decoratedRows.length > 3}
            <div class="flex justify-center pt-1">
              <button
                type="button"
                class="text-xs font-medium text-primary hover:underline"
                onclick={() => (showAllRows = !showAllRows)}
                aria-expanded={showAllRows}
              >
                {showAllRows ? 'See less' : 'See more'}
              </button>
            </div>
          {/if}
        {/if}
      </div>
    {/if}
  </div>
</Modal>

<style>
  .current-row {
    background-color: color-mix(in srgb, var(--color-info) 8%, transparent);
  }
</style>
