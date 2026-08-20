# Introducing Dialog: a signed, content-addressed substrate for machine-readable knowledge

*2026-08-20 — by [AUTHOR NAME]*

A language model that answers a factual question is doing one of two things. It
is either recalling something compressed into its weights during training, or it
is reading something a retrieval system scraped off the web. Neither is
checkable. The first has no provenance at all. The second has a URL, which is
not provenance: the page is mutable, it was probably not written by whoever it
attributes, and by the time anyone questions the answer the page may say
something else.

That is a substrate problem, not a model problem. The web was built for people to
read, and it makes almost no promises a machine can verify: not about who said a
thing, not about whether it still says the same thing, not about whether two
pages that disagree are disagreeing or just out of date.

Dialog is an attempt at a different substrate. It is a distributed, append-only
ontology graph protocol: a way to publish statements so that each one has a
stable identifier derived from its content, a cryptographic author, a position in
that author's history, and a place for other authors to argue with it. The
specification is at
[github.com/vrinek/Dialog](https://github.com/vrinek/Dialog), currently v0.8.0.

## Atoms, bonds, molecules

Everything in Dialog is built from three primitives, and all three are
content-addressed with SHA-256 — the identifier is a function of the content, so
the same statement published by two different people has one identifier.

An **atom** is an entity:

```
{"description": "Paris, the capital of France"}
SHA-256: 6545050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360965411fd
CID:     bafyreidfiucqui6ufw55tl3dnis7w2rxhptk45pz3eufhhczinqjmvar7u
```

A **bond** is a sentence template:

```
{"template": "_A_ is the capital of _B_"}
SHA-256: f295b89289597b4486784ad03d0be8bdab09a0d20070a893afa4f4d307811340
```

A **molecule** is a bond with its slots filled — a complete statement:

```
{"bond": <digest of the bond>, "fillers": [<Paris>, <France>]}
SHA-256: f9f124b06af6aa7d5f2381462afdeaca628fe3ac8b994253e5c08a3f5d128afb
```

Fillers can be atoms, bonds, other molecules, IPFS URIs, or scalar literals with
units and datetime ranges. There is no global schema: an application publishes
the bonds it needs, and a bond is just another content-addressed entity.

Authors publish by appending signed blocks to their own chain. A block holds an
ordered list of operations (`create_atom`, `create_bond`, `create_molecule`,
`rotate_key`), a `prev` link to the author's previous block, optional `refs` into
other authors' chains, and an Ed25519 signature over deterministic CBOR with a
domain separator. Ten validation rules decide whether a block is acceptable.
Every reference inside a structure is a raw 32-byte digest; the 36-byte CIDv1
appears only at the boundary, where humans and APIs see it.

## Disagreement is a value, not an error

Five standard meta-bonds let an author make claims about claims: `_A_ is the same
as _B_`, `_A_ is true`, `_A_ is untrue`, `_A_ contradicts _B_`, `_A_ supersedes
_B_`. They are ordinary molecules — no special block type, no special validation
— and are interpreted only when a reader distils their view.

Here is what that buys, taken verbatim from the demo in `demo/`, where three
fictional authors publish European geography with deliberate disputes:

```
molecule: Miravel is the capital of Valdoria [truth: conflicted]
    published by atlas
    digest 2d8619fffd0c9335e9b24e148681c86b3aec95853a7545a97c4aea381b021b68

Truth: conflicted.
Subscribed authors disagree, and Dialog does not resolve that: both positions
stand, and neither has been discarded.

Assertions (2):
  - gazetteer says it is untrue, in gazetteer block 4 of 4 (their last word)
  - atlas says it is true, in atlas block 6 of 6 (their last word)

Declared to contradict:
  - «Port Casta is the capital of Valdoria» (76c072a49644b2d2…)
```

Two authors, two positions, no winner. The protocol requires implementations to
detect conflicts, requires them to surface conflicts, and forbids them from
silently discarding either side. What to do about it is the application's
decision, made with both sides and both names in hand.

One consequence is worth stating because it surprises people: publishing a
molecule is not asserting it. Almost every molecule in any Dialog graph is
*unasserted*. Silence is not denial, and an assertion is a separate, attributable
act.

## Three layers, and why "true" is local

Readers process what they hear in three layers. **L1** is what we heard: raw
signed blocks, validated, stored. **L2** is what we know: every operation
extracted into one accumulated graph, tagged with who published it, with no
interpretation. **L3** is what we accept: L2 filtered to the authors this reader
subscribes to, with meta-bond semantics applied and every conflict surfaced.

Subscriptions are local and private. So L3 is local: "true" in Dialog always
means "true given who you are listening to". Drop one author from the
subscription set in the demo and the Valdoria dispute vanishes — not resolved,
just no longer a dispute among the people you are listening to:

```
Subscribed to atlas and errata; was atlas, gazetteer and errata.
Entities: 93 → 72 (-21). Conflicts: 2 → 0.
  Nothing was resolved. The authors who disagreed are no longer both in the
  view, so there is no longer a disagreement to surface — the assertions are
  still in L2, and re-subscribing brings them back.
```

Trust is a per-reader computation over signed data, not a global fact that a
network votes on.

## The demo: an LLM answering from signed facts

`demo/` is the founding use case wired end to end. Three authors, 14 blocks, 94
operations, 93 entities in L2, committed as raw block bytes and byte-identical on
regeneration. An MCP server replays them through the reference implementation's
whole pipeline and exposes the L3 view to an assistant as six tools: lookup,
truth, conflicts, equivalents, provenance, subscriptions.

The assistant answers "what is the capital of France" with Paris, the digest of
the molecule, and the author who published it. It answers "what is the capital of
Valdoria" with *there is a dispute*, and names both sides. It answers "how many
people live in Poland" with the current figure and the correction chain that
produced it — who corrected whom, in which block. The grounding is in facts
addressed by their content, attributed to whoever said them, and honest about
being disputed.

## What is deliberately not here

- **No consensus.** Every author has their own chain. Nobody agrees on a global
  ordering, because there is nothing global to order. Two authors' assertions are
  not ordered against each other — they conflict, and the conflict is the output.
- **No token, no fees, no incentive layer.** Blocks are self-authenticating
  bytes; they can move over HTTP, a USB stick, or email, and file exchange is a
  complete conforming transport.
- **No global state and no global schema.** There is no canonical view of the
  graph. There is your view, computed from your subscriptions.
- **No conflict resolution.** The protocol will not pick a winner for you, and
  forbids an implementation from doing it on your behalf.
- **No query language.** SQL, GraphQL, an API — implementation's choice.

The word "blockchain" fits the per-author hash chain and nothing else. There is
no shared chain, no mining, no validators, no network-wide agreement.

## What actually exists today

- A prose specification in eight documents (v0.8.0), using RFC 2119 keywords and
  CDDL: data model, block format, encoding, cryptography, processing model,
  meta-bond library, and an optional transport profile.
- A **Go reference implementation** of the whole L1 → L2 → L3 pipeline. One
  third-party dependency, `golang.org/x/crypto`. The CBOR codec is hand-rolled,
  because the profile is a small restricted subset and writing it audits the
  specification more honestly than a library would.
- A **TypeScript implementation of the wire format and transport, written
  clean-room** — built from `spec/` and `vectors/` alone, by agents with no
  access to the Go source. This was not ceremony. It found the dCBOR nesting
  bound was unspecified and the two implementations had picked 1024 and *no
  bound*; it found the reference-resolution scan limit had two different defaults
  counting two different things, both conforming to the same sentence. Both were
  invisible to each implementation's own tests. Both are now numbers in the spec.
- **230 conformance vectors** in four language-agnostic JSON files, pinning
  canonical bytes, digests, CIDs, signing inputs, signatures, ciphertexts, and
  the byte strings a conforming implementation must reject. They are generated,
  never hand-edited, and a diff in them is a breaking change.
- A **cross-implementation interop harness** in CI: Go server against TypeScript
  client and the reverse, over three scenarios including fork discovery at the
  genesis position, asserting both clients' summaries against an analytically
  generated expectation and against each other. 71 checks on the current tree.
- **Eight fuzz targets**, six over the decoders and two comparing the hand-rolled
  dCBOR codec against an independent CBOR implementation as an oracle. The
  decoder targets have been run for roughly 44 million executions with no
  crashers found; they run nightly.
- The **MCP grounding demo** described above.

## What I am not claiming

- **The transport profile is a Draft, and I mean Draft.** It has seven named open
  questions, each with a todo and each of which would move normative text if it
  were settled the other way: whether a block's plaintext head is separately
  serveable, whether fork detection deserves a multi-source obligation in the
  specification proper, a serving node's obligations versus a storing node's,
  locating a foreign block from a digest alone, what the subscription-privacy
  SHOULD actually demands, where a successor chain is served, and the fact that
  freshness has no signal at all. Nothing is invalid for not speaking the profile.
- **Two implementations by one project is not an ecosystem.** They were built
  clean-room from each other specifically so their agreement means something, and
  that is the strongest claim available: it is evidence the spec plus the vectors
  are a sufficient interop contract. It is not evidence that a third party,
  starting cold, will have the same experience. I would like to find out.
- **The cryptography has had no external review.** The primitives are
  conservative and boring on purpose — Ed25519, X25519, XChaCha20-Poly1305,
  HKDF-SHA-256, SHA-256 — and the compositions are the standard ones. But
  "conservative choices, carefully assembled, unreviewed" is exactly the sentence
  that precedes a lot of broken systems, and I am not going to pretend otherwise.
- **Key compromise is deferred to v2.** Rotation exists; it proves a link between
  two chains, not continuity of control. An author who lost a key and an author
  who rotated deliberately are indistinguishable from the blocks. KERI-style
  pre-rotation is the leading candidate.
- **L2 scalability is unsolved.** Pruning rules may be needed as graphs grow, and
  I do not have them.

## What I want

Three things, in descending order of how much they would help.

**A third implementation, by someone with no connection to this project.**
`vectors/` and `spec/` are meant to be enough. Two implementations that agree
tell me they are probably enough; a stranger's implementation would tell me
whether they actually are. If you build one and something in the spec is silent
or says two things, that is a bug in the spec and I want it filed as one.

**A review of the cryptography and the block validation rules** from someone who
does this professionally. Particularly the signing input and domain separation,
the AAD binding on private blocks, and the ten validation rules' interaction with
demand-driven foreign-block resolution.

**An argument that the data model is wrong.** The three-primitive ontology and
the five meta-bonds are the load-bearing design decisions, and they were chosen
in a design session rather than proven against a large corpus. If you have
modelled real knowledge domains and know why this shape breaks at scale, that is
the most useful thing anyone could tell me right now.

Issues and discussions at
[github.com/vrinek/Dialog](https://github.com/vrinek/Dialog). Start with
[`spec/00-overview.md`](../../spec/00-overview.md), or clone the repo and run the
demo — it is one `go build` and one MCP config entry.
