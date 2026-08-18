package accept

import (
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// assertWithdrawn is the assertion every test of a withdrawn meta-molecule ends
// on: it is not applied, and it is still an entity of the view.
func assertWithdrawn(t *testing.T, v *View, meta cid.Digest, want bool) {
	t.Helper()
	if !v.Has(meta) {
		t.Fatalf("the meta-molecule %s is not in the view; withdrawing does not delete", meta)
	}
	if got := slices.Contains(v.WithdrawnMetaMolecules(), meta); got != want {
		t.Errorf("WithdrawnMetaMolecules contains %s = %v, want %v (list: %s)",
			meta, got, want, digests(v.WithdrawnMetaMolecules()))
	}
}

// twoStatements publishes the vocabulary and two molecules stating the same
// fact two ways, which is what most of this file's tests need before they can
// declare anything about them.
func twoStatements(w *world, b *block.Builder) (vocabulary *block.Block, a, c cid.Digest) {
	w.t.Helper()
	capital := block.MustCreateBond("_A_ is the capital of _B_")
	first, firstOps := statement(capital, "Paris, the capital of France", "France")
	second, secondOps := statement(capital, "Paris, France", "The French Republic")

	ops := []block.Operation{capital}
	ops = append(ops, firstOps...)
	ops = append(ops, secondOps...)
	ops = append(ops,
		metaBondOp(entity.TemplateEquivalence),
		metaBondOp(entity.TemplateTruthAssertion),
		metaBondOp(entity.TemplateTruthRetraction),
		metaBondOp(entity.TemplateContradiction),
		metaBondOp(entity.TemplateSupersession),
	)
	vocabulary = w.publish(b, ops...)
	return vocabulary, first.Molecule().Digest(), second.Molecule().Digest()
}

// TestRetractedEquivalenceSplitsTheClass covers the rule of
// spec/06-meta-bonds.md, "Withdrawing meta-molecules", over the meta-bond it
// matters most for: "A withdrawn equivalence unifies nothing".
//
// Alice declares two molecules the same and asserts one of them true, which
// under the class reading makes both true. She then retracts her own
// equivalence. The class splits, and the assertion narrows with it to the
// molecule she actually named — the effects that depended on the class go with
// the class.
func TestRetractedEquivalenceSplitsTheClass(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, a, b := twoStatements(w, alice)

	equivalence := sameAs(entity.FillerMolecule, a, b)
	w.publish(alice, equivalence, isTrue(a))

	meta := equivalence.Molecule().Digest()
	v := w.view(alice.PublicKey())
	if !v.Equivalent(a, b) {
		t.Fatal("the equivalence did not join the two molecules while it stood")
	}
	assertTruth(t, v, b, Asserted)
	assertWithdrawn(t, v, meta, false)

	// Alice takes it back.
	w.publish(alice, isUntrue(meta))
	v = w.view(alice.PublicKey())

	if v.Equivalent(a, b) {
		t.Error("a retracted equivalence still unifies its two molecules")
	}
	for _, d := range []cid.Digest{a, b} {
		if got := v.EquivalenceClass(d); !slices.Equal(got, []cid.Digest{d}) {
			t.Errorf("EquivalenceClass(%s) = %s, want itself alone", d, digests(got))
		}
	}
	// The assertion is Alice's and stands; it just no longer crosses to b.
	assertTruth(t, v, a, Asserted)
	assertTruth(t, v, b, Unasserted)

	// The withdrawn equivalence is still an entity of the view, and its own
	// truth state records what happened to it.
	assertWithdrawn(t, v, meta, true)
	assertTruth(t, v, meta, Retracted)
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("one author changing their mind surfaced %v", got)
	}
}

// TestRetractedContradictionIsNotSurfaced is the same rule over
// "_A_ contradicts _B_": a withdrawn contradiction is not surfaced.
func TestRetractedContradictionIsNotSurfaced(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, a, b := twoStatements(w, alice)

	declaration := contradicts(a, b)
	meta := declaration.Molecule().Digest()
	w.publish(alice, declaration)

	v := w.view(alice.PublicKey())
	if got := v.ConflictsOfKind(ConflictContradiction); len(got) != 1 {
		t.Fatalf("the standing contradiction surfaced %d conflicts, want 1", len(got))
	}
	if got := v.Contradictions(a); !slices.Equal(got, []cid.Digest{b}) {
		t.Fatalf("Contradictions(a) = %s, want [%s]", digests(got), b)
	}

	w.publish(alice, isUntrue(meta))
	v = w.view(alice.PublicKey())

	if got := v.ConflictsOfKind(ConflictContradiction); len(got) != 0 {
		t.Errorf("a retracted contradiction is still surfaced: %v", got)
	}
	if got := v.Contradictions(a); len(got) != 0 {
		t.Errorf("Contradictions(a) = %s, want nothing", digests(got))
	}
	assertWithdrawn(t, v, meta, true)
}

