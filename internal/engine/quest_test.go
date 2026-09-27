package engine

import (
	"fmt"
	"testing"
)

func questTestCatalogue() *Catalogue {
	cat := testCatalogue()
	cat.Items = []ItemDefinition{{ID: "herb", NameZH: "草药", Category: ItemMaterial, StackLimit: 99}}
	cat.NPCs = []NPCDefinition{{ID: "wen", NameZH: "温管事", Adult: true, InitialAgeYears: 40,
		LifespanYears: 100, HomeLocation: "market"}}
	cat.Locations = []LocationDefinition{
		{ID: "market", NameZH: "坊市", Neighbors: []string{"sect"}, Safe: true},
		{ID: "sect", NameZH: "宗门", Neighbors: []string{"market"}, Safe: true},
	}
	cat.Quests = []QuestDefinition{
		{ID: "chore", NameZH: "杂务", Kind: QuestChore, MonthCost: ConfigValue{Value: 1}, LocationID: "market",
			FailureChancePermille: ConfigValue{Value: 0}, GiverNPCID: "wen", Repeatable: true,
			Rewards: []GrantEffect{{Kind: GrantAdditive, Target: "resources.spirit_stones", Amount: 30, Reason: "test chore"}}},
		{ID: "failed", NameZH: "失败委托", Kind: QuestDelivery, MonthCost: ConfigValue{Value: 1}, LocationID: "market",
			FailureChancePermille: ConfigValue{Provenance: ProvenanceDesignNote, Value: PermilleScale}, Repeatable: true,
			Rewards: []GrantEffect{{Kind: GrantAdditive, Target: "resources.spirit_stones", Amount: 99, Reason: "test reward"}}},
		{ID: "entry", NameZH: "入宗试行", Kind: QuestDelivery, MonthCost: ConfigValue{Value: 1}, LocationID: "sect",
			FailureChancePermille: ConfigValue{Value: 0}, Repeatable: false,
			Rewards: []GrantEffect{{Kind: GrantAdditive, Target: "resources.contribution", Amount: 10, Reason: "test entry"}}},
		{ID: "patrol", NameZH: "巡逻", Kind: QuestPatrol, MonthCost: ConfigValue{Value: 1}, LocationID: "sect",
			FailureChancePermille: ConfigValue{Value: 0}, RequiresSect: true, Repeatable: true,
			Rewards: []GrantEffect{{Kind: GrantAdditive, Target: "resources.contribution", Amount: 20, Reason: "test patrol"}}},
	}
	cat.Sects = []SectDefinition{{ID: "sect", NameZH: "宗门", EntryQuestID: "entry",
		MonthlyIncome: ConfigValue{Value: 20}}}
	return cat
}

func questTestEngine(t *testing.T) (*Engine, *MemoryStore) {
	t.Helper()
	state := readyState()
	state.World.CurrentLocation = "market"
	state.World.VisitedLocations = []string{"market", "sect"}
	state.World.NPCs = map[string]NPC{"wen": {ID: "wen", IsAlive: true, LifespanYears: 100, Location: "market"}}
	state.Player.Inventory.Stacks = []ItemStack{{ItemID: "herb", Quantity: 3}}
	state.Player.Resources[ResSpiritStones] = 100
	return newTestEngineWithCatalogue(t, state, questTestCatalogue())
}

func questAcceptCommand(e *Engine, actionID, id string) Command {
	c := cmd(e, actionID, KindAcceptQuest)
	c.TargetID = id
	c.Payload = Payload{QuestID: id}
	return c
}

func questRunCommand(e *Engine, actionID, id string) Command {
	c := cmd(e, actionID, KindQuestRun)
	c.TargetID = id
	c.Payload = Payload{QuestID: id}
	e.State().Pending.Confirmation = &PendingConfirmation{Token: "confirm-" + id,
		Kind: KindQuestRun, TargetID: id, ExpiresAtRevision: e.State().Revision}
	c.ConfirmationToken = "confirm-" + id
	return c
}

func questClaimCommand(e *Engine, actionID, id string) Command {
	c := cmd(e, actionID, KindClaimReward)
	c.TargetID = id
	c.Payload = Payload{QuestID: id}
	return c
}

