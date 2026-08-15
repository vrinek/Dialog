package graph

import (
	"bytes"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/privacy"
)

// ingestPrivate is the reference wiring for a private block: decrypt and
// validate in one call, then hand the payload to the graph
// (spec/05-processing-model.md, "Private chains", step 2).
func ingestPrivate(t testing.TB, g *Graph, store *block.MemStore, b *block.Block, key privacy.Key) block.Payload {
	t.Helper()
	if err := store.Add(b); err != nil {
		t.Fatalf("storing %s: %v", b, err)
	}
	p, _, err := privacy.OpenAndValidate(b, key, store, nil)
	if err != nil {
		t.Fatalf("opening and validating %s: %v", b, err)
	}
	if err := g.IngestPayload(b, p); err != nil {
		t.Fatalf("ingesting %s: %v", b, err)
	}
	return p
}

// TestSpecFullDataFlow reproduces the worked example of
// spec/05-processing-model.md, "Examples", "Full data flow", as far as L2
// reaches:
//
//	Author Alice publishes a block:
//	  Block: {ops: [create_atom("Paris"), create_bond("_A_ is in _B_"), ...]}
//	L1: Block is validated and stored in Alice's chain
//	L2: Operations are extracted:
//	  - Atom "Paris" added to graph, tagged with Alice's pubkey
//	  - Bond "_A_ is in _B_" added to graph, tagged with Alice's pubkey
//
// The rest of the example is L3: Bob subscribes and sees both, Carol does not
// subscribe and sees neither. What this test asserts about that is the sentence
// the example ends on — Alice's data is in L2 whether or not anyone subscribes
// to her ("in which case Alice's data is in L2 but not L3").
func TestSpecFullDataFlow(t *testing.T) {
	paris := block.MustCreateAtom("Paris")
	isIn := block.MustCreateBond("_A_ is in _B_")

	alice := mustBuilder(t, 1)
	aliceBlock, err := alice.Public(1740067200, nil, paris, isIn)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}

	// L1: the block is validated and stored before L2 sees it.
	store := block.NewMemStore()
	if err := store.Add(aliceBlock); err != nil {
		t.Fatalf("storing Alice's block: %v", err)
	}
	if _, err := block.Validate(aliceBlock, store, nil); err != nil {
		t.Fatalf("Alice's block must be valid: %v", err)
	}

	// L2: the operations are extracted.
	g := New()
	if err := g.Ingest(aliceBlock); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if g.Len() != 2 {
		t.Fatalf("the graph holds %d entities, want exactly 2 — the atom and the bond", g.Len())
	}
	atom := mustLookup(t, g, paris.Atom().Digest())
	if atom.Kind() != block.KindAtom || !atom.AuthoredBy(alice.PublicKey()) {
		t.Errorf(`atom "Paris" = %v, want an atom tagged with Alice's pubkey`, atom)
	}
	bond := mustLookup(t, g, isIn.Bond().Digest())
	if bond.Kind() != block.KindBond || !bond.AuthoredBy(alice.PublicKey()) {
		t.Errorf(`bond "_A_ is in _B_" = %v, want a bond tagged with Alice's pubkey`, bond)
	}
	for _, e := range g.Entries() {
		prov := e.Authors()
		if len(prov) != 1 || prov[0].Block != aliceBlock.Digest() {
			t.Errorf("%v: provenance %v, want Alice's block %s", e, authorsOf(e), aliceBlock.Digest())
		}
	}

	// Carol subscribes to nobody. L2 is unaffected: "L2 is unaffected — it
	// accumulates all data pulled at L1 without filtering"
	// (spec/05-processing-model.md, "Filtering rules"). The graph has no notion
	// of a subscription at all — Carol's key simply authored nothing.
	carol := testPub(t, 3)
	if got := g.EntriesByAuthor(carol); got != nil {
		t.Errorf("Carol authored %d entities, want none", len(got))
	}
	if g.Len() != 2 {
		t.Error("querying by an unsubscribed author changed the graph")
	}
}

