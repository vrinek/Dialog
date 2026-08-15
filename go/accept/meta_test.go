package accept

import (
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// TestEquivalenceClosure covers spec/06-meta-bonds.md, "Equivalence": "If A is
// the same as B, and B is the same as C, then A, B, and C are all equivalent."
//
// Alice publishes three molecules saying the same thing three ways and two
// equivalences joining them into a chain. The class closes transitively, and an
// assertion about any member is an assertion about all of them: "Implementations
// SHOULD treat equivalent entities as interchangeable when querying L3."
func TestEquivalenceClosure(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	capital := block.MustCreateBond("_A_ is the capital of _B_")
	first, firstOps := statement(capital, "Paris, the capital of France", "France")
	second, secondOps := statement(capital, "Paris, France", "The French Republic")
	third, thirdOps := statement(capital, "The City of Light", "L'Hexagone")

	ops := []block.Operation{capital}
	ops = append(ops, firstOps...)
	ops = append(ops, secondOps...)
	ops = append(ops, thirdOps...)
	a, b, c := first.Molecule().Digest(), second.Molecule().Digest(), third.Molecule().Digest()
	ops = append(ops,
		metaBondOp(entity.TemplateEquivalence),
		sameAs(entity.FillerMolecule, a, b),
		sameAs(entity.FillerMolecule, b, c),
	)
	w.publish(alice, ops...)

	v := w.view(alice.PublicKey())
	want := []cid.Digest{a, b, c}
	slices.SortFunc(want, compareDigests)
	for _, d := range want {
		if got := v.EquivalenceClass(d); !slices.Equal(got, want) {
			t.Errorf("EquivalenceClass(%s) = %s, want the whole class %s", d, digests(got), digests(want))
		}
	}
	for _, pair := range [][2]cid.Digest{{a, b}, {b, c}, {a, c}, {c, a}} {
		if !v.Equivalent(pair[0], pair[1]) {
			t.Errorf("%s and %s are not equivalent; equivalence is symmetric and transitive", pair[0], pair[1])
		}
	}

	// An assertion about the third molecule is an assertion about the class.
	w.publish(alice, metaBondOp(entity.TemplateTruthAssertion), isTrue(c))
	v = w.view(alice.PublicKey())
	for _, d := range want {
		assertTruth(t, v, d, Asserted)
	}
	// And a later retraction of the first retracts the class with it.
	w.publish(alice, metaBondOp(entity.TemplateTruthRetraction), isUntrue(a))
	v = w.view(alice.PublicKey())
	for _, d := range want {
		assertTruth(t, v, d, Retracted)
	}

	// A molecule nobody has equated to anything is a class of one, and a
	// digest the view does not hold has no class at all.
	lone, loneOps := statement(capital, "Berlin", "Germany")
	w.publish(alice, loneOps...)
	v = w.view(alice.PublicKey())
	if got := v.EquivalenceClass(lone.Molecule().Digest()); len(got) != 1 || got[0] != lone.Molecule().Digest() {
		t.Errorf("EquivalenceClass of an unequated molecule = %s, want itself alone", digests(got))
	}
	if got := v.EquivalenceClass(cid.Digest{}); got != nil {
		t.Errorf("EquivalenceClass of a digest the view does not hold = %s, want none", digests(got))
	}
	if v.Equivalent(a, cid.Digest{}) {
		t.Error("a digest the view does not hold is equivalent to nothing")
	}
}

// TestEquivalenceOfAtomsAndBonds is the worked example of spec/06-meta-bonds.md,
// "Declaring atom equivalence" and "Declaring bond equivalence": the meta-bond
// takes any of the three entity types, as long as both fillers are the same
// one.
func TestEquivalenceOfAtomsAndBonds(t *testing.T) {
	w := newWorld(t)
	authorC := w.builder(3)

	parisA := block.MustCreateAtom("Paris, the capital of France")
	parisB := block.MustCreateAtom("Paris, France")
	capitalA := block.MustCreateBond("_A_ is the capital of _B_")
	capitalB := block.MustCreateBond("_A_ is the capital city of _B_")

	w.publish(authorC, parisA, parisB, capitalA, capitalB,
		metaBondOp(entity.TemplateEquivalence),
		sameAs(entity.FillerAtom, parisA.Atom().Digest(), parisB.Atom().Digest()),
		sameAs(entity.FillerBond, capitalA.Bond().Digest(), capitalB.Bond().Digest()),
	)

	v := w.view(authorC.PublicKey())
	if !v.Equivalent(parisA.Atom().Digest(), parisB.Atom().Digest()) {
		t.Error("the two Paris atoms are not equivalent")
	}
	if !v.Equivalent(capitalA.Bond().Digest(), capitalB.Bond().Digest()) {
		t.Error("the two capital-of bonds are not equivalent")
	}
	// Two classes, not one: nothing joins an atom to a bond.
	if v.Equivalent(parisA.Atom().Digest(), capitalA.Bond().Digest()) {
		t.Error("an atom and a bond ended up in one equivalence class")
	}
}

// TestMalformedMetaMoleculesAreIgnored covers the case a bond digest cannot
// rule out: a molecule whose bond is a standard meta-bond and whose fillers do
// not fit its template.
//
// Block validation checks the number of fillers against the bond's variable
// count and the shape of each filler, never the filler types a particular bond
// expects (spec/02-block-format.md, "Validation" rule 5), so every molecule here
// is publishable and valid. L3 declines to read them as the assertions their
// bonds name, and they stay in the view as plain molecules.
func TestMalformedMetaMoleculesAreIgnored(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice := w.builder(1)

	// "_A_ is true" over an atom, where the template wants a molecule.
	truthOfAnAtom := block.MustCreateMolecule(entity.MetaBondTruthAssertion, []entity.Filler{
		entity.AtomFiller(s.paris.Atom().Digest()),
	})
	// "_A_ is the same as _B_" across two types, which the specification
	// forbids: "Both fillers MUST be the same type".
	crossKind := block.MustCreateMolecule(entity.MetaBondEquivalence, []entity.Filler{
		entity.AtomFiller(s.paris.Atom().Digest()),
		entity.MoleculeFiller(s.digest()),
	})
	// "_A_ supersedes _B_" over a bond.
	supersedesABond := block.MustCreateMolecule(entity.MetaBondSupersession, []entity.Filler{
		entity.MoleculeFiller(s.digest()),
		entity.BondFiller(s.capital.Bond().Digest()),
	})

	w.publish(alice, append(s.ops(),
		metaBondOp(entity.TemplateTruthAssertion),
		metaBondOp(entity.TemplateEquivalence),
		metaBondOp(entity.TemplateSupersession),
		truthOfAnAtom, crossKind, supersedesABond)...)

	v := w.view(alice.PublicKey())
	want := []cid.Digest{
		truthOfAnAtom.Molecule().Digest(),
		crossKind.Molecule().Digest(),
		supersedesABond.Molecule().Digest(),
	}
	slices.SortFunc(want, compareDigests)
	if got := v.MalformedMetaMolecules(); !slices.Equal(got, want) {
		t.Errorf("MalformedMetaMolecules = %s, want %s", digests(got), digests(want))
	}
	// None of them means anything.
	assertTruth(t, v, s.digest(), Unasserted)
	if got := v.Truth(s.paris.Atom().Digest()); got != Unasserted {
		t.Errorf("the atom is %s; a truth meta-bond over an atom asserts nothing", got)
	}
	if v.Equivalent(s.paris.Atom().Digest(), s.digest()) {
		t.Error("a cross-type equivalence was applied")
	}
	if v.IsSuperseded(s.capital.Bond().Digest()) || len(v.Supersedes(s.digest())) != 0 {
		t.Error("a supersession over a bond was applied")
	}
	// They are molecules of the view like any other, all the same.
	for _, d := range want {
		if !v.Has(d) {
			t.Errorf("malformed meta-molecule %s was dropped from the view", d)
		}
	}
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("a malformed meta-molecule is not a conflict: %v", got)
	}
}

