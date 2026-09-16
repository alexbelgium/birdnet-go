/**
 * Column layout state. The server copy (Realtime.Species.SpeciesWorkspace) is
 * authoritative; localStorage only lets the table render instantly before it loads.
 */
import { getStoredValue, setStoredValue } from '$lib/utils/storage';
import { fetchLayout, saveLayout } from './api';
import { DEFAULT_LAYOUT, normalizeLayout } from './columns';
import { isAbortError } from './requestSlot';
import type { WorkspaceLayout } from './types';

export const LAYOUT_CACHE_KEY = 'species-workspace-layout-v1';

function isLayoutShape(value: unknown): value is WorkspaceLayout {
  return !!value && typeof value === 'object' && Array.isArray((value as WorkspaceLayout).columns);
}

export interface LayoutDeps {
  fetch: (_signal?: AbortSignal) => Promise<WorkspaceLayout>;
  save: (_layout: WorkspaceLayout) => Promise<WorkspaceLayout>;
}

export function createLayoutStore(deps: LayoutDeps = { fetch: fetchLayout, save: saveLayout }) {
  let layout = $state<WorkspaceLayout>(
    normalizeLayout(getStoredValue(LAYOUT_CACHE_KEY, DEFAULT_LAYOUT, isLayoutShape))
  );
  let saveError = $state(false);
  let loadError = $state(false);
  let saving = $state(false);

  function apply(next: WorkspaceLayout) {
    layout = normalizeLayout(next);
    setStoredValue(LAYOUT_CACHE_KEY, layout);
  }

  return {
    get layout() {
      return layout;
    },
    get saving() {
      return saving;
    },
    get saveError() {
      return saveError;
    },
    get loadError() {
      return loadError;
    },
    /** Reconciles with the server copy. A failure keeps the cached layout. */
    async load(signal?: AbortSignal) {
      try {
        apply(await deps.fetch(signal));
        loadError = false;
      } catch (error) {
        if (!isAbortError(error)) loadError = true;
      }
    },
    /** Applies immediately and persists; rolls back if the server rejects it. */
    async save(next: WorkspaceLayout) {
      const previous = layout;
      apply(next);
      saving = true;
      saveError = false;
      try {
        apply(await deps.save(layout));
      } catch {
        apply(previous);
        saveError = true;
      } finally {
        saving = false;
      }
    },
    /** Changes the sort locally and persists it. */
    setSort(column: string, direction: 'asc' | 'desc') {
      return this.save({ ...layout, sort: { column, direction } });
    },
    reset() {
      return this.save(DEFAULT_LAYOUT);
    },
  };
}

export type LayoutStore = ReturnType<typeof createLayoutStore>;
