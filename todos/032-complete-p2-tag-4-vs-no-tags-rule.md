---
status: complete
priority: p2
issue_id: "032"
tags: [specification-consistency, encoding, data-model, dcbor]
dependencies: ["003"]
---

# Tag 4 Decimal Fractions vs the "No Tags" Rule

## Problem Statement

Issue #3 was resolved by encoding non-integer scalar values as CBOR tag 4
decimal fractions: `spec/01-data-model.md` now defines
`"value" => int / #6.4([int, int])`. `spec/03-encoding.md` rule 6 still reads
"**No tags.** CBOR tags (major type 6) MUST NOT be used unless explicitly
required by this specification."

The escape clause technically covers tag 4 — it *is* explicitly required by
`spec/01-data-model.md` — but the encoding document, which is the one an
implementer reads to build the codec, never names the exception. Nothing in
`spec/03-encoding.md` mentions tag 4: not the rule, not the "CBOR encoding
reference" table, not the examples. An implementer who builds the encoder from
document 03 (the natural reading order, and the order the implementation plan
follows) produces a codec that rejects valid Dialog scalar fillers, and only
finds out when the data model is implemented one phase later.

The deterministic encoding of the tag itself is also unstated: `c4` followed by
a definite-length two-element array, exponent first, both elements shortest-form
integers, and no other tag accepted. Those are the obvious readings, but they
are the kind of detail this document exists to pin down — it pins down far less
consequential things, such as the encoding of an empty array.

## Findings

- `spec/03-encoding.md:36`: rule 6, "No tags ... unless explicitly required by
  this specification" — no cross-reference to the one place that requires one.
- `spec/01-data-model.md:108`: `"value" => int / #6.4([int, int])`
- `spec/01-data-model.md:137,141`: prose and the `3.14` → `#6.4([-2, 314])`
  example.
- `spec/03-encoding.md:132-147`: the "CBOR encoding reference" table lists map
  heads, string heads, `5820`, `f6`, `00`, `01` and `80` — but not `c4`.
- The dCBOR profile permits tags in general, so nothing outside Dialog's own
  text settles which tags are in scope.
- Consequence for validators: a strict decoder cannot both "reject all tags" and
  "accept scalar fillers". One of the two documents has to give.

## Proposed Solutions

### Option 1: Name the exception in `spec/03-encoding.md` (Recommended)

- Reword rule 6: "**No tags, with one exception.** CBOR tags (major type 6) MUST
  NOT be used, except tag 4 (decimal fraction) where
  [01-data-model.md](01-data-model.md) requires it for scalar filler values.
  Implementations MUST reject every other tag."
- Add the deterministic encoding of tag 4: head `c4`, then a definite-length
  array of exactly two elements, `[exponent, mantissa]`, each a shortest-form
  major type 0 or 1 integer.
- Add a row to the encoding reference table (`c4 82` + two ints) and a worked
  example (`3.14` → `c4 82 21 19 013a`).
- **Pros**: document 03 becomes sufficient to build a conforming codec, which is
  its stated job; validators get an unambiguous rule.
- **Cons**: none beyond the edit.
- **Effort**: Small
- **Risk**: Low

### Option 2: Drop tag 4 and use a plain two-element array

- Encode decimals as `[exponent, mantissa]` with no tag, distinguished by the
  filler's `type` field, which already identifies the value as a scalar.
- **Pros**: the "no tags" rule becomes absolute and Dialog's CBOR subset stays
  minimal; no tag machinery in any implementation.
- **Cons**: reopens Issue #3, which was decided the other way; loses the
  self-describing tag for anyone inspecting the bytes with generic CBOR tooling.
- **Effort**: Small (spec), but a decision reversal
- **Risk**: Medium

## Recommended Action

Option 1. Issue #3's decision stands; document 03 just needs to state it, and
the tag's canonical encoding needs pinning down before the `entity` package
lands.

