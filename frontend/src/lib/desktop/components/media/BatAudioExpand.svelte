<!--
  BatAudioExpand.svelte

  Self-contained "audible bat playback" toolbar row (issue #3572).

  Bat echolocation calls are ultrasonic and inaudible in raw recordings.
  This component fetches a server-side time-expanded copy slowed by a fixed
  factor (5×/10×/16×/20×) and resampled to 48 kHz so the calls fall into
  human hearing range (e.g. a 45 kHz call at 5× becomes ~9 kHz).

  The component is invisible for non-bat detections (GET /expand returns
  isBat=false). It shows a disabled control with an explanatory tooltip when
  the source recording is below the 96 kHz ultrasonic floor.

  Usage: <BatAudioExpand detectionId={detection.id.toString()} />
  Rendered below AudioToolbar when enableClipExtraction is true.
  POST endpoint requires authentication (same as /process).
-->
<script lang="ts">
  /* global Audio */
  import { onMount } from 'svelte';
  import { untrack } from 'svelte';
  import { Ear, Download, ChevronDown, Loader2, Play, Pause } from '@lucide/svelte';
  import { dropdown } from '$lib/utils/transitions';
  import { t } from '$lib/i18n';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { getCsrfToken } from '$lib/utils/api';
  import { loggers } from '$lib/utils/logger';

  const logger = loggers.audio;
  const STORAGE_KEY = 'birdnet-bat-expand-factor';
  const ALLOWED_FACTORS = [5, 10, 16, 20];

  interface ExpandInfo {
    supported: boolean;
    isBat: boolean;
    sourceRate: number;
    minSourceRate: number;
    outputRate: number;
    defaultFactor: number;
    factors: number[];
  }

  interface Props {
    detectionId: string;
  }

  let { detectionId }: Props = $props();

  let expandInfo = $state<ExpandInfo | null>(null);
  let factor = $state(5);
  let showFactorMenu = $state(false);
  let isGenerating = $state(false);
  let expandedBlobUrl = $state<string | null>(null);
  let isPlayingExpanded = $state(false);
  let expandError = $state<string | null>(null);
  let audioEl: HTMLAudioElement | null = null;

  function loadStoredFactor(): number {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw !== null) {
        const n = parseInt(raw, 10);
        if (ALLOWED_FACTORS.includes(n)) return n;
      }
    } catch {
      // localStorage unavailable (e.g. private browsing restrictions)
    }
    return 5;
  }

  function storeFactor(f: number) {
    try {
      localStorage.setItem(STORAGE_KEY, String(f));
    } catch {
      // ignore
    }
  }

  function csrfHeaders(): Record<string, string> {
    const tok = getCsrfToken();
    return tok ? { 'X-CSRF-Token': tok } : {};
  }

  async function fetchInfo(id: string) {
    try {
      const res = await fetch(buildAppUrl(`/api/v2/audio/${encodeURIComponent(id)}/expand`));
      if (!res.ok) return;
      const data = (await res.json()) as ExpandInfo;
      expandInfo = data;
      // Apply server default only if user has no stored preference
      if (localStorage.getItem(STORAGE_KEY) === null) {
        factor = data.defaultFactor;
      }
    } catch (err) {
      logger.debug('bat expand info fetch failed', { id, error: err });
    }
  }

  async function playExpanded() {
    if (isGenerating || !expandInfo?.supported) return;

    // Pause if already playing
    if (isPlayingExpanded && audioEl) {
      audioEl.pause();
      return;
    }

    // Re-use cached blob for the same factor
    if (expandedBlobUrl && audioEl) {
      audioEl.currentTime = 0;
      await audioEl.play();
      isPlayingExpanded = true;
      return;
    }

    isGenerating = true;
    expandError = null;
    try {
      const res = await fetch(
        buildAppUrl(`/api/v2/audio/${encodeURIComponent(detectionId)}/expand?factor=${factor}`),
        { method: 'POST', headers: csrfHeaders() }
      );
      if (!res.ok) {
        let msg = t('components.audioPlayer.batExpand.error');
        try {
          const errData = (await res.json()) as { message?: string };
          msg = errData.message ?? msg;
        } catch {
          // use default
        }
        throw new Error(msg);
      }
      const blob = await res.blob();

      const prev = expandedBlobUrl;
      expandedBlobUrl = URL.createObjectURL(blob);
      if (prev) URL.revokeObjectURL(prev);

      if (!audioEl) {
        audioEl = new Audio();
        audioEl.addEventListener('ended', () => {
          isPlayingExpanded = false;
        });
        audioEl.addEventListener('pause', () => {
          isPlayingExpanded = false;
        });
        audioEl.addEventListener('error', () => {
          isPlayingExpanded = false;
        });
      }
      audioEl.src = expandedBlobUrl;
      await audioEl.play();
      isPlayingExpanded = true;
    } catch (err) {
      expandError =
        err instanceof Error ? err.message : t('components.audioPlayer.batExpand.error');
      logger.error('bat expand play failed', err as Error);
    } finally {
      isGenerating = false;
    }
  }

  async function downloadExpanded() {
    if (isGenerating || !expandInfo?.supported) return;
    isGenerating = true;
    expandError = null;
    try {
      const res = await fetch(
        buildAppUrl(
          `/api/v2/audio/${encodeURIComponent(detectionId)}/expand?factor=${factor}&download=1`
        ),
        { method: 'POST', headers: csrfHeaders() }
      );
      if (!res.ok) throw new Error(t('components.audioPlayer.batExpand.error'));
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `detection_${detectionId}_${factor}x_audible.wav`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) {
      expandError =
        err instanceof Error ? err.message : t('components.audioPlayer.batExpand.error');
    } finally {
      isGenerating = false;
    }
  }

  function selectFactor(f: number) {
    factor = f;
    storeFactor(f);
    showFactorMenu = false;
    // Invalidate cached blob — new factor needs new audio
    if (expandedBlobUrl) {
      URL.revokeObjectURL(expandedBlobUrl);
      expandedBlobUrl = null;
    }
    if (audioEl) {
      audioEl.pause();
      isPlayingExpanded = false;
    }
  }

  function handleOutsideClick(e: MouseEvent) {
    const el = e.target as HTMLElement | null;
    if (showFactorMenu && !el?.closest('.bat-factor-dropdown')) {
      showFactorMenu = false;
    }
  }

  onMount(() => {
    factor = loadStoredFactor();
    return () => {
      if (audioEl) {
        audioEl.pause();
        audioEl = null;
      }
      if (expandedBlobUrl) {
        URL.revokeObjectURL(expandedBlobUrl);
      }
    };
  });

  // Re-fetch capability info whenever detectionId changes.
  // untrack() prevents expandedBlobUrl and other state writes from creating
  // circular effect dependencies.
  $effect(() => {
    const id = detectionId;
    untrack(() => {
      expandInfo = null;
      expandError = null;
      isPlayingExpanded = false;
      if (audioEl) audioEl.pause();
      const url = expandedBlobUrl;
      expandedBlobUrl = null;
      if (url) URL.revokeObjectURL(url);
      fetchInfo(id);
    });
  });
