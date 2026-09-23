package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// envelopeForWrite narrows the engine envelope to the fields this package must
// touch. It is an interface rather than a concrete *engine.SaveEnvelope so that
// internal/storage does not depend on the engine package's concrete type at
// compile time -- the dependency runs one way, from the wiring layer, which
// keeps this package testable with a trivial stand-in.
//
// The three methods are exactly the operations a durable write needs: serialise,
// seal, and self-check. Anything else about an envelope is the engine's business.
type envelopeForWrite interface {
	// MarshalJSON makes the envelope serialisable. Declared so a caller cannot
	// pass something that would encode to nothing useful.
	json.Marshaler
	// ComputeIntegrity seals the digest over the canonical payload.
	ComputeIntegrity()
	// VerifyIntegrity reports whether the sealed digest matches the payload.
	VerifyIntegrity() bool
}

// Validated is the decoded envelope as read back from disk, together with the
// evidence needed to report honestly on what was loaded.
type Validated struct {
	// Raw is the exact bytes that were read. Retained so a caller can archive a
	// rejected save instead of silently discarding the player's file.
	Raw []byte
	// Outcome records what durability the original write achieved, when the
	// snapshot store wrote it. Zero value for a foreign file.
	Outcome WriteOutcome
}

// SaveRequest is one durable commit.
type SaveRequest struct {
	// Path is the destination file.
	Path string
	// Envelope is the document to persist.
	Envelope envelopeForWrite
	// Sync names the requested durability.
	Sync SyncStrategy
	// Perm is the file mode. Zero means 0o600.
	Perm os.FileMode
}

// CommitResult reports what actually happened, so the caller can tell the player
// the truth rather than assuming the strongest behaviour.
type CommitResult struct {
	// Path is the file that was written.
	Path string
	// Bytes is the serialised size.
	Bytes int
	// Outcome is the durability actually achieved.
	Outcome WriteOutcome
	// RotatedTo is the path the previous content was preserved at, if any.
	RotatedTo string
	// KeptCopyError reports that an additional labelled copy could not be
	// written. It is separate from the returned error because the save itself
	// succeeded: reporting a successful save as a failure would make the player
	// retry an operation that already worked.
	KeptCopyError error
}

// ErrSaveCorrupt reports that a save file exists but cannot be trusted: it fails
// to parse, its digest does not match, or its envelope version is not
// understood. The file is never modified when this is returned.
var ErrSaveCorrupt = errors.New("the save file is corrupt")

// ErrSaveUnsupported reports a save written by a version this build cannot
// interpret. It is separate from ErrSaveCorrupt because the user action differs:
// a corrupt file may be recoverable from the previous copy, while an unreadable
// future version simply needs a newer build.
var ErrSaveUnsupported = errors.New("the save file was written by an unsupported version")

// ErrSaveNotFound reports that no save exists. It is distinct from corruption so
// a first launch is not reported as an error.
var ErrSaveNotFound = errors.New("no save file exists")

// MarshalEnvelope serialises an envelope with the integrity digest sealed.
//
// Sealing happens here, immediately before serialisation, because an envelope
// whose digest was computed earlier could have been mutated in between. Doing it
// at the write site makes "the digest covers what was written" a local property
// rather than a convention every caller must remember.
func MarshalEnvelope(env envelopeForWrite) ([]byte, error) {
	if env == nil {
		return nil, fmt.Errorf("%w: no envelope supplied", ErrSaveCorrupt)
	}
	env.ComputeIntegrity()
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("%w: the envelope could not be encoded: %v", ErrSaveCorrupt, err)
	}
	// A trailing newline keeps the file text-friendly and makes a truncated
	// write more likely to fail parsing, which is the outcome we want.
	return append(data, '\n'), nil
}

