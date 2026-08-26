<!--
DailySummaryCard.svelte - Daily bird species detection summary table

Purpose:
- Displays daily bird species summaries with hourly detection counts
- Provides interactive heatmap visualization of detection patterns
- Supports date navigation and real-time updates via SSE
- Integrates sun times to highlight sunrise/sunset hours

Features:
- Progressive loading states (skeleton → spinner → loaded/error)
- Responsive hourly/bi-hourly column grouping based on viewport
- Color-coded heatmap cells showing detection intensity
- Daylight visualization row showing sunrise/sunset times
- Species badges with colored initials (GitHub-style heatmap design)
- Real-time animation for new species and count increases
- URL memoization with LRU cache for performance optimization
- Heatmap legend showing intensity scale (Less → More)
- Date picker navigation with keyboard shortcuts
- Clickable cells linking to detailed detection views

Props:
- data: DailySpeciesSummary[] - Array of species detection summaries
- loading?: boolean - Loading state indicator (default: false)
- error?: string | null - Error message to display (default: null)
- selectedDate: string - Currently selected date in YYYY-MM-DD format
- showThumbnails?: boolean - Show thumbnails or colored badge placeholders (default: true)
- onPreviousDay: () => void - Callback for previous day navigation
- onNextDay: () => void - Callback for next day navigation
- onGoToToday: () => void - Callback for "today" button click
- onDateChange: (date: string) => void - Callback for date picker changes

Performance Optimizations:
- $state.raw() for static data structures (caches, render functions)
- $derived.by() for complex reactive calculations
- LRU cache for URL memoization (500 entries max)
- Optimized animation cleanup with requestAnimationFrame
- Efficient data sorting and max count calculations

Responsive Breakpoints:
- Wide (≥1600px): All hourly columns, taller heatmap cells
- Desktop (1024-1599px): All hourly columns visible
- Tablet (768-1023px): Bi-hourly columns only
- Mobile (<768px): compact MobileSummaryTable replaces the heatmap grid
-->

