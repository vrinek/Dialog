---
status: complete
priority: p1
issue_id: "002"
tags: [security, key-rotation, interoperability]
dependencies: ["001"]
---

# Key Atom Binding Undefined

## Problem Statement

Key rotation relies on atoms whose descriptions are string representations of keys (e.g., "Ed25519 key 0xOLD..."). There is no canonical format for how a binary public key becomes an atom description string. Different implementations could produce different strings for the same key, breaking interoperability and making key rotation non-deterministic across clients.

## Findings

- `spec/06-meta-bonds.md:94` says atom descriptions should match the `pub` field of subsequent blocks
- No canonical serialization format defined for key-to-string conversion
- Examples use informal notation: `"Ed25519 key 0xOLD..."` (line 159-160)
- Atoms are content-addressed — different strings = different CIDs = broken rotation
- Depends on Issue #1 (key rotation architecture) — if key rotation moves to a block-level field, atom binding may become moot

## Proposed Solutions

### Option 1: Define canonical atom description format
- Specify exact format: e.g., `"ed25519:<base64url-no-pad>"` or `"ed25519:<hex-lowercase>"`
- Add normative requirement for this format in `04-cryptography.md`
- **Pros**: Simple, deterministic, interoperable
- **Cons**: Couples atom description format to cryptographic concerns
- **Effort**: Small
- **Risk**: Low

### Option 2: Use raw key bytes as a new filler type
- Add filler type 5 for raw binary public keys
- Key rotation molecule uses binary fillers instead of atom descriptions
- **Pros**: No string ambiguity, type-safe
- **Cons**: Adds a filler type, increases data model complexity
- **Effort**: Medium
- **Risk**: Medium — changes data model

### Option 3: Block-level field makes this moot
- If Issue #1 is resolved with a block-level `key_rotation` field containing raw key bytes, atom binding is no longer needed
- **Pros**: Problem eliminated entirely
- **Cons**: Depends on Issue #1 resolution path
- **Effort**: None (if Issue #1 takes this path)
- **Risk**: None

## Recommended Action

Wait for Issue #1 resolution. If key rotation stays as a meta-molecule, implement Option 1. If it moves to block-level, this issue is resolved automatically.

## Technical Details

- **Affected Files**: `spec/06-meta-bonds.md`, possibly `spec/04-cryptography.md`, `spec/01-data-model.md`
- **Related Components**: Key rotation mechanism, atom content addressing
- **Database Changes**: No

## Acceptance Criteria

- [ ] Key bytes have a single canonical string representation (or are handled as binary)
- [ ] Two implementations produce identical CIDs for the same key's atom
- [ ] Key rotation examples updated with canonical format
- [ ] Interoperability guaranteed across implementations

## Resources

- Original finding: Multi-agent review (Security Sentinel, Architecture Strategist, Pattern Recognition)
- Depends on: Issue #1 (key rotation layer violation)
- Related: `spec/06-meta-bonds.md:86-94`

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready
- Dependency on Issue #1 noted

**Learnings:**
- Resolution path depends heavily on Issue #1 outcome
- Option 3 (block-level field) would eliminate this issue entirely

## Notes

Source: Triage session on 2026-02-24
