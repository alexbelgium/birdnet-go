<script lang="ts">
  import ConfidenceBadge from '../ConfidenceBadge.svelte';
  import type { DailySpeciesSummary } from '$lib/types/detection.types';

  // Fixed width for each stat cell — keeps all rows aligned.
  const STAT_CELL_CLASS = 'shrink-0 w-20 flex items-center justify-center px-1';
  const HEADER_CELL_CLASS =
    'shrink-0 w-20 flex items-center justify-center px-1 text-center leading-tight';

  interface Props {
    variant: 'header' | 'data' | 'spacer';
    item?: DailySpeciesSummary;
  }

  let { variant, item }: Props = $props();
</script>

{#if variant === 'header'}
  <div
    class="{HEADER_CELL_CLASS} text-xs"
    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
  >
    Max<br />confidence
  </div>
  <div
    class="{HEADER_CELL_CLASS} text-xs"
    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
  >
    Total<br />detections
  </div>
{:else if variant === 'data'}
  <div class={STAT_CELL_CLASS}>
    {#if item?.max_confidence != null}
      <ConfidenceBadge confidence={item.max_confidence} />
    {:else}
      <span class="text-xs text-[var(--color-base-content)]/40">—</span>
    {/if}
  </div>
  <div class="{STAT_CELL_CLASS} text-sm font-medium tabular-nums">
    {item?.count ?? 0}
  </div>
{:else}
  <div class="shrink-0 w-20"></div>
  <div class="shrink-0 w-20"></div>
{/if}
