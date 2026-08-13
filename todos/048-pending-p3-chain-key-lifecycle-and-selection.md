---
status: pending
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

- [ ] Whether a successor chain reuses the previous chain's content key is
      stated
- [ ] The absence of re-keying and revocation in v1 is stated as scope rather
      than left to inference
- [ ] How a node selects the key for a private block is described, or trial
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

## Notes

Source: Go reference implementation, phase 4 (privacy).
