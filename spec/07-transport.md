# Transport Profile

**Version:** <<VERSION>> | **Status:** Draft (optional profile)

## Abstract

This document defines an optional interoperability profile for moving Dialog blocks between nodes: one serialization that is both a wire body and a file, six abstract operations over it, and a binding of those operations to HTTP. It also states the client and server obligations that make the profile safe — chiefly that the transport carries no trust, and that a source can lie only by omission.

**Transport remains outside the protocol.** [00-overview.md](00-overview.md) lists transport among the things the protocol does not define, and that is unchanged: no block, no chain and no implementation is invalid for not speaking this profile, nothing here changes a byte of the wire format, and no block's validity depends on any transport being reachable. File-based exchange — the serialization below, at rest, on a disk or a USB stick or an email attachment — is a complete conforming transport. This document exists so that two implementations that *do* want to talk over a network have one thing they can both speak, and so that a future profile over some other substrate has the same six questions to answer.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", "MAY" and "OPTIONAL" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

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

A client learns whether a range ended at the tip by comparing the last block it received with the tip the source reports (`tip`, or the tip statement carried alongside a range response in a binding that has one). That comparison tells the client where the *source* says the chain ends. It is not evidence about where the chain actually ends — see "What a server does not guarantee".

#### As a file

A block sequence written to a file is a chain file. Nothing is added and nothing is removed: a range response saved to disk is a valid chain file, and a chain file offered to a server is a valid announce body. The conventional extension is `.dialog`; a file holding exactly one block MAY use `.block`, which is the same thing at length one.

This is the property that keeps offline exchange from being a parallel mechanism. The grounding demo's committed chain directory is the degenerate case rather than an exception to it: each `.block` file is a one-block sequence, and concatenating one author's block files in the order the directory's index lists them yields, byte for byte, the range response for that author's whole chain from the genesis position. That equality is a test rather than a claim — `demo/internal/replay`'s `TestRangeResponseIsTheConcatenatedBlockFiles` serves the committed directory and compares the bytes. The index beside them is a local convenience — it carries no authority, and a reader that trusted it would still have to validate every block (see [02-block-format.md](02-block-format.md), "Validation").

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
- A **conforming server** MUST implement `tip`, `range`, `block`, `blocks` and `siblings`. `announce` is OPTIONAL: a read-only mirror is conforming.
- **An OPTIONAL operation a server does not offer answers 404.** That covers `announce` on a read-only mirror and the event stream of "Subscription mapping", which is the other optional thing with a path of its own. A client reads such a 404 the way it reads every other one in this profile — a fact about this source, and never a statement that the operation does not exist. This profile has no discovery document by design ("What this profile leaves out"), so the status code is the only way a client learns that an optional operation is absent; servers SHOULD therefore distinguish the two kinds of 404 by problem type (see "Status codes").

#### tip

*Request:* an author key. *Response:* one block — the **tip**, as defined below — or "I do not have it" when the source holds no tip for that author.

The tip is defined **constructively**, as the end of a walk through the source's own store: begin at the genesis position, take the block the source holds there, then the block whose `prev` names that block, and continue. The tip is the last block the walk reaches. The walk crosses every block the source holds, whatever validation verdict it has reached about any of them — it is a claim about connectivity and not about validity (server rule 7). Where the source holds more than one block at a position, the walk follows the branch that source serves (below). Two consequences, which are the first two server rules of "What a conforming server serves" rather than separate obligations:

- A source that holds no block at the genesis position holds **no tip** for that author, whatever else of that chain it holds, and answers "I do not have it".
- A source whose store has a **hole** — a position it holds no block at, with blocks of that chain beyond it — has as its tip the last block the walk reaches before the hole, and serves a `range` that ends at the same block.

*Informative.* A hole is the server's problem to fix rather than something this profile gives it a way to describe: it fetches the missing block, and the walk then goes further and the tip moves on its own. Until then a store holding blocks 3, 4 and 5 of a chain whose first three it never received reports no tip and serves an empty range, while still serving those three blocks by digest — `block` and `blocks` make no claim about a chain, and a store with a hole can answer them honestly. The alternative definition — the tip is any block the store holds that nothing else names as a predecessor — is the one a store's own index answers cheaply, and it is the one that makes a server report a tip it cannot serve a `range` to, which server rule 1 refuses.

The response is the **block itself**, not a statement of its digest. A client computes the digest and the CID from the bytes it received, which means the source cannot misreport the tip's identity; it can only choose which tip to show, which is the freshness gap and is not fixable here (see "What a server does not guarantee").

A source that holds a fork has more than one candidate at a position, so the walk above needs a branch. The source chooses, and the choice MUST be **deterministic and stable per author**: for as long as the source holds the same blocks of that chain, every `tip` and every `range` it answers MUST follow the same branch. A source choosing per request would satisfy each sentence of this profile read alone and be useless, because a client's repeated requests would then name no single chain. `tip` and `range` MUST agree on the branch, which they do by construction when the tip is the end of the walk `range` performs. The source MUST also answer `siblings` honestly about the divergence; a client that cares about forks does not learn about them from `tip`.

