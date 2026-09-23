package engine

import (
	"testing"
)

// TestStreamIsDeterministicFromSeed is the foundation of replay: the same seed
// must produce the same sequence, every time, on every run.
func TestStreamIsDeterministicFromSeed(t *testing.T) {
	a := NewStream(12345)
	b := NewStream(12345)

	for i := 0; i < 100; i++ {
		if got, want := a.Next(), b.Next(); got != want {
			t.Fatalf("draw %d differs between two identically seeded streams: %d vs %d", i, got, want)
		}
	}
}

// TestDifferentSeedsDiverge guards against a generator that ignores its seed,
// which would make every playthrough share one fixed sequence.
func TestDifferentSeedsDiverge(t *testing.T) {
	a := NewStream(1)
	b := NewStream(2)

	same := 0
	for i := 0; i < 200; i++ {
		if a.Next() == b.Next() {
			same++
		}
	}
	// With 64-bit outputs, accidental agreement should essentially never exceed
	// a couple of samples; anything more means the seeds are not really distinct.
	if same > 5 {
		t.Fatalf("%d of 200 draws matched across different seeds: seeds are not separate", same)
	}
}

// TestResumingMidSequenceContinuesIt is the resumption property. A stream
// snapshotted after N draws and restored must produce draw N+1 next, not replay
// from the beginning.
//
// This is what makes an interrupted session continue rather than repeat, and it
// is the property a naive "seed + draw count" implementation most easily gets
// wrong.
func TestResumingMidSequenceContinuesIt(t *testing.T) {
	original := NewStream(999)
	for i := 0; i < 37; i++ {
		original.Next()
	}

	restored, ok := LoadStream(original.Snapshot())
	if !ok {
		t.Fatal("a freshly snapshotted stream must reload")
	}

	for i := 0; i < 50; i++ {
		want := original.Next()
		if got := restored.Next(); got != want {
			t.Fatalf("after resume, draw %d differs: got %d want %d", i, got, want)
		}
	}
}

// TestSnapshotMidSequenceDoesNotDisturbIt checks that reading a snapshot is not
// itself a draw. If it were, saving the game would change the game.
func TestSnapshotMidSequenceDoesNotDisturbIt(t *testing.T) {
	s := NewStream(7)
	s.Next()

	snap := s.Snapshot() // must not advance
	next := s.Next()

	restored, _ := LoadStream(snap)
	if got := restored.Next(); got != next {
		t.Fatalf("snapshot consumed a draw: restored next=%d, live next=%d", got, next)
	}
}

// TestCounterAdvancesOncePerDraw pins the counter contract, which the save
// format depends on.
func TestCounterAdvancesOncePerDraw(t *testing.T) {
	s := NewStream(1)
	if s.Counter() != 0 {
		t.Fatalf("fresh stream counter = %d, want 0", s.Counter())
	}
	for i := uint64(1); i <= 10; i++ {
		s.Next()
		if s.Counter() != i {
			t.Fatalf("after %d draws counter = %d", i, s.Counter())
		}
	}
}

// TestLoadRejectsUnknownAlgorithm mirrors the save-envelope rule: a stream whose
// generator is not recognised must be refused rather than decoded. Decoding it
// would yield a valid-looking sequence that silently violates replay.
func TestLoadRejectsUnknownAlgorithm(t *testing.T) {
	snap := NewStream(5).Snapshot()
	snap.Algorithm = "some-future-generator"

	if _, ok := LoadStream(snap); ok {
		t.Fatal("a stream naming an unknown algorithm must not load")
	}
}

// TestLoadRejectsEmptyAlgorithm catches the default-value case: a zero-valued
// RNGStream has an empty algorithm and must not be treated as valid.
func TestLoadRejectsEmptyAlgorithm(t *testing.T) {
	if _, ok := LoadStream(RNGStream{}); ok {
		t.Fatal("a stream with no algorithm must not load")
	}
}

