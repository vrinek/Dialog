---
status: complete
priority: p2
issue_id: "031"
tags: [specification-gap, encoding, dcbor, interoperability]
dependencies: []
---

# dCBOR Profile Summary Is Incomplete

## Problem Statement

`spec/03-encoding.md` § "Deterministic CBOR" says all Dialog CBOR "MUST conform
to the dCBOR application profile" and then gives seven rules "in summary". An
implementer reading only those seven rules — which is what the phrasing invites,
and what the rest of the document's worked examples support — builds an encoder
that is *not* dCBOR-conforming, because the summary omits requirements that
change the bytes on the wire and therefore change every digest and CID.

Three specific gaps:

1. **Text normalization.** The dCBOR draft requires text strings to be in
   Unicode Normalization Form C (NFC). The summary does not mention it. Two
   implementations given the same user-visible string in different normal forms
   (e.g. `"é"` as U+00E9 versus U+0065 U+0301, which is routine on macOS
   filesystems and in some IME output) will produce different atom digests for
   what is the same logical atom. That defeats the content-addressing property
   the document opens with: "the same entity created by different authors
   produces the same identifier".
2. **UTF-8 well-formedness.** Neither the summary nor the CDDL says whether a
   decoder must reject a text string that is not well-formed UTF-8. RFC 8949
   requires text strings to be valid UTF-8, but the spec's normative framing is
   "the dCBOR profile", not "RFC 8949 plus these rules", so the obligation is
   inherited only indirectly.
3. **Simple values other than null.** Rule 5 bans floats and rule 7 permits
   null. Booleans (`0xf4`/`0xf5`) and `undefined` (`0xf7`) are simply not
   mentioned — and dCBOR itself *permits* booleans, so "conform to dCBOR" and
   Dialog's own list give different answers. No Dialog structure uses a boolean
   today, so the practical question is what a *validator* must do when it meets
   one: accept it as legal-but-unused, or reject the document.

Underlying all three: Dialog's profile is strictly **narrower** than dCBOR (rule
5 already forbids floats, which dCBOR allows after numeric reduction), so "MUST
conform to the dCBOR application profile" is not literally what Dialog means. The
document would be more accurate, and more useful, as a self-contained definition.

## Findings

- `spec/03-encoding.md:27-37`: the "in summary" list; rules 1-7 as published.
- `spec/03-encoding.md:21`: "Two conforming implementations encoding the same
  logical structure MUST produce identical bytes" — the property that gap 1
  breaks.
- The dCBOR draft (draft-mcnally-deterministic-cbor, revision 12), cited as
  normative, requires encoders to "only emit text strings that are in Unicode
  Normalization Form C (NFC)" and decoders to "reject any encoded text strings
  that are not in NFC". Nothing in Dialog's summary reflects this, and no Dialog
  document mentions normalization anywhere.
- The same draft states that "only the three 'simple' (major type 7) values
  `false` (0xf4), `true` (0xf5), and `null` (0xf6) and the floating point values
  are valid in dCBOR" — so booleans are dCBOR-valid, and floats are too (after
  the draft's numeric-reduction step), both of which Dialog intends to exclude.
- Atom descriptions (`spec/01-data-model.md`) are free-form user text, so gap 1
  is reachable from ordinary use, not only from adversarial input.
- No CDDL in the spec constrains a value to `bool`, and no operation or filler
  type carries one, so gap 3 is currently about validator strictness only.

## Proposed Solutions

### Option 1: Make the summary exhaustive and self-contained (Recommended)

- Restate the rule set as the complete definition of Dialog's profile rather than
  a summary of someone else's, adding:
  - Text strings MUST be well-formed UTF-8 and MUST be in Unicode Normalization
    Form C (NFC). Encoders MUST normalize before encoding; decoders MUST reject
    text that is not NFC.
  - The only major type 7 value permitted is `null` (`0xf6`). Booleans,
    `undefined`, and all other simple values MUST be rejected.
- Keep the dCBOR draft as an informative "this profile is a subset of" pointer.
- **Pros**: an implementer can build a conforming encoder from this document
  alone, which is what a wire-format spec is for; closes the digest-divergence
  hole.
- **Cons**: adds a Unicode normalization dependency to every implementation
  (in Go, `golang.org/x/text/unicode/norm`; the zero-dependency rule in
  `docs/plans/2026-08-12-go-reference-implementation.md` would need revisiting,
  or a decoder-side NFC *check* could be implemented without a full normalizer).
- **Effort**: Small for the spec, Medium for implementations
- **Risk**: Low

### Option 2: Reference the dCBOR draft normatively and drop the summary

- Say "Dialog CBOR is dCBOR, restricted to the following types" and delete the
  seven-rule list.
- **Pros**: no chance of the summary drifting from the draft.
- **Cons**: the draft is unstable and its later revisions add rules (numeric
  reduction) that interact awkwardly with Dialog's "no floats" position;
  implementers lose a readable local definition.
- **Effort**: Trivial
- **Risk**: Medium

### Option 3: Explicitly narrow the profile — Dialog is not full dCBOR

