package block

import (
	"crypto/ed25519"
	"fmt"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// The four operation names of spec/02-block-format.md, "Operations". There are
// exactly four operation types and no extension point.
const (
	OpCreateAtom     = "create_atom"
	OpCreateBond     = "create_bond"
	OpCreateMolecule = "create_molecule"
	OpRotateKey      = "rotate_key"
)

// Map keys of the four operations.
const (
	keyOp          = "op"
	keyDescription = "description"
	keyTemplate    = "template"
	keyBond        = "bond"
	keyFillers     = "fillers"
	keyNewPub      = "new_pub"
)

// An EntityKind names one of the three content-addressed primitives. It is
// what a digest carried by an operation is required to resolve to: a
// create_molecule's bond field names a bond, a type 0 filler names an atom,
// and so on (spec/01-data-model.md, "Filler types").
type EntityKind uint8

// The three entity kinds.
const (
	KindAtom EntityKind = iota
	KindBond
	KindMolecule
)

// String names the entity kind.
func (k EntityKind) String() string {
	switch k {
	case KindAtom:
		return "atom"
	case KindBond:
		return "bond"
	case KindMolecule:
		return "molecule"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(k))
	}
}

// A Reference is one entity digest carried by an operation, together with the
// kind of entity it names and where in the operation it sits. Block validation
// requires every Reference to be reachable (spec/02-block-format.md,
// "Validation" rule 4).
type Reference struct {
	// Digest is the referenced entity's raw 32-byte digest.
	Digest cid.Digest
	// Kind is the entity kind the reference's position requires.
	Kind EntityKind
	// Field names the position for error messages, e.g. "bond" or
	// "filler 1 unit".
	Field string
}

func (r Reference) String() string {
	return fmt.Sprintf("%s %s %s", r.Field, r.Kind, r.Digest)
}

// An Operation is one entry of a block's ops list. The only implementations
// are CreateAtom, CreateBond, CreateMolecule and RotateKey — the four
// operation types of spec/02-block-format.md, which is a closed set.
type Operation interface {
	// Op returns the operation's name, the value of its "op" key.
	Op() string
	// Value returns the operation as a dCBOR value.
	Value() dcbor.Value
	// Creates returns the digest and kind of the entity the operation
	// creates. ok is false for rotate_key, which creates no entity.
	Creates() (d cid.Digest, k EntityKind, ok bool)
	// References returns the entity digests the operation carries, in the
	// order they appear in it.
	References() []Reference

	isOperation()
}

// EncodeOperation returns the canonical dCBOR encoding of an operation.
// Operations are not content-addressed on their own — they are encoded as part
// of a block — but their bytes are useful for tests and conformance vectors.
func EncodeOperation(op Operation) []byte { return dcbor.MustEncode(op.Value()) }

// A CreateAtom operation creates an atom:
//
//	create-atom = { "op" => "create_atom", "description" => tstr }
//
// The atom's identifier is SHA-256(dCBOR({"description": <description>})) —
// the description alone, without the "op" key
// (spec/02-block-format.md, "create_atom").
type CreateAtom struct{ atom entity.Atom }

// NewCreateAtom returns the operation creating the atom with the given
// description, which must be a non-empty UTF-8 string.
func NewCreateAtom(description string) (CreateAtom, error) {
	a, err := entity.NewAtom(description)
	if err != nil {
		return CreateAtom{}, err
	}
	return CreateAtom{atom: a}, nil
}

// MustCreateAtom is NewCreateAtom, panicking on error.
func MustCreateAtom(description string) CreateAtom {
	op, err := NewCreateAtom(description)
	if err != nil {
		panic(err)
	}
	return op
}

// Atom returns the atom the operation creates.
func (o CreateAtom) Atom() entity.Atom { return o.atom }

// Description returns the atom's description.
func (o CreateAtom) Description() string { return o.atom.Description() }

// Op returns "create_atom".
func (o CreateAtom) Op() string { return OpCreateAtom }

