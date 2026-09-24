//go:build windows

package tui

import (
	"os"
	"testing"
)

// TestNulDeviceIsNotInteractive pins why a bare character-device check is
// insufficient on Windows. NUL is a character device, so a redirected NUL
// handle looked interactive: a bare launch created the user data directory,
// entered terminal setup, and failed with exit code 1 instead of refusing
// with exit code 2.
func TestNulDeviceIsNotInteractive(t *testing.T) {
	nul, err := os.OpenFile("NUL", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open NUL: %v", err)
	}
	defer func() { _ = nul.Close() }()

	// Control group. If NUL ever stops being a character device, the naive
	// check would reject it and this test would pass for the wrong reason.
	if !isCharacterDevice(nul) {
		t.Fatal("NUL is no longer a character device; the naive check would now reject it and this regression test no longer observes the defect")
	}
	if IsInteractive(nul, nul) {
		t.Error("IsInteractive accepted the NUL device as a console; a bare launch would create the user data directory and fail with exit code 1 instead of refusing with exit code 2")
	}
}

func TestIsInteractiveRejectsRegularFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-console-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() { _ = file.Close() }()

	if IsInteractive(file, file) {
		t.Error("IsInteractive accepted a regular file as a console")
	}
}

func TestIsInteractiveRejectsNilStreams(t *testing.T) {
	if IsInteractive(nil, nil) {
		t.Error("IsInteractive accepted nil streams as a console")
	}
}
