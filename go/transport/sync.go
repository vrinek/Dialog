package transport

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A Syncer obtains chains over the transport and validates every block on
// receipt into a [block.ValidatingStore].
//
// It implements the client rules of spec/07-transport.md as behaviour rather
// than as advice:
//
//   - Every block is validated per spec/02-block-format.md before it is stored
//     or its operations reach L2, with no step removed because the bytes arrived
//     over a network. The store does that; the syncer's job is to offer blocks
//     to it in an order that gives it a chance.
//   - A block's refs are resolved by one batch fetch and not one request per
//     digest, because the scan limit's default of 256 is a count of blocks and
//     not of round trips.
//   - A fetch that fails is not an invalidity. A block whose refs the syncer
//     could not obtain lands *stored but unvalidated*, and a later source that
//     supplies the missing block settles it — which is the whole reason the
//     store revalidates on arrival.
//   - Each chain is obtained from every configured source, and the union is
//     what reveals a fork. This is the multi-source rule, and it is the part of
//     the profile that does the most work.
type Syncer struct {
	// Store is where blocks land. It is the node's L1.
	Store *block.ValidatingStore
	// Sources are the places to obtain chains from, in the order they are
	// consulted. More than one is the profile's SHOULD, and the reason is not
	// redundancy but detection: fork detection is a reachability property, not
	// a query, and a node that only ever hears one version of a chain from one
	// source satisfies validation rule 9 vacuously and forever
	// (spec/07-transport.md, "The multi-source rule").
	Sources []Source
	// PageLimit is the limit asked of each range request. Zero lets each source
	// choose, which is the profile's default.
	PageLimit int
	// MaxPages bounds how many range requests one chain sync will issue against
	// one source. Zero means DefaultMaxPages. It is a client-side resource
	// bound, not a protocol rule: a source can always claim a longer chain than
	// a client is willing to read.
	MaxPages int
	// Resolve, when true, fetches the foreign blocks a received block's refs
	// name and the store does not hold, before offering that block. It is on in
	// a syncer built by NewSyncer.
	Resolve bool

	// resume remembers, per source and author, the position that source's next
	// range should ask after — the last block it handed over. It is what keeps a
	// second sync from re-reading a chain from its genesis block, and it is per
	// source because two sources may be on different branches of a fork and a
	// position one holds may be one the other has never heard of.
	resume []map[string]cid.Digest
}

// DefaultMaxPages bounds a single chain sync from a single source.
const DefaultMaxPages = 1024

// NewSyncer returns a syncer over a store and a set of sources, with
// demand-driven resolution on.
func NewSyncer(store *block.ValidatingStore, sources ...Source) *Syncer {
	return &Syncer{Store: store, Sources: sources, Resolve: true}
}

// A ChainSync is what one call to [Syncer.SyncChain] did.
type ChainSync struct {
	// Pub is the author whose chain was synced.
	Pub ed25519.PublicKey
	// Sources is one report per source consulted, in the syncer's order.
	Sources []SourceSync
	// Received are the digests of the blocks that arrived, in arrival order,
	// counting each block once however many sources sent it.
	Received []cid.Digest
	// Accepted are the received blocks the store validated and accepted.
	Accepted []cid.Digest
	// Unvalidated are the received blocks the store holds without having decided
	// about them: their ancestry, or a block their refs name, has not arrived.
	// They are neither accepted nor refused, and another source may settle them.
	Unvalidated []cid.Digest
	// Forks are the forks the store holds in this author's chain after the sync.
	//
	// A fork here is the multi-source rule paying off: two sources serving
	// different branches produce two blocks at one position in one store, which
	// is exactly the condition validation rule 9 names — and neither source had
	// to admit to anything (spec/02-block-format.md, "Validation" rule 9).
	Forks []block.Fork
}

// A SourceSync is what one source contributed to a chain sync.
type SourceSync struct {
	// Source names the source, as it names itself.
	Source string
	// Tip is the tip that source claimed, or nil if it claimed none. Two
	// sources claiming different tips means one is behind, or they are on
	// different branches, or one is withholding — and the first and the third
	// are indistinguishable (spec/07-transport.md, "What a server does not
	// guarantee"; todo 075).
	Tip *cid.Digest
	// Blocks is how many blocks this source handed over that the sync had not
	// already received.
	Blocks int
	// Err is why this source contributed nothing, or nil.
	//
	// A source that fails is not a failure of the sync. That is the point of
	// having more than one: a 404, a timeout or a refusal is a fact about that
	// source, and the next one is asked the same question.
	Err error
}

