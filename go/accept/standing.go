package accept

import (
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// a standingRecord is one statement an author made about a meta-molecule,
// placed in that author's block order: publishing it, asserting it true, or
// retracting it.
type standingRecord struct {
	author  authorKey
	pos     position
	retract bool
}

// compareStandingRecords groups an author's statements by lineage and orders
// them within it, which is the only order this file needs and one that does not
// depend on the order the blocks arrived in.
func compareStandingRecords(a, b standingRecord) int {
	if c := compareAuthorKeys(a.author, b.author); c != 0 {
		return c
	}
	if c := compareDigests(a.pos.lineage, b.pos.lineage); c != 0 {
		return c
	}
	return a.pos.index - b.pos.index
}

// applyStanding drops the equivalences, contradictions and supersessions whose
// authors have all taken them back, and records them for
// View.WithdrawnMetaMolecules.
//
// "Implementations MUST apply a meta-molecule's semantics while at least one
// subscribed author who published it still backs it [and] MUST NOT apply them
// once every subscribed author who published it has withdrawn their backing"
// (spec/06-meta-bonds.md, "Withdrawing meta-molecules"). The rules that section
// states, and how each is read here:
//
//   - Publication is backing. An author who published a meta-molecule and said
//     nothing further about it backs it, which is why an unasserted
//     meta-molecule — almost every one there is — applies.
//
//   - An explicit "«M» is true" from a publishing author is backing too, and
//     "«M» is untrue" from one withdraws theirs. Which of an author's
//     statements counts is the later-wins reading of block order that settles
//     any other molecule (spec/05-processing-model.md, "Assertion order"), so
//     re-publishing or re-asserting after a retraction backs it once more.
//
//   - A retraction by a subscribed author who never published the
//     meta-molecule does not withdraw it: nobody has a veto over another
//     author's declaration. Such a retraction is a disagreement about the
//     meta-molecule, and applyTruth surfaces it exactly as it surfaces a
//     disagreement about any other molecule.
//
//   - The truth meta-molecules are not gated. A retraction of a retraction is
//     one author restating their position, which block order already settles,
//     and gating them would start a regress with no bottom. So c.truths is
//     passed through untouched.
//
// The gate reads the assertions naming the meta-molecule itself rather than
// those naming its equivalence class, and runs before closeEquivalences:
// standing decides what the closure contains, so consulting the closure here
// would define it in terms of itself (spec/06-meta-bonds.md, "Withdrawing
// meta-molecules"). Nothing is deleted either way — a withdrawn meta-molecule
// stays an entity of the view, with its authorship and its own truth state.
func applyStanding(v *View, c *claims, order *blockOrder) error {
	// The truth claims by subject, so that the statements about one
	// meta-molecule are found without scanning them all again per claim. Built
	// in the order read produced, which is digest order.
	bySubject := make(map[cid.Digest][]claim, len(c.truths))
	for _, t := range c.truths {
		bySubject[t.a] = append(bySubject[t.a], t)
	}

	var withdrawn digestSet
	keep := func(gated []claim) ([]claim, error) {
		out := gated[:0:0]
		for _, cl := range gated {
			stands, err := standing(cl, bySubject, order)
			if err != nil {
				return nil, err
			}
			if stands {
				out = append(out, cl)
				continue
			}
			withdrawn.add(cl.meta)
		}
		return out, nil
	}

	var err error
	if c.equivalences, err = keep(c.equivalences); err != nil {
		return err
	}
	if c.contradictions, err = keep(c.contradictions); err != nil {
		return err
	}
	if c.supersessions, err = keep(c.supersessions); err != nil {
		return err
	}
	v.withdrawn = withdrawn.list()
	return nil
}

// standing reports whether at least one subscribed author who published this
// meta-molecule still backs it.
func standing(cl claim, bySubject map[cid.Digest][]claim, order *blockOrder) (bool, error) {
	publisher := make(map[authorKey]bool, len(cl.prov))
	records := make([]standingRecord, 0, len(cl.prov))
	for _, a := range cl.prov {
		author, ok := keyOf(a.Author)
		if !ok { // unreachable: an authorship tag comes from a validated block's pub field
			continue
		}
		pos, err := order.of(a.Block)
		if err != nil {
			return false, fmt.Errorf("accept: ordering the publication of %s: %w", cl.meta, err)
		}
		publisher[author] = true
		records = append(records, standingRecord{author: author, pos: pos})
	}
	if len(records) == 0 { // unreachable: an entity is in the view because a subscribed author published it
		return false, nil
	}

	for _, t := range bySubject[cl.meta] {
		for _, a := range t.prov {
			author, ok := keyOf(a.Author)
			if !ok || !publisher[author] {
				continue // only a publishing author's word bears on the backing
			}
			pos, err := order.of(a.Block)
			if err != nil {
				return false, fmt.Errorf("accept: ordering the assertion %s about %s: %w", t.meta, cl.meta, err)
			}
			records = append(records, standingRecord{
				author: author, pos: pos, retract: t.stance == Retracted,
			})
		}
	}
	slices.SortFunc(records, compareStandingRecords)

	// One group per (author, lineage) — in practice one group per author, a key
	// signing one chain. The group's last word is what that author holds; the
	// backing stands unless every record at that last position is a retraction,
	// so an author who publishes and retracts at the same point of their chain
	// has said two things no ordering settles and has withdrawn nothing.
	for start := 0; start < len(records); {
		end := start
		for end < len(records) &&
			records[end].author == records[start].author &&
			records[end].pos.lineage == records[start].pos.lineage {
			end++
		}
		last := records[end-1].pos.index // the group is sorted by index
		stands := false
		for i := start; i < end; i++ {
			if records[i].pos.index == last && !records[i].retract {
				stands = true
			}
		}
		if stands {
			return true, nil
		}
		start = end
	}
	return false, nil
}
