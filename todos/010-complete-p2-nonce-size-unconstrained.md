---
status: complete
priority: p2
issue_id: "010"
tags: [specification-consistency, cryptography, cddl]
dependencies: []
---

# Nonce Size Unconstrained in CDDL

## Problem Statement

Block format CDDL defines nonce as bare `bstr` but XChaCha20-Poly1305 requires exactly 24 bytes. Wrong-sized nonces pass schema validation but fail at crypto layer.

## Findings

- `spec/02-block-format.md:65`: `"nonce" => bstr` (no size constraint)
- `spec/04-cryptography.md:59`: Also bare `bstr` in signing input CDDL
- XChaCha20-Poly1305 requires 192-bit (24-byte) nonce
- Standard ChaCha20 uses 96-bit (12-byte) nonce — easy mistake

## Proposed Solutions

### Option 1: Add .size 24 constraint (Recommended)
- Change `"nonce" => bstr` to `"nonce" => bstr .size 24` in both files
- **Pros**: Catches wrong nonce size at schema validation, self-documenting
- **Cons**: None
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Option 1. Two-line fix.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (line 65), `spec/04-cryptography.md` (line 59)
- **Related Components**: CDDL schemas, private block encryption
- **Database Changes**: No

## Acceptance Criteria

- [x] Both CDDL definitions use `bstr .size 24`
- [x] No bare `bstr` for nonce anywhere in spec

## Resources

- Original finding: Multi-agent review (Security Sentinel, Pattern Recognition)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Quick win — constrain sizes in CDDL wherever the crypto mandates them

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/02-block-format.md:76` and `spec/04-cryptography.md:79` both read `"nonce" => bstr .size 24  ; 192-bit XChaCha20 nonce`.
- A grep for `nonce` across `spec/` finds no remaining bare `bstr`.

## Notes

Source: Triage session on 2026-02-24
