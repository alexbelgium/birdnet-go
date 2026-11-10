<!--
  Species workspace route (/ui/analytics/species-workspace).
  Without ?species it shows the overview of every species; with
  ?species=<scientific name> it shows that species' recordings.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '$lib/i18n';
  import { appState } from '$lib/stores/appState.svelte';
  import { navigation } from '$lib/stores/navigation.svelte';
  import WorkspaceOverview from './overview/WorkspaceOverview.svelte';
  import SpeciesDetail from './detail/SpeciesDetail.svelte';

  const ROUTE = '/ui/analytics/species-workspace';

  let search = $state(typeof window === 'undefined' ? '' : window.location.search);
  const selectedSpecies = $derived(new URLSearchParams(search).get('species')?.trim() ?? '');
  const accessAllowed = $derived(!appState.security.enabled || appState.security.accessAllowed);

  function readUrl() {
    search = window.location.search;
  }

  // SPA navigation keeps the pathname; re-read the query whenever the route changes.
  $effect(() => {
    void navigation.currentPath;
    readUrl();
  });

  onMount(() => {
    window.addEventListener('popstate', readUrl);
    return () => window.removeEventListener('popstate', readUrl);
  });

  /** Navigates within the workspace, replacing query parameters. */
  function go(params: Record<string, string | undefined>, replace = false) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== '') query.set(key, value);
    }
    const url = query.size > 0 ? `${ROUTE}?${query}` : ROUTE;
    if (replace) navigation.redirect(url);
    else navigation.navigate(url);
    readUrl();
  }
</script>

<div class="col-span-12 space-y-4" role="region" aria-label={t('speciesWorkspace.title')}>
  {#if !accessAllowed}
    <div class="card bg-[var(--color-base-100)] shadow-xs">
      <div class="card-body card-padding">
        <h1 class="card-title text-2xl">{t('speciesWorkspace.title')}</h1>
        <p role="alert">{t('speciesWorkspace.states.loginRequired')}</p>
      </div>
    </div>
  {:else if selectedSpecies}
    {#key selectedSpecies}
      <SpeciesDetail
        scientificName={selectedSpecies}
        query={search}
        onBack={() => go({})}
        onQueryChange={(params, replace) => go({ species: selectedSpecies, ...params }, replace)}
      />
    {/key}
  {:else}
    <WorkspaceOverview onOpenSpecies={name => go({ species: name })} />
  {/if}
</div>
