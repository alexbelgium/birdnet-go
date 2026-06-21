<script lang="ts">
  // Use prop callback instead of legacy event dispatcher
  import { onMount, onDestroy } from 'svelte';
  import ConfidenceCircle from '$lib/desktop/components/data/ConfidenceCircle.svelte';
  import VerificationBadges from '$lib/desktop/components/ui/VerificationBadges.svelte';
  import SourceBadge from '$lib/desktop/features/dashboard/components/SourceBadge.svelte';
  import { Volume2 } from '@lucide/svelte';
  import { t } from '$lib/i18n';
  import type { Detection } from '$lib/types/detection.types';
  import { navigation } from '$lib/stores/navigation.svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { createSpectrogramLoader } from '$lib/utils/spectrogramLoader.svelte';

  interface Props {
    detection: Detection;
    onDetailsClick?: (_id: number) => void;
    onPlayMobileAudio?: (_payload: {
      audioUrl: string;
      speciesName: string;
      detectionId: number;
    }) => void;
    className?: string;
  }

  let { detection, onDetailsClick, onPlayMobileAudio, className = '' }: Props = $props();

  // Localize the common name for the visitor's UI locale, falling back to the
  // server-provided common name then the scientific name (mirrors DetectionRow).
  const displayName = $derived(localizeSpeciesName(detection.scientificName, detection.commonName));

  const loader = createSpectrogramLoader({ size: 'md', raw: true });
  let cardElement = $state<HTMLElement | undefined>(undefined);
  let isVisible = $state(false);

  // Start/stop loader based on visibility, matching the recent detections card.
  $effect(() => {
    if (isVisible) {
      loader.start(detection.id);
    } else {
      loader.stop();
    }
  });

  function handlePlay() {
    const audioUrl = buildAppUrl(`/api/v2/audio/${detection.id}`);
    if (onPlayMobileAudio) {
      onPlayMobileAudio({ audioUrl, speciesName: displayName, detectionId: detection.id });
    }
  }

  function goToDetails() {
    if (onDetailsClick) {
      onDetailsClick(detection.id);
    } else {
      navigation.navigate(`/ui/detections/${detection.id}`);
    }
  }

  // eslint-disable-next-line no-undef -- browser global
  let observer: IntersectionObserver | undefined;

  onMount(() => {
    if (!cardElement) return;

    // eslint-disable-next-line no-undef -- browser global
    observer = new IntersectionObserver(
      entries => {
        for (const entry of entries) {
          isVisible = entry.isIntersecting;
        }
      },
      { rootMargin: '200px 0px' }
    );
    observer.observe(cardElement);
  });

  onDestroy(() => {
    observer?.disconnect();
    loader.destroy();
  });
</script>

<section
  bind:this={cardElement}
  class={`card bg-[var(--color-base-100)] shadow-xs relative overflow-hidden ${className}`}
>
  {#if loader.showSpinner}
    <div class="absolute inset-0 flex items-center justify-center bg-[var(--color-base-200)]/50">
      <span class="loading loading-spinner loading-md text-[var(--color-base-content)]/50"></span>
    </div>
  {/if}
  {#if !loader.error && loader.spectrogramUrl}
    <img
      src={loader.spectrogramUrl}
      alt={t('components.audio.spectrogramAlt')}
      class="absolute left-0 bottom-0 w-full min-h-full object-cover object-bottom transition-opacity duration-300"
      class:opacity-0={loader.state === 'loading'}
      style:image-rendering="pixelated"
      decoding="async"
      onload={() => loader.handleImageLoad()}
      onerror={() => loader.handleImageError()}
    />
  {/if}
  <div class="card-body p-3 space-y-3 relative">
    <!-- Header: Names and confidence -->
    <div class="flex items-start gap-3">
      <div class="flex-1 min-w-0">
        <div class="text-xs font-semibold leading-tight truncate">
          {displayName}
        </div>
        <div class="text-[11px] opacity-70 truncate">
          {detection.scientificName}
        </div>
        <div class="mt-1 text-[11px] opacity-70">
          {detection.date}
          {detection.time}
        </div>
        {#if detection.source}
          <div class="mt-1">
            <SourceBadge {detection} variant="inline" />
          </div>
        {/if}
      </div>
      <div class="shrink-0">
        <ConfidenceCircle confidence={detection.confidence} size="sm" />
      </div>
    </div>

    <!-- Status badges -->
    <div class="flex flex-wrap gap-2">
      <VerificationBadges {detection} />
    </div>

    <!-- Actions -->
    <div class="flex items-center gap-2">
      <button
        class="btn btn-primary btn-sm text-xs bg-primary/70 border-primary/70 hover:bg-primary/80 hover:border-primary/80"
        onclick={handlePlay}
        aria-label={t('search.detailsPanel.playAudio', { species: displayName })}
      >
        <Volume2 class="h-4 w-4" />
        {t('common.actions.play')}
      </button>
      <button
        class="btn btn-outline btn-sm text-xs"
        onclick={goToDetails}
        aria-label={t('search.detailsPanel.viewDetails', { species: displayName })}
      >
        {t('common.actions.view')}
      </button>
    </div>
  </div>
</section>
