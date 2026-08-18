---
status: complete
priority: p2
issue_id: "058"
tags: [conformance-vectors, data-model, validation, interoperability]
dependencies: []
---

# `entities.json` Pins No Invalid Entity

## Problem Statement

`spec/01-data-model.md` is a document of MUSTs about rejection: a description
MUST be non-empty, a template MUST contain one or more variables with unique
names, a filler's value MUST match its type tag, an IPFS URI MUST NOT be empty,
a timestamp MUST be the one canonical spelling of a real Gregorian date, and
`from` MUST NOT be later than `to`. `vectors/entities.json` demonstrates none of
them. Its five sections are 26 *valid* cases; there is no `invalid` section, and
so no byte string a conforming implementation is required to refuse.

`vectors/dcbor.json` has 54 invalid cases and `blocks.json` has 23 — three of
which (`filler_type_out_of_range`, `filler_value_shape_mismatch`, `empty_fillers`)
even cite `spec/01-data-model.md`, from the block file, where they are reachable
only by an implementation that has already built blocks. Everything else the
data model refuses is untested by the interop contract.

The consequence is the one `vectors/README.md` names: "If your implementation
reproduces them, it interoperates." Two implementations can pass every entity
vector and still disagree about which entities exist. One accepts
`{"description": ""}`, `{"template": "hello"}` or a range running backwards, mints
digests for them and publishes blocks carrying them; the other refuses to parse
those blocks. Since a digest is an identity, an entity one node stores and
another cannot read is exactly the divergence the vectors exist to prevent — and
it is invisible today, because nothing tests for it.

The timestamp rules are the sharpest case. Rule 6 of "Datetime ranges" fixes the
calendar as proleptic Gregorian and works the consequence out in prose —
`1500-02-29T00:00:00Z` is not a Dialog timestamp, `1600-02-29T00:00:00Z` is — and
neither string appears in the vectors. An implementation that validates with its
platform's date parser (JavaScript's `Date`, which rolls `2024-02-30` forward to
March 1; a Julian-aware library; anything accepting `+00:00`) passes every
committed vector.

## Findings

- `vectors/entities.json`: sections `atoms` (5), `bonds` (2), `meta_bonds` (5),
  `molecules` (3), `fillers` (11). All valid, no `invalid` section, no `rule`
  or `reason` field anywhere in the file.
- `vectors/README.md`, "Case shapes": the `invalid` shape (`bytes`, `rule`,
  `reason`) is defined generically — "**Invalid cases** (`invalid` sections)" —
  and the entities row of the file table lists no such section.
- `vectors/blocks.json`, `invalid`: `filler_type_out_of_range`,
  `filler_value_shape_mismatch` and `empty_fillers` cite
  "spec/01-data-model.md, Filler types". They are entity rules pinned in the
  block file, and they are the only three.
- Rules stated in `spec/01-data-model.md` with no vector at all: non-empty
  description; non-empty template; at least one variable; unique variable names;
  the leftmost-longest disambiguation table (`_A_B_`, `_A__B_`, `type_of`,
  `_a_`); non-empty IPFS URI; the six timestamp rules; range ordering; the
  closed-map rule as it applies to an entity map and to a scalar value map
  (`{"unit": …}` with no `"value"`, `{"from": …}` with no `"to"`, a range
  carrying a `unit`).
- The valid side has gaps of its own: no vector carries a leap day, a
  pre-1582 date, or a bond template exercising any row of the disambiguation
  table — `bonds` and `meta_bonds` are seven templates of the shape `_A_ … _B_`.

## Proposed Solutions

### Option 1: Add an `invalid` section to `entities.json` (Recommended)

One case per MUST, in the shape the other files already use — `bytes`, `rule`
naming the section of `spec/01-data-model.md`, `reason`. A minimal set:

- `empty_description`, `description_not_text`
- `empty_template`, `template_without_variable`, `template_repeats_variable`
- `bond_reference_is_a_cid` (36 bytes where 32 are required)
- `filler_type_5`, `filler_type_0_short_value`, `filler_type_3_empty_uri`,
  `filler_type_3_bytes_value`
- `scalar_unit_without_value`, `scalar_range_missing_endpoint`,
  `scalar_range_with_unit`, `scalar_value_is_text`
- `timestamp_lowercase_z`, `timestamp_numeric_offset`,
  `timestamp_fractional_seconds`, `timestamp_leap_second`,
  `timestamp_not_a_real_date` (`2024-02-30`), `timestamp_julian_leap_day`
  (`1500-02-29`), `range_inverted`
- an entity map with an undeclared key, and one missing a required key, so that
  rule 8 is pinned at the entity layer as well as the block layer

Plus a handful of valid cases the current file lacks: a leap day
(`1600-02-29T00:00:00Z`), a template from each row of the disambiguation table,
and a molecule with an IPFS filler.

- **Pros**: the entity layer gains the property the dCBOR layer already has —
  passing the vectors means agreeing about what exists, not only about what
  bytes valid things have. Costs nothing at runtime and breaks no committed
  byte.
