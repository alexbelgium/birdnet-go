<!-- Page 2: all recordings of one species. -->
<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { ArrowLeft, ExternalLink, LineChart, ListChecks, Lock } from '@lucide/svelte';
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import ConfirmModal from '$lib/desktop/components/modals/ConfirmModal.svelte';
  import ReanalyzeModal from '$lib/desktop/components/modals/ReanalyzeModal.svelte';
  import SpeciesThumbnail from '$lib/desktop/components/modals/SpeciesThumbnail.svelte';
  import AudioPlayer from '$lib/desktop/components/media/AudioPlayer.svelte';
  import ActionMenu from '$lib/desktop/components/ui/ActionMenu.svelte';
  import Pagination from '$lib/desktop/components/ui/Pagination.svelte';
  import SpeciesHistoryModal from '$lib/desktop/features/dashboard/components/daily-summary/SpeciesHistoryModal.svelte';
  import { useDetectionActions } from '$lib/desktop/features/detections/composables/useDetectionActions.svelte';
  import { getLocale, t } from '$lib/i18n';
  import {
    hydrateExcludedSpecies,
    isExcluded,
    setExcluded,
  } from '$lib/stores/excludedSpecies.svelte';
  import { toastActions } from '$lib/stores/toast';
  import type { Detection } from '$lib/types/detection.types';
  import { getLocalDateString } from '$lib/utils/date';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import * as api from '../api';
  import { ebirdSpeciesUrl, observationsUrl } from '../externalLinks';
  import { formatCount, formatDateTime, formatPercent } from '../format';
  import { createRequestSlot, isAbortError } from '../requestSlot';
  import type { MembershipKind, WorkspaceSort, WorkspaceSpecies } from '../types';
  import { createWorkspaceData } from '../workspaceData.svelte';
  import RecordingRow from './RecordingRow.svelte';
  import { detailParams, parseDetailQuery, type RecordingHandlers } from './detailQuery';

  interface Props {
    scientificName: string;
    query: string;
    onBack: () => void;
    onQueryChange: (_params: Record<string, string | undefined>, _replace?: boolean) => void;
  }

  let { scientificName, query, onBack, onQueryChange }: Props = $props();

  const PER_PAGE = 25;
  const KINDS: MembershipKind[] = ['excluded', 'included', 'confirmed'];

  const data = createWorkspaceData();
  const listSlot = createRequestSlot();
  const infoSlot = createRequestSlot();
  const bestSlot = createRequestSlot();

  const view = $derived(parseDetailQuery(query));
  let info = $state<WorkspaceSpecies | null>(null);
  let infoStatus = $state<'loading' | 'ready' | 'missing' | 'error'>('loading');
  let page = $state<api.RecordingsPage | null>(null);
  let listStatus = $state<'loading' | 'ready' | 'error'>('loading');
  let refreshToken = $state(0);
  let selecting = $state(false);
  const selected = new SvelteSet<number>();
  let showGraph = $state(false);
  let observationsHref = $state('');
  // Full detection behind the best recording, for its action menu.
  let bestDetection = $state<Detection | null>(null);

  const displayName = $derived(localizeSpeciesName(scientificName, info?.commonName));
  const stat = $derived(data.stats.get(scientificName));
  const best = $derived(data.best.get(scientificName));
  const ebirdHref = $derived(info ? ebirdSpeciesUrl(info.speciesCode, getLocale()) : null);

  // Stats are loaded for this species only; the whole-database stats are not needed here.
  $effect(() => {
    const name = scientificName;
    data.ensure(new Set(['memberships']));
    // Untracked: the refresh reads and writes the stores this effect must not follow.
    untrack(() => void data.refreshSpecies(name));
    void observationsUrl(name).then(url => (observationsHref = url));
  });

  $effect(() => {
    const id = best?.id;
    bestDetection = null;
    if (id == null) {
      bestSlot.abort();
      return;
    }
    bestSlot
      .run(signal => api.fetchDetection(id, signal))
      .then(detection => (bestDetection = detection))
      .catch(error => {
        // Without the detection the best recording stays playable, just without actions.
        if (!isAbortError(error)) bestDetection = null;
      });
  });

  $effect(() => {
    void refreshToken;
    infoStatus = 'loading';
    infoSlot
      .run(signal => api.fetchSpecies(signal, scientificName))
      .then(rows => {
        info = rows[0] ?? null;
        infoStatus = info ? 'ready' : 'missing';
      })
      .catch(error => {
        if (!isAbortError(error)) infoStatus = 'error';
      });
  });

  // A newer query (page, sort, filter, refresh) aborts and replaces the one in flight.
  $effect(() => {
    const params = {
      species: scientificName,
      page: view.page,
      perPage: PER_PAGE,
      sort: view.sort,
      locked: view.locked,
    };
    void refreshToken;
    listStatus = 'loading';
    listSlot
      .run(signal => api.fetchRecordings(params, signal))
      .then(result => {
        page = result;
        listStatus = 'ready';
      })
      .catch(error => {
        if (!isAbortError(error)) listStatus = 'error';
      });
  });

  onDestroy(() => {
    listSlot.abort();
    infoSlot.abort();
    bestSlot.abort();
    data.dispose();
  });

  void hydrateExcludedSpecies();

  function refresh() {
    void data.refreshSpecies(scientificName);
    data.retry('memberships');
    selected.clear();
    refreshToken++;
  }

  const actions = useDetectionActions({
    onRefresh: refresh,
    isSpeciesExcluded: isExcluded,
    onToggleExclusion: setExcluded,
  });

  const handlers: RecordingHandlers = {
    onReview: r => actions.handleReview(r),
    onMarkCorrect: r => void actions.handleMarkCorrect(r),
    onMarkFalsePositive: r => void actions.handleMarkFalsePositive(r),
    onToggleLock: r => actions.handleToggleLock(r),
    onDelete: r => actions.handleDelete(r),
    onReanalyze: r => actions.handleReanalyze(r),
  };

  function update(next: Partial<typeof view>, replace = false) {
    onQueryChange(detailParams({ ...view, ...next }), replace);
  }

  function setSort(column: 'date' | 'confidence') {
    const current = view.sort.startsWith(column) ? view.sort : null;
    const sort: WorkspaceSort = current === `${column}_desc` ? `${column}_asc` : `${column}_desc`;
    update({ sort, page: 1 });
  }

  function ariaSort(column: 'date' | 'confidence') {
    if (!view.sort.startsWith(column)) return 'none';
    return view.sort.endsWith('asc') ? 'ascending' : 'descending';
  }

  function toggleLockedOnly() {
    // Selection belongs to the previous result set.
    selected.clear();
    update({ locked: !view.locked, page: 1 });
  }

  async function toggleMembership(kind: MembershipKind) {
    const member = data.isMember(kind, scientificName);
    if (member === null) return;
    try {
      await data.setMembership(kind, scientificName, !member);
    } catch {
      toastActions.error(
        t('speciesWorkspace.actions.updateFailed', {
          list: t(`speciesWorkspace.membership.${kind}`),
          species: displayName,
        })
      );
    }
  }

  const pageIds = $derived(page?.data.map(r => r.id) ?? []);
  const allSelected = $derived(pageIds.length > 0 && pageIds.every(id => selected.has(id)));
  const selectedIds = $derived([...selected].map(String));

  async function runBulk(action: 'correct' | 'false_positive' | 'lock' | 'unlock' | 'delete') {
    if (selectedIds.length === 0) return;
    try {
      const result =
        action === 'lock' || action === 'unlock'
          ? await api.batchLock(selectedIds, action === 'lock')
          : action === 'delete'
            ? await api.batchDelete(selectedIds)
            : await api.batchReview(selectedIds, action);
      toastActions.success(
        t('speciesWorkspace.table.bulkResult', {
          processed: result.processed,
          skipped: result.skipped,
        })
      );
    } catch {
      toastActions.error(t('speciesWorkspace.table.bulkFailed'));
    }
    refresh();
  }

  let bulkDeleteOpen = $state(false);
