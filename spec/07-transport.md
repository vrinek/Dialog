# Transport Profile

**Version:** <<VERSION>> | **Status:** Draft (optional profile)

## Abstract

This document defines an optional interoperability profile for moving Dialog blocks between nodes: one serialization that is both a wire body and a file, six abstract operations over it, and a binding of those operations to HTTP. It also states the client and server obligations that make the profile safe — chiefly that the transport carries no trust, and that a source can lie only by omission.

**Transport remains outside the protocol.** [00-overview.md](00-overview.md) lists transport among the things the protocol does not define, and that is unchanged: no block, no chain and no implementation is invalid for not speaking this profile, nothing here changes a byte of the wire format, and no block's validity depends on any transport being reachable. File-based exchange — the serialization below, at rest, on a disk or a USB stick or an email attachment — is a complete conforming transport. This document exists so that two implementations that *do* want to talk over a network have one thing they can both speak, and so that a future profile over some other substrate has the same six questions to answer.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Source:** Anything a node obtains blocks from: a server, a file, a directory, a removable disk, another node.
- **Server:** A source that answers the operations of this profile over a network.
- **Client:** A node that issues the operations of this profile against a source.
- **Block sequence:** The one serialization this profile defines — an [RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742) CBOR sequence of block encodings.
- **Range:** A contiguous run of one author chain's blocks, in chain order.
- **Position:** A place in an author chain, named by the digest of the block that occupies it, or by the **genesis position**, which is the place before the genesis block and is named by the absence of a digest.
- **Sibling set:** Every block a source holds from one author whose `prev` names one position. A sibling set with more than one member is a fork (see [02-block-format.md](02-block-format.md), validation rule 9).
- **Announce:** The one operation that moves blocks toward a source rather than away from it.

## Overview

### What this profile is and is not

Every Dialog block is self-authenticating. The signature, the author key, the chain link and the content address are all inside the bytes, and the validation rules of [02-block-format.md](02-block-format.md) need nothing whatever from the channel that carried them. A source therefore **cannot lie about a block's contents**; it can only **lie by omission** — withholding a block, a branch of a fork, or a tip.

That single fact sets the shape of everything below. This profile specifies no authentication, no confidentiality, no session and no trusted party, because none of those would buy a client anything it does not already hold inside the block. What it specifies instead is: how blocks are laid out in a byte stream, six ways of asking for some, the obligation to verify everything on arrival, and the two properties the channel genuinely cannot give — completeness and freshness — stated as gaps rather than papered over.

### The shape of it

```
        ┌──────────────┐   tip / range / block / blocks / siblings   ┌──────────┐
        │              │ ──────────────────────────────────────────▶ │          │
        │    client    │                                             │  source  │
        │  (a node)    │ ◀────────────────────────────────────────── │          │
        └──────────────┘        a block sequence, verified           └──────────┘
               │                                                          ▲
               │  announce                                                │
               └──────────────────────────────────────────────────────────┘

  a "source" is a server, a file, a directory, or a disk in an envelope;
  the block sequence is the same bytes in every case
```

An author's node publishes by announcing to the servers it uses; a server replicates to another server with the same operation; a client never needs a mechanism a server does not also speak. There is no separate peer protocol because there is no separate peer.

## Specification

### The block sequence

There is exactly one representation of "some blocks" in this profile, and it is used for every response body, every request body that carries blocks, and every file.

A **block sequence** is a CBOR sequence ([RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742)): the canonical dCBOR encoding of zero or more blocks, concatenated, with no framing, no length prefix, no wrapper and no separator. Each block's encoding is exactly the bytes its digest is taken over and its signature covers (see [02-block-format.md](02-block-format.md), "Block identification").

```cddl
; A block sequence is not a single CBOR item. It is zero or more top-level
; dCBOR items, each of which is a block as defined in 02-block-format.md.
block-sequence = *block   ; RFC 8742 CBOR sequence framing: none
```

Rules:

1. Every item MUST be a well-formed dCBOR encoding of a block as defined in [02-block-format.md](02-block-format.md). A sequence containing anything else is malformed as a whole.
2. A sequence MAY be empty. An empty sequence is a zero-length byte string, and it is a valid answer meaning "none".
3. A reader MUST decode items one after another until the input is exhausted, and MUST treat a truncated final item as an error rather than as the end of the sequence.
4. The sequence carries **no metadata**: no count, no author, no position, no timestamp, no signature of its own. Everything a reader needs is inside the blocks, which is what makes a saved response and a hand-carried file the same artifact.

#### Ordering

Ordering is a property of the operation that produced the sequence, not of the format, and every operation below fixes one:

- A **range** MUST be in chain order, genesis-ward first: for every item after the first, its `prev` field MUST equal the digest of the item immediately before it. The blocks MUST be contiguous — a source MUST NOT skip a block it holds, and MUST NOT reorder.
- A **sibling set** MUST be ordered by ascending bytewise comparison of the blocks' digests. The order is fixed so that two sources holding the same set produce the same bytes, which makes the response cacheable and comparable.
- A **digest-keyed fetch** MUST return the blocks in the order the request named them, omitting any the source does not hold. A client MUST NOT identify a returned block by its position in the sequence; it identifies each block by re-hashing it (see "Verification obligations").
- An **announce** body carrying blocks of one author MUST be in chain order. Blocks of several authors MAY be interleaved in any way that keeps each author's own blocks in chain order.

#### A partial range

