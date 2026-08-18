package accept

import (
	"crypto/ed25519"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// A TruthState is what a view holds about a molecule, from the truth
// meta-molecules of spec/06-meta-bonds.md that subscribed authors published
// about it.
type TruthState uint8

// The four states. Asserted and Retracted are also the two stances an
// Assertion can take.
const (
	// Unasserted is a molecule no subscribed author has said anything about,
	// and the state of any digest the view does not hold. It is not the same
	// as false: the protocol has no negation, only assertion and retraction.
	Unasserted TruthState = iota
	// Asserted is "A molecule asserted as true by a subscribed author SHOULD
	// be treated as factual in L3" (spec/06-meta-bonds.md, "Truth
	// assertion"), with no subscribed author holding the opposite.
	Asserted
	// Retracted is "A molecule asserted as untrue by a subscribed author
	// SHOULD be excluded or flagged in L3" (spec/06-meta-bonds.md, "Truth
	// retraction"), with no subscribed author holding the opposite.
	Retracted
	// Conflicted is subscribed authors disagreeing. The protocol requires it
	// to be surfaced and forbids resolving it here, so it is a state of its
	// own rather than a winner: see View.Conflicts.
	Conflicted
)

// String names the state.
func (s TruthState) String() string {
	switch s {
	case Unasserted:
		return "unasserted"
	case Asserted:
		return "asserted"
	case Retracted:
		return "retracted"
	case Conflicted:
		return "conflicted"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(s))
	}
}

// An Assertion is one truth meta-molecule, published by one subscribed author
// in one block, as it bears on a molecule of the view.
type Assertion struct {
	// Author is the subscribed author who published the meta-molecule.
	Author ed25519.PublicKey
	// Stance is Asserted for "_A_ is true" and Retracted for "_A_ is untrue".
	Stance TruthState
	// Meta is the digest of the meta-molecule.
	Meta cid.Digest
	// Block is the block the author published it in — the provenance tag of
	// spec/05-processing-model.md, "Accumulation rules", step 3, and what
	// gives the assertion its place in the author's block order.
	Block cid.Digest
	// Subject is the molecule the meta-molecule names. It may be a different
	// molecule from the one queried, when the two are equivalent: an
	// assertion applies across an equivalence class.
	Subject cid.Digest
	// Latest reports whether this assertion is its author's last word — the
	// one that decided their position. An author who asserts and later
	// retracts leaves two assertions, of which only the retraction is latest
	// ("the later assertion (by block order) takes precedence",
	// spec/06-meta-bonds.md, "Truth retraction").
	Latest bool
}

func (a Assertion) String() string {
	suffix := ""
	if a.Latest {
		suffix = ", latest"
	}
	return fmt.Sprintf("%x %s %s (in block %s%s)", a.Author[:8], a.Stance, a.Subject, a.Block, suffix)
}

// a truthRecord is one assertion placed in its author's block order.
type truthRecord struct {
	class   cid.Digest // the equivalence class the assertion lands on
	lineage cid.Digest // the logical author, as blockOrder identifies one
	index   int        // the position of the block in that lineage
	author  authorKey
	stance  TruthState
	meta    cid.Digest
	block   cid.Digest
	subject cid.Digest
	latest  bool
}

// compareTruthRecords is the deterministic order the records are processed in.
func compareTruthRecords(a, b truthRecord) int {
	if c := compareDigests(a.class, b.class); c != 0 {
		return c
	}
	if c := compareDigests(a.lineage, b.lineage); c != 0 {
		return c
	}
	if c := a.index - b.index; c != 0 {
		return c
	}
	if c := compareAuthorKeys(a.author, b.author); c != 0 {
		return c
	}
	if c := int(a.stance) - int(b.stance); c != 0 {
		return c
	}
	if c := compareDigests(a.meta, b.meta); c != 0 {
		return c
	}
	if c := compareDigests(a.block, b.block); c != 0 {
		return c
	}
	return compareDigests(a.subject, b.subject)
}

