---
status: ready
priority: p2
issue_id: "062"
tags: [conformance-vectors, cryptography, privacy, validation, interoperability]
dependencies: []
---

# `privacy.json` Pins No Invalid Case

## Problem Statement

`vectors/privacy.json` has five sections — `payload`, `aead`, `x25519`,
`key_wrap`, `private_block` — and every one of them is an *accepting* case: a
value that encodes to a pinned byte string, a seed that converts to a pinned
key, a shared secret that derives a pinned wrapping key, a wrap that opens to a
pinned content key, a block that verifies and decrypts. There is no `invalid`
section at all, unlike `dcbor.json` (54 invalid cases) and `entities.json` (38,
added to settle todo 058) and unlike `blocks.json`'s `invalid` (23) and
`invalid_in_chain` (12, added to settle todo 061).

That leaves every rejection rule `spec/04-cryptography.md` states in prose
unpinned by the interop contract:

- **Non-canonical *y*** — "Implementations MUST reject a key with y ≥ p"
  ("Ed25519-to-X25519 conversion", step 2).
- ***y* = 1** — "Implementations MUST reject y = 1" (step 3).
- **All-zero agreement (small-order key)** — "Implementations MUST reject an
  all-zero agreement result ... and MUST NOT derive a wrapping key from it"
  (the discussion after step 5).
- **Wrapped-key length** — "Implementations MUST reject a wrapped key of any
  other length [than 72 bytes] without attempting to decrypt it" ("Wrapped key
  format").
- **AAD or ciphertext tamper** — "If the AAD does not match during
  decryption ..., the block MUST be rejected. If decryption fails for any
  other reason ..., the block MUST also be rejected." ("Decryption procedure").

None of these has a single byte in the vector file. Two implementations can
pass every committed case in `privacy.json` while disagreeing about, say,
whether a non-canonical *y* is rejected before or only after the point where a
wrong division would silently produce a bogus key — the same shape of gap
todos 058 and 061 recorded for the two files below this one in the layer stack.

## Findings

- `vectors/privacy.json`, all five sections (11 cases total): every case is an
  accept-and-reproduce-these-bytes case. Verified against the TypeScript
  implementation (`ts/src/privacy.ts`, `ts/test/privacy.test.ts`) — none of the
  11 cases exercises a rejection path.
- The five rejection rules above are each stated only in `spec/04-cryptography.md`'s
  prose (quoted above), never as a CDDL constraint or a table row a vector
  generator could derive mechanically.
- The TypeScript suite's tests for all five rules are hand-written from that
  prose (`ts/test/privacy.test.ts`, the "Rejections" section and the
  "Ed25519-to-X25519 conversion rejections" section at the bottom). Nothing in
  `vectors/` says the reference implementation must agree with any of them —
  in particular, nothing pins *where* the small-order rejection is expected to
  surface, which spec/04 explicitly leaves to the implementation ("An
  implementation MAY instead reject small-order encodings earlier, at
  conversion time").
- Unlike `entities.json` and `blocks.json`'s gaps, none of these five rules
  needs a store or a multi-block scenario — every one is checkable from a
  single seed, key or ciphertext plus one deliberately wrong input, so the
  existing case shape (`hex` in, `hex` or `error` out) is already sufficient;
  no new section shape like `invalid_in_chain` is required.

## Proposed Solutions

### Option 1: Add an `invalid` section to `privacy.json` (Recommended)

One case per rule, each naming the rule it exercises and the bytes needed to
reproduce the rejection:

```jsonc
{
  "name": "y_not_canonical",
  "rule": "spec/04-cryptography.md, Ed25519-to-X25519 conversion, public keys, step 2",
  "reason": "y = p (2^255 - 19) is not a canonical encoding; no Ed25519 public key produces it.",
  "input": "<32-byte hex encoding of y = p, sign bit clear>"
},
{
  "name": "y_equals_one",
  "rule": "... step 3",
  "input": "<32-byte hex encoding of y = 1>"
},
{
  "name": "small_order_agreement",
  "rule": "... the all-zero agreement result",
  "x25519_private_key": "<any valid scalar>",
  "x25519_public_key": "<a known low-order u, e.g. u = 0, RFC 7748 §5.2>"
},
{
  "name": "wrapped_key_wrong_length",
  "rule": "... Wrapped key format",
  "wrapping_key": "<32 bytes>",
  "wrapped_key": "<71 or 73 bytes>"
},
{
  "name": "tampered_enc",
  "rule": "... Decryption procedure",
  "content_key": "<pinned>",
  "block": "<the private_block case's block, with one bit of enc flipped>"
}
```

Five cases cover the five rules; `tampered_enc`'s sibling cases (tampered
nonce, tampered `v`/`pub`/`prev`, wrong key) are the same rule and can be one
case each or folded into `tampered_enc`'s `reason`, at the generator's
discretion.

- **Pros**: closes the gap with committed bytes, not implementer-invented
  fixtures; the existing case shape already fits, since none of these five
  rules needs a multi-block scenario.
- **Cons**: none of note — this is strictly additive, unlike `blocks.json`'s
  `invalid_in_chain`, which needed a new shape.
- **Effort**: Small (generator + vectors), Small (implementations, which
  mostly already have these tests hand-written and would swap them for
  vector-driven ones)
- **Risk**: Low — additive.

### Option 2: Leave it

- **Pros**: nothing to do.
- **Cons**: an implementation that checks the non-canonical-*y* rule with `>`
  instead of `>=`, or that never checks it and instead lets a bad reduction
  silently produce a working-but-wrong key, passes every committed vector.
  These are exactly the bugs a conformance suite exists to catch, and the
  X25519 conversion is the section spec/04 itself flags as "the single most
  common place for an implementation to go wrong" (`vectors/README.md`,
  "Using them in a new implementation").
- **Risk**: Medium-High — silent key-derivation bugs are the worst kind to
  ship, since they fail only against a specific other implementation's edge
  case, not against your own round trip.

## Recommended Action

Option 1. `small_order_agreement` is the one worth writing first: it is the
rule spec/04 explicitly says an implementation may choose to enforce at either
of two different points ("MAY instead reject small-order encodings earlier, at
conversion time — libsodium does"), which makes it the rule most likely to
produce two implementations that both "reject" but disagree about which
function call the rejection happens in — invisible until a test asserts on the
call site rather than just on "throws".

## Technical Details

- **Affected Files**: `vectors/privacy.json` (new `invalid` section),
  `vectors/README.md` (the file table's section counts and case-shape
  description), the vector generator, both implementations' privacy test
  suites
- **Related Components**: private-block encryption, key wrapping, L1/L2
  cryptographic primitives
- **Database Changes**: No

## Acceptance Criteria

- [ ] `vectors/privacy.json` pins at least one rejection case per rule: the two
      X25519-conversion rejections, the small-order agreement, the wrapped-key
      length check, and AEAD tamper/wrong-key rejection
- [ ] `vectors/README.md` records the new section, its shape and its case
      count
- [ ] Both implementations replay each case and reject it, reporting the rule
      class the case names

## Notes

Source: TypeScript implementation, phase 4 (privacy). Sibling of todo 058
(`entities.json`) and todo 061 (`blocks.json`), which recorded the same shape
of gap at the two layers below this one and were settled by adding `invalid`
(and, for blocks, `invalid_in_chain`) sections. This is the shallowest of the
three: every rule here is checkable from a single input plus one wrong value,
with no store and no multi-block scenario needed.
