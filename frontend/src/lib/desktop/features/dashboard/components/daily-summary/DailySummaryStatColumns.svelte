<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import ConfidencePill from './ConfidencePill.svelte';

  // Narrow fixed width for each stat cell — keeps all rows aligned and
  // leaves room for the species column + hour grid on smaller screens.
  // Cells are sticky during horizontal scroll of the heatmap grid, offset
  // by the (dynamic) species column width since the species column is
  // sticky at left-0 and these cells sit just to its right, with an
  // opaque background so heatmap cells slide underneath.
  const STAT_CELL_CLASS =
    'shrink-0 w-14 flex items-center justify-center px-1 sticky left-[var(--species-col-width)] z-[5] bg-[var(--color-base-100)]';
  const STAT_CELL_2_CLASS =
    'shrink-0 w-14 flex items-center justify-center px-1 sticky left-[calc(var(--species-col-width)+3.5rem)] z-[5] bg-[var(--color-base-100)]';
  const HEADER_TEXT_CLASS = 'text-center leading-tight text-[10px]';
  // Subtle right edge on the second stat cell separates the stat block from the hour grid.
  const SEPARATOR_CLASS = 'border-r border-[var(--color-base-200)]';

  interface Props {
    variant: 'header' | 'data' | 'spacer';
    item?: DailySpeciesSummary;
  }

  let { variant, item }: Props = $props();
</script>

{#if variant === 'header'}
  <div
    class="{STAT_CELL_CLASS} {HEADER_TEXT_CLASS}"
    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
  >
    Max<br />confidence
  </div>
  <div
    class="{STAT_CELL_2_CLASS} {HEADER_TEXT_CLASS} {SEPARATOR_CLASS}"
    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
  >
    Total<br />detections
  </div>
{:else if variant === 'data'}
  <div class={STAT_CELL_CLASS}>
    {#if item?.max_confidence != null}
      {@const pct = Math.round(Math.max(0, Math.min(1, item.max_confidence)) * 100)}
      <ConfidencePill percent={pct} />
    {:else}
      <span class="text-xs text-[var(--color-base-content)]/40">—</span>
    {/if}
  </div>
  <div class="{STAT_CELL_2_CLASS} {SEPARATOR_CLASS} text-sm font-medium tabular-nums">
    {item?.count ?? 0}
  </div>
{:else}
  <div class={STAT_CELL_CLASS}></div>
  <div class="{STAT_CELL_2_CLASS} {SEPARATOR_CLASS}"></div>
{/if}