- **Cons**: the generator grows a case list; some cases (a template with no
  variable) are only invalid at the entity layer, so the file's readers must
  understand that `bytes` is well-formed dCBOR that the *data model* refuses.
- **Effort**: Medium (generator + vectors), Small (implementations)
- **Risk**: Low — additive; no existing case changes.

### Option 2: Put the entity rejection cases in `blocks.json`

Extend the three that are already there to cover the rest, on the grounds that
an entity only reaches the wire inside a block.

- **Pros**: no new section; matches where validation happens in a node.
- **Cons**: an implementation working through the vectors in the order
  `vectors/README.md` prescribes builds entities before blocks and would have no
  rejection tests at the step where it writes the code; and the cases would test
  block validation rather than the data model, which is the same conflation the
  three existing cases already create.
- **Risk**: Medium

### Option 3: Leave it, and treat the spec text as sufficient

- **Pros**: nothing to do.
- **Cons**: the interop contract stays silent on the whole rejection half of
  `spec/01-data-model.md`, and divergence stays invisible.
- **Risk**: High for a protocol whose identifiers are hashes.

## Recommended Action

Option 1. The timestamp cases matter most — they are where a platform's date
library will quietly disagree with the specification — and the calendar pair
(`1500-02-29` rejected, `1600-02-29` accepted) should be in the file verbatim,
since the prose already works that example out.

## Technical Details

- **Affected Files**: `vectors/entities.json` (new `invalid` section, a few
  added valid cases), `vectors/README.md` (the file table's section counts),
  the vector generator, both implementations' entity test suites
- **Related Components**: data model, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `vectors/entities.json` carries an `invalid` section covering every
      rejection rule of `spec/01-data-model.md`
- [x] The timestamp profile's six rules and the calendar rule are each pinned by
      at least one case, including the `1500-02-29` / `1600-02-29` pair
- [x] `vectors/README.md` records the new section and its case count
- [x] Both implementations reject every case in it

## Work Log

### 2026-08-18 - Filed While Implementing ts/src/entity.ts

**By:** Claude

Found building the second implementation from `spec/` and `vectors/` only. The
TypeScript entity layer enforces every rule the specification states and rejects
each with its own error code, but the tests that prove it are hand-written from
the prose — `ts/test/entity.test.ts`, everything below "Rejections" — and
nothing in `vectors/` says another implementation must agree with any of them.
The valid half of the file, by contrast, passed on the first run: 26 cases, all
byte-identical, which is what a good vector file feels like.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1. `vectors/entities.json` gains an
`invalid` section, and the timestamp cases are the sharp end of it.

**Changes:**

- `go/internal/vectors/entities.go`: an `invalid` section of **38 cases**,
  each with `bytes`, a `rule` naming the section of the specification it
  violates, a `reason`, and a new `kind` field — `atom`, `bond`, `molecule` or
  `filler` — naming the decoder that must refuse it, because the entity layer
  has one decoder per kind. Coverage: the empty and non-text description; the
  empty template, the one with no variable, the one whose variables are
  lowercase and the one that repeats a name; the molecule with no fillers, an
  empty filler list, an undeclared key and a CID in its `bond` field; type 5,
  a text type tag, a 31-byte reference, a CID reference, a text reference, the
  empty and byte-string IPFS URI, a bare-integer scalar, and a filler map with
  an extra or a missing key; a unit without a value, a 31-byte unit, a text
  quantity, an undeclared scalar key, a non-canonical decimal fraction, a
  half-written range and a range carrying a unit; and one case per timestamp
  rule — the plain date, `+00:00`, lowercase `t` and `z`, fractional seconds,
  the leap second, `2024-02-30`, `1500-02-29`, and a range running backwards.
- The generator checks every case against the reference decoder before it
  emits it (`mustReject`), so the file cannot come to pin a rejection this
  implementation does not make.
- Two valid cases joined them, both of which the file lacked:
  `fillers/scalar_datetime_leap_day` (`1600-02-29`, the accepting half of the
  calendar pair rule 6 works out in prose) and `molecules/truth_of_an_atom`
  (todo 059).
- `go/conformance_test.go`: `TestEntityVectors` decodes every invalid case with
  the decoder its `kind` names and fails if any of them is accepted.
- `ts/test/entity.test.ts`: the same, plus a `rule` → `EntityErrorCode` map, so
  a case is not merely rejected but rejected as the right class of error; the
  one dCBOR-layer case is listed as such. Count assertions updated in
  `entity.test.ts` and `cid.test.ts`, and the latter skips the new section.
- `vectors/README.md`: the entities row, the `kind` field in "Case shapes", and
  a paragraph in the walkthrough on why the invalid half is the one that
  decides which entities exist.

Not done, and not needed for this todo: the disambiguation-table bond templates
and a molecule with an IPFS filler. Both are exercised by unit tests in both
implementations; neither is a rejection rule.

## Notes

Source: TypeScript implementation, phase 2 (entity).
