# Cryptography

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document specifies the cryptographic operations in Dialog: Ed25519 signatures for block signing, X25519 key agreement for private block encryption, and the exact procedures for each operation.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

## Overview

Dialog uses two cryptographic schemes:

1. **Ed25519** ([RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032)) for block signatures — every block is signed by its author
2. **X25519 + XChaCha20-Poly1305** for private block encryption — `refs`, `ts`, and `ops` are encrypted; only chain management fields stay plaintext

Both schemes use Curve25519 keys. An author's Ed25519 signing key can be converted to an X25519 key agreement key, so a single keypair serves both purposes.

## Specification

### Author identity

An author is identified by their Ed25519 public key — a 32-byte value. This key is the author's identity. There is no separate identity layer.

Profile information (name, avatar, etc.) is expressed as molecules in the author's chain that describe the key.

### Key encoding

| Key type | Encoding | Size |
|----------|----------|------|
| Ed25519 public key | Raw bytes | 32 bytes |
| Ed25519 signature | Raw bytes | 64 bytes |
| X25519 public key | Raw bytes (converted from Ed25519) | 32 bytes |

Keys are stored as raw byte strings (`bstr`) in CBOR. No multicodec prefix is used within the protocol's internal structures. When keys need to be communicated externally (e.g., displayed to users, shared out-of-band), implementations MAY use multicodec-prefixed representations.

### Block signing

#### Signature input

The signature covers all block fields **except** the signature itself. To compute the signature:

1. Construct the block as a CBOR map with all fields except `sig`
2. Encode the map as dCBOR
3. Sign the resulting bytes with the author's Ed25519 private key

For public and rotation blocks:

```cddl
signing-input-public = {
  "v"    => uint,
  "type" => tstr,              ; "public" or "rotation"
  "pub"  => bstr .size 32,
  "prev" => bstr .size 32 / null,
  "refs" => [* bstr .size 32],
  "ts"   => uint,
  "ops"  => [+ operation]
}
```

The two block types share one signing input, so the `prev` field is written here as the union both admit. A rotation block's `prev` is in fact never null: a rotation block is never a genesis block (see [02-block-format.md](02-block-format.md), "Rotation block").

For private blocks (where `refs`, `ts`, and `ops` are encrypted into the `enc` field):

```cddl
signing-input-private = {
  "v"     => uint,
  "type"  => "private",
  "pub"   => bstr .size 32,
  "prev"  => bstr .size 32 / null,
  "enc"   => bstr .size (16..), ; ciphertext of refs + ts + ops
  "nonce" => bstr .size 24      ; 192-bit XChaCha20 nonce
}
```

The lower bound on `enc` is the 16-byte Poly1305 authentication tag every XChaCha20-Poly1305 ciphertext carries; see [02-block-format.md](02-block-format.md), "Private block".

#### Signing procedure

```
signing_bytes = dCBOR(block without "sig" field)
signing_input = "dialog-v1-block" || signing_bytes
signature = Ed25519_Sign(private_key, signing_input)
```

The signing input is prefixed with the domain separator string `"dialog-v1-block"` (15 bytes, UTF-8 encoded) to prevent cross-protocol signature replay attacks.

The complete block is then: all the signing input fields plus `"sig" => signature`.

#### Verification procedure

```
signing_bytes = dCBOR(received block without "sig" field)
signing_input = "dialog-v1-block" || signing_bytes
valid = Ed25519_Verify(block["pub"], signing_input, block["sig"])
```

Implementations MUST reject blocks where verification fails.

### Private block encryption

Private blocks encrypt `refs`, `ts`, and `ops` together into a single `enc` field. Only chain management fields (`v`, `type`, `pub`, `sig`, `prev`) remain in plaintext. This prevents metadata leakage of timing information and social graph (via refs) to non-recipient nodes.

#### Encryption scheme

- **Algorithm:** XChaCha20-Poly1305 (AEAD)
- **Key:** 256-bit symmetric key (unique per chain, not per block)
- **Nonce:** 192-bit (24 bytes), unique per block, stored in the `nonce` field

#### Encryption procedure

```
plaintext = dCBOR({"refs": block.refs, "ts": block.ts, "ops": block.ops})
aad = dCBOR({"v": block.v, "type": block.type, "pub": block.pub,
             "prev": block.prev})
ciphertext = XChaCha20Poly1305_Encrypt(symmetric_key, nonce, plaintext, aad)
```

The plaintext is the dCBOR encoding of a map containing the three encrypted fields: `refs` (foreign block references), `ts` (timestamp), and `ops` (operations list).

