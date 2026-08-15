package accept

import (
	"errors"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/graph"
	"github.com/vrinek/Dialog/go/privacy"
)

// TestSpecFullDataFlow reproduces the L3 half of the worked example of
// spec/05-processing-model.md, "Examples", "Full data flow" — the half the L2
// tests could only point at:
//
//	Bob subscribes to Alice:
//	L3 (Bob's view): Bob's L3 includes:
//	  - Atom "Paris" (because Alice is subscribed)
//	  - Bond "_A_ is in _B_" (because Alice is subscribed)
//
//	Carol does NOT subscribe to Alice:
//	L3 (Carol's view): Carol's L3 does NOT include Alice's data
//	  - Unless Carol subscribes to an author who references Alice's blocks
//	    (in which case Alice's data is in L2 but not L3)
func TestSpecFullDataFlow(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.builder(1), w.builder(2)

	paris := block.MustCreateAtom("Paris")
	isIn := block.MustCreateBond("_A_ is in _B_")
	aliceBlock := w.publish(alice, paris, isIn)

	// Bob subscribes to Alice. Both of Alice's entities are in his view.
	bobsView := w.view(alice.PublicKey())
	if bobsView.Len() != 2 {
		t.Fatalf("Bob's L3 holds %d entities, want 2: %s", bobsView.Len(), digests(bobsView.Digests()))
	}
	for _, d := range []cid.Digest{paris.Atom().Digest(), isIn.Bond().Digest()} {
		if !bobsView.Has(d) {
			t.Errorf("Bob's L3 does not hold %s, and he subscribes to Alice who published it", d)
		}
	}

	// Carol subscribes to Bob, who references Alice's block. Alice's data is
	// pulled into L2 as validation context and stays out of Carol's L3.
	molecule := block.MustCreateMolecule(isIn.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(block.MustCreateAtom("France").Atom().Digest()),
	})
	w.publishRefs(bob, []cid.Digest{aliceBlock.Digest()}, block.MustCreateAtom("France"), molecule)

	carolsView := w.view(bob.PublicKey())
	for _, d := range []cid.Digest{paris.Atom().Digest(), isIn.Bond().Digest()} {
		if carolsView.Has(d) {
			t.Errorf("Carol's L3 holds Alice's %s; she subscribes to Bob, not to Alice", d)
		}
		if !w.graph.Has(d) {
			t.Errorf("Alice's %s is not in L2; the example puts it there as validation context", d)
		}
	}
	// What Carol does see is Bob's own entities — the molecule included, even
	// though the bond it names is not in her view. Filtering is per entity.
	if !carolsView.Has(molecule.Molecule().Digest()) {
		t.Error("Carol's L3 does not hold Bob's molecule")
	}

	// Carol subscribing to nobody sees nothing at all.
	empty := w.view()
	if empty.Len() != 0 {
		t.Errorf("a view with no subscriptions holds %d entities, want none", empty.Len())
	}
}

