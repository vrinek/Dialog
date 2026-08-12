# Encoding

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document specifies how Dialog data structures are serialized to bytes using deterministic CBOR, how content identifiers (CIDs) are computed, and the exact parameters for all encoding operations. Implementations MUST follow these rules exactly to achieve interoperable content addressing.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

## Overview

Dialog uses three encoding technologies:

1. **CBOR** ([RFC 8949](https://datatracker.ietf.org/doc/html/rfc8949)) for serialization
2. **Multihash** ([multiformats/multihash](https://github.com/multiformats/multihash)) for self-describing hashes
3. **CID** ([multiformats/cid](https://github.com/multiformats/cid)) for content identifiers

All three use fixed parameters with no implementor choice. Two conforming implementations encoding the same logical structure MUST produce identical bytes.

## Specification

### Deterministic CBOR

All CBOR encoding in Dialog MUST conform to the dCBOR application profile as specified in [draft-mcnally-deterministic-cbor](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/).

In summary, the following rules apply:

1. **Shortest integer encoding.** Integers MUST use the shortest possible encoding (major type 0 or 1 with the smallest additional information value).
2. **Sorted map keys.** Map keys MUST be sorted in bytewise lexicographic order of their CBOR encoding.
3. **No duplicate map keys.** Each map key MUST appear at most once.
4. **No indefinite-length items.** All arrays, maps, byte strings, and text strings MUST use definite-length encoding.
5. **No floating-point values.** Dialog CBOR documents MUST NOT contain floating-point values (major type 7, additional information 25-27). All numbers MUST be integers. Scalar values requiring decimal precision MUST use a fixed-point representation (integer value with an implicit or explicit scale factor).
6. **No tags, with one exception.** CBOR tags (major type 6) MUST NOT be used, except tag 4 (decimal fraction) where [01-data-model.md](01-data-model.md) requires it for scalar filler values. Implementations MUST reject every other tag.
7. **Null is allowed.** The CBOR null value (`0xf6`) MAY be used where the specification permits null (e.g., the `prev` field of a genesis block).

#### Decimal fractions

Tag 4 is the only tag permitted in a Dialog document (rule 6). Its deterministic encoding is:

```
c4                      ; tag 4 (decimal fraction)
  82                    ; definite-length array of exactly 2 elements
    <exponent>          ; shortest-form major type 0 or 1 integer
    <mantissa>          ; shortest-form major type 0 or 1 integer
```

The array MUST have definite length and exactly two elements, exponent first. Neither element may itself be a tag, a float, or any type other than a major type 0 or 1 integer, and each MUST use the shortest-form encoding of rule 1. The value denoted is `mantissa × 10^exponent`.

Because a CID is a hash of the encoded bytes, every value MUST have exactly one representation. The following canonicalization rules make the representation unique:

1. Tag 4 MUST be used only for values that are not representable as a CBOR integer — that is, the exponent MUST be negative. Whole numbers MUST be encoded as plain integers (major type 0 or 1), never as a decimal fraction.
2. The mantissa MUST NOT be divisible by 10: trailing decimal zeros MUST be stripped from the mantissa and absorbed into the exponent (`[-2, 3140]` is invalid; the same value is `[-1, 314]`). The mantissa MUST NOT be zero, since zero is a whole number and rule 1 requires it to be encoded as the integer `0`.
3. Decoders MUST reject a tag 4 value that violates these rules as non-canonical, exactly as they reject a non-shortest integer encoding.

### Content identifiers (CIDs)

Content identifiers in Dialog use CIDv1 with the following fixed parameters:

| Parameter | Value | Multicodec |
|-----------|-------|------------|
| CID version | 1 | — |
| Content codec | dag-cbor | `0x71` |
| Hash function | SHA-256 | `0x12` |
| Digest length | 32 bytes | `0x20` |

Implementations MUST reject CIDs that use different parameters.

A CID is constructed as:

```
CID = varint(1) || varint(0x71) || varint(0x12) || varint(0x20) || digest
```

Where `||` denotes concatenation and `varint` is an unsigned variable-length integer as specified by [multiformats/unsigned-varint](https://github.com/multiformats/unsigned-varint). For the values used in Dialog (all < 128), varints are single bytes:

```
CID = 0x01 || 0x71 || 0x12 || 0x20 || <32 bytes SHA-256 digest>
```

Total CID size: **36 bytes** (4 prefix bytes + 32 digest bytes).

### Computing an entity's CID

To compute the CID of any Dialog entity (atom, bond, molecule, or block):

1. Encode the entity as a CBOR map following dCBOR rules
2. Compute the SHA-256 hash of the resulting bytes
3. Construct the CID from the hash digest

```
CID(entity) = 0x01 || 0x71 || 0x12 || 0x20 || SHA-256(dCBOR(entity))
```

### Internal references

Within Dialog data structures (molecules referencing atoms, blocks referencing previous blocks), references use the **raw SHA-256 digest** (32 bytes), not the full CID. This avoids the 4-byte overhead per reference, since the CID parameters are fixed protocol-wide and carry no additional information.

Every reference value carried inside a Dialog CBOR structure is a 32-byte digest, encoded as a CBOR byte string (`5820` followed by the 32 digest bytes). This includes:

- The `prev` field of a block (or `null` for a genesis block)
- Each entry in a block's `refs` list
- The `bond` field of a molecule and of a `create_molecule` operation
- Filler values of type 0 (atom), 1 (bond), and 2 (molecule)

IPFS URI fillers (type 3) are not internal references: they carry an IPFS content identifier as a text string, whose format is defined by IPFS and is out of scope for this rule.

The full 36-byte CID is used only for external references — communicating entity identifiers between systems, APIs, logs, and other human-readable contexts. It never appears in the fields listed above; the `bstr .size 32` constraint in each CDDL definition excludes it.

### Multihash format

When a full multihash is needed (e.g., in CID construction), it follows the multihash specification:

```
multihash = varint(hash-function-code) || varint(digest-length) || digest
         = 0x12 || 0x20 || <32 bytes>
```

Total multihash size: **34 bytes** (2 prefix bytes + 32 digest bytes).

## Security Considerations

- Deterministic encoding is critical for content addressing. Any deviation from dCBOR rules will produce different hashes for the same logical content, breaking interoperability.
- SHA-256 provides 128-bit collision resistance, which is sufficient for the foreseeable future.
- Locking down CID parameters to a single configuration eliminates a class of interoperability bugs where implementations use different hash functions or codecs.
- If SHA-256 is ever broken, the multihash and CID formats allow migration to a new hash function by changing the hash function code. This would require a new protocol version.

## Examples

### Encoding an atom

```
Input:     {"description": "France"}

Step 1 — dCBOR encode:
  Map (1 item):
    key:   "description" (tstr, 11 bytes)
    value: "France" (tstr, 6 bytes)
  CBOR hex: a16b6465736372697074696f6e664672616e6365

Step 2 — SHA-256:
  e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842

Step 3 — CID:
  01 71 12 20 e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b047
  5512b842
```

### Encoding a decimal fraction

The scalar value `3.14`, as it appears in a molecule's filler:

```
Logical:   3.14 = 314 × 10^-2  →  #6.4([-2, 314])

dCBOR encode:
  c4         ; tag 4 (decimal fraction)
  82         ; array of 2 elements
  21         ; -2   (major type 1, shortest form)
  19 013a    ; 314  (major type 0, two-byte argument)

CBOR hex:  c48221 19013a
```

The mantissa 314 is not divisible by 10 and the exponent is negative, so this is the canonical form. `#6.4([-1, 31])` denotes a different value (3.1); `#6.4([-3, 3140])` denotes the same value and is therefore **invalid** — decoders MUST reject it. The whole number `3` is encoded as the integer `03`, never as `#6.4([0, 3])`.

### CBOR encoding reference

Common CBOR patterns used in Dialog:

| Pattern | CBOR hex | Notes |
|---------|----------|-------|
| Map with 1 key | `a1` | Major type 5, additional info 1 |
| Map with 2 keys | `a2` | Major type 5, additional info 2 |
| Short text string (< 24 bytes) | `6n` + bytes | Major type 3, length n |
| Longer text string (24-255 bytes) | `78 nn` + bytes | Major type 3, one-byte length |
| 32-byte byte string | `5820` + bytes | Major type 2, length 32 |
| Null | `f6` | Simple value 22 |
| Integer 0 | `00` | Major type 0, value 0 |
| Integer 1 | `01` | Major type 0, value 1 |
| Empty array | `80` | Major type 4, length 0 |
| Decimal fraction | `c482` + exponent + mantissa | Tag 4, 2-element array `[exponent, mantissa]` |

## References

### Normative
- [RFC 8949: CBOR](https://datatracker.ietf.org/doc/html/rfc8949) — Concise Binary Object Representation
- [draft-mcnally-deterministic-cbor](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/) — Deterministic CBOR application profile
- [multiformats/cid](https://github.com/multiformats/cid) — Content Identifier specification
- [multiformats/multihash](https://github.com/multiformats/multihash) — Self-describing hash specification
- [multiformats/unsigned-varint](https://github.com/multiformats/unsigned-varint) — Unsigned variable-length integer encoding
- [RFC 8610: CDDL](https://datatracker.ietf.org/doc/html/rfc8610) — Concise Data Definition Language

### Informative
- [cbor.io](https://cbor.io/) — CBOR tools and implementations
