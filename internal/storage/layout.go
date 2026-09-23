package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// AppDirName is the directory created under the user's local application data
// root. It is deliberately not the executable's name so that a renamed binary
// does not silently start a second data root.
const AppDirName = "WendaoChangsheng"

// DataRootEnvVar lets a caller redirect the data root explicitly. Tests and a
// future portable mode use it. It is an *override*, never a fallback: if it is
// set but unusable, resolution fails rather than quietly writing elsewhere.
const DataRootEnvVar = "WENDAO_DATA_ROOT"

// Layout describes where a character's files live.
type Layout struct {
	// Root is the application data root, e.g.
	// %LOCALAPPDATA%\WendaoChangsheng.
	Root string
}

// ErrNoDataRoot reports that no usable data root could be determined.
//
// Resolution has exactly three sources and no fallback: an explicit override,
// then the platform's per-user local application data directory. If both are
// unavailable the game must refuse to start a save rather than write player
// progress into the installation directory or the current working directory.
// The design document forbids that fallback explicitly, and a silent fallback
// is how a game ends up scattering saves into whatever directory it was
// launched from.
var ErrNoDataRoot = errors.New(
	"no usable data root: set " + DataRootEnvVar + " or make the per-user " +
		"local application data directory available; " +
		"the game will not write saves into the installation or working directory")

// ResolveDataRoot determines the application data root without creating it.
//
// The override is checked first so tests can point at a temporary directory.
// An override that is set but blank, relative, or unexpandable is an error
// rather than a silent skip: a caller that asked for a specific location and
// did not get it must be told.
func ResolveDataRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv(DataRootEnvVar)); override != "" {
		if !filepath.IsAbs(override) {
			return "", &DataRootError{
				Path:   override,
				Reason: "the override is not an absolute path",
			}
		}
		return filepath.Clean(override), nil
	}

	base, ok := platformLocalAppData()
	if !ok || strings.TrimSpace(base) == "" {
		return "", ErrNoDataRoot
	}
	if !filepath.IsAbs(base) {
		return "", &DataRootError{
			Path:   base,
			Reason: "the platform local application data directory is not an absolute path",
		}
	}

	return filepath.Join(filepath.Clean(base), AppDirName), nil
}

// DataRootError describes a data root that was found but is unusable.
type DataRootError struct {
	// Path is the location that was rejected.
	Path string
	// Reason explains the rejection in terms a player can act on.
	Reason string
}

// Error implements error.
func (e *DataRootError) Error() string {
	return "unusable data root " + e.Path + ": " + e.Reason
}

// EnsureLayout resolves the data root and creates the directory tree.
//
// Creation is separate from resolution so that a read-only operation can
// succeed against an existing root without creating directories as a side
// effect. Writes call this; reads call ResolveDataRoot.
func EnsureLayout() (Layout, error) {
	root, err := ResolveDataRoot()
	if err != nil {
		return Layout{}, err
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return Layout{}, &DataRootError{
			Path:   root,
			Reason: "could not create the data root: " + err.Error(),
		}
	}

	return Layout{Root: root}, nil
}

// LayoutFor builds the layout for an already-resolved root. It does not touch
// the filesystem.
func LayoutFor(root string) Layout {
	return Layout{Root: filepath.Clean(root)}
}

// EnsureLayoutFor creates the directory tree for an already-resolved layout.
//
// It validates the layout first. An empty Root would otherwise be silently
// interpreted as the current directory, which would put a player's saves in
// whatever directory the program happened to start in -- the one place the
// design explicitly forbids.
func EnsureLayoutFor(l Layout) (Layout, error) {
	if strings.TrimSpace(l.Root) == "" {
		return Layout{}, &DataRootError{
			Path:   l.Root,
			Reason: "the layout has no root; refusing to fall back to the working directory",
		}
	}
	if err := os.MkdirAll(l.Root, 0o700); err != nil {
		return Layout{}, &DataRootError{
			Path:   l.Root,
			Reason: "could not create the data root: " + err.Error(),
		}
	}
	// The subdirectories are created up front so a later write never has to
	// create a directory as a side effect of saving, which would turn a save
	// failure into a confusing permission error.
	for _, dir := range []string{
		filepath.Join(l.Root, "chars"),
		l.LocksDir(),
		l.LogsDir(),
		l.PreservedDir(),
	} {
		if err := l.ensureDir(dir); err != nil {
			return Layout{}, err
		}
	}
	return l, nil
}

// EnsureCharacterDir creates the per-character directory subtree, including the
// checkpoints and manual-save directories.
//
// This is deliberately separate from EnsureLayoutFor. The root is created once
// at startup; a character directory is created when a character is created or
// first opened. Creating *every* character's directory up front would mean
// startup fails because of a directory belonging to a character the player has
// not touched in months.
func (l Layout) EnsureCharacterDir(gameID string) (Layout, error) {
	if strings.TrimSpace(l.Root) == "" {
		return Layout{}, &DataRootError{
			Path:   l.Root,
			Reason: "the layout has no root; refusing to fall back to the working directory",
		}
	}
	for _, dir := range []string{
		l.CharDir(gameID),
		l.CheckpointDir(gameID),
		l.ManualDir(gameID),
	} {
		if err := l.ensureDir(dir); err != nil {
			return Layout{}, err
		}
	}
	return l, nil
}

// ensureDir creates one directory, wrapping any failure with the path that
// failed so a permission problem names the directory instead of the operation.
func (l Layout) ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &DataRootError{
			Path:   dir,
			Reason: "could not create a data subdirectory: " + err.Error(),
		}
	}
	return nil
}

