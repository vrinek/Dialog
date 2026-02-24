# Processing Model

**Version:** 1.0 (2026-02-20) | **Status:** Draft

## Abstract

This document defines Dialog's three-layer processing model: how raw blocks (Layer 1) become an ontology graph (Layer 2) and are filtered into application truth (Layer 3). It specifies the normative rules for L1 validation, L2 accumulation, and L3 subscription filtering.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Author chain:** The linear sequence of blocks published by a single author.
- **Author subscription:** A user's declaration that they accept data from a specific author.
- **Blockchain subscription:** A user's subscription to the blocks of a specific author chain.
- **Foreign chain loading:** The process of pulling a referenced foreign chain's history into L2.

## Overview

Dialog processes data through three layers:

| Layer | Name | Contains | Role |
|-------|------|----------|------|
| L1 | "What we heard" | Raw signed blocks | Blockchain data with cryptographic integrity |
| L2 | "What we know" | Ontology graph | Accumulated, author-tagged knowledge |
| L3 | "What we accept" | Filtered graph | Subjective truth based on subscriptions |

Data flows strictly downward: L1 → L2 → L3. Applications read from L3 and write to L1.

## Specification

### Layer 1 — Block storage and validation

Layer 1 is responsible for storing, validating, and exchanging blocks.

#### Block reception

When a node receives a block, it MUST:

1. Validate the block according to the rules in [02-block-format.md](02-block-format.md)
2. If valid, store the block and make it available for L2 processing
3. If invalid, reject the block

#### Chain management

A node maintains the set of author chains it is subscribed to. For each chain, it stores all blocks from the genesis block to the current tip.

A user MUST subscribe to the blockchain of every author they subscribe to. A user MAY subscribe to additional blockchains.

### Layer 2 — Ontology graph accumulation

Layer 2 is a single, unified ontology graph built by extracting operations from all stored blocks.

#### Accumulation rules

For each valid block in L1, the node MUST:

1. Extract each operation from the block's `ops` list (decrypting first if the block is private and the node holds the key)
2. Compute the CID of the resulting entity (atom, bond, or molecule) per [01-data-model.md](01-data-model.md) and [03-encoding.md](03-encoding.md)
3. Add the entity to the L2 graph, tagged with:
   - The author's public key (from the block's `pub` field)
   - The block's CID (provenance)

L2 is append-only. Entities MUST NOT be removed or modified once added.

If an entity with the same CID already exists in L2 (because the same content was published by a different author, or re-published by the same author), the new authorship record is added alongside the existing one. The entity itself is not duplicated.

#### Foreign chain loading

When a block references a foreign block (via the `refs` field), the foreign chain's history up to and including the referenced block MUST be loaded into L2, even if the user does not subscribe to that foreign author.

Specifically:

1. Retrieve the foreign block and all its ancestors (following `prev` links to the genesis block)
2. Validate each block
3. Process each block through L2 accumulation (extract operations, add to graph)

Foreign chain data is present in L2 for validation context but is not automatically promoted to L3 (see Layer 3 below).

#### No interpretation

L2 performs no interpretation of data. Meta-molecules (e.g., "X is true", "A is the same as B") are stored as regular molecules in L2. Their special semantics are only applied during L2→L3 processing.

### Layer 3 — Subscription filtering and truth distillation

Layer 3 is the application's source of truth. It is L2 filtered by the user's author subscriptions.

#### Author subscriptions

A user maintains a list of subscribed authors. This list is:

- **Local configuration.** Subscriptions are stored locally on the user's node.
- **Private.** Subscriptions are never published on-chain. Other users cannot see who a user subscribes to.

#### Filtering rules

Only data from subscribed authors passes from L2 to L3:

1. For each entity in L2, check if any of its authors (from the authorship tags) is in the user's subscription list
2. If yes, the entity is included in L3
3. If no, the entity is excluded from L3

Foreign chain data that was loaded into L2 for validation context is excluded from L3 unless the user independently subscribes to the foreign author.

