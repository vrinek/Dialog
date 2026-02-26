---
status: done
priority: p1
issue_id: "001"
tags: [architecture, key-rotation, layer-model]
dependencies: []
---

# Key Rotation Layer Violation

## Problem Statement

Key rotation is defined as a meta-molecule in `06-meta-bonds.md`, which means it's only interpreted during L2→L3 processing. However, L1 chain validation needs to know about key rotations to accept blocks signed by a new key. This creates a circular dependency: L1 can't validate blocks without L3 knowledge, but L3 depends on L1.

## Findings

- Key rotation defined as meta-bond #6 in `spec/06-meta-bonds.md:83-94`
- L3 semantics say "implementations MUST accept blocks signed by the new key" (line 92)
- But L1 validation (`spec/05-processing-model.md:38-43`) happens before L3 processing
- Chain integrity rule in `spec/02-block-format.md:133` says "all blocks MUST have same author" — undefined after key change
- All 5 review agents independently flagged this as the #1 architectural issue

## Proposed Solutions

### Option 1: Elevate key rotation to L1 operation
- Add a 4th operation type `rotate_key` to block format
- L1 validates rotation at block level, no L3 dependency
- **Pros**: Clean separation, no circular dependency
- **Cons**: Breaks the "only 3 operation types" simplicity, key rotation is no longer "just a molecule"
- **Effort**: Medium
- **Risk**: Medium — changes core block format

### Option 2: L1 pre-scan for key rotation meta-molecules
- Before full L1 validation, scan ops for key rotation meta-molecules
- Build a key rotation index at L1 level
- **Pros**: Keeps meta-molecule model intact, minimal format change
- **Cons**: L1 now needs knowledge of specific meta-bond CIDs, blurs layer separation
- **Effort**: Medium
- **Risk**: Low — additive change

### Option 3: Define key rotation as a block-level field
- Add an optional `key_rotation` field to the block structure
- The meta-molecule still exists for L3 semantics, but L1 uses the block field
- **Pros**: Clean L1 validation, meta-molecule model preserved for L3
- **Cons**: Dual representation of same concept
- **Effort**: Small
- **Risk**: Low

## Recommended Action

**Decision made during triage: Option 1 (L1 operation) with chain-ending semantics.**

- `rotate_key` is a 4th operation type carrying the new public key bytes
- A rotation block MUST contain only the `rotate_key` op — no other operations
- The rotation block ends the old key's chain; the new key starts a fresh chain (new genesis)
- The new key's genesis block SHOULD reference the rotation block via `refs`
- Implementation marks old key inactive and auto-subscribes to new key (same author)
- Key rotation meta-bond (#6) removed from `06-meta-bonds.md` — replaced by L1 operation
- This also resolves Issue #2 (key atom binding) and Issue #12 (chain integrity)

## Technical Details

- **Affected Files**: `spec/02-block-format.md`, `spec/05-processing-model.md`, `spec/06-meta-bonds.md`
- **Related Components**: Block validation pipeline, chain integrity rules, meta-bond library
- **Database Changes**: No

## Acceptance Criteria

- [ ] `rotate_key` operation type defined in `02-block-format.md`
- [ ] Rotation block constraints specified (solo-op only)
- [ ] Chain-ending semantics documented
- [ ] New chain genesis linkage defined (SHOULD ref rotation block)
- [ ] Key rotation meta-bond removed from `06-meta-bonds.md` (5 standard meta-bonds, not 6)
- [ ] Chain integrity rule updated in `02-block-format.md`
- [ ] Processing model updated for chain succession in `05-processing-model.md`
- [ ] No circular L1↔L3 dependency

## Resources

- Original finding: Multi-agent review (Architecture, Security, SpecFlow, Simplicity, Pattern agents)
- Related: Issue #2 (key atom binding), Issue #12 (chain integrity vs key rotation)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- This is the highest-priority architectural issue in the spec
- Resolution will cascade to issues #2 and #12

## Notes

Source: Triage session on 2026-02-24
