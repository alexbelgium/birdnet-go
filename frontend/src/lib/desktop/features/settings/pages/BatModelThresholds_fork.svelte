<script lang="ts">
  import { t } from '$lib/i18n';
  import { settingsStore, settingsActions } from '$lib/stores/settings';
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import NumberField from '$lib/desktop/components/forms/NumberField.svelte';

  const minimumThreshold = 0.1;
  const minimumBackgroundMax = 0.5;
  const backgroundMaxStep = 0.005;
  const models = [
    { key: 'batDetect2', name: 'BatDetect2' },
    { key: 'bsgBat', name: 'BSG-BAT' },
  ] satisfies { key: 'batDetect2' | 'bsgBat'; name: string }[];
  let store = $derived($settingsStore);
  let sharedThreshold = $derived(store.formData.bat?.threshold ?? 0.5);

  function updateThreshold(key: 'batDetect2' | 'bsgBat', threshold: number) {
    const current = store.formData.forkModels;
    const model = key === 'batDetect2' ? current?.batDetect2 : current?.bsgBat;
    settingsActions.updateSection('forkModels', {
      ...current,
      [key]: { ...model, threshold },
    });
  }

  function updateBackgroundMax(value: number) {
    const current = store.formData.forkModels;
    const backgroundMax = value > 0 && value < minimumBackgroundMax ? 0 : value;
    settingsActions.updateSection('forkModels', {
      ...current,
      bsgBat: { ...current?.bsgBat, backgroundMax },
    });
  }
</script>

{#each models as model (model.key)}
  {@const threshold = store.formData.forkModels?.[model.key]?.threshold ?? 0}
  <Checkbox
    checked={threshold > 0}
    label={`${model.name}: ${t('analysis.detection.secondaryThresholdOverride.label')}`}
    disabled={store.isLoading || store.isSaving}
    helpText={t('analysis.detection.batThresholdOverride.helpText')}
    onchange={enabled =>
      updateThreshold(model.key, enabled ? Math.max(minimumThreshold, sharedThreshold) : 0)}
  />
  <NumberField
    label={`${model.name}: ${t('analysis.detection.batThreshold.label')}`}
    value={threshold > 0 ? threshold : sharedThreshold}
    onUpdate={value => updateThreshold(model.key, value)}
    min={minimumThreshold}
    max={0.99}
    step={0.01}
    disabled={store.isLoading || store.isSaving || threshold === 0}
    helpText={t('analysis.detection.batThresholdOverride.helpText')}
  />
  {#if model.key === 'bsgBat'}
    <NumberField
      label={t('analysis.detection.bsgBatBackgroundMax.label')}
      value={store.formData.forkModels?.bsgBat?.backgroundMax ?? 0}
      onUpdate={updateBackgroundMax}
      min={0}
      max={1}
      step={backgroundMaxStep}
      disabled={store.isLoading || store.isSaving}
      helpText={t('analysis.detection.bsgBatBackgroundMax.helpText')}
    />
  {/if}
{/each}
