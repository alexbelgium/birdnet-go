<script lang="ts">
  /* eslint-disable security/detect-object-injection -- trusted settings search walks runtime config paths */
  import { RotateCcw, Save, Search, SlidersHorizontal } from '@lucide/svelte';
  import { settingsActions, settingsStore } from '$lib/stores/settings';
  import { navigation } from '$lib/stores/navigation.svelte';

  type EditableValue = string | number | boolean | string[] | number[] | boolean[];
  type ValueType = 'boolean' | 'number' | 'string' | 'array';

  interface SettingResult {
    id: string;
    path: string[];
    label: string;
    section: string;
    route: string;
    valueType: ValueType;
    sensitive: boolean;
    keywords: string;
  }

  const SECTION_ROUTES: Record<string, { label: string; route: string }> = {
    main: { label: 'Node', route: '/ui/settings/main' },
    birdnet: { label: 'Analysis', route: '/ui/settings/analysis' },
    bat: { label: 'Analysis', route: '/ui/settings/analysis' },
    realtime: { label: 'Analysis', route: '/ui/settings/analysis' },
    webServer: { label: 'Security', route: '/ui/settings/security' },
    security: { label: 'Security', route: '/ui/settings/security' },
    sentry: { label: 'Support', route: '/ui/settings/support' },
    output: { label: 'Node', route: '/ui/settings/main' },
    backup: { label: 'Node', route: '/ui/settings/main' },
    notification: { label: 'Notifications', route: '/ui/settings/notifications' },
    taxonomySynonyms: { label: 'Species', route: '/ui/settings/species' },
  };

  const REALTIME_ROUTES: Record<string, { label: string; route: string }> = {
    audio: { label: 'Audio', route: '/ui/settings/audio' },
    rtsp: { label: 'Audio', route: '/ui/settings/audio' },
    dashboard: { label: 'User Interface', route: '/ui/settings/userinterface' },
    privacyFilter: { label: 'Detection Filters', route: '/ui/settings/detectionfilters' },
    dogBarkFilter: { label: 'Detection Filters', route: '/ui/settings/detectionfilters' },
    daylightFilter: { label: 'Detection Filters', route: '/ui/settings/detectionfilters' },
    birdweather: { label: 'Integrations', route: '/ui/settings/integrations' },
    mqtt: { label: 'Integrations', route: '/ui/settings/integrations' },
    telemetry: { label: 'Integrations', route: '/ui/settings/integrations' },
    weather: { label: 'Integrations', route: '/ui/settings/integrations' },
    ebird: { label: 'Integrations', route: '/ui/settings/integrations' },
    species: { label: 'Species', route: '/ui/settings/species' },
    speciesTracking: { label: 'Species', route: '/ui/settings/species' },
    dynamicThreshold: { label: 'Analysis', route: '/ui/settings/analysis' },
    falsePositiveFilter: { label: 'Analysis', route: '/ui/settings/analysis' },
    extendedCapture: { label: 'Analysis', route: '/ui/settings/analysis' },
  };

  const SENSITIVE_RE = /(password|secret|apiKey|apikey|token|dsn|clientSecret)/i;
  const MAX_RESULTS = 80;

  let searchTerm = $state('');
  let saveError = $state('');

  function humanize(value: string): string {
    return value
      .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
      .replace(/[_-]+/g, ' ')
      .replace(/\b\w/g, match => match.toUpperCase());
  }

  function routeForPath(path: string[]): { label: string; route: string } {
    if (path[0] === 'realtime' && path[1]) {
      return REALTIME_ROUTES[path[1]] ?? SECTION_ROUTES.realtime;
    }
    return SECTION_ROUTES[path[0]] ?? { label: humanize(path[0] ?? 'Settings'), route: '/ui/settings/main' };
  }

  function isEditableArray(value: unknown): value is string[] | number[] | boolean[] {
    return Array.isArray(value) && value.every(item => ['string', 'number', 'boolean'].includes(typeof item));
  }

  function cloneSettings<T>(value: T): T {
    return JSON.parse(JSON.stringify(value)) as T;
  }

  function getValueAtPath(source: unknown, path: string[]): unknown {
    return path.reduce<unknown>((current, key) => {
      if (!current || typeof current !== 'object') return undefined;
      return (current as Record<string, unknown>)[key];
    }, source);
  }

  function setValueAtPath(target: Record<string, unknown>, path: string[], value: unknown): void {
    let current: Record<string, unknown> = target;
    for (let i = 0; i < path.length - 1; i += 1) {
      const key = path[i];
      const next = current[key];
      if (!next || typeof next !== 'object') current[key] = {};
      current = current[key] as Record<string, unknown>;
    }
    current[path[path.length - 1]] = value;
  }

  function updateSetting(result: SettingResult, value: EditableValue): void {
    settingsStore.update(state => {
      const formData = cloneSettings(state.formData) as Record<string, unknown>;
      setValueAtPath(formData, result.path, value);
      return { ...state, formData: formData as typeof state.formData };
    });
  }

  function updateArraySetting(result: SettingResult, rawValue: string): void {
    const current = currentValue(result);
    const values = rawValue
      .split(/[\n,]/)
      .map(item => item.trim())
      .filter(Boolean);

    if (Array.isArray(current) && current.every(item => typeof item === 'number')) {
      updateSetting(
        result,
        values.map(item => Number(item)).filter(Number.isFinite)
      );
      return;
    }

    if (Array.isArray(current) && current.every(item => typeof item === 'boolean')) {
      updateSetting(result, values.map(item => item.toLowerCase() === 'true'));
      return;
    }

    updateSetting(result, values);
  }

  function collectSettings(source: unknown, path: string[] = [], results: SettingResult[] = []): SettingResult[] {
    if (!source || typeof source !== 'object') return results;

    for (const [key, value] of Object.entries(source as Record<string, unknown>)) {
      if (key === 'version' || key === 'buildDate' || key === 'systemId') continue;
      const nextPath = [...path, key];
      const route = routeForPath(nextPath);
      const sensitive = SENSITIVE_RE.test(nextPath.join('.'));
      const pathLabel = nextPath.map(humanize).join(' / ');
      const base = {
        id: nextPath.join('.'),
        path: nextPath,
        label: humanize(key),
        section: route.label,
        route: route.route,
        sensitive,
      };

      if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
        results.push({
          ...base,
          valueType: typeof value,
          keywords: [pathLabel, route.label, sensitive ? '' : String(value)].join(' ').toLowerCase(),
        });
        continue;
      }

      if (isEditableArray(value)) {
        results.push({
          ...base,
          valueType: 'array',
          keywords: [pathLabel, route.label, sensitive ? '' : value.join(' ')].join(' ').toLowerCase(),
        });
        continue;
      }

      collectSettings(value, nextPath, results);
    }

    return results;
  }

  const allResults = $derived(collectSettings($settingsStore.formData));
  const filteredResults = $derived.by(() => {
    const terms = searchTerm
      .trim()
      .toLowerCase()
      .split(/\s+/)
      .filter(Boolean);

    if (terms.length === 0) return [];
    return allResults.filter(result => terms.every(term => result.keywords.includes(term))).slice(0, MAX_RESULTS);
  });

  async function applySettings(): Promise<void> {
    saveError = '';
    try {
      await settingsActions.saveSettings();
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Settings save failed';
    }
  }

  function resetSettings(): void {
    settingsActions.resetAllSettings();
  }

  function openSettings(result: SettingResult): void {
    navigation.navigate(result.route);
  }

  function currentValue(result: SettingResult): EditableValue {
    return getValueAtPath($settingsStore.formData, result.path) as EditableValue;
  }
