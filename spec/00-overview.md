# Dialog Protocol Specification — Overview

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

Dialog is a distributed, append-only ontology graph protocol. This document provides an overview of the protocol, its goals, scope, and architecture. It serves as the entry point to the specification, which is split across multiple documents.

## Goals

Dialog defines a protocol — not a single piece of software. The goal is interoperability: different software built by different people that can exchange and validate the same data. Think Ethereum network vs Ethereum clients.

## Scope

### What the protocol defines

The protocol defines the ontology data model, block format and validation, encoding parameters, cryptographic primitives, the three-layer processing model, and the standard meta-bond library. See the [Document index](#document-index) at the bottom of this page for the full list of specification documents.

### What the protocol does NOT define

- **Transport.** How nodes discover each other and exchange blocks is an implementation concern. The protocol defines the data format and validation rules only. [07-transport.md](07-transport.md) is an **optional interoperability profile**, not part of this list's exception: no block, chain or implementation is invalid for not speaking it, and file-based exchange is a complete conforming transport.
- **Conflict resolution strategy.** When subscribed authors disagree, the protocol surfaces the conflict but does not dictate how to resolve it.
- **Query interface.** How applications query Layer 3 (SQL, GraphQL, API, etc.) is an implementation choice.
- **L3 processing rules** beyond author filtering, meta-molecule application, and mandatory conflict detection.

## Architecture

Dialog processes data through three layers:

```
              ┌───────────────────┐
              │    Application    │
              └───┬───────────▲───┘
   writes to      │           │    reads from
      L1          │           │       L3
                  │   ┌───────┴───────────┐
                  │   │      Layer 3      │  "What we accept"
                  │   └───────▲───────────┘
                  │           │
                  │   ┌───────┴───────────┐
                  │   │      Layer 2      │  "What we know"
                  │   └───────▲───────────┘
                  │           │
                  │   ┌───────┴───────────┐
                  └──▶│      Layer 1      │  "What we heard"
                      └───────────────────┘
```

**Layer 1** stores raw blocks. Users subscribe to authors, determining which chains the node fetches and stores. Each author maintains their own chain of signed blocks. The `prev` field links each block to its predecessor in the same chain (ordering only). The `refs` field optionally references specific blocks in other chains that define the entities this block needs, forming a DAG. Both fields hold 32-byte block digests (see [03-encoding.md](03-encoding.md), "Internal references").

**Layer 2** is the accumulated ontology graph. Operations are extracted from blocks and added to a single graph, tagged with authorship. No interpretation occurs — meta-molecules are stored as regular molecules.

**Layer 3** is the application's truth. L2 distilled by filtering to subscribed authors, applying meta-molecule semantics, and surfacing conflicts.

Applications read from L3 only. To write, applications send operations to a blockchain node and wait for propagation through L1 → L2 → L3.

## Core concepts

### Content-addressed ontology

All data in Dialog is built from three primitives:

- **Atoms** — entities (e.g., "Paris, the capital of France")
- **Bonds** — relationship templates (e.g., "_A_ is the capital of _B_")
- **Molecules** — complete statements (e.g., "[Paris] is the capital of [France]")

All three are content-addressed: their identifier is determined by their content, not by who created them. The same entity created by different authors produces the same identifier.

### Blocks and chains

Authors publish data by creating blocks. A block contains operations (create_atom, create_bond, create_molecule, rotate_key) and is signed by the author's Ed25519 key. Each block links to the previous block in the author's chain (for ordering) and optionally references specific blocks in other authors' chains (for CID resolution).

### Author subscriptions

Users choose which authors to trust by subscribing to them. Subscriptions drive chain fetching at L1 and author filtering at L3. L2 is unaffected — it accumulates everything pulled at L1. Subscriptions are private and local.

### Meta-molecules

Some molecules carry special meaning: asserting truth, retracting claims, declaring equivalence, noting contradictions, or superseding earlier statements. These meta-molecules are regular molecules at L1/L2 but are interpreted during L2→L3 processing. Key rotation is handled separately as an L1 block-level operation (see [02-block-format.md](02-block-format.md)).

## Conventions

This specification uses:

- **RFC 2119 keywords** (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) for normative requirements
- **CDDL** ([RFC 8610](https://datatracker.ietf.org/doc/html/rfc8610)) for CBOR schema definitions
- **dCBOR** ([draft-mcnally-deterministic-cbor](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/)) for deterministic encoding

In examples, angle brackets denote placeholder values:

- `<digest of X>` — the raw 32-byte SHA-256 digest of X. This is the form every reference inside a Dialog structure takes (see [03-encoding.md](03-encoding.md), "Internal references").
- `<CID of X>` — the full 36-byte CIDv1 of X, used only where an external identifier is meant.
- `<n bytes: ...>` — an opaque byte string of the stated length (keys, signatures, ciphertext).

## Fixed parameters

The following parameters are fixed protocol-wide. Implementations MUST use exactly these values:

| Parameter | Value |
|-----------|-------|
| Serialization | CBOR (dCBOR profile) |
| Hash function | SHA-256 |
| CID version | CIDv1, codec `dag-cbor` (0x71) — external identifiers only |
| Internal reference format | Raw 32-byte SHA-256 digest |
| Signature algorithm | Ed25519 |
| Key agreement | X25519 (Ed25519 keys converted) |
| Symmetric encryption | XChaCha20-Poly1305 |
| Key derivation (KDF) | HKDF-SHA-256 (RFC 5869), salt: empty, info: "dialog-v1-key-wrap", output: 32 bytes |
| Signing domain separator | "dialog-v1-block" (byte prefix) |
| Protocol version | 1 |

## Open questions (v1)

The following are explicitly deferred to future protocol versions:

- **Key compromise handling.** KERI-style pre-rotation is the leading candidate.
- **L2 scalability.** Pruning rules may be needed as graphs grow.
- **Bond schema evolution.** Edge cases in cross-application schema bridging.
- **Equivalence composition.** Whether an equivalence between two molecules can be derived from equivalences between their bonds and fillers, rather than only declared (see [06-meta-bonds.md](06-meta-bonds.md), "Equivalence").

## Document index

| # | Document | Covers |
|---|----------|--------|
| 00 | [Overview](00-overview.md) | This document |
| 01 | [Data Model](01-data-model.md) | Atoms, bonds, molecules, filler types |
| 02 | [Block Format](02-block-format.md) | Block structure, operations, validation |
| 03 | [Encoding](03-encoding.md) | dCBOR, CID parameters, multihash |
| 04 | [Cryptography](04-cryptography.md) | Ed25519 signatures, X25519 encryption |
| 05 | [Processing Model](05-processing-model.md) | L1/L2/L3 layers, subscriptions |
| 06 | [Meta-Bonds](06-meta-bonds.md) | Standard meta-bond library, extension process |
| 07 | [Transport Profile](07-transport.md) | *Optional profile.* One serialization for wire and file, six sync operations, an HTTP binding |

Documents 00–06 are the protocol. Document 07 is an optional profile: normative for an implementation that chooses to speak it, and binding on nothing else.

Document 07 remains **Draft**. It now has one implementation — `go/transport`, which serves and syncs the grounding demo's committed chains over HTTP — and writing it surfaced five places where the draft is silent or says two things; they are listed in that document's "Gaps the first implementation found" and are what has to be settled before the profile leaves Draft.
