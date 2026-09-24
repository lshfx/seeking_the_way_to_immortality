package engine

import "fmt"

// The world event scheduler.
//
// Design R17 fixes the shape: one scheduling pass per settled world month, a
// base 20% chance of a random event, and exactly one pending node shown at a
// time with the rest queued or expired. Design 13.2 step 5 places the pass after
// the death check and after quest expiry, and design 13.2 step 6 records that a
// zero-extra-month choice at month end must not re-trigger the month end.
//
// Two properties are load-bearing and are the reason this is a separate file
// rather than a few lines inside the settlement function:
//
//   - The pass runs only where a month actually settles. Queries never reach
//     here, and a combat round never advances the world month, so "查询、战斗
//     招式和现实等待不抽月奇遇" is satisfied by construction rather than by a
//     guard that could be forgotten.
//   - The base check draws exactly one value from the world stream per settled
//     month, whether or not any event is eligible. Tying the draw to the month
//     rather than to the candidate pool means adding or removing content later
//     does not shift the stream position of an existing save.

// DefaultEventBaseChancePermille is the fallback base chance used when the
// catalogue does not configure one. Design R17 states 20%.
const DefaultEventBaseChancePermille = 200

// runMonthEndEvents performs design 13.2 step 5 for a month that has just
// settled.
//
// Order matters: expiry first, so an instance that ran out of time cannot be
// promoted in the same breath; then raising; then promotion, so a freshly raised
// instance competes with the backlog on priority rather than jumping the queue.
func (e *Engine) runMonthEndEvents(s *GameState, result *CommandResult) {
	if s == nil || s.Player == nil {
		return
	}
	// A character who died this month takes no new event. Design 13.2 step 5
	// is explicit that a finished character does not draw again.
	if s.Player.Ended || s.Phase == PhaseEnded {
		return
	}

	e.expireQueuedEvents(s, result)
	e.raiseMonthEvents(s, result)
	e.promoteQueuedEvent(s, result)
}

// expireQueuedEvents drops queued instances whose deadline has passed.
//
// An expired instance is *not* un-raised: it stays in World.RaisedEvents, so it
// still counts against the event's occurrence cap and cooldown. Forgetting it
// would turn "ignore the event until it goes away" into a way to see the same
// limited event again and again.
func (e *Engine) expireQueuedEvents(s *GameState, result *CommandResult) {
	if s == nil || len(s.Pending.EventQueue) == 0 {
		return
	}
	now := s.Counters.WorldMonth

	kept := make([]QueuedEvent, 0, len(s.Pending.EventQueue))
	for _, q := range s.Pending.EventQueue {
		if q.ExpiresAtWorldMonth != 0 && now >= q.ExpiresAtWorldMonth {
			result.Events = append(result.Events, ResultEvent{
				Kind:       ResultEventExpired,
				ID:         q.EventID,
				InstanceID: q.InstanceID,
				Detail:     "queued event expired unanswered",
			})
			continue
		}
		kept = append(kept, q)
	}

	if len(kept) == 0 {
		// Preserve nil rather than an empty slice so that a state which never
		// queued anything keeps digesting the same way as one that queued and
		// drained.
		s.Pending.EventQueue = nil
		return
	}
	s.Pending.EventQueue = kept
}

// raiseMonthEvents decides what this month raises: forced events first, then the
// base check and a weighted draw.
func (e *Engine) raiseMonthEvents(s *GameState, result *CommandResult) {
	if e.Catalogue == nil || len(e.Catalogue.Events) == 0 {
		// No content is a legitimate state, not an error. The month simply
		// passes, and play continues on the ordinary scene actions.
		return
	}

	// Forced events. Design 13.2 step 5 settles 必发事件 before the base check,
	// because a forced event is one the story cannot skip.
	for i := range e.Catalogue.Events {
		ev := &e.Catalogue.Events[i]
		if !ev.Forced {
			continue
		}
		if !e.eventMayRaise(s, ev) {
			continue
		}
		e.enqueueEvent(s, ev, true, result)
	}

	// The base check. It is drawn even when nothing is eligible so that the
	// world stream advances once per settled month regardless of content.
	stream, err := worldStreamOf(s)
	if err != nil {
		// A state whose random streams cannot be loaded is already broken;
		// validation reports it. Skipping the draw here keeps the month from
		// failing outright, and the missing draw is visible as a stream that
		// did not advance.
		return
	}
	chance := DefaultEventBaseChancePermille
	// Provenance, not value, decides whether the catalogue configured this. A
	// configured 0 means "this catalogue never fires a random event", which is
	// a legitimate content decision; treating 0 as "unset" and falling back to
	// 20% would silently override it.
	if e.Catalogue.EventBaseChancePermille.Provenance != "" {
		chance = e.Catalogue.EventBaseChancePermille.Value
	}
	passed := stream.Chance(chance)
	s.RNG = snapshotStreams(s.RNG, stream)

	if !passed {
		return
	}

	candidates := e.randomEventCandidates(s)
	if len(candidates) == 0 {
		result.Events = append(result.Events, ResultEvent{
			Kind:   ResultEventRaised,
			ID:     "",
			Detail: "base event check passed but no event was eligible",
		})
		return
	}

	chosen := weightedPick(stream, candidates)
	if chosen == nil {
		return
	}
	s.RNG = snapshotStreams(s.RNG, stream)
	e.enqueueEvent(s, chosen, false, result)
}

