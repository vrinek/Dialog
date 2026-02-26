---
status: done
priority: p2
issue_id: "027"
tags: [meta-bonds, key-rotation]
dependencies: ["001", "015"]
---

# Remove Key Rotation Meta-Bond (Deprecated)

## Problem Statement

`_A_ rotates key to _B_` is deprecated in favor of the rotation block type (decided in Issue #1 triage). The meta-bond library should have 5 standard meta-bonds, not 6.

## Findings

- `spec/06-meta-bonds.md:83-94`: Key rotation meta-bond still present
- Triage decided: key rotation is a 4th operation type in a dedicated rotation block
- Meta-bond #6 is now redundant

## Proposed Solution

Remove key rotation meta-bond from `06-meta-bonds.md`. Update all references to "six standard meta-bonds" → "five standard meta-bonds". Update abstract, examples, and any cross-references.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md`, `spec/00-overview.md`, `README.md`

## Acceptance Criteria

- [ ] Key rotation meta-bond removed from 06
- [ ] "Six" → "five" everywhere
- [ ] Key rotation examples removed or moved to 02-block-format
- [ ] Cross-references updated

## Work Log

### 2026-02-24 - Approved for Work
**By:** User review
- Confirmed deprecation per triage decision on rotation block type

## Notes

Source: User review on 2026-02-24
