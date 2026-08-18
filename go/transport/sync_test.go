package transport

import (
	"crypto/ed25519"
	"errors"
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// TestColdSync is the profile's own worked example in miniature: a node with an
// empty store learns a chain from one source, tip first, and validates every
// block on receipt (spec/07-transport.md, "A full sync session").
func TestColdSync(t *testing.T) {
	pub, blocks := testChain(t, 40, 5)
	source, _ := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	store := block.NewValidatingStore(nil)
	syncer := NewSyncer(store, source)
	result, err := syncer.SyncChain(t.Context(), pub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if !slices.Equal(result.Received, digests(blocks)) {
		t.Errorf("received %v, want the chain in order %v", result.Received, digests(blocks))
	}
	if len(result.Accepted) != len(blocks) {
		t.Errorf("accepted %d of %d blocks", len(result.Accepted), len(blocks))
	}
	if len(result.Forks) != 0 {
		t.Errorf("an unforked chain reported forks: %v", result.Forks)
	}
	if result.Sources[0].Tip == nil || *result.Sources[0].Tip != blocks[4].Digest() {
		t.Errorf("the source's tip claim is %v, want the chain tip", result.Sources[0].Tip)
	}

	// Syncing again asks after the last block it received and costs nothing.
	second, err := syncer.SyncChain(t.Context(), pub)
	if err != nil {
		t.Fatalf("SyncChain again: %v", err)
	}
	if len(second.Received) != 0 {
		t.Errorf("a second sync received %d blocks, want none", len(second.Received))
	}

	// The store the sync filled is a node's L1: the chain validates from it.
	if _, err := block.ValidateChain(blocks[4].Digest(), store, nil); err != nil {
		t.Errorf("the synced store does not validate the chain: %v", err)
	}
}

// TestSyncPaginates: a client continues a truncated range from the last block it
// received, which needs no cursor, no session and no server-side state.
func TestSyncPaginates(t *testing.T) {
	pub, blocks := testChain(t, 41, 9)
	source, _ := serve(t, ServerConfig{Store: memStore(t, blocks...), MaxRangeLimit: 2})

	store := block.NewValidatingStore(nil)
	result, err := NewSyncer(store, source).SyncChain(t.Context(), pub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if !slices.Equal(result.Received, digests(blocks)) {
		t.Errorf("received %v, want the whole chain in order", result.Received)
	}
}

// TestDemandDrivenResolutionOverTheTransport: a block whose refs name a block of
// a chain this node does not follow is resolved by fetching that block, in one
// request rather than one per digest (spec/07-transport.md, "Interaction with
// the scan limit").
func TestDemandDrivenResolutionOverTheTransport(t *testing.T) {
	_, definition, usePub, use := definitionAndUse(t, 42, 43)
	// One server holds both chains; the client follows only the second.
	held := append(slices.Clone(definition), use)
	source, _ := serve(t, ServerConfig{Store: memStore(t, held...)})

	store := block.NewValidatingStore(nil)
	result, err := NewSyncer(store, source).SyncChain(t.Context(), usePub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if len(result.Accepted) != 1 || result.Accepted[0] != use.Digest() {
		t.Fatalf("result = %+v, want the subscribed block accepted", result)
	}
	// The foreign block was fetched and is in the store, held rather than
	// accepted: its own predecessor was never asked for, and that is enough,
	// because resolution reads blocks and not verdicts.
	if !store.Has(definition[1].Digest()) {
		t.Fatal("the foreign block its refs named was not fetched")
	}
	if store.Accepted(definition[1].Digest()) {
		t.Error("the foreign block was accepted; nothing validated its ancestry")
	}
}

// TestUndecidedSyncSettlesFromASecondSource is the client rule that a failed
// fetch is not an invalidity, end to end: one source withholds the block a
// refs entry names, the block that named it lands stored but unvalidated, and a
// second source supplying the missing block settles it
// (spec/07-transport.md, "Interaction with the scan limit", point 3).
func TestUndecidedSyncSettlesFromASecondSource(t *testing.T) {
	_, definition, usePub, use := definitionAndUse(t, 44, 45)

	// The first source holds the using chain and not the definition.
	withholding, _ := serve(t, ServerConfig{Store: memStore(t, use)})
	store := block.NewValidatingStore(nil)
	syncer := NewSyncer(store, withholding)

	result, err := syncer.SyncChain(t.Context(), usePub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if len(result.Unvalidated) != 1 || result.Unvalidated[0] != use.Digest() {
		t.Fatalf("result = %+v, want the block held as stored but unvalidated", result)
	}
	if len(result.Accepted) != 0 {
		t.Errorf("result = %+v, want nothing accepted", result)
	}
	// It is undecided, not invalid: the client has not been shown that the
	// block is wrong, only that it could not finish deciding.
	pending := store.Pending(use.Digest())
	if pending == nil || !block.IsUnvalidated(pending) {
		t.Fatalf("pending = %v, want an undecided verdict", pending)
	}
	if awaited, ok := block.Awaiting(pending); !ok || awaited != definition[1].Digest() {
		t.Errorf("the block is waiting for %s, want the withheld block %s", awaited, definition[1].Digest())
	}

	// A second source holds it. Fetching the one block settles the verdict,
	// which is what "SHOULD retry from another source" buys.
	supplying, _ := serve(t, ServerConfig{Store: memStore(t, definition...)})
	missing, err := supplying.Block(t.Context(), definition[1].Digest())
	if err != nil {
		t.Fatalf("fetching the missing block: %v", err)
	}
	if _, err := store.Add(missing); err != nil {
		t.Fatalf("storing the missing block: %v", err)
	}
	if !store.Accepted(use.Digest()) {
		t.Error("the held block was not settled by the block it was waiting for arriving")
	}
}

// TestResolverIsABlockSource pins the adapter: block.Validate fetching over the
// transport, a failed fetch producing the undecided verdict rather than an
// invalid one, and no unit of the scan limit spent on a fetch that failed.
func TestResolverIsABlockSource(t *testing.T) {
	_, definition, _, use := definitionAndUse(t, 46, 47)

	t.Run("a fetch that succeeds decides the block", func(t *testing.T) {
		source, _ := serve(t, ServerConfig{Store: memStore(t, definition...)})
		local := block.NewMemStore()
		local.MustAdd(use)
		resolver := NewResolver(t.Context(), local, source)

		report, err := block.Validate(use, resolver, nil)
		if err != nil {
			t.Fatalf("Validate over the transport: %v", err)
		}
		if report.Scanned == 0 {
			t.Error("the foreign block was read and no scan unit was counted")
		}
		if len(resolver.Failures()) != 0 {
			t.Errorf("failures = %v, want none", resolver.Failures())
		}
	})

	t.Run("a fetch that fails leaves the verdict undecided", func(t *testing.T) {
		empty, _ := serve(t, ServerConfig{Store: block.NewMemStore()})
		local := block.NewMemStore()
		local.MustAdd(use)
		resolver := NewResolver(t.Context(), local, empty)

		report, err := block.Validate(use, resolver, nil)
		if !block.IsUnvalidated(err) {
			t.Fatalf("Validate = %v, want the undecided verdict; a failed fetch is not an invalidity", err)
		}
		if report != nil && report.Scanned != 0 {
			t.Errorf("Scanned = %d, want 0: a digest no source returned was not scanned", report.Scanned)
		}
		failures := resolver.Failures()
		if len(failures) != 1 || failures[0].Digest != definition[1].Digest() {
			t.Errorf("failures = %v, want the one digest no source held", failures)
		}
	})

	t.Run("a source with nothing behind it is the local store", func(t *testing.T) {
		local := block.NewMemStore()
		local.MustAdd(definition...)
		resolver := NewResolver(t.Context(), local)
		if _, err := resolver.Block(definition[0].Digest()); err != nil {
			t.Errorf("the local store was not consulted: %v", err)
		}
		if _, err := resolver.Block(use.Digest()); err == nil {
			t.Error("a digest nothing holds resolved")
		}
	})
}

// TestForkIsInvisibleFromOneSourceAndVisibleFromTwo is the design document's
// central claim, proven in code.
//
// Fork detection is a reachability property, not a query. Validation rule 9
// fires when a node *holds* two blocks with the same prev from the same author;
// a node that only ever hears one version of a chain from one source satisfies
// the rule vacuously and forever. Two sources with different branches produce a
// fork at the client even when neither admits to one
// (spec/07-transport.md, "The multi-source rule").
func TestForkIsInvisibleFromOneSourceAndVisibleFromTwo(t *testing.T) {
	pub, genesis, branches := forkedChain(t)
	left, _ := serve(t, ServerConfig{Store: memStore(t, genesis, branches[0])})
	right, _ := serve(t, ServerConfig{Store: memStore(t, genesis, branches[1])})

	// One source. Each server is honest about everything it holds — its tip,
	// its range and its sibling set are all consistent — and the node that asks
	// only it sees a linear chain.
	for i, only := range []Source{left, right} {
		store := block.NewValidatingStore(nil)
		result, err := NewSyncer(store, only).SyncChain(t.Context(), pub)
		if err != nil {
			t.Fatalf("source %d: SyncChain: %v", i, err)
		}
		if len(result.Forks) != 0 {
			t.Errorf("source %d: forks = %v, want none: one source cannot show a fork it does not serve", i, result.Forks)
		}
		if len(result.Received) != 2 {
			t.Errorf("source %d: received %d blocks, want the genesis block and one branch", i, len(result.Received))
		}
		siblings, err := only.Siblings(t.Context(), pub, prevOf(t, branches[0]))
		if err != nil {
			t.Fatalf("source %d: Siblings: %v", i, err)
		}
		if len(siblings) != 1 {
			t.Errorf("source %d: the sibling set has %d members; a one-member answer is not a statement that the chain does not fork", i, len(siblings))
		}
	}

	// Two sources, the same code twice, and the fork is at the client.
	store := block.NewValidatingStore(nil)
	result, err := NewSyncer(store, left, right).SyncChain(t.Context(), pub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if len(result.Forks) != 1 {
		t.Fatalf("forks = %v, want the one the union reveals", result.Forks)
	}
	fork := result.Forks[0]
	if !fork.Pub.Equal(pub) {
		t.Errorf("the fork names another author")
	}
	if fork.Prev == nil || *fork.Prev != genesis.Digest() {
		t.Errorf("the fork is at %v, want the genesis block's position", fork.Prev)
	}
	want := digests(branches)
	slices.SortFunc(want, func(a, b cid.Digest) int { return slices.Compare(a[:], b[:]) })
	if !slices.Equal(fork.Blocks, want) {
		t.Errorf("the fork holds %v, want both branches %v", fork.Blocks, want)
	}
	if len(result.Received) != 3 {
		t.Errorf("received %d blocks, want the genesis block and both branches", len(result.Received))
	}
	// Both sources claimed a tip, and they are not the same tip. That is the
	// signal a client sees before it knows why — behind, ahead, forked, or
	// withholding, and the first and the last are indistinguishable.
	if *result.Sources[0].Tip == *result.Sources[1].Tip {
		t.Error("the two sources claimed the same tip; the fixture is not divergent")
	}
}

// TestSuccessionSync: after a node validates a rotation block it knows new_pub
// and nothing about where that chain is served, so a client of this profile
// looks for the successor at every source it already uses, and asks siblings at
// the successor's genesis position — because more than one genesis block
// claiming the same rotation is the ambiguous-succession condition and the node
// MUST surface it rather than pick (spec/07-transport.md, "Chain succession").
func TestSuccessionSync(t *testing.T) {
	old := testBuilder(t, 48)
	genesis, err := old.Public(1, nil, block.MustCreateAtom("the first key's block"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	newKey := testKey(t, 49)
	newPub := newKey.Public().(ed25519.PublicKey) //nolint:forcetypeassert // an Ed25519 key's public half is one.
	rotation, err := old.Rotation(2, nil, newPub)
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	successor := testBuilder(t, 49)
	heir, err := successor.Public(3, []cid.Digest{rotation.Digest()}, block.MustCreateAtom("the second key's block"))
	if err != nil {
		t.Fatalf("successor genesis: %v", err)
	}

	source, _ := serve(t, ServerConfig{Store: memStore(t, genesis, rotation, heir)})
	store := block.NewValidatingStore(nil)
	syncer := NewSyncer(store, source)

	if _, err := syncer.SyncChain(t.Context(), genesis.PublicKey()); err != nil {
		t.Fatalf("syncing the predecessor: %v", err)
	}
	tip, err := store.Block(rotation.Digest())
	if err != nil {
		t.Fatalf("the rotation block did not land: %v", err)
	}
	appointed, ok := tip.RotateKey()
	if !ok {
		t.Fatal("the chain's tip is not a rotation block")
	}

	// The successor chain is looked for at the same source, which is a hosting
	// convention and not a protocol fact (todo 074).
	heirSync, err := syncer.SyncChain(t.Context(), appointed.NewPublicKey())
	if err != nil {
		t.Fatalf("syncing the successor: %v", err)
	}
	if len(heirSync.Accepted) != 1 || heirSync.Accepted[0] != heir.Digest() {
		t.Fatalf("result = %+v, want the successor's genesis block accepted", heirSync)
	}
	if _, err := block.ValidateSuccession(rotation, heir); err != nil {
		t.Errorf("the synced blocks do not form a succession: %v", err)
	}
	if _, err := block.ValidateHistory([]cid.Digest{rotation.Digest(), heir.Digest()}, store, nil); err != nil {
		t.Errorf("ValidateHistory over the synced store: %v", err)
	}

	// Two genesis blocks claiming the same rotation are two members of the
	// genesis position's sibling set, which is how the ambiguous-succession
	// condition of spec/02-block-format.md, "rotate_key", is detected. A second
	// source serving the rival claim is enough, and the sync surfaces it rather
	// than picking one.
	rival, err := testBuilder(t, 49).Public(4, []cid.Digest{rotation.Digest()}, block.MustCreateAtom("a rival claim to the same rotation"))
	if err != nil {
		t.Fatalf("the rival genesis block: %v", err)
	}
	other, _ := serve(t, ServerConfig{Store: memStore(t, rotation, rival)})
	ambiguous := NewSyncer(block.NewValidatingStore(nil), source, other)
	if _, err := ambiguous.SyncChain(t.Context(), genesis.PublicKey()); err != nil {
		t.Fatalf("syncing the predecessor: %v", err)
	}
	surfaced, err := ambiguous.SyncChain(t.Context(), appointed.NewPublicKey())
	if err != nil {
		t.Fatalf("syncing the successor from two sources: %v", err)
	}
	if len(surfaced.Forks) != 1 || surfaced.Forks[0].Prev != nil {
		t.Fatalf("forks = %v, want one at the genesis position of the successor chain", surfaced.Forks)
	}
	if len(surfaced.Forks[0].Blocks) != 2 {
		t.Errorf("the ambiguous succession holds %d claims, want 2", len(surfaced.Forks[0].Blocks))
	}
	// And the same question asked of the refs graph agrees.
	successors, fork, err := block.Successors(rotation, ambiguous.Store)
	if err != nil {
		t.Fatalf("Successors: %v", err)
	}
	if len(successors) != 2 || fork == nil {
		t.Errorf("Successors = %v, %v; want two claims and a fork", successors, fork)
	}
}

// prevOf returns a block's predecessor as a position.
func prevOf(t *testing.T, b *block.Block) *cid.Digest {
	t.Helper()
	d, ok := b.Prev()
	if !ok {
		return nil
	}
	return &d
}

// TestSyncSurvivesASourceThatFails: a source that fails is not a failure of the
// sync. That is the point of having more than one — a 404, a timeout or a
// refusal is a fact about that source, and the next one is asked the same
// question (spec/07-transport.md, "The multi-source rule").
func TestSyncSurvivesASourceThatFails(t *testing.T) {
	pub, blocks := testChain(t, 50, 3)
	working, _ := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	// A client pointed at a server that is not there.
	broken, err := NewClient("http://127.0.0.1:1/dialog/v1", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	store := block.NewValidatingStore(nil)
	result, err := NewSyncer(store, broken, working).SyncChain(t.Context(), pub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if result.Sources[0].Err == nil {
		t.Error("the unreachable source reported no error")
	}
	if result.Sources[0].Blocks != 0 {
		t.Error("the unreachable source contributed blocks")
	}
	if len(result.Accepted) != len(blocks) {
		t.Errorf("accepted %d of %d blocks; a failing source stopped the sync", len(result.Accepted), len(blocks))
	}
}

// TestSyncChainFrom is the single-source sync: what a node does when it has one
// source, with the standing consequence that a fork it is not shown is a fork it
// will never see.
func TestSyncChainFrom(t *testing.T) {
	pub, genesis, branches := forkedChain(t)
	left, _ := serve(t, ServerConfig{Store: memStore(t, genesis, branches[0])})
	right, _ := serve(t, ServerConfig{Store: memStore(t, genesis, branches[1])})

	store := block.NewValidatingStore(nil)
	syncer := NewSyncer(store, left, right)
	one, err := syncer.SyncChainFrom(t.Context(), left, pub)
	if err != nil {
		t.Fatalf("SyncChainFrom: %v", err)
	}
	if len(one.Sources) != 1 {
		t.Fatalf("the sync consulted %d sources, want 1", len(one.Sources))
	}
	if len(one.Forks) != 0 {
		t.Errorf("one source showed a fork it does not serve: %v", one.Forks)
	}

	// The same syncer, asked to use everything it has, finds it.
	all, err := syncer.SyncChain(t.Context(), pub)
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if len(all.Forks) != 1 {
		t.Errorf("forks = %v, want the one the second source reveals", all.Forks)
	}
}

// TestPollAndPrefetch covers the two helpers a node uses between syncs: polling
// a tip for a few dozen bytes, and pulling a batch of foreign blocks in one
// exchange because the scan limit is a block count and not a round-trip budget.
func TestPollAndPrefetch(t *testing.T) {
	pub, blocks := testChain(t, 51, 2)
	source, _ := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	first, err := Poll(t.Context(), source, pub, "")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if first.Block == nil || first.ETag == "" {
		t.Fatalf("Poll = %+v, want the tip and its entity tag", first)
	}
	unchanged, err := Poll(t.Context(), source, pub, first.ETag)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !unchanged.Unchanged {
		t.Error("polling an unmoved chain fetched the tip again")
	}

	resolver := NewResolver(t.Context(), block.NewMemStore(), source)
	resolver.Prefetch(digests(blocks))
	// Both are cached now, so resolving them touches nothing but the cache; a
	// digest nobody holds is still reported as a fetch that did not succeed.
	for _, b := range blocks {
		if got, err := resolver.Block(b.Digest()); err != nil || got.Digest() != b.Digest() {
			t.Errorf("Block(%s) = %v, %v after Prefetch", b.Digest(), got, err)
		}
	}
	_, missing := testChain(t, 52, 1)
	resolver.Prefetch([]cid.Digest{missing[0].Digest()})
	if _, err := resolver.Block(missing[0].Digest()); !errors.Is(err, block.ErrNotFound) {
		t.Errorf("err = %v, want a failed fetch", err)
	}
}

// TestSyncReportsARejectedBlock: a source that serves a block a rule shows wrong
// is not a reason to stop syncing, and the block is not silently dropped — the
// client noticed, and says so.
func TestSyncReportsARejectedBlock(t *testing.T) {
	bad := invalidBlock(t, 53)
	source, _ := serve(t, ServerConfig{Store: memStore(t, bad)})

	store := block.NewValidatingStore(nil)
	result, err := NewSyncer(store, source).SyncChain(t.Context(), bad.PublicKey())
	if err != nil {
		t.Fatalf("SyncChain: %v", err)
	}
	if len(result.Rejected) != 1 || result.Rejected[0] != bad.Digest() {
		t.Fatalf("result = %+v, want the block reported as rejected", result)
	}
	if store.Has(bad.Digest()) {
		t.Error("a rejected block was stored")
	}
}
