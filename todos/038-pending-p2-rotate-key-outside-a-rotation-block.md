---
status: pending
priority: p2
issue_id: "038"
tags: [specification-gap, block-format, key-rotation, security]
dependencies: ["015"]
---

# Nothing Forbids a rotate_key Operation Outside a Rotation Block

## Problem Statement

`spec/02-block-format.md` defines the operation union as

```cddl
operation = create-atom / create-bond / create-molecule / rotate-key
```

and a public block's `ops` as `[+ operation]`. A public block whose operations
include a `rotate_key` therefore satisfies the CDDL. The rotation block's own
definition says it "MUST contain exactly one `rotate_key` operation and no
other operations", which constrains rotation blocks — not the other two types.

So the specification does not say what this block means:

```
{"v": 1, "type": "public", ..., "ops": [
   {"op": "create_atom", "description": "France"},
   {"op": "rotate_key", "new_pub": <32 bytes>}]}
```

Does the author's chain end here? The `rotate_key` prose says "The rotation
block is the last block in the current key's chain" and "Implementations MUST
NOT accept further blocks signed by the old key after the rotation block",
both phrased in terms of the *block*, not the operation. A node that keys the
rule off the block type keeps accepting the author's later blocks; a node that
keys it off the operation stops. The two then disagree about which blocks
exist in the chain — a partition an attacker can trigger deliberately, and the
kind of ambiguity that issue #15 introduced the `type` field to remove.

## Findings

- `spec/02-block-format.md` "Operations": the union includes `rotate-key`, and
  nothing scopes it to one block type.
- `spec/02-block-format.md` "Rotation block": constrains rotation blocks only.
- `spec/02-block-format.md` "Validation dispatch": `"rotation"`: `ops` contains
  exactly one `rotate_key` operation. Says nothing about `"public"` or
  `"private"` carrying one.
- `spec/02-block-format.md` "rotate_key" and `spec/05-processing-model.md`
  "Chain succession": both describe the effect in terms of "a rotation block",
  which is evidence for the intended reading but is not a MUST NOT.
- The private case is worse than the public one: a private block's `ops` are
  encrypted, so a `rotate_key` inside one would end the chain for recipients
  and not for anybody else.

## Proposed Solutions

### Option 1: Confine rotate_key to rotation blocks (Recommended)

- Add to "Operations": "A `rotate_key` operation MUST appear only in a block
  whose `type` is `"rotation"`. Implementations MUST reject a public or private
  block carrying one."
- Optionally split the CDDL so the union used by `public-block` and
  `private-block` is `create-atom / create-bond / create-molecule`, and only
  `rotation-block` admits `rotate-key`. Then the grammar says it too.
- **Pros**: the block type alone tells every node, including one that cannot
  decrypt, where a chain ends. That is precisely what the `type` field is for.
- **Cons**: none identified.
- **Effort**: Small (spec), none (Go — already implemented this way)
- **Risk**: Low

### Option 2: Define what a rotate_key in a public block means

- e.g. "a `rotate_key` anywhere in a block ends the chain at that block".
- **Cons**: a private block can then end a chain invisibly, and the rotation
  block type loses its purpose.
- **Risk**: Medium

## Recommended Action

Option 1, with the CDDL split so that the constraint is expressed in the
grammar rather than only in prose.

## Technical Details

- **Affected Files**: `spec/02-block-format.md` ("Operations" CDDL and prose,
  "Validation dispatch", "Validation")
- **Related Components**: block validation, chain succession, key rotation
- **Database Changes**: No

## Acceptance Criteria

- [ ] The specification says whether a non-rotation block may carry a
      `rotate_key` operation
- [ ] If not, the CDDL for public and private blocks excludes it
- [ ] `go/block` matches the ratified rule

## Work Log

### 2026-08-13 - Filed While Implementing go/block
**By:** Claude

`Content.Validate` rejects a `rotate_key` in a public block, on the reading
that chain-ending semantics belong to the block type the `type` field
announces. The choice is recorded here rather than presented as the
specification's. The private case is not checkable at all until the privacy
package exists, which is a second reason to forbid the operation outright.

## Notes

Source: Go reference implementation, phase 3 (block).
