package block

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/vrinek/Dialog/go/cid"
)

// ErrNotFound is what a Source returns for a digest it does not hold. It is
// distinct from a validation failure: a block that cannot be fetched is
// unresolved, not invalid, and validation says which of the two it hit. A
// validation error carrying it means the block is stored but unvalidated; see
// IsUnvalidated.
var ErrNotFound = errors.New("block: not found")

// A Source hands out blocks by digest. It is the minimum a validator needs:
// prev linkage walks it, and reference resolution fetches through it
// (spec/05-processing-model.md, "Resolution procedure").
//
// Block returns ErrNotFound (wrapped or bare) when the digest is not held. Any
// other error is a fault in the store itself and aborts validation.
type Source interface {
	Block(d cid.Digest) (*Block, error)
}

// A Siblings source can list the blocks it holds that claim a given
// predecessor for a given author. Validate uses it for fork detection
// (spec/02-block-format.md, "Validation" rule 9), which is impossible from a
// single block: a fork is a property of a set of blocks.
//
// A Source that cannot answer the question simply does not implement this
// interface, and Validate reports that rule 9 could not be checked.
type Siblings interface {
	// BlocksWithPrev returns the digests of the stored blocks signed by pub
	// whose prev field equals prev. A nil prev means the genesis position.
	BlocksWithPrev(pub ed25519.PublicKey, prev *cid.Digest) []cid.Digest
}

// A Referrers source can list the blocks it holds whose refs name a given
// digest — the refs graph read backwards. Successors uses it to find the
// genesis blocks claiming a rotation block, which is the one question a
// forward-only walk of refs cannot answer
// (spec/02-block-format.md, "Verifiable succession").
//
// Only a public or rotation block can be listed: a private block's refs are
// inside its ciphertext, so a source that does not hold the decryption key
// cannot index them.
type Referrers interface {
	// BlocksReferencing returns the digests of the stored blocks that list d
	// in their refs.
	BlocksReferencing(d cid.Digest) []cid.Digest
}

// A Fork is two or more distinct blocks by the same author claiming the same
// predecessor — divergent histories for one chain
// (spec/02-block-format.md, "Validation" rule 9).
//
// Detection is normative; what to do about it is not. This package detects and
// reports; the caller chooses whether to reject, flag or accept-first-seen.
type Fork struct {
	// Pub is the author whose chain forked.
	Pub ed25519.PublicKey
	// Prev is the predecessor both blocks claim, or nil for two genesis
	// blocks — two blocks with a null prev are as much a fork as any other
	// pair, since both claim the same (empty) position.
	Prev *cid.Digest
	// Blocks are the digests of the conflicting blocks, in ascending order.
	Blocks []cid.Digest
}

func (f Fork) String() string {
	at := "the genesis position"
	if f.Prev != nil {
		at = "prev " + f.Prev.String()
	}
	return fmt.Sprintf("fork in the chain of %x at %s: %d blocks claim it", f.Pub[:8], at, len(f.Blocks))
}

// A ForkError reports a fork discovered while storing a block. The block has
// still been stored: MemStore's policy is accept-and-flag, and the caller
// decides what the fork means.
type ForkError struct{ Fork Fork }

func (e *ForkError) Error() string { return "block: " + e.Fork.String() }

// IsFork reports whether a and b are a fork of each other: distinct blocks,
// same author, same prev.
func IsFork(a, b *Block) bool {
	if a.Digest() == b.Digest() || !a.SameAuthor(b) {
		return false
	}
	ap, aok := a.Prev()
	bp, bok := b.Prev()
	if aok != bok {
		return false
	}
	return !aok || ap == bp
}

// A MemStore is an in-memory Source. It holds blocks by digest, indexes them
// by (author, prev) so that forks are noticed as they arrive and by referenced
// digest so that the refs graph can be read backwards, and is safe for
// concurrent use.
type MemStore struct {
	mu        sync.RWMutex
	blocks    map[cid.Digest]*Block
	position  map[string][]cid.Digest     // (pub, prev) -> blocks claiming it
	referrers map[cid.Digest][]cid.Digest // referenced digest -> blocks listing it
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{
		blocks:    make(map[cid.Digest]*Block),
		position:  make(map[string][]cid.Digest),
		referrers: make(map[cid.Digest][]cid.Digest),
	}
}

// positionKey identifies a slot in an author's chain: the author's key plus
// the predecessor a block claims. Genesis blocks share the slot with a null
// prev.
func positionKey(pub ed25519.PublicKey, prev *cid.Digest) string {
	if prev == nil {
		return string(pub) + "\x00genesis"
	}
	return string(pub) + "\x00" + string(prev[:])
}

