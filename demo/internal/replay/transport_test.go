package replay_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/demo/chains"
	"github.com/vrinek/Dialog/demo/internal/chainfile"
	"github.com/vrinek/Dialog/demo/internal/render"
	"github.com/vrinek/Dialog/demo/internal/replay"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/graph"
	"github.com/vrinek/Dialog/go/transport"
)

// TestRangeResponseIsTheConcatenatedBlockFiles is the claim
// spec/07-transport.md, "As a file", makes about this very directory:
// concatenating one author's block files in the order the index lists them
// yields, byte for byte, the range response for that author's whole chain from
// the genesis position.
//
// The committed chain directory is the degenerate case of the block sequence
// rather than an exception to it — each .block file is a one-block sequence —
// and this is what keeps offline exchange from being a parallel mechanism.
func TestRangeResponseIsTheConcatenatedBlockFiles(t *testing.T) {
	index, parsed, err := chainfile.Read(chains.FS)
	if err != nil {
		t.Fatalf("reading the chain directory: %v", err)
	}
	node, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the chain directory: %v", err)
	}
	srv, err := transport.NewServer(transport.ServerConfig{Store: node.Store})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	for i, entry := range index.Chains {
		// The bytes on disk, joined in index order and nothing else.
		var want []byte
		for _, b := range entry.Blocks {
			raw, err := fs.ReadFile(chains.FS, b.File)
			if err != nil {
				t.Fatalf("reading %s: %v", b.File, err)
			}
			want = append(want, raw...)
		}

		url := ts.URL + transport.DefaultPrefix + "/chains/" + entry.PublicKey + "/blocks?limit=64"
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("range for %s: %v", entry.Author, err)
		}
		got, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("reading the range response: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("range for %s: status %d", entry.Author, resp.StatusCode)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("the range response for %s is %d bytes, the concatenated block files are %d", entry.Author, len(got), len(want))
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != transport.MediaTypeBlocks {
			t.Errorf("%s: Content-Type = %q", entry.Author, ct)
		}
		// The tip the server reports for the chain is the last block the index
		// lists, which is what makes the file and the response one artifact in
		// both directions.
		wantTip := entry.Blocks[len(entry.Blocks)-1].CID
		if got := resp.Header.Get(transport.HeaderTip); got != wantTip {
			t.Errorf("%s: %s = %q, want %q", entry.Author, transport.HeaderTip, got, wantTip)
		}

		// And the same bytes read as a chain file give the chain back.
		blocks, err := transport.DecodeSeq(want)
		if err != nil {
			t.Fatalf("decoding %s's concatenated files: %v", entry.Author, err)
		}
		if !slices.Equal(digestsOf(blocks), digestsOf(parsed[i].Blocks)) {
			t.Errorf("%s: the concatenation decodes to a different chain", entry.Author)
		}
	}
}

