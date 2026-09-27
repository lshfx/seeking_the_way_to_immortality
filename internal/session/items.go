package session

import (
	"fmt"
	"strconv"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

func (s *Session) usableItems(state *engine.GameState) []engine.ItemStack {
	var out []engine.ItemStack
	if state == nil || state.Player == nil {
		return out
	}
	for _, stack := range state.Player.Inventory.Stacks {
		if item := s.marketItem(stack.ItemID); item != nil && item.UseOutsideCombat && stack.Quantity > 0 {
			out = append(out, stack)
		}
	}
	return out
}

func (s *Session) itemUseModel(m panel.Model, state *engine.GameState) panel.Model {
	m.Title = "使用随身丹药"
	m.Scene = "每次使用一枚；立刻保存，不推进游戏月份。气血已满或当前修为已满时不会消耗。"
	section := panel.DetailSection{ID: "usable-items", Title: "可直接使用的物品"}
	items := s.usableItems(state)
	start, end, current, pages := marketPageBounds(s.pageIndex, len(items))
	for i, stack := range items[start:end] {
		item := s.marketItem(stack.ItemID)
		section.Rows = append(section.Rows, panel.DetailRow{Label: item.NameZH,
			Value: fmt.Sprintf("持有%d · %s", stack.Quantity, item.Description)})
		m.Options = append(m.Options, panel.Option{Key: strconv.Itoa(i + 1), Label: "使用一枚" + item.NameZH,
			CommandKind: string(engine.KindUseItem), TargetID: item.ID})
	}
	if len(items) == 0 {
		section.Lines = []string{"没有可直接使用的丹药。筑基丹留待突破时使用。"}
	}
	m.Details = []panel.DetailSection{section}
	if current > 0 {
		m.Options = append(m.Options, panel.Option{Key: "p", Label: "上一页", CommandKind: "LOCAL_PAGE"})
	}
	if current+1 < pages {
		m.Options = append(m.Options, panel.Option{Key: "n", Label: "下一页", CommandKind: "LOCAL_PAGE"})
	}
	m.Options = append(m.Options, panel.Option{Key: "b", Label: "返回背包", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"})
	return m
}

func (s *Session) handleItemUseKey(key string, state *engine.GameState) error {
	if key == "b" {
		s.page = pageInventory
		return nil
	}
	items := s.usableItems(state)
	start, end, current, pages := marketPageBounds(s.pageIndex, len(items))
	if key == "n" && current+1 < pages {
		s.pageIndex++
		return nil
	}
	if key == "p" && current > 0 {
		s.pageIndex--
		return nil
	}
	for i, stack := range items[start:end] {
		if key != strconv.Itoa(i+1) {
			continue
		}
		before := state.Revision
		if err := s.submit(engine.KindUseItem, engine.Payload{ItemID: stack.ItemID, Quantity: 1}, stack.ItemID); err != nil {
			return err
		}
		if !s.saveFailed && s.engine.State().Revision != before {
			s.page = pageInventory
			s.notice = &panel.Notice{Kind: "info", Text: "物品已使用并自动保存；游戏时间没有推进。"}
		}
		return nil
	}
	s.unknownKey()
	return nil
}
