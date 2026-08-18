// Package replay loads the committed demo chains the way a Dialog node loads
// blocks it has received: it decodes them, validates every one of them against
// the ten rules of spec/02-block-format.md, accumulates the operations of the
// valid ones into an L2 graph, and builds L3 views for whatever set of authors
// a caller subscribes to.
//
//	chainfile.Read -> block.ValidateChain -> graph.Ingest -> accept.Build
//	  L1 bytes          L1 validation         L2            L3
//
// Nothing here bypasses validation. The graph package's contract is that its
// caller has already validated every block it ingests
// (spec/05-processing-model.md, "Block reception": a stored but unvalidated
// block MUST NOT be made available for L2 processing), and this is the caller
// that keeps it: a chain that fails validation is an error and none of its
// blocks reach L2.
//
// # Replay order
//
// Chains are validated in the order the index lists them, which is the order
// they were published in. That matters because reference resolution reads
// blocks out of the store: gazetteer's blocks name atlas's in refs, and errata's
// name both. Every block is in the store before validation begins, so the order
// is a matter of validating a chain's dependencies before it rather than of
// finding them at all.
//
// # Views are cheap and disposable
//
// A View is a snapshot over one subscription set, and the accept package builds
// it as a pure function of the graph and the set. Changing subscriptions means
// building another view, which is what View does — and why the demo can show
// that truth is subscription-relative simply by asking twice.
package replay

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"io/fs"
	"os"

	"github.com/vrinek/Dialog/demo/internal/chainfile"
	"github.com/vrinek/Dialog/demo/internal/render"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/graph"
)

// A Chain is one replayed author chain: the blocks, and the validation report
// the L1 layer produced for them.
type Chain struct {
	// Author is the demo name the index files the chain under.
	Author string
	// Pub is the key every block of the chain is signed with.
	Pub ed25519.PublicKey
	// Blocks are the chain's blocks, genesis first.
	Blocks []*block.Block
	// Report is what block.ValidateChain returned: warnings, detected forks,
	// and the count of foreign blocks reference resolution scanned.
	Report *block.Report
}

// Tip returns the digest of the chain's last block.
func (c Chain) Tip() cid.Digest { return c.Blocks[len(c.Blocks)-1].Digest() }

// A Node is the state a Dialog node holds for the demo's chains: the L1 block
// store, the L2 graph built from it, and the chains as they were replayed.
//
// It is read-only once Load returns. Views are built on demand and hold no
// reference to it.
type Node struct {
	// Store is L1: every block of every chain, by digest.
	Store *block.MemStore
	// Graph is L2: the accumulated, author-tagged ontology graph.
	Graph *graph.Graph
	// Chains are the replayed chains, in the index's order.
	Chains []Chain
	// Index is the manifest the chains were read from.
	Index chainfile.Index
}

// Load replays a chain directory into a node.
//
// The file system is the chain directory itself — the one holding index.json —
// so demo/chains's embedded FS and os.DirFS of a checkout are both valid
// arguments. Every block is decoded, checked against the index, stored,
// validated and ingested; anything that fails is an error, and a partially
// loaded node is never returned.
func Load(fsys fs.FS) (*Node, error) {
	index, chains, err := chainfile.Read(fsys)
	if err != nil {
		return nil, err
	}
	n := &Node{Store: block.NewMemStore(), Graph: graph.New(), Index: index}

	// L1, step 1: store what arrived. A block is stored before it is
	// validated — validation reads the store for the block's predecessor and
	// for the blocks its refs name — and storing is where a chain fork would
	// be noticed.
	for _, c := range chains {
		for _, b := range c.Blocks {
			if err := n.Store.Add(b); err != nil {
				return nil, fmt.Errorf("replay: storing %s block %s: %w", c.Author, b.Digest(), err)
			}
		}
	}

	// L1, step 2: validate each chain from its genesis block forward, and L2:
	// ingest the blocks of a chain that validated.
	for _, c := range chains {
		if len(c.Blocks) == 0 {
			return nil, fmt.Errorf("replay: chain %s has no blocks", c.Author)
		}
		tip := c.Blocks[len(c.Blocks)-1].Digest()
		validated, err := block.ValidateChain(tip, n.Store, nil)
		if err != nil {
			return nil, fmt.Errorf("replay: validating the %s chain: %w", c.Author, err)
		}
		if err := checkChain(c, validated); err != nil {
			return nil, err
		}
		for _, b := range validated.Blocks {
			if err := n.Graph.Ingest(b); err != nil {
				return nil, fmt.Errorf("replay: ingesting %s block %s: %w", c.Author, b.Digest(), err)
			}
		}
		n.Chains = append(n.Chains, Chain{
			Author: c.Author, Pub: c.Pub, Blocks: validated.Blocks, Report: validated.Report,
		})
	}
	return n, nil
}

