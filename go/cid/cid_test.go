package cid

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// The worked example of spec/03-encoding.md, "Encoding an atom".
const (
	exampleCBOR   = "a16b6465736372697074696f6e664672616e6365"
	exampleDigest = "e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842"
	exampleCID    = "01711220" + exampleDigest
)

// TestSpecWorkedExample reproduces the atom example of spec/03-encoding.md
// end to end: dCBOR bytes, SHA-256 digest, and CID.
func TestSpecWorkedExample(t *testing.T) {
	encoded, err := dcbor.Encode(dcbor.Map{{Key: "description", Value: dcbor.Text("France")}})
	if err != nil {
		t.Fatalf("dcbor.Encode: %v", err)
	}
	if got := hex.EncodeToString(encoded); got != exampleCBOR {
		t.Fatalf("step 1, dCBOR = %s, want %s", got, exampleCBOR)
	}

	digest := SumDigest(encoded)
	if got := digest.String(); got != exampleDigest {
		t.Fatalf("step 2, SHA-256 = %s, want %s", got, exampleDigest)
	}

	c := digest.CID()
	if got := c.String(); got != exampleCID {
		t.Fatalf("step 3, CID = %s, want %s", got, exampleCID)
	}
	if got := SumCID(encoded); got != c {
		t.Errorf("SumCID = %s, want %s", got, c)
	}
	if got := c.Digest(); got != digest {
		t.Errorf("CID.Digest round trip = %s, want %s", got, digest)
	}
}

func TestCIDLayout(t *testing.T) {
	c := SumCID(nil)
	if len(c) != 36 {
		t.Errorf("CID size = %d, want 36", len(c))
	}
	want := []struct {
		name string
		i    int
		b    byte
	}{
		{"version", 0, 0x01},
		{"codec", 1, 0x71},
		{"hash function", 2, 0x12},
		{"digest length", 3, 0x20},
	}
	for _, w := range want {
		if c[w.i] != w.b {
			t.Errorf("%s byte = 0x%02x, want 0x%02x", w.name, c[w.i], w.b)
		}
	}
}

func TestSumDigestKnownVectors(t *testing.T) {
	tests := []struct {
		name string
		in   string // hex
		want string
	}{
		{"empty input", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"atom example", exampleCBOR, exampleDigest},
		{"dCBOR null", "f6", "b0b2988b6bbe724bacda5e9e524736de0bc7dae41c46b4213c50e1d35d4e5f13"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in, err := hex.DecodeString(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := SumDigest(in).String(); got != tc.want {
				t.Errorf("SumDigest = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMultihash(t *testing.T) {
	d, err := ParseDigestHex(exampleDigest)
	if err != nil {
		t.Fatal(err)
	}
	mh := d.Multihash()
	if len(mh) != MultihashSize {
		t.Fatalf("multihash size = %d, want %d", len(mh), MultihashSize)
	}
	want := "1220" + exampleDigest
	if got := hex.EncodeToString(mh); got != want {
		t.Errorf("multihash = %s, want %s", got, want)
	}
}

func TestParseCIDRejectsWrongParameters(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want string
	}{
		{"wrong version (CIDv0-style)", "00711220" + exampleDigest, "CID version"},
		{"wrong version (2)", "02711220" + exampleDigest, "CID version"},
		{"wrong codec (raw)", "01551220" + exampleDigest, "content codec"},
		{"wrong codec (dag-pb)", "01701220" + exampleDigest, "content codec"},
		{"wrong hash (sha2-512)", "01711320" + exampleDigest, "hash function"},
		{"wrong hash (blake3)", "01711e20" + exampleDigest, "hash function"},
		{"wrong digest length byte", "01711210" + exampleDigest, "digest length"},
		{"too short", "01711220" + exampleDigest[:62], "want 36"},
		{"too long", "01711220" + exampleDigest + "00", "want 36"},
		{"bare digest", exampleDigest, "want 36"},
		{"empty", "", "want 36"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseCID(b); err == nil {
				t.Fatalf("ParseCID(%s) accepted invalid parameters", tc.hex)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseCID error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestParseCIDAcceptsValid(t *testing.T) {
	b, err := hex.DecodeString(exampleCID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCID(b)
	if err != nil {
		t.Fatalf("ParseCID: %v", err)
	}
	if c.String() != exampleCID {
		t.Errorf("ParseCID round trip = %s, want %s", c, exampleCID)
	}
	if c.Digest().String() != exampleDigest {
		t.Errorf("Digest = %s, want %s", c.Digest(), exampleDigest)
	}
}

func TestParseHexRoundTrip(t *testing.T) {
	d, err := ParseDigestHex(exampleDigest)
	if err != nil {
		t.Fatalf("ParseDigestHex: %v", err)
	}
	if d.String() != exampleDigest {
		t.Errorf("Digest hex round trip = %s, want %s", d, exampleDigest)
	}

	c, err := ParseCIDHex(exampleCID)
	if err != nil {
		t.Fatalf("ParseCIDHex: %v", err)
	}
	if c != d.CID() {
		t.Errorf("ParseCIDHex = %s, want %s", c, d.CID())
	}
}

func TestParseHexErrors(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) error
		in   string
		want string
	}{
		{"digest odd length", digestHexErr, exampleDigest[:63], "invalid digest hex"},
		{"digest non-hex", digestHexErr, strings.Repeat("zz", 32), "invalid digest hex"},
		{"digest wrong size", digestHexErr, exampleDigest[:60], "want 32"},
		{"CID non-hex", cidHexErr, strings.Repeat("zz", 36), "invalid CID hex"},
		{"CID wrong size", cidHexErr, exampleDigest, "want 36"},
		{"CID wrong prefix", cidHexErr, "01551220" + exampleDigest, "content codec"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn(tc.in)
			if err == nil {
				t.Fatalf("parsing %q should have failed", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func digestHexErr(s string) error { _, err := ParseDigestHex(s); return err }
func cidHexErr(s string) error    { _, err := ParseCIDHex(s); return err }

func TestParseDigestSizes(t *testing.T) {
	if _, err := ParseDigest(make([]byte, 31)); err == nil {
		t.Error("ParseDigest accepted 31 bytes")
	}
	if _, err := ParseDigest(make([]byte, 33)); err == nil {
		t.Error("ParseDigest accepted 33 bytes")
	}
	if _, err := ParseDigest(make([]byte, 32)); err != nil {
		t.Errorf("ParseDigest(32 bytes): %v", err)
	}
}

// TestBytesAreCopies guards against callers mutating a Digest or CID through
// the slice they were handed.
func TestBytesAreCopies(t *testing.T) {
	d := SumDigest([]byte("x"))
	b := d.Bytes()
	b[0] ^= 0xff
	if d.Bytes()[0] == b[0] {
		t.Error("Digest.Bytes aliases the digest")
	}

	c := d.CID()
	cb := c.Bytes()
	cb[0] ^= 0xff
	if c.Bytes()[0] == cb[0] {
		t.Error("CID.Bytes aliases the CID")
	}
}

// TestDigestIsTheInternalReferenceForm documents the encoding of a reference
// inside a Dialog structure: a CBOR byte string, 5820 followed by the digest
// (spec/03-encoding.md, "Internal references").
func TestDigestIsTheInternalReferenceForm(t *testing.T) {
	d, err := ParseDigestHex(exampleDigest)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := dcbor.Encode(dcbor.Bytes(d.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	want := "5820" + exampleDigest
	if got := hex.EncodeToString(encoded); got != want {
		t.Errorf("reference encoding = %s, want %s", got, want)
	}
}
