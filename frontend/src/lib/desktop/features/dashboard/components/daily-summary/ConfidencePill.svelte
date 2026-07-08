<script lang="ts">
  // Small color-coded confidence badge shared by the desktop stat columns and
  // the mobile summary rows, so confidence reads identically everywhere.
  import { computeConfidenceColor } from '../../utils/dailySummaryStats';

  interface Props {
    /** Confidence percent, 0–100 (caller clamps/rounds). */
    percent: number;
  }

  let { percent }: Props = $props();
</script>

<div
  class="w-9 rounded text-center text-[10px] font-semibold text-white tabular-nums"
  style:background-color={computeConfidenceColor(percent)}
  title="Max confidence: {percent}%"
>
  {percent}%
</div>
