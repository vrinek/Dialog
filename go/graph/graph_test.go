package graph

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/privacy"
)

func testKey(t testing.TB, seed byte) ed25519.PrivateKey {
	t.Helper()
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func testPub(t testing.TB, seed byte) ed25519.PublicKey {
	t.Helper()
	pub, ok := testKey(t, seed).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("test key does not yield an Ed25519 public key")
	}
	return pub
}

func mustBuilder(t testing.TB, seed byte) *block.Builder {
	t.Helper()
	b, err := block.NewBuilder(testKey(t, seed))
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

// testKeyReader feeds privacy.Seal a deterministic nonce, so that a test's
// blocks — and therefore their digests — are the same on every run.
type testKeyReader struct{ b byte }

func (r *testKeyReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
		r.b++
	}
	return len(p), nil
}

// ingest is the reference wiring of the package documentation, written out once
// so that every scenario test goes through it: validate first, ingest only if
// validation succeeded (spec/05-processing-model.md, "Accumulation rules").
func ingest(t testing.TB, g *Graph, store *block.MemStore, b *block.Block) {
	t.Helper()
	if err := store.Add(b); err != nil {
		t.Fatalf("storing %s: %v", b, err)
	}
	if _, err := block.Validate(b, store, nil); err != nil {
		t.Fatalf("validating %s: %v", b, err)
	}
	if err := g.Ingest(b); err != nil {
		t.Fatalf("ingesting %s: %v", b, err)
	}
}

func mustLookup(t testing.TB, g *Graph, d cid.Digest) Entry {
	t.Helper()
	e, ok := g.Lookup(d)
	if !ok {
		t.Fatalf("the graph does not hold %s", d)
	}
	return e
}

// authorSeeds renders an entry's authorship records as the seeds of the test
// keys that signed them, which is what a failure message can be read.
func authorsOf(e Entry) []string {
	out := make([]string, 0, len(e.authors))
	for _, a := range e.authors {
		out = append(out, a.String())
	}
	return out
}

// TestIngestPublicBlock covers the three accumulation steps of
// spec/05-processing-model.md: every operation's entity is extracted, keyed by
// its own digest, and tagged with the block's author and the block's identity.
func TestIngestPublicBlock(t *testing.T) {
	paris := block.MustCreateAtom("Paris, the capital of France")
	france := block.MustCreateAtom("France")
	capital := block.MustCreateBond("_A_ is the capital of _B_")
	molecule := block.MustCreateMolecule(capital.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})

	alice := mustBuilder(t, 1)
	b, err := alice.Public(1740067200, nil, paris, france, capital, molecule)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}

	g, store := New(), block.NewMemStore()
	ingest(t, g, store, b)

	if g.Len() != 4 {
		t.Errorf("the graph holds %d entities, want 4 (two atoms, a bond and a molecule)", g.Len())
	}
	if g.BlockCount() != 1 || !g.HasBlock(b.Digest()) {
		t.Errorf("the graph reports %d block(s) and HasBlock(%s) = %v", g.BlockCount(), b.Digest(), g.HasBlock(b.Digest()))
	}

	for _, tc := range []struct {
		name   string
		digest cid.Digest
		kind   block.EntityKind
	}{
		{"atom Paris", paris.Atom().Digest(), block.KindAtom},
		{"atom France", france.Atom().Digest(), block.KindAtom},
		{"bond", capital.Bond().Digest(), block.KindBond},
		{"molecule", molecule.Molecule().Digest(), block.KindMolecule},
	} {
		e := mustLookup(t, g, tc.digest)
		if e.Kind() != tc.kind {
			t.Errorf("%s: kind = %s, want %s", tc.name, e.Kind(), tc.kind)
		}
		if e.Digest() != tc.digest {
			t.Errorf("%s: digest = %s, want %s", tc.name, e.Digest(), tc.digest)
		}
		if e.CID() != tc.digest.CID() {
			t.Errorf("%s: CID = %s, want %s", tc.name, e.CID(), tc.digest.CID())
		}
		got := e.Authors()
		if len(got) != 1 {
			t.Fatalf("%s: %d authorship record(s) %v, want exactly 1", tc.name, len(got), authorsOf(e))
		}
		if !bytes.Equal(got[0].Author, alice.PublicKey()) {
			t.Errorf("%s: tagged with %x, want the block's pub %x", tc.name, got[0].Author, alice.PublicKey())
		}
		if got[0].Block != b.Digest() {
			t.Errorf("%s: provenance %s, want the block's digest %s", tc.name, got[0].Block, b.Digest())
		}
		if got[0].CID() != b.CID() {
			t.Errorf("%s: provenance CID %s, want %s", tc.name, got[0].CID(), b.CID())
		}
		if !e.AuthoredBy(alice.PublicKey()) {
			t.Errorf("%s: AuthoredBy(alice) = false", tc.name)
		}
		if e.AuthoredBy(testPub(t, 9)) {
			t.Errorf("%s: AuthoredBy(a key that published nothing) = true", tc.name)
		}
	}

	// The stored values are the entities themselves, not just their digests.
	e := mustLookup(t, g, paris.Atom().Digest())
	atom, ok := e.Atom()
	if !ok {
		t.Fatalf("the atom entry does not hold an atom: %v", e)
	}
	if atom.Description() != "Paris, the capital of France" {
		t.Errorf("atom description = %q", atom.Description())
	}
	if _, ok := e.Bond(); ok {
		t.Error("an atom entry must not answer Bond")
	}
	if _, ok := e.Molecule(); ok {
		t.Error("an atom entry must not answer Molecule")
	}
	if b, ok := mustLookup(t, g, capital.Bond().Digest()).Bond(); !ok || b.Template() != "_A_ is the capital of _B_" {
		t.Errorf("bond entry = %v, %v", b, ok)
	}
	if m, ok := mustLookup(t, g, molecule.Molecule().Digest()).Molecule(); !ok || m.Bond() != capital.Bond().Digest() {
		t.Errorf("molecule entry = %v, %v", m, ok)
	}
	if !bytes.Equal(mustLookup(t, g, paris.Atom().Digest()).Entity().Bytes(), paris.Atom().Bytes()) {
		t.Error("the stored entity's canonical bytes are not the atom's")
	}
}

