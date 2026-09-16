// audio_level_spectrum.go - Opt-in spectrum data on the audio level SSE stream.
//
// Browsers whose AnalyserNode cannot read HLS-backed media (Safari/WebKit,
// https://bugs.webkit.org/show_bug.cgi?id=180696) render the live spectrogram
// waterfall from server-computed magnitude bins instead of their own Web Audio
// graph. Those bins ride this existing stream rather than a dedicated endpoint,
// so they are stripped by default: a column is ~700 bytes per source per
// update, which the level-meter clients that make up most of this endpoint's
// traffic have no use for.
package audio

import "github.com/tphakala/birdnet-go/internal/audiocore"

// spectrumQueryParam is the query parameter a client uses to opt in.
const spectrumQueryParam = "spectrum"

// spectrumRequested reports whether a client asked for magnitude bins for a
// source. An absent parameter (the default) means no spectrum at all;
// "1"/"true" means every source; any other value is read as a source ID and
// selects just that source, which keeps the payload flat on multi-source
// installs where a spectrogram only ever renders one of them.
func spectrumRequested(param, sourceID string) bool {
	switch param {
	case "":
		return false
	case "1", "true":
		return true
	default:
		return param == sourceID
	}
}

// filterSpectrum drops the spectrum fields in place unless this client asked
// for that source.
func filterSpectrum(data *audiocore.AudioLevelData, param string) {
	if !spectrumRequested(param, data.Source) {
		*data = withoutSpectrum(*data)
	}
}

// withoutSpectrum returns a copy carrying no spectrum. Used both to strip
// non-requested data and to keep the column out of long-lived copies, where it
// would pin a buffer per source and risk reaching a client that never opted in.
func withoutSpectrum(data audiocore.AudioLevelData) audiocore.AudioLevelData { //nolint:gocritic // hugeParam: value semantics keep call sites a single expression
	data.Spectrum = nil
	data.SpectrumSampleRate = 0
	data.SpectrumTime = 0
	return data
}

// carryPendingSpectrum keeps the newest column alive until a send actually
// carries it.
//
// A column is produced at most every 50ms, but frames arrive far more often —
// a sound card callback is a few milliseconds, and an FFmpeg pipe read returns
// whatever bytes are available. Every frame in between publishes an
// AudioLevelData with no spectrum, and the SSE rate limiter runs on its own
// clock, so without this the send lands on a spectrum-less update far more
// often than not and the client sees no columns at all.
func carryPendingSpectrum(incoming *audiocore.AudioLevelData, previous audiocore.AudioLevelData) {
	if incoming.Spectrum != nil || previous.Spectrum == nil {
		return
	}
	incoming.Spectrum = previous.Spectrum
	incoming.SpectrumSampleRate = previous.SpectrumSampleRate
	incoming.SpectrumTime = previous.SpectrumTime
}
