# Dialog Protocol Specification — Overview

**Version:** 1.0 (2026-02-20) | **Status:** Draft

## Abstract

Dialog is a distributed, append-only ontology graph protocol. This document provides an overview of the protocol, its goals, scope, and architecture. It serves as the entry point to the specification, which is split across multiple documents.

## Goals

Dialog defines a protocol — not a single piece of software. The goal is interoperability: different software built by different people that can exchange and validate the same data. Think Ethereum network vs Ethereum clients.

## Scope

### What the protocol defines

| Concern | Document |
|---------|----------|
| Ontology data types (atoms, bonds, molecules) and content addressing | [01-data-model.md](01-data-model.md) |
| Block structure, operations, chain linking, and validation | [02-block-format.md](02-block-format.md) |
| CBOR encoding, CID format, and hashing parameters | [03-encoding.md](03-encoding.md) |
| Signatures, encryption, and key management | [04-cryptography.md](04-cryptography.md) |
| Three-layer processing model (L1/L2/L3) | [05-processing-model.md](05-processing-model.md) |
| Standard meta-bond library | [06-meta-bonds.md](06-meta-bonds.md) |

### What the protocol does NOT define

- **Transport.** How nodes discover each other and exchange blocks is an implementation concern. The protocol defines the data format and validation rules only.
- **Conflict resolution strategy.** When subscribed authors disagree, the protocol surfaces the conflict but does not dictate how to resolve it.
- **Query interface.** How applications query Layer 3 (SQL, GraphQL, API, etc.) is an implementation choice.
- **L3 processing rules** beyond subscription-based filtering and mandatory conflict detection.

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

**Layer 1** stores raw blocks. Each author maintains their own chain of signed blocks. Blocks reference previous blocks in the same chain and optionally reference blocks in other chains, forming a DAG.

**Layer 2** is the accumulated ontology graph. Operations are extracted from blocks and added to a single graph, tagged with authorship. No interpretation occurs — meta-molecules are stored as regular molecules.

**Layer 3** is the application's truth. L2 is filtered by the user's author subscriptions (private, local config). Meta-molecules are applied. Conflicts are surfaced.

Applications read from L3 only. To write, applications send operations to a blockchain node and wait for propagation through L1 → L2 → L3.

## Core concepts

### Content-addressed ontology

All data in Dialog is built from three primitives:

- **Atoms** — entities (e.g., "Paris, the capital of France")
- **Bonds** — relationship templates (e.g., "_A_ is the capital of _B_")
- **Molecules** — complete statements (e.g., "[Paris] is the capital of [France]")

All three are content-addressed: their identifier is determined by their content, not by who created them. The same entity created by different authors produces the same identifier.

### Blocks and chains

Authors publish data by creating blocks. A block contains operations (create_atom, create_bond, create_molecule) and is signed by the author's Ed25519 key. Each block links to the previous block in the author's chain and optionally to blocks in other authors' chains.

### Author subscriptions

Users choose which authors to trust by subscribing to them. Only data from subscribed authors passes from L2 to L3. Subscriptions are private and local.

### Meta-molecules

Some molecules carry special meaning: asserting truth, retracting claims, declaring equivalence, noting contradictions, superseding earlier statements, or rotating keys. These meta-molecules are regular molecules at L1/L2 but are interpreted during L2→L3 processing.

## Conventions

This specification uses:

- **RFC 2119 keywords** (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) for normative requirements
- **CDDL** ([RFC 8610](https://datatracker.ietf.org/doc/html/rfc8610)) for CBOR schema definitions
- **dCBOR** ([draft-mcnally-deterministic-cbor](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/)) for deterministic encoding

## Fixed parameters

The following parameters are fixed protocol-wide. Implementations MUST use exactly these values:

| Parameter | Value |
|-----------|-------|
| Serialization | CBOR (dCBOR profile) |
| Hash function | SHA-256 |
| CID version | CIDv1, codec `dag-cbor` (0x71) |
| Signature algorithm | Ed25519 |
| Key agreement | X25519 (Ed25519 keys converted) |
| Symmetric encryption | XChaCha20-Poly1305 |
| Protocol version | 1 |

## Open questions (v1)

The following are explicitly deferred to future protocol versions:

- **Key compromise handling.** KERI-style pre-rotation is the leading candidate.
- **L2 scalability.** Pruning rules may be needed as graphs grow.
- **Bond schema evolution.** Edge cases in cross-application schema bridging.

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