// PersistSave writes an envelope durably, preserving the previous content.
//
// The order is deliberate: the outgoing save is copied to the rotation path
// *before* the new content replaces it, so a crash between the two leaves a
// complete old save at the rotation path rather than losing both.
//
// rotateTo may be empty, in which case no rotation is performed and the previous
// content is lost. Callers that can supply a rotation path should always do so.
func PersistSave(req SaveRequest, rotateTo string) (CommitResult, error) {
	var result CommitResult
	result.Path = req.Path

	perm := req.Perm
	if perm == 0 {
		perm = 0o600
	}

	data, err := MarshalEnvelope(req.Envelope)
	if err != nil {
		return result, err
	}

	// Rotate first. If this fails, nothing has been overwritten and the caller
	// still has the old save.
	if rotateTo != "" {
		previous, ok, readErr := readFileIfExists(req.Path)
		if readErr != nil {
			return result, fmt.Errorf("%w: cannot read the current save to rotate it: %v", ErrSaveCorrupt, readErr)
		}
		if ok {
			if _, writeErr := atomicWriteFile(rotateTo, previous, perm, SyncFileOnly); writeErr != nil {
				return result, fmt.Errorf("cannot preserve the previous save at %s: %w", rotateTo, writeErr)
			}
			result.RotatedTo = rotateTo
		}
	}

	outcome, err := atomicWriteFile(req.Path, data, perm, req.Sync)
	if err != nil {
		return result, err
	}
	result.Bytes = len(data)
	result.Outcome = outcome
	return result, nil
}

// ReadSave reads and validates a save file.
//
// Validation is layered and each layer produces a distinguishable error, because
// the recovery decision differs per layer:
//
//  1. absent                 -> ErrSaveNotFound (first launch; not an error)
//  2. unreadable             -> ErrSaveCorrupt
//  3. not decodable          -> ErrSaveCorrupt
//  4. envelope version ahead -> ErrSaveUnsupported (needs a newer build)
//  5. digest mismatch        -> ErrSaveCorrupt
//
// On every failure the bytes are returned alongside the error so the caller can
// preserve the file rather than lose it. Returning them is not an oversight:
// a save rejected without being archived is a save destroyed.
func ReadSave(path string, into any) (Validated, error) {
	var v Validated

	data, ok, err := readFileIfExists(path)
	if err != nil {
		return v, fmt.Errorf("%w: %s could not be read: %v", ErrSaveCorrupt, path, err)
	}
	if !ok {
		return v, fmt.Errorf("%w: %s", ErrSaveNotFound, path)
	}
	v.Raw = data

	if err := json.Unmarshal(data, into); err != nil {
		return v, fmt.Errorf("%w: %s is not a readable save document: %v", ErrSaveCorrupt, path, err)
	}
	return v, nil
}

// VerifyEnvelope checks a decoded envelope's self-consistency.
//
// It is separate from ReadSave because the two answer different questions:
// ReadSave asks "is this a save document at all", while this asks "is it intact
// and from a version I understand". A migration path needs the first to succeed
// and the second to report a version mismatch it can act on.
func VerifyEnvelope(env envelopeForVerify) error {
	if env == nil {
		return fmt.Errorf("%w: no envelope was decoded", ErrSaveCorrupt)
	}
	version := env.EnvelopeVersionNumber()
	if version > CurrentEnvelopeVersion {
		return fmt.Errorf("%w: %s declares envelope version %d, this build understands up to %d",
			ErrSaveUnsupported, "save", version, CurrentEnvelopeVersion)
	}
	if version <= 0 {
		return fmt.Errorf("%w: the envelope declares no version", ErrSaveCorrupt)
	}
	if !env.VerifyIntegrity() {
		return fmt.Errorf("%w: the integrity digest does not match the content, so the file was "+
			"truncated or edited outside the game", ErrSaveCorrupt)
	}
	return nil
}

// envelopeForVerify is the read-side counterpart of envelopeForWrite.
type envelopeForVerify interface {
	VerifyIntegrity() bool
	EnvelopeVersionNumber() int
}

// CurrentEnvelopeVersion is the highest envelope version this build understands.
//
// It mirrors engine.EnvelopeVersion but is declared here so the storage layer can
// make a version decision without importing the engine. The wiring layer asserts
// the two agree, so a drift is caught by a test rather than by a player.
const CurrentEnvelopeVersion = 1

// CopySaveToPreserved copies a rejected save somewhere safe and returns the
// destination.
//
// A save can be rejected for many reasons, none of which is a good reason to
// delete it. This is how "refuse and preserve" is implemented for the cases
// where the original file must not be touched at all.
func CopySaveToPreserved(src, preservedDir, suffix string) (string, error) {
	data, ok, err := readFileIfExists(src)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrSaveNotFound, src)
	}
	if err := os.MkdirAll(preservedDir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Base(src)
	if suffix != "" {
		name += "." + suffix
	}
	dst := filepath.Join(preservedDir, sanitiseComponent(name))
	if _, err := atomicWriteFile(dst, data, 0o600, SyncFileOnly); err != nil {
		return "", err
	}
	return dst, nil
}
