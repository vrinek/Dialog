package accept

import (
	"bytes"
	"crypto/ed25519"
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// onlyDeclaration insists that a reading has exactly one declaration behind it
// and returns it, which is the shape of every case here: one meta-molecule, one
// question about who published it.
func onlyDeclaration(t *testing.T, what string, decls []Declaration) Declaration {
	t.Helper()
	if len(decls) != 1 {
		t.Fatalf("%s has %d declarations, want 1: %v", what, len(decls), decls)
	}
	return decls[0]
}

// assertBacking checks who a declaration stands on, and where each of them
// published it. want is the expected (author, block) pairs in order.
func assertBacking(t *testing.T, decl Declaration, want ...Backing) {
	t.Helper()
	if len(decl.Backing) != len(want) {
		t.Fatalf("%s is backed by %v, want %d record(s)", decl.Meta, decl.Backing, len(want))
	}
	for i, got := range decl.Backing {
		if !bytes.Equal(got.Author, want[i].Author) {
			t.Errorf("backing %d is by %x, want %x", i, got.Author[:8], want[i].Author[:8])
		}
		if got.Block != want[i].Block {
			t.Errorf("backing %d is in block %s, want %s", i, got.Block, want[i].Block)
		}
		if !bytes.Equal(got.Position.Author, got.Author) {
			t.Errorf("backing %d places the block under %x, want its own author %x",
				i, got.Position.Author[:8], got.Author[:8])
		}
		if got.Position.IsZero() {
			t.Errorf("backing %d is unplaced; every block of the view has a position", i)
		}
	}
}

// TestEquivalenceDeclarationsNameTheDeclarer is todos/067's question over the
// meta-bond it was filed for: an application shown an equivalence class must be
// able to say who put it together, because "Holland and the Netherlands are the
// same place" is a claim by an author and not a fact of the world.
func TestEquivalenceDeclarationsNameTheDeclarer(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, a, b := twoStatements(w, alice)

	equivalence := sameAs(entity.FillerMolecule, a, b)
	declared := w.publish(alice, equivalence)
	meta := equivalence.Molecule().Digest()

	v := w.view(alice.PublicKey())
	if !v.Equivalent(a, b) {
		t.Fatal("the equivalence did not apply, so there is nothing to attribute")
	}

	decl := onlyDeclaration(t, "the class of a", v.EquivalenceDeclarations(a))
	if decl.Meta != meta {
		t.Errorf("the class is declared by %s, want the equivalence %s", decl.Meta, meta)
	}
	if decl.Template != entity.TemplateEquivalence {
		t.Errorf("the declaration reads %q, want %q", decl.Template, entity.TemplateEquivalence)
	}
	if decl.A != a || decl.B != b {
		t.Errorf("the declaration names %s and %s, want %s and %s", decl.A, decl.B, a, b)
	}
	assertBacking(t, decl, Backing{Author: alice.PublicKey(), Block: declared.Digest()})
	if got := decl.Authors(); len(got) != 1 || !bytes.Equal(got[0], alice.PublicKey()) {
		t.Errorf("the declaration is attributed to %d author(s), want Alice alone", len(got))
	}
	// The declaring block's position is the one the view decided standing by.
	pos, ok := v.BlockPosition(declared.Digest())
	if got := decl.Backing[0].Position; !ok || got.Height != pos.Height || got.Length != pos.Length ||
		got.Lineage != pos.Lineage {
		t.Errorf("the backing places the block at %s, BlockPosition says %s (%v)",
			decl.Backing[0].Position, pos, ok)
	}

	// The class is what the declaration answers for, so the other end gives
	// the same answer.
	if other := onlyDeclaration(t, "the class of b", v.EquivalenceDeclarations(b)); other.Meta != decl.Meta {
		t.Errorf("the two ends of one class are declared by %s and %s", decl.Meta, other.Meta)
	}
	// A class of one has nothing to attribute, and neither has a digest the
	// view does not hold.
	if got := v.EquivalenceDeclarations(meta); len(got) != 0 {
		t.Errorf("the equivalence meta-molecule is in a class of one but reports %v", got)
	}
	if got := v.EquivalenceDeclarations(cid.Digest{}); got != nil {
		t.Errorf("a digest the view does not hold reports %v", got)
	}
}

// TestDeclarationsDropTheAuthorWhoWithdrew is the withdrawal rule of
// spec/06-meta-bonds.md, "Withdrawing meta-molecules", read as an attribution
// question: two authors publish one equivalence, one takes it back, and the
// declaration must name the author who still stands behind it and not the one
// who stopped. When the last of them retracts, there is no declaration at all.
func TestDeclarationsDropTheAuthorWhoWithdrew(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.builder(1), w.builder(2)
	vocabulary, a, b := twoStatements(w, alice)

	equivalence := sameAs(entity.FillerMolecule, a, b)
	meta := equivalence.Molecule().Digest()
	aliceBlock := w.publish(alice, equivalence)
	refs := []cid.Digest{vocabulary.Digest(), aliceBlock.Digest()}
	bobBlock := w.publishRefs(bob, refs, equivalence)

	// Backing is ascending by author key, which is neither publication order
	// nor a ranking: no author comes first in this package.
	joint := []Backing{
		{Author: alice.PublicKey(), Block: aliceBlock.Digest()},
		{Author: bob.PublicKey(), Block: bobBlock.Digest()},
	}
	slices.SortFunc(joint, func(x, y Backing) int { return bytes.Compare(x.Author, y.Author) })

	subs := []ed25519.PublicKey{alice.PublicKey(), bob.PublicKey()}
	decl := onlyDeclaration(t, "the jointly declared class", w.view(subs...).EquivalenceDeclarations(a))
	assertBacking(t, decl, joint...)

	// Alice takes hers back. The equivalence stands on Bob's word, and says so.
	w.publish(alice, isUntrue(meta))
	v := w.view(subs...)
	if !v.Equivalent(a, b) {
		t.Fatal("one author's retraction withdrew a jointly published equivalence")
	}
	decl = onlyDeclaration(t, "the class after Alice retracted", v.EquivalenceDeclarations(a))
	assertBacking(t, decl, Backing{Author: bob.PublicKey(), Block: bobBlock.Digest()})
	if got := decl.Authors(); len(got) != 1 || !bytes.Equal(got[0], bob.PublicKey()) {
		t.Errorf("the withdrawn author is still credited: %d author(s)", len(got))
	}

	// And when Bob follows, nobody is left behind it: the declaration is gone
	// rather than unattributed, and the meta-molecule is in the withdrawn list.
	w.publishRefs(bob, refs, isUntrue(meta))
	v = w.view(subs...)
	if got := v.EquivalenceDeclarations(a); len(got) != 0 {
		t.Errorf("a withdrawn equivalence still declares %v", got)
	}
	assertWithdrawn(t, v, meta, true)
}

// TestSupersessionDeclarationsRunBothWays: "who says this figure replaces that
// one" is asked from either end — from the superseded molecule, which an
// application is about to hide, and from the current one it is about to show.
func TestSupersessionDeclarationsRunBothWays(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, old, corrected := twoStatements(w, alice)

	declaration := supersedes(corrected, old)
	meta := declaration.Molecule().Digest()
	published := w.publish(alice, declaration)

	v := w.view(alice.PublicKey())
	if !v.IsSuperseded(old) {
		t.Fatal("the supersession did not apply, so there is nothing to attribute")
	}
	for _, from := range []struct {
		name   string
		digest cid.Digest
	}{{"the superseded molecule", old}, {"the current one", corrected}} {
		decl := onlyDeclaration(t, from.name, v.SupersessionDeclarations(from.digest))
		if decl.Meta != meta {
			t.Errorf("%s is replaced by the declaration %s, want %s", from.name, decl.Meta, meta)
		}
		if decl.Template != entity.TemplateSupersession {
			t.Errorf("%s reads %q, want %q", from.name, decl.Template, entity.TemplateSupersession)
		}
		// A and B keep the template's direction: A replaces B.
		if decl.A != corrected || decl.B != old {
			t.Errorf("%s declares %s supersedes %s, want %s supersedes %s",
				from.name, decl.A, decl.B, corrected, old)
		}
		assertBacking(t, decl, Backing{Author: alice.PublicKey(), Block: published.Digest()})
	}

	// Retracting it takes the attribution with the reading: the caller never
	// has to repeat the withdrawal rule.
	w.publish(alice, isUntrue(meta))
	v = w.view(alice.PublicKey())
	if got := v.SupersessionDeclarations(old); len(got) != 0 {
		t.Errorf("a withdrawn supersession still declares %v", got)
	}
}

// TestContradictionDeclarationsAgreeWithTheConflict rounds out the symmetry the
// package already had for contradictions: Conflict.Meta and Conflict.Declarers
// answer per surfaced conflict, and these answer the same thing per entity.
func TestContradictionDeclarationsAgreeWithTheConflict(t *testing.T) {
	w := newWorld(t)
	alice := w.builder(1)
	_, a, b := twoStatements(w, alice)

	declaration := contradicts(a, b)
	meta := declaration.Molecule().Digest()
	published := w.publish(alice, declaration)

	v := w.view(alice.PublicKey())
	conflicts := v.ConflictsOfKind(ConflictContradiction)
	if len(conflicts) != 1 {
		t.Fatalf("%d contradictions surfaced, want 1", len(conflicts))
	}

	decl := onlyDeclaration(t, "the contradiction", v.ContradictionDeclarations(a))
	if decl.Meta != meta || decl.Template != entity.TemplateContradiction {
		t.Errorf("the contradiction is declared by %s (%q), want %s", decl.Meta, decl.Template, meta)
	}
	if decl.A != a || decl.B != b {
		t.Errorf("the declaration names %s and %s, want %s and %s", decl.A, decl.B, a, b)
	}
	assertBacking(t, decl, Backing{Author: alice.PublicKey(), Block: published.Digest()})
	if !slices.Equal(conflicts[0].Meta, []cid.Digest{decl.Meta}) {
		t.Errorf("the conflict names %s, the declaration %s", digests(conflicts[0].Meta), decl.Meta)
	}
	if got, want := decl.Authors(), conflicts[0].Declarers; len(got) != len(want) ||
		!bytes.Equal(got[0], want[0]) {
		t.Errorf("the declaration is by %d author(s), the conflict by %d", len(got), len(want))
	}
	if other := onlyDeclaration(t, "the other molecule", v.ContradictionDeclarations(b)); other.Meta != meta {
		t.Errorf("the two sides are declared by %s and %s", meta, other.Meta)
	}
}

// TestBlockPositionsPlaceEveryBlockOfTheView is todos/068: L3 walks the chains
// to decide which assertion is later, and reports where it walked to.
func TestBlockPositionsPlaceEveryBlockOfTheView(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.builder(1), w.builder(2)
	s := newSubject()

	first := w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion))...)
	second := w.publish(alice, isTrue(s.digest()))
	third := w.publish(alice, block.MustCreateAtom("a later thought"))
	bobBlock := w.publishRefs(bob, []cid.Digest{first.Digest()}, block.MustCreateAtom("Bob was here"))

	v := w.view(alice.PublicKey())
	for want, blk := range []*block.Block{first, second, third} {
		pos, ok := v.BlockPosition(blk.Digest())
		if !ok {
			t.Fatalf("block %d of Alice's chain has no position", want)
		}
		if pos.Height != want {
			t.Errorf("block %d is at height %d", want, pos.Height)
		}
		if pos.Length != 3 {
			t.Errorf("block %d says the chain is %d long, want 3", want, pos.Length)
		}
		if pos.Lineage != first.Digest() {
			t.Errorf("block %d is in lineage %s, want Alice's genesis %s", want, pos.Lineage, first.Digest())
		}
		if !bytes.Equal(pos.Author, alice.PublicKey()) {
			t.Errorf("block %d is attributed to %x", want, pos.Author[:8])
		}
	}

	// A block whose entities this view does not admit is not placed: the view
	// is one subscriber's, and Bob is not among them.
	if pos, ok := v.BlockPosition(bobBlock.Digest()); ok {
		t.Errorf("an unsubscribed author's block is placed at %s", pos)
	}
	if pos, ok := v.BlockPosition(cid.Digest{}); ok {
		t.Errorf("a digest naming no block is placed at %s", pos)
	}
	// Subscribing to Bob places his chain, separately: nothing orders two
	// authors against each other.
	both := w.view(alice.PublicKey(), bob.PublicKey())
	pos, ok := both.BlockPosition(bobBlock.Digest())
	if !ok {
		t.Fatal("Bob's block has no position in a view that admits it")
	}
	if pos.Lineage == first.Digest() || pos.Height != 0 || pos.Length != 1 {
		t.Errorf("Bob's genesis block is placed at %s, want the start of his own lineage", pos)
	}

	// And an assertion carries the position that decided it.
	assertions := v.Assertions(s.digest())
	if len(assertions) != 1 {
		t.Fatalf("%d assertions, want Alice's one", len(assertions))
	}
	if got, want := assertions[0].Position.Height, 1; got != want {
		t.Errorf("the assertion is placed at height %d, want %d — the block it was published in", got, want)
	}
	if !assertions[0].Latest {
		t.Error("Alice's only assertion is not her last word")
	}
}

