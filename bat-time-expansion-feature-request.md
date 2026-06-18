# Audible bat playback via time expansion

## Depends on: tphakala/birdnet-go#3529 (merge after)

This feature is intentionally minimal because #3529 already delivers the plumbing it needs. Reuse it; do **not** re-add any of it:

| #3529 already provides | Reuse for this feature |
|---|---|
| `ModelType string \`json:"modelType,omitempty"\`` on `DetectionResponse` (`internal/api/v2/detections.go`) | backend already knows the model type per detection |
| `modelType?: string` on the frontend `Detection` (`detection.types.ts`) | frontend already has it on the detail page |
| `modelType` prop + `const MODEL_TYPE_BAT = 'bat'` + `isBatSpectrogram` in `AudioPlayer.svelte` | reuse `MODEL_TYPE_BAT` / the bat check as the visibility gate |
| `BatProfile()` re-enabled → native 0–128 kHz ultrasonic spectrogram | spectrogram is already correct; this feature never touches it |

So the entire feature is one new backend endpoint plus one dropdown — every gate and every model-type lookup already exists.

## Summary

Bat detections capture ultrasonic audio (96–384 kHz) that is inaudible at normal playback. This adds an **audible review mode**: a derived copy slowed by a selectable factor via FFmpeg `asetrate`+`aresample`, resampled to 48 kHz for the browser. The original clip is never modified; the ultrasonic spectrogram is left as-is.

## Transform

For factor **F**, reinterpret the source at `sourceRate/F` Hz then resample to 48 kHz — `asetrate`+`aresample`, **not** `atempo` (pitch must drop):

```
ffmpeg -i input.wav -af "asetrate=${sourceRate}/${F},aresample=48000" out.wav
```

A 45 kHz call at 5× → 9 kHz (audible); duration becomes `original × F`; output is always 48 kHz.

| Preset | Factor | Notes |
|---|---|---|
| Original | — | unchanged file |
| 5× *(default)* | 5 | common European bats (pipistrelles, noctules, serotines) |
| 10× | 10 | standard bat-detector reference; high-frequency species / reduced hearing |
| 16× / 20× | 16 / 20 | very high-frequency calls / hearing comfort |

## Backend — one endpoint, mirroring `/process`

`POST /api/v2/audio/:id/expand` (auth required), registered beside `/process` and `/clip` in `initMediaRoutes` (`media.go:211`). Body `{"factor": 5}`. Handler `ExpandAudioByID` copies `ProcessAudioByID` (`media.go:857–968`) and changes only the filter:

1. Validate `factor` against a fixed allow-list (`5/10/16/20`); reject anything else.
2. Gate on `GetNoteModelType(noteID) == "bat"` — the same call used for spectrogram profile selection (`media.go:1081/1459/1828`). 422 if not bat.
3. Resolve + validate the clip path via `SecureFS`, as `/process` does.
4. `processingCache.get(expansionCacheKey(noteID, factor))` — new key mirroring `processingCacheKey` (`process_cache.go:35`); reuse the existing 15-min cache.
5. Acquire `processingSemaphore` (503 when full).
6. Probe the local file's sample rate (reuse `parseProbeOutput`, `probe.go:106`; not `ProbeStreamInfo`, which adds RTSP flags). Reject `< MinBatSampleRate` (96 kHz, `probe.go:19`) → 422 with a typed sentinel error.
7. Run `asetrate=sourceRate/factor,aresample=48000` into a temp file under `.tmp-processing/` (WAV muxer needs seekable output — same pattern as `media.go:928–942`), read back, `defer os.Remove`.
8. `processingCache.put`, return `audio/wav`. Never write the original path; never feed derived audio to classification.

No new infrastructure — `processingCache`, `processingSemaphore`, `SecureFS`, auth, and the schema are all reused unchanged.

## Frontend — one dropdown, reusing existing wiring

- Add an expansion dropdown to `AudioToolbar.svelte` following the existing denoise-dropdown pattern (`AudioToolbar.svelte:96–307`), rendered only for bat detections. Reuse the `modelType` already threaded into the player by #3529 (gate on `MODEL_TYPE_BAT`).
- Options: Original / 5× (default) / 10× (standard) / 16× / 20×.
- On select: `POST /api/v2/audio/:id/expand`, swap the `<audio>` source to the returned blob URL; "Original" restores `GET /api/v2/audio/:id`.
- Leave the spectrogram untouched — the ultrasonic view from #3529 is more informative than a frequency-shifted one.
- Inline notice when active: *"Audible review copy — {factor}× slowed. Original recording is unchanged."*
- Surface the backend 422 message if the source rate is too low (the response doesn't expose `sourceRate`, so no client-side pre-check).
- Add i18n keys to `en.json`, run `npm run i18n:sync && npm run generate:i18n-types`, update all 15 locales.

## Scope

**In (v1):** the `/expand` endpoint (reusing `/process`, cache, semaphore) and the AudioToolbar dropdown gated on bat model type.

**Out (v1):** heterodyne; auto peak-frequency detection; arbitrary factors; exposing `sourceRate` to the client; any change to capture, inference, spectrograms, or the original clip.

## Acceptance criteria

- For a bat detection with a ≥ 96 kHz source, the user can play 5×/10×/16×/20× expanded audio at 48 kHz; default offered is 5×, 10× labelled standard.
- Control shows iff the detection is bat-type (reusing #3529's `modelType`); backend returns 422 for non-bat or sub-96 kHz sources and the UI surfaces the message.
- Original clip is byte-identical before/after; derived audio is clearly labelled and never classified.
- Repeated `(detection, factor)` requests are served from the existing `processingCache`.
- No re-implementation of the `modelType` plumbing already delivered by #3529.
- `golangci-lint run -v` and `npm run check:all` pass clean; all 15 locales updated.
