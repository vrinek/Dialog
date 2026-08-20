---
status: complete
priority: p3
issue_id: "029"
tags: [documentation, readme]
dependencies: ["001", "015", "022", "026", "027", "028"]
---

# Update README After Spec Changes

## Problem Statement

README reflects the current spec. After applying all spec changes (key rotation redesign, block type field, reference model, equivalence update, meta-bond removal, subscription rework), the README needs a review pass to stay in sync.

## Proposed Solution

Review and update README after all spec changes land. Single pass at the end.

## Technical Details

- **Affected Files**: `README.md`

## Acceptance Criteria

- [x] README matches updated spec on all points
- [x] Key rotation described as block type, not meta-bond
- [x] 5 meta-bonds, not 6
- [x] Subscriptions correctly described
- [x] Block reference model updated
- [x] Equivalence described for atoms, bonds, and molecules

## Work Log

### 2026-02-24 - Approved for Work
**By:** User review
- Do this last, after all spec changes

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `README.md` matches the current spec on every point listed: three block types with the rotation block described at lines 175-181, `prev` as ordering-only and `refs` as CID-providing blocks with fork detection called a normative requirement (line 185), five meta-bonds with equivalence covering atoms, bonds and molecules (lines 195-199), subscriptions as a cross-cutting L1/L3 concern (line 159), and internal references as raw 32-byte digests with CIDs external-only (line 171).
- The document index (lines 27-34) includes 07-transport as an optional profile, and the Go package table reflects the current implementation layout.

## Notes

Source: User review on 2026-02-24
