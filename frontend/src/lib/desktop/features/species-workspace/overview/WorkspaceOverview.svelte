<!-- Page 1: every species with search, taxon filter, editable columns and actions. -->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { Check, Pencil, RefreshCw, RotateCcw, Search } from '@lucide/svelte';
  import TaxonFilterDropdown from '$lib/desktop/features/dashboard/components/daily-summary/TaxonFilterDropdown.svelte';
  import {
    classifyTaxon,
    type TaxonCounts,
    type TaxonFilter,
  } from '$lib/desktop/features/dashboard/utils/taxonFilter';
  import { getLocale, t } from '$lib/i18n';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import {
    neededGroups,
    visibleColumns,
    getColumn,
    type ColumnId,
    type DataGroup,
  } from '../columns';
  import { createLayoutStore } from '../layout.svelte';
  import { matchesSearch } from '../search';
  import { sortRows } from '../sort';
  import { createWorkspaceData } from '../workspaceData.svelte';
  import BestRecordingModal from './BestRecordingModal.svelte';
  import ColumnEditor from './ColumnEditor.svelte';
  import DeleteSpeciesModal from './DeleteSpeciesModal.svelte';
  import SpeciesCell from './SpeciesCell.svelte';
  import { columnSortValue, type SpeciesRow } from './rows';

  interface Props {
    onOpenSpecies: (_scientificName: string) => void;
  }

  let { onOpenSpecies }: Props = $props();

  const BEST_DEBOUNCE_MS = 150;

  const data = createWorkspaceData();
  const layoutStore = createLayoutStore();

  let query = $state('');
  let taxon = $state<TaxonFilter>('all');
  let editing = $state(false);
  let bestTarget = $state<SpeciesRow | null>(null);
  let deleteTarget = $state<SpeciesRow | null>(null);

  const layout = $derived(layoutStore.layout);
  const columns = $derived(visibleColumns(layout));
  const groups = $derived(neededGroups(layout));

  // Hidden columns never request their data: only the visible columns' groups load.
  $effect(() => {
    data.ensure(new Set<DataGroup>(['inventory', ...groups]));
  });

  const rows = $derived<SpeciesRow[]>(
    data.species.map(s => ({
      ...s,
      displayName: localizeSpeciesName(s.scientificName, s.commonName),
    }))
  );

  const counts = $derived.by<TaxonCounts>(() => {
    const out: TaxonCounts = { all: rows.length, bird: 0, bat: 0, other: 0 };
    for (const row of rows) out[classifyTaxon({ scientific_name: row.scientificName })]++;
    return out;
  });

  const visibleRows = $derived.by(() => {
    const filtered = rows.filter(
      row =>
        (taxon === 'all' || classifyTaxon({ scientific_name: row.scientificName }) === taxon) &&
        matchesSearch(row, query)
    );
    const sortColumn = (getColumn(layout.sort.column)?.id ?? 'count') as ColumnId;
    return sortRows(
      filtered,
      row => columnSortValue(sortColumn, row, data),
      layout.sort.direction,
      getLocale()
    );
  });

  const groupErrors = $derived(
    (['inventory', 'stats', 'memberships', 'range', 'best'] as DataGroup[]).filter(
      g => data.status[g] === 'error' && (g === 'inventory' || groups.has(g))
    )
  );

  // T2: best recordings for rows that scroll into view, batched.
  const pendingVisible = new Set<string>();
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  // eslint-disable-next-line no-undef -- browser global
  let observer: IntersectionObserver | undefined;

  function flushVisible() {
    debounceTimer = undefined;
    if (groups.has('best') && pendingVisible.size > 0) data.requestBest([...pendingVisible]);
    pendingVisible.clear();
  }

  function observeRow(node: HTMLElement, name: string) {
    node.dataset.species = name;
    observer?.observe(node);
    return {
      update(next: string) {
        node.dataset.species = next;
      },
      destroy() {
        observer?.unobserve(node);
      },
    };
  }

  onMount(() => {
    void layoutStore.load();
    // eslint-disable-next-line no-undef -- browser global
    if (typeof IntersectionObserver === 'undefined') return;
    // eslint-disable-next-line no-undef -- browser global
    observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const name = (entry.target as HTMLElement).dataset.species;
        if (entry.isIntersecting && name) pendingVisible.add(name);
      }
      if (pendingVisible.size > 0 && debounceTimer === undefined) {
        debounceTimer = setTimeout(flushVisible, BEST_DEBOUNCE_MS);
      }
    });
    for (const node of document.querySelectorAll<HTMLElement>('[data-species]')) {
      observer.observe(node);
    }
  });

  // Showing the best-recording column after rows are on screen: re-scan visible rows.
  $effect(() => {
    if (!groups.has('best') || !observer) return;
    for (const node of document.querySelectorAll<HTMLElement>('[data-species]')) {
      observer.unobserve(node);
      observer.observe(node);
    }
  });

  onDestroy(() => {
    observer?.disconnect();
    clearTimeout(debounceTimer);
    data.dispose();
  });

  function toggleSort(column: ColumnId) {
    const direction =
      layout.sort.column === column
        ? layout.sort.direction === 'asc'
          ? 'desc'
          : 'asc'
        : column === 'species'
          ? 'asc'
          : 'desc';
    void layoutStore.setSort(column, direction);
  }

  function refresh() {
    data.invalidate();
    data.ensure(new Set<DataGroup>(['inventory', ...groups]));
    observer?.disconnect();
    for (const node of document.querySelectorAll<HTMLElement>('[data-species]')) {
      observer?.observe(node);
    }
  }

  function ariaSort(column: ColumnId): 'ascending' | 'descending' | 'none' {
    if (layout.sort.column !== column) return 'none';
    return layout.sort.direction === 'asc' ? 'ascending' : 'descending';
  }
