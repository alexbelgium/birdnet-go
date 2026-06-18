# Feature: Audible bat playback via time expansion

## Summary

BirdNET-Go bat detections capture ultrasonic recordings (typically 192–384 kHz) that are inaudible at normal playback speed. This feature adds an **audible review mode** for bat detections that slows the recording by a selectable time-expansion factor using FFmpeg's `asetrate` filter — the same approach used by hardware bat detectors. The original clip is never modified.

## Motivation

Users monitoring bats with BirdNET-Go cannot aurally verify detections because the source recordings are ultrasonic. Time expansion is the standard lossless technique: the audio is reinterpreted at a lower declared sample rate (e.g., 256 kHz ÷ 5 = 51.2 kHz) and resampled to 48 kHz for browser playback, which shifts a 45 kHz call down to 9 kHz — clearly audible. No pitch information is lost.

## Rationale for default factors

| Factor | Expanded range (20–55 kHz source) | Notes |
|--------|-----------------------------------|-------|
| 5× | ~4–11 kHz | Good for common European bats (pipistrelles, noctules, serotines) |
| 10× | ~2–5.5 kHz | Standard bat-detector reference, useful for high-frequency species and users with reduced hearing |
| 16× | ~1.25–3.4 kHz | Very high-frequency species or strong hearing loss |
| 20× | ~1–2.75 kHz | Maximum comfort / high-frequency calls above 60 kHz |

## Scope

### Included in v1

- 5×, 10×, 16×, 20× time expansion factors
- Backend endpoint producing 48 kHz WAV for browser playback, gated on model type using the same mechanism as spectrogram frequency profile selection
- `modelType` field added to the single-detection API response so the frontend can gate the UI
- Per-factor caching using the existing `processingCache` infrastructure
- Frontend control visible only for bat model detections with qualifying source sample rates
- Clear labelling that the audio is a derived audible review copy

### Excluded from v1

- Heterodyne simulation
- Automatic frequency-peak detection or factor suggestion
- Modification of the original clip in any way
- `modelType` in the bulk detections list endpoint (N+1 query concern — single detection only)

---

## Backend Implementation

### 1. Expose `modelType` in the single-detection API response — `internal/api/v2/detections.go`

Add the field to `DetectionResponse`:

```go
type DetectionResponse struct {
    // ... existing fields ...
    ModelType string `json:"modelType,omitempty"` // "bat", "bird", "multi", etc.
}
```

Populate it in `GetDetection` only (not `convertNotesToDetectionResponses`, to avoid N+1 queries in bulk listings):

```go
func (c *Controller) GetDetection(ctx echo.Context) error {
    id := ctx.Param("id")
    note, err := c.DS.Get(id)
    if err != nil {
        return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
    }

    weatherCache := make(map[string][]datastore.HourlyWeather)
    detection := c.noteToDetectionResponse(&note, true, weatherCache)

    // Populate modelType using the same mechanism as spectrogram frequency
    // profile selection (media.go lines 1081, 1459, 1828).
    if modelType, mtErr := c.DS.GetNoteModelType(id); mtErr == nil {
        detection.ModelType = modelType
    }

    if !c.isClientAuthenticated(ctx) {
        detection.Source = nil
    }
    return ctx.JSON(http.StatusOK, detection)
}
```

### 2. New constants — `internal/audiocore/ffmpeg/expand.go` (new file)

```go
// Valid time-expansion factors for bat audible review.
const (
    TimeExpansionFactor5  = 5
    TimeExpansionFactor10 = 10
    TimeExpansionFactor16 = 16
    TimeExpansionFactor20 = 20
)

var validExpansionFactors = map[int]bool{
    TimeExpansionFactor5:  true,
    TimeExpansionFactor10: true,
    TimeExpansionFactor16: true,
    TimeExpansionFactor20: true,
}

// IsValidExpansionFactor reports whether factor is a supported time-expansion value.
func IsValidExpansionFactor(factor int) bool {
    return validExpansionFactors[factor]
}
```

Note: `MinBatSampleRate = 96000` already exists in `internal/audiocore/ffmpeg/probe.go:18` — reuse it.

