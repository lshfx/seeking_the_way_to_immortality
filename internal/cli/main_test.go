package cli

import (
	"os"
	"testing"
)

// childEnvFlag makes the test binary act as the CLI itself. That lets the
// no-terminal refusal be observed with real NUL handles, a real environment,
// and a real process exit code instead of an in-process approximation.
const childEnvFlag = "WENDAO_CLI_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnvFlag) == "1" {
		os.Exit(Run(nil, os.Stdout, os.Stderr, "dev"))
	}
	os.Exit(m.Run())
}
