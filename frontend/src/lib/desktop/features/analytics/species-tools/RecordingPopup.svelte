<script lang="ts">
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import SpectrogramPlayer from '$lib/desktop/components/media/SpectrogramPlayer.svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { t } from '$lib/i18n';
  import type { Recording } from './types';
  let {
    recording,
    title,
    onClose,
  }: { recording: Recording | null; title: string; onClose: () => void } = $props();
</script>

<Modal isOpen={!!recording} {title} size="3xl" {onClose}>
  {#if recording}
    <p class="mb-3">
      {(recording.confidence * 100).toFixed(1)}% {recording.locked
        ? t('analytics.speciesTools.locked')
        : ''}
    </p>
    {#key recording.id}<SpectrogramPlayer
        audioUrl={buildAppUrl(`/api/v2/audio/${recording.id}`)}
        detectionId={String(recording.id)}
        spectrogramSize="xl"
      />{/key}
  {/if}
</Modal>
