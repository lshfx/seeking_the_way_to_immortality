//go:build !windows

package storage

import (
	"os"
	"path/filepath"
)

// platformLocalAppData returns the per-user local application data directory.
//
// This branch exists so the package compiles and its tests run on a
// development machine that is not Windows. It follows the XDG convention on
// Unix-likes, which is also what macOS shells commonly export when the user
// has set it.
//
// This is NOT a support claim. TASK-03 records that only Windows x64 has been
// tested, and ADR-002 states that untested platforms are not declared
// supported. Returning a path here keeps the test suite runnable; it does not
// mean a player save on these platforms has been verified.
func platformLocalAppData() (string, bool) {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" && filepath.IsAbs(base) {
		return base, true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".local", "share"), true
}