### 3. New function — `internal/audiocore/ffmpeg/expand.go`

```go
// TimeExpand applies time-expansion to a local audio file by interpreting the
// source at sourceRate/factor Hz (asetrate) and resampling to 48 kHz.
// Returns a WAV buffer suitable for browser playback.
// The source file is never modified.
func TimeExpand(ctx context.Context, inputPath, ffmpegPath string, factor int) (*bytes.Buffer, error)
```

**Implementation notes:**

- Add `ProbeFileInfo(ctx context.Context, filePath, ffmpegPath string) (*StreamInfo, error)` — a new exported function that calls ffprobe on a local file path. Do not reuse `ProbeStreamInfo`: that function adds RTSP-specific flags inappropriate for local files. It can reuse the existing private `parseProbeOutput` helper.
- Validate `factor` via `IsValidExpansionFactor` before any I/O.
- Reject sources where `sourceRate < MinBatSampleRate` with a typed sentinel error: `var ErrSourceRateTooLow = errors.NewStd("source sample rate too low for bat time expansion")`.
- Compute `expandedRate = sourceRate / factor`.
- FFmpeg filter: `-af "asetrate={expandedRate},aresample=48000"`.
- **Write to a temp file, not a pipe.** WAV muxer needs seekable output to write valid RIFF chunk sizes. Follow the exact pattern in `ProcessAudioByID` (`media.go:928–942`): temp file under `{SFS.BaseDir()}/.tmp-processing/`, run FFmpeg, read back, `defer os.Remove`.
- Adaptive timeout: `max(2×sourceDuration, 30s)` clamped to 10 minutes (matching `ExtractClip` in `clip.go:130`).

### 4. New cache key — `internal/api/v2/process_cache.go`

```go
// expansionCacheKey builds a deterministic filename for time-expansion cache lookup.
func expansionCacheKey(detectionID string, factor int) string {
    safeID := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(detectionID)
    return fmt.Sprintf("expand_%s_x%d.wav", safeID, factor)
}
```

### 5. New request type and handler — `internal/api/v2/media.go`

```go
// ExpandAudioRequest defines the request body for POST /api/v2/audio/:id/expand.
type ExpandAudioRequest struct {
    Factor int `json:"factor"` // 5, 10, 16, or 20
}
```

**`ExpandAudioByID`** follows the exact same structure as `ProcessAudioByID` (lines 857–968) with one additional step — the model type gate that mirrors spectrogram frequency profile selection:

```
POST /api/v2/audio/:id/expand
Body: {"factor": 5}
Auth: required (same middleware as /process and /clip)
Response: audio/wav, 48 kHz, browser-playable
```

Handler steps:

1. Require datastore.
2. Parse and validate `ExpandAudioRequest` — reject unknown/invalid factors via `ffmpeg.IsValidExpansionFactor`.
3. **Gate on model type** — call `c.DS.GetNoteModelType(noteID)`, exactly as done at `media.go:1081`, `media.go:1459`, and `media.go:1828` for spectrogram frequency profile selection. If the returned type is not `"bat"`, return HTTP 422: "Time expansion is only available for bat detections."
4. Resolve clip path via `c.DS.GetNoteClipPath(noteID)` + `c.normalizeAndValidatePathWithLogger`.
5. Check cache: `expansionCacheKey(noteID, req.Factor)` → `c.processingCache.get(...)`.
6. Acquire `c.processingSemaphore` (non-blocking, 503 on full).
7. Call `ffmpeg.TimeExpand(ctx, absolutePath, ffmpegPath, req.Factor)`.
8. Handle `ffmpeg.ErrSourceRateTooLow` → HTTP 422: "Source recording sample rate is too low for bat time expansion. Minimum required: 96 kHz."
9. Cache result via `c.processingCache.put(...)` (15-minute TTL, shared with existing processing cache).
10. Return `ctx.Blob(http.StatusOK, MimeTypeWAV, wavData)`.

### 6. Route registration — `internal/api/v2/media.go`, `initMediaRoutes`