The Additional Authenticated Data (AAD) MUST be the deterministic CBOR encoding of a map containing all plaintext block fields (excluding `sig`, `enc`, and `nonce`): `v`, `type`, `pub`, and `prev`. The AAD binds the ciphertext to the block's metadata, preventing payload-swapping attacks. Since dCBOR mandates deterministic map key ordering, the AAD encoding is unambiguous.

The `enc` field of the private block contains the ciphertext. The `nonce` field contains the nonce used for encryption.

#### Decryption procedure

```
aad = dCBOR({"v": block.v, "type": block.type, "pub": block.pub,
             "prev": block.prev})
plaintext = XChaCha20Poly1305_Decrypt(symmetric_key, block["nonce"], block["enc"], aad)
payload = dCBOR_decode(plaintext)  ; yields {"refs": [...], "ts": uint, "ops": [...]}
```

The decrypted payload is a CBOR map with three fields: `refs`, `ts`, and `ops`.

If the AAD does not match during decryption (authentication tag verification fails), the block MUST be rejected. If decryption fails for any other reason (invalid key or tampered ciphertext), the block MUST also be rejected.

#### Key management

Each private chain uses a single symmetric key. This key is shared with authorized readers through per-recipient key wrapping:

1. Convert the author's Ed25519 private key to an X25519 private key
2. Convert each recipient's Ed25519 public key to an X25519 public key
3. Perform X25519 key agreement between the author and each recipient
4. Derive a wrapping key using HKDF-SHA-256 (RFC 5869) and encrypt the symmetric chain key