// TestFilteringIsPerEntity pins the rule of spec/05-processing-model.md,
// "Filtering rules", to the letter: "For each entity in L2, check if any of its
// authors (from the authorship tags) is in the user's subscription list."
//
// Any, not all: an entity two authors published is in the view when either of
// them is subscribed. And per entity, not per dependency: Bob's molecule is in
// the view whether or not the bond it names is. The view reports what it holds
// and does not prune around the gaps; see todo 053.
func TestFilteringIsPerEntity(t *testing.T) {
	w := newWorld(t)
	alice, bob, carol := w.builder(1), w.builder(2), w.builder(4)

	shared := block.MustCreateAtom("Paris")
	capital := block.MustCreateBond("_A_ is the capital of _B_")
	france := block.MustCreateAtom("France")
	aliceBlock := w.publish(alice, shared, capital, france)

	molecule := block.MustCreateMolecule(capital.Bond(), []entity.Filler{
		entity.AtomFiller(shared.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})
	// Bob republishes the shared atom and adds the molecule; he never
	// publishes the bond, reaching it through refs instead.
	w.publishRefs(bob, []cid.Digest{aliceBlock.Digest()}, shared, molecule)

	v := w.view(bob.PublicKey())
	// The shared atom has two authorship records and one of them is Bob's.
	if !v.Has(shared.Atom().Digest()) {
		t.Error("the shared atom is not in Bob's view, and he is one of its authors")
	}
	// The bond has one, and it is Alice's.
	if v.Has(capital.Bond().Digest()) {
		t.Error("the bond is in Bob's view; only Alice published it")
	}
	// The molecule is in the view even though the bond it names is not.
	if !v.Has(molecule.Molecule().Digest()) {
		t.Fatal("Bob's molecule is not in his own view")
	}
	m, ok := v.Lookup(molecule.Molecule().Digest())
	if !ok {
		t.Fatal("Lookup failed for a molecule the view holds")
	}
	mol, ok := m.Molecule()
	if !ok {
		t.Fatal("the entry does not answer Molecule")
	}
	if v.Has(mol.Bond()) {
		t.Error("the molecule's bond is in the view; the test needs it not to be")
	}
	// France is Alice's alone and stays out, so the molecule's fillers are
	// half-resolvable in this view too.
	if v.Has(france.Atom().Digest()) {
		t.Error("France is in Bob's view; only Alice published it")
	}

	// Carol subscribes to nobody who published anything.
	if got := w.view(carol.PublicKey()); got.Len() != 0 {
		t.Errorf("Carol's view holds %d entities, want none", got.Len())
	}
	// Subscribing to both authors is the union, not a merge: one entity, two
	// records, one place in the view.
	both := w.view(alice.PublicKey(), bob.PublicKey())
	if both.Len() != w.graph.Len() {
		t.Errorf("subscribing to every author yields %d of %d entities", both.Len(), w.graph.Len())
	}
	if e, ok := both.Lookup(shared.Atom().Digest()); !ok || len(e.Authors()) != 2 {
		t.Error("the shared atom should appear once, with both authorship records")
	}
}

// TestSpecPrivateChainFiltering covers the ratified reading of
// spec/05-processing-model.md, "Private chains", step 3: private chain data is
// filtered exactly as any other data is, and "Decryption capability and
// subscription are orthogonal."
//
// The node holds the content key of a chain it did not write — which is what
// per-recipient key wrapping exists for (spec/04-cryptography.md, "Key
// management") — so the chain's operations reach L2. They reach L3 only when
// its author is subscribed.
func TestSpecPrivateChainFiltering(t *testing.T) {
	w := newWorld(t)
	author, reader := w.builder(6), w.builder(7)

	note := block.MustCreateAtom("a private observation")
	when := block.MustCreateAtom("2026-08-15")
	noted := block.MustCreateBond("_A_ was noted on _B_")
	molecule := block.MustCreateMolecule(noted.Bond(), []entity.Filler{
		entity.AtomFiller(note.Atom().Digest()),
		entity.AtomFiller(when.Atom().Digest()),
	})
	w.private(author, w.key(40), note, when, noted, molecule)
	w.publish(reader, block.MustCreateAtom("the reader's own public note"))

	private := []cid.Digest{
		note.Atom().Digest(), when.Atom().Digest(),
		noted.Bond().Digest(), molecule.Molecule().Digest(),
	}
	for _, d := range private {
		if !w.graph.Has(d) {
			t.Fatalf("%s is not in L2; the node holds the key and decrypted the block", d)
		}
	}

	// The reader holds the key and has not subscribed to the author. Nothing
	// of the author's is in their view.
	readersView := w.view(reader.PublicKey())
	for _, d := range private {
		if readersView.Has(d) {
			t.Errorf("%s is in the reader's L3; a content key is a capability to read, not a declaration to accept", d)
		}
	}
	if readersView.Len() != 1 {
		t.Errorf("the reader's view holds %d entities, want only their own", readersView.Len())
	}

	// Subscribing to the author is what admits the data, exactly as for
	// public data.
	subscribed := w.view(reader.PublicKey(), author.PublicKey())
	for _, d := range private {
		if !subscribed.Has(d) {
			t.Errorf("%s is missing from a view subscribed to its author", d)
		}
	}

	// And the author's own node: their key is one they hold, which is a
	// subscription like any other.
	own, err := Build(w.graph, w.store, NewSubscriptions().SubscribeOwn(author.PublicKey()))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, d := range private {
		if !own.Has(d) {
			t.Errorf("%s is missing from the author's own view", d)
		}
	}
	if !own.subs.IsOwn(author.PublicKey()) || !own.subs.Contains(author.PublicKey()) {
		t.Error("an own key must be both own and subscribed")
	}
	if own.subs.IsOwn(reader.PublicKey()) {
		t.Error("a key that was never added as own must not be reported as one")
	}
}

// TestUndecryptablePrivateChainIsInvisible is the other half of
// spec/05-processing-model.md, "Private chains", step 2: "If not, the block is
// opaque." A node without the key has nothing in L2 to filter, so the
// subscription buys it nothing.
func TestUndecryptablePrivateChainIsInvisible(t *testing.T) {
	w := newWorld(t)
	author := w.builder(6)
	note := block.MustCreateAtom("a private observation")

	payload := block.Payload{TS: 1_700_000_000, Ops: []block.Operation{note}}
	blk, err := privacy.SealBlock(author, w.key(40), payload, &testKeyReader{b: 11})
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	if err := w.store.Add(blk); err != nil {
		t.Fatalf("storing the private block: %v", err)
	}
	// The node stores the block and cannot open it, so nothing is ingested.
	if err := w.graph.Ingest(blk); err == nil {
		t.Fatal("a private block was ingested without its payload")
	}

	v := w.view(author.PublicKey())
	if v.Len() != 0 {
		t.Errorf("the view holds %d entities from an undecryptable chain, want none", v.Len())
	}
	if v.Has(note.Atom().Digest()) {
		t.Error("the note reached L3 without ever reaching L2")
	}
}

// TestBuildRejectsMissingInputs covers the two things Build cannot work
// without, and the one it can: a nil subscription set is an empty view, not an
// error.
func TestBuildRejectsMissingInputs(t *testing.T) {
	w := newWorld(t)
	w.publish(w.builder(1), block.MustCreateAtom("Paris"))

	if _, err := Build(nil, w.store, NewSubscriptions()); !errors.Is(err, ErrNoGraph) {
		t.Errorf("Build with no graph: %v, want ErrNoGraph", err)
	}
	if _, err := Build(w.graph, nil, NewSubscriptions()); !errors.Is(err, ErrNoSource) {
		t.Errorf("Build with no source: %v, want ErrNoSource", err)
	}
	v, err := Build(w.graph, w.store, nil)
	if err != nil {
		t.Fatalf("Build with nil subscriptions: %v", err)
	}
	if v.Len() != 0 || len(v.Subscriptions()) != 0 {
		t.Errorf("a nil subscription set yielded %d entities, want an empty view", v.Len())
	}
}

// TestBuildNeedsTheBlocksItOrdersBy pins the contract of the L1 source: a block
// L2 holds is by definition one L1 validated and holds ("for each valid block in
// L1", spec/05-processing-model.md, "Accumulation rules"), so a source missing
// one is an inconsistency between the layers rather than a state L3 can
// interpret.
func TestBuildNeedsTheBlocksItOrdersBy(t *testing.T) {
	w := newWorld(t)
	s := newSubject()
	alice := w.builder(1)
	w.publish(alice, append(s.ops(), metaBondOp(entity.TemplateTruthAssertion), isTrue(s.digest()))...)

	// An empty store: the graph names blocks it does not hold.
	_, err := Build(w.graph, block.NewMemStore(), NewSubscriptions(alice.PublicKey()))
	if !errors.Is(err, block.ErrNotFound) {
		t.Errorf("Build over a source missing the assertion's block: %v, want an error wrapping block.ErrNotFound", err)
	}

	// Without a truth meta-molecule there is nothing to order, and nothing to
	// read from the source.
	plain := graph.New()
	if err := plain.Ingest(w.blocks[0]); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if _, err := Build(plain, block.NewMemStore(), NewSubscriptions()); err != nil {
		t.Errorf("Build with nothing to order: %v", err)
	}
}

// TestSubscriptionsSet covers the subscription set itself: the ordering it
// answers in, the keys it refuses, and the snapshot Build takes.
func TestSubscriptionsSet(t *testing.T) {
	alice, bob := testPub(t, 1), testPub(t, 2)
	subs := NewSubscriptions(bob, alice, bob)
	if subs.Len() != 2 {
		t.Errorf("Len = %d, want 2 — subscribing twice is once", subs.Len())
	}
	keys := subs.Keys()
	if len(keys) != 2 || compareKeys(keys[0], keys[1]) >= 0 {
		t.Errorf("Keys are not ascending: %x", keys)
	}
	if !subs.Contains(alice) || !subs.Contains(bob) {
		t.Error("a subscribed author is not contained")
	}
	if subs.Contains(testPub(t, 3)) {
		t.Error("an author who was never subscribed is contained")
	}
	// A key no block can carry matches nothing.
	if subs.Subscribe([]byte{1, 2, 3}).Len() != 2 {
		t.Error("a key that is not 32 bytes was accepted")
	}
	if subs.Contains([]byte{1, 2, 3}) {
		t.Error("a key that is not 32 bytes is contained")
	}
	// Own is a reason for a subscription, not a second kind of one.
	if subs.IsOwn(alice) {
		t.Error("a plain subscription is not an own key")
	}
	subs.SubscribeOwn(alice)
	if !subs.IsOwn(alice) || subs.Len() != 2 {
		t.Error("marking a subscribed key as own should not add a subscription")
	}

	// A view is a snapshot: subscribing afterwards does not change it.
	w := newWorld(t)
	w.publish(w.builder(1), block.MustCreateAtom("Paris"))
	held := NewSubscriptions()
	v, err := Build(w.graph, w.store, held)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	held.Subscribe(alice)
	if v.Len() != 0 || len(v.Subscriptions()) != 0 {
		t.Error("subscribing after Build changed the view")
	}
}
