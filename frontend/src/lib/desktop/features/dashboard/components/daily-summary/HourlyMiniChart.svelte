<script module lang="ts">
  // Bar geometry is part of the chart's contract. The SVG is stretched to
  // whatever width its column gives it, so these no longer set anyone's layout —
  // but hourAxis.ts derives its bar-centre fraction (BAR_WIDTH/2 / BAR_STRIDE)
  // from them, which is what keeps the axis ticks over the bars they label.
  export const BAR_WIDTH = 3;
  export const BAR_STRIDE = 4; // bar width + gap
</script>

<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { safeArrayAccess } from '$lib/utils/security';
  import { computeHourDaylightColor } from '../../utils/dailySummaryStats';

  interface Props {
    item: DailySpeciesSummary;
    sunriseHour: number | null;
    sunsetHour: number | null;
    maxHour?: number; // last hour to render (inclusive); default 23 = all 24 bars
    chartHeight?: number; // SVG height in px; default 20
  }

  let { item, sunriseHour, sunsetHour, maxHour = 23, chartHeight = 20 }: Props = $props();

  const barCount = $derived(maxHour + 1); // 1–24
  const svgWidth = $derived(barCount * BAR_STRIDE);
  const maxBarHeight = $derived(chartHeight - 2); // 2 px headroom at top

  const maxCount = $derived(Math.max(...item.hourly_counts.slice(0, barCount), 1));

  const bars = $derived(
    Array.from({ length: barCount }, (_, hour) => {
      const count = safeArrayAccess(item.hourly_counts, hour, 0) ?? 0;
      const barHeight = count > 0 ? Math.max(2, Math.round((count / maxCount) * maxBarHeight)) : 1;
      return {
        hour,
        count,
        barHeight,
        color: computeHourDaylightColor(hour, sunriseHour, sunsetHour),
      };
    })
  );
</script>

<!--
  Pure SVG bar chart — renders bars 0..maxHour (default all 24).
  SVG width scales proportionally so no empty space is shown for future hours.
  Bar color encodes time-of-day (dark indigo = night, green = midday).
-->
<svg
  viewBox="0 0 {svgWidth} {chartHeight}"
  width={svgWidth}
  height={chartHeight}
  preserveAspectRatio="none"
  role="img"
  aria-label="Hourly detection frequency"
>
  {#each bars as { hour, count, barHeight, color } (hour)}
    <rect
      x={hour * BAR_STRIDE}
      y={chartHeight - barHeight}
      width={BAR_WIDTH}
      height={barHeight}
      fill={count > 0 ? color : 'rgba(120,120,120,0.15)'}
      rx="0.5"
    />
  {/each}
</svg>