</script>

<svelte:document onclick={handleOutsideClick} />

{#if expandInfo?.isBat}
  {@const disabled = !expandInfo.supported}
  {@const disabledReason = t('components.audioPlayer.batExpand.disabledTooltip', {
    minRate: Math.round(expandInfo.minSourceRate / 1000),
  })}

  <div
    class="bat-expand-toolbar"
    role="group"
    aria-label={t('components.audioPlayer.batExpand.title')}
  >
    <!-- Section label -->
    <div class="bat-expand-label" aria-hidden="true">
      <Ear size={14} />
      <span>{t('components.audioPlayer.batExpand.title')}</span>
    </div>

    <!-- Factor dropdown (how many times slower) -->
    <div class="bat-factor-dropdown">
      <button
        class="expand-btn"
        disabled={disabled || isGenerating}
        title={disabled ? disabledReason : t('components.audioPlayer.batExpand.factorTooltip')}
        aria-label={t('components.audioPlayer.batExpand.factorTooltip')}
        aria-expanded={showFactorMenu}
        aria-haspopup="listbox"
        onclick={() => {
          if (!disabled) showFactorMenu = !showFactorMenu;
        }}
      >
        <span>{factor}×</span>
        <ChevronDown size={10} />
      </button>
      {#if showFactorMenu}
        <div
          class="factor-menu"
          role="listbox"
          aria-label={t('components.audioPlayer.batExpand.factorTooltip')}
          in:dropdown={{ y: -4, duration: 120 }}
          out:dropdown={{ y: -4, duration: 80 }}
        >
          {#each expandInfo.factors as f (f)}
            <button
              class="factor-option"
              class:selected={factor === f}
              role="option"
              aria-selected={factor === f}
              onclick={() => selectFactor(f)}
            >
              {f}×
              {#if expandInfo.sourceRate > 0}
                <span class="factor-hz">
                  (~{Math.round(expandInfo.sourceRate / f / 1000)} kHz)
                </span>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Play / Pause button -->
    <button
      class="expand-btn"
      class:active={isPlayingExpanded}
      disabled={disabled || isGenerating}
      title={disabled
        ? disabledReason
        : isPlayingExpanded
          ? t('components.audioPlayer.batExpand.pause')
          : t('components.audioPlayer.batExpand.play')}
      aria-label={isPlayingExpanded
        ? t('components.audioPlayer.batExpand.pause')
        : t('components.audioPlayer.batExpand.play')}
      onclick={playExpanded}
    >
      {#if isGenerating}
        <Loader2 size={14} class="animate-spin" />
      {:else if isPlayingExpanded}
        <Pause size={14} />
      {:else}
        <Play size={14} />
      {/if}
    </button>

    <!-- Download button -->
    <button
      class="expand-btn"
      disabled={disabled || isGenerating}
      title={disabled ? disabledReason : t('components.audioPlayer.batExpand.download')}
      aria-label={t('components.audioPlayer.batExpand.download')}
      onclick={downloadExpanded}
    >
      <Download size={14} />
    </button>

    {#if expandError}
      <span class="expand-error" role="alert" aria-live="assertive">{expandError}</span>
    {/if}
  </div>
{/if}

<style>
  .bat-expand-toolbar {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.375rem 0.5rem;
    background: var(--color-base-200);
    border: 1px solid var(--color-base-300);
    border-radius: var(--radius-field);
    font-size: 0.75rem;
    margin-top: 0.25rem;
  }

  .bat-expand-label {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    opacity: 0.7;
    font-size: 0.6875rem;
    font-weight: 500;
    margin-right: 0.25rem;
    white-space: nowrap;
  }

  .bat-factor-dropdown {
    position: relative;
  }

  .expand-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.25rem;
    padding: 0.25rem 0.375rem;
    background: none;
    border: 1px solid var(--color-base-300);
    border-radius: var(--radius-selector);
    color: var(--color-base-content);
    cursor: pointer;
    font-size: 0.6875rem;
    font-weight: 500;
    transition: all 0.15s ease;
  }

  .expand-btn:hover:not(:disabled) {
    background: var(--color-base-300);
  }

  .expand-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .expand-btn.active {
    background: var(--color-primary);
    color: var(--color-primary-content, #fff);
    border-color: var(--color-primary);
  }

  .factor-menu {
    position: absolute;
    bottom: 100%;
    left: 0;
    margin-bottom: 0.25rem;
    background: var(--color-base-100);
    border: 1px solid var(--color-base-300);
    border-radius: var(--radius-field);
    box-shadow: 0 4px 12px rgb(0 0 0 / 0.15);
    z-index: 10;
    min-width: 110px;
  }

  .factor-option {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    width: 100%;
    padding: 0.375rem 0.625rem;
    text-align: left;
    background: none;
    border: none;
    color: var(--color-base-content);
    cursor: pointer;
    font-size: 0.75rem;
  }

  .factor-option:hover {
    background: var(--color-base-200);
  }

  .factor-option.selected {
    color: var(--color-primary);
    font-weight: 600;
  }

  .factor-hz {
    opacity: 0.6;
    font-size: 0.625rem;
  }

  .expand-error {
    font-size: 0.6875rem;
    color: var(--color-error, #ef4444);
    max-width: 16rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-left: 0.25rem;
  }
</style>
