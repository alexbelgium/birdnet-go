<!--
  Confirms and runs a species delete. Counts come from a fresh inventory row so
  the message matches what the delete removes: every unlocked detection, false
  positives included; locked detections are kept.
-->
<script lang="ts">
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import { t } from '$lib/i18n';
  import { fetchSpecies, fetchStats } from '../api';
  import { deleteAllUnlocked, SpeciesDeleteError, type DeleteProgress } from '../deleteSpecies';
  import { formatCount, formatPercent } from '../format';
  import { isAbortError } from '../requestSlot';
  import type { WorkspaceSpecies } from '../types';

  interface Props {
    target: { scientificName: string; displayName: string } | null;
    onClose: () => void;
    /** Called after any deletion attempt that may have removed detections. */
    onChanged: () => void;
  }

  let { target, onClose, onChanged }: Props = $props();

  type Phase = 'loading' | 'confirm' | 'running' | 'done' | 'error';
  let phase = $state<Phase>('loading');
  let row = $state<WorkspaceSpecies | null>(null);
  let falsePositive = $state<number | null>(null);
  let progress = $state<DeleteProgress | null>(null);
  let errorMessage = $state('');
  let controller: AbortController | null = null;

  const deletable = $derived(row ? Math.max(row.total - row.locked, 0) : 0);
  // The bar's end: the confirmed count, grown if more detections arrived meanwhile.
  const progressMax = $derived(
    progress ? Math.max(deletable, progress.deleted + progress.remaining, 1) : 1
  );

  $effect(() => {
    if (!target) return;
    const name = target.scientificName;
    const loadController = new AbortController();
    phase = 'loading';
    row = null;
    falsePositive = null;
    progress = null;
    errorMessage = '';
    Promise.all([
      fetchSpecies(loadController.signal, name),
      fetchStats(loadController.signal, name),
    ])
      .then(([rows, stats]) => {
        row = rows[0] ?? null;
        falsePositive = stats.find(s => s.scientificName === name)?.falsePositive ?? 0;
        phase = 'confirm';
      })
      .catch(error => {
        if (isAbortError(error)) return;
        errorMessage = t('speciesWorkspace.states.loadFailed', {
          what: t('speciesWorkspace.groups.inventory'),
        });
        phase = 'error';
      });
    return () => loadController.abort();
  });

  async function run() {
    if (!target) return;
    controller = new AbortController();
    phase = 'running';
    // Show the bar at 0% right away instead of waiting for the first chunk.
    progress = { deleted: 0, locked: 0, reassigned: 0, remaining: deletable };
    try {
      progress = await deleteAllUnlocked(target.scientificName, {
        signal: controller.signal,
        onProgress: p => (progress = p),
      });
      phase = 'done';
    } catch (error) {
      if (isAbortError(error)) {
        errorMessage = t('speciesWorkspace.delete.cancelled');
      } else if (error instanceof SpeciesDeleteError) {
        errorMessage = t('speciesWorkspace.delete.stalled');
        progress = error.progress;
      } else {
        errorMessage = t('speciesWorkspace.delete.error');
      }
      phase = 'error';
    } finally {
      controller = null;
      onChanged();
    }
  }

  function close() {
    controller?.abort();
    onClose();
  }
</script>

<Modal
  isOpen={target !== null}
  title={target ? t('speciesWorkspace.delete.title', { species: target.displayName }) : ''}
  size="lg"
  onClose={close}
>
  <div class="space-y-3">
    {#if phase === 'loading'}
      <p role="status">{t('speciesWorkspace.states.loading')}</p>
    {:else if phase === 'confirm' && row}
      <p>
        {t('speciesWorkspace.delete.message', {
          deletable: formatCount(deletable),
          total: formatCount(row.total),
          falsePositive: formatCount(falsePositive ?? 0),
          locked: formatCount(row.locked),
        })}
      </p>
      {#if deletable === 0}
        <p class="text-sm opacity-70">{t('speciesWorkspace.delete.nothing')}</p>
      {/if}
    {:else if phase === 'confirm'}
      <p>{t('speciesWorkspace.states.notFound')}</p>
    {/if}

    {#if progress && (phase === 'running' || phase === 'done' || phase === 'error')}
      <div class="space-y-1">
        <div class="flex items-baseline justify-between gap-2 text-sm">
          <span role="status" aria-live="polite">
            {phase === 'running' ? t('speciesWorkspace.delete.running') : ''}
            {t('speciesWorkspace.delete.progress', {
              deleted: formatCount(progress.deleted),
              locked: formatCount(progress.locked + (row?.locked ?? 0)),
            })}
          </span>
          <span class="font-medium tabular-nums"
            >{formatPercent(Math.min(progress.deleted / progressMax, 1))}</span
          >
        </div>
        <progress
          class="progress progress-error w-full"
          value={progress.deleted}
          max={progressMax}
          aria-label={t('speciesWorkspace.delete.progressLabel')}
        ></progress>
      </div>
    {/if}

    {#if phase === 'done'}
      <p role="status">{t('speciesWorkspace.delete.done')}</p>
    {:else if phase === 'error'}
      <p role="alert" class="text-[var(--color-error)]">{errorMessage}</p>
    {/if}
  </div>

  {#snippet footer()}
    {#if phase === 'running'}
      <button type="button" class="btn btn-ghost" onclick={() => controller?.abort()}>
        {t('speciesWorkspace.delete.stop')}
      </button>
    {:else if phase === 'confirm' && row && deletable > 0}
      <button type="button" class="btn btn-ghost" onclick={close}
        >{t('common.buttons.cancel')}</button
      >
      <button type="button" class="btn btn-error" onclick={run}>
        {t('speciesWorkspace.delete.confirm', { count: formatCount(deletable) })}
      </button>
    {:else if phase === 'error' && target}
      <button type="button" class="btn btn-ghost" onclick={close}
        >{t('common.buttons.close')}</button
      >
      <button type="button" class="btn btn-error" onclick={run}
        >{t('speciesWorkspace.states.retry')}</button
      >
    {:else}
      <button type="button" class="btn" onclick={close}>{t('common.buttons.close')}</button>
    {/if}
  {/snippet}
</Modal>