// TestRetractedSupersessionRestoresTheCurrentMolecule is the same rule over
// "_A_ supersedes _B_": a withdrawn supersession marks nothing as replaced, so
// the molecule it had deprecated is current again.
func TestRetractedSupersessionRestoresTheCurrentMolecule(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, old, corrected := twoStatements(w, alice)

	declaration := supersedes(corrected, old)
	meta := declaration.Molecule().Digest()
	w.publish(alice, declaration)

	v := w.view(alice.PublicKey())
	if !v.IsSuperseded(old) {
		t.Fatal("the standing supersession did not mark the old molecule")
	}

	w.publish(alice, isUntrue(meta))
	v = w.view(alice.PublicKey())

	if v.IsSuperseded(old) {
		t.Error("a retracted supersession still marks the molecule it named")
	}
	if got := v.Current(old); !slices.Equal(got, []cid.Digest{old}) {
		t.Errorf("Current(old) = %s, want itself: nothing replaces it any more", digests(got))
	}
	if got := v.Supersedes(corrected); len(got) != 0 {
		t.Errorf("Supersedes(corrected) = %s, want nothing", digests(got))
	}
	assertWithdrawn(t, v, meta, true)
	if !slices.Contains(v.Accepted(), old) {
		t.Error("the un-superseded molecule is not accepted again")
	}
}

// TestAnotherAuthorsRetractionDoesNotWithdraw covers rule 3 of
// spec/06-meta-bonds.md, "Withdrawing meta-molecules": "A retraction published
// by a subscribed author who did not publish the meta-molecule MUST NOT
// withdraw it."
//
// Alice declares an equivalence and asserts it true. Bob, who published no such
// molecule, says it is untrue. The equivalence goes on unifying — nobody has a
// veto over another author's declaration — and the disagreement about it is
// surfaced as the truth conflict it is.
func TestAnotherAuthorsRetractionDoesNotWithdraw(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.builder(1), w.builder(2)
	vocabulary, a, b := twoStatements(w, alice)

	equivalence := sameAs(entity.FillerMolecule, a, b)
	meta := equivalence.Molecule().Digest()
	aliceBlock := w.publish(alice, equivalence, isTrue(meta))
	w.publishRefs(bob, []cid.Digest{vocabulary.Digest(), aliceBlock.Digest()}, isUntrue(meta))

	v := w.view(alice.PublicKey(), bob.PublicKey())
	if !v.Equivalent(a, b) {
		t.Error("Bob's retraction withdrew Alice's equivalence; only its own author can")
	}
	assertWithdrawn(t, v, meta, false)

	// The disagreement is not swallowed: it is a truth conflict about the
	// meta-molecule, surfaced like any other.
	assertTruth(t, v, meta, Conflicted)
	disagreements := v.ConflictsOfKind(ConflictTruthDisagreement)
	if len(disagreements) != 1 {
		t.Fatalf("%d truth disagreements, want 1 over the equivalence itself", len(disagreements))
	}
	if !slices.Contains(disagreements[0].Molecules, meta) {
		t.Errorf("the disagreement is over %s, want the equivalence %s",
			digests(disagreements[0].Molecules), meta)
	}

	// Bob can make the declaration his own by publishing it: then his
	// retraction is about a meta-molecule he authored. Alice's backing still
	// keeps it standing.
	w.publishRefs(bob, []cid.Digest{vocabulary.Digest(), aliceBlock.Digest()}, equivalence, isUntrue(meta))
	v = w.view(alice.PublicKey(), bob.PublicKey())
	if !v.Equivalent(a, b) {
		t.Error("the equivalence fell with one of its two authors; one standing backing is enough")
	}
	assertWithdrawn(t, v, meta, false)

	// And a view of Bob alone — where Alice's backing is filtered out — no
	// longer has any author standing behind it.
	only := w.view(bob.PublicKey())
	if only.Equivalent(a, b) {
		t.Error("with only Bob subscribed, his own retracted equivalence still applies")
	}
}

// TestBackingIsPerAuthor is the "at least one" of rule 1: two authors publish
// the same equivalence, and it stands until both have taken it back.
func TestBackingIsPerAuthor(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.builder(1), w.builder(2)
	vocabulary, a, b := twoStatements(w, alice)

	equivalence := sameAs(entity.FillerMolecule, a, b)
	meta := equivalence.Molecule().Digest()
	aliceBlock := w.publish(alice, equivalence)
	refs := []cid.Digest{vocabulary.Digest(), aliceBlock.Digest()}
	w.publishRefs(bob, refs, equivalence)

	w.publish(alice, isUntrue(meta))
	v := w.view(alice.PublicKey(), bob.PublicKey())
	if !v.Equivalent(a, b) {
		t.Error("Alice's retraction withdrew the equivalence Bob also published")
	}
	assertWithdrawn(t, v, meta, false)

	w.publishRefs(bob, refs, isUntrue(meta))
	v = w.view(alice.PublicKey(), bob.PublicKey())
	if v.Equivalent(a, b) {
		t.Error("the equivalence still applies with both its authors having retracted it")
	}
	assertWithdrawn(t, v, meta, true)
}

