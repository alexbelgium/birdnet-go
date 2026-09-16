<script lang="ts">
  import { onMount } from 'svelte';
  import { getLocale, t } from '$lib/i18n';
  import { navigation } from '$lib/stores/navigation.svelte';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import Button from '$lib/desktop/components/ui/Button.svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import { ArrowUp, ArrowDown, Play } from '@lucide/svelte';
  import { createWorkspace } from '../species-tools/workspace.svelte';
  import {
    columns,
    type Column,
    type SpeciesRow,
    type Recording,
    type SortKey,
  } from '../species-tools/types';
  import { columnLabel } from '../species-tools/labels';
  import { normalizeSearch } from '../species-tools/format';
  import { matchesTaxonFilter, type TaxonFilter } from '../species-tools/taxonFilter';
  import SpeciesActions from '../species-tools/SpeciesActions.svelte';
  import SpeciesRecordings from '../species-tools/SpeciesRecordings.svelte';
  import RecordingPopup from '../species-tools/RecordingPopup.svelte';
  import '../species-tools/workspace.css';

  const workspace = createWorkspace();
  let search = $state('');
  let taxon = $state<TaxonFilter>('all');
  let editing = $state(false);
  let draft = $state<Column[]>([]);
  let deleteTarget = $state<SpeciesRow | null>(null);
  let deleting = $state(false);
  let deleteError = $state('');
  let recording = $state<Recording | null>(null);
  let recordingTitle = $state('');
  const selectedName = $derived(new URLSearchParams(navigation.currentSearch).get('species') ?? '');
  const selected = $derived(workspace.rows.find(row => row.scientific_name === selectedName));
  const visibleRows = $derived(
    workspace.orderedRows.filter(
      row =>
        matchesTaxonFilter(row, taxon) &&
        normalizeSearch(
          [
            localizeSpeciesName(row.scientific_name, row.common_name),
            row.common_name,
            row.scientific_name,
          ].join(' ')
        ).includes(normalizeSearch(search))
    )
  );
  const taxons: TaxonFilter[] = ['all', 'bird', 'bat', 'other'];
  function taxonLabel(value: TaxonFilter) {
    switch (value) {
      case 'all':
        return t('analytics.speciesTools.all');
      case 'bird':
        return t('analytics.speciesTools.birds');
      case 'bat':
        return t('analytics.speciesTools.bats');
      case 'other':
        return t('analytics.speciesTools.others');
    }
  }
  onMount(() => {
    void workspace.load();
    return workspace.dispose;
  });
  $effect(() => {
    workspace.selectSpecies(selectedName);
  });
  function speciesUrl(row: SpeciesRow) {
    const query = new URLSearchParams({
      species: row.scientific_name,
      queryType: 'species',
      sortBy: 'confidence_desc',
    });
    return `/ui/analytics/species-list?${query}`;
  }
  function display(row: SpeciesRow, column: Column): string {
    if (
      column === 'best' &&
      workspace.recordings.has(row.scientific_name) &&
      !workspace.recordings.get(row.scientific_name)
    )
      return t('analytics.speciesTools.audioUnavailable');
    const value = workspace.value(row, column);
    if (value === null) return workspace.pending.has(column) ? '…' : '—';
    if (['max_confidence', 'correct', 'range', 'best'].includes(column))
      return `${(Number(value) * 100).toFixed(1)}%`;
    if (['confirmed', 'included', 'excluded'].includes(column))
      return value ? t('common.yes') : t('common.no');
    if (column === 'last_heard')
      return new Date(String(value).replace(' ', 'T')).toLocaleDateString(getLocale());
    return typeof value === 'number' ? value.toLocaleString(getLocale()) : value;
  }
  function editColumn(column: Column, enabled: boolean) {
    draft = enabled ? [...draft, column] : draft.filter(value => value !== column);
  }
  function moveColumn(column: Column, delta: number) {
    const next = [...draft];
    const i = next.indexOf(column);
    const j = i + delta;
    if (i < 0 || j < 0 || j >= next.length) return;
    // eslint-disable-next-line security/detect-object-injection -- Both indices are bounds-checked above.
    [next[i], next[j]] = [next[j], next[i]];
    draft = next;
  }
  function showRecording(row: SpeciesRow) {
    recordingTitle = localizeSpeciesName(row.scientific_name, row.common_name);
    recording = workspace.recordings.get(row.scientific_name) ?? null;
  }
  async function requestDelete(row: SpeciesRow) {
    deleteError = '';
    try {
      deleteTarget = await workspace.deletionTarget(row);
    } catch {
      deleteError = t('analytics.speciesTools.manage.loadFailed');
    }
  }
  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    deleteError = '';
    try {
      await workspace.remove(deleteTarget);
      deleteTarget = null;
    } catch {
      deleteError = t('analytics.speciesTools.manage.deleteFailed');
    } finally {
      deleting = false;
    }
  }
