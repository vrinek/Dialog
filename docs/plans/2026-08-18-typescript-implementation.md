# TypeScript implementation — design and plan

**Status:** in-progress
**Date:** 2026-08-18

## Purpose

Second, independent implementation of the Dialog wire format, written against
`spec/` and `vectors/` **only**. This is the test of the project's central thesis:
that the spec plus the conformance vectors are a sufficient interop contract.
Every place the TypeScript implementation cannot be completed from those two
sources alone is a spec/vectors defect and gets filed as a `todos/` entry.

A browser-capable implementation is also strategically necessary in its own
right: Dialog's stated vision is an off-ramp from the web, which requires the
protocol to run where the web runs.

## The clean-room rule

**Implementing agents MUST NOT read anything under `go/`.** Allowed inputs:
`spec/*.md`, `vectors/*`, and this plan. The Go implementation's design
decisions (package shapes, naming, internal helpers) must not leak; only the
spec's normative text and the vectors' bytes constrain the TypeScript code.
The one exception: `go/` may be *executed* (`go test`, `genvectors -check`)
by the final cross-validation phase, never read.

## Decisions

- **Location:** `ts/` subdirectory, package name `dialog-protocol`, `private: true`
  (npm publication is a later, user-approved step).
- **Runtime:** Node 24 (native TypeScript type-stripping — no build step for tests),
  ES modules. Browser compatibility is a design constraint (no Node-only APIs in
  library code; `node:` imports only in tests/CLI).
- **Dependencies:** the audited, zero-transitive-dependency noble family only —
  `@noble/curves` (ed25519, x25519), `@noble/ciphers` (xchacha20poly1305),
  `@noble/hashes` (sha256, sha512, hkdf). dCBOR is hand-rolled from spec/03,
  same rationale as the first implementation. Dev-deps: `typescript` (type-check
  via `tsc --noEmit`), nothing else; tests use `node:test`.
- **Layout mirrors the protocol, not the Go code:** `src/dcbor.ts`, `src/cid.ts`,
  `src/entity.ts`, `src/block.ts`, `src/privacy.ts` (agents may split files as
  the code demands — the constraint is what each area implements, per spec).
- **Scope: wire format** (the four vector files). L2/L3 are node behavior, not
  interop surface; a TypeScript port of them is future work.
- **Conformance is the test suite:** each phase's tests load the corresponding
  `vectors/*.json` and verify every case — valid bytes reproduced exactly,
  invalid bytes rejected. Additional unit tests are welcome; the vectors are
  the acceptance bar.
- **CI:** `.github/workflows/ts.yml` — setup Node 24 (SHA-pinned action),
  `npm ci`, `tsc --noEmit`, `node --test`, on every push/PR.

## Phases

1. Scaffold (`package.json`, `tsconfig.json`, lockfile) + `dcbor` + `cid`,
   passing every case in `vectors/dcbor.json` and the entity-CID parts the
   file exercises.
   — **done:** `src/dcbor.ts` (encoder, strict decoder, `Decimal` with the tag 4
   canonicalization rules), `src/cid.ts` (digest, CID, multihash, the multibase
   base32 text form) and `src/hex.ts`. All 92 cases of `vectors/dcbor.json` pass
   — 41 valid ones in both directions, 51 invalid ones rejected with the class
   of rule the vector names — and all 26 cases of `vectors/entities.json`
   re-encode, with the digest, CID and `cid_text` of all 15 entities recomputed
   from their bytes. Two gaps filed: 056 (map keys are text strings only in the
   vectors) and 057 (no nesting bound; the codec picks 1024).
2. `entity` — every case in `vectors/entities.json`.
3. `block` — signing, digests, structural validation; every case in
   `vectors/blocks.json` (chain, forks, invalid cases; validation rules per
   spec/02 as far as the vectors exercise them, full rule engine per spec).
4. `privacy` — every case in `vectors/privacy.json` (AEAD, AAD, key wrap,
   X25519 conversion).
5. CI workflow + docs (README implementations table, AGENTS.md) + this plan
   marked complete. Cross-validation note: both implementations green against
   the same committed vectors on the same commit.

## Rules for implementing agents

- Clean-room rule above is absolute.
- Spec is normative; vectors are ground truth; any gap between what the spec
  says and what a vector contains is a todo (never silently resolve; next free
  number: 058 — phase 1 filed 056 and 057).
- `tsc --noEmit` and `node --test` clean before every commit; granular commits,
  conventional messages, required trailers; no pushes.
