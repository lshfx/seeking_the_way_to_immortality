package session

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

const marketPageSize = 3

func (s *Session) travelModel(m panel.Model, state *engine.GameState) panel.Model {
	m.Title = "前往邻近场景"
	m.Scene = s.sceneName(state)
	section := panel.DetailSection{ID: "travel", Title: "可到达地点"}
	for i, id := range s.neighbours(state) {
		name := s.locationName(id)
		section.Rows = append(section.Rows, panel.DetailRow{Label: name, Value: "移动不耗游戏月"})
		m.Options = append(m.Options, panel.Option{Key: strconv.Itoa(i + 1), Label: "前往" + name,
			CommandKind: string(engine.KindTravel), TargetID: id})
	}
	if len(section.Rows) == 0 {
		section.Lines = []string{"当前位置没有可到达的邻近地点。"}
	}
	m.Details = []panel.DetailSection{section}
	m.Options = append(m.Options, panel.Option{Key: "b", Label: "返回", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"})
	return m
}

func (s *Session) neighbours(state *engine.GameState) []string {
	if state == nil || state.World == nil {
		return nil
	}
	for _, location := range s.catalogue.Locations {
		if location.ID == state.World.CurrentLocation {
			return location.Neighbors
		}
	}
	return nil
}

func (s *Session) locationName(id string) string {
	for _, location := range s.catalogue.Locations {
		if location.ID == id {
			return location.NameZH
		}
	}
	return id
}

func (s *Session) handleTravelKey(key string, state *engine.GameState) error {
	if key == "b" {
		s.page = pageMain
		return nil
	}
	for i, id := range s.neighbours(state) {
		if key == strconv.Itoa(i+1) {
			return s.submit(engine.KindTravel, engine.Payload{LocationID: id}, id)
		}
	}
	s.unknownKey()
	return nil
}

func (s *Session) marketOffers(state *engine.GameState) []engine.MarketOfferDefinition {
	if state == nil || state.Player == nil {
		return nil
	}
	if s.tradeSide != engine.TradeSell {
		return s.catalogue.MarketOffers
	}
	var held []engine.MarketOfferDefinition
	for _, offer := range s.catalogue.MarketOffers {
		for _, stack := range state.Player.Inventory.Stacks {
			if stack.ItemID == offer.ItemID && stack.Quantity > 0 {
				held = append(held, offer)
				break
			}
		}
	}
	return held
}

func (s *Session) marketModel(m panel.Model, state *engine.GameState) panel.Model {
	m.Title = "坊市 · 买入"
	if s.tradeSide == engine.TradeSell {
		m.Title = "坊市 · 卖出"
	}
	m.Scene = s.sceneName(state)
	section := panel.DetailSection{ID: "market", Title: "明码标价；离开页面不会刷新库存"}
	offers := s.marketOffers(state)
	start, end, pageIndex, pageCount := marketPageBounds(s.pageIndex, len(offers))
	for i, offer := range offers[start:end] {
		item := s.marketItem(offer.ItemID)
		if item == nil {
			continue
		}
		stock := state.World.MarketStock[offer.ItemID]
		held := int64(0)
		for _, stack := range state.Player.Inventory.Stacks {
			if stack.ItemID == offer.ItemID {
				held = stack.Quantity
				break
			}
		}
		price := item.BuyPrice.Value
		if s.tradeSide == engine.TradeSell {
			price = item.SellPrice.Value
		}
		section.Rows = append(section.Rows, panel.DetailRow{Label: item.NameZH,
			Value: fmt.Sprintf("单价%d灵石 · 坊市%d · 持有%d", price, stock, held)})
		m.Options = append(m.Options, panel.Option{Key: strconv.Itoa(i + 1), Label: "选择" + item.NameZH,
			CommandKind: string(engine.KindTrade), TargetID: offer.ItemID})
	}
	if len(offers) == 0 {
		section.Lines = []string{"没有可出售的随身物品。"}
	}
	if pageCount > 1 {
		section.Lines = append(section.Lines, fmt.Sprintf("第%d/%d页；价格和库存不随翻页改变。", pageIndex+1, pageCount))
	}
	m.Details = []panel.DetailSection{section}
	if pageIndex > 0 {
		m.Options = append(m.Options, panel.Option{Key: "p", Label: "上一页", CommandKind: "LOCAL_PAGE"})
	}
	if pageIndex+1 < pageCount {
		m.Options = append(m.Options, panel.Option{Key: "n", Label: "下一页", CommandKind: "LOCAL_PAGE"})
	}
	label := "切换到卖出"
	if s.tradeSide == engine.TradeSell {
		label = "切换到买入"
	}
	m.Options = append(m.Options,
		panel.Option{Key: "t", Label: label, CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "b", Label: "返回场景", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"})
	return m
}

func marketPageBounds(index, count int) (start, end, current, pages int) {
	pages = (count + marketPageSize - 1) / marketPageSize
	if pages == 0 {
		pages = 1
	}
	if index < 0 {
		index = 0
	}
	if index >= pages {
		index = pages - 1
	}
	start = index * marketPageSize
	end = start + marketPageSize
	if end > count {
		end = count
	}
	return start, end, index, pages
}

func (s *Session) marketItem(id string) *engine.ItemDefinition {
	for i := range s.catalogue.Items {
		if s.catalogue.Items[i].ID == id {
			return &s.catalogue.Items[i]
		}
	}
	return nil
}

func (s *Session) handleMarketKey(key string, state *engine.GameState) error {
	if key == "b" {
		s.page = pageMain
		return nil
	}
	if key == "t" {
		if s.tradeSide == engine.TradeSell {
			s.tradeSide = engine.TradeBuy
		} else {
			s.tradeSide = engine.TradeSell
		}
		s.pageIndex = 0
		return nil
	}
	offers := s.marketOffers(state)
	start, end, current, pages := marketPageBounds(s.pageIndex, len(offers))
	if key == "n" && current+1 < pages {
		s.pageIndex++
		return nil
	}
	if key == "p" && current > 0 {
		s.pageIndex--
		return nil
	}
	for i, offer := range offers[start:end] {
		if key != strconv.Itoa(i+1) {
			continue
		}
		quote, err := s.engine.PreviewTrade(s.tradeSide, offer.ItemID, 1)
		if err != nil {
			s.notifyTradeError(err)
			return nil
		}
		s.tradeQuote = &quote
		s.page = pageTradeConfirm
		return nil
	}
	s.unknownKey()
	return nil
}

func (s *Session) tradeConfirmModel(m panel.Model) panel.Model {
	q := s.tradeQuote
	if q == nil {
		m.Title = "交易预览已取消"
		m.Options = []panel.Option{{Key: "b", Label: "返回坊市", CommandKind: "LOCAL_PAGE"},
			{Key: "q", Label: "退出", CommandKind: "QUIT"}}
		return m
	}
	name := q.ItemID
	if item := s.marketItem(q.ItemID); item != nil {
		name = item.NameZH
	}
	action := "买入"
	cost := fmt.Sprintf("花费%d灵石；交易后余额%d", q.Total, q.BalanceAfter)
	if q.Side == engine.TradeSell {
		action = "卖出"
		cost = fmt.Sprintf("交出%s×%d；获得%d灵石；交易后余额%d", name, q.Quantity, q.Total, q.BalanceAfter)
	}
	m.Title = "确认坊市交易"
	m.Scene = "坊市交易不会推进游戏月份。"
	m.Confirmation = &panel.ConfirmationBlock{Token: q.Token, Title: action + name,
		Summary:  fmt.Sprintf("数量%d · 单价%d灵石 · 总价%d灵石；坊市库存%d→%d", q.Quantity, q.UnitPrice, q.Total, q.StockBefore, q.StockAfter),
		CostText: cost, ConfirmLabel: "确认交易", CancelLabel: "取消", ExpiresAtRevision: q.Revision}
	m.Options = []panel.Option{
		{Key: "+", Label: "增加数量", CommandKind: "LOCAL_PAGE"},
		{Key: "-", Label: "减少数量", CommandKind: "LOCAL_PAGE", Disabled: q.Quantity == 1},
		{Key: "c", Label: "确认交易", CommandKind: string(engine.KindTrade), TargetID: q.ItemID, CostText: cost},
		{Key: "b", Label: "取消返回坊市", CommandKind: "LOCAL_PAGE"},
		{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"},
	}
	return m
}

func (s *Session) handleTradeConfirmKey(key string) error {
	q := s.tradeQuote
	if q == nil {
		s.page = pageMarket
		return nil
	}
	if key == "b" {
		s.engine.CancelTradeQuote()
		s.tradeQuote = nil
		s.page = pageMarket
		return nil
	}
	if key == "+" || key == "-" {
		quantity := q.Quantity + 1
		if key == "-" {
			quantity = q.Quantity - 1
		}
		if quantity < 1 {
			s.Notify("交易数量至少为1。")
			return nil
		}
		updated, err := s.engine.PreviewTrade(q.Side, q.ItemID, quantity)
		if err != nil {
			s.notifyTradeError(err)
			return nil
		}
		s.tradeQuote = &updated
		return nil
	}
	if key == "c" {
		oldRevision := s.engine.State().Revision
		if err := s.submitWithToken(engine.KindTrade,
			engine.Payload{TradeSide: q.Side, ItemID: q.ItemID, Quantity: q.Quantity}, q.ItemID, q.Token); err != nil {
			return err
		}
		if s.saveFailed {
			return nil
		}
		if s.engine.State().Revision != oldRevision {
			s.tradeQuote = nil
			s.page = pageMarket
			s.notice = &panel.Notice{Kind: "info", Text: "买卖已结算并保存；背包、灵石和坊市库存同时更新。"}
		}
		return nil
	}
	s.unknownKey()
	return nil
}

func (s *Session) notifyTradeError(err error) {
	var tradeErr engine.TradeError
	text := "交易条件未满足；数量、库存、余额或背包容量不够。"
	if errors.As(err, &tradeErr) {
		switch tradeErr.Code {
		case engine.ErrInsufficientFunds:
			text = "灵石不足；交易未执行，也不会自动借贷。"
		case engine.ErrQuantityNotHeld:
			text = "随身没有足够数量的物品可出售。"
		case engine.ErrUnknownItem:
			text = "坊市没有这件商品。"
		case engine.ErrBadPhase:
			text = "当前待决状态不能交易。"
		}
	}
	s.Notify(text)
}
