# Audible bat playback via server-side time expansion

## Summary

Add a server-side **time-expansion playback** mode so users reviewing ultrasonic bat detections can hear the calls. BirdNET-Go already performs a ~5.3× time-expansion internally for bat inference (the "slow-down trick", `doc/wiki/detection-pipeline.md`); this feature exposes an equivalent transform on the **review/detail page** as a selectable, clearly-labelled *derived* audio copy, leaving the original recording untouched.

## Background in this codebase (why this fits)

- Bat capture at high sample rates (96–256 kHz) is already a supported configuration (`AudioSourceConfig.SampleRate`, model `bat`).
- The inference pipeline reinterprets 256 kHz audio as 48 kHz — a 5.33× slow-down — to make ultrasonic calls audible to the embedding model. This feature applies the same idea for human listening.
- FFmpeg is already integrated (`internal/audiocore/ffmpeg/`, with `-af` filter chains used for denoise/normalize/gain), so the transform needs no new dependency.
- Spectrograms are already generated and cached server-side with a parameter-keyed, immutable-cache, dedup-queue pattern (`internal/api/v2/media.go`, `internal/spectrogram/`). The derived audio should reuse this exact pattern.

## Prerequisite to confirm before implementation (blocking)

Determine the on-disk sample rate of saved bat clips (from the `CaptureBuffer`, which holds audio at the source rate):

1. **Saved at native rate (e.g., 256 kHz):** server-side expansion is required and fully recoverable → implement as below.
2. **Saved already downsampled/relabelled to 48 kHz:** ultrasonic content is already shifted/aliased; this feature reduces to UI labelling plus optional *further* expansion, and high-frequency detail may not be recoverable.

The implementation plan below assumes case (1); the UI/messaging should degrade gracefully for case (2).

## Proposed transform

Generalize the existing slow-down trick. For an expansion factor **F**, reinterpret then resample to a standard 48 kHz playback rate — using `asetrate` + `aresample`, **not** `atempo` (we *want* the pitch to drop; `atempo` preserves pitch and is wrong here):

```
ffmpeg -i input.wav -af "asetrate=${SOURCE_SAMPLE_RATE}/${F},aresample=48000" output_audible_${F}x.wav
```

A frequency `f` becomes `f/F`; output duration becomes `original × F`; output is always 48 kHz (browser-playable). This is independent of the source rate, so it works for 192/256/384 kHz sources.

### Presets

| Preset | Factor | Notes |
|---|---|---|
| **Original** | 1× | unchanged file |
| **Native / matches detection** *(recommended default)* | `sourceRate ÷ 48000` (≈5.3× at 256 kHz, 4× at 192 kHz, 8× at 384 kHz) | reproduces exactly what the detector "heard" and what the spectrogram shows |
| 5× | 5 | short clips, common 20–55 kHz European species |
| 10× | 10 | standard bat-detector reference; higher-frequency species / poor HF hearing |
| 16× / 20× | 16 / 20 | very high-frequency calls / hearing comfort |

**Rationale for Native/Auto default:** BirdNET-Go's *native* expansion is `sourceRate/48000`. Offering this as the default preset lines the playback up 1:1 with the frequency axis already displayed in the spectrogram for that detection. The fixed 5/10/16/20× presets remain available.

## Feature gating: use the same bat model check as the spectrogram

The spectrogram frequency axis for each detection is chosen server-side by `ProfileForModelType(modelType)` in `internal/spectrogram/frequency_profile.go`. When a detection comes from the `bat` model (BattyBirdNET), this returns `BatProfile()` — no resampling, 18 kHz high-pass — which produces a different frequency axis from the bird default. The `modelType` value (`"bat"`, `"bird"`, `"multi"`) is persisted in the database and already retrieved via `c.DS.GetNoteModelType(noteID)` in `internal/api/v2/media.go`.

**The time-expansion UI should apply the same gate:** show it if and only if `ProfileForModelType(modelType)` would return `BatProfile()` — i.e., `modelType == "bat"`.

This requires one small change: the `Detection` API response (currently defined in `frontend/src/lib/types/detection.types.ts`) does not include `modelType`. The field must be added to the backend response and the frontend type so the `DetectionDetail.svelte` component can conditionally render the time-expansion control without an extra API call.

