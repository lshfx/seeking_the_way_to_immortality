package engine

// Deterministic random streams.
//
// The engine must be able to replay: the same snapshot plus the same action
// sequence must yield the same mechanical state, on any machine, forever. That
// rules out math/rand (whose global source is seedable but whose default is not
// stable across Go releases) and crypto/rand (non-reproducible by definition).
//
// The generator here is a SplitMix64-style counter-based mixer. Each draw is a
// pure function of (seed state, counter), so a stream can be advanced, snapshotted
// and restored without carrying a hidden history. Nothing here reads the clock,
// the environment, or any entropy source.

// RNG algorithm identifiers. The algorithm name travels inside every stream so
// that a future generator is never silently reinterpreted as this one: a save
// written under one algorithm must be recognised as foreign rather than
// decoded into plausible-looking but wrong numbers.
const (
	// RNGAlgorithmSplitMix64 is the M1 generator.
	RNGAlgorithmSplitMix64 = "splitmix64-counter-v1"
)

// splitMix64 constants. These are the reference SplitMix64 mixing constants;
// the generator's output depends on them, so they are part of the format.
const (
	sm64Gamma = uint64(0x9e3779b97f4a7c15)
	sm64Mul1  = uint64(0xbf58476d1ce4e5b9)
	sm64Mul2  = uint64(0x94d049bb133111eb)
)

// Stream is a mutable cursor over one named random stream.
//
// It is deliberately not a global: TASK-05's pipeline takes a stream explicitly
// so that a preview or a query cannot accidentally advance the game's randomness.
type Stream struct {
	state     uint64
	counter   uint64
	algorithm string
}

// NewStream creates a stream with the given seed. The seed is whatever the game
// derives from the game identity; it is not a wall-clock value.
func NewStream(seed uint64) *Stream {
	return &Stream{
		state:     seed,
		counter:   0,
		algorithm: RNGAlgorithmSplitMix64,
	}
}

// Algorithm reports the generator identity.
func (s *Stream) Algorithm() string { return s.algorithm }

// Counter reports how many draws have been taken. It is persisted so that a
// resumed game continues the sequence rather than repeating it.
func (s *Stream) Counter() uint64 { return s.counter }

// State reports the seed state.
func (s *Stream) State() uint64 { return s.state }

// Next returns the next raw 64-bit value and advances the counter.
//
// It is a pure function of (state, counter): draw N is always
// mix(state + N*gamma) regardless of how many draws happened before, which is
// what makes a restored stream resume mid-sequence correctly.
func (s *Stream) Next() uint64 {
	z := s.state + s.counter*sm64Gamma
	s.counter++

	z = (z ^ (z >> 30)) * sm64Mul1
	z = (z ^ (z >> 27)) * sm64Mul2
	return z ^ (z >> 31)
}

// NextUint64N returns a value in [0, n) using rejection sampling, so the
// distribution is exactly uniform rather than slightly biased by a modulo.
//
// A biased roll would be a silent correctness bug: over enough trials it would
// make rare outcomes measurably more common than the design's stated odds, and
// it would be almost impossible to notice from play.
//
// n must be positive; n <= 1 returns 0 without consuming a draw.
func (s *Stream) NextUint64N(n uint64) uint64 {
	if n <= 1 {
		return 0
	}

	// Discard the top `threshold` values that would make the modulo uneven.
	threshold := (^uint64(0) - n + 1) % n
	for {
		v := s.Next()
		if v >= threshold {
			return v % n
		}
	}
}

// IntRange returns a value in [lo, hi] inclusive. It is the form most rule text
// uses ("造成 3~5 点伤害").
func (s *Stream) IntRange(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	span := uint64(hi-lo) + 1
	return lo + int(s.NextUint64N(span))
}

// Chance reports whether an event with the given permille probability occurs.
//
// Probability is expressed in permille rather than a float because the engine
// bans floating point: a float threshold would make the same save produce
// different results on a differently optimising build.
func (s *Stream) Chance(permille int) bool {
	if permille <= 0 {
		return false
	}
	if permille >= PermilleScale {
		// A certainty still consumes no draw, so a guaranteed outcome does not
		// shift everything after it.
		return true
	}
	return s.NextUint64N(PermilleScale) < uint64(permille)
}

// PercentChance is Chance expressed in whole percent, for rule text that reads
// "30% 概率".
func (s *Stream) PercentChance(percent int) bool {
	return s.Chance(percent * (PermilleScale / PercentScale))
}

// RollSCALE returns a fixed-point value in [0, SCALE] inclusive. It exists so
// that fractional multipliers can be drawn without introducing floats.
func (s *Stream) RollSCALE() int {
	return int(s.NextUint64N(SCALE + 1))
}

