---
title: "feat: Write Dialog protocol specification"
type: feat
status: completed
date: 2026-02-20
---

# Write Dialog Protocol Specification

## Overview

Write a formal, modular protocol specification for Dialog v1 based on the decisions captured in the [brainstorm document](../brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md). The spec should be precise enough for independent implementors to achieve interoperability.

## Format

- Modular Markdown documents in `spec/`
- RFC 2119 keywords (MUST, SHOULD, MAY) for normative requirements
- CDDL ([RFC 8610](https://datatracker.ietf.org/doc/html/rfc8610)) for CBOR structure definitions
- Worked examples with logical representation + CBOR hex + CID
- Reference dCBOR ([Internet-Draft](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/)) for deterministic encoding

## Documents

Each document follows this structure: Status, Abstract, Terminology, Overview (non-normative), Specification (normative), Security Considerations, Examples, References.

| Document | Covers | Source (brainstorm sections) |
|----------|--------|------------------------------|
| `00-overview.md` | Protocol goals, scope (defines / does not define), architecture summary, document index | "What We're Building", "Why This Approach" |
| `01-data-model.md` | Atoms, bonds, molecules, filler types, content addressing | "Content-addressed everything", "Filler value types" |
| `02-block-format.md` | Block fields (public + private), operations, validation rules, chain linking, foreign references | "Block format", "Three operation types" |
| `03-encoding.md` | dCBOR profile, CID parameters (CIDv1, `dag-cbor`/0x71, SHA-256/0x12, 32 bytes), multihash | "Serialization: CBOR" |
| `04-cryptography.md` | Ed25519 signatures, X25519 key agreement, key encoding, signature input, private block encryption | "Cryptography", "Private blockchain" |
| `05-processing-model.md` | L1/L2/L3 layers, block validation pipeline, L2 accumulation, L3 subscription filtering, foreign chain loading | "Three-layer processing model", "Applications read from L3 only" |
| `06-meta-bonds.md` | Standard meta-bond library (6 bonds), semantics, key rotation via meta-molecule, RFC-like extension process | "Standard Meta-Bond Library", "Key rotation mechanism" |

## Writing Order

1. `01-data-model.md` and `03-encoding.md` first — they define the foundational types everything else references
2. `02-block-format.md` — depends on data model and encoding
3. `04-cryptography.md` — depends on encoding (key/signature byte formats)
4. `05-processing-model.md` — depends on block format
5. `06-meta-bonds.md` — depends on data model and processing model
6. `00-overview.md` last — summarizes and indexes everything

## Acceptance Criteria

- [x] All 7 spec documents written in `spec/`
- [x] Every CBOR structure has a CDDL definition
- [x] At least one complete worked example (a block with all 3 operation types, encoded to CBOR hex, with CID)
- [x] RFC 2119 keywords used consistently
- [x] CID parameters locked down (no implementor choice)
- [x] README updated to link to `spec/`
- [x] `Dialog architecture.md` archived (superseded by spec)

## References

- [AT Protocol specs](https://atproto.com/specs/data-model) — structural model for CBOR + content addressing
- [Nostr NIP-01](https://github.com/nostr-protocol/nips/blob/master/01.md) — model for "not over-engineered v1"
- [RFC 8610: CDDL](https://datatracker.ietf.org/doc/html/rfc8610) — CBOR schema language
- [dCBOR Internet-Draft](https://datatracker.ietf.org/doc/draft-mcnally-deterministic-cbor/) — deterministic CBOR profile
- [CID spec](https://github.com/multiformats/cid) — content identifier format
- [KERI KIDs](https://github.com/decentralized-identity/keri) — modular spec development alongside implementation