// TestBlockPositionsCountThroughARotation pins what a key rotation does to the
// count: "Block order continues across a key rotation: every block of a
// successor chain comes after every block of the chain it succeeds"
// (spec/05-processing-model.md, "Assertion order"), so the successor's genesis
// block is not height 0 — the rotation block it succeeds is counted, whether or
// not it published anything.
func TestBlockPositionsCountThroughARotation(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice, next := w.builder(1), w.builder(3)

	first := w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)
	rotation := w.rotate(alice, next.PublicKey())
	successor := w.succeed(next, rotation, []cid.Digest{first.Digest()},
		metaBondOp(entity.TemplateTruthRetraction), isUntrue(s.digest()))

	v := w.view(alice.PublicKey(), next.PublicKey())
	pos, ok := v.BlockPosition(successor.Digest())
	if !ok {
		t.Fatal("the successor chain's genesis block has no position")
	}
	if pos.Height != 2 || pos.Length != 3 {
		t.Errorf("the successor's genesis block is %s, want block 3 of 3: the rotation block is counted", pos)
	}
	if pos.Lineage != first.Digest() {
		t.Errorf("the successor is in lineage %s, want the original genesis %s", pos.Lineage, first.Digest())
	}
	if !bytes.Equal(pos.Author, next.PublicKey()) {
		t.Errorf("the successor's block is attributed to %x, want the new key %x",
			pos.Author[:8], next.PublicKey()[:8])
	}
	// The rotation block itself published nothing, so the view had no reason
	// to place it — the count runs through it all the same.
	if _, ok := v.BlockPosition(rotation.Digest()); ok {
		t.Error("a block that published no entity of the view is placed")
	}

	// The retraction wins by that order, and reports the position it won at.
	assertTruth(t, v, s.digest(), Retracted)
	for _, a := range v.Assertions(s.digest()) {
		if a.Latest != (a.Stance == Retracted) {
			t.Errorf("assertion %v is latest=%v", a, a.Latest)
		}
		if a.Position.Lineage != first.Digest() {
			t.Errorf("assertion %v is in lineage %s, want one lineage across the rotation",
				a, a.Position.Lineage)
		}
	}
}
