# Transport — design document

**Status:** draft for discussion
**Date:** 2026-08-18
**Normative status:** none. This document is analysis and a recommendation. It
changes no specification text, defines no wire format, and uses no RFC 2119
keyword in a binding sense. Anything here that survives review becomes
`spec/07-transport.md`, and the open questions at the end become `todos/`.

## Why this document, when transport is out of scope

"Transport is out of scope" appears in the README, in
[`spec/00-overview.md`](../../spec/00-overview.md) and in the
[2026-02-20 brainstorm](../brainstorms/2026-02-20-dialog-protocol-design-brainstorm.md),
and it is right: the protocol defines data and validation, and blocks are
self-authenticating, so they can move over anything. That decision is not under
review here.

What is under review is a narrower claim the repository has never examined: that
*nothing* about transport is protocol business. Three things in the
specification say otherwise.

1. **Fork detection is normative and transport-contingent.**
   [`spec/02`](../../spec/02-block-format.md) validation rule 9 requires a node
   to detect a fork when it *receives* a block whose `prev` matches a stored
   block's from the same author. A node that only ever hears one version of a
   chain, from one source, satisfies rule 9 vacuously and forever. The rule is
   normative; whether it ever fires is a property of the transport.
2. **Resolution is a fetch primitive with a normative bound.**
   [`spec/05`](../../spec/05-processing-model.md) "Resolution procedure" step 4
   says "fetch blocks listed in H's `refs`", and the scan limit — the one
   number in the specification whose value decides validity — is defined in
   units of *fetched-and-scanned foreign blocks*. The specification already
   describes an operation only a transport can perform.
3. **The specification asks transport for something and names nobody to ask.**
   `spec/05` Security Considerations: "the set of blockchains a node requests
   from the network may reveal subscription information at the transport layer.
   Transport implementations SHOULD consider this." That is a SHOULD addressed
   to a layer this project has never specified, described, or sketched.

So: still out of scope as a mandate, worth writing down as a profile. The
deliverable this document argues for is an *optional interoperability profile* —
one that two implementations can both speak, that nothing is invalid for not
speaking, and that leaves the file-based exchange the demo already relies on
exactly as valid as it is today.

## 1. Requirements extracted from the specification

Each requirement below is derived from existing text and cited. Where the
derivation is an inference rather than a quotation, it says so.

### R1 — Ordered chain sync, genesis to tip

> "A node maintains the set of author chains it is subscribed to. Subscriptions
> determine which chains the node fetches and stores at L1. [...] For each
> chain, it stores all blocks from the genesis block to the current tip."
> — `spec/05`, "Chain management"

Validity is inductive from genesis (`spec/02`, "Validation"), and a block whose
predecessor is absent or unvalidated is **stored but unvalidated** and MUST NOT
reach L2 (`spec/05`, "Block reception", step 4). Two consequences for
transport:

- The natural primitive is an **ordered range**, not a set. A transport that
  delivers blocks out of order is correct but wasteful: every block ahead of
  the frontier sits unvalidated until its predecessors land.
- Sync is **resumable by position**. A node that holds up to block *n* wants
  *n+1* onward, and the only stable name it has for its position is the digest
  of the block it holds. Height is not a protocol concept — no field carries
  it — so a transport that indexes by height is inventing state the blocks do
  not contain, and must be prepared for that index to be wrong about a forked
  chain.

### R2 — Tip discovery and change notification

Not stated as a rule; implied by "the current tip" above and by the write path:
"To write, an application sends operations to a Layer 1 blockchain node. The
data flows through L1 → L2 → L3 before the application sees its own updates"
(`spec/05`, "Application interface"). Somebody has to learn that a chain moved.

The minimum is: given an author's public key, learn the digest of that author's
current tip. Everything else — push, long-poll, polling interval — is a
latency/traffic trade-off over that one question.

### R3 — Fork and sibling discovery

> "If a node receives a block whose `prev` value matches the `prev` of another
> block already stored from the same `pub` key, the node MUST detect this as a
> chain fork." — `spec/02`, validation rule 9

And the same shape again for succession:

> "If a node holds more than one genesis block referencing the same rotation
> block, the succession is ambiguous and the node MUST surface the conflict."
> — `spec/02`, "rotate_key"

Both are conditioned on *holding both blocks*. Neither can fire on a node that
is served one linear history by one server. This is the requirement most easily
overlooked and the one with the sharpest transport consequence: **a transport
that cannot express "are there other blocks with this `prev`?" reduces a
normative MUST to a formality.** Two ways out, and a v1 profile should support
both:

- an explicit **sibling query** — blocks from author *P* whose `prev` is *D* —
  which a well-behaved server answers with everything it has;
- **multi-source sync** of the same chain, so that two servers with different
  branches produce a fork at the client even if neither server admits to one.

The second is strictly stronger, because a server serving a fork has every
incentive to serve only one side of it. Fork detection is ultimately a
reachability property, not a query, and the profile should say so rather than
pretend a query solves it.

### R4 — Demand-driven fetch of a specific block by digest

> "4. For still-unresolved digests, fetch blocks listed in H's `refs` [...]
> 5. If a referenced block's entities themselves have unresolved transitive
> dependencies [...] recurse into that block's own `refs`" — `spec/05`,
> "Resolution procedure"

Three properties of this fetch are fixed by the specification and constrain any
transport:

