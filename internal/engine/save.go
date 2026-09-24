package engine

// SaveEnvelope is the persisted document. It wraps the mechanical state with
// the metadata needed to decide whether a save can be trusted and resumed:
// version identity, an integrity digest, the pending choice set and the
// consumed-action records.
//
// The envelope deliberately contains no credentials and no file paths: the
// engine must remain independent of the filesystem, so everything about *where*
// a save lives belongs to internal/storage (TASK-06).
type SaveEnvelope struct {
	// EnvelopeVersion identifies this wrapper's shape, separately from the
	// state schema so the two can evolve independently.
	EnvelopeVersion int `json:"envelope_version"`

	// Version identity. A save is only meaningful together with the rules and
	// content that produced it, so all three travel with it.
	SchemaVersion  int `json:"schema_version"`
	RulesVersion   int `json:"rules_version"`
	ContentVersion int `json:"content_version"`

	// Identity.
	GameID   string `json:"game_id"`
	BranchID string `json:"branch_id"`
	// Revision is the state revision this envelope was written from.
	Revision uint64 `json:"revision"`

	// State is the complete mechanical state.
	State GameState `json:"state"`

	// PendingChoices freezes the candidate set of any pending decision. It is
	// duplicated out of State so that a reader can verify the candidates
	// without understanding the pending sub-state union, and so that a resume
	// cannot silently re-roll them.
	PendingChoices []FrozenChoiceSet `json:"pending_choices,omitempty"`

	// Consumed records every action and event already settled. It is the
	// anti-double-payout ledger: TASK-05/06 consult it before applying a
	// reward, so loading a checkpoint cannot pay twice.
	Consumed ConsumedLedger `json:"consumed"`

	// LogTail is a bounded tail of the human-readable log, kept so a resumed
	// session can show recent history. It never affects replay.
	LogTail []string `json:"log_tail,omitempty"`

	// Integrity is the digest of the canonical serialisation of everything
	// above except this field. A mismatch means the document was truncated or
	// edited outside the game, and storage must refuse it rather than guess.
	Integrity IntegrityBlock `json:"integrity"`
}

// EnvelopeVersion is the current wrapper version.
const EnvelopeVersion = 1

// FrozenChoiceSet is one decision's frozen candidates.
type FrozenChoiceSet struct {
	// Source identifies where the choices came from, e.g. "event" or
	// "breakthrough".
	Source string `json:"source"`
	// InstanceID distinguishes this occurrence.
	InstanceID string `json:"instance_id"`
	// ChoiceIDs are the candidates, in presentation order.
	ChoiceIDs []string `json:"choice_ids"`
}

// ConsumedLedger records settled actions and events.
type ConsumedLedger struct {
	// ActionIDs are the action ids already committed. A duplicate submit
	// consults this to return the original result instead of re-applying.
	ActionIDs []string `json:"action_ids"`
	// EntryIDs are the event/breakthrough instance ids already resolved.
	EntryIDs []string `json:"entry_ids"`
	// Options are the choices taken, keyed by instance id, so a reward tied to
	// a specific option is never re-granted.
	Options map[string]string `json:"options"`
}

// IntegrityBlock carries the digest and the algorithm identity.
type IntegrityBlock struct {
	// Algorithm names the digest, so a future change to the hashing does not
	// silently reinterpret an old save's digest as the new algorithm's output.
	Algorithm string `json:"algorithm"`
	// Digest is the hex-encoded digest of the canonical payload.
	Digest string `json:"digest"`
}

// Integrity algorithm names.
const (
	// IntegrityAlgorithmFP is a pure-Go FNV-1a based digest. It is chosen
	// because the engine must not import crypto/rand or any hash that could
	// vary, and because determinism matters more than cryptographic strength
	// here: the digest detects truncation and accidental edit, not a
	// determined adversary. TASK-06 may add a stronger digest at the storage
	// boundary without changing the engine contract, by naming a new algorithm.
	IntegrityAlgorithmFP = "fnv1a64-canonical-v1"
)

