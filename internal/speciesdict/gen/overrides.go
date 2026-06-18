package main

// localOverrides provides translations for species that appear untranslated
// (key == value) in the OpenFauna dataset. Applied only when placeholder detected.
// Sources: GBIF vernacular names API, iNaturalist preferred common names.
var localOverrides = map[string]map[string]string{
	"cs": {
		"Isophya costata":        "kobylka širočelá",
		"Melidectes torquatus":   "květosavka pestrá",
		"Tettigonia cantans":     "kobylka zpěvavá",
		"Tettigonia viridissima": "kobylka zelená",
	},
	"de": {
		"Cicada orni":             "Eschenzikade",
		"Cicadetta brevipennis":   "Kurzflügel-Singzikade",
		"Cicadetta cantilatrix":   "Honigader-Bergzikade",
		"Cicadetta montana":       "Bergsingzikade",
		"Dimissalna dimissa":      "Südliche Bergsingzikade",
		"Gampsocleis glabra":      "Heideschrecke",
		"Meconema thalassinum":    "Eichenschrecke",
		"Oligoglena tibialis":     "Zwergsingzikade",
		"Rhacocleis annulata":     "Italienische Strauchschrecke",
		"Tettigettalna argentata": "Silbrige Zikade",
		"Tettigettula pygmea":     "Zwergsingzikade",
		"Thyreonotus corsicus":    "Korsische Schildschrecke",
		"Tibicina quadrisignata":  "Schwarzer Scherenschleifer",
		"Tibicina steveni":        "Gelber Scherenschleifer",
	},
	// Note: many bat entries returned Estonian names via GBIF (ISO 639-2 "est" matches "es"
	// prefix check), so only confirmed Spanish names are included here.
	"es": {
		"Cicada barbara":           "Cigarra del Olivo",
		"Myotis mystacinus":        "Murciélago Bigotudo",
		"Pipistrellus maderensis":  "Murciélago De Madeira",
		"Plecotus spec.":           "Orejudo sp.",
		"Rhinolophus blasii":       "murciélago dálmata de herradura",
		"Rhinolophus hipposideros": "Murciélago pequeño de herradura",
	},
	// Note: some bat entries returned West Frisian names via GBIF ("frl" matches "fr"
	// prefix check); only confirmed French names are included here.
	"fr": {
		"Myotis daubentonii":        "Murin de Daubenton",
		"Myotis emarginatus":        "Murin a oreilles échancrées",
		"Myotis myotis":             "Grand Murin",
		"Myotis mystacinus":         "Murin a moustaches",
		"Myotis nattereri":          "Murin de Natterer",
		"Nyctalus lasiopterus":      "Grande Noctule",
		"Nyctalus noctula":          "Noctule commune",
		"Pipistrellus kuhlii":       "Pipistrelle de Kuhl",
		"Pipistrellus maderensis":   "Pipistrelle de Madère",
		"Pipistrellus nathusii":     "Pipistrelle de Nathusius",
		"Pipistrellus pipistrellus": "Pipistrelle commune",
		"Pipistrellus pygmaeus":     "Pipistrelle pygmée",
		"Plecotus spec.":            "Oreillard sp.",
		"Rhinolophus blasii":        "Rhinolophe de Blasius",
		"Rhinolophus ferrumequinum": "Grand Rhinolophe",
		"Tadarida teniotis":         "Molosse de Cestoni",
		"Vespertilio murinus":       "Sérotine bicolore",
	},
	// Note: "Ialtóg Leisler" (Irish Gaelic) removed from Nyctalus leisleri.
	"it": {
		"Ixobrychus minutus":        "Tarabusino",
		"Myotis dasycneme":          "Vespertilio dasicneme",
		"Myotis daubentonii":        "Vespertilio di Daubenton",
		"Myotis emarginatus":        "Vespertilio smarginato",
		"Myotis myotis":             "Vespertilio maggiore",
		"Myotis mystacinus":         "Vespertilio mustacchino",
		"Myotis nattereri":          "Vespertilio di Natterer",
		"Nyctalus lasiopterus":      "Nottola gigante",
		"Nyctalus noctula":          "Nottola comune",
		"Pipistrellus kuhlii":       "Pipistrello albolimbato",
		"Pipistrellus maderensis":   "Pipistrello di Madeira",
		"Pipistrellus nathusii":     "Pipistrello di Nathusius",
		"Pipistrellus pipistrellus": "Pipistrello nano",
		"Pipistrellus pygmaeus":     "Pipistrello pigmeo",
		"Plecotus spec.":            "Orecchione sp.",
		"Rhinolophus blasii":        "Ferro di cavallo di Blasius",
		"Rhinolophus ferrumequinum": "Ferro di cavallo maggiore",
		"Rhinolophus hipposideros":  "Ferro di cavallo minore",
		"Tadarida teniotis":         "Molosso del Cestoni",
		"Vespertilio murinus":       "Serotino bicolore",
	},
	"pt": {
		"Cicada barbara":          "Cigarra-comum-do-sul",
		"Cicada orni":             "Cigarra-comum",
		"Euryphara contentei":     "Flautista-de-olhos-vermelhos",
		"Hilaphura varipes":       "Cigarrão-abelhudo",
		"Meconema thalassinum":    "Saltão-dos-carvalhos",
		"Tettigettalna argentata": "Cigarra-prateada",
		"Tettigettalna estrellae": "Cigarra-das-serras",
		"Tettigettalna josei":     "Cigarra-de-josé",
		"Tettigettalna mariae":    "Cigarra-de-maria",
		"Tibicina garricola":      "Flautista-dos-matos",
		"Tibicina quadrisignata":  "Cigarra-de-quatro-pintas",
	},
	"sv": {
		"Myotis dasycneme":          "dammfladdermus",
		"Myotis daubentonii":        "vattenfladdermus",
		"Myotis myotis":             "större musöra",
		"Myotis mystacinus":         "mustaschfladdermus",
		"Myotis nattereri":          "fransfladdermus",
		"Nyctalus leisleri":         "mindre brunfladdermus",
		"Nyctalus noctula":          "större brunfladdermus",
		"Pipistrellus kuhlii":       "parkpipistrell",
		"Pipistrellus nathusii":     "trollpipistrell",
		"Pipistrellus pipistrellus": "pipistrell",
		"Pipistrellus pygmaeus":     "dvärgpipistrell",
		"Plecotus auritus":          "Brunlångöra",
		"Plecotus spec.":            "Fladdermus sp.",
		"Vespertilio murinus":       "gråskimlig fladdermus",
	},
}

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
