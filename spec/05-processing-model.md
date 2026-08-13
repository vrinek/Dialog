# Processing Model

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document defines Dialog's three-layer processing model: how raw blocks (Layer 1) become an ontology graph (Layer 2) and are distilled into application truth (Layer 3). It specifies the normative rules for L1 validation, L2 accumulation, and L3 truth distillation.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Author chain:** The linear sequence of blocks published by a single author.
- **Author subscription:** A user's declaration that they accept data from a specific author.
- **Blockchain subscription:** A user's subscription to the blocks of a specific author chain.
- **Foreign chain loading:** The process of resolving referenced foreign blocks into L2 via demand-driven traversal.

## Overview

Dialog processes data through three layers:

| Layer | Name | Contains | Role |
|-------|------|----------|------|
| L1 | "What we heard" | Raw signed blocks | Blockchain data with cryptographic integrity |
| L2 | "What we know" | Ontology graph | Accumulated, author-tagged knowledge |
| L3 | "What we accept" | Filtered graph | Subjective truth: subscription filtering, meta-molecule application, conflict surfacing |

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

A node maintains the set of author chains it is subscribed to. Subscriptions determine which chains the node fetches and stores at L1. A node only pulls blocks from chains it is subscribed to. For each chain, it stores all blocks from the genesis block to the current tip.

A user MUST subscribe to the blockchain of every author they subscribe to. A user MAY subscribe to additional blockchains.

#### Chain succession (key rotation)

When a node processes a rotation block (see [02-block-format.md](02-block-format.md)):

1. The node MUST mark the old key as inactive — no further blocks are accepted for it
2. The node MUST add the new key (from the `rotate_key` operation) to the set of known chains
3. If the user subscribes to the old key's author, the implementation SHOULD auto-subscribe to the new key's chain, treating it as the same logical author
4. Author identity (mapping multiple keys to a single author) is implementation-scoped

The new key's genesis block SHOULD reference the rotation block via `refs` to establish verifiable key succession.

### Layer 2 — Ontology graph accumulation

Layer 2 is a single, unified ontology graph built by extracting operations from all stored blocks.

#### Accumulation rules

For each valid block in L1, the node MUST:

1. Extract each operation from the block's `ops` list (for private blocks, decrypt the `enc` field first to recover `refs`, `ts`, and `ops` — see [04-cryptography.md](04-cryptography.md))
2. Compute the CID of the resulting entity (atom, bond, or molecule) per [01-data-model.md](01-data-model.md) and [03-encoding.md](03-encoding.md)
3. Add the entity to the L2 graph, tagged with:
   - The author's public key (from the block's `pub` field)
   - The block's CID (provenance)

L2 is append-only. Entities MUST NOT be removed or modified once added.

If an entity with the same CID already exists in L2 (because the same content was published by a different author, or re-published by the same author), the new authorship record is added alongside the existing one. The entity itself is not duplicated.

#### Foreign chain loading (demand-driven)

When a block references foreign blocks (via the `refs` field), the implementation resolves the entity digests carried by the block's operations through demand-driven traversal of the explicit refs graph.

##### Reference semantics

The `refs` field lists specific blocks that define entities needed by the current block's operations. Each entry is a 32-byte block digest (see [03-encoding.md](03-encoding.md), "Internal references"). References are explicit CID providers — they point to the blocks where needed entities were created.

The `prev` field is strictly for chain ordering (append-only semantics and fork detection). It is NOT used for CID resolution.

##### Resolution procedure

To validate a block H:

1. Extract all entity digests referenced by H's operations (bond digests, filler values)
2. Check if each digest was created in H itself (an earlier operation in the same block)
3. For unresolved digests, check ancestor blocks in the same chain (following `prev`)
4. For still-unresolved digests, fetch blocks listed in H's `refs` and check if the entity was created there
5. If a referenced block's entities themselves have unresolved transitive dependencies (e.g., a molecule references a bond not yet seen), recurse into that block's own `refs`
6. Continue until all digests are transitively resolved, or the scan limit is reached

##### Scan limit

