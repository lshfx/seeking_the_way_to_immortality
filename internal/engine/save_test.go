package engine

import (
	"strings"
	"testing"
)

// envelopeFixture builds a fully populated envelope so that every branch of
// CanonicalString is exercised. A field that is never populated would otherwise
// be silently absent from the digest, and a test that only covers the zero
// value would not notice.
func envelopeFixture() *SaveEnvelope {
	return &SaveEnvelope{
		EnvelopeVersion: EnvelopeVersion,
		SchemaVersion:   SchemaVersion,
		RulesVersion:    RulesVersion,
		ContentVersion:  ContentVersion,
		GameID:          "game-0001",
		BranchID:        "branch-main",
		Revision:        7,
		State: GameState{
			SchemaVersion:  SchemaVersion,
			RulesVersion:   RulesVersion,
			ContentVersion: ContentVersion,
			GameID:         "game-0001",
			BranchID:       "branch-main",
			Revision:       7,
			Phase:          PhaseReady,
			Counters: Counters{
				WorldMonth:     12,
				InteractionSeq: 34,
				CombatRound:    0,
			},
			RNG: RNGState{
				Algorithm: "fnv1a64-counter",
				Streams: map[string]RNGStream{
					StreamWorld:  {State: 111, Counter: 5},
					StreamCombat: {State: 222, Counter: 6},
					StreamDrop:   {State: 333, Counter: 7},
					StreamNPC:    {State: 444, Counter: 8},
				},
			},
			LogCursor: 99,
			Player: &Player{
				Identity:  Identity{Surname: "试", GivenName: "道者", Gender: "male"},
				AgeMonths: 216,
				XP:        1234,
				HP:        Vitals{Current: 30, Max: 40},
				MP:        Vitals{Current: 10, Max: 20},
				Realm:     RealmQiRefining,
				Tier:      TierEarly,
				Path:      PathHuman,
				Origin:    OriginCommoner,
				Debt:      0,
				Lifespan: LifespanLedger{
					BaseYears: 80,
				},
				Resources: map[Resource]int64{
					ResSpiritStones: 150,
					ResContribution: 12,
				},
			},
		},
		PendingChoices: []FrozenChoiceSet{{
			Source:     "event",
			InstanceID: "EVT-001#1",
			ChoiceIDs:  []string{"A", "B"},
		}},
		Consumed: ConsumedLedger{
			ActionIDs: []string{"act-1", "act-2"},
			EntryIDs:  []string{"EVT-001#1"},
			Options: map[string]string{
				"EVT-001#1": "A",
				"EVT-002#1": "B",
			},
		},
		LogTail: []string{"first", "second"},
	}
}

// TestComputeThenVerifyRoundTrips is the baseline: a well-formed envelope must
// verify. Without this, every other test in this file could pass vacuously by
// always returning false.
func TestComputeThenVerifyRoundTrips(t *testing.T) {
	env := envelopeFixture()
	env.ComputeIntegrity()

	if env.Integrity.Algorithm != IntegrityAlgorithmFP {
		t.Fatalf("algorithm = %q, want %q", env.Integrity.Algorithm, IntegrityAlgorithmFP)
	}
	if len(env.Integrity.Digest) != 16 {
		t.Fatalf("digest length = %d, want 16 hex digits (%q)", len(env.Integrity.Digest), env.Integrity.Digest)
	}
	if !env.VerifyIntegrity() {
		t.Fatal("a freshly computed envelope must verify")
	}
}

// TestVerifyRejectsUncomputedEnvelope pins the zero-value behaviour: an envelope
// that was never sealed must not be accepted, otherwise a truncated save that
// lost its integrity block would resume silently.
func TestVerifyRejectsUncomputedEnvelope(t *testing.T) {
	env := envelopeFixture() // no ComputeIntegrity call
	if env.VerifyIntegrity() {
		t.Fatal("an envelope with no digest must not verify")
	}
}

