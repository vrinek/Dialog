package block

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// A Payload is a private block's decrypted content — the three fields a public
// block carries in the clear and a private block hides
// (spec/02-block-format.md, "Private block"):
//
//	private-block-payload = {
//	  "refs" => [* bstr .size 32], ; foreign block references
//	  "ts"   => uint,              ; Unix timestamp
//	  "ops"  => [+ operation]      ; ordered list of operations
//	}
//
// Encode produces exactly the byte string a private block's enc field is the
// ciphertext of, and DecodePayload consumes exactly what its decryption
// yields (spec/04-cryptography.md, "Private block encryption"). This package
// does no encryption; the privacy package supplies the key and the AEAD.
//
// The same three fields are encoded the same way in a public block's signing
// input — Content.SigningValue builds the plaintext part from a Payload — so
// there is one encoder for them and one decoder, and a private block's
// payload cannot drift from a public block's fields.
type Payload struct {
	// Refs lists the CID-providing blocks this block's operations depend on.
	Refs []cid.Digest
	// TS is the author's self-reported Unix timestamp, in seconds. It is
	// untrusted, and hidden from non-recipients precisely so that it cannot be
	// correlated (spec/02-block-format.md, "Security Considerations").
	TS uint64
	// Ops are the block's operations, in order.
	Ops []Operation
}

// Clone returns a deep copy of p. Operations are immutable values, so the
// slice is copied but its elements are shared.
func (p Payload) Clone() Payload {
	p.Refs = slices.Clone(p.Refs)
	p.Ops = slices.Clone(p.Ops)
	return p
}

// Validate reports whether p is a structurally well-formed private payload:
// a refs list that names each dependency once, at least one operation, and no
// rotate_key operation.
//
// All three are rules a holder of the decryption key must enforce and nobody
// else can. The duplicate check is the structural half of validation rule 10,
// which "applies to a private block's encrypted refs exactly as [it does] to a
// plaintext one, and [is] checked by the parties that decrypt it"
// (spec/02-block-format.md, "The refs list"). The non-empty check is rule 7.
// The rotate_key check is the one spec/02-block-format.md, "Validation
// dispatch", states in as many words: "a party that decrypts the payload MUST
// reject the block if it finds a rotate_key operation" — a chain ends where the
// type field says it ends, not inside a ciphertext.
//
// The other half of rule 10, and rules 4, 5 and 6, need other blocks; they are
// ValidatePayload's business.
func (p Payload) Validate() error {
	if err := uniqueRefs(p.Refs); err != nil {
		return err
	}
	if len(p.Ops) == 0 {
		return fmt.Errorf("block: the decrypted payload has no operations; %q must hold at least one", keyOps)
	}
	for i, op := range p.Ops {
		if op == nil {
			return fmt.Errorf("block: decrypted payload operation %d is nil", i)
		}
		if _, ok := op.(RotateKey); ok {
			return fmt.Errorf("block: decrypted payload operation %d is %s; a rotate_key operation may appear only in a rotation block, which every node can read", i, OpRotateKey)
		}
	}
	return nil
}

// Value returns the payload as a dCBOR map. Entry order is irrelevant —
// dcbor.Encode sorts the keys into the canonical bytewise order.
func (p Payload) Value() dcbor.Map {
	refs := make(dcbor.Array, 0, len(p.Refs))
	for _, r := range p.Refs {
		refs = append(refs, dcbor.Bytes(r.Bytes()))
	}
	ops := make(dcbor.Array, 0, len(p.Ops))
	for _, op := range p.Ops {
		ops = append(ops, op.Value())
	}
	return dcbor.Map{
		{Key: keyRefs, Value: refs},
		{Key: keyTS, Value: dcbor.Uint(p.TS)},
		{Key: keyOps, Value: ops},
	}
}

// Encode returns the canonical dCBOR encoding of the payload — the plaintext a
// private block's enc field is the ciphertext of.
func (p Payload) Encode() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return dcbor.Encode(p.Value())
}