// eventMayRaise applies every gate that does not depend on the draw: eligibility
// conditions, cooldown, occurrence cap and de-duplication.
func (e *Engine) eventMayRaise(s *GameState, ev *EventDefinition) bool {
	if ev == nil || ev.ID == "" {
		return false
	}

	ok, err := EvalPreconditions(s, e.Catalogue, ev.Eligibility)
	if err != nil || !ok {
		return false
	}

	// The node must belong to where the player actually is. Design 14 gives
	// every node a scene, and the scenario is a travel graph; without this check
	// a sect patrol could fire while the player is sitting in their cave, which
	// makes both the scene column and the graph decorative.
	if ev.Scene != "" {
		if s.World == nil || s.World.CurrentLocation != ev.Scene {
			return false
		}
	}

	// Cooldown is measured from the last time the event was raised, not from
	// the last time it was answered. See World.RaisedEvents for why.
	if ev.CooldownMonths > 0 {
		if last, seen := lastRaisedMonth(s, ev.ID); seen {
			if s.Counters.WorldMonth-last < int64(ev.CooldownMonths) {
				return false
			}
		}
	}

	if ev.MaxOccurrences > 0 && raisedCount(s, ev.ID) >= int64(ev.MaxOccurrences) {
		return false
	}

	// Never raise a second copy of an event the player is already sitting on
	// or already has queued. Without this a repeatable, zero-cooldown event
	// would fill the queue with identical instances.
	if s.Pending.Event != nil && s.Pending.Event.EventID == ev.ID {
		return false
	}
	for _, q := range s.Pending.EventQueue {
		if q.EventID == ev.ID {
			return false
		}
	}

	return true
}