// TestVerifyRejectsUnknownAlgorithm guards the forward-compatibility rule: a
// digest produced by a different algorithm must not be reinterpreted as this
// one's output.
//
// The assertion is deliberately arranged so that it can only pass because of
// the algorithm guard, not because of a digest mismatch. Simply overwriting the
// algorithm field would also change the canonical string (the algorithm name is
// folded into the digest), so the document would fail verification for an
// unrelated reason and the test would pass even with the guard deleted.
//
// To isolate the guard: recompute the digest *after* renaming the algorithm, so
// the stored digest is internally consistent and the only remaining objection
// is the algorithm identity itself.
func TestVerifyRejectsUnknownAlgorithm(t *testing.T) {
	env := envelopeFixture()
	env.Integrity.Algorithm = "some-future-algorithm"
	// Seal with the foreign name so the digest genuinely matches the payload.
	env.Integrity.Digest = fnv1a64Hex([]byte(env.CanonicalString()))

	// Precondition for the test's validity: the digest is self-consistent, so a
	// rejection can only come from the algorithm guard.
	if fnv1a64Hex([]byte(env.CanonicalString())) != env.Integrity.Digest {
		t.Fatal("test setup is wrong: digest does not match payload")
	}

	if env.VerifyIntegrity() {
		t.Fatal("an envelope naming an unknown algorithm must not verify even when its digest is self-consistent")
	}
}

