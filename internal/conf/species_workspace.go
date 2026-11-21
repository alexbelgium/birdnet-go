package conf

import "slices"

// SpeciesWorkspaceSortAscending and SpeciesWorkspaceSortDescending are the sort directions.
const (
	SpeciesWorkspaceSortAscending  = "asc"
	SpeciesWorkspaceSortDescending = "desc"
)

// SpeciesWorkspaceColumn is one column of the species workspace table.
type SpeciesWorkspaceColumn struct {
	ID      string `yaml:"id" json:"id"`           // column identifier from SpeciesWorkspaceColumnIDs
	Visible bool   `yaml:"visible" json:"visible"` // whether the column is shown
}

// SpeciesWorkspaceSort is the saved sort of the species workspace table.
type SpeciesWorkspaceSort struct {
	Column    string `yaml:"column" json:"column"`       // column identifier
	Direction string `yaml:"direction" json:"direction"` // "asc" or "desc"
}

// SpeciesWorkspaceLayout is the saved column order, visibility, sort and phone
// density of the species workspace. It is written only through
// /api/v2/species-workspace/layout.
type SpeciesWorkspaceLayout struct {
	Columns   []SpeciesWorkspaceColumn `yaml:"columns,omitempty" json:"columns"` // ordered columns
	Sort      SpeciesWorkspaceSort     `yaml:"sort,omitempty" json:"sort"`       // active sort
	Condensed bool                     `yaml:"condensed" json:"condensed"`       // use two-line rows on phones
}

// speciesWorkspaceColumn describes a known column and its defaults.
type speciesWorkspaceColumn struct {
	id             string
	mandatory      bool // always present and visible
	visibleDefault bool
	sortable       bool
}

// speciesWorkspaceColumns is the column registry, in default order. It must stay
// in sync with the frontend registry (features/species-workspace/columns.ts).
var speciesWorkspaceColumns = []speciesWorkspaceColumn{
	{id: "species", mandatory: true, visibleDefault: true, sortable: true},
	{id: "count", visibleDefault: true, sortable: true},
	{id: "maxConfidence", visibleDefault: true, sortable: true},
	{id: "firstSeen", sortable: true},
	{id: "lastSeen", visibleDefault: true, sortable: true},
	{id: "range", sortable: true},
	{id: "verification", visibleDefault: true, sortable: true},
	{id: "excluded", sortable: true},
	{id: "included", sortable: true},
	{id: "confirmed", visibleDefault: true, sortable: true},
	{id: "bestRecording", visibleDefault: true},
	{id: "actions", mandatory: true, visibleDefault: true},
}

// DefaultSpeciesWorkspaceLayout returns the layout used when none is saved.
func DefaultSpeciesWorkspaceLayout() SpeciesWorkspaceLayout {
	cols := make([]SpeciesWorkspaceColumn, 0, len(speciesWorkspaceColumns))
	for _, c := range speciesWorkspaceColumns {
		cols = append(cols, SpeciesWorkspaceColumn{ID: c.id, Visible: c.visibleDefault})
	}
	return SpeciesWorkspaceLayout{
		Columns: cols,
		Sort:    SpeciesWorkspaceSort{Column: "count", Direction: SpeciesWorkspaceSortDescending},
	}
}

// NormalizeSpeciesWorkspaceLayout returns a valid layout: unknown and duplicate
// columns are dropped, mandatory columns are forced visible, columns missing from
// the saved order are appended with their defaults, and an unsortable or unknown
// sort falls back to the default.
func NormalizeSpeciesWorkspaceLayout(in SpeciesWorkspaceLayout) SpeciesWorkspaceLayout {
	known := make(map[string]speciesWorkspaceColumn, len(speciesWorkspaceColumns))
	for _, c := range speciesWorkspaceColumns {
		known[c.id] = c
	}
	out := SpeciesWorkspaceLayout{
		Columns:   make([]SpeciesWorkspaceColumn, 0, len(speciesWorkspaceColumns)),
		Condensed: in.Condensed,
	}
	seen := make(map[string]bool, len(speciesWorkspaceColumns))
	for _, col := range in.Columns {
		def, ok := known[col.ID]
		if !ok || seen[col.ID] {
			continue
		}
		seen[col.ID] = true
		out.Columns = append(out.Columns, SpeciesWorkspaceColumn{ID: col.ID, Visible: col.Visible || def.mandatory})
	}
	for _, def := range speciesWorkspaceColumns {
		if !seen[def.id] {
			out.Columns = append(out.Columns, SpeciesWorkspaceColumn{ID: def.id, Visible: def.visibleDefault})
		}
	}
	defaults := DefaultSpeciesWorkspaceLayout().Sort
	out.Sort = in.Sort
	sortDef, ok := known[in.Sort.Column]
	if !ok || !sortDef.sortable {
		out.Sort.Column = defaults.Column
	}
	if !slices.Contains([]string{SpeciesWorkspaceSortAscending, SpeciesWorkspaceSortDescending}, in.Sort.Direction) {
		out.Sort.Direction = defaults.Direction
	}
	return out
}