<script lang="ts">
  import DatePicker from '$lib/desktop/components/ui/DatePicker.svelte';
  import SkeletonDailySummary from '$lib/desktop/components/ui/SkeletonDailySummary.svelte';
  import { t } from '$lib/i18n';
  import type { Component } from 'svelte';
  import type { DailySpeciesSummary, LatestWeatherResponse } from '$lib/types/detection.types';
  import { getLocalDateString, getDateInTimezone } from '$lib/utils/date';
  import {
    buildHourlyDetectionUrl,
    buildSpeciesDetectionUrl,
    buildSpeciesHourUrl,
  } from '$lib/utils/detectionUrls';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { loggers } from '$lib/utils/logger';
  import { LRUCache } from '$lib/utils/LRUCache';
  import { getStoredValue, setStoredValue } from '$lib/utils/storage';
  import type { MobileSortKey, MobileSortDir } from './daily-summary/MobileSummaryTable.svelte';
  import { safeArrayAccess, safeGet } from '$lib/utils/security';
  import {
    WEATHER_ICON_MAP,
    UNKNOWN_WEATHER_INFO,
    getEffectiveWeatherCode,
    translateWeatherCondition,
    isNightTime,
  } from '$lib/utils/weather';
  import {
    convertTemperature,
    getTemperatureSymbol,
    type TemperatureUnit,
  } from '$lib/utils/formatters';
  import { dashboardSettings, speciesTrackingSettings } from '$lib/stores/settings';
  import {
    resolveNoveltyCategory,
    noveltyCategoryColorVar,
  } from '$lib/desktop/features/dashboard/utils/noveltyCategory';
  import {
    ChevronDown,
    ChevronLeft,
    ChevronRight,
    History,
    Star,
    XCircle,
    Sun,
    Moon,
    CloudSun,
    Cloud,
    CloudDrizzle,
    CloudRain,
    CloudLightning,
    CloudSnow,
    Snowflake,
    CloudFog,
  } from '@lucide/svelte';
  import { untrack } from 'svelte';
  import AnimatedCounter from './AnimatedCounter.svelte';
  import BirdThumbnailPopup from './BirdThumbnailPopup.svelte';
  import SunTimeTooltip from './SunTimeTooltip.svelte';
  import DailySummaryOverview from './daily-summary/DailySummaryOverview.svelte';
  import DailySummaryStatColumns from './daily-summary/DailySummaryStatColumns.svelte';
  import MobileSummaryTable from './daily-summary/MobileSummaryTable.svelte';
  import SpeciesDetailCard from './daily-summary/SpeciesDetailCard.svelte';
  import SpeciesEbirdLink from './daily-summary/SpeciesEbirdLink.svelte';
  import TaxonFilterDropdown from './daily-summary/TaxonFilterDropdown.svelte';
  import {
    getSpeciesBadgeColor,
    getSpeciesInitials,
  } from '$lib/desktop/features/dashboard/utils/speciesBadge';
  import {
    filterByTaxon,
    taxonCounts,
    type TaxonFilter,
  } from '$lib/desktop/features/dashboard/utils/taxonFilter';

  const logger = loggers.ui;

  // Progressive loading timing constants (optimized for Svelte 5)
  const LOADING_PHASES = $state.raw({
    skeleton: 0, // 0ms - show skeleton immediately to reserve space
    spinner: 650, // 650ms - show spinner if still loading
  });

  // Heatmap scaling configuration
  // MAX_HEAT_COUNT: detection count at which maximum intensity (9) is reached
  // INTENSITY_LEVELS: number of color intensity levels (1-9, plus 0 for empty)
  const HEATMAP_CONFIG = {
    MAX_HEAT_COUNT: 50,
    INTENSITY_LEVELS: 9,
  } as const;

  // Consolidated configuration for magic numbers
  const CONFIG = {
    CACHE: {
      SUN_TIMES_MAX_ENTRIES: 30, // Max days of sun times to cache
      URL_MAX_ENTRIES: 500, // Max URLs to cache for memoization
    },
    DAYLIGHT: {
      DAWN_DUSK_HOURS_OFFSET: 2, // Hours before sunrise / after sunset for pre-dawn/dusk
      MIDDAY_INTENSITY_THRESHOLD: 0.3, // Distance from midday for "mid-day" classification
      DAY_INTENSITY_THRESHOLD: 0.7, // Distance from midday for "day" classification
      DEEP_NIGHT_END: 4, // Hour when deep night ends (0-4)
      DEEP_NIGHT_START: 21, // Hour when deep night starts (21-23)
      NIGHT_MORNING: 5, // Morning twilight hour
      NIGHT_EVENING: 20, // Evening twilight hour
    },
    QUERY: {
      DEFAULT_NUM_RESULTS: 25, // Default number of results for detection queries
    },
    SKELETON: {
      SPECIES_COUNT: 8, // Number of skeleton rows to show during loading
    },
    SPECIES_COLUMN: {
      BASE_WIDTH: 5.5, // rem - thumbnail (2) + gap (0.5) + padding (1) + ebird icon (0.75) + gap (0.5) + buffer (0.75)
      CHAR_WIDTH: 0.52, // rem per character for text-sm font
      MIN_WIDTH: 9, // rem - minimum column width
      MAX_WIDTH: 22, // rem - maximum column width (prevents excessive width)
    },
  } as const;

  interface SunTimes {
    sunrise: string; // ISO date string
    sunset: string; // ISO date string
    timezone: string; // IANA timezone name from server
  }

  // Hourly weather data from API
  interface HourlyWeatherResponse {
    time: string; // "HH:mm:ss"
    temperature: number;
    weather_main?: string;
    weather_desc?: string; // yr.no symbol like "partlycloudy_night"
    weather_icon?: string; // icon code or "unknown"
  }

  // Column type definitions
  interface BaseColumn {
    key: string;
    header?: string;
    className?: string;
    align?: string;
  }

  interface SpeciesColumn extends BaseColumn {
    type: 'species';
    sortable: boolean;
  }

  interface HourlyColumn extends BaseColumn {
    type: 'hourly';
    hour: number;
    align: string;
  }

  interface BiHourlyColumn extends BaseColumn {
    type: 'bi-hourly';
    hour: number;
    align: string;
  }

  type ColumnDefinition = SpeciesColumn | HourlyColumn | BiHourlyColumn;

  // URL builder types
  interface URLBuilders {
    species: (_species: DailySpeciesSummary) => string;
    speciesHour: (_species: DailySpeciesSummary, _hour: number, _duration?: number) => string;
    hourly: (_hour: number, _duration?: number) => string;
  }

  interface Props {
    data: DailySpeciesSummary[];
    loading?: boolean;
    error?: string | null;
    selectedDate: string;
    showThumbnails?: boolean;
    speciesLimit?: number;
    onPreviousDay: () => void;
    onNextDay: () => void;
    onGoToToday: () => void;
    onDateChange: (_date: string) => void;
    onServerTimezone?: (_timezone: string) => void;
  }

  let {
    data = [],
    loading = false,
    error = null,
    selectedDate,
    showThumbnails = true,
    speciesLimit = 0,
    onPreviousDay,
    onNextDay,
    onGoToToday,
    onDateChange,
    onServerTimezone,
  }: Props = $props();

  // Progressive loading state management
  let loadingPhase = $state<'skeleton' | 'spinner' | 'loaded' | 'error'>('skeleton');
  let showDelayedIndicator = $state(false);

  // Sun times state
  let sunTimes = $state<SunTimes | null>(null);

  // Server timezone from sun times API (used for date constraints)
  let serverTimezone = $state('');

  // Hourly weather state
  let hourlyWeather = $state<HourlyWeatherResponse[]>([]);
  // Map for O(1) hour lookup (populated when hourlyWeather changes)
  let hourlyWeatherMap = $state(new Map<number, HourlyWeatherResponse>());

  // Temperature unit preference (from dashboard settings store, consistent with BannerCard/System/Search)
  let temperatureUnit: TemperatureUnit = $derived(
    $dashboardSettings?.temperatureUnit === 'fahrenheit' ? 'imperial' : 'metric'
  );

  // Cache for sun times to avoid repeated API calls - use LRUCache to limit memory usage
  const sunTimesCache = $state.raw(
    new LRUCache<string, SunTimes>(CONFIG.CACHE.SUN_TIMES_MAX_ENTRIES)
  );

  // Cache for hourly weather to avoid repeated API calls
  const hourlyWeatherCache = $state.raw(
    new LRUCache<string, HourlyWeatherResponse[]>(CONFIG.CACHE.SUN_TIMES_MAX_ENTRIES)
  );

  // Optimize loading state management with proper dependency tracking
  $effect(() => {
    if (loading) {
      loadingPhase = 'skeleton'; // Show skeleton immediately to reserve space
      showDelayedIndicator = false;

      // Use untrack to prevent the timer from becoming a reactive dependency
      const spinnerTimer = setTimeout(() => {
        if (untrack(() => loading)) {
          loadingPhase = 'spinner';
          showDelayedIndicator = true;
        }
      }, LOADING_PHASES.spinner);

      return () => {
        clearTimeout(spinnerTimer);
      };
    } else {
      loadingPhase = error ? 'error' : 'loaded';
      showDelayedIndicator = false;
    }
  });

  // Fetch sun times from weather API with caching
  async function fetchSunTimes(date: string): Promise<SunTimes | null> {
    // Check cache first using LRUCache methods
    const cached = sunTimesCache.get(date);
    if (cached) {
      return cached;
    }

    try {
      const response = await fetch(buildAppUrl(`/api/v2/weather/sun/${date}`));
      if (!response.ok) {
        const errorMsg = `Failed to fetch sun times: ${response.status} ${response.statusText}`;
        logger.warn(errorMsg);
        return null;
      }
      const responseData = await response.json();
      const sunTimesData: SunTimes = {
        sunrise: responseData.sunrise,
        sunset: responseData.sunset,
        timezone: responseData.timezone ?? '',
      };

      // Cache the result
      sunTimesCache.set(date, sunTimesData);

      return sunTimesData;
    } catch (fetchError) {
      const errorMsg =
        fetchError instanceof Error ? fetchError.message : 'Unknown error fetching sun times';
      logger.warn('Error fetching sun times:', errorMsg);
      return null;
    }
  }

  // Update sun times when selected date changes
  // Uses captured date to prevent stale data from overwriting fresh data on rapid date changes
  $effect(() => {
    const currentDate = selectedDate;
    if (currentDate) {
      fetchSunTimes(currentDate).then(times => {
        // Only update if this is still the current date (prevents race condition)
        if (selectedDate === currentDate) {
          sunTimes = times;
          // Propagate server timezone to parent for date navigation constraints
          if (times?.timezone && times.timezone !== serverTimezone) {
            serverTimezone = times.timezone;
            onServerTimezone?.(times.timezone);
          }
        }
      });
    }
  });

  // Fetch hourly weather data from API with caching
  async function fetchHourlyWeather(date: string): Promise<HourlyWeatherResponse[]> {
    // Validate date format (YYYY-MM-DD) before making API request
    const dateRegex = /^\d{4}-\d{2}-\d{2}$/;
    if (!dateRegex.test(date)) {
      logger.warn(`Invalid date format provided to fetchHourlyWeather: ${date}`);
      return [];
    }

    // Check cache first
    const cached = hourlyWeatherCache.get(date);
    if (cached) {
      return cached;
    }

    try {
      const response = await fetch(buildAppUrl(`/api/v2/weather/hourly/${date}`));
      if (!response.ok) {
        logger.warn(`Failed to fetch hourly weather: ${response.status} ${response.statusText}`);
        return [];
      }
      const responseData = await response.json();
      const weatherData: HourlyWeatherResponse[] = responseData.data || [];

      // Cache the result
      hourlyWeatherCache.set(date, weatherData);

      return weatherData;
    } catch (fetchError) {
      const errorMsg =
        fetchError instanceof Error ? fetchError.message : 'Unknown error fetching hourly weather';
      logger.warn('Error fetching hourly weather:', errorMsg);
      return [];
    }
  }

  // Update hourly weather when selected date changes.
  // Uses captured date to prevent stale data from overwriting fresh data on rapid date changes.
  // On desktop the heatmap renders the full hourly weather row immediately. On mobile only the
  // detail card's peak-hour line uses it (and only once a row is expanded), so there the fetch is
  // deferred to browser idle time to stay off the first-paint critical path.
  $effect(() => {
    const currentDate = selectedDate;
    if (!currentDate) return;

    const run = () => {
      fetchHourlyWeather(currentDate).then(data => {
        // Only update if this is still the current date (prevents race condition)
        if (selectedDate === currentDate) {
          hourlyWeather = data;
          // Build hour-keyed map for O(1) lookup
          hourlyWeatherMap = new Map(
            data
              .filter(
                (w): w is typeof w & { time: string } =>
                  !!w && typeof w.time === 'string' && w.time.includes(':')
              )
              .map(w => [parseInt(w.time.split(':')[0], 10), w])
          );
        }
      });
    };

    if (!isMobileViewport) {
      run();
      return;
    }
    // Mobile: defer to idle so it never competes with first paint.
    if (typeof globalThis.requestIdleCallback === 'function') {
      const id = globalThis.requestIdleCallback(run, { timeout: 2000 });
      return () => globalThis.cancelIdleCallback(id);
    }
    const id = setTimeout(run, 300);
    return () => clearTimeout(id);
  });

  // Calculate which hour column corresponds to sunrise/sunset.
  // Parses the hour directly from the RFC3339 string to preserve the server's timezone.
  // Using new Date().getHours() would convert to browser local time, causing wrong
  // column placement when browser and server are in different timezones (#3005).
  const getSunHourFromTime = (timeStr: string): number | null => {
    if (!timeStr) return null;
    try {
      const tIndex = timeStr.indexOf('T');
      if (tIndex === -1) return null;
      const hour = parseInt(timeStr.substring(tIndex + 1, tIndex + 3), 10);
      return isNaN(hour) || hour < 0 || hour > 23 ? null : hour;
    } catch (parseError) {
      logger.error('Error parsing time', parseError, { timeStr });
      return null;
    }
  };

  // Pre-computed sunrise/sunset hours to avoid recalculating in template loops
  const sunriseHour = $derived(sunTimes ? getSunHourFromTime(sunTimes.sunrise) : null);
  const sunsetHour = $derived(sunTimes ? getSunHourFromTime(sunTimes.sunset) : null);

  // Find weather data for a specific hour (O(1) map lookup)
  const getHourlyWeatherData = (hour: number): HourlyWeatherResponse | undefined => {
    return hourlyWeatherMap.get(hour);
  };

  // Get weather emoji for a specific hour
  const getHourlyWeatherEmoji = (hour: number): string => {
    const hourData = getHourlyWeatherData(hour);
    if (!hourData) return '';

    const iconCode = getEffectiveWeatherCode(hourData.weather_icon, hourData.weather_desc);
    if (!iconCode) return '';

    // Determine if it's night based on hour relative to sunrise/sunset
    // Fallback checks both OpenWeatherMap 'n' suffix and yr.no '_night' suffix in description
    const isNight =
      sunriseHour !== null && sunsetHour !== null
        ? hour < sunriseHour || hour >= sunsetHour
        : (hourData.weather_icon?.endsWith('n') ?? false) ||
          (hourData.weather_desc?.includes('_night') ?? false);

    const weatherInfo = safeGet(WEATHER_ICON_MAP, iconCode, UNKNOWN_WEATHER_INFO);
    return isNight ? weatherInfo.night : weatherInfo.day;
  };

  // Get tooltip text for hourly weather
  const getHourlyWeatherTooltip = (hour: number): string => {
    const hourData = getHourlyWeatherData(hour);
    if (!hourData) return '';

    // Translate raw weather description to human-readable text
    const rawDesc = hourData.weather_main || hourData.weather_desc || '';
    const desc = translateWeatherCondition(rawDesc);

    // Convert temperature from Celsius (API storage) to user's preferred unit
    let temp = '';
    if (hourData.temperature !== undefined) {
      const convertedTemp = convertTemperature(hourData.temperature, temperatureUnit);
      const symbol = getTemperatureSymbol(temperatureUnit);
      temp = `${convertedTemp.toFixed(1)}${symbol}`;
    }

    return [desc, temp].filter(Boolean).join(', ');
  };

  // Compact per-hour weather (emoji + rounded temperature) for the mobile detail
  // card's peak-hour line. Undefined when no weather is known for that hour.
  const getHourWeather = (hour: number): { emoji: string; tempText: string } | undefined => {
    const emoji = getHourlyWeatherEmoji(hour);
    const hourData = getHourlyWeatherData(hour);
    const tempText =
      hourData && typeof hourData.temperature === 'number'
        ? `${Math.round(convertTemperature(hourData.temperature, temperatureUnit))}${getTemperatureSymbol(temperatureUnit)}`
        : '';
    if (!emoji && !tempText) return undefined;
    return { emoji, tempText };
  };

  // Get daylight class for an hour based on its position relative to sunrise/sunset
  // Returns: 'deep-night', 'night', 'pre-dawn', 'sunrise', 'early-day', 'day', 'mid-day', 'late-day', 'sunset', 'dusk', 'evening'
  const getDaylightClass = (hour: number): string => {
    const { DAWN_DUSK_HOURS_OFFSET, MIDDAY_INTENSITY_THRESHOLD, DAY_INTENSITY_THRESHOLD } =
      CONFIG.DAYLIGHT;
    const { DEEP_NIGHT_END, DEEP_NIGHT_START, NIGHT_MORNING, NIGHT_EVENING } = CONFIG.DAYLIGHT;

    // Use pre-computed derived values for performance
    if (sunriseHour === null || sunsetHour === null) return 'night';

    // Sunrise hour - special gradient
    if (hour === sunriseHour) return 'sunrise';
    // Sunset hour - special gradient
    if (hour === sunsetHour) return 'sunset';

    // Pre-dawn (hours before sunrise)
    if (hour >= sunriseHour - DAWN_DUSK_HOURS_OFFSET && hour < sunriseHour) return 'pre-dawn';

    // Dusk (hours after sunset)
    if (hour > sunsetHour && hour <= sunsetHour + DAWN_DUSK_HOURS_OFFSET) return 'dusk';

    // Daylight hours
    if (hour > sunriseHour && hour < sunsetHour) {
      const midday = (sunriseHour + sunsetHour) / 2;
      const distanceFromMidday = Math.abs(hour - midday);
      const daylightDuration = (sunsetHour - sunriseHour) / 2;

      // Categorize daylight intensity
      if (distanceFromMidday < daylightDuration * MIDDAY_INTENSITY_THRESHOLD) return 'mid-day';
      if (distanceFromMidday < daylightDuration * DAY_INTENSITY_THRESHOLD) return 'day';
      return hour < midday ? 'early-day' : 'late-day';
    }

    // Night hours - vary by distance from midnight
    if (hour >= 0 && hour <= DEEP_NIGHT_END) return 'deep-night';
    if (hour >= DEEP_NIGHT_START && hour <= 23) return 'deep-night';
    if (hour === NIGHT_MORNING || hour === NIGHT_EVENING) return 'night';
    return 'evening';
  };

  /**
   * Calculate heatmap intensity using simple fixed-range scaling.
   * Maps detection counts evenly across intensity levels 1-9 based on HEATMAP_CONFIG.
   * - 0 detections → intensity 0 (empty cell)
   * - 1-6 detections → intensity 1
   * - 7-12 detections → intensity 2
   * - ...
   * - 45-50 detections → intensity 9
   * - 50+ detections → intensity 9
   *
   * @param count - The detection count for this cell
   * @returns Intensity value from 0-9
   */
  const getHeatmapIntensity = (count: number): number => {
    if (count <= 0) return 0;
    const { MAX_HEAT_COUNT, INTENSITY_LEVELS } = HEATMAP_CONFIG;
    const stepSize = MAX_HEAT_COUNT / INTENSITY_LEVELS;
    return Math.min(INTENSITY_LEVELS, Math.max(1, Math.ceil(count / stepSize)));
  };

  // Static column metadata - use $state.raw() for performance (no deep reactivity needed)
  const staticColumnDefs = $state.raw<ColumnDefinition[]>([
    {
      key: 'common_name',
      type: 'species' as const,
      sortable: true,
      className: 'font-medium whitespace-nowrap species-column',
    },
    // Progress bar column removed to save horizontal space - see mockup design
    ...Array.from({ length: 24 }, (_, hour) => ({
      key: `hour_${hour}`,
      type: 'hourly' as const,
      hour,
      header: hour.toString().padStart(2, '0'),
      align: 'center',
      className: 'hour-data hourly-count px-0',
    })),
    ...Array.from({ length: 12 }, (_, i) => {
      const hour = i * 2;
      return {
        key: `bi_hour_${hour}`,
        type: 'bi-hourly' as const,
        hour,
        header: hour.toString().padStart(2, '0'),
        align: 'center',
        className: 'hour-data bi-hourly-count bi-hourly px-0',
      };
    }),
  ]);

  // Reactive columns with only dynamic headers - use $derived.by for complex logic
  const columns = $derived.by((): ColumnDefinition[] => {
    // Early return for empty data to prevent unnecessary calculations
    if (staticColumnDefs.length === 0) return [];

    return staticColumnDefs.map(colDef => ({
      ...colDef,
      header:
        colDef.type === 'species' ? t('dashboard.dailySummary.columns.species') : colDef.header,
    }));
  });

  // Track and log unexpected column types once (performance optimization)
  const loggedUnexpectedColumns = new Set<string>();
  $effect(() => {
    if (import.meta.env.DEV) {
      const expectedTypes = new Set(['species', 'hourly', 'bi-hourly']);

      columns.forEach(column => {
        if (!expectedTypes.has(column.type) && !loggedUnexpectedColumns.has(column.key)) {
          logger.warn('Unexpected column type detected', null, {
            columnKey: column.key,
            columnType: column.type,
            component: 'DailySummaryCard',
            action: 'columnValidation',
          });
          loggedUnexpectedColumns.add(column.key);
        }
      });
    }
  });

  // Pre-computed render functions - use $state.raw for performance (static functions)
  const renderFunctions = $state.raw({
    hourly: (item: DailySpeciesSummary, hour: number) =>
      safeArrayAccess(item.hourly_counts, hour, 0) ?? 0,
    'bi-hourly': (item: DailySpeciesSummary, hour: number) =>
      (safeArrayAccess(item.hourly_counts, hour, 0) ?? 0) +
      (safeArrayAccess(item.hourly_counts, hour + 1, 0) ?? 0),
  });

  // Phase 4: Optimized URL building with memoization for 90%+ performance improvement
  const urlCache = $state.raw(new LRUCache<string, string>(CONFIG.CACHE.URL_MAX_ENTRIES));
  const urlBuilders = $state<URLBuilders>({
    // Default functions to prevent undefined errors during initial render
    species: () => '#',
    speciesHour: () => '#',
    hourly: () => '#',
  });

  // Reactive URL builder factory - clears cache when selectedDate changes
  $effect(() => {
    // Clear cache when selectedDate changes to prevent stale URLs
    urlCache.clear();

    // Create optimized, memoized URL builders
    urlBuilders.species = (species: DailySpeciesSummary) => {
      const cacheKey = `species:${species.scientific_name}:${selectedDate}`;
      if (!urlCache.has(cacheKey)) {
        urlCache.set(
          cacheKey,
          buildSpeciesDetectionUrl(
            species.scientific_name,
            selectedDate,
            CONFIG.QUERY.DEFAULT_NUM_RESULTS,
            0
          )
        );
      }
      return urlCache.get(cacheKey)!;
    };

    urlBuilders.speciesHour = (
      species: DailySpeciesSummary,
      hour: number,
      duration: number = 1
    ) => {
      const cacheKey = `species-hour:${species.scientific_name}:${selectedDate}:${hour}:${duration}`;
      if (!urlCache.has(cacheKey)) {
        urlCache.set(
          cacheKey,
          buildSpeciesHourUrl(
            species.scientific_name,
            selectedDate,
            hour,
            duration,
            CONFIG.QUERY.DEFAULT_NUM_RESULTS,
            0
          )
        );
      }
      return urlCache.get(cacheKey)!;
    };

    urlBuilders.hourly = (hour: number, duration: number = 1) => {
      const cacheKey = `hourly:${selectedDate}:${hour}:${duration}`;
      if (!urlCache.has(cacheKey)) {
        urlCache.set(
          cacheKey,
          buildHourlyDetectionUrl(selectedDate, hour, duration, CONFIG.QUERY.DEFAULT_NUM_RESULTS, 0)
        );
      }
      return urlCache.get(cacheKey)!;
    };
  });

  // LRU cache automatically manages memory, no need for periodic cleanup

  // Minute-level tick so serverTodayDate recomputes across midnight
  let nowTick = $state(Date.now());
  $effect(() => {
    const id = setInterval(() => {
      nowTick = Date.now();
    }, 60_000);
    return () => clearInterval(id);
  });

  // Use server timezone for "today" check when available, falling back to browser local
  const serverTodayDate = $derived.by(() => {
    void nowTick;
    return serverTimezone ? getDateInTimezone(serverTimezone) : getLocalDateString();
  });
  const isToday = $derived(selectedDate === serverTodayDate);

  // Last hour (inclusive) rendered by the hourly mini charts: truncated to the
  // current hour when viewing today so no empty future bars show. nowTick keeps
  // it advancing across hour changes and past midnight.
  const chartMaxHour = $derived.by(() => {
    void nowTick;
    return isToday ? new Date().getHours() : 23;
  });

  // Desktop: scientific name of the species whose detail card is expanded
  // under its heatmap row, or null. One row at a time, mirroring mobile.
  let expandedSpecies = $state<string | null>(null);

  // Grid root, used to restore focus to a row's expand button after collapse.
  let gridEl = $state<HTMLDivElement>();

  function toggleRowExpansion(scientificName: string) {
    expandedSpecies = expandedSpecies === scientificName ? null : scientificName;
  }

  function collapseRowExpansion(scientificName: string) {
    expandedSpecies = null;
    // Return focus to the row's expand button (next frame) so keyboard users
    // keep their place after the detail card closes.
    window.requestAnimationFrame(() => {
      const escaped =
        typeof window.CSS?.escape === 'function'
          ? window.CSS.escape(scientificName)
          : scientificName;
      gridEl?.querySelector<HTMLElement>(`[data-expand-btn="${escaped}"]`)?.focus();
    });
  }

  // Absence threshold for the infrequent novelty tier; undefined disables it so
  // the category never activates when species tracking (or its infrequent
  // sub-toggle) is turned off, matching the gating in NewSpeciesHighlightsCard.
  const infrequentThresholdDays = $derived(
    $speciesTrackingSettings?.enabled === true &&
      $speciesTrackingSettings.infrequentTracking?.enabled === true
      ? ($speciesTrackingSettings.infrequentTracking.absenceDays ?? 14)
      : undefined
  );

  // Check for reduced motion preference for performance and accessibility
  const prefersReducedMotion = $derived(
    typeof window !== 'undefined'
      ? (window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches ?? false)
      : false
  );

  // Viewport-aware rendering. On mobile (<768px) only the compact
  // MobileSummaryTable is rendered; the desktop heatmap DOM is skipped
  // entirely rather than hidden with CSS. Initialized synchronously so the
  // very first render is already correct (no flash of desktop DOM on phones).
  const MOBILE_VIEWPORT_QUERY = '(max-width: 767px)';
  let isMobileViewport = $state(
    typeof window !== 'undefined' && typeof window.matchMedia === 'function'
      ? window.matchMedia(MOBILE_VIEWPORT_QUERY).matches
      : false
  );
  $effect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia(MOBILE_VIEWPORT_QUERY);
    isMobileViewport = mq.matches;
    // Structural type avoids referencing the MediaQueryListEvent browser global
    // (not in the eslint no-undef globals list); only `matches` is needed.
    const handleChange = (e: { matches: boolean }) => {
      isMobileViewport = e.matches;
    };
    mq.addEventListener('change', handleChange);
    return () => mq.removeEventListener('change', handleChange);
  });

  // ── Mobile weather stat ─────────────────────────────────────────────────────
  // Desktop/tablet get weather inside the heatmap's hourly row; the mobile table
  // has none. Surface current conditions as one extra stat in the overview bar,
  // but only for "today" on a phone (past dates have no "current" weather, and
  // the desktop already shows it). Silently absent when the weather provider is
  // off — never an error state. Mirrors BannerCard's fetch/refresh/error handling.
  let mobileWeather = $state<LatestWeatherResponse | null>(null);
  const WEATHER_REFRESH_MS = 10 * 60_000;

  $effect(() => {
    if (!isMobileViewport || !isToday) {
      mobileWeather = null;
      return;
    }
    const controller = new AbortController();
    const load = async () => {
      try {
        const resp = await fetch(buildAppUrl('/api/v2/weather/latest'), {
          signal: controller.signal,
        });
        if (!resp.ok) throw new Error('weather unavailable');
        mobileWeather = await resp.json();
      } catch (e: unknown) {
        if (e instanceof Error && e.name === 'AbortError') return;
        mobileWeather = null; // degrade quietly: no stat rather than an error
      }
    };
    load();
    const id = setInterval(load, WEATHER_REFRESH_MS);
    return () => {
      controller.abort();
      clearInterval(id);
    };
  });

  // Lucide icon standing in for a base weather code (day/night aware for clear sky).
  function weatherIconFor(code: string, night: boolean): Component {
    switch (code) {
      case '01':
        return night ? Moon : Sun;
      case '02':
      case '03':
      case '04':
        return night ? Cloud : CloudSun;
      case '09':
        return CloudDrizzle;
      case '10':
        return CloudRain;
      case '11':
        return CloudLightning;
      case '12':
        return CloudSnow;
      case '13':
        return Snowflake;
      case '50':
        return CloudFog;
      default:
        return Cloud;
    }
  }

  // Overview weather stat (icon + rounded temperature + condition label), or
  // undefined when there is nothing to show. Passed only to the mobile overview.
  const mobileWeatherStat = $derived.by(() => {
    const hourly = mobileWeather?.hourly;
    if (!hourly || typeof hourly.temperature !== 'number') return undefined;
    const code = getEffectiveWeatherCode(hourly.weather_icon, hourly.weather_desc);
    const night = isNightTime(hourly.weather_icon);
    const info = code
      ? safeGet(WEATHER_ICON_MAP, code, UNKNOWN_WEATHER_INFO)
      : UNKNOWN_WEATHER_INFO;
    const temp = Math.round(convertTemperature(hourly.temperature, temperatureUnit));
    return {
      icon: weatherIconFor(code, night),
      count: `${temp}${getTemperatureSymbol(temperatureUnit)}`,
      label: (hourly.weather_desc ?? info.description).toLowerCase(),
    };
  });

  // Swipe navigation (mobile only): a horizontal swipe on the species list
  // changes the selected day. The 2:1 horizontal-dominance test keeps normal
  // vertical scrolling from ever triggering navigation; listeners are passive
  // so native scroll is never blocked.
  const SWIPE_MIN_DX_PX = 60;
  let swipeStartX = 0;
  let swipeStartY = 0;

  function handleSwipeStart(e: TouchEvent) {
    const touch = e.touches.item(0);
    if (!touch) return;
    swipeStartX = touch.clientX;
    swipeStartY = touch.clientY;
  }

  function handleSwipeEnd(e: TouchEvent) {
    const touch = e.changedTouches.item(0);
    if (!touch) return;
    const dx = touch.clientX - swipeStartX;
    const dy = touch.clientY - swipeStartY;
    if (Math.abs(dx) < SWIPE_MIN_DX_PX || Math.abs(dx) <= 2 * Math.abs(dy)) return;
    if (dx > 0) {
      onPreviousDay();
    } else if (!isToday) {
      // Same guard as the next-day chevron: no navigating past today.
      onNextDay();
    }
  }

  // Taxon filter (Birds / Bats / Others) selected via the header dropdown.
  // Classification logic lives in utils/taxonFilter.ts; the card only stores the
  // selection and applies it to the row list and the overview counts.
  let taxonFilter = $state<TaxonFilter>('all');
  const taxonCountsValue = $derived(taxonCounts(data));
  const visibleData = $derived(filterByTaxon(data, taxonFilter));

  // All species sorted by count desc, then latest detection — no limit applied.
  // Used by MobileSummaryTable to show every detected species.
  const sortedUnlimited = $derived.by(() => {
    if (visibleData.length === 0) return [];
    return [...visibleData].sort((a: DailySpeciesSummary, b: DailySpeciesSummary) => {
      if (b.count !== a.count) return b.count - a.count;
      return (b.latest_heard ?? '').localeCompare(a.latest_heard ?? '');
    });
  });

  // Desktop heatmap view: same sort order but capped to speciesLimit for performance.
  const sortedData = $derived.by(() => {
    if (speciesLimit > 0 && sortedUnlimited.length > speciesLimit) {
      return sortedUnlimited.slice(0, speciesLimit);
    }
    return sortedUnlimited;
  });

  // Initial row cap for the compact mobile table. The daily-summary API is
  // already fetched with &limit=summaryLimit (default 30), so `data` is capped
  // server-side; this smaller cap keeps the *initial* mobile render short (each
  // row draws an SVG) and gives a real "Show all" affordance to reach the rest
  // without a long scroll past every species to the content below.
  const MOBILE_INITIAL_ROW_CAP = 20;

  // ── Mobile sort ───────────────────────────────────────────────────────────
  // The compact table lets the user re-sort via its column headers. Persisted so
  // the choice survives reloads. Desktop keeps sortedUnlimited's fixed count-desc
  // order — only the mobile table reads mobileSorted.
  const MOBILE_SORT_STORAGE_KEY = 'dashboard-mobile-sort';
  const DEFAULT_MOBILE_SORT: { key: MobileSortKey; dir: MobileSortDir } = {
    key: 'count',
    dir: 'desc',
  };

  function isMobileSortState(v: unknown): v is { key: MobileSortKey; dir: MobileSortDir } {
    if (typeof v !== 'object' || v === null) return false;
    const { key, dir } = v as Record<string, unknown>;
    return (
      (key === 'count' || key === 'name' || key === 'conf' || key === 'latest') &&
      (dir === 'asc' || dir === 'desc')
    );
  }

  let mobileSort = $state<{ key: MobileSortKey; dir: MobileSortDir }>(
    getStoredValue(MOBILE_SORT_STORAGE_KEY, DEFAULT_MOBILE_SORT, isMobileSortState)
  );

  $effect(() => {
    setStoredValue(MOBILE_SORT_STORAGE_KEY, mobileSort);
  });

  // Sensible initial direction when switching to a new column: text ascends,
  // magnitudes/recency descend (biggest / most-recent first).
  function defaultDirFor(key: MobileSortKey): MobileSortDir {
    return key === 'name' ? 'asc' : 'desc';
  }

  function handleMobileSortChange(key: MobileSortKey) {
    mobileSort =
      mobileSort.key === key
        ? { key, dir: mobileSort.dir === 'asc' ? 'desc' : 'asc' }
        : { key, dir: defaultDirFor(key) };
  }

  // Mobile table rows in the user-selected order. Falls back to count desc then
  // latest heard as a stable tiebreak so equal primary keys never jitter.
  const mobileSorted = $derived.by(() => {
    if (visibleData.length === 0) return [];
    const { key, dir } = mobileSort;
    const sign = dir === 'asc' ? 1 : -1;
    return [...visibleData].sort((a: DailySpeciesSummary, b: DailySpeciesSummary) => {
      let primary = 0;
      switch (key) {
        case 'name':
          primary = localizeSpeciesName(a.scientific_name, a.common_name).localeCompare(
            localizeSpeciesName(b.scientific_name, b.common_name)
          );
          break;
        case 'conf':
          primary = (a.max_confidence ?? 0) - (b.max_confidence ?? 0);
          break;
        case 'latest':
          primary = (a.latest_heard ?? '').localeCompare(b.latest_heard ?? '');
          break;
        default:
          primary = a.count - b.count;
          break;
      }
      if (primary !== 0) return sign * primary;
      if (b.count !== a.count) return b.count - a.count;
      return (b.latest_heard ?? '').localeCompare(a.latest_heard ?? '');
    });
  });

  // Calculate dynamic species column width based on longest name
  // This ensures all rows align properly regardless of name length
  // Uses CONFIG.SPECIES_COLUMN constants for easy adjustment
  const speciesColumnWidth = $derived.by(() => {
    const { BASE_WIDTH, CHAR_WIDTH, MIN_WIDTH, MAX_WIDTH } = CONFIG.SPECIES_COLUMN;

    if (data.length === 0) return `${MIN_WIDTH}rem`;

    // Find the longest species name
    const longestName = data.reduce(
      (longest, item) => (item.common_name.length > longest.length ? item.common_name : longest),
      ''
    );
    const maxLength = longestName.length;

    // Calculate width: base (thumbnail + gap + icons) + character width estimate
    const calculatedWidth = BASE_WIDTH + maxLength * CHAR_WIDTH;

    // Clamp between min and max
    const finalWidth = Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, calculatedWidth));

    return `${finalWidth}rem`;
  });
