---
status: ready
priority: p2
issue_id: "101"
tags: [specification-gap, normative-consistency, key-rotation, terminology]
dependencies: [005]
---

# Whole-Spec Normative-Keyword Consistency Sweep

## Problem Statement

Todo 005 fixed one MUST/SHOULD conflict between `spec/02-block-format.md` and
`spec/05-processing-model.md`, but its broader closing criterion — "no other
normative conflicts exist across the spec" — was asserted, not verified. This
todo performs that verification: every normative statement (MUST, MUST NOT,
SHOULD, SHOULD NOT, MAY, REQUIRED, RECOMMENDED, OPTIONAL) in `spec/00-overview.md`
through `spec/07-transport.md`, and every normative-sounding claim in
`README.md`, read against every other, looking for the same requirement
stated at different strengths, contradictory requirements, a normative
statement about something another document defines differently, and a
normative keyword used somewhere the Terminology section of that document
does not define it.

**Result: not clean.** One substantive conflict (contradictory MUST/implementation-scoped
requirements for the same case, traced to a single authoring commit) and one
minor terminology gap (a keyword used without being defined) were found.

## Method

1. Read all eight spec documents in full: `00-overview.md` through
   `07-transport.md` (2,930 lines total).
2. Extracted every normative statement and its subject, watching in
   particular for the same rule restated in a different document — chain
   succession, fork detection, scan limit default, revalidation-on-arrival,
   private-block reference rules, and the CID/digest/text-form distinctions
   are each stated in two or more documents and were cross-checked pairwise.
3. Grepped for the full RFC 2119 keyword set (`MUST`, `MUST NOT`, `SHOULD`,
   `SHOULD NOT`, `MAY`, `REQUIRED`, `RECOMMENDED`, `OPTIONAL`, `SHALL`) across
   `spec/*.md` to find keywords used without being in a document's own
   Terminology list.
4. Read `README.md` in full and checked its normative-sounding claims (block
   format, validation rule count, cryptography, meta-bond count and
   templates, document index) against the spec text they summarize.
5. Where a candidate conflict was found, used `git log -p -S` to find the
   commit that introduced each side, to establish whether the split was
   deliberate (two eras of the text, one superseding the other) or a single
   authoring inconsistency.

## Findings

### 1. Ambiguous succession: MUST-surface-only vs. MUST-NOT-pick-a-successor (P2 — filed as todo 102)

`spec/02-block-format.md:204` (the "rotate_key" section, "Verifiable
succession", second of the "Two further constraints follow" bullets) and
`spec/05-processing-model.md:81` (Layer 1, "Chain succession (key rotation)")
both describe what a node does when more than one genesis block references
the same rotation block, and they say different things about whether a node
may pick one as *the* successor on its own:

- `spec/02-block-format.md:204`: "the handling strategy (reject, flag,
  **accept-first-seen**) is **implementation-scoped**, exactly as in
  validation rule 9" — i.e. a node MAY silently treat one candidate as the
  successor, provided it also detects and surfaces the ambiguity.
- `spec/05-processing-model.md:81`: "the node MUST surface the conflict as it
  surfaces a fork, and **MUST NOT pick a successor on its own**" — i.e. the
  same "accept-first-seen" strategy that the other document permits is
  explicitly forbidden here.
- `spec/05-processing-model.md:244` ("Assertion order") restates the
  `05-processing-model.md:81` reading: "the node MUST surface the conflict
  rather than pick a successor."
- `spec/07-transport.md:380` ("Chain succession", client rules) also sides
  with the stricter reading: "the node MUST surface it rather than pick."

`git log -p -S "MUST NOT pick a successor" -- spec/05-processing-model.md`
shows both halves of the split were written in the **same commit**
(`0a8266c`, "spec: make key succession verifiable (todo 042)"): the
`02-block-format.md` hunk of that commit added the "implementation-scoped...
accept-first-seen" language, and the `05-processing-model.md` hunk of the
same commit added "MUST NOT pick a successor on its own" — an authoring
inconsistency within one commit rather than a drift between two. Todo 042's
own ratified decision text (`todos/042-complete-p3-key-succession-linkage-is-only-a-should.md`,
"Ratified and Applied", item 3) matches the `02-block-format.md` reading
("resolution implementation-scoped"), so `05-processing-model.md`'s stronger
prohibition appears to be the one that drifted from what was actually
decided, not the other way around.

`go/block/chain.go`'s `Successors` function follows the `02-block-format.md`
reading: its doc comment says the ambiguous case is "reported, not resolved"
and the function reports the fork without forbidding a caller from choosing
one candidate.

This is exactly the shape of conflict todo 005 fixed (a MUST-level
requirement contradicted by an implementation-scoped one for the same case)
and is severe enough — it bears on what a conforming implementation is and
is not permitted to do during key rotation, a security-relevant path — to
warrant its own tracking todo. Filed as `todos/102`.

### 2. `OPTIONAL` used without being defined (P3, minor)

Every spec document's Terminology section states: "The key words 'MUST',
'MUST NOT', 'SHOULD', 'SHOULD NOT', and 'MAY' are to be interpreted as
described in RFC 2119" (see e.g. `spec/07-transport.md:13`), and
`spec/00-overview.md:85` lists the same five keywords under "Conventions".
Neither list includes `OPTIONAL`, `REQUIRED`, `RECOMMENDED`, or `SHALL`.