// TestColdSyncMatchesTheFileReplay serves the demo's three chains from one node
// and syncs a second node from scratch over HTTP, then compares what the two
// nodes know.
//
// The comparison is the whole point. A node that received its blocks over a
// network and a node that read them off a disk must reach the same L1 store, the
// same L2 graph and the same L3 views, because the transport carries no trust
// and adds nothing: every guarantee comes from the blocks
// (spec/07-transport.md, "The transport carries no trust").
func TestColdSyncMatchesTheFileReplay(t *testing.T) {
	fromFiles, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the chain directory: %v", err)
	}
	srv, err := transport.NewServer(transport.ServerConfig{Store: fromFiles.Store})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	source, err := transport.NewClient(ts.URL+transport.DefaultPrefix, &transport.ClientConfig{HTTP: ts.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// The second node: an empty store, and the profile's own sequence — tip,
	// range, validate every block on receipt.
	store := block.NewValidatingStore(nil)
	syncer := transport.NewSyncer(store, source)
	for _, chain := range fromFiles.Chains {
		result, err := syncer.SyncChain(t.Context(), chain.Pub)
		if err != nil {
			t.Fatalf("syncing %s: %v", chain.Author, err)
		}
		if len(result.Received) != len(chain.Blocks) {
			t.Fatalf("syncing %s received %d blocks, want %d", chain.Author, len(result.Received), len(chain.Blocks))
		}
		if len(result.Accepted) != len(chain.Blocks) {
			t.Fatalf("syncing %s accepted %d of %d blocks: %+v", chain.Author, len(result.Accepted), len(chain.Blocks), result)
		}
		if len(result.Forks) != 0 {
			t.Errorf("syncing %s reported forks: %v", chain.Author, result.Forks)
		}
		if !slices.Equal(result.Received, digestsOf(chain.Blocks)) {
			t.Errorf("syncing %s received the blocks in another order", chain.Author)
		}
	}
	if store.Len() != fromFiles.BlockCount() {
		t.Fatalf("the synced store holds %d blocks, the file replay holds %d", store.Len(), fromFiles.BlockCount())
	}

	// L2 from the synced store, by the same path replay.Load uses: validate
	// each chain from its tip, then ingest the blocks of a chain that
	// validated. Nothing bypasses validation because the bytes arrived over a
	// network.
	synced := graph.New()
	for _, chain := range fromFiles.Chains {
		validated, err := block.ValidateChain(chain.Tip(), store, nil)
		if err != nil {
			t.Fatalf("validating %s over the synced store: %v", chain.Author, err)
		}
		if !slices.Equal(digestsOf(validated.Blocks), digestsOf(chain.Blocks)) {
			t.Fatalf("%s validates to a different chain over the synced store", chain.Author)
		}
		for _, b := range validated.Blocks {
			if err := synced.Ingest(b); err != nil {
				t.Fatalf("ingesting %s: %v", b.Digest(), err)
			}
		}
	}

	// L3, for every subscription set the demo's own tests care about: the three
	// authors alone, in pairs, and all together.
	authors := fromFiles.Authors()
	sets := [][]string{{}, {authors[0]}, {authors[1]}, {authors[2]},
		{authors[0], authors[1]}, {authors[0], authors[2]}, {authors[1], authors[2]}, authors}
	for _, set := range sets {
		want, err := fromFiles.View(set...)
		if err != nil {
			t.Fatalf("the file replay's view for %v: %v", set, err)
		}
		subs := accept.NewSubscriptions()
		for _, name := range set {
			pub, ok := fromFiles.PublicKey(name)
			if !ok {
				t.Fatalf("no key for %s", name)
			}
			subs.Subscribe(pub)
		}
		got, err := accept.Build(synced, store, subs)
		if err != nil {
			t.Fatalf("the synced node's view for %v: %v", set, err)
		}
		if len(set) == len(authors) && (want.Len() == 0 || len(want.Conflicts()) == 0) {
			t.Fatalf("the all-authors view holds %d entities and %d conflicts; an empty comparison proves nothing", want.Len(), len(want.Conflicts()))
		}
		if wantText, gotText := snapshot(want), snapshot(got); wantText != gotText {
			t.Errorf("the two nodes disagree for the subscription set %v:\n--- from files ---\n%s\n--- from the transport ---\n%s", set, wantText, gotText)
		}
	}
}

// TestAnnounceTheDemoChains offers the committed chains to an empty node the
// other way round — errata first, whose blocks reference atlas's — and reads the
// receipt.
//
// It is the profile's held verdict on real data: a block whose refs name a block
// the source does not hold is *stored but unvalidated*, neither accepted nor
// refused, and the ancestry arriving later settles it
// (spec/07-transport.md, "announce"; spec/05-processing-model.md, "Block
// reception").
func TestAnnounceTheDemoChains(t *testing.T) {
	fromFiles, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the chain directory: %v", err)
	}
	store := block.NewValidatingStore(nil)
	srv, err := transport.NewServer(transport.ServerConfig{Store: store, Announce: transport.StoreAnnouncer(store)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	client, err := transport.NewClient(ts.URL+transport.DefaultPrefix, &transport.ClientConfig{HTTP: ts.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ordered := fromFiles.Chains
	last := ordered[len(ordered)-1]
	held, err := client.Announce(t.Context(), last.Blocks)
	if err != nil {
		t.Fatalf("announcing %s: %v", last.Author, err)
	}
	if len(held.Rejected) != 0 {
		t.Fatalf("announcing %s was refused: %+v", last.Author, held.Rejected)
	}
	if len(held.Held) == 0 {
		t.Fatalf("announcing %s alone accepted everything; its blocks reference chains this node does not hold", last.Author)
	}

	// The chains it depends on follow, and the held blocks settle.
	for _, chain := range ordered[:len(ordered)-1] {
		if _, err := client.Announce(t.Context(), chain.Blocks); err != nil {
			t.Fatalf("announcing %s: %v", chain.Author, err)
		}
	}
	again, err := client.Announce(t.Context(), last.Blocks)
	if err != nil {
		t.Fatalf("re-announcing %s: %v", last.Author, err)
	}
	if len(again.Accepted) != len(last.Blocks) || len(again.Held) != 0 {
		t.Errorf("receipt = %+v, want every block accepted once its dependencies arrived", again)
	}
	for _, chain := range fromFiles.Chains {
		for _, b := range chain.Blocks {
			if !store.Accepted(b.Digest()) {
				t.Errorf("%s block %s was not accepted", chain.Author, b.Digest())
			}
		}
	}
}

// TestSyncFromTwoMirrorsAgrees: the multi-source rule against the demo's own
// data. Two servers holding the same chains reveal no fork, which is the honest
// case the rule is cheap in — "ask two sources" is the same code twice
// (spec/07-transport.md, "The multi-source rule").
func TestSyncFromTwoMirrorsAgrees(t *testing.T) {
	node, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the chain directory: %v", err)
	}
	sources := make([]transport.Source, 0, 2)
	for range 2 {
		srv, err := transport.NewServer(transport.ServerConfig{Store: node.Store})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		ts := httptest.NewServer(srv)
		t.Cleanup(ts.Close)
		client, err := transport.NewClient(ts.URL+transport.DefaultPrefix, &transport.ClientConfig{HTTP: ts.Client()})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		sources = append(sources, client)
	}

	store := block.NewValidatingStore(nil)
	syncer := transport.NewSyncer(store, sources...)
	for _, chain := range node.Chains {
		result, err := syncer.SyncChain(context.Background(), chain.Pub)
		if err != nil {
			t.Fatalf("syncing %s: %v", chain.Author, err)
		}
		if len(result.Forks) != 0 {
			t.Errorf("two mirrors of one store disagree about %s: %v", chain.Author, result.Forks)
		}
		if len(result.Sources) != 2 {
			t.Fatalf("the sync consulted %d sources", len(result.Sources))
		}
		for i, s := range result.Sources {
			if s.Err != nil {
				t.Errorf("source %d: %v", i, s.Err)
			}
			if s.Tip == nil || *s.Tip != chain.Tip() {
				t.Errorf("source %d claimed tip %v, want %s", i, s.Tip, chain.Tip())
			}
		}
	}
	if store.Len() != node.BlockCount() {
		t.Errorf("the synced store holds %d blocks, want %d", store.Len(), node.BlockCount())
	}
}

// snapshot renders everything an L3 view knows, in the view's own deterministic
// order, as text. Two views that render identically are the same view: the
// digests, their kinds, the sentence each spells, the truth state, the
// supersession and the conflicts are between them the whole of what the layer
// answers.
func snapshot(v *accept.View) string {
	var b strings.Builder
	r := render.New(v)
	fmt.Fprintf(&b, "%d entities, %d conflicts\n", v.Len(), len(v.Conflicts()))
	for _, d := range v.Digests() {
		entry, ok := v.Lookup(d)
		if !ok {
			fmt.Fprintf(&b, "%s MISSING\n", d)
			continue
		}
		fmt.Fprintf(&b, "%s %s truth=%v superseded=%v %q\n", d, entry.Kind(), v.Truth(d), v.IsSuperseded(d), r.Text(d))
		for _, e := range v.EquivalenceClass(d) {
			fmt.Fprintf(&b, "  equivalent %s\n", e)
		}
		for _, s := range v.Supersedes(d) {
			fmt.Fprintf(&b, "  supersedes %s\n", s)
		}
		for _, c := range v.Contradictions(d) {
			fmt.Fprintf(&b, "  contradicts %s\n", c)
		}
	}
	for _, c := range v.Conflicts() {
		fmt.Fprintf(&b, "conflict %v\n", c)
	}
	return b.String()
}

func digestsOf(blocks []*block.Block) []cid.Digest {
	out := make([]cid.Digest, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Digest())
	}
	return out
}