// TestSpecForeignChainLoading reproduces the second worked example of
// spec/05-processing-model.md, "Foreign chain loading (demand-driven
// resolution)":
//
//	Alice's chain: block_1 → block_2 → block_3
//	Bob's chain:   block_A → block_B (refs: [Alice's block_2])
//	...
//	6. Alice's block_1 is NOT fetched (not needed for resolution)
//	Alice's data in L2 is limited to what was actually needed.
//
// What lands in L2 is therefore exactly what resolution touched: Bob's two
// blocks and the one block of Alice's that provided the bond.
func TestSpecForeignChainLoading(t *testing.T) {
	store := block.NewMemStore()

	// Alice's chain. Only block_2 provides an entity Bob's block needs.
	alice := mustBuilder(t, 1)
	block1, err := alice.Public(100, nil, block.MustCreateAtom("Alice's first entity"))
	if err != nil {
		t.Fatalf("block_1: %v", err)
	}
	capital := block.MustCreateBond("_A_ is the capital of _B_")
	block2, err := alice.Public(200, nil, capital)
	if err != nil {
		t.Fatalf("block_2: %v", err)
	}
	block3, err := alice.Public(300, nil, block.MustCreateAtom("Alice's third entity"))
	if err != nil {
		t.Fatalf("block_3: %v", err)
	}
	if err := store.AddAll(block1, block2, block3); err != nil {
		t.Fatalf("storing Alice's chain: %v", err)
	}

	// Bob's chain. block_B's molecule needs the bond block_2 created.
	bob := mustBuilder(t, 2)
	blockA, err := bob.Public(400, nil, block.MustCreateAtom("Bob's first entity"))
	if err != nil {
		t.Fatalf("block_A: %v", err)
	}
	paris := block.MustCreateAtom("Paris, the capital of France")
	france := block.MustCreateAtom("France")
	molecule := block.MustCreateMolecule(capital.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})
	blockB, err := bob.Public(500, []cid.Digest{block2.Digest()}, paris, france, molecule)
	if err != nil {
		t.Fatalf("block_B: %v", err)
	}
	if err := store.AddAll(blockA, blockB); err != nil {
		t.Fatalf("storing Bob's chain: %v", err)
	}

	report, err := block.Validate(blockB, store, nil)
	if err != nil {
		t.Fatalf("block_B must be valid: %v", err)
	}
	if report.Scanned != 1 {
		t.Errorf("resolution scanned %d foreign block(s), want 1 — block_1 and block_3 are not needed", report.Scanned)
	}

	// L2 receives Bob's chain and the one foreign block resolution needed. Bob
	// is subscribed to; Alice is not, and her block is here as validation
	// context, which L2 accumulates like anything else.
	g := New()
	for _, b := range []*block.Block{blockA, blockB, block2} {
		if err := g.Ingest(b); err != nil {
			t.Fatalf("ingesting %s: %v", b, err)
		}
	}

	if g.Len() != 5 {
		t.Errorf("the graph holds %d entities, want 5 (Bob's four, Alice's bond)", g.Len())
	}
	bond := mustLookup(t, g, capital.Bond().Digest())
	if !bond.AuthoredBy(alice.PublicKey()) || bond.AuthoredBy(bob.PublicKey()) {
		t.Errorf("the bond is tagged %v; it was created in Alice's block_2 and only there", authorsOf(bond))
	}
	if mol := mustLookup(t, g, molecule.Molecule().Digest()); !mol.AuthoredBy(bob.PublicKey()) {
		t.Errorf("the molecule is tagged %v, want Bob", authorsOf(mol))
	}
	// "Alice's data in L2 is limited to what was actually needed."
	for _, unwanted := range []struct {
		what string
		d    cid.Digest
	}{
		{"block_1's atom", entity.MustAtom("Alice's first entity").Digest()},
		{"block_3's atom", entity.MustAtom("Alice's third entity").Digest()},
	} {
		if g.Has(unwanted.d) {
			t.Errorf("%s reached L2; the example does not fetch that block", unwanted.what)
		}
	}
	if g.HasBlock(block1.Digest()) || g.HasBlock(block3.Digest()) {
		t.Error("a block resolution never needed was ingested")
	}
}

