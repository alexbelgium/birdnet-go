<!--
  TaxonFilterDropdown

  Top-right filter for the Daily Summary card. Lets the user limit the species
  table to Birds, Bats, or Others (non-bird sound classes), with All as the
  default. Self-contained: classification lives in utils/taxonFilter.ts and the
  host card only renders this component and applies the chosen filter.

  @component
-->
<script lang="ts">
  import type { Component } from 'svelte';
  import { cn } from '$lib/utils/cn';
  import { t } from '$lib/i18n';
  import { dropdown } from '$lib/utils/transitions';
  import { Bird, Check, ChevronDown, ListFilter, Shapes } from '@lucide/svelte';
  import BatIcon from '$lib/components/icons/BatIcon.svelte';
  import type { TaxonCounts, TaxonFilter } from '../../utils/taxonFilter';

  interface Props {
    /** Currently selected filter. */
    value: TaxonFilter;
    /** Row counts per group, shown next to each option. */
    counts: TaxonCounts;
    /** Fired when the user picks a different filter. */
    onChange: (_value: TaxonFilter) => void;
  }

  let { value, counts, onChange }: Props = $props();

  interface Option {
    value: TaxonFilter;
    icon: Component;
  }

  const OPTIONS: readonly Option[] = [
    { value: 'all', icon: ListFilter },
    { value: 'bird', icon: Bird },
    { value: 'bat', icon: BatIcon },
    { value: 'other', icon: Shapes },
  ];

  const current = $derived(OPTIONS.find(o => o.value === value) ?? OPTIONS[0]);
  const CurrentIcon = $derived(current.icon);
  const currentLabel = $derived(t(`dashboard.dailySummary.taxonFilter.${current.value}`));

  let isOpen = $state(false);
  let containerElement: HTMLDivElement;

  function select(next: TaxonFilter) {
    isOpen = false;
    if (next !== value) onChange(next);
  }

  function handleClickOutside(event: MouseEvent) {
    if (isOpen && containerElement && !containerElement.contains(event.target as Node)) {
      isOpen = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (isOpen && event.key === 'Escape') {
      isOpen = false;
    }
  }

  $effect(() => {
    if (!isOpen) return;
    document.addEventListener('click', handleClickOutside);
    document.addEventListener('keydown', handleKeydown);
    return () => {
      document.removeEventListener('click', handleClickOutside);
      document.removeEventListener('keydown', handleKeydown);
    };
  });
</script>

<div bind:this={containerElement} class="relative shrink-0">
  <button
    type="button"
    onclick={() => (isOpen = !isOpen)}
    class="btn btn-sm btn-ghost gap-1.5"
    aria-haspopup="menu"
    aria-expanded={isOpen}
    aria-label={t('dashboard.dailySummary.taxonFilter.ariaLabel', { current: currentLabel })}
  >
    <CurrentIcon class="size-4" />
    <span class="hidden sm:inline">{currentLabel}</span>
    <ChevronDown class="size-3.5 opacity-60" />
  </button>

  {#if isOpen}
    <ul
      in:dropdown
      out:dropdown={{ duration: 100 }}
      class="absolute left-0 sm:left-auto sm:right-0 mt-2 z-[1100] w-44 p-2 shadow-lg rounded-lg border bg-[var(--color-base-100)] border-[var(--color-base-300)]"
      role="menu"
    >
      {#each OPTIONS as option (option.value)}
        {@const OptionIcon = option.icon}
        {@const selected = option.value === value}
        {@const optionLabel = t(`dashboard.dailySummary.taxonFilter.${option.value}`)}
        <li>
          <button
            type="button"
            onclick={() => select(option.value)}
            class={cn(
              'flex w-full items-center gap-2 px-3 py-2 rounded-md text-sm text-left transition-colors hover:bg-[var(--color-base-300)]',
              selected && 'font-medium'
            )}
            role="menuitemradio"
            aria-checked={selected}
          >
            <OptionIcon class="size-4 shrink-0" />
            <span class="grow">{optionLabel}</span>
            <span class="tabular-nums text-xs text-[var(--color-base-content)]/50">
              {counts[option.value]}
            </span>
            {#if selected}
              <Check class="size-4 text-[var(--color-primary)]" />
            {:else}
              <span class="size-4 shrink-0"></span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>
