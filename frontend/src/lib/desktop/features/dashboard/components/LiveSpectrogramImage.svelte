<script lang="ts">
  import { buildAppUrl } from '$lib/utils/urlHelpers';

  let { refreshKey }: { refreshKey: unknown } = $props();

  let visibleSrc = $state<string>();
  let failures = $state(0);
  let generation = 0;

  const endpoint = buildAppUrl('/api/v2/spectrogram/live');

  function loadNext(): void {
    if (document.hidden) return;

    const requestGeneration = ++generation;
    const nextSrc = `${endpoint}?_=${requestGeneration}`;
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
