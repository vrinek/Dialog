// Package vectors builds Dialog's conformance test vectors: the
// language-agnostic JSON files under vectors/ at the root of this repository.
//
// The vectors are the interop contract. Every byte in them is derived here
// from the packages of this module — nothing is transcribed by hand — and
// every input is a fixed, documented constant, so Build is a pure function:
// the same source produces the same bytes on every run, on every machine.
// cmd/genvectors writes what Build returns; the conformance test reads the
// committed files back and checks that the implementation still reproduces
// them. A diff in vectors/ is therefore never noise: it is a change to the
// bytes another implementation must match.
//
// The JSON shape lives in internal/vectorfile, which imports nothing of the
// protocol, so that the packages verified here can read the committed files
// without importing their own generator.
//
// This package is internal. Its output, not its API, is the deliverable.
package vectors

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/internal/vectorfile"
)

// Format is the version tag every vectors file carries.
const Format = vectorfile.Format

// The file schema, aliased so that the builders below read as one package.
type (
	Document    = vectorfile.Document
	Section     = vectorfile.Section
	File        = vectorfile.File
	Value       = vectorfile.Value
	Entry       = vectorfile.Entry
	DCBORCase   = vectorfile.DCBORCase
	InvalidCase = vectorfile.InvalidCase
	EntityCase  = vectorfile.EntityCase
	FillerCase  = vectorfile.FillerCase
	KeyCase     = vectorfile.KeyCase
	BlockInputs = vectorfile.BlockInputs
	BlockCase   = vectorfile.BlockCase
	ForkCase    = vectorfile.ForkCase

	PrivacyInputs = vectorfile.PrivacyInputs
	PrivacyCase   = vectorfile.PrivacyCase
	X25519Case    = vectorfile.X25519Case
	WrapCase      = vectorfile.WrapCase
)

// Build derives the whole vector set. The order of the returned files is
// fixed, and so is every byte in them.
func Build() ([]File, error) {
	files := []File{{Name: "dcbor.json", Doc: dcborDocument()}}
	for _, build := range []func() (File, error){entitiesFile, blocksFile, privacyFile} {
		f, err := build()
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// Names lists the file names Build produces, in order.
func Names() []string {
	return []string{"dcbor.json", "entities.json", "blocks.json", "privacy.json"}
}

// describe converts a dCBOR value into its JSON model. It panics on a value
// outside the profile, which cannot occur: every value it is given came from
// this module's encoder or decoder.
func describe(v dcbor.Value) Value {
	switch t := v.(type) {
	case dcbor.Uint:
		return Value{Type: "uint", Number: strconv.FormatUint(uint64(t), 10)}
	case dcbor.Neg:
		// Neg n denotes -1-n, which is why the string is built from the
		// negative value and not from the encoded argument.
		if i, ok := t.Int64(); ok {
			return Value{Type: "neg", Number: strconv.FormatInt(i, 10)}
		}
		// Below -2^63: "-(n+1)" is the only exact decimal form left.
		return Value{Type: "neg", Number: "-" + addOne(uint64(t))}
	case dcbor.Decimal:
		return Value{
			Type:     "decimal",
			Exponent: strconv.FormatInt(t.Exponent, 10),
			Mantissa: strconv.FormatInt(t.Mantissa, 10),
		}
	case dcbor.Text:
		return Value{Type: "text", Text: string(t)}
	case dcbor.Bytes:
		return Value{Type: "bytes", Bytes: hex.EncodeToString(t)}
	case dcbor.Array:
		out := Value{Type: "array"}
		for _, item := range t {
			out.Items = append(out.Items, describe(item))
		}
		return out
	case dcbor.Map:
		// The entries are listed in canonical order, which is the order
		// dcbor.Encode writes them in and not necessarily the order the map
		// was built in. Encoding and re-decoding is the shortest way to get it
		// and keeps this model honest about what the bytes say.
		canonical, err := dcbor.Decode(dcbor.MustEncode(t))
		if err != nil { // unreachable: MustEncode produced the bytes
			panic(fmt.Sprintf("vectors: canonical map: %v", err))
		}
		m, ok := canonical.(dcbor.Map)
		if !ok { // unreachable: a map encodes to a map
			panic("vectors: a map did not decode to a map")
		}
		out := Value{Type: "map"}
		for _, e := range m {
			out.Entries = append(out.Entries, Entry{Key: e.Key, Value: describe(e.Value)})
		}
		return out
	case dcbor.NullValue:
		return Value{Type: "null"}
	default:
		panic(fmt.Sprintf("vectors: %T is outside the dCBOR profile", v))
	}
}

// describePointer is describe for the optional Value of a privacy case.
func describePointer(v dcbor.Value) *Value {
	d := describe(v)
	return &d
}

// addOne returns the decimal form of n+1 for the one case where it overflows
// uint64: the most negative CBOR integer, -2^64.
func addOne(n uint64) string {
	if n != ^uint64(0) {
		return strconv.FormatUint(n+1, 10)
	}
	return "18446744073709551616"
}

// hexOf encodes bytes for a vector field. Every byte string in the files is
// lowercase hex with no separators.
func hexOf(b []byte) string { return hex.EncodeToString(b) }

// mustHexBytes reads back a hex field this package itself produced.
func mustHexBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil { // unreachable: hexOf wrote it
		panic(fmt.Sprintf("vectors: %q is not hex: %v", s, err))
	}
	return b
}