// TestContradictionIsSurfaced covers the MUST of spec/06-meta-bonds.md,
// "Contradiction": "If both molecules are present in L3 (asserted by subscribed
// authors), the implementation MUST surface the contradiction to the application
// layer."
//
// Which of the two is true, if either, is not decided here: "Resolution strategy
// is implementation-scoped."
func TestContradictionIsSurfaced(t *testing.T) {
	w := newWorld(t)
	alice, carol := w.builder(1), w.builder(4)

	capital := block.MustCreateBond("_A_ is the capital of _B_")
	first, firstOps := statement(capital, "Paris", "France")
	second, secondOps := statement(capital, "Lyon", "France")
	a, b := first.Molecule().Digest(), second.Molecule().Digest()

	ops := []block.Operation{capital}
	ops = append(ops, firstOps...)
	ops = append(ops, secondOps...)
	ops = append(ops, metaBondOp(entity.TemplateContradiction), contradicts(a, b))
	w.publish(alice, ops...)

	v := w.view(alice.PublicKey())
	conflicts := v.ConflictsOfKind(ConflictContradiction)
	if len(conflicts) != 1 {
		t.Fatalf("%d contradiction(s), want 1: %v", len(conflicts), v.Conflicts())
	}
	want := []cid.Digest{a, b}
	slices.SortFunc(want, compareDigests)
	if !slices.Equal(conflicts[0].Molecules, want) {
		t.Errorf("the conflict is about %s, want %s", digests(conflicts[0].Molecules), digests(want))
	}
	if len(conflicts[0].Sides) != 2 {
		t.Fatalf("%d side(s), want one per molecule", len(conflicts[0].Sides))
	}
	for i, side := range conflicts[0].Sides {
		if side.Molecule != want[i] {
			t.Errorf("side %d is about %s, want %s", i, side.Molecule, want[i])
		}
		if len(side.Authors) != 1 {
			t.Errorf("side %d names %d author(s), want Alice alone", i, len(side.Authors))
		}
	}
	// The relation is symmetric: each molecule names the other.
	if got := v.Contradictions(a); len(got) != 1 || got[0] != b {
		t.Errorf("Contradictions(%s) = %s, want %s", a, digests(got), b)
	}
	if got := v.Contradictions(b); len(got) != 1 || got[0] != a {
		t.Errorf("Contradictions(%s) = %s, want %s", b, digests(got), a)
	}
	// Neither molecule is thereby untrue: the protocol surfaces and stops.
	assertTruth(t, v, a, Unasserted)
	assertTruth(t, v, b, Unasserted)

	// A contradiction one of whose molecules is not in L3 is not surfaced:
	// "If both molecules are present in L3". Carol republishes the pair and
	// the contradiction; a view subscribing only to her, over a graph where
	// she published only one of the two, has nothing to surface.
	w2 := newWorld(t)
	aliceBlock := w2.publish(w2.builder(1), ops...)
	w2.publishRefs(carol, []cid.Digest{aliceBlock.Digest()}, second, contradicts(a, b))
	only := w2.view(carol.PublicKey())
	if !only.Has(b) || only.Has(a) {
		t.Fatalf("the view holds %d entities; want Carol's molecule and not Alice's", only.Len())
	}
	if got := only.ConflictsOfKind(ConflictContradiction); len(got) != 0 {
		t.Errorf("a contradiction with one molecule out of view was surfaced: %v", got)
	}
	if got := only.Contradictions(b); len(got) != 0 {
		t.Errorf("Contradictions(%s) = %s, want none", b, digests(got))
	}
}