The two conversions `Ed25519_to_X25519` stands for are specified in [Ed25519-to-X25519 conversion](#ed25519-to-x25519-conversion), below the procedure they are used in.

```
author_x25519_sk = Ed25519_to_X25519(author_ed25519_sk)
recipient_x25519_pk = Ed25519_to_X25519(recipient_ed25519_pk)
shared_secret = X25519(author_x25519_sk, recipient_x25519_pk)
wrapping_key = HKDF-SHA-256(
  salt:   empty (zero-length byte string),
  ikm:    shared_secret,
  info:   "dialog-v1-key-wrap",
  length: 32 bytes
)
wrap_nonce  = random_bytes(24)
wrapped_key = wrap_nonce || XChaCha20Poly1305_Encrypt(
  wrapping_key, wrap_nonce, chain_symmetric_key, aad: empty
)
```

##### Ed25519-to-X25519 conversion

`Ed25519_to_X25519` is two different procedures — one for public keys, one for private keys — and both are specified here in full. They are not interchangeable and neither is derivable from the other: the public half maps a curve point, the private half derives a scalar.

Throughout, *p* is 2<sup>255</sup> − 19, the field of Curve25519, and all arithmetic is modulo *p*.

**Public keys.** An Ed25519 public key is the compressed encoding of an Edwards point ([RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032) §5.1.2): 32 bytes holding the *y* coordinate in little-endian order, with the most significant bit of the last byte carrying the sign of *x*. The conversion is the birational map of [RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) §4.1:

1. Clear the most significant bit of byte 31. It is the sign of *x* and takes no part in the map.
2. Read the remaining 255 bits as a little-endian integer *y*. Implementations MUST reject a key with *y* ≥ *p*: such an encoding is not canonical, and no Ed25519 public key produces one.
3. Implementations MUST reject *y* = 1. That is the identity point, 1 − *y* has no inverse, and its Montgomery image is the point at infinity, which has no *u* coordinate to encode.
4. Compute *u* = (1 + *y*) · (1 − *y*)<sup>−1</sup> mod *p*.
5. The X25519 public key is *u* encoded as 32 bytes, little-endian ([RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) §5).

Only *y* enters the map, so recovering *x* is not required.

A public key of small order maps to a *u* of small order, and the X25519 agreement with it yields all zeroes. Implementations MUST reject an all-zero agreement result ([RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) §6.1) and MUST NOT derive a wrapping key from it. An implementation MAY instead reject small-order encodings earlier, at conversion time — libsodium does — which changes where the failure is reported but never whether one occurs: no wrapped key is produced or opened under a small-order key either way.

**Private keys.** An Ed25519 private key is a 32-byte seed, not a scalar. The X25519 private key is the scalar that seed expands to ([RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032) §5.1.5):

1. Compute *h* = SHA-512(seed).
2. Take *s* = *h*[0..31], the lower half.
3. Clamp it: `s[0] &= 248`, `s[31] &= 127`, `s[31] |= 64`.
4. *s* is the X25519 private key, and its X25519 public key is the birational image of the seed's Ed25519 public key.

Implementations that hold an Ed25519 private key in the 64-byte form many libraries use — seed followed by public key — take the seed from its first 32 bytes. Using the seed itself as an X25519 scalar is **not** the conversion: it produces a valid X25519 key that agrees with nobody.

*Informative.* These are exactly libsodium's `crypto_sign_ed25519_pk_to_curve25519` and `crypto_sign_ed25519_sk_to_curve25519`, and produce identical output for every valid Ed25519 key. They differ only in which invalid inputs they turn away: libsodium's public conversion rejects small-order and off-subgroup encodings but accepts a non-canonical *y*, so an implementation built on it performs step 2's canonicality check itself.

A worked example of both conversions, with all intermediate values, is in [Wrapping a chain key](#wrapping-a-chain-key).

##### Wrapped key format

The wrap uses XChaCha20-Poly1305, the same AEAD as block encryption, with an empty AAD. A wrapped key is the concatenation of the nonce and the AEAD output, and is therefore always 72 bytes:

| Offset | Size | Content |
|--------|------|---------|
| 0 | 24 | `wrap_nonce`, the XChaCha20 nonce |
| 24 | 32 | Ciphertext of the 32-byte chain symmetric key |
| 56 | 16 | Poly1305 authentication tag |
| | **72** | **Total** |

Implementations MUST reject a wrapped key of any other length without attempting to decrypt it: the plaintext is a fixed-size key, so every conforming wrap has exactly this size, and any other length is a malformed or truncated value.

`wrap_nonce` MUST be freshly generated for every wrap, from a cryptographically secure random source. The requirement is stronger here than for block encryption. A wrapping key is a pure function of one pair of identities and the constant info string, so it is the same key for every wrap between those two parties, for every chain and for all time; there is no per-chain or per-message input to separate two wraps. Reusing a nonce would therefore reuse a keystream across wraps under a long-lived key. XChaCha20's 192-bit nonce space makes a random collision negligible, which is why a random nonce is sufficient and no counter state has to be kept.

The AAD is empty. Nothing needs to be bound to the ciphertext by the AEAD because the wrapping key already binds it: the key is derived from the X25519 agreement between exactly one author and one recipient, through HKDF with the info string `"dialog-v1-key-wrap"`. Only those two parties can derive it, and it is derived for no other purpose, so a wrapped key that authenticates under it necessarily came from the other end of that pair. An implementation MUST NOT include additional authenticated data: a non-empty AAD produces bytes no conforming recipient can open.

The mechanism for distributing wrapped keys to recipients is out of scope (implementation-specific), and so is any envelope that carries them. A wrapped key names neither the recipient it was made for nor the chain whose key it holds; a reader handed several of them either tracks the association itself or tries each in turn, which the 16-byte tag makes a certain and cheap test. The protocol only defines the encryption of block content.

**Default use case:** A user's private chain is encrypted with a symmetric key known only to the user's own devices. The key is shared between devices through an out-of-band mechanism (e.g., QR code, manual transfer, Tailscale, etc.).

### Key rotation

Key rotation is an L1 block-level operation, not a meta-molecule. See [02-block-format.md](02-block-format.md) for the `rotate_key` operation type and rotation block structure.

When an author publishes a rotation block containing a `rotate_key` operation with the new public key bytes, the current chain ends. The new key begins a fresh chain. Implementations MUST mark the old key as inactive and MUST NOT accept further blocks signed by it.

## Security Considerations

- **Ed25519 security level:** ~128-bit security. Sufficient for the foreseeable future.
- **Nonce reuse:** Reusing a nonce with XChaCha20-Poly1305 under the same key completely breaks confidentiality. Implementations MUST generate unique nonces for every private block. Using a counter or random 24-byte value are both acceptable. XChaCha20's 192-bit nonce space makes random nonce collisions negligible. The same requirement applies to key wrapping and binds harder there: a wrapping key is long-lived by construction — one pair of identities has one wrapping key, for every chain and for all time — so a repeated `wrap_nonce` repeats a keystream under a key that may have been in use for years. See "Wrapped key format".
- **Metadata leakage:** Private blocks encrypt `refs`, `ts`, and `ops` together. An observer can see that a private block exists and its position in the chain (`prev`), but cannot learn the block's timestamp, what other blocks it references, or what operations it contains. This prevents social graph analysis via refs and timing correlation via timestamps.
- **Key compromise:** If an author's Ed25519 private key is compromised, the attacker can sign blocks and publish fraudulent key rotations. v1 of the protocol does not include pre-rotation or social recovery. Key compromise handling is deferred to a future protocol version. See the [brainstorm document](../docs/brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md) for candidate approaches.
- **Ed25519 to X25519 conversion:** This is a well-established operation (used by libsodium and others). The security properties are preserved. The two procedures are specified in full under "Ed25519-to-X25519 conversion"; the libsodium functions named there are informative, and an implementation is conformant by matching the specified steps, not a particular library. See [RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) for X25519 and [this analysis](https://moderncrypto.org/mail-archive/curves/2014/000205.html) for the conversion.

## Examples

### Signing a public block

```
1. Construct the block without signature:
   {
     "v":    1,
     "type": "public",
     "pub":  <32 bytes>,
     "prev": null,
     "refs": [],
     "ts":   1740067200,
     "ops":  [{"op": "create_atom", "description": "France"}]
   }

2. Encode as dCBOR:
   signing_bytes = <resulting bytes>

3. Prepend domain separator and sign:
   signing_input = "dialog-v1-block" || signing_bytes
   signature = Ed25519_Sign(private_key, signing_input)

4. Complete block:
   {
     "v":    1,
     "type": "public",
     "pub":  <32 bytes>,
     "sig":  <64 bytes>,
     "prev": null,
     "refs": [],
     "ts":   1740067200,
     "ops":  [{"op": "create_atom", "description": "France"}]
   }
```

### Encrypting a private block

```
1. Encode the encrypted payload (refs + ts + ops) as dCBOR:
   plaintext = dCBOR({
     "refs": [],
     "ts":   1740067200,
     "ops":  [{"op": "create_atom", "description": "My private note"}]
   })

2. Generate a unique 24-byte nonce:
   nonce = random_bytes(24)

3. Compute AAD from plaintext block fields:
   aad = dCBOR({"v": 1, "type": "private", "pub": <32 bytes>,
                "prev": <digest of previous block, or null>})

4. Encrypt:
   ciphertext = XChaCha20Poly1305_Encrypt(chain_key, nonce, plaintext, aad)

5. Construct private block:
   {
     "v":     1,
     "type":  "private",
     "pub":   <32 bytes>,
     "sig":   <64 bytes>,
     "prev":  <digest of previous block, or null>,
     "enc":   <ciphertext bytes>,
     "nonce": <24 bytes>
   }
```

### Wrapping a chain key

A worked example with every intermediate value, so that an implementation can be checked step by step. The Ed25519 seeds and the wrap nonce are fixed to make the output reproducible; a real wrap MUST take a fresh random nonce.

```
Author Ed25519 seed        0101...01  (32 bytes of 0x01)
Author Ed25519 public key  8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c
Author X25519 private key  58e86efb75fa4e2c410f46e16de9f6acae1a1703528651b69bc176c088bef36e
Author X25519 public key   1b1b58dd50ea14b60da17b790cd02754d970c9bab864ebb3c0f3016fe51d3f57

Reader Ed25519 seed        0202...02  (32 bytes of 0x02)
Reader Ed25519 public key  8139770ea87d175f56a35466c34c7ecccb8d8a91b4ee37a25df60f5b8fc9b394
Reader X25519 private key  a83c626bc9c38c8c201878ebb1d5b0b50ac40e8986c78793db1d4ef369fca14e
Reader X25519 public key   60346e7c911a5f6ba154129174cafe75b294ac3bbd5549632f48cec6266f8410

shared_secret  = X25519(author X25519 sk, reader X25519 pk)
               = 4181d7302557342bdb6d061c4b1eebea828ecb625c3368b7111680793307220b
                 (the reader derives the same value from their own private key
                  and the author's public key)

wrapping_key   = HKDF-SHA-256(salt: empty, ikm: shared_secret,
                              info: "dialog-v1-key-wrap", length: 32)
               = 657dbd5e5d21dcb81a44415ddf3a8b9f9fa44c7d832d678c9962079aa01fe68d

chain key      = 1111...11  (32 bytes of 0x11)
wrap_nonce     = 2222...22  (24 bytes of 0x22)

wrapped_key    = wrap_nonce || XChaCha20Poly1305_Encrypt(
                   wrapping_key, wrap_nonce, chain key, aad: empty)
               = 222222222222222222222222222222222222222222222222
                 bdafc49fb94819665a9993f60272336caf98fd5fd4fb1b302e94cc2b5a8ccbd6
                 1b25f1def2b48d225f13e64d5f0ffa90
                 (72 bytes: 24 nonce, 32 ciphertext, 16 tag)
```

## References

### Normative
- [RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032) — Edwards-Curve Digital Signature Algorithm (Ed25519)
- [RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) — Elliptic Curves for Security (X25519)
- [RFC 5869](https://datatracker.ietf.org/doc/html/rfc5869) — HMAC-based Extract-and-Expand Key Derivation Function (HKDF)
- [draft-irtf-cfrg-xchacha](https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-xchacha-03) — XChaCha20-Poly1305
- [03-encoding.md](03-encoding.md) — dCBOR encoding rules

### Informative
- [02-block-format.md](02-block-format.md) — Block format and rotation block structure
- [libsodium](https://doc.libsodium.org/) — Reference implementation for Ed25519, X25519, and XChaCha20-Poly1305
