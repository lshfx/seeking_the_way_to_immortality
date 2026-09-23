package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SettingsVersion is independent of the character save schema so a broken
// preference file can never make player progress unreadable.
const SettingsVersion = 1

// Settings contains presentation preferences only. It never participates in
// the game state or save integrity digest.
type Settings struct {
	Version   int    `json:"version"`
	Theme     string `json:"theme"`
	ColorMode string `json:"color_mode"`
	Silent    bool   `json:"silent"`
}

// DefaultSettings returns the safe first-run preferences.
func DefaultSettings() Settings {
	return Settings{Version: SettingsVersion, Theme: "jade", ColorMode: "auto", Silent: true}
}

// LoadSettings loads presentation preferences. A missing or malformed file
// falls back to defaults and reports why; it never modifies character saves.
func LoadSettings(layout Layout) (Settings, string, error) {
	defaults := DefaultSettings()
	if strings.TrimSpace(layout.Root) == "" || !filepath.IsAbs(layout.Root) {
		return defaults, "", &DataRootError{Path: layout.Root, Reason: "settings require an absolute per-user data root"}
	}
	data, err := os.ReadFile(layout.SettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return defaults, "", nil
	}
	if err != nil {
		return defaults, "", err
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return defaults, "设置文件无法读取，已使用安全默认值；角色存档未受影响。", nil
	}
	if settings.Version != SettingsVersion || !settings.valid() {
		return defaults, "设置版本或选项无效，已使用安全默认值；角色存档未受影响。", nil
	}
	return settings, "", nil
}

// SaveSettings atomically writes preferences outside the character directory.
func SaveSettings(layout Layout, settings Settings) error {
	if strings.TrimSpace(layout.Root) == "" || !filepath.IsAbs(layout.Root) {
		return &DataRootError{Path: layout.Root, Reason: "settings require an absolute per-user data root"}
	}
	if settings.Version == 0 {
		settings.Version = SettingsVersion
	}
	if !settings.valid() {
		return errors.New("invalid presentation settings")
	}
	if _, err := EnsureLayoutFor(layout); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = atomicWriteFile(layout.SettingsPath(), data, 0o600, SyncFull)
	return err
}

func (s Settings) valid() bool {
	if s.Version != SettingsVersion {
		return false
	}
	if s.Theme != "jade" && s.Theme != "paper" {
		return false
	}
	switch s.ColorMode {
	case "auto", "basic", "none":
		return true
	default:
		return false
	}
}
