// Package engine owns deterministic domain rules. It must remain independent
// from terminals, filesystems, clocks, and networks.
package engine

// Status exposes the implemented playable slice without claiming the entire
// planned M1 game is complete.
type Status struct {
	Implemented bool
	Reason      string
}

// CurrentStatus is pure and performs no I/O.
func CurrentStatus() Status {
	return Status{
		Implemented: true,
		Reason:      "TASK-09 short loop: character creation, ordinary cultivation, local save and recovery",
	}
}