// LoadDir replays the chain directory at path.
func LoadDir(path string) (*Node, error) { return Load(os.DirFS(path)) }

// checkChain compares the chain L1 validated against the one the index
// described. The index is a convenience and carries no authority (see the
// chainfile package), so the blocks validation walked — which it found by
// following prev from the tip — must be exactly the blocks the index listed,
// in the same order, or the directory is describing something other than what
// it holds.
func checkChain(want chainfile.Chain, got *block.Chain) error {
	if len(got.Blocks) != len(want.Blocks) {
		return fmt.Errorf("replay: the %s chain validates to %d blocks, the index lists %d",
			want.Author, len(got.Blocks), len(want.Blocks))
	}
	for i, b := range got.Blocks {
		if b.Digest() != want.Blocks[i].Digest() {
			return fmt.Errorf("replay: the %s chain's block %d is %s, the index lists %s",
				want.Author, i, b.Digest(), want.Blocks[i].Digest())
		}
	}
	if !bytes.Equal(got.Genesis().PublicKey(), want.Pub) {
		return fmt.Errorf("replay: the %s chain is signed by %s, the index says %s",
			want.Author, render.AuthorKey(got.Genesis().PublicKey()), render.AuthorKey(want.Pub))
	}
	if len(got.Report.Forks) > 0 {
		return fmt.Errorf("replay: the %s chain forks: %v", want.Author, got.Report.Forks)
	}
	if len(got.Report.Unchecked) > 0 {
		return fmt.Errorf("replay: the %s chain leaves rules %v unchecked", want.Author, got.Report.Unchecked)
	}
	if len(got.Report.Warnings) > 0 {
		return fmt.Errorf("replay: the %s chain validated with warnings: %v", want.Author, got.Report.Warnings)
	}
	return nil
}

// Authors returns the demo names of the replayed chains, in the index's order.
func (n *Node) Authors() []string {
	names := make([]string, 0, len(n.Chains))
	for _, c := range n.Chains {
		names = append(names, c.Author)
	}
	return names
}

// PublicKey returns an author's key by demo name.
func (n *Node) PublicKey(author string) (ed25519.PublicKey, bool) {
	for _, c := range n.Chains {
		if c.Author == author {
			return c.Pub, true
		}
	}
	return nil, false
}

// AuthorName returns the demo name of a public key — the reverse lookup an
// application needs to attribute a claim to a name rather than to 32 bytes.
func (n *Node) AuthorName(pub ed25519.PublicKey) (string, bool) {
	for _, c := range n.Chains {
		if bytes.Equal(c.Pub, pub) {
			return c.Author, true
		}
	}
	return "", false
}

// Chain returns a replayed chain by author name.
func (n *Node) Chain(author string) (Chain, bool) {
	for _, c := range n.Chains {
		if c.Author == author {
			return c, true
		}
	}
	return Chain{}, false
}

// Subscriptions builds a subscription set from author names. An unknown name
// is an error rather than an empty subscription: a user who subscribes to a
// misspelled author would otherwise get a view that silently lacks them.
func (n *Node) Subscriptions(authors ...string) (*accept.Subscriptions, error) {
	subs := accept.NewSubscriptions()
	for _, a := range authors {
		pub, ok := n.PublicKey(a)
		if !ok {
			return nil, fmt.Errorf("replay: no chain for author %q; the demo has %v", a, n.Authors())
		}
		subs.Subscribe(pub)
	}
	return subs, nil
}

// View builds the L3 view for a set of subscribed authors, named by their demo
// names. Subscribing to nobody yields an empty view, which is the honest answer
// and not an error: an entity reaches L3 because an author was subscribed to.
func (n *Node) View(authors ...string) (*accept.View, error) {
	subs, err := n.Subscriptions(authors...)
	if err != nil {
		return nil, err
	}
	v, err := accept.Build(n.Graph, n.Store, subs)
	if err != nil {
		return nil, fmt.Errorf("replay: building the view for %v: %w", authors, err)
	}
	return v, nil
}

// BlockCount returns the number of blocks replayed.
func (n *Node) BlockCount() int { return n.Store.Len() }