// TestReassertionRestoresTheEffect is the latest-wins half of the rule: "An
// author who publishes an equivalence, retracts it, and later publishes or
// asserts it again backs it once more" (spec/06-meta-bonds.md, "Withdrawing
// meta-molecules"). Both routes back are exercised — a fresh "«M» is true", and
// a re-publication of the meta-molecule itself.
func TestReassertionRestoresTheEffect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		restore func(meta cid.Digest, equivalence block.CreateMolecule) block.Operation
	}{
		{"an explicit assertion", func(meta cid.Digest, _ block.CreateMolecule) block.Operation {
			return isTrue(meta)
		}},
		{"re-publishing the meta-molecule", func(_ cid.Digest, equivalence block.CreateMolecule) block.Operation {
			return equivalence
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			alice := w.builder(1)
			_, a, b := twoStatements(w, alice)

			equivalence := sameAs(entity.FillerMolecule, a, b)
			meta := equivalence.Molecule().Digest()
			w.publish(alice, equivalence)
			w.publish(alice, isUntrue(meta))

			v := w.view(alice.PublicKey())
			if v.Equivalent(a, b) {
				t.Fatal("the retracted equivalence still applies")
			}

			w.publish(alice, tc.restore(meta, equivalence))
			v = w.view(alice.PublicKey())
			if !v.Equivalent(a, b) {
				t.Error("the equivalence did not come back; the author's later word backs it again")
			}
			assertWithdrawn(t, v, meta, false)
		})
	}
}

// TestTruthAssertionsAreNotGated covers rule 4: the truth meta-bonds are not
// themselves subject to withdrawal, which is what stops the regress.
//
// Alice asserts a molecule true, retracts it, and then retracts the retraction
// itself. If truth meta-molecules were gated, that last block would revive the
// assertion; they are not, so it says nothing about the molecule. Her position
// on the molecule is still the retraction, which is the latest thing she said
// about it.
func TestTruthAssertionsAreNotGated(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, a, _ := twoStatements(w, alice)

	w.publish(alice, isTrue(a))
	retraction := isUntrue(a)
	w.publish(alice, retraction)
	meta := retraction.Molecule().Digest()
	w.publish(alice, isUntrue(meta))

	v := w.view(alice.PublicKey())
	assertTruth(t, v, a, Retracted)
	assertWithdrawn(t, v, meta, false)
	if got := v.WithdrawnMetaMolecules(); len(got) != 0 {
		t.Errorf("WithdrawnMetaMolecules = %s, want nothing: no truth meta-molecule is gated", digests(got))
	}
	// The retraction of the retraction is itself an ordinary molecule of the
	// view, with the truth state that record gives it.
	assertTruth(t, v, meta, Retracted)
}

// TestStandingIsReadOffTheMetaMoleculeItself pins the application order of
// spec/06-meta-bonds.md, "Withdrawing meta-molecules": standing is read from the
// assertions naming the meta-molecule, not from those naming its equivalence
// class, because the closure is one of the things standing decides.
//
// Alice declares two contradictions equivalent to each other and retracts one of
// them. Under a class-keyed gate the retraction would take both down and the
// closure would be defined in terms of itself; here it takes down exactly the
// one she named.
func TestStandingIsReadOffTheMetaMoleculeItself(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, a, b := twoStatements(w, alice)

	first := contradicts(a, b)
	second := contradicts(b, a)
	firstMeta, secondMeta := first.Molecule().Digest(), second.Molecule().Digest()
	w.publish(alice, first, second, sameAs(entity.FillerMolecule, firstMeta, secondMeta))
	w.publish(alice, isUntrue(firstMeta))

	v := w.view(alice.PublicKey())
	if !v.Equivalent(firstMeta, secondMeta) {
		t.Fatal("the two contradictions are not equivalent, so the test states nothing")
	}
	assertWithdrawn(t, v, firstMeta, true)
	assertWithdrawn(t, v, secondMeta, false)
	// The second contradiction still stands, so the two molecules are still
	// surfaced as contradictory.
	if got := v.ConflictsOfKind(ConflictContradiction); len(got) != 1 {
		t.Errorf("%d contradictions surfaced, want the one still standing", len(got))
	}
}
