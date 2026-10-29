package classifier

// batCommonNames maps scientific names shared by bat classifiers to their
// English common names. Classifier-specific label files remain authoritative
// for class order and membership.
var batCommonNames = map[string]string{
	"Barbastella barbastellus":  "Western barbastelle",
	"Eptesicus nilssonii":       "Northern bat",
	"Eptesicus serotinus":       "Serotine bat",
	"Hypsugo savii":             "Savi's pipistrelle",
	"Miniopterus schreibersii":  "Common bent-wing bat",
	"Myotis alcathoe":           "Alcathoe bat",
	"Myotis bechsteinii":        "Bechstein's bat",
	"Myotis brandtii":           "Brandt's bat",
	"Myotis crypticus":          "Cryptic myotis",
	"Myotis daubentonii":        "Daubenton's bat",
	"Myotis mystacinus":         "Whiskered bat",
	"Myotis nattereri":          "Natterer's bat",
	"Nyctalus leisleri":         "Lesser noctule",
	"Nyctalus noctula":          "Common noctule",
	"Pipistrellus kuhlii":       "Kuhl's pipistrelle",
	"Pipistrellus nathusii":     "Nathusius' pipistrelle",
	"Pipistrellus pipistrellus": "Common pipistrelle",
	"Pipistrellus pygmaeus":     "Soprano pipistrelle",
	"Plecotus auritus":          "Brown long-eared bat",
	"Plecotus austriacus":       "Grey long-eared bat",
	"Rhinolophus euryale":       "Mediterranean horseshoe bat",
	"Rhinolophus ferrumequinum": "Greater horseshoe bat",
	"Rhinolophus hipposideros":  "Lesser horseshoe bat",
	"Tadarida teniotis":         "European free-tailed bat",
	"Vespertilio murinus":       "Parti-coloured bat",
}
