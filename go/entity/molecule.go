package entity

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// Keys of a molecule (spec/01-data-model.md, "Molecules").
const (
	keyBond    = "bond"
	keyFillers = "fillers"
)

// A Molecule is a complete statement — a bond with its variables filled in:
//
//	molecule = {
//	  "bond"    => bstr .size 32,
//	  "fillers" => [+ filler]
//	}
//
// The bond is referenced by its raw 32-byte digest, and the fillers are
// positionally matched to the variables of the bond's template, in the order
// the variables appear in it.
//
// The rule that the number of fillers MUST equal the number of variables in
// the referenced bond template cannot be checked from a molecule alone, since
// a molecule carries only the bond's digest. NewMoleculeFor and
// ValidateAgainst check it where the bond is available; see the package
// documentation.
//
// The zero Molecule is not a molecule; Bytes, Digest and CID panic on it.
type Molecule struct {
	bond    cid.Digest
	fillers []Filler
	enc     []byte // canonical dCBOR; nil only in the zero value
}

// NewMolecule returns the molecule referencing the bond with digest bond and
// filled by fillers.
//
// It validates everything a molecule carries: at least one filler
// (`[+ filler]`), and a well-formed payload for each. It cannot check the
// filler count against the bond's template — use NewMoleculeFor when the bond
// is at hand, or ValidateAgainst once it has been resolved.
func NewMolecule(bond cid.Digest, fillers []Filler) (Molecule, error) {
	if len(fillers) == 0 {
		return Molecule{}, fmt.Errorf("entity: molecule has no fillers; a molecule must carry at least one")
	}
	for i, f := range fillers {
		if err := f.validate(); err != nil {
			return Molecule{}, fmt.Errorf("entity: molecule filler %d: %w", i, err)
		}
	}
	m := Molecule{bond: bond, fillers: slices.Clone(fillers)}
	m.enc = dcbor.MustEncode(m.Value())
	return m, nil
}

// NewMoleculeFor returns the molecule for a known bond, checking that the
// number of fillers equals the number of variables in the bond's template
// (spec/01-data-model.md, "Molecules").
func NewMoleculeFor(b Bond, fillers []Filler) (Molecule, error) {
	if b.enc == nil {
		return Molecule{}, fmt.Errorf("entity: zero-value Bond; build bonds with NewBond or DecodeBond")
	}
	if len(fillers) != b.VariableCount() {
		return Molecule{}, fillerCountError(len(fillers), b)
	}
	return NewMolecule(b.Digest(), fillers)
}

// MustMolecule is NewMoleculeFor, panicking on error. It is meant for
// molecules known to be well-formed at the call site (tests, vectors).
func MustMolecule(b Bond, fillers []Filler) Molecule {
	m, err := NewMoleculeFor(b, fillers)
	if err != nil {
		panic(err)
	}
	return m
}

// DecodeMolecule parses and validates the canonical dCBOR encoding of a
// molecule: exactly the keys "bond" and "fillers", a 32-byte digest, and a
// non-empty array of well-formed fillers.
//
// As with NewMolecule, the filler count is not checked against the bond's
// template, which the bytes do not carry.
func DecodeMolecule(b []byte) (Molecule, error) {
	m, err := decodeEntityMap(b, "molecule", keyBond, keyFillers)
	if err != nil {
		return Molecule{}, err
	}
	bond, err := digestField(m, keyBond, "molecule")
	if err != nil {
		return Molecule{}, err
	}
	fv, _ := m.Get(keyFillers)
	arr, ok := fv.(dcbor.Array)
	if !ok {
		return Molecule{}, fmt.Errorf("entity: molecule %q must be an array, got %s", keyFillers, kindOf(fv))
	}
	if len(arr) == 0 {
		return Molecule{}, fmt.Errorf("entity: molecule has an empty %q array; a molecule must carry at least one filler", keyFillers)
	}
	fillers := make([]Filler, 0, len(arr))
	for i, item := range arr {
		f, err := fillerFromValue(item)
		if err != nil {
			return Molecule{}, fmt.Errorf("entity: molecule filler %d: %w", i, err)
		}
		fillers = append(fillers, f)
	}
	return NewMolecule(bond, fillers)
}

// Bond returns the digest of the bond this molecule instantiates.
func (m Molecule) Bond() cid.Digest { return m.bond }

// Fillers returns a copy of the molecule's fillers, in template order.
func (m Molecule) Fillers() []Filler { return slices.Clone(m.fillers) }

// Value returns the molecule as a dCBOR value.
func (m Molecule) Value() dcbor.Value {
	fillers := make(dcbor.Array, 0, len(m.fillers))
	for _, f := range m.fillers {
		fillers = append(fillers, f.Value())
	}
	return dcbor.Map{
		{Key: keyBond, Value: dcbor.Bytes(m.bond.Bytes())},
		{Key: keyFillers, Value: fillers},
	}
}

// Bytes returns a copy of the molecule's canonical dCBOR encoding.
func (m Molecule) Bytes() []byte { return bytes.Clone(m.encoding()) }

// Digest returns SHA-256(dCBOR(molecule)), the form every reference to this
// molecule takes inside a Dialog structure.
func (m Molecule) Digest() cid.Digest { return cid.SumDigest(m.encoding()) }

// CID returns the molecule's external 36-byte content identifier.
func (m Molecule) CID() cid.CID { return m.Digest().CID() }

// ValidateAgainst checks the molecule against the bond it references: that b
// is in fact that bond, and that the filler count equals the bond's variable
// count (spec/01-data-model.md, "Molecules"; enforced at block validation per
// spec/02-block-format.md, "Validation" rule 5).
func (m Molecule) ValidateAgainst(b Bond) error {
	if b.enc == nil {
		return fmt.Errorf("entity: zero-value Bond; build bonds with NewBond or DecodeBond")
	}
	if got := b.Digest(); got != m.bond {
		return fmt.Errorf("entity: molecule references bond %s, not %s", m.bond, got)
	}
	if len(m.fillers) != b.VariableCount() {
		return fillerCountError(len(m.fillers), b)
	}
	return nil
}

// String renders the molecule for logs and test failures.
func (m Molecule) String() string {
	if m.enc == nil {
		return "molecule(invalid)"
	}
	return fmt.Sprintf("molecule(bond %s, %d filler(s), %s)", m.bond, len(m.fillers), m.CID())
}

func (m Molecule) encoding() []byte {
	if m.enc == nil {
		panic("entity: zero-value Molecule has no encoding; build molecules with NewMolecule, NewMoleculeFor or DecodeMolecule")
	}
	return m.enc
}

func fillerCountError(n int, b Bond) error {
	return fmt.Errorf("entity: molecule has %d filler(s) but bond %q declares %d variable(s) %v; the counts must be equal",
		n, b.Template(), b.VariableCount(), b.Variables())
}