// CanonicalString renders the envelope's mechanical content as a stable,
// order-independent string suitable for digesting.
//
// It is deliberately explicit rather than reflective: a field added to the
// state will not silently enter the digest, which would change every existing
// save's digest. Instead, digesting a new field is a deliberate edit here,
// paired with a schema bump.
func (s *SaveEnvelope) CanonicalString() string {
	var b []byte
	appendStr := func(tag, v string) {
		b = append(b, tag...)
		b = append(b, '=')
		b = append(b, v...)
		b = append(b, '\n')
	}
	appendInt := func(tag string, v int64) {
		b = append(b, tag...)
		b = append(b, '=')
		b = appendIntBytes(b, v)
		b = append(b, '\n')
	}
	appendUint := func(tag string, v uint64) {
		b = append(b, tag...)
		b = append(b, '=')
		b = appendUintBytes(b, v)
		b = append(b, '\n')
	}

	appendInt("envelope_version", int64(s.EnvelopeVersion))
	appendInt("schema_version", int64(s.SchemaVersion))
	appendInt("rules_version", int64(s.RulesVersion))
	appendInt("content_version", int64(s.ContentVersion))
	appendStr("game_id", s.GameID)
	appendStr("branch_id", s.BranchID)
	appendUint("revision", s.Revision)

	st := &s.State
	appendInt("state.schema_version", int64(st.SchemaVersion))
	appendInt("state.rules_version", int64(st.RulesVersion))
	appendInt("state.content_version", int64(st.ContentVersion))
	appendStr("state.game_id", st.GameID)
	appendStr("state.branch_id", st.BranchID)
	appendUint("state.revision", st.Revision)
	appendStr("state.phase", string(st.Phase))
	appendInt("state.counters.world_month", st.Counters.WorldMonth)
	appendInt("state.counters.interaction_seq", st.Counters.InteractionSeq)
	appendInt("state.counters.combat_round", st.Counters.CombatRound)
	appendStr("state.rng.algorithm", st.RNG.Algorithm)
	// Streams are sorted so map iteration order cannot change the digest.
	for _, name := range sortedKeys(st.RNG.Streams) {
		stream := st.RNG.Streams[name]
		appendStr("state.rng.stream", name)
		appendUint("state.rng.stream."+name+".state", stream.State)
		appendUint("state.rng.stream."+name+".counter", stream.Counter)
	}
	appendUint("state.log_cursor", st.LogCursor)
	if st.Player != nil {
		appendInt("state.player.age_months", st.Player.AgeMonths)
		appendInt("state.player.xp", st.Player.XP)
		appendStr("state.player.spirit_root", string(st.Player.SpiritRoot))
		appendStr("state.player.constitution", st.Player.Constitution)
		appendStr("state.player.primary_technique", st.Player.PrimaryTechniqueID)
		for _, id := range st.Player.SecondaryTechniqueIDs {
			appendStr("state.player.secondary_technique", id)
		}
		for _, id := range st.Player.TalentIDs {
			appendStr("state.player.talent", id)
		}
		appendInt("state.player.attributes.strength", int64(st.Player.Attributes.Strength))
		appendInt("state.player.attributes.agility", int64(st.Player.Attributes.Agility))
		appendInt("state.player.attributes.constitution", int64(st.Player.Attributes.Constitution))
		appendInt("state.player.attributes.comprehension", int64(st.Player.Attributes.Comprehension))
		appendInt("state.player.attributes.aptitude", int64(st.Player.Attributes.Aptitude))
		appendInt("state.player.attributes.fortune", int64(st.Player.Attributes.Fortune))
		for _, key := range sortedKeys(st.Player.Attributes.Growth) {
			appendStr("state.player.attributes.growth", key)
			appendInt("state.player.attributes.growth."+key, int64(st.Player.Attributes.Growth[key]))
		}
		appendInt("state.player.derived.effective_aptitude", int64(st.Player.Derived.EffectiveAptitude))
		appendInt("state.player.derived.cultivation_rate", st.Player.Derived.CultivationRate)
		appendInt("state.player.hp.current", st.Player.HP.Current)
		appendInt("state.player.hp.max", st.Player.HP.Max)
		appendInt("state.player.mp.current", st.Player.MP.Current)
		appendInt("state.player.mp.max", st.Player.MP.Max)
		appendStr("state.player.realm", string(st.Player.Realm))
		appendStr("state.player.tier", string(st.Player.Tier))
		appendStr("state.player.path", string(st.Player.Path))
		appendStr("state.player.origin", string(st.Player.Origin))
		appendInt("state.player.debt", st.Player.Debt)
		appendInt("state.player.lifespan.base", int64(st.Player.Lifespan.BaseYears))
		appendInt("state.player.ended", boolToInt(st.Player.Ended))
		appendInt("state.player.condition.mood", int64(st.Player.Condition.Mood))
		for _, effect := range st.Player.Condition.Effects {
			appendStr("state.player.condition.effect.id", effect.ID)
			appendInt("state.player.condition.effect.months_remaining", int64(effect.MonthsRemaining))
			appendInt("state.player.condition.effect.stacks", int64(effect.Stacks))
		}
		for _, id := range sortedKeys(st.Player.Proficiencies) {
			appendStr("state.player.proficiency", id)
			appendInt("state.player.proficiency."+id, st.Player.Proficiencies[id])
		}
		for _, key := range sortedKeys(st.Player.BreakthroughBonuses) {
			appendStr("state.player.breakthrough_bonus", key)
			appendInt("state.player.breakthrough_bonus."+key, st.Player.BreakthroughBonuses[key])
		}
		for _, rel := range st.Player.Relations {
			appendStr("state.player.relation.npc", rel.NPCID)
			appendInt("state.player.relation.value", int64(rel.Value))
			appendInt("state.player.relation.met", boolToInt(rel.Met))
		}
		for _, res := range sortedResourceKeys(st.Player.Resources) {
			appendStr("state.player.resource", res)
			appendInt("state.player.resource."+res, st.Player.Resources[Resource(res)])
		}
	}

	// World event state. TASK-10 made these fields writable by play, so they
	// must be digested: a tampered queue or occurrence ledger would otherwise
	// change which events fire without the integrity check noticing.
	if st.World != nil {
		appendInt("state.world.event_instance_seq", st.World.EventInstanceSeq)
		for _, re := range st.World.RaisedEvents {
			appendStr("state.world.raised_event.id", re.EventID)
			appendStr("state.world.raised_event.instance", re.InstanceID)
			appendInt("state.world.raised_event.month", re.WorldMonth)
			appendInt("state.world.raised_event.forced", boolToInt(re.Forced))
		}
		for _, ce := range st.World.ConsumedEvents {
			appendStr("state.world.consumed_event.id", ce.EventID)
			appendStr("state.world.consumed_event.instance", ce.InstanceID)
			appendStr("state.world.consumed_event.choice", ce.ChoiceID)
			appendInt("state.world.consumed_event.month", ce.WorldMonth)
		}
		for _, flag := range sortedKeys(st.World.Flags) {
			appendStr("state.world.flag", flag)
			appendInt("state.world.flag."+flag, boolToInt(st.World.Flags[flag]))
		}
	}

	if st.Pending.Event != nil {
		appendStr("state.pending.event.id", st.Pending.Event.EventID)
		appendStr("state.pending.event.instance", st.Pending.Event.InstanceID)
		appendStr("state.pending.event.parent_action", st.Pending.Event.ParentActionID)
		appendInt("state.pending.event.world_month", st.Pending.Event.WorldMonth)
		for _, id := range st.Pending.Event.ChoiceIDs {
			appendStr("state.pending.event.choice", id)
		}
	}
	for _, q := range st.Pending.EventQueue {
		appendStr("state.pending.event_queue.id", q.EventID)
		appendStr("state.pending.event_queue.instance", q.InstanceID)
		appendInt("state.pending.event_queue.priority", int64(q.Priority))
		appendInt("state.pending.event_queue.raised_at", q.RaisedAtWorldMonth)
		appendInt("state.pending.event_queue.expires_at", q.ExpiresAtWorldMonth)
		for _, id := range q.ChoiceIDs {
			appendStr("state.pending.event_queue.choice", id)
		}
	}

	for _, cs := range s.PendingChoices {
		appendStr("pending_choice.source", cs.Source)
		appendStr("pending_choice.instance", cs.InstanceID)
		for _, id := range cs.ChoiceIDs {
			appendStr("pending_choice.choice", id)
		}
	}

	for _, id := range s.Consumed.ActionIDs {
		appendStr("consumed.action", id)
	}
	for _, id := range s.Consumed.EntryIDs {
		appendStr("consumed.entry", id)
	}
	for _, k := range sortedKeys(s.Consumed.Options) {
		appendStr("consumed.option."+k, s.Consumed.Options[k])
	}

	appendStr("integrity.algorithm", s.Integrity.Algorithm)
	return string(b)
}

