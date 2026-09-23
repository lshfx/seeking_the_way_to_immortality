package engine

import "testing"

// TestEpochIsYear387MonthOne pins the opening date from the design.
func TestEpochIsYear387MonthOne(t *testing.T) {
	d := DateFromMonths(0)

	if d.Year != 387 {
		t.Fatalf("epoch year = %d, want 387", d.Year)
	}
	if d.Month != 1 {
		t.Fatalf("epoch month = %d, want 1", d.Month)
	}
	if d.Season != SeasonSpring {
		t.Fatalf("epoch season = %q, want spring", d.Season)
	}
}

// TestDateRollsOverYearBoundary checks the month-to-year carry, which is the
// arithmetic most likely to be off by one.
func TestDateRollsOverYearBoundary(t *testing.T) {
	cases := []struct {
		elapsed int64
		year    int
		month   int
		season  Season
	}{
		{0, 387, 1, SeasonSpring},
		{1, 387, 2, SeasonSpring},
		{2, 387, 3, SeasonSpring},
		{3, 387, 4, SeasonSummer},
		{5, 387, 6, SeasonSummer},
		{6, 387, 7, SeasonAutumn},
		{8, 387, 9, SeasonAutumn},
		{9, 387, 10, SeasonWinter},
		{11, 387, 12, SeasonWinter},
		{12, 388, 1, SeasonSpring},
		{13, 388, 2, SeasonSpring},
		{23, 388, 12, SeasonWinter},
		{24, 389, 1, SeasonSpring},
		{12 * 10, 397, 1, SeasonSpring},
	}

	for _, c := range cases {
		d := DateFromMonths(c.elapsed)
		if d.Year != c.year || d.Month != c.month || d.Season != c.season {
			t.Errorf("DateFromMonths(%d) = %d年%d月 %s, want %d年%d月 %s",
				c.elapsed, d.Year, d.Month, d.Season, c.year, c.month, c.season)
		}
	}
}

// TestSeasonGroupingIsByQuarter verifies the 1-3/4-6/7-9/10-12 grouping holds
// for every month of a year, rather than only for the sampled cases above.
func TestSeasonGroupingIsByQuarter(t *testing.T) {
	want := []Season{
		SeasonSpring, SeasonSpring, SeasonSpring,
		SeasonSummer, SeasonSummer, SeasonSummer,
		SeasonAutumn, SeasonAutumn, SeasonAutumn,
		SeasonWinter, SeasonWinter, SeasonWinter,
	}

	for i := 0; i < 12; i++ {
		if got := SeasonOf(int64(i)); got != want[i] {
			t.Errorf("SeasonOf(%d) = %q, want %q", i, got, want[i])
		}
	}

	// The pattern must repeat in the following year too, not just in year one.
	for i := 0; i < 12; i++ {
		if got := SeasonOf(int64(i + 12)); got != want[i] {
			t.Errorf("SeasonOf(%d) = %q, want %q", i+12, got, want[i])
		}
	}
}

// TestNegativeCountsDoNotProduceAbsurdDates pins the corrupt-state behaviour. A
// negative month counter must not render as "387 年 0 月" or a wrapped year,
// which would look like a legitimate date and hide the corruption.
func TestNegativeCountsDoNotProduceAbsurdDates(t *testing.T) {
	for _, elapsed := range []int64{-1, -12, -1000} {
		d := DateFromMonths(elapsed)

		if d.Month < 1 || d.Month > 12 {
			t.Errorf("DateFromMonths(%d) produced month %d, outside 1-12", elapsed, d.Month)
		}
		if !d.Season.Valid() {
			t.Errorf("DateFromMonths(%d) produced invalid season %q", elapsed, d.Season)
		}
		if d.Year != EpochYear {
			t.Errorf("DateFromMonths(%d) moved the year to %d", elapsed, d.Year)
		}
		// The raw counter is still reported so the corruption stays visible.
		if d.MonthIndex != elapsed {
			t.Errorf("DateFromMonths(%d) obscured the counter as %d", elapsed, d.MonthIndex)
		}
	}
}

// TestSeasonLabels covers the presentation strings.
func TestSeasonLabels(t *testing.T) {
	want := map[Season]string{
		SeasonSpring: "春",
		SeasonSummer: "夏",
		SeasonAutumn: "秋",
		SeasonWinter: "冬",
	}

	for s, label := range want {
		if got := s.LabelZH(); got != label {
			t.Errorf("%s.LabelZH() = %q, want %q", s, got, label)
		}
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}

	if Season("monsoon").Valid() {
		t.Error("an invented season must not validate")
	}
	if got := Season("monsoon").LabelZH(); got != "?" {
		t.Errorf("unknown season label = %q, want ?", got)
	}
}