A range operation returns a **contiguous prefix** of what the client asked for, never a subset with holes. Three things can end a response short of the chain tip: the client's requested maximum, a limit of the source's own, or a position past which the source holds nothing (a hole in what it stores, or simply the end of what it has).

A client expresses "give me the rest" by issuing the range operation again with its position set to the digest of the last block it received. This is the only continuation mechanism, and it needs no cursor, no session and no server-side state: the position *is* the digest of a block the client holds, so a client that stops and resumes a week later on a different machine resumes correctly.

A client learns whether a range ended at the tip by comparing the last block it received with the tip the source reports (`tip`, or the tip statement carried alongside a range response in a binding that has one). That comparison tells the client where the *source* says the chain ends. It is not evidence about where the chain actually ends — see "What a source does not guarantee".

#### As a file

A block sequence written to a file is a chain file. Nothing is added and nothing is removed: a range response saved to disk is a valid chain file, and a chain file offered to a server is a valid announce body. The conventional extension is `.dialog`; a file holding exactly one block MAY use `.block`, which is the same thing at length one.

This is the property that keeps offline exchange from being a parallel mechanism. The grounding demo's committed chain directory is the degenerate case rather than an exception to it: each `.block` file is a one-block sequence, and concatenating one author's block files in the order the directory's index lists them yields, byte for byte, the range response for that author's whole chain from the genesis position. The index beside them is a local convenience — it carries no authority, and a reader that trusted it would still have to validate every block (see [02-block-format.md](02-block-format.md), "Validation").

### The six operations

The operations are defined here abstractly — a name, a request, a response, and what the response means — so that a binding to something other than HTTP answers the same questions. The HTTP binding follows.

| Operation | Request | Response | Answers |
|-----------|---------|----------|---------|
| `tip` | an author key | the block at that author's tip, as the source holds it | "has this chain moved?" |
| `range` | an author key, a position, an optional maximum count | a contiguous range of that author's chain beginning at the block after the position | chain sync, genesis to tip |
| `block` | one block digest | that block | demand-driven resolution of one `refs` entry |
| `blocks` | a list of block digests | the subset the source holds | demand-driven resolution within the scan limit |
| `siblings` | an author key, a position | every block the source holds from that author whose `prev` names that position | fork and succession discovery |
| `announce` | a block sequence | which of the blocks the source accepted | publication and replication |

Common rules:

- **A source answers only about what it holds.** "I do not have it" is the answer to every question a source cannot answer from its own store. It is never evidence that the thing does not exist, and a client MUST NOT treat it as such.
- **Every operation is independent.** No operation establishes state that a later one depends on, no operation requires an identifier for the client, and the same request repeated returns the same answer or a later one. `announce` is the only operation that changes a source.
- **Every operation is safe to answer for anyone.** This profile defines no authorization; see "Server rules" for what a deployment may put in front of it.
- A source MUST implement `tip`, `range`, `block`, `blocks` and `siblings` to be a conforming server. `announce` is OPTIONAL: a read-only mirror is conforming.

#### tip

*Request:* an author key. *Response:* one block — the block that occupies the tip position of that author's chain in the source's store — or "I do not have it" when the source holds no block from that author.

The response is the **block itself**, not a statement of its digest. A client computes the digest and the CID from the bytes it received, which means the source cannot misreport the tip's identity; it can only choose which tip to show, which is the freshness gap and is not fixable here (see "What a source does not guarantee").

A source that holds a fork has more than one candidate for the tip position. It MUST answer `tip` with exactly one of them — the choice is source policy — and it MUST answer `siblings` honestly about the divergence. A client that cares about forks does not learn about them from `tip`.

#### range

*Request:* an author key, a position, and an optional maximum number of blocks. *Response:* a block sequence in chain order, beginning with the block whose `prev` names the requested position and continuing contiguously.

The position is **exclusive**: the block naming it is not in the response, the blocks after it are. The genesis position requests the chain from its genesis block.

A source MUST NOT return more blocks than the requested maximum. It MAY return fewer, for any reason, including one of its own. If it holds at least one block at the requested position it MUST return at least one block. If it holds none, the response is an empty sequence — which is the answer both when the client is already at the tip and when the source's store stops there, and those two are distinguished by comparing against `tip`, not by the emptiness.

If the source holds more than one block at the requested position — a fork — it MUST answer `range` along one branch only, consistently with what its `tip` reports, and MUST NOT interleave branches. Untangling a fork is `siblings`' job.

#### block

*Request:* one block digest. *Response:* that block, or "I do not have it".

This is the primitive that [05-processing-model.md](05-processing-model.md)'s "Resolution procedure" step 4 needs. The request carries a digest and nothing else, because a `refs` entry carries a digest and nothing else: no author, no chain, no locator (see [03-encoding.md](03-encoding.md), "Internal references"). A source can therefore answer only for blocks in its own store, and a client that must resolve a reference no source it knows holds has an open problem, not a protocol error — see todo 072.

#### blocks

*Request:* a list of block digests. *Response:* a block sequence holding the subset the source holds, in the order requested.

`blocks` exists because `block` does not scale to the validation path. The scan limit of [05-processing-model.md](05-processing-model.md) counts distinct foreign blocks scanned per block validated and defaults to 256, so a worst-case honest validation is 256 resolutions; done one round trip at a time over a network, that is a latency budget nobody can pay. A conforming server MUST accept a request naming at least 256 digests, so that the whole budget fits in one exchange.

