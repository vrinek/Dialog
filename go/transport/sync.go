package transport

import (
	"bytes"
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
//   - A source whose tip the syncer does not hold after that source's range is
//     exhausted is *pursued*: the tip is fetched by digest and its prev walked
//     backward until the walk meets a block the store holds. That is what turns
//     an empty range into a fork, and it is the multi-source rule's operational
//     content rather than an optimisation (spec/07-transport.md, "Pursuing an
//     advertised tip").
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
	// MaxPursuit bounds the backward walk from a tip a source advertised and
	// this client does not hold. Zero means DefaultMaxPursuit.
	//
	// The walk MUST be bounded and the bound is the client's own: it follows a
	// chain of the source's choosing, of a length the source controls
	// (spec/07-transport.md, "Pursuing an advertised tip").
	MaxPursuit int
	// Resolve, when true, fetches the foreign blocks a received block's refs
	// name and the store does not hold, before offering that block. It is on in
	// a syncer built by NewSyncer.
	Resolve bool
	// AskFromHeldPosition makes a source this syncer has not asked before be
	// asked from the position this client's own chain already reaches, rather
	// than from the genesis position. It changes nothing for a source already
	// asked once, which is always asked from where it left off.
	//
	// The profile permits both and prefers the genesis position, which is why
	// the zero value is the one to have: a client asking a source it has not
	// synced this chain from before SHOULD ask from the genesis position and MAY
	// ask from the position it holds, and SHOULD record which it did
	// (spec/07-transport.md, "First contact with a source"). The two differ in
	// cost and in which mechanism finds a fork. Asking from the genesis position
	// re-downloads the shared prefix and delivers the divergent blocks in the
	// range itself; asking from where this client is asks for nothing it already
	// holds, and the answer to a source on another branch is the empty range and
	// the unreachable tip that "Pursuing an advertised tip" is written for — the
	// case that section calls the *normal* answer a second source gives about a
	// forked chain, which is why the pursuit is not optional in effect for a
	// client setting this. Either way rule 9 fires; only the traffic and the
	// report differ. The profile's other SHOULD is the caller's: record which of
	// the two a run used, as cmd/dialog-sync's -from does.
	AskFromHeldPosition bool

	// resume remembers, per source and author, the position that source's next
	// range should ask after — the last block it handed over. It is what keeps a
	// second sync from re-reading a chain from its genesis block, and it is per
	// source because two sources may be on different branches of a fork and a
	// position one holds may be one the other has never heard of.
	resume []map[string]cid.Digest
}

// DefaultMaxPages bounds a single chain sync from a single source.
const DefaultMaxPages = 1024

// A PursuitEnd names how the backward walk after an advertised tip ended.
//
// The profile settles on three names and forbids collapsing the middle one into
// either of the others: a walk that runs out of predecessors is not a failure,
// because every fetch succeeded and every block verified, and it is not a walk
// that met a block the client holds either (spec/07-transport.md, "Pursuing an
// advertised tip").
type PursuitEnd string

// The three ends of a pursuit, and the absence of one.
const (
	// PursuitNone is no pursuit at all: the source's tip was a block this client
	// already held, which is the usual answer.
	PursuitNone PursuitEnd = ""
	// PursuitHeld is the point of the exercise: the walk met a block this client
	// holds, and the block it arrived from and the one already held after that
	// position are two blocks with one prev from one author — validation rule 9,
	// in this client's own store.
	PursuitHeld PursuitEnd = "held"
	// PursuitGenesis is a walk that ran out of predecessors: the source serves a
	// chain sharing no block with this client's, the two genesis blocks are now
	// both in the store, and they are a fork at the genesis position in the
	// strict sense of rule 9 (spec/02-block-format.md, "Validation" rule 9 and
	// "rotate_key"). Nothing failed, and the fork is in ChainSync.Forks.
	PursuitGenesis PursuitEnd = "genesis"
	// PursuitFailed is a fetch that did not succeed, in any of its kinds: a
	// source that would not serve the block, bytes that hashed to something
	// else, or this client's own bound. SourceSync.PursuitErr carries which.
	PursuitFailed PursuitEnd = "failed"
)

