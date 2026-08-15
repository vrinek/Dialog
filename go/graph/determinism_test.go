package graph

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
)

// fingerprint renders everything a Graph can be asked, in the order it answers,
// as one string. Two graphs holding the same data must produce the same
// fingerprint whatever order the blocks reached them in: the order of a returned
// slice is part of this package's contract, and Go randomises map iteration
// precisely so that code depending on it fails visibly.
func fingerprint(g *Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "entities=%d blocks=%d\n", g.Len(), g.BlockCount())
	for _, e := range g.Entries() {
		fmt.Fprintf(&b, "entity %s %s %x\n", e.Kind(), e.Digest(), e.Entity().Bytes())
		for _, a := range e.Authors() {
			fmt.Fprintf(&b, "  by %x in %s\n", a.Author, a.Block)
		}
		for _, a := range g.Provenance(e.Digest()) {
			fmt.Fprintf(&b, "  provenance %x %s\n", a.Author, a.Block)
		}
	}
	for _, k := range []block.EntityKind{block.KindAtom, block.KindBond, block.KindMolecule} {
		fmt.Fprintf(&b, "kind %s\n", k)
		for _, e := range g.EntriesOfKind(k) {
			fmt.Fprintf(&b, "  %s\n", e.Digest())
		}
	}
	for _, pub := range g.Authors() {
		fmt.Fprintf(&b, "author %x\n", pub)
		for _, e := range g.EntriesByAuthor(pub) {
			fmt.Fprintf(&b, "  %s\n", e.Digest())
		}
	}
	for _, d := range g.Blocks() {
		fmt.Fprintf(&b, "block %s\n", d)
	}
	return b.String()
}

// TestDeterministicQueryOrder rebuilds the same graph a hundred times from the
// same blocks in a hundred different ingestion orders, and requires every query
// to answer identically every time.
//
// The ingestion orders would be invalid at L1 — a block arriving before its
// predecessor cannot be validated there — but the scenario's blocks were all
// validated when they were built, and L2 accumulation is order-independent by
// construction: an entity is keyed by its digest and an authorship record is a
// set member, neither of which depends on when it arrived.
func TestDeterministicQueryOrder(t *testing.T) {
	s := buildScenario(t)

	first := New()
	s.ingestAll(t, first, s.order())
	want := fingerprint(first)
	if want == "" {
		t.Fatal("the scenario produced an empty graph")
	}

	rng := rand.New(rand.NewPCG(0x1a2b3c4d, 0x5e6f7a8b))
	for i := range 100 {
		order := s.order()
		rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })

		g := New()
		s.ingestAll(t, g, order)
		if got := fingerprint(g); got != want {
			t.Fatalf("rebuild %d in order %v produced a different graph:\n got %s\nwant %s", i, order, got, want)
		}
	}
}

// TestQueryOrderIsAscending pins the order itself, not just its stability:
// entities by digest, blocks by digest, authors by key, and authorship records
// by author and then by block.
func TestQueryOrderIsAscending(t *testing.T) {
	s := buildScenario(t)
	g := New()
	s.ingestAll(t, g, s.order())

	entries := g.Entries()
	for i := 1; i < len(entries); i++ {
		if a, b := entries[i-1].Digest(), entries[i].Digest(); bytes.Compare(a[:], b[:]) >= 0 {
			t.Errorf("Entries is not ascending at %d: %s then %s", i, a, b)
		}
	}
	for _, k := range []block.EntityKind{block.KindAtom, block.KindBond, block.KindMolecule} {
		of := g.EntriesOfKind(k)
		for i := 1; i < len(of); i++ {
			if a, b := of[i-1].Digest(), of[i].Digest(); bytes.Compare(a[:], b[:]) >= 0 {
				t.Errorf("EntriesOfKind(%s) is not ascending at %d: %s then %s", k, i, a, b)
			}
		}
	}
	blocks := g.Blocks()
	for i := 1; i < len(blocks); i++ {
		if bytes.Compare(blocks[i-1][:], blocks[i][:]) >= 0 {
			t.Errorf("Blocks is not ascending at %d: %s then %s", i, blocks[i-1], blocks[i])
		}
	}
	authors := g.Authors()
	for i := 1; i < len(authors); i++ {
		if bytes.Compare(authors[i-1], authors[i]) >= 0 {
			t.Errorf("Authors is not ascending at %d: %x then %x", i, authors[i-1], authors[i])
		}
	}
	for _, pub := range authors {
		byAuthor := g.EntriesByAuthor(pub)
		for i := 1; i < len(byAuthor); i++ {
			if a, b := byAuthor[i-1].Digest(), byAuthor[i].Digest(); bytes.Compare(a[:], b[:]) >= 0 {
				t.Errorf("EntriesByAuthor(%x) is not ascending at %d: %s then %s", pub[:8], i, a, b)
			}
		}
	}
	// France carries four records; they must be ordered by author, then block.
	records := g.Provenance(s.france)
	if len(records) < 2 {
		t.Fatalf("France carries %d authorship records, want several", len(records))
	}
	for i := 1; i < len(records); i++ {
		prev, cur := records[i-1], records[i]
		c := bytes.Compare(prev.Author, cur.Author)
		if c > 0 || (c == 0 && bytes.Compare(prev.Block[:], cur.Block[:]) >= 0) {
			t.Errorf("Provenance is not ascending at %d: %v then %v", i, prev, cur)
		}
	}
}
