package engine

// Game time.
//
// The design rule (11.5.2) is that real time never maps to game time. Sleeping
// the process, changing the system clock, or leaving the window for a week must
// all leave the world exactly where it was. The only way a month passes is that a
// command with a month cost settles.
//
// Consequently nothing in this file reads the clock. Time is a pure function of
// the stored counter.

// Calendar constants. The game opens in 天玄历 387 年 一月.
const (
	// EpochYear is the opening in-world year.
	EpochYear = 387
	// MonthsPerYear is fixed: the calendar has no leap month, which keeps age
	// arithmetic exact and avoids an entire class of drift.
	MonthsPerYear = 12
)

// Season is the quarter label shown on the panel.
type Season string

// Seasons, matching the design's 1-3 / 4-6 / 7-9 / 10-12 grouping.
const (
	SeasonSpring Season = "spring" // 春 1-3
	SeasonSummer Season = "summer" // 夏 4-6
	SeasonAutumn Season = "autumn" // 秋 7-9
	SeasonWinter Season = "winter" // 冬 10-12
)

// Chinese labels for presentation. Kept beside the identity so a renderer never
// invents its own wording.
var seasonZH = map[Season]string{
	SeasonSpring: "春",
	SeasonSummer: "夏",
	SeasonAutumn: "秋",
	SeasonWinter: "冬",
}

// LabelZH returns the single-character Chinese label.
func (s Season) LabelZH() string {
	if v, ok := seasonZH[s]; ok {
		return v
	}
	return "?"
}

// Valid reports whether s is a known season.
func (s Season) Valid() bool {
	_, ok := seasonZH[s]
	return ok
}

// SeasonOf returns the season for a zero-based month index (0 = 一月).
//
// A negative index is clamped to winter's first month rather than wrapping,
// because a negative month can only arise from corrupt state and wrapping would
// disguise the corruption as a plausible season.
func SeasonOf(monthIndex int64) Season {
	if monthIndex < 0 {
		return SeasonWinter
	}
	switch (monthIndex % MonthsPerYear) / 3 {
	case 0:
		return SeasonSpring
	case 1:
		return SeasonSummer
	case 2:
		return SeasonAutumn
	default:
		return SeasonWinter
	}
}

// Date is a resolved in-world date. It is computed, never stored, so there is
// exactly one representation on disk (the month counter) and no chance of the
// two drifting apart.
type Date struct {
	// Year is the in-world year.
	Year int
	// Month is 1-12.
	Month int
	// MonthIndex is the zero-based count of elapsed months since the epoch. It
	// is the authoritative value; Year and Month are derived for display.
	MonthIndex int64
	// Season is derived from MonthIndex.
	Season Season
}

// DateFromMonths resolves an elapsed-month counter into a displayable date.
//
// Elapsed months are counted from the first month of EpochYear, so counter 0 is
// 387 年 一月.
func DateFromMonths(elapsed int64) Date {
	// Floor division: a negative or corrupt counter must not produce a
	// nonsensical month such as 0 or 13.
	if elapsed < 0 {
		return Date{
			Year:       EpochYear,
			Month:      1,
			MonthIndex: elapsed,
			Season:     SeasonOf(0),
		}
	}

	yearOffset := elapsed / MonthsPerYear
	monthIndex := elapsed % MonthsPerYear

	return Date{
		Year:       EpochYear + int(yearOffset),
		Month:      int(monthIndex) + 1,
		MonthIndex: elapsed,
		Season:     SeasonOf(monthIndex),
	}
}

// Calendar is the clock as the engine sees it: a pair of counters plus the
// derived date.
type Calendar struct {
	// WorldMonth is the number of completed, settled world months.
	WorldMonth int64
	// CombatRound is the current battle's round, or 0 outside battle.
	CombatRound int64
	// Date is the resolved in-world date for WorldMonth.
	Date Date
}

// CalendarFor builds the calendar view from the persisted counters.
func CalendarFor(s *GameState) Calendar {
	if s == nil {
		return Calendar{}
	}
	return Calendar{
		WorldMonth:  s.Counters.WorldMonth,
		CombatRound: s.Counters.CombatRound,
		Date:        DateFromMonths(s.Counters.WorldMonth),
	}
}

// AgeInYears renders an age held in months as whole years, truncating.
//
// Truncation rather than rounding is deliberate: a character aged 215 months is
// 17, not 18, and an off-by-one at the lifespan boundary would either kill a
// character a month early or let them live past their ceiling.
func AgeInYears(months int64) int {
	if months < 0 {
		return 0
	}
	return int(months / MonthsPerYear)
}

// AgeRemainderMonths returns the leftover months after whole years, for the
// panel's "17 岁 11 月" style display.
func AgeRemainderMonths(months int64) int {
	if months < 0 {
		return 0
	}
	return int(months % MonthsPerYear)
}

// LifespanMonths returns the total lifespan ceiling in months, base plus every
// granted bonus.
//
// Summing here rather than tracking a running total means a bonus can never be
// double-counted by a retried command: the ledger is the single source.
func LifespanMonths(l LifespanLedger) int64 {
	total := int64(l.BaseYears) * MonthsPerYear
	for _, b := range l.Bonuses {
		total += int64(b.Years) * MonthsPerYear
	}
	return total
}

// LifespanExhausted reports whether age has reached or passed the ceiling.
//
// It is inclusive: reaching the ceiling exactly is death, so a character whose
// lifespan is 960 months dies in month 960, not 961.
func LifespanExhausted(ageMonths int64, l LifespanLedger) bool {
	return ageMonths >= LifespanMonths(l)
}

// AdvanceWorldMonth adds one settled month: the world date moves forward and the
// character ages by exactly one month.
//
// Aging is colocated with the month advance on purpose. If a caller could
// advance the month without aging, a lifespan bug would be silent; if it could
// age without advancing, the panel would disagree with the record.
//
// It returns the new world month so callers can log it.
func AdvanceWorldMonth(s *GameState) int64 {
	if s == nil {
		return 0
	}
	s.Counters.WorldMonth++
	if s.Player != nil {
		s.Player.AgeMonths++
	}
	return s.Counters.WorldMonth
}

// AdvanceCombatRound adds one combat round. It deliberately does not touch the
// world month or the character's age: a battle is not the passage of time, and
// the design states that a month is charged once by the parent action.
func AdvanceCombatRound(s *GameState) int64 {
	if s == nil {
		return 0
	}
	s.Counters.CombatRound++
	return s.Counters.CombatRound
}

// ResetCombatRound clears the battle round counter when a battle ends.
func ResetCombatRound(s *GameState) {
	if s == nil {
		return
	}
	s.Counters.CombatRound = 0
}

// AdvanceInteractionSeq records one committed domain action.
//
// Callers must invoke this only for actions that actually changed domain state.
// Queries, empty input and duplicate submits must leave it alone, which is why
// it is a separate function rather than something the pipeline does
// unconditionally.
func AdvanceInteractionSeq(s *GameState) int64 {
	if s == nil {
		return 0
	}
	s.Counters.InteractionSeq++
	return s.Counters.InteractionSeq
}
