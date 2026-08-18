---
status: complete
priority: p2
issue_id: "056"
tags: [specification-gap, encoding, dcbor, conformance-vectors]
dependencies: []
---

# "Map Keys Are Text Strings" Is a Vector Rule With No Normative Text

## Problem Statement

`vectors/dcbor.json` requires a conforming decoder to reject two byte strings
whose stated rule names no section of the specification:

- `map_key_uint` — `a10102`, rule
  *"spec/03-encoding.md, Deterministic CBOR (map keys are text strings)"*
- `map_key_bytes` — `a1410102`, same rule

`spec/03-encoding.md` never says that. Its eight deterministic rules constrain
key *order* (rule 2) and key *uniqueness* (rule 3) over "their CBOR encoding",
wording that presumes nothing about the key's major type, and RFC 8949 §4.2.1 —
which the section builds on — explicitly admits keys of any type. Every other
invalid case in the file cites a numbered rule or an RFC section; these two cite
a parenthetical.

The closest normative hook is rule 8 (closed maps): every map "carries exactly
the key set its definition declares", and every definition in the specification
declares text keys, so an integer key is an undeclared key. That reasoning works
for a decoder that knows which definition it is decoding. It does not work for
the layer the vectors are testing: `Decode(bytes)` with no schema in hand, which
is exactly what `vectors/dcbor.json` exercises and what an implementation builds
first. A clean-room codec written from `spec/03-encoding.md` alone accepts
`a10102`, and only the vectors tell it otherwise.

This matters beyond tidiness. The map-key type is inside the hashed bytes: a
decoder that accepts an integer key computes a digest for a structure another
implementation refuses, which is the precise failure mode the section's own
informative note about rule 8 describes.

## Findings

- `spec/03-encoding.md`, "Deterministic CBOR", rules 1-8: no rule restricts the
  major type of a map key. Rule 2 sorts "their CBOR encoding"; rule 3 forbids
  duplicates; rule 8 governs the key *set* of a defined map.
- RFC 8949 §4.2.1, the profile's stated base, sorts keys of arbitrary type and
  permits them.
- `vectors/dcbor.json`, `invalid` section, cases `map_key_uint` and
  `map_key_bytes`: rejection required, rule cited as a parenthetical.
- Every CDDL definition in `spec/01-data-model.md` and `spec/02-block-format.md`
  uses text keys exclusively, so nothing in the protocol *needs* another key
  type — the restriction is uncontroversial, merely unwritten.

## Proposed Solutions

### Option 1: State it as a numbered rule (Recommended)

Add to `spec/03-encoding.md`, "Deterministic CBOR", a rule 9:

> **Text map keys.** Every map key MUST be a text string (major type 3).
> Implementations MUST reject a map whose key is of any other major type,
> whether or not the map's definition is known.

- **Pros**: the codec layer becomes implementable from the specification alone,
  which is the property `vectors/README.md` claims for the whole document set;
  the vector's `rule` string gains a number to cite; rejection no longer depends
  on knowing which definition a map belongs to.
- **Cons**: one more rule; forecloses non-text keys in a future version (which
  would be a protocol-version change anyway).
- **Effort**: Small (spec), none (implementations already do this)
- **Risk**: Low

### Option 2: Leave the rule implicit and re-cite the vector to rule 8

Change the two cases' `rule` strings to name rule 8 and add a sentence to rule 8
saying an undeclared key includes a key that is not a text string.

- **Pros**: no new rule.
- **Cons**: rule 8 is about a map's key *set* against its definition; a
  schema-free decoder has no definition to check against, so the citation
  explains the rejection only after the fact.
- **Risk**: Medium — leaves the first thing an implementer builds unspecified.

## Recommended Action

Option 1, and update the two vectors' `rule` strings to cite the new rule
number. The vectors do not change byte-for-byte, so this is not a breaking
change to the interop contract.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` ("Deterministic CBOR"),
  `vectors/dcbor.json` (`rule` strings of `map_key_uint` and `map_key_bytes`,
  regenerated), any implementation's dCBOR decoder
- **Related Components**: dCBOR codec, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/03-encoding.md` states, normatively, that map keys are text strings
- [x] The two `invalid` cases cite that statement
- [x] A codec written from `spec/03-encoding.md` alone rejects `a10102`

## Work Log

### 2026-08-18 - Filed While Implementing ts/src/dcbor.ts
**By:** Claude

Found building the second implementation from `spec/` and `vectors/` only. The
TypeScript decoder rejects a non-text map key with its own error class
(`map-key-type`), which is the behaviour the vectors demand; the specification
text it would cite does not exist.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. Map keys are text strings, stated as a
numbered rule of the profile rather than inferred from rule 8.

**Changes:**

- `spec/03-encoding.md`, "Deterministic CBOR": **rule 9, "Text map keys"** —
  every map key MUST be a text string (major type 3); encoders MUST NOT emit
  and decoders MUST reject a key of any other major type, whether or not the
  map's definition is known. An informative note places it as the schema-free
  half of rule 8: rule 8 answers the question for a decoder holding a
  definition, rule 9 for the one holding only bytes, which is the layer
  `vectors/dcbor.json` tests and the first layer an implementer builds.
- `vectors/dcbor.json`: `map_key_uint` and `map_key_bytes` now cite
  "Deterministic CBOR rule 9 (text map keys)". No byte moved; the generator's
  `ruleTextKeys` constant carries the same string.
- `go/dcbor`: no behaviour change; `textKey` cites rule 9.
- `ts/src/dcbor.ts`: no behaviour change; the `map-key-type` error code is
  documented against rule 9, and the conformance suite maps the rule string to
  it by number like every other rule.

## Notes

Source: TypeScript implementation, phase 1 (dcbor + cid).
