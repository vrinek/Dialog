---
status: complete
priority: p2
issue_id: "014"
tags: [specification-accuracy, terminology, encoding, data-model]
dependencies: []
---

# CID vs Digest Conflation

## Problem Statement

The spec conflates CIDs (36-byte multicodec+multihash) with raw SHA-256 digests (32 bytes) in multiple places. Placeholder notation is inconsistent (`<CID of ...>` vs `<hash of ...>`). This ambiguity could cause implementations to use the wrong size/format for references.

## Findings

- `spec/06-meta-bonds.md:110`: "content hashes (CIDs)" — these are different things
- `spec/06-meta-bonds.md:147,168`: Examples use `<CID of ...>` for bond field in molecules
- Placeholder notation varies across docs: `<CID of ...>`, `<hash of ...>`, mixed usage
- Need to clarify: which fields hold full CIDs and which hold raw digests

## Proposed Solutions

### Option 1: Audit and clarify all references (Recommended)
- Determine definitively which fields use CIDs vs digests (requires design discussion)
- Use `<CID of ...>` only where full CID is meant
- Use `<digest of ...>` where raw SHA-256 hash is meant
- Fix "content hashes (CIDs)" to use correct terminology
- Standardize placeholder notation across all 7 spec docs
- **Pros**: Eliminates ambiguity, ensures interoperability
- **Cons**: Requires a design discussion first to settle which is which
- **Effort**: Small (once the decision is made)
- **Risk**: Low

## Recommended Action

**Requires discussion before implementation.** Need to decide for each reference type (block refs, molecule bond field, molecule filler values, prev field, etc.) whether it holds a CID or a digest. Then do a terminology audit across all docs.

## Technical Details

- **Affected Files**: All spec docs, primarily `spec/06-meta-bonds.md`, `spec/01-data-model.md`, `spec/02-block-format.md`
- **Related Components**: Content addressing, molecule structure, block references
- **Database Changes**: No

## Acceptance Criteria

- [x] Design discussion completed: CID vs digest for each reference type
- [x] Consistent terminology across all 7 spec docs
- [x] Placeholder notation standardized
- [x] "Content hashes (CIDs)" corrected
- [x] Examples use correct notation matching the decided format

## Resources

- Original finding: Multi-agent review (Pattern Recognition, Architecture Strategist)
- `spec/03-encoding.md`: CID construction (CIDv1, dag-cbor, SHA-256)
- `spec/01-data-model.md`: Filler type definitions

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- User noted: need to discuss which fields are CIDs vs digests before fixing
- Status changed from pending → ready

**Learnings:**
- This is a terminology + design issue, not just a find-and-replace
- Resolution affects how implementations serialize references

### 2026-08-12 - Decision Ratified and Audit Completed
**By:** Claude

**Ratified decision (now normative in `spec/03-encoding.md`, "Internal references"):**
- Every reference value carried inside a Dialog CBOR structure is a **raw 32-byte
  SHA-256 digest**, encoded as a CBOR byte string (`5820` + 32 bytes). This covers:
  block `prev`, each entry of block `refs`, a molecule's / `create_molecule`'s
  `bond` field, and filler values of type 0 (atom), 1 (bond), 2 (molecule).
- The full **36-byte CIDv1** (`0x01 0x71 0x12 0x20` + digest) is used **only in
  external contexts**: cross-system communication, APIs, logs, and human-readable
  identifiers.
- **IPFS URI fillers (type 3) are out of scope** — they carry IPFS's own content
  identifier as a text string.
- **Placeholder notation:** `<digest of X>` for in-structure 32-byte references,
  `<CID of X>` only where the 36-byte external form is genuinely meant. The
  convention is documented in `spec/00-overview.md` § Conventions.

**Actions (audit across all 7 spec docs + README.md):**
- `spec/03-encoding.md`: expanded "Internal references" into the citable normative
  anchor — enumerated the digest-carrying fields, excluded IPFS URI fillers, and
  stated that a full CID never appears in those fields.
- `spec/00-overview.md`: added the placeholder-notation convention; added an
  "Internal reference format" row to the fixed-parameter table and scoped the CID
  row to external identifiers; reworded the L1 `prev`/`refs` description.
- `spec/01-data-model.md`: added a "Digest" terminology entry, scoped the CID
  entry to external identifiers, cross-referenced 03 from the filler-type notes
  (and called out type 3 as not an internal reference), replaced "content hashes"
  with "SHA-256 digests", and converted the molecule example to `<digest of ...>`.
- `spec/02-block-format.md`: terminology now says `prev` digests / 32-byte foreign
  block reference; `create_molecule` and "Block identification" cross-reference 03;
  validation rule 4 says "entity digests"; all examples use `<digest of ...>` and
  the `<n bytes: ...>` form.
- `spec/04-cryptography.md`: example `prev` placeholders now
  `<digest of previous block, or null>`.
- `spec/05-processing-model.md`: resolution procedure, scan-limit rule, and the
  foreign-loading example now speak of entity digests rather than CIDs; `refs`
  entries documented as 32-byte block digests with a cross-reference to 03.
- `spec/06-meta-bonds.md`: fixed "content hashes (CIDs)" → digest of the
  dCBOR-encoded template, with the `bond`-field-is-a-digest cross-reference; all
  `<CID of ...>` bond fields and `<hash of ...>` fillers in the three equivalence
  examples became `<digest of ...>`; `<different hash>` → `<different digest>`.
  The `→ CID: 01711220...` lines were kept — they show the external 36-byte form
  on purpose, and the atom-equivalence example now says so explicitly.
- `README.md`: ontology model now uses `digest(...)`, plus a paragraph stating the
  internal-digest / external-CID split; "previous block hash" → digest.

**Deliberately left unchanged (flagged, not guessed):**
- The defined term **"CID-providing block"** and the phrase **"CID resolution"**
  (`spec/00-overview.md`, `spec/02-block-format.md`, `spec/05-processing-model.md`,
  `README.md`). These name the *entity-identity* concept, not a serialized field,
  and the term comes from the block reference model of Issue #22. Renaming is a
  separate vocabulary decision.
- `spec/05-processing-model.md` L2 accumulation ("compute the CID of the resulting
  entity", "the block's CID (provenance)"): local storage identity, not an on-wire
  reference. Either form is derivable; left as CID.
- The optional `unit` field of a scalar filler (`bstr .size 32`, digest of a unit
  atom) is a reference type not named in the ratified list. It is already a
  32-byte digest and therefore consistent with the rule, so it was left as-is and
  not added to the enumeration in 03 (the list is introduced with "This includes").

## Notes

Source: Triage session on 2026-02-24
Design discussion resolved on 2026-08-12; the rule now lives in
`spec/03-encoding.md` § "Internal references". Unblocks Issue #23.