// Value returns the operation as a dCBOR value.
func (o CreateAtom) Value() dcbor.Value {
	return dcbor.Map{
		{Key: keyOp, Value: dcbor.Text(OpCreateAtom)},
		{Key: keyDescription, Value: dcbor.Text(o.atom.Description())},
	}
}

// Creates returns the digest of the atom.
func (o CreateAtom) Creates() (cid.Digest, EntityKind, bool) {
	return o.atom.Digest(), KindAtom, true
}

// References returns nil: an atom carries no digests.
func (o CreateAtom) References() []Reference { return nil }

func (o CreateAtom) String() string { return fmt.Sprintf("create_atom(%q)", o.atom.Description()) }

func (CreateAtom) isOperation() {}

// A CreateBond operation creates a bond:
//
//	create-bond = { "op" => "create_bond", "template" => tstr }
//
// The bond's identifier is SHA-256(dCBOR({"template": <template>}))
// (spec/02-block-format.md, "create_bond").
type CreateBond struct{ bond entity.Bond }

// NewCreateBond returns the operation creating the bond with the given
// template, which must be well-formed under the grammar of
// spec/01-data-model.md.
func NewCreateBond(template string) (CreateBond, error) {
	b, err := entity.NewBond(template)
	if err != nil {
		return CreateBond{}, err
	}
	return CreateBond{bond: b}, nil
}

// MustCreateBond is NewCreateBond, panicking on error.
func MustCreateBond(template string) CreateBond {
	op, err := NewCreateBond(template)
	if err != nil {
		panic(err)
	}
	return op
}

// Bond returns the bond the operation creates.
func (o CreateBond) Bond() entity.Bond { return o.bond }

// Template returns the bond's template.
func (o CreateBond) Template() string { return o.bond.Template() }

// Op returns "create_bond".
func (o CreateBond) Op() string { return OpCreateBond }

// Value returns the operation as a dCBOR value.
func (o CreateBond) Value() dcbor.Value {
	return dcbor.Map{
		{Key: keyOp, Value: dcbor.Text(OpCreateBond)},
		{Key: keyTemplate, Value: dcbor.Text(o.bond.Template())},
	}
}

// Creates returns the digest of the bond.
func (o CreateBond) Creates() (cid.Digest, EntityKind, bool) {
	return o.bond.Digest(), KindBond, true
}

// References returns nil: a bond carries no digests.
func (o CreateBond) References() []Reference { return nil }

func (o CreateBond) String() string { return fmt.Sprintf("create_bond(%q)", o.bond.Template()) }

func (CreateBond) isOperation() {}

// A CreateMolecule operation creates a molecule:
//
//	create-molecule = {
//	  "op"      => "create_molecule",
//	  "bond"    => bstr .size 32,
//	  "fillers" => [+ filler]
//	}
//
// The molecule's identifier is
// SHA-256(dCBOR({"bond": <bond_digest>, "fillers": <fillers>})) — the same map
// without the "op" key, which is exactly the molecule entity
// (spec/02-block-format.md, "create_molecule").
type CreateMolecule struct{ molecule entity.Molecule }

// NewCreateMolecule returns the operation creating a molecule from a bond
// digest and its fillers. The filler count cannot be checked here — the bond's
// template is not at hand — and is enforced at block validation
// (spec/02-block-format.md, "Validation" rule 5).
func NewCreateMolecule(bond cid.Digest, fillers []entity.Filler) (CreateMolecule, error) {
	m, err := entity.NewMolecule(bond, fillers)
	if err != nil {
		return CreateMolecule{}, err
	}
	return CreateMolecule{molecule: m}, nil
}

// NewCreateMoleculeFor returns the operation creating a molecule for a known
// bond, checking the filler count against the bond's template.
func NewCreateMoleculeFor(b entity.Bond, fillers []entity.Filler) (CreateMolecule, error) {
	m, err := entity.NewMoleculeFor(b, fillers)
	if err != nil {
		return CreateMolecule{}, err
	}
	return CreateMolecule{molecule: m}, nil
}

