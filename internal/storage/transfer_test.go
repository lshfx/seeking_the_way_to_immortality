package storage

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeVerify rebuilds the checks ValidateImport delegates to the wiring layer.
// It reads the envelope version and the digest out of the generic map, mirroring
// what a typed codec would do. The tests assert on the *store's* decisions, not
// on this helper, so a bug here would show up as a test that passes for the
// wrong reason only if it also satisfied the digest -- which it does not.
func fakeVerify(doc map[string]any) (int, bool, string) {
	version, _ := doc["envelope_version"].(float64)
	digest, _ := doc["digest"].(string)
	payload, _ := doc["payload"].(string)
	gameID, _ := doc["game_id"].(string)
	revision, _ := doc["revision"].(float64)

	want := "d:" + payload + ":" + gameID + ":" + itoa64(uint64(revision))
	if digest != want {
		return int(version), false, "the digest does not match the payload"
	}
	return int(version), true, ""
}

func itoa64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// writeExported writes a save the way ExportSave would, so import tests read a
// document with the same shape as a real export.
func writeExported(t *testing.T, dir, name string, env *fakeEnvelope) string {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("cannot encode the fixture: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("cannot write the fixture: %v", err)
	}
	return path
}

// TestValidateImportRejectsAMissingFile pins that a path that does not exist is
// a refusal, not an empty success that would then clear the current save.
func TestValidateImportRejectsAMissingFile(t *testing.T) {
	_, err := ValidateImport(filepath.Join(t.TempDir(), "absent.json"), fakeVerify)
	if !errors.Is(err, ErrImportRefused) {
		t.Fatalf("error = %v, want ErrImportRefused", err)
	}
	if !errors.Is(err, ErrSaveNotFound) {
		t.Errorf("the error does not say the file was absent: %v", err)
	}
}

// TestValidateImportRejectsNonJSON is the "picking a photo instead of a save"
// case. It must be refused with an explanation rather than crashing.
func TestValidateImportRejectsNonJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notasave.json")
	if err := os.WriteFile(path, []byte("this is not json at all"), 0o600); err != nil {
		t.Fatalf("cannot write the fixture: %v", err)
	}

	_, err := ValidateImport(path, fakeVerify)
	if !errors.Is(err, ErrImportRefused) {
		t.Fatalf("error = %v, want ErrImportRefused", err)
	}
	if !strings.Contains(err.Error(), "not a readable save document") {
		t.Errorf("the error does not explain what is wrong: %v", err)
	}
}

// TestValidateImportRejectsATruncatedTransfer is the reason a digest check
// exists on the import path specifically. A file that travelled through a mail
// client or a chat app can be cut short and still be valid JSON.
func TestValidateImportRejectsATruncatedTransfer(t *testing.T) {
	dir := t.TempDir()
	env := newFakeEnvelope("hero", 1, "a long payload that will be truncated")
	env.Seal()
	path := writeExported(t, dir, "export.json", env)

	// Truncate the payload while keeping the file valid JSON, so only the
	// digest can catch it.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the fixture: %v", err)
	}
	tampered := strings.Replace(string(data), `"a long payload that will be truncated"`,
		`"a long pay"`, 1)
	if tampered == string(data) {
		t.Fatal("the test failed to modify the payload; it would prove nothing")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("cannot rewrite the fixture: %v", err)
	}

	_, err = ValidateImport(path, fakeVerify)
	if !errors.Is(err, ErrImportRefused) {
		t.Fatalf("error = %v, want ErrImportRefused", err)
	}
	if !strings.Contains(err.Error(), "integrity check") {
		t.Errorf("the error does not blame the integrity check: %v", err)
	}
}

// TestValidateImportRejectsAFutureVersionWithTheRightAdvice pins the message
// direction: an import from a newer build needs "update the game", and the error
// must not read as corruption.
func TestValidateImportRejectsAFutureVersionWithTheRightAdvice(t *testing.T) {
	dir := t.TempDir()
	env := newFakeEnvelope("hero", 1, "from the future")
	env.EnvelopeVersion = CurrentEnvelopeVersion + 1
	env.Seal()
	path := writeExported(t, dir, "future.json", env)

	_, err := ValidateImport(path, fakeVerify)
	if !errors.Is(err, ErrImportRefused) {
		t.Fatalf("error = %v, want ErrImportRefused", err)
	}
	if !strings.Contains(err.Error(), "newer version") {
		t.Errorf("the error does not tell the player to update: %v", err)
	}
	if errors.Is(err, ErrSaveCorrupt) {
		t.Error("an import from a newer build was reported as corruption")
	}
}

