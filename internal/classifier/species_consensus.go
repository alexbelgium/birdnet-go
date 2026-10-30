// species_consensus.go exposes the per-model species support, model-family and
// taxonomic-class lookups used by the processor's first-daily-detection
// consensus rule for both birds and bats. All of it is read-only queries over
// data the orchestrator and the OpenFauna dataset already hold; nothing here
// participates in inference.
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
// An unknown model ID resolves to the BirdNET default, i.e. bird-capable. This
// also reports true for a multi-taxa model (Perch, BirdNET v3.0+): it predicts
// birds alongside other taxa, so it is a valid peer for bird confirmation.
func IsBirdCapableModel(modelID string) bool {
	return resolveModelFamily(modelID) != entities.ModelTypeBat
}

// IsBatCapableModel reports whether a model classifies bats. Unlike the
// multi-taxa models bird confirmation also accepts, a bat model's label set is
// exclusively bat species, so this is also a definitive "scientificName is a
// bat species" signal: no taxonomy lookup can tell that apart from a species
// OpenFauna simply has not catalogued (see IsBirdSpecies), but the model that
// produced the detection can.
func IsBatCapableModel(modelID string) bool {
	return resolveModelFamily(modelID) == entities.ModelTypeBat
}

// resolveModelFamily is the shared model-type lookup IsBirdCapableModel and
// IsBatCapableModel are both defined in terms of, so the two can never disagree
// about a given model ID.
func resolveModelFamily(modelID string) entities.ModelType {
	info := DetectionModelInfoForID(modelID)
	return detection.ResolveModelType(info.Name, info.Version)
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
func (o *Orchestrator) SpeciesSharedByBirdModels(scientificName string, restrictTo []string, minModels int) (shared, ok bool) {
	return o.speciesSharedByModelFamily(scientificName, restrictTo, minModels, IsBirdCapableModel)
}

// SpeciesSharedByBatModels is SpeciesSharedByBirdModels' bat-capable
// counterpart, for the same consensus rule applied to a bat detection.
func (o *Orchestrator) SpeciesSharedByBatModels(scientificName string, restrictTo []string, minModels int) (shared, ok bool) {
	return o.speciesSharedByModelFamily(scientificName, restrictTo, minModels, IsBatCapableModel)
}

// speciesSharedByModelFamily is SpeciesSharedByBirdModels and
// SpeciesSharedByBatModels' shared implementation, parameterized on which
// models are eligible peers.
//
// Locking follows AllLabels: model IDs and entry pointers are snapshotted under
// o.mu, which is released before any entry.mu is taken. Instances are captured
// under entry.mu, then Labels is called after releasing it, matching AllLabels.
func (o *Orchestrator) speciesSharedByModelFamily(scientificName string, restrictTo []string, minModels int, eligible func(string) bool) (shared, ok bool) {
	if o == nil || minModels < 1 {
		return false, false
	}
	key := canonicalSpeciesKey(scientificName)
	if key == "" {
		return false, false
	}

	o.mu.RLock()
	refs := make([]entryRef, 0, len(o.models))
	for id, entry := range o.models {
		// IsModelActive reads an atomic, not o.mu, so it is safe under the RLock.
		if !eligible(id) || !o.IsModelActive(id) {
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
		ref.entry.mu.Lock()
		instance := ref.entry.instance
		ref.entry.mu.Unlock()
		var labels []string
		if instance != nil {
			labels = instance.Labels()
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
