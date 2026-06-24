<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { safeArrayAccess } from '$lib/utils/security';
  import { computeHourDaylightColor } from '../../utils/dailySummaryStats';

  interface Props {
    item: DailySpeciesSummary;
    sunriseHour: number | null;
    sunsetHour: number | null;
    tall?: boolean;
  }

  let { item, sunriseHour, sunsetHour, tall = false }: Props = $props();

  // SVG dimensions: 24 bars × 4 px (3 px bar + 1 px gap) = 96 px wide.
  const SVG_WIDTH = 96;
  const BAR_WIDTH = 3;
  const BAR_STRIDE = 4; // bar width + gap

  const svgHeight = $derived(tall ? 48 : 20);
  const maxBarHeight = $derived(svgHeight - 2); // leaves 2 px headroom at top

  const maxCount = $derived(Math.max(...item.hourly_counts, 1));

  const bars = $derived(
    Array.from({ length: 24 }, (_, hour) => {
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
  Pure SVG bar chart — 24 bars, always covering the full day.
  Bar color encodes time-of-day (dark indigo = night, green = midday).
  No labels or sunrise/sunset markers; the parent table provides context.
  Raw SVG is used here instead of D3 because there are no axes, scales,
  or layout complexity — just 24 fixed-position rectangles.
-->
<svg
  viewBox="0 0 {SVG_WIDTH} {svgHeight}"
  width={SVG_WIDTH}
  height={svgHeight}
  role="img"
  aria-label="Hourly detection frequency"
>
  {#each bars as { hour, count, barHeight, color } (hour)}
    <rect
      x={hour * BAR_STRIDE}
      y={svgHeight - barHeight}
      width={BAR_WIDTH}
      height={barHeight}
      fill={count > 0 ? color : 'rgba(120,120,120,0.15)'}
      rx="0.5"
    />
  {/each}
</svg>
