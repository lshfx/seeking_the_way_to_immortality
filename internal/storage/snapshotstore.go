package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// CheckpointRetention is how many pre-risk checkpoints are kept per character.
//
// The number is a product decision recorded in ADR-003: checkpoints exist so a
// player who regrets a risky action has somewhere to go back to, and five covers
// a run of bad decisions without letting the directory grow without bound.
const CheckpointRetention = 5

// SnapshotStore is the durable, file-backed store.
//
// It is generic over the envelope so this package does not import the engine:
// the wiring layer supplies the concrete type and the accessors. That keeps the
// dependency arrow pointing one way and lets every test here run with a trivial
// stand-in.
type SnapshotStore[T any] struct {
	layout  Layout
	gameID  string
	perm    os.FileMode
	sync    SyncStrategy
	codec   Codec[T]
	current *T
}

// Codec bridges a concrete envelope type to the operations this package needs.
//
// It exists instead of an interface on T because T is a struct (not a pointer to
// an interface), and because the operations are few and semantically distinct:
// encode, decode, seal, verify, and read the declared version.
type Codec[T any] struct {
	// New returns a zero value to decode into. Called before every decode so a
	// failed decode cannot leave a partially-populated value behind.
	New func() *T
	// Seal fills in the integrity digest. Called immediately before encoding.
	Seal func(*T)
	// Verify reports whether the digest matches the payload.
	Verify func(*T) bool
	// Version reports the envelope version the document declares.
	Version func(*T) int
	// GameID reports the character the document belongs to, for a cross-save
	// sanity check.
	GameID func(*T) string
	// Revision reports the state revision, used for checkpoint naming and to
	// reject an older write over a newer one.
	Revision func(*T) uint64
	// Validate is an optional extra check applied to an outgoing envelope, so a
	// state that cannot survive a round trip is refused *before* it replaces a
	// good save. Nil means no extra check.
	Validate func(*T) error
}

// NewSnapshotStore creates a store rooted at a layout for one character.
func NewSnapshotStore[T any](layout Layout, gameID string, codec Codec[T]) (*SnapshotStore[T], error) {
	if codec.New == nil || codec.Seal == nil || codec.Verify == nil || codec.Version == nil {
		return nil, fmt.Errorf("%w: the codec is incomplete", ErrSaveCorrupt)
	}
	if _, err := EnsureLayoutFor(layout); err != nil {
		return nil, err
	}
	// The character subtree is created here rather than lazily inside the write
	// path: a save must never depend on a directory being created as a side
	// effect, because a failure at that point reads as "could not write the
	// save" and hides the real cause.
	if _, err := layout.EnsureCharacterDir(gameID); err != nil {
		return nil, err
	}
	return &SnapshotStore[T]{
		layout: layout,
		gameID: gameID,
		perm:   0o600,
		sync:   SyncFull,
		codec:  codec,
	}, nil
}

// SetSyncStrategy overrides the durability used for subsequent writes. It exists
// for tests that need to observe the pre-fsync window, and for a future setting.
// It is not a player-facing knob.
func (s *SnapshotStore[T]) SetSyncStrategy(strategy SyncStrategy) { s.sync = strategy }

// SavePath exposes the current save path, for diagnostics.
func (s *SnapshotStore[T]) SavePath() string { return s.layout.SavePath(s.gameID) }

// PrevSavePath exposes the rotation path.
func (s *SnapshotStore[T]) PrevSavePath() string { return s.layout.PrevSavePath(s.gameID) }

// LoadResult reports what Load found, so startup can tell the user the truth
// about whether their progress was recovered, rolled back, or lost.
type LoadResult[T any] struct {
	// Envelope is the loaded document, or nil when nothing usable was found.
	Envelope *T
	// Source names where the document came from: "current", "previous", or ""
	// when nothing loaded.
	Source string
	// Recovered reports that the current save was unusable and the previous one
	// was used instead. This is the case the player must be told about.
	Recovered bool
	// CurrentProblem is the reason the current save was rejected, when it was.
	// Retained so the user can be shown a real cause instead of "unknown error".
	CurrentProblem error
	// PreservedPath is where the rejected current save was archived, when it was.
	PreservedPath string
}

