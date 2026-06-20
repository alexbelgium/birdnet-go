<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { computeConfidenceColor } from '../../utils/dailySummaryStats';

  // Narrow fixed width for each stat cell — keeps all rows aligned and
  // leaves room for the species column + hour grid on smaller screens.
  const STAT_CELL_CLASS = 'shrink-0 w-14 flex items-center justify-center px-1';
  const HEADER_CELL_CLASS =
    'shrink-0 w-14 flex items-center justify-center px-1 text-center leading-tight text-[10px]';
  // Subtle right edge separates the stat block from the species column.
  const SEPARATOR_CLASS = 'border-r border-[var(--color-base-200)]';

  interface Props {
    variant: 'header' | 'data' | 'spacer';
    item?: DailySpeciesSummary;
  }

  let { variant, item }: Props = $props();
</script>

{#if variant === 'header'}
  <div
    class={HEADER_CELL_CLASS}
    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
  >
    Max<br />confidence
  </div>
  <div
    class="{HEADER_CELL_CLASS} {SEPARATOR_CLASS}"
    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
  >
    Total<br />detections
  </div>
{:else if variant === 'data'}
  <div class={STAT_CELL_CLASS}>
    {#if item?.max_confidence != null}
      {@const pct = Math.round(Math.max(0, Math.min(1, item.max_confidence)) * 100)}
      <div
        class="rounded px-1 text-[10px] font-semibold text-white tabular-nums"
        style:background-color={computeConfidenceColor(pct)}
        title="Max confidence: {pct}%"
      >
        {pct}%
      </div>
    {:else}
      <span class="text-xs text-[var(--color-base-content)]/40">—</span>
    {/if}
  </div>
  <div class="{STAT_CELL_CLASS} {SEPARATOR_CLASS} text-sm font-medium tabular-nums">
    {item?.count ?? 0}
  </div>
{:else}
  <div class="shrink-0 w-14"></div>
  <div class="shrink-0 w-14 {SEPARATOR_CLASS}"></div>
{/if}