</script>

<div class="card bg-[var(--color-base-100)] shadow-xs">
  <div class="card-body card-padding space-y-3">
    <button type="button" class="btn btn-ghost btn-sm w-fit gap-1" onclick={onBack}>
      <ArrowLeft class="size-4" />{t('speciesWorkspace.detail.back')}
    </button>

    <div class="flex items-center gap-4">
      <SpeciesThumbnail {scientificName} commonName={displayName} size="md" />
      <div class="min-w-0">
        <h1 class="card-title text-xl sm:text-2xl">
          {t('speciesWorkspace.detail.title', { species: displayName })}
        </h1>
        <p class="italic opacity-70">{scientificName}</p>
      </div>
    </div>

    {#if infoStatus === 'error'}
      <div role="alert" class="alert alert-error alert-soft flex items-center justify-between py-2">
        <span
          >{t('speciesWorkspace.states.loadFailed', {
            what: t('speciesWorkspace.groups.inventory'),
          })}</span
        >
        <button type="button" class="btn btn-sm" onclick={() => refreshToken++}
          >{t('speciesWorkspace.states.retry')}</button
        >
      </div>
    {:else if infoStatus === 'missing'}
      <p role="status">{t('speciesWorkspace.states.notFound')}</p>
    {/if}

    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm" aria-live="polite">
      {#if info}
        <span>{t('speciesWorkspace.detail.detections', { count: formatCount(info.total) })}</span>
        <span aria-hidden="true">·</span>
        <span>
          {t('speciesWorkspace.detail.maxConfidence', {
            value: stat?.maxConfidence != null ? formatPercent(stat.maxConfidence) : '—',
          })}
        </span>
        <span aria-hidden="true">·</span>
        <span
          >{t('speciesWorkspace.detail.lastDetected', {
            date: formatDateTime(info.lastSeen),
          })}</span
        >
      {:else if infoStatus === 'loading'}
        <span class="h-4 w-64 animate-pulse rounded bg-[var(--color-base-300)]"></span>
      {/if}
      <span class="grow"></span>
      {#each KINDS as kind (kind)}
        {@const member = data.isMember(kind, scientificName)}
        <span title={member === null ? t('speciesWorkspace.states.membershipLoading') : undefined}>
          <Checkbox
            checked={member === true}
            disabled={member === null || data.togglePending.has(`${kind}:${scientificName}`)}
            size="sm"
            onchange={() => toggleMembership(kind)}
          >
            <span aria-hidden="true">{t(`speciesWorkspace.membership.${kind}`)}</span>
            <span class="sr-only">
              {t('speciesWorkspace.detail.toggleAria', {
                list: t(`speciesWorkspace.membership.${kind}`),
                species: displayName,
              })}
            </span>
          </Checkbox>
        </span>
      {/each}
    </div>

    <section aria-label={t('speciesWorkspace.best.title', { species: displayName })}>
      {#if best}
        <p class="mb-1 flex items-center gap-2 text-sm">
          <span class="font-medium">{t('speciesWorkspace.detail.bestRecording')}</span>
          {#if best.locked}
            <span class="badge badge-sm gap-1"
              ><Lock class="size-3" />{t('speciesWorkspace.best.locked')}</span
            >
          {/if}
          <span>{formatPercent(best.confidence)}</span>
          {#if bestDetection}
            <ActionMenu
              className="ml-auto"
              detection={bestDetection}
              onReview={() => bestDetection && handlers.onReview(bestDetection)}
              onReanalyze={() => bestDetection && handlers.onReanalyze(bestDetection)}
              onMarkCorrect={() => bestDetection && handlers.onMarkCorrect(bestDetection)}
              onMarkFalsePositive={() =>
                bestDetection && handlers.onMarkFalsePositive(bestDetection)}
              onToggleLock={() => bestDetection && handlers.onToggleLock(bestDetection)}
              onDelete={() => bestDetection && handlers.onDelete(bestDetection)}
            />
          {/if}
        </p>
        <AudioPlayer
          audioUrl={buildAppUrl(`/api/v2/audio/${best.id}`)}
          detectionId={String(best.id)}
          showSpectrogram={true}
          spectrogramSize="lg"
          responsive={true}
          className="w-full"
        />
      {:else if best === null}
        <p role="status" class="text-sm">
          <span class="font-medium">{t('speciesWorkspace.best.unavailable')}</span> —
          {t('speciesWorkspace.best.unavailableHint')}
        </p>
      {:else if data.bestFailed.has(scientificName)}
        <p role="alert" class="text-sm">
          {t('speciesWorkspace.states.loadFailed', { what: t('speciesWorkspace.groups.best') })}
          <button type="button" class="btn btn-xs" onclick={() => data.retry('best')}
            >{t('speciesWorkspace.states.retry')}</button
          >
        </p>
      {:else}
        <div class="h-32 animate-pulse rounded bg-[var(--color-base-200)]" aria-busy="true"></div>
      {/if}
    </section>

    <div class="flex flex-wrap items-center gap-2">
      <button type="button" class="btn btn-sm gap-1" onclick={() => (showGraph = true)}>
        <LineChart class="size-4" />{t('speciesWorkspace.detail.quickGraph')}
      </button>
      {#if ebirdHref}
        <a
          class="btn btn-sm gap-1"
          href={ebirdHref}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t('speciesWorkspace.detail.openExternal', {
            site: 'eBird',
            species: displayName,
          })}
        >
          eBird<ExternalLink class="size-3.5" />
        </a>
      {/if}
      <a
        class="btn btn-sm gap-1"
        href={observationsHref || undefined}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={t('speciesWorkspace.detail.openExternal', {
          site: 'Observations.be',
          species: displayName,
        })}
      >
        Observations.be<ExternalLink class="size-3.5" />
      </a>
      <span class="grow"></span>
      <button
        type="button"
        class="btn btn-sm gap-1"
        aria-pressed={selecting}
        onclick={() => {
          selecting = !selecting;
          selected.clear();
        }}
      >
        <ListChecks class="size-4" />
        {selecting
          ? t('speciesWorkspace.detail.cancelSelect')
          : t('speciesWorkspace.detail.select')}
      </button>
      <button
        type="button"
        class="btn btn-sm gap-1"
        class:btn-primary={view.locked}
        aria-pressed={view.locked}
        onclick={toggleLockedOnly}
      >
        <Lock class="size-4" />{t('speciesWorkspace.detail.lockedOnly')}
      </button>
    </div>

    {#if selecting}
      <div
        class="sticky top-0 z-20 flex flex-wrap items-center gap-2 rounded-lg bg-[var(--color-base-200)] p-2"
        role="toolbar"
        aria-label={t('speciesWorkspace.table.bulkActions')}
      >
        <span class="text-sm" aria-live="polite"
          >{t('speciesWorkspace.table.selected', { count: selected.size })}</span
        >
        <button
          type="button"
          class="btn btn-xs"
          disabled={selected.size === 0 || view.locked}
          onclick={() => runBulk('correct')}>{t('speciesWorkspace.table.markCorrect')}</button
        >
        <button
          type="button"
          class="btn btn-xs"
          disabled={selected.size === 0 || view.locked}
          onclick={() => runBulk('false_positive')}
          >{t('speciesWorkspace.table.markFalsePositive')}</button
        >
        <button
          type="button"
          class="btn btn-xs"
          disabled={selected.size === 0}
          onclick={() => runBulk('lock')}>{t('speciesWorkspace.table.lock')}</button
        >
        <button
          type="button"
          class="btn btn-xs"
          disabled={selected.size === 0}
          onclick={() => runBulk('unlock')}>{t('speciesWorkspace.table.unlock')}</button
        >
        <button
          type="button"
          class="btn btn-xs btn-error"
          disabled={selected.size === 0 || view.locked}
          onclick={() => (bulkDeleteOpen = true)}
          >{t('speciesWorkspace.table.deleteSelected')}</button
        >
        {#if view.locked}
          <span class="text-xs opacity-70">{t('speciesWorkspace.table.bulkLockedHint')}</span>
        {/if}
      </div>
    {/if}

    {#if listStatus === 'error'}
      <div role="alert" class="alert alert-error alert-soft flex items-center justify-between py-2">
        <span
          >{t('speciesWorkspace.states.loadFailed', {
            what: t('speciesWorkspace.groups.recordings'),
          })}</span
        >
        <button type="button" class="btn btn-sm" onclick={() => refreshToken++}
          >{t('speciesWorkspace.states.retry')}</button
        >
      </div>
    {:else if page && page.data.length === 0 && listStatus === 'ready'}
      <p>
        {view.locked ? t('speciesWorkspace.table.emptyLocked') : t('speciesWorkspace.table.empty')}
      </p>
    {:else if page}
      <div class="hidden overflow-x-auto md:block" aria-busy={listStatus === 'loading'}>
        <table class="table table-sm w-full" class:opacity-60={listStatus === 'loading'}>
          <thead>
            <tr>
              {#if selecting}
                <th class="w-10">
                  <Checkbox
                    checked={allSelected}
                    indeterminate={!allSelected && pageIds.some(id => selected.has(id))}
                    onchange={checked =>
                      pageIds.forEach(id => (checked ? selected.add(id) : selected.delete(id)))}
                  >
                    <span class="sr-only">{t('speciesWorkspace.table.selectAll')}</span>
                  </Checkbox>
                </th>
              {/if}
              <th aria-sort={ariaSort('date')}>
                <button type="button" class="font-semibold" onclick={() => setSort('date')}>
                  {t('speciesWorkspace.table.dateTime')}
                  {#if view.sort.startsWith('date')}<span aria-hidden="true"
                      >{view.sort.endsWith('asc') ? '↑' : '↓'}</span
                    >{/if}
                </button>
              </th>
              <th>{t('speciesWorkspace.table.weather')}</th>
              <th>{t('speciesWorkspace.table.source')}</th>
              <th aria-sort={ariaSort('confidence')}>
                <button type="button" class="font-semibold" onclick={() => setSort('confidence')}>
                  {t('speciesWorkspace.table.confidence')}
                  {#if view.sort.startsWith('confidence')}<span aria-hidden="true"
                      >{view.sort.endsWith('asc') ? '↑' : '↓'}</span
                    >{/if}
                </button>
              </th>
              <th>{t('speciesWorkspace.table.model')}</th>
              <th>{t('speciesWorkspace.table.status')}</th>
              <th>{t('speciesWorkspace.table.recording')}</th>
              <th><span class="sr-only">{t('speciesWorkspace.table.actions')}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each page.data as recording (recording.id)}
              <RecordingRow
                {recording}
                layout="row"
                {selecting}
                selected={selected.has(recording.id)}
                onSelect={(id, on) => (on ? selected.add(id) : selected.delete(id))}
                {handlers}
              />
            {/each}
          </tbody>
        </table>
      </div>
      <div class="md:hidden">
        <label class="mb-2 flex items-center gap-2 whitespace-nowrap text-sm">
          {t('speciesWorkspace.sort.label')}
          <select
            class="select select-sm"
            value={view.sort}
            onchange={e => update({ sort: e.currentTarget.value as WorkspaceSort, page: 1 })}
          >
            <option value="confidence_desc">{t('speciesWorkspace.table.sortConfidenceDesc')}</option
            >
            <option value="confidence_asc">{t('speciesWorkspace.table.sortConfidenceAsc')}</option>
            <option value="date_desc">{t('speciesWorkspace.table.sortDateDesc')}</option>
            <option value="date_asc">{t('speciesWorkspace.table.sortDateAsc')}</option>
          </select>
        </label>
        <ul class="space-y-2" aria-busy={listStatus === 'loading'}>
          {#each page.data as recording (recording.id)}
            <RecordingRow
              {recording}
              layout="card"
              {selecting}
              selected={selected.has(recording.id)}
              onSelect={(id, on) => (on ? selected.add(id) : selected.delete(id))}
              {handlers}
            />
          {/each}
        </ul>
      </div>
      {#if page.totalPages > 1}
        <Pagination
          currentPage={page.page}
          totalPages={page.totalPages}
          disabled={listStatus === 'loading'}
          onPageChange={p => {
            selected.clear();
            update({ page: p });
          }}
        />
      {/if}
    {:else}
      <div class="space-y-2" aria-busy="true" role="status">
        <span class="sr-only">{t('speciesWorkspace.states.loading')}</span>
        {#each [0, 1, 2, 3, 4] as i (i)}<div
            class="h-12 animate-pulse rounded bg-[var(--color-base-200)]"
          ></div>{/each}
      </div>
    {/if}
  </div>
</div>

{#if showGraph}
  <SpeciesHistoryModal
    {scientificName}
    {displayName}
    selectedDate={getLocalDateString()}
    onClose={() => (showGraph = false)}
  />
{/if}

<!-- Reanalysis runs in place (from #62); mounted only while open so the shared
     Modal can move focus into it. A correction or deletion refreshes the page. -->
{#if actions.reanalyzeTarget}
  <ReanalyzeModal
    isOpen={true}
    detection={actions.reanalyzeTarget}
    onClose={() => actions.closeReanalyze()}
    onCorrected={() => {
      actions.closeReanalyze();
      refresh();
    }}
    onDeleted={() => {
      actions.closeReanalyze();
      refresh();
    }}
  />
{/if}

{#if actions.showConfirmModal}
  <ConfirmModal
    isOpen={actions.showConfirmModal}
    title={actions.confirmModalConfig.title}
    message={actions.confirmModalConfig.message}
    confirmLabel={actions.confirmModalConfig.confirmLabel}
    onClose={actions.closeModal}
    onConfirm={actions.confirmModal}
  />
{/if}

{#if bulkDeleteOpen}
  <ConfirmModal
    isOpen={bulkDeleteOpen}
    title={t('speciesWorkspace.table.deleteSelectedTitle', {
      count: selected.size,
      species: displayName,
    })}
    message={t('speciesWorkspace.table.deleteSelectedMessage')}
    confirmLabel={t('common.buttons.delete')}
    confirmVariant="error"
    onClose={() => (bulkDeleteOpen = false)}
    onConfirm={async () => {
      bulkDeleteOpen = false;
      await runBulk('delete');
    }}
  />
{/if}
