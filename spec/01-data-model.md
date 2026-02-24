# Data Model

**Version:** 1.0 (2026-02-20) | **Status:** Draft

## Abstract

This document defines the Dialog ontology data model: atoms, bonds, and molecules. All entities are content-addressed and author-independent. Two implementations that encode the same logical entity MUST produce the same identifier.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Content identifier (CID):** A self-describing hash pointer. See [03-encoding.md](03-encoding.md).
- **dCBOR:** Deterministic CBOR encoding profile. See [03-encoding.md](03-encoding.md).

## Overview

Dialog represents knowledge as a graph of three primitives:

- **Atoms** are entities — unique, unambiguous points of reference.
- **Bonds** are relationship templates — sentences with blanks.
- **Molecules** are complete statements — bonds with their blanks filled in.

All three are content-addressed: their identifier is the CID of their deterministic CBOR encoding. The same entity created by different authors produces the same CID.

## Specification

### Atoms

An atom represents a single, unambiguous entity. Each atom is identified by its description string.

```cddl
atom = {
  "description" => tstr
}
```

The description MUST be a non-empty UTF-8 string. Any difference in the description string, however minor, produces a different atom. For example, "Paris, the capital of France" and "Paris, France" are two distinct atoms.

The atom's CID is computed by encoding the atom as dCBOR and hashing the result. See [03-encoding.md](03-encoding.md) for the exact procedure.

Atom equivalence is not handled at the data model level. Two atoms with different descriptions that refer to the same real-world entity are unified through the "is the same as" meta-bond. See [06-meta-bonds.md](06-meta-bonds.md).

### Bonds

A bond is a relationship template — a sentence with named variables.

```cddl
bond = {
  "template" => tstr
}
```

The template MUST be a non-empty UTF-8 string containing one or more variables. Variables are delimited by underscores: `_VariableName_`. Variable names MUST be unique within a bond.

Examples:
- `_A_ is the capital of _B_`
- `_Person_ founded _Company_`
- `_A_ occurred before _B_`

The bond's CID is computed identically to an atom's: dCBOR encode, then hash.

### Molecules

A molecule is a complete statement: a bond with all its variables filled in.

```cddl
molecule = {
  "bond" => bstr .size 32,    ; SHA-256 digest of the bond
  "fillers" => [+ filler]     ; ordered list, one per variable
}

filler = {
  "type" => filler-type,
  "value" => filler-value
}

filler-type = &(
  atom: 0,
  bond: 1,
  molecule: 2,
  ipfs-uri: 3,
  scalar: 4
)

filler-value = bstr / tstr / scalar-value

scalar-value = {
  ? "unit" => bstr .size 32,  ; SHA-256 digest of a unit atom
  "value" => number,           ; integer or float
}
/ datetime-range

datetime-range = {
  "from" => tstr,              ; RFC 3339 datetime string
  "to" => tstr                 ; RFC 3339 datetime string
}
```

#### Filler types

Each filler has a type tag and a value:

| Type | Tag | Value | Description |
|------|-----|-------|-------------|
| Atom reference | 0 | `bstr .size 32` | SHA-256 digest of the referenced atom |
| Bond reference | 1 | `bstr .size 32` | SHA-256 digest of the referenced bond |
| Molecule reference | 2 | `bstr .size 32` | SHA-256 digest of the referenced molecule |
| IPFS URI | 3 | `tstr` | IPFS content identifier as string (e.g., `"bafyrei..."`) |
| Scalar | 4 | `scalar-value` | A numeric value, optionally with a unit, or a datetime range |

Filler types 0, 1, and 2 use the raw SHA-256 digest (32 bytes), not the full CID. This avoids redundancy since the CID parameters are fixed protocol-wide.

#### Scalars

A scalar is one of:
- A unitless number (integer or float)
- A number with a unit (the unit is an atom, referenced by its SHA-256 digest)
- A datetime range (two RFC 3339 timestamps)

There are no plain dates in Dialog. The date "Thursday, Feb 20, 2026" is represented as a datetime range from `2026-02-20T00:00:00Z` to `2026-02-20T23:59:59Z`.

#### Content addressing

The molecule's CID is computed from its dCBOR encoding, identically to atoms and bonds. Since the molecule references its bond and fillers by their content hashes, the same assertion by different authors produces the same molecule CID.

### Entity identification summary

All entities are identified by: `CID(dCBOR(entity))`.

| Entity | CBOR map | Hash input |
|--------|----------|------------|
| Atom | `{"description": "..."}` | dCBOR encoding of the map |
| Bond | `{"template": "..."}` | dCBOR encoding of the map |
| Molecule | `{"bond": <digest>, "fillers": [...]}` | dCBOR encoding of the map |

## Security Considerations

- Atom descriptions are free-form strings. Implementations SHOULD sanitize descriptions before display to prevent injection attacks.
- Content addressing means that identical content always produces the same ID. An attacker cannot create a "different" atom with the same description — this is a feature, not a bug.
- Hash collisions (two different entities producing the same CID) are computationally infeasible with SHA-256.

## Examples

### Atom

Creating the atom "Paris, the capital of France":

```
Logical:   {"description": "Paris, the capital of France"}
CBOR hex:  a16b6465736372697074696f6e781c50617269732c20746865206361706974616c
           206f66204672616e6365
SHA-256:   6545050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360965411fd
CID:       017112206545050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360
           965411fd
```

### Bond

Creating the bond "_A_ is the capital of _B_":

```
Logical:   {"template": "_A_ is the capital of _B_"}
CBOR hex:  a16874656d706c61746578195f415f20697320746865206361706974616c206f66
           205f425f
SHA-256:   f295b89289597b4486784ad03d0be8bdab09a0d20070a893afa4f4d307811340
CID:       01711220f295b89289597b4486784ad03d0be8bdab09a0d20070a893afa4f4d3
           07811340
```

### Molecule

Creating the molecule "[Paris, the capital of France] is the capital of [France]":

```
Logical:   {
             "bond": <SHA-256 of bond>,
             "fillers": [
               {"type": 0, "value": <SHA-256 of "Paris, the capital of France">},
               {"type": 0, "value": <SHA-256 of "France">}
             ]
           }
CBOR hex:  a264626f6e645820f295b89289597b4486784ad03d0be8bdab09a0d20070a893
           afa4f4d3078113406766696c6c65727382a26474797065006576616c7565582065
           45050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360965411fda264
           74797065006576616c75655820e57761b439ee0cbb7ef79422b0cce927d7d0147e
           00a5281cc173b0475512b842
SHA-256:   f9f124b06af6aa7d5f2381462afdeaca628fe3ac8b994253e5c08a3f5d128afb
CID:       01711220f9f124b06af6aa7d5f2381462afdeaca628fe3ac8b994253e5c08a3f
           5d128afb
```

## References

### Normative
- [03-encoding.md](03-encoding.md) — CBOR encoding and CID parameters
- [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) — Date and time format for datetime ranges

### Informative
- [06-meta-bonds.md](06-meta-bonds.md) — How meta-molecules handle atom equivalence and other state assertions
