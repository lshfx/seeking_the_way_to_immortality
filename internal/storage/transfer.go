package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExportSave writes a character's current save to a location chosen by the
// player, so progress can be moved between machines or kept as a backup.
//
// The export is a *copy* of the stored document, byte for byte. It is
// deliberately not re-encoded: re-encoding would apply this build's formatting
// and, worse, might silently normalise a field, so an exported file could
// differ from the stored one in ways nobody intended. An export is for moving
// bytes, not for interpreting them.
//
// The export must not target a path inside the data root. A player who exports
// onto their own save file would destroy it, and the file picker makes that easy
// to do by accident.
func ExportSave(src, dest string, dataRoot string) (int, error) {
	data, ok, err := readFileIfExists(src)
	if err != nil {
		return 0, fmt.Errorf("cannot read the save to export: %w", err)
	}
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrSaveNotFound, src)
	}

	if err := refuseExportInsideDataRoot(dest, dataRoot); err != nil {
		return 0, err
	}

	if dir := filepath.Dir(dest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return 0, fmt.Errorf("cannot create the export directory: %w", err)
		}
	}
	// SyncFileOnly rather than SyncFull: an export is a portable copy the player
	// will move away, and the eventual copy is what needs to be durable. Full
	// sync would also mean creating and removing a directory entry in a
	// directory this process does not own.
	if _, err := atomicWriteFile(dest, data, 0o600, SyncFileOnly); err != nil {
		return 0, fmt.Errorf("cannot write the export: %w", err)
	}
	return len(data), nil
}

// refuseExportInsideDataRoot rejects an export path that lands inside the game's
// own data directory.
//
// Writing an export over the live save is the one export mistake that is
// unrecoverable, and it is the easiest one to make: a file dialog opens in the
// last-used folder, which for many players is the save folder itself.
func refuseExportInsideDataRoot(dest, dataRoot string) error {
	if strings.TrimSpace(dataRoot) == "" {
		return nil
	}
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return fmt.Errorf("cannot resolve the data root %q: %w", dataRoot, err)
	}
	target, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("cannot resolve the export path %q: %w", dest, err)
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		// Different volumes: the path cannot be inside the root.
		return nil
	}
	if rel == "." || (rel != ".." && !hasParentPrefix(rel)) {
		return fmt.Errorf("%w: the export path %s is inside the game's data directory %s; "+
			"export somewhere else so the live save cannot be overwritten",
			ErrExportRefused, dest, dataRoot)
	}
	return nil
}

// hasParentPrefix reports whether a relative path starts with "..".
func hasParentPrefix(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}

// ErrExportRefused reports an export that was rejected before writing anything.
var ErrExportRefused = errors.New("the export was refused")

// ImportResult reports what an import did, so the UI can tell the player the
// truth rather than guessing from whether an error was returned.
type ImportResult struct {
	// Envelope is the validated document, ready to be committed by the caller.
	// Nil when the import failed.
	Envelope map[string]any
	// Bytes is the size of the imported document.
	Bytes int
	// Source is the path that was imported.
	Source string
}

