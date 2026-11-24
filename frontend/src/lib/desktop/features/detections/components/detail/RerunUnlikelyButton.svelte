<!--
  Re-runs the ultrasonic "unlikely" filter on a saved bat detection with the current
  filter settings (POST /api/v2/detections/:id/rerun-unlikely). The server sets or
  clears the unlikely flag and its automatic comment; this reloads the detection and
  reports the verdict. Shown only when the server marks the detection eligible
  (canRerunUnlikely: a bat model with an ultrasonic band).
-->
<script lang="ts">
  import Button from '$lib/desktop/components/ui/Button.svelte';
  import { t } from '$lib/i18n';
  import { toastActions } from '$lib/stores/toast';
  import { api, ApiError } from '$lib/utils/api';
  import type { DetectionDetailData, SavedChange } from './detailTypes';

  interface Props {
    detection: DetectionDetailData;
    canReview: boolean;
    /** Another save is out or the detection is locked. */
    blocked: boolean;
    statusId: string;
    onChanged: (_change: SavedChange) => Promise<boolean> | void;
    onError: (_message: string) => void;
  }

  interface RerunResponse {
    outcome: 'unlikely' | 'probable' | 'unchanged';
    unlikely: boolean;
    ultrasonicCV: number;
    ultrasonicCVThreshold: number;
  }

  let { detection, canReview, blocked, statusId, onChanged, onError }: Props = $props();

  let running = $state(false);
  let unavailable = $derived(blocked || running);

  async function rerun() {
    if (!canReview || !detection.canRerunUnlikely || unavailable) return;
    const { id, scientificName } = detection;
    running = true;
    try {
      let result: RerunResponse;
      try {
        result = await api.post<RerunResponse>(`/api/v2/detections/${id}/rerun-unlikely`);
      } catch (error) {
        // A refusal carries the server's reason (error_key rerunUnlikelyRefused),
        // which the API client has already translated into userMessage.
        onError(
          error instanceof ApiError && error.errorKey
            ? error.userMessage
            : t('detections.detail.review.rerunUnlikelyFailed')
        );
        return;
      }
      const params = {
        cv: result.ultrasonicCV.toFixed(3),
        threshold: result.ultrasonicCVThreshold.toFixed(2),
      };
      toastActions.success(
        result.unlikely
          ? t('detections.detail.review.rerunUnlikelyResultUnlikely', params)
          : t('detections.detail.review.rerunUnlikelyResultProbable', params)
      );
      const reloaded = await onChanged({ detectionId: id, scientificName, speciesStats: false });
      if (reloaded === false) onError(t('detections.detail.review.reloadFailed'));
    } finally {
      running = false;
    }
  }
</script>

{#if canReview && detection.canRerunUnlikely}
  <Button
    variant="default"
    size="md"
    title={t('detections.detail.review.rerunUnlikelyHelp')}
    aria-describedby={unavailable ? statusId : undefined}
    aria-disabled={unavailable ? 'true' : undefined}
    aria-busy={running ? 'true' : undefined}
    onclick={rerun}
  >
    {t('detections.detail.review.rerunUnlikely')}
  </Button>
{/if}