- Say Dialog uses "a restricted deterministic CBOR profile compatible with, but
  narrower than, dCBOR", list the rules including UTF-8 and the major-7
  restriction, and explicitly decide *against* an NFC requirement, documenting
  that callers are responsible for normalizing text before it enters the
  protocol.
- **Pros**: no normalization dependency; matches what the seven rules already
  describe.
- **Cons**: pushes a content-addressing correctness requirement onto every
  application; two honest applications will still disagree on a digest.
- **Effort**: Small
- **Risk**: Medium

## Recommended Action

Option 1 for gaps 2 and 3 in any case — those are cheap and uncontroversial.
Gap 1 (NFC) needs a decision, because it is the one that costs implementations a
Unicode dependency; Option 3 is the honest fallback if that cost is judged too
high, but the trade-off should be written down rather than left silent.

Current behaviour of the Go reference implementation (`go/dcbor`), pending that
decision:

- Text strings and map keys that are not well-formed UTF-8 are rejected by both
  the encoder and the decoder.
- Every major type 7 value except `null` is rejected with a descriptive error,
  booleans and `undefined` included.
- **NFC is not enforced**, because it cannot be done in the standard library
  alone and the plan's zero-dependency rule is in force. This is a known,
  deliberate deviation from dCBOR and is recorded here rather than in code
  comments alone.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` (§ Deterministic CBOR),
  `go/dcbor/value.go`, `go/dcbor/decode.go`,
  `docs/plans/2026-08-12-go-reference-implementation.md` (dependency rule, if
  NFC is adopted)
- **Related Components**: Content addressing, atom digests, conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] The profile section states whether NFC normalization is required, and of
      whom (encoder, decoder, or both) — it is forbidden, of both
- [x] UTF-8 well-formedness is stated normatively
- [x] The treatment of booleans, `undefined` and other simple values is stated
- [x] `go/dcbor` matches the resolved rules
- [x] If NFC is adopted, conformance vectors include a non-NFC rejection case
      — not applicable: NFC was rejected, so there is no such case

## Resources

- Original finding: Go reference implementation, phase 2 (`go/dcbor`)
- [draft-mcnally-deterministic-cbor](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/)
- [RFC 8949 §3.1](https://datatracker.ietf.org/doc/html/rfc8949#section-3.1) —
  text strings are UTF-8
- [Unicode UAX #15](https://unicode.org/reports/tr15/) — normalization forms

## Work Log

### 2026-08-12 - Filed During Implementation
**By:** Claude

Found while implementing the strict `go/dcbor` decoder. Every rejection rule in
the seven-point summary was implementable as written; these three questions had
no answer in the spec and had to be decided in code.

### 2026-08-12 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** the profile becomes self-contained (Option 1's
framing), and Unicode normalization is **rejected** (Option 3's substance for
gap 1). Dialog defines its own deterministic profile rather than incorporating
draft-mcnally-deterministic-cbor by reference; that draft is inspiration, not
normative.

**Changes:**

- `spec/03-encoding.md` § Deterministic CBOR is now defined as RFC 8949 §4.2.1
  Core Deterministic Encoding Requirements plus Dialog's numbered restrictions,
  and states outright that it is the complete definition — no other document is
  needed to build a conforming codec. It also notes that the profile is
  narrower than the deterministic profiles in circulation, so conformance to
  one implies nothing about the other.
- draft-mcnally-deterministic-cbor moved from Normative to Informative
  references, described as the inspiration for this profile.
- **Gap 1 (NFC), resolved: no normalization.** New normative subsection "Text
  strings and Unicode": text strings MUST be valid UTF-8; content addressing
  operates on raw UTF-8 bytes; implementations MUST NOT apply Unicode
  normalization before hashing or comparison. Rationale (informative):
  visually identical strings with different code points are distinct entities,
  exactly as strings differing in case or whitespace are, and semantic
  equivalence is asserted with the `_A_ is the same as _B_` meta-bond of
  `06-meta-bonds.md` rather than decided by the codec. Authoring tools SHOULD
  normalize user input to NFC at capture time, before the entity is created.
  This also removes the Unicode dependency the zero-dependency rule in
  `docs/plans/2026-08-12-go-reference-implementation.md` was in tension with.
- **Gap 2 (UTF-8), resolved:** stated normatively in the same subsection, for
  both encoders and decoders.
- **Gap 3 (simple values), resolved:** rule 7 now reads "Null is the only
  simple value" — `false`, `true`, `undefined` and every other major type 7
  value MUST NOT be used and MUST be rejected. This states what the published
  rule list already implied and what `go/dcbor` already did; no bytes change.
- Security Considerations in both `03-encoding.md` and `01-data-model.md` now
  cover confusable/homoglyph descriptions: implementations SHOULD surface them
  and MUST NOT merge them silently.
- `go/dcbor`: no behaviour change — UTF-8 validity enforced, no normalization,
  all non-null simple values rejected — which is now exactly what the spec
  requires. Doc comments were rewritten to present no-normalization as
  intended behaviour rather than a known gap, and `TestTextIsNotNormalized`
  pins that the precomposed and decomposed forms of "café" encode differently
  and both round-trip byte for byte.

## Notes

Source: Go reference implementation, phase 2 (dcbor + cid).
