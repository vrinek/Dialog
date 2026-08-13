---
status: complete
priority: p3
issue_id: "047"
tags: [specification-gap, cryptography, interoperability]
dependencies: ["046"]
---

# The Ed25519-to-X25519 Conversion Is Normative for Points and Informative for Scalars

## Problem Statement

`spec/04-cryptography.md` § "Key management" says:

> The Ed25519-to-X25519 conversion MUST follow the birational map specified in
> RFC 7748 §4.1. Reference implementations include libsodium's
> `crypto_sign_ed25519_pk_to_curve25519` and
> `crypto_sign_ed25519_sk_to_curve25519`.

RFC 7748 §4.1 gives the map between the Edwards and Montgomery *curves*:
`u = (1 + y) / (1 - y)`. That is the whole of the public-key conversion and
nothing of the private-key one. An Ed25519 private key is a 32-byte seed, not a
scalar; the scalar is the low half of SHA-512 over the seed, clamped, and that
derivation is in RFC 8032 §5.1.5 — not in RFC 7748 §4.1, and not cited here.

An implementer who follows the normative sentence and skips the informative one
has two plausible readings: use the seed directly as the X25519 scalar, or
derive it as RFC 8032 does. Both produce a valid X25519 key; they produce
*different* ones, so the two implementations derive different shared secrets
and every wrapped key between them fails to open — with an authentication
error that says nothing about why.

The failure is silent in the worst way: each side's own round trip works
perfectly.

## Findings

- `spec/04-cryptography.md`, "Key management": the MUST quoted above, and the
  pseudocode `author_x25519_sk = Ed25519_to_X25519(author_ed25519_sk)`, which
  names a function the specification does not define.
- RFC 7748 §4.1: the birational map, points only.
- RFC 8032 §5.1.5: `h = SHA-512(seed)`, `s = h[0..31]` with `s[0] &= 248`,
  `s[31] &= 127`, `s[31] |= 64` — the clamped scalar libsodium's
  `crypto_sign_ed25519_sk_to_curve25519` returns.
- `go/privacy/wrap.go`, `X25519PrivateFromEd25519`: implements the libsodium
  behaviour and says in its doc comment that the specification pins it only by
  naming a reference implementation.
- The public half is unambiguous, but two of its edge cases are unmentioned:
  a non-canonical `y` (not reduced modulo 2^255−19) and `y = 1` (the identity,
  whose Montgomery image is at infinity). `go/privacy` rejects both.

## Proposed Solutions

### Option 1: Spell out both halves (Recommended)

- Public: `u = (1 + y) / (1 - y) mod 2^255 - 19`, where `y` is the Ed25519
  public key read little-endian with the top bit (the sign of `x`) cleared;
  reject `y = 1` and any `y` not reduced.
- Private: `SHA-512(seed)[0..31]`, clamped per RFC 8032 §5.1.5, is the X25519
  scalar; cite RFC 8032 alongside RFC 7748.
- **Pros**: removes the guess; both halves become vector-testable.
- **Cons**: a few lines of formulae in a document that otherwise defers to RFCs.
- **Effort**: Small (spec), none (Go)
- **Risk**: Low

### Option 2: Make libsodium normative

- "The conversion MUST match libsodium's `crypto_sign_ed25519_pk_to_curve25519`
  and `crypto_sign_ed25519_sk_to_curve25519`."
- **Pros**: one sentence, unambiguous in practice.
- **Cons**: pins the protocol to a library's behaviour; an implementation with
  no libsodium has to reverse-engineer it anyway.
- **Risk**: Low

### Option 3: Use separate X25519 keys

- Stop converting: let an author publish an X25519 key of their own for
  encryption.
- **Pros**: no conversion to specify; the cleanest cryptographic hygiene.
- **Cons**: a second key to distribute and bind to the identity, which is
  exactly what "a single keypair serves both purposes" was meant to avoid.
- **Risk**: Medium

## Recommended Action

Option 1, plus a conformance vector giving both conversions for a fixed seed
(the wrapping-key vector of issue #46 would exercise them end to end).

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` ("Key management",
  "References"), `go/privacy/wrap.go`, `vectors/`
- **Related Components**: key wrapping, private chains
- **Database Changes**: No

## Acceptance Criteria

- [x] The private-key conversion is specified or its reference made normative
- [x] The public-key conversion states how `y` is read and which encodings are
      rejected
- [x] A conformance vector pins both conversions for a fixed seed

## Work Log

### 2026-08-13 - Filed While Implementing go/privacy
**By:** Claude

`X25519PublicFromEd25519` follows RFC 7748 §4.1 directly (big.Int arithmetic
over public data). `X25519PrivateFromEd25519` follows libsodium, which is the
reading the cited reference implementation fixes but the cited RFC section does
not. A test asserts the two agree — the converted private key's public key is
the converted public key — which catches the mismatch inside one implementation
but cannot catch a disagreement between two.

### 2026-08-13 - Ratified and Applied
**By:** Claude

**Decision (project lead):** Option 1. Both halves of the conversion are now
specified step by step in `spec/04-cryptography.md`, under their own heading,
and libsodium is informative rather than load-bearing.

The public half: clear the sign bit, read *y* little-endian, reject *y* ≥ *p*
and *y* = 1, compute *u* = (1 + *y*)/(1 − *y*) mod *p*, encode little-endian.
The private half: *s* = SHA-512(seed)[0..31], clamped per RFC 8032 §5.1.5 — with
the warning that using the seed itself produces a valid X25519 key that agrees
with nobody, which is the failure this issue was filed about. RFC 8032 is now
cited for the scalar as RFC 7748 is for the point; both were already normative
references.

**Small-order and non-canonical keys.** The specification states what
`go/privacy` does, exactly. Non-canonical *y* and *y* = 1 are rejected at the
conversion. A small-order public key is rejected at the *agreement*, which
yields all zeroes for one (RFC 7748 §6.1) — the rejection is a MUST, and its
placement is explicitly left to the implementation, since no wrapping key is
derived either way. Rejecting off-curve encodings is not required: only *y*
enters the map, `X25519PublicFromEd25519` never decompresses the point, and a
key that is not a valid Ed25519 point cannot sign a block or hold an identity in
Dialog. This is where libsodium's rejection set differs from this
implementation's, and the specification says so in an informative note rather
than pretending the two coincide.

**Changes:**

- `spec/04-cryptography.md`: the one-sentence MUST citing RFC 7748 §4.1 is
  replaced by the "Ed25519-to-X25519 conversion" subsection, with the five-step
  public procedure, the four-step private one, the small-order rule, and an
  informative note on where libsodium agrees and where it does not; the "Key
  management" pseudocode now points at it; the "Ed25519 to X25519 conversion"
  security consideration says conformance is to the steps, not to a library;
  the worked example added for issue #46 gives both conversions for both
  parties.
- `go/privacy/wrap.go`: the doc comments of `X25519PublicFromEd25519`,
  `X25519PrivateFromEd25519` and `WrappingKey` cite the new subsection, state
  which encodings each rejects and where, and record the libsodium difference.
  No behaviour change.
- `go/privacy/spec_test.go`: `TestX25519Conversion` gains a "worked example"
  subtest pinning the X25519 private and public keys for seeds 0x01 and 0x02
  and the shared secret they agree on — the bytes the specification prints —
  and a "small-order keys" subtest asserting that neither a `WrappingKey` nor a
  `Wrap` comes out for the order-2 and order-4 points.

## Notes

Source: Go reference implementation, phase 4 (privacy).
