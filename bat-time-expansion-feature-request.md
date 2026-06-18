# Audible bat playback via server-side time expansion

## Summary

Add a server-side **time-expansion playback** mode so users reviewing ultrasonic bat detections can hear the calls. BirdNET-Go already performs a ~5.3× time-expansion internally for bat inference (the "slow-down trick", `doc/wiki/detection-pipeline.md`); this feature exposes an equivalent transform on the **review/detail page** as a selectable, clearly-labelled *derived* audio copy, leaving the original recording untouched.

## Background in this codebase (why this fits)

- Bat capture at high sample rates (96–256 kHz) is already a supported configuration (`AudioSourceConfig.SampleRate`, model `bat`).
- The inference pipeline reinterprets 256 kHz audio as 48 kHz — a 5.33× slow-down — to make ultrasonic calls audible to the embedding model. This feature applies the same idea for human listening.
- FFmpeg is already integrated (`internal/audiocore/ffmpeg/`, with `-af` filter chains used for denoise/normalize/gain), so the transform needs no new dependency.
- The existing `ProcessAudioByID` / `ExtractAudioClipByID` handlers (`internal/api/v2/media.go`) already generate FFmpeg-derived audio on demand and serve it back to the player — this feature is a thin extension of that path, not a new service.

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

A small fixed set of integer factors (no computed/"auto" factor — keeps the endpoint a simple allow-list and avoids any implied relationship to the spectrogram axis, which is currently rendered with bird defaults regardless of model type):

| Preset | Factor | Notes |
|---|---|---|
| **Original** | 1× | unchanged file |
| 5× *(default)* | 5 | short clips, common 20–55 kHz European species |
| 10× | 10 | standard bat-detector reference; higher-frequency species / poor HF hearing |
| 16× / 20× | 16 / 20 | very high-frequency calls / hearing comfort |

## Feature gating: probed sample rate is the gate; bat model is only a hint

The transform is only meaningful — and the backend only accepts it — when the clip's actual sample rate is above the 48 kHz playback target. The single source of truth for "is this clip ultrasonic?" is therefore the **probed sample rate of the stored clip**, not the model type.

Model type is an unreliable proxy here:

- `AudioSourceConfig.Validate` accepts the `model` value independently of `SampleRate` and only validates `SampleRate` when it is nonzero, so a `bat`-model source can carry a ≤ 48 kHz rate.
- This document's own blocking prerequisite flags that bat clips may be **saved/relabelled at 48 kHz**. In that case a model-type gate would show the selector while the expansion endpoint correctly rejects the clip — exactly the ambiguous/broken state the frontend guidelines warn against.

**Gating rule:**

