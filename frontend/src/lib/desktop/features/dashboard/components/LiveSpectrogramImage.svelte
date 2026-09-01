<script lang="ts">
  import { buildAppUrl } from '$lib/utils/urlHelpers';

  let { refreshKey, source }: { refreshKey: unknown; source?: string } = $props();

  let visibleSrc = $state<string>();
  let failures = $state(0);
  let generation = 0;

  const endpoint = buildAppUrl('/api/v2/spectrogram/live');
  const BIRD_NYQUIST_KHZ = 12;
  const BIRD_TICKS_KHZ = [12, 10, 8, 6, 5, 4, 3, 2, 1];

  function loadNext(): void {
    if (document.hidden) return;

    const requestGeneration = ++generation;
    // Pin the image to the source the displayed detections came from; without it
    // the server falls back to its first sorted source, which on a multi-source
    // install can be unrelated to the species chips shown beside it.
    const sourceParam = source ? `source=${encodeURIComponent(source)}&` : '';
    const nextSrc = `${endpoint}?${sourceParam}_=${requestGeneration}`;
    const preload = new window.Image();
    preload.onload = () => {
      if (requestGeneration !== generation) return;
      visibleSrc = nextSrc;
      failures = 0;
    };
    preload.onerror = () => {
      if (requestGeneration !== generation) return;
      failures++;
    };
    preload.src = nextSrc;
  }

  $effect(() => {
    void refreshKey;
    void source;
    loadNext();
    return () => generation++;
  });
</script>

{#if visibleSrc && failures < 2}
  <div class="px-4 pt-4">
    <div
      class="spectrogram-image relative w-full overflow-hidden rounded-lg border border-[var(--color-base-200)] bg-[var(--color-base-200)]"
    >
      <img src={visibleSrc} alt="Live audio spectrogram" class="h-full w-full object-fill" />
      {#each BIRD_TICKS_KHZ as freq (freq)}
        <span
          class="freq-label"
          style:bottom="{(freq / BIRD_NYQUIST_KHZ) * 100}%"
          aria-hidden="true">{freq}k</span
        >
        <div
          class="freq-line"
          style:bottom="{(freq / BIRD_NYQUIST_KHZ) * 100}%"
          aria-hidden="true"
        ></div>
      {/each}
    </div>
  </div>
{/if}

<style>
  .spectrogram-image {
    aspect-ratio: 2 / 1;
  }

  .freq-label {
    position: absolute;
    left: 4px;
    transform: translateY(50%);
    font-size: 0.6875rem;
    font-weight: 600;
    color: rgb(255 255 255 / 0.75);
    text-shadow:
      0 0 3px rgb(0 0 0 / 1),
      0 0 6px rgb(0 0 0 / 0.8),
      1px 1px 2px rgb(0 0 0 / 0.9);
    line-height: 1;
    pointer-events: none;
    z-index: 3;
  }

  .freq-line {
    position: absolute;
    left: 0;
    right: 0;
    height: 1px;
    background: rgb(255 255 255 / 0.12);
    pointer-events: none;
    z-index: 3;
  }
</style>
