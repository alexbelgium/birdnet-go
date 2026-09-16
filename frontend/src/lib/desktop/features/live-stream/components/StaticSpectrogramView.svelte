<script lang="ts">
  import { AlertCircle, Loader2, RefreshCw } from '@lucide/svelte';
  import { untrack } from 'svelte';
  import { t } from '$lib/i18n';
  import { getLocalTimeString } from '$lib/utils/date';
  import { buildAppUrl } from '$lib/utils/urlHelpers';

  interface Props {
    sourceId: string;
    sourceName?: string;
  }

  const CAPTURE_DURATION_SECONDS = 10;
  const SESSION_DURATION_MS = 2 * 60 * 1000;
  const RETRY_DELAY_MS = 2000;
  const SAMPLE_RATE_HEADER = 'X-Spectrogram-Sample-Rate';
  const GENERATED_AT_HEADER = 'X-Spectrogram-Generated-At';

  let { sourceId, sourceName = '' }: Props = $props();
  let imageUrl = $state<string | null>(null);
  let sampleRate = $state(0);
  let generatedAt = $state<string | null>(null);
  let recording = $state(false);
  let error = $state<string | null>(null);
  let sessionExpired = $state(false);
  let sessionStartedAt = $state(Date.now());

  const nyquistKhz = $derived(sampleRate / 2000);
  const frequencyLabels = $derived(
    sampleRate > 0 ? [nyquistKhz, nyquistKhz * 0.75, nyquistKhz * 0.5, nyquistKhz * 0.25, 0] : []
  );

  function formatKhz(value: number): string {
    return `${Number.isInteger(value) ? value.toFixed(0) : value.toFixed(1)} kHz`;
  }

  function replaceImage(nextUrl: string | null): void {
    if (imageUrl) URL.revokeObjectURL(imageUrl);
    imageUrl = nextUrl;
  }

  function waitForRetry(signal: AbortSignal): Promise<void> {
    return new Promise(resolve => {
      if (signal.aborted) {
        resolve();
        return;
      }
      const timer = globalThis.setTimeout(resolve, RETRY_DELAY_MS);
      signal.addEventListener(
        'abort',
        () => {
          globalThis.clearTimeout(timer);
          resolve();
        },
        { once: true }
      );
    });
  }

  async function captureContinuously(captureSourceId: string, signal: AbortSignal): Promise<void> {
    while (!signal.aborted) {
      recording = true;
      error = null;
      try {
        const encodedSource = encodeURIComponent(captureSourceId);
        const response = await fetch(
          buildAppUrl(
            `/api/v2/streams/spectrogram/${encodedSource}?duration=${CAPTURE_DURATION_SECONDS}`
          ),
          { signal, credentials: 'same-origin' }
        );
        if (!response.ok) {
          throw new Error(`Static spectrogram request failed (${response.status})`);
        }

        const blob = await response.blob();
        if (signal.aborted) return;
        const nextSampleRate = Number(response.headers.get(SAMPLE_RATE_HEADER));
        if (!Number.isFinite(nextSampleRate) || nextSampleRate <= 0) {
          throw new Error('Static spectrogram response did not include a sample rate');
        }
        const generatedHeader = response.headers.get(GENERATED_AT_HEADER);
        const generatedDate = generatedHeader ? new Date(generatedHeader) : new Date();
        sampleRate = nextSampleRate;
        generatedAt = getLocalTimeString(
          Number.isNaN(generatedDate.getTime()) ? new Date() : generatedDate
        );
        replaceImage(URL.createObjectURL(blob));
      } catch (captureError) {
        if (signal.aborted) return;
        recording = false;
        error = captureError instanceof Error ? captureError.message : String(captureError);
        await waitForRetry(signal);
      }
    }
  }

  function restartSession(): void {
    sessionStartedAt = Date.now();
  }

  $effect(() => {
    const captureSourceId = sourceId;
    const sessionDeadline = sessionStartedAt + SESSION_DURATION_MS;

    // untrack: replaceImage reads imageUrl, which would otherwise make every
    // rendered image re-run this effect and restart the capture loop.
    untrack(() => {
      sessionExpired = false;
      recording = false;
      error = null;
      generatedAt = null;
      sampleRate = 0;
      replaceImage(null);
    });

    if (!captureSourceId) return;

    const remainingSessionTime = sessionDeadline - Date.now();
    if (remainingSessionTime <= 0) {
      sessionExpired = true;
      return;
    }

    const controller = new AbortController();
    const sessionTimer = globalThis.setTimeout(() => {
      sessionExpired = true;
      recording = false;
      controller.abort();
    }, remainingSessionTime);
    void captureContinuously(captureSourceId, controller.signal);

    return () => {
      globalThis.clearTimeout(sessionTimer);
      controller.abort();
    };
  });

  $effect(() => {
    return () => replaceImage(null);
  });
</script>

<div class="flex h-full min-h-0 flex-col bg-black text-white">
  <div class="flex min-h-0 flex-1 items-stretch">
    <div
      class="relative w-16 shrink-0 border-r border-white/20 text-[10px] tabular-nums text-white/70"
      aria-label={t('spectrogram.static.frequencyAxis')}
    >
      {#each frequencyLabels as label, index (index)}
        <span
          class="absolute right-2 -translate-y-1/2"
          style:top={`${(index / (frequencyLabels.length - 1)) * 100}%`}
        >
          {formatKhz(label)}
        </span>
      {/each}
    </div>

    <div class="relative min-w-0 flex-1 overflow-hidden">
      {#if imageUrl}
        <img
          src={imageUrl}
          alt={t('spectrogram.static.imageAlt', { source: sourceName })}
          class="h-full w-full object-fill"
        />
      {:else if !sessionExpired}
        <div class="flex h-full flex-col items-center justify-center gap-3 text-white/70">
          <Loader2 class="size-8 animate-spin" />
          <span class="text-sm">{t('spectrogram.static.recording')}</span>
        </div>
      {/if}

      {#if sessionExpired}
        <div
          class="absolute inset-0 flex flex-col items-center justify-center gap-4 bg-black/80 p-6"
        >
          <p class="max-w-md text-center text-sm">{t('spectrogram.static.sessionExpired')}</p>
          <button
            type="button"
            onclick={restartSession}
            class="inline-flex items-center gap-2 rounded-lg bg-[var(--color-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
          >
            <RefreshCw class="size-4" />
            {t('spectrogram.static.restart')}
          </button>
        </div>
      {/if}
    </div>
  </div>

  <div
    class="flex min-h-10 flex-none items-center gap-4 border-t border-white/20 px-4 py-2 text-xs text-white/70"
    aria-live="polite"
  >
    {#if recording && !sessionExpired}
      <span class="inline-flex items-center gap-2">
        <span class="size-2 animate-pulse rounded-full bg-red-500"></span>
        {t('spectrogram.static.recording')}
      </span>
    {/if}
    {#if generatedAt}
      <span>{t('spectrogram.static.generatedAt', { time: generatedAt })}</span>
    {/if}
    {#if error && !sessionExpired}
      <span class="inline-flex items-center gap-1 text-red-300">
        <AlertCircle class="size-4" />
        {t('spectrogram.static.captureFailed')}
      </span>
    {/if}
    <span class="ml-auto">{t('spectrogram.static.fullRange')}</span>
  </div>
</div>
