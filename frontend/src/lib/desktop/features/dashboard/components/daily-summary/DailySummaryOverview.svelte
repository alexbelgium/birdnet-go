<script lang="ts">
  import type { Component } from 'svelte';
  import StatsSummaryBar from '$lib/desktop/components/ui/StatsSummaryBar.svelte';
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { Activity, Clock, Feather, Star } from '@lucide/svelte';
  import { computeOverviewStats } from '../../utils/dailySummaryStats';

  interface StatItem {
    icon: Component;
    iconClass?: string;
    count: number | string;
    label: string;
  }

  interface Props {
    data: DailySpeciesSummary[];
    selectedDate: string;
    /** Optional weather stat appended to the bar (current conditions on mobile today). */
    weatherStat?: StatItem;
  }

  let { data, selectedDate, weatherStat }: Props = $props();

  const stats = $derived(computeOverviewStats(data, selectedDate));

  // Base three stats, plus "new species" when any are new today, plus the
  // optional weather stat. Built dynamically so absent stats leave no divider.
  const barStats = $derived.by(() => {
    const items: StatItem[] = [
      { icon: Activity, count: stats.total, label: 'detections' },
      { icon: Clock, count: stats.lastHour, label: 'last hour' },
      { icon: Feather, count: stats.speciesCount, label: 'species' },
    ];
    if (stats.newSpecies > 0) {
      items.push({
        icon: Star,
        iconClass: 'text-[var(--color-warning)]',
        count: stats.newSpecies,
        label: 'new',
      });
    }
    if (weatherStat) {
      items.push(weatherStat);
    }
    return items;
  });
</script>

<div class="mb-4 px-1 text-[var(--color-base-content)]/70">
  <StatsSummaryBar stats={barStats} class="flex-wrap gap-y-1" />
</div>