// Add stores a block.
//
// Storing the same block twice is a no-op. If another stored block by the same
// author already claims this block's predecessor, Add stores the new block and
// returns a *ForkError describing the fork: detection is required, and the
// handling strategy belongs to the caller (spec/02-block-format.md,
// "Validation" rule 9).
func (s *MemStore) Add(b *Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := b.Digest()
	if _, ok := s.blocks[d]; ok {
		return nil
	}
	s.blocks[d] = b
	for _, ref := range b.content.Refs {
		s.referrers[ref] = append(s.referrers[ref], d)
	}

	key := positionKey(b.content.Pub, b.content.Prev)
	siblings := s.position[key]
	s.position[key] = append(siblings, d)
	if len(siblings) == 0 {
		return nil
	}
	return &ForkError{Fork: s.forkAt(b.PublicKey(), b.content.Prev, key)}
}

// AddAll stores several blocks, stopping at the first error.
func (s *MemStore) AddAll(blocks ...*Block) error {
	for _, b := range blocks {
		if err := s.Add(b); err != nil {
			return err
		}
	}
	return nil
}

// MustAdd is Add, panicking on error. It is meant for test setups where a fork
// would be a bug in the test.
func (s *MemStore) MustAdd(blocks ...*Block) {
	if err := s.AddAll(blocks...); err != nil {
		panic(err)
	}
}

// Block returns the block with the given digest, or ErrNotFound.
func (s *MemStore) Block(d cid.Digest) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blocks[d]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, d)
	}
	return b, nil
}

// Has reports whether the store holds the block.
func (s *MemStore) Has(d cid.Digest) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.blocks[d]
	return ok
}

// Len returns the number of blocks stored.
func (s *MemStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.blocks)
}

// BlocksWithPrev implements Siblings.
func (s *MemStore) BlocksWithPrev(pub ed25519.PublicKey, prev *cid.Digest) []cid.Digest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.position[positionKey(pub, prev)])
}

// BlocksReferencing implements Referrers.
func (s *MemStore) BlocksReferencing(d cid.Digest) []cid.Digest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.referrers[d])
}

// Forks returns every fork the store holds, ordered by the lowest block digest
// in each. Two stores holding the same blocks therefore report the same list in
// the same order, whatever order the blocks arrived in.
func (s *MemStore) Forks() []Fork {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var forks []Fork
	// Ranging over the index is safe here only because the result is sorted
	// below: every position is visited, and the order they are visited in
	// cannot reach the caller.
	//nolint:gocritic // the map iteration order is erased by the sort below.
	for _, digests := range s.position {
		if len(digests) < 2 {
			continue
		}
		b := s.blocks[digests[0]]
		forks = append(forks, s.forkAt(b.PublicKey(), b.content.Prev, positionKey(b.content.Pub, b.content.Prev)))
	}
	// Each fork's Blocks are sorted and no digest belongs to two positions, so
	// the lowest digest is a total order over the forks.
	sort.Slice(forks, func(i, j int) bool {
		return string(forks[i].Blocks[0][:]) < string(forks[j].Blocks[0][:])
	})
	return forks
}

// Tips returns the digests of the author's stored blocks that no other stored
// block names as its predecessor. A linear, complete chain has exactly one.
func (s *MemStore) Tips(pub ed25519.PublicKey) []cid.Digest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tips []cid.Digest
	// The candidates are collected in map order and sorted before they are
	// returned, so no caller can observe the order they were found in.
	//nolint:gocritic // the map iteration order is erased by the sort below.
	for d, b := range s.blocks {
		if !slices.Equal(b.content.Pub, pub) {
			continue
		}
		if len(s.position[positionKey(pub, &d)]) == 0 {
			tips = append(tips, d)
		}
	}
	sort.Slice(tips, func(i, j int) bool { return string(tips[i][:]) < string(tips[j][:]) })
	return tips
}

// forkAt builds the Fork record for one position. The caller holds the lock.
func (s *MemStore) forkAt(pub ed25519.PublicKey, prev *cid.Digest, key string) Fork {
	digests := slices.Clone(s.position[key])
	sort.Slice(digests, func(i, j int) bool { return string(digests[i][:]) < string(digests[j][:]) })
	f := Fork{Pub: pub, Blocks: digests}
	if prev != nil {
		p := *prev
		f.Prev = &p
	}
	return f
}
