// Command task11-audit reports whether the shipped M1 catalogue satisfies the
// TASK-11 content acceptance criteria.
//
// It exists because the criteria are content-shaped, not code-shaped: "each node
// offers 2-4 choices", "every NPC's age is explicit", "the location graph is
// connected", "the two early goals can succeed, fail or be abandoned". A test
// can assert those, but a reviewer also needs to *read* the answer, and the
// design document's 内容交付表 asks for a per-node table.
//
// Run:
//
//	go run ./tools/task11-audit
//
// It prints the audit and exits non-zero when a criterion is violated, so it can
// be used as a gate rather than only as a report.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/content"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

func main() {
	cat, err := content.LoadM1()
	if err != nil {
		fmt.Println("the shipped catalogue does not load:", err)
		os.Exit(1)
	}

	problems := 0
	report := func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
	problem := func(format string, args ...any) {
		problems++
		fmt.Printf("  !! "+format+"\n", args...)
	}

	// --- Event nodes -------------------------------------------------------
	report("== 事件节点（设计 14 的 EVT-001～012）==")
	report("%-9s %-7s %-6s %-7s %-6s %-4s %-4s %s", "节点", "选项数", "风险", "必发", "教程", "限次", "句数", "后继节点")
	for _, ev := range cat.Events {
		nextNodes := make([]string, 0, len(ev.FollowUps))
		for _, fu := range ev.FollowUps {
			nextNodes = append(nextNodes, fu.NodeID)
		}
		sort.Strings(nextNodes)

		report("%-9s %-7d %-6d %-7v %-6v %-4d %-4d %v",
			ev.ID, len(ev.Choices), countRisks(ev), ev.Forced, ev.IsTutorial, ev.MaxOccurrences,
			sentences(ev.TextZH), nextNodes)

		// Design 14: "节点1～3句". The node needs its own display text, separate
		// from Purpose, which is written for a reviewer.
		if strings.TrimSpace(ev.TextZH) == "" {
			problem("%s has no display text; design 14 requires one to three sentences", ev.ID)
		} else if n := sentences(ev.TextZH); n < 1 || n > 3 {
			problem("%s has %d sentences of display text; design 14 requires one to three", ev.ID, n)
		}

		// Design 14: "节点1～3句、2～4选择".
		if len(ev.Choices) < 2 || len(ev.Choices) > 4 {
			problem("%s offers %d choices; design 14 requires 2 to 4", ev.ID, len(ev.Choices))
		}
		if len(ev.Choices) == 0 {
			problem("%s offers no choices at all", ev.ID)
		}
		// A risky choice must warn before it is taken.
		for _, ch := range ev.Choices {
			for _, risk := range ch.Risks {
				if risk.PreviewText == "" {
					problem("%s/%s warns a risk without previewing it", ev.ID, ch.ID)
				}
			}
		}
	}

	// --- Location graph ----------------------------------------------------
	report("")
	report("== 场景连通（设计 6.3 的四个 M1 场景）==")
	locationIDs := map[string]bool{}
	for _, loc := range cat.Locations {
		locationIDs[loc.ID] = true
	}
	for _, loc := range cat.Locations {
		report("%-14s %-10s 安全=%-5v 邻接=%v", loc.ID, loc.Environment, loc.Safe, loc.Neighbors)
		for _, n := range loc.Neighbors {
			if !locationIDs[n] {
				problem("%s names neighbour %q which is not declared", loc.ID, n)
			}
		}
		if len(loc.Neighbors) == 0 {
			problem("%s has no neighbours, so it is unreachable", loc.ID)
		}
	}
	if unreachable := reachableFrom(cat.Locations); len(unreachable) > 0 {
		problem("these locations are not reachable from the first declared location: %v", unreachable)
	}

	// --- NPCs --------------------------------------------------------------
	report("")
	report("== NPC（设计 14：至少 3 名具名 NPC，年龄必须明确）==")
	for _, npc := range cat.NPCs {
		origin := "原创"
		if npc.FromManuscript {
			origin = "原稿"
		}
		report("%-14s %-8s 成年=%-5v 出身=%-6s 境界=%s%s 寿元=%d年 台词=%d",
			npc.ID, npc.NameZH, npc.Adult, origin, npc.InitialRealm, npc.InitialTier,
			npc.LifespanYears, len(npc.DialogueIDs))
		if npc.NewSetting && npc.FromManuscript {
			problem("%s is marked both as manuscript and as a new setting", npc.ID)
		}
		// Design 14: "所有NPC年龄明确". An NPC with no declared age is not
		// explicit, and a later relationship system cannot infer one safely.
		if npc.InitialAgeYears == 0 {
			problem("%s declares no age; design 14 requires every NPC's age to be explicit", npc.ID)
		}
		if npc.InitialAgeYears > npc.LifespanYears {
			problem("%s starts at age %d but has a lifespan of %d years",
				npc.ID, npc.InitialAgeYears, npc.LifespanYears)
		}
		if !npc.Adult {
			problem("%s is not marked adult; M1 ships no content that may involve a minor", npc.ID)
		}
	}
	if len(cat.NPCs) < 3 {
		problem("only %d NPCs are declared; design 14 requires at least 3", len(cat.NPCs))
	}

	// --- Early goals -------------------------------------------------------
	report("")
	report("== 两条早期目标（设计 6.3：问道飞升 / 守护所爱）==")
	for _, quest := range cat.Quests {
		report("%-18s %-10s 可重复=%-5v 冷却=%-3d 需入宗=%-5v 发放者=%s",
			quest.ID, quest.Kind, quest.Repeatable, quest.CooldownMonths, quest.RequiresSect, quest.GiverNPCID)
		if quest.MonthCost.Value <= 0 {
			problem("%s costs no month, so it cannot be run as a 13.1 action", quest.ID)
		}
		if quest.GiverNPCID != "" && !npcDeclared(&cat, quest.GiverNPCID) {
			problem("%s names giver %q which is not declared", quest.ID, quest.GiverNPCID)
		}
	}

	// --- Breakthrough tribulation nodes ------------------------------------
	report("")
	report("== 突破表与后继雷劫节点（设计 14：补足后继雷劫节点）==")
	trialRefs := map[string]int{}
	for _, bt := range cat.Breakthroughs {
		for _, node := range bt.TrialNodes {
			trialRefs[node]++
		}
	}
	if len(trialRefs) == 0 {
		problem("no breakthrough declares a trial node, so a major breakthrough has no tribulation to resolve")
	}
	for _, node := range sortedKeys(trialRefs) {
		declared := false
		for _, ev := range cat.Events {
			if ev.ID == node {
				declared = true
				break
			}
		}
		report("%-9s 被 %d 条突破表引用%s", node, trialRefs[node], map[bool]string{true: "", false: "（未声明）"}[declared])
		if !declared {
			problem("trial node %q is referenced but not declared as an event", node)
		}
	}

	// --- Early goals -------------------------------------------------------
	report("")
	report("== 两条早期目标的成功 / 失败 / 放弃分支 ==")
	flagSetters := map[string]int{}
	for _, ev := range cat.Events {
		for _, ch := range ev.Choices {
			for _, eff := range ch.Effects {
				if strings.HasPrefix(eff.Target, "flags.") {
					flagSetters[strings.TrimPrefix(eff.Target, "flags.")]++
				}
			}
		}
	}
	for _, goal := range cat.EarlyGoals {
		report("%-18s %-10s 成功=%-28s 失败=%-28s 放弃=%s",
			goal.ID, goal.NameZH, goal.SuccessFlag, goal.FailureFlag, goal.AbandonFlag)
		for _, br := range []struct{ name, flag string }{
			{"成功", goal.SuccessFlag}, {"失败", goal.FailureFlag}, {"放弃", goal.AbandonFlag},
		} {
			if br.flag == "" {
				problem("%s declares no %s flag", goal.ID, br.name)
			} else if flagSetters[br.flag] == 0 {
				problem("%s 的%s分支没有选项能设置 %s，分支不可达", goal.ID, br.name, br.flag)
			}
		}
	}
	if len(cat.EarlyGoals) != 2 {
		problem("M1 declares %d early goals; ADR-001 names exactly two", len(cat.EarlyGoals))
	}

	// --- Non-human path hints ---------------------------------------------
	report("")
	report("== 其他道途的范围提示（设计 6.3：未开发入口标记未开放）==")
	open := map[engine.Path]bool{}
	for _, p := range cat.M1Paths {
		open[p] = true
	}
	for _, p := range []engine.Path{engine.PathHuman, engine.PathEarthly, engine.PathHeavenly} {
		report("%-10s 本版开放=%v", p, open[p])
	}
	if open[engine.PathEarthly] || open[engine.PathHeavenly] {
		problem("M1 must not open the earthly or heavenly path; ADR-001 would have to be revised first")
	}
	if !open[engine.PathHuman] {
		problem("the human path is not open, so nothing is playable")
	}

	// --- Summary -----------------------------------------------------------
	report("")
	if problems == 0 {
		report("审计通过：内容满足 TASK-11 的验收条件。")
		return
	}
	report("审计发现 %d 处缺口。", problems)
	os.Exit(1)
}

