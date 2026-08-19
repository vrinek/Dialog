# TypeScript implementation — design and plan

**Status:** complete (phases 1-5); phase 6 (transport) added 2026-08-19
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
`spec/*.md`, `vectors/*`, this plan, and `demo/chains/` as data — the committed
`.block` files and `index.json` are readable bytes, `demo/`'s Go source is not.
The Go implementation's design
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
- **Scope: wire format** (the four vector files), and — since phase 6 — the
  optional transport profile of `spec/07-transport.md`, client and server. L2/L3
  are node behavior, not interop surface; a TypeScript port of them is future
  work.
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
   vectors) and 057 (no nesting bound; the codec picked 1024). Both are since
   settled as rules 9 and 10 of spec/03-encoding.md; the bound is 64, counted
   over containers, and `dcbor.json` grew four cases pinning it, so the counts
   above now read 96 cases: 42 valid and 54 invalid.
2. `entity` — every case in `vectors/entities.json`.
   — **done:** `src/entity.ts` — validating constructors for atoms, bonds and
   molecules, the template-variable grammar with its leftmost-longest match,
   the five filler types bound to the one value shape each tag permits, the
   canonical timestamp profile including the proleptic Gregorian calendar rule,
   canonical encoding with digest/CID/CID text for every entity, strict
   decoders enforcing the closed-map rule, and the five standard meta-bonds of
   spec/06 with their templates, variables and digests. All 26 cases of
   `vectors/entities.json` pass — each rebuilt from the published JSON value
   model through the constructors, encoded byte-identically and decoded back,
   with the three molecules also recomputed from their descriptions and
   templates alone. The filler-count rule sits where the specification puts it,
   at the layer that has resolved the bond (spec/02, "Validation" rule 5).
   Two gaps filed: 058 (entities.json pins no invalid entity, so spec/01's
   rejection rules — the timestamp profile above all — are untested by the
   interop contract; the suite's rejection tests are hand-written from the
   prose) and 059 (a meta-bond's `Fillers:` line is not a validation rule
   anywhere, so this implementation applies no filler-type check to one). Both
   are since settled: `entities.json` grew an `invalid` section of 38 cases —
   one or more per rejection rule of spec/01, the six timestamp rules and the
   `1500-02-29` / `1600-02-29` calendar pair included — plus two valid cases,
   so the counts above now read 66 cases, 28 valid and 38 invalid, over 16
   entities. spec/06 now states that the `Fillers:` lines are L3 recognition
   criteria and not validity rules, which is the reading this implementation
   took.
3. `block` — signing, digests, structural validation; every case in
   `vectors/blocks.json` (chain, forks, invalid cases; validation rules per
   spec/02 as far as the vectors exercise them, full rule engine per spec).
   — **done:** `src/block.ts` — the three block types and four operations with
   validating constructors, canonical encoding, the block digest and CID over
   the complete encoding (signature included) and a strict decoder enforcing
   the closed-map rule per type and per operation; Ed25519 signing and
   verification per spec/04, over the `dialog-v1-block` domain separator and
   the dCBOR of the block without `sig`, with `@noble/curves` as the one new
   runtime dependency; and the ten numbered validation rules of spec/02 run in
   order against a block source, including the demand-driven resolution
   procedure of spec/05 (same block, then `prev` ancestors, then `refs`
   transitively, under a scan limit), the data-model rule with its filler count
   and digest-kind binding, the rotation and succession rules, and an in-memory
   `BlockStore` that carries the induction — a block whose ancestry has not
   arrived is held as stored but unvalidated and re-validated when it lands —
   and surfaces forks and ambiguous successions rather than settling them.
   All 30 cases of `vectors/blocks.json` pass: the five chain blocks and the
   fork block rebuilt from the published value model, re-encoded byte for byte,
   their signing bytes and signing input reproduced, their signatures verified
   *and* re-signed from the seeds, their digests, CIDs and CID texts recomputed;
   the chain replayed in order into a store and validated block by block; the
   fork pair detected with the digests the `forks` case pins; and all 23 invalid
   cases rejected by the class of rule their `rule` field names. Two gaps filed:
   060 (spec/05's scan limit has no default and no stated counting unit, so one
   number an implementation invents decides validity; this codec picked 1024
   foreign blocks per validation, configurable) and 061 (`blocks.json` pins no
   chain-relative rejection at all — rules 3, 4, 5, 6, the own-chain half of 10
   and the succession rules have no invalid bytes, so the suite's tests for them
   are hand-written from the prose).