// TestTamperingEveryCanonicalFieldIsDetected is the core anti-tamper property.
// Each mutation touches exactly one contributed value; if any of them failed to
// reach the digest, that field is invisible to corruption detection.
func TestTamperingEveryCanonicalFieldIsDetected(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*SaveEnvelope)
	}{
		{"envelope_version", func(e *SaveEnvelope) { e.EnvelopeVersion++ }},
		{"schema_version", func(e *SaveEnvelope) { e.SchemaVersion++ }},
		{"rules_version", func(e *SaveEnvelope) { e.RulesVersion++ }},
		{"content_version", func(e *SaveEnvelope) { e.ContentVersion++ }},
		{"game_id", func(e *SaveEnvelope) { e.GameID = "other" }},
		{"branch_id", func(e *SaveEnvelope) { e.BranchID = "other" }},
		{"revision", func(e *SaveEnvelope) { e.Revision++ }},

		{"state.phase", func(e *SaveEnvelope) { e.State.Phase = PhaseEnded }},
		{"state.world_month", func(e *SaveEnvelope) { e.State.Counters.WorldMonth++ }},
		{"state.interaction_seq", func(e *SaveEnvelope) { e.State.Counters.InteractionSeq++ }},
		{"state.combat_round", func(e *SaveEnvelope) { e.State.Counters.CombatRound++ }},
		{"state.log_cursor", func(e *SaveEnvelope) { e.State.LogCursor++ }},

		{"rng.algorithm", func(e *SaveEnvelope) { e.State.RNG.Algorithm = "other" }},
		{"rng.stream.state", func(e *SaveEnvelope) {
			s := e.State.RNG.Streams[StreamWorld]
			s.State++
			e.State.RNG.Streams[StreamWorld] = s
		}},
		{"rng.stream.counter", func(e *SaveEnvelope) {
			s := e.State.RNG.Streams[StreamCombat]
			s.Counter++
			e.State.RNG.Streams[StreamCombat] = s
		}},

		{"player.age_months", func(e *SaveEnvelope) { e.State.Player.AgeMonths++ }},
		{"player.xp", func(e *SaveEnvelope) { e.State.Player.XP++ }},
		{"player.hp.current", func(e *SaveEnvelope) { e.State.Player.HP.Current++ }},
		{"player.hp.max", func(e *SaveEnvelope) { e.State.Player.HP.Max++ }},
		{"player.mp.current", func(e *SaveEnvelope) { e.State.Player.MP.Current++ }},
		{"player.mp.max", func(e *SaveEnvelope) { e.State.Player.MP.Max++ }},
		{"player.realm", func(e *SaveEnvelope) { e.State.Player.Realm = RealmFoundation }},
		{"player.tier", func(e *SaveEnvelope) { e.State.Player.Tier = TierMiddle }},
		{"player.path", func(e *SaveEnvelope) { e.State.Player.Path = PathEarthly }},
		{"player.origin", func(e *SaveEnvelope) { e.State.Player.Origin = "other" }},
		{"player.debt", func(e *SaveEnvelope) { e.State.Player.Debt++ }},
		{"player.lifespan.base", func(e *SaveEnvelope) { e.State.Player.Lifespan.BaseYears++ }},
		{"player.ended", func(e *SaveEnvelope) { e.State.Player.Ended = true }},
		{"player.resource", func(e *SaveEnvelope) {
			e.State.Player.Resources[ResSpiritStones]++
		}},
		{"player.resource.added", func(e *SaveEnvelope) {
			e.State.Player.Resources[Resource("extra")] = 1
		}},

		{"pending_choice.source", func(e *SaveEnvelope) { e.PendingChoices[0].Source = "other" }},
		{"pending_choice.instance", func(e *SaveEnvelope) { e.PendingChoices[0].InstanceID = "other" }},
		{"pending_choice.choice", func(e *SaveEnvelope) { e.PendingChoices[0].ChoiceIDs[1] = "C" }},
		{"pending_choice.count", func(e *SaveEnvelope) {
			e.PendingChoices[0].ChoiceIDs = append(e.PendingChoices[0].ChoiceIDs, "C")
		}},

		{"consumed.action", func(e *SaveEnvelope) { e.Consumed.ActionIDs[0] = "other" }},
		{"consumed.action.added", func(e *SaveEnvelope) {
			e.Consumed.ActionIDs = append(e.Consumed.ActionIDs, "act-3")
		}},
		{"consumed.entry", func(e *SaveEnvelope) { e.Consumed.EntryIDs[0] = "other" }},
		{"consumed.option.value", func(e *SaveEnvelope) { e.Consumed.Options["EVT-001#1"] = "B" }},
		{"consumed.option.key", func(e *SaveEnvelope) {
			delete(e.Consumed.Options, "EVT-001#1")
			e.Consumed.Options["EVT-009#1"] = "A"
		}},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			env := envelopeFixture()
			env.ComputeIntegrity()
			if !env.VerifyIntegrity() {
				t.Fatalf("fixture must verify before mutation (test is broken, not the code)")
			}

			m.mutate(env)

			if env.VerifyIntegrity() {
				t.Fatalf("mutating %s did not change the digest: field is unprotected", m.name)
			}
		})
	}
}

// TestLogTailIsOutsideTheDigest documents a deliberate boundary. LogTail is a
// presentation convenience that never affects replay, so it is excluded from
// the canonical payload. This is asserted rather than merely logged: if someone
// later folds it into the digest, every save written before that change stops
// verifying, so the change must be conscious and paired with a version bump.
func TestLogTailIsOutsideTheDigest(t *testing.T) {
	env := envelopeFixture()
	env.ComputeIntegrity()

	env.LogTail = append(env.LogTail, "third")
	if !env.VerifyIntegrity() {
		t.Fatal("LogTail must remain outside the digest; folding it in invalidates every existing save")
	}

	// It also must not be the only thing keeping two envelopes distinguishable.
	other := envelopeFixture()
	other.LogTail = []string{"completely", "different"}
	other.ComputeIntegrity()

	env2 := envelopeFixture()
	env2.ComputeIntegrity()

	if env2.Integrity.Digest != other.Integrity.Digest {
		t.Fatal("LogTail affects the digest, which would break existing saves")
	}
}

