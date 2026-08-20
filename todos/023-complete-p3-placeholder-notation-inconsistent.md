---
status: complete
priority: p3
issue_id: "023"
tags: [documentation, examples, consistency]
dependencies: ["014"]
---

# Placeholder Notation Inconsistent

## Problem Statement

Examples use inconsistent placeholder notation: `<CID of ...>`, `<hash of ...>`, `<different hash>`. Should be standardized after Issue #14 resolves CID vs digest semantics.

## Findings

- Mixed notation across `spec/01-data-model.md`, `spec/02-block-format.md`, `spec/06-meta-bonds.md`
- No convention established for placeholder format in examples

## Proposed Solutions

### Option 1: Standardize notation (Recommended)
- After Issue #14 decides CID vs digest for each field, use consistent format
- E.g., `<CID: "description">` for CIDs, `<digest: "description">` for digests
- Find-and-replace across all spec docs
- **Effort**: Small
- **Risk**: Low

## Recommended Action

Do after Issue #14 is resolved.

## Technical Details

- **Affected Files**: All spec docs with examples

## Acceptance Criteria

- [x] Single consistent placeholder format across all examples
- [x] Notation distinguishes CIDs from digests per Issue #14 decisions

## Work Log

### 2026-02-24 - Approved for Work
**By:** Claude Triage System
- Status changed from pending → ready

### 2026-08-12 - Notation Standardized
**By:** Claude

Blocker (Issue #14) resolved the same day: references inside Dialog structures are
raw 32-byte SHA-256 digests; the full 36-byte CIDv1 is external-only
(`spec/03-encoding.md` § "Internal references").

**Convention adopted** and documented in `spec/00-overview.md` § Conventions:

- `<digest of X>` — raw 32-byte SHA-256 digest of X; the form of every reference
  inside a Dialog structure (`prev`, `refs`, molecule `bond`, fillers of type 0/1/2)
- `<CID of X>` — full 36-byte CIDv1; used only where an external identifier is meant
- `<n bytes: ...>` — opaque byte string of the stated length (keys, signatures,
  ciphertext)

**Applied:**
- `spec/06-meta-bonds.md`: `<CID of ...>` (bond fields) and `<hash of ...>`
  (fillers) → `<digest of ...>`; `<different hash>` → `<different digest>`
- `spec/01-data-model.md`, `spec/02-block-format.md`: `<SHA-256 of ...>` and
  `<hash of ...>` → `<digest of ...>`
- `spec/02-block-format.md`: `<author B's key>` / `<signature>` →
  `<32 bytes: ...>` / `<64 bytes: ...>`
- `spec/04-cryptography.md`: `<32 bytes or null>` for `prev` →
  `<digest of previous block, or null>`

**Verification:** `grep -rn "<CID of\|<hash of" spec/ README.md` returns exactly one
hit — the `<CID of X>` line that defines the convention in `spec/00-overview.md`.
The remaining literal `→ CID: 01711220...` lines in `spec/06-meta-bonds.md` are
intentional: they display the external 36-byte form.

## Notes

Source: Triage session on 2026-02-24
Completed 2026-08-12 together with Issue #14.
