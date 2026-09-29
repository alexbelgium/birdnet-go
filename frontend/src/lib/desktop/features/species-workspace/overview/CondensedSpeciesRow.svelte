<!-- Two-line phone row for a species workspace entry. -->
<script lang="ts">
  import {
    BadgeCheck,
    CalendarClock,
    CalendarPlus,
    EyeOff,
    ListPlus,
    MapPin,
    Play,
    ShieldCheck,
    VolumeX,
  } from '@lucide/svelte';
  import SpeciesThumbnail from '$lib/desktop/components/modals/SpeciesThumbnail.svelte';
  import { t } from '$lib/i18n';
  import type { ColumnDef } from '../columns';
  import { formatCompactCount, formatCount, formatPercent, formatShortDate } from '../format';
  import type { MembershipKind } from '../types';
  import type { WorkspaceData } from '../workspaceData.svelte';
  import SpeciesActionsMenu from './SpeciesActionsMenu.svelte';
  import { verificationRatio, type SpeciesRow } from './rows';

  interface Props {
    row: SpeciesRow;
    columns: ColumnDef[];
    data: WorkspaceData;
    onOpenSpecies: (_scientificName: string) => void;
    onPlayBest: (_row: SpeciesRow) => void;
    onDelete: (_row: SpeciesRow) => void;
  }

  let { row, columns, data, onOpenSpecies, onPlayBest, onDelete }: Props = $props();

  const stat = $derived(data.stats.get(row.scientificName));
  const recording = $derived(data.best.get(row.scientificName));
  const visible = $derived(new Set(columns.map(column => column.id)));
  const secondaryColumns = $derived(
    columns.filter(
      column =>
        !['species', 'count', 'maxConfidence', 'bestRecording', 'actions'].includes(column.id)
    )
  );
  const MEMBERSHIP_COLUMNS = new Set(['excluded', 'included', 'confirmed']);
  const valueColumns = $derived(secondaryColumns.filter(c => !MEMBERSHIP_COLUMNS.has(c.id)));
  const membershipColumns = $derived(secondaryColumns.filter(c => MEMBERSHIP_COLUMNS.has(c.id)));
  const rangeDisplay = $derived.by(() => {
    if (data.status.range === 'loading' || data.status.range === 'idle') {
      return t('speciesWorkspace.states.loading');
    }
    if (data.status.range === 'error') return t('speciesWorkspace.states.notAvailable');
    const score = data.ranges.get(row.scientificName);
    return score === undefined
      ? t('speciesWorkspace.states.notAvailable')
      : formatPercent(score, 1);
  });
  const verificationDisplay = $derived.by(() => {
    if (data.status.stats === 'loading' || data.status.stats === 'idle') {
      return t('speciesWorkspace.states.loading');
    }
    if (data.status.stats === 'error') return t('speciesWorkspace.states.notAvailable');
    const ratio = verificationRatio(data, row.scientificName);
    return ratio === null ? t('speciesWorkspace.states.notAvailable') : formatPercent(ratio);
  });

  function openSpecies(event: MouseEvent) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    onOpenSpecies(row.scientificName);
  }

  function valueLabel(column: ColumnDef, value: string): string {
    return `${t(column.labelKey)}: ${value}`;
  }

  function membershipLabel(column: ColumnDef, kind: MembershipKind, member: boolean): string {
    return t('speciesWorkspace.membership.state', {
      list: t(column.labelKey),
      species: row.displayName,
      state: member ? t('speciesWorkspace.membership.yes') : t('speciesWorkspace.membership.no'),
    });
  }
</script>

