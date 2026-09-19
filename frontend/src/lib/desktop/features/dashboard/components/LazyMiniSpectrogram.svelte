<script lang="ts">
  /**
   * LazyMiniSpectrogram — defers loading of MiniSpectrogram (and its heavy
   * hls.js / Web Audio dependencies) until after the dashboard has mounted.
   *
   * MiniSpectrogram statically imports hls.js, which is large. Importing it
   * eagerly from DashboardPage pulled hls.js into the main dashboard chunk and
   * blocked first paint — even though the live spectrogram never auto-starts
   * unless the user has access and previously enabled it. Loading it on mount
   * via dynamic import keeps hls.js in a separate chunk fetched off the
   * critical path, with no behavioural change for users who use the widget.
   */
  import { onMount } from 'svelte';
  import type { Component } from 'svelte';
  import type { PendingDetection } from '$lib/types/pending.types';

  let { pendingDetections = [] }: { pendingDetections?: PendingDetection[] } = $props();

  let MiniSpectrogram = $state<Component<{ pendingDetections?: PendingDetection[] }> | null>(null);

  onMount(async () => {
    const module = await import('./MiniSpectrogram.svelte');
    MiniSpectrogram = module.default;
  });
</script>

{#if MiniSpectrogram}
  <MiniSpectrogram {pendingDetections} />
{/if}
