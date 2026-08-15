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
4. If the block cannot be validated because its `prev` predecessor is not held, or is itself unvalidated, hold it as **stored but unvalidated**, or discard it — the choice is implementation-scoped

Validation is incremental: because every block in the store was validated when it was received, checking a new block's chain integrity is a lookup among accepted blocks rather than a re-validation of its ancestry. Validity is therefore defined inductively from the genesis block, and the induction is carried by the store (see [02-block-format.md](02-block-format.md), "Validation").

A **stored but unvalidated** block is one whose bytes a node holds and whose validity it has not been able to establish, because the block's ancestry has not arrived. Such a block is neither valid nor invalid. A node MAY keep it while it fetches the missing ancestors, and MUST validate it once they are available and validated; a node MAY instead discard it and re-request it later. A stored but unvalidated block MUST NOT be made available for L2 processing: its operations contribute nothing to the ontology graph until the block is validated. Nodes MUST NOT treat it as the predecessor of another block for the purposes of rule 3.

#### Chain management

A node maintains the set of author chains it is subscribed to. Subscriptions determine which chains the node fetches and stores at L1. A node only pulls blocks from chains it is subscribed to. For each chain, it stores all blocks from the genesis block to the current tip.

A user MUST subscribe to the blockchain of every author they subscribe to. A user MAY subscribe to additional blockchains.

#### Chain succession (key rotation)

When a node processes a rotation block (see [02-block-format.md](02-block-format.md)):

1. The node MUST mark the old key as inactive — no further blocks are accepted for it
2. The node MUST add the new key (from the `rotate_key` operation) to the set of known chains
3. If the user subscribes to the old key's author, the implementation SHOULD auto-subscribe to the new key's chain, treating it as the same logical author
4. Author identity (mapping multiple keys to a single author) is implementation-scoped

The new key's genesis block MUST be a public block and MUST reference the rotation block via `refs`; a chain whose genesis block does not is not the successor of that rotation, whatever key signed it (see [02-block-format.md](02-block-format.md), "rotate_key"). The genesis block is public so that every node can read the reference these steps ask it to act on; the blocks after it MAY be private. Steps 2 and 3 apply to the chain that carries the reference. If more than one genesis block references the same rotation block, the succession is ambiguous: the node MUST surface the conflict as it surfaces a fork, and MUST NOT pick a successor on its own.

### Layer 2 — Ontology graph accumulation

Layer 2 is a single, unified ontology graph built by extracting operations from all stored blocks.

#### Accumulation rules

For each valid block in L1 — that is, each block whose validation succeeded, never one that is stored but unvalidated — the node MUST:

1. Extract each operation from the block's `ops` list (for private blocks, decrypt the `enc` field first to recover `refs`, `ts`, and `ops` — see [04-cryptography.md](04-cryptography.md))
2. Compute the CID of the resulting entity (atom, bond, or molecule) per [01-data-model.md](01-data-model.md) and [03-encoding.md](03-encoding.md)
3. Add the entity to the L2 graph, tagged with:
   - The author's public key (from the block's `pub` field)
   - The block's CID (provenance)

The two tags of step 3 are the **minimum** every entity MUST carry, not a closed list. An implementation MAY record further provenance alongside them — the block's `ts`, the block's position in its author chain, local arrival order — provided no such value takes part in a validity decision. One L3 rule depends on provenance beyond the two required tags; see "Meta-molecule application" below.

Only the three entity-creating operations feed accumulation. A `rotate_key` operation creates no entity (see [02-block-format.md](02-block-format.md), "rotate_key"): step 2 has no CID to compute for it and step 3 nothing to add, so a rotation block contributes nothing to the ontology graph. A node's whole response to a rotation block is the L1 procedure of "Chain succession (key rotation)" above.

A node MAY record which blocks it has already processed, so that re-processing its store is idempotent — rotation blocks and any other block that contributed no entity included. Such bookkeeping is implementation-scoped and MUST NOT change what the graph contains.

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

- A public block's `refs` MUST NOT name a private block; public and rotation blocks MAY be named, a rotation block being in the clear for every node
- A private block's `refs` MAY name a block of any type
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

This test is uniform over every entity in L2, whatever kind of block it came from. Public and private, own chain and foreign chain, plain molecule and meta-molecule: the only question L3 asks of an entity is whether one of its authorship tags names a subscribed author. See "Private chains" below for what that means for a chain the node can decrypt.

*Informative.* The test is per entity and is not transitive. An entity reaches L3 on its own authorship tags; the entities its fields name — a molecule's `bond`, a molecule's fillers — are tested separately and may fail the test, because nothing requires them to have been published by the same author. A view can therefore hold a molecule whose bond only an unsubscribed author ever published, which is exactly the shape of the "Foreign chain loading (demand-driven resolution)" example below. This is expected and is not an error: closing L3 over an entity's references would admit unsubscribed authors' entities, which the paragraph above rules out, and dropping the molecule would discard data the user did subscribe to. L1 validation guarantees that every digest an operation names resolves (see [02-block-format.md](02-block-format.md), "Validation"), so L2 always holds the referenced entity, and the L3 implementation renders such a molecule by reading the missing bond or filler from L2 on the application's behalf. Doing so leaves "Application interface" below intact — the application still reads only L3 — and does not make the referenced entity accepted: it supplies the words, not the truth.

Note: Subscriptions are a cross-cutting concern. At L1, they determine which chains to fetch and store. At L3, they determine which authors' data to accept into application truth. L2 is unaffected — it accumulates all data pulled at L1 without filtering.

#### Meta-molecule application

Meta-molecules are applied during L2→L3 processing. The protocol defines the standard meta-bond library (see [06-meta-bonds.md](06-meta-bonds.md)) but the specific processing behavior is **implementation-scoped**.

