# Audible bat playback via time expansion

## Depends on (merge after): tphakala/birdnet-go#3529

This feature is designed to land **after** the bat-spectrogram fix (#3529). That PR already does work both earlier drafts treated as in-scope, so we simply consume it:

- Adds `ModelType` to the single-detection `DetectionResponse` and the frontend `Detection` type, and wires it into `AudioPlayer.svelte` / `DetectionDetail.svelte`.
- Re-enables `BatProfile()` so bat detections render a native ultrasonic spectrogram (0–128 kHz axis), instead of the bird-default 0–12 kHz axis.

**Consequences for this feature:** do **not** re-add `modelType` to the API or the frontend type — verify it's present from #3529 and reuse it. The frontend gate (`detection.modelType === 'bat'`) is then literally the same model-type selection that drives the spectrogram frequency axis.

## Summary

BirdNET-Go bat detections capture ultrasonic audio (typically 96–384 kHz) that is inaudible at normal playback. This adds an **audible review mode**: a derived copy slowed by a selectable time-expansion factor via FFmpeg `asetrate`+`aresample`, resampled to 48 kHz for the browser. The original clip is never modified, and the (ultrasonic) spectrogram is left as-is.

## Transform

For factor **F**, reinterpret the source at `sourceRate/F` Hz then resample to 48 kHz — `asetrate`+`aresample`, **not** `atempo` (we want pitch to drop):

```
ffmpeg -i input.wav -af "asetrate=${sourceRate}/${F},aresample=48000" out.wav
```

A 45 kHz call at 5× → 9 kHz (audible); duration becomes `original × F`; output is always 48 kHz.

### Presets (fixed allow-list)

| Preset | Factor | Notes |
|---|---|---|
| Original | — | unchanged file |
| 5× *(default)* | 5 | common European bats (pipistrelles, noctules, serotines) |
| 10× | 10 | standard bat-detector reference; high-frequency species / reduced hearing |
| 16× / 20× | 16 / 20 | very high-frequency calls / hearing comfort |

## Gating

Two layers, each using existing mechanisms:

- **Frontend visibility:** render the control only when `detection.modelType === 'bat'` — the same `GetNoteModelType` value that selects the spectrogram frequency profile (`media.go:1081/1459/1828`). `modelType` is already on the response thanks to #3529.
- **Backend correctness:** the `/expand` handler gates on `GetNoteModelType == "bat"` (422 otherwise) and rejects sources below `MinBatSampleRate` (96 kHz, `internal/audiocore/ffmpeg/probe.go:19`) with a typed error surfaced as 422. This covers the edge case of a bat-model source whose clip is ≤ 96 kHz, so the UI gate and backend never silently disagree.

## Backend (reuse `/process` end to end)

1. **Endpoint:** `POST /api/v2/audio/:id/expand` (auth required), registered next to `/process` and `/clip` in `initMediaRoutes` (`media.go:211`). Body `{"factor": 5}`.
2. **Handler `ExpandAudioByID`:** mirror `ProcessAudioByID` (`media.go:857–968`) step for step:
   - validate `factor` against a fixed allow-list (`5/10/16/20`);
   - gate on `GetNoteModelType(noteID) == "bat"` → 422 if not;
   - resolve + validate clip path via `SecureFS` as `/process` does;
   - check `processingCache` with a new `expansionCacheKey(noteID, factor)` (mirror `processingCacheKey`, `process_cache.go:35`);
   - acquire `processingSemaphore` (503 when full);
   - run the FFmpeg transform into a temp file under `.tmp-processing/` (the WAV muxer needs seekable output — same temp-file pattern as `media.go:928–942`), read back, `defer os.Remove`;
   - `processingCache.put` (existing 15-min TTL) and return `audio/wav`.
3. **Probe + transform helper** in `internal/audiocore/ffmpeg/`: probe the local file's sample rate (reuse the private `parseProbeOutput`, `probe.go:106`; `ProbeStreamInfo` adds RTSP flags unsuitable for files), reject `< MinBatSampleRate`, then apply `asetrate,aresample`. Never write the original path.
4. Derived audio is **never** fed to classification.

Nothing new is added to `processingCache`, `processingSemaphore`, `SecureFS`, the schema, or the auth model — all reused as-is.

## Frontend (reuse the AudioToolbar denoise dropdown)

- Add a small expansion dropdown to `AudioToolbar.svelte`, following the existing denoise-dropdown pattern (`AudioToolbar.svelte:96–307`), rendered only when `detection.modelType === 'bat'`.
- Options: Original / 5× (default) / 10× (standard) / 16× / 20×.
- On select, `POST /api/v2/audio/:id/expand`; swap the `<audio>` source to the returned blob URL. Selecting "Original" restores `GET /api/v2/audio/:id`.
- **Do not regenerate the spectrogram** — the ultrasonic spectrogram (from #3529) is more informative for ID than a frequency-shifted one.
- Show an inline notice when active: *"Audible review copy — {factor}× slowed. Original recording is unchanged."*
- Surface the backend 422 message if the source rate is too low (the response doesn't expose `sourceRate`, so don't pre-check client-side).
- Add i18n keys to `en.json`, run `npm run i18n:sync && npm run generate:i18n-types`, update all 15 locales.

## Scope

**In (v1):** 5×/10×/16×/20× presets; `POST /audio/:id/expand` reusing the `/process` path, cache, and semaphore; frontend dropdown gated on `modelType === 'bat'`; clear "derived copy" labelling.

**Out (v1):** heterodyne; auto peak-frequency detection; arbitrary factors; exposing `sourceRate` to the client for pre-validation (nice-to-have follow-up to disable the control with a tooltip instead of a 422); any change to capture/inference, spectrograms, or the original clip.

## Acceptance criteria

- For a bat detection with a ≥ 96 kHz source, the user can play 5×/10×/16×/20× expanded audio at 48 kHz in the browser; default offered is 5×, 10× labelled standard.
- Control is shown iff `modelType === 'bat'`; backend returns 422 for non-bat or sub-96 kHz sources and the UI surfaces the message.
- Original clip is byte-identical before/after; derived audio is clearly labelled and never classified.
- Repeated `(detection, factor)` requests are served from the existing `processingCache`.
- No duplication of the `modelType` plumbing already delivered by #3529.
- `golangci-lint run -v` and `npm run check:all` pass clean; all 15 locales updated.
