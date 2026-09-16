<!-- One cell of the species workspace, rendered from the column registry. -->
<script lang="ts">
  import { Lock, Play } from '@lucide/svelte';
  import SpeciesThumbnail from '$lib/desktop/components/modals/SpeciesThumbnail.svelte';
  import ConfidenceCircle from '$lib/desktop/components/data/ConfidenceCircle.svelte';
  import { t } from '$lib/i18n';
  import type { ColumnId } from '../columns';
  import { formatCount, formatDateTime, formatPercent } from '../format';
  import type { WorkspaceData } from '../workspaceData.svelte';
  import SpeciesActionsMenu from './SpeciesActionsMenu.svelte';
  import { verificationRatio, type SpeciesRow } from './rows';

  interface Props {
    column: ColumnId;
    row: SpeciesRow;
    data: WorkspaceData;
    onOpenSpecies: (_scientificName: string) => void;
    onPlayBest: (_row: SpeciesRow) => void;
    onDelete: (_row: SpeciesRow) => void;
  }

  let { column, row, data, onOpenSpecies, onPlayBest, onDelete }: Props = $props();

  const stat = $derived(data.stats.get(row.scientificName));
  const recording = $derived(data.best.get(row.scientificName));

  function openSpecies(event: MouseEvent) {
    // Keep browser behaviour for new-tab / new-window clicks.
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    onOpenSpecies(row.scientificName);
  }
</script>

{#snippet skeleton()}
  <span
    class="inline-block h-4 w-12 animate-pulse rounded bg-[var(--color-base-300)]"
    aria-hidden="true"
  ></span>
  <span class="sr-only">{t('speciesWorkspace.states.loading')}</span>
{/snippet}

{#snippet membership(kind: 'confirmed' | 'included' | 'excluded')}
  {@const member = data.isMember(kind, row.scientificName)}
  {#if member === null}
    {#if data.status.memberships === 'error'}
      <span aria-label={t('speciesWorkspace.states.notAvailable')}>—</span>
    {:else}
      {@render skeleton()}
    {/if}
  {:else}
    <span
      class="badge badge-sm {member ? 'badge-primary' : 'badge-ghost'}"
      aria-label={t('speciesWorkspace.membership.state', {
        list: t(`speciesWorkspace.membership.${kind}`),
        species: row.displayName,
        state: member ? t('speciesWorkspace.membership.yes') : t('speciesWorkspace.membership.no'),
      })}
    >
      {member ? t('speciesWorkspace.membership.yes') : t('speciesWorkspace.membership.no')}
    </span>
  {/if}
{/snippet}

{#if column === 'species'}
  <a
    href={`/ui/analytics/species-workspace?species=${encodeURIComponent(row.scientificName)}`}
    class="flex min-w-0 items-center gap-3 hover:text-[var(--color-primary)]"
    onclick={openSpecies}
  >
    <SpeciesThumbnail
      scientificName={row.scientificName}
      commonName={row.displayName}
      size="sm"
      className="w-12! h-9! shadow-none"
    />
    <span class="min-w-0">
      <span class="block truncate font-medium">{row.displayName}</span>
      <span class="block truncate text-xs italic opacity-60">{row.scientificName}</span>
    </span>
  </a>
{:else if column === 'count'}
  <span class="tabular-nums">{formatCount(row.total)}</span>
{:else if column === 'firstSeen'}
  <span class="whitespace-nowrap">{formatDateTime(row.firstSeen)}</span>
{:else if column === 'lastSeen'}
  <span class="whitespace-nowrap">{formatDateTime(row.lastSeen)}</span>
{:else if column === 'maxConfidence'}
  {#if data.status.stats !== 'ready'}
    {#if data.status.stats === 'error'}—{:else}{@render skeleton()}{/if}
  {:else if stat?.maxConfidence != null}
    <ConfidenceCircle confidence={stat.maxConfidence} size="sm" />
  {:else}
    <span title={t('speciesWorkspace.best.unavailableHint')}>—</span>
  {/if}
{:else if column === 'verification'}
  {#if data.status.stats !== 'ready'}
    {#if data.status.stats === 'error'}—{:else}{@render skeleton()}{/if}
  {:else}
    {@const ratio = verificationRatio(data, row.scientificName)}
    <span
      class="tabular-nums"
      title={t('speciesWorkspace.verification', {
        correct: stat?.correct ?? 0,
        falsePositive: stat?.falsePositive ?? 0,
      })}
    >
      {ratio === null ? '—' : formatPercent(ratio)}
    </span>
  {/if}
{:else if column === 'range'}
  {#if data.status.range !== 'ready'}
    {#if data.status.range === 'error'}—{:else}{@render skeleton()}{/if}
  {:else}
    {@const score = data.ranges.get(row.scientificName)}
    <span class="tabular-nums" title={t('speciesWorkspace.columnHelp.range')}>
      {score === undefined ? t('speciesWorkspace.states.notAvailable') : formatPercent(score, 1)}
    </span>
  {/if}
{:else if column === 'excluded' || column === 'included' || column === 'confirmed'}
  {@render membership(column)}
{:else if column === 'bestRecording'}
  {#if recording === undefined}
    {#if data.bestFailed.has(row.scientificName)}—{:else}{@render skeleton()}{/if}
  {:else if recording === null}
    <span class="text-xs opacity-60">{t('speciesWorkspace.best.unavailable')}</span>
  {:else}
    <button
      type="button"
      class="btn btn-ghost btn-xs gap-1"
      onclick={() => onPlayBest(row)}
      aria-label={t('speciesWorkspace.best.play', { species: row.displayName })}
    >
      <Play class="size-3.5" />
      <span class="tabular-nums">{formatPercent(recording.confidence)}</span>
      {#if recording.locked}
        <Lock class="size-3" aria-label={t('speciesWorkspace.best.locked')} />
      {/if}
    </button>
  {/if}
{:else if column === 'actions'}
  <SpeciesActionsMenu {row} {data} {onDelete} />
{/if}