The protocol requires:
- Implementations MUST recognize the standard meta-bonds
- Implementations MUST surface conflicts (e.g., when one subscribed author asserts "X is true" and another asserts "X is untrue") to the application layer

The protocol does NOT require any specific conflict resolution strategy. Possible strategies include:
- Flag for user intervention
- Author priority ranking
- Latest-wins (by block order — see "Assertion order" below)
- Application-specific logic

*Informative.* Meta-molecule semantics are computed over the subjects a view holds. [06-meta-bonds.md](06-meta-bonds.md) states that condition for two of the five meta-bonds — "If both molecules are present in L3" for contradiction, "If both A and B are in L3" for supersession — and the reference implementation generalizes it to truth assertion, truth retraction and equivalence, reading "present in L3" as "admitted by the filtering rules above" and nothing more. Two things follow. A subscribed author's meta-molecule about a subject the view does not hold has no L3 effect while the subject is absent, and takes effect on a later rebuild that finds it present: nothing is lost, because the meta-molecule itself is in L2 and in the view. And a meta-molecule never admits its own subject to L3 — admission is the subscription's business alone, and an author who could vouch data into a reader's view would invert the direction of control the filtering rules establish.

##### Assertion order

Some meta-bonds are defined in terms of one author's later statement overriding their earlier one — "If the same author previously asserted the molecule as true, the later assertion (by block order) takes precedence" ([06-meta-bonds.md](06-meta-bonds.md), "Truth retraction"). **Block order** is the position of the block a meta-molecule was published in within its author chain: the sequence the `prev` field defines, from the genesis block to the tip. It is recovered from the provenance tag of "Accumulation rules" step 3 — which names the block — together with the chain that block sits in.

Block order continues across a key rotation: every block of a successor chain comes after every block of the chain it succeeds, the two being joined by the reference the successor's genesis block carries (see "Chain succession (key rotation)"). Where a succession is ambiguous, so is the order, and the node MUST surface the conflict rather than pick a successor.

A block's `ts` field MUST NOT be used as this order. It is self-reported wall-clock metadata that no node verifies (see [02-block-format.md](02-block-format.md), "Security Considerations"), so an author who backdates or postdates a block would win every ordering decision; and a private block's `ts` lives inside its `enc` field, so an order built on it would resolve the same conflict differently on two nodes depending on which decryption keys each holds. An application MAY still order by timestamp as its own strategy, but that is not the ordering this specification means by "block order".

Assertion order is defined **within one author's chain only**. The assertions of two different authors are not ordered against each other: disagreement between subscribed authors is a conflict, and MUST be surfaced rather than settled by an ordering.

*Informative.* One author can publish the same meta-molecule more than once, and re-publication adds an authorship record rather than a new entity (see "Accumulation rules" above), so an author may hold several positions in their chain for one assertion. The reference implementation dates the assertion by the latest of them: re-publishing a meta-molecule re-states it, and an author's position on a molecule is the one their latest block naming it holds. An author who asserts a molecule true, later retracts it, and later still publishes the assertion again therefore holds it true. The reading is not forced by anything above — L2 has no notion of an operation that changed nothing, so no implementation can tell a deliberate re-statement from a redundant republication — and an implementation that dates an assertion by the block it first appeared in is equally conformant. It must choose one, because the two orders resolve the same chain differently.

*Informative.* Continuing the order across a rotation is an ordering rule and not an identity rule: it does not merge the two keys, does not change the authorship tags of "Accumulation rules", and does not change the filtering rules above, which stay per key. Mapping several keys to one author remains implementation-scoped ("Chain succession (key rotation)", step 4). What carries a *subscription* across a rotation is step 3 of that section — the SHOULD to auto-subscribe to the successor chain, treating it as the same logical author — so a node that follows it keeps the user's view intact across the rotation, and a node that does not leaves the user to subscribe to the new key.

#### Application interface

Applications MUST read from L3. Applications MUST NOT read directly from L1 or L2 for application data.

To write, an application sends operations to a Layer 1 blockchain node. The data flows through L1 → L2 → L3 before the application sees its own updates. Implementations MAY use optimistic heuristics (e.g., reflecting a change in the UI before L3 confirmation), but L3 remains the sole source of truth.

### Private chains

A user MAY maintain one or more private chains (see [04-cryptography.md](04-cryptography.md) for the encryption scheme). Private chain data flows through the same L1 → L2 → L3 pipeline:

1. L1: Private blocks are stored and validated (chain structure only via `prev`, since `refs`, `ts`, and `ops` are encrypted in the `enc` field)
2. L2: If the node holds the decryption key, the `enc` field is decrypted to recover `refs`, `ts`, and `ops`, and the operations are added to the graph. If not, the block is opaque.
3. L3: Private chain data is filtered exactly as any other data is — an entity is included when one of its authorship tags names a subscribed author (see "Filtering rules"). A private block carries its author's key in the clear, in its `pub` field, so the test is the same one and needs no special case. A user is always considered subscribed to the chains signed by a key they hold, which is what makes their own private chains unconditional: that is an instance of the filtering rule, not a mechanism beside it.

Decryption capability and subscription are orthogonal. An author MAY wrap a private chain's content key for another reader (see [04-cryptography.md](04-cryptography.md), "Key management"); that reader can then decrypt the chain's blocks at L2 and still not see its entities at L3, because a content key is a capability to read, not a declaration to accept. A reader who wants an author's private data in their L3 subscribes to that author, exactly as they would for public data. An author therefore cannot push data into a reader's truth by handing over a key, and a reader who loses interest unsubscribes without having to surrender anything.

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