- **The key is a 32-byte block digest and nothing else.** A `refs` entry
  carries no author, no chain hint, no locator (`spec/03`, "Internal
  references"). Resolution is therefore *content-addressed lookup in a space
  with no routing information*, which is a materially harder problem than
  "fetch author P's chain" and is the single strongest argument for either a
  content-addressed substrate or a per-block author hint carried out of band.
- **It is on the validation path and it is bounded.** The scan limit counts
  distinct foreign blocks scanned, defaults to 256, and "if resolution must
  scan a further foreign block once the limit has been reached, the block being
  validated MUST be treated as invalid" (`spec/05`, "Scan limit"). Up to 256
  round trips per block validated is a latency budget that argues for batch
  fetch and for a served-set that is generous rather than minimal.
- **A cheap header fetch has a defined use.** "Neither does a block a node
  fetches only to read its type or its author, for validation rules 6 and 10,
  without reading its operations" (`spec/05`, "Scan limit"). The specification
  explicitly anticipates fetching a block for its plaintext head alone. Whether
  a transport can *safely* serve one is a different question — the signature
  covers the whole block, so a head on its own is unverifiable. See Q2.

### R5 — Succession discovery

> "The genesis block of the successor chain MUST list the rotation block's
> digest in its `refs`." — `spec/02`, "Verifiable succession"

> "2. The node MUST add the new key [...] to the set of known chains. 3. If the
> user subscribes to the old key's author, the implementation SHOULD
> auto-subscribe to the new key's chain" — `spec/05`, "Chain succession"

The evidence link points *backwards*: successor genesis → rotation block.
Nothing in the rotation block points forward to a block that did not exist when
it was signed. But the rotation block does carry `new_pub`, so the lookup a
node needs is not a reverse-ref search — it is **fetch the chain of `new_pub`
and check whether its genesis names this rotation block**. That reduces R5 to
R1 plus one predicate, with two wrinkles:

- The ambiguity case (several genesis blocks claiming the same rotation) is R3
  at chain position zero, and needs the same sibling query.
- A node that has just learned `new_pub` from a rotation block has no idea
  *where* that chain is served. If the answer is "the same place the old chain
  was", the profile should say so, because that is a hosting convention, not a
  protocol fact, and an author who rotates keys and moves hosts at the same
  moment disappears.

### R6 — Subscription privacy

> "Author subscriptions are never published, protecting users from social graph
> analysis. However, the set of blockchains a node requests from the network may
> reveal subscription information at the transport layer. Transport
> implementations SHOULD consider this." — `spec/05`, Security Considerations

This is the only place the specification addresses transport directly, and it is
a SHOULD with no addressee. What a design can honestly offer:

- **The leak is real and no cheap mechanism removes it.** A server asked for a
  chain knows someone wanted that chain. Private information retrieval and
  mixnets remove it at costs no v1 profile will pay.
- **Two structural facts already blunt it, and are worth stating.** First, the
  requested set is a *superset* of the accepted set: L1 pulls chains that L3
  filters out, both because "a user MAY subscribe to additional blockchains"
  (`spec/05`, "Chain management") and because demand-driven resolution fetches
  foreign blocks from authors nobody subscribed to. An observer of the request
  stream sees blockchain subscriptions, which are not author subscriptions
  (the distinction todo 028 drew). Second, subscriptions are *local
  configuration*; nothing obliges a node to reveal the whole set to one server,
  so partitioning requests across servers is available for free.
- **The honest ceiling for v1** is therefore: state the leak, define what the
  profile does *not* do about it, and specify the cheap mitigations — no
  request identifiers or auth by default, so that requests are not linkable
  into a session; supersets over minimal fetches where the cost is bounded;
  and a self-hosted or community server so that "the server knows" and "a
  stranger knows" are different statements.

### R7 — Private-block distribution

> "Only chain management fields (`v`, `type`, `pub`, `sig`, `prev`) remain in
> plaintext" — `spec/02`, "Private block"

> "Non-recipient nodes (those without the decryption key) MAY safely drop
> private blocks they cannot decrypt" — `spec/05`, "Public/private reference
> rules"

A transport moves private blocks as opaque bytes, and this is mostly free: the
block is self-describing enough to be validated structurally and chained. Three
things are not free:

- **A relay cannot index a private block's contents**, including its `refs`.
  So a digest-keyed "who defines entity X" index can only ever cover public and
  rotation blocks. Digest-keyed *block* fetch is unaffected — the block's own
  digest is over its ciphertext-carrying encoding.
- **Dropping private blocks severs the chain for everyone downstream.** The
  permission to drop is stated for a *node's own store*; a node acting as a
  server for someone else's chain that drops the private blocks serves a chain
  with holes, and every block after the first hole fails `spec/02` rule 3 at
  the client. A serving profile should say plainly that a server retains and
  serves opaque blocks it cannot read, and that a client must be able to tell
  "the chain ends here" from "this server is not giving me everything".
- **Key distribution stays out.** "The mechanism for distributing wrapped keys
  to recipients is out of scope (implementation-specific), and so is any
  envelope that carries them" (`spec/04`, "Wrapped key format"). A transport
  profile should not quietly become that envelope; if it ever carries wrapped
  keys, that is a separate decision with its own document.

### R8 — Transport must remain optional

> "an implementation MAY include all transitively-needed operations in a single
> block, making it self-contained. This 'fat block' strategy is an
> implementation choice for offline or portable use cases." — `spec/05`, "Fat
> blocks"

And in practice: the grounding demo ships chains as committed binary files with
an index, and lists "no transport (chains are files)" among its non-goals
([`docs/plans/2026-08-18-grounding-demo.md`](../plans/2026-08-18-grounding-demo.md)).
That path works today and must keep working. Concretely:

- No block's validity may depend on a transport being reachable. (It cannot
  today; the requirement is that no profile introduce such a dependency —
  freshness and completeness are not validity.)
- A file, a directory, a tarball or a USB stick is a conforming transport, and
  the profile should be shaped so that "here is a byte stream of blocks" is a
  degenerate case of it rather than a different thing.

