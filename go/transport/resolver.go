package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A Resolver is a [block.Source] that reads a local store first and falls back
// to the transport for what the store does not hold.
//
// It is the adapter that lets demand-driven resolution reach across a network:
// spec/05-processing-model.md's resolution procedure asks a source for a block
// by digest, spec/07-transport.md's `block` and `blocks` operations answer
// exactly that question, and this joins them. Pass it to [block.Validate] or
// [block.ValidateChain] in place of a store and rule 4 will fetch what it needs.
//
// # A failed fetch is not an invalidity
//
// Every failure — a 404, a timeout, a refused connection, a body that hashed to
// something else — is reported as [block.ErrNotFound]. That is not a loss of
// information but the profile's rule made mechanical: a block whose refs a
// client cannot obtain has not been shown invalid, the client has simply not
// been able to decide, and the verdict is *stored but unvalidated*
// (spec/07-transport.md, "Interaction with the scan limit", point 3). Reporting
// anything else would let a source that withholds one foreign block invalidate a
// valid block at every client, which is precisely the attack the rule exists to
// close. What the failures were is kept in [Resolver.Failures] for a caller that
// wants to retry or to complain.
//
// It follows that a failed fetch costs no unit of the scan limit: the limit
// counts distinct foreign blocks *scanned* — fetched and read for the
// definitions they carry — and the validator does not count a fetch that
// returned ErrNotFound.
//
// # The context
//
// [block.Source] takes no context, because it is the protocol's interface and
// the protocol has no network in it. A Resolver therefore carries the context
// its fetches run under, which is why one is built per validation rather than
// held for the life of a node. Use [Resolver.WithContext] for the next one.
type Resolver struct {
	local   block.Source
	sources []Source
	ctx     context.Context

	mu      sync.Mutex
	fetched map[cid.Digest]*block.Block
	failed  map[cid.Digest]error
	order   []ResolveFailure
}

// A ResolveFailure records one digest this resolver could not obtain, and why.
// The error is a diagnostic: validation has already treated the digest as
// unavailable, which is not the same as absent from the world.
type ResolveFailure struct {
	Digest cid.Digest
	Err    error
}

// NewResolver returns a resolver over a local store and any number of sources,
// tried in order.
//
// A nil local store is allowed: the resolver is then a pure transport source,
// which is occasionally what a one-off validation wants. Passing no sources
// makes it exactly the local store, which is how a caller turns fetching off
// without changing the call.
func NewResolver(ctx context.Context, local block.Source, sources ...Source) *Resolver {
	return &Resolver{
		local:   local,
		sources: sources,
		ctx:     ctx,
		fetched: make(map[cid.Digest]*block.Block),
		failed:  make(map[cid.Digest]error),
	}
}

// WithContext returns a resolver over the same local store and sources, running
// its fetches under another context and starting with an empty cache.
func (r *Resolver) WithContext(ctx context.Context) *Resolver {
	return NewResolver(ctx, r.local, r.sources...)
}

// Block implements [block.Source].
//
// The local store answers first, then each source in order. A block a source
// hands over is re-hashed by the [Client] that fetched it, so a block that
// arrives here is one whose bytes hash to the digest that was asked for; a
// source that sent anything else has not answered.
//
// Both outcomes are cached for the life of the resolver. A digest that could not
// be obtained is not asked for again during one validation, which keeps a block
// naming an unobtainable digest from costing one round trip per mention of it.
func (r *Resolver) Block(d cid.Digest) (*block.Block, error) {
	if r.local != nil {
		if b, err := r.local.Block(d); err == nil {
			return b, nil
		} else if !errors.Is(err, block.ErrNotFound) {
			return nil, err
		}
	}
	if hit, ok := r.cached(d); ok {
		return hit.block, hit.err
	}
	if err := r.ctx.Err(); err != nil {
		return nil, r.fail(d, err)
	}
	var last error
	for _, src := range r.sources {
		b, err := src.Block(r.ctx, d)
		if err == nil {
			r.keep(d, b)
			return b, nil
		}
		last = err
	}
	if last == nil {
		last = ErrNotHeld
	}
	return nil, r.fail(d, last)
}

// Prefetch obtains several digests in one exchange and caches them, which is
// what a caller does before validating a block whose refs it can read: the scan
// limit's default of 256 is a count of blocks, not of round trips, and it is
// only affordable as one exchange (spec/07-transport.md, "Interaction with the
// scan limit", point 1).
//
// It reports nothing. A digest no source returned is left to Block, which will
// report it as not held at the moment resolution actually needs it — and only if
// it does, since a refs entry no digest needed is never fetched at all.
func (r *Resolver) Prefetch(digests []cid.Digest) {
	var missing []cid.Digest
	for _, d := range digests {
		if !r.held(d) {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 || r.ctx.Err() != nil {
		return
	}
	for _, src := range r.sources {
		blocks, err := src.Blocks(r.ctx, missing)
		if err != nil {
			continue
		}
		for _, b := range blocks {
			r.keep(b.Digest(), b)
		}
		missing = r.stillMissing(missing)
		if len(missing) == 0 {
			return
		}
	}
}

// Failures returns the digests this resolver could not obtain, in the order it
// gave up on them.
func (r *Resolver) Failures() []ResolveFailure {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ResolveFailure(nil), r.order...)
}

// A cacheHit is one decided digest: the block, or the error that stands for the
// fetch that did not succeed.
type cacheHit struct {
	block *block.Block
	err   error
}

func (r *Resolver) cached(d cid.Digest) (cacheHit, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.fetched[d]; ok {
		return cacheHit{block: b}, true
	}
	if err, ok := r.failed[d]; ok {
		return cacheHit{err: err}, true
	}
	return cacheHit{}, false
}

// held reports whether the digest needs fetching at all: the local store has it,
// or this resolver has already decided about it.
func (r *Resolver) held(d cid.Digest) bool {
	if r.local != nil {
		if _, err := r.local.Block(d); err == nil {
			return true
		}
	}
	_, ok := r.cached(d)
	return ok
}

func (r *Resolver) keep(d cid.Digest, b *block.Block) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fetched[d] = b
	delete(r.failed, d)
}

// fail records a failed fetch and returns the error validation will see: one
// that wraps block.ErrNotFound, whatever the cause was.
func (r *Resolver) fail(d cid.Digest, cause error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Both are wrapped: ErrNotFound is what validation branches on — a fetch
	// that did not succeed, never an invalidity — and the cause is what a
	// person debugging the sync needs.
	err := fmt.Errorf("%w: %s could not be fetched: %w", block.ErrNotFound, d, cause)
	if _, already := r.failed[d]; !already {
		r.order = append(r.order, ResolveFailure{Digest: d, Err: cause})
	}
	r.failed[d] = err
	return err
}

func (r *Resolver) stillMissing(digests []cid.Digest) []cid.Digest {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []cid.Digest
	for _, d := range digests {
		if _, ok := r.fetched[d]; !ok {
			out = append(out, d)
		}
	}
	return out
}