// DefaultMaxPursuit bounds the backward walk from an advertised tip. It is the
// scan limit's default, for no deeper reason than that a client already spends
// that many fetches on one block's worth of resolution and this is the same
// order of expense: a divergence deeper than this is a chain the two sources
// have not shared for a long time, and one sync is not the place to close it.
const DefaultMaxPursuit = 256

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
	// Rejected are the received blocks the store refused: blocks a rule showed
	// wrong, which are not stored. A source that serves one is not one to trust
	// further, but it is not a reason to stop syncing from it either — the
	// blocks are self-authenticating, and this is the client noticing.
	Rejected []cid.Digest
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
	// Pursued is how many blocks the backward walk from this source's advertised
	// tip fetched, and is zero when no pursuit was needed — which is the usual
	// case, because the usual answer is a range that reaches the tip.
	//
	// A pursuit happens when this source's tip is a block the client does not
	// hold after the source's range is exhausted, which means the source serves
	// a chain the client's position is not on (spec/07-transport.md, "Pursuing
	// an advertised tip").
	Pursued int
	// PursuitEnd is how that walk ended, by the name the profile settles on, and
	// is PursuitNone when no pursuit was needed.
	PursuitEnd PursuitEnd
	// PursuitErr is why the backward walk stopped short of a block the client
	// holds, or nil. It is set exactly when PursuitEnd is PursuitFailed, and it
	// is the finer kind the profile permits a client to report.
	//
	// It is a failed fetch and nothing more. No verdict about any block follows
	// from it: a source advertising a tip it will not serve is indistinguishable
	// from one that lost it, and neither is evidence of a fork, of an
	// invalidity, or of a lie.
	PursuitErr error
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
		received, err := s.syncFrom(ctx, i, src, pub, &report)
		report.Err = err
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
			// The store does not hold it, which happens only if a rule showed
			// it wrong on arrival. It arrived, so it stays in Received, and the
			// verdict it got is the third list.
			result.Rejected = append(result.Rejected, d)
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
	single := &Syncer{Store: s.Store, Sources: []Source{src}, PageLimit: s.PageLimit, MaxPages: s.MaxPages, MaxPursuit: s.MaxPursuit, Resolve: s.Resolve}
	return single.SyncChain(ctx, pub)
}

