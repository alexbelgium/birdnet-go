// species_consensus.go exposes the per-model species support and taxonomic-class
// lookups used by the processor's first-daily-detection consensus rule. Both are
// read-only queries over data the orchestrator and the OpenFauna dataset already
// hold; nothing here participates in inference.
package classifier

import (
	"strings"
	"sync"

	"github.com/tphakala/birdnet-go/internal/openfauna"
)

// avesClass is the OpenFauna taxonomic class name for birds.
const avesClass = "Aves"

// birdClassCache memoizes IsBirdSpecies verdicts by canonical species key.
// openfauna.LookupMeta streams the whole embedded metadata set on an index miss,
// so the same species must not pay for it twice. The key space is bounded by the
// loaded models' label sets.
//
//nolint:gochecknoglobals // process-wide memo over immutable embedded data
var birdClassCache sync.Map // canonical species key -> birdClassVerdict

type birdClassVerdict struct {
	isBird bool
	known  bool
}

// IsBirdSpecies reports whether scientificName belongs to class Aves according to
// the embedded OpenFauna metadata.
//
// known is false when the taxon carries no metadata, which callers must treat as
// "cannot be evaluated reliably" rather than as "not a bird": the bat classifier's
// labels and Perch's non-species sound classes both land there, as does any
// species OpenFauna has not catalogued.
func IsBirdSpecies(scientificName string) (isBird, known bool) {
	key := canonicalSpeciesKey(scientificName)
	if key == "" {
		return false, false
	}
	if cached, ok := birdClassCache.Load(key); ok {
		v := cached.(birdClassVerdict) //nolint:errcheck // only this file stores here
		return v.isBird, v.known
	}

	meta, ok := openfauna.LookupMeta(key)
	v := birdClassVerdict{
		isBird: ok && strings.EqualFold(meta.Class, avesClass),
		known:  ok && meta.Class != "",
	}
	birdClassCache.Store(key, v)
	return v.isBird, v.known
}

// isBirdCapableModel reports whether a registry model classifies birds. Every
// model in the registry does except the bat classifier, so this is expressed as
// the single bat exclusion rather than an allow-list that a future bird model
// would silently fall out of.
func isBirdCapableModel(modelID string) bool {
	return modelID != RegistryIDBat
}

// BirdModelSpeciesSupport reports how many currently loaded, active, bird-capable
// models are relevant to a detection, and how many of those can predict
// scientificName at all.
//
// restrictTo, when non-nil, narrows the answer to that set of model IDs — the
// processor passes the models actually analysing the audio source, since a model
// that never sees the source can never confirm a detection on it. A nil
// restrictTo considers every loaded model.
//
// ok is false when the answer cannot be trusted (no orchestrator, unusable
// species name, or a model whose labels cannot be read because its instance is
// mid-reload). Callers must fail open on it rather than infer a count of zero.
//
// Locking follows AllLabels: model IDs and entry pointers are snapshotted under
// o.mu, which is released before any entry.mu is taken, because the reload,
// unload and delete paths deliberately order those two locks the other way.
func (o *Orchestrator) BirdModelSpeciesSupport(scientificName string, restrictTo map[string]struct{}) (relevant, supporting int, ok bool) {
	if o == nil {
		return 0, 0, false
	}
	key := canonicalSpeciesKey(scientificName)
	if key == "" {
		return 0, 0, false
	}

	o.mu.RLock()
	primary := o.primary
	primaryID := o.ModelInfo.ID
	refs := make([]entryRef, 0, len(o.models))
	for id, entry := range o.models {
		refs = append(refs, entryRef{id: id, entry: entry})
	}
	o.mu.RUnlock()

	for _, ref := range refs {
		if !isBirdCapableModel(ref.id) || !o.IsModelActive(ref.id) {
			continue
		}
		if restrictTo != nil {
			if _, wanted := restrictTo[ref.id]; !wanted {
				continue
			}
		}

		var labels []string
		if primary != nil && ref.id == primaryID {
			// BirdNET.Labels takes the model's own lock, so entry.mu is neither
			// needed nor safe to hold here. Same reasoning as AllLabels.
			labels = primary.Labels()
		} else {
			ref.entry.mu.Lock()
			if ref.entry.instance != nil {
				labels = ref.entry.instance.Labels()
			}
			ref.entry.mu.Unlock()
		}
		if len(labels) == 0 {
			// A loaded model we cannot read labels for makes the whole comparison
			// unreliable, so report that rather than quietly under-counting.
			return 0, 0, false
		}

		relevant++
		if labelsCoverSpecies(labels, key) {
			supporting++
		}
	}

	return relevant, supporting, true
}

// labelsCoverSpecies reports whether any label canonicalizes to key. It scans
// rather than building a set: callers ask about one species at a time and cache
// the verdict, so the transient map would cost more than the walk.
func labelsCoverSpecies(labels []string, key string) bool {
	for _, label := range labels {
		if canonicalSpeciesKey(label) == key {
			return true
		}
	}
	return false
}