*The reference rule, informative.* Take the block with the lowest digest, comparing bytewise — the same order `siblings` is sorted in. It is a function of the blocks alone, so it is stable across requests, across restarts and across two sources holding the same blocks, and it costs no stored state. A source MAY choose on any other ground; what is normative is determinism and stability, not this rule.

#### range

*Request:* an author key, a position, and an optional maximum number of blocks. *Response:* a block sequence in chain order, beginning with the block whose `prev` names the requested position and continuing contiguously.

The position is **exclusive**: the block naming it is not in the response, the blocks after it are. The genesis position requests the chain from its genesis block.

A source MUST NOT return more blocks than the requested maximum. It MAY return fewer, for any reason, including one of its own. If it holds at least one block at the requested position it MUST return at least one block. If it holds none, the response is an empty sequence — which is the answer both when the client is already at the tip and when the source's store stops there, and those two are distinguished by comparing against `tip`, not by the emptiness. A third case hides behind the same emptiness and is the important one: a source that reports a tip the client does not hold, and no blocks after the position the client asked from, is serving a chain the client's position is not on. See "Pursuing an advertised tip".

If the source holds more than one block at the requested position — a fork — it MUST answer `range` along one branch only, consistently with what its `tip` reports and with the same deterministic, per-author-stable choice `tip` makes (see "tip"), and MUST NOT interleave branches. Untangling a fork is `siblings`' job.

#### block

*Request:* one block digest. *Response:* that block, or "I do not have it".

This is the primitive that [05-processing-model.md](05-processing-model.md)'s "Resolution procedure" step 4 needs. The request carries a digest and nothing else, because a `refs` entry carries a digest and nothing else: no author, no chain, no locator (see [03-encoding.md](03-encoding.md), "Internal references"). A source can therefore answer only for blocks in its own store, and a client that must resolve a reference no source it knows holds has an open problem, not a protocol error — see todo 072.

#### blocks

*Request:* a list of block digests. *Response:* a block sequence holding the subset the source holds, in the order requested.

`blocks` exists because `block` does not scale to the validation path. The scan limit of [05-processing-model.md](05-processing-model.md) counts distinct foreign blocks scanned per block validated and defaults to 256, so a worst-case honest validation is 256 resolutions; done one round trip at a time over a network, that is a latency budget nobody can pay. A conforming server MUST accept a request naming at least 256 digests, so that the whole budget fits in one exchange.

A request MUST NOT name the same digest twice; a source MAY reject such a request or MAY answer it as though the duplicate were absent. Digests the source does not hold are simply not in the response, and the response says nothing about why.

#### siblings

*Request:* an author key and a position. *Response:* every block the source holds signed by that key whose `prev` names that position, ordered by ascending digest.

This is the operation that gives [02-block-format.md](02-block-format.md)'s validation rule 9 something to fire on. A source MUST include every such block it holds — including the one it would itself serve from `range` and `tip`, so that the client sees a set rather than a difference, and including any it holds as *stored but unvalidated* (server rule 7), since a block a source cannot yet judge is exactly the one whose omission would hide a fork — and MUST NOT choose a winner. The genesis position asks the question for the start of the chain, which is how the ambiguous-succession condition of [02-block-format.md](02-block-format.md), "rotate_key", is detected: two genesis blocks referencing the same rotation block are two members of the genesis position's sibling set.

A response with one member is not a statement that the chain does not fork. It is a statement that this source is not showing more than one block there, which a source serving one side of a fork has every incentive to do. `siblings` is a convenience for the honest case; the mechanism that actually detects forks is the multi-source rule below.

#### announce

*Request:* a block sequence. *Response:* a statement of what the source did with each block.

The source MUST validate every block per [02-block-format.md](02-block-format.md) before storing it, exactly as it would a block from any other origin, and MUST NOT store as valid a block whose predecessor it does not hold and has not validated — such a block is *stored but unvalidated* or discarded, per [05-processing-model.md](05-processing-model.md), "Block reception". A source MAY refuse an announce entirely, for reasons that are its own policy: quota, rate, acquaintance, disk.

**A refusal by policy is 403**, with the problem type `urn:dialog:problem:announce-refused` (see "Status codes"), and it carries **no receipt**: nothing was judged, so there are no dispositions to report, and a source that answered 200 with every block `rejected` would be reporting a verdict it never reached. A client reads a 403 as a fact about this source and about nothing else — the blocks are not implicated, another source may take them, and this one may take them later, so the announce is worth retrying elsewhere and, after whatever policy provoked it has changed, here. It is distinct from the 404 a server that does not implement `announce` at all answers (`urn:dialog:problem:operation-not-offered`, "The six operations"), which is a fact about the server rather than about the request, and which asking again will not change.

**A disposition is decided after the whole sequence.** A source MUST determine each block's disposition once it has processed the entire announce, and not as each block is offered. A block settled by a later block of the same sequence — a definition the announce also carries, a predecessor announced after the block that names it in another author's interleaving — is therefore reported by its final state, `accepted` rather than `held`.