### R0 — What transport does *not* have to provide

Worth stating because it is the lever the whole recommendation turns on: **every
block is self-authenticating.** Signature, author key, chain link and content
address are all inside the bytes, and `spec/02` validation needs nothing from
the channel. A server therefore cannot lie about block *contents*; it can only
lie by **omission** — withholding a block, a fork branch, or a tip. This
collapses the trust requirements on a transport to two properties, neither of
which is confidentiality or authenticity:

- **completeness** (did I get everything?) — unverifiable from one source,
  which is R3's real content;
- **freshness** (is this the tip?) — unverifiable at all, since no block
  carries trustworthy time (`spec/02`: timestamps are "self-reported and
  untrusted").

A transport design that starts from "the channel needs no trust" reaches very
different conclusions than one that starts from "the channel needs TLS".

## 2. Candidate substrates

Every status claim below was checked on 2026-08-18 against primary sources —
release feeds, commit logs, project blogs, registries. Dates are given because a
"maturity" verdict without one rots within a year. Where verification failed,
the text says so; a survey that only reports what it could confirm is a survey
that quietly overstates its confidence.

### 2.1 libp2p (gossipsub + Kademlia DHT)

**Verdict: healthy code, downgraded stewardship, stalled specs.** Interplanetary
Shipyard handed `go-libp2p` and `js-libp2p` to community maintainers on
2025-08-21 citing "resourcing challenges", with its maintenance ending
2025-09-30; no formal successor was announced, and continuity is inferred from
the fact that the same individuals keep committing. The libp2p Foundation
announced in 2023 alongside the IPFS Foundation never appeared — the IPFS one
launched 2025-06-03, the libp2p one is absent from every 2025–26 primary source
and appears to have been replaced by a grant pool ("libp2p Core Fund",
$3.3M in 2024–25). `libp2p/specs` has had no commit since 2026-02-26.
`go-libp2p` still ships (v0.49.0, 2026-07-28); the Rust umbrella crate has not
released since 2025-06-27.

**Gives us:** NAT traversal, multiplexing, transport negotiation, live fan-out
via gossipsub, and peer/content routing via the DHT — which is the one thing on
this list that answers R4's "fetch by digest with no routing information"
directly.

**Costs:** the largest dependency surface of any option, in a stack that just
lost its funded maintainer. Gossipsub gives fan-out of *new* blocks and no
history: catch-up, ordering and backfill (R1) are entirely ours to build.
Deployed state is v1.2 everywhere; v1.3 merged 2025-08-15 and ships nowhere;
"v2" is an unmerged draft PR. The DHT's reprovide cost — long the standing
objection — was genuinely fixed (Provide/Reprovide Sweep, default in Kubo v0.39,
2025-11-27, ~97% fewer lookups at 100k CIDs), so that criticism should be
dropped.

**Subscription privacy: the worst on this list.** A DHT lookup discloses the
target key to every node it touches, and gossipsub topic subscriptions disclose
which authors you follow to your mesh. IPFS's own documentation concedes third
parties can determine "what CIDs are being requested, when, and by whom". The
fix — IPIP-373, double-hashed DHT — has been an open PR since 2023-01-31 and is
unmerged after three and a half years. For a protocol whose specification
contains an explicit subscription-privacy SHOULD, adopting the substrate with
the loudest broadcast semantics would be perverse.

### 2.2 Secure Scuttlebutt

**Verdict: the closest prior art, and effectively dead as a maintained stack.**
This is the honest assessment the brief was asked for and it is not a close
call. `ssb-ebt` last pushed 2022; `ssb-server` 2022; `ssb-db2` 2024; Patchwork
archived 2021. The entire `ssb-ngi-pointer` organisation — Rooms 2.0, bendy
butt, meta-feeds, HTTP invites, the whole NGI-funded modernisation — was
archived on 2022-05-13/14, specified and implemented and never adopted
(`ssb-meta-feeds` gets 38 npm downloads a week). Manyverse's maintainer wrote on
2024-07-03: "Personally I will not do any more work on Manyverse. And my
impression is no one else is planning to either." Planetary migrated to Nostr in
2023. The one live descendant is tinySSB, a university research project. The
SSB Consortium's Open Collective runs on roughly a thousand dollars a year.

**Gives us — as a design, not a dependency:** SSB's model *is* Dialog's L1. One
signed hash-chained append-only log per identity, replicated whole. And EBT
(epidemic broadcast trees, `ssb-ebt`) is the best existing answer to R1 at
scale: a single duplex stream carrying a vector clock of `{feed: seq}` for all
feeds at once, where the sequence is stored as `seq << 1 | !rx` so that the low
bit *is* the plumtree eager/lazy edge — even means "send me this feed from
here", odd means "I have it, stay quiet", `-1` means "I don't replicate this".
Plus request skipping: persist the peer's last clock and omit unchanged feeds on
reconnect, making reconnect cost proportional to change rather than to feed
count.

**Costs:** adopting SSB means adopting a dead stack, a competing identity and
feed format, secret-handshake/box-stream instead of TLS, and a documented set of
frozen bugs (silent replication stalls, `ssb-ebt` #77; a gapless-prefix
assumption that forecloses sparse fetch, which R4 needs). We should read EBT and
implement nothing of SSB.

**Subscription privacy: the anti-pattern, precisely.** EBT's opening move is to
send the peer your entire interest set with positions, and request skipping
means the peer *persists* it across sessions. Whatever `spec/05`'s SHOULD means,
it means not doing that.

### 2.3 Nostr's relay model

**Verdict: healthy infrastructure, architecturally right, format wrong.** NIPs
repo active through 2026-08-08, `strfry` commits through 2026-08-18. The Go
ecosystem moved: `nbd-wtf/go-nostr` and `fiatjaf/khatru` were both archived
2026-01-24 and the maintained module is now `fiatjaf.com/nostr`. Current relay
counts and user numbers are **not verifiable** — nostr.watch's API returns 402
and nostr.band was unreachable — so treat any figure as stale.

**Gives us:** the architecture, which is the right one. Dumb, independent,
self-hostable servers holding self-verifying signed objects; clients talk to
several; no consensus, no routing layer, no membership. `REQ` with an `ids`
filter is exactly R4. And NIP-77 (negentropy, range-based set reconciliation) is
a genuinely reusable artifact with a maintained Go implementation.

**Costs:** Nostr events are not a log. They are independent, unordered, with
client-supplied timestamps, no hash chain, no sequence, no gap detection —
so R1, R3 and the entire inductive-validity story would have to be rebuilt on
top of a substrate that actively resists it. Carrying Dialog blocks as
base64 blobs inside JSON events is possible and unpleasant, and buys us a relay
network that has no reason to serve us. The economics are also a warning:
a peer-reviewed relay study (ACM ToN, September 2025) found each post
replicated to a mean of 34.6 relays with **98.2% of download traffic spent on
duplicates**, and 95% of free relays unable to cover costs.

**Subscription privacy: zero, and nobody is working on it.** Filters are
plaintext lists of the pubkeys you care about; NIP-42 attributes them to your
key; NIP-65 publishes your relay↔author map as a matter of routing. No blinded
query, PIR or oblivious-filter proposal exists in the NIPs repository.

### 2.4 Iroh

**Verdict: the healthiest project surveyed; its useful parts are not 1.0.**
`iroh` 1.0.0 shipped 2026-06-15 after 65 pre-releases, current 1.0.3
(2026-07-20), with a stated wire-stability commitment across 1.x and bindings
for Python, Node, Swift and Kotlin. But the sub-protocols were spun out and
remain 0.x, and `iroh-blobs`' own README says: "this version of iroh-blobs is
not yet considered production quality." `iroh-willow` is effectively abandoned
(0.0.1, published 2025-02-07).

**Gives us:** QUIC with hole punching, dial-by-public-key, relay fallback — the
NAT problem solved properly. `iroh-blobs` fetches blobs *or byte ranges* by
BLAKE3 hash as verified streams, which is R4 done well.

**Costs:** it is a Rust library, not a wire specification. A protocol with a Go
reference implementation and a TypeScript one cannot adopt a Rust library as its
interoperability contract without either FFI or a re-implementation of an
unspecified protocol. BLAKE3-keyed content addressing also does not match
Dialog's SHA-256 digests, so `iroh-blobs`' addressing would sit *beside* ours,
not replace it. And n0 is a VC-backed startup monetising hosted relays — the
library is MIT/Apache and relays are self-hostable, so the escape hatch is real,
but the dependency is commercial.

**Subscription privacy: no help.** Relay traffic is end-to-end encrypted, but
default discovery publishes signed pkarr/DNS records mapping endpoint ID to home
relay and optionally direct addresses, and the documentation offers no privacy
analysis of that.

### 2.5 Plain HTTPS endpoints (a "Dialog server")

**Verdict: the boring option, and the one with the strongest current prior
art.** The reference point is not a hypothetical — it is AT Protocol's
`com.atproto.sync.*`, which is exactly this shape and is running in production:
one signed append-only repository per identity, `getLatestCommit`,
`getRepo`, `getBlocks(did, cids[])`, and a firehose. Sync v1.1 (rollout from
2025-05-02) added `#sync` events and `prevData`, the previous Merkle root, so
that a client holding root *n* can verify the step to root *n+1* from the diff
alone — inductive verification of a log over plain HTTP, which is structurally
what `spec/02`'s inductive validity wants. A non-archival full-network relay
now runs on about 2 vCPU and 12 GB RAM, community-reported at ~$34/month.

Bluesky-the-app is shrinking (mobile MAU ~10.4M in June 2026, down 27% YoY, and
roughly half its Q4-2024 peak) and federation is thinner than advertised
(~3,486 independent PDSes holding ~0.2% of accounts; AppViews remain a
chokepoint). None of that is an argument against the *sync API's* design, which
is what we would be borrowing.

**Gives us:** every requirement, directly and with no new concepts. Ordered
range fetch (R1), a tip endpoint (R2), a sibling query we would define (R3),
digest-keyed fetch (R4), and a chain-by-author lookup for succession (R5).
Self-hostable behind anything — a VPS, Tailscale, a Tor onion, a static file
server. CDN- and proxy-cacheable, because content-addressed blocks are
immutable. Implementable in an afternoon in Go, TypeScript, or with `nginx` and
a directory of files.

**Costs:** no NAT traversal (a client must reach a server), no peer discovery
(you learn a URL out of band), and no push without either long-poll or SSE. It
does not scale to "everyone runs a node behind CGNAT and nobody runs a server" —
which is a real limitation and not one Dialog needs solved today.

**Subscription privacy: bad by default, and the only option where we control
the defaults.** A server we ask knows what we asked for. But we get to specify
no authentication and no client identifier, so requests are not linkable into a
session; we get to recommend splitting chains across servers; and immutable
block responses mean a shared CDN or proxy absorbs part of the pattern. One
uncomfortable observation from the AT Protocol numbers is worth recording: when
mirroring an entire network costs $34/month, "fetch everything and filter
locally" becomes an affordable brute-force form of private information
retrieval, and is the only mitigation on this page that actually works.

### 2.6 ActivityPub-style federation

**Verdict: healthy and formally re-energised, and the wrong shape.** The 2018
Recommendation has never been revised, but a new W3C Social Web Working Group is
chartered 2026-01-15 to 2028-01-31 and is working through 14 ActivityPub and 23
ActivityStreams errata plus LOLA (account portability). This is maintenance, not
"ActivityPub 2".

**Gives us:** little that Dialog needs. AP is push-based: the origin server
walks the recipient collections and issues one signed HTTP POST per receiving
inbox at write time, whether or not anyone reads. There is *no pull path* —
if a delivery failed, you simply never have it, and backfill is a draft FEP.
There is no log, no sequence, no hash chain, and that absence is precisely why
deletes are advisory, migration is unsolved and threads fragment. Objects are
addressed by HTTPS URL, not content hash, so identity is location-bound. The
FEPs that would fix this for us — FEP-8b32 (object integrity proofs, filed
2022-11-12) and FEP-ef61 (portable `ap://` objects, filed 2023-12-06) — describe
Dialog's problem space and have been DRAFT for approaching four years. Roughly
11 of ~150 FEPs have ever reached FINAL.

**Costs:** everything above, plus the fan-out bill. Followers across 5,000
instances means 5,000 outbound signed POSTs per post, paid by the author.

**Subscription privacy: negative.** `Follow` is a public activity and follower
collections are ordinarily world-readable — the interest graph is published by
design, which is the exact inverse of `spec/05`'s "Subscriptions are never
published". Worse, Mastodon's hardened configuration ("authorized fetch")
*mandates* HTTP-signed GETs, so the security-conscious setting makes every read
disclose who is reading.

### 2.7 IPFS / IPNS for block storage

**Verdict: software healthy, commercial ecosystem thinned, and the good part is
not the part people mean.** Kubo ships on a 6–9 week cadence (v0.43,
2026-08-03), Helia is healthy, Shipyard still maintains IPFS — notably, it kept
IPFS and dropped libp2p. Meanwhile the pinning market collapsed: Fleek shut
hosting 2026-01-31, Infura closed to new users, Storacha dropped IPFS pinning,
Scaleway shut its service, Cloudflare's gateways died in 2024, Brave removed its
node. And on 2026-05-11 Shipyard began redirecting `ipfs.io`/`dweb.link` browser
navigation to `inbrowser.link`, announcing "additional rate-limits on the legacy
gateways over the course of the year" (no thresholds published — **unverified**
how severe).