func TestQuestLifecycleReservesRunsAndPaysOnce(t *testing.T) {
	e, store := questTestEngine(t)
	if r := e.Submit(questAcceptCommand(e, "accept", "chore")); !r.OK {
		t.Fatalf("accept rejected: %s", r.Code)
	}
	if got := e.State().World.Quests["chore"]; got.Status != QuestAccepted || got.AcceptedWorldMonth != 0 {
		t.Fatalf("wrong accepted state: %+v", got)
	}
	if r := e.Submit(questRunCommand(e, "run", "chore")); !r.OK || r.MonthCostApplied != 1 {
		t.Fatalf("run rejected or did not settle one month: %#v", r)
	}
	if got := e.State().World.Quests["chore"]; got.Status != QuestComplete || got.Progress != 1 {
		t.Fatalf("wrong completed state: %+v", got)
	}
	before := e.State().Player.Resources[ResSpiritStones]
	if r := e.Submit(questClaimCommand(e, "claim", "chore")); !r.OK {
		t.Fatalf("claim rejected: %s", r.Code)
	}
	if e.State().Player.Resources[ResSpiritStones] != before+30 || e.State().World.Quests["chore"].Status != QuestClaimed {
		t.Fatal("successful quest did not pay once")
	}
	if r := e.Submit(questClaimCommand(e, "claim-again", "chore")); r.OK {
		t.Fatal("a claimed reward was paid twice")
	}
	restarted := NewEngine(store, e.Catalogue, CloneGameState(store.Committed()))
	if got := restarted.State().World.Quests["chore"]; got.Status != QuestClaimed || restarted.State().Player.Resources[ResSpiritStones] != before+30 {
		t.Fatalf("quest progress did not survive restart: %+v", got)
	}
}

func TestFailedQuestNeverPaysSuccessReward(t *testing.T) {
	e, _ := questTestEngine(t)
	if r := e.Submit(questAcceptCommand(e, "accept-fail", "failed")); !r.OK {
		t.Fatalf("accept rejected: %s", r.Code)
	}
	before := e.State().Player.Resources[ResSpiritStones]
	if r := e.Submit(questRunCommand(e, "run-fail", "failed")); !r.OK {
		t.Fatalf("failed run was rejected: %s", r.Code)
	}
	if got := e.State().World.Quests["failed"]; got.Status != QuestFailed {
		t.Fatalf("quest status = %s, want failed", got.Status)
	}
	if r := e.Submit(questClaimCommand(e, "claim-fail", "failed")); r.OK {
		t.Fatal("failed quest accepted a success claim")
	}
	if e.State().Player.Resources[ResSpiritStones] != before {
		t.Fatal("failed quest changed the reward balance")
	}
}

func TestSafeChoreMatchesFoundationPillIncomeBaseline(t *testing.T) {
	e, _ := questTestEngine(t)
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("chore-%d", i)
		if r := e.Submit(questAcceptCommand(e, id+"-accept", "chore")); !r.OK {
			t.Fatalf("chore %d accept rejected: %s", i, r.Code)
		}
		if r := e.Submit(questRunCommand(e, id+"-run", "chore")); !r.OK || r.MonthCostApplied != 1 {
			t.Fatalf("chore %d run did not consume one month: %#v", i, r)
		}
		if r := e.Submit(questClaimCommand(e, id+"-claim", "chore")); !r.OK {
			t.Fatalf("chore %d claim rejected: %s", i, r.Code)
		}
	}
	if got := e.State().Player.Resources[ResSpiritStones]; got != 520 {
		t.Fatalf("14 safe chores should turn 100 stones into 520, got %d", got)
	}
	if got := e.State().Counters.WorldMonth; got != 14 {
		t.Fatalf("14 safe chores should cost 14 world months, got %d", got)
	}
}

func TestSectEntryAndPatrolIncome(t *testing.T) {
	e, _ := questTestEngine(t)
	e.State().World.CurrentLocation = "sect"
	if r := e.Submit(questAcceptCommand(e, "entry-a", "entry")); !r.OK {
		t.Fatalf("entry accept rejected: %s", r.Code)
	}
	if r := e.Submit(questRunCommand(e, "entry-r", "entry")); !r.OK {
		t.Fatalf("entry run rejected: %s", r.Code)
	}
	if r := e.Submit(questClaimCommand(e, "entry-c", "entry")); !r.OK {
		t.Fatalf("entry claim rejected: %s", r.Code)
	}
	if r := e.Submit(func() Command {
		c := cmd(e, "join", KindJoinSect)
		c.TargetID = "sect"
		c.Payload = Payload{QuestID: "sect"}
		return c
	}()); !r.OK {
		t.Fatalf("join rejected: %s", r.Code)
	}
	if e.State().Player.SectID != "sect" {
		t.Fatal("sect membership was not recorded")
	}
	if r := e.Submit(questAcceptCommand(e, "patrol-a", "patrol")); !r.OK {
		t.Fatalf("patrol accept rejected: %s", r.Code)
	}
	if r := e.Submit(questRunCommand(e, "patrol-r", "patrol")); !r.OK {
		t.Fatalf("patrol run rejected: %s", r.Code)
	}
	if r := e.Submit(questClaimCommand(e, "patrol-c", "patrol")); !r.OK {
		t.Fatalf("patrol claim rejected: %s", r.Code)
	}
	if e.State().Player.Resources[ResContribution] != 30 || e.State().Player.Resources[ResSpiritStones] != 120 {
		t.Fatalf("sect entry/patrol resources wrong: %#v", e.State().Player.Resources)
	}
}
