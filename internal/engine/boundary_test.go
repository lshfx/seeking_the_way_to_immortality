package engine

import (
	"go/build"
	"strings"
	"testing"
)

// boundaryTestNeedles lists the facilities the domain engine must never reach
// for directly. ADR-002 requires the engine to stay independent from
// terminals, filesystems, clocks, and networks so that rules remain
// deterministic and replayable; TASK-03 only establishes the boundary, but the
// guard belongs in the Go test suite so a violation fails `go test ./...` on
// its own, without depending on a PowerShell script being run.
var boundaryTestNeedles = []string{
	// Networking.
	`"net"`,
	`"net/http"`,
	`"net/url"`,
	// Filesystem and process environment.
	`"os"`,
	`"io/fs"`,
	`"path/filepath"`,
	`"os/exec"`,
	// Wall clock and randomness.
	`"time"`,
	`"math/rand"`,
	"crypto/rand",
	`"syscall"`,
	// Sibling packages that own I/O or presentation responsibilities.
	"github.com/lshfx/seeking_the_way_to_immortality/internal/cli",
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage",
	"github.com/lshfx/seeking_the_way_to_immortality/internal/tui",
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel",
	"github.com/lshfx/seeking_the_way_to_immortality/internal/session",
	"github.com/lshfx/seeking_the_way_to_immortality/internal/content",
}

// TestEngineImportsStayWithinTheDomainBoundary inspects the package's real
// import graph (including test files) and fails if a forbidden dependency
// appears.
func TestEngineImportsStayWithinTheDomainBoundary(t *testing.T) {
	pkg, err := build.ImportDir(".", build.ImportComment)
	if err != nil {
		t.Fatalf("failed to inspect engine package: %v", err)
	}

	all := make([]string, 0, len(pkg.Imports)+len(pkg.TestImports)+len(pkg.XTestImports))
	all = append(all, pkg.Imports...)
	all = append(all, pkg.TestImports...)
	all = append(all, pkg.XTestImports...)

	seen := make(map[string]bool, len(all))
	for _, imp := range all {
		seen[imp] = true
	}

	for _, needle := range boundaryTestNeedles {
		// Compare on package paths, not raw quoted substrings, so that
		// "os/exec" is not accidentally matched by the "os" entry.
		target := strings.Trim(needle, `"`)
		for imp := range seen {
			if imp == target || strings.HasPrefix(imp, target+"/") {
				t.Errorf("engine must not import %q (found %q): keeps rules deterministic and offline", target, imp)
			}
		}
	}
}

// TestEngineCurrentStatusIsPure documents that the current implementation has
// no side effects: calling it twice must yield identical values.
func TestEngineCurrentStatusIsPure(t *testing.T) {
	first := CurrentStatus()
	second := CurrentStatus()
	if first != second {
		t.Fatalf("CurrentStatus is not pure: %+v vs %+v", first, second)
	}
}
