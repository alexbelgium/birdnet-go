<script lang="ts">
  import { t } from '$lib/i18n';
  import { settingsStore, settingsActions } from '$lib/stores/settings';
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import NumberField from '$lib/desktop/components/forms/NumberField.svelte';

  const minimumThreshold = 0.1;
  let store = $derived($settingsStore);
  let sharedThreshold = $derived(store.formData.bat?.threshold ?? 0.5);
  let threshold = $derived(store.formData.forkModels?.batDetect2?.threshold ?? 0);

  function updateThreshold(threshold: number) {
    const current = store.formData.forkModels;
    settingsActions.updateSection('forkModels', {
      ...current,
      batDetect2: { ...current?.batDetect2, threshold },
    });
  }
</script>

<Checkbox
  checked={threshold > 0}
  label={`BatDetect2: ${t('analysis.detection.secondaryThresholdOverride.label')}`}
  disabled={store.isLoading || store.isSaving}
  helpText={t('analysis.detection.batThresholdOverride.helpText')}
  onchange={enabled => updateThreshold(enabled ? Math.max(minimumThreshold, sharedThreshold) : 0)}
/>
<NumberField
  label={`BatDetect2: ${t('analysis.detection.batThreshold.label')}`}
  value={threshold > 0 ? threshold : sharedThreshold}
  onUpdate={updateThreshold}
  min={minimumThreshold}
  max={0.99}
  step={0.01}
  disabled={store.isLoading || store.isSaving || threshold === 0}
  helpText={t('analysis.detection.batThresholdOverride.helpText')}
/>