**Gives us:** two distinguishable things. IPNS as an author-head pointer is
signed, sequence-numbered and self-certifying, and ProbeLab's live measurement
(published 2025-09-12) found **100% retrieval success** with records surviving
three days of churn — but a **median resolve of ~11 seconds and a tail of
37–60 s**, which their own write-up calls a generally bad user experience. That
makes IPNS a background-refresh mechanism and disqualifies it from R2's hot
path. The genuinely good part is the direction of travel: HTTP retrieval became
the default in Kubo v0.36 (2025-07-14), and the trustless-gateway spec plus
`@helia/verified-fetch` amount to *verifiable content-addressed blocks over
ordinary HTTPS with client-side verification and no swarm membership* — which is
the recommendation of §3 arrived at from the other direction.

**Costs:** taking a hard dependency on public pinning or public gateways in 2026
is imprudent on the evidence above. Putting IPNS resolution on the validation
path with R4's 256-block budget would be indefensible. Note also that Dialog
already depends on IPFS for exactly one thing — type-3 fillers carry IPFS URIs —
and that dependency is bounded and out of transport's scope; extending it to
block storage would widen it substantially.

**Subscription privacy: actively hostile.** DHT lookups leak the CID, Bitswap
broadcasts wantlists, reproviding advertises what you hold. HTTP retrieval
trades that for leaking the CID to a gateway operator — a different leak, not a
smaller one.