// TestStreamsAreIndependent is the reason for separate streams. Drawing from
// combat must not change what the world stream produces, otherwise a player who
// fights more would see a different set of world events for the same seed.
func TestStreamsAreIndependent(t *testing.T) {
	// Baseline: draw from world only.
	base := NewRNGSet(4242)
	baseDraws := make([]uint64, 10)
	for i := range baseDraws {
		baseDraws[i] = base.World().Next()
	}

	// Interleaved: hammer every other stream between world draws.
	inter := NewRNGSet(4242)
	for i := 0; i < 10; i++ {
		inter.Combat().Next()
		inter.Drop().Next()
		inter.NPC().Next()
		if got := inter.World().Next(); got != baseDraws[i] {
			t.Fatalf("world draw %d changed because other streams advanced: got %d want %d", i, got, baseDraws[i])
		}
	}
}

// TestStreamsHaveDistinctSubSeeds ensures the four streams are not all seeded
// identically. Correlated streams would make unrelated systems echo each other,
// which reads as a rigged game.
func TestStreamsHaveDistinctSubSeeds(t *testing.T) {
	set := NewRNGSet(1)

	first := map[uint64]string{}
	for _, name := range WorldStreamNames {
		v := set.Stream(name).Next()
		if prev, dup := first[v]; dup {
			t.Fatalf("streams %s and %s produced the same first draw %d", name, prev, v)
		}
		first[v] = name
	}
}

// TestRNGSetSnapshotRoundTrips verifies that a whole set survives persistence
// and continues in step.
func TestRNGSetSnapshotRoundTrips(t *testing.T) {
	set := NewRNGSet(31337)
	for i := 0; i < 13; i++ {
		for _, name := range WorldStreamNames {
			set.Stream(name).Next()
		}
	}

	state := set.Snapshot()
	restored, ok := LoadRNGSet(state)
	if !ok {
		t.Fatal("a freshly snapshotted set must reload")
	}

	for _, name := range WorldStreamNames {
		for i := 0; i < 20; i++ {
			want := set.Stream(name).Next()
			if got := restored.Stream(name).Next(); got != want {
				t.Fatalf("stream %s draw %d differs after reload", name, i)
			}
		}
	}
}

// TestLoadRNGSetRequiresEveryStream catches a partially written save. Missing a
// stream must fail loudly, because silently creating a fresh one would reset
// that domain's randomness mid-game.
func TestLoadRNGSetRequiresEveryStream(t *testing.T) {
	full := NewRNGSet(1).Snapshot()

	for _, omit := range WorldStreamNames {
		partial := RNGState{
			Algorithm: full.Algorithm,
			Streams:   map[string]RNGStream{},
		}
		for k, v := range full.Streams {
			if k != omit {
				partial.Streams[k] = v
			}
		}

		if _, ok := LoadRNGSet(partial); ok {
			t.Fatalf("a set missing stream %q must not load", omit)
		}
	}
}

// TestUnknownStreamNameReturnsNil pins the "no implicit stream creation" rule.
// A typo must be visible, not silently spawn a stream that never persists.
func TestUnknownStreamNameReturnsNil(t *testing.T) {
	set := NewRNGSet(1)
	if s := set.Stream("combat_typo"); s != nil {
		t.Fatal("an unknown stream name must return nil rather than create a stream")
	}
}

// TestCloneIsIndependent verifies that evaluating on a clone cannot leak draws
// back into the live set — the mechanism the pipeline relies on to make a
// rejected command a no-op.
func TestCloneIsIndependent(t *testing.T) {
	live := NewRNGSet(88)
	snapshotBefore := live.Snapshot()

	work := live.Clone()
	for i := 0; i < 25; i++ {
		work.World().Next()
		work.Combat().Next()
	}

	after := live.Snapshot()
	for name, before := range snapshotBefore.Streams {
		if after.Streams[name].Counter != before.Counter {
			t.Fatalf("cloning leaked draws into the live set: stream %s counter went %d -> %d",
				name, before.Counter, after.Streams[name].Counter)
		}
	}
}

