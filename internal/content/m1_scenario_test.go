package content

import (
	"sort"
	"strings"
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// These tests assert the TASK-11 acceptance criteria directly on the shipped
// catalogue, not on a fixture. A fixture can satisfy a rule while the shipped
// data does not, and the shipped data is what a player actually meets.
//
// tools/task11-audit prints the same facts in a form a reviewer can read; this
// file is the gate that fails a build.

// ---------------------------------------------------------------------------
// Event nodes
// ---------------------------------------------------------------------------

// designNodeIDs are the twelve nodes design 14 specifies. The list is fixed
// here rather than derived from the catalogue, so that deleting a node is a
// failure rather than a smaller catalogue.
var designNodeIDs = []string{
	"EVT-001", "EVT-002", "EVT-003", "EVT-004", "EVT-005", "EVT-006",
	"EVT-007", "EVT-008", "EVT-009", "EVT-010", "EVT-011", "EVT-012",
}

func TestEveryDesignNodeIsShipped(t *testing.T) {
	c := M1()
	have := map[string]bool{}
	for _, ev := range c.Events {
		have[ev.ID] = true
	}
	for _, id := range designNodeIDs {
		if !have[id] {
			t.Errorf("design 14 node %s is not in the shipped catalogue", id)
		}
	}
}

func TestEveryEventNodeHasOneToThreeSentences(t *testing.T) {
	// Design 14: "节点1～3句". Purpose is written for a reviewer; TextZH is what
	// the player reads, and a node without it is a menu rather than a scene.
	for _, ev := range M1().Events {
		text := strings.TrimSpace(ev.TextZH)
		if text == "" {
			t.Errorf("%s has no display text", ev.ID)
			continue
		}
		if n := sentenceCount(text); n < 1 || n > 3 {
			t.Errorf("%s has %d sentences of display text, want 1 to 3", ev.ID, n)
		}
	}
}

func TestEveryEventOffersTwoToFourChoices(t *testing.T) {
	for _, ev := range M1().Events {
		if n := len(ev.Choices); n < 2 || n > 4 {
			t.Errorf("%s offers %d choices; design 14 requires 2 to 4", ev.ID, n)
		}
	}
}

func TestEveryWarnedRiskIsPreviewed(t *testing.T) {
	// "危险提前提示": a risk the player is not told about is a trap, not a
	// choice. The preview must be non-empty and must not be a bare adjective —
	// the design asks for a concrete probability, which the RiskSpec carries
	// separately.
	for _, ev := range M1().Events {
		for _, ch := range ev.Choices {
			for _, risk := range ch.Risks {
				if strings.TrimSpace(risk.PreviewText) == "" {
					t.Errorf("%s/%s warns a risk without previewing it", ev.ID, ch.ID)
				}
				if risk.ProbabilityPct.Value <= 0 || risk.ProbabilityPct.Value > 100 {
					t.Errorf("%s/%s previews a risk with probability %d, want 1..100",
						ev.ID, ch.ID, risk.ProbabilityPct.Value)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The travel graph
// ---------------------------------------------------------------------------

func TestTravelGraphIsConnectedAndSymmetric(t *testing.T) {
	c := M1()
	neighbours := map[string][]string{}
	for _, loc := range c.Locations {
		neighbours[loc.ID] = loc.Neighbors
	}

	// Symmetry: an edge without a return would strand the player.
	for _, loc := range c.Locations {
		for _, n := range loc.Neighbors {
			if !contains(neighbours[n], loc.ID) {
				t.Errorf("%s reaches %s but not the other way round", loc.ID, n)
			}
		}
	}

	// Connectivity from the declared start.
	start := "cave_dwelling"
	if _, ok := neighbours[start]; !ok {
		t.Fatalf("the declared start location %s is not in the catalogue", start)
	}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, n := range neighbours[cur] {
			if seen[n] {
				continue
			}
			if _, declared := neighbours[n]; !declared {
				continue
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	for _, loc := range c.Locations {
		if !seen[loc.ID] {
			t.Errorf("%s is not reachable from %s", loc.ID, start)
		}
	}

	// The low-risk training ground must be the one place that is not safe, or
	// the scene column means nothing.
	unsafe := 0
	for _, loc := range c.Locations {
		if !loc.Safe {
			unsafe++
		}
	}
	if unsafe == 0 {
		t.Error("every location is marked safe, so the low-risk training ground is not modelled")
	}
}

// ---------------------------------------------------------------------------
// NPCs
// ---------------------------------------------------------------------------

func TestEveryNPCHasAnExplicitAge(t *testing.T) {
	// Design 14: "所有NPC年龄明确". It is not derivable from the realm or the
	// lifespan, and an NPC of unknown age is exactly the one that must never be
	// offered romance content later.
	c := M1()
	for _, npc := range c.NPCs {
		if npc.InitialAgeYears <= 0 {
			t.Errorf("%s declares no age", npc.ID)
		}
		if npc.LifespanYears > 0 && npc.InitialAgeYears > npc.LifespanYears {
			t.Errorf("%s starts at %d but has a lifespan of %d years",
				npc.ID, npc.InitialAgeYears, npc.LifespanYears)
		}
	}
}

func TestEveryNPCIsAdultAndItsSourceIsMarked(t *testing.T) {
	c := M1()
	if len(c.NPCs) < 3 {
		t.Fatalf("only %d NPCs are shipped; design 14 requires at least 3", len(c.NPCs))
	}
	for _, npc := range c.NPCs {
		if !npc.Adult {
			t.Errorf("%s is not marked adult", npc.ID)
		}
		// Design 14: an original addition must be marked as 新增设定 so a
		// reviewer can tell it apart from the manuscript's cast.
		if npc.FromManuscript == npc.NewSetting {
			t.Errorf("%s must be marked either as from the manuscript or as a new "+
				"setting, and not both or neither", npc.ID)
		}
	}
}

func TestEveryNPCHomeAndDialogueResolves(t *testing.T) {
	c := M1()
	locations := map[string]bool{}
	for _, loc := range c.Locations {
		locations[loc.ID] = true
	}
	dialogues := map[string]bool{}
	for _, d := range c.Dialogues {
		dialogues[d.ID] = true
	}
	for _, npc := range c.NPCs {
		if !locations[npc.HomeLocation] {
			t.Errorf("%s lives at %q, which is not a declared location", npc.ID, npc.HomeLocation)
		}
		for _, d := range npc.DialogueIDs {
			if !dialogues[d] {
				t.Errorf("%s offers dialogue %q, which is not declared", npc.ID, d)
			}
		}
	}
}

func TestEveryDialogueBelongsToADeclaredNPC(t *testing.T) {
	// The control group for the check above: dialogue must point at a real NPC
	// as well, or a "fixed fact sheet" is only half a record.
	c := M1()
	npcs := map[string]bool{}
	for _, npc := range c.NPCs {
		npcs[npc.ID] = true
	}
	for _, d := range c.Dialogues {
		if !npcs[d.NPCID] {
			t.Errorf("dialogue %s belongs to %q, which is not a declared NPC", d.ID, d.NPCID)
		}
	}
}

// ---------------------------------------------------------------------------
// The two early goals
// ---------------------------------------------------------------------------

func TestBothEarlyGoalsAreDeclared(t *testing.T) {
	c := M1()
	if len(c.EarlyGoals) != 2 {
		t.Fatalf("%d early goals are declared; ADR-001 names exactly two", len(c.EarlyGoals))
	}
	names := []string{}
	for _, g := range c.EarlyGoals {
		names = append(names, g.NameZH)
	}
	sort.Strings(names)
	want := []string{"守护所爱", "问道飞升"}
	sort.Strings(want)
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("early goal names = %v, want %v", names, want)
		}
	}
}

func TestBothEarlyGoalsCanSucceedFailAndBeAbandoned(t *testing.T) {
	// Every branch must be settable by some choice, or it is a promise rather
	// than a path. This mirrors the catalogue validator deliberately: the test
	// states the property in the shipped-data terms a reviewer reads.
	c := M1()
	setters := map[string][]string{}
	for _, ev := range c.Events {
		for _, ch := range ev.Choices {
			for _, eff := range ch.Effects {
				if strings.HasPrefix(eff.Target, "flags.") {
					name := strings.TrimPrefix(eff.Target, "flags.")
					setters[name] = append(setters[name], ev.ID+"/"+ch.ID)
				}
			}
		}
	}
	for _, g := range c.EarlyGoals {
		for _, br := range []struct{ label, flag string }{
			{"成功", g.SuccessFlag},
			{"失败", g.FailureFlag},
			{"放弃", g.AbandonFlag},
		} {
			if br.flag == "" {
				t.Errorf("%s declares no %s flag", g.ID, br.label)
				continue
			}
			if len(setters[br.flag]) == 0 {
				t.Errorf("%s 的%s分支没有选项能设置 %s", g.ID, br.label, br.flag)
			}
		}
		if g.SuccessFlag == g.FailureFlag || g.SuccessFlag == g.AbandonFlag || g.FailureFlag == g.AbandonFlag {
			t.Errorf("%s reuses one flag for two outcomes", g.ID)
		}
	}
}

func TestBothEarlyGoalsAreAdvanceableAsARogue(t *testing.T) {
	// ADR-001: "两个目标均可通过散修路径推进，不以入宗、恋爱或AI为通关前提."
	//
	// The check is a necessary condition rather than a full reachability proof:
	// it verifies that the choice advancing the goal, and the event carrying it,
	// do not require sect membership. Anything stronger would have to trace item
	// provenance, and claiming that would be claiming more than is checked.
	c := M1()

	sectGated := map[string]bool{} // event id -> the event itself is gated
	for _, ev := range c.Events {
		for _, cond := range ev.Eligibility {
			if cond.Kind == engine.CondSectMember && cond.Value == 1 {
				sectGated[ev.ID] = true
			}
		}
	}

	open := map[string]bool{} // goal success flag -> reachable as a rogue
	for _, ev := range c.Events {
		for _, ch := range ev.Choices {
			gated := sectGated[ev.ID]
			for _, cond := range ch.Requires {
				if cond.Kind == engine.CondSectMember && cond.Value == 1 {
					gated = true
				}
			}
			if gated {
				continue
			}
			for _, eff := range ch.Effects {
				open[strings.TrimPrefix(eff.Target, "flags.")] = true
			}
		}
	}

	for _, g := range c.EarlyGoals {
		if !open[g.SuccessFlag] {
			t.Errorf("%s can only be advanced by joining a sect; ADR-001 requires a "+
				"rogue path for both goals", g.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Breakthroughs and paths
// ---------------------------------------------------------------------------

func TestEveryMajorBreakthroughHasATrialNode(t *testing.T) {
	// Design 14: "补足后继雷劫节点". A major advance without a tribulation would
	// be a dice roll rather than a scene.
	c := M1()
	events := map[string]bool{}
	for _, ev := range c.Events {
		events[ev.ID] = true
	}

	majors := 0
	for _, bt := range c.Breakthroughs {
		if !bt.IsMajor {
			continue
		}
		majors++
		if len(bt.TrialNodes) == 0 {
			t.Errorf("major breakthrough %s -> %s has no trial node",
				bt.FromRealm, bt.ToRealm)
			continue
		}
		for _, node := range bt.TrialNodes {
			if !events[node] {
				t.Errorf("breakthrough %s -> %s names trial node %q, which is not declared",
					bt.FromRealm, bt.ToRealm, node)
			}
		}
	}
	if majors == 0 {
		t.Fatal("no major breakthrough is declared, so the tribulation rule proves nothing")
	}
}

func TestOnlyTheHumanPathIsOpen(t *testing.T) {
	c := M1()
	open := map[engine.Path]bool{}
	for _, p := range c.M1Paths {
		open[p] = true
	}
	if !open[engine.PathHuman] {
		t.Error("the human path is not open, so nothing is playable")
	}
	if open[engine.PathEarthly] || open[engine.PathHeavenly] {
		t.Error("M1 must not open the earthly or heavenly path; ADR-001 would have to be revised")
	}
	// The unopened paths must still be described, so the range is visible to a
	// reviewer rather than silently absent.
	described := 0
	for _, r := range c.Realms {
		_ = r
		described++
	}
	if described != len(engine.RealmOrder) {
		t.Errorf("the catalogue describes %d realms, want all %d", described, len(engine.RealmOrder))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sentenceCount(text string) int {
	n := 0
	for _, r := range text {
		switch r {
		case '。', '！', '？', '.', '!', '?':
			n++
		}
	}
	return n
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
