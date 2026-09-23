package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, &stdout, &stderr, "0.0.0-test")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wendao 0.0.0-test") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunDiagnoseIsExplicitlyOfflineAndReportsThePlayableSlice(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--diagnose"}, &stdout, &stderr, "dev")
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"network=disabled", "game_state=short_loop_implemented"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("diagnose output missing %q: %q", want, stdout.String())
		}
	}
}

func TestRunRejectsUnknownArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--play"}, &stdout, &stderr, "dev")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "未知参数") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunHelpListsEveryDocumentedFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--help"}, &stdout, &stderr, "dev")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("help must not write to stderr: %q", stderr.String())
	}
	out := stdout.String()
	for _, flag := range []string{"--help", "--version", "--diagnose"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output missing %q", flag)
		}
	}
}

func TestRunHelpShortFlagMatchesLongFlag(t *testing.T) {
	var longOut, shortOut, errBuf bytes.Buffer
	if code := Run([]string{"--help"}, &longOut, &errBuf, "dev"); code != 0 {
		t.Fatalf("--help exit code = %d", code)
	}
	if code := Run([]string{"-h"}, &shortOut, &errBuf, "dev"); code != 0 {
		t.Fatalf("-h exit code = %d", code)
	}
	if longOut.String() != shortOut.String() {
		t.Fatalf("-h output differs from --help:\n%q\n%q", shortOut.String(), longOut.String())
	}
}

func TestRunRejectsTooManyArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version", "--diagnose"}, &stdout, &stderr, "dev")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("rejected input must not print to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "参数过多") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestRunWithoutArgumentsRefusesRedirectedStreams so ANSI input and screen
// controls are never written to a log or pipe.
func TestRunWithoutArgumentsRefusesRedirectedStreams(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, &stdout, &stderr, "dev")
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "交互式终端") {
		t.Fatalf("bare invocation should explain the TTY requirement: %q", stderr.String())
	}
}

// The engine's status reports the current slice without claiming all planned
// M1 systems are complete.
func TestEngineReportsTheImplementedShortLoop(t *testing.T) {
	status := engine.CurrentStatus()
	if !status.Implemented {
		t.Fatal("the creation and cultivation short loop must be reported as implemented")
	}
	if !strings.Contains(status.Reason, "short loop") {
		t.Fatalf("status reason must describe the limited playable slice: %q", status.Reason)
	}
}
