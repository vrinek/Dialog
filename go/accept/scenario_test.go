package accept

import (
	"crypto/ed25519"
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// A scenario is one node's whole state — L1 store, L2 graph and the
// subscriptions that make an L3 view of them — exercising every meta-bond at
// once, so that the interactions between them are pinned and not just each in
// isolation.
//
// Five authors publish into it:
//
//   - Alice states that Paris is the capital of France and asserts it true,
//     then rotates her key; her successor chain retracts it. One lineage, two
//     keys, and the later word wins (spec/05-processing-model.md, "Assertion
//     order").
//   - Bob publishes a population figure, a correction of it, and the
//     supersession joining them, plus a second statement he declares equivalent
//     to Alice's — and a wrong equivalence between his two figures, which he
//     retracts a block later, so it declares nothing.
//   - Dave disagrees with Bob about the correction, asserting one true and
//     Erin asserting it untrue: a conflict, surfaced.
//   - Mallory, whom nobody subscribes to, retracts everything in sight.
//   - The user's own private chain carries a note and a contradiction.
type scenario struct {
	*world

	alice, successor, bob, dave, erin, mallory *block.Builder
	own                                        *block.Builder

	capital, population         block.CreateBond
	parisCapital, parisSameFact cid.Digest
	oldFigure, newFigure        cid.Digest
	wrongEquivalence            cid.Digest
	privateNote                 cid.Digest
	claimA, claimB              cid.Digest
}

// subscribed returns the authors the scenario's user accepts data from —
// everyone but Mallory.
func (s *scenario) subscribed() []ed25519.PublicKey {
	return []ed25519.PublicKey{
		s.alice.PublicKey(), s.successor.PublicKey(), s.bob.PublicKey(),
		s.dave.PublicKey(), s.erin.PublicKey(), s.own.PublicKey(),
	}
}

func buildScenario(t testing.TB) *scenario {
	t.Helper()
	w := newWorld(t)
	s := &scenario{
		world:     w,
		alice:     w.builder(1),
		successor: w.builder(2),
		bob:       w.builder(3),
		dave:      w.builder(4),
		erin:      w.builder(5),
		mallory:   w.builder(9),
		own:       w.builder(7),
	}
	s.capital = block.MustCreateBond("_A_ is the capital of _B_")
	s.population = block.MustCreateBond("_A_ has population _B_")

	// Alice: the statement and an assertion that it is true.
	parisCapital, capitalOps := statement(s.capital, "Paris, the capital of France", "France")
	s.parisCapital = parisCapital.Molecule().Digest()
	ops := []block.Operation{s.capital}
	ops = append(ops, capitalOps...)
	ops = append(ops, metaBondOp(entity.TemplateTruthAssertion), isTrue(s.parisCapital))
	aliceBlock := w.publish(s.alice, ops...)

	// Alice rotates, and her successor chain retracts the assertion. Same
	// lineage: the retraction is later.
	rotation := w.rotate(s.alice, s.successor.PublicKey())
	w.succeed(s.successor, rotation, []cid.Digest{aliceBlock.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.parisCapital))

	// Bob: a figure, its correction, the supersession, and a second way of
	// saying what Alice said, declared equivalent to hers.
	oldFigure, oldOps := statement(s.population, "Paris", "2,150,000 (2010)")
	newFigure, newOps := statement(s.population, "Paris", "2,102,650 (2023)")
	sameFact, sameOps := statement(s.capital, "Paris, France", "The French Republic")
	s.oldFigure, s.newFigure = oldFigure.Molecule().Digest(), newFigure.Molecule().Digest()
	s.parisSameFact = sameFact.Molecule().Digest()

	bobOps := []block.Operation{s.population}
	bobOps = append(bobOps, oldOps...)
	bobOps = append(bobOps, newOps...)
	bobOps = append(bobOps, sameOps...)
	bobOps = append(bobOps,
		metaBondOp(entity.TemplateSupersession), supersedes(s.newFigure, s.oldFigure),
		metaBondOp(entity.TemplateEquivalence), sameAs(entity.FillerMolecule, s.parisCapital, s.parisSameFact),
	)
	bobBlock := w.publishRefs(s.bob, []cid.Digest{aliceBlock.Digest()}, bobOps...)

	// Bob also declares his two population figures the same — a mistake, since
	// they are two censuses — and takes it back in his next block. A withdrawn
	// equivalence unifies nothing (spec/06-meta-bonds.md, "Withdrawing
	// meta-molecules"), so the supersession between the two figures is a chain
	// and not the cycle it would be inside one class.
	wrong := sameAs(entity.FillerMolecule, s.oldFigure, s.newFigure)
	s.wrongEquivalence = wrong.Molecule().Digest()
	w.publishRefs(s.bob, []cid.Digest{aliceBlock.Digest()}, wrong)
	w.publishRefs(s.bob, []cid.Digest{aliceBlock.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.wrongEquivalence))

	// Dave and Erin disagree about the corrected figure.
	w.publishRefs(s.dave, []cid.Digest{bobBlock.Digest(), aliceBlock.Digest()}, isTrue(s.newFigure))
	w.publishRefs(s.erin, []cid.Digest{bobBlock.Digest(), aliceBlock.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.newFigure))

	// Mallory, subscribed to by nobody, retracts everything.
	w.publishRefs(s.mallory, []cid.Digest{bobBlock.Digest(), aliceBlock.Digest()},
		metaBondOp(entity.TemplateTruthRetraction),
		isUntrue(s.oldFigure), isUntrue(s.parisSameFact))

	// The user's own private chain: a note, two claims and a contradiction
	// between them.
	note := block.MustCreateAtom("a private observation")
	claimA, claimAOps := statement(s.capital, "Lyon", "France")
	claimB, claimBOps := statement(s.capital, "Marseille", "France")
	s.privateNote = note.Atom().Digest()
	s.claimA, s.claimB = claimA.Molecule().Digest(), claimB.Molecule().Digest()

	privateOps := []block.Operation{note, s.capital}
	privateOps = append(privateOps, claimAOps...)
	privateOps = append(privateOps, claimBOps...)
	privateOps = append(privateOps,
		metaBondOp(entity.TemplateContradiction), contradicts(s.claimA, s.claimB))
	w.private(s.own, w.key(40), privateOps...)

	return s
}

// TestScenario walks the whole of L3's job over one node's state: filtering,
// all five meta-bonds, and the conflicts that fall out of them.
func TestScenario(t *testing.T) {
	s := buildScenario(t)
	v := s.view(s.subscribed()...)

	// Filtering. Mallory's retractions are in L2 and not in L3, and neither
	// is she among the authors of anything the view holds.
	mallorysRetraction := isUntrue(s.oldFigure).Molecule().Digest()
	if !s.graph.Has(mallorysRetraction) {
		t.Fatal("Mallory's retraction is missing from L2, which filters nothing")
	}
	if v.Has(mallorysRetraction) {
		t.Error("Mallory's retraction is in L3, and nobody subscribes to her")
	}
	if v.Len() >= s.graph.Len() {
		t.Errorf("the view holds %d of L2's %d entities; Mallory's should be missing", v.Len(), s.graph.Len())
	}

	// Truth across a key rotation: Alice asserted, her successor retracted,
	// and the retraction is later in the same lineage.
	assertTruth(t, v, s.parisCapital, Retracted)
	// The equivalence carries it to Bob's way of saying the same thing.
	if !v.Equivalent(s.parisCapital, s.parisSameFact) {
		t.Error("Bob's equivalence did not join the two statements")
	}
	assertTruth(t, v, s.parisSameFact, Retracted)

	// Supersession: the corrected figure replaces the old one, both are kept.
	if !v.IsSuperseded(s.oldFigure) || v.IsSuperseded(s.newFigure) {
		t.Error("the correction should supersede the old figure and nothing should supersede it")
	}
	if got := v.Current(s.oldFigure); len(got) != 1 || got[0] != s.newFigure {
		t.Errorf("Current(old figure) = %s, want %s", digests(got), s.newFigure)
	}

	// Bob's retracted equivalence declares nothing: the two figures are two
	// classes, so the supersession between them is a chain rather than a class
	// replacing itself. The meta-molecule itself is still in the view.
	if v.Equivalent(s.oldFigure, s.newFigure) {
		t.Error("Bob's retracted equivalence still unifies his two figures")
	}
	if got := v.WithdrawnMetaMolecules(); !slices.Equal(got, []cid.Digest{s.wrongEquivalence}) {
		t.Errorf("WithdrawnMetaMolecules = %s, want Bob's retracted equivalence [%s]",
			digests(got), s.wrongEquivalence)
	}
	if !v.Has(s.wrongEquivalence) {
		t.Error("the withdrawn equivalence left the view; withdrawing is not deleting")
	}

	// Dave and Erin disagree about the correction: Conflicted, not resolved.
	assertTruth(t, v, s.newFigure, Conflicted)

	// The private chain is in the view because its author is a key the user
	// holds — an ordinary subscription.
	if !v.Has(s.privateNote) {
		t.Error("the user's own private note is not in their view")
	}
	if got := v.Contradictions(s.claimA); len(got) != 1 || got[0] != s.claimB {
		t.Errorf("Contradictions(claimA) = %s, want claimB %s", digests(got), s.claimB)
	}

	// Two conflicts, in a deterministic order: a truth disagreement and a
	// contradiction, and nothing else.
	conflicts := v.Conflicts()
	if len(conflicts) != 2 {
		t.Fatalf("%d conflict(s), want 2: %v", len(conflicts), conflicts)
	}
	if conflicts[0].Kind != ConflictTruthDisagreement || conflicts[1].Kind != ConflictContradiction {
		t.Errorf("conflicts are %v then %v, want the truth disagreement first", conflicts[0].Kind, conflicts[1].Kind)
	}
	for i := 1; i < len(conflicts); i++ {
		if compareConflicts(conflicts[i-1], conflicts[i]) >= 0 {
			t.Errorf("Conflicts is not in ascending order at %d", i)
		}
	}

	// Accepted: what an application would show. The retracted statements and
	// the superseded figure are out; the conflicted one stays, because
	// dropping a side would be resolving the conflict.
	accepted := v.Accepted()
	for _, out := range []cid.Digest{s.parisCapital, s.parisSameFact, s.oldFigure} {
		if slices.Contains(accepted, out) {
			t.Errorf("Accepted contains %s, which is retracted or superseded", out)
		}
	}
	for _, in := range []cid.Digest{s.newFigure, s.claimA, s.claimB} {
		if !slices.Contains(accepted, in) {
			t.Errorf("Accepted is missing %s", in)
		}
	}
	for i := 1; i < len(accepted); i++ {
		if compareDigests(accepted[i-1], accepted[i]) >= 0 {
			t.Errorf("Accepted is not ascending at %d", i)
		}
	}

	// Unsubscribing from Erin removes the disagreement rather than resolving
	// it: one voice left, and it says the figure is true.
	without := s.view(s.alice.PublicKey(), s.successor.PublicKey(), s.bob.PublicKey(),
		s.dave.PublicKey(), s.own.PublicKey())
	assertTruth(t, without, s.newFigure, Asserted)
	if got := without.ConflictsOfKind(ConflictTruthDisagreement); len(got) != 0 {
		t.Errorf("the disagreement survived unsubscribing from one side: %v", got)
	}
}

// TestQueryOrderIsAscending pins the order itself, not only its stability.
func TestQueryOrderIsAscending(t *testing.T) {
	s := buildScenario(t)
	v := s.view(s.subscribed()...)

	entries := v.Entries()
	if len(entries) == 0 {
		t.Fatal("the scenario produced an empty view")
	}
	for i := 1; i < len(entries); i++ {
		if compareDigests(entries[i-1].Digest(), entries[i].Digest()) >= 0 {
			t.Errorf("Entries is not ascending at %d", i)
		}
	}
	for _, k := range []block.EntityKind{block.KindAtom, block.KindBond, block.KindMolecule} {
		of := v.DigestsOfKind(k)
		for i := 1; i < len(of); i++ {
			if compareDigests(of[i-1], of[i]) >= 0 {
				t.Errorf("DigestsOfKind(%s) is not ascending at %d", k, i)
			}
		}
	}
	subs := v.Subscriptions()
	for i := 1; i < len(subs); i++ {
		if compareKeys(subs[i-1], subs[i]) >= 0 {
			t.Errorf("Subscriptions is not ascending at %d", i)
		}
	}
	for _, d := range v.Digests() {
		for _, list := range [][]cid.Digest{
			v.EquivalenceClass(d), v.Supersedes(d), v.SupersededBy(d),
			v.Current(d), v.Contradictions(d),
		} {
			for i := 1; i < len(list); i++ {
				if compareDigests(list[i-1], list[i]) >= 0 {
					t.Errorf("a query about %s answered out of order at %d", d, i)
				}
			}
		}
		assertions := v.Assertions(d)
		for i := 1; i < len(assertions); i++ {
			prev, cur := assertions[i-1], assertions[i]
			c := compareKeys(prev.Author, cur.Author)
			if c > 0 || (c == 0 && compareDigests(prev.Block, cur.Block) > 0) {
				t.Errorf("Assertions(%s) is not ordered at %d", d, i)
			}
		}
	}
}