// MustCreateMolecule is NewCreateMoleculeFor, panicking on error.
func MustCreateMolecule(b entity.Bond, fillers []entity.Filler) CreateMolecule {
	op, err := NewCreateMoleculeFor(b, fillers)
	if err != nil {
		panic(err)
	}
	return op
}

// Molecule returns the molecule the operation creates.
func (o CreateMolecule) Molecule() entity.Molecule { return o.molecule }

// Op returns "create_molecule".
func (o CreateMolecule) Op() string { return OpCreateMolecule }

// Value returns the operation as a dCBOR value.
func (o CreateMolecule) Value() dcbor.Value {
	m, ok := o.molecule.Value().(dcbor.Map)
	if !ok { // unreachable: a molecule always encodes as a map
		panic("block: molecule value is not a map")
	}
	return append(dcbor.Map{{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)}}, m...)
}

// Creates returns the digest of the molecule.
func (o CreateMolecule) Creates() (cid.Digest, EntityKind, bool) {
	return o.molecule.Digest(), KindMolecule, true
}

// References returns the bond digest and every digest carried by a filler: the
// atom, bond and molecule references of types 0, 1 and 2, and the unit atom of
// a scalar filler that has one. Each must be reachable from the block
// (spec/02-block-format.md, "Validation" rule 4).
func (o CreateMolecule) References() []Reference {
	refs := []Reference{{Digest: o.molecule.Bond(), Kind: KindBond, Field: keyBond}}
	for i, f := range o.molecule.Fillers() {
		switch {
		case f.Type().IsRef():
			d, _ := f.Ref()
			refs = append(refs, Reference{Digest: d, Kind: fillerKind(f.Type()), Field: fmt.Sprintf("filler %d", i)})
		case f.Type() == entity.FillerScalar:
			s, _ := f.Scalar()
			if unit, ok := s.Unit(); ok {
				refs = append(refs, Reference{Digest: unit, Kind: KindAtom, Field: fmt.Sprintf("filler %d unit", i)})
			}
		}
	}
	return refs
}

func (o CreateMolecule) String() string {
	return fmt.Sprintf("create_molecule(bond %s, %d filler(s))", o.molecule.Bond(), len(o.molecule.Fillers()))
}

func (CreateMolecule) isOperation() {}

// fillerKind maps a reference filler type to the entity kind it names.
func fillerKind(t entity.FillerType) EntityKind {
	switch t {
	case entity.FillerBond:
		return KindBond
	case entity.FillerMolecule:
		return KindMolecule
	default:
		return KindAtom
	}
}

// A RotateKey operation ends the current key's chain and names its successor:
//
//	rotate-key-op = { "op" => "rotate_key", "new_pub" => bstr .size 32 }
//
// It may appear only in a rotation block, which contains exactly one of them
// and nothing else (spec/02-block-format.md, "Rotation block").
type RotateKey struct{ newPub [ed25519.PublicKeySize]byte }

// NewRotateKey returns the operation rotating to newPub, which must be a raw
// 32-byte Ed25519 public key.
func NewRotateKey(newPub ed25519.PublicKey) (RotateKey, error) {
	var op RotateKey
	if len(newPub) != ed25519.PublicKeySize {
		return op, fmt.Errorf("block: rotate_key new_pub is %d bytes, want %d", len(newPub), ed25519.PublicKeySize)
	}
	copy(op.newPub[:], newPub)
	return op, nil
}

// MustRotateKey is NewRotateKey, panicking on error.
func MustRotateKey(newPub ed25519.PublicKey) RotateKey {
	op, err := NewRotateKey(newPub)
	if err != nil {
		panic(err)
	}
	return op
}

// NewPublicKey returns the successor key.
func (o RotateKey) NewPublicKey() ed25519.PublicKey {
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	copy(pub, o.newPub[:])
	return pub
}

