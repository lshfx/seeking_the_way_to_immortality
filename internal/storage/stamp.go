package storage

import (
	"strconv"
	"time"
)

// nowStamp is the clock used for timestamped filenames. It is a variable so a
// test can pin it and assert the exact name produced, rather than accepting
// whatever the wall clock happened to say.
var nowStamp = time.Now

// timeStampSuffix returns a filesystem-safe UTC timestamp for naming a preserved
// copy.
//
// It uses UTC and a fixed-width layout because these names appear in directory
// listings a user may have to read and sort. Local time would make two runs in
// different sessions order confusingly, and a variable-width month or day would
// break lexicographic ordering.
func timeStampSuffix() string {
	return nowStamp().UTC().Format("20060102T150405Z")
}

// RevisionSuffix renders a revision for a diagnostic filename.
func RevisionSuffix(revision uint64) string {
	return "rev" + strconv.FormatUint(revision, 10)
}