// TestSpecNoInterpretation covers spec/05-processing-model.md, "No
// interpretation": "Meta-molecules (e.g., 'X is true', 'A is the same as B') are
// stored as regular molecules in L2. Their special semantics are only applied
// during L2→L3 processing."
//
// So two subscribed authors flatly contradicting each other about the same
// molecule — one asserts it is true, the other that it is untrue — produce three
// molecules in L2 and no conflict: surfacing the conflict is L3's duty
// (spec/05-processing-model.md, "Meta-molecule application"), and L2 has no
// opinion to have.
func TestSpecNoInterpretation(t *testing.T) {
	store := block.NewMemStore()
	g := New()

	// The subject molecule: "Paris is the capital of France".
	capital := block.MustCreateBond("_A_ is the capital of _B_")
	paris := block.MustCreateAtom("Paris, the capital of France")
	france := block.MustCreateAtom("France")
	subject := block.MustCreateMolecule(capital.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})

	// Alice asserts it is true; Bob asserts it is untrue. Both meta-molecules
	// are built from the standard meta-bonds of spec/06-meta-bonds.md.
	assertion := block.MustCreateBond(entity.TemplateTruthAssertion)
	retraction := block.MustCreateBond(entity.TemplateTruthRetraction)
	isTrue := block.MustCreateMolecule(assertion.Bond(), []entity.Filler{
		entity.MoleculeFiller(subject.Molecule().Digest()),
	})
	isUntrue := block.MustCreateMolecule(retraction.Bond(), []entity.Filler{
		entity.MoleculeFiller(subject.Molecule().Digest()),
	})

	alice := mustBuilder(t, 1)
	aliceBlock, err := alice.Public(100, nil, paris, france, capital, subject, assertion, isTrue)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	ingest(t, g, store, aliceBlock)

	bob := mustBuilder(t, 2)
	bobBlock, err := bob.Public(200, []cid.Digest{aliceBlock.Digest()}, retraction, isUntrue)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	ingest(t, g, store, bobBlock)

	// Three molecules, no interpretation, no conflict, no error.
	molecules := g.EntriesOfKind(block.KindMolecule)
	if len(molecules) != 3 {
		t.Fatalf("the graph holds %d molecules, want 3 — the subject and the two meta-molecules", len(molecules))
	}
	for _, d := range []cid.Digest{isTrue.Molecule().Digest(), isUntrue.Molecule().Digest()} {
		e := mustLookup(t, g, d)
		if e.Kind() != block.KindMolecule {
			t.Errorf("meta-molecule %s is stored as %s, want a plain molecule", d, e.Kind())
		}
		m, ok := e.Molecule()
		if !ok {
			t.Fatalf("meta-molecule %s does not answer Molecule", d)
		}
		// The bond digest is what makes it a meta-molecule, and it is there for
		// L3 to read — L2 stored it without looking.
		if !entity.IsMetaBond(m.Bond()) {
			t.Errorf("molecule %s: bond %s is not a standard meta-bond", d, m.Bond())
		}
	}
	// The subject molecule carries one authorship record — Alice's. That Bob
	// published a retraction of it says nothing about who authored it.
	subjectEntry := mustLookup(t, g, subject.Molecule().Digest())
	if len(subjectEntry.Authors()) != 1 || !subjectEntry.AuthoredBy(alice.PublicKey()) {
		t.Errorf("the subject molecule is tagged %v, want Alice alone", authorsOf(subjectEntry))
	}
	if !mustLookup(t, g, isUntrue.Molecule().Digest()).AuthoredBy(bob.PublicKey()) {
		t.Error(`the "is untrue" molecule must be tagged with Bob's key`)
	}
}

// TestSpecPrivateChain covers spec/05-processing-model.md, "Private chains",
// step 2: "If the node holds the decryption key, the enc field is decrypted to
// recover refs, ts, and ops, and the operations are added to the graph. If not,
// the block is opaque."
//
// Both halves are here: the key holder's graph gains the private chain's
// entities, tagged with the author's key, which a private block carries in the
// clear; the node without the key can do nothing but store the block, and its
// graph refuses it.
func TestSpecPrivateChain(t *testing.T) {
	key, err := privacy.GenerateKey(&testKeyReader{b: 40})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	author := mustBuilder(t, 6)

	diary := block.MustCreateAtom("a private observation")
	noted := block.MustCreateBond("_A_ was noted on _B_")
	when := block.MustCreateAtom("2026-08-15")
	molecule := block.MustCreateMolecule(noted.Bond(), []entity.Filler{
		entity.AtomFiller(diary.Atom().Digest()),
		entity.AtomFiller(when.Atom().Digest()),
	})
	payload := block.Payload{TS: 1755216000, Ops: []block.Operation{diary, when, noted, molecule}}
	private, err := privacy.SealBlock(author, key, payload, &testKeyReader{b: 11})
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}

	// The key holder's node.
	store, g := block.NewMemStore(), New()
	opened := ingestPrivate(t, g, store, private, key)
	if opened.TS != payload.TS {
		t.Errorf("decrypted ts = %d, want %d", opened.TS, payload.TS)
	}
	if g.Len() != 4 {
		t.Errorf("the graph holds %d entities, want 4", g.Len())
	}
	for _, d := range []cid.Digest{
		diary.Atom().Digest(), when.Atom().Digest(),
		noted.Bond().Digest(), molecule.Molecule().Digest(),
	} {
		e := mustLookup(t, g, d)
		if !e.AuthoredBy(author.PublicKey()) {
			t.Errorf("%v is tagged %v, want the block's pub key", e, authorsOf(e))
		}
		if prov := e.Authors(); len(prov) != 1 || prov[0].Block != private.Digest() {
			t.Errorf("%v: provenance %v, want the private block %s", e, authorsOf(e), private.Digest())
		}
	}

	// A node without the key: the block is opaque, and nothing about it can
	// reach L2.
	opaque := New()
	if err := opaque.Ingest(private); err == nil {
		t.Error("a node without the key ingested a private block")
	}
	if _, err := privacy.Open(private, privacy.Key{}); err == nil {
		t.Error("the wrong key opened the block")
	}
	if opaque.Len() != 0 || opaque.BlockCount() != 0 {
		t.Errorf("the opaque node's graph holds %d entities from %d block(s), want none", opaque.Len(), opaque.BlockCount())
	}

	// The author's key is in the clear even for that node, which is what makes
	// the authorship tag of a decrypted block unambiguous.
	if !bytes.Equal(private.PublicKey(), author.PublicKey()) {
		t.Error("a private block does not carry its author's key in the clear")
	}
}