Current behaviour of the Go reference implementation (`go/dcbor`): **all tags
are rejected**, tag 4 included, with the error "CBOR tags (major type 6) are not
permitted". This is the literal reading of `spec/03-encoding.md` rule 6 and is
correct for every structure implemented so far (no phase-2 or phase-3 structure
carries a scalar filler). It is **not** sufficient for `entity`: a `Decimal`
value type must be added to the dcbor value model, with encode and decode
support, before scalar fillers can round-trip. The rejection is covered by a
regression test (`TestDecodeRejects/tag_4_(decimal_fraction)`) that will need
updating at that point.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` (rule 6, encoding reference table,
  examples), `spec/01-data-model.md` (cross-reference), `go/dcbor/value.go`,
  `go/dcbor/encode.go`, `go/dcbor/decode.go`, `go/dcbor/decode_test.go`
- **Related Components**: Scalar fillers, entity encoding, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/03-encoding.md` rule 6 names tag 4 as the sole exception and points
      at `01-data-model.md`
- [x] The deterministic encoding of a tag 4 value is specified (head, array
      length, element order, shortest-form elements)
- [x] The CBOR encoding reference table includes the tag 4 pattern
- [x] A worked example shows a decimal fraction end to end
- [x] `go/dcbor` gains a Decimal value type accepting exactly tag 4, still
      rejecting every other tag
- [ ] Conformance vectors include a scalar filler with a decimal value
      *(deferred to phase 5, when `cmd/genvectors` lands)*

## Resources

- Original finding: Go reference implementation, phase 2 (`go/dcbor`)
- `todos/003-ready-p1-float-int-contradiction.md` — the decision that introduced
  tag 4
- [RFC 8949 §3.4.4](https://datatracker.ietf.org/doc/html/rfc8949#section-3.4.4)
  — decimal fractions

## Work Log

### 2026-08-12 - Filed During Implementation
**By:** Claude

Found while implementing the `go/dcbor` decoder against rule 6. Implemented the
literal rule (reject every tag) and recorded the gap here rather than
anticipating the data model from a document that does not mention it.

### 2026-08-12 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1. Tag 4 (decimal fraction) is the single
exception to the "no tags" rule, and its encoding is canonicalized so that
each value has exactly one representation — a requirement content addressing
imposes but RFC 8949 does not.

**Ratified canonicalization rules (all normative MUSTs):**

1. Tag 4 is used only for values not representable as a CBOR integer: the
   exponent MUST be negative. Whole numbers are plain integers.
2. The mantissa MUST NOT be divisible by 10 (trailing zeros are stripped into
   the exponent) and MUST NOT be zero (zero is a whole number, rule 1).
3. Decoders MUST reject tag-4 values violating these rules as non-canonical.

**Changes:**

- `spec/03-encoding.md`: rule 6 reworded to "No tags, with one exception",
  naming tag 4 and pointing at `01-data-model.md`; new "Decimal fractions"
  subsection giving the deterministic encoding (`c4`, definite two-element
  array `[exponent, mantissa]`, shortest-form major type 0/1 integers) and the
  three canonicalization rules; new encoding-reference row
  (`c482` + exponent + mantissa); new worked example `3.14` → `c4 82 21
  19 013a`.
- `spec/01-data-model.md` § Scalars: cross-reference to the canonicalization
  rules and a note that whole numbers use plain integer encoding. The CDDL
  (`int / #6.4([int, int])`) is unchanged — it remains structurally correct.
- `go/dcbor`: new `Decimal{Exponent, Mantissa int64}` value kind;
  `NewDecimal` canonicalizes (strips trailing zeros, returns `Uint`/`Neg` when
  the value is whole); `Encode` refuses to emit a non-canonical `Decimal`;
  `Decode` accepts exactly `c4` + definite two-element array of shortest-form
  integers and enforces all three rules with descriptive errors, still
  rejecting every other tag. The int64 bound on both components is documented
  on the type; widening it would need a big-integer value kind.
- Tests: the worked example both ways, negative mantissa, int64 boundaries,
  a decimal inside a filler-shaped map, and rejection cases for positive/zero
  exponent, zero mantissa, mantissa divisible by 10, wrong array length,
  floats/text/tags inside the array, indefinite array inside the tag,
  non-shortest elements, and other tags. The former
  `TestDecodeRejects/tag_4_(decimal_fraction)` case is replaced by the tag-4
  acceptance and canonicalization cases; the remaining tag cases now expect
  "except tag 4". Fuzz corpus seeded with canonical and non-canonical tag-4
  inputs; `-fuzztime=10s` clean.

## Notes

Source: Go reference implementation, phase 2 (dcbor + cid).
