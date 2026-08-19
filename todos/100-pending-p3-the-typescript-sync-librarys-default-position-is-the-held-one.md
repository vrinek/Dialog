---
status: pending
priority: p3
issue_id: "100"
tags: [transport, typescript, api-design, interoperability]
dependencies: ["099"]
---

# The TypeScript Sync Library's Default Position Is the Held One

## Problem Statement

`todos/099` settled which position a client asks a source it has not synced a
chain from before: `spec/07-transport.md`, "First contact with a source", says
it SHOULD ask from the genesis position, MAY ask from the position it holds, and
SHOULD record which. The two *clients* obey it — `go/cmd/dialog-sync` and
`ts/scripts/sync.ts` both default `-from` to `genesis` — and that is what the
interop harness measures.

The two *libraries* do not agree underneath. `go/transport`'s `Syncer` has
`AskFromHeldPosition bool`, so its zero value is the genesis position and a
caller who configures nothing takes the SHOULD. `ts/`'s `syncChain` takes
`SyncOptions.from`, and omitting it means the store's own constructive tip:

```ts
let position = options.from === undefined ? (sourceTip(store, pub)?.digest ?? null) : options.from;
```

A caller who omits the option takes the MAY without saying so, and — this is the
part that matters — takes it *silently*, because the difference is invisible
until the sources disagree. It is not wrong: the profile permits both, and for a
source already synced from, the held position is not merely permitted but
correct. It is that the same library call means different things in the two
implementations, in the one place `todos/099` was filed to make them agree.

## Findings

- `ts/src/transport.ts`, `SyncOptions.from` and `syncChain`: `undefined` resolves
  to `sourceTip(store, pub)`, which is the client's own chain tip and the genesis
  position only when the store holds nothing of that author. So a first sync of
  an author defaults to genesis and a *second source* of that author defaults to
  held, which is exactly the case the profile's SHOULD is about.
- `go/transport/sync.go`, `Syncer.AskFromHeldPosition` and `resume`: the resume
  map is per source, and a source not yet asked has no entry, so the default is
  the genesis position and the held position is opt-in.
- `ts/scripts/sync.ts` passes `{ from: null }` for `-from genesis`, its default,
  which is why `interop/run.sh` sees the two clients agree.
- The doc comment on `SyncOptions.from` now says all of this (the change that
  closed `todos/099`), so the divergence is documented rather than hidden. What
  is unsettled is whether documenting it is the right answer.

## Proposed Solutions

### Option 1: Leave the default and keep the documentation (Recommended for now)

- **Pros**: no behaviour change, no test churn, and the default is the right one
  for `syncChain`'s most common call — resuming a source already synced from,
  where the held position is not a policy choice but the correct cursor.
- **Cons**: two libraries whose zero configuration means different things; a
  caller reaching for the profile's SHOULD has to know to pass `null`.
- **Effort**: none
- **Risk**: Low

### Option 2: Make the option three-valued

Replace the `Uint8Array | null | undefined` overload with an explicit
`from: "genesis" | "held" | Uint8Array`, defaulting to `"genesis"`, so the two
policies are named rather than encoded in the absence of a value.

- **Pros**: the defaults agree, the policy is named at the call site, and the
  explicit-position case stays available for a caller resuming a cursor it
  stored itself.
- **Cons**: a breaking change to a published interface, and it moves
  `scripts/sync.ts`, the transport tests and possibly the interop expectations
  if any of them relies on the current default.
- **Effort**: Small
- **Risk**: Medium — the transport tests use the default heavily.

### Option 3: Flip the default only

Make `from` omitted mean the genesis position.

- **Pros**: the smallest change that makes the two libraries agree.
- **Cons**: silently makes every resume re-download the whole chain, which is
  the one thing the per-source cursor exists to avoid. Worse than either
  alternative.
- **Risk**: High

## Recommended Action

Option 1 until something needs Option 2. The gap is real but it is an API-shape
question and not an interoperability one: no byte on the wire and no verdict in
a store depends on it, and both shipped clients already take the SHOULD. Revisit
if a third caller of `syncChain` appears, or if `SyncOptions` is being changed
for another reason anyway.

## Technical Details

- **Affected Files**: `ts/src/transport.ts` (`SyncOptions.from`, `syncChain`),
  `ts/scripts/sync.ts`, `ts/test/transport.sync.test.ts`
- **Related Components**: `spec/07-transport.md`, "First contact with a source";
  `go/transport`'s `Syncer.AskFromHeldPosition`
- **Database Changes**: No

## Acceptance Criteria

- [ ] Either the two libraries' zero configuration means the same position, or
      the reason they differ is recorded as a deliberate API choice rather than
      as an accident of what `undefined` happened to mean

## Work Log

### 2026-08-19 - Filed While Closing 099

**By:** Claude

Found while writing the doc comment 099 asked for: the sentence "both reference
implementations default to the genesis position" is true of the clients and not
of the libraries, and the doc comment had to say which it meant.

## Notes

Source: applying `todos/099`.