`spec/07-transport.md` nonetheless uses capitalized `OPTIONAL` as an RFC 2119
keyword six times:

- `spec/07-transport.md:112` — "`announce` is OPTIONAL: a read-only mirror is
  conforming."
- `spec/07-transport.md:113` — "An OPTIONAL operation a server does not offer
  answers 404" (and again later in the same sentence).
- `spec/07-transport.md:263` — the 404 status-code table entry.
- `spec/07-transport.md:281` — the `operation-not-offered` problem-type table
  entry.
- `spec/07-transport.md:299` — "OPTIONAL; a server that does not implement it
  MUST ignore the parameter" (long polling).
- `spec/07-transport.md:300` — "OPTIONAL; a server that does not implement it
  answers 404" (the tip event stream).

No other spec document uses `OPTIONAL`, `REQUIRED`, `RECOMMENDED`, or `SHALL`
as a capitalized keyword (checked with `grep -n "OPTIONAL\|REQUIRED\|RECOMMENDED\|SHALL\b" spec/*.md`),
so this is confined to `spec/07-transport.md`. The meaning is unambiguous in
context — `OPTIONAL` reads the same as RFC 2119's definition, which is a
synonym for MAY applied to a whole feature — and no reading of the affected
sentences would change if the word were defined. This is a terminology-list
omission rather than a substantive conflict: nothing contradicts `OPTIONAL`'s
plain RFC 2119 meaning, but a document that names RFC 2119 as its normative
authority and then uses one of that RFC's keywords without listing it invites
exactly the kind of doubt this sweep was commissioned to rule out.

**Suggested fix**, for whoever next has write access to `spec/`: add
`OPTIONAL` to `spec/07-transport.md`'s Terminology list (the only document
that needs it), e.g. "...and 'MAY', and the key word 'OPTIONAL', are to be
interpreted as described in RFC 2119."

### Considered and ruled out

- **`Dialog-Tip` header: "MUST carry" (line 203) vs. "omits the header" (line
  214).** `spec/07-transport.md:203` states "A 200 response to `tip` or
  `range` MUST carry a `Dialog-Tip` header," and `spec/07-transport.md:211-214`
  ("Where the server holds no tip") states that a `range` 200 response with
  no tip for that author "omits the header." Read in isolation the two look
  like a MUST contradicted three paragraphs later. In context this is
  ordinary general-rule-then-exception drafting, not a drift: the exception
  is in the same section, immediately follows the general rule, and is
  accompanied by an explicit rationale for why an empty/null header value
  was rejected in favour of omission ("this profile mints no second spelling
  of a position"). Not counted as a conflict.
- **README.md.** Checked its block-format summary, validation-rule count
  ("the ten validation rules" — `02-block-format.md` does define exactly ten),
  cryptography summary, and the five meta-bond templates against the spec
  text they summarize; all matched. README does not itself use RFC 2119
  keywords normatively (its "should" in "a minimal set of meta-bonds all
  implementations should support" is lowercase, conversational, and README
  states no RFC 2119 convention of its own), so it carries no normative
  claim capable of conflicting with the spec.
- **Scan-limit default (256) and its keyword (SHOULD).** Stated with `SHOULD`
  in `05-processing-model.md` (twice) and referenced descriptively (no
  keyword, "is user-configurable, and defaults to 256") in
  `02-block-format.md` and `07-transport.md` (twice more). The descriptive
  references never restate it as MUST or otherwise change its strength, so
  this is consistent, not a conflict.
- **Private-chain nonce requirements** (`MAY` be counter-or-random for block
  encryption vs. `MUST` be freshly random for key wrapping, in
  `04-cryptography.md`). Different strengths for different operations, and
  the document explicitly explains why wrapping's requirement is the
  stronger one ("The requirement is stronger here than for block
  encryption"). Not a conflict — a deliberately reasoned distinction.

## Acceptance Criteria

- [x] Every normative statement in `spec/00-overview.md` through
      `spec/07-transport.md` has been read and cross-checked against every
      other document for conflicting strength or contradictory content
- [x] `README.md`'s normative-sounding claims have been checked against the
      spec text they summarize
- [x] Every use of an RFC 2119 keyword (including `OPTIONAL`, `REQUIRED`,
      `RECOMMENDED`, `SHALL`) has been checked against the Terminology
      section of the document it appears in
- [ ] The ambiguous-succession conflict (finding 1) is resolved in the spec
      text — tracked as `todos/102`
- [ ] `OPTIONAL` is added to `spec/07-transport.md`'s Terminology list
      (finding 2)

## Work Log

### 2026-08-20 - Swept the Whole Spec

**By:** Claude

Read all eight spec documents and README.md in full (2,930 + 287 lines) and
cross-checked every normative statement pairwise for conflicts, per the
method above. Todo 005's closing criterion — that no other normative
conflicts exist — does not hold: one substantive conflict was found (finding
1, filed as `todos/102`) and one minor terminology gap (finding 2, left open
here since this agent's writable scope was `todos/` only and could not edit
`spec/`). Three other candidates were investigated and ruled out (see
"Considered and ruled out"). No conflicts were found in `README.md`.

## Notes

Source: requested sweep of todo 005's unverified closing criterion. This
agent's writable scope was `todos/` only; the fixes this todo and `todos/102`
describe require write access to `spec/`, which a future session should
apply.