</script>

<section class="species-workspace col-span-12">
  <div hidden={!!selectedName}>
    <div class="sw-heading">
      <div>
        <h1>{t('analytics.speciesTools.title')}</h1>
        <p>{t('analytics.speciesTools.manage.allTimeNote')}</p>
      </div>
      <Button
        onclick={() => {
          draft = [...workspace.preferences.columns];
          editing = true;
        }}>{t('analytics.speciesTools.editColumns')}</Button
      >
    </div>
    {#if editing}
      <div class="sw-editor">
        {#each columns as column (column)}<div class="sw-editor-row">
            <label
              ><input
                type="checkbox"
                checked={draft.includes(column)}
                onchange={event => editColumn(column, event.currentTarget.checked)}
              />
              {columnLabel(column)}</label
            >{#if draft.includes(column)}<Button
                disabled={draft.indexOf(column) === 0}
                onclick={() => moveColumn(column, -1)}
                aria-label={t('analytics.speciesTools.moveUp')}><ArrowUp class="size-4" /></Button
              ><span>{draft.indexOf(column) + 1}</span><Button
                disabled={draft.indexOf(column) === draft.length - 1}
                onclick={() => moveColumn(column, 1)}
                aria-label={t('analytics.speciesTools.moveDown')}
                ><ArrowDown class="size-4" /></Button
              >{/if}
          </div>{/each}
        <div class="sw-actions">
          <Button
            variant="primary"
            onclick={() => {
              workspace.saveColumns(draft);
              editing = false;
            }}>{t('common.buttons.save')}</Button
          ><Button onclick={() => (editing = false)}>{t('common.buttons.cancel')}</Button>
        </div>
      </div>
    {/if}
    <div class="sw-filters">
      <input
        type="search"
        bind:value={search}
        placeholder={t('analytics.speciesTools.search')}
        aria-label={t('analytics.speciesTools.search')}
      /><select bind:value={taxon} aria-label={t('analytics.speciesTools.group')}
        >{#each taxons as value (value)}<option {value}>{taxonLabel(value)}</option>{/each}</select
      >
    </div>
    {#if deleteError && !deleteTarget}<p role="alert">{deleteError}</p>{/if}
    <div class="sw-status">
      <span aria-live="polite"
        >{t('analytics.speciesTools.showingSpecies', {
          shown: visibleRows.length,
          total: workspace.rows.length,
        })}</span
      >{#if workspace.pending.size}<span>{t('common.ui.loading')}</span>{/if}
    </div>
    {#each [...workspace.errors] as [group, message] (group)}<p role="alert">
        {message}
        <Button onclick={() => (group === 'inventory' ? workspace.load() : workspace.retry())}
          >{t('common.retry')}</Button
        >
      </p>{/each}
    <div class="sw-mobile-sort">
      <select
        value={workspace.preferences.sort}
        onchange={event => workspace.sort(event.currentTarget.value as SortKey)}
        aria-label={t('analytics.speciesTools.sort')}
        ><option value="name">{t('analytics.species.headers.species')}</option
        >{#each workspace.preferences.columns as column (column)}<option value={column}
            >{columnLabel(column)}</option
          >{/each}</select
      ><Button
        onclick={() => workspace.sort(workspace.preferences.sort)}
        aria-label={t('analytics.speciesTools.sort')}
        >{workspace.preferences.ascending ? '↑' : '↓'}</Button
      >
    </div>
    <div
      class="sw-table"
      role="region"
      aria-label={t('analytics.speciesTools.title')}
      onpointerenter={() => workspace.setInteracting(true)}
      onpointerleave={event =>
        workspace.setInteracting(event.currentTarget.contains(document.activeElement))}
      onfocusin={() => workspace.setInteracting(true)}
      onfocusout={event => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null))
          workspace.setInteracting(event.currentTarget.matches(':hover'));
      }}
    >
      <table>
        <thead
          ><tr
            ><th
              aria-sort={workspace.preferences.sort === 'name'
                ? workspace.preferences.ascending
                  ? 'ascending'
                  : 'descending'
                : 'none'}
              ><button type="button" onclick={() => workspace.sort('name')}
                >{t('analytics.species.headers.species')}</button
              ></th
            >{#each workspace.preferences.columns as column (column)}<th
                aria-sort={workspace.preferences.sort === column
                  ? workspace.preferences.ascending
                    ? 'ascending'
                    : 'descending'
                  : 'none'}
                ><button type="button" onclick={() => workspace.sort(column)}
                  >{columnLabel(column)}
                  {workspace.preferences.sort === column
                    ? workspace.preferences.ascending
                      ? '↑'
                      : '↓'
                    : ''}</button
                ></th
              >{/each}<th>{t('detections.headers.actions')}</th></tr
          ></thead
        >
        <tbody
          >{#each visibleRows as row (row.scientific_name)}<tr
              ><td
                ><a
                  href={buildAppUrl(speciesUrl(row))}
                  onclick={event => {
                    if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
                    event.preventDefault();
                    navigation.navigate(speciesUrl(row));
                  }}>{localizeSpeciesName(row.scientific_name, row.common_name)}</a
                ><small>{row.scientific_name}</small></td
              >{#each workspace.preferences.columns as column (column)}<td
                  >{#if column === 'best'}{#if workspace.recordings.get(row.scientific_name)}<Button
                        onclick={() => showRecording(row)}
                        ><Play class="size-4" />{display(row, column)}</Button
                      >{:else}<span
                        >{workspace.recordings.has(row.scientific_name)
                          ? t('analytics.speciesTools.audioUnavailable')
                          : '…'}</span
                      >{/if}{:else}{display(row, column)}{/if}</td
                >{/each}<td
                ><SpeciesActions {workspace} species={row} onDelete={requestDelete} /></td
              ></tr
            >{/each}</tbody
        >
      </table>
    </div>
    <div class="sw-mobile-list">
      {#each visibleRows as row (row.scientific_name)}<article>
          <div class="sw-heading">
            <a
              href={buildAppUrl(speciesUrl(row))}
              onclick={event => {
                event.preventDefault();
                navigation.navigate(speciesUrl(row));
              }}>{localizeSpeciesName(row.scientific_name, row.common_name)}</a
            ><SpeciesActions {workspace} species={row} onDelete={requestDelete} />
          </div>
          <details>
            <summary
              >{workspace.preferences.columns
                .slice(0, 2)
                .map(column => `${columnLabel(column)}: ${display(row, column)}`)
                .join(' · ') || row.scientific_name}</summary
            >
            <p>{row.scientific_name}</p>
            {#each workspace.preferences.columns.slice(2) as column (column)}<p>
                {columnLabel(column)}: {column === 'best' &&
                workspace.recordings.has(row.scientific_name) &&
                !workspace.recordings.get(row.scientific_name)
                  ? t('analytics.speciesTools.audioUnavailable')
                  : display(row, column)}
              </p>{/each}{#if workspace.preferences.columns.includes('best') && workspace.recordings.get(row.scientific_name)}<Button
                onclick={() => showRecording(row)}
                >{t('analytics.speciesTools.bestRecording')}</Button
              >{/if}
          </details>
        </article>{/each}
    </div>
  </div>
  {#if selectedName}{#if selected}{#key selectedName}<SpeciesRecordings
          {workspace}
          species={selected}
        />{/key}{:else}<p>
        {workspace.pending.has('inventory')
          ? t('common.ui.loading')
          : t('analytics.species.noSpeciesFound')}
      </p>
      <Button onclick={() => navigation.navigate('/ui/analytics/species-list')}
        >{t('analytics.speciesTools.title')}</Button
      >{/if}{/if}
</section>
<Modal
  isOpen={!!deleteTarget}
  title={t('analytics.speciesTools.manage.deleteTitle')}
  type="confirm"
  confirmVariant="error"
  confirmLabel={t('analytics.speciesTools.manage.deleteConfirm')}
  loading={deleting}
  onConfirm={confirmDelete}
  onClose={() => {
    if (!deleting) deleteTarget = null;
  }}
>
  {#if deleteTarget}<p>
      {t('analytics.speciesTools.manage.deleteMessage', {
        species: localizeSpeciesName(deleteTarget.scientific_name, deleteTarget.common_name),
        count: deleteTarget.count ?? 0,
      })}
    </p>
    <p>{t('analytics.speciesTools.manage.deleteWarning')}</p>{/if}
  {#if deleteError}<p role="alert">{deleteError}</p>{/if}
</Modal>
<RecordingPopup {recording} title={recordingTitle} onClose={() => (recording = null)} />
