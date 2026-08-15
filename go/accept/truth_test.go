package accept

import (
	"bytes"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// assertTruth is the assertion every truth test ends on.
func assertTruth(t *testing.T, v *View, d cid.Digest, want TruthState) {
	t.Helper()
	if got := v.Truth(d); got != want {
		t.Errorf("Truth(%s) = %s, want %s\nassertions: %v\nconflicts: %v", d, got, want, v.Assertions(d), v.Conflicts())
	}
}

// TestMultiAuthorTruthAgreement is two subscribed authors publishing the same
// "_A_ is true" molecule about the same statement. Agreement is not a conflict:
// "A molecule asserted as true by a subscribed author SHOULD be treated as
// factual in L3" (spec/06-meta-bonds.md, "Truth assertion").
//
// The two authors publish one entity between them, since the meta-molecule is
// content-addressed and they said the same thing — it carries two authorship
// records (spec/05-processing-model.md, "Accumulation rules"), and both of them
// count.
func TestMultiAuthorTruthAgreement(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice, bob := w.builder(1), w.builder(2)

	ops := append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))
	aliceBlock := w.publish(alice, ops...)
	w.publishRefs(bob, []cid.Digest{aliceBlock.Digest()}, isTrue(s.digest()))

	v := w.view(alice.PublicKey(), bob.PublicKey())
	assertTruth(t, v, s.digest(), Asserted)
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("agreement produced %d conflict(s): %v", len(got), got)
	}

	assertions := v.Assertions(s.digest())
	if len(assertions) != 2 {
		t.Fatalf("%d assertion(s), want 2 — one per author: %v", len(assertions), assertions)
	}
	for _, a := range assertions {
		if a.Stance != Asserted || !a.Latest {
			t.Errorf("assertion %v: want an asserted, latest record", a)
		}
	}
	if !bytes.Equal(assertions[0].Author, alice.PublicKey()) && !bytes.Equal(assertions[0].Author, bob.PublicKey()) {
		t.Errorf("assertion by %x, want Alice or Bob", assertions[0].Author[:8])
	}
	// One entity, two authorship records: the meta-molecule is the same
	// content, published twice.
	e, ok := v.Lookup(isTrue(s.digest()).Molecule().Digest())
	if !ok {
		t.Fatal("the view does not hold the meta-molecule")
	}
	if len(e.Authors()) != 2 {
		t.Errorf("the meta-molecule carries %d authorship record(s), want 2", len(e.Authors()))
	}
}

// TestSameAuthorFlipIsRetraction covers spec/06-meta-bonds.md, "Truth
// retraction": "If the same author previously asserted the molecule as true,
// the later assertion (by block order) takes precedence."
//
// Alice says it is true, then says it is untrue. Two assertions, one winner,
// and the loser is kept: the protocol forbids discarding it.
func TestSameAuthorFlipIsRetraction(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice := w.builder(1)

	w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)
	w.publish(alice, metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.digest()))

	v := w.view(alice.PublicKey())
	assertTruth(t, v, s.digest(), Retracted)
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("one author changing their mind is not a conflict, got %v", got)
	}

	assertions := v.Assertions(s.digest())
	if len(assertions) != 2 {
		t.Fatalf("%d assertion(s), want 2 — the retraction wins, the assertion is kept: %v", len(assertions), assertions)
	}
	for _, a := range assertions {
		if want := a.Stance == Retracted; a.Latest != want {
			t.Errorf("assertion %v: Latest = %v, want %v — the later block wins", a, a.Latest, want)
		}
	}

	// And back again: a third block re-asserting it makes it true once more.
	w.publish(alice, isTrue(s.digest()))
	assertTruth(t, w.view(alice.PublicKey()), s.digest(), Asserted)
}

