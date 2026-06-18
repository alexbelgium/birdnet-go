package main

// localOverrides contains verified common-name translations that are absent or
// incorrect in the upstream OpenFauna dataset. They are applied after
// BuildLocaleDictionary so that a localized override always wins over a Latin-
// name placeholder regardless of what the upstream CSV contains.
//
// Entries here fall into two categories:
//  1. Real species whose OpenFauna record uses the Latin name as a placeholder
//     (e.g. Pipistrellus pipistrellus has no French row, or the French row is
//     "Pipistrellus pipistrellus"). We supply the correct vernacular name.
//  2. BirdNET-specific pseudo-entries (e.g. "Plecotus spec.") that will never
//     appear in OpenFauna because they are not canonical species. We provide
//     locale-appropriate labels for the dashboard.
//
// Sources: SFEPM (fr bats), SECEM (es bats), GIRC (it bats), ArtDatabanken (sv
// bats), standard regional checklists (it birds), IOC World Bird List names.
//
// Format: localOverrides[uiLocale][scientificName] = commonName
var localOverrides = map[string]map[string]string{
	"fr": {
		// Bats – Chiroptera
		"Myotis dasycneme":          "Murin des marais",
		"Myotis daubentonii":        "Murin de Daubenton",
		"Myotis emarginatus":        "Murin à oreilles échancrées",
		"Myotis myotis":             "Grand Murin",
		"Myotis mystacinus":         "Murin à moustaches",
		"Myotis nattereri":          "Murin de Natterer",
		"Nyctalus lasiopterus":      "Grande Noctule",
		"Nyctalus leisleri":         "Noctule de Leisler",
		"Nyctalus noctula":          "Noctule commune",
		"Pipistrellus kuhlii":       "Pipistrelle de Kuhl",
		"Pipistrellus maderensis":   "Pipistrelle de Madère",
		"Pipistrellus nathusii":     "Pipistrelle de Nathusius",
		"Pipistrellus pipistrellus": "Pipistrelle commune",
		"Pipistrellus pygmaeus":     "Pipistrelle pygmée",
		"Plecotus spec.":            "Oreillard sp.",
		"Rhinolophus blasii":        "Rhinolophe de Blasius",
		"Rhinolophus ferrumequinum": "Grand Rhinolophe",
		"Rhinolophus hipposideros":  "Petit Rhinolophe",
		"Tadarida teniotis":         "Molosse de Cestoni",
		"Vespertilio murinus":       "Sérotine bicolore",
	},
	"es": {
		// Bats – Chiroptera
		"Myotis dasycneme":          "Murciélago de charca",
		"Myotis daubentonii":        "Murciélago ratonero ribereño",
		"Myotis emarginatus":        "Murciélago ratonero pardo",
		"Myotis myotis":             "Murciélago ratonero grande",
		"Myotis mystacinus":         "Murciélago ratonero bigotudo",
		"Myotis nattereri":          "Murciélago ratonero gris",
		"Nyctalus lasiopterus":      "Nóctulo grande",
		"Nyctalus leisleri":         "Nóctulo mediano",
		"Nyctalus noctula":          "Nóctulo común",
		"Pipistrellus kuhlii":       "Murciélago de borde claro",
		"Pipistrellus maderensis":   "Murciélago enano de Madeira",
		"Pipistrellus nathusii":     "Murciélago de Nathusius",
		"Pipistrellus pipistrellus": "Murciélago enano",
		"Pipistrellus pygmaeus":     "Murciélago soprano",
		"Plecotus spec.":            "Orejudo sp.",
		"Rhinolophus blasii":        "Murciélago de herradura mediterráneo",
		"Rhinolophus ferrumequinum": "Murciélago grande de herradura",
		"Rhinolophus hipposideros":  "Murciélago pequeño de herradura",
		"Tadarida teniotis":         "Murciélago rabudo",
		"Vespertilio murinus":       "Murciélago bicolor",
	},
	"it": {
		// Bats – Chiroptera
		"Myotis dasycneme":          "Vespertilio dasicneme",
		"Myotis daubentonii":        "Vespertilio di Daubenton",
		"Myotis emarginatus":        "Vespertilio smarginato",
		"Myotis myotis":             "Vespertilio maggiore",
		"Myotis mystacinus":         "Vespertilio mustacchino",
		"Myotis nattereri":          "Vespertilio di Natterer",
		"Nyctalus lasiopterus":      "Nottola gigante",
		"Nyctalus leisleri":         "Nottola di Leisler",
		"Nyctalus noctula":          "Nottola comune",
		"Pipistrellus kuhlii":       "Pipistrello di Kuhl",
		"Pipistrellus maderensis":   "Pipistrello di Madeira",
		"Pipistrellus nathusii":     "Pipistrello di Nathusius",
		"Pipistrellus pipistrellus": "Pipistrello nano",
		"Pipistrellus pygmaeus":     "Pipistrello soprano",
		"Plecotus spec.":            "Orecchione sp.",
		"Rhinolophus blasii":        "Ferro di cavallo di Blasius",
		"Rhinolophus ferrumequinum": "Ferro di cavallo maggiore",
		"Rhinolophus hipposideros":  "Ferro di cavallo minore",
		"Tadarida teniotis":         "Molosso di Cestoni",
		"Vespertilio murinus":       "Serotino bicolore",
		// Birds – Aves
		"Antilophia galeata":    "Manachino dal cappuccio",
		"Charadrius collaris":   "Corriere collarino",
		"Charadrius falklandicus": "Corriere delle Falkland",
		"Charadrius javanicus":  "Corriere di Giava",
		"Charadrius modestus":   "Corriere australe",
		"Charadrius peronii":    "Corriere di Peron",
		"Charadrius veredus":    "Corriere orientale",
		"Charadrius wilsonia":   "Corriere di Wilson",
		"Ixobrychus minutus":    "Tarabusino",
	},
	"sv": {
		// Bats – Chiroptera
		"Myotis dasycneme":          "Dammfladdermus",
		"Myotis daubentonii":        "Vattenfladdermus",
		"Myotis myotis":             "Stor musöra",
		"Myotis mystacinus":         "Mustaschfladdermus",
		"Myotis nattereri":          "Fransfladdermus",
		"Nyctalus leisleri":         "Leislers fladdermus",
		"Nyctalus noctula":          "Stor fladdermus",
		"Pipistrellus kuhlii":       "Kuhlpipistrell",
		"Pipistrellus nathusii":     "Nathusius pipistrell",
		"Pipistrellus pipistrellus": "Dvärgfladdermus",
		"Pipistrellus pygmaeus":     "Sopranpipistrell",
		"Plecotus auritus":          "Brunlångöra",
		"Vespertilio murinus":       "Gråfladdermus",
	},
}

// applyOverrides writes each entry from localOverrides[locale] into dict,
// but only when the current value is the Latin name used as a placeholder
// (i.e. key == value). This avoids clobbering any legitimate upstream
// translation that the openfauna dataset may later supply for this species.
func applyOverrides(dict map[string]string, locale string) {
	entries, ok := localOverrides[locale]
	if !ok {
		return
	}
	for sci, common := range entries {
		if current, exists := dict[sci]; !exists || current == sci {
			dict[sci] = common
		}
	}
}
