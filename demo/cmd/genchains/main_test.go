package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// committedDir is the chain directory this command writes, relative to this
// package.
const committedDir = "../../chains"

// TestCommittedChainsAreUpToDate is the demo's version of the conformance
// test: the bytes under demo/chains must be the bytes this command produces
// today.
//
// A failure here is never fixed by editing a .block file. It means the demo's
// content, its chain layout, or a canonical encoding in the library moved, and
// the fix is to run
//
//	go run ./cmd/genchains
//
// and to review the diff. Every digest in the directory is derived from the
// bytes, so a diff means every reader of the committed chains sees different
// identifiers than before.
func TestCommittedChainsAreUpToDate(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatalf("building the chains: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("the generator produced no files")
	}
	written := make(map[string]bool, len(files))
	for _, f := range files {
		written[filepath.FromSlash(f.Path)] = true
		path := filepath.Join(committedDir, filepath.FromSlash(f.Path))
		have, err := os.ReadFile(path) //nolint:gosec // a fixed directory of committed test data.
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is missing; run `go run ./cmd/genchains`", f.Path)
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !bytes.Equal(have, f.Bytes) {
			t.Errorf("%s is stale: the committed file is %d bytes and the generator produces %d; run `go run ./cmd/genchains`",
				f.Path, len(have), len(f.Bytes))
		}
	}

	// The other direction: a file the generator no longer produces would sit
	// in the directory unnoticed, and the loader would read a world the
	// generator cannot reproduce.
	err = filepath.WalkDir(committedDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) == ".go" {
			return nil
		}
		rel, err := filepath.Rel(committedDir, path)
		if err != nil {
			return err
		}
		if !written[rel] {
			t.Errorf("%s is in the chain directory but the generator does not produce it", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", committedDir, err)
	}
}