// Snapshot captures the cursor for persistence.
func (s *Stream) Snapshot() RNGStream {
	return RNGStream{
		State:     s.state,
		Counter:   s.counter,
		Algorithm: s.algorithm,
	}
}

// LoadStream restores a stream from a persisted cursor.
//
// It fails rather than guessing when the algorithm is unknown: resuming under
// the wrong generator would produce a sequence that looks random and is
// therefore very hard to diagnose, whereas refusing is immediate and obvious.
func LoadStream(snap RNGStream) (*Stream, bool) {
	if snap.Algorithm != RNGAlgorithmSplitMix64 {
		return nil, false
	}
	return &Stream{
		state:     snap.State,
		counter:   snap.Counter,
		algorithm: snap.Algorithm,
	}, true
}

// Clone returns an independent copy. The pipeline evaluates a command on a
// copy so that a rejected command leaves no trace in the live stream.
func (s *Stream) Clone() *Stream {
	c := *s
	return &c
}

// RNGSet holds one stream per named domain, so that a combat roll cannot shift
// which world event fires next.
type RNGSet struct {
	streams map[string]*Stream
}

// NewRNGSet builds a set from a single seed, deriving an independent sub-seed
// per stream. Deriving rather than reusing the seed matters: identical seeds
// would make the combat and drop streams emit correlated values, which is the
// kind of correlation that makes a drop table look rigged.
func NewRNGSet(seed uint64) *RNGSet {
	set := &RNGSet{streams: make(map[string]*Stream, len(WorldStreamNames))}
	for i, name := range WorldStreamNames {
		// A distinct odd offset per stream name keeps the sub-seeds apart.
		sub := seed + uint64(i+1)*sm64Gamma
		// Mix once so that adjacent seeds do not produce adjacent first draws.
		sub = (sub ^ (sub >> 30)) * sm64Mul1
		set.streams[name] = NewStream(sub)
	}
	return set
}

// Stream returns the named stream. It returns nil for an unknown name rather
// than creating one on the fly: a typo would otherwise silently establish a new
// stream that never gets persisted.
func (set *RNGSet) Stream(name string) *Stream {
	if set == nil {
		return nil
	}
	return set.streams[name]
}

// World is shorthand for the world-event stream.
func (set *RNGSet) World() *Stream { return set.Stream(StreamWorld) }

// Combat is shorthand for the combat stream.
func (set *RNGSet) Combat() *Stream { return set.Stream(StreamCombat) }

// Drop is shorthand for the drop stream.
func (set *RNGSet) Drop() *Stream { return set.Stream(StreamDrop) }

// NPC is shorthand for the NPC stream.
func (set *RNGSet) NPC() *Stream { return set.Stream(StreamNPC) }

// Snapshot captures every stream for persistence.
func (set *RNGSet) Snapshot() RNGState {
	out := RNGState{
		Algorithm: RNGAlgorithmSplitMix64,
		Streams:   make(map[string]RNGStream, len(set.streams)),
	}
	for name, st := range set.streams {
		out.Streams[name] = st.Snapshot()
	}
	return out
}

// Clone returns a deep copy, so the pipeline can evaluate against a throwaway
// set.
func (set *RNGSet) Clone() *RNGSet {
	out := &RNGSet{streams: make(map[string]*Stream, len(set.streams))}
	for name, st := range set.streams {
		out.streams[name] = st.Clone()
	}
	return out
}

// LoadRNGSet restores every stream from a persisted state. It fails if any
// required stream is absent or names an unknown algorithm, so a partially
// written save cannot be resumed into a subtly different sequence.
func LoadRNGSet(state RNGState) (*RNGSet, bool) {
	if state.Algorithm != RNGAlgorithmSplitMix64 {
		return nil, false
	}
	out := &RNGSet{streams: make(map[string]*Stream, len(WorldStreamNames))}
	for _, name := range WorldStreamNames {
		snap, ok := state.Streams[name]
		if !ok {
			return nil, false
		}
		st, ok := LoadStream(snap)
		if !ok {
			return nil, false
		}
		out.streams[name] = st
	}
	return out, true
}

// SeedFromIdentity derives a stable seed from the game and branch identity.
//
// It uses the same FNV-1a primitive as the save integrity digest so the engine
// needs only one hashing implementation. Deriving from identity rather than a
// clock is what makes a replay reproducible.
func SeedFromIdentity(gameID, branchID string) uint64 {
	return fnv1a64([]byte(gameID + "\x00" + branchID))
}

// fnv1a64 returns the raw FNV-1a 64-bit digest. fnv1a64Hex in save.go renders
// the same function as hex; both exist so neither call site has to format and
// then parse.
func fnv1a64(data []byte) uint64 {
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)
	h := offset64
	for _, c := range data {
		h ^= uint64(c)
		h *= prime64
	}
	return h
}
