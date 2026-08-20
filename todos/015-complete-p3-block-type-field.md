---
status: complete
priority: p3
issue_id: "015"
tags: [block-format, specification-gap, architecture]
dependencies: ["001"]
---

# Add Block Type Field

## Problem Statement

The spec has no explicit way to discriminate between public, private, and rotation blocks. With key rotation becoming an L1 operation (Issue #1), there are now three distinct block structures with different validation rules. An explicit `type` field makes discrimination and validation dispatch clear.

## Findings

- No normative rule for how to tell public from private blocks
- `nonce` presence was the implied discriminator but never stated
- Key rotation blocks (from Issue #1) are a third structure: solo-op, chain-ending
- Three block types have different fields and validation rules

## Proposed Solutions

### Option 1: Add `type` field with three values (Approved)
- Add `"type" => "public" / "private" / "rotation"` field to block structure
- Validation rules dispatch on type:
  - `public`: `ops` is plaintext array, no `nonce` field
  - `private`: `ops` is ciphertext, `nonce` field required (`bstr .size 24`)
  - `rotation`: `ops` contains exactly one `rotate_key` operation, no other ops
- Keep `ver` field solely for protocol version
- **Pros**: Explicit discrimination, clean validation dispatch, independent versioning
- **Cons**: One more field per block
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Add `type` field. Update CDDL in `02-block-format.md` to show three block variants. Each variant has its own validation rules.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` (block structure CDDL, validation rules), `spec/04-cryptography.md` (signing input)
- **Related Components**: Block structure, validation pipeline, signing input
- **Database Changes**: No

## Acceptance Criteria

- [x] `type` field added to block CDDL with three valid values
- [x] Validation rules documented per block type
- [x] `nonce` field only present/required for `private` type
- [x] `rotation` type constraints documented (solo-op)
- [x] Signing input updated to include `type` field
- [x] Examples updated to show type field

## Resources

- Original finding: Multi-agent review (Pattern Recognition, Simplicity)
- Depends on: Issue #1 (key rotation as L1 operation)

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
**Actions:**
- Issue approved during triage with user decision for separate `type` field
- User considered combining with `ver` but chose separation of concerns
- Status: ready

**Learnings:**
- Version and type are independent axes — keep them separate
- Three block types means version changes could affect only one type

### 2026-08-20 - Audit against current spec
**By:** Claude audit pass
**Actions:**
- Resolved. `spec/02-block-format.md:46` defines `"type" => tstr` with the three values, and "Validation" rule 1 dispatches per type: public (plaintext `ops`, no `nonce`/`enc`, no `rotate_key`), private (`enc` plus `nonce .size 24`, no plaintext `ops`/`refs`/`ts`), rotation (exactly one `rotate_key` and nothing else).
- The rotation-block CDDL and its solo-op constraint are at `spec/02-block-format.md:100-125`.
- The signing input carries `type` in both variants: `spec/04-cryptography.md`, "Signature input" (`signing-input-public`, `signing-input-private`).
- The block examples in `spec/02-block-format.md` all carry the field.

## Notes

Source: Triage session on 2026-02-24
User-designed solution during triage.
