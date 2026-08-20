---
status: done
priority: p3
issue_id: "019"
tags: [cryptography, specification-gap, interoperability]
dependencies: []
---

# Ed25519→X25519 Conversion Not Fully Specified

## Problem Statement

The spec mentions Ed25519-to-X25519 key conversion for key agreement but doesn't specify the conversion procedure or reference a standard.

## Findings

- `spec/04-cryptography.md`: Mentions conversion but no procedure or reference
- Different libraries may implement the birational map differently
- libsodium's `crypto_sign_ed25519_pk_to_curve25519` is the de facto standard

## Proposed Solutions

### Option 1: Reference standard conversion (Recommended)
- Cite RFC 7748 and/or libsodium's conversion function as reference
- **Effort**: Small
- **Risk**: Low

## Recommended Action

One sentence + normative reference.

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` (key agreement section)

## Acceptance Criteria

- [x] Conversion procedure referenced (RFC 7748 or equivalent)
- [x] Added to normative references

## Work Log

### 2026-02-26 - Resolved (Already Addressed)
**By:** Claude Agent
- Both acceptance criteria were already satisfied in the current spec
- Line 129 of `spec/04-cryptography.md` specifies the conversion procedure with RFC 7748 S4.1 citation and libsodium reference implementations
- Line 231 of `spec/04-cryptography.md` lists RFC 7748 in normative references
- No spec changes needed; marked as done

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

## Notes

Source: Triage session on 2026-02-24