Implementations MAY set a user-configurable limit on the number of foreign blocks scanned during recursive resolution. This limit SHOULD have a safe default. If the limit is reached before all digests resolve, the block MUST be treated as invalid (unresolvable references).

##### Public/private reference rules

- Public blocks MUST only reference public blocks in their `refs` field
- Private blocks MAY reference either public or private blocks
- Non-recipient nodes (those without the decryption key) MAY safely drop private blocks they cannot decrypt

The first rule is evaluated as each referenced block is resolved, not by fetching every entry of `refs` in advance: a node that resolves a referenced block and finds it private MUST reject the referencing public block, and reports the rule as unchecked for an entry it does not hold. Resolution is demand-driven, so an entry that resolution never needs may never be fetched. The same applies to the own-chain half of validation rule 10 (see [02-block-format.md](02-block-format.md), "The refs list").

##### Undecryptable reference handling

If a node can decrypt block H but cannot decrypt a block listed in H's `refs`, this is a validation error. The node MUST surface this error to the application layer. The node MUST NOT silently accept the block with partial validation.

##### Fat blocks

Since operations are idempotent at L2 (same CID = same entity), an implementation MAY include all transitively-needed operations in a single block, making it self-contained. This "fat block" strategy is an implementation choice for offline or portable use cases. The protocol does not mandate or prohibit it.

Foreign chain data is present in L2 for validation context but is not automatically promoted to L3 (see Layer 3 below).

#### No interpretation

L2 performs no interpretation of data. Meta-molecules (e.g., "X is true", "A is the same as B") are stored as regular molecules in L2. Their special semantics are only applied during L2→L3 processing.

### Layer 3 — Truth distillation

Layer 3 is the application's source of truth. It filters L2 by subscribed authors, applies meta-molecule semantics, and surfaces conflicts.

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

Note: Subscriptions are a cross-cutting concern. At L1, they determine which chains to fetch and store. At L3, they determine which authors' data to accept into application truth. L2 is unaffected — it accumulates all data pulled at L1 without filtering.

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

A user MAY maintain one or more private chains (see [04-cryptography.md](04-cryptography.md) for the encryption scheme). Private chain data flows through the same L1 → L2 → L3 pipeline:

1. L1: Private blocks are stored and validated (chain structure only via `prev`, since `refs`, `ts`, and `ops` are encrypted in the `enc` field)
2. L2: If the node holds the decryption key, the `enc` field is decrypted to recover `refs`, `ts`, and `ops`, and the operations are added to the graph. If not, the block is opaque.
3. L3: Private chain data from the user's own chain is included in L3 (the user always "subscribes" to their own chains).

## Security Considerations

- **L2 growth:** L2 grows unboundedly as more blocks are processed and foreign chains are loaded. This is an accepted trade-off for v1. Pruning rules may be defined in a future protocol version.
- **Recursive resolution depth:** A malicious author could construct deeply nested ref chains to force excessive traversal. The configurable scan limit (see Foreign chain loading) provides protection. Implementations SHOULD set a safe default and allow user configuration.
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

### Foreign chain loading (demand-driven resolution)

```
Alice's chain: block_1 → block_2 → block_3
Bob's chain:   block_A → block_B (refs: [Alice's block_2])

When processing Bob's block_B:
  1. block_B has a create_molecule referencing a bond
  2. Bond digest not found in block_B itself or Bob's block_A
  3. Check refs: Alice's block_2 is listed
  4. Fetch Alice's block_2 — bond was created there
  5. All digests resolved — block_B is valid
  6. Alice's block_1 is NOT fetched (not needed for resolution)

Alice's data in L2 is limited to what was actually needed.
```

## References

### Normative
- [01-data-model.md](01-data-model.md) — Entity definitions and content addressing
- [02-block-format.md](02-block-format.md) — Block structure and validation rules
- [03-encoding.md](03-encoding.md) — CID computation
- [04-cryptography.md](04-cryptography.md) — Private block encryption
- [06-meta-bonds.md](06-meta-bonds.md) — Standard meta-bond library and L3 application rules