// DecodePayload parses the plaintext recovered from a private block's enc
// field.
//
// The dCBOR profile applies to the payload exactly as it applies to the block
// that carries it: unsorted or duplicate keys, non-shortest integers, floats,
// indefinite lengths, unexpected tags and trailing bytes are all rejected, the
// map is closed on its three declared keys, and every operation is validated
// against its own closed definition. A ciphertext that decrypts under the right
// key is authentic, not well-formed; an author whose encoder is wrong produces
// a block whose payload only its recipients can reject, which is a reason to
// check strictly rather than a reason not to.
func DecodePayload(b []byte) (Payload, error) {
	v, err := dcbor.Decode(b)
	if err != nil {
		return Payload{}, fmt.Errorf("block: decrypted payload is not valid dCBOR: %w", err)
	}
	m, ok := v.(dcbor.Map)
	if !ok {
		return Payload{}, fmt.Errorf("block: a decrypted payload must be a CBOR map, got %s", kindOf(v))
	}
	if err := requireKeys(m, "private block payload", keyRefs, keyTS, keyOps); err != nil {
		return Payload{}, err
	}

	var p Payload
	if p.Refs, err = refsField(m); err != nil {
		return Payload{}, err
	}
	if p.TS, err = uintField(m, keyTS, "private block payload"); err != nil {
		return Payload{}, err
	}
	if p.Ops, err = opsField(m); err != nil {
		return Payload{}, err
	}
	if err := p.Validate(); err != nil {
		return Payload{}, err
	}
	// dcbor.Decode has already rejected non-canonical bytes, so re-encoding
	// must reproduce the input exactly; the comparison turns that invariant
	// into a check, as Decode does for a block.
	encoded, err := p.Encode()
	if err != nil {
		return Payload{}, err
	}
	if !bytes.Equal(encoded, b) {
		return Payload{}, fmt.Errorf("block: the decrypted payload is not the canonical dCBOR encoding of the payload it decodes to")
	}
	return p, nil
}

// ValidatePayload runs the four validation rules of spec/02-block-format.md
// that a private block leaves unchecked — 4, 5, 6 and 10 — against the payload
// a holder of the decryption key has recovered.
//
// Validate does everything a node without the key can do (rules 1, 2, 3, 7, 8
// and 9) and lists these four in the report's Unchecked field. This is the
// other half: "For private blocks, validation of rules 4, 5, 6, and 10 is only
// possible by entities that hold the decryption key." Together the two calls
// are a complete validation of a private block.
//
// Rule 6 places no constraint on a private block's refs — they may name a block
// of any type (spec/02-block-format.md, "Validation" rule 6) — so what this
// pass adds for rule 6 is nothing, and for rule 10 the own-chain half. Rules 4
// and 5 are the substance: every entity digest the decrypted operations carry
// must be reachable, and must resolve to an entity of the kind its position
// names.
//
// A referenced block that is itself private contributes no definitions unless
// the caller supplies a Decrypter for it, since this package holds no keys. If
// a digest needs one of those definitions, rule 4's verdict is *not
// determinable*: the error wraps ErrUndecryptable and answers true to
// IsUnvalidated, so the block is stored but unvalidated and never invalid,
// which is what spec/05-processing-model.md, "Undecryptable reference
// handling", requires of a node that can read one block but not the one it
// depends on. A key can be wrapped for a further recipient at any time, so the
// question stays open; a caller holding the other chain's key passes it in
// through Options.Decrypter and the same call decides.
func ValidatePayload(b *Block, p Payload, src Source, opts *Options) (*Report, error) {
	if b == nil {
		return nil, fmt.Errorf("block: ValidatePayload called with a nil block")
	}
	if b.content.Type != TypePrivate {
		return nil, fmt.Errorf("block: %s is a %s block; its refs, ts and ops are already in the clear, so Validate checks every rule", b.CID(), b.content.Type)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	report := &Report{}
	if err := validateReferences(b, p.Refs, p.Ops, src, opts, report); err != nil {
		return nil, err
	}
	return report, nil
}
