package graph

import (
	"crypto/ed25519"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/privacy"
)

// A scenario is a node's whole L1 state — the store, the blocks in the order
// they were published, and the payloads of the private ones — built from real
// signed blocks and validated exactly as a node would validate them. The graph
// tests ingest it in several orders; the determinism test ingests it a hundred
// times.
type scenario struct {
	store    *block.MemStore
	blocks   []*block.Block
	payloads map[cid.Digest]block.Payload

	alice, bob, carol *block.Builder
	successor         *block.Builder
	rotation          *block.Block

	// The entities the scenario's authors publish, for the assertions.
	paris, france, capital, molecule cid.Digest
	privateNote                      cid.Digest
	successorAtom                    cid.Digest
}

// buildScenario publishes four chains into one store:
//
//   - Alice publishes two atoms, a bond and the molecule that joins them.
//   - Bob republishes the same molecule and atoms, referencing Alice's block for
//     the bond he did not create — a foreign reference, and three entities that
//     end up with two authorship records each.
//   - Alice rotates her key: a rotation block ends her chain, and a successor
//     chain under the new key opens with a public genesis block referencing it
//     (spec/02-block-format.md, "Verifiable succession"). The successor
//     republishes "France" once more.
//   - Carol keeps a private chain. Her blocks are opaque to anyone without the
//     key; this node holds it.
//
// Every block is validated before it is returned, so a test may ingest them in
// any order without breaking the contract the graph documents.
func buildScenario(t testing.TB) *scenario {
	t.Helper()

	s := &scenario{store: block.NewMemStore(), payloads: make(map[cid.Digest]block.Payload)}
	s.alice, s.bob, s.carol = mustBuilder(t, 1), mustBuilder(t, 2), mustBuilder(t, 4)
	s.successor = mustBuilder(t, 3)

	paris := block.MustCreateAtom("Paris, the capital of France")
	france := block.MustCreateAtom("France")
	capital := block.MustCreateBond("_A_ is the capital of _B_")
	molecule := block.MustCreateMolecule(capital.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})
	s.paris, s.france = paris.Atom().Digest(), france.Atom().Digest()
	s.capital, s.molecule = capital.Bond().Digest(), molecule.Molecule().Digest()

	// Alice.
	aliceGenesis := s.publish(t, s.alice, 100, nil, paris, france, capital, molecule)

	// Bob, republishing Alice's molecule. His block creates the two atoms
	// itself and reaches the bond through refs.
	s.publish(t, s.bob, 200, nil, block.MustCreateAtom("Bob's first entity"))
	s.publish(t, s.bob, 300, []cid.Digest{aliceGenesis.Digest()}, paris, france, molecule)

	// Alice rotates. The rotation block carries one rotate_key operation and
	// creates no entity.
	rotation, err := s.alice.Rotation(400, nil, s.successor.PublicKey())
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	s.rotation = s.validated(t, rotation)

	// The successor chain. Its genesis block must be public and must reference
	// the rotation block; Succeeds does both.
	if err := s.successor.Succeeds(rotation); err != nil {
		t.Fatalf("Succeeds: %v", err)
	}
	successorAtom := block.MustCreateAtom("a second life for this author")
	s.successorAtom = successorAtom.Atom().Digest()
	s.publish(t, s.successor, 500, nil, france, successorAtom)

	// Carol's private chain.
	key, err := privacy.GenerateKey(&testKeyReader{b: 40})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	note := block.MustCreateAtom("a private observation")
	s.privateNote = note.Atom().Digest()
	// A self-contained private block: it republishes everything its molecule
	// needs rather than referencing another chain, which is the "fat block"
	// strategy spec/05-processing-model.md permits.
	payload := block.Payload{TS: 600, Ops: []block.Operation{note, paris, france, capital, molecule}}
	private, err := privacy.SealBlock(s.carol, key, payload, &testKeyReader{b: 11})
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	if err := s.store.Add(private); err != nil {
		t.Fatalf("storing Carol's block: %v", err)
	}
	opened, _, err := privacy.OpenAndValidate(private, key, s.store, nil)
	if err != nil {
		t.Fatalf("Carol's private block must be valid: %v", err)
	}
	s.blocks = append(s.blocks, private)
	s.payloads[private.Digest()] = opened

	return s
}