// TestIngestIsIdempotent covers the invariant a node re-processing its store
// depends on: ingesting the same block twice adds nothing
// (spec/05-processing-model.md, "Fat blocks": operations are idempotent at L2,
// same CID = same entity).
func TestIngestIsIdempotent(t *testing.T) {
	alice := mustBuilder(t, 1)
	b, err := alice.Public(100, nil, block.MustCreateAtom("France"), block.MustCreateBond("_A_ is in _B_"))
	if err != nil {
		t.Fatalf("alice: %v", err)
	}

	g, store := New(), block.NewMemStore()
	ingest(t, g, store, b)
	before := g.Entries()

	for i := 0; i < 3; i++ {
		if err := g.Ingest(b); err != nil {
			t.Fatalf("re-ingesting the same block: %v", err)
		}
	}
	if g.Len() != 2 || g.BlockCount() != 1 {
		t.Errorf("after four ingestions of one block: %d entities and %d block(s), want 2 and 1", g.Len(), g.BlockCount())
	}
	after := g.Entries()
	if len(before) != len(after) {
		t.Fatalf("entry count changed from %d to %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Digest() != after[i].Digest() || len(after[i].Authors()) != 1 {
			t.Errorf("entry %d: %v became %v with %d authorship record(s)", i, before[i], after[i], len(after[i].Authors()))
		}
	}
}