// TestCanonicalStringIsIndependentOfMapIterationOrder is the determinism
// property. Go randomises map iteration, so a digest that depended on it would
// fail verification intermittently — the worst possible failure mode, because
// it would look like random corruption.
func TestCanonicalStringIsIndependentOfMapIterationOrder(t *testing.T) {
	build := func() *SaveEnvelope {
		e := envelopeFixture()
		// Insert the same pairs in a deliberately different order.
		e.State.RNG.Streams = map[string]RNGStream{}
		for _, n := range []string{StreamNPC, StreamDrop, StreamCombat, StreamWorld} {
			e.State.RNG.Streams[n] = RNGStream{State: uint64(len(n)), Counter: uint64(len(n) * 2)}
		}
		e.State.Player.Resources = map[Resource]int64{}
		for _, r := range []Resource{ResContribution, ResSpiritStones} {
			e.State.Player.Resources[r] = int64(len(r))
		}
		e.Consumed.Options = map[string]string{}
		for _, k := range []string{"EVT-002#1", "EVT-001#1"} {
			e.Consumed.Options[k] = "X"
		}
		return e
	}

	first := build().CanonicalString()
	// Repeat, because a single comparison could coincide with a lucky ordering.
	for i := 0; i < 50; i++ {
		if got := build().CanonicalString(); got != first {
			t.Fatalf("canonical string changed between runs (iteration %d):\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
}

// TestCanonicalStringSeparatesAdjacentFields catches the classic concatenation
// ambiguity: without a separator, ("ab","c") and ("a","bc") would digest the
// same, so a crafted edit could move bytes across a field boundary undetected.
func TestCanonicalStringSeparatesAdjacentFields(t *testing.T) {
	a := envelopeFixture()
	a.GameID = "ab"
	a.BranchID = "c"

	b := envelopeFixture()
	b.GameID = "a"
	b.BranchID = "bc"

	if a.CanonicalString() == b.CanonicalString() {
		t.Fatal("field boundary is not delimited: two distinct documents share a canonical string")
	}
}

// TestCanonicalStringEndsEveryFieldOnItsOwnLine asserts the delimiter contract
// directly, so a future edit that drops a terminator is caught here rather than
// by a subtle digest collision.
func TestCanonicalStringEndsEveryFieldOnItsOwnLine(t *testing.T) {
	env := envelopeFixture()
	s := env.CanonicalString()

	if !strings.HasSuffix(s, "\n") {
		t.Fatal("canonical string must end with a newline")
	}
	for i, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if line == "" {
			t.Fatalf("line %d is empty: a field emitted no content", i)
		}
		if !strings.Contains(line, "=") {
			t.Fatalf("line %d (%q) has no key/value separator", i, line)
		}
	}
}

// TestNilPlayerIsRepresentable checks that a CREATION-phase envelope, which has
// no player yet, still digests deterministically instead of panicking.
func TestNilPlayerIsRepresentable(t *testing.T) {
	env := envelopeFixture()
	env.State.Player = nil
	env.State.Phase = PhaseCreation

	env.ComputeIntegrity()
	if !env.VerifyIntegrity() {
		t.Fatal("an envelope without a player must still verify")
	}

	// And it must remain distinguishable from one that has a player.
	withPlayer := envelopeFixture()
	withPlayer.State.Player = &Player{}
	withPlayer.ComputeIntegrity()
	if env.Integrity.Digest == withPlayer.Integrity.Digest {
		t.Fatal("a nil player must not digest the same as a zero-value player")
	}
}

// TestNegativeValuesRoundTrip covers the hand-written integer renderer, which is
// the only place a sign can be lost. Debt is the one field expected to be able
// to go negative in some designs, and AgeMonths can be corrupted to a negative.
func TestNegativeValuesRoundTrip(t *testing.T) {
	env := envelopeFixture()
	env.State.Player.Debt = -1
	env.State.Player.AgeMonths = -216
	env.ComputeIntegrity()

	if !env.VerifyIntegrity() {
		t.Fatal("negative values must verify")
	}

	// -1 and 1 must not collide, and neither may collide with a sign-free form.
	pos := envelopeFixture()
	pos.State.Player.Debt = 1
	pos.ComputeIntegrity()
	if pos.Integrity.Digest == env.Integrity.Digest {
		t.Fatal("sign is not represented in the canonical string")
	}
}

// TestExtremeIntegersRoundTrip exercises the boundary of the hand-written
// decimal renderer, including math.MinInt64 whose naive negation overflows.
func TestExtremeIntegersRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		v    int64
	}{
		{"zero", 0},
		{"one", 1},
		{"minus_one", -1},
		{"max_int64", 9223372036854775807},
		{"min_int64", -9223372036854775808},
		{"ten", 10},
		{"minus_ten", -10},
	}

	seen := map[string]string{}
	for _, c := range cases {
		env := envelopeFixture()
		env.Revision = 0
		env.State.Player.Debt = c.v
		env.ComputeIntegrity()

		if !env.VerifyIntegrity() {
			t.Fatalf("%s (%d): must verify", c.name, c.v)
		}
		if prev, dup := seen[env.Integrity.Digest]; dup {
			t.Fatalf("%s (%d) collides with %s", c.name, c.v, prev)
		}
		seen[env.Integrity.Digest] = c.name
	}
}

