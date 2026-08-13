---
status: complete
priority: p2
issue_id: "046"
tags: [specification-gap, cryptography, interoperability, privacy]
dependencies: []
---

# Key Wrapping Says "Encrypt" and Nothing Else

## Problem Statement

`spec/04-cryptography.md` § "Key management" specifies the derivation of the
wrapping key completely — X25519 agreement, HKDF-SHA-256, empty salt, info
`"dialog-v1-key-wrap"`, 32 bytes — and then writes the step that uses it as:

```
wrapped_key = Encrypt(wrapping_key, chain_symmetric_key)
```

`Encrypt` is not a defined term anywhere in the specification. Four things are
missing, and each of them is enough on its own to make two conforming
implementations unable to read each other's wrapped keys:

1. **The algorithm.** XChaCha20-Poly1305 is the only AEAD the protocol names,
   but the section does not say to use it. AES-KW (RFC 3394) is the usual
   answer for a key-wrap step and would be an equally reasonable guess.
2. **The nonce.** If the algorithm is an AEAD, the wrap needs one, and none is
   mentioned. This is not a detail: the wrapping key is a pure function of one
   pair of identities and the constant info string, so it is *the same key for
   every wrap between those two parties, for every chain and for all time*. A
   fixed or implicit nonce would reuse a keystream across wraps.
3. **The AAD.** Whether the wrap binds anything — the recipient, the author,
   the chain — is unstated.
4. **The serialization.** Nothing says how nonce and ciphertext are laid out,
   so even two implementations that agree on 1–3 can disagree on the bytes.

A fifth question sits next to these: a wrapped key carries no indication of
*who it is for* or *whose chain key it is*. The specification says the
distribution mechanism is out of scope, which disposes of the transport but not
of the envelope — a reader handed a set of wrapped keys has nothing to match
against.

## Findings

- `spec/04-cryptography.md`, "Key management": the four-step procedure and the
  pseudocode block quoted above. Everything up to `wrapping_key` is exact.
- `spec/04-cryptography.md`, "Overview" and "Encryption scheme": XChaCha20-
  Poly1305 is the protocol's AEAD, used for block payloads. Nothing extends
  that choice to the wrap.
- `spec/04-cryptography.md`, "Security Considerations", "Nonce reuse": the
  warning is written for block nonces and applies with more force to a wrapping
  key, which is long-lived by construction.
- `go/privacy/wrap.go`, `Wrap`/`Unwrap`: implements the defensible reading —
  XChaCha20-Poly1305, a fresh random 24-byte nonce per wrap, no AAD, and
  `nonce || ciphertext` (72 bytes) as the layout — and says in its doc comment
  that the format is the implementation's and not the protocol's.

## Proposed Solutions

### Option 1: Specify the wrap as an XChaCha20-Poly1305 sealed box (Recommended)

- `wrapped_key = nonce || XChaCha20Poly1305_Encrypt(wrapping_key, nonce,
  chain_symmetric_key, aad)`, nonce 24 random bytes, and state the AAD (empty,
  or the two X25519 public keys — see Option 3).
- **Pros**: one AEAD in the whole protocol; the nonce problem is solved by the
  192-bit nonce space that already justifies XChaCha20 elsewhere; the layout is
  fixed and testable with vectors.
- **Cons**: 72 bytes per recipient rather than AES-KW's 40.
- **Effort**: Small (spec), none (Go — this is the implemented reading)
- **Risk**: Low

### Option 2: Specify AES-KW (RFC 3394)

- **Pros**: the standard answer for wrapping a key with a KEK; deterministic,
  no nonce, 40 bytes.
- **Cons**: introduces AES to a protocol that otherwise has none, and an
  implementation targeting embedded or WASM environments now needs two
  primitives instead of one.
- **Risk**: Low

### Option 3: Bind the wrap to the pair

- Whichever AEAD is chosen, make the AAD the two X25519 public keys (author
  then recipient) so that a wrapped key cannot be presented as coming from
  someone else.