### 2.8 Adjacent prior art worth reading, not adopting

Four things surfaced that are not substrates but should inform
`spec/07-transport`:

- **Range-based set reconciliation** (Meyer, arXiv:2212.13567) and its
  production incarnation **negentropy** (active, commits 2026-08-01; stable wire
  spec; a maintained Go implementation at `fiatjaf.com/nostr/nip77`).
  Reconciles two sets in bandwidth proportional to their difference. Dialog
  does not need it for chain sync — see §3 — but it is the right tool the day
  someone reconciles *sets* of blocks rather than chains.
- **AT Protocol's `getRecord`**, which returns the Merkle path proving a
  record's existence *or non-existence* against a signed root. Dialog has no
  commitment structure that could support such a proof, and R0's completeness
  gap is exactly what such a proof would close. Worth knowing that the
  technique exists before concluding the gap is unfixable.
- **The tlog-tiles design** (`c2sp.org/tlog-tiles`, with `filippo.io/torchwood`,
  `filippo.io/sunlight`, and `transparency-dev/tessera` past 1.0 as of
  2026-07-16) — the best-maintained append-only-log tooling in Go, serving a
  Merkle tree as **static CDN-cacheable files** so that clients compute proofs
  locally and the server generates none. Chrome's 2026 Certificate Transparency
  mandate moved that whole ecosystem onto it. If Dialog ever wants verifiable
  completeness, this is the family to look at, and it is boring HTTP.
- **Hypercore** (Holepunch; `hypercore` 11.35.1, npm 2026-07-28, and the
  surrounding `autobase`/`corestore`/`hyperswarm` stack all committing this
  month) — a secure distributed append-only signed log with a Merkle tree over
  it, sparse replication with verified reads, and bitfield-plus-range request
  exchange. The live, maintained counterexample to "SSB is dead, therefore
  append-only logs are dead". Its privacy device is instructive as a *near
  miss*: the discovery key is a one-way derivation of the public key, so a
  core can be announced without exposing the read key — but anyone who knows
  the public key can compute the discovery key and watch for it, which makes it
  obfuscation rather than interest privacy.
