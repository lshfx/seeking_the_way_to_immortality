// Package panel defines read-only presentation data. Domain calculations must
// stay in engine and session rather than being duplicated here.
package panel

// Model is deliberately minimal in TASK-03. TASK-04 will version the complete
// PanelModel contract after terminal and state contracts converge.
type Model struct {
	Title   string
	Message string
}