// TestTimestampsAreIgnored pins the rule of spec/05-processing-model.md,
// "Assertion order": the order is the author's block order, and "A block's ts
// field MUST NOT be used as this order."
//
// Alice publishes her assertion with a timestamp far in the future and her
// retraction with one far in the past. Ordering by ts would keep the molecule
// true; ordering by the chain retracts it. The test runs it both ways round, so
// that neither answer can be reached by accident.
func TestTimestampsAreIgnored(t *testing.T) {
	const (
		distantFuture = 4_000_000_000
		longAgo       = 1_000_000
	)
	for _, tc := range []struct {
		name   string
		second func(cid.Digest) block.CreateMolecule
		want   TruthState
	}{
		{"assertion first, retraction later in the chain", isUntrue, Retracted},
		{"retraction first, assertion later in the chain", isTrue, Asserted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			s := newSubject()
			alice := w.builder(1)

			first, second := isTrue, tc.second
			if tc.want == Asserted {
				first = isUntrue
			}
			// The first block claims a timestamp in the distant future...
			w.publishAt(alice, distantFuture, nil, append(s.ops(),
				metaBondOp(entity.TemplateTruthAssertion),
				metaBondOp(entity.TemplateTruthRetraction),
				first(s.digest()))...)
			// ...and the second, which really is later, claims one long past.
			w.publishAt(alice, longAgo, nil, second(s.digest()))

			v := w.view(alice.PublicKey())
			assertTruth(t, v, s.digest(), tc.want)

			// The blocks say the opposite of what the chain says, which is the
			// whole point of the test.
			if w.blocks[0].TS() <= w.blocks[1].TS() {
				t.Fatalf("the test is not testing anything: ts %d then %d", w.blocks[0].TS(), w.blocks[1].TS())
			}
		})
	}
}

// TestCrossAuthorTruthConflict covers the MUST of spec/05-processing-model.md,
// "Meta-molecule application": "Implementations MUST surface conflicts (e.g.,
// when one subscribed author asserts 'X is true' and another asserts 'X is
// untrue') to the application layer."
//
// Nothing resolves it. Both assertions are kept, the state is Conflicted rather
// than a winner, and the Conflict names the authors on each side.
func TestCrossAuthorTruthConflict(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice, bob := w.builder(1), w.builder(2)

	aliceBlock := w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)
	w.publishRefs(bob, []cid.Digest{aliceBlock.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.digest()))

	v := w.view(alice.PublicKey(), bob.PublicKey())
	assertTruth(t, v, s.digest(), Conflicted)

	conflicts := v.ConflictsOfKind(ConflictTruthDisagreement)
	if len(conflicts) != 1 {
		t.Fatalf("%d truth disagreement(s), want 1: %v", len(conflicts), v.Conflicts())
	}
	c := conflicts[0]
	if len(c.Molecules) != 1 || c.Molecules[0] != s.digest() {
		t.Errorf("the conflict is about %s, want the subject molecule %s", digests(c.Molecules), s.digest())
	}
	if len(c.Sides) != 2 {
		t.Fatalf("%d side(s), want 2: %v", len(c.Sides), c.Sides)
	}
	if c.Sides[0].Stance != StanceTrue || len(c.Sides[0].Authors) != 1 || !bytes.Equal(c.Sides[0].Authors[0], alice.PublicKey()) {
		t.Errorf(`the "is true" side is %v, want Alice alone`, c.Sides[0])
	}
	if c.Sides[1].Stance != StanceUntrue || len(c.Sides[1].Authors) != 1 || !bytes.Equal(c.Sides[1].Authors[0], bob.PublicKey()) {
		t.Errorf(`the "is untrue" side is %v, want Bob alone`, c.Sides[1])
	}
	if len(c.Meta) != 2 {
		t.Errorf("the conflict names %d meta-molecule(s), want both", len(c.Meta))
	}
	if len(c.Declarers) != 2 {
		t.Errorf("the conflict names %d declarer(s), want both authors", len(c.Declarers))
	}
	// Nothing was discarded: both assertions are still there to resolve from.
	if got := v.Assertions(s.digest()); len(got) != 2 {
		t.Errorf("%d assertion(s) survived the conflict, want 2: %v", len(got), got)
	}
}

