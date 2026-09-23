//go:build windows

package storage

import "os"

// platformLocalAppData returns the per-user local application data directory.
//
// It reads LOCALAPPDATA rather than calling SHGetKnownFolderPath because the
// engine-adjacent packages must not pull in syscall wrappers, and because the
// environment variable is what the design document names. A missing or
// relative value is reported as unavailable so the caller refuses to save
// rather than guessing a location.
func platformLocalAppData() (string, bool) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", false
	}
	return base, true
}