// TestSameEntityTwoAuthors is the accumulation rule stated in
// spec/05-processing-model.md: "If an entity with the same CID already exists in
// L2 (because the same content was published by a different author, or
// re-published by the same author), the new authorship record is added alongside
// the existing one. The entity itself is not duplicated."
func TestSameEntityTwoAuthors(t *testing.T) {
	france := block.MustCreateAtom("France")

	alice, bob := mustBuilder(t, 1), mustBuilder(t, 2)
	aliceBlock, err := alice.Public(100, nil, france)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	// The same author, re-publishing the same content in a later block.
	aliceAgain, err := alice.Public(200, nil, france, block.MustCreateAtom("Paris"))
	if err != nil {
		t.Fatalf("alice again: %v", err)
	}
	bobBlock, err := bob.Public(300, nil, france)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}

	g, store := New(), block.NewMemStore()
	ingest(t, g, store, aliceBlock)
	ingest(t, g, store, aliceAgain)
	ingest(t, g, store, bobBlock)

	if g.Len() != 2 {
		t.Errorf("the graph holds %d entities, want 2 — France was published three times and is one entity", g.Len())
	}
	e := mustLookup(t, g, france.Atom().Digest())
	got := e.Authors()
	if len(got) != 3 {
		t.Fatalf("France carries %d authorship records %v, want 3", len(got), authorsOf(e))
	}
	// Sorted by author and then by block, so the two Alice records are adjacent
	// and in a fixed order whatever order the blocks arrived in.
	want := []Authorship{
		{Author: alice.PublicKey(), Block: aliceBlock.Digest()},
		{Author: alice.PublicKey(), Block: aliceAgain.Digest()},
		{Author: bob.PublicKey(), Block: bobBlock.Digest()},
	}
	sortAuthorships(want)
	for i, w := range want {
		if !bytes.Equal(got[i].Author, w.Author) || got[i].Block != w.Block {
			t.Errorf("record %d = %v, want %v", i, got[i], w)
		}
	}
	if !e.AuthoredBy(alice.PublicKey()) || !e.AuthoredBy(bob.PublicKey()) {
		t.Error("France must be authored by both Alice and Bob")
	}

	// Provenance is the same list, reachable without the entry.
	prov := g.Provenance(france.Atom().Digest())
	if len(prov) != 3 {
		t.Errorf("Provenance returned %d record(s), want 3", len(prov))
	}
	if got := g.Provenance(cid.Digest{}); got != nil {
		t.Errorf("Provenance of an unknown digest = %v, want nil", got)
	}

	// Paris was published by Alice alone.
	paris := mustLookup(t, g, entity.MustAtom("Paris").Digest())
	if len(paris.Authors()) != 1 || !paris.AuthoredBy(alice.PublicKey()) || paris.AuthoredBy(bob.PublicKey()) {
		t.Errorf("Paris authors = %v, want Alice only", authorsOf(paris))
	}
}

// sortAuthorships puts a list in the order the graph returns it: by author key,
// then by block digest.
func sortAuthorships(a []Authorship) {
	slices.SortFunc(a, func(x, y Authorship) int {
		if c := bytes.Compare(x.Author, y.Author); c != 0 {
			return c
		}
		return bytes.Compare(x.Block[:], y.Block[:])
	})
}

// TestQueries covers the query surface: by kind, by author, and the author list.
func TestQueries(t *testing.T) {
	g, store := New(), block.NewMemStore()

	bond := block.MustCreateBond("_A_ is in _B_")
	paris := block.MustCreateAtom("Paris")
	france := block.MustCreateAtom("France")
	molecule := block.MustCreateMolecule(bond.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})

	alice, bob := mustBuilder(t, 1), mustBuilder(t, 2)
	aliceBlock, err := alice.Public(100, nil, paris, france, bond, molecule)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	ingest(t, g, store, aliceBlock)

	bobBlock, err := bob.Public(200, nil, block.MustCreateAtom("Bob's own atom"))
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	ingest(t, g, store, bobBlock)

	if got := len(g.EntriesOfKind(block.KindAtom)); got != 3 {
		t.Errorf("%d atoms, want 3", got)
	}
	if got := len(g.EntriesOfKind(block.KindBond)); got != 1 {
		t.Errorf("%d bonds, want 1", got)
	}
	if got := len(g.EntriesOfKind(block.KindMolecule)); got != 1 {
		t.Errorf("%d molecules, want 1", got)
	}
	for _, e := range g.EntriesOfKind(block.KindAtom) {
		if _, ok := e.Atom(); !ok {
			t.Errorf("EntriesOfKind(atom) returned %v", e)
		}
	}
	if got := len(g.Entries()); got != g.Len() || got != 5 {
		t.Errorf("Entries returned %d, Len is %d, want 5", got, g.Len())
	}

	if got := len(g.EntriesByAuthor(alice.PublicKey())); got != 4 {
		t.Errorf("Alice authored %d entities, want 4", got)
	}
	if got := len(g.EntriesByAuthor(bob.PublicKey())); got != 1 {
		t.Errorf("Bob authored %d entities, want 1", got)
	}
	if got := g.EntriesByAuthor(testPub(t, 9)); got != nil {
		t.Errorf("an author who published nothing has %d entities, want none", len(got))
	}
	if got := g.EntriesByAuthor([]byte("too short")); got != nil {
		t.Errorf("EntriesByAuthor with a malformed key = %v, want nil", got)
	}

	authors := g.Authors()
	if len(authors) != 2 {
		t.Fatalf("%d authors, want 2", len(authors))
	}
	if bytes.Compare(authors[0], authors[1]) >= 0 {
		t.Errorf("Authors are not in ascending key order: %x then %x", authors[0], authors[1])
	}
	for _, want := range []ed25519.PublicKey{alice.PublicKey(), bob.PublicKey()} {
		found := false
		for _, a := range authors {
			found = found || bytes.Equal(a, want)
		}
		if !found {
			t.Errorf("Authors is missing %x", want[:8])
		}
	}

	// Kind and Has answer for a held digest and refuse an unheld one.
	if k, ok := g.Kind(bond.Bond().Digest()); !ok || k != block.KindBond {
		t.Errorf("Kind(bond) = %s, %v", k, ok)
	}
	if _, ok := g.Kind(cid.Digest{}); ok {
		t.Error("Kind of an unheld digest reported ok")
	}
	if g.Has(cid.Digest{}) {
		t.Error("Has of an unheld digest reported true")
	}
	if !g.Has(molecule.Molecule().Digest()) {
		t.Error("Has of a held digest reported false")
	}

	blocks := g.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("%d blocks, want 2", len(blocks))
	}
	if bytes.Compare(blocks[0][:], blocks[1][:]) >= 0 {
		t.Errorf("Blocks is not in ascending order: %s then %s", blocks[0], blocks[1])
	}
	if g.String() == "" {
		t.Error("String is empty")
	}
}

