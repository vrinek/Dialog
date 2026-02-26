---
status: done
priority: p1
issue_id: "004"
tags: [security, cryptography, interoperability]
dependencies: []
---

# KDF Unspecified

## Problem Statement

Private chain encryption in `04-cryptography.md:109-126` references `KDF(shared_secret)` to derive a wrapping key from the X25519 shared secret, but never names the KDF algorithm or its parameters. Without this, implementations will produce incompatible encrypted blocks.

## Findings

- `spec/04-cryptography.md:109-126`: Shows `Encrypt(KDF(shared_secret), chain_symmetric_key)` with no KDF definition
- X25519 shared secret is 32 bytes of raw key material — must be processed through a KDF before use
- No salt, info string, or output length specified
- Fixed parameters table in `spec/00-overview.md:100-108` does not list a KDF

## Proposed Solutions

### Option 1: Pin HKDF-SHA-256 with explicit parameters (Recommended)
- Algorithm: HKDF-SHA-256 (RFC 5869)
- Salt: empty (zero-length)
- Info: `"dialog-v1-key-wrap"` (protocol domain separator)
- Output length: 32 bytes (for XChaCha20-Poly1305 key)
- Add to fixed parameters table in `00-overview.md`
- **Pros**: Industry standard, already uses SHA-256 elsewhere, fully deterministic
- **Cons**: None
- **Effort**: Small
- **Risk**: Low

### Option 2: Use raw X25519 output directly
- Skip KDF, use 32-byte shared secret as wrapping key directly
- **Pros**: Simpler
- **Cons**: Violates cryptographic best practice — raw DH output has biased bits
- **Effort**: Small
- **Risk**: High — cryptographically unsound

## Recommended Action

Option 1. HKDF-SHA-256 is the standard choice. Add normative text with exact parameters.

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` (add KDF section), `spec/00-overview.md` (add to fixed params table)
- **Related Components**: Private block encryption, key agreement
- **Database Changes**: No

## Acceptance Criteria

- [ ] KDF algorithm named (HKDF-SHA-256)
- [ ] All parameters specified: salt, info, output length
- [ ] Added to fixed parameters table in `00-overview.md`
- [ ] RFC 5869 added to normative references in `04-cryptography.md`
- [ ] `KDF(shared_secret)` replaced with explicit HKDF invocation in text

## Resources

- Original finding: Multi-agent review (Security Sentinel, Architecture Strategist)
- RFC 5869: HMAC-based Extract-and-Expand Key Derivation Function
- Related: Issue #7 (no AAD), Issue #11 (no domain separator)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Quick win with high security impact
- Info string provides domain separation for the KDF context

## Notes

Source: Triage session on 2026-02-24
