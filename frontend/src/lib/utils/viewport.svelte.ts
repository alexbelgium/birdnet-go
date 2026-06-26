/**
 * Shared viewport / breakpoint helpers.
 *
 * Single source of truth for the responsive breakpoints that drive
 * mobile-specific dashboard behaviour (smaller spectrograms, fewer detection
 * cards, lighter heatmap DOM). Keeping the breakpoints here as named constants
 * avoids scattering magic-number `matchMedia` checks across components and keeps
 * them in sync with the CSS media queries they mirror.
 */

import { onDestroy } from 'svelte';

const isBrowser = typeof window !== 'undefined';

/**
 * Tailwind `sm` breakpoint. At or below this width we treat the client as a
 * phone for the purposes of payload-reducing optimizations (spectrogram size,
 * detection-card count, preloading aggressiveness).
 */
export const MOBILE_MAX_WIDTH_PX = 640;

/**
 * Below this width the daily-summary heatmap collapses to the six-hourly grid.
 * Mirrors the `@media (max-width: 479px)` rule in DailySummaryCard.svelte.
 */
export const SMALL_MOBILE_MAX_WIDTH_PX = 479;

/**
 * At or below this width the daily-summary heatmap uses the bi-hourly grid
 * (anything wider uses the hourly grid). Mirrors the `@media (max-width: 1023px)`
 * boundary in DailySummaryCard.svelte.
 */
export const NON_DESKTOP_MAX_WIDTH_PX = 1023;

/**
 * Synchronous one-shot check: is the viewport at or below the mobile breakpoint?
 *
 * Use this for decisions made once at component init (e.g. which spectrogram
 * size to request). For values that must react to resize/rotate, use
 * {@link useMediaQuery} instead.
 */
export function isMobileViewport(): boolean {
  // matchMedia can be undefined in some test/embedded environments.
  if (!isBrowser || typeof window.matchMedia !== 'function') return false;
  return window.matchMedia(`(max-width: ${MOBILE_MAX_WIDTH_PX}px)`).matches;
}

/**
 * Reactive media-query helper backed by a `MediaQueryList` listener.
 *
 * Must be called during component initialization so its `onDestroy` cleanup is
 * registered (same constraint as Svelte lifecycle hooks). Returns an object
 * with a reactive `matches` getter that updates on viewport changes.
 *
 * @example
 * ```svelte
 * const mobile = useMediaQuery(`(max-width: ${MOBILE_MAX_WIDTH_PX}px)`);
 * // ... mobile.matches is reactive
 * ```
 */
export function useMediaQuery(query: string): { readonly matches: boolean } {
  // matchMedia can be undefined in some test/embedded environments; fall back to
  // a static `false` (desktop-equivalent) and skip the listener in that case.
  const mql =
    isBrowser && typeof window.matchMedia === 'function' ? window.matchMedia(query) : null;
  let matches = $state(mql?.matches ?? false);

  if (mql) {
    const handler = (event: MediaQueryListEvent) => {
      matches = event.matches;
    };
    mql.addEventListener('change', handler);
    onDestroy(() => mql.removeEventListener('change', handler));
  }

  return {
    get matches() {
      return matches;
    },
  };
}

/**
 * Reactive convenience wrapper for the mobile breakpoint. See {@link useMediaQuery}
 * for the lifecycle constraint.
 */
export function useIsMobile(): { readonly matches: boolean } {
  return useMediaQuery(`(max-width: ${MOBILE_MAX_WIDTH_PX}px)`);
}
