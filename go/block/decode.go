package block

import (
	"bytes"
	"fmt"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// Decode parses and validates the canonical dCBOR encoding of a signed block.
//
// It performs every check that the bytes alone support: Dialog's dCBOR profile
// (through dcbor.Decode, which rejects unsorted or duplicate keys, non-shortest
// encodings, floats, indefinite lengths, tags other than 4 and trailing bytes),
// the recognized protocol version, the exact field set of the block's type, the
// size of every fixed-width field, the shape of every operation, the rotation
// block's single rotate_key operation, and the Ed25519 signature over
// "dialog-v1-block" || dCBOR(block without "sig").
//
// Maps are closed: a block or operation map that carries a key its definition
// does not declare, or omits one it declares, is rejected
// (spec/03-encoding.md, "Deterministic CBOR" rule 8, restated for blocks and
// operations in spec/02-block-format.md, "Validation dispatch"). Extra keys are
// never ignored — a later protocol version announces its new fields through v,
// which rule 1 checks first.
//
// What Decode cannot check is everything that depends on other blocks — chain
// linkage, reachability, fork detection. Pass the result to Validate for those
// (spec/02-block-format.md, "Validation" rules 3, 4, 5, 6 and 9).
func Decode(b []byte) (*Block, error) {
	v, err := dcbor.Decode(b)
	if err != nil {
		return nil, fmt.Errorf("block: not valid dCBOR: %w", err)
	}
	m, ok := v.(dcbor.Map)
	if !ok {
		return nil, fmt.Errorf("block: a block must be a CBOR map, got %s", kindOf(v))
	}

	// Rule 1, the version, comes first: every other field's meaning is the
	// version's to define (spec/02-block-format.md, "Validation" rule 1).
	version, err := uintField(m, keyV, "block")
	if err != nil {
		return nil, err
	}
	if version != Version {
		return nil, fmt.Errorf("block: unrecognized protocol version %d, want %d", version, Version)
	}

	typeValue, err := textField(m, keyType, "block")
	if err != nil {
		return nil, err
	}
	blockType := Type(typeValue)
	if !blockType.Valid() {
		return nil, fmt.Errorf("block: unknown block type %q; it must be %q, %q or %q", typeValue, TypePublic, TypePrivate, TypeRotation)
	}

	// The field set is the type's, exactly: no missing key, no extra key.
	keys := []string{keyV, keyType, keyPub, keySig, keyPrev, keyRefs, keyTS, keyOps}
	if blockType == TypePrivate {
		keys = []string{keyV, keyType, keyPub, keySig, keyPrev, keyEnc, keyNonce}
	}
	if err := requireKeys(m, string(blockType)+" block", keys...); err != nil {
		return nil, err
	}

	c := Content{Version: version, Type: blockType}
	if c.Pub, err = bytesField(m, keyPub, "block", PublicKeySize); err != nil {
		return nil, err
	}
	sig, err := bytesField(m, keySig, "block", SignatureSize)
	if err != nil {
		return nil, err
	}
	if c.Prev, err = prevField(m); err != nil {
		return nil, err
	}

	if blockType == TypePrivate {
		encValue, _ := m.Get(keyEnc)
		enc, ok := encValue.(dcbor.Bytes)
		if !ok {
			return nil, fmt.Errorf("block: %q must be a byte string, got %s", keyEnc, kindOf(encValue))
		}
		// enc is opaque here: it is a ciphertext this package does not read.
		// Its one structural constraint is a floor — bstr .size (16..), the
		// Poly1305 tag every XChaCha20-Poly1305 ciphertext carries — which
		// Content.Validate enforces, and which is checkable without the
		// decryption key (spec/02-block-format.md, "Private block"). There is no
		// ceiling: a size limit is local resource policy, not block validity.
		c.Enc = []byte(enc)
		if c.Nonce, err = bytesField(m, keyNonce, "block", NonceSize); err != nil {
			return nil, err
		}
	} else {
		if c.Refs, err = refsField(m); err != nil {
			return nil, err
		}
		if c.TS, err = uintField(m, keyTS, "block"); err != nil {
			return nil, err
		}
		if c.Ops, err = opsField(m); err != nil {
			return nil, err
		}
	}

	blk, err := assemble(c, sig)
	if err != nil {
		return nil, err
	}
	// Rule 8: the block MUST be encoded as valid dCBOR. dcbor.Decode has
	// already rejected non-canonical bytes, so re-encoding must reproduce the
	// input exactly; the comparison turns that invariant into a check.
	if !bytes.Equal(blk.enc, b) {
		return nil, fmt.Errorf("block: input is not the canonical dCBOR encoding of the block it decodes to")
	}
	return blk, nil
}

// prevField reads the prev field: a 32-byte digest, or null for a genesis
// block (spec/02-block-format.md).
func prevField(m dcbor.Map) (*cid.Digest, error) {
	v, ok := m.Get(keyPrev)
	if !ok {
		return nil, fmt.Errorf("block: is missing the %q key", keyPrev)
	}
	if _, isNull := v.(dcbor.NullValue); isNull {
		return nil, nil
	}
	b, ok := v.(dcbor.Bytes)
	if !ok {
		return nil, fmt.Errorf("block: %q must be a 32-byte byte string or null, got %s", keyPrev, kindOf(v))
	}
	d, err := cid.ParseDigest(b)
	if err != nil {
		return nil, fmt.Errorf("block: %q: %w", keyPrev, err)
	}
	return &d, nil
}

// refsField reads the refs array. Its entries are 32-byte block digests; the
// list MAY be empty (spec/02-block-format.md).
func refsField(m dcbor.Map) ([]cid.Digest, error) {
	v, _ := m.Get(keyRefs)
	arr, ok := v.(dcbor.Array)
	if !ok {
		return nil, fmt.Errorf("block: %q must be an array, got %s", keyRefs, kindOf(v))
	}
	refs := make([]cid.Digest, 0, len(arr))
	for i, item := range arr {
		b, ok := item.(dcbor.Bytes)
		if !ok {
			return nil, fmt.Errorf("block: %q entry %d must be a byte string, got %s", keyRefs, i, kindOf(item))
		}
		d, err := cid.ParseDigest(b)
		if err != nil {
			return nil, fmt.Errorf("block: %q entry %d: %w", keyRefs, i, err)
		}
		refs = append(refs, d)
	}
	return refs, nil
}

// opsField reads the ops array, which MUST hold at least one operation
// (spec/02-block-format.md, "Validation" rule 7).
func opsField(m dcbor.Map) ([]Operation, error) {
	v, _ := m.Get(keyOps)
	arr, ok := v.(dcbor.Array)
	if !ok {
		return nil, fmt.Errorf("block: %q must be an array, got %s", keyOps, kindOf(v))
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("block: %q is empty; a block must contain at least one operation", keyOps)
	}
	ops := make([]Operation, 0, len(arr))
	for i, item := range arr {
		op, err := decodeOperation(item)
		if err != nil {
			return nil, fmt.Errorf("block: operation %d: %w", i, err)
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// requireKeys reports an error unless m holds exactly the given keys — the
// closed-map rule of spec/03-encoding.md, "Deterministic CBOR" rule 8: no
// undeclared key, no missing declared one. A decoded map never carries a
// duplicate key, so a matching length plus a lookup for each expected key is
// exhaustive.
func requireKeys(m dcbor.Map, what string, keys ...string) error {
	for _, k := range keys {
		if _, ok := m.Get(k); !ok {
			return fmt.Errorf("block: %s is missing the %q key; it must hold exactly %s", what, k, quoteList(keys))
		}
	}
	if len(m) != len(keys) {
		for _, e := range m {
			if !contains(keys, e.Key) {
				return fmt.Errorf("block: %s carries the unknown key %q; it must hold exactly %s", what, e.Key, quoteList(keys))
			}
		}
		return fmt.Errorf("block: %s has %d key(s), want exactly %d (%s)", what, len(m), len(keys), quoteList(keys))
	}
	return nil
}

func contains(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// textField reads a text-string field.
func textField(m dcbor.Map, key, what string) (string, error) {
	v, ok := m.Get(key)
	if !ok {
		return "", fmt.Errorf("block: %s is missing the %q key", what, key)
	}
	t, ok := v.(dcbor.Text)
	if !ok {
		return "", fmt.Errorf("block: %s %q must be a text string, got %s", what, key, kindOf(v))
	}
	return string(t), nil
}

// uintField reads an unsigned-integer field.
func uintField(m dcbor.Map, key, what string) (uint64, error) {
	v, ok := m.Get(key)
	if !ok {
		return 0, fmt.Errorf("block: %s is missing the %q key", what, key)
	}
	u, ok := v.(dcbor.Uint)
	if !ok {
		return 0, fmt.Errorf("block: %s %q must be an unsigned integer, got %s", what, key, kindOf(v))
	}
	return uint64(u), nil
}

// bytesField reads a byte-string field of an exact size.
func bytesField(m dcbor.Map, key, what string, size int) ([]byte, error) {
	v, ok := m.Get(key)
	if !ok {
		return nil, fmt.Errorf("block: %s is missing the %q key", what, key)
	}
	b, ok := v.(dcbor.Bytes)
	if !ok {
		return nil, fmt.Errorf("block: %s %q must be a byte string, got %s", what, key, kindOf(v))
	}
	if len(b) != size {
		return nil, fmt.Errorf("block: %s %q is %d bytes, want %d", what, key, len(b), size)
	}
	return []byte(b), nil
}

// kindOf names the dCBOR kind of v for error messages.
func kindOf(v dcbor.Value) string {
	switch v.(type) {
	case dcbor.Uint, dcbor.Neg:
		return "an integer"
	case dcbor.Decimal:
		return "a decimal fraction"
	case dcbor.Text:
		return "a text string"
	case dcbor.Bytes:
		return "a byte string"
	case dcbor.Array:
		return "an array"
	case dcbor.Map:
		return "a map"
	case dcbor.NullValue:
		return "null"
	case nil:
		return "nothing"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// quoteList renders a key list for error messages.
func quoteList(keys []string) string {
	s := ""
	for i, k := range keys {
		if i > 0 {
			if i == len(keys)-1 {
				s += " and "
			} else {
				s += ", "
			}
		}
		s += fmt.Sprintf("%q", k)
	}
	return s
}
