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

// TestEquivalenceIsDeclaredNeverDerived names the rule of
// spec/06-meta-bonds.md, "Equivalence": the closure is over the pairs
// subscribed authors declared, and "no equivalence between two molecules is
// derived from an equivalence between their bonds, or between the entities
// filling them".
//
// Alice publishes the same fact twice, once with each of two bonds she has
// declared equivalent, over atoms she has declared equivalent position by
// position. Every part of the two molecules is interchangeable and the
// molecules are still two classes, with two independent truth states: her
// assertion about one leaves the other unasserted. Declaring the molecules
// themselves equivalent is what joins them.
func TestEquivalenceIsDeclaredNeverDerived(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	capitalOf := block.MustCreateBond("_A_ is the capital of _B_")
	capitalCityOf := block.MustCreateBond("_A_ is the capital city of _B_")
	first, firstOps := statement(capitalOf, "Amsterdam", "Netherlands")
	second, secondOps := statement(capitalCityOf, "Amsterdam", "Holland")

	ops := []block.Operation{capitalOf, capitalCityOf}
	ops = append(ops, firstOps...)
	ops = append(ops, secondOps...)
	ops = append(ops,
		metaBondOp(entity.TemplateEquivalence),
		sameAs(entity.FillerBond, capitalOf.Bond().Digest(), capitalCityOf.Bond().Digest()),
		sameAs(entity.FillerAtom,
			block.MustCreateAtom("Netherlands").Atom().Digest(),
			block.MustCreateAtom("Holland").Atom().Digest()),
		metaBondOp(entity.TemplateTruthAssertion),
		isTrue(first.Molecule().Digest()),
	)
	w.publish(alice, ops...)

	a, b := first.Molecule().Digest(), second.Molecule().Digest()
	v := w.view(alice.PublicKey())

	// The parts are interchangeable.
	if !v.Equivalent(capitalOf.Bond().Digest(), capitalCityOf.Bond().Digest()) {
		t.Fatal("the two bonds are not equivalent, so the test states nothing")
	}
	if !v.Equivalent(block.MustCreateAtom("Netherlands").Atom().Digest(),
		block.MustCreateAtom("Holland").Atom().Digest()) {
		t.Fatal("the two country atoms are not equivalent, so the test states nothing")
	}
	// The molecules built from them are not.
	if v.Equivalent(a, b) {
		t.Error("an equivalence between two molecules was derived from the equivalence of their parts")
	}
	if got := v.EquivalenceClass(b); !slices.Equal(got, []cid.Digest{b}) {
		t.Errorf("EquivalenceClass of the undeclared molecule = %s, want itself alone", digests(got))
	}
	// And the truth states are independent, the class being what carries one.
	assertTruth(t, v, a, Asserted)
	assertTruth(t, v, b, Unasserted)

	// Declaring the molecules equivalent is what joins them, and the assertion
	// then crosses the class.
	w.publish(alice, sameAs(entity.FillerMolecule, a, b))
	v = w.view(alice.PublicKey())
	if !v.Equivalent(a, b) {
		t.Fatal("the declared molecule equivalence did not join the two molecules")
	}
	assertTruth(t, v, b, Asserted)
}