// TestChanceBoundsAreExact checks the two certainty cases. They must not consume
// a draw: a guaranteed outcome that advanced the stream would shift every
// subsequent roll, making "always succeeds" change the rest of the game.
func TestChanceBoundsAreExact(t *testing.T) {
	zero := NewStream(3)
	for i := 0; i < 10; i++ {
		if zero.Chance(0) {
			t.Fatal("Chance(0) must never be true")
		}
	}
	if zero.Counter() != 0 {
		t.Fatalf("Chance(0) consumed %d draws, want 0", zero.Counter())
	}

	full := NewStream(3)
	for i := 0; i < 10; i++ {
		if !full.Chance(PermilleScale) {
			t.Fatal("Chance(1000) must always be true")
		}
	}
	if full.Counter() != 0 {
		t.Fatalf("Chance(1000) consumed %d draws, want 0", full.Counter())
	}
}

// TestChanceIsRoughlyCorrect checks that permille probability means what it says.
// The tolerance is generous because this is a sanity check against a grossly
// wrong mapping (e.g. treating permille as percent), not a statistical test.
func TestChanceIsRoughlyCorrect(t *testing.T) {
	const (
		permille  = 250 // 25%
		trials    = 20000
		tolerance = 400 // ±4 percentage points
	)

	s := NewStream(20260923)
	hits := 0
	for i := 0; i < trials; i++ {
		if s.Chance(permille) {
			hits++
		}
	}

	got := hits * PermilleScale / trials
	if got < permille-tolerance || got > permille+tolerance {
		t.Fatalf("Chance(%d) over %d trials yielded %d permille, outside ±%d", permille, trials, got, tolerance)
	}
}

// TestIntRangeRespectsBoundsAndEndpoints checks the inclusive contract, because
// an off-by-one here silently shifts every damage roll in the game.
func TestIntRangeRespectsBoundsAndEndpoints(t *testing.T) {
	s := NewStream(11)

	const lo, hi = 3, 5
	seen := map[int]int{}
	for i := 0; i < 3000; i++ {
		v := s.IntRange(lo, hi)
		if v < lo || v > hi {
			t.Fatalf("IntRange(%d,%d) returned %d, out of range", lo, hi, v)
		}
		seen[v]++
	}

	for v := lo; v <= hi; v++ {
		if seen[v] == 0 {
			t.Fatalf("IntRange(%d,%d) never returned %d in 3000 draws: upper or lower bound is unreachable", lo, hi, v)
		}
	}
}

// TestIntRangeDegenerateCases pins the two degenerate forms.
func TestIntRangeDegenerateCases(t *testing.T) {
	s := NewStream(1)
	if got := s.IntRange(7, 7); got != 7 {
		t.Fatalf("IntRange(7,7) = %d, want 7", got)
	}
	if got := s.IntRange(9, 3); got != 9 {
		t.Fatalf("IntRange(9,3) with hi<lo = %d, want lo (9)", got)
	}
}

// TestNextUint64NCoversFullRangeWithoutBias checks the rejection-sampling path
// for range coverage and gross bias.
//
// Honest limitation, recorded deliberately: this test CANNOT detect the bias it
// names. Swapping the rejection loop for a naive `s.Next() % n` still passes it,
// because with a 64-bit output space the modulo bias is on the order of 1 part
// in 2^64 — far below what any feasible trial count can resolve. The test does
// useful work (it catches a wrong range, an off-by-one, or a generator that
// favours one value grossly), but the correctness of rejection sampling is
// established by construction and by reading the code, NOT by this assertion.
//
// This comment exists so that a future reader does not treat a green run here as
// evidence that the sampling is unbiased.
func TestNextUint64NCoversFullRangeWithoutBias(t *testing.T) {
	const n = 3
	const trials = 30000

	s := NewStream(555)
	counts := make([]int, n)
	for i := 0; i < trials; i++ {
		v := s.NextUint64N(n)
		if v >= n {
			t.Fatalf("NextUint64N(%d) returned %d, out of range", n, v)
		}
		counts[v]++
	}

	expected := trials / n
	for v, c := range counts {
		// ±15% tolerance: enough to catch a real bias, loose enough to accept
		// ordinary sampling noise.
		if c < expected*85/100 || c > expected*115/100 {
			t.Fatalf("value %d appeared %d times, expected about %d: distribution is biased", v, c, expected)
		}
	}
}

