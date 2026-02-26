---
status: complete
priority: p2
issue_id: "012"
tags: [architecture, key-rotation, block-validation, chain-integrity]
dependencies: ["001"]
---

# Chain Integrity vs Key Rotation

## Problem Statement

`02-block-format.md:133` says "all blocks MUST have the same author" (same `pub` key), but key rotation changes the `pub` field. The rule as written rejects post-rotation blocks.

## Findings

- `spec/02-block-format.md:133`: Chain integrity requires same `pub` across all blocks
- Key rotation changes the `pub` field — directly contradicts this rule
- This is a downstream consequence of Issue #1 (key rotation architecture)

## Proposed Solution: Key Rotation as Chain-Ending Operation

Resolved by the design chosen in Issue #1:

1. **Key rotation is a new 4th operation type** (`rotate_key`) carrying the new public key bytes
2. **A rotation block MUST contain only the `rotate_key` operation** — no other ops allowed
3. **The rotation block is the last block of the old key's chain** — signals chain end
4. **The new key starts a fresh chain** (new genesis block)
5. **Implementation marks old key as inactive** — no further blocks accepted for it
6. **Implementation adds new key to subscription list**, associated with the same author
7. **Author identity is implementation-scoped** — the protocol defines key succession, not author naming

### Chain integrity rule update

Change from "all blocks MUST have the same `pub`" to: "within a single chain, all blocks MUST have the same `pub` field." A chain ends when a rotation block is published. The new key begins a separate chain.

### New chain linkage

The new key's genesis block SHOULD reference the rotation block via `refs`, creating a verifiable succession link. This allows any node to verify the key succession by checking: rotation block (signed by old key, contains new key) → new genesis block (signed by new key, refs rotation block).

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (chain integrity rule, add rotate_key op), `spec/05-processing-model.md` (chain management), `spec/06-meta-bonds.md` (remove key rotation meta-bond or keep as alias)
- **Related Components**: Block validation, chain management, subscription handling
- **Database Changes**: No

## Acceptance Criteria

- [ ] Chain integrity rule clarified: same `pub` within a single chain
- [ ] `rotate_key` operation defined in block format
- [ ] Rotation block constraints specified (solo-op only)
- [ ] New chain genesis linkage defined (SHOULD ref rotation block)
- [ ] Old key marked inactive after rotation
- [ ] Key rotation meta-bond in 06 either removed or redefined as non-normative

## Resources

- Depends on: Issue #1 (key rotation architecture)
- Also resolves: Issue #2 (key atom binding — no longer needed with raw key bytes in operation)
- Original finding: Multi-agent review (all agents)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage with user-designed chain-ending model
- Rotation block = solo-op, ends old chain, new key starts fresh chain
- Status: ready

**Learnings:**
- "Each key = one chain" is much simpler than "one chain, multiple keys"
- Author identity as implementation concept keeps protocol clean

## Notes

Source: Triage session on 2026-02-24
User-designed solution during triage.
