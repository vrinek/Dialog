// Package chains reads a directory of block files and answers the constructive
// chain questions of spec/07-transport.md over a store.
//
// It exists for the two command-line binaries — dialog-serve and dialog-sync —
// and for the interop harness's fixture generator, which are the three places
// that need "a chain on disk" and "the end of the forward walk" without needing
// the transport's HTTP surface. It is internal because neither is protocol: a
// directory of .block files is a convention of this repository (see
// demo/internal/chainfile), and the walk is a restatement of what the profile
// already defines and the server already performs.
package chains

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// Extension is the suffix of a file holding one block.
//
// A .block file holds exactly the bytes block.Block.Bytes returns — the bytes
// that go on the wire and the bytes the digest is taken over. There is no
// envelope, no length prefix and no framing, which is what makes a saved
// response and a hand-carried chain file the same artifact
// (spec/07-transport.md, "Block sequence").
const Extension = ".block"

// Load reads every .block file under a directory, in ascending path order, and
// decodes each one.
//
// The order is the directory's, not the chain's: a reader that needs chain order
// gets it from the blocks' own prev links, which is the only ordering that
// carries any authority. Any manifest a directory happens to carry is ignored —
// every claim such a file makes about a block is checked by decoding the block,
// so reading it would add a second source of truth and no information.
func Load(dir string) ([]*block.Block, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == Extension {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("chains: reading %s: %w", dir, err)
	}
	sort.Strings(paths)
	blocks := make([]*block.Block, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's own argument
		if err != nil {
			return nil, fmt.Errorf("chains: %w", err)
		}
		b, err := block.Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("chains: decoding %s: %w", path, err)
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// Positions is the one question a constructive walk asks: which blocks of this
// author claim this position. Both stores of the block package answer it.
type Positions interface {
	BlocksWithPrev(pub ed25519.PublicKey, prev *cid.Digest) []cid.Digest
}

// Walk returns the digests of an author's chain in chain order, from the genesis
// position onward, as the store holds it.
//
// This is the profile's own, constructive definition: the chain is the forward
// walk from the genesis position through the blocks the store holds, and the tip
// is where that walk stops. At a fork it takes the profile's reference rule —
// the lowest digest bytewise — which is deterministic, stable per author, and a
// function of the blocks alone (spec/07-transport.md, "tip"; todo 086).
//
// A store holding blocks 3, 4 and 5 of a chain whose first three it never
// received therefore walks nothing at all: the walk stops at the hole in both
// directions, which is exactly why a server that computes its tip this way
// cannot report one it is unable to serve a range to.
func Walk(p Positions, pub ed25519.PublicKey) []cid.Digest {
	var out []cid.Digest
	pos := (*cid.Digest)(nil)
	for {
		next := p.BlocksWithPrev(pub, pos)
		if len(next) == 0 {
			return out
		}
		d := slices.MinFunc(next, func(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) })
		out = append(out, d)
		pos = &d
	}
}

// Reachable returns every block of an author the store holds that is reachable
// forward from the genesis position, each position's blocks before their
// successors'.
//
// It is Walk with every branch of every fork followed instead of one, which
// makes it the set of blocks the client can say anything about: a block whose
// predecessor the store does not hold is on no chain this store can name, and is
// not in it. That is the same constructive stance the profile takes about a tip,
// applied to a whole chain rather than to its end.
//
// The order is a breadth-first one, so every block is preceded by the block it
// names as its predecessor — which is the order a store wants them offered in if
// it is to decide each one the first time it validates it.
func Reachable(p Positions, pub ed25519.PublicKey) []cid.Digest {
	var out []cid.Digest
	frontier := []*cid.Digest{nil}
	for len(frontier) > 0 {
		pos := frontier[0]
		frontier = frontier[1:]
		at := p.BlocksWithPrev(pub, pos)
		slices.SortFunc(at, func(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) })
		for _, d := range at {
			if slices.Contains(out, d) {
				continue
			}
			out = append(out, d)
			frontier = append(frontier, &d)
		}
	}
	return out
}

// All is Reachable in ascending digest order, which is how a set of blocks is
// written down where the order it was found in carries no meaning.
func All(p Positions, pub ed25519.PublicKey) []cid.Digest {
	out := Reachable(p, pub)
	slices.SortFunc(out, func(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) })
	return out
}

// Tip returns the last digest of Walk, and whether there was one.
func Tip(p Positions, pub ed25519.PublicKey) (cid.Digest, bool) {
	walk := Walk(p, pub)
	if len(walk) == 0 {
		return cid.Digest{}, false
	}
	return walk[len(walk)-1], true
}

// Authors returns the distinct author keys of a set of blocks, in ascending key
// order, so that two runs over the same blocks name them in the same order.
func Authors(blocks []*block.Block) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, b := range blocks {
		pub := b.PublicKey()
		if !slices.ContainsFunc(out, func(k ed25519.PublicKey) bool { return k.Equal(pub) }) {
			out = append(out, pub)
		}
	}
	slices.SortFunc(out, func(a, b ed25519.PublicKey) int { return bytes.Compare(a, b) })
	return out
}
