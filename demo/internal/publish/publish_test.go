package publish_test

import (
	"bytes"
	"testing"

	"github.com/vrinek/Dialog/demo/internal/content"
	"github.com/vrinek/Dialog/demo/internal/publish"
)

// TestBuildProducesTheDocumentedChains pins the chain layout the package
// documentation describes. A block moved from one place in a chain to another
// changes every digest after it, so this is also the first thing to look at
// when the committed chains stop matching.
func TestBuildProducesTheDocumentedChains(t *testing.T) {
	chains, store, err := publish.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []struct {
		author string
		ops    []int
	}{
		{content.AuthorAtlas, []int{5, 22, 11, 11, 11, 2}},
		{content.AuthorGazetteer, []int{10, 5, 3, 4}},
		{content.AuthorErrata, []int{3, 2, 2, 1}},
	}
	if len(chains) != len(want) {
		t.Fatalf("Build returned %d chains, want %d", len(chains), len(want))
	}
	blocks := 0
	for i, w := range want {
		c := chains[i]
		if c.Author != w.author {
			t.Errorf("chain %d is %s, want %s", i, c.Author, w.author)
			continue
		}
		if !bytes.Equal(c.Pub, content.PublicKey(w.author)) {
			t.Errorf("chain %s is signed by %x, want %x", c.Author, c.Pub, content.PublicKey(w.author))
		}
		if len(c.Blocks) != len(w.ops) {
			t.Errorf("chain %s has %d blocks, want %d", c.Author, len(c.Blocks), len(w.ops))
			continue
		}
		for j, b := range c.Blocks {
			if got := len(b.Ops()); got != w.ops[j] {
				t.Errorf("%s block %d carries %d operations, want %d", c.Author, j, got, w.ops[j])
			}
			if got := b.TS(); got != content.TS(blocks) {
				t.Errorf("%s block %d is stamped %d, want %d", c.Author, j, got, content.TS(blocks))
			}
			blocks++
		}
		if got, ok := c.Blocks[0].Prev(); ok {
			t.Errorf("the genesis block of %s links to %s; a genesis block's prev is null", c.Author, got)
		}
		for j := 1; j < len(c.Blocks); j++ {
			prev, ok := c.Blocks[j].Prev()
			if !ok {
				t.Errorf("%s block %d has a null prev but is not the genesis block", c.Author, j)
				continue
			}
			if prev != c.Blocks[j-1].Digest() {
				t.Errorf("%s block %d links to %s, want %s", c.Author, j, prev, c.Blocks[j-1].Digest())
			}
		}
	}
	if store.Len() != blocks {
		t.Errorf("the store holds %d blocks, the chains hold %d", store.Len(), blocks)
	}
	if forks := store.Forks(); len(forks) > 0 {
		t.Errorf("the demo's chains fork: %v", forks)
	}
}

// TestBuildIsDeterministic is the property the committed chains rest on: the
// same content, the same keys and the same timestamps produce the same bytes,
// so a regenerated chain directory is byte-identical to the one in the
// repository.
func TestBuildIsDeterministic(t *testing.T) {
	first, _, err := publish.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	second, _, err := publish.Build()
	if err != nil {
		t.Fatalf("Build again: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("two builds produced %d and %d chains", len(first), len(second))
	}
	for i := range first {
		a, b := first[i], second[i]
		if len(a.Blocks) != len(b.Blocks) {
			t.Fatalf("chain %s: two builds produced %d and %d blocks", a.Author, len(a.Blocks), len(b.Blocks))
		}
		for j := range a.Blocks {
			if !bytes.Equal(a.Blocks[j].Bytes(), b.Blocks[j].Bytes()) {
				t.Errorf("chain %s block %d differs between two builds: %s and %s",
					a.Author, j, a.Blocks[j].Digest(), b.Blocks[j].Digest())
			}
		}
	}
}

// TestForeignReferencesOnly checks rule 10's own-chain half from the author's
// side: every refs entry names a block of another author's chain. Validation
// enforces it — Build would have failed — and this says which invariant the
// chain layout is keeping.
func TestForeignReferencesOnly(t *testing.T) {
	chains, store, err := publish.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, c := range chains {
		for i, b := range c.Blocks {
			for _, ref := range b.Refs() {
				target, err := store.Block(ref)
				if err != nil {
					t.Errorf("%s block %d references %s, which the store does not hold: %v", c.Author, i, ref, err)
					continue
				}
				if bytes.Equal(target.PublicKey(), c.Pub) {
					t.Errorf("%s block %d references %s, a block of its own chain", c.Author, i, ref)
				}
			}
		}
	}
}
