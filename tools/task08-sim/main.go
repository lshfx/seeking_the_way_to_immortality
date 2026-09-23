package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/content"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

type simulation struct {
	Profile             string `json:"profile"`
	Aptitude            int    `json:"aptitude"`
	BasePoints          int    `json:"base_points"`
	MonthlyRateSubunits int64  `json:"monthly_rate_subunits"`
	MonthlyRate         string `json:"monthly_rate"`
	ThresholdSubunits   int64  `json:"threshold_subunits"`
	ActionsToThreshold  int64  `json:"actions_to_threshold"`
	FinalActionGain     string `json:"final_action_gain"`
	AtThreshold         bool   `json:"at_threshold"`
	AutoBreakthrough    bool   `json:"auto_breakthrough"`
}

func main() {
	catalogue := content.M1()
	profiles := []struct {
		name string
		base engine.BaseAttributes
	}{
		{"low", engine.BaseAttributes{Strength: 11, Agility: 11, Constitution: 11, Comprehension: 11, Aptitude: 5, Fortune: 11}},
		{"middle", engine.BaseAttributes{Strength: 10, Agility: 10, Constitution: 10, Comprehension: 10, Aptitude: 10, Fortune: 10}},
		{"high", engine.BaseAttributes{Strength: 9, Agility: 9, Constitution: 9, Comprehension: 9, Aptitude: 15, Fortune: 9}},
	}

	results := make([]simulation, 0, len(profiles))
	for _, profile := range profiles {
		result, err := run(profile.name, profile.base, &catalogue)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		results = append(results, result)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(name string, base engine.BaseAttributes, catalogue *engine.Catalogue) (simulation, error) {
	selection := engine.CreationSelection{
		Surname: "模拟", GivenName: name, Gender: engine.GenderUnspecified,
		AgeYears: 21, Origin: engine.OriginCommoner, Path: engine.PathHuman,
		SpiritRoot: engine.RootTrue, Constitution: "ordinary",
		Strength: base.Strength, Agility: base.Agility,
		ConstitutionAttr: base.Constitution, Comprehension: base.Comprehension,
		Aptitude: base.Aptitude, Fortune: base.Fortune,
	}
	draft := engine.NewCreationDraft("task08-"+name, "main")
	draft.ApplySelection(selection)
	draft.FixAllocation(base)
	player, report := engine.CreatePlayer(catalogue, draft)
	if !report.OK() {
		return simulation{}, fmt.Errorf("%s", report.Error())
	}
	world := &engine.World{CurrentLocation: "cave_dwelling"}
	preview, err := engine.PreviewCultivation(player, world, catalogue, engine.ActionNormal)
	if err != nil {
		return simulation{}, err
	}
	threshold := preview.Threshold
	months := int64(0)
	lastGain := int64(0)
	for player.XP < threshold {
		preview, err = engine.PreviewCultivation(player, world, catalogue, engine.ActionNormal)
		if err != nil {
			return simulation{}, err
		}
		lastGain = preview.NextGain
		player.XP = preview.XPAfterNextAction
		months++
	}
	return simulation{
		Profile: name, Aptitude: player.Attributes.Aptitude, BasePoints: base.Total(),
		MonthlyRateSubunits: preview.Rate, MonthlyRate: formatScale(preview.Rate),
		ThresholdSubunits: threshold, ActionsToThreshold: months,
		FinalActionGain: formatScale(lastGain), AtThreshold: player.XP == threshold,
		AutoBreakthrough: false,
	}, nil
}

func formatScale(value int64) string {
	whole := value / engine.SCALE
	fraction := value % engine.SCALE
	if fraction == 0 {
		return fmt.Sprintf("%d", whole)
	}
	fractionText := fmt.Sprintf("%04d", fraction)
	fractionText = strings.TrimRight(fractionText, "0")
	return fmt.Sprintf("%s.%s", fmt.Sprint(whole), fractionText)
}