## Backend implementation

Reuse existing infrastructure rather than a standalone service.

1. **Expose `modelType` in the detection response** — add the value from `GetNoteModelType` to the detection JSON already returned to the frontend. This is the single prerequisite for frontend gating.

2. **Endpoint** — extend the existing audio-serving surface (`internal/api/v2/media.go`) instead of inventing a parallel one. Either:
   - add an `expansion` query param to `GET /api/v2/audio/:id` (e.g. `?expansion=10` or `?expansion=native`), or
   - add a sibling to the existing `POST /api/v2/audio/:id/process` / `ExtractAudioClipByID` handlers.
   Validate `expansion` against an allow-list of presets (no arbitrary floats → avoids cache-key explosion and abuse).

3. **Probe** the source rate via the existing ffprobe path (`internal/audiocore/ffmpeg/validate.go`); reject with a clear error if the probed rate is ≤ 48000 Hz.

4. **Generate** the derived 48 kHz file through the existing FFmpeg filter-chain mechanism (`internal/audiocore/ffmpeg/export.go` / `clip.go`), adding the `asetrate,aresample` filter. Never write to the original path.

5. **Cache** on disk keyed by `(noteID, sourceRate, factor)`, mirroring the spectrogram cache: immutable `Cache-Control` headers and a `sync.Map`-based in-flight dedup queue so concurrent requests don't regenerate the same file. Serve `audio/wav` with `Content-Disposition: inline`.

6. **Never** feed derived audio back into classification; it is a review artifact only.

7. Respect `SecureFS` path handling already used by the media handlers.

## Frontend implementation

- The existing client-side speed control (`SPEED_OPTIONS`, `applyPlaybackRate`, `preservesPitch=false` in `frontend/src/lib/utils/audio.ts`) **cannot** serve this: browser `playbackRate` floors at 0.25 (4× max) and browsers can't decode ultrasonic WAV. Keep it separate and unchanged.

- **Visibility gate:** show the time-expansion control only when `detection.modelType === "bat"` — the same condition that (when re-enabled) triggers `BatProfile()` in the spectrogram generator. This mirrors the spectrogram's own bat-model check exactly. When `modelType` is not `"bat"`, hide the control entirely.

- Add an **"Audible bat playback"** preset selector in `AudioPlayer.svelte` (best placed alongside the existing `AudioSettingsButton`, on `DetectionDetail.svelte`). Selecting a preset swaps `audioUrl` to the expansion endpoint; selecting "Original" restores the source URL.

- When a derived preset is active, show a clear notice, e.g. *"Audible review copy — 10× slowed (standard bat-detector time expansion). Original recording is unchanged."*

- Optional nudge for peak frequency > ~60 kHz: *"This recording contains very high-frequency calls; 10× or 16× may be easier to hear."* — only if existing spectrogram/`frequency_profile.go` analysis can supply peak frequency cheaply; otherwise out of scope for v1.

- Add labels to `frontend/static/messages/en.json` and run the i18n sync/type-gen scripts.

## Scope

**In scope (v1):** Original + Native/Auto + 5× + 10× + 16× + 20× presets; server-side generation; on-disk caching; clear "derived copy" labelling; 192/256/384 kHz source support; visibility gating by `modelType === "bat"`.

**Out of scope (v1):** heterodyne playback; automatic peak-frequency detection (unless trivially reusable); arbitrary user-chosen factors; modifying the live capture/inference path.

## Acceptance criteria

- For a bat-model detection (`modelType === "bat"`), the user can play a 48 kHz audible time-expanded copy in the browser.
- For all other model types, the time-expansion control is hidden — same gate as the bat spectrogram frequency axis.
- Default preset is **Native/Auto** (`sourceRate/48000`); 5×/10×/16×/20× are selectable; 10× is labelled the standard reference.
- The original recording file is never modified, and the derived audio is clearly labelled as a review copy.
- Derived audio is generated server-side via FFmpeg `asetrate,aresample` (pitch intentionally lowered; `atempo` not used), output at 48 kHz, and cached (keyed by note + rate + factor) so repeat requests don't regenerate.
- Derived audio is never used as classifier input.
- Works for at least 192/256/384 kHz sources.
- `modelType` is present in the detection API response so the frontend can apply the gating without an extra round-trip.