// SyncChain obtains one author's chain from every configured source and
// validates every block into the store.
//
// It returns an error only when no source could be asked at all — an empty
// source list, or a cancelled context. A source that answered badly is reported
// in its own SourceSync.Err and does not stop the others, because a client that
// gave up on the first failure would have no use for a second source.
func (s *Syncer) SyncChain(ctx context.Context, pub ed25519.PublicKey) (*ChainSync, error) {
	if len(s.Sources) == 0 {
		return nil, errors.New("transport: the syncer has no sources")
	}
	if s.Store == nil {
		return nil, errors.New("transport: the syncer has no store")
	}
	result := &ChainSync{Pub: pub}
	seen := make(map[cid.Digest]bool)
	for i, src := range s.Sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report := SourceSync{Source: fmt.Sprint(src)}
		received, tip, err := s.syncFrom(ctx, i, src, pub)
		report.Tip, report.Err = tip, err
		for _, d := range received {
			if seen[d] {
				continue
			}
			seen[d] = true
			report.Blocks++
			result.Received = append(result.Received, d)
		}
		result.Sources = append(result.Sources, report)
	}

	// The sibling query at every position the store now holds more than one
	// block at: a source that serves one branch may still answer honestly about
	// the other, and a third branch nobody's range revealed shows up here.
	if err := s.querySiblings(ctx, pub, seen, result); err != nil {
		return nil, err
	}

	for _, d := range result.Received {
		switch verdict, _ := s.Store.Verdict(d); verdict {
		case block.VerdictValid:
			result.Accepted = append(result.Accepted, d)
		case block.VerdictUnvalidated:
			result.Unvalidated = append(result.Unvalidated, d)
		case block.VerdictUnknown:
			// The store does not hold it, which happens only if it was rejected
			// on arrival. It arrived, so it stays in Received, and it belongs to
			// neither of the other two lists.
		}
	}
	for _, fork := range s.Store.Forks() {
		if fork.Pub.Equal(pub) {
			result.Forks = append(result.Forks, fork)
		}
	}
	return result, nil
}

// SyncChainFrom obtains one author's chain from one source. It is SyncChain
// against a single source, and it is what a node does when it has one — with the
// standing consequence that a fork it is not shown is a fork it will never see.
func (s *Syncer) SyncChainFrom(ctx context.Context, src Source, pub ed25519.PublicKey) (*ChainSync, error) {
	single := &Syncer{Store: s.Store, Sources: []Source{src}, PageLimit: s.PageLimit, MaxPages: s.MaxPages, Resolve: s.Resolve}
	return single.SyncChain(ctx, pub)
}

// syncFrom walks one source's copy of one chain: tip first, so that the client
// knows whether there is anything to fetch, then ranges until the last block
// received is the tip the source claims or the source stops handing blocks over.
func (s *Syncer) syncFrom(ctx context.Context, index int, src Source, pub ed25519.PublicKey) ([]cid.Digest, *cid.Digest, error) {
	var received []cid.Digest

	tip, tipErr := src.Tip(ctx, pub, "")
	var claimed *cid.Digest
	if tipErr == nil && tip.Block != nil {
		d := tip.Block.Digest()
		claimed = &d
	}
	if tipErr != nil && !errors.Is(tipErr, ErrNotHeld) {
		return nil, nil, tipErr
	}
	if claimed == nil {
		// The source holds nothing for this author. That is a fact about the
		// source and not about the chain, so it is not an error worth stopping
		// for, and there is nothing to range for either.
		return nil, nil, tipErr
	}

	after := s.resumeAt(index, pub)
	maxPages := s.MaxPages
	if maxPages == 0 {
		maxPages = DefaultMaxPages
	}
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return received, claimed, err
		}
		result, err := src.Range(ctx, pub, after, s.PageLimit)
		if err != nil {
			return received, claimed, err
		}
		if len(result.Blocks) == 0 {
			// An empty range is the answer both when the client is already at
			// the tip and when the source's store stops there. The two are told
			// apart by the tip, not by the emptiness — and a client that
			// asked again would get the same empty answer forever.
			break
		}
		for _, b := range result.Blocks {
			if err := s.offer(ctx, src, b); err != nil {
				return received, claimed, err
			}
			received = append(received, b.Digest())
		}
		last, _ := result.Last()
		after = &last
		s.setResume(index, pub, last)
		if last == *claimed {
			break
		}
	}
	return received, claimed, nil
}

