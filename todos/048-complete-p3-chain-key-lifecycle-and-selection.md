---
status: complete
priority: p3
issue_id: "048"
tags: [specification-gap, cryptography, key-rotation, privacy]
dependencies: ["046"]
---

# A Chain's Symmetric Key Has No Lifecycle and a Private Block Names No Key

## Problem Statement

`spec/04-cryptography.md` § "Encryption scheme" and § "Key management" give the
content key one property — "unique per chain, not per block" — and nothing
else. Three questions follow immediately and none is answered.

**1. What is "a chain" when a key rotates?** A rotation block ends the current
key's chain and a new chain begins under the successor Ed25519 key
(`spec/02-block-format.md`, "rotate_key"). Does the successor chain use the
same symmetric key, or a new one? If the same, a symmetric key outlives the
identity key it was shared under; if a new one, nothing says so, and a reader
who was given the old chain's key silently loses access at the rotation with no
signal that this is what happened.

**2. Can a chain re-key, and what does revocation mean?** A reader who is given
the key can read every private block of the chain, past and future, for as long
as the chain exists. There is no re-key operation, no key generation counter,
and no way to say "from this block on, a different key". Removing a reader is
therefore impossible without abandoning the chain. This may well be the
intended v1 scope — the default use case in the specification is a user's own
devices, where there is nobody to remove — but the specification does not say
it is the scope, so an implementer cannot tell a deliberate omission from a
gap.

**3. How does a reader know which key opens a block?** A private block's
plaintext fields are `v`, `type`, `pub`, `sig`, `prev`. None of them names a
key. A node holding keys for several private chains can map `pub` to a key
*if* it tracks that mapping itself, which the protocol neither describes nor
forbids; otherwise it must try every key it holds against every private block
and read the AEAD tag as the answer.

## Findings

- `spec/04-cryptography.md`, "Encryption scheme": "**Key:** 256-bit symmetric
  key (unique per chain, not per block)".
- `spec/04-cryptography.md`, "Key management": "Each private chain uses a
  single symmetric key", the per-recipient wrapping, and "**Default use case:**
  A user's private chain is encrypted with a symmetric key known only to the
  user's own devices."
- `spec/04-cryptography.md`, "Key rotation": covers the Ed25519 identity key
  only. The symmetric key is not mentioned.
- `spec/02-block-format.md`, "Private block": the plaintext field set, which
  carries no key identifier.
- `spec/04-cryptography.md`, "Security Considerations": covers nonce reuse and
  identity-key compromise; content-key compromise and reader removal are not
  discussed.
- `go/privacy`, `KeyRing.DecryptPayload`: tries each held key in order and
  takes the first that authenticates, because the protocol gives a block no
  field to select on. The doc comment says so.

## Proposed Solutions

### Option 1: State the scope in Security Considerations (Recommended)

- Say plainly that a content key is per Ed25519 chain and shared for the life
  of that chain; that v1 has no re-keying and no revocation, so a reader who
  has been given a key can read the chain until it ends; and that a successor
  chain SHOULD use a fresh content key, since it is a new chain.
- **Pros**: costs nothing, removes the ambiguity, and puts the limitation where
  an implementer will read it.
- **Cons**: does not solve revocation, only names it as out of scope.
- **Effort**: Small (spec), none (Go)
- **Risk**: Low

### Option 2: Add a key generation to the block

- A plaintext `kid` or generation counter in the private block, letting a chain
  re-key at a block boundary and letting a reader select a key without trial
  decryption.
- **Pros**: revocation becomes expressible; key selection becomes exact.
- **Cons**: a new plaintext field is new metadata (which key era a block
  belongs to is a signal), and the field set of a v1 block is closed — this is
  a v2 change.
- **Risk**: Medium

### Option 3: Say nothing

- **Cons**: implementations diverge on the rotation question in a way that only
  shows up when someone rotates a key on a private chain, which is exactly when
  data is hardest to recover.
- **Risk**: Medium

## Recommended Action

Option 1 for v1, with Option 2 recorded as the shape a future version would
take if reader revocation is ever in scope.

## Technical Details

- **Affected Files**: `spec/04-cryptography.md` ("Encryption scheme", "Key
  management", "Key rotation", "Security Considerations")
- **Related Components**: private chains, key rotation, subscriptions
- **Database Changes**: No

## Acceptance Criteria

- [x] Whether a successor chain reuses the previous chain's content key is
      stated
- [x] The absence of re-keying and revocation in v1 is stated as scope rather
      than left to inference
- [x] How a node selects the key for a private block is described, or trial
      decryption is named as the expected behaviour

## Work Log

### 2026-08-13 - Filed While Implementing go/privacy
**By:** Claude

Found while designing the recipient side. `KeyRing` exists because a private
block offers nothing to select a key on; trial decryption is safe and cheap,
but it is a decision the implementation made rather than one it read. The
rotation question was reached from the other direction — a `Builder` for a
successor chain has no opinion about the content key, and neither does the
specification.

### 2026-08-13 - Ratified and Applied
**By:** Claude

**Decision (project lead):** Option 1, with one correction to its wording. The
key lifecycle is out of scope for v1 and the specification now says so; but
where Option 1 proposed that a successor chain SHOULD take a fresh content key,
the ratified text says it **MAY** reuse the key of the chain it succeeds, and
MAY take a fresh one.

This is what the implementation actually does, and the reason for the change:
`rotate_key` rotates the Ed25519 identity key and touches nothing symmetric,
`privacy.SealBlock` takes the content key as a parameter, and a `block.Builder`
that succeeds a rotation is handed whichever key its author chose. Neither
`go/privacy` nor `go/block` has an opinion, so a SHOULD would have been a rule
the reference implementation does not implement. No block records the choice —
a private block carries no key identifier — so it travels out of band with the
key itself, which is where the distribution mechanism already lives.

Option 2 (a `kid` or generation counter) is recorded in the specification as the
shape a future version would take, together with an explicit MUST NOT against
adding such a field to a v1 block: each block type's field set is closed.

**Changes:**

- `spec/04-cryptography.md`: a new "Content key lifecycle" subsection under
  "Private block encryption" — one key per chain (and what "unique per chain"
  does *not* mean), the MAY across rotation with the out-of-band consequence,
  trial decryption named as the expected way to select a key, and re-keying,
  revocation, key identifiers and per-block keys deferred to a future version;
  an informative note on the only form of reader removal v1 can express. The
  "Encryption scheme" key bullet points at it, and a new "Content key
  compromise" security consideration states that the key exposes the chain's
  whole history and that no forward secrecy is provided.
- `go/privacy/privacy.go`: doc-comment changes only. The package doc records
  that the lifecycle is the caller's decision within what v1 allows; `Key` notes
  that one key per chain does not mean one chain per key; `KeyRing` cites the
  specification for trial decryption instead of this issue; `Key.String` cites
  the new security consideration for not printing.
- `go/privacy/privacy_test.go`: `TestContentKeyLifecycle` confirms the model —
  one key across a chain's blocks, a successor chain opening under the inherited
  key, the AAD still refusing a payload moved between the two chains despite the
  shared key, and a `KeyRing` selecting by trial decryption and reporting an
  unreadable block when it holds no key that fits.

No behaviour changed, which is the finding: the implementation was already the
v1 model, and what was missing was the statement that this is the model.

## Notes

Source: Go reference implementation, phase 4 (privacy).