// TestSameAuthorConflictsWithItself is one author holding both positions at one
// point of their chain — here, in a single block. Block order settles which of
// an author's blocks is later and says nothing about two statements in the same
// block, so this is a disagreement like any other and is surfaced.
func TestSameAuthorConflictsWithItself(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice := w.builder(1)

	w.publish(alice, append(s.ops(),
		metaBondOp(entity.TemplateTruthAssertion),
		metaBondOp(entity.TemplateTruthRetraction),
		isTrue(s.digest()), isUntrue(s.digest()))...)

	v := w.view(alice.PublicKey())
	assertTruth(t, v, s.digest(), Conflicted)
	conflicts := v.ConflictsOfKind(ConflictTruthDisagreement)
	if len(conflicts) != 1 {
		t.Fatalf("%d truth disagreement(s), want 1", len(conflicts))
	}
	for _, side := range conflicts[0].Sides {
		if len(side.Authors) != 1 || !bytes.Equal(side.Authors[0], alice.PublicKey()) {
			t.Errorf("side %v: want Alice, who is on both", side)
		}
	}
}

// TestUnsubscribedAuthorHasNoEffect is the filtering rule doing its work on
// meta-molecules: an author nobody subscribed to can publish any assertion they
// like and it changes nothing.
//
// "L3 filtering mitigates this — the assertion only affects users who subscribe
// to that author" (spec/06-meta-bonds.md, "Security Considerations").
func TestUnsubscribedAuthorHasNoEffect(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice, mallory := w.builder(1), w.builder(9)

	aliceBlock := w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)
	w.publishRefs(mallory, []cid.Digest{aliceBlock.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.digest()))

	// Alice alone: Mallory's retraction is in L2 and not in L3.
	v := w.view(alice.PublicKey())
	assertTruth(t, v, s.digest(), Asserted)
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("an unsubscribed author produced %d conflict(s): %v", len(got), got)
	}
	if v.Has(isUntrue(s.digest()).Molecule().Digest()) {
		t.Error("the unsubscribed author's meta-molecule is in the view")
	}
	if !w.graph.Has(isUntrue(s.digest()).Molecule().Digest()) {
		t.Error("the retraction should still be in L2; L2 accumulates without filtering")
	}

	// Subscribing to Mallory is what gives the retraction effect.
	assertTruth(t, w.view(alice.PublicKey(), mallory.PublicKey()), s.digest(), Conflicted)
}

// TestRotationContinuesAssertionOrder covers the rotation half of
// spec/05-processing-model.md, "Assertion order": "Block order continues across
// a key rotation: every block of a successor chain comes after every block of
// the chain it succeeds."
//
// Alice asserts under her first key, rotates, and retracts under her second.
// Two keys, one lineage: the retraction is later, so it wins, and the two keys
// do not argue with each other.
func TestRotationContinuesAssertionOrder(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice, next := w.builder(1), w.builder(3)

	first := w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)
	rotation := w.rotate(alice, next.PublicKey())
	w.succeed(next, rotation, []cid.Digest{first.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.digest()))

	v := w.view(alice.PublicKey(), next.PublicKey())
	assertTruth(t, v, s.digest(), Retracted)
	if got := v.Conflicts(); len(got) != 0 {
		t.Errorf("a key rotation is not a disagreement between two authors: %v", got)
	}

	// The two keys are still two authors everywhere else: filtering is per
	// key, and the successor's data needs its own subscription.
	onlyOld := w.view(alice.PublicKey())
	assertTruth(t, onlyOld, s.digest(), Asserted)
	if onlyOld.Has(isUntrue(s.digest()).Molecule().Digest()) {
		t.Error("the successor key's retraction is in a view that does not subscribe to it")
	}
}

