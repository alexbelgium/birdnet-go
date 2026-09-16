<!-- Best recording of a species in the shared Modal (focus trap and restore). -->
<script lang="ts">
  import { Lock } from '@lucide/svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import SpectrogramPlayer from '$lib/desktop/components/media/SpectrogramPlayer.svelte';
  import { t } from '$lib/i18n';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { formatPercent } from '../format';
  import type { BestRecording } from '../types';

  interface Props {
    speciesName: string | null;
    recording: BestRecording | null | undefined;
    onClose: () => void;
  }

  let { speciesName, recording, onClose }: Props = $props();
</script>

<Modal
  isOpen={speciesName !== null}
  title={speciesName ? t('speciesWorkspace.best.title', { species: speciesName }) : ''}
  size="3xl"
  {onClose}
>
  {#if recording}
    <div class="space-y-3">
      <p class="flex items-center gap-2 text-sm">
        {#if recording.locked}
          <span class="badge badge-sm gap-1"
            ><Lock class="size-3" />{t('speciesWorkspace.best.locked')}</span
          >
        {/if}
        <span
          >{t('speciesWorkspace.best.confidence', {
            value: formatPercent(recording.confidence),
          })}</span
        >
      </p>
      <SpectrogramPlayer
        audioUrl={buildAppUrl(`/api/v2/audio/${recording.id}`)}
        detectionId={String(recording.id)}
        spectrogramSize="lg"
      />
    </div>
  {:else if recording === null}
    <div role="status">
      <p class="font-medium">{t('speciesWorkspace.best.unavailable')}</p>
      <p class="text-sm opacity-70">{t('speciesWorkspace.best.unavailableHint')}</p>
    </div>
  {:else}
    <p role="status">{t('speciesWorkspace.states.loading')}</p>
  {/if}
</Modal>
