<!-- One recording row, composed from the shared detection building blocks. -->
<script lang="ts">
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import ConfidenceCircle from '$lib/desktop/components/data/ConfidenceCircle.svelte';
  import WeatherMetrics from '$lib/desktop/components/data/WeatherMetrics.svelte';
  import SpectrogramPlayer from '$lib/desktop/components/media/SpectrogramPlayer.svelte';
  import ActionMenu from '$lib/desktop/components/ui/ActionMenu.svelte';
  import VerificationBadges from '$lib/desktop/components/ui/VerificationBadges.svelte';
  import SourceBadge from '$lib/desktop/features/dashboard/components/SourceBadge.svelte';
  import { t } from '$lib/i18n';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import type { WorkspaceRecording } from '../api';
  import type { RecordingHandlers } from './detailQuery';
  import { formatDateTime } from '../format';

  interface Props {
    recording: WorkspaceRecording;
    layout: 'row' | 'card';
    selecting: boolean;
    selected: boolean;
    onSelect: (_id: number, _selected: boolean) => void;
    handlers: RecordingHandlers;
  }

  let { recording, layout, selecting, selected, onSelect, handlers }: Props = $props();

  const when = $derived(formatDateTime(recording.timestamp));
  const modelName = $derived(recording.modelName ?? t('speciesWorkspace.states.unknownModel'));
</script>

{#snippet selectBox()}
  <Checkbox checked={selected} onchange={checked => onSelect(recording.id, checked)}>
    <span class="sr-only">{t('speciesWorkspace.table.selectRow', { date: when })}</span>
  </Checkbox>
{/snippet}

{#snippet weather()}
  {#if recording.weather}
    <WeatherMetrics
      weatherIcon={recording.weather.weatherIcon}
      weatherDescription={recording.weather.description}
      temperature={recording.weather.temperature}
      windSpeed={recording.weather.windSpeed}
      windGust={recording.weather.windGust}
      units={recording.weather.units}
      timeOfDay={recording.timeOfDay === 'night' ? 'night' : 'day'}
      size="sm"
    />
  {/if}
{/snippet}

{#snippet player()}
  {#if recording.clipName}
    <SpectrogramPlayer
      audioUrl={buildAppUrl(`/api/v2/audio/${recording.id}`)}
      detectionId={String(recording.id)}
      spectrogramSize="md"
    />
  {:else}
    <span class="text-xs opacity-60">{t('speciesWorkspace.best.unavailable')}</span>
  {/if}
{/snippet}

{#snippet actions()}
  <ActionMenu
    detection={recording}
    onReview={() => handlers.onReview(recording)}
    onReanalyze={() => handlers.onReanalyze(recording)}
    onMarkCorrect={() => handlers.onMarkCorrect(recording)}
    onMarkFalsePositive={() => handlers.onMarkFalsePositive(recording)}
    onToggleLock={() => handlers.onToggleLock(recording)}
    onDelete={() => handlers.onDelete(recording)}
  />
{/snippet}

{#if layout === 'row'}
  <tr class="hover">
    {#if selecting}<td class="w-10">{@render selectBox()}</td>{/if}
    <td class="whitespace-nowrap">{when}</td>
    <td>{@render weather()}</td>
    <td><SourceBadge detection={recording} variant="inline" /></td>
    <td><ConfidenceCircle confidence={recording.confidence} size="sm" /></td>
    <td class="whitespace-nowrap">{modelName}</td>
    <td><VerificationBadges detection={recording} /></td>
    <td>{@render player()}</td>
    <td>{@render actions()}</td>
  </tr>
{:else}
  <li class="space-y-2 rounded-lg border border-[var(--color-base-300)] p-3">
    <div class="flex items-center justify-between gap-2">
      <div class="flex items-center gap-2">
        {#if selecting}{@render selectBox()}{/if}
        <span class="font-medium">{when}</span>
      </div>
      <div class="flex items-center gap-2">
        <ConfidenceCircle confidence={recording.confidence} size="sm" />
        {@render actions()}
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-2 text-sm">
      {@render weather()}
      <SourceBadge detection={recording} variant="inline" />
      <span>{modelName}</span>
      <VerificationBadges detection={recording} />
    </div>
    {@render player()}
  </li>
{/if}
