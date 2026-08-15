package accept

import (
	"crypto/ed25519"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// A ConflictKind names what kind of disagreement a Conflict reports.
type ConflictKind uint8

// The four conflicts this package detects. Every one of them is a disagreement
// L3 MUST surface and MUST NOT resolve on its own (spec/05-processing-model.md,
// "Meta-molecule application"; spec/06-meta-bonds.md, "Conflict handling").
const (
	// ConflictTruthDisagreement is subscribed authors disagreeing about
	// whether a molecule is true: "when one subscribed author asserts 'X is
	// true' and another asserts 'X is untrue'" (spec/05-processing-model.md).
	// It also covers one author holding both positions at the same point in
	// their chain, which no ordering can settle.
	ConflictTruthDisagreement ConflictKind = iota + 1
	// ConflictContradiction is a declared contradiction whose two molecules
	// are both in the view: "If both molecules are present in L3 [...] the
	// implementation MUST surface the contradiction to the application layer"
	// (spec/06-meta-bonds.md, "Contradiction").
	ConflictContradiction
	// ConflictSupersessionCycle is a set of molecules that supersede each
	// other in a loop, so that no member of it can be the current version.
	ConflictSupersessionCycle
	// ConflictAmbiguousSuccession is a key succession with more than one
	// claimant: "If more than one genesis block references the same rotation
	// block, the succession is ambiguous: the node MUST surface the conflict
	// as it surfaces a fork, and MUST NOT pick a successor on its own"
	// (spec/05-processing-model.md, "Chain succession"). It reaches L3
	// because block order runs through the succession
	// (spec/05-processing-model.md, "Assertion order"): an ambiguous
	// succession is an ambiguous order.
	ConflictAmbiguousSuccession
)

// String names the kind.
func (k ConflictKind) String() string {
	switch k {
	case ConflictTruthDisagreement:
		return "truth disagreement"
	case ConflictContradiction:
		return "contradiction"
	case ConflictSupersessionCycle:
		return "supersession cycle"
	case ConflictAmbiguousSuccession:
		return "ambiguous succession"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(k))
	}
}

// Stance labels are what a Side of a truth disagreement holds.
const (
	// StanceTrue is the side of the authors whose last word is "_A_ is true".
	StanceTrue = "is true"
	// StanceUntrue is the side of the authors whose last word is "_A_ is
	// untrue".
	StanceUntrue = "is untrue"
)

// A Side is one party to a conflict: what it holds, and which subscribed
// authors hold it.
type Side struct {
	// Stance is StanceTrue or StanceUntrue for a truth disagreement, and the
	// empty string for a conflict whose sides are identified by Molecule.
	Stance string
	// Molecule is the molecule this side is about — one of the two molecules
	// of a contradiction. It is the zero digest when the side is a stance
	// rather than a molecule.
	Molecule cid.Digest
	// Authors are the subscribed authors on this side, ascending by key.
	Authors []ed25519.PublicKey
}

func (s Side) String() string {
	what := s.Stance
	if what == "" {
		what = s.Molecule.String()
	}
	return fmt.Sprintf("%s: %d author(s)", what, len(s.Authors))
}

// A Conflict is a disagreement the view surfaces to the application. The
// protocol requires detection and surfacing, and requires that conflicting
// assertions are not silently discarded; it requires no particular resolution
// (spec/06-meta-bonds.md, "Conflict handling"). So this package builds these
// values and stops: nothing in it prefers one side.
//
// Every slice is in a deterministic order, and so is View.Conflicts.
type Conflict struct {
	// Kind says what kind of disagreement this is.
	Kind ConflictKind
	// Molecules are the molecules the conflict is about, ascending. For a
	// truth disagreement they are the members of the subject's equivalence
	// class that are in the view; for a contradiction, the two molecules
	// declared contradictory; for a supersession cycle, the members of the
	// cycle. An ambiguous succession is about blocks, not molecules, and
	// leaves this empty.
	Molecules []cid.Digest
	// Sides are the parties, in a deterministic order: for a truth
	// disagreement, the "is true" side first and the "is untrue" side second;
	// for a contradiction, one side per molecule in ascending digest order.
	// A supersession cycle and an ambiguous succession have no sides.
	Sides []Side
	// Meta are the meta-molecules that declared the conflict, ascending.
	Meta []cid.Digest
	// Blocks are the blocks an ambiguous succession is between, ascending:
	// the several genesis blocks claiming one rotation block, or the several
	// rotation blocks one genesis block claims. Empty for every other kind.
	Blocks []cid.Digest
	// Declarers are the subscribed authors who published Meta — or, for an
	// ambiguous succession, who signed Blocks — ascending by key.
	Declarers []ed25519.PublicKey
}

