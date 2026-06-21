<!--
  SearchSettings.svelte

  Keyword search across ALL application settings with inline editing + Apply.

  Design goals (self-contained, low-maintenance feature):
  - The searchable list is built by walking the live settings store at runtime
    (`settingsStore.formData`). New settings added anywhere in the app appear
    here automatically with ZERO changes to this file.
  - Each matching scalar setting (boolean/number/string) is edited inline using
    the existing shared form controls and written back through the existing
    `settingsActions.updateSection()`. The "Apply settings" button next to the
    search bar calls the existing `settingsActions.saveSettings()` (PUT
    /api/v2/settings) so edits are persisted and hot-reloaded server-side.
  - Complex settings (arrays/objects) and values we cannot infer a control for
    are shown as "edit on its page" links instead.
  - The ONLY area-level (not field-level) mapping that may rarely need updates is
    SECTION_ROUTE_RULES below, used purely for the convenience "open page" link.

  All supporting logic lives in this single new file by design.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { Search, Save, ExternalLink, SlidersHorizontal } from '@lucide/svelte';
  import { settingsStore, settingsActions, type SettingsFormData } from '$lib/stores/settings';
  import { hasSettingsChanged } from '$lib/utils/settingsChanges';
  import { safeGet } from '$lib/utils/security';
  import { navigation } from '$lib/stores/navigation.svelte';
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import NumberField from '$lib/desktop/components/forms/NumberField.svelte';
  import TextInput from '$lib/desktop/components/forms/TextInput.svelte';
  import EmptyState from '$lib/desktop/components/ui/EmptyState.svelte';
  import LoadingSpinner from '$lib/desktop/components/ui/LoadingSpinner.svelte';

  // ---------------------------------------------------------------------------
  // Types
  // ---------------------------------------------------------------------------
  type SettingKind = 'boolean' | 'number' | 'string' | 'complex';

  interface SettingEntry {
    path: string; // dotted store path, e.g. 'realtime.audio.export.gain'
    section: keyof SettingsFormData; // top-level section (path's first segment)
    segments: string[]; // ['realtime','audio','export','gain']
    label: string; // humanized last segment, e.g. 'Gain'
    breadcrumb: string; // 'Realtime › Audio › Export › Gain'
    kind: SettingKind;
    secret: boolean; // render string as password input
    haystack: string; // lowercased text used for matching
    route: string | null; // settings section route key for the "open page" link
  }

  const MAX_RESULTS = 100;

  // Top-level keys that are not user-facing settings; never indexed.
  const META_KEYS = new Set(['version', 'buildDate', 'systemId', 'input', 'debug']);
  // Plain objects we treat as a single complex (non-recursed) leaf.
  const OPAQUE_OBJECT_KEYS = new Set(['taxonomySynonyms', 'layout']);

  // Field-name patterns that should be masked as password inputs.
  const SECRET_RE = /(password|secret|token|apikey|api_key|dsn|key)$/i;

  // ---------------------------------------------------------------------------
  // Area-level path -> settings section route mapping (for the "open page" link).
  // Ordered: first match wins. Coarse on purpose — new fields under an existing
  // area map automatically, so this rarely needs maintenance. Unknown paths get
  // no link (the inline editor still works).
  // ---------------------------------------------------------------------------
  const SECTION_ROUTE_RULES: Array<readonly [RegExp, string]> = [
    [/^birdnet\.(latitude|longitude|locationConfigured)/, 'main'],
    [/^birdnet\./, 'analysis'],
    [/^bat\./, 'analysis'],
    [/^main\./, 'main'],
    [/^output\./, 'main'],
    [/^backup\./, 'main'],
    [/^sentry\./, 'support'],
    [/^notification\./, 'notifications'],
    [/^security\./, 'security'],
    [/^webServer\./, 'security'],
    [/^realtime\.weather/, 'main'],
    [/^realtime\.dashboard/, 'userinterface'],
    [/^realtime\.(audio|rtsp|extendedCapture)/, 'audio'],
    [/^realtime\.(species|speciesTracking)/, 'species'],
    [
      /^realtime\.(privacyFilter|dogBarkFilter|daylightFilter|falsePositiveFilter)/,
      'detectionfilters',
    ],
    [/^realtime\.dynamicThreshold/, 'analysis'],
    [/^realtime\.(mqtt|birdweather|ebird|observability|weather)/, 'integrations'],
    [/^realtime\.(push|alerting)/, 'notifications'],
    [/^realtime\./, 'analysis'],
  ];

  function sectionRouteFor(path: string): string | null {
    for (const [re, route] of SECTION_ROUTE_RULES) {
      if (re.test(path)) return route;
    }
    return null;
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------
  function isPlainObject(value: unknown): value is Record<string, unknown> {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
    const proto = Object.getPrototypeOf(value);
    return proto === null || proto === Object.prototype;
  }

  function humanize(segment: string): string {
    return segment
      .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
      .replace(/[_-]+/g, ' ')
      .replace(/\b\w/g, c => c.toUpperCase())
      .trim();
  }

  // Read an arbitrary dotted path from an object using the safe accessor.
  function getPath(obj: unknown, segments: string[]): unknown {
    let acc: unknown = obj;
    for (const key of segments) {
      if (!isPlainObject(acc)) return undefined;
      acc = safeGet(acc, key, undefined);
    }
    return acc;
  }

  // updateSection is generic over the section key; the walker guarantees that
  // `section` is a real top-level section and `data` is a full clone of it, so a
  // single loosened signature here is safe and avoids `any`.
  const updateSectionLoose = settingsActions.updateSection as (
    _section: keyof SettingsFormData,
    _data: Record<string, unknown>
  ) => void;

  // ---------------------------------------------------------------------------
  // Index: flatten formData into leaf/complex entries
  // ---------------------------------------------------------------------------
  function flatten(formData: SettingsFormData): SettingEntry[] {
    const out: SettingEntry[] = [];

    function pushEntry(segments: string[], kind: SettingKind): void {
      const path = segments.join('.');
      const last = segments[segments.length - 1];
      const breadcrumb = segments.map(humanize).join(' › ');
      out.push({
        path,
        section: segments[0] as keyof SettingsFormData,
        segments,
        label: humanize(last),
        breadcrumb,
        kind,
        secret: SECRET_RE.test(last),
        haystack: `${path} ${breadcrumb}`.toLowerCase(),
        route: sectionRouteFor(path),
      });
    }

    function walk(node: Record<string, unknown>, trail: string[]): void {
      for (const key of Object.keys(node)) {
        if (trail.length === 0 && META_KEYS.has(key)) continue;
        const segments = [...trail, key];
        const value = safeGet(node, key, undefined);

        if (typeof value === 'boolean') {
          pushEntry(segments, 'boolean');
        } else if (typeof value === 'number') {
          pushEntry(segments, 'number');
        } else if (typeof value === 'string') {
          pushEntry(segments, 'string');
        } else if (Array.isArray(value) || OPAQUE_OBJECT_KEYS.has(key)) {
          // Lists and opaque maps: discoverable but edited on their own page.
          pushEntry(segments, 'complex');
        } else if (isPlainObject(value)) {
          walk(value, segments);
        }
        // null / undefined leaves are skipped (cannot infer a control type).
      }
    }

    walk(formData as unknown as Record<string, unknown>, []);
    return out;
  }

  // ---------------------------------------------------------------------------
  // Reactive state
  // ---------------------------------------------------------------------------
  let searchTerm = $state('');

  let ready = $derived($settingsStore.dataLoaded);
  let loading = $derived($settingsStore.isLoading);
  let saving = $derived($settingsStore.isSaving);
  let hasChanges = $derived(
    hasSettingsChanged($settingsStore.originalData, $settingsStore.formData)
  );

  // Rebuilt whenever settings change. Cheap (a few hundred entries).
  let index = $derived(ready ? flatten($settingsStore.formData) : []);

  let allResults = $derived.by(() => {
    const q = searchTerm.trim().toLowerCase();
    if (q.length === 0) return [];
    const terms = q.split(/\s+/);
    return index.filter(entry => terms.every(term => entry.haystack.includes(term)));
  });
  let results = $derived(allResults.slice(0, MAX_RESULTS));
  let truncated = $derived(allResults.length > MAX_RESULTS);

  const applyBlockedReason = $derived.by(() => {
    if (saving) return 'Applying changes…';
    if (!hasChanges) return 'No unsaved changes to apply.';
    return '';
  });
  let canApply = $derived(hasChanges && !saving);

  onMount(() => {
    if (!$settingsStore.dataLoaded && !$settingsStore.isLoading) {
      settingsActions.loadSettings().catch(() => {
        // loadSettings already records the error on the store and toasts.
      });
    }
  });

  // ---------------------------------------------------------------------------
  // Editing
  // ---------------------------------------------------------------------------
  function commitEdit(entry: SettingEntry, newValue: boolean | number | string): void {
    const section = entry.section;
    const current = safeGet(
      $settingsStore.formData as unknown as Record<string, unknown>,
      section as string,
      undefined
    );
    if (!isPlainObject(current)) return;

    // Deep clone the whole section (settings are JSON-safe) and set the nested
    // value, preserving every sibling field. Mirrors the store's own clone idiom.
    const clone = JSON.parse(JSON.stringify(current)) as Record<string, unknown>;
    const rel = entry.segments.slice(1);
    const lastKey = rel.at(-1);
    if (lastKey === undefined) return;

    // Walk to the parent of the target field via the safe accessor.
    let node: Record<string, unknown> = clone;
    for (const key of rel.slice(0, -1)) {
      const next = safeGet(node, key, undefined);
      if (!isPlainObject(next)) return; // structure changed underneath us; bail
      node = next;
    }
    // eslint-disable-next-line security/detect-object-injection -- lastKey comes from the walked store structure, not user input
    node[lastKey] = newValue;

    updateSectionLoose(section, clone);
  }

  async function applySettings(): Promise<void> {
    if (!canApply) return;
    try {
      await settingsActions.saveSettings(); // shows success/error toast itself
    } catch {
      // saveSettings records the error on the store and toasts; nothing to add.
    }
  }

  function openSettingsPage(route: string): void {
    navigation.navigate(`/ui/settings/${route}`);
  }

  // Typed value accessors (kind already narrows the runtime type).
  const asBool = (v: unknown): boolean => v === true;
  const asNum = (v: unknown): number => (typeof v === 'number' ? v : 0);
  const asStr = (v: unknown): string => (typeof v === 'string' ? v : '');
</script>

<div class="col-span-12 space-y-4" role="region" aria-label="Search Settings">
  <!-- Search bar + Apply -->
  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <h2 class="card-title flex items-center gap-2" id="search-settings-heading">
        <SlidersHorizontal class="size-5" />
        Search Settings
      </h2>
      <p class="text-sm opacity-70" style:color="var(--color-base-content)">
        Search every setting by keyword, change it inline, then apply. New settings appear here
        automatically.
      </p>

      <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
        <div class="form-control grow">
          <label class="label" for="settings-search-input">
            <span class="label-text">Keyword</span>
          </label>
          <div class="relative">
            <span
              class="pointer-events-none absolute inset-y-0 left-3 flex items-center opacity-60"
            >
              <Search class="size-4" />
            </span>
            <input
              id="settings-search-input"
              type="search"
              class="input w-full pl-9"
              placeholder="e.g. gain, mqtt, threshold, retention…"
              bind:value={searchTerm}
              aria-describedby="settings-search-help"
            />
          </div>
          <span id="settings-search-help" class="help-text">
            Matches setting names and their location path. Space separates multiple keywords (all
            must match).
          </span>
        </div>

        <div class="flex flex-col items-stretch gap-1">
          <button
            type="button"
            class="btn btn-primary"
            disabled={!canApply}
            title={!canApply ? applyBlockedReason : 'Save all changed settings'}
            aria-describedby="apply-help"
            onclick={applySettings}
          >
            {#if saving}
              <span
                class="size-4 animate-spin rounded-full border-2 border-current border-t-transparent"
                aria-hidden="true"
              ></span>
              Applying…
            {:else}
              <Save class="size-4" />
              Apply settings
            {/if}
          </button>
          <span
            id="apply-help"
            class="text-xs"
            class:opacity-70={!hasChanges || saving}
            style:color={hasChanges && !saving
              ? 'var(--color-warning)'
              : 'var(--color-base-content)'}
            role="status"
            aria-live="polite"
          >
            {#if saving}
              Applying changes…
            {:else if hasChanges}
              You have unsaved changes.
            {:else}
              No unsaved changes.
            {/if}
          </span>
        </div>
      </div>
    </div>
  </div>

  <!-- Results -->
  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <div class="flex items-center justify-between">
        <h2 class="card-title">Search Results</h2>
        {#if searchTerm.trim() && ready}
          <span
            class="text-sm opacity-70"
            aria-live="polite"
            style:color="var(--color-base-content)"
          >
            {allResults.length} match{allResults.length === 1 ? '' : 'es'}
          </span>
        {/if}
      </div>

      {#if !ready}
        <div class="flex flex-col items-center gap-3 py-12">
          <LoadingSpinner size="lg" />
          <p class="text-sm opacity-70" style:color="var(--color-base-content)">
            {loading ? 'Loading settings…' : 'Preparing settings…'}
          </p>
        </div>
      {:else if searchTerm.trim().length === 0}
        <EmptyState
          title="Search your settings"
          description="Type a keyword above to find any setting. You can edit matches right here and apply them without leaving this page."
        />
      {:else if results.length === 0}
        <EmptyState
          title="No settings match “{searchTerm.trim()}”"
          description="Try a different or shorter keyword — for example a single word like “audio”, “mqtt”, or “threshold”."
        />
      {:else}
        <ul class="divide-y divide-[var(--color-base-300)]">
          {#each results as entry (entry.path)}
            {@const current = getPath($settingsStore.formData, entry.segments)}
            <li class="flex flex-col gap-2 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="min-w-0 grow">
                <div class="truncate text-xs opacity-60" style:color="var(--color-base-content)">
                  {entry.breadcrumb}
                </div>

                <div class="mt-1 max-w-md">
                  {#if entry.kind === 'boolean'}
                    <Checkbox
                      checked={asBool(current)}
                      label={entry.label}
                      onchange={checked => commitEdit(entry, checked)}
                    />
                  {:else if entry.kind === 'number'}
                    <NumberField
                      label={entry.label}
                      value={asNum(current)}
                      onUpdate={value => commitEdit(entry, value)}
                    />
                  {:else if entry.kind === 'string'}
                    <TextInput
                      label={entry.label}
                      value={asStr(current)}
                      type={entry.secret ? 'password' : 'text'}
                      onchange={value => commitEdit(entry, value)}
                    />
                  {:else}
                    <div class="text-sm font-medium" style:color="var(--color-base-content)">
                      {entry.label}
                    </div>
                    <div class="text-xs opacity-70" style:color="var(--color-base-content)">
                      List or grouped setting — edit it on its settings page.
                    </div>
                  {/if}
                </div>
              </div>

              {#if entry.route}
                <div class="shrink-0">
                  <button
                    type="button"
                    class="btn btn-sm btn-ghost"
                    onclick={() => entry.route && openSettingsPage(entry.route)}
                    title="Open this setting on its full settings page"
                  >
                    <ExternalLink class="size-4" />
                    Open page
                  </button>
                </div>
              {/if}
            </li>
          {/each}
        </ul>

        {#if truncated}
          <p class="mt-3 text-sm opacity-70" style:color="var(--color-base-content)">
            Showing the first {MAX_RESULTS} of {allResults.length} matches. Refine your keyword to narrow
            the list.
          </p>
        {/if}
      {/if}
    </div>
  </div>
</div>