{#snippet skeleton(width = 'w-8')}
  <span
    class="inline-block h-3.5 {width} animate-pulse rounded bg-[var(--color-base-300)]"
    aria-hidden="true"
  ></span>
  <span class="sr-only">{t('speciesWorkspace.states.loading')}</span>
{/snippet}

{#snippet membershipIcon(column: ColumnDef, kind: MembershipKind)}
  {@const member = data.isMember(kind, row.scientificName)}
  {#if member === null}
    {@const state =
      data.status.memberships === 'error'
        ? t('speciesWorkspace.states.notAvailable')
        : t('speciesWorkspace.states.loading')}
    {@const label = t('speciesWorkspace.membership.state', {
      list: t(column.labelKey),
      species: row.displayName,
      state,
    })}
    <span
      class="inline-flex size-5 items-center justify-center rounded text-[var(--color-base-content)] opacity-30"
      data-column={column.id}
      data-membership-state="loading"
      title={label}
      aria-label={label}
    >
      {@render skeleton('w-4')}
    </span>
  {:else}
    {@const label = membershipLabel(column, kind, member)}
    <span
      class="inline-flex size-5 items-center justify-center {member
        ? 'text-[var(--color-primary)] opacity-100'
        : 'text-[var(--color-base-content)] opacity-30'}"
      data-column={column.id}
      data-membership-state={member ? 'active' : 'inactive'}
      title={label}
      aria-label={label}
    >
      {#if kind === 'excluded'}
        <EyeOff class="size-4" aria-hidden="true" />
      {:else if kind === 'included'}
        <ListPlus class="size-4" aria-hidden="true" />
      {:else}
        <BadgeCheck class="size-4" aria-hidden="true" />
      {/if}
    </span>
  {/if}
{/snippet}

<div class="min-w-0 py-1.5" data-testid="condensed-species-row">
  <div class="flex min-w-0 items-center gap-1" data-testid="condensed-primary">
    <a
      href={`/ui/analytics/species-workspace?species=${encodeURIComponent(row.scientificName)}`}
      class="flex min-w-0 grow items-center gap-2 hover:text-[var(--color-primary)]"
      onclick={openSpecies}
      title={row.displayName}
    >
      <SpeciesThumbnail
        scientificName={row.scientificName}
        commonName={row.displayName}
        size="sm"
        className="w-9! h-7! rounded-md! shadow-none"
      />
      <span class="min-w-0 truncate text-sm font-medium">{row.displayName}</span>
    </a>

    <span class="flex shrink-0 items-center gap-1">
      {#if visible.has('count')}
        {@const countColumn = columns.find(column => column.id === 'count')}
        {#if countColumn}
          {@const fullCount = formatCount(row.total)}
          <span
            class="min-w-8 text-right text-xs font-semibold tabular-nums"
            title={valueLabel(countColumn, fullCount)}
            aria-label={valueLabel(countColumn, fullCount)}
          >
            {formatCompactCount(row.total)}
          </span>
        {/if}
      {/if}

      {#if visible.has('maxConfidence')}
        {@const confidenceColumn = columns.find(column => column.id === 'maxConfidence')}
        <span class="inline-flex min-w-8 justify-center text-xs tabular-nums">
          {#if data.status.stats !== 'ready'}
            {#if data.status.stats === 'error'}
              <span aria-label={t('speciesWorkspace.states.notAvailable')}>—</span>
            {:else}
              {@render skeleton()}
            {/if}
          {:else if stat?.maxConfidence != null && confidenceColumn}
            {@const confidence = formatPercent(stat.maxConfidence)}
            <span
              title={valueLabel(confidenceColumn, confidence)}
              aria-label={valueLabel(confidenceColumn, confidence)}>{confidence}</span
            >
          {:else}
            <span title={t('speciesWorkspace.best.unavailableHint')}>—</span>
          {/if}
        </span>
      {/if}

      {#if visible.has('bestRecording')}
        {#if recording === undefined}
          {#if data.bestFailed.has(row.scientificName)}
            <span aria-label={t('speciesWorkspace.states.notAvailable')}>—</span>
          {:else}
            {@render skeleton('w-5')}
          {/if}
        {:else if recording === null}
          <span
            class="inline-flex size-7 items-center justify-center opacity-30"
            title={t('speciesWorkspace.condensed.noRecording')}
            aria-label={t('speciesWorkspace.condensed.noRecording')}
          >
            <VolumeX class="size-4" aria-hidden="true" />
          </span>
        {:else}
          <button
            type="button"
            class="inline-flex size-7 items-center justify-center rounded-md hover:bg-[var(--color-base-200)] focus-visible:outline-2 focus-visible:outline-[var(--color-primary)]"
            onclick={() => onPlayBest(row)}
            aria-label={t('speciesWorkspace.best.play', { species: row.displayName })}
            title={t('speciesWorkspace.best.play', { species: row.displayName })}
          >
            <Play class="size-4" aria-hidden="true" />
          </button>
        {/if}
      {/if}

      {#if visible.has('actions')}
        <SpeciesActionsMenu {row} {data} {onDelete} />
      {/if}
    </span>
  </div>

  <!-- Values shrink and clip first; list-membership icons always stay visible. -->
  <div
    class="mt-1 flex min-h-5 min-w-0 items-center gap-2 pl-1 text-xs min-[400px]:pl-11"
    data-testid="condensed-secondary"
  >
    <div class="flex min-w-0 items-center gap-2 overflow-hidden">
      {#each valueColumns as column (column.id)}
        {#if column.id === 'firstSeen' || column.id === 'lastSeen'}
          {@const date = formatShortDate(column.id === 'firstSeen' ? row.firstSeen : row.lastSeen)}
          {@const display = date || t('speciesWorkspace.states.notAvailable')}
          <span
            class="inline-flex shrink-0 items-center gap-1 whitespace-nowrap"
            data-column={column.id}
            title={valueLabel(column, display)}
            aria-label={valueLabel(column, display)}
          >
            {#if column.id === 'firstSeen'}
              <CalendarPlus class="size-3.5 opacity-60" aria-hidden="true" />
            {:else}
              <CalendarClock class="size-3.5 opacity-60" aria-hidden="true" />
            {/if}
            <span>{display}</span>
          </span>
        {:else if column.id === 'range'}
          <span
            class="inline-flex shrink-0 items-center gap-1"
            data-column="range"
            title={valueLabel(column, rangeDisplay)}
            aria-label={valueLabel(column, rangeDisplay)}
          >
            <MapPin class="size-3.5 opacity-60" aria-hidden="true" />
            {#if data.status.range !== 'ready'}
              {#if data.status.range === 'error'}
                <span aria-label={valueLabel(column, t('speciesWorkspace.states.notAvailable'))}
                  >—</span
                >
              {:else}
                {@render skeleton()}
              {/if}
            {:else}
              <span>{rangeDisplay}</span>
            {/if}
          </span>
        {:else if column.id === 'verification'}
          <span
            class="inline-flex shrink-0 items-center gap-1"
            data-column="verification"
            title={valueLabel(column, verificationDisplay)}
            aria-label={valueLabel(column, verificationDisplay)}
          >
            <ShieldCheck class="size-3.5 opacity-60" aria-hidden="true" />
            {#if data.status.stats !== 'ready'}
              {#if data.status.stats === 'error'}
                <span aria-label={valueLabel(column, t('speciesWorkspace.states.notAvailable'))}
                  >—</span
                >
              {:else}
                {@render skeleton()}
              {/if}
            {:else}
              <span>{verificationDisplay}</span>
            {/if}
          </span>
        {/if}
      {/each}
    </div>
    <div class="ml-auto flex shrink-0 items-center gap-1">
      {#each membershipColumns as column (column.id)}
        {#if column.id === 'excluded'}
          {@render membershipIcon(column, 'excluded')}
        {:else if column.id === 'included'}
          {@render membershipIcon(column, 'included')}
        {:else if column.id === 'confirmed'}
          {@render membershipIcon(column, 'confirmed')}
        {/if}
      {/each}
    </div>
  </div>
</div>