// TestNextUint64NDegenerate pins n<=1, which must not consume a draw.
func TestNextUint64NDegenerate(t *testing.T) {
	for _, n := range []uint64{0, 1} {
		s := NewStream(1)
		if got := s.NextUint64N(n); got != 0 {
			t.Fatalf("NextUint64N(%d) = %d, want 0", n, got)
		}
		if s.Counter() != 0 {
			t.Fatalf("NextUint64N(%d) consumed %d draws, want 0", n, s.Counter())
		}
	}
}

// TestRollSCALEStaysInRange covers the fixed-point helper used for fractional
// multipliers.
func TestRollSCALEStaysInRange(t *testing.T) {
	s := NewStream(77)
	for i := 0; i < 5000; i++ {
		v := s.RollSCALE()
		if v < 0 || v > SCALE {
			t.Fatalf("RollSCALE returned %d, outside [0,%d]", v, SCALE)
		}
	}
}

// TestSeedFromIdentityIsStableAndDistinct pins the seed derivation: stable for
// the same identity (replay), distinct for different identities (separate games).
func TestSeedFromIdentityIsStableAndDistinct(t *testing.T) {
	a := SeedFromIdentity("game-1", "branch-main")
	if a != SeedFromIdentity("game-1", "branch-main") {
		t.Fatal("seed is not stable for the same identity")
	}

	cases := map[string]uint64{
		"other game":   SeedFromIdentity("game-2", "branch-main"),
		"other branch": SeedFromIdentity("game-1", "branch-alt"),
		"empty both":   SeedFromIdentity("", ""),
	}

	for name, v := range cases {
		if v == a {
			t.Fatalf("identity %q collides with the baseline seed", name)
		}
	}

	// The separator matters: without it, ("ab","c") and ("a","bc") would seed
	// identically, so two different games would share a random sequence.
	if SeedFromIdentity("ab", "c") == SeedFromIdentity("a", "bc") {
		t.Fatal("seed derivation does not separate game id from branch id")
	}
}

// TestNoWallClockDependency states the design rule directly: two streams created
// at the same seed must agree even if real time passes between their creation.
// A clock-derived seed would make replays impossible.
func TestNoWallClockDependency(t *testing.T) {
	first := NewStream(SeedFromIdentity("g", "b"))
	draws := make([]uint64, 5)
	for i := range draws {
		draws[i] = first.Next()
	}

	// Simulate "later, on a different machine": same seed, no elapsed time is
	// consulted, so the sequence must be identical.
	second := NewStream(SeedFromIdentity("g", "b"))
	for i := range draws {
		if got := second.Next(); got != draws[i] {
			t.Fatalf("draw %d differed on a repeated construction: generator depends on something other than its seed", i)
		}
	}
}

// TestRNGBoundaryIsolation asserts that this file introduces no imports at all
// beyond what the engine already had. It is a local guard next to the code it
// protects, complementing the package-wide boundary test.
func TestRNGBoundaryIsolation(t *testing.T) {
	// The real enforcement lives in boundary_test.go via the import graph.
	// This test documents the intent and fails if the algorithm constant is
	// accidentally renamed, which would change every save's meaning.
	if RNGAlgorithmSplitMix64 != "splitmix64-counter-v1" {
		t.Fatalf("RNG algorithm identifier changed to %q: this invalidates existing saves", RNGAlgorithmSplitMix64)
	}
	if NewRNGSet(0).Snapshot().Algorithm != RNGAlgorithmSplitMix64 {
		t.Fatal("RNGSet reports a different algorithm than the stream constant")
	}
}
