---
status: done
priority: p2
issue_id: "011"
tags: [security, cryptography, signing]
dependencies: []
---

# No Domain Separator in Signing

## Problem Statement

The signing input for blocks is the dCBOR encoding of block fields with no domain separator. This enables cross-protocol replay if another protocol also signs dCBOR maps with Ed25519.

## Findings

- `spec/04-cryptography.md`: Signing input is `dCBOR(block_fields_minus_sig)`
- No prefix, context string, or domain tag
- Ed25519 over dCBOR is not unique to Dialog — other protocols could use the same pattern
- Cross-protocol replay is a known attack vector when domain separation is missing

## Proposed Solutions

### Option 1: Prefix signing input with domain string (Recommended)
- Signing input: `"dialog-v1-block" || dCBOR(block_fields)`
- The domain string is a fixed byte prefix concatenated before the CBOR bytes
- **Pros**: Simple, effective, standard practice
- **Cons**: None
- **Effort**: Small
- **Risk**: Low

### Option 2: Include domain string inside the CBOR map
- Add a `"protocol" => "dialog-v1"` field to the signing input map
- **Pros**: Stays within CBOR structure
- **Cons**: Changes the signing input map structure, could be confused with a block field
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Option 1. Byte prefix is the standard approach for domain separation (used by TLS, SSH, etc.).

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` (signing input section)
- **Related Components**: Block signing, signature verification
- **Database Changes**: No

## Acceptance Criteria

- [x] Domain separator string defined as normative constant
- [x] Signing input construction updated to include prefix
- [x] Verification procedure updated to match
- [x] Domain string added to fixed parameters table in `00-overview.md`

## Resources

- Original finding: Multi-agent review (Security Sentinel)
- Related: Issue #4 (KDF info string provides domain separation for encryption)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage session
- Status changed from pending → ready

**Learnings:**
- Domain separation is a cryptographic hygiene practice — always include it

### 2026-02-26 - Resolved
**By:** Claude (pr-comment-resolver)
**Actions:**
- Verified all changes already applied in working tree
- `spec/04-cryptography.md`: Signing procedure prefixes with `"dialog-v1-block"`, verification matches, example updated
- `spec/00-overview.md`: `Signing domain separator` row added to fixed parameters table
- All four acceptance criteria satisfied
- Status changed from ready -> done

## Notes

Source: Triage session on 2026-02-24