// TestMalformedMetaMoleculesAreIgnored covers the case a bond digest cannot
// rule out: a molecule whose bond is a standard meta-bond and whose fillers do
// not fit its template.
//
// A meta-bond's Fillers line is a recognition criterion applied at L2→L3, not a
// validity rule (spec/06-meta-bonds.md, "Meta-molecules are regular molecules"):
// block validation checks the number of fillers against the bond's variable
// count and the shape of each filler, never the filler types a particular bond
// expects (spec/02-block-format.md, "Validation" rule 5), so every molecule here
// is publishable and valid. L3 declines to read them as the assertions their
// bonds name — the MUST NOT of that section — and surfaces them as plain
// molecules of the view, which is its SHOULD.
//
// The truth-of-an-atom case is pinned in bytes as the truth_of_an_atom molecule
// of vectors/entities.json, where it is a *valid* entity.
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
		// Nothing is equivalent to anything here, so every side is a class
		// of one: the molecule the meta-molecule named.
		if len(side.Molecules) != 1 || side.Molecules[0] != want[i] {
			t.Errorf("side %d is about %s, want %s", i, digests(side.Molecules), want[i])
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
// nothing to hide (spec/05-processing-model.md, "Meta-molecule application").
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

// TestSupersessionCrossesTheEquivalenceClass is the reference reading of
// "interchangeable" applied to supersession: "a truth assertion, a truth
// retraction, a contradiction or a supersession naming any member of an
// equivalence class is read as a statement about the whole class"
// (spec/06-meta-bonds.md, "Equivalence").
//
// Two statements of an old figure are equivalent, two statements of a new one
// are equivalent, and Alice declares that one of the new pair supersedes one of
// the old pair. Both ends widen: every member of the old class is replaced, and
// every member of the new class replaces it.
func TestSupersessionCrossesTheEquivalenceClass(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	bond := block.MustCreateBond("_A_ has population _B_")
	oldOne, oldOneOps := statement(bond, "Paris", "2,150,000 (2010)")
	oldTwo, oldTwoOps := statement(bond, "Paris, France", "2,150,000 in 2010")
	newOne, newOneOps := statement(bond, "Paris", "2,102,650 (2023)")
	newTwo, newTwoOps := statement(bond, "Paris, France", "2,102,650 in 2023")

	olds := []cid.Digest{oldOne.Molecule().Digest(), oldTwo.Molecule().Digest()}
	news := []cid.Digest{newOne.Molecule().Digest(), newTwo.Molecule().Digest()}

	ops := []block.Operation{bond}
	ops = append(ops, oldOneOps...)
	ops = append(ops, oldTwoOps...)
	ops = append(ops, newOneOps...)
	ops = append(ops, newTwoOps...)
	ops = append(ops,
		metaBondOp(entity.TemplateEquivalence),
		sameAs(entity.FillerMolecule, olds[0], olds[1]),
		sameAs(entity.FillerMolecule, news[0], news[1]),
		metaBondOp(entity.TemplateSupersession),
		supersedes(news[0], olds[0]),
	)
	w.publish(alice, ops...)
	slices.SortFunc(olds, compareDigests)
	slices.SortFunc(news, compareDigests)

	v := w.view(alice.PublicKey())
	for _, d := range olds {
		if !v.IsSuperseded(d) {
			t.Errorf("%s is equivalent to a superseded molecule and is therefore superseded", d)
		}
		if got := v.SupersededBy(d); !slices.Equal(got, news) {
			t.Errorf("SupersededBy(%s) = %s, want the whole replacing class %s", d, digests(got), digests(news))
		}
		if got := v.Current(d); !slices.Equal(got, news) {
			t.Errorf("Current(%s) = %s, want %s", d, digests(got), digests(news))
		}
	}
	for _, d := range news {
		if v.IsSuperseded(d) {
			t.Errorf("%s replaces the old class and is replaced by nothing", d)
		}
		if got := v.Supersedes(d); !slices.Equal(got, olds) {
			t.Errorf("Supersedes(%s) = %s, want the whole replaced class %s", d, digests(got), digests(olds))
		}
	}
	// Widening a supersession is not a disagreement, and nothing is dropped.
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("a supersession across two classes is not a conflict: %v", got)
	}
	accepted := v.Accepted()
	for _, d := range olds {
		if !v.Has(d) {
			t.Errorf("superseded molecule %s was dropped from the view", d)
		}
		if slices.Contains(accepted, d) {
			t.Errorf("Accepted = %s, want the whole superseded class left out", digests(accepted))
		}
	}
	for _, d := range news {
		if !slices.Contains(accepted, d) {
			t.Errorf("Accepted = %s, want the current class %s in it", digests(accepted), digests(news))
		}
	}
}