// sentences counts sentence-ending punctuation in a node's display text. It is
// the same proxy the validator uses, so the report and the gate agree.
func sentences(text string) int {
	n := 0
	for _, r := range text {
		switch r {
		case '。', '！', '？', '.', '!', '?':
			n++
		}
	}
	return n
}

func countRisks(ev engine.EventDefinition) int {
	n := 0
	for _, ch := range ev.Choices {
		n += len(ch.Risks)
	}
	return n
}

func npcDeclared(cat *engine.Catalogue, id string) bool {
	for _, npc := range cat.NPCs {
		if npc.ID == id {
			return true
		}
	}
	return false
}

// reachableFrom walks the travel graph and returns the locations that cannot be
// reached from the first declared one.
func reachableFrom(locations []engine.LocationDefinition) []string {
	if len(locations) == 0 {
		return nil
	}
	adjacent := map[string][]string{}
	for _, loc := range locations {
		adjacent[loc.ID] = loc.Neighbors
	}

	seen := map[string]bool{locations[0].ID: true}
	queue := []string{locations[0].ID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[current] {
			if seen[next] {
				continue
			}
			if _, ok := adjacent[next]; !ok {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}

	var unreachable []string
	for _, loc := range locations {
		if !seen[loc.ID] {
			unreachable = append(unreachable, loc.ID)
		}
	}
	sort.Strings(unreachable)
	return unreachable
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