// syncFrom walks one source's copy of one chain: tip first, so that the client
// knows whether there is anything to fetch, then ranges until the last block
// received is the tip the source claims or the source stops handing blocks over,
// then — if the claimed tip is still a block this client does not hold — the
// backward walk that pursues it.
func (s *Syncer) syncFrom(ctx context.Context, index int, src Source, pub ed25519.PublicKey, report *SourceSync) ([]cid.Digest, error) {
	var received []cid.Digest

	tip, tipErr := src.Tip(ctx, pub, "")
	var claimed *cid.Digest
	if tipErr == nil && tip.Block != nil {
		d := tip.Block.Digest()
		claimed = &d
	}
	report.Tip = claimed
	if tipErr != nil && !errors.Is(tipErr, ErrNotHeld) {
		return nil, tipErr
	}
	if claimed == nil {
		// The source holds nothing for this author. That is a fact about the
		// source and not about the chain, so it is not an error worth stopping
		// for, and there is nothing to range for either.
		return nil, tipErr
	}

	after := s.resumeAt(index, pub)
	if after == nil && s.AskFromHeldPosition {
		if held, ok := s.heldTip(pub); ok {
			after = &held
		}
	}
	maxPages := s.MaxPages
	if maxPages == 0 {
		maxPages = DefaultMaxPages
	}
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return received, err
		}
		result, err := src.Range(ctx, pub, after, s.PageLimit)
		if err != nil {
			return received, err
		}
		if len(result.Blocks) == 0 {
			// An empty range is the answer both when the client is already at
			// the tip and when the source's store stops there — and in a third
			// case, when this source serves a chain the client's position is
			// not on. The emptiness tells them apart from nothing; the tip
			// does, and the pursuit below is what the third case costs.
			break
		}
		for _, b := range result.Blocks {
			if err := s.offer(ctx, src, b); err != nil {
				return received, err
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

	if !s.Store.Has(*claimed) {
		pursued, end, err := s.pursue(ctx, src, pub, *claimed)
		received = append(received, pursued...)
		report.Pursued, report.PursuitEnd, report.PursuitErr = len(pursued), end, err
	}
	return received, nil
}

// pursue follows a tip a source advertised and this client does not hold, back
// along prev and one block at a time, until the walk meets a block the store
// already holds.
//
// This is the moment the multi-source rule either fires or does not. The source
// holds a tip, so its store does not simply stop; the client is not at that tip,
// so it is not caught up; and the range after the client's position was empty,
// so the source's walk does not pass through that position at all. What is left
// is that the source serves a chain this client's position is not on, and
// treating that as "no new blocks" walks away from a fork one request from
// detection — which satisfies validation rule 9 vacuously against two honest
// sources serving one branch each (spec/07-transport.md, "Pursuing an advertised
// tip"; "The multi-source rule").
//
// Reaching a held block is the point of it. The block the walk arrived from and
// the block the store already held after that position are then two blocks with
// one prev from one author, in this client's own store, which is exactly the
// condition rule 9 names — and rule 9 fires on the store rather than on the
// transport, so nothing here is a special case of fork detection.
//
// Reaching a genesis block is the other way the walk ends without failing: the
// source's chain shares no block with this client's, which is the most
// fundamental fork there is, and the two genesis blocks now in the store are a
// fork at the genesis position that rule 9 names as surely as any other. It is
// reported as PursuitGenesis and never as a failure.
//
// A walk that fails ends in fetches that did not succeed and in nothing else:
// the error is returned for the report, no verdict about any block follows from
// it, and whatever the walk did obtain is offered to the store anyway, because
// those blocks arrived and are as real as any others.
func (s *Syncer) pursue(ctx context.Context, src Source, pub ed25519.PublicKey, tip cid.Digest) ([]cid.Digest, PursuitEnd, error) {
	limit := s.MaxPursuit
	if limit == 0 {
		limit = DefaultMaxPursuit
	}
	var walked []*block.Block
	end := PursuitHeld
	target := tip
	for step := 0; !s.Store.Has(target); step++ {
		if err := ctx.Err(); err != nil {
			return s.offerWalk(ctx, src, walked), PursuitFailed, err
		}
		if step >= limit {
			// The walk MUST be bounded, and the bound is this client's own: the
			// chain being followed is the source's, and so is its length.
			return s.offerWalk(ctx, src, walked), PursuitFailed, fmt.Errorf("transport: pursuing the tip %s reached this client's bound of %d blocks without meeting one it holds", tip, limit)
		}
		b, err := src.Block(ctx, target)
		if err != nil {
			return s.offerWalk(ctx, src, walked), PursuitFailed, fmt.Errorf("transport: pursuing the tip %s: %w", tip, err)
		}
		// Identify the block by the digest computed from its bytes, never by
		// what was asked for, and never by anything the source said about it.
		if b.Digest() != target {
			return s.offerWalk(ctx, src, walked), PursuitFailed, fmt.Errorf("transport: pursuing the tip %s: the source answered %s for %s: %w", tip, b.Digest(), target, ErrNotHeld)
		}
		if !bytes.Equal(b.PublicKey(), pub) {
			// A block of another author cannot be a step of this author's
			// chain, whatever the source thinks it is answering.
			return s.offerWalk(ctx, src, walked), PursuitFailed, fmt.Errorf("transport: pursuing the tip %s: %s is signed by another author: %w", tip, b.Digest(), ErrNotHeld)
		}
		walked = append(walked, b)
		prev, ok := b.Prev()
		if !ok {
			// A genesis block, and this client holds none of its ancestry
			// because it has none. The two chains share no block at all, and
			// the two genesis blocks now in the store are the fork — the
			// sibling pair at the genesis position, which is where the ambiguous
			// succession of spec/02-block-format.md, "rotate_key", is detected
			// too.
			end = PursuitGenesis
			break
		}
		target = prev
	}
	return s.offerWalk(ctx, src, walked), end, nil
}

// offerWalk hands a backward walk's blocks to the store in chain order, which is
// the reverse of the order they were fetched in.
//
// The order is not cosmetic. Offered tip-first, every block waits for the
// predecessor that has not arrived yet and the store settles them one by one as
// the walk unwinds; offered genesis-ward first, each is decided the first time
// it is validated.
func (s *Syncer) offerWalk(ctx context.Context, src Source, walked []*block.Block) []cid.Digest {
	out := make([]cid.Digest, 0, len(walked))
	for i := len(walked) - 1; i >= 0; i-- {
		if err := s.offer(ctx, src, walked[i]); err != nil {
			break
		}
		out = append(out, walked[i].Digest())
	}
	return out
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Resolve {
		s.resolveRefs(ctx, src, b)
	}
	// An invalid block is not a transport failure and does not stop the sync:
	// the source handed over something the protocol rejects, which is a fact
	// about the block. It is not stored, it is reported in ChainSync.Rejected,
	// and the blocks after it in the chain will fail rule 3 and be held.
	_, _ = s.Store.Add(b)
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
func (s *Syncer) resolveRefs(ctx context.Context, src Source, b *block.Block) {
	var missing []cid.Digest
	for _, ref := range b.Refs() {
		if !s.Store.Has(ref) && !slices.Contains(missing, ref) {
			missing = append(missing, ref)
		}
	}
	if len(missing) == 0 {
		return
	}
	fetched, err := src.Blocks(ctx, missing)
	if err != nil {
		// A failed fetch is not an invalidity, and it is not a reason to stop
		// syncing. The block that named the digest will be held, and another
		// source may settle it later.
		return
	}
	for _, foreign := range fetched {
		// A foreign block that is itself invalid is refused by the store and
		// changes nothing: the block that named it is then held, exactly as if
		// the fetch had failed.
		_, _ = s.Store.Add(foreign)
	}
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

// heldTip returns the end of the forward walk from the genesis position through
// the blocks this client's own store holds, which is the position it is at.
//
// It is the profile's constructive definition of a tip asked of the client's own
// store rather than of a source's, and it takes the same reference rule at a
// fork — the lowest digest — so that the position a client asks from is a
// function of the blocks it holds and of nothing else
// (spec/07-transport.md, "tip"; todo 086).
func (s *Syncer) heldTip(pub ed25519.PublicKey) (cid.Digest, bool) {
	var tip cid.Digest
	var held bool
	pos := (*cid.Digest)(nil)
	for {
		at := s.Store.BlocksWithPrev(pub, pos)
		if len(at) == 0 {
			return tip, held
		}
		next := slices.MinFunc(at, func(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) })
		tip, held = next, true
		pos = &next
	}
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
