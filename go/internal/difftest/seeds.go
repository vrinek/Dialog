package difftest

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vrinek/Dialog/go/internal/vectorfile"
)

// vectorsFile is the committed dCBOR conformance vector file, relative to the
// repository root. It is read, never copied: the vectors are the interop
// contract, and a second copy of them inside a fuzzing harness would be a
// second contract that nothing keeps current.
const vectorsFile = "vectors/dcbor.json"

// validSections are the sections of vectors/dcbor.json whose cases carry a
// byte string a conforming decoder MUST accept. The "invalid" section carries
// the ones it MUST reject, and is read separately because its cases have a
// different shape.
var validSections = []string{"encoding_reference", "canonical", "decimal_fractions"}

// A Seed is one byte string from the conformance vectors, with enough context
// for a fuzz failure to name where it came from.
type Seed struct {
	// Name is "<section>/<case>".
	Name string
	// Bytes is the byte string itself.
	Bytes []byte
	// Valid reports whether the vectors say a conforming decoder accepts it.
	Valid bool
	// Rule is the specification rule an invalid case violates; empty for a
	// valid one.
	Rule string
}

// VectorSeeds returns every byte string vectors/dcbor.json pins, valid and
// invalid alike.
//
// Both fuzz targets are seeded from it, which means the ordinary `go test`
// run — the one CI does, with no fuzzing budget — already replays the whole
// conformance corpus through both comparisons. The invalid cases matter as
// much as the valid ones and arguably more: each is a byte string Dialog
// rejects for a named rule, so each is a direct test of whether the oracle
// rejects it too or whether the divergence is in the allowlist.
func VectorSeeds() ([]Seed, error) {
	path, err := vectorsPath()
	if err != nil {
		return nil, err
	}
	doc, err := vectorfile.Read(path)
	if err != nil {
		return nil, err
	}

	var seeds []Seed
	for _, name := range validSections {
		section, ok := doc.Section(name)
		if !ok {
			return nil, fmt.Errorf("difftest: %s has no %q section", vectorsFile, name)
		}
		cases, err := vectorfile.DecodeCases[vectorfile.DCBORCase](section)
		if err != nil {
			return nil, err
		}
		if len(cases) == 0 {
			return nil, fmt.Errorf("difftest: %s section %q is empty", vectorsFile, name)
		}
		for _, tc := range cases {
			b, err := decodeHex(name, tc.Name, tc.DCBOR)
			if err != nil {
				return nil, err
			}
			seeds = append(seeds, Seed{Name: name + "/" + tc.Name, Bytes: b, Valid: true})
		}
	}

	section, ok := doc.Section("invalid")
	if !ok {
		return nil, fmt.Errorf("difftest: %s has no %q section", vectorsFile, "invalid")
	}
	invalid, err := vectorfile.DecodeCases[vectorfile.InvalidCase](section)
	if err != nil {
		return nil, err
	}
	if len(invalid) == 0 {
		return nil, fmt.Errorf("difftest: %s section %q is empty", vectorsFile, "invalid")
	}
	for _, tc := range invalid {
		b, err := decodeHex("invalid", tc.Name, tc.Bytes)
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, Seed{Name: "invalid/" + tc.Name, Bytes: b, Rule: tc.Rule})
	}
	return seeds, nil
}

func decodeHex(section, name, s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return nil, fmt.Errorf("difftest: %s/%s: %q is not hex: %w", section, name, s, err)
	}
	return b, nil
}

// vectorsPath locates vectors/dcbor.json from this source file rather than
// from the working directory, so that the seeds are found whichever package
// inside this module asks for them.
func vectorsPath() (string, error) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("difftest: cannot locate this package's source directory")
	}
	// go/internal/difftest/seeds.go -> the repository root is three levels up.
	root := filepath.Join(filepath.Dir(self), "..", "..", "..")
	return filepath.Join(root, filepath.FromSlash(vectorsFile)), nil
}
