---
status: pending
priority: p2
issue_id: "051"
tags: [specification-gap, processing-model, privacy, subscriptions, layer-3]
dependencies: ["028"]
---

# A Decrypted Foreign Private Chain Has No Stated Place in L3

## Problem Statement

`spec/05-processing-model.md` § "Private chains" describes the flow for one case
and states the L3 rule for that case only:

> 3. L3: Private chain data from the user's own chain is included in L3 (the
>    user always "subscribes" to their own chains).

But a private chain is not only ever the reader's own. `spec/04-cryptography.md`
§ "Key management" specifies per-recipient key wrapping — an X25519 agreement
between the author's key and *a recipient's* — whose entire purpose is to give
someone else the content key of a chain they did not write. The specification
therefore provides for exactly the case its L3 rule does not cover: **author A's
private chain, whose content key reader B holds**.

At L2 there is no ambiguity, and the implementation needed none: a private
block's `pub` is in the clear, so the entities recovered from its payload are
tagged with A's key and A's block, exactly as a public block's would be. The
question is what L3 does with them:

**1. Does holding the key imply a subscription?** § "Filtering rules" is
unconditional — "For each entity in L2, check if any of its authors [...] is in
the user's subscription list" — so if B has not subscribed to A, A's private
data is filtered out of B's L3 even though A encrypted it *for B specifically*.
That is a defensible reading (subscription is a separate, local decision) and a
surprising outcome (why wrap a key for a reader who then does not see the data).
The "always subscribes to their own chains" parenthesis shows the document is
willing to make holding-implies-accepting an implicit rule; it just does not say
whether the same applies here.

**2. What is "the user's own chain"?** The rule is written in terms of chain
ownership, and the only thing that identifies a chain is its `pub` key. A user's
own chain is one signed by a key they hold. That is the natural reading, but it
is not stated, and it matters at a rotation: after a key rotation, the successor
chain is signed by a different key, and § "Chain succession" leaves author
identity implementation-scoped. Whether a user's *former* chain is still "their
own chain" for this rule is unanswered.

**3. What does the L1 subscription rule mean for a private chain?** § "Chain
management" requires that "A user MUST subscribe to the blockchain of every
author they subscribe to." A reader given a content key must be pulling that
chain's blocks to have anything to decrypt — so the blockchain subscription is
implied by the arrangement, while the author subscription may or may not exist.
The two halves of the word "subscription" (todo 028) come apart precisely here.

## Findings

- `spec/05-processing-model.md`, "Private chains", steps 2 and 3: L2 decrypts
  "if the node holds the decryption key"; L3 covers only "the user's own chain".
- `spec/05-processing-model.md`, "Filtering rules": the unconditional
  subscription test, and "Foreign chain data that was loaded into L2 for
  validation context is excluded from L3 unless the user independently
  subscribes to the foreign author."
- `spec/05-processing-model.md`, "Chain management": "A user MUST subscribe to
  the blockchain of every author they subscribe to."
- `spec/04-cryptography.md`, "Key management": per-recipient key wrapping, whose
  premise is that a reader who is not the author holds the chain's content key.
- `spec/05-processing-model.md`, "Chain succession": author identity across a
  rotation is implementation-scoped.
- `go/graph`, `Graph.IngestPayload`: tags a decrypted private block's entities
  with the block's `pub` and the block's digest, without asking whose key opened
  it. The doc comment says the key is in the clear, which is what makes the tag
  unambiguous; who accepts those entities is left to L3.
- Related: todo 028 (subscriptions are not an L3-only concern).

## Proposed Solutions

### Option 1: State the rule in terms of keys held, not chains owned (Recommended)

- Rewrite § "Private chains" step 3 as: private chain data a node can decrypt is
  subject to the same L3 filtering as any other data — it is included when the
  chain's author is subscribed to — and note that a user is always considered
  subscribed to chains signed by keys they hold, which is what makes their own
  chains unconditional.
- Add a sentence: being given a chain's content key is not a subscription;
  a reader who wants an author's private data in L3 subscribes to that author,
  exactly as for public data.
- **Pros**: one rule for public and private data, no special case, and it says
  what "own chain" means; leaves the reader in control of their own L3.
- **Cons**: an author who wraps a key for a reader still cannot cause that data
  to appear in the reader's view — an application that wants that behaviour has
  to prompt for a subscription.
- **Effort**: Small (spec), none (Go)
- **Risk**: Low

### Option 2: Holding the key implies subscription

- State that decryptability itself admits data to L3.
- **Pros**: matches the intuition that a wrapped key is an invitation.
- **Cons**: an author could push data into a reader's truth by handing over a
  key, which inverts the direction of control the subscription model is built
  on; and it makes the L3 view depend on a node's key store rather than on the
  user's stated subscriptions.
- **Risk**: High

### Option 3: Say nothing

- **Cons**: the one case key wrapping exists to serve has no defined L3
  behaviour; two implementations will differ on whether a shared private chain
  is visible, which the user experiences as data silently missing.
- **Risk**: Medium

## Recommended Action

Option 1. Keep filtering author-based and uniform, define "own chain" as "signed
by a key the user holds", and say explicitly that a content key is a capability
to read, not a declaration to accept.

## Technical Details

- **Affected Files**: `spec/05-processing-model.md` ("Private chains",
  "Filtering rules")
- **Related Components**: L3 filtering, private chains, key management
- **Database Changes**: No

## Acceptance Criteria

- [ ] The L3 rule covers a private chain the node can decrypt but did not write
- [ ] "The user's own chain" is defined
- [ ] Whether holding a content key implies an author subscription is stated

## Work Log

### 2026-08-15 - Filed While Implementing go/graph

**By:** Claude

Found writing `IngestPayload`. L2 needed no answer — the author's key is a
plaintext field of a private block, so the authorship tag is the same whoever
decrypted it — but the question is unavoidable one layer up, and the `accept`
package will have to pick a reading. Filed before that happens so the pick is
recorded as a pick.

## Notes

Source: Go reference implementation, phase 6 (L2 ontology graph).
