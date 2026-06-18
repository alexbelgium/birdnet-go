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

### Also expose `sourceRate` on the single detection (for pre-emptive UI gating)

Unlike `modelType`, `sourceRate` is **not** provided by #3529, so add it here. In `GetDetection` only (single detection — never the bulk list, to avoid an N+1 of probes), populate a new field by probing the clip with the same `parseProbeOutput` helper used by `/expand`, reusing the existing ffprobe result cache if one is available:

- Backend: `SourceRate int \`json:"sourceRate,omitempty"\`` on `DetectionResponse`.
- Frontend: `sourceRate?: number` on the `Detection` type.

This lets the UI disable the control with an explanatory tooltip *before* the user clicks, instead of letting a request fail with a 422 (per `frontend/CLAUDE.md` — no ambiguous disabled states). The backend 422 guard stays as the authoritative check.

### Download support

The same handler serves downloads: when the request carries `?download=1`, set `Content-Disposition: attachment` with a descriptive filename (e.g. `{commonName}_{date}_{factor}x.wav`) instead of inline. No separate endpoint.

## Frontend — one dropdown, reusing existing wiring

- Add an expansion dropdown to `AudioToolbar.svelte` following the existing denoise-dropdown pattern (`AudioToolbar.svelte:96–307`), rendered only for bat detections. Reuse the `modelType` already threaded into the player by #3529 (gate on `MODEL_TYPE_BAT`).
- Options: Original / 5× (default) / 10× (standard) / 16× / 20×.
- On select: `POST /api/v2/audio/:id/expand`, swap the `<audio>` source to the returned blob URL; "Original" restores `GET /api/v2/audio/:id`.
- **Disable the control with a tooltip** when `detection.sourceRate` is below `MinBatSampleRate` (96 kHz) — e.g. *"Needs a ≥ 96 kHz recording for time expansion."* — instead of allowing a click that 422s. Keep surfacing the backend 422 message as a fallback for the rare case where `sourceRate` is absent.
- **Download:** a small download button next to the dropdown links to `…/expand?download=1` for the active factor, so users can save the audible WAV.
- **Remember the last-used factor** in `localStorage` so reviewing a run of bat detections keeps the chosen factor. Default to 5× when nothing is stored.
- Leave the spectrogram untouched — the ultrasonic view from #3529 is more informative than a frequency-shifted one.
- Inline notice when active: *"Audible review copy — {factor}× slowed. Original recording is unchanged."*
- Add i18n keys to `en.json`, run `npm run i18n:sync && npm run generate:i18n-types`, update all 15 locales.

## Scope

**In (v1):** the `/expand` endpoint (reusing `/process`, cache, semaphore) with `?download=1` support; `sourceRate` exposed on the single-detection response; the AudioToolbar dropdown gated on bat model type, with pre-emptive disable-with-tooltip, a download button, and last-used-factor persistence.

**Out (v1):** heterodyne; auto peak-frequency detection; arbitrary factors; applying expansion to region/clip export; any change to capture, inference, spectrograms, or the original clip.

## Acceptance criteria

- For a bat detection with a ≥ 96 kHz source, the user can play 5×/10×/16×/20× expanded audio at 48 kHz; default offered is 5×, 10× labelled standard.
- Control shows iff the detection is bat-type (reusing #3529's `modelType`); backend returns 422 for non-bat or sub-96 kHz sources and the UI surfaces the message.
- The single-detection response includes `sourceRate`; when it is below 96 kHz the control is disabled with an explanatory tooltip rather than erroring on click.
- The active expanded audio can be downloaded as a WAV via `?download=1`.
- The last-used factor is restored from `localStorage` across detections (default 5×).
- Original clip is byte-identical before/after; derived audio is clearly labelled and never classified.
- Repeated `(detection, factor)` requests are served from the existing `processingCache`.
- No re-implementation of the `modelType` plumbing already delivered by #3529.
- `golangci-lint run -v` and `npm run check:all` pass clean; all 15 locales updated.