</script>

<div class="col-span-12 space-y-4" role="region" aria-label="Search Settings">
  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <h2 class="card-title" id="settings-search-heading">Search Settings</h2>

      <form
        class="space-y-4"
        onsubmit={event => {
          event.preventDefault();
        }}
        aria-labelledby="settings-search-heading"
      >
        <div class="form-control">
          <label class="label" for="settingsSearch">
            <span class="label-text">Keyword</span>
          </label>
          <input
            type="text"
            id="settingsSearch"
            bind:value={searchTerm}
            placeholder="Search threshold, mqtt, locale, audio, notifications..."
            class="input w-full"
          />
        </div>

        <div class="flex flex-row gap-4 justify-end">
          <button type="button" class="btn btn-ghost shrink-0" onclick={resetSettings}>
            <RotateCcw class="size-5 mr-2" />
            Reset
          </button>
          <button
            type="button"
            class="btn btn-primary shrink-0"
            onclick={applySettings}
            disabled={$settingsStore.isSaving}
          >
            {#if $settingsStore.isSaving}
              <span class="loading loading-spinner loading-sm mr-2" aria-hidden="true"></span>
            {:else}
              <Save class="size-5 mr-2" />
            {/if}
            Apply Settings
          </button>
        </div>
      </form>

      {#if saveError}
        <div class="alert alert-error mt-4" role="alert">{saveError}</div>
      {/if}
    </div>
  </div>

  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <div class="flex items-center justify-between">
        <h2 class="card-title" id="settings-search-results-heading">Search Results</h2>
        {#if searchTerm.trim()}
          <span class="text-sm text-[var(--color-base-content)] opacity-70" aria-live="polite">
            {filteredResults.length} result{filteredResults.length === 1 ? '' : 's'}
          </span>
        {/if}
      </div>

      {#if !searchTerm.trim()}
        <div
          class="mt-6 bg-[var(--color-base-200)] rounded-lg p-4 flex flex-col items-center justify-center min-h-[200px]"
          aria-labelledby="settings-search-results-heading"
        >
          <span class="text-[var(--color-base-content)] opacity-30 text-[4rem]" aria-hidden="true">
            <Search class="size-12" />
          </span>
          <p class="text-[var(--color-base-content)] opacity-50 text-center mt-4">
            Search settings by keyword.
          </p>
          <p class="text-[var(--color-base-content)] opacity-50 text-center text-sm">
            Matching editable settings appear here. Change values, then Apply Settings.
          </p>
        </div>
      {:else if filteredResults.length === 0}
        <div
          class="mt-6 bg-[var(--color-base-200)] rounded-lg p-4 flex flex-col items-center justify-center min-h-[200px]"
        >
          <SlidersHorizontal class="size-12 opacity-30" />
          <p class="text-[var(--color-base-content)] opacity-50 text-center mt-4">No setting found.</p>
        </div>
      {:else}
        <div class="overflow-x-auto mt-4 hidden md:block" aria-labelledby="settings-search-results-heading">
          <table class="table w-full">
            <thead>
              <tr>
                <th scope="col">Setting</th>
                <th scope="col">Section</th>
                <th scope="col">Value</th>
                <th scope="col">Action</th>
              </tr>
            </thead>
            <tbody>
              {#each filteredResults as result (result.id)}
                {@const value = currentValue(result)}
                <tr>
                  <td>
                    <div class="font-medium">{result.label}</div>
                    <div class="text-xs opacity-60 font-mono">{result.path.join('.')}</div>
                  </td>
                  <td>{result.section}</td>
                  <td class="min-w-64">
                    {#if result.valueType === 'boolean'}
                      <input
                        type="checkbox"
                        class="toggle toggle-primary"
                        checked={Boolean(value)}
                        onchange={event => updateSetting(result, event.currentTarget.checked)}
                      />
                    {:else if result.valueType === 'number'}
                      <input
                        type="number"
                        class="input input-sm w-full"
                        value={Number(value)}
                        oninput={event => updateSetting(result, Number(event.currentTarget.value))}
                      />
                    {:else if result.valueType === 'array'}
                      <textarea
                        class="textarea textarea-sm w-full font-mono"
                        rows="2"
                        value={Array.isArray(value) ? value.join('\n') : ''}
                        oninput={event => updateArraySetting(result, event.currentTarget.value)}
                      ></textarea>
                    {:else}
                      <input
                        type={result.sensitive ? 'password' : 'text'}
                        class="input input-sm w-full"
                        value={String(value ?? '')}
                        oninput={event => updateSetting(result, event.currentTarget.value)}
                      />
                    {/if}
                  </td>
                  <td>
                    <button type="button" class="btn btn-sm btn-outline" onclick={() => openSettings(result)}>
                      Open section
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        <div class="mt-4 grid gap-3 md:hidden">
          {#each filteredResults as result (result.id)}
            {@const value = currentValue(result)}
            <div class="rounded-lg bg-[var(--color-base-200)] p-4 space-y-3">
              <div>
                <div class="font-medium">{result.label}</div>
                <div class="text-xs opacity-60 font-mono">{result.path.join('.')}</div>
                <div class="text-xs opacity-70 mt-1">{result.section}</div>
              </div>

              {#if result.valueType === 'boolean'}
                <input
                  type="checkbox"
                  class="toggle toggle-primary"
                  checked={Boolean(value)}
                  onchange={event => updateSetting(result, event.currentTarget.checked)}
                />
              {:else if result.valueType === 'number'}
                <input
                  type="number"
                  class="input input-sm w-full"
                  value={Number(value)}
                  oninput={event => updateSetting(result, Number(event.currentTarget.value))}
                />
              {:else if result.valueType === 'array'}
                <textarea
                  class="textarea textarea-sm w-full font-mono"
                  rows="3"
                  value={Array.isArray(value) ? value.join('\n') : ''}
                  oninput={event => updateArraySetting(result, event.currentTarget.value)}
                ></textarea>
              {:else}
                <input
                  type={result.sensitive ? 'password' : 'text'}
                  class="input input-sm w-full"
                  value={String(value ?? '')}
                  oninput={event => updateSetting(result, event.currentTarget.value)}
                />
              {/if}

              <button type="button" class="btn btn-sm btn-outline w-full" onclick={() => openSettings(result)}>
                Open section
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>