- **Backend** rejects/410s expansion requests whose probed source rate ≤ 48000 Hz (unchanged).
- **Frontend** shows the control based on the **probed/effective clip sample rate > 48000 Hz**, surfaced in the detection response (see below). `modelType == "bat"` is used only as a cheap *hint* (e.g., to decide whether it's worth probing, or to label the control "bat playback"), never as the sole visibility condition. This keeps the UI gate and the backend gate in agreement.

To support this, the detection API response (currently `frontend/src/lib/types/detection.types.ts`, which has no rate/model field) should expose the clip's **sample rate** (and optionally `modelType` as a hint). The frontend gates on the rate so it never offers a preset the backend will reject.

## Backend implementation

Reuse existing infrastructure rather than a standalone service.

1. **Expose the clip sample rate in the detection response** — surface the probed/effective sample rate (and optionally `modelType` from `GetNoteModelType` as a hint) in the detection JSON returned to the frontend. The rate is what the frontend gates on so its visibility condition matches the backend's `≤ 48000 Hz` rejection.

2. **Endpoint** — add an `expansion` query param (`5`/`10`/`16`/`20`) to the existing audio-serving surface (`internal/api/v2/media.go`), reusing the `ProcessAudioByID` path rather than inventing a parallel one. Validate `expansion` against the fixed allow-list above (reject anything else).

3. **Probe** the source rate via the existing ffprobe path (`internal/audiocore/ffmpeg/validate.go`); reject with a clear error if the probed rate is ≤ 48000 Hz.

4. **Generate** the derived 48 kHz audio through the existing FFmpeg filter-chain mechanism (`internal/audiocore/ffmpeg/export.go` / `clip.go`), adding the `asetrate,aresample` filter — exactly as the current denoise/normalize/gain filters are applied. Never write to the original path. Serve `audio/wav` inline with normal cache headers so the browser caches the response; server-side disk caching is **not** required for v1 (the clips are short and generation is fast — add it later only if profiling shows repeated regeneration is a problem).

5. **Never** feed derived audio back into classification; it is a review artifact only.

6. Respect `SecureFS` path handling already used by the media handlers.

## Frontend implementation

- The existing client-side speed control (`SPEED_OPTIONS`, `applyPlaybackRate`, `preservesPitch=false` in `frontend/src/lib/utils/audio.ts`) **cannot** serve this: browser `playbackRate` floors at 0.25 (4× max) and browsers can't decode ultrasonic WAV. Keep it separate and unchanged.

- **Visibility gate:** show the time-expansion control only when the detection's probed/effective clip **sample rate > 48000 Hz** — the same threshold the backend uses to accept the expansion request, so the UI never offers a preset the server will reject. `modelType === "bat"` may be used as a hint (e.g. to label the control or skip probing) but is **not** the sole gate, because bat clips can be stored/relabelled at 48 kHz. When the rate is ≤ 48000 Hz, hide the control (optionally with an info tooltip explaining it needs a high-sample-rate recording).

- Add an **"Audible bat playback"** preset selector in `AudioPlayer.svelte` (best placed alongside the existing `AudioSettingsButton`, on `DetectionDetail.svelte`). Selecting a preset swaps `audioUrl` to the expansion endpoint; selecting "Original" restores the source URL.

- When a derived preset is active, show a clear notice, e.g. *"Audible review copy — 10× slowed (standard bat-detector time expansion). Original recording is unchanged."*

- Optional nudge for peak frequency > ~60 kHz: *"This recording contains very high-frequency calls; 10× or 16× may be easier to hear."* — only if existing spectrogram/`frequency_profile.go` analysis can supply peak frequency cheaply; otherwise out of scope for v1.

- Add labels to `frontend/static/messages/en.json` and run the i18n sync/type-gen scripts.

## Scope

**In scope (v1):** Original + 5× + 10× + 16× + 20× presets; on-demand server-side generation reusing the existing FFmpeg/`ProcessAudioByID` path; clear "derived copy" labelling; 192/256/384 kHz source support; visibility gating by probed clip sample rate > 48000 Hz (with `modelType == "bat"` as a hint only).

**Out of scope (v1):** heterodyne playback; automatic peak-frequency detection; arbitrary user-chosen factors; server-side disk caching; modifying the live capture/inference path.

## Acceptance criteria

- For a detection whose clip sample rate is above 48 kHz, the user can play a 48 kHz audible time-expanded copy in the browser.
- The time-expansion control is shown if and only if the probed clip rate > 48000 Hz, so the frontend visibility condition and the backend acceptance condition always agree; clips at ≤ 48 kHz never show a selector the server would reject.
- Default preset is **5×**; 10×/16×/20× are selectable; 10× is labelled the standard reference.
- The original recording file is never modified, and the derived audio is clearly labelled as a review copy.
- Derived audio is generated server-side via FFmpeg `asetrate,aresample` (pitch intentionally lowered; `atempo` not used), output at 48 kHz.
- Derived audio is never used as classifier input.
- Works for at least 192/256/384 kHz sources.
- The clip sample rate is present in the detection API response so the frontend can apply the gating without an extra round-trip.