</script>

{#snippet navigationControls()}
  <div class="flex items-center gap-2 w-full justify-between md:w-auto md:justify-end">
    <!-- Previous day button -->
    <button
      onclick={onPreviousDay}
      class="btn btn-sm btn-ghost shrink-0"
      aria-label={t('dashboard.dailySummary.navigation.previousDay')}
    >
      <ChevronLeft class="size-5" />
    </button>

    <!-- Date picker with consistent width -->
    <DatePicker
      value={selectedDate}
      onChange={onDateChange}
      onTodayClick={onGoToToday}
      maxDate={serverTodayDate}
      className="mx-auto md:mx-2 w-auto"
    />

    <!-- Next day button -->
    <button
      onclick={onNextDay}
      class="btn btn-sm btn-ghost shrink-0"
      disabled={isToday}
      aria-label={t('dashboard.dailySummary.navigation.nextDay')}
    >
      <ChevronRight class="size-5" />
    </button>
  </div>
{/snippet}

{#snippet sunIcon(sunType: 'sunrise' | 'sunset', sunTime: string | undefined, shouldShow: boolean)}
  {#if shouldShow && sunTime}
    {@const tIdx = sunTime.indexOf('T')}
    {@const formattedTime = tIdx !== -1 ? sunTime.substring(tIdx + 1, tIdx + 6) : ''}
    {#if formattedTime}
      <SunTimeTooltip {sunType} time={formattedTime} />
    {/if}
  {/if}
{/snippet}

<!-- Live region for screen reader announcements of loading state changes -->
<div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
  {#if loadingPhase === 'skeleton'}
    {t('dashboard.dailySummary.loading.preparing')}
  {:else if loadingPhase === 'spinner'}
    {t('dashboard.dailySummary.loading.fetching')}
  {:else if loadingPhase === 'error'}
    {t('dashboard.dailySummary.loading.error')}
  {:else if loadingPhase === 'loaded'}
    {t('dashboard.dailySummary.loading.complete')}
  {/if}
</div>

<!-- Progressive loading implementation -->
{#if loadingPhase === 'skeleton'}
  <SkeletonDailySummary {showThumbnails} speciesCount={CONFIG.SKELETON.SPECIES_COUNT} />
{:else if loadingPhase === 'spinner'}
  <SkeletonDailySummary
    {showThumbnails}
    showSpinner={showDelayedIndicator}
    speciesCount={CONFIG.SKELETON.SPECIES_COUNT}
  />
{:else if loadingPhase === 'error'}
  <section
    class="daily-summary-card card col-span-12 bg-[var(--color-base-100)] shadow-sm rounded-2xl border border-border-100 overflow-visible"
  >
    <div class="px-6 py-4 border-b border-[var(--color-base-200)] overflow-visible">
      <div
        class="flex flex-col gap-2 md:flex-row md:items-center md:justify-between overflow-visible"
      >
        <div class="flex flex-col">
          <h3 class="font-semibold">{t('dashboard.dailySummary.title')}</h3>
          <p class="text-sm text-[var(--color-base-content)]/60">
            {t('dashboard.dailySummary.subtitle')}
          </p>
        </div>
        {@render navigationControls()}
      </div>
    </div>
    <div class="p-6">
      <div class="alert alert-error">
        <XCircle class="size-6" />
        <span>{error}</span>
      </div>
    </div>
  </section>
{:else if loadingPhase === 'loaded'}
  <section
    class="daily-summary-card card col-span-12 bg-[var(--color-base-100)] shadow-sm rounded-2xl border border-border-100 overflow-visible"
  >
    <!-- Card Header with Date Navigation -->
    <div class="px-6 py-4 border-b border-[var(--color-base-200)] overflow-visible">
      <div
        class="flex flex-col gap-2 md:flex-row md:items-center md:justify-between overflow-visible"
      >
        <div class="flex flex-col">
          <h3 class="font-semibold">{t('dashboard.dailySummary.title')}</h3>
          <p class="text-sm text-[var(--color-base-content)]/60">
            {t('dashboard.dailySummary.subtitle')}
          </p>
        </div>
        <div class="flex items-center gap-2 justify-between md:justify-end">
          <TaxonFilterDropdown
            value={taxonFilter}
            counts={taxonCountsValue}
            onChange={value => (taxonFilter = value)}
          />
          {@render navigationControls()}
        </div>
      </div>
    </div>

    <!-- Grid Content -->
    <div class="p-6 pt-8">
      <DailySummaryOverview data={visibleData} {selectedDate} weatherStat={mobileWeatherStat} />

      {#if isMobileViewport}
        <!-- Mobile compact table (<768px): render ONLY this so phones never
             construct the desktop heatmap DOM (24 hourly + 12 bi-hourly cells
             per species row). CSS-only hiding still builds that DOM, which was
             a major first-paint cost on mobile.
             Horizontal swipe on the list navigates between days; the wrapper
             only observes touches (passive), it is not itself a control. -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div ontouchstart={handleSwipeStart} ontouchend={handleSwipeEnd}>
          <MobileSummaryTable
            data={mobileSorted}
            {sunriseHour}
            {sunsetHour}
            getSpeciesUrl={urlBuilders.species}
            {showThumbnails}
            {selectedDate}
            maxHour={chartMaxHour}
            limit={MOBILE_INITIAL_ROW_CAP}
            sortKey={mobileSort.key}
            sortDir={mobileSort.dir}
            onSortChange={handleMobileSortChange}
            {getHourWeather}
          />
        </div>
      {:else}
        <!-- Desktop/tablet heatmap (≥768px) -->
        <div class="overflow-x-auto overflow-y-visible">
          <div
            bind:this={gridEl}
            class="daily-summary-grid min-w-[900px]"
            style:--species-col-width={speciesColumnWidth}
          >
            <!-- Hourly weather visualization row (only shown if weather data exists) -->
            {#if hourlyWeather.length > 0}
              <div class="flex mb-1">
                <!-- Empty label column to align with other rows -->
                <div class="species-label-col shrink-0"></div>
                <DailySummaryStatColumns variant="spacer" />

                <!-- Hourly weather (desktop) -->
                <div class="hourly-grid flex-1 grid">
                  {#each Array(24) as _, hour (hour)}
                    {@const emoji = getHourlyWeatherEmoji(hour)}
                    <div
                      class="h-5 flex items-center justify-center text-sm weather-cell"
                      title={getHourlyWeatherTooltip(hour)}
                    >
                      {emoji || ''}
                    </div>
                  {/each}
                </div>

                <!-- Bi-hourly weather (tablet/mobile) -->
                <div class="bi-hourly-grid flex-1 grid">
                  {#each Array(12) as _, i (i)}
                    {@const hour = i * 2}
                    {@const emoji = getHourlyWeatherEmoji(hour)}
                    <div
                      class="h-5 flex items-center justify-center text-sm weather-cell"
                      title={getHourlyWeatherTooltip(hour)}
                    >
                      {emoji || ''}
                    </div>
                  {/each}
                </div>
              </div>
            {/if}

            <!-- Daylight visualization row -->
            <div class="flex mb-1">
              <div class="species-label-col shrink-0 flex items-center">
                <span
                  class="text-xs text-[var(--color-base-content)]/60 font-normal whitespace-nowrap"
                  >{t('dashboard.dailySummary.daylight.label')}</span
                >
              </div>
              <DailySummaryStatColumns variant="spacer" />
              <!-- Hourly daylight (desktop) -->
              <div class="hourly-grid flex-1 grid">
                {#each Array(24) as _, hour (hour)}
                  {@const daylightClass = getDaylightClass(hour)}
                  <div
                    class="h-5 rounded-sm daylight-cell daylight-{daylightClass} relative flex items-center justify-center"
                  >
                    {@render sunIcon('sunrise', sunTimes?.sunrise, hour === sunriseHour)}
                    {@render sunIcon('sunset', sunTimes?.sunset, hour === sunsetHour)}
                  </div>
                {/each}
              </div>
              <!-- Bi-hourly daylight (tablet/mobile) -->
              <div class="bi-hourly-grid flex-1 grid">
                {#each Array(12) as _, i (i)}
                  {@const hour = i * 2}
                  {@const daylightClass = getDaylightClass(hour)}
                  {@const showSunrise =
                    sunriseHour !== null && hour <= sunriseHour && sunriseHour < hour + 2}
                  {@const showSunset =
                    sunsetHour !== null &&
                    hour <= sunsetHour &&
                    sunsetHour < hour + 2 &&
                    !showSunrise}
                  <div
                    class="h-5 rounded-sm daylight-cell daylight-{daylightClass} relative flex items-center justify-center"
                  >
                    {@render sunIcon('sunrise', sunTimes?.sunrise, showSunrise)}
                    {@render sunIcon('sunset', sunTimes?.sunset, showSunset)}
                  </div>
                {/each}
              </div>
            </div>

            <!-- Hours header row -->
            <div class="flex mb-1">
              <div class="species-label-col shrink-0"></div>
              <DailySummaryStatColumns variant="header" />
              <!-- Hourly headers (desktop) -->
              <div class="hourly-grid flex-1 grid text-xs">
                {#each Array(24) as _, hour (hour)}
                  <a
                    href={urlBuilders.hourly(hour, 1)}
                    class="text-center hover:text-[var(--color-primary)] cursor-pointer"
                    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
                    title={t('dashboard.dailySummary.tooltips.viewHourly', {
                      hour: hour.toString().padStart(2, '0'),
                    })}
                  >
                    {hour.toString().padStart(2, '0')}
                  </a>
                {/each}
              </div>
              <!-- Bi-hourly headers (tablet/mobile) -->
              <div class="bi-hourly-grid flex-1 grid text-xs">
                {#each Array(12) as _, i (i)}
                  {@const hour = i * 2}
                  <a
                    href={urlBuilders.hourly(hour, 2)}
                    class="text-center hover:text-[var(--color-primary)] cursor-pointer"
                    style:color="color-mix(in srgb, var(--color-base-content) 50%, transparent)"
                    title={t('dashboard.dailySummary.tooltips.viewBiHourly', {
                      startHour: hour.toString().padStart(2, '0'),
                      endHour: (hour + 2).toString().padStart(2, '0'),
                    })}
                  >
                    {hour.toString().padStart(2, '0')}
                  </a>
                {/each}
              </div>
            </div>

            <!-- Species rows -->
            <div class="flex flex-col" style:gap="var(--grid-gap)">
              {#each sortedData as item, index (`${item.scientific_name}_${index}`)}
                {@const displayName = localizeSpeciesName(item.scientific_name, item.common_name)}
                {@const isRowExpanded = expandedSpecies === item.scientific_name}
                {@const noveltyCat = resolveNoveltyCategory(item, {
                  infrequentThresholdDays,
                  isToday,
                })}
                <!-- Row background click toggles the detail card; links/buttons inside
                     keep their own actions. Keyboard access goes through the chevron
                     button, so the div itself needs no key handler. -->
                <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
                <div
                  class="flex items-center species-row"
                  class:new-species={item.isNew && !prefersReducedMotion}
                  onclick={(e: MouseEvent) => {
                    const target = e.target as HTMLElement | null;
                    if (target?.closest('a, button')) return;
                    toggleRowExpansion(item.scientific_name);
                  }}
                >
                  <!-- Species info column -->
                  <div class="species-label-col shrink-0 flex items-center gap-2 pr-2">
                    {#if showThumbnails}
                      <BirdThumbnailPopup
                        thumbnailUrl={item.thumbnail_url
                          ? buildAppUrl(item.thumbnail_url)
                          : buildAppUrl(
                              `/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`
                            )}
                        commonName={item.common_name}
                        scientificName={item.scientific_name}
                        detectionUrl={urlBuilders.species(item)}
                      />
                    {:else}
                      <a
                        href={urlBuilders.species(item)}
                        class="species-badge shrink-0"
                        style:background-color={getSpeciesBadgeColor(item.scientific_name)}
                        title={item.scientific_name}
                      >
                        {getSpeciesInitials(displayName)}
                      </a>
                    {/if}
                    <div class="flex flex-col min-w-0 flex-1">
                      <a
                        href={urlBuilders.species(item)}
                        class="text-sm hover:text-[var(--color-primary)] cursor-pointer font-medium leading-tight flex items-center gap-1 overflow-hidden"
                        title={displayName}
                      >
                        <span class="truncate flex-1">{displayName}</span>
                        {#if noveltyCat === 'lifetime'}
                          <span
                            class="inline-block shrink-0"
                            style:color={noveltyCategoryColorVar('lifetime')}
                            title={`New species (first seen ${item.days_since_first_seen ?? 0} day${(item.days_since_first_seen ?? 0) === 1 ? '' : 's'} ago)`}
                          >
                            <Star class="size-3 fill-current" />
                          </span>
                        {:else if noveltyCat === 'year'}
                          <span
                            class="shrink-0"
                            style:color={noveltyCategoryColorVar('year')}
                            title={`First time this year (${item.days_this_year ?? 0} day${(item.days_this_year ?? 0) === 1 ? '' : 's'} ago)`}
                          >
                            📅
                          </span>
                        {:else if noveltyCat === 'season'}
                          <span
                            class="shrink-0"
                            style:color={noveltyCategoryColorVar('season')}
                            title={`First time this ${item.current_season || 'season'} (${item.days_this_season ?? 0} day${(item.days_this_season ?? 0) === 1 ? '' : 's'} ago)`}
                          >
                            🌿
                          </span>
                        {:else if noveltyCat === 'infrequent'}
                          <span
                            class="inline-block shrink-0"
                            style:color={noveltyCategoryColorVar('infrequent')}
                            title={t('dashboard.dailySummary.tooltips.infrequent', {
                              days: item.days_since_last_seen ?? 0,
                            })}
                          >
                            <History class="size-3" />
                          </span>
                        {/if}
                      </a>
                      <span class="species-sci-subline truncate">{item.scientific_name}</span>
                    </div>
                    <SpeciesEbirdLink speciesCode={item.species_code} {displayName} />
                    <button
                      type="button"
                      class="row-expand-btn shrink-0"
                      data-expand-btn={item.scientific_name}
                      aria-expanded={isRowExpanded}
                      aria-label="{isRowExpanded ? 'Hide' : 'Show'} details for {displayName}"
                      onclick={() => toggleRowExpansion(item.scientific_name)}
                    >
                      <ChevronDown
                        class="size-4 expand-icon {isRowExpanded ? 'expand-icon-open' : ''}"
                      />
                    </button>
                  </div>

                  <DailySummaryStatColumns variant="data" {item} />

                  <!-- Hourly heatmap cells (desktop) -->
                  <div class="hourly-grid flex-1 grid">
                    {#each Array(24) as _, hour (hour)}
                      {@const count = safeArrayAccess(item.hourly_counts, hour, 0) ?? 0}
                      {@const intensity = getHeatmapIntensity(count)}
                      <div
                        class="heatmap-cell h-8 rounded-sm heatmap-color-{intensity} flex items-center justify-center text-xs font-medium"
                        class:hour-updated={item.hourlyUpdated?.includes(hour) &&
                          !prefersReducedMotion}
                      >
                        {#if count > 0}
                          <a
                            href={urlBuilders.speciesHour(item, hour, 1)}
                            class="w-full h-full flex items-center justify-center cursor-pointer hover:opacity-80"
                            title={t('dashboard.dailySummary.tooltips.hourlyDetections', {
                              count,
                              hour: hour.toString().padStart(2, '0'),
                            })}
                          >
                            <AnimatedCounter value={count} />
                          </a>
                        {/if}
                      </div>
                    {/each}
                  </div>

                  <!-- Bi-hourly heatmap cells (tablet/mobile) -->
                  <div class="bi-hourly-grid flex-1 grid">
                    {#each Array(12) as _, i (i)}
                      {@const hour = i * 2}
                      {@const count = renderFunctions['bi-hourly'](item, hour)}
                      {@const intensity = getHeatmapIntensity(count)}
                      <div
                        class="heatmap-cell h-8 rounded-sm heatmap-color-{intensity} flex items-center justify-center text-xs font-medium"
                      >
                        {#if count > 0}
                          <a
                            href={urlBuilders.speciesHour(item, hour, 2)}
                            class="w-full h-full flex items-center justify-center cursor-pointer hover:opacity-80"
                            title={t('dashboard.dailySummary.tooltips.biHourlyDetections', {
                              count,
                              startHour: hour.toString().padStart(2, '0'),
                              endHour: (hour + 2).toString().padStart(2, '0'),
                            })}
                          >
                            <AnimatedCounter value={count} />
                          </a>
                        {/if}
                      </div>
                    {/each}
                  </div>
                </div>

                {#if isRowExpanded}
                  <!-- Inline species detail (same card as the mobile expanded view,
                       with a taller chart) — brings eBird/detections/history actions
                       and the daylight-colored hourly chart to desktop rows. -->
                  <SpeciesDetailCard
                    {item}
                    {sunriseHour}
                    {sunsetHour}
                    {displayName}
                    speciesUrl={urlBuilders.species(item)}
                    maxHour={chartMaxHour}
                    onCollapse={() => collapseRowExpansion(item.scientific_name)}
                    {selectedDate}
                    chartHeight={96}
                  />
                {/if}
              {/each}
            </div>
          </div>

          {#if sortedData.length === 0}
            <div
              class="text-center py-8"
              style:color="color-mix(in srgb, var(--color-base-content) 60%, transparent)"
            >
              {t('dashboard.dailySummary.noSpecies')}
            </div>
          {/if}

          <!-- Heatmap Legend -->
          {#if sortedData.length > 0}
            <div
              class="flex justify-end items-center gap-1.5 mt-3 text-xs text-[var(--color-base-content)]/60"
            >
              <span>{t('dashboard.dailySummary.legend.less')}</span>
              <div class="flex gap-0.5">
                {#each [0, 1, 2, 3, 4, 5, 6, 7, 8, 9] as intensity (intensity)}
                  <div
                    class="w-3 h-3 rounded-sm heatmap-color-{intensity}"
                    title="Intensity {intensity}"
                  ></div>
                {/each}
              </div>
              <span>{t('dashboard.dailySummary.legend.more')}</span>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </section>
{/if}

<style>
  /* ========================================================================
     CSS Custom Properties for Daily Summary Grid
     Scoped to component to avoid global conflicts
     ======================================================================== */
  .daily-summary-card {
    /* Grid layout properties */
    --grid-cell-height: 1.25rem;
    --grid-cell-radius: 4px;
    --grid-gap: 4px; /* Gap between grid cells */

    /* Species column width fallbacks (actual width is set dynamically via JS)
       These are fallbacks only - the dynamic width is set via style:--species-col-width */
    --species-col-min-width: 9rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MIN_WIDTH */
    --species-col-max-width: 16rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MAX_WIDTH */

    /* Light theme heatmap colors */
    --heatmap-color-0: #f0f9fc;
    --heatmap-color-1: #e0f3f8;
    --heatmap-color-2: #ccebf6;
    --heatmap-color-3: #99d7ed;
    --heatmap-color-4: #66c2e4;
    --heatmap-color-5: #33ade1;
    --heatmap-color-6: #0099d8;
    --heatmap-color-7: #0077be;
    --heatmap-color-8: #005595;
    --heatmap-color-9: #036;

    /* Animation durations */
    --anim-count-pop: 600ms;
    --anim-heart-pulse: 1000ms;
    --anim-new-species: 800ms;
  }

  /* ========================================================================
     CSS Grid Layout Styles
     ======================================================================== */

  /* Species label column - fixed width calculated from longest species name.
     Sticky at the leading edge (species name comes first, then the two
     stat cells, then the hour grid) so species names stay readable while
     the hour grid scrolls horizontally on narrower screens. */
  .species-label-col {
    width: var(--species-col-width, var(--species-col-min-width));
    position: sticky;
    left: 0;
    z-index: 5;
    background: var(--color-base-100);
    border-right: 1px solid var(--color-base-200);
  }

  /* CSS Grid for hour columns - equal columns using minmax(0, 1fr) */
  /* Default: show hourly (desktop), hide bi-hourly */
  .hourly-grid {
    display: grid;
    grid-template-columns: repeat(24, minmax(0, 1fr));
    gap: var(--grid-gap);
  }

  .bi-hourly-grid {
    display: none;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    gap: var(--grid-gap);
  }

  /* Heatmap cell base styles */
  .heatmap-cell {
    transition:
      opacity 0.15s ease,
      transform 0.15s ease;
  }

  .heatmap-cell a {
    color: inherit;
    text-decoration: none;
  }

  /* Species row - consistent height; two text lines (name + scientific name).
     The whole row toggles the inline species detail card. */
  .species-row {
    min-height: 2.5rem;
    border-radius: var(--grid-cell-radius);
    transition: background-color 0.15s ease;
    cursor: pointer;
  }

  .species-row:hover {
    background-color: var(--hover-overlay);
  }

  /* Scientific name subline under the species name (same treatment as the
     mobile summary table's landscape mode) */
  .species-sci-subline {
    font-size: 0.6875rem;
    font-style: italic;
    line-height: 1.2;
    color: color-mix(in srgb, var(--color-base-content) 55%, transparent);
  }

  /* Chevron that expands the species detail card under the row */
  .row-expand-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.5rem;
    height: 1.5rem;
    border-radius: 9999px;
    border: none;
    background: none;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
    cursor: pointer;
    padding: 0;
  }

  .row-expand-btn:hover {
    background: color-mix(in srgb, var(--color-base-content) 10%, transparent);
    color: var(--color-base-content);
  }

  .row-expand-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .row-expand-btn :global(.expand-icon) {
    transition: transform 0.15s ease;
  }

  .row-expand-btn :global(.expand-icon-open) {
    transform: rotate(180deg);
  }

  /* Empty cells background */
  :global(.heatmap-color-0) {
    background-color: var(--color-base-300);
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light'] .heatmap-color-0) {
    background-color: #e2e8f0;
  }

  :global([data-theme='dark'] .heatmap-color-0) {
    background-color: #1e293b;
  }

  /* ========================================================================
     Responsive Grid Display
     ======================================================================== */

  /* Tablet (768-1023px): show bi-hourly. Below 768px the desktop grid is not
     rendered at all — MobileSummaryTable takes over. */
  @media (min-width: 768px) and (max-width: 1023px) {
    .hourly-grid {
      display: none;
    }

    .bi-hourly-grid {
      display: grid;
    }
  }

  /* Wide desktop (≥1600px): taller heatmap cells use the extra room */
  @media (min-width: 1600px) {
    .daily-summary-card .heatmap-cell {
      height: 2.25rem;
    }
  }

  /* ========================================================================
     Heatmap Colors
     ======================================================================== */

  /* Dark theme heatmap colors - more vibrant and saturated */
  /* Must use .daily-summary-card scope to override the light theme vars defined above */
  :global([data-theme='dark']) .daily-summary-card {
    --heatmap-color-0: #1e293b;
    --heatmap-color-1: #164e63;
    --heatmap-color-2: #0e7490;
    --heatmap-color-3: #0891b2;
    --heatmap-color-4: #06b6d4;
    --heatmap-color-5: #22d3ee;
    --heatmap-color-6: #38bdf8;
    --heatmap-color-7: #60a5fa;
    --heatmap-color-8: #93c5fd;
    --heatmap-color-9: #bfdbfe;
    --heatmap-text-1: #fff;
    --heatmap-text-2: #fff;
    --heatmap-text-3: #fff;
    --heatmap-text-4: #000;
    --heatmap-text-5: #000;
    --heatmap-text-6: #000;
    --heatmap-text-7: #000;
    --heatmap-text-8: #000;
    --heatmap-text-9: #000;
  }

  /* Heatmap cell styles - solid colors with rounded corners */
  :global(.heatmap-color-1),
  :global(.heatmap-color-2),
  :global(.heatmap-color-3),
  :global(.heatmap-color-4),
  :global(.heatmap-color-5),
  :global(.heatmap-color-6),
  :global(.heatmap-color-7),
  :global(.heatmap-color-8),
  :global(.heatmap-color-9) {
    border-radius: var(--grid-cell-radius);
  }

  :global(.heatmap-color-1) {
    background-color: var(--heatmap-color-1);
    color: var(--heatmap-text-1, #000);
  }

  :global(.heatmap-color-2) {
    background-color: var(--heatmap-color-2);
    color: var(--heatmap-text-2, #000);
  }

  :global(.heatmap-color-3) {
    background-color: var(--heatmap-color-3);
    color: var(--heatmap-text-3, #000);
  }

  :global(.heatmap-color-4) {
    background-color: var(--heatmap-color-4);
    color: var(--heatmap-text-4, #000);
  }

  :global(.heatmap-color-5) {
    background-color: var(--heatmap-color-5);
    color: var(--heatmap-text-5, #fff);
  }

  :global(.heatmap-color-6) {
    background-color: var(--heatmap-color-6);
    color: var(--heatmap-text-6, #fff);
  }

  :global(.heatmap-color-7) {
    background-color: var(--heatmap-color-7);
    color: var(--heatmap-text-7, #fff);
  }

  :global(.heatmap-color-8) {
    background-color: var(--heatmap-color-8);
    color: var(--heatmap-text-8, #fff);
  }

  :global(.heatmap-color-9) {
    background-color: var(--heatmap-color-9);
    color: var(--heatmap-text-9, #fff);
  }

  /* Dark theme text color overrides */
  :global([data-theme='dark'] .heatmap-color-1),
  :global([data-theme='dark'] .heatmap-color-2),
  :global([data-theme='dark'] .heatmap-color-3) {
    color: #fff;
  }

  :global([data-theme='dark'] .heatmap-color-4),
  :global([data-theme='dark'] .heatmap-color-5),
  :global([data-theme='dark'] .heatmap-color-6),
  :global([data-theme='dark'] .heatmap-color-7),
  :global([data-theme='dark'] .heatmap-color-8),
  :global([data-theme='dark'] .heatmap-color-9) {
    color: #000;
  }

  /* Dynamic Update Animations - not in custom.css */

  /* Count increment animation */
  @keyframes countPop {
    0% {
      transform: scale(1);
    }

    50% {
      transform: scale(1.3);
      background-color: color-mix(in srgb, var(--color-success) 30%, transparent);
      box-shadow: 0 0 10px color-mix(in srgb, var(--color-success) 50%, transparent);
    }

    100% {
      transform: scale(1);
      background-color: color-mix(in srgb, var(--color-success) 0%, transparent);
    }
  }

  .count-increased {
    animation: countPop var(--anim-count-pop) cubic-bezier(0.4, 0, 0.2, 1);
  }

  /* New species row animation */
  @keyframes newSpeciesSlide {
    0% {
      transform: translateY(-30px);
      opacity: 0;
      background-color: color-mix(in srgb, var(--color-primary) 15%, transparent);
    }

    100% {
      transform: translateY(0);
      opacity: 1;
      background-color: color-mix(in srgb, var(--color-primary) 0%, transparent);
    }
  }

  .new-species {
    animation: newSpeciesSlide var(--anim-new-species) cubic-bezier(0.25, 0.46, 0.45, 0.94);
  }

  /* Heatmap cell heart pulse animation */
  @keyframes heartPulse {
    0% {
      transform: scale(1);
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--color-primary) 70%, transparent);
    }

    15% {
      transform: scale(1.15);
      box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary) 50%, transparent);
    }

    25% {
      transform: scale(1.05);
      box-shadow: 0 0 0 6px color-mix(in srgb, var(--color-primary) 30%, transparent);
    }

    35% {
      transform: scale(1.12);
      box-shadow: 0 0 0 8px color-mix(in srgb, var(--color-primary) 10%, transparent);
    }

    45% {
      transform: scale(1);
      box-shadow: 0 0 0 10px color-mix(in srgb, var(--color-primary) 0%, transparent);
    }

    100% {
      transform: scale(1);
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--color-primary) 0%, transparent);
    }
  }

  .hour-updated {
    animation: heartPulse var(--anim-heart-pulse) ease-out;
    position: relative;
    z-index: 10;
  }

  /* Respect user's reduced motion preference */
  @media (prefers-reduced-motion: reduce) {
    .count-increased,
    .new-species,
    .hour-updated {
      animation: none;
      transition: none;
    }
  }

  /* ========================================================================
     Species Column & Badge Styles
     ======================================================================== */

  :global(.species-column) {
    width: auto;
    min-width: 0;
    max-width: var(--species-col-max-width, 18rem);
    padding: 0 0.75rem 0 0.5rem !important;
  }

  .species-badge {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2rem; /* w-8 - match thumbnail width */
    height: 1.5rem; /* 4:3 aspect ratio to match avicommons images */
    border-radius: 0.375rem;
    font-size: 0.625rem;
    font-weight: 700;
    color: white;
    text-decoration: none;
    text-shadow: 0 1px 2px rgb(0 0 0 / 0.3);
    transition:
      transform 0.15s ease,
      box-shadow 0.15s ease;
  }

  .species-badge:hover {
    transform: scale(1.1);
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.25);
  }

  /* ========================================================================
     Daylight Row Styles
     ======================================================================== */

  .daylight-cell {
    position: relative;
    transition: background-color 0.2s ease;
    overflow: visible;
  }

  :global(.overflow-y-visible) {
    overflow-y: visible !important;
  }

  /* ========================================================================
     Daylight Color Classes - Gradual shading from night to day
     ======================================================================== */

  /* Deep night (midnight - 4am, 9pm - midnight) - darkest indigo */
  .daylight-deep-night {
    background-color: rgb(30 27 75 / 0.5); /* indigo-950/50 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-deep-night {
    background-color: rgb(30 27 75 / 0.3); /* indigo-950/30 */
  }

  /* Night (5am, 8pm) - lighter indigo */
  .daylight-night {
    background-color: rgb(49 46 129 / 0.4); /* indigo-900/40 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-night {
    background-color: rgb(49 46 129 / 0.2); /* indigo-900/20 */
  }

  /* Evening (6-7pm) - transition indigo */
  .daylight-evening {
    background-color: rgb(67 56 202 / 0.3); /* indigo-700/30 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-evening {
    background-color: rgb(67 56 202 / 0.15); /* indigo-700/15 */
  }

  /* Pre-dawn (1-2 hours before sunrise) - transitional purple/indigo */
  .daylight-pre-dawn {
    background-color: rgb(99 102 241 / 0.3); /* indigo-500/30 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-pre-dawn {
    background-color: rgb(99 102 241 / 0.2); /* indigo-500/20 */
  }

  /* Sunrise - gradient from orange to amber */
  .daylight-sunrise {
    background: linear-gradient(to right, #fb923c, #fbbf24); /* orange-400 to amber-400 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-sunrise {
    background: linear-gradient(to right, #f97316, #fcd34d); /* orange-500 to amber-300 */
  }

  /* Early day (just after sunrise) - soft warm amber */
  .daylight-early-day {
    background-color: rgb(251 191 36 / 0.4); /* amber-400/40 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-early-day {
    background-color: rgb(252 211 77 / 0.6); /* amber-300/60 */
  }

  /* Day (mid-morning, mid-afternoon) - medium amber */
  .daylight-day {
    background-color: rgb(251 191 36 / 0.5); /* amber-400/50 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-day {
    background-color: rgb(252 211 77 / 0.7); /* amber-300/70 */
  }

  /* Mid-day (peak daylight) - brightest amber/yellow */
  .daylight-mid-day {
    background-color: rgb(253 224 71 / 0.6); /* yellow-300/60 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-mid-day {
    background-color: rgb(254 240 138 / 0.8); /* yellow-200/80 */
  }

  /* Late day (before sunset) - soft warm amber */
  .daylight-late-day {
    background-color: rgb(251 191 36 / 0.4); /* amber-400/40 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-late-day {
    background-color: rgb(252 211 77 / 0.6); /* amber-300/60 */
  }

  /* Sunset - gradient from rose to purple */
  .daylight-sunset {
    background: linear-gradient(to right, #fda4af, #c084fc); /* rose-300 to purple-400 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-sunset {
    background: linear-gradient(to right, #fb7185, #a855f7); /* rose-400 to purple-500 */
  }

  /* Dusk (1-2 hours after sunset) - transitional purple */
  .daylight-dusk {
    background-color: rgb(139 92 246 / 0.25); /* violet-500/25 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-dusk {
    background-color: rgb(139 92 246 / 0.15); /* violet-500/15 */
  }
</style>
