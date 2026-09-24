//go:build windows

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBareLaunchWithNulStreamsRefusesWithoutCreatingDataRoot is the end-to-end
// counterpart of the tui unit test. It runs this test binary as the CLI with
// both standard streams bound to the NUL device. NUL is a character device, so
// the previous character-device-only interactivity check accepted it, created
// the user data directory, and then failed inside terminal setup with exit
// code 1 instead of refusing with exit code 2.
func TestBareLaunchWithNulStreamsRefusesWithoutCreatingDataRoot(t *testing.T) {
	dataRoot := t.TempDir()

	stdin, err := os.OpenFile("NUL", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open NUL for stdin: %v", err)
	}
	defer func() { _ = stdin.Close() }()
	stdout, err := os.OpenFile("NUL", os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open NUL for stdout: %v", err)
	}
	defer func() { _ = stdout.Close() }()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}

	cmd := exec.Command(self)
	cmd.Env = envOverrides(childEnvFlag, "1", "LOCALAPPDATA", dataRoot)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if code != 2 {
		t.Fatalf("bare launch with NUL streams must refuse with exit code 2: exit=%d err=%v stderr=%q", code, runErr, stderr.String())
	}
	if !strings.Contains(stderr.String(), "交互式终端") {
		t.Fatalf("refusal must explain the terminal requirement: %q", stderr.String())
	}
	if _, statErr := os.Stat(filepath.Join(dataRoot, "WendaoChangsheng")); !os.IsNotExist(statErr) {
		t.Fatalf("a refused launch must not create the user data directory: stat err=%v", statErr)
	}
}

// envOverrides returns the current environment with the given name/value pairs
// applied, dropping any inherited entry that uses the same name so the child
// cannot observe both values.
func envOverrides(pairs ...string) []string {
	replaced := make(map[string]bool, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		replaced[strings.ToUpper(pairs[i])] = true
	}
	merged := make([]string, 0, len(os.Environ())+len(pairs)/2)
	for _, entry := range os.Environ() {
		name := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			name = entry[:idx]
		}
		if replaced[strings.ToUpper(name)] {
			continue
		}
		merged = append(merged, entry)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		merged = append(merged, pairs[i]+"="+pairs[i+1])
	}
	return merged
}
