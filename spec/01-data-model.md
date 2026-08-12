# Data Model

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document defines the Dialog ontology data model: atoms, bonds, and molecules. All entities are content-addressed and author-independent. Two implementations that encode the same logical entity MUST produce the same identifier.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Content identifier (CID):** A self-describing hash pointer, 36 bytes. Used for external identifiers only. See [03-encoding.md](03-encoding.md).
- **Digest:** The raw 32-byte SHA-256 hash of an entity's dCBOR encoding. Every reference inside a Dialog structure is a digest, not a CID. See [03-encoding.md](03-encoding.md), "Internal references".
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

The template MUST be a non-empty UTF-8 string containing one or more variables. Variables are identified by the following grammar:

```abnf
variable = "_" 1*UCALPHA "_"
UCALPHA  = %x41-5A          ; A-Z
```

A variable is an underscore, followed by one or more uppercase ASCII letters (`A`-`Z`), followed by an underscore. Implementations MUST parse variables using a leftmost-longest match: scan left to right, and when an underscore is encountered, consume the longest sequence of uppercase ASCII letters followed by a closing underscore. All other underscores in the template are literal text.

Variable names MUST be unique within a bond template.

**Disambiguation.** Because the parser uses leftmost-longest match, ambiguous sequences resolve deterministically:

- `_AB_` is a single variable `AB`.
- `_A_B_` is the variable `A` followed by the literal text `B_`, because after consuming `_A_` the next character `B` is not preceded by an opening underscore.
- `_A__B_` is the variable `A` followed by the variable `B` (the middle two underscores serve as the closing underscore of `A` and the opening underscore of `B`).
- `type_of` contains no variables — the underscore is not followed by a closing `_UCALPHA+_` sequence.
- `_a_` contains no variables — lowercase letters do not match `UCALPHA`.

Examples:
- `_A_ is the capital of _B_`
- `_X_ founded _Y_`
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
  "value" => int / #6.4([int, int]),  ; integer or decimal fraction (CBOR tag 4)
}
/ datetime-range

datetime-range = {
  "from" => tstr,              ; RFC 3339 datetime string
  "to" => tstr                 ; RFC 3339 datetime string
}
```

The number of fillers in a molecule MUST equal the number of variables in the referenced bond template. The fillers are positionally matched to variables in the order they appear in the template.

#### Filler types

Each filler has a type tag and a value:

| Type | Tag | Value | Description |
|------|-----|-------|-------------|
| Atom reference | 0 | `bstr .size 32` | SHA-256 digest of the referenced atom |
| Bond reference | 1 | `bstr .size 32` | SHA-256 digest of the referenced bond |
| Molecule reference | 2 | `bstr .size 32` | SHA-256 digest of the referenced molecule |
| IPFS URI | 3 | `tstr` | IPFS content identifier as string (e.g., `"bafyrei..."`) |
| Scalar | 4 | `scalar-value` | A numeric value, optionally with a unit, or a datetime range |

Filler types 0, 1, and 2 use the raw SHA-256 digest (32 bytes), not the full CID, as does the molecule's `bond` field. See [03-encoding.md](03-encoding.md), "Internal references". Type 3 (IPFS URI) is not an internal reference — it carries an IPFS content identifier as a text string.

#### Scalars

A scalar is one of:
- A unitless integer, or a decimal fraction encoded as CBOR tag 4 (`[exponent, mantissa]`, both integers)
- An integer or decimal fraction with a unit (the unit is an atom, referenced by its SHA-256 digest)
- A datetime range (two RFC 3339 timestamps)

Decimal fractions use CBOR tag 4, encoding the value as `[exponent, mantissa]` where both are integers. For example, `3.14` is encoded as `#6.4([-2, 314])`. This is dCBOR-compatible since both components are integers -- no IEEE 754 floats are used.

Tag 4 is the only tag Dialog permits, and its encoding is canonicalized so that each value has exactly one representation: the exponent MUST be negative and the mantissa MUST NOT be zero or divisible by 10. Whole numbers are therefore always encoded as plain integers, never as decimal fractions. See [03-encoding.md](03-encoding.md), "Decimal fractions", for the normative rules.

There are no plain dates in Dialog. The date "Thursday, Feb 20, 2026" is represented as a datetime range from `2026-02-20T00:00:00Z` to `2026-02-20T23:59:59Z`.

#### Content addressing

The molecule's CID is computed from its dCBOR encoding, identically to atoms and bonds. Since the molecule references its bond and fillers by their SHA-256 digests, the same assertion by different authors produces the same molecule CID.

### Entity identification summary

All entities are identified by: `CID(dCBOR(entity))` externally, and by the raw digest `SHA-256(dCBOR(entity))` within Dialog structures. Both forms carry the same digest — see [03-encoding.md](03-encoding.md), "Internal references".

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
             "bond": <digest of bond>,
             "fillers": [
               {"type": 0, "value": <digest of "Paris, the capital of France">},
               {"type": 0, "value": <digest of "France">}
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
