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

**Rationale for Native/Auto default:** BirdNET-Go's *native* expansion factor is `sourceRate/48000` — the same ratio the inference pipeline uses. The fixed 5/10/16/20× presets remain available alongside it.

> **Caveat — do not claim 1:1 alignment with the displayed spectrogram (yet).** `ProfileForModelType()` currently returns `BirdProfile()` for *all* model types — the bat profile is intentionally disabled (`internal/spectrogram/frequency_profile.go:42-44`, "Bat profile is temporarily disabled due to spectrogram generation bugs"). So the spectrogram shown for a bat detection today is rendered with bird defaults (resampled to 24 kHz, Nyquist 12 kHz — see the `FREQ_NYQUIST_KHZ`/`FREQ_TICKS_KHZ` overlay in `AudioPlayer.svelte`). Until `BatProfile()` is re-enabled, the Native/Auto audio and the on-screen frequency axis are **not** the same scale, and the UI must not assert that they are. Treat 1:1 spectrogram alignment as a follow-up that lands together with the bat-spectrogram fix.

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

- **Visibility gate:** show the time-expansion control only when the detection's probed/effective clip **sample rate > 48000 Hz** — the same threshold the backend uses to accept the expansion request, so the UI never offers a preset the server will reject. `modelType === "bat"` may be used as a hint (e.g. to label the control or skip probing) but is **not** the sole gate, because bat clips can be stored/relabelled at 48 kHz. When the rate is ≤ 48000 Hz, hide the control (optionally with an info tooltip explaining it needs a high-sample-rate recording).

- Add an **"Audible bat playback"** preset selector in `AudioPlayer.svelte` (best placed alongside the existing `AudioSettingsButton`, on `DetectionDetail.svelte`). Selecting a preset swaps `audioUrl` to the expansion endpoint; selecting "Original" restores the source URL.

- When a derived preset is active, show a clear notice, e.g. *"Audible review copy — 10× slowed (standard bat-detector time expansion). Original recording is unchanged."*

- Optional nudge for peak frequency > ~60 kHz: *"This recording contains very high-frequency calls; 10× or 16× may be easier to hear."* — only if existing spectrogram/`frequency_profile.go` analysis can supply peak frequency cheaply; otherwise out of scope for v1.

- Add labels to `frontend/static/messages/en.json` and run the i18n sync/type-gen scripts.

## Scope

**In scope (v1):** Original + Native/Auto + 5× + 10× + 16× + 20× presets; server-side generation; on-disk caching; clear "derived copy" labelling; 192/256/384 kHz source support; visibility gating by probed clip sample rate > 48000 Hz (with `modelType == "bat"` as a hint only).

**Out of scope (v1):** heterodyne playback; automatic peak-frequency detection (unless trivially reusable); arbitrary user-chosen factors; modifying the live capture/inference path; 1:1 alignment of Native-preset audio with the displayed spectrogram (blocked on re-enabling `BatProfile()`).

## Acceptance criteria

- For a detection whose clip sample rate is above 48 kHz, the user can play a 48 kHz audible time-expanded copy in the browser.
- The time-expansion control is shown if and only if the probed clip rate > 48000 Hz, so the frontend visibility condition and the backend acceptance condition always agree; clips at ≤ 48 kHz never show a selector the server would reject.
- Default preset is **Native/Auto** (`sourceRate/48000`); 5×/10×/16×/20× are selectable; 10× is labelled the standard reference. The UI does not claim the Native preset matches the on-screen spectrogram scale while `ProfileForModelType` still returns bird defaults.
- The original recording file is never modified, and the derived audio is clearly labelled as a review copy.
- Derived audio is generated server-side via FFmpeg `asetrate,aresample` (pitch intentionally lowered; `atempo` not used), output at 48 kHz, and cached (keyed by note + rate + factor) so repeat requests don't regenerate.
- Derived audio is never used as classifier input.
- Works for at least 192/256/384 kHz sources.
- The clip sample rate is present in the detection API response so the frontend can apply the gating without an extra round-trip.
