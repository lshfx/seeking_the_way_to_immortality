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

func TestRunDiagnoseIsExplicitlyOfflineAndUnimplemented(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--diagnose"}, &stdout, &stderr, "dev")
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"network=disabled", "game_state=not_implemented"} {
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

// This boundary check calls the pure engine package directly. The engine
// package imports no CLI, terminal, filesystem, clock, or network facilities.
func TestEngineDoesNotPretendGameRulesExist(t *testing.T) {
	status := engine.CurrentStatus()
	if status.Implemented {
		t.Fatal("TASK-03 skeleton must not claim that the engine is implemented")
	}
	if status.Reason == "" {
		t.Fatal("unimplemented status needs a reason")
	}
}