A request MUST NOT name the same digest twice; a source MAY reject such a request or MAY answer it as though the duplicate were absent. Digests the source does not hold are simply not in the response, and the response says nothing about why.

#### siblings

*Request:* an author key and a position. *Response:* every block the source holds signed by that key whose `prev` names that position, ordered by ascending digest.

This is the operation that gives [02-block-format.md](02-block-format.md)'s validation rule 9 something to fire on. A source MUST include every such block it holds — including the one it would itself serve from `range` and `tip`, so that the client sees a set rather than a difference — and MUST NOT choose a winner. The genesis position asks the question for the start of the chain, which is how the ambiguous-succession condition of [02-block-format.md](02-block-format.md), "rotate_key", is detected: two genesis blocks referencing the same rotation block are two members of the genesis position's sibling set.

A response with one member is not a statement that the chain does not fork. It is a statement that this source is not showing more than one block there, which a source serving one side of a fork has every incentive to do. `siblings` is a convenience for the honest case; the mechanism that actually detects forks is the multi-source rule below.

#### announce

*Request:* a block sequence. *Response:* a statement of what the source did with each block.

The source MUST validate every block per [02-block-format.md](02-block-format.md) before storing it, exactly as it would a block from any other origin, and MUST NOT store as valid a block whose predecessor it does not hold and has not validated — such a block is *stored but unvalidated* or discarded, per [05-processing-model.md](05-processing-model.md), "Block reception". A source MAY refuse an announce entirely, for reasons that are its own policy: quota, rate, acquaintance, disk.

`announce` carries no authority in either direction. The announcer is not asserting anything a block does not already say, and the source is not endorsing anything by accepting one. It is the same bytes moving the other way.

### HTTP binding

A server is named by a **base URL**, learned out of band. Every path below is relative to it. The default prefix is `/dialog/v1`; a server MAY be mounted at any base URL, and a client is configured with the whole base URL rather than with a host. The `v1` names the version of *this profile*, not the protocol version carried in a block's `v` field.

`{author}` is an author's Ed25519 public key in the canonical text form of [03-encoding.md](03-encoding.md), "Text representation of author keys": multibase base32, 56 characters, always beginning `b5ua`. `{cid}` is a block's CID in the base32 text form of [03-encoding.md](03-encoding.md), "Content identifiers (CIDs)" → "Text representation": 59 characters, always beginning `bafyrei`. Both are case-sensitive, and a server MUST reject any other spelling of either with 400 rather than normalizing it — the two forms are canonical in both directions, and a server that accepted a variant would be minting aliases (see [03-encoding.md](03-encoding.md), Security Considerations).

A block digest inside a `refs` or `prev` field is 32 raw bytes and has no text form of its own. A client converts it to the CID text form before placing it in a URL, by the fixed prefix of [03-encoding.md](03-encoding.md), "Computing an entity's CID". Hexadecimal MUST NOT appear in a path or a query parameter: it is a byte dump, not an identifier.

| Operation | Method and path |
|-----------|-----------------|
| `tip` | `GET /dialog/v1/chains/{author}/tip` |
| `range` | `GET /dialog/v1/chains/{author}/blocks?after={cid}&limit={n}` |
| `block` | `GET /dialog/v1/blocks/{cid}` |
| `blocks` | `POST /dialog/v1/blocks/fetch` |
| `siblings` | `GET /dialog/v1/chains/{author}/siblings?prev={cid}` |
| `announce` | `POST /dialog/v1/announce` |

- `after` and `prev` **omitted** denote the genesis position. The literal string `null` MUST NOT be used and MUST be rejected with 400; exactly one spelling of a position is admitted, for the same reason exactly one spelling of a CID is.
- `limit` is a positive decimal integer. A server MAY cap it and MUST NOT exceed it. Absent, the server chooses.
- `HEAD` MUST be supported wherever `GET` is. Any other method on a defined path MUST return 405 with an `Allow` header.

#### Bodies and content types

