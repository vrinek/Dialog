# dialog-protocol

A clean-room TypeScript implementation of the [Dialog protocol](https://github.com/vrinek/Dialog)
wire format: dCBOR encoding, content identifiers, entities (atoms, bonds,
molecules), blocks, block sequences, and the transport profile.

Dialog is a distributed, append-only ontology graph protocol. This package
implements the on-the-wire and on-disk byte format the protocol specifies —
what is sometimes called L1 in the spec's own terms — so that data produced
here is byte-for-byte interoperable with the
[Go reference implementation](https://github.com/vrinek/Dialog/tree/main/go),
against which it is tested in CI.

## Install

```sh
npm install dialog-protocol
```

Requires Node.js 24 or later.

## Usage

Each module is exported individually — there is no single package-wide entry
point — so import from the subpath you need:

```ts
import { newAtom, entityCidText } from "dialog-protocol/entity";

const atom = newAtom("the Eiffel Tower");
console.log(entityCidText(atom));
// bafyreiaehulesvefhdp6zhju4fs5qkmrvj3pubp3iz5c4ht3iefqs5thdu
```

Verifying a block sequence read from disk or off the wire:

```ts
import { decodeBlockSequence } from "dialog-protocol/blockseq";

const items = decodeBlockSequence(bytes); // throws BlockSequenceError on any violation
for (const { block, digest } of items) {
  console.log(block.op, digest);
}
```

## Modules

| Subpath | Covers |
|---|---|
| `dialog-protocol/dcbor` | The deterministic CBOR profile all encoding builds on |
| `dialog-protocol/cid` | Digests, content identifiers (CIDs), author key text |
| `dialog-protocol/entity` | Atoms, bonds, molecules, fillers, the standard meta-bonds |
| `dialog-protocol/block` | Block structure, operations, the ten validation rules |
| `dialog-protocol/blockseq` | Block sequence framing (wire and `.dialog`/`.block` file format) |
| `dialog-protocol/privacy` | Ed25519 signatures, X25519 encryption, key rotation |
| `dialog-protocol/transport` | The client/server transport profile (`spec/07-transport.md`) |
| `dialog-protocol/node-http` | A Node `http` listener binding for the transport profile |
| `dialog-protocol/hex` | Byte/hex helpers used across the package |

## Scope

This package implements the protocol's wire format and transport profile —
spec documents [00](https://github.com/vrinek/Dialog/blob/main/spec/00-overview.md)
through [07](https://github.com/vrinek/Dialog/blob/main/spec/07-transport.md).
It does not yet implement L2 (a persistent store with subscriptions) or L3
(author filtering, meta-molecule application, conflict detection) processing
— see [`spec/05-processing-model.md`](https://github.com/vrinek/Dialog/blob/main/spec/05-processing-model.md).

## Specification

The full protocol specification lives in the main
[Dialog repository](https://github.com/vrinek/Dialog), starting at
[`spec/00-overview.md`](https://github.com/vrinek/Dialog/blob/main/spec/00-overview.md).

## Publishing story

This package ships as compiled JavaScript with `.d.ts` declarations (built
from the TypeScript sources with `tsc`), not raw `.ts`. See
[`PUBLISHING.md`](https://github.com/vrinek/Dialog/blob/main/ts/PUBLISHING.md)
for how releases are built.

## License

MIT