// Op returns "rotate_key".
func (o RotateKey) Op() string { return OpRotateKey }

// Value returns the operation as a dCBOR value.
func (o RotateKey) Value() dcbor.Value {
	return dcbor.Map{
		{Key: keyOp, Value: dcbor.Text(OpRotateKey)},
		{Key: keyNewPub, Value: dcbor.Bytes(o.NewPublicKey())},
	}
}

// Creates reports that rotate_key creates no entity.
func (o RotateKey) Creates() (cid.Digest, EntityKind, bool) { return cid.Digest{}, 0, false }

// References returns nil: new_pub is a key, not an entity digest.
func (o RotateKey) References() []Reference { return nil }

func (o RotateKey) String() string { return fmt.Sprintf("rotate_key(%x)", o.newPub[:8]) }

func (RotateKey) isOperation() {}

// decodeOperation validates one entry of a decoded ops array against the CDDL
// of spec/02-block-format.md, "Operations". The value has already been through
// dcbor.Decode, so what remains is structural: the key set, the operation
// name, and the type of each field.
func decodeOperation(v dcbor.Value) (Operation, error) {
	m, ok := v.(dcbor.Map)
	if !ok {
		return nil, fmt.Errorf("block: operation must be a CBOR map, got %s", kindOf(v))
	}
	nameValue, ok := m.Get(keyOp)
	if !ok {
		return nil, fmt.Errorf("block: operation is missing the %q key", keyOp)
	}
	name, ok := nameValue.(dcbor.Text)
	if !ok {
		return nil, fmt.Errorf("block: operation %q must be a text string, got %s", keyOp, kindOf(nameValue))
	}

	switch string(name) {
	case OpCreateAtom:
		if err := requireKeys(m, OpCreateAtom, keyOp, keyDescription); err != nil {
			return nil, err
		}
		description, err := textField(m, keyDescription, OpCreateAtom)
		if err != nil {
			return nil, err
		}
		return NewCreateAtom(description)

	case OpCreateBond:
		if err := requireKeys(m, OpCreateBond, keyOp, keyTemplate); err != nil {
			return nil, err
		}
		template, err := textField(m, keyTemplate, OpCreateBond)
		if err != nil {
			return nil, err
		}
		return NewCreateBond(template)

	case OpCreateMolecule:
		if err := requireKeys(m, OpCreateMolecule, keyOp, keyBond, keyFillers); err != nil {
			return nil, err
		}
		// The operation without its "op" key is exactly a molecule entity, so
		// the entity decoder validates the bond digest and every filler, and
		// the molecule it returns carries the identifier the operation defines
		// (spec/02-block-format.md, "create_molecule").
		bond, _ := m.Get(keyBond)
		fillers, _ := m.Get(keyFillers)
		encoded, err := dcbor.Encode(dcbor.Map{
			{Key: keyBond, Value: bond},
			{Key: keyFillers, Value: fillers},
		})
		if err != nil {
			return nil, fmt.Errorf("block: %s: %w", OpCreateMolecule, err)
		}
		mol, err := entity.DecodeMolecule(encoded)
		if err != nil {
			return nil, fmt.Errorf("block: %s: %w", OpCreateMolecule, err)
		}
		return CreateMolecule{molecule: mol}, nil

	case OpRotateKey:
		if err := requireKeys(m, OpRotateKey, keyOp, keyNewPub); err != nil {
			return nil, err
		}
		newPub, err := bytesField(m, keyNewPub, OpRotateKey, ed25519.PublicKeySize)
		if err != nil {
			return nil, err
		}
		return NewRotateKey(newPub)

	default:
		return nil, fmt.Errorf("block: unknown operation %q; spec/02-block-format.md defines exactly four: %q, %q, %q and %q",
			string(name), OpCreateAtom, OpCreateBond, OpCreateMolecule, OpRotateKey)
	}
}