// randomEventCandidates lists the events eligible for the weighted draw.
//
// Forced events are excluded on purpose: a forced event that could also be
// drawn would consume the month's random slot as well as its forced slot, and
// then be unable to fire on the following month.
func (e *Engine) randomEventCandidates(s *GameState) []*EventDefinition {
	var out []*EventDefinition
	for i := range e.Catalogue.Events {
		ev := &e.Catalogue.Events[i]
		if ev.Forced || ev.Weight.Value <= 0 {
			continue
		}
		if !e.eventMayRaise(s, ev) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// enqueueEvent records a raised instance and puts it in the queue.
//
// There is no parent action to record. The instance is raised at month end,
// after the parent has already settled, so a choice taken on it inherits no
// timing: design 13.2 step 6 says a zero-extra-month month-end choice must not
// re-trigger the month end.
func (e *Engine) enqueueEvent(s *GameState, ev *EventDefinition, forced bool, result *CommandResult) {
	if s.World == nil {
		return
	}

	s.World.EventInstanceSeq++
	instanceID := ev.ID + "#" + itoa64(s.World.EventInstanceSeq)

	raised := RaisedEvent{
		EventID:    ev.ID,
		InstanceID: instanceID,
		WorldMonth: s.Counters.WorldMonth,
		Forced:     forced,
	}
	s.World.RaisedEvents = append(s.World.RaisedEvents, raised)

	expiresAt := int64(0)
	if ev.ExpiresAfterMonths > 0 {
		expiresAt = s.Counters.WorldMonth + int64(ev.ExpiresAfterMonths)
	}

	s.Pending.EventQueue = append(s.Pending.EventQueue, QueuedEvent{
		EventID:             ev.ID,
		InstanceID:          instanceID,
		Priority:            ev.Priority.Value,
		RaisedAtWorldMonth:  s.Counters.WorldMonth,
		ExpiresAtWorldMonth: expiresAt,
		ChoiceIDs:           choiceIDsOf(ev),
	})

	result.Events = append(result.Events, ResultEvent{
		Kind:       ResultEventRaised,
		ID:         ev.ID,
		InstanceID: instanceID,
		Detail:     forcedDetail(forced),
	})
}

// promoteQueuedEvent shows the best queued instance, if nothing is showing.
//
// "Best" is highest priority first, then earliest raise, then queue order. The
// tie-breaks are spelled out rather than left to the sort's stability because a
// different order would be a different game, and the design forbids a reload
// from changing what the player is looking at.
func (e *Engine) promoteQueuedEvent(s *GameState, result *CommandResult) {
	if s.Pending.Event != nil || len(s.Pending.EventQueue) == 0 {
		return
	}

	best := 0
	for i := 1; i < len(s.Pending.EventQueue); i++ {
		candidate := s.Pending.EventQueue[i]
		incumbent := s.Pending.EventQueue[best]
		if candidate.Priority > incumbent.Priority {
			best = i
			continue
		}
		if candidate.Priority < incumbent.Priority {
			continue
		}
		if candidate.RaisedAtWorldMonth < incumbent.RaisedAtWorldMonth {
			best = i
		}
	}

	q := s.Pending.EventQueue[best]
	s.Pending.EventQueue = append(s.Pending.EventQueue[:best], s.Pending.EventQueue[best+1:]...)
	if len(s.Pending.EventQueue) == 0 {
		s.Pending.EventQueue = nil
	}

	s.Pending.Event = &PendingEvent{
		EventID:        q.EventID,
		InstanceID:     q.InstanceID,
		ParentActionID: "",
		ChoiceIDs:      append([]string(nil), q.ChoiceIDs...),
		WorldMonth:     q.RaisedAtWorldMonth,
	}
	s.Phase = PhaseEventPending

	result.Events = append(result.Events, ResultEvent{
		Kind:       ResultEventPromoted,
		ID:         q.EventID,
		InstanceID: q.InstanceID,
	})
}

// choiceIDsOf freezes an event's candidate choices.
func choiceIDsOf(ev *EventDefinition) []string {
	out := make([]string, 0, len(ev.Choices))
	for _, ch := range ev.Choices {
		out = append(out, ch.ID)
	}
	return out
}

// forcedDetail renders the raise reason for the log.
func forcedDetail(forced bool) string {
	if forced {
		return "forced"
	}
	return "random"
}

// raisedCount counts how many instances of an event have been raised.
func raisedCount(s *GameState, eventID string) int64 {
	if s == nil || s.World == nil {
		return 0
	}
	var n int64
	for _, re := range s.World.RaisedEvents {
		if re.EventID == eventID {
			n++
		}
	}
	return n
}

// lastRaisedMonth returns the most recent month in which an event was raised.
func lastRaisedMonth(s *GameState, eventID string) (int64, bool) {
	if s == nil || s.World == nil {
		return 0, false
	}
	var (
		best  int64
		found bool
	)
	for _, re := range s.World.RaisedEvents {
		if re.EventID != eventID {
			continue
		}
		if !found || re.WorldMonth > best {
			best = re.WorldMonth
			found = true
		}
	}
	return best, found
}

// weightedPick draws one event from a weighted list.
//
// The list order is the catalogue order, which is stable across builds, and the
// accumulation is integer, so the same stream value always selects the same
// event.
func weightedPick(stream *Stream, events []*EventDefinition) *EventDefinition {
	if stream == nil || len(events) == 0 {
		return nil
	}
	total := int64(0)
	for _, ev := range events {
		total += int64(ev.Weight.Value)
	}
	if total <= 0 {
		return nil
	}

	roll := int64(stream.NextUint64N(uint64(total)))
	acc := int64(0)
	for _, ev := range events {
		acc += int64(ev.Weight.Value)
		if roll < acc {
			return ev
		}
	}
	return events[len(events)-1]
}

// worldStreamOf loads the persisted world event stream.
func worldStreamOf(s *GameState) (*Stream, error) {
	set, ok := LoadRNGSet(s.RNG)
	if !ok {
		return nil, fmt.Errorf("the world random stream could not be loaded")
	}
	stream := set.World()
	if stream == nil {
		return nil, fmt.Errorf("the world random stream is missing")
	}
	return stream, nil
}

// snapshotStreams writes one stream's cursor back into the persisted state.
//
// Only the stream that was actually drawn from is written back, so a pass that
// touches the world stream cannot perturb combat, drops or NPCs.
func snapshotStreams(state RNGState, stream *Stream) RNGState {
	if state.Streams == nil {
		state.Streams = map[string]RNGStream{}
	}
	state.Streams[StreamWorld] = stream.Snapshot()
	state.Algorithm = RNGAlgorithmSplitMix64
	return state
}

// itoa64 renders an int64 without importing strconv.
func itoa64(v int64) string {
	return string(appendIntBytes(nil, v))
}