// Load reads the newest usable save, falling back to the rotation copy.
//
// The fallback order and its side effects are the whole point of this method:
//
//  1. current is tried first
//  2. if current is corrupt or unsupported, the previous copy is tried
//  3. a rejected current save is *copied aside*, never deleted, so the player
//     can hand it to a future version or to a human
//
// A rejected save is never deleted and never overwritten by this call. Reporting
// a loss honestly is better than making the loss invisible.
func (s *SnapshotStore[T]) Load() (LoadResult[T], error) {
	var result LoadResult[T]

	currentPath := s.layout.SavePath(s.gameID)
	env, err := s.decodeFile(currentPath)
	if err == nil {
		result.Envelope = env
		result.Source = "current"
		s.current = env
		return result, nil
	}

	if errors.Is(err, ErrSaveNotFound) {
		// No current save at all. The previous copy may still exist, which is
		// what a crash between rotation and write leaves behind.
		prev, prevErr := s.decodeFile(s.layout.PrevSavePath(s.gameID))
		if prevErr == nil {
			result.Envelope = prev
			result.Source = "previous"
			result.Recovered = true
			s.current = prev
			return result, nil
		}
		return result, fmt.Errorf("%w: %s", ErrSaveNotFound, currentPath)
	}

	// The current save exists but is unusable. Preserve it before doing
	// anything else, so no later step can lose it.
	result.CurrentProblem = err
	if preserved, perr := CopySaveToPreserved(currentPath, s.layout.PreservedDir(),
		timeStampSuffix()); perr == nil {
		result.PreservedPath = preserved
	} else {
		// Preservation failing is itself worth reporting: proceeding without a
		// preserved copy means the next write destroys whatever was there.
		return result, fmt.Errorf("the current save is unusable (%v) and could not be preserved: %w",
			err, perr)
	}

	prev, prevErr := s.decodeFile(s.layout.PrevSavePath(s.gameID))
	if prevErr == nil {
		result.Envelope = prev
		result.Source = "previous"
		result.Recovered = true
		s.current = prev
		return result, nil
	}

	// Both copies are unusable. The error class is chosen deliberately rather
	// than always being "corrupt", because the *user action* differs: a save
	// from a newer build means "update the game", while a damaged save means
	// "restore a backup".
	//
	// The current save's classification therefore decides the class, and the
	// previous copy can only make it worse. Reporting an unsupported save as
	// corrupt would send a player down the wrong recovery path.
	if !errors.Is(err, ErrSaveUnsupported) {
		return result, fmt.Errorf("%w: the current save is unusable (%v) and so is the previous one (%v); "+
			"the damaged current save was preserved at %s",
			ErrSaveCorrupt, err, prevErr, result.PreservedPath)
	}
	// A save from a newer build must not be overwritten either. Only
	// ErrSaveUnsupported is attached, *not* ErrSaveCorrupt: a caller asking
	// "is this corruption?" must get a clean no, because "update the game" and
	// "restore a backup" are different instructions and an error that matches
	// both cannot drive either. That the previous copy is also unreadable is
	// stated in the message, where it informs a human without muddying the
	// machine-readable classification.
	return result, fmt.Errorf("%w: the current save is unusable (%v) and so is the previous one (%v); "+
		"the newer save was preserved at %s and this build must not overwrite it",
		ErrSaveUnsupported, err, prevErr, result.PreservedPath)
}

// decodeFile reads and fully validates one file.
func (s *SnapshotStore[T]) decodeFile(path string) (*T, error) {
	data, ok, err := readFileIfExists(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s could not be read: %v", ErrSaveCorrupt, path, err)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSaveNotFound, path)
	}

	target := s.codec.New()
	if target == nil {
		return nil, fmt.Errorf("%w: the codec produced no decode target", ErrSaveCorrupt)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return nil, fmt.Errorf("%w: %s is not a readable save document: %v", ErrSaveCorrupt, path, err)
	}

	version := s.codec.Version(target)
	if version > CurrentEnvelopeVersion {
		return nil, fmt.Errorf("%w: %s declares envelope version %d and this build understands up to %d; "+
			"open it with a newer build", ErrSaveUnsupported, path, version, CurrentEnvelopeVersion)
	}
	if version <= 0 {
		return nil, fmt.Errorf("%w: %s declares no envelope version", ErrSaveCorrupt, path)
	}
	if !s.codec.Verify(target) {
		return nil, fmt.Errorf("%w: the integrity digest of %s does not match its content, so it was "+
			"truncated or edited outside the game", ErrSaveCorrupt, path)
	}
	if s.codec.GameID != nil {
		if got := s.codec.GameID(target); got != "" && got != s.gameID {
			return nil, fmt.Errorf("%w: %s holds character %q but was loaded as %q",
				ErrSaveCorrupt, path, got, s.gameID)
		}
	}
	return target, nil
}