</script>

{#snippet toolbarButtons()}
  <button
    type="button"
    class="btn btn-ghost btn-sm"
    aria-label={t('speciesWorkspace.refresh')}
    title={t('speciesWorkspace.refresh')}
    onclick={refresh}
  >
    <RefreshCw class="size-4" />
  </button>
  {#if editing}
    <button type="button" class="btn btn-ghost btn-sm gap-1" onclick={() => layoutStore.reset()}>
      <RotateCcw class="size-4" />{t('speciesWorkspace.edit.reset')}
    </button>
    <button type="button" class="btn btn-primary btn-sm gap-1" onclick={() => (editing = false)}>
      <Check class="size-4" />{t('speciesWorkspace.edit.done')}
    </button>
  {:else}
    <button
      type="button"
      class="btn btn-sm gap-1"
      aria-label={t('speciesWorkspace.edit.edit')}
      onclick={() => (editing = true)}
    >
      <Pencil class="size-4" /><span class="hidden sm:inline"
        >{t('speciesWorkspace.edit.edit')}</span
      >
    </button>
  {/if}
{/snippet}

{#snippet cell(column: ColumnId, row: SpeciesRow)}
  <SpeciesCell
    {column}
    {row}
    {data}
    {onOpenSpecies}
    onPlayBest={r => (bestTarget = r)}
    onDelete={r => (deleteTarget = r)}
  />
{/snippet}

<div class="card bg-[var(--color-base-100)] shadow-xs">
  <div class="card-body card-padding">
    <h1 class="card-title text-2xl">{t('speciesWorkspace.title')}</h1>
    <p class="opacity-60">{t('speciesWorkspace.subtitle')}</p>
  </div>
</div>

<div class="card bg-[var(--color-base-100)] shadow-xs">
  <div class="card-body card-padding space-y-3">
    <!-- Sticky toolbar -->
    <div
      class="sticky top-0 z-20 -mx-2 flex flex-wrap items-center gap-2 bg-[var(--color-base-100)] px-2 py-2"
    >
      <div class="flex w-full min-w-0 items-center gap-2 md:w-auto md:max-w-sm md:grow">
        <label class="input input-sm flex min-w-0 grow items-center gap-2">
          <Search class="size-4 opacity-60" aria-hidden="true" />
          <span class="sr-only">{t('speciesWorkspace.search.label')}</span>
          <input
            type="search"
            class="min-w-0 grow"
            placeholder={t('speciesWorkspace.search.placeholder')}
            bind:value={query}
          />
        </label>
        <div class="flex items-center gap-1 md:hidden">{@render toolbarButtons()}</div>
      </div>
      <TaxonFilterDropdown value={taxon} {counts} onChange={v => (taxon = v)} />
      <!-- Mobile sort control; the table header sorts on desktop. -->
      <label class="flex items-center gap-1 whitespace-nowrap md:hidden">
        <span class="sr-only text-sm sm:not-sr-only">{t('speciesWorkspace.sort.label')}</span>
        <select
          class="select select-sm w-36"
          value={layout.sort.column}
          onchange={e => layoutStore.setSort(e.currentTarget.value, layout.sort.direction)}
        >
          {#each columns.filter(c => c.sortable) as column (column.id)}
            <option value={column.id}>{t(column.labelKey)}</option>
          {/each}
        </select>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          aria-label={t('speciesWorkspace.sort.toggleDirection')}
          onclick={() =>
            layoutStore.setSort(
              layout.sort.column,
              layout.sort.direction === 'asc' ? 'desc' : 'asc'
            )}
        >
          {layout.sort.direction === 'asc' ? '↑' : '↓'}
        </button>
      </label>
      <span class="hidden grow md:block"></span>
      <div class="hidden items-center gap-2 md:flex">{@render toolbarButtons()}</div>
    </div>

    {#if editing}
      <ColumnEditor {layout} onChange={next => layoutStore.save(next)} />
    {/if}
    {#if layoutStore.saveError}
      <p role="alert" class="text-sm text-[var(--color-error)]">
        {t('speciesWorkspace.edit.saveFailed')}
      </p>
    {/if}

    {#each groupErrors as group (group)}
      <div role="alert" class="alert alert-error alert-soft flex items-center justify-between py-2">
        <span
          >{t('speciesWorkspace.states.loadFailed', {
            what: t(`speciesWorkspace.groups.${group}`),
          })}</span
        >
        <button type="button" class="btn btn-sm" onclick={() => data.retry(group)}>
          {t('speciesWorkspace.states.retry')}
        </button>
      </div>
    {/each}

    {#if data.status.inventory !== 'ready' && rows.length === 0}
      {#if data.status.inventory !== 'error'}
        <div class="space-y-2" aria-busy="true" role="status">
          <span class="sr-only">{t('speciesWorkspace.states.loading')}</span>
          {#each [0, 1, 2, 3, 4, 5] as i (i)}
            <div class="h-10 animate-pulse rounded bg-[var(--color-base-200)]"></div>
          {/each}
        </div>
      {/if}
    {:else if rows.length === 0}
      <p>{t('speciesWorkspace.states.empty')}</p>
    {:else if visibleRows.length === 0}
      <p>{t('speciesWorkspace.states.noMatch')}</p>
    {:else}
      <!-- Desktop table -->
      <div class="hidden overflow-x-auto md:block">
        <table class="table table-sm w-full">
          <thead>
            <tr>
              {#each columns as column (column.id)}
                <th scope="col" aria-sort={column.sortable ? ariaSort(column.id) : undefined}>
                  {#if column.sortable}
                    <button
                      type="button"
                      class="flex items-center gap-1 font-semibold"
                      onclick={() => toggleSort(column.id)}
                      aria-label={t('speciesWorkspace.sort.sortBy', { column: t(column.labelKey) })}
                    >
                      {t(column.labelKey)}
                      {#if layout.sort.column === column.id}
                        <span aria-hidden="true">{layout.sort.direction === 'asc' ? '↑' : '↓'}</span
                        >
                      {/if}
                    </button>
                  {:else}
                    <span class:sr-only={column.id === 'actions'}>{t(column.labelKey)}</span>
                  {/if}
                </th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each visibleRows as row (row.scientificName)}
              <tr class="hover" use:observeRow={row.scientificName}>
                {#each columns as column (column.id)}
                  <td>{@render cell(column.id, row)}</td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <!-- Mobile cards -->
      <ul class="space-y-2 md:hidden">
        {#each visibleRows as row (row.scientificName)}
          <li
            class="rounded-lg border border-[var(--color-base-300)] p-3"
            use:observeRow={row.scientificName}
          >
            <div class="flex items-start justify-between gap-2">
              {@render cell('species', row)}
              {#if columns.some(c => c.id === 'actions')}{@render cell('actions', row)}{/if}
            </div>
            <dl class="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
              {#each columns.filter(c => c.id !== 'species' && c.id !== 'actions') as column (column.id)}
                <dt class="opacity-60">{t(column.labelKey)}</dt>
                <dd class="text-right">{@render cell(column.id, row)}</dd>
              {/each}
            </dl>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>

<!-- Mounted only while open: the shared Modal can only move focus into a
     dialog that is already visible when it opens. -->
{#if bestTarget}
  <BestRecordingModal
    speciesName={bestTarget.displayName}
    recording={data.best.get(bestTarget.scientificName)}
    onClose={() => (bestTarget = null)}
  />
{/if}

{#if deleteTarget}
  <DeleteSpeciesModal
    target={deleteTarget}
    onClose={() => (deleteTarget = null)}
    onChanged={refresh}
  />
{/if}