func (c Conflict) String() string {
	switch c.Kind {
	case ConflictAmbiguousSuccession:
		return fmt.Sprintf("%s between %d block(s)", c.Kind, len(c.Blocks))
	case ConflictTruthDisagreement, ConflictContradiction, ConflictSupersessionCycle:
		return fmt.Sprintf("%s over %d molecule(s), declared by %d author(s)", c.Kind, len(c.Molecules), len(c.Declarers))
	default:
		return c.Kind.String()
	}
}

// clone deep-copies a conflict, so that a caller cannot reach into the view
// through the slices it hands out.
func (c Conflict) clone() Conflict {
	out := Conflict{
		Kind:      c.Kind,
		Molecules: slices.Clone(c.Molecules),
		Meta:      slices.Clone(c.Meta),
		Blocks:    slices.Clone(c.Blocks),
		Declarers: clonePublicKeys(c.Declarers),
	}
	if c.Sides != nil {
		out.Sides = make([]Side, len(c.Sides))
		for i, s := range c.Sides {
			out.Sides[i] = Side{Stance: s.Stance, Molecule: s.Molecule, Authors: clonePublicKeys(s.Authors)}
		}
	}
	return out
}

// compareConflicts is the deterministic order View.Conflicts answers in: by
// kind, then by the molecules, blocks and meta-molecules involved. Two
// conflicts cannot agree on all four without being the same conflict.
func compareConflicts(a, b Conflict) int {
	if c := int(a.Kind) - int(b.Kind); c != 0 {
		return c
	}
	if c := slices.CompareFunc(a.Molecules, b.Molecules, compareDigests); c != 0 {
		return c
	}
	if c := slices.CompareFunc(a.Blocks, b.Blocks, compareDigests); c != 0 {
		return c
	}
	return slices.CompareFunc(a.Meta, b.Meta, compareDigests)
}

// clonePublicKeys deep-copies a key slice.
func clonePublicKeys(src []ed25519.PublicKey) []ed25519.PublicKey {
	if src == nil {
		return nil
	}
	out := make([]ed25519.PublicKey, len(src))
	for i, k := range src {
		out[i] = slices.Clone(k)
	}
	return out
}

// a keySet collects public keys without duplicates and hands them back in
// ascending order, which is how every author list in this package is built.
type keySet struct {
	seen map[authorKey]bool
	keys []authorKey
}

func (s *keySet) add(k authorKey) {
	if s.seen == nil {
		s.seen = make(map[authorKey]bool)
	}
	if s.seen[k] {
		return
	}
	s.seen[k] = true
	i, _ := slices.BinarySearchFunc(s.keys, k, compareAuthorKeys)
	s.keys = slices.Insert(s.keys, i, k)
}

func (s *keySet) len() int { return len(s.keys) }

func (s *keySet) public() []ed25519.PublicKey {
	if len(s.keys) == 0 {
		return nil
	}
	out := make([]ed25519.PublicKey, len(s.keys))
	for i, k := range s.keys {
		out[i] = k.public()
	}
	return out
}

// a digestSet is the same for digests.
type digestSet struct {
	seen    map[cid.Digest]bool
	digests []cid.Digest
}

func (s *digestSet) add(d cid.Digest) {
	if s.seen == nil {
		s.seen = make(map[cid.Digest]bool)
	}
	if s.seen[d] {
		return
	}
	s.seen[d] = true
	i, _ := slices.BinarySearchFunc(s.digests, d, compareDigests)
	s.digests = slices.Insert(s.digests, i, d)
}

func (s *digestSet) has(d cid.Digest) bool { return s.seen[d] }

func (s *digestSet) list() []cid.Digest { return slices.Clone(s.digests) }
