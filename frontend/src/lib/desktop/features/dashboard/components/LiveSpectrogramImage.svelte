<script lang="ts">
  import { buildAppUrl } from '$lib/utils/urlHelpers';

  let { refreshKey, source }: { refreshKey: unknown; source?: string } = $props();

  let visibleSrc = $state<string>();
  let failures = $state(0);
  let generation = 0;

  const endpoint = buildAppUrl('/api/v2/spectrogram/live');

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
    <img
      src={visibleSrc}
      alt="Live audio spectrogram"
      class="aspect-[10/3] w-full rounded-lg border border-[var(--color-base-200)] bg-[var(--color-base-200)] object-cover"
    />
  </div>
{/if}
