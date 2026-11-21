<!--
  ReanalyzeHost.svelte

  The single mount point of the reanalysis modal, placed once in App.svelte.
  Openers call openReanalyze() from $lib/stores/reanalyze.svelte instead of
  mounting their own modal.
-->
<script lang="ts">
  import ReanalyzeModal from './ReanalyzeModal.svelte';
  import { reanalyzeState, closeReanalyze } from '$lib/stores/reanalyze.svelte';
</script>

<!-- Mounted only while open, so the app carries no hidden dialog otherwise. -->
{#if reanalyzeState.detection}
  <ReanalyzeModal
    isOpen
    detection={reanalyzeState.detection}
    onClose={closeReanalyze}
    onCorrected={() => reanalyzeState.callbacks.onChanged?.()}
    onDeleted={() => (reanalyzeState.callbacks.onDeleted ?? reanalyzeState.callbacks.onChanged)?.()}
  />
{/if}