// TestAdvanceWorldMonthAgesExactlyOnce is the core coupling guarantee: one
// settled month means one month older, and the two can never diverge.
func TestAdvanceWorldMonthAgesExactlyOnce(t *testing.T) {
	s := &GameState{Player: &Player{AgeMonths: 200}}

	for i := int64(1); i <= 25; i++ {
		got := AdvanceWorldMonth(s)

		if got != i {
			t.Fatalf("AdvanceWorldMonth returned %d, want %d", got, i)
		}
		if s.Counters.WorldMonth != i {
			t.Fatalf("world month = %d, want %d", s.Counters.WorldMonth, i)
		}
		if s.Player.AgeMonths != 200+i {
			t.Fatalf("age = %d, want %d", s.Player.AgeMonths, 200+i)
		}
	}
}

// TestAdvanceWorldMonthToleratesNilPlayer covers the CREATION phase, where the
// month may need to move before a character exists. It must not panic.
func TestAdvanceWorldMonthToleratesNilPlayer(t *testing.T) {
	s := &GameState{}

	if got := AdvanceWorldMonth(s); got != 1 {
		t.Fatalf("AdvanceWorldMonth with no player = %d, want 1", got)
	}
	for i := 0; i < 5; i++ {
		AdvanceWorldMonth(s)
	}
	if s.Counters.WorldMonth != 6 {
		t.Fatalf("world month = %d, want 6", s.Counters.WorldMonth)
	}
}

// TestCombatRoundDoesNotAdvanceWorldOrAge is the rule that a battle is not the
// passage of time. Without this, a long fight would age the character.
func TestCombatRoundDoesNotAdvanceWorldOrAge(t *testing.T) {
	s := &GameState{Player: &Player{AgeMonths: 300}}

	for i := int64(1); i <= 30; i++ {
		if got := AdvanceCombatRound(s); got != i {
			t.Fatalf("AdvanceCombatRound returned %d, want %d", got, i)
		}
	}

	if s.Counters.WorldMonth != 0 {
		t.Fatalf("combat advanced the world month to %d, want 0", s.Counters.WorldMonth)
	}
	if s.Player.AgeMonths != 300 {
		t.Fatalf("combat aged the character to %d, want 300", s.Player.AgeMonths)
	}
}

// TestResetCombatRound covers the battle-end reset.
func TestResetCombatRound(t *testing.T) {
	s := &GameState{}
	for i := 0; i < 5; i++ {
		AdvanceCombatRound(s)
	}
	ResetCombatRound(s)

	if s.Counters.CombatRound != 0 {
		t.Fatalf("combat round = %d after reset, want 0", s.Counters.CombatRound)
	}
}

// TestInteractionSeqIsSeparateFromWorldMonth documents that the two counters
// measure different things. A zero-month action still counts as an interaction.
func TestInteractionSeqIsSeparateFromWorldMonth(t *testing.T) {
	s := &GameState{Player: &Player{}}

	AdvanceInteractionSeq(s) // e.g. a trade
	AdvanceInteractionSeq(s) // e.g. accepting a quest
	AdvanceInteractionSeq(s)

	if s.Counters.InteractionSeq != 3 {
		t.Fatalf("interaction seq = %d, want 3", s.Counters.InteractionSeq)
	}
	if s.Counters.WorldMonth != 0 {
		t.Fatalf("zero-month actions advanced the world to %d", s.Counters.WorldMonth)
	}
	if s.Player.AgeMonths != 0 {
		t.Fatalf("zero-month actions aged the character to %d", s.Player.AgeMonths)
	}
}

// TestAgeRenderingTruncates pins the boundary behaviour that matters for
// lifespan: 215 months is 17 years old, not 18.
func TestAgeRenderingTruncates(t *testing.T) {
	cases := []struct {
		months    int64
		years     int
		remainder int
	}{
		{0, 0, 0},
		{1, 0, 1},
		{11, 0, 11},
		{12, 1, 0},
		{13, 1, 1},
		{215, 17, 11},
		{216, 18, 0},
		{959, 79, 11},
		{960, 80, 0},
	}

	for _, c := range cases {
		if got := AgeInYears(c.months); got != c.years {
			t.Errorf("AgeInYears(%d) = %d, want %d", c.months, got, c.years)
		}
		if got := AgeRemainderMonths(c.months); got != c.remainder {
			t.Errorf("AgeRemainderMonths(%d) = %d, want %d", c.months, got, c.remainder)
		}
	}
}

// TestNegativeAgeRendersAsZero covers corrupt input.
func TestNegativeAgeRendersAsZero(t *testing.T) {
	if got := AgeInYears(-5); got != 0 {
		t.Fatalf("AgeInYears(-5) = %d, want 0", got)
	}
	if got := AgeRemainderMonths(-5); got != 0 {
		t.Fatalf("AgeRemainderMonths(-5) = %d, want 0", got)
	}
}

