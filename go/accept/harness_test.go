package accept

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/graph"
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

// A world is a node's whole L1 and L2 state: the block store, the graph built
// from it, and the blocks in publication order. Every test drives the real
// pipeline through it — sign, store, validate, ingest — so that an L3 view is
// always built from blocks a node would have accepted.
//
//	block.Builder -> block.Validate -> graph.Ingest -> accept.Build
type world struct {
	t        testing.TB
	store    *block.MemStore
	graph    *graph.Graph
	blocks   []*block.Block
	payloads map[cid.Digest]block.Payload
	ts       uint64
}

func newWorld(t testing.TB) *world {
	t.Helper()
	return &world{
		t:        t,
		store:    block.NewMemStore(),
		graph:    graph.New(),
		payloads: make(map[cid.Digest]block.Payload),
		ts:       1_700_000_000,
	}
}

// builder returns the chain writer for a test key.
func (w *world) builder(seed byte) *block.Builder {
	w.t.Helper()
	b, err := block.NewBuilder(testKey(w.t, seed))
	if err != nil {
		w.t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

// nextTS returns an increasing timestamp, so that blocks published in order
// carry timestamps in that order too — except where a test deliberately breaks
// it.
func (w *world) nextTS() uint64 {
	w.ts += 60
	return w.ts
}

// publish signs, stores, validates and ingests a public block.
func (w *world) publish(b *block.Builder, ops ...block.Operation) *block.Block {
	w.t.Helper()
	return w.publishAt(b, w.nextTS(), nil, ops...)
}

// publishRefs is publish with an explicit refs list.
func (w *world) publishRefs(b *block.Builder, refs []cid.Digest, ops ...block.Operation) *block.Block {
	w.t.Helper()
	return w.publishAt(b, w.nextTS(), refs, ops...)
}

// publishAt is publish with an explicit timestamp, for the tests that need the
// ts field to disagree with the block order.
func (w *world) publishAt(b *block.Builder, ts uint64, refs []cid.Digest, ops ...block.Operation) *block.Block {
	w.t.Helper()
	blk, err := b.Public(ts, refs, ops...)
	if err != nil {
		w.t.Fatalf("signing a block for %x: %v", b.PublicKey()[:8], err)
	}
	return w.accept(blk)
}

// rotate ends a builder's chain with a rotation block and returns it.
func (w *world) rotate(b *block.Builder, newPub ed25519.PublicKey) *block.Block {
	w.t.Helper()
	blk, err := b.Rotation(w.nextTS(), nil, newPub)
	if err != nil {
		w.t.Fatalf("rotation: %v", err)
	}
	return w.accept(blk)
}

// succeed opens the successor chain of a rotation block with a public genesis
// block that references it (spec/02-block-format.md, "Verifiable succession").
// Builder.Succeeds prepends the reference to the rotation block; refs carries
// whatever else the genesis block's operations need to resolve.
func (w *world) succeed(b *block.Builder, rotation *block.Block, refs []cid.Digest, ops ...block.Operation) *block.Block {
	w.t.Helper()
	if err := b.Succeeds(rotation); err != nil {
		w.t.Fatalf("Succeeds: %v", err)
	}
	return w.publishRefs(b, refs, ops...)
}

// forked signs, stores, validates and ingests a block that knowingly forks
// another — two genesis blocks claiming one rotation, in the one test that
// needs an ambiguous succession. The store's policy is accept-and-flag, so the
// ForkError it reports is the expected outcome and not a failure.
func (w *world) forked(b *block.Builder, rotation *block.Block, ops ...block.Operation) *block.Block {
	w.t.Helper()
	if err := b.Succeeds(rotation); err != nil {
		w.t.Fatalf("Succeeds: %v", err)
	}
	blk, err := b.Public(w.nextTS(), nil, ops...)
	if err != nil {
		w.t.Fatalf("signing a forking block: %v", err)
	}
	var forkErr *block.ForkError
	if err := w.store.Add(blk); err != nil && !errors.As(err, &forkErr) {
		w.t.Fatalf("storing %s: %v", blk, err)
	}
	if _, err := block.Validate(blk, w.store, nil); err != nil {
		w.t.Fatalf("%s must be valid: %v", blk, err)
	}
	if err := w.graph.Ingest(blk); err != nil {
		w.t.Fatalf("ingesting %s: %v", blk, err)
	}
	w.blocks = append(w.blocks, blk)
	return blk
}

// private seals, stores, validates, decrypts and ingests a private block. The
// node holds the key, which is what lets its payload reach L2 at all
// (spec/05-processing-model.md, "Private chains", step 2).
func (w *world) private(b *block.Builder, key privacy.Key, ops ...block.Operation) *block.Block {
	w.t.Helper()
	payload := block.Payload{TS: w.nextTS(), Ops: ops}
	blk, err := privacy.SealBlock(b, key, payload, &testKeyReader{b: 11})
	if err != nil {
		w.t.Fatalf("SealBlock: %v", err)
	}
	if err := w.store.Add(blk); err != nil {
		w.t.Fatalf("storing %s: %v", blk, err)
	}
	opened, _, err := privacy.OpenAndValidate(blk, key, w.store, nil)
	if err != nil {
		w.t.Fatalf("%s must be valid: %v", blk, err)
	}
	if err := w.graph.IngestPayload(blk, opened); err != nil {
		w.t.Fatalf("ingesting %s: %v", blk, err)
	}
	w.blocks = append(w.blocks, blk)
	w.payloads[blk.Digest()] = opened
	return blk
}

// accept stores, validates and ingests a block, in the order a node does: L2
// may only see a block whose validation succeeded
// (spec/05-processing-model.md, "Block reception").
func (w *world) accept(blk *block.Block) *block.Block {
	w.t.Helper()
	if err := w.store.Add(blk); err != nil {
		w.t.Fatalf("storing %s: %v", blk, err)
	}
	if _, err := block.Validate(blk, w.store, nil); err != nil {
		w.t.Fatalf("%s must be valid: %v", blk, err)
	}
	if err := w.graph.Ingest(blk); err != nil {
		w.t.Fatalf("ingesting %s: %v", blk, err)
	}
	w.blocks = append(w.blocks, blk)
	return blk
}

// key returns a deterministic content key for a private chain.
func (w *world) key(seed byte) privacy.Key {
	w.t.Helper()
	k, err := privacy.GenerateKey(&testKeyReader{b: seed})
	if err != nil {
		w.t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

// view builds the L3 view for a set of subscribed authors.
func (w *world) view(pubs ...ed25519.PublicKey) *View {
	w.t.Helper()
	v, err := Build(w.graph, w.store, NewSubscriptions(pubs...))
	if err != nil {
		w.t.Fatalf("Build: %v", err)
	}
	return v
}

// rebuild replays the world's blocks into a fresh graph in the given order and
// builds a view from it, which is how the determinism test shuffles ingestion.
func (w *world) rebuild(order []int, pubs ...ed25519.PublicKey) *View {
	w.t.Helper()
	g := graph.New()
	for _, i := range order {
		b := w.blocks[i]
		if p, ok := w.payloads[b.Digest()]; ok {
			if err := g.IngestPayload(b, p); err != nil {
				w.t.Fatalf("ingesting private block %s: %v", b, err)
			}
			continue
		}
		if err := g.Ingest(b); err != nil {
			w.t.Fatalf("ingesting %s: %v", b, err)
		}
	}
	v, err := Build(g, w.store, NewSubscriptions(pubs...))
	if err != nil {
		w.t.Fatalf("Build: %v", err)
	}
	return v
}

// The subject matter every test states things about: two atoms, a bond, and the
// molecule that joins them.
type subject struct {
	paris, france block.CreateAtom
	capital       block.CreateBond
	molecule      block.CreateMolecule
}

func newSubject() subject {
	s := subject{
		paris:   block.MustCreateAtom("Paris, the capital of France"),
		france:  block.MustCreateAtom("France"),
		capital: block.MustCreateBond("_A_ is the capital of _B_"),
	}
	s.molecule = block.MustCreateMolecule(s.capital.Bond(), []entity.Filler{
		entity.AtomFiller(s.paris.Atom().Digest()),
		entity.AtomFiller(s.france.Atom().Digest()),
	})
	return s
}

// ops returns the operations that publish the whole subject.
func (s subject) ops() []block.Operation {
	return []block.Operation{s.paris, s.france, s.capital, s.molecule}
}

func (s subject) digest() cid.Digest { return s.molecule.Molecule().Digest() }

// The five standard meta-bonds as operations, so that a test can publish one
// alongside the meta-molecule that uses it.
func metaBondOp(template string) block.CreateBond { return block.MustCreateBond(template) }

// isTrue returns the "_A_ is true" meta-molecule about d
// (spec/06-meta-bonds.md, "Truth assertion").
func isTrue(d cid.Digest) block.CreateMolecule {
	return block.MustCreateMolecule(entity.MetaBondTruthAssertion, []entity.Filler{entity.MoleculeFiller(d)})
}

// isUntrue returns the "_A_ is untrue" meta-molecule about d.
func isUntrue(d cid.Digest) block.CreateMolecule {
	return block.MustCreateMolecule(entity.MetaBondTruthRetraction, []entity.Filler{entity.MoleculeFiller(d)})
}

// sameAs returns the "_A_ is the same as _B_" meta-molecule over two entities
// of one type.
func sameAs(t entity.FillerType, a, b cid.Digest) block.CreateMolecule {
	fa, err := entity.RefFiller(t, a)
	if err != nil {
		panic(err)
	}
	fb, err := entity.RefFiller(t, b)
	if err != nil {
		panic(err)
	}
	return block.MustCreateMolecule(entity.MetaBondEquivalence, []entity.Filler{fa, fb})
}

// contradicts returns the "_A_ contradicts _B_" meta-molecule.
func contradicts(a, b cid.Digest) block.CreateMolecule {
	return block.MustCreateMolecule(entity.MetaBondContradiction, []entity.Filler{
		entity.MoleculeFiller(a), entity.MoleculeFiller(b),
	})
}

// supersedes returns the "_A_ supersedes _B_" meta-molecule.
func supersedes(a, b cid.Digest) block.CreateMolecule {
	return block.MustCreateMolecule(entity.MetaBondSupersession, []entity.Filler{
		entity.MoleculeFiller(a), entity.MoleculeFiller(b),
	})
}

// statement returns a molecule stating that atom a is the capital of atom b,
// with the operations that create all three.
func statement(bond block.CreateBond, a, b string) (block.CreateMolecule, []block.Operation) {
	atomA, atomB := block.MustCreateAtom(a), block.MustCreateAtom(b)
	m := block.MustCreateMolecule(bond.Bond(), []entity.Filler{
		entity.AtomFiller(atomA.Atom().Digest()),
		entity.AtomFiller(atomB.Atom().Digest()),
	})
	return m, []block.Operation{atomA, atomB, m}
}

// digests renders a digest slice for a failure message.
func digests(ds []cid.Digest) string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.String()
	}
	return fmt.Sprint(out)
}