// ComputeIntegrity fills in the digest over the canonical payload. Storage
// calls it after populating every other field and before writing.
func (s *SaveEnvelope) ComputeIntegrity() {
	s.Integrity.Algorithm = IntegrityAlgorithmFP
	s.Integrity.Digest = fnv1a64Hex([]byte(s.CanonicalString()))
}

// VerifyIntegrity recomputes the digest and compares it with the stored one.
// It returns false when the document was truncated or edited, so storage can
// refuse it instead of resuming from a corrupt state.
func (s *SaveEnvelope) VerifyIntegrity() bool {
	if s.Integrity.Algorithm != IntegrityAlgorithmFP {
		return false
	}
	want := fnv1a64Hex([]byte(s.CanonicalString()))
	return want == s.Integrity.Digest
}

// boolToInt renders a bool as 0/1 for the canonical string.
func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// fnv1a64Hex computes the FNV-1a 64-bit digest of data and returns it as
// lowercase hex. The hash itself lives in rng.go as fnv1a64 so the engine keeps
// a single implementation; this wrapper only renders it.
func fnv1a64Hex(data []byte) string {
	return hexUint64(fnv1a64(data))
}

// hexUint64 renders v as 16 lowercase hex digits, zero-padded.
func hexUint64(v uint64) string {
	const digits = "0123456789abcdef"
	var out [16]byte
	for i := 15; i >= 0; i-- {
		out[i] = digits[v&0xf]
		v >>= 4
	}
	return string(out[:])
}

// appendIntBytes appends the decimal rendering of v to b.
func appendIntBytes(b []byte, v int64) []byte {
	if v < 0 {
		b = append(b, '-')
		// Negate in uint space so math.MinInt64 does not overflow.
		return appendUintBytes(b, uint64(-(v+1))+1)
	}
	return appendUintBytes(b, uint64(v))
}

// appendUintBytes appends the decimal rendering of v to b.
func appendUintBytes(b []byte, v uint64) []byte {
	if v == 0 {
		return append(b, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for v > 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	return append(b, tmp[i:]...)
}

// sortedKeys returns a map's keys in ascending order so that digesting never
// depends on Go's randomised map iteration.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// A tiny insertion sort avoids importing sort in this file's hot path and
	// keeps the ordering rule local and obvious.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// sortedResourceKeys returns a resource map's keys in ascending order.
func sortedResourceKeys(m map[Resource]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
