# Block Format

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document defines the structure of Dialog blocks, the four operation types, chain linking rules, and block validation. A block is the unit of data in Layer 1 — a signed, append-only container of ontology operations.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Author chain:** The linear sequence of blocks published by a single author, linked by `prev` digests.
- **Genesis block:** The first block in an author's chain (`prev` is null).
- **Foreign block reference:** A 32-byte digest pointing to a specific block in another author's chain that defines entities needed by the referencing block.
- **CID-providing block:** A block listed in `refs` whose operations define entities referenced by the current block.

## Overview

Dialog uses an IOTA-inspired blockchain model where each author maintains their own chain. Every block is signed by its author, references the previous block in the same chain, and optionally references blocks in other authors' chains. The signed-block chain provides two non-negotiable properties:

1. **Verifiable authorship ordering** — a tamper-proof sequence of what each author published
2. **Strict append-only semantics** — data can only be added, never mutated

## Specification

### Public block

```cddl
public-block = {
  "v"    => uint,              ; protocol version
  "type" => "public",          ; block type
  "pub"  => bstr .size 32,    ; author's Ed25519 public key
  "sig"  => bstr .size 64,    ; Ed25519 signature
  "prev" => bstr .size 32     ; SHA-256 digest of previous block
           / null,             ; null for genesis block
  "refs" => [* bstr .size 32], ; foreign block references (SHA-256 digests)
  "ts"   => uint,              ; Unix timestamp (seconds since epoch)
  "ops"  => [+ operation]      ; ordered list of operations
}
```

