---
status: complete
priority: p2
issue_id: "076"
tags: [transport, specification-gap, encoding, cryptography, interoperability, conformance-vectors]
dependencies: []
---

# A Canonical Text Form for an Author's Public Key

## Problem Statement

`spec/03-encoding.md` defines a canonical text representation for a CID — the
multibase base32 form, `b` plus the lowercase unpadded RFC 4648 encoding of the
36 CID bytes — and `spec/04-cryptography.md` defines none for the other
identifier the protocol has. An author is an Ed25519 public key
(`spec/04-cryptography.md`, "Author identity"), and the only thing the
specification said about writing one down was:

> When keys need to be communicated externally (e.g., displayed to users, shared
> out-of-band), implementations MAY use multicodec-prefixed representations.
> — `spec/04-cryptography.md`, "Key encoding"

A MAY over an unnamed family of representations is not an interoperable
identifier. Two implementations displaying the same author would print different
strings, and neither could read the other's. That is tolerable while a key never
leaves a process; it stops being tolerable the moment anything names a chain by
its author — a URL path, a subscription file, a log line, an API argument. The
transport design document put it plainly: "Nothing about transport can be
written down before this exists."

## Findings

- `spec/03-encoding.md`, "Text representation": the CID form, and the pattern to
  follow — a fixed alphabet, a fixed multibase code, an exact length, explicit
  rejections, and a statement that hex byte dumps are not a text form.
- `spec/04-cryptography.md`, "Key encoding": raw 32-byte keys inside the
  protocol's CBOR, no multicodec prefix, and the MAY quoted above for external
  use.
- `vectors/blocks.json` and `vectors/privacy.json` pinned each test key's raw
  bytes and nothing else, so no conformance case covered rendering a key.
- `docs/design/2026-08-18-transport-design.md` §4.1: `{author}` appears in five
  of the sketch's six endpoints; §5 Q1 asks for "a canonical, case-stable,
  URL-safe encoding" and asks whether it is a bare multibase string or a
  CID-like structure with a codec prefix.
- The multicodec table gives `ed25519-pub` the code `0xed`. Unlike every code a
  Dialog CID uses (`0x01`, `0x71`, `0x12`, `0x20`, all below 128), 237 does not
  fit in a single-byte unsigned varint: `varint(0xed) = 0xed 0x01`. This is the
  detail a reimplementation gets wrong, and it is why the prefixed key is 34
  bytes rather than 33.

## Proposed Solutions

### Option 1: Multicodec-prefixed multibase base32 (Recommended, ratified)

`"b" || base32-lower-nopad(0xed 0x01 || key)`. Self-describing, one text
alphabet with the CID form, case-stable and URL-safe.

- **Pros**: a decoder can tell what it is holding, and cannot mistake it for a
  CID (56 characters beginning `b5ua`, against 59 beginning `bafyrei`); the same
  34 bytes in base58btc are exactly a `did:key` payload, so interoperation with
  that ecosystem costs a re-encoding and no new bytes; the base32 codec is
  already implemented for CIDs in both implementations.
- **Cons**: two bytes of overhead per rendered key, and a text form 56
  characters long where a bare base32 key would be 53.
- **Effort**: Small (spec), Small (both implementations), Small (vectors)
- **Risk**: Low — additive; no protocol byte moves.

### Option 2: Bare multibase base32 over the 32 key bytes

- **Pros**: shortest; nothing to strip after decoding.
- **Cons**: not self-describing — the string says "these are bytes in base32"
  and nothing about what they are; no `did:key` relationship; a 32-byte and a
  36-byte base32 string are distinguishable only by length, which is a fragile
  thing to key a type on.

### Option 3: A CID-like structure over the key

Wrap the key in a CIDv1 with a key codec.

- **Pros**: one parser for both identifiers.
- **Cons**: a CID identifies *content by its hash*; a public key is not a hash
  of anything, and putting one behind a multihash prefix would be a lie about
  what the identifier means. It would also collide conceptually with
  `spec/03`'s "Implementations MUST reject CIDs that use different parameters".

## Recommended Action

Option 1.

## Technical Details

