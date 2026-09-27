package session

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

func (s *Session) questDefinition(id string) *engine.QuestDefinition {
	for i := range s.catalogue.Quests {
		if s.catalogue.Quests[i].ID == id {
			return &s.catalogue.Quests[i]
		}
	}
	return nil
}

func (s *Session) questsModel(m panel.Model, state *engine.GameState) panel.Model {
	m.Title = "委托与收入"
	m.Scene = s.sceneName(state)
	section := panel.DetailSection{ID: "quests", Title: "接取与领取不耗月；执行一次委托耗时一月"}
	start, end, current, pages := marketPageBounds(s.pageIndex, len(s.catalogue.Quests))
	for i, def := range s.catalogue.Quests[start:end] {
		qs := questState(state, def.ID)
		section.Rows = append(section.Rows, panel.DetailRow{Label: def.NameZH,
			Value: questStatusText(qs, state.Counters.WorldMonth) + " · " + def.Description})
		m.Options = append(m.Options, panel.Option{Key: strconv.Itoa(i + 1), Label: "查看" + def.NameZH,
			CommandKind: "LOCAL_PAGE", TargetID: def.ID})
	}
	if pages > 1 {
		section.Lines = append(section.Lines, fmt.Sprintf("第%d/%d页；选中委托后查看接取、执行或领取。", current+1, pages))
	}
	m.Details = []panel.DetailSection{section}
	if current > 0 {
		m.Options = append(m.Options, panel.Option{Key: "p", Label: "上一页", CommandKind: "LOCAL_PAGE"})
	}
	if current+1 < pages {
		m.Options = append(m.Options, panel.Option{Key: "n", Label: "下一页", CommandKind: "LOCAL_PAGE"})
	}
	m.Options = append(m.Options, panel.Option{Key: "b", Label: "返回场景", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"})
	return m
}

func (s *Session) questDetailModel(m panel.Model, state *engine.GameState) panel.Model {
	def := s.questDefinition(s.selectedQuestID)
	if def == nil {
		m.Title = "委托不存在"
		m.Details = []panel.DetailSection{{ID: "quest-error", Title: "提示", Lines: []string{"当前委托已不在内容目录中。"}}}
		m.Options = []panel.Option{{Key: "b", Label: "返回委托", CommandKind: "LOCAL_PAGE"}, {Key: "q", Label: "退出", CommandKind: "QUIT"}}
		return m
	}
	qs := questState(state, def.ID)
	m.Title = def.NameZH
	m.Scene = def.Description
	lines := []string{
		"状态：" + questStatusText(qs, state.Counters.WorldMonth),
		fmt.Sprintf("执行地点：%s；耗时：%d 游戏月", s.locationName(def.LocationID), def.MonthCost.Value),
		fmt.Sprintf("失败概率：%d‰；失败不发成功奖励。", def.FailureChancePermille.Value),
	}
	if len(def.RequiredItems) > 0 {
		var costs []string
		for _, item := range def.RequiredItems {
			costs = append(costs, fmt.Sprintf("%s×%d", s.itemLabel(item.ItemID), item.Quantity))
		}
		lines = append(lines, "接取时预留："+strings.Join(costs, "、"))
	}
	if qs.AvailableAfterWorldMonth > state.Counters.WorldMonth {
		lines = append(lines, fmt.Sprintf("下次可接取：世界月%d。", qs.AvailableAfterWorldMonth))
	}
	m.Details = []panel.DetailSection{{ID: "quest-detail", Title: "委托说明", Lines: lines}}
	if qs.Status == engine.QuestAccepted {
		m.Options = append(m.Options, panel.Option{Key: "x", Label: "执行委托（耗时1月）", CommandKind: string(engine.KindQuestRun), TargetID: def.ID, CostText: "耗时 1 月"})
	}
	if qs.Status == engine.QuestComplete {
		m.Options = append(m.Options, panel.Option{Key: "c", Label: "领取成功奖励", CommandKind: string(engine.KindClaimReward), TargetID: def.ID})
	}
	if qs.Status == engine.QuestOffered || qs.Status == engine.QuestClaimed || qs.Status == engine.QuestFailed || qs.Status == engine.QuestAbandoned || qs.Status == engine.QuestExpired {
		if def.Repeatable || qs.Status == engine.QuestOffered {
			m.Options = append(m.Options, panel.Option{Key: "a", Label: "接取委托", CommandKind: string(engine.KindAcceptQuest), TargetID: def.ID})
		}
	}
	if def.ID == "quest_sect_entry" && qs.Status == engine.QuestClaimed && state.Player.SectID == "" {
		m.Options = append(m.Options, panel.Option{Key: "j", Label: "确认加入青云宗", CommandKind: string(engine.KindJoinSect), TargetID: "qingyun_sect"})
	}
	m.Options = append(m.Options, panel.Option{Key: "b", Label: "返回委托列表", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"})
	return m
}

func (s *Session) questConfirmModel(m panel.Model, state *engine.GameState) panel.Model {
	def := s.questDefinition(s.selectedQuestID)
	if def == nil || s.questToken == "" {
		m.Title = "委托预览已取消"
		m.Options = []panel.Option{{Key: "b", Label: "返回委托", CommandKind: "LOCAL_PAGE"}, {Key: "q", Label: "退出", CommandKind: "QUIT"}}
		return m
	}
	m.Title = "确认执行委托"
	m.Scene = s.sceneName(state)
	m.Confirmation = &panel.ConfirmationBlock{Token: s.questToken, Title: def.NameZH,
		Summary:      fmt.Sprintf("执行地点：%s · 成功后可领取配置奖励", s.locationName(def.LocationID)),
		CostText:     fmt.Sprintf("耗时%d游戏月；失败概率%d‰，失败不发成功奖励", def.MonthCost.Value, def.FailureChancePermille.Value),
		ConfirmLabel: "确认执行", CancelLabel: "取消", ExpiresAtRevision: state.Revision}
	m.Options = []panel.Option{
		{Key: "c", Label: "确认执行", CommandKind: string(engine.KindQuestRun), TargetID: def.ID, CostText: "耗时 1 月"},
		{Key: "b", Label: "取消返回委托", CommandKind: "LOCAL_PAGE"},
		{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"},
	}
	return m
}

func (s *Session) handleQuestsKey(key string, state *engine.GameState) error {
	if key == "b" {
		s.page = pageMain
		return nil
	}
	start, end, current, pages := marketPageBounds(s.pageIndex, len(s.catalogue.Quests))
	if key == "n" && current+1 < pages {
		s.pageIndex++
		return nil
	}
	if key == "p" && current > 0 {
		s.pageIndex--
		return nil
	}
	for i := start; i < end; i++ {
		if key != strconv.Itoa(i-start+1) {
			continue
		}
		s.selectedQuestID = s.catalogue.Quests[i].ID
		s.page = pageQuestDetail
		return nil
	}
	s.unknownKey()
	return nil
}

func (s *Session) handleQuestDetailKey(key string, state *engine.GameState) error {
	if key == "b" {
		s.page = pageQuests
		return nil
	}
	def := s.questDefinition(s.selectedQuestID)
	if def == nil {
		s.unknownKey()
		return nil
	}
	qs := questState(state, def.ID)
	switch key {
	case "a":
		return s.submit(engine.KindAcceptQuest, engine.Payload{QuestID: def.ID}, def.ID)
	case "x":
		if qs.Status != engine.QuestAccepted {
			s.Notify("请先接取这项委托。")
			return nil
		}
		s.questToken = fmt.Sprintf("quest:%d:%s", state.Revision, def.ID)
		state.Pending.Confirmation = &engine.PendingConfirmation{Token: s.questToken, Kind: engine.KindQuestRun,
			TargetID: def.ID, ExpiresAtRevision: state.Revision,
			Preview: engine.ConfirmationPreview{MonthCost: def.MonthCost.Value,
				Risks: []string{fmt.Sprintf("失败概率%d‰；失败不发奖励", def.FailureChancePermille.Value)}}}
		s.page = pageQuestConfirm
		return nil
	case "c":
		return s.submit(engine.KindClaimReward, engine.Payload{QuestID: def.ID}, def.ID)
	case "j":
		return s.submit(engine.KindJoinSect, engine.Payload{QuestID: "qingyun_sect"}, "qingyun_sect")
	default:
		s.unknownKey()
		return nil
	}
}

func (s *Session) handleQuestConfirmKey(key string, state *engine.GameState) error {
	if key == "b" {
		state.Pending.Confirmation = nil
		s.questToken = ""
		s.page = pageQuestDetail
		return nil
	}
	if key != "c" {
		s.unknownKey()
		return nil
	}
	def := s.questDefinition(s.selectedQuestID)
	if def == nil {
		s.page = pageQuests
		return nil
	}
	err := s.submitWithToken(engine.KindQuestRun, engine.Payload{QuestID: def.ID}, def.ID, s.questToken)
	if err == nil && !s.saveFailed {
		s.questToken = ""
	}
	return err
}

func questState(state *engine.GameState, id string) engine.QuestState {
	if state != nil && state.World != nil {
		if qs, ok := state.World.Quests[id]; ok {
			return qs
		}
	}
	return engine.QuestState{QuestID: id, Status: engine.QuestOffered}
}

func questStatusText(qs engine.QuestState, month int64) string {
	switch qs.Status {
	case engine.QuestAccepted:
		return "已接取"
	case engine.QuestComplete:
		return "已完成，待领取"
	case engine.QuestClaimed:
		if qs.AvailableAfterWorldMonth > month {
			return fmt.Sprintf("已领取，冷却至月%d", qs.AvailableAfterWorldMonth)
		}
		return "可再次接取"
	case engine.QuestFailed:
		return "执行失败，无成功奖励"
	default:
		return "可接取"
	}
}

func (s *Session) itemLabel(id string) string {
	for _, item := range s.catalogue.Items {
		if item.ID == id {
			return item.NameZH
		}
	}
	return id
}