| Body | Media type |
|------|------------|
| A block sequence | `application/dialog-blocks+cbor-seq` |
| A `blocks` request | `application/json` |
| An `announce` receipt | `application/json` |
| Any error | `application/problem+json` ([RFC 9457](https://datatracker.ietf.org/doc/html/rfc9457)) |

The block media type uses the `+cbor-seq` structured syntax suffix registered by [RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742). A client SHOULD send `Accept: application/dialog-blocks+cbor-seq` and MUST accept `application/cbor-seq` as an equivalent, since a plain file server offering a directory of chain files will send the generic type and its bytes are the same bytes. A server MUST NOT serve a block sequence under any other type.

Bodies that carry blocks are CBOR sequences. Every other body is JSON, because the alternative — a second CBOR profile for envelopes — would put dCBOR in a place where its rules answer no question, and because a diagnostic a person reads at three in the morning should be readable without a decoder.

A `blocks` request body is one JSON object:

```json
{"digests": ["bafyrei…", "bafyrei…"]}
```

`POST /dialog/v1/blocks/fetch` is the one request in this profile that looks unsafe and is not: it is a `GET` with an argument list too long for a URL. It MUST have no side effects, it MUST be idempotent, and a server MAY make its response cacheable by explicit `Cache-Control`.

An `announce` receipt is one JSON object mapping each submitted block's CID to what became of it:

```json
{"accepted": ["bafyrei…"], "held": ["bafyrei…"], "rejected": {"bafyrei…": "signature check failed"}}
```

`accepted` are blocks the server validated and stored; `held` are blocks it is keeping as *stored but unvalidated* pending their ancestry; `rejected` names each block it refused and why, in prose meant for a person. Every submitted block MUST appear in exactly one of the three, and a server that already held a block reports it as `accepted`.

#### Status codes

| Code | Meaning in this profile |
|------|-------------------------|
| 200 | Here is the answer. For a range or a sibling set the answer may be an empty sequence. |
| 202 | The announce was taken for later processing; the receipt is incomplete or absent. |
| 304 | The `tip` is unchanged from the `If-None-Match` the client sent. |
| 400 | The request was malformed: a bad author key, a bad CID, a non-canonical spelling of either, a bad `limit`. |
| 404 | **I do not have it.** Never "it does not exist." |
| 405 | Wrong method for a defined path. |
| 406 | The client's `Accept` excludes the only type this server can send. |
| 413 | The announce or fetch body is larger than this server accepts. |
| 415 | The request body's media type is not the one the operation defines. |
| 429 | Rate limited; `Retry-After` SHOULD be present. |
| 503 | Temporarily unable; `Retry-After` SHOULD be present. |

404 carries the whole weight of the completeness gap, and its natural HTTP reading is the wrong one, so it is written down: a source's absence of a block is a fact about the source. It says nothing about whether the block exists, whether the author published it, or whether some other source has it.

Error bodies are RFC 9457 problem details. The `title` and `detail` members are for people; a client MUST branch on the status code and MUST NOT parse `detail`.

#### Caching

- `GET /dialog/v1/blocks/{cid}` is immutable: the response for a given CID can never change, because the CID is the hash of the response. A server MUST send `Cache-Control: public, max-age=31536000, immutable`. This is not a performance note — it means a CDN, a corporate proxy or a local cache can serve foreign-block resolution, which is exactly the traffic the scan limit makes hot, and it means those caches absorb part of the request-pattern leak for free.
- `GET .../tip` MUST carry a strong `ETag` whose value is the tip block's CID, and SHOULD carry `Cache-Control: no-cache` so that a cache revalidates rather than answers. `If-None-Match` on this endpoint is how a client polls a chain for a few dozen bytes.
- Range and sibling responses are not immutable — a source's store grows — and a server SHOULD serve them with `Cache-Control: no-cache` or a short `max-age`.

#### Subscription mapping

An L1 blockchain subscription (see [05-processing-model.md](05-processing-model.md), "Chain management") becomes one of three things, in increasing order of server cooperation and decreasing order of privacy. All three are mappings of the `tip` operation; none is a seventh operation.

1. **Polling `tip` with `If-None-Match`.** A 304 is a few dozen bytes. Every conforming client and server supports this, and it needs no server feature beyond a correct `ETag`. It is the baseline.
2. **Long-polling: `GET .../tip?wait={seconds}`.** The server holds the request open until the tip differs from the client's `If-None-Match` or `wait` seconds elapse, then answers 200 or 304. A server MAY cap `wait` and MUST answer within its own cap. OPTIONAL; a server that does not implement it MUST ignore the parameter and answer immediately, which degrades to polling.
3. **A tip event stream: `GET /dialog/v1/events?author={author}&author={author}`**, as server-sent events, emitting one event per chain movement whose data is a JSON object `{"author": "b5ua…", "tip": "bafyrei…"}`. One connection covers many chains. OPTIONAL.

The event stream is the only part of this profile that hands one server a client's whole subscription set in a single, durable, correlated act, which is precisely the leak [05-processing-model.md](05-processing-model.md)'s Security Considerations warn about. It is specified as a convenience with a stated cost, and it is not the recommended mode.

**Webhooks are deliberately absent.** They require the client to be reachable, they need a registration and authentication surface, and the registration is a durable server-side record of exactly what a user follows. Each of those is a cost this profile is built to avoid.

### Client rules

#### Verification obligations

A client MUST validate every block it receives, from every source, per [02-block-format.md](02-block-format.md), before the block is stored or its operations reach L2 — the ordinary "Block reception" procedure of [05-processing-model.md](05-processing-model.md), with no step removed because the bytes arrived over a network. In particular:

1. A client MUST re-hash every block it receives and MUST identify it by the digest it computes, never by the position the block held in a sequence, never by the URL it was fetched from, and never by anything a source said about it. A `block` response whose bytes hash to something other than the requested digest is a failed fetch, not a block.
2. A client MUST verify the range property of a range response for itself, by checking that each block's `prev` names the block before it and that the first block's `prev` names the position it asked about. A source that skips a block produces a break the client sees immediately; *within* a range, completeness is free.
3. A client MUST NOT treat transport-level authenticity as validation. TLS protects the request pattern, not the data. A plaintext mirror is a downgrade in privacy and not in integrity.
4. A client MUST NOT let a source's answer decide a validation outcome that the source's bytes do not compel. In particular, a 404 for a `refs` entry is a fetch that did not succeed; it is not a finding that the reference is unresolvable.

#### The multi-source rule

**A node SHOULD obtain each chain it follows from more than one source, and compare.**

A source is anything: two servers, a server and a file, a server and a friend's disk. This rule is the part of the profile that does the most work, and it needs no wire support at all — every response is self-verifying and stateless, so "ask two sources" is the same code twice.

The reason is that **fork detection is a reachability property, not a query.** [02-block-format.md](02-block-format.md)'s validation rule 9 requires a node to detect a fork when it *holds* two blocks with the same `prev` from the same author. A node that only ever hears one version of a chain, from one source, satisfies rule 9 vacuously and forever: the rule is normative, and whether it can ever fire is a property of the transport. The `siblings` operation helps only where the source is honest, and a source serving one branch of a fork has every incentive not to be. Two sources with different branches produce a fork at the client even when neither admits to one.

The rule also blunts the subscription leak: a node that splits twenty chains across four servers hands no single party the full set.

Whether the specification proper should carry this obligation, rather than an optional profile, is open — see todo 070.

#### Interaction with the scan limit

Demand-driven resolution is on the validation path and is bounded (see [05-processing-model.md](05-processing-model.md), "Scan limit"). Three consequences for a client of this profile:

1. **Batch.** A client resolving a block's `refs` SHOULD issue one `blocks` request naming every digest it still needs, rather than a `block` request per digest. The limit's default of 256 is a count of blocks, not of round trips, and it is only affordable as one exchange.
2. **A fetch that fails is not a unit of the limit.** The limit counts distinct foreign blocks *scanned* — fetched and read for the definitions they carry. A digest no source returned was not scanned and MUST NOT be counted, exactly as [05-processing-model.md](05-processing-model.md) says of a `refs` entry the node does not hold.
3. **A failed fetch is not an invalidity.** A block whose `refs` a client cannot obtain has not been shown invalid; the client has not been able to decide. The verdict it deserves is the *stored but unvalidated* status of [05-processing-model.md](05-processing-model.md), "Block reception" — but that status is defined there for a missing `prev` and not for an unfetchable `refs` entry, so the specification does not currently say. See todo 078; until it is settled, a client of this profile MUST NOT report a block as invalid on the strength of a fetch that failed, and SHOULD retry from another source.

#### Chain succession

After a node validates a rotation block it knows `new_pub` and nothing about where that chain is served ([05-processing-model.md](05-processing-model.md), "Chain succession"). A client of this profile SHOULD look for the successor chain at every source it already uses for the predecessor, starting with the one the rotation block came from, and SHOULD issue `siblings` at the successor's genesis position, because more than one genesis block claiming the same rotation is the ambiguous-succession condition and the node MUST surface it rather than pick.

That is a hosting convention and not a protocol fact: an author who rotates a key and changes hosts in the same week disappears from a client that only knows the old host. See todo 074.

### Server rules

#### What a conforming server serves

1. A server MUST serve, for every author chain it holds, **every block it holds of that chain, in chain order, from the genesis block to the tip it reports**, through `range`. Serving a chain means serving all of it; a server that answers `tip` for an author and cannot answer `range` from the genesis position for the same author is not conforming.
2. A server MUST NOT skip a block within a range, MUST NOT reorder one, and MUST NOT serve across a hole in its own store. Where it holds a gap it MUST end the response before the gap, so that the client's `prev` walk terminates cleanly and the client can tell "the source stops here" from "the chain ends here" by asking `tip`.
3. A server MUST retain and serve, as opaque bytes, every block of a chain it serves that it cannot read. A private block is opaque to a non-recipient by design ([02-block-format.md](02-block-format.md), "Private block"), and [05-processing-model.md](05-processing-model.md) permits a non-recipient node to *drop* private blocks it cannot decrypt — but that permission is about a node's own store. A server that drops them publishes a chain with a hole, and every block after the hole fails validation rule 3 at every client. Storage policy and serving policy are different policies; whether the specification proper should say so is open, see todo 071.
4. A server MUST serve `siblings` honestly: every block it holds at the named position, with no winner chosen.
5. A server MUST NOT require authentication or any client identifier for the five read operations. A deployment MAY put whatever it likes in front of these endpoints — a private server, an allowlist, a VPN — but a server that requires a client to identify itself makes that client's requests linkable into a durable identity, which is the opposite of what the subscription-privacy consideration asks for. See todo 073.
6. A server SHOULD be reachable over TLS, and MAY be reachable over plaintext HTTP. Neither changes what a client must verify.

#### What a server does not guarantee

These are stated rather than pretended, because a client that assumes them is wrong and a client that knows it is not:

- **Freshness.** Nothing in this profile distinguishes "this is the tip" from "this is the tip I am willing to show you". No block carries trustworthy time — timestamps are self-reported and untrusted ([02-block-format.md](02-block-format.md), Security Considerations) — and there is no signed object a server could produce to attest to a tip, because v1's block-type set has no room for one. A server that withholds the newest blocks is indistinguishable from an author who stopped publishing. See todo 075.
- **Completeness of a sibling set.** A one-member sibling set is what an honest server sends for an unforked chain and what a dishonest one sends for a forked chain. The client-side answer is the multi-source rule; there is no server-side one. See todo 070.
- **Existence.** A 404 is a fact about the server's store and about nothing else.
- **Order of arrival, delivery, or any liveness property at all.** A server is an untrusted mirror. The profile's guarantees are exactly the guarantees inside the blocks.

#### Resource limits

A server MUST bound what a request can cost it, and the bounds are policy, not protocol. It MUST accept a `blocks` request naming at least 256 digests, so that the scan limit's default fits in one exchange, and MAY reject a larger one with 413. It MAY cap `limit` on a range, `wait` on a long poll, the size of an announce body, the number of concurrent event streams, and the request rate, answering 413 or 429 as appropriate. A server MUST NOT respond to a bound being exceeded by serving a partial answer it presents as complete: a truncated range is a legitimate short response because ranges are contiguous prefixes; a truncated sibling set or a truncated block is not, and MUST be an error instead.

### What this profile leaves out

- **Peer and server discovery.** A client learns a base URL the way it learns a podcast feed's: somebody tells it. No DHT, no registry, no bootstrap list, no `.well-known` document.
- **NAT traversal, hole punching, direct peer connections.** A client talks to servers. A user who wants no third party runs a server on a machine they control, at a private address or an onion address, and the profile is unchanged — it is HTTP, and it does not care what the URL resolves to.
- **Authentication, accounts and sessions.** See server rule 5.
- **A head-only fetch.** [05-processing-model.md](05-processing-model.md)'s scan limit anticipates "a block a node fetches only to read its type or its author", and this profile offers no way to serve one, deliberately: a block's signature covers the whole block, so a plaintext head served alone is unverifiable, and acting on an unverifiable head would let a server induce a wrong rule-6 rejection of a valid block. Batch fetch is the answer to the round-trip problem instead. See todo 069.
- **Wrapped-key distribution.** Out of scope by [04-cryptography.md](04-cryptography.md), "Wrapped key format", and staying out. A transport profile MUST NOT become the envelope that carries content keys; that is a separate decision with its own document.
- **IPFS URI filler resolution.** A type-3 filler carries IPFS's own identifier ([03-encoding.md](03-encoding.md), "Internal references"). Fetching what it points at is IPFS's business.
- **Deletion, expiry, retention, quotas, payment.** Server policy. The protocol is append-only; a server's disk is not.
- **Set reconciliation.** A Dialog chain is totally ordered by `prev` with a single head, so reconciling one is: compare one digest; if it differs, ask for everything after the one you hold. That is one round trip and no algorithm. The range-based reconciliation protocols built for *unordered* sets solve a problem this data model does not have, and this profile deliberately does not reach for one.

### Open questions

This draft deliberately settles none of the following. Each has a todo, each is cited at the point above where it bites, and each would change normative text somewhere if it were settled the other way.

| Todo | Question | Where it bites |
|------|----------|----------------|
| [069](../todos/069-pending-p2-is-a-blocks-plaintext-head-separately-serveable.md) | Is a block's plaintext head separately serveable? | "What this profile leaves out"; the scan limit's head-fetch clause |
| [070](../todos/070-pending-p2-does-fork-detection-deserve-a-multi-source-obligation.md) | Does fork detection deserve a multi-source obligation in the specification proper? | "The multi-source rule"; `siblings`; completeness |
| [071](../todos/071-pending-p3-a-serving-nodes-obligations-versus-a-storing-nodes.md) | A serving node's obligations versus a storing node's | Server rule 3, opaque private blocks |
| [072](../todos/072-pending-p2-locating-a-foreign-block-from-a-digest-alone.md) | Locating a foreign block from a digest alone | `block`; resolution beyond one source's held set |
| [073](../todos/073-pending-p3-what-the-subscription-privacy-should-actually-demands.md) | What the subscription-privacy SHOULD actually demands | Server rule 5; the event stream; Security Considerations |
| [074](../todos/074-pending-p3-where-is-the-successor-chain-served.md) | Where is the successor chain served? | "Chain succession" |
| [075](../todos/075-pending-p3-freshness-has-no-signal.md) | Freshness has no signal | `tip`; "What a server does not guarantee" |
| [078](../todos/078-pending-p2-an-unfetchable-refs-entry-has-no-defined-verdict.md) | An unfetchable `refs` entry has no defined verdict | "Interaction with the scan limit", point 3 |

## Security Considerations

- **The transport carries no trust, and this is load-bearing.** Every guarantee a client gets comes from the blocks: the signature, the chain link and the content address are all inside the bytes. A client that skips verification because a response arrived over TLS from a server it operates has removed the only thing protecting it, and gained nothing, since the server it operates received those blocks from somewhere too.
- **Subscription leakage is real and this profile does not remove it.** A server asked for a chain knows someone wanted that chain, and the set of chains a node requests is a picture of what it follows. The mechanisms that would remove this leak — private information retrieval, mixnets — cost more than any v1 profile will pay, and nothing shipped in the surveyed field solves it. What the profile does instead: no authentication and no client identifier by default, so requests are not linkable into a session; immutable, cacheable block responses, so a shared cache absorbs part of the pattern; an explicit recommendation to partition chains across servers; and the event stream flagged as the one mode that hands a server the whole set at once. Two structural facts blunt it further and are worth stating: the requested set is a *superset* of the accepted set, because L1 pulls chains L3 filters out and demand-driven resolution fetches foreign blocks from authors nobody subscribed to, so an observer sees blockchain subscriptions and not author subscriptions; and subscriptions are local configuration, so nothing obliges a node to show one server the whole set. See todo 073 for what the specification's SHOULD should be turned into.
- **Lying by omission is the whole attack surface.** A source cannot forge a block, alter one, or attribute one falsely. It can withhold: a tip, so that a client believes an author went quiet; a branch, so that a client believes a forked chain is linear; a `refs` target, so that a client cannot finish validating a block that is in fact valid. Every one of those is invisible from a single source and detectable from two, which is the multi-source rule's entire justification.
- **The unverifiable head.** No client can verify that the block a server calls the tip is the author's latest. This is not a deficiency of the binding; there is nothing to verify against, because no block carries trustworthy time and no signed object in v1 attests to a chain's end. Applications MUST NOT treat "the tip I last fetched" as "the author's current state" for any decision where the difference matters, and MUST NOT infer from a static tip that an author has stopped publishing. See todos 069 and 075.
- **Resource exhaustion.** A `blocks` request naming a hundred thousand digests, a range with no limit over a chain of millions, a long poll held open by ten thousand clients, and an announce body of arbitrary size are all denial-of-service vectors that cost the attacker one request each. A server MUST bound each of them ("Resource limits"). A client is exposed symmetrically: a response body can be arbitrarily large, and a client MUST bound what it will read and MUST enforce the dCBOR decoding rules of [03-encoding.md](03-encoding.md) — including its nesting bound, which is what stops a run of one-byte array heads from driving a recursive decoder into a stack overflow — on every item of the sequence, since the bytes arrive from a hostile network by default.
- **A cache is a party to the exchange.** `immutable` on block responses is what makes foreign-block resolution affordable, and it also means an intermediary sees which blocks a population fetches. That is a smaller leak than a server-side request log and a different one, not an absent one.
- **An announce endpoint is a write endpoint.** A server accepting announces from anyone will accept storage from anyone. Validation bounds what can be stored to well-formed signed blocks, which is a real bound and not a quota; rate limiting and policy are the server's own.

## Examples

Real values throughout: the keys, digests and CIDs below are the grounding demo's committed chains, whose blocks live in `demo/chains/`.

| Author | Key (text form) | Blocks | Tip |
|--------|-----------------|--------|-----|
| atlas | `b5ua3y66r5fac352uhwvjhtqw6qyk4so2vt2bxqisck4avcl3lf3zt6i` | 6 | `bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e` |
| gazetteer | `b5uatugx2rd2fkyiw7rtrssojo2x4ochyic4d55fzjzmwwlpqz2pe4bi` | 4 | `bafyreifxee6srorhi7cj5nirr62276ixdkgt47sgh6sl4wv2vfrguwveku` |
| errata | `b5uawwbrvnfbfkgkhxsii52p4447rsxmm4lu65xfnkbuywk7wm3xqyhq` | 4 | `bafyreif3aq6agreuac7edhkd7omkpx7uqukwa4yt36fq6ckda2groln7qu` |

### A full sync session

A node with an empty store syncs all three chains from `https://mirror.example/dialog/v1`, validates them, and announces what it publishes itself. It has a second source configured, `https://second.example/dialog/v1`, per the multi-source rule.

```
1. Tip discovery — has this chain moved?

GET /dialog/v1/chains/b5ua3y66r5fac352uhwvjhtqw6qyk4so2vt2bxqisck4avcl3lf3zt6i/tip
Accept: application/dialog-blocks+cbor-seq

200 OK
Content-Type: application/dialog-blocks+cbor-seq
Content-Length: 334
ETag: "bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e"
Cache-Control: no-cache

<334 bytes: atlas's tip block>

The client hashes those 334 bytes, gets
bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e, and has
verified the ETag rather than believed it. It holds nothing yet, so
the tip differs from its position and there is a range to fetch.

2. Range fetch — from the genesis position, "after" omitted

GET /dialog/v1/chains/b5ua3y66r5fac352uhwvjhtqw6qyk4so2vt2bxqisck4avcl3lf3zt6i/blocks?limit=64
Accept: application/dialog-blocks+cbor-seq

200 OK
Content-Type: application/dialog-blocks+cbor-seq
Content-Length: 8661

<6 blocks concatenated: 395 + 965 + 1964 + 2855 + 2148 + 334 bytes>

3. Validation — the client's own work, on every block, in order

  block 0  prev = null                          genesis, rule 3's base case
  block 1  prev = digest(block 0)               matches the item before it
  block 2  prev = digest(block 1)
  block 3  prev = digest(block 2)
  block 4  prev = digest(block 3)
  block 5  prev = digest(block 4)  and hashes to the tip from step 1

  Each block is validated per 02-block-format.md — signature against pub,
  chain integrity against the block already accepted, reachability of every
  digest its operations name — before its operations reach L2. The range
  property is checked by the client, not asserted by the server: a skipped
  block would break the prev walk at the point of the skip.

4. The other two chains, the same way

GET /dialog/v1/chains/b5uatugx2rd2fkyiw7rtrssojo2x4ochyic4d55fzjzmwwlpqz2pe4bi/blocks
    → 3194 bytes, 4 blocks, ending at
      bafyreifxee6srorhi7cj5nirr62276ixdkgt47sgh6sl4wv2vfrguwveku

GET /dialog/v1/chains/b5uawwbrvnfbfkgkhxsii52p4447rsxmm4lu65xfnkbuywk7wm3xqyhq/blocks
    → 2605 bytes, 4 blocks, ending at
      bafyreif3aq6agreuac7edhkd7omkpx7uqukwa4yt36fq6ckda2groln7qu

  gazetteer's and errata's blocks name atlas's in refs, which the client
  already holds, so resolution costs no fetches. 14 blocks, three requests.

5. The second source — the multi-source rule

GET https://second.example/dialog/v1/chains/b5ua3y66…/tip

  Same tip: the two sources agree and nothing more is needed. A different
  tip means one of three things, and the client tells them apart by fetching
  the range from the second source too:
    - the second source is behind or ahead      (its range is a prefix or an
                                                 extension of the first's)
    - the second source has a different branch  (the prev walk diverges at
                                                 some position — a fork, and
                                                 rule 9 fires)
    - the second source is lying by omission    (indistinguishable from the
                                                 first case; see todo 075)

  The explicit sibling query asks the divergent position directly:

GET /dialog/v1/chains/b5ua3y66…/siblings?prev=bafyreihqcci23eexzfsuiph6gxjeler4mldggpuy5mqihp5hfjjez3gadu

  200 OK, one block from each source that holds one — two blocks in the
  client's store at one position, from one pub key, which is exactly the
  condition validation rule 9 names.

6. Steady state — polling, a few dozen bytes per chain

GET /dialog/v1/chains/b5ua3y66…/tip
If-None-Match: "bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e"

304 Not Modified

7. Publishing — this node's own new block

POST /dialog/v1/announce
Content-Type: application/dialog-blocks+cbor-seq

<1 block: the node's own tip, signed by its own key>

200 OK
Content-Type: application/json

{"accepted": ["bafyrei…"], "held": [], "rejected": {}}

  The server validated it before storing it, as it would a block from any
  other origin. Acceptance is not endorsement, and the receipt is a report,
  not a signature.
```

### Fetching a foreign block by digest

A node subscribed to `errata` alone validates errata's genesis block, `bafyreicrab2mvkd7nv4quzzp3dzibsgmohotvr3pgltpv5aum3vufyxsh4`. Its operations name entities defined elsewhere, and its `refs` list carries three 32-byte digests and nothing else — no author, no chain hint, no locator:

```
5693c25cc1b72b71fca62897d451d821a3e6dc4738c76c42d98d333a96d91fa9
302b0ea052802a0264a9e2cf9d0df2a0fd15bf41ca70956361af9e29567eeb9c
e89c73df95b9693ba87cb7dcdd0d29b8315b727c39d655556e1bf77ef8c543a4
```

Each becomes a CID by the fixed prefix of [03-encoding.md](03-encoding.md), and the three go out in one request rather than three, because the scan limit is a block count and not a round-trip budget:

```
POST /dialog/v1/blocks/fetch
Content-Type: application/json
Accept: application/dialog-blocks+cbor-seq

{"digests": [
  "bafyreicwspbfzqnxfny7zjris7kfdwbbuptnyrzyy5wefwmngm5jnwi7ve",
  "bafyreibqfmhkauuafibgjkpcz6oq34va7uk36qokockwgynptyuvm7xltq",
  "bafyreihitrz57fnzne52q7fx3toq2knygfnxe7bz2zkvk3q3657prrkduq"
]}

200 OK
Content-Type: application/dialog-blocks+cbor-seq
Content-Length: 4215

<3 blocks in the order requested: 395 + 965 + 2855 bytes>
```

The client re-hashes each block and matches it to the digest it asked for. It happens that all three are atlas's — blocks 0, 1 and 3 of that chain — but nothing in the request said so and nothing in the response says so either; the client learns the author by reading each block's `pub` field after it has verified the signature over it. Three distinct foreign blocks were scanned, so three units of the scan limit were spent, whatever the sequence's byte count.

A single block, when only one is needed, is a plain cacheable `GET`:

```
GET /dialog/v1/blocks/bafyreihitrz57fnzne52q7fx3toq2knygfnxe7bz2zkvk3q3657prrkduq
Accept: application/dialog-blocks+cbor-seq

200 OK
Content-Type: application/dialog-blocks+cbor-seq
Content-Length: 2855
Cache-Control: public, max-age=31536000, immutable
```

Had the server answered 404, the client would have learned that *this server does not hold that block* — not that the reference is unresolvable, not that errata's genesis block is invalid, and not that a unit of the scan limit was spent. It would try another source.

## References

### Normative
- [02-block-format.md](02-block-format.md) — Block structure, the ten validation rules, fork detection, block identification
- [03-encoding.md](03-encoding.md) — dCBOR, the CID text form, the author key text form, internal references
- [05-processing-model.md](05-processing-model.md) — Block reception, chain management, demand-driven resolution, the scan limit, chain succession
- [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119) — Key words for requirement levels
- [RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742) — CBOR Sequences, the framing (none) this profile's serialization uses, and the `+cbor-seq` structured syntax suffix
- [RFC 9110](https://datatracker.ietf.org/doc/html/rfc9110) — HTTP Semantics: methods, status codes, conditional requests
- [RFC 9111](https://datatracker.ietf.org/doc/html/rfc9111) — HTTP Caching, including the `immutable` response directive's home in [RFC 8246](https://datatracker.ietf.org/doc/html/rfc8246)
- [RFC 9457](https://datatracker.ietf.org/doc/html/rfc9457) — Problem Details for HTTP APIs, the shape of every error body
- [RFC 8610: CDDL](https://datatracker.ietf.org/doc/html/rfc8610) — Concise Data Definition Language

### Informative
- [00-overview.md](00-overview.md) — Scope, and the statement that transport is not part of it
- [04-cryptography.md](04-cryptography.md) — Why a block is self-authenticating, and why wrapped-key distribution stays out of this profile
- [Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) — WHATWG HTML Living Standard, the event stream the optional subscription mapping uses
- [AT Protocol sync](https://atproto.com/specs/sync) — the closest running prior art for a per-identity signed append-only log served over plain HTTP, and the source of this profile's shape
- [`docs/design/2026-08-18-transport-design.md`](../docs/design/2026-08-18-transport-design.md) — the design document this profile was drafted from: the requirements extracted from the specification, the survey of candidate substrates, and the reasoning for choosing HTTP