// TestValidateImportAcceptsAGoodFile is the control. Without it the rejection
// tests above would all pass against a validator that refuses everything.
func TestValidateImportAcceptsAGoodFile(t *testing.T) {
	dir := t.TempDir()
	env := newFakeEnvelope("hero", 7, "a perfectly ordinary save")
	env.Seal()
	path := writeExported(t, dir, "good.json", env)

	result, err := ValidateImport(path, fakeVerify)
	if err != nil {
		t.Fatalf("a good save was refused: %v", err)
	}
	if result.Envelope == nil {
		t.Fatal("no envelope was returned for a good save")
	}
	if result.Source != path {
		t.Errorf("source = %q, want %q", result.Source, path)
	}
	if result.Bytes == 0 {
		t.Error("the byte count was not reported")
	}
}

// TestValidateImportTouchesNothingOnDisk is the actual "a bad import must not
// overwrite the current save" guarantee, expressed structurally.
//
// The test does not check that the current save is unchanged *after* the call.
// That would be a weaker claim: it would still pass if the function wrote and
// then restored. Instead it fingerprints every file in the data root before and
// after, so any write at all is caught -- including one that happens to leave
// the same bytes behind.
//
// It also fingerprints the directory the imported file lives in. Validation has
// no business writing anywhere, and the first version of this test only watched
// the data root -- so a validator that scribbled next to the file it was reading
// would have gone unnoticed. Both trees are checked for that reason.
func TestValidateImportTouchesNothingOnDisk(t *testing.T) {
	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 3, "the live save")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	dataRoot := filepath.Dir(filepath.Dir(filepath.Dir(store.SavePath())))

	// Build a bad import and try to import it.
	badDir := t.TempDir()
	bad := writeExported(t, badDir, "bad.json", newFakeEnvelope("hero", 4, "tampered"))
	raw, err := os.ReadFile(bad)
	if err != nil {
		t.Fatalf("cannot read the fixture: %v", err)
	}
	if err := os.WriteFile(bad, []byte(strings.Replace(string(raw), `"tampered"`, `"changed"`, 1)), 0o600); err != nil {
		t.Fatalf("cannot rewrite the fixture: %v", err)
	}

	for _, root := range []string{dataRoot, badDir} {
		t.Run("watching "+filepath.Base(root), func(t *testing.T) {
			before := treeFingerprint(t, root)
			_, err := ValidateImport(bad, fakeVerify)
			if !errors.Is(err, ErrImportRefused) {
				t.Fatalf("error = %v, want ErrImportRefused", err)
			}
			after := treeFingerprint(t, root)
			// Compare both directions: a file may have been added OR removed,
			// and a length-only check would miss a rename.
			for path, sum := range before {
				got, still := after[path]
				if !still {
					t.Errorf("%s was removed during a refused import", path)
					continue
				}
				if got != sum {
					t.Errorf("%s changed during a refused import", path)
				}
			}
			for path := range after {
				if _, existed := before[path]; !existed {
					t.Errorf("%s was created during a refused import; validation must not write",
						path)
				}
			}
		})
	}

	// And the live save is still the live save.
	loaded, err := store.decodeFile(store.SavePath())
	if err != nil {
		t.Fatalf("the live save became unreadable: %v", err)
	}
	if loaded.Payload != "the live save" {
		t.Errorf("the live save was replaced by a refused import: %q", loaded.Payload)
	}
}

// TestExportSaveRefusesToWriteInsideTheDataRoot is the export-side guard. A file
// dialog that opens in the save folder makes this the easiest destructive
// mistake a player can make.
func TestExportSaveRefusesToWriteInsideTheDataRoot(t *testing.T) {
	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 1, "content")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	dataRoot := filepath.Dir(filepath.Dir(filepath.Dir(store.SavePath())))

	// Every one of these lands inside the data root in a different way.
	hostile := []string{
		store.SavePath(),
		filepath.Join(dataRoot, "settings.json"),
		filepath.Join(dataRoot, "chars", "other", "save.json"),
	}
	for _, dest := range hostile {
		if _, err := ExportSave(store.SavePath(), dest, dataRoot); !errors.Is(err, ErrExportRefused) {
			t.Errorf("exporting to %s was allowed (err = %v); it would overwrite game data",
				dest, err)
		}
	}
}

// TestExportSaveAllowsAPathOutsideTheDataRoot is the control for the guard. A
// check that refuses everything would satisfy the test above and make exporting
// impossible.
func TestExportSaveAllowsAPathOutsideTheDataRoot(t *testing.T) {
	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 1, "portable")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	dataRoot := filepath.Dir(filepath.Dir(filepath.Dir(store.SavePath())))

	dest := filepath.Join(t.TempDir(), "exported.json")
	n, err := ExportSave(store.SavePath(), dest, dataRoot)
	if err != nil {
		t.Fatalf("a legitimate export was refused: %v", err)
	}
	if n == 0 {
		t.Error("the export reported no bytes")
	}

	// It must be a byte-for-byte copy, not a re-encoding.
	orig, _, _ := readFileIfExists(store.SavePath())
	got, ok, _ := readFileIfExists(dest)
	if !ok {
		t.Fatal("no export was written")
	}
	if string(orig) != string(got) {
		t.Errorf("the export is not byte-identical to the stored save:\n stored: %q\n export: %q",
			orig, got)
	}
}

