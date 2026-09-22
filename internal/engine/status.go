// Package engine owns deterministic domain rules. It must remain independent
// from terminals, filesystems, clocks, and networks.
package engine

// Status exposes only whether the game engine has been implemented. Full game
// state and commands are intentionally deferred to TASK-04 and TASK-05.
type Status struct {
	Implemented bool
	Reason      string
}

// CurrentStatus is pure and performs no I/O.
func CurrentStatus() Status {
	return Status{
		Implemented: false,
		Reason:      "game contracts begin in TASK-04",
	}
}
