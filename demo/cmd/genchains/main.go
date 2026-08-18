// Command genchains writes the grounding demo's three author chains to
// demo/chains.
//
// Usage:
//
//	go run ./cmd/genchains            # write ./chains
//	go run ./cmd/genchains -o DIR     # write DIR
//	go run ./cmd/genchains -check     # do not write; report stale files
//
// The output is deterministic: the signing keys come from fixed seeds, the
// block timestamps from a fixed base, Ed25519 signatures are deterministic and
// the encoding is canonical dCBOR. Running it twice produces identical bytes,
// so `git diff --exit-code chains/` after a run is a meaningful check — a diff
// means the demo's content, its chain layout or the library's canonical bytes
// changed, and a reader of the committed chains would get different digests.
//
// Every block is validated as it is signed. The committed files are therefore
// a world a node would accept, and the loader in internal/replay validates
// them again from the bytes.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vrinek/Dialog/demo/internal/chainfile"
	"github.com/vrinek/Dialog/demo/internal/publish"
)

func main() {
	dir := flag.String("o", "chains", "directory to write the chains to")
	check := flag.Bool("check", false, "do not write; report whether the committed files are up to date")
	flag.Parse()

	if err := run(*dir, *check); err != nil {
		fmt.Fprintln(os.Stderr, "genchains:", err)
		os.Exit(1)
	}
}

func run(dir string, check bool) error {
	files, err := Files()
	if err != nil {
		return err
	}
	var stale []string
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		have, err := os.ReadFile(path) //nolint:gosec // the path is this program's own output directory joined with a name it chose.
		switch {
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return err
		case err != nil || !bytes.Equal(have, f.Bytes):
			stale = append(stale, f.Path)
		}
		if check {
			continue
		}
		// These are committed source files in a public repository, not private
		// data; they get the permissions the rest of the checkout has.
		//nolint:gosec // G301: a directory of committed files.
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		//nolint:gosec // G306: see above.
		if err := os.WriteFile(path, f.Bytes, 0o644); err != nil {
			return err
		}
	}
	if check {
		if len(stale) > 0 {
			return fmt.Errorf("%s is stale; run `go run ./cmd/genchains` and commit the result:\n\t%s",
				dir, strings.Join(stale, "\n\t"))
		}
		fmt.Printf("%s is up to date (%d files)\n", dir, len(files))
		return nil
	}
	fmt.Printf("wrote %d files to %s\n", len(files), dir)
	return nil
}

// Files builds the chains and renders them as the files of a chain directory.
// It is the whole of this command's work, exported within the package so that
// the regeneration test can compare its output against the committed bytes.
func Files() ([]chainfile.File, error) {
	chains, _, err := publish.Build()
	if err != nil {
		return nil, err
	}
	out := make([]chainfile.Chain, 0, len(chains))
	for _, c := range chains {
		out = append(out, chainfile.Chain{Author: c.Author, Pub: c.Pub, Blocks: c.Blocks})
	}
	return chainfile.Build(out)
}