// ValidateImport reads and fully validates a save file that is about to replace
// the current save, without touching anything on disk.
//
// This is the "a bad import must not overwrite the current save" requirement,
// and the way to satisfy it is not to check carefully before writing -- it is to
// never write at all in this function. Validation and commitment are separate
// calls, and the caller commits only after this returns successfully.
//
// The checks are the same ones every other read path applies, and they are
// applied in the same order, because an import that skipped a check would be a
// way to load a document the normal path refuses:
//
//  1. the file must be readable and be JSON at all
//  2. it must declare an envelope version this build understands
//  3. it must be self-consistent under its own integrity digest
//
// A digest check is the important one for an import specifically: the file has
// travelled, possibly through a mail client or a chat app, and a truncated
// transfer is exactly the failure that produces a file which is still valid
// JSON but is no longer the save that was exported.
func ValidateImport(path string, verify func(map[string]any) (version int, ok bool, why string)) (ImportResult, error) {
	var result ImportResult

	data, ok, err := readFileIfExists(path)
	if err != nil {
		return result, fmt.Errorf("%w: %s could not be read: %v", ErrImportRefused, path, err)
	}
	if !ok {
		// Both causes are attached: ErrImportRefused is what the caller acts
		// on, and ErrSaveNotFound lets it distinguish "you picked a file that
		// is not there" from "you picked a file that is not a save". Printing
		// the text is not enough -- a caller matches on the error.
		return result, fmt.Errorf("%w: %w: %s", ErrImportRefused, ErrSaveNotFound, path)
	}
	result.Source = path
	result.Bytes = len(data)

	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return result, fmt.Errorf("%w: %s is not a readable save document: %v", ErrImportRefused, path, err)
	}

	if verify == nil {
		return result, fmt.Errorf("%w: no integrity check was supplied", ErrImportRefused)
	}
	version, digestOK, why := verify(doc)
	if version > CurrentEnvelopeVersion {
		return result, fmt.Errorf("%w: %s was written by a newer version of the game "+
			"(envelope version %d; this build understands up to %d); update the game and try again",
			ErrImportRefused, path, version, CurrentEnvelopeVersion)
	}
	if version <= 0 {
		return result, fmt.Errorf("%w: %s does not declare a version, so it is not a save file",
			ErrImportRefused, path)
	}
	if !digestOK {
		return result, fmt.Errorf("%w: %s failed its integrity check (%s); "+
			"the file was most likely truncated in transit, so importing it would replace a good "+
			"save with an incomplete one", ErrImportRefused, path, why)
	}

	result.Envelope = doc
	return result, nil
}

// ErrImportRefused reports an import that was rejected. The current save was not
// touched.
var ErrImportRefused = errors.New("the import was refused and the current save was left untouched")

// ImportIntoSave replaces the current save with an imported document, keeping a
// copy of the outgoing save so the import is reversible.
//
// The import writes through the same atomic path as a normal save and rotates
// the outgoing save to the previous slot first, so an import has the same crash
// properties as any other write: either the old complete save or the new
// complete save is what a restart finds.
//
// The caller must have called ValidateImport first. That is stated rather than
// re-checked because the validation needs the envelope's own accessors, which
// only the wiring layer has -- and a partial re-check here would give the false
// impression that this function is safe to call on its own.
func (s *SnapshotStore[T]) ImportIntoSave(imported *T, keepOutgoingAt string) (CommitResult, error) {
	var result CommitResult
	if imported == nil {
		return result, fmt.Errorf("%w: there is nothing to import", ErrImportRefused)
	}

	currentPath := s.layout.SavePath(s.gameID)

	// Rotate first, exactly as Commit does, so a crash during the import leaves
	// the pre-import save recoverable.
	if previous, ok, rerr := readFileIfExists(currentPath); rerr == nil && ok {
		if _, werr := atomicWriteFile(s.layout.PrevSavePath(s.gameID), previous, s.perm, SyncFileOnly); werr != nil {
			return result, fmt.Errorf("cannot preserve the pre-import save: %w", werr)
		}
	} else if rerr != nil {
		return result, fmt.Errorf("cannot read the pre-import save to preserve it: %w", rerr)
	}

	if err := s.codec.encodeValidate(imported); err != nil {
		return result, err
	}
	s.codec.Seal(imported)
	data, err := json.Marshal(imported)
	if err != nil {
		return result, fmt.Errorf("cannot encode the imported save: %w", err)
	}
	data = append(data, '\n')

	if _, err := atomicWriteFile(currentPath, data, s.perm, s.sync); err != nil {
		return result, err
	}

	// The caller may also want a labelled copy, e.g. "imported-from-laptop".
	// Failing to write it does not undo the import, so it is reported but not
	// treated as a failed import -- the save itself is already in place.
	if keepOutgoingAt != "" {
		if _, err := atomicWriteFile(keepOutgoingAt, data, s.perm, SyncFileOnly); err != nil {
			result.KeptCopyError = err
		}
	}

	s.current = imported
	result.Path = currentPath
	result.Bytes = len(data)
	return result, nil
}