// CharDir is the directory for one character. The game id is sanitised so a
// hostile or corrupted id cannot escape the root: a save that could write
// outside its own directory would be a directory-traversal bug reachable from
// a hand-edited file.
func (l Layout) CharDir(gameID string) string {
	return filepath.Join(l.Root, "chars", sanitiseComponent(gameID))
}

// SavePath is the primary automatic recovery point.
func (l Layout) SavePath(gameID string) string {
	return filepath.Join(l.CharDir(gameID), "save.json")
}

// PrevSavePath is the previous valid automatic recovery point. Keeping it means
// a kill during a write always leaves at least one complete save readable.
func (l Layout) PrevSavePath(gameID string) string {
	return filepath.Join(l.CharDir(gameID), "save.json.prev")
}

// CheckpointDir holds pre-risk checkpoints for a character.
func (l Layout) CheckpointDir(gameID string) string {
	return filepath.Join(l.CharDir(gameID), "checkpoints")
}

// ManualDir holds named manual saves, which are never auto-deleted.
func (l Layout) ManualDir(gameID string) string {
	return filepath.Join(l.CharDir(gameID), "manual")
}

// ManualSavePath is one named manual save.
func (l Layout) ManualSavePath(gameID, name string) string {
	return filepath.Join(l.ManualDir(gameID), sanitiseComponent(name)+".json")
}

// CheckpointPath is one pre-risk checkpoint, keyed by revision so the name is
// derived from state rather than from a clock the storage layer must not read.
func (l Layout) CheckpointPath(gameID string, revision uint64) string {
	return filepath.Join(l.CheckpointDir(gameID), revisionName(revision)+".json")
}

// SettingsPath is the settings file. It is deliberately outside chars/ so that
// deleting a character does not reset the player's colour scheme, and so a
// corrupt settings file can fall back to defaults without touching progress.
func (l Layout) SettingsPath() string {
	return filepath.Join(l.Root, "settings.json")
}

// LocksDir holds lock files.
func (l Layout) LocksDir() string {
	return filepath.Join(l.Root, "locks")
}

// LogsDir holds rolling diagnostic logs.
func (l Layout) LogsDir() string {
	return filepath.Join(l.Root, "logs")
}

// sanitiseComponent makes an arbitrary identifier safe to use as one path
// element. It rejects separators, parent references, drive-relative prefixes,
// reserved device names, and control characters.
//
// A rejected component becomes a deterministic escaped form rather than an
// error, because a game id is generated by the program and a corrupted one
// should still be *locatable* — the character must remain reachable so the
// player can export or delete it. Returning an error here would turn a
// recoverable corruption into an unreachable save.
func sanitiseComponent(s string) string {
	if s == "" {
		return "_empty"
	}

	var b strings.Builder
	changed := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			// A leading dot could produce "." or ".." or a hidden file; those
			// are handled by the explicit checks below rather than here, so a
			// dot in the middle of a name stays readable.
			b.WriteRune(r)
		default:
			// Percent-encode anything else so the mapping is reversible and
			// collisions between distinct ids are impossible.
			writePercent(&b, r)
			changed = true
		}
	}

	out := b.String()
	if out == "." || out == ".." || out == "" {
		// These would escape or alias the parent directory.
		return "_" + percentOf(s)
	}
	if isReservedDeviceName(out) {
		// CON, PRN, AUX, NUL, COM1..9, LPT1..9 are device names on Windows
		// and cannot be used as file names regardless of extension.
		return "_" + out
	}
	_ = changed
	return out
}

// writePercent renders r as %XX over its UTF-8 bytes.
func writePercent(b *strings.Builder, r rune) {
	const hex = "0123456789ABCDEF"
	for _, by := range []byte(string(r)) {
		b.WriteByte('%')
		b.WriteByte(hex[by>>4])
		b.WriteByte(hex[by&0x0F])
	}
}

// percentOf renders every byte of s as %XX.
func percentOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		writePercent(&b, r)
	}
	return b.String()
}

// isReservedDeviceName reports whether name is a Windows reserved device name
// (case-insensitively, and ignoring any extension).
func isReservedDeviceName(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		up := strings.ToUpper(base)
		switch {
		case strings.HasPrefix(up, "COM"), strings.HasPrefix(up, "LPT"):
			if up[3] >= '1' && up[3] <= '9' {
				return true
			}
		}
	}
	return false
}

// revisionName renders a revision as a fixed-width decimal so that checkpoint
// files sort lexicographically in the same order they sort numerically. A
// 20-digit width covers the full uint64 range.
func revisionName(revision uint64) string {
	const width = 20
	var buf [width]byte
	for i := width - 1; i >= 0; i-- {
		buf[i] = byte('0' + revision%10)
		revision /= 10
	}
	return string(buf[:])
}

// parseRevisionName is the inverse of revisionName, for pruning and listing.
//
// It is strict on purpose: it accepts only the exact fixed-width form this
// package writes. A permissive parser would read "10" and "00000000000000000010"
// as the same revision, and a prune based on that could delete a file written by
// a different scheme. Returning ok=false leaves the file alone.
func parseRevisionName(name string) (uint64, bool) {
	const width = 20
	if len(name) != width+len(".json") || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	digits := name[:width]
	var value uint64
	for i := 0; i < width; i++ {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		// Overflow is refused rather than wrapped: a wrapped value would sort
		// as a small revision and be pruned first, losing the newest checkpoint.
		if value > (^uint64(0))/10 {
			return 0, false
		}
		value = value*10 + uint64(c-'0')
	}
	return value, true
}

// PreservedDir holds saves that were rejected, kept so nothing a player wrote is
// ever silently discarded.
func (l Layout) PreservedDir() string {
	return filepath.Join(l.Root, "preserved")
}
