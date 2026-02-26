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

For private blocks (where `refs`, `ts`, and `ops` are encrypted into the `enc` field):

```cddl
signing-input-private = {
  "v"     => uint,
  "type"  => "private",
  "pub"   => bstr .size 32,
  "prev"  => bstr .size 32 / null,
  "enc"   => bstr,             ; ciphertext of refs + ts + ops
  "nonce" => bstr .size 24     ; 192-bit XChaCha20 nonce
}
```

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

The Ed25519-to-X25519 conversion MUST follow the birational map specified in RFC 7748 S4.1. Reference implementations include libsodium's `crypto_sign_ed25519_pk_to_curve25519` and `crypto_sign_ed25519_sk_to_curve25519`.

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
wrapped_key = Encrypt(wrapping_key, chain_symmetric_key)
```

The mechanism for distributing wrapped keys to recipients is out of scope (implementation-specific). The protocol only defines the encryption of block content.

**Default use case:** A user's private chain is encrypted with a symmetric key known only to the user's own devices. The key is shared between devices through an out-of-band mechanism (e.g., QR code, manual transfer, Tailscale, etc.).

### Key rotation

Key rotation is an L1 block-level operation, not a meta-molecule. See [02-block-format.md](02-block-format.md) for the `rotate_key` operation type and rotation block structure.

When an author publishes a rotation block containing a `rotate_key` operation with the new public key bytes, the current chain ends. The new key begins a fresh chain. Implementations MUST mark the old key as inactive and MUST NOT accept further blocks signed by it.

## Security Considerations

- **Ed25519 security level:** ~128-bit security. Sufficient for the foreseeable future.
- **Nonce reuse:** Reusing a nonce with XChaCha20-Poly1305 under the same key completely breaks confidentiality. Implementations MUST generate unique nonces for every private block. Using a counter or random 24-byte value are both acceptable. XChaCha20's 192-bit nonce space makes random nonce collisions negligible.
- **Metadata leakage:** Private blocks encrypt `refs`, `ts`, and `ops` together. An observer can see that a private block exists and its position in the chain (`prev`), but cannot learn the block's timestamp, what other blocks it references, or what operations it contains. This prevents social graph analysis via refs and timing correlation via timestamps.
- **Key compromise:** If an author's Ed25519 private key is compromised, the attacker can sign blocks and publish fraudulent key rotations. v1 of the protocol does not include pre-rotation or social recovery. Key compromise handling is deferred to a future protocol version. See the [brainstorm document](../docs/brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md) for candidate approaches.
- **Ed25519 to X25519 conversion:** This is a well-established operation (used by libsodium and others). The security properties are preserved. See [RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) for X25519 and [this analysis](https://moderncrypto.org/mail-archive/curves/2014/000205.html) for the conversion.

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
                "prev": <32 bytes or null>})

4. Encrypt:
   ciphertext = XChaCha20Poly1305_Encrypt(chain_key, nonce, plaintext, aad)

5. Construct private block:
   {
     "v":     1,
     "type":  "private",
     "pub":   <32 bytes>,
     "sig":   <64 bytes>,
     "prev":  <32 bytes or null>,
     "enc":   <ciphertext bytes>,
     "nonce": <24 bytes>
   }
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