```go
// Time expansion for bat audible review (requires authentication)
c.Echo.POST("/api/v2/audio/:id/expand", c.ExpandAudioByID, c.authMiddleware)
```

Follows the same namespace convention as `/api/v2/audio/:id/process` and `/api/v2/audio/:id/clip`. Update `internal/api/v2/README.md` with the new endpoint.

---

## Frontend Implementation

### 1. Update frontend `Detection` type — `frontend/src/lib/types/detection.types.ts`

```typescript
export interface Detection {
  // ... existing fields ...
  modelType?: string; // "bat", "bird", "multi" — only present on single-detection responses
}
```

### 2. Bat detection gate

The expansion control is **only rendered when `detection.modelType === 'bat'`**. This uses the same model type string returned by `c.DS.GetNoteModelType()` on the backend — the identical function used at `media.go:1081`, `media.go:1459`, and `media.go:1828` to select the spectrogram frequency profile (BatProfile vs BirdProfile).

```typescript
// Derived from the same GetNoteModelType source as spectrogram axis selection
let isBatDetection = $derived(detection?.modelType === 'bat');
```

### 3. Source sample rate check

The backend returns HTTP 422 with a clear message for `ErrSourceRateTooLow`. Surface that message in the UI. Do not pre-validate on the client — the detection response does not expose the clip's source sample rate.

**Future enhancement (not in v1):** Expose `sourceRate` in the single-detection response so the UI can disable the control with a tooltip before the user attempts a call that would fail.

### 4. UI location — `AudioToolbar.svelte`

Add a new `bat-expansion-controls` toolbar group between the processing controls and export controls, only rendered when `isBatDetection` is true.

New props:

```typescript
interface Props {
  // ... existing props ...
  isBatDetection: boolean;
  expansionFactor: number | null; // null = original, 5/10/16/20 = expanded
  isExpanding: boolean;
  expansionError: string | null;
  onExpansionFactorChange: (_factor: number | null) => void;
}
```

UI: a dropdown following the existing `denoise-dropdown` pattern (lines 96–307 of `AudioToolbar.svelte`) with these options:

| Option | i18n key |
|--------|----------|
| Original | `components.audioPlayer.batExpansion.original` |
| 5× slowed | `components.audioPlayer.batExpansion.factor5` |
| 10× standard | `components.audioPlayer.batExpansion.factor10` |
| 16× slowed | `components.audioPlayer.batExpansion.factor16` |
| 20× slowed | `components.audioPlayer.batExpansion.factor20` |

When expanded audio is active, render an inline notice below the toolbar:

> Audible review copy — 5× slowed. Original recording is unchanged.

### 5. Audio source switching — `DetectionDetail.svelte` / `AudioPlayer.svelte`

When the user selects a factor, call `POST /api/v2/audio/:id/expand` with `{"factor": N}`. On success, swap the `<audio>` element's source to the returned blob URL. Restoring factor to null restores the original URL from `GET /api/v2/audio/:id`.

The spectrogram is **not regenerated** for expanded audio. The ultrasonic spectrogram remains visible — it shows the original frequency content, which is more informative for bat ID than a frequency-shifted version.

### 6. i18n — `frontend/static/messages/en.json`

Add keys, then run `npm run i18n:sync && npm run generate:i18n-types`:

```json
{
  "components": {
    "audioPlayer": {
      "batExpansion": {
        "label": "Bat review",
        "original": "Original",
        "factor5": "5× slowed",
        "factor10": "10× standard",
        "factor16": "16× slowed",
        "factor20": "20× slowed",
        "derivedNotice": "Audible review copy — {factor}× slowed. Original recording is unchanged.",
        "lowRateWarning": "Source sample rate too low for time expansion (minimum 96 kHz).",
        "loading": "Generating audible review..."
      }
    }
  }
}
```

Update all 15 locale files. `npm run i18n:sync` fills English as fallback.

---

## What Does NOT Need to Change

