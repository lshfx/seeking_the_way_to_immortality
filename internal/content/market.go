package content

import "github.com/lshfx/seeking_the_way_to_immortality/internal/engine"

// The M1 market has fixed stock until a trade changes it. The quantities are
// trial balance values, not numbers from the source manuscript.
func m1MarketOffers() []engine.MarketOfferDefinition {
	return []engine.MarketOfferDefinition{
		{ItemID: "qi_gathering_pill", InitialStock: designNote(30, "M1坊市初始库存候选")},
		{ItemID: "foundation_pill", InitialStock: designNote(8, "M1坊市初始库存候选")},
		{ItemID: "healing_pill", InitialStock: designNote(20, "M1坊市初始库存候选")},
		{ItemID: "spirit_herb", InitialStock: designNote(30, "M1坊市初始库存候选")},
		{ItemID: "rare_herb", InitialStock: designNote(8, "M1坊市初始库存候选")},
		{ItemID: "basic_sword", InitialStock: designNote(3, "M1坊市初始库存候选")},
		{ItemID: "cloth_robe", InitialStock: designNote(3, "M1坊市初始库存候选")},
		{ItemID: "seal_talisman", InitialStock: designNote(5, "M1坊市初始库存候选")},
	}
}