// Commit writes the envelope as the current save, rotating the old one.
//
// The revision guard is not an optimisation. Writing an older revision over a
// newer one is how a stale window destroys progress, so it is refused rather
// than trusted to callers.
func (s *SnapshotStore[T]) Commit(env *T) error {
	if env == nil {
		return fmt.Errorf("%w: nothing to commit", ErrSaveCorrupt)
	}

	currentPath := s.layout.SavePath(s.gameID)

	if s.codec.Revision != nil && s.current != nil {
		incoming := s.codec.Revision(env)
		existing := s.codec.Revision(s.current)
		if incoming < existing {
			return fmt.Errorf("%w: cannot write revision %d over the newer revision %d",
				ErrStaleRevision, incoming, existing)
		}
	}

	if err := s.codec.encodeValidate(env); err != nil {
		return err
	}

	s.codec.Seal(env)
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("cannot encode the save: %w", err)
	}
	data = append(data, '\n')

	// Rotate the outgoing save before overwriting it. A crash between these two
	// steps leaves a complete old save at the rotation path.
	if previous, ok, rerr := readFileIfExists(currentPath); rerr == nil && ok {
		if _, werr := atomicWriteFile(s.layout.PrevSavePath(s.gameID), previous, s.perm, SyncFileOnly); werr != nil {
			return fmt.Errorf("cannot preserve the previous save: %w", werr)
		}
	} else if rerr != nil {
		return fmt.Errorf("cannot read the current save to preserve it: %w", rerr)
	}

	if _, err := atomicWriteFile(currentPath, data, s.perm, s.sync); err != nil {
		return err
	}
	s.current = env
	return nil
}

// encodeValidate applies the codec's optional outgoing check.
func (c Codec[T]) encodeValidate(env *T) error {
	if c.Validate == nil {
		return nil
	}
	return c.Validate(env)
}

// ErrStaleRevision reports an attempt to write an older revision over a newer
// one.
var ErrStaleRevision = errors.New("refusing to overwrite a newer revision with an older one")

// Checkpoint writes a pre-risk checkpoint, then prunes to the retention limit.
//
// Checkpoints are named by revision so a listing is ordered by progress, and the
// zero-padded revision is what makes that ordering correct.
func (s *SnapshotStore[T]) Checkpoint(env *T, revision uint64) (string, error) {
	if env == nil {
		return "", fmt.Errorf("%w: nothing to checkpoint", ErrSaveCorrupt)
	}
	s.codec.Seal(env)
	data, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("cannot encode the checkpoint: %w", err)
	}
	data = append(data, '\n')

	path := s.layout.CheckpointPath(s.gameID, revision)
	if _, err := atomicWriteFile(path, data, s.perm, s.sync); err != nil {
		return "", err
	}
	if err := s.PruneCheckpoints(); err != nil {
		// The checkpoint was written; a prune failure is a housekeeping problem,
		// not a reason to tell the player their checkpoint failed.
		return path, nil
	}
	return path, nil
}

// PruneCheckpoints removes the oldest checkpoints beyond the retention limit.
//
// Named manual saves are never touched: the design promises they survive
// automatic cleanup, and that promise is what makes them worth having.
func (s *SnapshotStore[T]) PruneCheckpoints() error {
	dir := s.layout.CheckpointDir(s.gameID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	type item struct {
		name string
		rev  uint64
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		rev, ok := parseRevisionName(e.Name())
		if !ok {
			// An unrecognised name is left alone rather than deleted. Deleting
			// what cannot be interpreted is how a user's file disappears.
			continue
		}
		items = append(items, item{name: e.Name(), rev: rev})
	}
	if len(items) <= CheckpointRetention {
		return nil
	}

	sort.Slice(items, func(i, j int) bool { return items[i].rev < items[j].rev })
	for _, it := range items[:len(items)-CheckpointRetention] {
		if err := os.Remove(filepath.Join(dir, it.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ListCheckpoints returns the retained checkpoints, newest first.
func (s *SnapshotStore[T]) ListCheckpoints() ([]uint64, error) {
	dir := s.layout.CheckpointDir(s.gameID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var revs []uint64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if rev, ok := parseRevisionName(e.Name()); ok {
			revs = append(revs, rev)
		}
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i] > revs[j] })
	return revs, nil
}
