package analysis

import (
	dsppcm "github.com/tphakala/go-audio-dsp/pcm"

	"github.com/tphakala/birdnet-go/internal/audiocore/batgate"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// batGateSummaryEvery is how many gated windows pass between two summary log lines.
const batGateSummaryEvery = 500

// batGateStats counts the gate decisions of one monitor and keeps its sample
// buffer, reused from window to window (a 3 s window at 256 kHz is 3 MB).
type batGateStats struct {
	checked, skipped, noEvent, mainsOnly int
	samples                              []float32
}

// batGateSkips runs the pre-inference bat gate on one window and reports whether
// the window must not be sent to the model. It does nothing for non-bat models
// or when the gate is off. In dry-run mode it logs but never skips.
func (m *BufferManager) batGateSkips(cfg *monitorConfig, data []byte, st *batGateStats) bool {
	gateCfg := conf.Setting().Bat.Gate
	if !gateCfg.Enabled || classifier.TaxonGroupFor(cfg.modelID) != classifier.TaxonGroupBat {
		return false
	}

	sourceRate := 0
	if cb, err := m.bufferMgr.CaptureBuffer(cfg.sourceID); err == nil {
		sourceRate = cb.SampleRate()
	}
	n := len(data) / conf.BytesPerSample
	if cap(st.samples) < n {
		st.samples = make([]float32, n)
	}
	samples := st.samples[:n]
	if _, err := dsppcm.BytesToFloat32(samples, data); err != nil {
		return false
	}

	v := batgate.Check(samples, cfg.spec.EffectiveSampleRate(), sourceRate, gateCfg.MainsHz)
	st.checked++
	if v.Skip {
		st.skipped++
		if v.Reason == batgate.ReasonMainsOnly {
			st.mainsOnly++
		} else {
			st.noEvent++
		}
	}

	m.logger.Debug("bat gate",
		logger.String("source_id", cfg.sourceID),
		logger.String("model_id", cfg.modelID),
		logger.Bool("skip", v.Skip),
		logger.Bool("dry_run", gateCfg.DryRun),
		logger.String("reason", v.Reason),
		logger.Int("events", v.Events),
		logger.Int("residual", v.Residual),
		logger.Float64("lock_score", v.LockScore))

	if st.checked%batGateSummaryEvery == 0 {
		m.logger.Info("bat gate summary",
			logger.String("source_id", cfg.sourceID),
			logger.String("model_id", cfg.modelID),
			logger.Bool("dry_run", gateCfg.DryRun),
			logger.Int("checked", st.checked),
			logger.Int("skipped", st.skipped),
			logger.Int("no_event", st.noEvent),
			logger.Int("mains_only", st.mainsOnly))
	}

	return v.Skip && !gateCfg.DryRun
}