| Field | Type | Description |
|-------|------|-------------|
| `v` | `uint` | Protocol version. MUST be `1` for this specification. Implementations MUST reject blocks with an unrecognized version. |
| `type` | `tstr` | Block type. MUST be `"public"`, `"private"`, or `"rotation"`. |
| `pub` | `bstr .size 32` | Author's Ed25519 public key (raw 32 bytes). |
| `sig` | `bstr .size 64` | Ed25519 signature over the block content. See [04-cryptography.md](04-cryptography.md) for the signing procedure. |
| `prev` | `bstr .size 32 / null` | SHA-256 digest of the previous block in this author's chain. Used strictly for chain ordering (append-only semantics and fork detection), NOT for CID resolution. MUST be `null` for the genesis block. MUST NOT be `null` for any other block. |
| `refs` | `[* bstr .size 32]` | Zero or more SHA-256 digests of CID-providing blocks (blocks whose operations define entities needed by this block). MAY be empty. Public blocks MUST only reference public blocks. See [Validation](#validation). |
| `ts` | `uint` | Self-reported Unix timestamp. Untrusted — useful for ordering heuristics but not for validation. The `ts` field SHOULD be greater than or equal to the `ts` of the previous block in the same chain. Implementations SHOULD warn on non-monotonic timestamps. |
| `ops` | `[+ operation]` | Ordered list of one or more operations. A block MUST contain at least one operation. |

### Private block

A private block encrypts `refs`, `ts`, and `ops` together. Only chain management fields remain in plaintext, minimizing metadata leakage (timing information and social graph via refs are hidden from non-recipients).

```cddl
private-block = {
  "v"     => uint,
  "type"  => "private",
  "pub"   => bstr .size 32,
  "sig"   => bstr .size 64,
  "prev"  => bstr .size 32 / null,
  "enc"   => bstr,             ; encrypted payload (refs + ts + ops)
  "nonce" => bstr .size 24     ; 192-bit XChaCha20 nonce
}
```

The `enc` field contains the ciphertext of a CBOR map with three fields: `refs`, `ts`, and `ops`. When decrypted, this yields:

```cddl
private-block-payload = {
  "refs" => [* bstr .size 32], ; foreign block references
  "ts"   => uint,              ; Unix timestamp
  "ops"  => [+ operation]      ; ordered list of operations
}
```

**Plaintext fields** (`v`, `type`, `pub`, `sig`, `prev`) allow untrusted nodes to validate chain structure (append-only ordering, fork detection) without accessing encrypted content. **Encrypted fields** (`refs`, `ts`, `ops`) are only available to recipients holding the decryption key.

See [04-cryptography.md](04-cryptography.md) for the encryption scheme.

### Rotation block

A rotation block signals the end of the current key's chain. It MUST contain exactly one `rotate_key` operation and no other operations. The `new_pub` field contains the raw bytes of the new Ed25519 public key.

```cddl
rotation-block = {
  "v"    => uint,
  "type" => "rotation",
  "pub"  => bstr .size 32,
  "sig"  => bstr .size 64,
  "prev" => bstr .size 32 / null,
  "refs" => [* bstr .size 32],
  "ts"   => uint,
  "ops"  => [rotate-key-op]       ; exactly one operation
}

rotate-key-op = {
  "op"      => "rotate_key",
  "new_pub" => bstr .size 32      ; new Ed25519 public key (raw bytes)
}
```

### Validation dispatch

Implementations MUST check the `type` field to determine block structure:

- `"public"`: `ops` is a plaintext array, no `nonce` or `enc` field.
- `"private"`: `enc` field contains ciphertext (refs + ts + ops), `nonce` field required. No plaintext `ops`, `refs`, or `ts` fields.
- `"rotation"`: `ops` contains exactly one `rotate_key` operation.

### Operations

There are exactly four operation types:

```cddl
operation = create-atom / create-bond / create-molecule / rotate-key

create-atom = {
  "op"          => "create_atom",
  "description" => tstr
}

create-bond = {
  "op"       => "create_bond",
  "template" => tstr
}

create-molecule = {
  "op"      => "create_molecule",
  "bond"    => bstr .size 32,     ; SHA-256 digest of the bond
  "fillers" => [+ filler]         ; ordered list of fillers
}
```

The `filler` type is defined in [01-data-model.md](01-data-model.md).

Operations in a block are ordered. The order determines evaluation sequence for validation purposes (an operation later in the list may reference entities created by earlier operations in the same block).

#### create_atom

Creates an atom. The atom's identifier is `SHA-256(dCBOR({"description": <description>}))`.

#### create_bond

Creates a bond. The bond's identifier is `SHA-256(dCBOR({"template": <template>}))`.

#### create_molecule

Creates a molecule. The molecule's identifier is `SHA-256(dCBOR({"bond": <bond_digest>, "fillers": <fillers>}))`.

The `bond` field and any atom/bond/molecule references in `fillers` are 32-byte digests (see [03-encoding.md](03-encoding.md), "Internal references") and MUST refer to entities that are **reachable** from this block (see Validation below). The number of fillers MUST equal the number of variables in the referenced bond template (see [01-data-model.md](01-data-model.md)).

#### rotate_key

Rotates the author's key. The rotation block is the last block in the current key's chain. A new chain begins with a genesis block signed by the new key. The new key's genesis block SHOULD reference the rotation block via `refs` to establish verifiable key succession.

Implementations MUST mark the old key as inactive after processing a rotation block. Implementations MUST NOT accept further blocks signed by the old key after the rotation block.

### Chain linking

Each author maintains a single linear chain of blocks:

```
genesis → block_1 → block_2 → ... → block_n (tip)
```

The `prev` field links each block to its predecessor. This forms a singly-linked list from tip to genesis. The `prev` link serves strictly for chain ordering — establishing append-only semantics and enabling fork detection. It is NOT used for CID resolution.

Each author chain MUST be strictly linear: each block (except the tip) has at most one successor.

Foreign block references (`refs`) create cross-chain links, forming a DAG (directed acyclic graph) across all authors' chains. A foreign reference means: "this block's operations reference entities defined in the referenced block." Each entry in `refs` points to a specific CID-providing block — the block where the needed entity was created. See [05-processing-model.md](05-processing-model.md) for the demand-driven resolution procedure.

### Validation

A block is **valid** if and only if:

1. **Version check.** The `v` field is a recognized protocol version.
2. **Signature check.** The `sig` field is a valid Ed25519 signature over the block content, verified against the `pub` key. See [04-cryptography.md](04-cryptography.md).
3. **Chain integrity.** If `prev` is not null, it MUST reference an existing, valid block with the same `pub` key. Within a single chain, all blocks MUST have the same `pub` field. A chain ends when a rotation block is published; the new key begins a separate chain.
4. **Operation validity.** Every operation in `ops` MUST reference only entity digests that are **reachable** — defined in:
   - The same block (an earlier operation in the `ops` list), or
   - Any ancestor block in the author's own chain (reachable via `prev`), or
   - Any CID-providing block listed in `refs`, or transitively through that block's own `refs` (demand-driven recursive resolution; see [05-processing-model.md](05-processing-model.md))
5. **Data model conformance.** Every `create_molecule` operation MUST satisfy the data model rules in [01-data-model.md](01-data-model.md). In particular, the number of fillers MUST equal the number of variables in the referenced bond template.
6. **Public/private reference rules.** Public blocks MUST only reference public blocks in their `refs` field. Private blocks MAY reference either public or private blocks.
7. **Non-empty operations.** The `ops` list MUST contain at least one operation.
8. **Deterministic encoding.** The block MUST be encoded as valid dCBOR. See [03-encoding.md](03-encoding.md).
9. **Fork detection.** If a node receives a block whose `prev` value matches the `prev` of another block already stored from the same `pub` key, the node MUST detect this as a chain fork. Fork handling strategy (reject, flag, accept-first-seen) is implementation-scoped.

For private blocks, validation of rules 4, 5, and 6 is only possible by entities that hold the decryption key (since `refs` and `ops` are encrypted).

### Block identification

A block's CID is computed from its complete dCBOR encoding, including the signature:

```
block-cid = CID(dCBOR(block))
```

Internal references to blocks (in the `prev` and `refs` fields) use the raw SHA-256 digest (32 bytes), not the full CID. See [03-encoding.md](03-encoding.md), "Internal references".

## Security Considerations

- The signature covers the entire block including all operations (and for private blocks, the encrypted payload). Tampering with any field invalidates the signature.
- Timestamps are self-reported and untrusted. Implementations MUST NOT use timestamps for validation decisions. They are informational only.
- Foreign block references expand the trust boundary: by referencing a foreign block, an author asserts that the referenced block's entities are needed and available. See [05-processing-model.md](05-processing-model.md) for how foreign references affect Layer 2.
- Private blocks hide operation content, timestamps, and foreign references. An observer can see that an author published a block and its position in the chain (`prev`), but not what it contains, when it was created, or what other blocks it references. This prevents metadata leakage of timing information and social graph (via refs).
- Chain forking: A malicious or buggy author could publish two blocks with the same `prev`, creating divergent histories. Implementations MUST detect forks (see Validation rule 9). The handling strategy is implementation-scoped.
- Public blocks MUST NOT reference private blocks. This prevents public data from depending on content that non-recipient nodes cannot validate.

## Examples

### Genesis block with four operations

A genesis block creating two atoms, one bond, and one molecule:

```
{
  "v":    1,
  "type": "public",
  "pub":  <32 bytes: author's Ed25519 public key>,
  "sig":  <64 bytes: Ed25519 signature>,
  "prev": null,
  "refs": [],
  "ts":   1740067200,
  "ops": [
    {"op": "create_atom", "description": "Paris, the capital of France"},
    {"op": "create_atom", "description": "France"},
    {"op": "create_bond", "template": "_A_ is the capital of _B_"},
    {"op": "create_molecule",
     "bond": <digest of bond>,
     "fillers": [
       {"type": 0, "value": <digest of "Paris, the capital of France">},
       {"type": 0, "value": <digest of "France">}
     ]}
  ]
}
```

This block is valid because:
- `prev` is null (genesis block)
- The `create_molecule` operation references the bond and atoms created by earlier operations in the same block
- The `refs` list is empty (no foreign dependencies)

### Block with foreign reference

```
{
  "v":    1,
  "type": "public",
  "pub":  <32 bytes: author B's Ed25519 public key>,
  "sig":  <64 bytes: Ed25519 signature>,
  "prev": <digest of author B's previous block>,
  "refs": [<digest of a block in author A's chain>],
  "ts":   1740153600,
  "ops": [
    {"op": "create_molecule",
     "bond": <digest of a bond defined in author A's chain>,
     "fillers": [...]}
  ]
}
```

This block is valid because the bond referenced in the molecule was created in the specific block in author A's chain that is listed in `refs`. The ref points directly to the CID-providing block, not to a chain tip.

## References

### Normative
- [01-data-model.md](01-data-model.md) — Filler type definitions
- [03-encoding.md](03-encoding.md) — dCBOR encoding and CID computation
- [04-cryptography.md](04-cryptography.md) — Signature and encryption procedures
- [05-processing-model.md](05-processing-model.md) — How blocks are processed through L1/L2/L3

### Informative
- [IOTA Tangle](https://wiki.iota.org/learn/protocols/iota2.0/core-concepts/data-structures/) — Inspiration for the multi-chain DAG model