// publish signs a public block onto a builder's chain, stores it and validates
// it, in the order a node does.
func (s *scenario) publish(t testing.TB, b *block.Builder, ts uint64, refs []cid.Digest, ops ...block.Operation) *block.Block {
	t.Helper()
	blk, err := b.Public(ts, refs, ops...)
	if err != nil {
		t.Fatalf("signing a block for %x: %v", b.PublicKey()[:8], err)
	}
	return s.validated(t, blk)
}

// validated stores a block and validates it, which is what earns it a place in
// the scenario: L2 may only see blocks whose validation succeeded
// (spec/05-processing-model.md, "Block reception").
func (s *scenario) validated(t testing.TB, blk *block.Block) *block.Block {
	t.Helper()
	if err := s.store.Add(blk); err != nil {
		t.Fatalf("storing %s: %v", blk, err)
	}
	if _, err := block.Validate(blk, s.store, nil); err != nil {
		t.Fatalf("%s must be valid: %v", blk, err)
	}
	s.blocks = append(s.blocks, blk)
	return blk
}

// ingestAll feeds the scenario's blocks to a graph in the given order, using
// IngestPayload for the private ones.
func (s *scenario) ingestAll(t testing.TB, g *Graph, order []int) {
	t.Helper()
	for _, i := range order {
		b := s.blocks[i]
		if p, ok := s.payloads[b.Digest()]; ok {
			if err := g.IngestPayload(b, p); err != nil {
				t.Fatalf("ingesting private block %s: %v", b, err)
			}
			continue
		}
		if err := g.Ingest(b); err != nil {
			t.Fatalf("ingesting %s: %v", b, err)
		}
	}
}

// order returns the identity ingestion order: publication order.
func (s *scenario) order() []int {
	out := make([]int, len(s.blocks))
	for i := range out {
		out[i] = i
	}
	return out
}

