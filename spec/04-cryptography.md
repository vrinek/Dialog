# Cryptography

**Version:** 1.0 (2026-02-20) | **Status:** Draft

## Abstract

This document specifies the cryptographic operations in Dialog: Ed25519 signatures for block signing, X25519 key agreement for private block encryption, and the exact procedures for each operation.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

## Overview

Dialog uses two cryptographic schemes:

1. **Ed25519** ([RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032)) for block signatures — every block is signed by its author
2. **X25519 + XChaCha20-Poly1305** for private block encryption — operations are encrypted, metadata stays plaintext

Both schemes use Curve25519 keys. An author's Ed25519 signing key can be converted to an X25519 key agreement key, so a single keypair serves both purposes.

## Specification

### Author identity

An author is identified by their Ed25519 public key — a 32-byte value. This key is the author's identity. There is no separate identity layer.

Profile information (name, avatar, etc.) is expressed as molecules in the author's chain that describe the key. See [06-meta-bonds.md](06-meta-bonds.md) for the key rotation meta-bond.

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

```cddl
; The signing input is the block without the "sig" field
signing-input = {
  "v"    => uint,
  "pub"  => bstr .size 32,
  "prev" => bstr .size 32 / null,
  "refs" => [* bstr .size 32],
  "ts"   => uint,
  "ops"  => [+ operation] / bstr  ; plaintext or encrypted
  ? "nonce" => bstr               ; present for private blocks
}
```

#### Signing procedure

```
signing_input = dCBOR(block without "sig" field)
signature = Ed25519_Sign(private_key, signing_input)
```

The complete block is then: all the signing input fields plus `"sig" => signature`.

#### Verification procedure

```
signing_input = dCBOR(received block without "sig" field)
valid = Ed25519_Verify(block["pub"], signing_input, block["sig"])
```

Implementations MUST reject blocks where verification fails.

### Private block encryption

Private blocks encrypt the operations list while keeping all metadata in plaintext.

#### Encryption scheme

- **Algorithm:** XChaCha20-Poly1305 (AEAD)
- **Key:** 256-bit symmetric key (unique per chain, not per block)
- **Nonce:** 192-bit (24 bytes), unique per block, stored in the `nonce` field

#### Encryption procedure

```
plaintext = dCBOR(operations list)  ; the [+ operation] array
ciphertext = XChaCha20Poly1305_Encrypt(symmetric_key, nonce, plaintext)
```

The `ops` field of the private block contains the ciphertext. The `nonce` field contains the nonce used for encryption.

#### Decryption procedure

```
plaintext = XChaCha20Poly1305_Decrypt(symmetric_key, block["nonce"], block["ops"])
operations = dCBOR_decode(plaintext)  ; yields [+ operation]
```

If decryption fails (invalid key or tampered ciphertext), the block MUST be rejected.

#### Key management

Each private chain uses a single symmetric key. This key is shared with authorized readers through per-recipient key wrapping:

1. Convert the author's Ed25519 private key to an X25519 private key
2. Convert each recipient's Ed25519 public key to an X25519 public key
3. Perform X25519 key agreement between the author and each recipient
4. Use the shared secret (or a KDF derivative) to encrypt the symmetric chain key

```
author_x25519_sk = Ed25519_to_X25519(author_ed25519_sk)
recipient_x25519_pk = Ed25519_to_X25519(recipient_ed25519_pk)
shared_secret = X25519(author_x25519_sk, recipient_x25519_pk)
wrapped_key = Encrypt(KDF(shared_secret), chain_symmetric_key)
```

The mechanism for distributing wrapped keys to recipients is out of scope (implementation-specific). The protocol only defines the encryption of block content.

**Default use case:** A user's private chain is encrypted with a symmetric key known only to the user's own devices. The key is shared between devices through an out-of-band mechanism (e.g., QR code, manual transfer, Tailscale, etc.).

### Key rotation

Key rotation is expressed as a meta-molecule, not a cryptographic operation. See [06-meta-bonds.md](06-meta-bonds.md).

When an author publishes a `[old_key] rotates key to [new_key]` molecule in a block signed by the old key, subsequent blocks in the chain are signed by the new key. Implementations processing the chain MUST accept the new key for blocks after the rotation.

## Security Considerations

- **Ed25519 security level:** ~128-bit security. Sufficient for the foreseeable future.
- **Nonce reuse:** Reusing a nonce with XChaCha20-Poly1305 under the same key completely breaks confidentiality. Implementations MUST generate unique nonces for every private block. Using a counter or random 24-byte value are both acceptable. XChaCha20's 192-bit nonce space makes random nonce collisions negligible.
- **Key compromise:** If an author's Ed25519 private key is compromised, the attacker can sign blocks and publish fraudulent key rotations. v1 of the protocol does not include pre-rotation or social recovery. Key compromise handling is deferred to a future protocol version. See the [brainstorm document](../docs/brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md) for candidate approaches.
- **Ed25519 to X25519 conversion:** This is a well-established operation (used by libsodium and others). The security properties are preserved. See [RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) for X25519 and [this analysis](https://moderncrypto.org/mail-archive/curves/2014/000205.html) for the conversion.

## Examples

### Signing a public block

```
1. Construct the block without signature:
   {
     "v":    1,
     "pub":  <32 bytes>,
     "prev": null,
     "refs": [],
     "ts":   1740067200,
     "ops":  [{"op": "create_atom", "description": "France"}]
   }

2. Encode as dCBOR:
   <resulting bytes>

3. Sign:
   signature = Ed25519_Sign(private_key, <dCBOR bytes>)

4. Complete block:
   {
     "v":    1,
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
1. Encode operations as dCBOR:
   plaintext = dCBOR([{"op": "create_atom", "description": "My private note"}])

2. Generate a unique 24-byte nonce:
   nonce = random_bytes(24)

3. Encrypt:
   ciphertext = XChaCha20Poly1305_Encrypt(chain_key, nonce, plaintext)

4. Construct private block:
   {
     "v":     1,
     "pub":   <32 bytes>,
     "sig":   <64 bytes>,
     "prev":  <32 bytes or null>,
     "refs":  [],
     "ts":    1740067200,
     "ops":   <ciphertext bytes>,
     "nonce": <24 bytes>
   }
```

## References

### Normative
- [RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032) — Edwards-Curve Digital Signature Algorithm (Ed25519)
- [RFC 7748](https://datatracker.ietf.org/doc/html/rfc7748) — Elliptic Curves for Security (X25519)
- [draft-irtf-cfrg-xchacha](https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-xchacha-03) — XChaCha20-Poly1305
- [03-encoding.md](03-encoding.md) — dCBOR encoding rules

### Informative
- [06-meta-bonds.md](06-meta-bonds.md) — Key rotation via meta-molecules
- [libsodium](https://doc.libsodium.org/) — Reference implementation for Ed25519, X25519, and XChaCha20-Poly1305
