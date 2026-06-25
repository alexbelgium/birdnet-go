<script lang="ts">
  import { getLocale } from '$lib/i18n';
  import { ExternalLink } from '@lucide/svelte';
  import { buildEbirdUrl, isValidEbirdCode } from '../../utils/dailySummaryStats';

  interface Props {
    speciesCode: string;
    displayName: string;
  }

  let { speciesCode, displayName }: Props = $props();
</script>

{#if isValidEbirdCode(speciesCode)}
  <a
    href={buildEbirdUrl(speciesCode, getLocale())}
    target="_blank"
    rel="noopener noreferrer"
    class="shrink-0 opacity-40 hover:opacity-100 hover:text-[var(--color-primary)]"
    title="View on eBird"
    aria-label="View {displayName} on eBird"
  >
    <ExternalLink class="size-3" />
  </a>
{/if}