// offer resolves what a block's refs need and then hands the block to the store,
// which validates it.
//
// The order is the affordable one. Resolving first means a block whose foreign
// definitions are obtainable is decided the first time it is validated;
// resolving after would leave it undecided and rely on the store re-offering it,
// which works but validates twice. A digest no source returned is simply not
// fetched, the block lands undecided, and neither the failed fetch nor the
// undecided verdict costs a unit of the scan limit
// (spec/07-transport.md, "Interaction with the scan limit").
func (s *Syncer) offer(ctx context.Context, src Source, b *block.Block) error {
	if s.Resolve {
		if err := s.resolveRefs(ctx, src, b); err != nil {
			return err
		}
	}
	if _, err := s.Store.Add(b); err != nil {
		// An invalid block is not a transport failure and does not stop the
		// sync: the source handed over something the protocol rejects, which is
		// a fact about the block. It is dropped, and the blocks after it in the
		// chain will fail rule 3 and be held.
		return nil
	}
	return nil
}

// resolveRefs fetches the blocks a block's refs name that the store does not
// hold, in one request, and offers them to the store.
//
// Every fetched block is validated on arrival like any other; a foreign block
// whose own chain this node does not follow is normally held as stored but
// unvalidated, and that is enough for the block that named it, because
// resolution reads blocks and not verdicts (spec/05-processing-model.md,
// "Resolution procedure").
func (s *Syncer) resolveRefs(ctx context.Context, src Source, b *block.Block) error {
	var missing []cid.Digest
	for _, ref := range b.Refs() {
		if !s.Store.Has(ref) && !slices.Contains(missing, ref) {
			missing = append(missing, ref)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	fetched, err := src.Blocks(ctx, missing)
	if err != nil {
		// A failed fetch is not an invalidity, and it is not a reason to stop
		// syncing. The block that named the digest will be held.
		return nil
	}
	for _, foreign := range fetched {
		if _, err := s.Store.Add(foreign); err != nil {
			continue
		}
	}
	return nil
}

// querySiblings asks every source about every position the store holds more than
// one block at for this author, and about the genesis position, which is where
// an ambiguous succession would show.
func (s *Syncer) querySiblings(ctx context.Context, pub ed25519.PublicKey, seen map[cid.Digest]bool, result *ChainSync) error {
	positions := []*cid.Digest{nil}
	for _, fork := range s.Store.Forks() {
		if fork.Pub.Equal(pub) {
			positions = append(positions, fork.Prev)
		}
	}
	for _, prev := range positions {
		for _, src := range s.Sources {
			if err := ctx.Err(); err != nil {
				return err
			}
			blocks, err := src.Siblings(ctx, pub, prev)
			if err != nil {
				continue
			}
			for _, b := range blocks {
				d := b.Digest()
				if seen[d] || s.Store.Has(d) {
					seen[d] = true
					continue
				}
				if _, err := s.Store.Add(b); err != nil {
					continue
				}
				seen[d] = true
				result.Received = append(result.Received, d)
			}
		}
	}
	return nil
}

// resumeAt returns the position this source's next range for this author should
// ask after.
func (s *Syncer) resumeAt(index int, pub ed25519.PublicKey) *cid.Digest {
	if index >= len(s.resume) || s.resume[index] == nil {
		return nil
	}
	d, ok := s.resume[index][string(pub)]
	if !ok {
		return nil
	}
	return &d
}

func (s *Syncer) setResume(index int, pub ed25519.PublicKey, d cid.Digest) {
	for len(s.resume) <= index {
		s.resume = append(s.resume, nil)
	}
	if s.resume[index] == nil {
		s.resume[index] = make(map[string]cid.Digest)
	}
	s.resume[index][string(pub)] = d
}

// Poll asks one source whether a chain has moved, using the entity tag from a
// previous answer.
//
// This is the baseline subscription of the profile: an L1 blockchain
// subscription is polling tip with If-None-Match, a 304 is a few dozen bytes,
// and it needs no server feature beyond a correct ETag. The two richer mappings
// — long polling and an event stream — are optional, and the event stream is the
// one mode that hands a single server a client's whole subscription set in one
// durable, correlated act (spec/07-transport.md, "Subscription mapping").
func Poll(ctx context.Context, src Source, pub ed25519.PublicKey, etag string) (*TipResult, error) {
	return src.Tip(ctx, pub, etag)
}
