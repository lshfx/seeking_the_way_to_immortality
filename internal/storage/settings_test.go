package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsPersistIndependentlyAndCorruptionUsesDefaults(t *testing.T) {
	layout, err := EnsureLayoutFor(LayoutFor(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	characterFile := filepath.Join(layout.CharDir("main"), "save.json")
	if err := os.MkdirAll(filepath.Dir(characterFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(characterFile, []byte("sentinel-save"), 0o600); err != nil {
		t.Fatal(err)
	}

	defaults, warning, err := LoadSettings(layout)
	if err != nil || warning != "" {
		t.Fatalf("first-run settings = %#v, warning=%q, err=%v", defaults, warning, err)
	}
	if defaults != DefaultSettings() {
		t.Fatalf("first-run settings = %#v, want defaults %#v", defaults, DefaultSettings())
	}
	changed := Settings{Version: SettingsVersion, Theme: "paper", ColorMode: "none", Silent: false}
	if err := SaveSettings(layout, changed); err != nil {
		t.Fatal(err)
	}
	loaded, warning, err := LoadSettings(layout)
	if err != nil || warning != "" || loaded != changed {
		t.Fatalf("reloaded settings = %#v, warning=%q, err=%v", loaded, warning, err)
	}

	if err := os.WriteFile(layout.SettingsPath(), []byte("{ broken json"), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback, warning, err := LoadSettings(layout)
	if err != nil || warning == "" || fallback != DefaultSettings() {
		t.Fatalf("corrupt settings fallback = %#v, warning=%q, err=%v", fallback, warning, err)
	}
	gotSave, err := os.ReadFile(characterFile)
	if err != nil || string(gotSave) != "sentinel-save" {
		t.Fatalf("loading corrupt settings touched the character save: %q, err=%v", gotSave, err)
	}
}

func TestSettingsRefuseRelativeRoot(t *testing.T) {
	if _, _, err := LoadSettings(LayoutFor("relative-data")); err == nil {
		t.Fatal("settings must not fall back to a relative or current-directory path")
	}
}
