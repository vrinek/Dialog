---
status: complete
priority: p2
issue_id: "007"
tags: [security, cryptography, private-blocks]
dependencies: []
---

# No AAD in AEAD

## Problem Statement

`04-cryptography.md:94-98` uses XChaCha20-Poly1305 (an AEAD cipher) for private block encryption, but no Additional Authenticated Data (AAD) is specified. The ciphertext is not cryptographically bound to block metadata, allowing an attacker to swap encrypted payloads between blocks without detection.

## Findings

- `spec/04-cryptography.md:94-98`: Encryption uses XChaCha20-Poly1305 but AAD is absent
- AEAD ciphers support AAD specifically to bind ciphertext to context — not using it wastes a key security property
- Block metadata (version, pub, prev, refs, timestamp) remains plaintext for private blocks
- Without AAD, encrypted ops can be transplanted between blocks

## Proposed Solutions

### Option 1: Bind all plaintext fields as AAD (Recommended)
- AAD = dCBOR encoding of all non-encrypted block fields (version, pub, prev, refs, ts)
- Add normative text: "The AAD MUST be the deterministic CBOR encoding of a map containing all plaintext block fields"
- **Pros**: Full binding, prevents all payload-swapping attacks, uses AEAD as designed
- **Cons**: Slightly more complex encryption/decryption (must assemble AAD)
- **Effort**: Small
- **Risk**: Low

### Option 2: Use block CID as AAD
- AAD = CID of the block (computed over the full block including ciphertext)
- **Pros**: Simple single value
- **Cons**: Circular — CID depends on ciphertext which depends on AAD
- **Effort**: N/A — not feasible
- **Risk**: N/A

## Recommended Action

Option 1. Specify AAD as dCBOR of plaintext fields.

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` (encryption section)
- **Related Components**: Private block encryption, block validation
- **Database Changes**: No

## Acceptance Criteria

- [x] AAD defined normatively with exact construction
- [x] Field ordering specified (dCBOR deterministic map ordering handles this)
- [x] Decryption validation: AAD mismatch MUST cause decryption failure
- [x] Example updated to show AAD construction

## Resources

- Original finding: Multi-agent review (Security Sentinel, Architecture Strategist)
- Related: Issue #4 (KDF), Issue #11 (domain separator)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Using AEAD without AAD is a common oversight — wastes a key security property

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/04-cryptography.md:128` defines the AAD as the deterministic CBOR encoding of a map of the plaintext block fields -- `v`, `type`, `pub`, `prev` -- with dCBOR key ordering making the encoding unambiguous.
- The field set is `v`/`type`/`pub`/`prev` rather than the list originally proposed here because `refs` and `ts` moved inside the ciphertext under todo 022; nothing left in the clear is unbound.
- An AAD mismatch (authentication tag failure) MUST reject the block: same file, line 143. Worked example at line 302.
- The separate key-wrap AEAD deliberately uses an empty AAD, with the reasoning written out at line 218.

## Notes

Source: Triage session on 2026-02-24
