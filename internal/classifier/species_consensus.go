// species_consensus.go exposes the per-model species support and taxonomic-class
// lookups used by the processor's first-daily-detection consensus rule. Both are
// read-only queries over data the orchestrator and the OpenFauna dataset already
// hold; nothing here participates in inference.
package classifier

import (
	"slices"
	"strings"
	"sync"

	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/detection"
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
	if cached, loaded := birdClassCache.Load(key); loaded {
		if v, isVerdict := cached.(birdClassVerdict); isVerdict {
			return v.isBird, v.known
		}
	}

	meta, found := openfauna.LookupMeta(key)
	v := birdClassVerdict{
		isBird: found && strings.EqualFold(meta.Class, avesClass),
		known:  found && meta.Class != "",
	}
	birdClassCache.Store(key, v)
	return v.isBird, v.known
}

// IsBirdCapableModel reports whether a model classifies birds. It reuses the
// model-type resolution the v2 datastore already uses to stamp a detection's
// taxonomic class, so a future model is classified by the same rule rather than
// by an allow-list here that it would silently fall out of.
//
// An unknown model ID resolves to the BirdNET default, i.e. bird-capable.
func IsBirdCapableModel(modelID string) bool {
	info := DetectionModelInfoForID(modelID)
	return detection.ResolveModelType(info.Name, info.Version) != entities.ModelTypeBat
}

// SpeciesSharedByBirdModels reports whether at least minModels bird-capable
// models are active and every one of them can predict scientificName. It is the
// "could a second model realistically have confirmed this?" test.
//
// restrictTo, when non-nil, narrows the answer to that set of model IDs — the
// processor passes the models actually analysing the audio source, since a model
// that never sees the source can never confirm a detection on it. A nil
// restrictTo considers every loaded model.
//
// ok is false when the answer cannot be trusted (no orchestrator, unusable
// species name, or a model whose labels cannot be read because its instance is
// mid-reload). Callers must fail open on it rather than read shared=false.
//
// Locking follows AllLabels: model IDs and entry pointers are snapshotted under
// o.mu, which is released before any entry.mu is taken, because the reload,
// unload and delete paths deliberately order those two locks the other way.
func (o *Orchestrator) SpeciesSharedByBirdModels(scientificName string, restrictTo []string, minModels int) (shared, ok bool) {
	if o == nil || minModels < 1 {
		return false, false
	}
	key := canonicalSpeciesKey(scientificName)
	if key == "" {
		return false, false
	}

	o.mu.RLock()
	primary := o.primary
	primaryID := o.ModelInfo.ID
	refs := make([]entryRef, 0, len(o.models))
	for id, entry := range o.models {
		// IsModelActive reads an atomic, not o.mu, so it is safe under the RLock.
		if !IsBirdCapableModel(id) || !o.IsModelActive(id) {
			continue
		}
		if restrictTo != nil && !slices.Contains(restrictTo, id) {
			continue
		}
		refs = append(refs, entryRef{id: id, entry: entry})
	}
	o.mu.RUnlock()

	// Reading label sets is by far the expensive part of this call (a clone of up
	// to ~15k strings per model, then a canonicalization per label), so settle the
	// quorum first: below it the answer is already no.
	if len(refs) < minModels {
		return false, true
	}

	for _, ref := range refs {
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
			return false, false
		}
		if !slices.ContainsFunc(labels, func(label string) bool { return canonicalSpeciesKey(label) == key }) {
			// One model that cannot predict the species settles it; skip the rest.
			return false, true
		}
	}

	return true, true
}
