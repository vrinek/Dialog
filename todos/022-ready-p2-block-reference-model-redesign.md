---
status: done
priority: p2
issue_id: "022"
tags: [architecture, block-format, foreign-chains, private-blocks, refs]
dependencies: ["001", "015"]
---

# Block Reference Model Redesign

## Problem Statement

Multiple review findings and triage discussions revealed that the current block reference model (refs as foreign chain tips + full chain traversal) needs a fundamental redesign. This issue captures the unified design decisions made during triage.

## Design Decisions (from triage)

### 1. Public/private reference rules

- Public blocks MUST only reference public blocks
- Private blocks MAY reference either public or private blocks
- Non-recipient nodes can safely drop private blocks they cannot decrypt

### 2. Private block metadata — minimal public surface

Since only recipients process private blocks, encrypt everything except chain management fields:
- **Public (plaintext):** `ver`, `type`, `pub`, `sig`, `prev`
- **Encrypted (alongside ops):** `refs`, `ts`
- This eliminates metadata leakage (timing, social graph via refs)

### 3. Refs as explicit CID providers (not chain tips)

- `refs` points to specific blocks that contain the CIDs needed by the current block
- Validation: all CIDs in block H must exist in H itself or in blocks listed in H's `refs`
- `prev` becomes purely about chain ordering (append-only semantics, fork detection)
- `prev` is no longer a validation input for CID resolution

### 4. Demand-driven recursive resolution

- Fetch referenced blocks → check if CIDs resolve
- If a referenced CID points to a molecule whose own dependencies (bond, atoms) aren't resolved → traverse that block's refs
- Repeat until all transitive CIDs resolve or scan limit is hit
- Traversal follows the explicit refs graph, not linear prev chains

### 5. Undecryptable private reference → error

- If a node can decrypt block H but not a block in H's refs → validation error
- Bubble error to user (they need the key for the referenced chain, or distrust H)
- No silent failures, no partial validation

### 6. Fat blocks are protocol-legal but not mandated

- Since ops are idempotent at L2 (same CID = same entity), including redundant ops is harmless
- Implementations MAY include all transitively-needed ops in a single block (self-contained)
- This is an implementation strategy choice (offline/portable vs network-efficient)
- The protocol defines validation rules; implementations choose their strategy

### 7. Configurable scan limit (from Issue #9)

- Implementations MAY set a user-configurable limit on recursive ref resolution depth
- Limit SHOULD have a safe default
- If limit is hit before all CIDs resolve, block is treated as invalid

## Impact on Other Issues

- **Supersedes Issue #8** (foreign chain loading contradiction) — no more "load to genesis"
- **Supersedes Issue #9** (reachability model) — replaced by explicit refs model
- **Resolves Issue #22** (metadata leakage) — refs+ts encrypted for private blocks
- **Informs Issue #15** (block type field) — private blocks have different encrypted field set

## Technical Details

- **Affected Files**:
  - `spec/02-block-format.md` — refs semantics, validation rules, private block structure
  - `spec/04-cryptography.md` — what gets encrypted (refs + ts + ops), AAD construction
  - `spec/05-processing-model.md` — foreign chain loading rewrite, L1 validation updates
  - `spec/00-overview.md` — architecture description updates
- **Related Components**: Block structure, validation pipeline, foreign chain loading, private block encryption
- **Database Changes**: No

## Acceptance Criteria

- [ ] Public→private reference prohibition stated as normative rule
- [ ] Private block encrypted fields defined (refs, ts, ops)
- [ ] Refs semantics changed from "chain tips" to "CID-providing blocks"
- [ ] `prev` role clarified as ordering-only (not CID resolution)
- [ ] Demand-driven recursive resolution documented
- [ ] Undecryptable ref → error behavior defined
- [ ] Scan limit with safe default specified
- [ ] Fat block strategy noted as implementation choice
- [ ] Examples updated to show new ref model
- [ ] AAD construction updated for new encrypted field set (Issue #7)

## Resources

- Original findings: Issues #8, #9, #15, #22 from multi-agent review
- Design decisions made during triage discussion on 2026-02-24

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Combined multiple issues into unified reference model redesign
- All design decisions from triage discussion captured
- Supersedes Issues #8, #9; resolves #22; informs #15
- Status: ready

**Learnings:**
- Explicit dependency graphs (refs → specific blocks) are more robust than implicit traversal (prev → genesis)
- "What does a non-recipient node do?" is the key question for private block design
- Fat blocks are a natural consequence of idempotent ops — useful insight for implementation guidance

## Notes

Source: Triage session on 2026-02-24
User-designed solution through iterative discussion.
