---
status: done
priority: p2
issue_id: "005"
tags: [specification-consistency, meta-bonds, rfc2119]
dependencies: []
---

# MUST/SHOULD Conflict on Meta-Bonds

## Problem Statement

`05-processing-model.md:110` says "Implementations MUST recognize the standard meta-bonds." `06-meta-bonds.md:26` says "Implementations SHOULD support the following six meta-bonds." These have different normative weight per RFC 2119, creating ambiguity about whether meta-bond support is required or recommended.

## Findings

- `spec/05-processing-model.md:110`: "Implementations MUST recognize the standard meta-bonds"
- `spec/06-meta-bonds.md:26`: "Implementations SHOULD support the following six meta-bonds"
- RFC 2119: MUST = absolute requirement, SHOULD = recommended but may be ignored with reason
- The processing model (05) is the authoritative document for L2→L3 behavior

## Proposed Solutions

### Option 1: Change 06 to MUST (Recommended)
- Update `06-meta-bonds.md:26`: SHOULD → MUST
- Aligns with processing model, ensures all implementations handle meta-bonds
- **Pros**: Consistent, ensures interoperability of core protocol feature
- **Cons**: None — meta-bonds are fundamental to the protocol
- **Effort**: Small
- **Risk**: Low

### Option 2: Change 05 to SHOULD
- Downgrade processing model requirement
- **Pros**: More permissive
- **Cons**: Undermines meta-bonds as a core protocol feature, weakens interoperability
- **Effort**: Small
- **Risk**: Medium — fragmented implementations

## Recommended Action

Option 1. One-word change in `06-meta-bonds.md:26`.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md` (line 26)
- **Related Components**: Meta-bond processing, L2→L3 pipeline
- **Database Changes**: No

## Acceptance Criteria

- [ ] Both documents use the same RFC 2119 keyword for meta-bond support
- [ ] No other MUST/SHOULD conflicts between spec documents

## Resources

- Original finding: Multi-agent review (SpecFlow Analyzer, Pattern Recognition)
- RFC 2119: Key words for use in RFCs

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Quick win — one-word fix

## Notes

Source: Triage session on 2026-02-24
