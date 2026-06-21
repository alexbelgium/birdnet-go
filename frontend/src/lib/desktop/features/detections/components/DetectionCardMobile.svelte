<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { Detection } from '$lib/types/detection.types';
  import ConfidenceBadge from '$lib/desktop/features/dashboard/components/ConfidenceBadge.svelte';
  import SourceBadge from '$lib/desktop/features/dashboard/components/SourceBadge.svelte';
  import PlayOverlay from '$lib/desktop/features/dashboard/components/PlayOverlay.svelte';
  import SpeciesInfoBar from '$lib/desktop/features/dashboard/components/SpeciesInfoBar.svelte';
  import { ExternalLink } from '@lucide/svelte';
  import { t } from '$lib/i18n';
  import { navigation } from '$lib/stores/navigation.svelte';
  import { createSpectrogramLoader } from '$lib/utils/spectrogramLoader.svelte';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';

  interface Props {
    detection: Detection;
    onDetailsClick?: (_id: number) => void;
    // Kept for backward compat with DetectionsList.svelte; audio is now handled
    // inline by PlayOverlay so this callback is never invoked.
    onPlayMobileAudio?: (_payload: {
      audioUrl: string;
      speciesName: string;
      detectionId: number;
    }) => void;
    className?: string;
  }

  let { detection, onDetailsClick, className = '' }: Props = $props();

  const displayName = $derived(localizeSpeciesName(detection.scientificName, detection.commonName));

  const loader = createSpectrogramLoader({ size: 'md', raw: true });
  let cardElement = $state<HTMLElement | undefined>(undefined);
  let isVisible = $state(false);

  $effect(() => {
    if (isVisible) {
      loader.start(detection.id);
    } else {
      loader.stop();
    }
  });

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

<article bind:this={cardElement} class={`detection-card-mobile relative rounded-xl ${className}`}>
  <div class="detection-card-inner">
    <!-- Spectrogram Background -->
    <div class="spectrogram-container">
      {#if loader.showSpinner}
        <div class="spectrogram-loading">
          <span class="loading loading-spinner loading-md text-[var(--color-base-content)]/50"
          ></span>
          {#if loader.isQueued}
            <span class="text-xs text-[var(--color-base-content)]/40 mt-1">Waiting...</span>
          {:else if loader.isGenerating}
            <span class="text-xs text-[var(--color-base-content)]/40 mt-1">Generating...</span>
          {/if}
        </div>
      {/if}

      {#if loader.error}
        <div class="spectrogram-error">
          <span class="text-sm text-[var(--color-base-content)]/50">Spectrogram unavailable</span>
        </div>
      {:else if loader.spectrogramUrl}
        <img
          src={loader.spectrogramUrl}
          alt="Spectrogram for {detection.commonName}"
          class="spectrogram-image"
          class:opacity-0={loader.state === 'loading'}
          decoding="async"
          style:image-rendering="pixelated"
          onload={() => loader.handleImageLoad()}
          onerror={() => loader.handleImageError()}
        />
      {/if}
    </div>

    <!-- Top-Left Badges: Confidence + Source -->
    <div class="absolute top-2 left-2 flex items-center gap-1.5 z-10">
      <ConfidenceBadge confidence={detection.confidence} />
      <SourceBadge {detection} variant="overlay" />
    </div>

    <!-- Top-Right: View Details -->
    <div class="absolute top-2 right-2 z-10">
      <button
        class="btn btn-circle btn-xs bg-black/30 border-0 text-white hover:bg-black/50"
        onclick={goToDetails}
        aria-label={t('search.detailsPanel.viewDetails', { species: displayName })}
      >
        <ExternalLink class="size-3" />
      </button>
    </div>

    <!-- Center Play Button -->
    <PlayOverlay detectionId={detection.id} />

    <!-- Bottom Species Info Bar -->
    <SpeciesInfoBar {detection} />
  </div>
</article>

<style>
  .detection-card-mobile {
    background-color: var(--color-base-100);
  }

  .detection-card-inner {
    position: relative;
    height: 12rem;
    border-radius: 0.75rem;
    overflow: hidden;
  }

  .spectrogram-container {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }

  .spectrogram-image {
    position: absolute;
    left: 0;
    bottom: 0;
    width: 100%;
    min-height: 100%;
    object-fit: cover;
    object-position: center bottom;
    transition: opacity 0.3s ease;
  }

  .spectrogram-loading,
  .spectrogram-error {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    background: linear-gradient(
      135deg,
      color-mix(in srgb, var(--color-base-200) 80%, transparent) 0%,
      color-mix(in srgb, var(--color-base-300) 60%, transparent) 100%
    );
  }

  :global([data-theme='dark']) .spectrogram-loading,
  :global([data-theme='dark']) .spectrogram-error {
    background: linear-gradient(135deg, rgb(30 41 59 / 0.9) 0%, rgb(15 23 42 / 0.95) 100%);
  }
</style>