Two things follow, and they are why the choice is normative. The receipt then describes the state the announcer's next request will meet, rather than one the source has already moved past; and announcing the same sequence twice produces the same receipt, which a block-by-block reading does not guarantee. The case is not a corner one, because a held block is settled by the *arrival* of what it was waiting for ([05-processing-model.md](05-processing-model.md), "Block reception", "Revalidation on arrival"), which may happen while the source is still reading the same request. A source that cannot answer either way in time answers 202 instead, whose receipt is incomplete or absent by definition.

`announce` carries no authority in either direction. The announcer is not asserting anything a block does not already say, and the source is not endorsing anything by accepting one. It is the same bytes moving the other way.

### HTTP binding

A server is named by a **base URL**, learned out of band. Every path below is relative to it. The default prefix is `/dialog/v1`; a server MAY be mounted at any base URL, and a client is configured with the whole base URL rather than with a host. The `v1` names the version of *this profile*, not the protocol version carried in a block's `v` field.

`{author}` is an author's Ed25519 public key in the canonical text form of [03-encoding.md](03-encoding.md), "Text representation of author keys": multibase base32, 56 characters, always beginning `b5ua`. `{cid}` is a block's CID in the base32 text form of [03-encoding.md](03-encoding.md), "Content identifiers (CIDs)" → "Text representation": 59 characters, always beginning `bafyrei`. Both are case-sensitive, and a server MUST reject any other spelling of either with 400 rather than normalizing it — the two forms are canonical in both directions, and a server that accepted a variant would be minting aliases (see [03-encoding.md](03-encoding.md), Security Considerations).

**One spelling means one byte sequence, and percent-encoding is a second spelling.** The path segments and query values this profile defines are drawn from alphabets that need no percent-encoding at all — base32 for both text forms, ASCII digits for `limit` — so a request target that percent-encodes any octet of one is malformed and MUST be rejected with 400. A server applies the canonical-form rules to the request target **as received**, not to a percent-decoded copy of it. This is the same rule as the paragraph above rather than a new one: `%62afyrei…` and `bafyrei…` are two byte strings naming one immutable resource, which is the alias the canonical text forms exist to prevent and which a cache keys twice. The cost is stated rather than hidden — an intermediary that re-encodes a request target turns a valid request into a 400 the user cannot see — and it is accepted, because the alternative is that every block resource acquires an unbounded set of URLs.

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
- `limit` is a positive decimal integer, and has exactly one spelling: one or more ASCII digits, the first of which is not `0`, with no sign, no decimal point and no whitespace. `01`, `+1`, `1.0` and `1e3` are malformed and MUST be rejected with 400, and so is `%201` — twice over, by the whitespace rule and by the percent-encoding rule above, which is the general form of what this bullet used to say about `limit` alone. A server MAY cap the value it honours and MUST NOT exceed its own cap, and MAY reject with 400 a value too large to be a plausible count of blocks. Absent, the server chooses.
- **A query parameter this profile defines for the operation being invoked, given more than once, is malformed** and MUST be rejected with 400. `after` twice is two positions and this profile does not say which would win; `prev` and `limit` are refused on the same ground, and a server that picked one of the values would be defining a rule that is written nowhere.
- **A query parameter this profile does not define for that operation MUST be ignored**, whether it appears once or many times. That covers a tracking parameter an intermediary appended, a parameter a later version of this profile defines, and a parameter of another operation. It is what makes the long poll's degradation work rather than an exception to it — "a server that does not implement it MUST ignore the parameter and answer immediately" ("Subscription mapping") is this rule applied to `wait`, and `wait=5&wait=6` on a server that does not implement long polling is ignored twice and is not a 400. It is also what lets a later parameter be added without breaking every server written against this version, which is the whole reason this profile does not reject the unknown. The cost is real and is accepted: a client that sends `prev` to `range`, or `after` to `siblings`, is answered from the genesis position with no signal that its parameter went nowhere. No block is misidentified by that — the client verifies everything it receives ("Verification obligations") — and forward compatibility is worth more than catching the mistake.
- `HEAD` MUST be supported wherever `GET` is. Any other method on a defined path MUST return 405 with an `Allow` header.

A 200 response to `tip` or `range` MUST carry a `Dialog-Tip` header whose value is the CID text form of the tip the server holds for that author at the moment of the response, as `tip` above defines a tip:

```
Dialog-Tip: bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e
```

The header is defined for those two operations and for no other. `block`, `blocks` and `siblings` answer about blocks rather than about where a chain ends, and a server MAY send the header there but a client MUST NOT expect it.

**Where the server holds no tip.** A server holds no tip for an author whose chain it cannot walk from the genesis position — it holds nothing of that chain, or nothing at its start (see `tip`). Then:

- `tip` answers **404**: the author is unknown to this source, in the same sense as every other 404 in this profile, and the response carries no `Dialog-Tip`.
- `range` answers 200 with an empty sequence, as "range" requires, and **omits the header**. It is omitted rather than given some empty or null value, because the header's value is a CID text form and this profile mints no second spelling of a position — an empty value, the literal `null` and a zero CID would each be one, which is exactly what `after=null` is refused for.

A client MUST treat an absent `Dialog-Tip` as "this source claims no tip for this author", and MUST NOT treat it as an error or as a protocol violation. A client that needs to distinguish that from a server which does not implement the header at all asks `tip`, which answers 404 in the first case and a block in the second; the information is one request away.