// applyTruth computes the truth state of every equivalence class the view's
// truth meta-molecules bear on, and surfaces the disagreements.
//
// The rules, in the order they are applied:
//
//  1. Only subscribed authors' assertions count, and only about molecules the
//     view holds. An assertion about a molecule that filtering left out of the
//     view has no effect while the molecule is absent, and takes effect on a
//     later rebuild that finds it present: "a subscribed author's meta-molecule
//     about a subject the view does not hold has no L3 effect while the subject
//     is absent" (spec/05-processing-model.md, "Meta-molecule application"). It
//     never admits its own subject, which is the subscription's business alone.
//
//  2. An assertion applies across the equivalence class of the molecule it
//     names: if A is the same as B, "A is true" is a statement about B as well.
//     The class, not the molecule, is what carries a truth state — the
//     reference reading of "interchangeable" (spec/06-meta-bonds.md,
//     "Equivalence").
//
//  3. Within one logical author, the later assertion wins: "If the same author
//     previously asserted the molecule as true, the later assertion (by block
//     order) takes precedence" (spec/06-meta-bonds.md, "Truth retraction").
//     Later means later in the author's chain, continuing across a key
//     rotation, and never by the block's ts (spec/05-processing-model.md,
//     "Assertion order"). An author who states both positions at one position
//     of their chain — in one block, or in two blocks of a fork — has said two
//     things at once, which no ordering settles: that is a conflict too.
//
//  4. Between authors nothing wins. Subscribed authors holding opposite
//     positions is Conflicted and a surfaced Conflict, never a resolution:
//     "Implementations MUST surface detected conflicts" and "MUST NOT silently
//     discard conflicting assertions" (spec/06-meta-bonds.md, "Conflict
//     handling").
func applyTruth(v *View, c claims, order *blockOrder) error {
	records, err := truthRecords(v, c, order)
	if err != nil {
		return err
	}
	slices.SortFunc(records, compareTruthRecords)
	markLatest(records)

	for start := 0; start < len(records); {
		end := start
		for end < len(records) && records[end].class == records[start].class {
			end++
		}
		v.applyClass(records[start:end])
		start = end
	}
	return nil
}

// truthRecords places every truth claim of the view in its author's block
// order.
func truthRecords(v *View, c claims, order *blockOrder) ([]truthRecord, error) {
	var records []truthRecord
	for _, cl := range c.truths {
		if !v.Has(cl.a) {
			continue // an assertion about a molecule outside the view
		}
		class := v.class[cl.a]
		for _, a := range cl.prov {
			pos, err := order.of(a.Block)
			if err != nil {
				return nil, fmt.Errorf("accept: ordering the assertion %s: %w", cl.meta, err)
			}
			author, ok := keyOf(a.Author)
			if !ok { // unreachable: an authorship tag comes from a validated block's pub field
				continue
			}
			records = append(records, truthRecord{
				class:   class,
				lineage: pos.lineage,
				index:   pos.index,
				author:  author,
				stance:  cl.stance,
				meta:    cl.meta,
				block:   a.Block,
				subject: cl.a,
			})
		}
	}
	return records, nil
}

// markLatest flags the records that sit at the highest position of their
// lineage within their class — one author's last word about that class.
//
// "Re-publishing a meta-molecule re-states it, and an author's position on a
// molecule is the one their latest block naming it holds"
// (spec/05-processing-model.md, "Assertion order"). An author who asserts,
// retracts and asserts again therefore holds the assertion.
func markLatest(records []truthRecord) {
	for start := 0; start < len(records); {
		end := start
		for end < len(records) &&
			records[end].class == records[start].class &&
			records[end].lineage == records[start].lineage {
			end++
		}
		max := records[end-1].index // the group is sorted by index
		for i := start; i < end; i++ {
			records[i].latest = records[i].index == max
		}
		start = end
	}
}

// applyClass reduces one equivalence class's assertions to a state, records
// them for View.Assertions, and surfaces the disagreement if there is one. The
// records are sorted and all share a class.
func (v *View) applyClass(records []truthRecord) {
	var (
		asserted, retracted keySet // the authors whose last word is each stance
		assertedMeta        digestSet
		retractedMeta       digestSet
		declarers           keySet
	)
	for _, r := range records {
		declarers.add(r.author)
		if !r.latest {
			continue
		}
		switch r.stance {
		case Retracted:
			retracted.add(r.author)
			retractedMeta.add(r.meta)
		case Asserted, Unasserted, Conflicted:
			// Only Asserted and Retracted are ever stored in a record; the
			// other two cannot occur and are named so that a new state
			// breaks the build rather than falling through.
			asserted.add(r.author)
			assertedMeta.add(r.meta)
		}
	}

	class := records[0].class
	state := Asserted
	switch {
	case asserted.len() > 0 && retracted.len() > 0:
		state = Conflicted
	case retracted.len() > 0:
		state = Retracted
	}
	v.truth[class] = state

	assertions := make([]Assertion, 0, len(records))
	for _, r := range records {
		assertions = append(assertions, Assertion{
			Author:  r.author.public(),
			Stance:  r.stance,
			Meta:    r.meta,
			Block:   r.block,
			Subject: r.subject,
			Latest:  r.latest,
		})
	}
	v.assertions[class] = assertions

	if state != Conflicted {
		return
	}
	var meta digestSet
	for _, d := range assertedMeta.list() {
		meta.add(d)
	}
	for _, d := range retractedMeta.list() {
		meta.add(d)
	}
	v.conflicts = append(v.conflicts, Conflict{
		Kind:      ConflictTruthDisagreement,
		Molecules: v.classMembers[class],
		Sides: []Side{
			{Stance: StanceTrue, Authors: asserted.public()},
			{Stance: StanceUntrue, Authors: retracted.public()},
		},
		Meta:      meta.list(),
		Declarers: declarers.public(),
	})
}