// TestAccessorsAreCopies is the append-only rule at the API level: nothing a
// query hands back is a window into the graph (spec/05-processing-model.md:
// "L2 is append-only. Entities MUST NOT be removed or modified once added").
func TestAccessorsAreCopies(t *testing.T) {
	alice := mustBuilder(t, 1)
	b, err := alice.Public(100, nil, block.MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	g, store := New(), block.NewMemStore()
	ingest(t, g, store, b)

	d := entity.MustAtom("France").Digest()
	e := mustLookup(t, g, d)

	authors := e.Authors()
	authors[0].Author[0] ^= 0xff
	authors[0].Block[0] ^= 0xff
	if got := mustLookup(t, g, d).Authors(); !bytes.Equal(got[0].Author, alice.PublicKey()) || got[0].Block != b.Digest() {
		t.Error("mutating a returned authorship record changed the graph")
	}

	prov := g.Provenance(d)
	prov[0].Author[0] ^= 0xff
	if got := g.Provenance(d); !bytes.Equal(got[0].Author, alice.PublicKey()) {
		t.Error("mutating a returned provenance record changed the graph")
	}

	blocks := g.Blocks()
	blocks[0] = cid.Digest{}
	if got := g.Blocks(); got[0] != b.Digest() {
		t.Error("mutating the returned block list changed the graph")
	}

	keys := g.Authors()
	keys[0][0] ^= 0xff
	if got := g.Authors(); !bytes.Equal(got[0], alice.PublicKey()) {
		t.Error("mutating a returned author key changed the graph")
	}

	entries := g.Entries()
	entries[0] = Entry{}
	if got := g.Entries(); got[0].Digest() != d {
		t.Error("mutating the returned entry list changed the graph")
	}
}

// TestIngestRejections covers the input the graph refuses. None of it is
// validation — that is the caller's, per the package documentation — only input
// there is no way to use.
func TestIngestRejections(t *testing.T) {
	g := New()

	if err := g.Ingest(nil); !errors.Is(err, ErrNilBlock) {
		t.Errorf("Ingest(nil) = %v, want ErrNilBlock", err)
	}
	if err := g.IngestPayload(nil, block.Payload{}); !errors.Is(err, ErrNilBlock) {
		t.Errorf("IngestPayload(nil, ...) = %v, want ErrNilBlock", err)
	}

	key, err := privacy.GenerateKey(&testKeyReader{b: 7})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	carol := mustBuilder(t, 4)
	payload := block.Payload{TS: 100, Ops: []block.Operation{block.MustCreateAtom("a private atom")}}
	private, err := privacy.SealBlock(carol, key, payload, &testKeyReader{b: 1})
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}

	// A private block without its payload: its operations are inside the
	// ciphertext, and this package holds no keys.
	if err := g.Ingest(private); !errors.Is(err, ErrPayloadRequired) {
		t.Errorf("Ingest(private block) = %v, want ErrPayloadRequired", err)
	}
	if g.Len() != 0 || g.BlockCount() != 0 {
		t.Errorf("a refused block left %d entities and %d block(s) behind", g.Len(), g.BlockCount())
	}

	// A public block handed to IngestPayload.
	alice := mustBuilder(t, 1)
	public, err := alice.Public(100, nil, block.MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	if err := g.IngestPayload(public, payload); err == nil {
		t.Error("IngestPayload accepted a public block; its ops are already in the clear")
	}
	rotation, err := alice.Rotation(200, nil, testPub(t, 3))
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	if err := g.IngestPayload(rotation, payload); err == nil {
		t.Error("IngestPayload accepted a rotation block")
	}

	// A payload that is not well-formed: no operations at all, and a rotate_key
	// operation, both of which block.Payload.Validate refuses.
	if err := g.IngestPayload(private, block.Payload{TS: 1}); err == nil {
		t.Error("IngestPayload accepted a payload with no operations")
	}
	rotateInPayload := block.Payload{TS: 1, Ops: []block.Operation{block.MustRotateKey(testPub(t, 5))}}
	if err := g.IngestPayload(private, rotateInPayload); err == nil {
		t.Error("IngestPayload accepted a payload carrying a rotate_key operation")
	}
	if g.Len() != 0 || g.BlockCount() != 0 {
		t.Errorf("refused input left %d entities and %d block(s) behind", g.Len(), g.BlockCount())
	}

	// The real payload goes in, and a second, different payload for the same
	// block is refused: a block's digest covers its ciphertext, and a ciphertext
	// has one plaintext.
	if err := g.IngestPayload(private, payload); err != nil {
		t.Fatalf("IngestPayload: %v", err)
	}
	if err := g.IngestPayload(private, payload); err != nil {
		t.Errorf("re-ingesting the same block with the same payload = %v, want a no-op", err)
	}
	other := block.Payload{TS: 100, Ops: []block.Operation{block.MustCreateAtom("a different private atom")}}
	if err := g.IngestPayload(private, other); !errors.Is(err, ErrPayloadMismatch) {
		t.Errorf("a second payload for the same block = %v, want ErrPayloadMismatch", err)
	}
	if g.Len() != 1 {
		t.Errorf("the graph holds %d entities, want 1 — the mismatched payload must not have been ingested", g.Len())
	}
	if g.Has(entity.MustAtom("a different private atom").Digest()) {
		t.Error("the mismatched payload's atom reached the graph")
	}
}

// TestConcurrentUse checks the documented concurrency policy: a Graph is safe
// for concurrent use, ingestion and queries alike. Run it with -race.
func TestConcurrentUse(t *testing.T) {
	const authors = 4
	blocks := make([]*block.Block, 0, authors)
	store := block.NewMemStore()
	for i := range authors {
		author := mustBuilder(t, byte(i+1))
		b, err := author.Public(uint64(100+i), nil,
			block.MustCreateAtom("shared atom"), // every author publishes it
			block.MustCreateAtom(string(rune('a'+i))+" atom"),
			block.MustCreateBond("_A_ is next to _B_"),
		)
		if err != nil {
			t.Fatalf("author %d: %v", i, err)
		}
		if err := store.Add(b); err != nil {
			t.Fatalf("store: %v", err)
		}
		if _, err := block.Validate(b, store, nil); err != nil {
			t.Fatalf("validating author %d's block: %v", i, err)
		}
		blocks = append(blocks, b)
	}

	g := New()
	done := make(chan struct{})
	for _, b := range blocks {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 20 {
				if err := g.Ingest(b); err != nil {
					t.Errorf("Ingest: %v", err)
					return
				}
			}
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			for range 20 {
				g.Entries()
				g.EntriesByAuthor(b.PublicKey())
				g.Provenance(entity.MustAtom("shared atom").Digest())
				g.Authors()
				g.Blocks()
				_ = g.Len()
			}
		}()
	}
	for range 2 * len(blocks) {
		<-done
	}

	if g.Len() != 1+authors+1 {
		t.Errorf("the graph holds %d entities, want %d", g.Len(), 2+authors)
	}
	if got := len(g.Provenance(entity.MustAtom("shared atom").Digest())); got != authors {
		t.Errorf("the shared atom carries %d authorship records, want %d", got, authors)
	}
	if g.BlockCount() != authors {
		t.Errorf("%d blocks ingested, want %d", g.BlockCount(), authors)
	}
}