A 304 response to `tip` SHOULD carry the header alongside the `ETag`, and a client MUST NOT require it there: a 304 says the value the client already holds is current, which is the whole answer.

The header is what lets a client tell a range that ended at the tip from one the server truncated, without a second request per page: when the last block of a range hashes to the `Dialog-Tip` value, the client is caught up as far as this server goes. The header is a **claim, not evidence** — a server that withholds its newest blocks reports the older tip here too, and nothing detects that (see "What a server does not guarantee" and todo 075). A client MUST NOT act on the value except to decide whether to ask for more; the identity of every block it stores comes from re-hashing the block.

#### Bodies and content types

| Body | Media type |
|------|------------|
| A block sequence | `application/dialog-blocks+cbor-seq` |
| A `blocks` request | `application/json` |
| An `announce` receipt | `application/json` |
| Any error | `application/problem+json` ([RFC 9457](https://datatracker.ietf.org/doc/html/rfc9457)) |

The block media type uses the `+cbor-seq` structured syntax suffix registered by [RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742). A client SHOULD send `Accept: application/dialog-blocks+cbor-seq` and MUST accept `application/cbor-seq` as an equivalent, since a plain file server offering a directory of chain files will send the generic type and its bytes are the same bytes. A server MUST NOT serve a block sequence under any other type.

**The equivalence holds in both directions.** An `announce` request body is a block sequence, and its `Content-Type` MUST be `application/dialog-blocks+cbor-seq` or `application/cbor-seq`; a server MUST reject any other type, and a missing one, with 415. Admitting the generic type is what makes "a chain file offered to a server is a valid announce body" ("As a file") true of a file whose type came from a file server rather than from a Dialog client, and it confuses nothing: the two types are the same bytes, the sequence carries no metadata, and a server that accepted both cannot be uncertain about what it received.

**`Accept` is not evaluated on `announce`.** The operation's only response bodies are JSON — a receipt or a problem document — and the server produces them whatever the request asked for, so there is nothing for a 406 to protect. A client that speaks this profile carries `Accept: application/dialog-blocks+cbor-seq` in its standing headers, and a server enforcing 406 uniformly would refuse writes it would otherwise take, over a header naming a type the response was never going to have. 406 is defined for the five read operations.

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

`accepted` are blocks the server validated and stored; `held` are blocks it is keeping as *stored but unvalidated* pending their ancestry; `rejected` names each block it refused and why, in prose meant for a person. Every submitted block MUST appear in exactly one of the three, and a server that already held a block reports it as `accepted`. All three describe the source's state after the whole sequence has been processed, not as each block was offered (see "announce").

#### Status codes

| Code | Meaning in this profile |
|------|-------------------------|
| 200 | Here is the answer. For a range or a sibling set the answer may be an empty sequence, and an empty range from a source holding no tip for that author carries no `Dialog-Tip`. |
| 202 | The announce was taken for later processing; the receipt is incomplete or absent. |
| 304 | The `tip` is unchanged from the `If-None-Match` the client sent. |
| 400 | The request was malformed: a bad author key, a bad CID, a non-canonical spelling of either (including a percent-encoded one), a bad `limit`, a parameter this operation defines given more than once. |
| 404 | **I do not have it.** Never "it does not exist." A `tip` for an author this source holds no tip for; a `block` it does not hold; the path of an OPTIONAL operation this server does not offer. |
| 405 | Wrong method for a defined path. |
| 403 | **This source refuses this announce by its own policy** — quota, acquaintance, or any other ground of its own. Defined for `announce` and for no other operation. |
| 406 | The client's `Accept` excludes the only type this server can send. Defined for the five read operations; `Accept` is not evaluated on `announce`. |
| 413 | The announce or fetch body is larger than this server accepts. |
| 415 | The request body's media type is not one the operation admits: an `announce` body under anything but the two block-sequence types, or under none. |
| 429 | Rate limited; `Retry-After` SHOULD be present. |
| 503 | Temporarily unable; `Retry-After` SHOULD be present. |

404 carries the whole weight of the completeness gap, and its natural HTTP reading is the wrong one, so it is written down: a source's absence of a block is a fact about the source. It says nothing about whether the block exists, whether the author published it, or whether some other source has it.

Error bodies are RFC 9457 problem details. The `title` and `detail` members are for people; a client MUST branch on the status code and MUST NOT parse `detail`.

This profile defines three problem types. Two of them are 404s, because 404 carries two different facts and a client's next move differs between them; the third distinguishes a refusal of an announce from every other reason a write can fail:

| `type` | What it says |
|--------|--------------|
| `urn:dialog:problem:not-held` | This source does not hold what was asked for: the block, or a tip for that author. Another source may hold it, and this one may hold it later. |
| `urn:dialog:problem:operation-not-offered` | This server does not implement the OPTIONAL operation at that path. Asking again, or asking with other arguments, will not change the answer; another server may offer it. |
| `urn:dialog:problem:announce-refused` | 403. This source takes announces and refuses **this** one, by a policy of its own. Nothing was judged and nothing was stored; the blocks are not implicated. Another source may take them, and this one may take them later. |

A server SHOULD send the applicable type on a 404, and on a 403 refusal of an announce, and MAY send `about:blank`, which [RFC 9457](https://datatracker.ietf.org/doc/html/rfc9457) defines as the status code and nothing more; a client MUST still branch on the status code, and MUST work against a server that sends none of the three types. Every other error in this profile uses `about:blank`: the status code already says the whole of it.

The three are URNs rather than URLs because they are identifiers and not links. This profile publishes no document at them, a client MUST NOT dereference them, and a URL would tie a protocol identifier to a hostname somebody has to keep answering.

#### Caching

- `GET /dialog/v1/blocks/{cid}` is immutable: the response for a given CID can never change, because the CID is the hash of the response. A server MUST send `Cache-Control: public, max-age=31536000, immutable`. This is not a performance note — it means a CDN, a corporate proxy or a local cache can serve foreign-block resolution, which is exactly the traffic the scan limit makes hot, and it means those caches absorb part of the request-pattern leak for free.
- `GET .../tip` MUST carry a strong `ETag` whose value is the tip block's CID, and SHOULD carry `Cache-Control: no-cache` so that a cache revalidates rather than answers. `If-None-Match` on this endpoint is how a client polls a chain for a few dozen bytes.
- Range and sibling responses are not immutable — a source's store grows — and a server SHOULD serve them with `Cache-Control: no-cache` or a short `max-age`.

#### Subscription mapping

An L1 blockchain subscription (see [05-processing-model.md](05-processing-model.md), "Chain management") becomes one of three things, in increasing order of server cooperation and decreasing order of privacy. All three are mappings of the `tip` operation; none is a seventh operation.

1. **Polling `tip` with `If-None-Match`.** A 304 is a few dozen bytes. Every conforming client and server supports this, and it needs no server feature beyond a correct `ETag`. It is the baseline.
2. **Long-polling: `GET .../tip?wait={seconds}`.** The server holds the request open until the tip differs from the client's `If-None-Match` or `wait` seconds elapse, then answers 200 or 304. A server MAY cap `wait` and MUST answer within its own cap. OPTIONAL; a server that does not implement it MUST ignore the parameter and answer immediately, which degrades to polling.
3. **A tip event stream: `GET /dialog/v1/events?author={author}&author={author}`**, as server-sent events, emitting one event per chain movement whose data is a JSON object `{"author": "b5ua…", "tip": "bafyrei…"}`. One connection covers many chains. OPTIONAL; a server that does not implement it answers 404 for the path, like any other operation it does not offer (see "The six operations").

The event stream is the only part of this profile that hands one server a client's whole subscription set in a single, durable, correlated act, which is precisely the leak [05-processing-model.md](05-processing-model.md)'s Security Considerations warn about. It is specified as a convenience with a stated cost, and it is not the recommended mode.

**Webhooks are deliberately absent.** They require the client to be reachable, they need a registration and authentication surface, and the registration is a durable server-side record of exactly what a user follows. Each of those is a cost this profile is built to avoid.

### Client rules

#### Verification obligations

A client MUST validate every block it receives, from every source, per [02-block-format.md](02-block-format.md), before the block is stored or its operations reach L2 — the ordinary "Block reception" procedure of [05-processing-model.md](05-processing-model.md), with no step removed because the bytes arrived over a network. In particular:

1. A client MUST re-hash every block it receives and MUST identify it by the digest it computes, never by the position the block held in a sequence, never by the URL it was fetched from, and never by anything a source said about it. A `block` response whose bytes hash to something other than the requested digest is a failed fetch, not a block.
2. A client MUST verify the range property of a range response for itself, by checking that each block's `prev` names the block before it and that the first block's `prev` names the position it asked about. A source that skips a block produces a break the client sees immediately; *within* a range, completeness is free.
3. A client MUST NOT treat transport-level authenticity as validation. TLS protects the request pattern, not the data. A plaintext mirror is a downgrade in privacy and not in integrity.
4. A client MUST NOT let a source's answer decide a validation outcome that the source's bytes do not compel. In particular, a 404 for a `refs` entry is a fetch that did not succeed; it is not a finding that the reference is unresolvable. The block that named it is stored but unvalidated, not invalid ([05-processing-model.md](05-processing-model.md), "Block reception", "Absence is not evidence"; [02-block-format.md](02-block-format.md), validation rule 4).

#### The multi-source rule

**A node SHOULD obtain each chain it follows from more than one source, and compare.**

A source is anything: two servers, a server and a file, a server and a friend's disk. This rule is the part of the profile that does the most work, and it needs no wire support at all — every response is self-verifying and stateless, so "ask two sources" is the same code twice.

The reason is that **fork detection is a reachability property, not a query.** [02-block-format.md](02-block-format.md)'s validation rule 9 requires a node to detect a fork when it *holds* two blocks with the same `prev` from the same author. A node that only ever hears one version of a chain, from one source, satisfies rule 9 vacuously and forever: the rule is normative, and whether it can ever fire is a property of the transport. The `siblings` operation helps only where the source is honest, and a source serving one branch of a fork has every incentive not to be. Two sources with different branches produce a fork at the client even when neither admits to one.

The rule also blunts the subscription leak: a node that splits twenty chains across four servers hands no single party the full set.

Comparing is not a state of mind, and the shape it takes on the wire is written down: "First contact with a source" is where the comparison starts, and "Pursuing an advertised tip" is what a client does with the answer a second source actually gives it.

Whether the specification proper should carry this obligation, rather than an optional profile, is open — see todo 070.

#### First contact with a source

Every `range` names a position, and positions are per source: the operation is exclusive and stateless, so "where did we get to" has an answer per source rather than per chain, and a position one source holds may be one another has never heard of. For a source this client has already synced this chain from, the position is where that source's last response ended. For a source it has not, there are two answers and this profile permits both.

**A client asking a source it has not previously synced a chain from SHOULD ask from the genesis position, and MAY ask from the position its own copy of that chain reaches. Either way it SHOULD record which of the two it asked from**, because the two are not the same exchange and the difference shows in everything downstream: which blocks arrived, how many bytes it cost, and which mechanism found a divergence.

From the **genesis position** the source serves its chain from the beginning. The shared prefix is re-sent — the cost, and it grows with the length of the chain rather than with the size of the disagreement — and in exchange every block that source holds arrives in the range, so a divergence arrives as blocks and validation rule 9 fires on the store as they are stored ([02-block-format.md](02-block-format.md)). The request asks nothing of the client's own state, which makes it the same request whatever branch the client is on and leaves the simplest audit trail: what this source served, in full, in one sequence.

From the **held position** the client asks for nothing it already holds, which is the cheapest question available and the only one whose cost is the distance between the two branches. What it gives up is that a source on another branch then answers an empty range and a tip the client cannot reach, and the divergence is found by "Pursuing an advertised tip" below and by nothing else. That is not a weakness of the choice — it is the case the pursuit is written for, and that section calls it the *normal* answer a second source gives about a forked chain — but it does mean the pursuit's obligation is not optional in effect for such a client: skip it and the fork is not found at all. Note also that a client asking from its held position asks about a branch it chose, and a source that has never heard of that block answers the empty range whether it is on another branch or merely behind. The two are indistinguishable, exactly as a source that is behind and one that is withholding are, and each costs a pursuit.

Neither choice is a conformance question and neither weakens the multi-source rule: both find every fork the sources between them expose, which is why the SHOULD here is a default for a client that must choose without knowing which case it is in, and not a rule about what may be asked.

*Informative.* Both reference implementations expose the choice per run and default to the genesis position, and the interop harness runs its scenarios both ways. Over the same servers the two produce the same store, the same chains, the same forks and the same verdicts; the only difference in the summary is whether a pursuit happened.

#### Pursuing an advertised tip

A client that has synced a source's range and finds that source's tip — the `Dialog-Tip` of the last range response, or the block `tip` answers with — naming a block it **does not hold** has learned something specific, and it is not "nothing new here". The source holds a tip, so its store does not simply stop; the client is not at that tip, so it is not caught up; and the range after the position the client asked from was empty, so the source's walk does not pass through that position at all. What is left is that the source serves a chain the client's position is not on: a branch, or a chain the client holds a different version of.

A client in that case MUST NOT treat the empty range as "no new blocks". It MUST **pursue the advertised tip**:

1. Fetch the block the tip names, **by digest**, from that source (`block`, or `blocks` for a batch of them).
2. Verify it as every other received block is verified ("Verification obligations") — in particular, re-hash it, and treat bytes that hash to anything but the digest asked for as a failed fetch and not as a block.
3. Read its `prev`. If the client holds that block, stop. If not, fetch that block by digest from the same source and repeat from step 2, walking backward one block at a time.
4. Stop when the client reaches a block it holds; when the block the walk reaches is a **genesis block**, which has no predecessor to ask for; when a fetch fails; or when the client's own bound on the walk's length is reached.

**The walk ends in one of three outcomes, and a client that reports the end names it by one of them**: *held*, the first stop condition; *genesis*, the second; and *failed*, the third and the fourth together, since the client's own bound is reached in the same way a fetch is not answered — with no block at the end of it. A client MAY report a failure's kind more precisely (a source that would not serve the block, bytes that hashed wrong, the bound), and MUST NOT collapse *genesis* into either of the other two: it is not a failure, because every fetch succeeded and every block verified, and it is not *held*, because no block the client holds was met.

**A walk that ends at a genesis block has found that this source's chain shares no block with the client's.** Two chains claim the author and they have nothing in common — the most fundamental fork there is, and the one this profile has to be able to see. The blocks the walk fetched are stored and validated like any others, and the client then holds two genesis blocks for one `pub` key: two distinct blocks claiming the same (empty) position, which is exactly the condition [02-block-format.md](02-block-format.md)'s validation rule 9 names, and which that document already treats as a fork in the strict sense (see "rotate_key", where the ambiguous succession of two genesis blocks referencing one rotation block is called out as such). The client MUST surface it as rule 9 requires, with the two genesis blocks as the sibling pair, and `siblings` at the genesis position is where the set is asked for directly. No verdict about either chain follows from the walk's end itself, exactly as none follows from reaching a held block.

**The walk MUST be bounded**, by a limit the client chooses. It is a chain of the source's choosing, of a length the source controls, and every other resource bound in this profile is the client's own for the same reason ("Resource limits", and the client's symmetric exposure in Security Considerations).

**Reaching a block the client holds is the point of the exercise.** At that position the client then holds two blocks with the same `prev` from the same author — the one it already had, and the one the backward walk arrived from — which is exactly the condition [02-block-format.md](02-block-format.md)'s validation rule 9 names. The client MUST surface the fork as that rule requires. Nothing about the pursuit is a special case of fork detection: the blocks are in the client's store, validated on arrival like any others, and rule 9 fires on the store rather than on the transport.

**A walk that fails ends in fetches that did not succeed, and in nothing else.** A 404, bytes that hash wrong, or the client's own bound reached are each a *failed fetch*, recorded as such: they are not evidence of a fork, not evidence of an invalidity, and not evidence that the source lied, because a source advertising a tip it will not serve is indistinguishable from one that lost it (the freshness gap; see "What a server does not guarantee"). No verdict about any block follows from a failed pursuit, exactly as no verdict follows from a `refs` entry no source returned ("Interaction with the scan limit", point 3).

This is the completion of the multi-source rule. That rule says "obtain each chain from more than one source, and compare"; this is what the comparison consists of at the moment it matters, because an empty range and an unreachable tip is the *normal* answer a second source gives about a forked chain. A client that walks away from it detects no fork at all against two sources that each serve one branch honestly — it satisfies validation rule 9 vacuously, which is the failure mode the multi-source rule exists to prevent — and so a client that skips the pursuit does not satisfy that rule's SHOULD.

*Informative.* Two other moves reach the same divergence, and a client MAY use either. Re-issuing the range from the genesis position costs one request and re-downloads the shared prefix; it delivers the divergent blocks too, so rule 9 fires on it as well. A binary search with `limit=1` over the positions of the client's own chain *locates* the divergence in logarithmically many requests but delivers nothing, so it must still be followed by a fetch — of the divergent block, or of `siblings` at the position it found. The backward walk is the one written as the obligation because its cost is the distance between the two branches rather than the length of the chain, and because it is the only one that asks for nothing the client already holds.

#### Interaction with the scan limit

Demand-driven resolution is on the validation path and is bounded (see [05-processing-model.md](05-processing-model.md), "Scan limit"). Three consequences for a client of this profile:

1. **Batch.** A client resolving a block's `refs` SHOULD issue one `blocks` request naming every digest it still needs, rather than a `block` request per digest. The limit's default of 256 is a count of blocks, not of round trips, and it is only affordable as one exchange.
2. **A fetch that fails is not a unit of the limit.** The limit counts distinct foreign blocks *scanned* — fetched and read for the definitions they carry. A digest no source returned was not scanned and MUST NOT be counted, exactly as [05-processing-model.md](05-processing-model.md) says of a `refs` entry the node does not hold.
3. **A failed fetch is not an invalidity.** A block whose `refs` a client cannot obtain has not been shown invalid; the client has not been able to decide. Its verdict is the *stored but unvalidated* status of [05-processing-model.md](05-processing-model.md), "Block reception", which covers an unobtainable `refs` entry exactly as it covers a missing `prev` — the third outcome of validation rule 4 ([02-block-format.md](02-block-format.md), "Validation"). A client MUST NOT report a block as invalid on the strength of a fetch that failed, MAY revalidate the block when the missing one arrives, and SHOULD retry from another source. The specification-level rule is the load-bearing one: it is what keeps a source that withholds a foreign block from invalidating a valid block at every client, whether or not the client speaks this profile.

#### Chain succession

After a node validates a rotation block it knows `new_pub` and nothing about where that chain is served ([05-processing-model.md](05-processing-model.md), "Chain succession"). A client of this profile SHOULD look for the successor chain at every source it already uses for the predecessor, starting with the one the rotation block came from, and SHOULD issue `siblings` at the successor's genesis position, because more than one genesis block claiming the same rotation is the ambiguous-succession condition and the node MUST surface it rather than pick.

That is a hosting convention and not a protocol fact: an author who rotates a key and changes hosts in the same week disappears from a client that only knows the old host. See todo 074.

### Server rules

#### What a conforming server serves

1. A server MUST serve, for every author chain it holds, **every block it holds of that chain, in chain order, from the genesis block to the tip it reports**, through `range`. Serving a chain means serving all of it; a server that answers `tip` for an author and cannot answer `range` from the genesis position for the same author is not conforming.
2. A server MUST NOT skip a block within a range, MUST NOT reorder one, and MUST NOT serve across a hole in its own store. Where it holds a gap it MUST end the response before the gap, so that the client's `prev` walk terminates cleanly and the client can tell "the source stops here" from "the chain ends here" by asking `tip`.

   Rules 1 and 2 are consequences of the constructive definition of a tip ("tip" above) rather than obligations a server has to check for itself: a server whose tip is the end of the forward walk its `range` performs cannot report a tip it is unable to serve a range to, and cannot serve across a hole, because the walk stops at the hole in both answers. A server that computes its tip some other way owes both rules separately.
3. A server MUST retain and serve, as opaque bytes, every block of a chain it serves that it cannot read. A private block is opaque to a non-recipient by design ([02-block-format.md](02-block-format.md), "Private block"), and [05-processing-model.md](05-processing-model.md) permits a non-recipient node to *drop* private blocks it cannot decrypt — but that permission is about a node's own store. A server that drops them publishes a chain with a hole, and every block after the hole fails validation rule 3 at every client. Storage policy and serving policy are different policies; whether the specification proper should say so is open, see todo 071.
4. A server MUST serve `siblings` honestly: every block it holds at the named position, with no winner chosen.
5. A server MUST NOT require authentication or any client identifier for the five read operations. A deployment MAY put whatever it likes in front of these endpoints — a private server, an allowlist, a VPN — but a server that requires a client to identify itself makes that client's requests linkable into a durable identity, which is the opposite of what the subscription-privacy consideration asks for. See todo 073.
6. A server SHOULD be reachable over TLS, and MAY be reachable over plaintext HTTP. Neither changes what a client must verify.
7. **A source serves what it holds, whatever verdict it has reached about it.** A server answers every operation from the blocks in its store, including blocks it holds as *stored but unvalidated* ([05-processing-model.md](05-processing-model.md), "Block reception"), and MUST NOT withhold a block on the ground that it has not been able to validate it. The hole under "tip" — a store holding blocks 3, 4 and 5 of a chain whose first three it never received, serving all three by digest — is this rule's most visible case rather than an exception to it.

   Two arguments, and they point the same way. A client MUST validate everything it receives regardless ("Verification obligations"), so a withheld block costs the client a *detection* and saves it nothing; and withholding is the source deciding a validity question on the client's behalf, which the client rules of this same document forbid a client from delegating. It bites hardest at `siblings`, where the block withheld is one side of a fork the source has not been able to judge — precisely the omission the multi-source rule exists to defeat — and where "I have not validated it yet" is unobservable, unfalsifiable, and identical on the wire to "I do not have it".

   **The `tip` and `range` walk is a claim about connectivity, not validity.** It follows `prev` links through the blocks the source holds, and a source that has validated none of them still has a well-defined tip. A source that performs **no validation at all** — a mirror that stores the bytes it is given and serves them back — is conforming to this profile. Validation is the client's obligation, and nothing here moves any part of it to the source; a source that validates does so for its own store's sake, and the choice is not observable on the wire. A source that offers `announce` still owes that operation's own obligation, which is about what it stores and not about what it serves: it MUST NOT store as *valid* a block it has not validated, and a source that validates nothing therefore holds everything it takes as *stored but unvalidated*, reports it as `held`, and serves it.

   The cost is that a source relays blocks that may later turn out to be invalid. The bound is the one `announce` already has and is the same for every server: validation bounds what may be stored *as valid*, storage policy bounds the rest, and a block that arrived well-formed and signed is the worst a client ever has to throw away.

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

The questions above were left open on purpose. Two further sets were not. Five places where this draft was silent, or said two things, and the first implementation (`go/transport`) had to choose, are settled in the text above with the reasoning in [todos 085 to 089](../todos/) — `Dialog-Tip` where there is no tip, the tip of a store with a hole and the stability of a fork's branch choice, the status of an OPTIONAL operation a server does not offer, when an announce receipt's dispositions are decided, and the spelling of `limit` and of a repeated query parameter.

Six more were found the same way by the second implementation (`ts/`), written clean-room against this document, and are settled above with the reasoning in [todos 090 to 095](../todos/) — percent-encoding as a second spelling, whether a source serves blocks it has not validated, the status code for an announce refused by policy, what a client does with an empty range whose tip it cannot reach, which media types an announce body admits and whether `Accept` is evaluated on it, and what a server does with a query parameter the operation does not define. Two implementations finding eleven such places between them is the argument for writing the second one.

Two further places were found by neither implementation alone but by running them against each other ([todos 096 and 099](../todos/)): the pursuit's walk back to a *genesis* block, which is a third outcome and not a failure, and which position a client asks a source it has not used before from — where the two clients chose differently, produced identical stores, and disagreed only about whether a pursuit had happened. Both are settled above, the second in "First contact with a source".

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
Dialog-Tip: bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e
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
Dialog-Tip: bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e

<6 blocks concatenated: 395 + 965 + 1964 + 2855 + 2148 + 334 bytes>

  The last block hashes to the Dialog-Tip value, so the range ended at the
  tip rather than at a limit and there is nothing to continue. Had it not,
  the client would repeat the request with
  after=<CID of the last block it received> and no other state.

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

  In the second and third cases the second source answers an empty range
  after the position the client synced to, and a Dialog-Tip the client
  does not hold. The client then pursues that tip: GET the named block by
  digest from the second source, read its prev, GET that, and so on until
  it reaches a block it already holds ("Pursuing an advertised tip"). The
  block it arrived from and the block it already held after that position
  are two blocks with one prev from one pub key — rule 9, in the client's
  own store, with no server having admitted to anything.

  The explicit sibling query asks the divergent position directly, once
  the pursuit has found it:

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