- **Affected Files**: `spec/03-encoding.md`, `spec/04-cryptography.md`,
  `go/cid/`, `ts/src/cid.ts`, `go/internal/vectorfile/vectorfile.go`,
  `go/internal/vectors/`, `vectors/blocks.json`, `vectors/privacy.json`,
  `vectors/README.md`
- **Related Components**: author identity, external identifiers, any future
  transport profile
- **Database Changes**: No

## Acceptance Criteria

- [x] The specification defines one canonical text form for an author's public
      key, with its alphabet, prefix, length and rejections
- [x] `spec/04-cryptography.md`'s "MAY use multicodec-prefixed representations"
      points at it instead
- [x] Both implementations render and parse the form, and reject every
      near-miss the specification lists
- [x] `vectors/` pins the text form of every test key, and both implementations
      reproduce it from the key bytes

## Work Log

### 2026-08-18 - Filed and Ratified in One Run

**By:** Claude

**Decision (project lead):** Option 1. The canonical text representation of an
author's Ed25519 public key is multibase base32 — lowercase RFC 4648, no
padding, `b` prefix — applied to the 34 bytes `0xed 0x01 || key`, the multicodec
`ed25519-pub` code followed by the 32 key bytes. Decoders MUST reject a wrong
multibase prefix, uppercase, padding, a wrong length, and a wrong multicodec
prefix.

**Changes:**

- `spec/03-encoding.md` gains "Text representation of author keys", beside the
  CID text form rather than in `spec/04`, because that is where text forms live
  and the two share an alphabet, a multibase code and a rejection discipline.
  It fixes the parameters in a table, spells out that `0xed` is 237 and
  therefore encodes as the *two* bytes `0xed 0x01`, states the 56-character
  length and the constant `b5ua` prefix, lists the five rejections, notes that
  the two text forms are unambiguous against each other, and works the example
  through with the `alice` test key. An informative paragraph records the
  `did:key` equivalence — the same 34 bytes in base58btc give
  `did:key:z6Mkon3Necd6NkkyfoGoHxid2znGc59LU3K7mubaRcFbLfLX` for that key —
  while defining no DID method.
- `spec/03-encoding.md`, Security Considerations: a new bullet covering both
  text forms. These strings are compared *as strings* — in an access list, a
  subscription set, a cache key — so a decoder that accepts a padded or
  uppercase variant is not being lenient, it is minting aliases.
- `spec/04-cryptography.md`, "Key encoding": the MAY becomes a MUST pointing at
  `spec/03`, with the note that the multicodec prefix exists in the text form
  only and never enters the protocol's CBOR structures, and that signatures and
  X25519 keys have no text form of their own.
- `go/cid/authorkey.go`: `AuthorKeyText`, `ParseAuthorKeyText` and
  `ParseAuthorKeyBytes`, in the package that already holds the base32 codec and
  the CID text form. Tests pin the worked example, the round trip over eight
  keys, the length and prefix, the mutual rejection of the two text forms, and
  each rejection case separately. `TestAuthorKeyMulticodecVarint` exists because
  the two-byte varint is the one parameter a reimplementation will get wrong.
- `ts/src/cid.ts`: the same, written clean-room from the new spec section by an
  agent that read `spec/` and `vectors/` only, with its own tests.
- `vectors/blocks.json` and `vectors/privacy.json`: every key in `inputs` gains
  `public_key_text`. Purely additive — no digest, signature, ciphertext or block
  byte moved — and both implementations verify they reproduce it from
  `public_key` and parse it back, rather than copying it.
- `vectors/README.md` documents the key inputs for the first time and says what
  the text form is: a rendering like `cid_text`, not a byte dump.

**Outcome:** the identifier every transport endpoint needs exists, is
conformance-tested in two implementations, and pastes into `did:key` tooling by
changing alphabets. `docs/design/2026-08-18-transport-design.md` §4.1's
`{author}` placeholder now names something real.

## Notes

Source: `docs/design/2026-08-18-transport-design.md` §5, Q1 — the one of the
eight open questions that was ratified rather than filed open. Q2–Q8 are
`todos/069` through `todos/075`.
