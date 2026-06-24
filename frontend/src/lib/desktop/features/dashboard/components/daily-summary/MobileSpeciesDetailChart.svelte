<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { safeArrayAccess } from '$lib/utils/security';
  import { computeHourDaylightColor } from '../../utils/dailySummaryStats';
  import * as d3 from 'd3';

  interface Props {
    item: DailySpeciesSummary;
    sunriseHour: number | null;
    sunsetHour: number | null;
  }

  let { item, sunriseHour, sunsetHour }: Props = $props();

  let containerEl: HTMLDivElement | undefined = $state();
  let svgEl: SVGSVGElement | undefined = $state();
  let containerWidth = $state(0);

  const MARGIN = { top: 8, right: 8, bottom: 36, left: 32 };
  const INNER_HEIGHT = 110;
  const TOTAL_HEIGHT = INNER_HEIGHT + MARGIN.top + MARGIN.bottom;
  const HOURS = Array.from({ length: 24 }, (_, i) => String(i));
  const TICK_HOURS = HOURS.filter((_, i) => i % 2 === 0);

  $effect(() => {
    if (!containerEl) return;
    const observer = new window.ResizeObserver(entries => {
      const entry = entries[0];
      if (entry) containerWidth = entry.contentRect.width;
    });
    observer.observe(containerEl);
    return () => observer.disconnect();
  });

  $effect(() => {
    if (!svgEl || containerWidth <= 0) return;

    const innerWidth = containerWidth - MARGIN.left - MARGIN.right;

    const hourlyData = HOURS.map(h => ({
      hour: h,
      count: safeArrayAccess(item.hourly_counts, Number(h), 0) ?? 0,
      color: computeHourDaylightColor(Number(h), sunriseHour, sunsetHour),
    }));
    const maxCount = Math.max(...hourlyData.map(d => d.count), 1);

    const xScale = d3.scaleBand().domain(HOURS).range([0, innerWidth]).padding(0.12);

    const yScale = d3
      .scaleLinear()
      .domain([0, maxCount * 1.1])
      .range([INNER_HEIGHT, 0])
      .nice();

    const svg = d3.select(svgEl);
    svg.selectAll('*').remove();
    svg
      .attr('width', containerWidth)
      .attr('height', TOTAL_HEIGHT)
      .attr('viewBox', `0 0 ${containerWidth} ${TOTAL_HEIGHT}`);

    const g = svg.append('g').attr('transform', `translate(${MARGIN.left},${MARGIN.top})`);

    // Faint horizontal grid lines
    g.append('g')
      .attr('class', 'grid')
      .call(
        d3
          .axisLeft(yScale)
          .ticks(4)
          .tickSize(-innerWidth)
          .tickFormat(() => '')
      )
      .call(sel => sel.select('.domain').remove())
      .call(sel =>
        sel
          .selectAll('.tick line')
          .attr('stroke', 'rgba(128,128,128,0.18)')
          .attr('stroke-dasharray', '3,3')
      );

    // Bars
    g.selectAll('rect.bar')
      .data(hourlyData)
      .enter()
      .append('rect')
      .attr('class', 'bar')
      .attr('x', d => xScale(d.hour) ?? 0)
      .attr('y', d => (d.count > 0 ? yScale(d.count) : INNER_HEIGHT - 1))
      .attr('width', xScale.bandwidth())
      .attr('height', d => (d.count > 0 ? INNER_HEIGHT - yScale(d.count) : 1))
      .attr('fill', d => (d.count > 0 ? d.color : 'rgba(120,120,120,0.15)'))
      .attr('rx', 1.5);

    // Y axis
    g.append('g')
      .call(d3.axisLeft(yScale).ticks(4).tickSize(3))
      .call(sel => sel.select('.domain').attr('stroke', 'rgba(128,128,128,0.3)'))
      .call(sel => sel.selectAll('.tick line').attr('stroke', 'rgba(128,128,128,0.3)'))
      .call(sel =>
        sel.selectAll('.tick text').attr('fill', 'currentColor').attr('font-size', '9px')
      );

    // X axis
    g.append('g')
      .attr('transform', `translate(0,${INNER_HEIGHT})`)
      .call(
        d3
          .axisBottom(xScale)
          .tickValues(TICK_HOURS)
          .tickFormat(h => h.padStart(2, '0'))
          .tickSize(3)
      )
      .call(sel => sel.select('.domain').attr('stroke', 'rgba(128,128,128,0.3)'))
      .call(sel => sel.selectAll('.tick line').attr('stroke', 'rgba(128,128,128,0.3)'))
      .call(sel =>
        sel.selectAll('.tick text').attr('fill', 'currentColor').attr('font-size', '9px')
      );

    // X axis label
    g.append('text')
      .attr('x', innerWidth / 2)
      .attr('y', INNER_HEIGHT + 30)
      .attr('text-anchor', 'middle')
      .attr('fill', 'currentColor')
      .attr('font-size', '10px')
      .attr('opacity', '0.55')
      .text('Hour');
  });
</script>

<!--
  D3 hourly bar chart for the Level 2 species detail view.
  Uses D3 as required by CLAUDE.md for charts with labelled axes.
  ResizeObserver drives containerWidth; $effect redraws whenever item or width changes.
-->
<div bind:this={containerEl} class="detail-chart-wrap">
  <svg bind:this={svgEl} role="img" aria-label="Hourly detection frequency for {item.common_name}"
  ></svg>
</div>

<style>
  .detail-chart-wrap {
    width: 100%;
    overflow: hidden;
  }

  .detail-chart-wrap svg {
    display: block;
    width: 100%;
    color: inherit;
    overflow: visible;
  }
</style>
