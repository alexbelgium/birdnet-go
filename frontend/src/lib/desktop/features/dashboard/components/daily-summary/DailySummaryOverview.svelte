<script lang="ts">
  import StatsSummaryBar from '$lib/desktop/components/ui/StatsSummaryBar.svelte';
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { Activity, Clock, Feather } from '@lucide/svelte';
  import { computeOverviewStats } from '../../utils/dailySummaryStats';

  interface Props {
    data: DailySpeciesSummary[];
    selectedDate: string;
  }

  let { data, selectedDate }: Props = $props();

  const stats = $derived(computeOverviewStats(data, selectedDate));
</script>

<div class="mb-4 px-1 text-[var(--color-base-content)]/70">
  <StatsSummaryBar
    stats={[
      { icon: Activity, count: stats.total, label: 'detections' },
      { icon: Clock, count: stats.lastHour, label: 'last hour' },
      { icon: Feather, count: stats.speciesCount, label: 'species' },
    ]}
  />
</div>