// TestAmbiguousSuccessionIsSurfaced covers spec/05-processing-model.md, "Chain
// succession": "If more than one genesis block references the same rotation
// block, the succession is ambiguous: the node MUST surface the conflict as it
// surfaces a fork, and MUST NOT pick a successor on its own."
//
// It reaches L3 because block order runs through the succession. With two
// claimants the junction joins nothing, so the successor's retraction is a
// different logical author's word and disagrees with the original rather than
// overriding it.
func TestAmbiguousSuccessionIsSurfaced(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice, next, twin := w.builder(1), w.builder(3), w.builder(3)

	first := w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)
	rotation := w.rotate(alice, next.PublicKey())
	w.succeed(next, rotation, []cid.Digest{first.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.digest()))
	// A second genesis block, signed by the same appointed key, claiming the
	// same rotation.
	w.forked(twin, rotation, block.MustCreateAtom("a second claimant"))

	v := w.view(alice.PublicKey(), next.PublicKey())
	ambiguous := v.ConflictsOfKind(ConflictAmbiguousSuccession)
	if len(ambiguous) != 1 {
		t.Fatalf("%d ambiguous succession(s), want 1: %v", len(ambiguous), v.Conflicts())
	}
	if len(ambiguous[0].Blocks) != 3 {
		t.Errorf("the conflict names %s, want the rotation block and both claimants", digests(ambiguous[0].Blocks))
	}
	if len(ambiguous[0].Molecules) != 0 {
		t.Errorf("an ambiguous succession is about blocks, not molecules: %s", digests(ambiguous[0].Molecules))
	}
	// The order through the junction is ambiguous, so nothing was joined and
	// the two keys disagree.
	assertTruth(t, v, s.digest(), Conflicted)
}

// TestAssertionsAboutOutOfViewMoleculesHaveNoEffect is filtering applied to the
// subject of an assertion. Bob asserts that a molecule only Carol published is
// true; Carol is not subscribed, so the molecule is not in L3, and there is
// nothing for the assertion to be about. See todo 054.
func TestAssertionsAboutOutOfViewMoleculesHaveNoEffect(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	carol, bob := w.builder(4), w.builder(2)

	carolBlock := w.publish(carol, s.ops()...)
	w.publishRefs(bob, []cid.Digest{carolBlock.Digest()},
		metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))

	v := w.view(bob.PublicKey())
	if v.Has(s.digest()) {
		t.Fatal("Carol's molecule is in a view that does not subscribe to her")
	}
	assertTruth(t, v, s.digest(), Unasserted)
	if got := v.Assertions(s.digest()); got != nil {
		t.Errorf("Assertions for an out-of-view molecule = %v, want none", got)
	}
	// Bob's meta-molecule is his own, so it is in the view — as a molecule.
	if !v.Has(isTrue(s.digest()).Molecule().Digest()) {
		t.Error("Bob's own meta-molecule is not in his view")
	}

	// Subscribing to Carol brings the subject in, and the assertion with it.
	assertTruth(t, w.view(bob.PublicKey(), carol.PublicKey()), s.digest(), Asserted)
}

// TestTruthOfNonMolecules is the type discipline of the truth meta-bonds: they
// take a molecule filler, so an atom or a bond is never asserted or retracted.
func TestTruthOfNonMolecules(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice := w.builder(1)
	w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)

	v := w.view(alice.PublicKey())
	for _, d := range []cid.Digest{s.paris.Atom().Digest(), s.capital.Bond().Digest()} {
		if got := v.Truth(d); got != Unasserted {
			t.Errorf("Truth(%s) = %s, want %s", d, got, Unasserted)
		}
	}
	var missing cid.Digest
	if got := v.Truth(missing); got != Unasserted {
		t.Errorf("Truth of a digest the view does not hold = %s, want %s", got, Unasserted)
	}
}