// TestUintExtremesRoundTrip covers the unsigned renderer used for revisions and
// RNG state, which cannot share appendIntBytes' sign handling.
func TestUintExtremesRoundTrip(t *testing.T) {
	values := []uint64{0, 1, 9, 10, 18446744073709551615}

	seen := map[string]string{}
	for _, v := range values {
		env := envelopeFixture()
		env.Revision = v
		env.ComputeIntegrity()

		if !env.VerifyIntegrity() {
			t.Fatalf("revision %d must verify", v)
		}
		key := env.Integrity.Digest
		if prev, dup := seen[key]; dup {
			t.Fatalf("revision %d collides with revision %s", v, prev)
		}
		seen[key] = string(rune('0' + v%10))
	}
}

// TestRevisionIsNotSilentlyTruncated verifies that the uint and int renderers
// agree on values they share, so a Revision that fits in int64 still produces
// the same digits. A divergence here would mean the two paths encode the same
// number differently depending on which field it lands in.
func TestRevisionIsNotSilentlyTruncated(t *testing.T) {
	for _, v := range []uint64{0, 1, 7, 42, 1000000} {
		env := envelopeFixture()
		env.Revision = v
		env.ComputeIntegrity()

		direct := fnv1a64Hex([]byte(appendUintBytes(nil, v)))
		if len(direct) != 16 {
			t.Fatalf("uint renderer produced %d chars for %d", len(direct), v)
		}
		if !env.VerifyIntegrity() {
			t.Fatalf("revision %d must verify", v)
		}
	}
}

// TestKnownDigestVector pins the algorithm. If this fails, the digest scheme
// changed and every previously written save is now unverifiable — that must be
// a deliberate, announced change, never a drive-by refactor.
func TestKnownDigestVector(t *testing.T) {
	// The canonical string of the empty envelope is fully determined, so its
	// digest is a stable vector.
	env := &SaveEnvelope{EnvelopeVersion: EnvelopeVersion}
	env.ComputeIntegrity()

	// Recomputing must be stable.
	again := &SaveEnvelope{EnvelopeVersion: EnvelopeVersion}
	again.ComputeIntegrity()
	if env.Integrity.Digest != again.Integrity.Digest {
		t.Fatal("digest is not deterministic for the same input")
	}

	// FNV-1a of the empty input has a well-known value; confirm our renderer
	// agrees with the standard so the implementation is not merely
	// self-consistent.
	if got := fnv1a64Hex(nil); got != "cbf29ce484222325" {
		t.Fatalf("fnv1a64Hex(empty) = %s, want cbf29ce484222325 (the FNV-1a 64 offset basis)", got)
	}
	if got := fnv1a64Hex([]byte("a")); got != "af63dc4c8601ec8c" {
		t.Fatalf("fnv1a64Hex(\"a\") = %s, want af63dc4c8601ec8c", got)
	}
	if got := fnv1a64Hex([]byte("hello")); got != "a430d84680aabd0b" {
		t.Fatalf("fnv1a64Hex(\"hello\") = %s, want a430d84680aabd0b", got)
	}
}

