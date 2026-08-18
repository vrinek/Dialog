package accept

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
)

// fingerprint renders everything a View can be asked, in the order it answers,
// as one string.
//
// Two views over the same blocks and the same subscriptions must produce the
// same fingerprint whatever order the blocks reached L2 in. A view is rebuilt
// from scratch on demand rather than maintained, so an order that came from a
// map would differ between two rebuilds of the very same data — which is why
// this package is under the determinism guard of go/ruleguard/rules.go.
func fingerprint(v *View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "entities=%d conflicts=%d\n", v.Len(), len(v.Conflicts()))
	for _, pub := range v.Subscriptions() {
		fmt.Fprintf(&b, "subscribed %x\n", pub)
	}
	for _, e := range v.Entries() {
		d := e.Digest()
		fmt.Fprintf(&b, "entity %s %s %x\n", e.Kind(), d, e.Entity().Bytes())
		for _, a := range e.Authors() {
			pos, placed := v.BlockPosition(a.Block)
			fmt.Fprintf(&b, "  by %x in %s at %s (%v)\n", a.Author, a.Block, pos, placed)
		}
		fmt.Fprintf(&b, "  truth=%s superseded=%v\n", v.Truth(d), v.IsSuperseded(d))
		fmt.Fprintf(&b, "  class=%s\n", digests(v.EquivalenceClass(d)))
		fmt.Fprintf(&b, "  supersedes=%s by=%s current=%s\n",
			digests(v.Supersedes(d)), digests(v.SupersededBy(d)), digests(v.Current(d)))
		fmt.Fprintf(&b, "  contradicts=%s\n", digests(v.Contradictions(d)))
		for _, a := range v.Assertions(d) {
			fmt.Fprintf(&b, "  assertion %v\n", a)
		}
		writeDeclarations(&b, "equivalence", v.EquivalenceDeclarations(d))
		writeDeclarations(&b, "supersession", v.SupersessionDeclarations(d))
		writeDeclarations(&b, "contradiction", v.ContradictionDeclarations(d))
	}
	for _, k := range []block.EntityKind{block.KindAtom, block.KindBond, block.KindMolecule} {
		fmt.Fprintf(&b, "kind %s %s\n", k, digests(v.DigestsOfKind(k)))
	}
	for _, c := range v.Conflicts() {
		fmt.Fprintf(&b, "conflict %s molecules=%s blocks=%s meta=%s\n",
			c.Kind, digests(c.Molecules), digests(c.Blocks), digests(c.Meta))
		for _, s := range c.Sides {
			fmt.Fprintf(&b, "  side %s %s\n", s.Stance, digests(s.Molecules))
			for _, a := range s.Authors {
				fmt.Fprintf(&b, "    %x\n", a)
			}
		}
		for _, a := range c.Declarers {
			fmt.Fprintf(&b, "  declared by %x\n", a)
		}
	}
	fmt.Fprintf(&b, "accepted=%s\n", digests(v.Accepted()))
	fmt.Fprintf(&b, "malformed=%s\n", digests(v.MalformedMetaMolecules()))
	fmt.Fprintf(&b, "withdrawn=%s\n", digests(v.WithdrawnMetaMolecules()))
	return b.String()
}

// writeDeclarations renders one reading's provenance — the meta-molecules
// behind it and the authors still backing each, with the block positions they
// were decided by.
func writeDeclarations(b *strings.Builder, kind string, decls []Declaration) {
	for _, d := range decls {
		fmt.Fprintf(b, "  %s declaration %v\n", kind, d)
		for _, back := range d.Backing {
			fmt.Fprintf(b, "    backed %v\n", back)
		}
	}
}

// TestDeterministicView rebuilds the same view a hundred times from the same
// blocks ingested into L2 in a hundred different orders, and requires every
// query to answer identically every time.
//
// The ingestion orders would be invalid at L1 — a block arriving before its
// predecessor cannot be validated there — but the scenario's blocks were all
// validated when they were built, and neither L2 accumulation nor this package
// depends on arrival order: an entity is keyed by its digest, an authorship
// record is a set member, and every order this package answers in comes from
// the data rather than from the sequence it arrived in.
func TestDeterministicView(t *testing.T) {
	s := buildScenario(t)
	order := make([]int, len(s.blocks))
	for i := range order {
		order[i] = i
	}

	want := fingerprint(s.rebuild(order, s.subscribed()...))
	if want == "" {
		t.Fatal("the scenario produced an empty view")
	}

	rng := rand.New(rand.NewPCG(0x51a7c0de, 0x0dd1e5e5))
	for i := range 100 {
		shuffled := make([]int, len(order))
		copy(shuffled, order)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })

		if got := fingerprint(s.rebuild(shuffled, s.subscribed()...)); got != want {
			t.Fatalf("rebuild %d in order %v produced a different view:\n got %s\nwant %s", i, shuffled, got, want)
		}
	}
}

// TestRebuildingIsPure checks the other half of "recompute on demand": building
// two views from one graph, and building one twice, changes nothing. A View
// holds no reference to the graph and writes to nothing.
func TestRebuildingIsPure(t *testing.T) {
	s := buildScenario(t)
	subs := s.subscribed()

	first := fingerprint(s.view(subs...))
	second := fingerprint(s.view(subs...))
	if first != second {
		t.Errorf("two views of one graph differ:\n got %s\nwant %s", second, first)
	}

	// A narrower view does not disturb the wider one.
	_ = s.view(s.bob.PublicKey())
	if got := fingerprint(s.view(subs...)); got != first {
		t.Errorf("building a second view changed the first:\n got %s\nwant %s", got, first)
	}
	// Nor does building a view change L2.
	before := s.graph.Len()
	_ = s.view(subs...)
	if s.graph.Len() != before {
		t.Errorf("L2 grew from %d to %d entities while building a view", before, s.graph.Len())
	}
}
