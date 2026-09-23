#!/usr/bin/env sh
# check-gofmt.sh - report real gofmt problems in Go source, ignoring the
# repository's CRLF line endings.
#
# On Windows the working tree uses CRLF, and gofmt always emits LF, so plain
# `gofmt -l` flags every file and drowns the signal. This script normalises
# line endings into a scratch directory inside the repo (never /tmp: native Go
# binaries cannot resolve MSYS /tmp paths) and runs gofmt there.
#
# Usage: sh scripts/check-gofmt.sh
# Exit: 0 when every file is correctly formatted, 1 otherwise.

set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
scratch="$repo_root/.task-cache/gofmt-check"

rm -rf "$scratch"
mkdir -p "$scratch"

# Collect .go files, excluding the build output and the scratch directory.
find "$repo_root" \
    -name '*.go' \
    -not -path "$repo_root/.task-cache/*" \
    -not -path "$repo_root/dist/*" \
    -print > "$scratch/files.txt"

if [ ! -s "$scratch/files.txt" ]; then
    echo "no Go files found"
    exit 0
fi

# Normalise each file into the scratch tree, preserving relative paths.
while IFS= read -r f; do
    rel=${f#"$repo_root/"}
    mkdir -p "$scratch/$(dirname "$rel")"
    tr -d '\r' < "$f" > "$scratch/$rel"
done < "$scratch/files.txt"

cd "$scratch"
if out=$(gofmt -l . 2>&1) && [ -z "$out" ]; then
    echo "gofmt: all Go files correctly formatted"
    exit 0
fi

echo "gofmt: the following files need formatting:"
echo "$out"
echo ""
echo "--- diffs ---"
gofmt -d . 2>&1 | head -120
exit 1
