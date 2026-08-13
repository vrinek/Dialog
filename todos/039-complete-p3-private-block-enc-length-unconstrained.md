---
status: complete
priority: p3
issue_id: "039"
tags: [specification-gap, block-format, cryptography, cddl]
dependencies: ["010"]
---

# A Private Block's enc Field Has No Length Constraint

## Problem Statement

`spec/02-block-format.md` defines a private block's ciphertext field as a bare
`bstr`:

```cddl
"enc"   => bstr,             ; encrypted payload (refs + ts + ops)
"nonce" => bstr .size 24     ; 192-bit XChaCha20 nonce
```

The nonce got its size constraint from issue #10; `enc` did not. A private
block whose `enc` is zero bytes long, or one byte long, is therefore
well-formed: it validates against the CDDL, its signature verifies, it links
into the chain, and a non-recipient node has no reason to refuse it. But it
cannot be a XChaCha20-Poly1305 ciphertext, whose minimum length is the 16-byte
Poly1305 tag, and the smallest possible *plaintext* is a three-key dCBOR map,
so a real `enc` is comfortably above 30 bytes.

The gap matters because private blocks are exactly the blocks that
non-recipients validate structurally and store without ever decrypting. A
sender can fill a chain with blocks that no recipient can ever open, and every
node will keep them, because nothing in the format says they are malformed.

## Findings

- `spec/02-block-format.md`, "Private block": `"enc" => bstr`.
- `spec/04-cryptography.md`, "Signature input": the same bare `bstr` in
  `signing-input-private`.
- `spec/04-cryptography.md`, "Encryption procedure": the ciphertext is
  `XChaCha20Poly1305_Encrypt(...)`, which always appends a 16-byte
  authentication tag, so `len(enc) >= 16` is implied by the algorithm and
  stated nowhere.
- The plaintext is `dCBOR({"refs": ..., "ts": ..., "ops": ...})`, whose
  smallest legal encoding is well above zero bytes, so a tighter floor than 16
  is derivable if the specification wants one.
- `todos/010` fixed the same class of omission for `nonce` and did not touch
  `enc`.

## Proposed Solutions

### Option 1: Constrain enc to the algorithm's minimum (Recommended)

- Change both CDDL definitions to `"enc" => bstr .size (16..)` and add a
  sentence: "The ciphertext MUST be at least 16 bytes, the size of the
  Poly1305 authentication tag; a shorter value cannot be the output of the
  AEAD and MUST be rejected."
- **Pros**: catches the malformed case at the structural layer, where
  non-recipients validate, rather than at a decryption that never happens.
- **Cons**: the bound is the algorithm's, so a future AEAD would revisit it.
- **Effort**: Trivial
- **Risk**: Low

### Option 2: Derive a tighter floor from the plaintext

- Compute the smallest legal dCBOR payload and state that bound instead.
- **Pros**: rejects a little more garbage.
- **Cons**: the number has to be recomputed whenever the payload shape changes,
  and it buys little over option 1.
- **Risk**: Low

### Option 3: Leave it unconstrained

- **Cons**: keeps a class of undecryptable blocks structurally valid forever.
- **Risk**: Medium

## Recommended Action

Option 1, in both `spec/02-block-format.md` and `spec/04-cryptography.md`, in
the same edit that issue #10 made for the nonce.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("Private block" CDDL),
  `spec/04-cryptography.md` ("Signature input" CDDL)
- **Related Components**: private block validation, the future privacy package
- **Database Changes**: No

## Acceptance Criteria

- [x] Both CDDL definitions constrain `enc`'s length
- [x] The prose says why the bound is what it is
- [x] `go/block` enforces the bound and its decoder table covers it

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

`Decode` accepts any `enc`, including an empty one, because that is what the
CDDL says and this package treats the field as opaque. The alternative —
inventing a minimum — would reject blocks another implementation accepts, so
the choice was to follow the text and record the gap here.

### 2026-08-13 - Ratified and Implemented
**By:** Claude

**Decision (project lead):** Option 1. `enc` MUST be at least 16 bytes — the
size of the Poly1305 authentication tag every XChaCha20-Poly1305 ciphertext
carries — and a shorter value is structurally invalid, rejected without any
attempt at decryption and whether or not the node holds the key. The tighter
plaintext-derived floor of option 2 is not taken: it would have to be
recomputed whenever the payload shape changes and buys little. The protocol
sets **no upper bound**; an implementation MAY impose a resource limit on the
size of a block it accepts or stores, but that is local policy and not part of
block validity, so a block one node declines on size grounds stays valid for
another.

**Changes:**

- `spec/02-block-format.md` § "Private block": the CDDL is
  `"enc" => bstr .size (16..)`, with prose giving the bound, its reason, and
  the note that this is a check every node can make on exactly the blocks most
  nodes never decrypt; an informative paragraph states the absence of an upper
  bound and the status of local size limits.
- `spec/04-cryptography.md` § "Signature input": `signing-input-private`
  carries the same constraint, with a pointer to the block format for the
  reason.
- `go/block/block.go`: new exported `MinEncSize = 16`; `Content.Validate`
  rejects a shorter ciphertext.
- `go/block/decode.go`: the comment that recorded the gap now states the
  constraint and where it is enforced.
- `go/block/builder.go`: `Private` no longer turns a nil `enc` into an empty
  one — an empty ciphertext is now invalid — and its doc comment gives both
  size requirements.
- `go/block/block_test.go`: new `TestPrivateBlockEncLowerBound` (0, 1 and 15
  bytes and nil are rejected; exactly 16 and 4096 are accepted and decode);
  new `testCiphertext` helper so every test's stand-in ciphertext is a legal
  length. `go/block/decode_test.go`: the rejection table gains an empty `enc`
  and a 15-byte one.

## Notes

Source: Go reference implementation, phase 3 (block).