#### Meta-molecule application

Meta-molecules are applied during L2→L3 processing. The protocol defines the standard meta-bond library (see [06-meta-bonds.md](06-meta-bonds.md)) but the specific processing behavior is **implementation-scoped**.

The protocol requires:
- Implementations MUST recognize the standard meta-bonds
- Implementations MUST surface conflicts (e.g., when one subscribed author asserts "X is true" and another asserts "X is untrue") to the application layer

The protocol does NOT require any specific conflict resolution strategy. Possible strategies include:
- Flag for user intervention
- Author priority ranking
- Latest-wins (by timestamp or block order)
- Application-specific logic

#### Application interface

Applications MUST read from L3. Applications MUST NOT read directly from L1 or L2 for application data.

To write, an application sends operations to a Layer 1 blockchain node. The data flows through L1 → L2 → L3 before the application sees its own updates. Implementations MAY use optimistic heuristics (e.g., reflecting a change in the UI before L3 confirmation), but L3 remains the sole source of truth.

### Private chains

Each user has at least one private chain (see [04-cryptography.md](04-cryptography.md) for the encryption scheme). Private chain data flows through the same L1 → L2 → L3 pipeline:

1. L1: Private blocks are stored and validated (chain structure only, since operations are encrypted)
2. L2: If the node holds the decryption key, operations are decrypted and added to the graph. If not, the block is opaque.
3. L3: Private chain data from the user's own chain is included in L3 (the user always "subscribes" to their own chains).

## Security Considerations

- **L2 growth:** L2 grows unboundedly as more blocks are processed and foreign chains are loaded. This is an accepted trade-off for v1. Pruning rules may be defined in a future protocol version.
- **Foreign chain loading:** A malicious author could reference a very large foreign chain, forcing nodes to download and process it. Implementations SHOULD set reasonable limits on foreign chain loading depth or size.
- **Subscription privacy:** Author subscriptions are never published, protecting users from social graph analysis. However, the set of blockchains a node requests from the network may reveal subscription information at the transport layer. Transport implementations SHOULD consider this.
- **Optimistic writes:** Applications using optimistic heuristics MUST handle the case where an operation fails to propagate to L3 (e.g., due to a validation failure discovered during L2 processing).

## Examples

### Full data flow

```
Author Alice publishes a block:
  Block: {ops: [create_atom("Paris"), create_bond("_A_ is in _B_"), ...]}

L1: Block is validated and stored in Alice's chain

L2: Operations are extracted:
  - Atom "Paris" added to graph, tagged with Alice's pubkey
  - Bond "_A_ is in _B_" added to graph, tagged with Alice's pubkey

Bob subscribes to Alice:

L3 (Bob's view): Bob's L3 includes:
  - Atom "Paris" (because Alice is subscribed)
  - Bond "_A_ is in _B_" (because Alice is subscribed)

Carol does NOT subscribe to Alice:

L3 (Carol's view): Carol's L3 does NOT include Alice's data
  - Unless Carol subscribes to an author who references Alice's blocks
    (in which case Alice's data is in L2 but not L3)
```

### Foreign chain loading

```
Alice's chain: block_1 → block_2 → block_3
Bob's chain:   block_A → block_B (refs: [Alice's block_2])

When processing Bob's block_B:
  1. Foreign ref points to Alice's block_2
  2. Load Alice's chain: block_1, block_2 (not block_3)
  3. Process Alice's block_1 and block_2 into L2

Bob's block_B can now reference entities from Alice's block_1 and block_2.
Alice's data is in L2 but only in L3 for users who subscribe to Alice.
```

## References

### Normative
- [01-data-model.md](01-data-model.md) — Entity definitions and content addressing
- [02-block-format.md](02-block-format.md) — Block structure and validation rules
- [03-encoding.md](03-encoding.md) — CID computation
- [04-cryptography.md](04-cryptography.md) — Private block encryption
- [06-meta-bonds.md](06-meta-bonds.md) — Standard meta-bond library and L3 application rules