// TestScenario walks the whole of L2's job over four real chains: accumulation
// across authors, a foreign reference, a key rotation and its successor chain,
// and a private chain the node holds the key for.
func TestScenario(t *testing.T) {
	s := buildScenario(t)
	g := New()
	s.ingestAll(t, g, s.order())

	// Six blocks in, five of them carrying operations.
	if g.BlockCount() != len(s.blocks) {
		t.Errorf("%d blocks ingested, want %d", g.BlockCount(), len(s.blocks))
	}
	if !g.HasBlock(s.rotation.Digest()) {
		t.Error("the rotation block was not recorded; re-ingesting it would not be a no-op")
	}
	// Paris, France, the bond, the molecule, Bob's first entity, the
	// successor's atom, Carol's private note.
	if g.Len() != 7 {
		t.Fatalf("the graph holds %d entities, want 7:\n%v", g.Len(), g.Entries())
	}

	// A rotate_key operation creates no entity: the successor key appears
	// nowhere in the graph, and the rotation block authored nothing.
	for _, e := range g.Entries() {
		for _, a := range e.Authors() {
			if a.Block == s.rotation.Digest() {
				t.Errorf("%v is tagged with the rotation block; a rotate_key operation creates no entity", e)
			}
		}
	}

	// Multi-tagging on same-CID republication. France was published by Alice,
	// by Bob, by Alice's successor key and inside Carol's private block: four
	// records, one entity.
	france := mustLookup(t, g, s.france)
	if got := len(france.Authors()); got != 4 {
		t.Errorf("France carries %d authorship records %v, want 4", got, authorsOf(france))
	}
	for _, pub := range []ed25519.PublicKey{
		s.alice.PublicKey(), s.bob.PublicKey(), s.successor.PublicKey(), s.carol.PublicKey(),
	} {
		if !france.AuthoredBy(pub) {
			t.Errorf("France is not tagged with %x", pub[:8])
		}
	}
	// The rotation does not merge the two keys: "Author identity (mapping
	// multiple keys to a single author) is implementation-scoped"
	// (spec/05-processing-model.md, "Chain succession"), and this graph keeps
	// the keys apart, as it found them in the pub fields.
	if got := len(g.Authors()); got != 4 {
		t.Errorf("%d authors, want 4 — a rotation is not an identity merge here", got)
	}

	// Bob reached the bond through refs but did not create it, so he is not
	// among its authors; the molecule he did create is tagged with both his key
	// and Alice's, and with Carol's, who published it privately.
	bond := mustLookup(t, g, s.capital)
	if bond.AuthoredBy(s.bob.PublicKey()) {
		t.Errorf("the bond is tagged %v; Bob referenced it, he did not publish it", authorsOf(bond))
	}
	if !bond.AuthoredBy(s.alice.PublicKey()) || !bond.AuthoredBy(s.carol.PublicKey()) {
		t.Errorf("the bond is tagged %v, want Alice and Carol", authorsOf(bond))
	}
	molecule := mustLookup(t, g, s.molecule)
	if got := len(molecule.Authors()); got != 3 {
		t.Errorf("the molecule carries %d records %v, want 3", got, authorsOf(molecule))
	}

	// The private chain's entities are in the same graph as everything else,
	// tagged with the pub key the private block carries in the clear.
	note := mustLookup(t, g, s.privateNote)
	if got := note.Authors(); len(got) != 1 || !note.AuthoredBy(s.carol.PublicKey()) {
		t.Errorf("the private note is tagged %v, want Carol alone", authorsOf(note))
	}

	// Per-author views.
	for _, tc := range []struct {
		name string
		pub  ed25519.PublicKey
		want int
	}{
		{"Alice", s.alice.PublicKey(), 4},
		{"Bob", s.bob.PublicKey(), 4},                     // his own atom plus the three he republished
		{"the successor key", s.successor.PublicKey(), 2}, // France and one new atom
		{"Carol", s.carol.PublicKey(), 5},                 // her note plus the four she republished
	} {
		if got := len(g.EntriesByAuthor(tc.pub)); got != tc.want {
			t.Errorf("%s authored %d entities, want %d", tc.name, got, tc.want)
		}
	}
	if !mustLookup(t, g, s.successorAtom).AuthoredBy(s.successor.PublicKey()) {
		t.Error("the successor chain's own atom is not tagged with the successor key")
	}

	// Kinds.
	if got := len(g.EntriesOfKind(block.KindAtom)); got != 5 {
		t.Errorf("%d atoms, want 5", got)
	}
	if got := len(g.EntriesOfKind(block.KindBond)); got != 1 {
		t.Errorf("%d bonds, want 1", got)
	}
	if got := len(g.EntriesOfKind(block.KindMolecule)); got != 1 {
		t.Errorf("%d molecules, want 1", got)
	}
}

// TestScenarioReingestionIsANoOp re-processes the entire store, which is what a
// node does after a restart, and checks that the graph is unchanged: entities,
// authorship records, authors and blocks alike.
func TestScenarioReingestionIsANoOp(t *testing.T) {
	s := buildScenario(t)

	once := New()
	s.ingestAll(t, once, s.order())
	want := fingerprint(once)

	twice := New()
	s.ingestAll(t, twice, s.order())
	s.ingestAll(t, twice, s.order())
	// And once more in reverse, since a re-processing node has no reason to
	// walk its store in the same order twice.
	reverse := s.order()
	for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}
	s.ingestAll(t, twice, reverse)

	if got := fingerprint(twice); got != want {
		t.Errorf("re-ingesting the store changed the graph:\n got %s\nwant %s", got, want)
	}
}
