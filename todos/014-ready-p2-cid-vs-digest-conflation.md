---
status: ready
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

- [ ] Design discussion completed: CID vs digest for each reference type
- [ ] Consistent terminology across all 7 spec docs
- [ ] Placeholder notation standardized
- [ ] "Content hashes (CIDs)" corrected
- [ ] Examples use correct notation matching the decided format

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

## Notes

Source: Triage session on 2026-02-24
Blocked on design discussion — do not implement until CID vs digest decision is made for each reference type.
