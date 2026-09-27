package session

import (
	"strings"
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage"
)

func TestQuickCreationChoosesNameAndGenderWithoutTyping(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if first := app.Model(); !strings.Contains(first.Options[0].Label, "沈云舟") {
		t.Fatalf("preset should offer a usable identity: %#v", first.Options)
	}
	if _, err := app.HandleKey("2"); err != nil {
		t.Fatal(err)
	}
	draft := app.State().Pending.Creation
	initialBase, ok := draft.FixedAllocation()
	if !ok || draft.Identity.Surname+draft.Identity.GivenName != "陆守拙" {
		t.Fatalf("preset did not apply the suggested identity: %#v", draft)
	}
	confirm := app.Model()
	if !strings.Contains(confirm.Scene, "陆守拙") || !strings.Contains(confirm.Scene, "未指定") ||
		!creationOptionExists(confirm, "n") || !creationOptionExists(confirm, "g") {
		t.Fatalf("confirmation should make name and gender choices obvious: %#v", confirm)
	}
	revision := app.State().Revision
	if _, err := app.HandleKey("n"); err != nil {
		t.Fatal(err)
	}
	if app.State().Revision != revision || app.Model().Title != "选择姓名" || !creationOptionExists(app.Model(), "4") {
		t.Fatal("opening the name menu should be local and show ready-made names")
	}
	if _, err := app.HandleKey("4"); err != nil {
		t.Fatal(err)
	}
	if got := app.State().Pending.Creation.Identity; got.Surname+got.GivenName != "苏清禾" || app.page != pageMain {
		t.Fatalf("suggested name was not saved and returned to confirmation: %#v", got)
	}
	if _, err := app.HandleKey("g"); err != nil {
		t.Fatal(err)
	}
	if app.Model().Title != "选择性别" {
		t.Fatal("gender menu did not open")
	}
	if _, err := app.HandleKey("2"); err != nil {
		t.Fatal(err)
	}
	draft = app.State().Pending.Creation
	finalBase, ok := draft.FixedAllocation()
	if !ok || finalBase != initialBase || draft.Identity.Gender != engine.GenderFemale || app.State().Counters.WorldMonth != 0 {
		t.Fatalf("identity choices changed game mechanics or time: %#v", draft)
	}
	if !strings.Contains(app.Model().Scene, "女") {
		t.Fatal("the confirmation screen should show the selected gender in Chinese")
	}
	if _, err := app.HandleKey("c"); err != nil {
		t.Fatal(err)
	}
	player := app.State().Player
	if player == nil || player.Identity.Surname+player.Identity.GivenName != "苏清禾" || player.Identity.Gender != engine.GenderFemale {
		t.Fatalf("confirmation lost the chosen identity: %#v", player)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got := restored.State().Player.Identity; got.Surname+got.GivenName != "苏清禾" || got.Gender != engine.GenderFemale {
		t.Fatalf("name or gender was not restored: %#v", got)
	}
}

func TestCustomCreationNameIsOneLineAndGenderStaysVerbatim(t *testing.T) {
	app, err := OpenAt(storage.LayoutFor(t.TempDir()), DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, key := range []string{"1", "n", "5"} {
		if _, err := app.HandleKey(key); err != nil {
			t.Fatal(err)
		}
	}
	if !app.TextInputActive() || app.Model().Title != "自定义姓名" {
		t.Fatal("custom name should start a single full-name input")
	}
	if err := app.HandleText("温"); err != nil {
		t.Fatal(err)
	}
	if !app.TextInputActive() || app.State().Pending.Creation.Identity.Surname != "沈" {
		t.Fatal("invalid short name should leave the saved draft unchanged")
	}
	if err := app.HandleText("欧阳修\r\n"); err != nil {
		t.Fatal(err)
	}
	if app.TextInputActive() || app.page != pageMain {
		t.Fatal("valid full name should save and return to confirmation")
	}
	if got := app.State().Pending.Creation.Identity; got.Surname != "欧阳" || got.GivenName != "修" {
		t.Fatalf("full name was not split for the existing save format: %#v", got)
	}
	for _, key := range []string{"g", "4"} {
		if _, err := app.HandleKey(key); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.HandleText("无性别/自定"); err != nil {
		t.Fatal(err)
	}
	if got := app.State().Pending.Creation.Identity.Gender; got != "无性别/自定" {
		t.Fatalf("custom gender was rewritten: %q", got)
	}
	if _, err := app.HandleKey("c"); err != nil {
		t.Fatal(err)
	}
	if got := app.State().Player.Identity.Gender; got != "无性别/自定" {
		t.Fatalf("confirmation lost custom gender: %q", got)
	}
}

func creationOptionExists(model panel.Model, key string) bool {
	for _, option := range model.Options {
		if option.Key == key {
			return true
		}
	}
	return false
}