- **Pros**: closes a gap the derivation only half-closes; costs nothing.
- **Cons**: a second thing to get byte-exactly right across implementations.
- **Risk**: Low

### Option 4: Define an envelope

- A small dCBOR structure carrying the recipient's public key, the author's
  public key and the wrapped bytes, marked informative if distribution stays
  out of scope.
- **Pros**: answers the "which of these is mine?" question once.
- **Cons**: edges into transport, which the specification deliberately leaves
  alone.
- **Risk**: Low

## Recommended Action

Option 1, with Option 3 folded in, and Option 4 as an informative appendix. Add
a conformance vector: fixed Ed25519 seeds for author and recipient, a fixed
content key and nonce, and the exact wrapped bytes — this is the one part of
the cryptography with no test vector anywhere, and it is the part two
implementations are most likely to disagree about.

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` ("Key management"),
  `go/privacy/wrap.go`, `vectors/`
- **Related Components**: private chains, key sharing between devices
- **Database Changes**: No

## Acceptance Criteria

- [x] The wrapping algorithm is named
- [x] The nonce (or the absence of one) is specified, with its uniqueness
      requirement stated relative to the long-lived wrapping key
- [x] The AAD is specified
- [x] The wrapped key's byte layout is specified
- [x] A conformance vector pins the result
- [x] `go/privacy` matches the ratified format

## Work Log

### 2026-08-13 - Filed While Implementing go/privacy
**By:** Claude

Found implementing spec/04's key management. Everything up to the wrapping key
is exact enough to write a vector for; the line that uses it is a placeholder.
The Go implementation picks the reading that reuses the protocol's own AEAD and
documents the choice at the two places a reader would look.

### 2026-08-13 - Ratified and Applied
**By:** Claude

**Decision (project lead):** Option 1, without Option 3 and without Option 4.
The wrap is an XChaCha20-Poly1305 sealed box under the wrapping key, with a
fresh random 24-byte nonce per wrap, an **empty AAD**, and `nonce || ciphertext`
as the layout — 72 bytes exactly (24 + 32 + 16), which decoders MUST reject any
departure from.

Option 3 (binding the two X25519 public keys as AAD) was declined as
redundant rather than wrong: the wrapping key is already a pure function of
that pair and of the info string `"dialog-v1-key-wrap"`, so a wrapped key that
authenticates under it can only have come from the other end of the pair. An
AAD would restate the derivation and give two implementations a second thing to
get byte-exactly right. Option 4 (an envelope) stays out: distribution is out of
scope, and the specification now says plainly that trial decryption is the
expected behaviour of a reader handed several wrapped keys.

**Changes:**

- `spec/04-cryptography.md`: the `wrapped_key` pseudocode line is replaced by
  the nonce generation and the sealed box; a new "Wrapped key format"
  subsection gives the offset/size table, the 72-byte total, the MUST to reject
  any other length without decrypting, the nonce-freshness MUST with the reason
  it binds harder here than for block encryption, and the reasoning for the
  empty AAD (with a MUST NOT against adding one); the "Nonce reuse" security
  consideration now covers the wrap; a new worked example, "Wrapping a chain
  key", gives every intermediate value in hex — both key conversions, the shared
  secret, the wrapping key, and the 72 wrapped bytes.
- `go/privacy/wrap.go`: `WrappedKeySize`, `Wrap` and `Unwrap` doc comments cite
  the specification instead of calling the format an implementation choice, and
  state why the AAD is empty. No behaviour change: the ratified format is the
  one implemented.
- `go/privacy/spec_test.go`: the vector constants are labelled as the
  specification's worked example; the "wrapped keys" subtest asserts the 72-byte
  composition, that the wrap begins with its nonce, and that five malformed
  lengths (nil, empty, truncated, nonce-only, one byte too long) are rejected on
  length rather than by the AEAD.

## Notes

Source: Go reference implementation, phase 4 (privacy).
