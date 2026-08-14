// Command genvectors writes Dialog's conformance test vectors to vectors/ at
// the root of this repository.
//
// Usage:
//
//	go run ./cmd/genvectors            # write ../vectors
//	go run ./cmd/genvectors -o DIR     # write DIR
//	go run ./cmd/genvectors -check     # exit non-zero if the files are stale
//
// The output is deterministic: no timestamps, no randomness, every key and
// nonce a documented constant. Running it twice produces identical bytes, so
// `git diff --exit-code vectors/` after a run is a meaningful check — a diff
// means the canonical bytes of the protocol changed, which is a breaking
// change for every implementation that reads them.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vrinek/Dialog/go/internal/vectors"
)

func main() {
	dir := flag.String("o", filepath.Join("..", "vectors"), "directory to write the vectors to")
	check := flag.Bool("check", false, "do not write; report whether the committed files are up to date")
	flag.Parse()

	if err := run(*dir, *check); err != nil {
		fmt.Fprintln(os.Stderr, "genvectors:", err)
		os.Exit(1)
	}
}

func run(dir string, check bool) error {
	files, err := vectors.Build()
	if err != nil {
		return err
	}
	if !check {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	var stale []string
	for _, f := range files {
		want, err := f.JSON()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, f.Name)
		got, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Equal(got, want) {
			continue
		}
		if check {
			stale = append(stale, f.Name)
			continue
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	if len(stale) > 0 {
		return fmt.Errorf("out of date: %v; run `go run ./cmd/genvectors` and review the diff", stale)
	}
	if check {
		fmt.Println("vectors are up to date")
	}
	return nil
}
