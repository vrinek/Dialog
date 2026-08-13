---
status: pending
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

- [ ] The private-key conversion is specified or its reference made normative
- [ ] The public-key conversion states how `y` is read and which encodings are
      rejected
- [ ] A conformance vector pins both conversions for a fixed seed

## Work Log

### 2026-08-13 - Filed While Implementing go/privacy
**By:** Claude

`X25519PublicFromEd25519` follows RFC 7748 §4.1 directly (big.Int arithmetic
over public data). `X25519PrivateFromEd25519` follows libsodium, which is the
reading the cited reference implementation fixes but the cited RFC section does
not. A test asserts the two agree — the converted private key's public key is
the converted public key — which catches the mismatch inside one implementation
but cannot catch a disagreement between two.

## Notes

Source: Go reference implementation, phase 4 (privacy).
