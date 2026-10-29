/**
 * reanalyze.svelte.ts
 *
 * App-wide state for the reanalysis modal. ReanalyzeHost, mounted once in
 * App.svelte, renders the modal for whichever detection was last opened here, so
 * the detail page and every detection list or card open it with one call instead
 * of each mounting its own copy.
 */

import type { Detection } from '$lib/types/detection.types';

/** What the opener wants to happen after the modal changes the detection. */
export interface ReanalyzeCallbacks {
  /** Called after the detection is corrected or its verdict is changed. */
  onChanged?: () => void;
  /** Called after the detection is deleted. Defaults to onChanged. */
  onDeleted?: () => void;
}

let detection = $state<Detection | null>(null);
// Not cleared on close: the modal calls onClose before onCorrected/onDeleted, so
// the callbacks must outlive the open state.
let callbacks: ReanalyzeCallbacks = {};

/** Read-only view of the modal state for ReanalyzeHost. */
export const reanalyzeState = {
  get detection() {
    return detection;
  },
  get callbacks() {
    return callbacks;
  },
};

/** Open the reanalysis modal for a detection. */
export function openReanalyze(target: Detection, cbs: ReanalyzeCallbacks = {}) {
  callbacks = cbs;
  detection = target;
}

/** Close the reanalysis modal. */
export function closeReanalyze() {
  detection = null;
}