- **Willow's Private Interest Overlap detection** — the only protocol-level
  design in the field that attacks query privacy (R6) rather than conceding it.
  It is honest about its own ceiling: the specification states such solutions
  "cannot prevent peers from confirming guesses about data they shouldn't know
  about". Status: Proposal, demoted from Candidate on 2025-10-26; the project's
  NLnet funding ended September 2025 and the repository has moved to Codeberg
  (still committing, 2026-08-18); no Go implementation, no production adopters.
  Read it for the framing, do not depend on it.

### 2.9 What the survey establishes

- **Nobody has shipped query privacy.** Not one substrate here has a working
  answer to R6, and the only serious design is an unfunded proposal that
  disclaims its own strength. Any v1 profile that claims to solve subscription
  privacy is lying; the honest move is to state the leak, take the cheap
  mitigations, and write down what is not solved.
- **The append-only-log-per-author model is not exotic** — SSB, Hypercore and
  AT Protocol all implement it — but the only maintained, specified,
  boringly-deployable version of it in 2026 runs over plain HTTP.
- **Content-addressed block fetch over HTTPS with client-side verification is
  where the whole field converged**, arriving from IPFS (HTTP retrieval by
  default), AT Protocol (`getBlocks` returning CAR), and transparency logs
  (static tiles) independently.
- **Funding is a design input.** NGI Zero, which paid for SSB's modernisation
  and Willow, held its final call on 2026-06-01. Shipyard dropped libp2p.
  Choosing a substrate is choosing a maintainer, and the substrate with no
  maintainer to lose is HTTP.

## 3. Recommendation

**Specify a minimal HTTP chain-sync profile as Dialog's v1 reference transport,
publish it as an optional profile rather than a protocol requirement, and pair
it with one client-side rule — sync each chain from more than one source — that
does more for correctness than any wire feature on offer.** Defer gossip and
peer-to-peer profiles until somebody has a use case that a server cannot serve,
and adopt none of the surveyed stacks wholesale.

The reasoning, in order of weight:

**1. R0 makes the substrate question much less interesting than it looks.**
Blocks are self-authenticating; a server cannot lie about content, only withhold
it. So the elaborate trust machinery that justifies libp2p's or SSB's handshakes
buys Dialog nothing it does not already have inside the block. What remains —
ordered ranges, a tip pointer, fetch-by-digest, a sibling query — is four HTTP
requests. Choosing a P2P stack to move self-verifying bytes is paying a large
dependency for a property we already hold.

**2. Dialog's L1 is a linear log, which makes the sophisticated tools
unnecessary.** Negentropy, range-based reconciliation and EBT's vector clocks
all solve *set* reconciliation, where two peers hold overlapping unordered
collections. A Dialog chain is totally ordered by `prev` with a single head.
Reconciling it is: compare one digest; if it differs, ask for everything after
the one you hold. That is one round trip and no algorithm. We should say this
out loud, because the temptation to reach for negentropy is real and it would be
solving a problem the data model does not have. (Where a set does appear — the
set of chains a node follows — the reconciliation cost is trivial and the
privacy cost of disclosing the set is the actual constraint.)

**3. The prior art for exactly this shape is running in production and is
cheap.** AT Protocol's sync API is a per-identity signed append-only log served
over HTTP with by-digest block fetch and an inductive verification hook, and a
full-network mirror of it costs about $34 a month. We do not have to guess
whether the shape works.

**4. Every alternative fails on maturity, fit or privacy, and usually two.**
SSB is the closest model and is unmaintained. libp2p lost its funded maintainer
and has the worst privacy properties on the list. Nostr's architecture is right
and its data model is wrong in exactly the place Dialog is opinionated. Iroh is
excellent and is a Rust library, which is not a thing a two-implementation
protocol can adopt as its interop contract. ActivityPub publishes the interest
graph by design. IPFS's block layer is converging on HTTP anyway, and its name
system is 11 seconds slow on a path that has a 256-fetch budget.

**5. HTTP is the only option where we set the privacy defaults.** We cannot
solve R6 — nobody has. But we can specify no authentication, no client
identifier, immutable cacheable block responses, and a recommendation to
partition chains across servers, and we can write down plainly that a server you
ask is a server that knows. That is a worse guarantee than Willow proposes and a
better one than anything shipped.

**6. It keeps R8 free.** If the wire body and the file format are the same CBOR
sequence of blocks, then the demo's committed chain files, an emailed
attachment, and a `GET` of a block range are the same artifact. Offline exchange
stops being an exception to the transport and becomes the transport with the
network removed. No other candidate has this property; most of them make
file-based exchange a second mechanism.

**What this recommendation is not.** It is not a claim that HTTP is the right
long-run answer for a network of phones behind CGNAT with no servers. When that
becomes the use case, the profile to write is an Iroh- or libp2p-based one, and
the reason to define the four operations abstractly now is so that a second
profile has the same four questions to answer. It is also not a claim that a
"Dialog server" is a trusted party: it is an untrusted mirror, and the
multi-source rule exists precisely because it is.

**What to build first, if this is accepted.** A conformance-testable HTTP
profile document, a server that is a directory of block files behind a static
file server plus one dynamic endpoint, and a client in the Go reference
implementation that syncs a chain from two sources and surfaces a fork when they
disagree. That last one is the deliverable that would tell us whether rule 9 is
a real rule.

## 4. Sketch of the v1 sync profile (non-normative)

Everything below is illustrative. Names, paths and media types are placeholders
for the discussion `spec/07-transport.md` would settle.