| Area | Reason |
|------|--------|
| Original clip storage | Time-expanded audio is derived on-demand; the original is never touched |
| `processingCache` infrastructure | Reused as-is with a new `expansionCacheKey` function |
| `processingSemaphore` | Reused as-is; expansion is no more expensive than denoise processing |
| Spectrogram generation | Remains ultrasonic; expanded audio is only for listening |
| `BatProfile()` in `frequency_profile.go` | Spectrogram concern, unrelated to this feature |
| `ai_models` / detection schema | No schema changes needed |
| Authentication model | Follows existing auth pattern; no new permissions |
| `convertNotesToDetectionResponses` | `modelType` is NOT added to bulk listings (avoids N+1 queries) |

---

## Acceptance Criteria

- [ ] For a bat detection (where `GetNoteModelType` returns `"bat"`) backed by a ≥ 96 kHz source recording, the user can request expanded audio at 5×, 10×, 16×, or 20× and hear it in the browser
- [ ] The default factor presented is 5×
- [ ] The 10× option is labelled as "standard/reference"
- [ ] The original recording file is byte-identical before and after the operation
- [ ] Expanded audio plays at 48 kHz in all major browsers (Chrome, Firefox, Safari)
- [ ] Expanded audio is clearly labelled as a derived review copy in the UI
- [ ] When source rate < 96 kHz, backend returns HTTP 422 and frontend surfaces a readable warning
- [ ] When detection is not bat type, backend returns HTTP 422 and frontend hides the control entirely (gate on `detection.modelType === 'bat'`)
- [ ] Expanded audio is not fed into AI classification
- [ ] Expansion control is invisible for bird and wildlife model detections
- [ ] Repeated requests for same detection + factor are served from cache (15-minute TTL matching `processingCacheTTL`)
- [ ] `GET /api/v2/detections/:id` returns `modelType` in its response; bulk list endpoint does not
- [ ] All Go code passes `golangci-lint run -v` with zero errors
- [ ] All frontend code passes `npm run check:all` with zero errors
- [ ] All 15 locale files are updated before PR merge

---

## Key Codebase References

| What | File | Lines |
|------|------|-------|
| **Model type gate in spectrogram** (the pattern to replicate) | `internal/api/v2/media.go` | 1081, 1459, 1828 |
| `GetNoteModelType` datastore method | `internal/datastore/interfaces.go` | — |
| Existing `/process` endpoint pattern to follow | `internal/api/v2/media.go` | 857–968 |
| Temp-file WAV output pattern | `internal/api/v2/media.go` | 928–942 |
| Cache key helper to mirror | `internal/api/v2/process_cache.go` | 34–50 |
| `processingCache` + `processingSemaphore` init | `internal/api/v2/api.go` | 483–499 |
| Route registration pattern | `internal/api/v2/media.go` | 208–215 |
| `MinBatSampleRate` constant (96000) | `internal/audiocore/ffmpeg/probe.go` | 18 |
| `parseProbeOutput` (private, reference for `ProbeFileInfo`) | `internal/audiocore/ffmpeg/probe.go` | 106–136 |
| `DetectionResponse` struct to extend | `internal/api/v2/detections.go` | 179–206 |
| `GetDetection` handler to update | `internal/api/v2/detections.go` | 1218–1232 |
| Frontend `Detection` type to extend | `frontend/src/lib/types/detection.types.ts` | 14–42 |
| `AudioToolbar.svelte` denoise dropdown pattern | `frontend/src/lib/desktop/components/media/AudioToolbar.svelte` | 96–307 |
| i18n sync command | `frontend/CLAUDE.md` | — |

---

## Notes

- **Model type selection is identical to spectrogram axis selection.** `ExpandAudioByID` calls `c.DS.GetNoteModelType(noteID)` at the same point and in the same way as `ServeSpectrogramByID`, `ProcessedSpectrogramByID`, and `GenerateSpectrogramByID`. No new model-type lookup mechanism is introduced.
- No schema migrations required
- No changes to detection storage or AI classification pipeline
- No risk to existing audio processing flows
- Builds entirely on existing infrastructure (`processingCache`, `processingSemaphore`, `SecureFS`, `GetNoteModelType`)
