---
status: complete
priority: p3
issue_id: "057"
tags: [specification-gap, encoding, dcbor, security, interoperability]
dependencies: []
---

# A dCBOR Document Has No Nesting Bound

## Problem Statement

`spec/03-encoding.md` fixes every byte of the encoding but says nothing about
how deeply arrays and maps may nest. Two consequences follow, and they pull in
opposite directions:

1. **Without a limit, a decoder is a denial-of-service target.** `8181818181…`
   — one byte per level — nests as deep as the input is long. A recursive
   decoder, which is what every implementation writes, exhausts its stack and
   dies with a language-level failure (a `RangeError` in JavaScript, a goroutine
   stack overflow in Go) rather than returning a clean rejection. A few kilobytes
   of input suffice. Blocks arrive from the network, so the input is hostile by
   default.
2. **With a limit, the limit is a compatibility surface.** Any implementation
   that imposes one — and all of them must — rejects documents another
   implementation accepts. Since the digest of a document is its identity, an
   entity that one node stores and another refuses to parse is an interop
   failure of exactly the kind the vectors exist to prevent, and nothing in the
   specification says where the boundary is.

No conformance vector exercises deep nesting, so the divergence is currently
invisible: two implementations can pick 128 and 10000 and both pass.

Real Dialog documents are shallow. The deepest structure the protocol defines is
a block containing `ops`, an operation containing a molecule, the molecule's
`fillers`, a filler, its scalar value map — about six levels. The bound is
therefore free to be generous and still purely defensive.

## Findings

- `spec/03-encoding.md`, "Deterministic CBOR": rules 1-8 cover integers, key
  order, duplicates, indefinite lengths, floats, tags, simple values and closed
  maps. Nothing about depth, and nothing about a maximum document size either.
- RFC 8949 §5.6 ("Generic encoders and decoders") notes that implementations
  commonly impose limits on nesting depth and expects them to be documented; it
  sets no value.
- `vectors/dcbor.json` has no nesting case beyond the two levels of
  `nested_structure`.
- `spec/02-block-format.md` bounds no structure by depth either; the CDDL's
  nesting is fixed and shallow, so a bound cannot break a valid document.

## Proposed Solutions

### Option 1: State a fixed maximum nesting depth (Recommended)

Add to `spec/03-encoding.md`, "Deterministic CBOR":

> **Nesting depth.** A Dialog document MUST NOT nest arrays and maps more than
> N levels deep, and decoders MUST reject one that does. The protocol's own
> definitions nest no more than a handful of levels; the bound exists so that a
> decoder can refuse hostile input without exhausting its stack.

A generous power of two — 256 or 1024 — is far above anything the protocol
defines and far below what threatens a stack.

- **Pros**: one number, identical rejections everywhere, no legitimate document
  affected; makes the security property explicit rather than accidental.
- **Cons**: another fixed parameter to carry.
- **Effort**: Small (spec), Small (implementations)
- **Risk**: Low

### Option 2: Bound the depth of each defined structure instead

Say that a decoder decodes into the CDDL definitions, whose depth is fixed, so
no generic bound is needed.

- **Pros**: no arbitrary constant.
- **Cons**: does not help a schema-free `Decode(bytes)`, which is the layer the
  dCBOR vectors test and the layer that faces the network first.
- **Risk**: Medium

### Additionally: add a conformance vector

Whatever the bound, `vectors/dcbor.json` should carry one `invalid` case that
exceeds it (a run of `81` bytes) so that the divergence stops being invisible.

## Recommended Action

Option 1 with N = 1024, plus the vector. If a smaller bound is preferred, any
value at least an order of magnitude above the protocol's own nesting works
equally well; what matters is that the number is written down once.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` ("Deterministic CBOR"),
  `vectors/dcbor.json` (one new `invalid` case), every implementation's decoder
- **Related Components**: dCBOR codec, block ingestion
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification states a maximum nesting depth (or states that there is
      none and how decoders are to survive without one)
- [x] A conformance vector exercises the bound
- [x] Implementations reject over-deep input as a protocol error, never as a
      stack overflow

## Work Log

### 2026-08-18 - Filed While Implementing ts/src/dcbor.ts
**By:** Claude

Found building the second implementation from `spec/` and `vectors/` only. The
TypeScript codec takes the defensible reading: a documented
`MAX_NESTING_DEPTH = 1024` enforced on both encode and decode, raising a `depth`
error. The value is a guess — the specification supplies none — so it is the one
place this implementation could differ from another and still pass every vector.

### 2026-08-18 - Ratified and Applied

**By:** Claude

**Decision (project lead):** Option 1 with **N = 64**, not the 1024 this todo
proposed. Protocol data needs single digits of nesting, so a tighter bound
costs nothing and refuses more; what the number buys is that the accept/reject
decision is identical across implementations and that a stack-exhaustion class
disappears.

The counting convention turned out to matter more than the number. Rule 10
settles it, and both implementations conform to *that* — neither counted this
way before:

- A **container** is an array or a map. Its depth is 1 when no container
  encloses it, one more than its enclosing container otherwise.
- Items that are not containers add nothing; they carry the depth of the
  container holding them.
- A decimal fraction is **one** container: tag 4 and its two-element content
  array together.

**Changes:**

- `spec/03-encoding.md`, "Deterministic CBOR": **rule 10, "Bounded nesting
  depth"**, with the definition above and the bound of 64; an informative
  paragraph on why a fixed number rather than an implementation's choice; a
  security-considerations bullet on rejecting rather than recursing.
- `go/dcbor`: `MaxDepth` was already 64 but charged a level for every item and
  checked on entry, so 65 nested arrays passed when the innermost was empty and
  failed when it held an integer. Encoder and decoder now count containers
  through one `enter` helper each. No committed vector moved.
- `ts/src/dcbor.ts`: `MAX_NESTING_DEPTH` 1024 → 64, counted the same way.
- `vectors/dcbor.json`: `canonical/nesting_depth_64` (accepted, both
  directions) and three `invalid` cases — `nesting_depth_65`,
  `nesting_depth_65_decimal` (the tag 4 half of the rule) and
  `nesting_far_beyond_the_bound` (256 levels, the stack-exhaustion case).
  Section counts in `vectors/README.md`: canonical 25 → 26, invalid 51 → 54,
  and `ts/test/dcbor.test.ts` asserts them.

## Notes

Source: TypeScript implementation, phase 1 (dcbor + cid).