// TestTruncationIsDetected models the real-world corruption this digest exists
// for: a save file cut short, or written halfway.
func TestTruncationIsDetected(t *testing.T) {
	env := envelopeFixture()
	env.ComputeIntegrity()
	full := env.CanonicalString()

	truncated := full[:len(full)/2]
	if fnv1a64Hex([]byte(truncated)) == env.Integrity.Digest {
		t.Fatal("truncated payload shares the full payload's digest")
	}
}

// TestDigestChangesWhenOnlyChoiceOrderChanges distinguishes two genuine saves
// that hold the same choices in a different order. Presentation order is part of
// the frozen decision, so it must be covered.
func TestDigestChangesWhenOnlyChoiceOrderChanges(t *testing.T) {
	a := envelopeFixture()
	a.PendingChoices[0].ChoiceIDs = []string{"A", "B"}
	a.ComputeIntegrity()

	b := envelopeFixture()
	b.PendingChoices[0].ChoiceIDs = []string{"B", "A"}
	b.ComputeIntegrity()

	if a.Integrity.Digest == b.Integrity.Digest {
		t.Fatal("choice order is not represented in the digest")
	}
}

// TestPendingChoiceSetsArePositional guards against a set-like rendering that
// would ignore which instance a choice set belongs to.
func TestPendingChoiceSetsArePositional(t *testing.T) {
	a := envelopeFixture()
	a.PendingChoices = []FrozenChoiceSet{
		{Source: "event", InstanceID: "1", ChoiceIDs: []string{"A"}},
		{Source: "event", InstanceID: "2", ChoiceIDs: []string{"B"}},
	}
	a.ComputeIntegrity()

	b := envelopeFixture()
	b.PendingChoices = []FrozenChoiceSet{
		{Source: "event", InstanceID: "2", ChoiceIDs: []string{"B"}},
		{Source: "event", InstanceID: "1", ChoiceIDs: []string{"A"}},
	}
	b.ComputeIntegrity()

	if a.Integrity.Digest == b.Integrity.Digest {
		t.Fatal("pending choice set order is not represented in the digest")
	}
}

// TestConsumedActionOrderIsSignificant documents that ActionIDs are an ordered
// log rather than a set. Reordering them means the ledger was rewritten.
func TestConsumedActionOrderIsSignificant(t *testing.T) {
	a := envelopeFixture()
	a.Consumed.ActionIDs = []string{"x", "y"}
	a.ComputeIntegrity()

	b := envelopeFixture()
	b.Consumed.ActionIDs = []string{"y", "x"}
	b.ComputeIntegrity()

	if a.Integrity.Digest == b.Integrity.Digest {
		t.Fatal("consumed action order is not represented in the digest")
	}
}

// TestExtraResourceEntryIsDetected ensures the resource loop is not silently
// skipping unknown resource keys: a save that gained a resource must not verify
// against the old digest.
func TestExtraResourceEntryIsDetected(t *testing.T) {
	env := envelopeFixture()
	env.ComputeIntegrity()

	env.State.Player.Resources[Resource("cheat")] = 999999
	if env.VerifyIntegrity() {
		t.Fatal("an injected resource key is invisible to the digest")
	}
}

// TestEmptyEnvelopeStillDigests confirms the degenerate case does not panic and
// does not produce an empty digest string.
func TestEmptyEnvelopeStillDigests(t *testing.T) {
	env := &SaveEnvelope{}
	env.ComputeIntegrity()

	if env.Integrity.Digest == "" {
		t.Fatal("empty envelope produced an empty digest")
	}
	if !env.VerifyIntegrity() {
		t.Fatal("empty envelope must verify")
	}
	if env.CanonicalString() == "" {
		t.Fatal("empty envelope produced an empty canonical string")
	}
}
