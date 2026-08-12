---
status: complete
priority: p2
issue_id: "030"
tags: [specification-gap, encoding, cid, interoperability]
dependencies: []
---

# External CID String Encoding Unspecified

## Problem Statement

`spec/03-encoding.md` fixes the *binary* form of a CID exactly (36 bytes,
`0x01 0x71 0x12 0x20` + digest) but never says how a CID is written as text.
Since the spec's own justification for the 36-byte form is that it is "used only
for external references — communicating entity identifiers between systems,
APIs, logs, and other human-readable contexts", the text form is precisely the
form that will cross implementation boundaries, and it is undefined.

Two conforming implementations can therefore print the same CID as
`01711220e57761...` (bare hex, what the spec's examples show) or as
`bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii`
(base32 multibase, what the multiformats CID specification the document
normatively references defines as the default text form for CIDv1). Neither is
wrong under the current text, and a consumer cannot know which to expect.

## Findings

- `spec/03-encoding.md:39-64` ("Content identifiers") defines only the byte
  layout; no `String()`/parse rule.
- `spec/03-encoding.md:111-130` (worked example) prints the CID as bare hex
  (`01 71 12 20 e57761b4...`), as does `spec/06-meta-bonds.md`
  (`→ CID: 01711220...`). Those are the only text renderings in the spec, and
  they are presented as illustration rather than as a normative encoding.
- The normative reference [multiformats/cid](https://github.com/multiformats/cid)
  specifies that CIDv1 in text is multibase-prefixed, with `base32` (prefix `b`)
  as the human-readable default. Adopting the reference wholesale therefore
  contradicts the spec's own examples.
- `spec/00-overview.md` § Conventions defines the `<CID of X>` placeholder but
  says nothing about its rendering either.
- Nothing in the spec says whether a text CID is case-sensitive, whether a
  `0x`-style prefix is allowed, or whether implementations must accept multiple
  text forms on input.

## Proposed Solutions

### Option 1: Adopt multibase base32, keep hex for spec examples (Recommended)

- Normative: "The text form of a CID is its multibase encoding; implementations
  MUST emit `base32` (multibase prefix `b`, lowercase, no padding) and MUST
  accept any multibase prefix they can decode."
- Rewrite the worked example to show both the byte layout and the base32 string,
  labelling the hex as a byte dump rather than an identifier.
- **Pros**: matches the normatively referenced multiformats spec, so Dialog CIDs
  interoperate with existing IPFS/CID tooling out of the box; case-insensitive,
  copy-pasteable, URL-safe.
- **Cons**: requires a base32 implementation in every language binding (small);
  the spec's examples must be regenerated.
- **Effort**: Small
- **Risk**: Low

### Option 2: Normative lowercase hex

- Normative: "The text form of a CID is 72 lowercase hex characters, with no
  prefix."
- **Pros**: zero new machinery; the spec's existing examples become correct as
  published.
- **Cons**: diverges from the multiformats specification Dialog cites as
  normative, so Dialog CIDs cannot be pasted into CID tooling; loses the
  self-describing multibase prefix, which is the whole point of multibase.
- **Effort**: Trivial
- **Risk**: Medium (interop surprise for anyone who assumes multiformats)

### Option 3: Declare it out of scope

- State explicitly that the text form is an application concern.
- **Pros**: honest about the current state.
- **Cons**: guarantees that two implementations printing "the CID" will disagree
  — exactly the class of bug the fixed-parameter table exists to eliminate.
- **Effort**: Trivial
- **Risk**: High

## Recommended Action

Option 1. Dialog already names multiformats/cid as a normative reference; the
text form should follow it rather than diverge silently. Until this is settled,
the Go reference implementation formats and parses both `Digest` and `CID` as
lowercase hex (`go/cid`: `Digest.String`, `CID.String`, `ParseDigestHex`,
`ParseCIDHex`), matching the spec's examples, and the doc comment on
`CID.String` flags that this is not a multibase string.

## Technical Details

- **Affected Files**: `spec/03-encoding.md` (Content identifiers, Examples),
  `spec/00-overview.md` (Conventions), `spec/06-meta-bonds.md` (`→ CID:` lines),
  `go/cid/cid.go`
- **Related Components**: External identifier display, API boundaries, logs,
  conformance vectors
- **Database Changes**: No

## Acceptance Criteria

- [x] `spec/03-encoding.md` states the normative text form of a CID
- [x] The rule covers what implementations MUST emit and what they MUST accept
- [x] Case sensitivity and any prefix are specified
- [x] All CID renderings in the spec use the chosen form (or are labelled as
      byte dumps)
- [x] `go/cid` implements the chosen form, with the existing hex helpers kept or
      removed accordingly
- [ ] The conformance vectors carry the text form alongside the bytes
      *(deferred to phase 5, when `cmd/genvectors` lands)*

## Resources

- Original finding: Go reference implementation, phase 2 (`go/cid`)
- [multiformats/cid](https://github.com/multiformats/cid) — text form of CIDv1
- [multiformats/multibase](https://github.com/multiformats/multibase)
- `docs/plans/2026-08-12-go-reference-implementation.md`

## Work Log

### 2026-08-12 - Filed During Implementation
**By:** Claude

Found while implementing `go/cid`. The binary form was unambiguous and the
worked example reproduced byte for byte; the text form had to be chosen without
guidance. Hex was implemented because it is what the spec's examples print, and
this entry records that the choice is not backed by normative text.

### 2026-08-12 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1. The canonical text representation of a
CID is **multibase base32** — the lowercase RFC 4648 alphabet without padding,
prefixed with the multibase code `b`, over all 36 CID bytes. That is the
standard CIDv1 text form, so Dialog CIDs paste straight into IPFS and
multiformats tooling. The form is exact on input: uppercase base32 (multibase
`B`), padding, and bare hex are rejected rather than accepted as alternates.

**Changes:**

- `spec/03-encoding.md` § Content identifiers: new "Text representation"
  subsection giving `text(CID) = "b" || base32-lower-nopad(<36 bytes>)`, the
  fixed 59-character length and `bafyrei` prefix, the MUST-emit /
  MUST-reject rules, and the statement that hexadecimal byte listings in the
  specification illustrate the binary form and are not a wire or text format.
- Worked example gained step 4, computed rather than guessed, and cross-checked
  against the value recorded in this entry:
  `bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii`
- `spec/01-data-model.md`: the Paris atom example now prints both the byte dump
  and the text form (`bafyreidfiucqui6ufw55tl3dnis7w2rxhptk45pz3eufhhczinqjmvar7u`).
  The truncated `→ CID: 01711220…` renderings in `06-meta-bonds.md` remain byte
  dumps, which the new normative sentence covers explicitly.
- Normative references: multiformats/multibase and RFC 4648 added.
- `go/cid`: `CID.String` now returns the multibase base32 form; the previous
  hex rendering is `CID.HexString`. New `ParseCIDString` rejects a missing or
  wrong multibase prefix, uppercase base32, padding, wrong length, non-base32
  characters, and any decoded bytes that fail the fixed-parameter validation.
  `Digest.String`, `ParseDigestHex` and `ParseCIDHex` keep their hex behaviour.
  New constants `MultibaseBase32` and `StringSize`.
- Tests: the France CID round-trips through the base32 string, arbitrary CIDs
  round-trip, and each rejection case above is covered.

## Notes

Source: Go reference implementation, phase 2 (dcbor + cid).