// TestSupersessionWithinAnEquivalenceClassIsACycle is the edge the class reading
// creates: "A supersedes B" where A and B are declared the same is a class that
// replaces itself, so no member of it can be the current version of anything.
// That is exactly what a supersession cycle is, and it is surfaced as one.
func TestSupersessionWithinAnEquivalenceClassIsACycle(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	bond := block.MustCreateBond("_A_ has population _B_")
	first, firstOps := statement(bond, "Paris", "2,102,650 (2023)")
	second, secondOps := statement(bond, "Paris, France", "2,102,650 in 2023")
	a, b := first.Molecule().Digest(), second.Molecule().Digest()

	ops := []block.Operation{bond}
	ops = append(ops, firstOps...)
	ops = append(ops, secondOps...)
	ops = append(ops,
		metaBondOp(entity.TemplateEquivalence), sameAs(entity.FillerMolecule, a, b),
		metaBondOp(entity.TemplateSupersession), supersedes(a, b),
	)
	w.publish(alice, ops...)

	v := w.view(alice.PublicKey())
	cycles := v.ConflictsOfKind(ConflictSupersessionCycle)
	if len(cycles) != 1 {
		t.Fatalf("%d supersession cycle(s), want 1: %v", len(cycles), v.Conflicts())
	}
	want := []cid.Digest{a, b}
	slices.SortFunc(want, compareDigests)
	if !slices.Equal(cycles[0].Molecules, want) {
		t.Errorf("the cycle is %s, want the whole class %s", digests(cycles[0].Molecules), digests(want))
	}
	for _, d := range want {
		if got := v.Current(d); got != nil {
			t.Errorf("Current(%s) = %s; a class that replaces itself has no current version", d, digests(got))
		}
		if !v.IsSuperseded(d) {
			t.Errorf("%s is in a class that replaces itself and is therefore superseded", d)
		}
	}
}

// TestContradictionCrossesTheEquivalenceClass is the same reading applied to
// contradiction: what contradicts a molecule contradicts everything
// interchangeable with it. The MUST of spec/06-meta-bonds.md, "Contradiction",
// is satisfied over the widened pair, and neither class is thereby untrue.
func TestContradictionCrossesTheEquivalenceClass(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)

	capital := block.MustCreateBond("_A_ is the capital of _B_")
	claimOne, claimOneOps := statement(capital, "Paris", "France")
	claimTwo, claimTwoOps := statement(capital, "Paris, France", "The French Republic")
	rival, rivalOps := statement(capital, "Lyon", "France")
	class := []cid.Digest{claimOne.Molecule().Digest(), claimTwo.Molecule().Digest()}
	other := rival.Molecule().Digest()

	ops := []block.Operation{capital}
	ops = append(ops, claimOneOps...)
	ops = append(ops, claimTwoOps...)
	ops = append(ops, rivalOps...)
	ops = append(ops,
		metaBondOp(entity.TemplateEquivalence), sameAs(entity.FillerMolecule, class[0], class[1]),
		metaBondOp(entity.TemplateContradiction), contradicts(class[0], other),
	)
	w.publish(alice, ops...)
	slices.SortFunc(class, compareDigests)

	v := w.view(alice.PublicKey())
	// The molecule the meta-molecule never named contradicts the rival too.
	for _, d := range class {
		if got := v.Contradictions(d); len(got) != 1 || got[0] != other {
			t.Errorf("Contradictions(%s) = %s, want %s", d, digests(got), other)
		}
	}
	if got := v.Contradictions(other); !slices.Equal(got, class) {
		t.Errorf("Contradictions(%s) = %s, want the whole class %s", other, digests(got), digests(class))
	}

	conflicts := v.ConflictsOfKind(ConflictContradiction)
	if len(conflicts) != 1 {
		t.Fatalf("%d contradiction(s), want 1: %v", len(conflicts), v.Conflicts())
	}
	want := append(slices.Clone(class), other)
	slices.SortFunc(want, compareDigests)
	if !slices.Equal(conflicts[0].Molecules, want) {
		t.Errorf("the conflict is about %s, want both classes %s", digests(conflicts[0].Molecules), digests(want))
	}
	if len(conflicts[0].Sides) != 2 {
		t.Fatalf("%d side(s), want one per class", len(conflicts[0].Sides))
	}
	var wide, lone Side
	for _, side := range conflicts[0].Sides {
		if len(side.Molecules) == 1 {
			lone = side
			continue
		}
		wide = side
	}
	if !slices.Equal(wide.Molecules, class) {
		t.Errorf("one side is %s, want the equivalence class %s", digests(wide.Molecules), digests(class))
	}
	if len(lone.Molecules) != 1 || lone.Molecules[0] != other {
		t.Errorf("the other side is %s, want %s alone", digests(lone.Molecules), other)
	}
	// Surfaced, and no more than that.
	for _, d := range want {
		assertTruth(t, v, d, Unasserted)
	}
}