// TestLifespanSumsBaseAndBonuses verifies the ledger is summed rather than
// stored, so a bonus can never be applied twice by a retried command.
func TestLifespanSumsBaseAndBonuses(t *testing.T) {
	l := LifespanLedger{
		BaseYears: 80,
		Bonuses: []LifespanBonus{
			{ID: "breakthrough-1", Years: 20, Reason: "筑基"},
			{ID: "pill-1", Years: 5, Reason: "延寿丹"},
		},
	}

	if got := LifespanMonths(l); got != int64(105*12) {
		t.Fatalf("LifespanMonths = %d, want %d", got, 105*12)
	}
}

// TestLifespanWithNoBonuses is the base case.
func TestLifespanWithNoBonuses(t *testing.T) {
	if got := LifespanMonths(LifespanLedger{BaseYears: 80}); got != 960 {
		t.Fatalf("LifespanMonths = %d, want 960", got)
	}
}

// TestLifespanExhaustedIsInclusive is the boundary that decides life or death.
// Reaching the ceiling exactly is death; being one month short is not.
func TestLifespanExhaustedIsInclusive(t *testing.T) {
	l := LifespanLedger{BaseYears: 80} // 960 months

	if LifespanExhausted(959, l) {
		t.Error("959 months with a 960-month ceiling must still be alive")
	}
	if !LifespanExhausted(960, l) {
		t.Error("960 months with a 960-month ceiling must be exhausted")
	}
	if !LifespanExhausted(961, l) {
		t.Error("961 months must be exhausted")
	}
}

// TestLifespanBonusExtendsTheBoundary checks that a bonus actually moves the
// death boundary, rather than merely being recorded.
func TestLifespanBonusExtendsTheBoundary(t *testing.T) {
	base := LifespanLedger{BaseYears: 80}
	extended := LifespanLedger{
		BaseYears: 80,
		Bonuses:   []LifespanBonus{{ID: "b1", Years: 10}},
	}

	if LifespanExhausted(960, extended) {
		t.Error("a +10 year bonus must keep the character alive at 960 months")
	}
	if !LifespanExhausted(1080, extended) {
		t.Error("the extended ceiling of 1080 months must be exhausted")
	}
	if !LifespanExhausted(960, base) {
		t.Error("the unextended ceiling must still be exhausted at 960")
	}
}

// TestCalendarForReadsCountersOnly states the "no wall clock" rule structurally:
// the calendar is a pure function of stored counters.
func TestCalendarForReadsCountersOnly(t *testing.T) {
	s := &GameState{
		Counters: Counters{WorldMonth: 18, CombatRound: 4},
	}

	c := CalendarFor(s)

	if c.WorldMonth != 18 || c.CombatRound != 4 {
		t.Fatalf("calendar counters = %d/%d, want 18/4", c.WorldMonth, c.CombatRound)
	}
	// 18 elapsed months from 387-01 is 388-07.
	if c.Date.Year != 388 || c.Date.Month != 7 {
		t.Fatalf("calendar date = %d年%d月, want 388年7月", c.Date.Year, c.Date.Month)
	}
	if c.Date.Season != SeasonAutumn {
		t.Fatalf("calendar season = %q, want autumn", c.Date.Season)
	}
}

// TestCalendarForNilState covers the defensive path.
func TestCalendarForNilState(t *testing.T) {
	if got := CalendarFor(nil); got.WorldMonth != 0 || got.Date.Year != 0 {
		t.Fatalf("CalendarFor(nil) = %+v, want zero value", got)
	}
}

// TestNoTimeImportsInClock is a local documentation guard. The package-wide
// import-graph check in boundary_test.go is the real enforcement; this asserts
// the clock's central promise in a readable place.
//
// Concretely: if someone added a `time.Now()` seed or "elapsed real days"
// helper to this file, the behaviour would still compile and most tests would
// still pass, but replay would break. The package boundary test catches the
// import; this test names why it matters.
func TestNoTimeImportsInClock(t *testing.T) {
	// Two games advanced by the same number of settled months must report the
	// same date regardless of when the test runs.
	a := &GameState{Player: &Player{}}
	b := &GameState{Player: &Player{}}

	for i := 0; i < 40; i++ {
		AdvanceWorldMonth(a)
		AdvanceWorldMonth(b)
	}

	if a.Counters.WorldMonth != b.Counters.WorldMonth {
		t.Fatal("identical month advances diverged: the clock is not deterministic")
	}
	if a.Player.AgeMonths != b.Player.AgeMonths {
		t.Fatal("identical month advances produced different ages")
	}
}