### 4.0 One serialization, two containers

The profile has exactly one representation of "some blocks": a **CBOR sequence**
([RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742)) — the canonical
dCBOR encoding of each block, concatenated, no framing. It is already what the
blocks are; the sequence adds nothing.

That one representation is the HTTP response body *and* the file format. A range
response saved to disk is a valid chain file; a chain file POSTed to a server is
a valid announce. This is what keeps R8 structural instead of a caveat: offline
exchange is not a parallel mechanism, it is the same bytes without the HTTP.

Order matters for a chain range (genesis-ward to tip-ward) and does not matter
for a digest-keyed multi-fetch. Both are unambiguous from the request.

### 4.1 Endpoints

Read paths — all `GET`, all cacheable, none requiring authentication:

| Request | Answers | Response |
|---------|---------|----------|
| `GET /dialog/v1/chains/{author}/tip` | R2 | `{"tip": "<CID>"}`, `ETag: "<CID>"` |
| `GET /dialog/v1/chains/{author}/blocks?after={CID}&limit={n}` | R1 | CBOR sequence, chain order; `after` omitted means from genesis |
| `GET /dialog/v1/blocks/{CID}` | R4 | one block, immutable, cache forever |
| `POST /dialog/v1/blocks/fetch` | R4 | body: CBOR sequence of digests; response: CBOR sequence of the blocks held. The one non-idempotent-looking `POST` that is really a `GET` with a long argument list — 256 digests do not fit in a URL |
| `GET /dialog/v1/chains/{author}/siblings?prev={CID}` | R3, R5 | every block this server holds from `{author}` whose `prev` is `{CID}`; `prev=null` returns genesis candidates |

Write path:

| Request | Answers | Response |
|---------|---------|----------|
| `POST /dialog/v1/announce` | publication, server-to-server | body: CBOR sequence of blocks. The server validates per `spec/02` and stores what it accepts |

Six endpoints. An author's node publishes by announcing to its server(s); a
server replicates to another server with the same call; a client never needs a
different mechanism from a server. There is no separate peer protocol because
there is no separate peer.

`{author}` is the author's Ed25519 public key in a text form the specification
did not define when this was written — see Q1, since settled: it is the
multibase base32 form of `spec/03`, "Text representation of author keys", 56
characters beginning `b5ua`. `{CID}` is the base32 CIDv1
text form of `spec/03`, which is exactly the "external identifier" role that
document reserves for it.

There is deliberately **no head-only endpoint**, despite R4 identifying a use
for one. A block's signature covers the whole block, so a plaintext head served
on its own cannot be verified, and acting on an unverifiable head would let a
server induce a wrong rule-6 rejection of a valid block. Q2 decides whether that
is fixable; until it is, batch fetch is the answer to the round-trip problem.

### 4.2 Semantics worth pinning

- **404 means "I do not have it", never "it does not exist."** A server's
  absence of a block is not evidence about the block. This distinction has to be
  written down, because the natural HTTP reading is the wrong one.
- **Range responses are self-checking.** A client validates a range by walking
  `prev` from the first block it receives; a server that skips a block produces
  a break the client sees immediately. Within a range, completeness is free.
  Between the last block and the real tip, it is not — a server that withholds
  the tip is indistinguishable from an author who stopped publishing (Q8).
- **Verify, then trust nothing else.** Every block is re-hashed and its
  signature checked on arrival. TLS protects the request pattern (R6), not the
  data; a plaintext HTTP mirror is a downgrade in privacy and not in integrity.
- **`Cache-Control: immutable` on `/blocks/{CID}`** is not a performance note.
  It means a CDN, a corporate proxy or a local squid can serve foreign-block
  resolution, which is exactly the traffic R4's 256-block budget makes hot, and
  it means those caches absorb some of R6's leak for free.

### 4.3 Subscriptions on the wire

An L1 blockchain subscription becomes one of three things, in increasing order
of server cooperation and decreasing order of privacy:

1. **Polling `/tip` with `If-None-Match`.** A 304 is a few dozen bytes. For a
   node following twenty chains at a one-minute interval this is nothing, and it
   is the only option that needs no server feature beyond correct ETags. It is
   the baseline every implementation supports.
2. **Long-poll: `GET /tip?wait={seconds}`.** The server holds the request until
   the tip differs from the client's `If-None-Match` or the timeout expires.
   One HTTP idiom, no new protocol, no client reachability requirement.
3. **A tip event stream: `GET /dialog/v1/events?author=…&author=…`**
   (server-sent events), emitting `{author, tip}` as chains move. One connection
   for many chains. This is the option that hands the server the client's whole
   subscription set in one durable, correlated act, which is precisely R6's
   concern — so the profile should describe it as a convenience with a stated
   privacy cost, not as the recommended mode.

**Webhooks are deliberately out.** They require the client to be reachable
(reintroducing the NAT problem this profile exists to avoid), they need a
registration and authentication surface, and the registration is a durable
server-side record of exactly what the user subscribes to. Every one of those is
a cost this profile is trying not to pay.

### 4.4 The multi-source rule

Not an endpoint — a client discipline, and the part of this sketch that does the
most work:

> A node SHOULD sync each chain it follows from more than one source, and
> compare. A source is anything: two servers, a server and a file, a server and
> a friend's USB stick.

This is what makes `spec/02` rule 9 fire in practice (R3). It needs no wire
support at all — every response is self-verifying and stateless, so "ask two
servers" is the same code twice — and it is the only mitigation for withholding
that does not require trusting anybody. It also happens to help R6: a node that
splits twenty chains across four servers hands no single party the full set.

### 4.5 What stays out of v1

