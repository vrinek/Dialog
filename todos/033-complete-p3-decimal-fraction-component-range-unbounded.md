---
status: complete
priority: p3
issue_id: "033"
tags: [specification-gap, encoding, data-model, dcbor]
dependencies: ["032"]
---

# Decimal Fraction Component Range Is Unbounded

## Problem Statement

Issue #32 pinned the deterministic encoding of a tag 4 decimal fraction:
`c4`, a definite two-element array `[exponent, mantissa]`, both shortest-form
major type 0 or 1 integers. It did not bound their *magnitude*, and neither
does the CDDL in `spec/01-data-model.md`, which says `#6.4([int, int])`.

CBOR's `int` spans −2^64 … 2^64−1, so a conforming encoder may emit a mantissa
of, say, 2^63 (`1b8000000000000000`) or an exponent of −2^64. Both are legal
under every rule the spec states. An implementation that stores the components
in a 64-bit *signed* integer — the natural choice, and what the Go reference
implementation does — cannot represent them and must reject the document. Two
conforming implementations therefore disagree on whether a valid Dialog block
can be decoded, which is the class of bug the encoding document exists to
eliminate.

The same question does not arise for plain integers: `Uint` and `Neg` in
`go/dcbor` carry the full CBOR range, because a bare integer's value fits a
`uint64` on either side of zero. It arises for tag 4 only because a decimal
fraction's components are *signed* quantities, and a signed 64-bit type covers
only half of CBOR's negative range.

## Findings

- `spec/03-encoding.md` § "Decimal fractions": specifies head, array length,
  element order, shortest form and the three canonicalization rules; says
  nothing about magnitude.
- `spec/01-data-model.md:108`: `"value" => int / #6.4([int, int])` — `int` is
  RFC 8610's unrestricted integer, both signs, full 64-bit magnitude.
- `go/dcbor` (`decode.go`, `decimalInt`) rejects any component above
  `math.MaxInt64` with "decimal fraction exponent/mantissa is outside the
  int64 range". `Decimal` holds two `int64`s, and the doc comment on the type
  records the limit as deliberate for v1.
- No Dialog structure needs a mantissa beyond 2^63: the scalar filler carries
  measurements and quantities, not arbitrary-precision numbers. The exponent
  is even less demanding — an exponent below −10^3 has no plausible use.
- An exponent of −2^64 with a legal mantissa also denotes a number no
  implementation can compute with; permitting it buys nothing and costs a
  bignum path in every language binding.

## Proposed Solutions

### Option 1: Bound both components to the int64 range (Recommended)

- Normative: "Both the exponent and the mantissa MUST be in the range
  −2^63 … 2^63−1. Implementations MUST reject a decimal fraction whose
  components fall outside it."
- Tighten the CDDL to a range-constrained type, or note the constraint in
  prose beside it.
- **Pros**: every implementation can use its native 64-bit signed integer; no
  bignum dependency; matches what `go/dcbor` already enforces; the rejection
  becomes a spec rule instead of an implementation limit.
- **Cons**: a hard ceiling that a future version would have to lift with a
  protocol version bump.
- **Effort**: Trivial (spec), none (Go)
- **Risk**: Low

### Option 2: Bound the exponent tightly, leave the mantissa wide

- Exponent in, say, −32768 … −1; mantissa unrestricted within CBOR's `int`.
- **Pros**: keeps room for high-precision quantities.
- **Cons**: still forces a bignum representation for the mantissa in every
  implementation, which is the expensive half.
- **Effort**: Small (spec), Medium (implementations)
- **Risk**: Medium

### Option 3: Permit the full CBOR range and require bignum handling

- **Pros**: no artificial ceiling; the CDDL as published becomes accurate.
- **Cons**: every language binding needs arbitrary-precision arithmetic to
  decode a scalar filler; enormous cost for a case with no known use.
- **Effort**: Large
- **Risk**: Medium

## Recommended Action

Option 1, unless someone has a scalar in mind that needs more than a signed
64-bit mantissa. Until it is decided, `go/dcbor` rejects out-of-range
components with a descriptive error, which is the conservative reading: it
never produces a wrong value, only refuses one it cannot represent.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` (§ Decimal fractions),
  `spec/01-data-model.md` (`scalar-value` CDDL), `go/dcbor/value.go`,
  `go/dcbor/decode.go`
- **Related Components**: Scalar fillers, entity encoding, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/03-encoding.md` states the permitted range of the exponent and the
      mantissa, or states explicitly that both are unbounded within CBOR's
      `int`
- [x] The CDDL in `spec/01-data-model.md` agrees with that range
- [x] `go/dcbor` matches the resolved rule, and the doc comment on `Decimal`
      cites the spec rather than describing a local limit
- [x] Conformance vectors include a boundary case at each end of the range
      (encoder tests cover the four boundary values; the `vectors/` files
      themselves land in phase 5 and are generated from these values)

## Resources

- Original finding: applying the ratified decision of issue #32
- `todos/032-complete-p2-tag-4-vs-no-tags-rule.md`
- [RFC 8949 §3.4.4](https://datatracker.ietf.org/doc/html/rfc8949#section-3.4.4)
  — decimal fractions
- [RFC 8610](https://datatracker.ietf.org/doc/html/rfc8610) — CDDL `int`

## Work Log

### 2026-08-12 - Filed During Implementation
**By:** Claude

Surfaced while implementing the ratified tag 4 rules of issue #32. The three
canonicalization rules were implementable exactly as written; the magnitude of
the two components was the one property the decision did not settle, and the
implementation had to pick a representation before it could decode anything.

### 2026-08-13 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1. The exponent and the mantissa of a tag 4
decimal fraction are each bounded to the signed 64-bit range
`-2^63 … 2^63-1`; decoders MUST reject a decimal fraction whose components
fall outside it. This is exactly what `go/dcbor` already enforced, so the
rejection is now a spec rule rather than an implementation limit.

**Changes:**

- `spec/03-encoding.md` § "Decimal fractions": new normative paragraph after
  the structural rules stating the bound as a MUST for encoders and decoders,
  with the rationale (native 64-bit signed integers everywhere, no bignum path
  in any language binding) and a note that a future protocol version may raise
  it.
- `spec/01-data-model.md` § Scalars: the same bound stated in prose, and a
  comment on the `scalar-value` CDDL pointing at the encoding document. The
  CDDL structure (`int / #6.4([int, int])`) is unchanged.
- `go/dcbor/value.go`: the `Decimal` doc comment now cites the spec rule
  instead of describing a local representation limit.
- `go/dcbor/decode_test.go`: rejection cases now cover both directions for
  both components — mantissa above and below the range, exponent above and
  below it — plus the two just-past-the-boundary values
  (`1b8000000000000000` and `3b8000000000000000`). The in-range boundary
  values (exponent `-2^63`, mantissa `2^63-1` and `-2^63`) were already
  covered by the encoder round-trip table.

## Notes

Source: Go reference implementation, phase 2 (dcbor + cid).