// TestSupersessionChain covers spec/06-meta-bonds.md, "Supersession": "Declares
// that molecule A replaces molecule B. [...] If both A and B are in L3,
// implementations SHOULD present A and hide or deprecate B."
//
// A supersedes B supersedes C. Every superseded molecule stays in the view and
// is marked; Current follows the chain to the molecule at the end of it.
func TestSupersessionChain(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	bond := block.MustCreateBond("_A_ has population _B_")
	third, thirdOps := statement(bond, "Paris", "2,100,000 (1990)")
	second, secondOps := statement(bond, "Paris", "2,150,000 (2010)")
	first, firstOps := statement(bond, "Paris", "2,102,650 (2023)")
	a, b, c := first.Molecule().Digest(), second.Molecule().Digest(), third.Molecule().Digest()

	ops := []block.Operation{bond}
	ops = append(ops, thirdOps...)
	ops = append(ops, secondOps...)
	ops = append(ops, firstOps...)
	ops = append(ops, metaBondOp(entity.TemplateSupersession), supersedes(b, c), supersedes(a, b))
	w.publish(alice, ops...)

	v := w.view(alice.PublicKey())
	for _, tc := range []struct {
		d          cid.Digest
		superseded bool
		by         []cid.Digest
		replaces   []cid.Digest
	}{
		{a, false, nil, []cid.Digest{b}},
		{b, true, []cid.Digest{a}, []cid.Digest{c}},
		{c, true, []cid.Digest{b}, nil},
	} {
		if got := v.IsSuperseded(tc.d); got != tc.superseded {
			t.Errorf("IsSuperseded(%s) = %v, want %v", tc.d, got, tc.superseded)
		}
		if got := v.SupersededBy(tc.d); !slices.Equal(got, tc.by) {
			t.Errorf("SupersededBy(%s) = %s, want %s", tc.d, digests(got), digests(tc.by))
		}
		if got := v.Supersedes(tc.d); !slices.Equal(got, tc.replaces) {
			t.Errorf("Supersedes(%s) = %s, want %s", tc.d, digests(got), digests(tc.replaces))
		}
		// The chain is followed transitively: everything's current version is
		// the head of the chain.
		if got := v.Current(tc.d); len(got) != 1 || got[0] != a {
			t.Errorf("Current(%s) = %s, want %s", tc.d, digests(got), a)
		}
	}
	// Nothing was removed. "Hide or deprecate" is the application's move, and
	// it needs the molecule to still be there to make it.
	for _, d := range []cid.Digest{a, b, c} {
		if !v.Has(d) {
			t.Errorf("superseded molecule %s was dropped from the view", d)
		}
	}
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("a supersession chain is not a conflict: %v", got)
	}
	// Accepted is the convenience that acts on the marks.
	accepted := v.Accepted()
	if slices.Contains(accepted, b) || slices.Contains(accepted, c) {
		t.Errorf("Accepted = %s, want the superseded molecules left out", digests(accepted))
	}
	if !slices.Contains(accepted, a) {
		t.Errorf("Accepted = %s, want the current molecule %s in it", digests(accepted), a)
	}
}