- **Peer/server discovery.** You learn a server's URL the way you learn a
  podcast feed's: somebody tells you. No DHT, no registry, no well-known
  bootstrap list.
- **NAT traversal, hole punching, direct peer connections.** A client talks to
  servers. A user who wants no server runs one on a machine they control, on a
  Tailscale address or a Tor onion, and the profile is unchanged — it is HTTP,
  and it does not care what the URL resolves to.
- **Transport authentication and accounts.** Optional and off by default:
  authentication makes a client's requests linkable into a durable identity,
  which is the opposite of what R6 wants. A private server can put whatever it
  likes in front of these six endpoints.
- **Wrapped-key distribution.** Out of scope by `spec/04`, and staying out (R7).
- **IPFS URI filler resolution.** A type-3 filler carries IPFS's own identifier
  (`spec/03`, "Internal references"). Fetching what it points at is IPFS's
  business, not a Dialog transport's.
- **Deletion, expiry, retention policy, quotas, payment.** Server policy. The
  protocol is append-only; a server's disk is not.

## 5. Open questions for `spec/07-transport`

Each of these now has a todo. Q1 is settled and applied; Q2–Q8 are deliberately
open and belong to whoever drafts `spec/07-transport.md`.

**Q1 — A text form for an author's public key.** — **settled**, see
[`todos/076`](../../todos/076-complete-p2-a-canonical-text-form-for-an-authors-public-key.md).
`spec/03` defines a text form for a digest (base32 CIDv1) and none for a 32-byte
Ed25519 public key, but every transport identifier for a chain needs one. Fix a
canonical, case-stable, URL-safe encoding — and decide whether it is a bare
multibase string or a CID-like structure with a codec prefix. Nothing about
transport can be written down before this exists.

*Answered:* multibase base32 (`b`, lowercase RFC 4648, unpadded) over the 34
bytes `0xed 0x01 || key` — the multicodec `ed25519-pub` prefix and the key. Self
-describing, one text alphabet with the CID form, 56 characters, always
beginning `b5ua`, and convertible to `did:key` by re-encoding the same bytes in
base58btc. Specified in `spec/03`, "Text representation of author keys", with
`spec/04`'s "Key encoding" MAY turned into a pointer at it; implemented in both
implementations and pinned in `vectors/` as each key's `public_key_text`. So
`{author}` in §4.1 is that string.

**Q2 — Is a block's plaintext head separately serveable?** — open,
[`todos/069`](../../todos/069-pending-p2-is-a-blocks-plaintext-head-separately-serveable.md).
`spec/05`'s scan limit anticipates "a block a node fetches only to read its type
or its author", but the signature covers the whole block, so a head alone is
unverifiable. A lying server could induce a wrong rule-6 rejection of a valid
public block. Decide: forbid partial serving; or state that a head is advisory
and MUST NOT decide validity; or accept that the scan-limit clause describes an
optimization no safe transport can offer.

**Q3 — Does fork detection deserve a multi-source obligation?** — open,
[`todos/070`](../../todos/070-pending-p2-does-fork-detection-deserve-a-multi-source-obligation.md).
`spec/02` rule 9 is normative and vacuous on a single-source node. Either add a
SHOULD ("a node SHOULD obtain each subscribed chain from more than one source")
or state explicitly in the security considerations that fork detection is
best-effort and bounded by the node's reach. The current silence reads as a
guarantee the protocol does not make.

**Q4 — A serving node's obligations versus a storing node's.** — open,
[`todos/071`](../../todos/071-pending-p3-a-serving-nodes-obligations-versus-a-storing-nodes.md).
`spec/05` permits a non-recipient node to drop private blocks it cannot decrypt.
A node acting as a *server* that does so publishes a chain with a hole, and
every block after it fails rule 3 at the client. Should the specification
separate storage policy from serving policy, and say that a server retains
opaque blocks?

**Q5 — Locating a foreign block from a digest alone.** — open,
[`todos/072`](../../todos/072-pending-p2-locating-a-foreign-block-from-a-digest-alone.md).
A `refs` entry carries no author and no locator, so demand-driven resolution is
content-addressed lookup with no routing information. Options: a server-side
digest index (possible only for the public blocks it holds); an out-of-band
author hint carried alongside the reference by the transport; or accepting that
resolution only works within a server's held set. This is the requirement that
comes nearest to justifying a change to the block format, and it should be
decided before, not after, a profile ships.

**Q6 — What does the subscription-privacy SHOULD actually demand?** — open,
[`todos/073`](../../todos/073-pending-p3-what-the-subscription-privacy-should-actually-demands.md).
`spec/05` tells transports to "consider" the leak and stops. Turn it into
something checkable — no persistent client identifier, no authentication by
default, request partitioning across servers, supersets over minimal fetches —
or downgrade it to informative text that says plainly that a server you ask is a
server that knows.

**Q7 — Where is the successor chain served?** — open,
[`todos/074`](../../todos/074-pending-p3-where-is-the-successor-chain-served.md).
After a rotation a node learns `new_pub` and nothing about where that chain
lives. Hosting continuity ("the same server as the predecessor") is an
assumption the profile would be making silently. Say it, or define how a
successor's location is discovered.

**Q8 — Freshness has no signal.** — open,
[`todos/075`](../../todos/075-pending-p3-freshness-has-no-signal.md).
Nothing distinguishes "this is the tip" from "this is the tip I am willing to
show you", and no block carries trustworthy time (`spec/02`: timestamps are
self-reported and untrusted). A signed tip attestation would be a new signed
object, which v1's closed block-type set does not have room for. Decide whether
this is accepted as unfixable in v1 and recorded in the security considerations,
or deferred to a version that can add an object.
