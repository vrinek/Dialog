---
status: pending
priority: p3
issue_id: "035"
tags: [specification-gap, data-model, cddl, validation]
dependencies: []
---

# Filler Value Type Is Not Bound to Filler Type in the CDDL

## Problem Statement

`spec/01-data-model.md` defines a filler as

```cddl
filler = {
  "type" => filler-type,
  "value" => filler-value
}

filler-type  = &(atom: 0, bond: 1, molecule: 2, ipfs-uri: 3, scalar: 4)
filler-value = bstr / tstr / scalar-value
```

The two fields are independent in the CDDL: `{"type": 0, "value": "hello"}`
validates, as does `{"type": 3, "value": h'00...'}` and `{"type": 4, "value":
h'...'}`. The correlation lives one section further down, in the "Filler
types" table ("Atom reference | 0 | `bstr .size 32`") and in the prose
"Filler types 0, 1, and 2 use the raw SHA-256 digest (32 bytes)".

A validator written from the CDDL — the normal way to build one, since
`spec/03-encoding.md` lists RFC 8610 as a normative reference — accepts
fillers that the table forbids. A validator written from the table rejects
them. Both implementors can point at the specification. Because a filler goes
into a molecule's digest, the two implementations disagree about which
molecules exist, not merely about which are pretty.

The `bstr` alternative in `filler-value` also carries no `.size 32`, although
"Internal references" in `spec/03-encoding.md` requires exactly 32 bytes for
filler types 0, 1 and 2 — so a 36-byte CID pasted into a filler value passes
the CDDL while violating the prose that exists precisely to forbid it.

A smaller question sits beside this one: nothing constrains the type 3 (IPFS
URI) string. `spec/03-encoding.md` says its "format is defined by IPFS and is
out of scope", which reads as "any text", so `{"type": 3, "value": ""}` is a
conforming filler pointing at nothing.

## Findings

- `spec/01-data-model.md:91-104`: the `filler` and `filler-value` CDDL, with
  no correlation between the two fields and no `.size 32` on `bstr`.
- `spec/01-data-model.md:124-132`: the filler-type table and the prose that
  do state the correlation and the digest size.
- `spec/03-encoding.md` § "Internal references": filler values of type 0, 1
  and 2 are 32-byte digests, encoded `5820` + digest; the full CID "never
  appears in the fields listed above; the `bstr .size 32` constraint in each
  CDDL definition excludes it" — but for fillers there is no such constraint
  in the CDDL to do the excluding.
- CDDL can express the correlation directly, e.g. as a union of four group
  alternatives, or with control operators; the language is not the obstacle.
- `go/entity` (this implementation) validates against the table: type 0-2
  require a 32-byte byte string, type 3 a non-empty text string, type 4 a
  scalar map, and any other type tag is rejected. The non-empty part of the
  type 3 rule is invented, since the specification says nothing.

## Proposed Solutions

### Option 1: Make the CDDL express the correlation (Recommended)

Replace `filler` with a union of per-type groups, so the schema alone is
enough to validate a filler:

```cddl
filler = atom-filler / bond-filler / molecule-filler / ipfs-filler
       / scalar-filler

atom-filler     = { "type" => 0, "value" => bstr .size 32 }
bond-filler     = { "type" => 1, "value" => bstr .size 32 }
molecule-filler = { "type" => 2, "value" => bstr .size 32 }
ipfs-filler     = { "type" => 3, "value" => tstr }
scalar-filler   = { "type" => 4, "value" => scalar-value }
```

- **Pros**: the table and the schema stop being two sources of truth; the
  `.size 32` constraint appears where "Internal references" claims it already
  is; no prose is needed to make a validator correct.
- **Cons**: the CDDL block grows by four lines.
- **Effort**: Small (spec), none (implementations that already follow the
  table)
- **Risk**: Low

### Option 2: Keep the CDDL and add a normative sentence

- Add "The type of `value` MUST match the type tag as given in the table
  below; implementations MUST reject a filler whose value does not" beneath
  the filler-type table.
- **Pros**: smallest possible diff.
- **Cons**: leaves a schema in the document that accepts invalid documents,
  which is how the divergence arose.
- **Effort**: Trivial
- **Risk**: Low

### Option 3: Leave as is

- **Cons**: two defensible validators disagree about molecule validity.
- **Risk**: High relative to the cost of fixing it.

## Recommended Action

Option 1, plus one sentence settling whether a type 3 IPFS URI may be the
empty string (the recommendation is that it MUST NOT be — an empty content
identifier addresses nothing, and rejecting it costs an implementation one
comparison).

## Technical Details

- **Affected Files**: `spec/01-data-model.md` (`filler` / `filler-value`
  CDDL and the filler-type table), `go/entity/filler.go`
- **Related Components**: Molecule validation, `create_molecule` operation
  validation, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [ ] The CDDL binds each filler type to the shape of its value, or a
      normative sentence does
- [ ] The 32-byte constraint on reference filler values appears in the CDDL,
      as `spec/03-encoding.md` § "Internal references" already claims
- [ ] Whether an empty type 3 string is permitted is stated
- [ ] `go/entity` matches the resolved rules
- [ ] Conformance vectors include a rejected type/value mismatch

## Resources

- [RFC 8610: CDDL](https://datatracker.ietf.org/doc/html/rfc8610) — group
  choices and control operators
- `spec/03-encoding.md` § "Internal references"
- `todos/014-ready-p2-cid-vs-digest-conflation.md` — the related question of
  where CIDs may appear

## Work Log

### 2026-08-13 - Filed During Implementation
**By:** Claude

Found while writing the filler decoder in `go/entity`. Validating strictly
against the filler-type table was the obvious reading, but it is strictly
narrower than the published CDDL, and the gap is invisible to anyone who
implements from the schema alone.

## Notes

Source: Go reference implementation, phase 3 (entity).
