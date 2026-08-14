package block

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// TestPayloadRoundTrip encodes and decodes the three fields a private block
// hides (spec/02-block-format.md, "private-block-payload").
func TestPayloadRoundTrip(t *testing.T) {
	provider := cid.SumDigest([]byte("a foreign block"))
	p := Payload{
		Refs: []cid.Digest{provider},
		TS:   1740067200,
		Ops:  []Operation{MustCreateAtom("France"), MustCreateBond("_A_ is the capital of _B_")},
	}
	encoded, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodePayload(encoded)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if got.TS != p.TS || len(got.Refs) != 1 || got.Refs[0] != provider || len(got.Ops) != 2 {
		t.Fatalf("round trip = %+v, want %+v", got, p)
	}
	first, ok := got.Ops[0].(CreateAtom)
	if !ok {
		t.Fatalf("first operation is %T, want CreateAtom", got.Ops[0])
	}
	if first.Description() != "France" {
		t.Errorf("first operation = %v, want create_atom(\"France\")", got.Ops[0])
	}

	// The payload's fields are encoded exactly as a public block encodes the
	// same three, which is the point of having one encoder: the payload bytes
	// are a subset of the signing input's bytes.
	c := Content{Version: Version, Type: TypePublic, Pub: make([]byte, PublicKeySize), Refs: p.Refs, TS: p.TS, Ops: p.Ops}
	signing, err := c.SigningBytes()
	if err != nil {
		t.Fatalf("SigningBytes: %v", err)
	}
	for _, e := range p.Value() {
		field := hex.EncodeToString(dcbor.MustEncode(dcbor.Map{e}))[2:] // drop the a1 map head
		if !strings.Contains(hex.EncodeToString(signing), field) {
			t.Errorf("the %q field of a payload is not encoded as a public block encodes it", e.Key)
		}
	}
}

// TestPayloadRejections covers what a key holder must refuse in a decrypted
// payload.
func TestPayloadRejections(t *testing.T) {
	d := cid.SumDigest([]byte("a block"))
	atom := MustCreateAtom("France")

	t.Run("duplicate refs", func(t *testing.T) {
		// The structural half of rule 10 applies to an encrypted refs list
		// exactly as to a plaintext one (spec/02-block-format.md, "The refs
		// list").
		if _, err := (Payload{Refs: []cid.Digest{d, d}, Ops: []Operation{atom}}).Encode(); err == nil {
			t.Error("a payload listing the same dependency twice must not encode")
		}
	})

	t.Run("no operations", func(t *testing.T) {
		if _, err := (Payload{TS: 1}).Encode(); err == nil {
			t.Error("a payload with no operations must not encode (rule 7)")
		}
	})

	t.Run("rotate_key", func(t *testing.T) {
		// "A party that decrypts the payload MUST reject the block if it finds
		// a rotate_key operation" (spec/02-block-format.md, "Validation
		// dispatch").
		p := Payload{Ops: []Operation{MustRotateKey(make([]byte, PublicKeySize))}}
		if _, err := p.Encode(); err == nil {
			t.Error("a payload carrying a rotate_key operation must not encode")
		}
		encoded := dcbor.MustEncode(dcbor.Map{
			{Key: keyRefs, Value: dcbor.Array{}},
			{Key: keyTS, Value: dcbor.Uint(1)},
			{Key: keyOps, Value: dcbor.Array{MustRotateKey(make([]byte, PublicKeySize)).Value()}},
		})
		if _, err := DecodePayload(encoded); err == nil {
			t.Error("a payload carrying a rotate_key operation must not decode")
		}
	})

	t.Run("wrong shape", func(t *testing.T) {
		for name, encoded := range map[string][]byte{
			"not a map":     dcbor.MustEncode(dcbor.Array{dcbor.Uint(1)}),
			"missing a key": dcbor.MustEncode(dcbor.Map{{Key: keyTS, Value: dcbor.Uint(1)}, {Key: keyOps, Value: dcbor.Array{atom.Value()}}}),
			"an extra key": dcbor.MustEncode(dcbor.Map{
				{Key: keyRefs, Value: dcbor.Array{}},
				{Key: keyTS, Value: dcbor.Uint(1)},
				{Key: keyOps, Value: dcbor.Array{atom.Value()}},
				{Key: keyPub, Value: dcbor.Bytes(make([]byte, PublicKeySize))},
			}),
			"a text timestamp": dcbor.MustEncode(dcbor.Map{
				{Key: keyRefs, Value: dcbor.Array{}},
				{Key: keyTS, Value: dcbor.Text("now")},
				{Key: keyOps, Value: dcbor.Array{atom.Value()}},
			}),
			"trailing bytes": append(dcbor.MustEncode(dcbor.Map{
				{Key: keyRefs, Value: dcbor.Array{}},
				{Key: keyTS, Value: dcbor.Uint(1)},
				{Key: keyOps, Value: dcbor.Array{atom.Value()}},
			}), 0x00),
		} {
			if _, err := DecodePayload(encoded); err == nil {
				t.Errorf("%s: DecodePayload accepted a payload it must reject", name)
			}
		}
	})
}

// TestValidatePayloadRejectsPlaintextBlocks: a public block's rules are all
// checked by Validate, so there is no payload to hand in.
func TestValidatePayloadRejectsPlaintextBlocks(t *testing.T) {
	author := mustBuilder(t, 1)
	b, err := author.Public(1, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := ValidatePayload(b, Payload{Ops: []Operation{MustCreateAtom("France")}}, NewMemStore(), nil); err == nil {
		t.Error("ValidatePayload must reject a block whose fields are already in the clear")
	}
}