// TestSupersessionCycleIsSurfaced is three molecules that replace each other in
// a loop. No member of a cycle can be the current version of anything, so there
// is no reading of it that the specification's "present A and hide B" survives:
// it is a disagreement, and it is surfaced.
func TestSupersessionCycleIsSurfaced(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	bond := block.MustCreateBond("_A_ has population _B_")
	first, firstOps := statement(bond, "Paris", "one")
	second, secondOps := statement(bond, "Paris", "two")
	third, thirdOps := statement(bond, "Paris", "three")
	a, b, c := first.Molecule().Digest(), second.Molecule().Digest(), third.Molecule().Digest()

	ops := []block.Operation{bond}
	ops = append(ops, firstOps...)
	ops = append(ops, secondOps...)
	ops = append(ops, thirdOps...)
	ops = append(ops, metaBondOp(entity.TemplateSupersession),
		supersedes(a, b), supersedes(b, c), supersedes(c, a))
	w.publish(alice, ops...)

	v := w.view(alice.PublicKey())
	cycles := v.ConflictsOfKind(ConflictSupersessionCycle)
	if len(cycles) != 1 {
		t.Fatalf("%d supersession cycle(s), want 1: %v", len(cycles), v.Conflicts())
	}
	want := []cid.Digest{a, b, c}
	slices.SortFunc(want, compareDigests)
	if !slices.Equal(cycles[0].Molecules, want) {
		t.Errorf("the cycle is %s, want all three molecules %s", digests(cycles[0].Molecules), digests(want))
	}
	if len(cycles[0].Meta) != 3 {
		t.Errorf("the cycle names %d meta-molecule(s), want the three edges", len(cycles[0].Meta))
	}
	if len(cycles[0].Declarers) != 1 {
		t.Errorf("the cycle names %d declarer(s), want Alice alone", len(cycles[0].Declarers))
	}
	for _, d := range want {
		if got := v.Current(d); got != nil {
			t.Errorf("Current(%s) = %s; a molecule in a cycle has no current version", d, digests(got))
		}
		if !v.IsSuperseded(d) {
			t.Errorf("%s is in a cycle and is therefore superseded", d)
		}
	}
}

// TestSupersessionOfOutOfViewMoleculesIsIgnored is the other half of "If both A
// and B are in L3": a supersession naming a molecule filtering left out has
// nothing to hide. See todo 054.
func TestSupersessionOfOutOfViewMoleculesIsIgnored(t *testing.T) {
	w := newWorld(t)
	carol, bob := w.builder(4), w.builder(2)

	bond := block.MustCreateBond("_A_ has population _B_")
	old, oldOps := statement(bond, "Paris", "2,150,000 (2010)")
	fresh, freshOps := statement(bond, "Paris", "2,102,650 (2023)")

	ops := []block.Operation{bond}
	ops = append(ops, oldOps...)
	carolBlock := w.publish(carol, ops...)

	bobOps := slices.Clone(freshOps)
	bobOps = append(bobOps, metaBondOp(entity.TemplateSupersession),
		supersedes(fresh.Molecule().Digest(), old.Molecule().Digest()))
	w.publishRefs(bob, []cid.Digest{carolBlock.Digest()}, bobOps...)

	// Bob alone: Carol's molecule is not in his view, so the supersession has
	// no target.
	v := w.view(bob.PublicKey())
	if v.Has(old.Molecule().Digest()) {
		t.Fatal("Carol's molecule is in a view that does not subscribe to her")
	}
	if got := v.Supersedes(fresh.Molecule().Digest()); len(got) != 0 {
		t.Errorf("Supersedes = %s, want none — the superseded molecule is out of view", digests(got))
	}

	// Subscribing to Carol brings it in, and the edge with it.
	both := w.view(bob.PublicKey(), carol.PublicKey())
	if !both.IsSuperseded(old.Molecule().Digest()) {
		t.Error("with both authors subscribed, the older molecule must be marked superseded")
	}
	if got := both.Current(old.Molecule().Digest()); len(got) != 1 || got[0] != fresh.Molecule().Digest() {
		t.Errorf("Current = %s, want %s", digests(got), fresh.Molecule().Digest())
	}
}