4. `privacy` — every case in `vectors/privacy.json` (AEAD, AAD, key wrap,
   X25519 conversion).
   — **done:** `src/privacy.ts` — the private-block payload (`refs`/`ts`/`ops`
   dCBOR, exactly as a public block encodes the same three fields) and its AAD
   (the plaintext fields but `sig`/`enc`/`nonce`), XChaCha20-Poly1305 seal and
   open over them via `@noble/ciphers` (the one new runtime dependency), the
   Ed25519-to-X25519 conversion hand-rolled from spec/04's five public and four
   private steps (field arithmetic mod 2^255−19, SHA-512 scalar expansion and
   clamping) with its stated rejections — non-canonical *y*, *y* = 1, and the
   all-zero agreement result of a small-order key, the last caught through
   `@noble/curves`' own zero-output check — and the per-recipient wrap: X25519
   agreement, HKDF-SHA-256 with the exact info string and an explicit
   zero-length salt, and the pinned 72-byte `nonce || ciphertext` layout,
   length-checked before any decryption is attempted. `build/seal` and
   `open`/`unwrap` APIs sit alongside `./block.ts`'s unsigned-block builders and
   `signBlock`, the same shape as the other three block types.
   All 11 cases of `vectors/privacy.json` pass: the payload and both AAD cases
   re-encode byte for byte, `enc` is reproduced by sealing under the pinned
   content key and nonce, all three X25519 conversions and both key-wrap cases
   (shared secret, wrapping key, 72-byte wrapped key) come out exactly as
   pinned, and the `private_block` case rebuilds, its signature verifies and
   re-derives, its digest/CID/CID text recompute, and it opens to the payload
   section's bytes. Hand-written tests cover tamper (`enc`, `nonce`, each
   AAD-covered field), wrong key, wrong recipient, strict-decode rejection of a
   non-canonical decrypted plaintext and of a `rotate_key` op inside a private
   payload, wrapped-key length violations, and — since no vector pins them —
   the three Ed25519-to-X25519 rejection rules. One gap filed: 062
   (`privacy.json` pins no invalid case at all, for any of the five rejection
   rules spec/04 states in prose). Since settled: `privacy.json` grew an
   `invalid` section of 13 cases — the two X25519-conversion rejections, the
   small-order agreement, four key-wrap rejections (three lengths and a
   tamper), three AEAD tamper cases, the enc floor, and the two payload cases
   (non-canonical plaintext, a `rotate_key` op) — each verified rejected by
   the Go reference implementation before emission and consumed by both
   implementations unmodified; the hand-written tests that duplicated a now-
   pinned rule were removed, and only the ones that are additional instances
   of a pinned rule (not new rules) remain.
