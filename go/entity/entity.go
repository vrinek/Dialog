// Package entity implements Dialog's three content-addressed primitives —
// atoms, bonds and molecules — together with the fillers a molecule is built
// from, as specified in spec/01-data-model.md.
//
// Every entity is a value type with unexported fields. The only ways to build
// one are the New* constructors and the Decode* functions, and both validate
// before they return, so an Atom, Bond or Molecule that exists is an entity
// the specification permits. That matters because these types produce
// identifiers: Digest and CID are total functions here, and no caller has to
// wonder whether the digest it just computed belongs to a structure another
// implementation would refuse.
//
// The zero value of each entity type is not an entity. Its Bytes, Digest and
// CID methods panic rather than hand back the identifier of an empty
// structure.
//
// Canonical bytes come from the dcbor package and identifiers from the cid
// package:
//
//	Digest(entity) = SHA-256(dCBOR(entity))
//	CID(entity)    = 0x01 || 0x71 || 0x12 || 0x20 || Digest(entity)
//
// References inside these structures — a molecule's bond field, and fillers of
// type 0, 1 and 2 — are raw 32-byte digests, never CIDs
// (spec/03-encoding.md, "Internal references").
//
// # What this package cannot validate
//
// A molecule carries the digest of its bond, not the bond's template, so the
// rule that "the number of fillers MUST equal the number of variables in the
// referenced bond template" (spec/01-data-model.md) is not checkable from a
// molecule alone. The specification places that check at block validation
// (spec/02-block-format.md, "Validation" rule 5), where the bond is reachable
// through the block's refs. Accordingly:
//
//   - NewMolecule and DecodeMolecule validate everything that is structural —
//     key set, value types, digest lengths, filler shapes — and cannot check
//     the filler count.
//   - NewMoleculeFor takes the bond itself and does check it.
//   - Molecule.ValidateAgainst checks a decoded molecule once its bond has
//     been resolved; the block package calls it during reachability
//     validation.
//
// Bond templates are parsed with the grammar of spec/01-data-model.md; see
// ParseTemplateVariables.
package entity

import (
	"fmt"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// decodeEntityMap parses b as dCBOR and requires the top-level value to be a
// map with exactly the given keys, in any order. dcbor.Decode has already
// rejected non-canonical input — unsorted or duplicate keys, non-shortest
// encodings, tags other than 4, floats, trailing bytes — so what remains is
// structural validation against the CDDL of spec/01-data-model.md.
func decodeEntityMap(b []byte, what string, keys ...string) (dcbor.Map, error) {
	v, err := dcbor.Decode(b)
	if err != nil {
		return nil, fmt.Errorf("entity: %s is not valid dCBOR: %w", what, err)
	}
	m, err := asMap(v, what)
	if err != nil {
		return nil, err
	}
	if err := requireKeys(m, what, keys...); err != nil {
		return nil, err
	}
	return m, nil
}

// asMap requires v to be a dCBOR map.
func asMap(v dcbor.Value, what string) (dcbor.Map, error) {
	m, ok := v.(dcbor.Map)
	if !ok {
		return nil, fmt.Errorf("entity: %s must be a CBOR map, got %s", what, kindOf(v))
	}
	return m, nil
}

// requireKeys reports an error unless m holds exactly the given keys. Decoded
// maps never carry a duplicate key, so a matching length plus a lookup for
// each expected key is exhaustive.
func requireKeys(m dcbor.Map, what string, keys ...string) error {
	if len(m) != len(keys) {
		return fmt.Errorf("entity: %s has %d key(s), want exactly %d (%s)", what, len(m), len(keys), quoteList(keys))
	}
	for _, k := range keys {
		if _, ok := m.Get(k); !ok {
			return fmt.Errorf("entity: %s is missing the %q key; it must hold exactly %s", what, k, quoteList(keys))
		}
	}
	return nil
}

// digestField reads a 32-byte digest from a byte-string field
// (spec/03-encoding.md, "Internal references").
func digestField(m dcbor.Map, key, what string) (cid.Digest, error) {
	v, ok := m.Get(key)
	if !ok {
		return cid.Digest{}, fmt.Errorf("entity: %s is missing the %q key", what, key)
	}
	return asDigest(v, fmt.Sprintf("%s %q", what, key))
}

// asDigest requires v to be a byte string holding exactly 32 bytes.
func asDigest(v dcbor.Value, what string) (cid.Digest, error) {
	b, ok := v.(dcbor.Bytes)
	if !ok {
		return cid.Digest{}, fmt.Errorf("entity: %s must be a byte string, got %s", what, kindOf(v))
	}
	d, err := cid.ParseDigest(b)
	if err != nil {
		return cid.Digest{}, fmt.Errorf("entity: %s: %w", what, err)
	}
	return d, nil
}

// textField reads a text-string field.
func textField(m dcbor.Map, key, what string) (string, error) {
	v, ok := m.Get(key)
	if !ok {
		return "", fmt.Errorf("entity: %s is missing the %q key", what, key)
	}
	t, ok := v.(dcbor.Text)
	if !ok {
		return "", fmt.Errorf("entity: %s %q must be a text string, got %s", what, key, kindOf(v))
	}
	return string(t), nil
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
