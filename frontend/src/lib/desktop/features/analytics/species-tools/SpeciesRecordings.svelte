<script lang="ts">
  import { getLocale, t } from '$lib/i18n';
  import { navigation } from '$lib/stores/navigation.svelte';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { getLocalDateString } from '$lib/utils/date';
  import { fetchWithCSRF } from '$lib/utils/api';
  import { handleBirdImageError } from '$lib/desktop/components/ui/image-utils';
  import Button from '$lib/desktop/components/ui/Button.svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import { Image, ExternalLink, Play } from '@lucide/svelte';
  import DetectionsPage from '../../detections/DetectionsPage.svelte';
  import SpeciesHistoryModal from './SpeciesHistoryModal.svelte';
  import RecordingPopup from './RecordingPopup.svelte';
  import { membershipNames, type SpeciesRow, type Recording } from './types';
  import { columnLabel } from './labels';
  import type { Workspace } from './workspace.svelte';
  let { workspace, species }: { workspace: Workspace; species: SpeciesRow } = $props();
  let showImage = $state(false);
  let showGraph = $state(false);
  let showMap = $state(false);
  let mapUrl = $state('');
  let mapLoading = $state(false);
  let mapError = $state(false);
  let recording = $state<Recording | null>(null);
  const name = $derived(localizeSpeciesName(species.scientific_name, species.common_name));
  const imageUrl = $derived(
    buildAppUrl(`/api/v2/media/species-image?name=${encodeURIComponent(species.scientific_name)}`)
  );
  const best = $derived(workspace.recordings.get(species.scientific_name));
  const ebirdUrl = $derived(
    species.species_code && /^[a-z0-9]+$/.test(species.species_code)
      ? `https://ebird.org/species/${species.species_code}/BE-WAL?siteLanguage=${encodeURIComponent(getLocale() === 'nb' ? 'no' : getLocale())}`
      : null
  );
  async function openMap() {
    showMap = true;
    if (mapUrl) return;
    mapLoading = true;
    mapError = false;
    try {
      const data = await fetchWithCSRF<{ url: string }>(
        `/api/v2/analytics/species/tools/observation-link?species=${encodeURIComponent(species.scientific_name)}`
      );
      mapUrl = data.url;
    } catch {
      mapError = true;
    } finally {
      mapLoading = false;
    }
  }
</script>

<div class="sw-detail">
  <Button variant="ghost" onclick={() => navigation.navigate('/ui/analytics/species-list')}
    >← {t('analytics.speciesList.title')}</Button
  >
  <div class="sw-heading">
    <div>
      <h1>{t('analytics.speciesTools.allRecordings', { name })}</h1>
      <p>{species.scientific_name}</p>
    </div>
    <Button onclick={() => (showImage = true)} aria-label={t('analytics.speciesTools.image')}
      ><Image class="size-4" /></Button
    >
  </div>
  <div class="sw-hero">
    <button
      type="button"
      onclick={() => (showImage = true)}
      aria-label={t('analytics.speciesTools.image')}
      ><img src={imageUrl} alt={name} onerror={handleBirdImageError} /></button
    >
    <div class="sw-best">
      <div class="sw-heading">
        <span>{t('analytics.speciesTools.bestRecording')}</span>{#if best}<span
            >{best.locked ? t('analytics.speciesTools.locked') : ''} · {(
              best.confidence * 100
            ).toFixed(1)}%</span
          >{/if}
      </div>
      {#if best}<button type="button" class="sw-spectrogram" onclick={() => (recording = best)}
          ><img
            src={buildAppUrl(`/api/v2/spectrogram/${best.id}?size=md&raw=true`)}
            alt={t('analytics.speciesTools.bestRecording')}
          /><span><Play class="size-4" />{t('analytics.speciesTools.play')}</span></button
        >{:else}<p>
          {best === null ? t('analytics.speciesTools.audioUnavailable') : t('common.ui.loading')}
        </p>{/if}
    </div>
  </div>
  <div class="sw-stats">
    <span
      >{columnLabel('count')}:
      <strong>{species.count?.toLocaleString(getLocale()) ?? '…'}</strong></span
    ><span
      >{columnLabel('max_confidence')}:
      <strong
        >{species.max_confidence === undefined
          ? '—'
          : `${(species.max_confidence * 100).toFixed(1)}%`}</strong
      ></span
    ><span
      >{columnLabel('last_heard')}:
      <strong
        >{species.last_heard
          ? new Date(species.last_heard.replace(' ', 'T')).toLocaleDateString(getLocale())
          : '—'}</strong
      ></span
    >{#each membershipNames as kind (kind)}<Button
        disabled={workspace.member(kind, species) === null ||
          workspace.pending.has(`toggle-${kind}`)}
        aria-pressed={workspace.member(kind, species) ?? false}
        onclick={() => workspace.toggle(kind, species)}
        >{columnLabel(kind)}: {workspace.member(kind, species) === null
          ? '…'
          : workspace.member(kind, species)
            ? t('common.yes')
            : t('common.no')}</Button
      >{/each}
  </div>
  {#each [...workspace.errors] as [group, message] (group)}<p role="alert">
      {message}<Button onclick={() => workspace.retry()}>{t('common.retry')}</Button>
    </p>{/each}
  <DetectionsPage speciesWorkspace onWorkspaceRefresh={() => workspace.refresh()}>
    {#snippet toolbar()}<Button onclick={() => (showGraph = true)}
        >{t('analytics.speciesTools.quickGraph')}</Button
      >{#if ebirdUrl}<a
          href={ebirdUrl}
          target="_blank"
          rel="noopener noreferrer"
          class="sw-external">eBird <ExternalLink class="size-4" /></a
        >{/if}<Button onclick={openMap}>Observations.be</Button>{/snippet}
  </DetectionsPage>
</div>
<Modal isOpen={showImage} title={name} size="3xl" onClose={() => (showImage = false)}
  ><img src={imageUrl} alt={name} class="w-full" onerror={handleBirdImageError} /></Modal
>
<Modal isOpen={showMap} title={`Observations.be · ${name}`} onClose={() => (showMap = false)}
  ><p>{t('analytics.speciesTools.mapExternalOnly')}</p>
  {#if mapLoading}<p>{t('common.ui.loading')}</p>{:else if mapUrl}<a
      class="sw-external"
      href={mapUrl}
      target="_blank"
      rel="noopener noreferrer"
      >{t('analytics.speciesTools.openMap')} <ExternalLink class="size-4" /></a
    >{:else if mapError}<p role="alert">{t('analytics.speciesTools.mapUnavailable')}</p>
    <Button onclick={openMap}>{t('common.retry')}</Button>{/if}</Modal
>
{#if showGraph}<SpeciesHistoryModal
    scientificName={species.scientific_name}
    displayName={name}
    selectedDate={getLocalDateString()}
    onClose={() => (showGraph = false)}
  />{/if}
<RecordingPopup {recording} title={name} onClose={() => (recording = null)} />
