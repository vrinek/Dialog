package entity

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// keyDescription is the sole key of an atom (spec/01-data-model.md, "Atoms").
const keyDescription = "description"

// An Atom is a single, unambiguous entity, identified by its description
// string:
//
//	atom = { "description" => tstr }
//
// Any difference in the description, however minor, is a different atom:
// content addressing operates on the raw UTF-8 bytes with no Unicode
// normalization (spec/03-encoding.md, "Text strings and Unicode"). Two atoms
// that name the same real-world thing are related with the "_A_ is the same
// as _B_" meta-bond, not by anything this type does.
//
// The zero Atom is not an atom; Bytes, Digest and CID panic on it. Build one
// with NewAtom, MustAtom or DecodeAtom.
type Atom struct {
	description string
	enc         []byte // canonical dCBOR; nil only in the zero value
}

// NewAtom returns the atom with the given description.
//
// The description MUST be a non-empty UTF-8 string
// (spec/01-data-model.md, "Atoms"); anything else is an error.
func NewAtom(description string) (Atom, error) {
	if description == "" {
		return Atom{}, fmt.Errorf("entity: atom description is empty; it must be a non-empty UTF-8 string")
	}
	if !utf8.ValidString(description) {
		return Atom{}, fmt.Errorf("entity: atom description is not valid UTF-8: %q", description)
	}
	return Atom{description: description, enc: dcbor.MustEncode(atomValue(description))}, nil
}

// MustAtom is NewAtom, panicking on error. It is meant for descriptions known
// to be valid at the call site (tests, constants, the standard meta-bonds).
func MustAtom(description string) Atom {
	a, err := NewAtom(description)
	if err != nil {
		panic(err)
	}
	return a
}

// DecodeAtom parses and validates the canonical dCBOR encoding of an atom.
//
// The input must be exactly the map `{"description": tstr}` in canonical
// form: any other key set, a non-text description, an empty description, or
// any deviation from Dialog's dCBOR profile is an error.
func DecodeAtom(b []byte) (Atom, error) {
	m, err := decodeEntityMap(b, "atom", keyDescription)
	if err != nil {
		return Atom{}, err
	}
	description, err := textField(m, keyDescription, "atom")
	if err != nil {
		return Atom{}, err
	}
	return NewAtom(description)
}

// Description returns the atom's description string.
func (a Atom) Description() string { return a.description }

// Value returns the atom as a dCBOR value.
func (a Atom) Value() dcbor.Value { return atomValue(a.description) }

// Bytes returns a copy of the atom's canonical dCBOR encoding.
func (a Atom) Bytes() []byte { return bytes.Clone(a.encoding()) }

// Digest returns SHA-256(dCBOR(atom)), the form every reference to this atom
// takes inside a Dialog structure.
func (a Atom) Digest() cid.Digest { return cid.SumDigest(a.encoding()) }

// CID returns the atom's external 36-byte content identifier.
func (a Atom) CID() cid.CID { return a.Digest().CID() }

// String renders the atom for logs and test failures.
func (a Atom) String() string {
	if a.enc == nil {
		return "atom(invalid)"
	}
	return fmt.Sprintf("atom(%q, %s)", a.description, a.CID())
}

func (a Atom) encoding() []byte {
	if a.enc == nil {
		panic("entity: zero-value Atom has no encoding; build atoms with NewAtom or DecodeAtom")
	}
	return a.enc
}

func atomValue(description string) dcbor.Value {
	return dcbor.Map{{Key: keyDescription, Value: dcbor.Text(description)}}
}