5. CI workflow + docs (README implementations table, AGENTS.md) + this plan
   marked complete. Cross-validation note: both implementations green against
   the same committed vectors on the same commit.
   — **done:** `.github/workflows/ts.yml`, mirroring `go.yml`'s conventions
   (SHA-pinned actions, `persist-credentials: false`, the same
   cache-poisoning suppression, one `check` job) — `npm ci`, `tsc --noEmit`,
   `node --test`, on push/PR/dispatch. `actionlint` and `zizmor` both report no
   findings. README's "Reference implementation" section became
   "Implementations", with Go (reference; the full L1–L3 pipeline; source of
   `vectors/`) and TypeScript (clean-room; wire format only; browser-safe;
   `@noble/*` deps) each in their own subsection, both verified against
   `vectors/`. AGENTS.md gained a "TypeScript implementation" subsection (the
   two commands, the clean-room rule restated for future agents, and the
   vector-consumption convention — case-count assertions, dispatch on a
   case's populated fields rather than a `kind` discriminant) and a second
   "Language & Framework" bullet. This plan marked complete; see "Outcome"
   below.

6. **The transport profile** (`spec/07-transport.md`), added after the plan's
   original five phases closed. Same clean-room rule, one input added:
   `demo/chains/` as *data* — the committed `.block` files and `index.json` are
   readable bytes, `demo/`'s Go source is not.
   — **done:** `src/blockseq.ts` (the RFC 8742 block sequence: encode, a strict
   reader with the caller's bounds, and the two ordering rules as named checks;
   `src/dcbor.ts` gained `decodeFirst`, since `decode` is the rule for a Dialog
   *document* and a sequence needs an item's length), `src/transport.ts` (the
   constructive tip walk shared by both sides, the six operations as a server
   over a `BlockStore`, the client with every client rule applied, and
   `syncChain` / `syncChainFromSources` / `resolveReferences`) and
   `src/node-http.ts` (the one Node-only module, and a type-only import, so
   library code stays browser-safe). 70 tests: the profile's conformance tables
   read off the text — media types, `Dialog-Tip`, `ETag`/`If-None-Match`, the
   status codes, the rejected spellings of an author key, a CID, a position and
   `limit`, the 256-digest floor — plus a cold sync of the three committed demo
   chains TS-to-TS with the downstream store's state asserted, a fork neither of
   two sources admits to appearing at the client that asks both, errata's blocks
   held undecided through two sources and settled by a third, an announce round
   trip with held and rejected dispositions, and the file-form equality the
   profile names as a test rather than a claim: one author's `.block` files
   concatenated in index order are that author's whole-chain range response,
   byte for byte. **Scope is now wire format + transport; L2 and L3 remain out.**
   Six gaps filed, 090 to 095 — see the commit and the todos themselves.

## Rules for implementing agents

- Clean-room rule above is absolute.
- Spec is normative; vectors are ground truth; any gap between what the spec
  says and what a vector contains is a todo (never silently resolve; next free
  number: 096 — phase 1 filed 056 and 057, phase 2 filed 058 and 059, phase 3
  filed 060 and 061, phase 4 filed 062, phase 6 filed 090 to 095).
- `tsc --noEmit` and `node --test` clean before every commit; granular commits,
  conventional messages, required trailers; no pushes.

## Outcome

Two independent implementations of the Dialog wire format now exist, kept
apart in the one sense that matters here: the TypeScript implementation never
read the Go one, only `spec/` and `vectors/`. Both are green against the same
committed `vectors/` on the same commit, which is the test this plan set out
to run — not "is Dialog implementable" (the Go implementation already
answered that) but "is `spec/` plus `vectors/` *enough*, on their own, to
reach the same bytes twice."

**Vector suite growth.** Every phase that hit a place the two documents did
not pin closed the gap by adding cases, never by trusting one implementation's
reading over the other's:

| File | Before | After | Todos |
|------|-------:|------:|-------|
| `dcbor.json` | 92 | 96 | 056, 057 |
| `entities.json` | 26 | 66 | 058, 059 |
| `blocks.json` | 30 | 42 | 060, 061 |
| `privacy.json` | 11 | 24 | 062 |
| **Total** | **159** | **228** | |

Every added case is an acceptance or a rejection now pinned for any future
implementation, not just these two — the whole point of a vector file over a
pairwise agreement.

**Divergences the clean-room process caught.** Two of the seven todos found a
place where the two implementations had already, independently, made
*different* choices — not just gaps in what the vectors tested, but instances
of the disagreement the whole exercise exists to surface:

- **The dCBOR nesting depth bound** (057): spec/03-encoding.md's rule 9
  ("Implementations MUST support nesting to a reasonable depth") named no
  number. The TypeScript implementation picked 1024; the Go implementation had
  no bound at all. Settled by naming 64, counted over containers, as rule 10
  of spec/03-encoding.md, with four new `dcbor.json` cases pinning it.
- **The reference-resolution scan limit** (060): spec/05-processing-model.md
  said an implementation SHOULD default it and MUST allow configuring it,
  without saying to what or over what unit. The TypeScript implementation had
  defaulted to 1024, counting distinct digests seen; the Go implementation to
  256, counting foreign blocks visited — two different numbers *and* two
  different definitions of what the number counts, both conforming to the
  same sentence. Settled at 256, foreign blocks, named in the specification;
  the TypeScript implementation's default changed to match.

Both were invisible to each implementation's own test suite — a codec or a
validator that only ever talks to itself has no way to notice its own
threshold is arbitrary — and both were found by the second implementation
existing at all, before either committed vector or hand-written test caught
them.

**Todos 056-062, all settled.** Every gap the clean-room process filed was
resolved by adding to `spec/` or `vectors/`, never by weakening either
implementation to match the other: 056 (map keys are text-only in the
vectors), 057 and 060 above, 058 (`entities.json`'s rejection rules were
unpinned), 059 (a meta-bond's `Fillers:` line is an L3 recognition criterion,
not a validity rule), 061 (`blocks.json`'s chain-relative rejections were
unpinned), 062 (`privacy.json`'s five rejection rules were unpinned). The
running total is seven todos filed over five phases and seven settled; none
remain open from this effort.