func TestExportSaveReportsAMissingSource(t *testing.T) {
	dir := t.TempDir()
	_, err := ExportSave(filepath.Join(dir, "absent.json"), filepath.Join(dir, "out.json"), "")
	if !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("error = %v, want ErrSaveNotFound", err)
	}
}

// TestExportSaveRefusesAnEmptyDataRoot is a deliberate non-guard, documented so
// its absence is not mistaken for an oversight: with no data root there is
// nothing to protect, and an export must still work.
func TestExportSaveRefusesAnEmptyDataRoot(t *testing.T) {
	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 1, "content")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out.json")
	if _, err := ExportSave(store.SavePath(), dest, ""); err != nil {
		t.Fatalf("an export with no data root was refused: %v", err)
	}
}

// TestImportIntoSaveKeepsTheOutgoingSave is the reversibility guarantee. An
// import that replaces the save without keeping the old one turns a mistake into
// a permanent loss.
func TestImportIntoSaveKeepsTheOutgoingSave(t *testing.T) {
	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 5, "the original")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	imported := newFakeEnvelope("hero", 9, "from another machine")
	result, err := store.ImportIntoSave(imported, "")
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Path != store.SavePath() {
		t.Errorf("the import reported path %q, want %q", result.Path, store.SavePath())
	}

	// The new content is live.
	live, err := store.decodeFile(store.SavePath())
	if err != nil {
		t.Fatalf("the imported save is unreadable: %v", err)
	}
	if live.Payload != "from another machine" {
		t.Errorf("live payload = %q, want the imported content", live.Payload)
	}

	// And the pre-import save is recoverable from the rotation slot.
	prev, err := store.decodeFile(store.PrevSavePath())
	if err != nil {
		t.Fatalf("the pre-import save was not preserved: %v", err)
	}
	if prev.Payload != "the original" {
		t.Errorf("previous payload = %q, want the original content", prev.Payload)
	}
}

func TestImportIntoSaveRefusesNothing(t *testing.T) {
	store := newTestStore(t, "hero")
	if _, err := store.ImportIntoSave(nil, ""); !errors.Is(err, ErrImportRefused) {
		t.Fatalf("error = %v, want ErrImportRefused", err)
	}
	if _, ok, _ := readFileIfExists(store.SavePath()); ok {
		t.Error("a refused import created a save file")
	}
}

// TestImportIntoSaveReportsAKeptCopyFailureWithoutFailing is the "report it but
// do not claim the import failed" case. Telling the player the import failed
// when the save is already in place would make them retry, and a retry would
// rotate the freshly imported save into the previous slot.
func TestImportIntoSaveReportsAKeptCopyFailureWithoutFailing(t *testing.T) {
	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 5, "the original")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// A directory that cannot exist as a file: the kept copy must fail while
	// the import still succeeds.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatalf("cannot create the blocking directory: %v", err)
	}

	result, err := store.ImportIntoSave(newFakeEnvelope("hero", 9, "imported"), blocked)
	if err != nil {
		t.Fatalf("the import failed even though the save was written: %v", err)
	}
	if result.KeptCopyError == nil {
		t.Fatal("the kept-copy failure was not reported; the caller would think the copy exists")
	}

	live, derr := store.decodeFile(store.SavePath())
	if derr != nil {
		t.Fatalf("the imported save is unreadable: %v", derr)
	}
	if live.Payload != "imported" {
		t.Errorf("live payload = %q, want the imported content", live.Payload)
	}
}

// treeFingerprint records every file under root with its size and modification
// time, so a before/after comparison catches any write at all.
//
// Modification time is included deliberately, and its absence was a defect: the
// atomic writer replaces a file with a temp-file rename, so a write that puts
// back the *same bytes* leaves the content identical and is only visible in the
// timestamp. A content-only fingerprint reported "nothing changed" while the file
// was being rewritten -- the same observation-vs-mutation trap this project has
// hit before, where the sampled quantity was equal in both the correct and the
// broken build.
func treeFingerprint(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[path] = fmt.Sprintf("size=%d mtime=%s sha=%x",
			info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano), shaOf(data))
		return nil
	})
	if err != nil {
		t.Fatalf("cannot fingerprint %s: %v", root, err)
	}
	return out
}

// shaOf hashes bytes for the fingerprint. It uses crypto/sha256 rather than the
// package's own digest because the fingerprint must not depend on anything the
// tests are verifying.
func shaOf(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
