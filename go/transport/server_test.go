package transport

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// TestServerPathsAndOperations walks the six operations at the paths the profile
// fixes, against a server holding one four-block chain
// (spec/07-transport.md, "HTTP binding", the method-and-path table).
func TestServerPathsAndOperations(t *testing.T) {
	pub, blocks := testChain(t, 1, 4)
	store := memStore(t, blocks...)
	client, ts := serve(t, ServerConfig{Store: store})
	author := authorText(t, pub)

	t.Run("tip", func(t *testing.T) {
		resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+author+"/tip", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != MediaTypeBlocks {
			t.Errorf("Content-Type = %q, want %q", ct, MediaTypeBlocks)
		}
		tipCID := blocks[3].CID().String()
		if got := resp.Header.Get(HeaderTip); got != tipCID {
			t.Errorf("%s = %q, want %q", HeaderTip, got, tipCID)
		}
		if got := resp.Header.Get("ETag"); got != `"`+tipCID+`"` {
			t.Errorf("ETag = %q, want a strong tag holding the tip's CID", got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache so that a cache revalidates", got)
		}
		// The response is the block itself, not a statement of its digest: the
		// client computes the identity from the bytes.
		body, _ := io.ReadAll(resp.Body)
		if d := cid.SumDigest(body); d != blocks[3].Digest() {
			t.Errorf("the body hashes to %s, want the tip %s", d, blocks[3].Digest())
		}
	})

	t.Run("range from the genesis position", func(t *testing.T) {
		result, err := client.Range(t.Context(), pub, nil, 0)
		if err != nil {
			t.Fatalf("Range: %v", err)
		}
		if !slices.Equal(digests(result.Blocks), digests(blocks)) {
			t.Errorf("the range is %v, want the whole chain %v", digests(result.Blocks), digests(blocks))
		}
		if !result.AtTip() {
			t.Error("the range ended at the tip and AtTip says otherwise")
		}
	})

	t.Run("range is exclusive of its position", func(t *testing.T) {
		after := blocks[1].Digest()
		result, err := client.Range(t.Context(), pub, &after, 0)
		if err != nil {
			t.Fatalf("Range: %v", err)
		}
		if !slices.Equal(digests(result.Blocks), digests(blocks[2:])) {
			t.Errorf("range after block 1 = %v, want blocks 2 and 3", digests(result.Blocks))
		}
	})

	t.Run("block by CID", func(t *testing.T) {
		resp := get(t, ts, http.MethodGet, DefaultPrefix+"/blocks/"+blocks[2].CID().String(), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		// The response for a given CID can never change, because the CID is the
		// hash of the response.
		if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("Cache-Control = %q, want the immutable directive", got)
		}
	})

	t.Run("blocks in the order requested", func(t *testing.T) {
		want := []cid.Digest{blocks[3].Digest(), blocks[0].Digest(), blocks[2].Digest()}
		got, err := client.Blocks(t.Context(), want)
		if err != nil {
			t.Fatalf("Blocks: %v", err)
		}
		if !slices.Equal(digests(got), want) {
			t.Errorf("the response is %v, want the request's order %v", digests(got), want)
		}
	})

	t.Run("blocks omits what the source does not hold", func(t *testing.T) {
		_, foreign := testChain(t, 2, 1)
		got, err := client.Blocks(t.Context(), []cid.Digest{blocks[0].Digest(), foreign[0].Digest()})
		if err != nil {
			t.Fatalf("Blocks: %v", err)
		}
		if len(got) != 1 || got[0].Digest() != blocks[0].Digest() {
			t.Errorf("the response is %v, want only the held block", digests(got))
		}
	})

	t.Run("siblings at the genesis position", func(t *testing.T) {
		got, err := client.Siblings(t.Context(), pub, nil)
		if err != nil {
			t.Fatalf("Siblings: %v", err)
		}
		if len(got) != 1 || got[0].Digest() != blocks[0].Digest() {
			t.Errorf("the genesis sibling set is %v, want the genesis block", digests(got))
		}
	})
}

// TestServerRule1: a server that answers tip for an author MUST be able to
// answer range from the genesis position for the same author. Serving a chain
// means serving all of it (spec/07-transport.md, "Server rules", rule 1).
func TestServerRule1TipAndRangeAgree(t *testing.T) {
	pub, blocks := testChain(t, 3, 5)
	client, _ := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	tip, err := client.Tip(t.Context(), pub, "")
	if err != nil {
		t.Fatalf("Tip: %v", err)
	}
	result, err := client.Range(t.Context(), pub, nil, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(result.Blocks) == 0 {
		t.Fatal("the server answered tip and served an empty range from the genesis position")
	}
	last := result.Blocks[len(result.Blocks)-1]
	if last.Digest() != tip.Block.Digest() {
		t.Errorf("the range ends at %s, tip says %s", last.Digest(), tip.Block.Digest())
	}
}

// TestServerStopsBeforeAHole: where a server holds a gap it MUST end the
// response before the gap, so that the client's prev walk terminates cleanly
// (spec/07-transport.md, "Server rules", rule 2).
func TestServerStopsBeforeAHole(t *testing.T) {
	pub, blocks := testChain(t, 4, 5)
	// Everything but block 2: a hole in the middle of the chain.
	held := []*block.Block{blocks[0], blocks[1], blocks[3], blocks[4]}
	client, _ := serve(t, ServerConfig{Store: memStore(t, held...)})

	result, err := client.Range(t.Context(), pub, nil, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if !slices.Equal(digests(result.Blocks), digests(blocks[:2])) {
		t.Errorf("the range is %v, want it to stop before the hole at %v", digests(result.Blocks), digests(blocks[:2]))
	}
	// And the tip it reports is the end of what it can serve, so that the two
	// answers do not contradict each other.
	tip, err := client.Tip(t.Context(), pub, "")
	if err != nil {
		t.Fatalf("Tip: %v", err)
	}
	if tip.Block.Digest() != blocks[1].Digest() {
		t.Errorf("tip = %s, want the last block the range can reach, %s", tip.Block.Digest(), blocks[1].Digest())
	}
	// The blocks past the hole are still served by digest, where no claim about
	// the chain is being made.
	if _, err := client.Block(t.Context(), blocks[4].Digest()); err != nil {
		t.Errorf("a block past the hole is not served by digest: %v", err)
	}
}

// TestNoGenesisIsNoTip: the tip is the end of the forward walk from the genesis
// position, so a source holding no block at that position holds no tip at all,
// whatever else of the chain it holds. It still serves those blocks by digest,
// where no claim about a chain is being made (spec/07-transport.md, "tip";
// todos/086).
func TestNoGenesisIsNoTip(t *testing.T) {
	pub, blocks := testChain(t, 32, 5)
	// Blocks 2, 3 and 4 of a chain whose first two this source never received.
	client, ts := serve(t, ServerConfig{Store: memStore(t, blocks[2:]...)})

	resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, pub)+"/tip", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("tip = %d, want 404: the walk from the genesis position reaches nothing", resp.StatusCode)
	}
	result, err := client.Range(t.Context(), pub, nil, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(result.Blocks) != 0 {
		t.Errorf("the range is %d blocks, want an empty sequence", len(result.Blocks))
	}
	for _, b := range blocks[2:] {
		if _, err := client.Block(t.Context(), b.Digest()); err != nil {
			t.Errorf("a block of the unreachable run is not served by digest: %v", err)
		}
	}
}

// TestRangeLimit: a source MUST NOT return more blocks than the requested
// maximum, and MAY return fewer. Continuation is the client asking again from
// the last block it received — no cursor, no session, no server-side state.
func TestRangeLimit(t *testing.T) {
	pub, blocks := testChain(t, 5, 6)
	client, _ := serve(t, ServerConfig{Store: memStore(t, blocks...), MaxRangeLimit: 4})

	result, err := client.Range(t.Context(), pub, nil, 2)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(result.Blocks) != 2 {
		t.Fatalf("limit=2 returned %d blocks", len(result.Blocks))
	}
	if result.AtTip() {
		t.Error("a truncated range claims to be at the tip")
	}

	// The server's own cap is lower than the client's ask, and it holds.
	capped, err := client.Range(t.Context(), pub, nil, 100)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(capped.Blocks) != 4 {
		t.Errorf("limit=100 against a cap of 4 returned %d blocks", len(capped.Blocks))
	}

	// Continue from the last block received, twice, and arrive at the tip.
	var walked []cid.Digest
	var after *cid.Digest
	for range 6 {
		page, err := client.Range(t.Context(), pub, after, 2)
		if err != nil {
			t.Fatalf("Range: %v", err)
		}
		if len(page.Blocks) == 0 {
			break
		}
		walked = append(walked, digests(page.Blocks)...)
		last, _ := page.Last()
		after = &last
		if page.AtTip() {
			break
		}
	}
	if !slices.Equal(walked, digests(blocks)) {
		t.Errorf("walking the chain two blocks at a time gave %v, want %v", walked, digests(blocks))
	}
}

// TestEmptyRangeIsNotAnError: an empty sequence is the answer both when the
// client is already at the tip and when the source's store stops there, and the
// two are told apart by the tip rather than by the emptiness.
func TestEmptyRangeIsNotAnError(t *testing.T) {
	pub, blocks := testChain(t, 6, 2)
	client, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	tip := blocks[1].Digest()
	result, err := client.Range(t.Context(), pub, &tip, 0)
	if err != nil {
		t.Fatalf("Range at the tip: %v", err)
	}
	if len(result.Blocks) != 0 {
		t.Errorf("a range after the tip returned %d blocks", len(result.Blocks))
	}
	resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, pub)+"/blocks?after="+tip.CID().String(), nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: an empty sequence is a valid answer meaning none", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("the empty sequence is %d bytes, want 0", len(body))
	}
}

// TestNoTipToReport: what a server sends where it holds no tip for an author.
// The header is REQUIRED on a 200 to tip or range and its value is a CID text
// form, so a source with no tip omits it rather than minting a second spelling
// of a position; a tip request in the same state is a 404
// (spec/07-transport.md, "HTTP binding", "Where the server holds no tip";
// todos/085).
func TestNoTipToReport(t *testing.T) {
	pub, blocks := testChain(t, 30, 2)
	unknownPub, _ := testChain(t, 31, 1)
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	t.Run("an empty range for an author this source holds nothing from", func(t *testing.T) {
		resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, unknownPub)+"/blocks", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 with an empty sequence", resp.StatusCode)
		}
		if got, present := resp.Header[HeaderTip]; present {
			t.Errorf("%s = %q; a source with no tip omits the header", HeaderTip, got)
		}
		body, _ := io.ReadAll(resp.Body)
		if len(body) != 0 {
			t.Errorf("the sequence is %d bytes, want 0", len(body))
		}
	})

	t.Run("tip for the same author is 404", func(t *testing.T) {
		resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, unknownPub)+"/tip", nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if got, present := resp.Header[HeaderTip]; present {
			t.Errorf("%s = %q on a 404", HeaderTip, got)
		}
	})

	t.Run("a 304 carries the header", func(t *testing.T) {
		etag := `"` + blocks[1].CID().String() + `"`
		resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, pub)+"/tip",
			http.Header{"If-None-Match": []string{etag}})
		if resp.StatusCode != http.StatusNotModified {
			t.Fatalf("status = %d, want 304", resp.StatusCode)
		}
		if got := resp.Header.Get(HeaderTip); got != blocks[1].CID().String() {
			t.Errorf("%s on a 304 = %q, want the tip's CID", HeaderTip, got)
		}
	})
}

// TestConditionalTip is the baseline subscription: polling tip with
// If-None-Match, where a 304 is a few dozen bytes
// (spec/07-transport.md, "Subscription mapping", point 1; "Caching").
func TestConditionalTip(t *testing.T) {
	pub, blocks := testChain(t, 7, 2)
	client, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	first, err := client.Tip(t.Context(), pub, "")
	if err != nil {
		t.Fatalf("Tip: %v", err)
	}
	again, err := client.Tip(t.Context(), pub, first.ETag)
	if err != nil {
		t.Fatalf("Tip with If-None-Match: %v", err)
	}
	if !again.Unchanged || again.Block != nil {
		t.Errorf("polling an unmoved tip = %+v, want unchanged and no block", again)
	}

	resp := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, pub)+"/tip",
		http.Header{"If-None-Match": []string{first.ETag}})
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("a 304 carried %d bytes of body", len(body))
	}
	// A tag that is not this tip's is a miss, and the block comes back.
	stale := `"` + blocks[0].CID().String() + `"`
	moved := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+authorText(t, pub)+"/tip",
		http.Header{"If-None-Match": []string{stale}})
	if moved.StatusCode != http.StatusOK {
		t.Errorf("status for a stale tag = %d, want 200", moved.StatusCode)
	}
}

// TestHeadIsSupportedWhereGetIs, with the headers of the GET and none of the
// body (spec/07-transport.md, "HTTP binding").
func TestHeadIsSupportedWhereGetIs(t *testing.T) {
	pub, blocks := testChain(t, 8, 3)
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})
	author := authorText(t, pub)

	for _, path := range []string{
		DefaultPrefix + "/chains/" + author + "/tip",
		DefaultPrefix + "/chains/" + author + "/blocks",
		DefaultPrefix + "/chains/" + author + "/siblings",
		DefaultPrefix + "/blocks/" + blocks[0].CID().String(),
	} {
		head := get(t, ts, http.MethodHead, path, nil)
		if head.StatusCode != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200", path, head.StatusCode)
			continue
		}
		body, _ := io.ReadAll(head.Body)
		if len(body) != 0 {
			t.Errorf("HEAD %s carried %d bytes of body", path, len(body))
		}
		if head.Header.Get("Content-Length") == "" {
			t.Errorf("HEAD %s sent no Content-Length", path)
		}
	}
}

// TestMethodNotAllowed: any other method on a defined path MUST return 405 with
// an Allow header.
func TestMethodNotAllowed(t *testing.T) {
	pub, blocks := testChain(t, 9, 2)
	store := block.NewValidatingStore(nil)
	if err := store.AddAll(blocks...); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}
	_, ts := serve(t, ServerConfig{Store: store, Announce: StoreAnnouncer(store)})
	author := authorText(t, pub)

	cases := []struct {
		path, method, allow string
	}{
		{DefaultPrefix + "/chains/" + author + "/tip", http.MethodPost, "GET, HEAD"},
		{DefaultPrefix + "/chains/" + author + "/blocks", http.MethodDelete, "GET, HEAD"},
		{DefaultPrefix + "/blocks/" + blocks[0].CID().String(), http.MethodPut, "GET, HEAD"},
		{DefaultPrefix + "/blocks/fetch", http.MethodGet, "POST"},
		{DefaultPrefix + "/announce", http.MethodGet, "POST"},
	}
	for _, c := range cases {
		resp := get(t, ts, c.method, c.path, nil)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", c.method, c.path, resp.StatusCode)
			continue
		}
		if got := resp.Header.Get("Allow"); got != c.allow {
			t.Errorf("%s %s: Allow = %q, want %q", c.method, c.path, got, c.allow)
		}
		assertProblem(t, resp, http.StatusMethodNotAllowed)
	}
}

// TestNonCanonicalSpellingsAreRejected: a server MUST reject any other spelling
// of an author key or a CID with 400 rather than normalizing it, because a
// server that accepted a variant would be minting aliases
// (spec/07-transport.md, "HTTP binding"; spec/03-encoding.md, Security
// Considerations).
func TestNonCanonicalSpellingsAreRejected(t *testing.T) {
	pub, blocks := testChain(t, 10, 2)
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})
	author := authorText(t, pub)
	blockCID := blocks[0].CID().String()
	rawDigest := blocks[0].Digest()

	cases := []struct{ name, path string }{
		{"an uppercase author key", DefaultPrefix + "/chains/" + strings.ToUpper(author) + "/tip"},
		{"a hex author key", DefaultPrefix + "/chains/" + hex.EncodeToString(pub) + "/tip"},
		{"an author key without its multibase prefix", DefaultPrefix + "/chains/" + author[1:] + "/tip"},
		{"an uppercase CID", DefaultPrefix + "/blocks/" + strings.ToUpper(blockCID)},
		{"a hex digest in place of a CID", DefaultPrefix + "/blocks/" + hex.EncodeToString(rawDigest[:])},
		{"the literal null as a position", DefaultPrefix + "/chains/" + author + "/blocks?after=null"},
		{"an empty position", DefaultPrefix + "/chains/" + author + "/blocks?after="},
		{"the literal null as a sibling position", DefaultPrefix + "/chains/" + author + "/siblings?prev=null"},
		{"a hex position", DefaultPrefix + "/chains/" + author + "/blocks?after=" + hex.EncodeToString(rawDigest[:])},
		{"a limit of zero", DefaultPrefix + "/chains/" + author + "/blocks?limit=0"},
		{"a negative limit", DefaultPrefix + "/chains/" + author + "/blocks?limit=-1"},
		{"a non-numeric limit", DefaultPrefix + "/chains/" + author + "/blocks?limit=many"},
		{"a limit with a leading zero", DefaultPrefix + "/chains/" + author + "/blocks?limit=01"},
		{"a signed limit", DefaultPrefix + "/chains/" + author + "/blocks?limit=%2B1"},
		{"a decimal limit", DefaultPrefix + "/chains/" + author + "/blocks?limit=1.0"},
		{"a limit with whitespace around it", DefaultPrefix + "/chains/" + author + "/blocks?limit=%201"},
		{"a limit in exponent notation", DefaultPrefix + "/chains/" + author + "/blocks?limit=1e3"},
		{"a limit too large to be a count of blocks", DefaultPrefix + "/chains/" + author + "/blocks?limit=99999999999999999999"},
		{"an empty limit", DefaultPrefix + "/chains/" + author + "/blocks?limit="},
		// A query parameter given more than once is malformed: two values, and
		// no rule saying which wins (spec/07-transport.md, "HTTP binding";
		// todos/089).
		{"limit given twice", DefaultPrefix + "/chains/" + author + "/blocks?limit=1&limit=2"},
		{"after given twice", DefaultPrefix + "/chains/" + author + "/blocks?after=" + blockCID + "&after=" + blockCID},
		{"prev given twice", DefaultPrefix + "/chains/" + author + "/siblings?prev=" + blockCID + "&prev=" + blockCID},
		// Percent-encoding is a second spelling, and one spelling means one
		// byte sequence. Each of these decodes to the canonical form a
		// URL library would hand a handler, which is exactly why the server
		// reads the target as it arrived (spec/07-transport.md, "HTTP
		// binding"; todos/090).
		{"a percent-encoded author key", DefaultPrefix + "/chains/%62" + author[1:] + "/tip"},
		{"a percent-encoded author key on a range", DefaultPrefix + "/chains/%62" + author[1:] + "/blocks"},
		{"a percent-encoded CID", DefaultPrefix + "/blocks/%62" + blockCID[1:]},
		{"a percent-encoded position", DefaultPrefix + "/chains/" + author + "/blocks?after=%62" + blockCID[1:]},
		{"a percent-encoded sibling position", DefaultPrefix + "/chains/" + author + "/siblings?prev=%62" + blockCID[1:]},
		{"a percent-encoded limit", DefaultPrefix + "/chains/" + author + "/blocks?limit=%31"},
	}
	for _, c := range cases {
		resp := get(t, ts, http.MethodGet, c.path, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.name, resp.StatusCode)
			continue
		}
		assertProblem(t, resp, http.StatusBadRequest)
	}
}

// TestUnknownQueryParametersAreIgnored: the repeated-parameter 400 is scoped to
// the parameters this profile defines for the operation being invoked, and
// everything else is ignored, once or many times.
//
// It is the rule the long poll's degradation already was — a server that does
// not implement wait MUST ignore it and answer immediately — and it is what
// lets a later version of this profile add a parameter without breaking a
// server written against this one. The cost is accepted and is real: prev sent
// to a range, or after sent to a sibling set, is answered from the genesis
// position with no signal that the parameter went nowhere
// (spec/07-transport.md, "HTTP binding"; todos/095).
func TestUnknownQueryParametersAreIgnored(t *testing.T) {
	pub, blocks := testChain(t, 60, 4)
	client, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})
	author := authorText(t, pub)
	blockCID := blocks[1].CID().String()

	ignored := []struct {
		name, path string
		want       int
	}{
		{"a tracking parameter an intermediary appended", DefaultPrefix + "/chains/" + author + "/blocks?limit=2&utm_source=somewhere", 2},
		{"a percent-encoded value of a parameter this profile does not define", DefaultPrefix + "/chains/" + author + "/blocks?limit=2&ref=%20a%20b", 2},
		// The long poll is OPTIONAL and this server does not implement it, so
		// the parameter is ignored and the answer is immediate — and a
		// repeated one is ignored twice rather than refused, which is what
		// keeps the two rules from contradicting each other.
		{"the long poll parameter, given once", DefaultPrefix + "/chains/" + author + "/blocks?wait=5", 4},
		{"the long poll parameter, given twice", DefaultPrefix + "/chains/" + author + "/blocks?wait=5&wait=6", 4},
		{"a parameter this profile defines for another operation", DefaultPrefix + "/chains/" + author + "/blocks?prev=" + blockCID, 4},
		{"that other parameter, given twice", DefaultPrefix + "/chains/" + author + "/blocks?prev=" + blockCID + "&prev=" + blockCID, 4},
	}
	for _, c := range ignored {
		resp := get(t, ts, http.MethodGet, c.path, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", c.name, resp.StatusCode)
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s: reading the body: %v", c.name, err)
		}
		got, err := DecodeSeq(raw)
		if err != nil {
			t.Fatalf("%s: decoding the body: %v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s: the range carries %d blocks, want %d", c.name, len(got), c.want)
		}
	}

	// prev on a range names a position the server never reads, so the answer is
	// the genesis position's: the blocks the parameter's sender meant to skip
	// are all there.
	all := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+author+"/siblings?after="+blockCID, nil)
	if all.StatusCode != http.StatusOK {
		t.Errorf("after on a sibling set: status = %d, want 200", all.StatusCode)
	}

	// And the parameters the operation does define are still refused for being
	// given twice, which is the scope the rule kept.
	twice := get(t, ts, http.MethodGet, DefaultPrefix+"/chains/"+author+"/blocks?limit=2&limit=2&x=1&x=2", nil)
	if twice.StatusCode != http.StatusBadRequest {
		t.Errorf("limit twice beside an unknown parameter twice: status = %d, want 400", twice.StatusCode)
	}
	assertProblem(t, twice, http.StatusBadRequest)

	// The client is unaffected: it sends the parameters it defines and no others.
	if _, err := client.Range(t.Context(), pub, nil, 2); err != nil {
		t.Fatalf("Range: %v", err)
	}
}

// TestNotHeldIsFourOhFour: 404 means "I do not have it" and never "it does not
// exist" (spec/07-transport.md, "Status codes").
func TestNotHeldIsFourOhFour(t *testing.T) {
	pub, blocks := testChain(t, 11, 2)
	unknownPub, unknownBlocks := testChain(t, 12, 1)
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})

	for _, c := range []struct{ name, path string }{
		{"an author this source holds nothing from", DefaultPrefix + "/chains/" + authorText(t, unknownPub) + "/tip"},
		{"a block this source does not hold", DefaultPrefix + "/blocks/" + unknownBlocks[0].CID().String()},
		{"a path this server does not define", DefaultPrefix + "/nothing"},
		{"the announce path of a read-only mirror", DefaultPrefix + "/announce"},
	} {
		resp := get(t, ts, http.MethodGet, c.path, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", c.name, resp.StatusCode)
			continue
		}
		p := assertProblem(t, resp, http.StatusNotFound)
		if !strings.Contains(p.Title, "not held") && !strings.Contains(p.Title, "no resource") && !strings.Contains(p.Title, "not offered") {
			t.Errorf("%s: title = %q; 404 is a fact about the source", c.name, p.Title)
		}
	}
	_ = pub
}

// TestOperationNotOffered: an OPTIONAL operation a server does not offer answers
// 404, and the problem type distinguishes it from a resource this source does
// not hold — the two are different facts and a client's next move differs
// between them (spec/07-transport.md, "The six operations"; "Status codes";
// todos/087).
func TestOperationNotOffered(t *testing.T) {
	pub, blocks := testChain(t, 33, 2)
	unknownPub, unknownBlocks := testChain(t, 34, 1)
	// A read-only mirror: no Announcer, which the profile makes conforming.
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})
	_ = pub

	notOffered := []struct{ name, method, path string }{
		{"announce on a read-only mirror", http.MethodPost, DefaultPrefix + "/announce"},
		{"announce asked with the wrong method", http.MethodGet, DefaultPrefix + "/announce"},
		{"the event stream this server does not implement", http.MethodGet, DefaultPrefix + "/events"},
	}
	for _, c := range notOffered {
		resp := get(t, ts, c.method, c.path, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", c.name, resp.StatusCode)
			continue
		}
		p := assertProblem(t, resp, http.StatusNotFound)
		if p.Type != ProblemOperationNotOffered {
			t.Errorf("%s: problem type = %q, want %q", c.name, p.Type, ProblemOperationNotOffered)
		}
	}

	notHeld := []struct{ name, path string }{
		{"a tip this source has no chain for", DefaultPrefix + "/chains/" + authorText(t, unknownPub) + "/tip"},
		{"a block this source does not hold", DefaultPrefix + "/blocks/" + unknownBlocks[0].CID().String()},
	}
	for _, c := range notHeld {
		resp := get(t, ts, http.MethodGet, c.path, nil)
		p := assertProblem(t, resp, http.StatusNotFound)
		if p.Type != ProblemNotHeld {
			t.Errorf("%s: problem type = %q, want %q", c.name, p.Type, ProblemNotHeld)
		}
	}

	// A server built with an Announcer serves the path, so the type never
	// appears there.
	store := block.NewValidatingStore(nil)
	if err := store.AddAll(blocks...); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}
	_, writable := serve(t, ServerConfig{Store: store, Announce: StoreAnnouncer(store)})
	resp := post(t, writable, DefaultPrefix+"/announce", MediaTypeBlocks, nil)
	if resp.StatusCode == http.StatusNotFound {
		t.Error("a server with an Announcer answered 404 for the announce path")
	}
}

// TestContentNegotiation: 406 when the client's Accept excludes the only type
// this server can send, and no 406 for the generic RFC 8742 type, which a client
// MUST accept as equivalent.
func TestContentNegotiation(t *testing.T) {
	pub, blocks := testChain(t, 13, 2)
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})
	path := DefaultPrefix + "/chains/" + authorText(t, pub) + "/tip"

	refused := get(t, ts, http.MethodGet, path, http.Header{"Accept": []string{"text/plain"}})
	if refused.StatusCode != http.StatusNotAcceptable {
		t.Errorf("Accept: text/plain = %d, want 406", refused.StatusCode)
	}
	assertProblem(t, refused, http.StatusNotAcceptable)

	for _, accept := range []string{MediaTypeBlocks, MediaTypeCBORSeq, "*/*", "application/*", ""} {
		header := http.Header{}
		if accept != "" {
			header.Set("Accept", accept)
		}
		resp := get(t, ts, http.MethodGet, path, header)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Accept: %q = %d, want 200", accept, resp.StatusCode)
		}
	}
}

// TestFetchRequestRules covers the blocks operation's own rules: the media type
// of its body, the floor on how many digests a conforming server accepts, the
// ceiling it MAY impose, and the duplicate a request MUST NOT contain
// (spec/07-transport.md, "blocks"; "Resource limits").
func TestFetchRequestRules(t *testing.T) {
	pub, blocks := testChain(t, 14, 2)
	_, ts := serve(t, ServerConfig{Store: memStore(t, blocks...)})
	_ = pub
	path := DefaultPrefix + "/blocks/fetch"

	t.Run("the body is JSON", func(t *testing.T) {
		resp := post(t, ts, path, MediaTypeBlocks, []byte(`{"digests":[]}`))
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415", resp.StatusCode)
		}
		assertProblem(t, resp, http.StatusUnsupportedMediaType)
	})

	t.Run("at least 256 digests are accepted", func(t *testing.T) {
		// A conforming server MUST accept a request naming at least 256
		// digests, so that the scan limit's default fits in one exchange.
		texts := make([]string, 0, MinBatchDigests)
		for i := range MinBatchDigests {
			var d cid.Digest
			d[0], d[1] = byte(i), byte(i>>8)
			texts = append(texts, d.CID().String())
		}
		texts[0] = blocks[0].CID().String()
		body, err := json.Marshal(fetchRequest{Digests: texts})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp := post(t, ts, path, MediaTypeJSON, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 for %d digests", resp.StatusCode, MinBatchDigests)
		}
		raw, _ := io.ReadAll(resp.Body)
		got, err := DecodeSeq(raw)
		if err != nil {
			t.Fatalf("DecodeSeq: %v", err)
		}
		if len(got) != 1 || got[0].Digest() != blocks[0].Digest() {
			t.Errorf("the response holds %v, want the one held block", digests(got))
		}
	})

	t.Run("more than the server accepts is 413", func(t *testing.T) {
		texts := make([]string, 0, MaxBatchDigests+1)
		for i := range MaxBatchDigests + 1 {
			var d cid.Digest
			d[0], d[1] = byte(i), byte(i>>8)
			texts = append(texts, d.CID().String())
		}
		body, err := json.Marshal(fetchRequest{Digests: texts})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp := post(t, ts, path, MediaTypeJSON, body)
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", resp.StatusCode)
		}
		assertProblem(t, resp, http.StatusRequestEntityTooLarge)
	})

	t.Run("a duplicated digest is refused", func(t *testing.T) {
		c := blocks[0].CID().String()
		resp := post(t, ts, path, MediaTypeJSON, []byte(`{"digests":["`+c+`","`+c+`"]}`))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("an unknown member is refused", func(t *testing.T) {
		resp := post(t, ts, path, MediaTypeJSON, []byte(`{"digests":[],"limit":3}`))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
}

// TestBodyBound: an announce body and a fetch body are attacker-chosen sizes,
// and each costs the attacker one request, so a server MUST bound them.
func TestBodyBound(t *testing.T) {
	_, blocks := testChain(t, 15, 1)
	store := block.NewValidatingStore(nil)
	_, ts := serve(t, ServerConfig{Store: store, Announce: StoreAnnouncer(store), MaxBodyBytes: 32})

	resp := post(t, ts, DefaultPrefix+"/announce", MediaTypeBlocks, blocks[0].Bytes())
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 for a body over the bound", resp.StatusCode)
	}
	assertProblem(t, resp, http.StatusRequestEntityTooLarge)
}

// TestServerServesOneBranchOfAFork: a source that holds a fork MUST answer tip
// with exactly one of the candidates and MUST answer range along one branch
// only, consistently with what its tip reports. Untangling a fork is siblings'
// job (spec/07-transport.md, "tip"; "range"; "siblings").
func TestServerServesOneBranchOfAFork(t *testing.T) {
	pub, genesis, branches := forkedChain(t)
	prev, _ := branches[0].Prev()
	store := memStore(t, append([]*block.Block{genesis}, branches...)...)
	forkClient, _ := serve(t, ServerConfig{Store: store})

	tip, err := forkClient.Tip(t.Context(), pub, "")
	if err != nil {
		t.Fatalf("Tip: %v", err)
	}
	result, err := forkClient.Range(t.Context(), pub, nil, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	last := result.Blocks[len(result.Blocks)-1]
	if last.Digest() != tip.Block.Digest() {
		t.Errorf("the range ends at %s and tip says %s: a server must be consistent across the two", last.Digest(), tip.Block.Digest())
	}
	if len(result.Blocks) != 2 {
		t.Errorf("the range is %d blocks, want the genesis block and one branch: branches must not be interleaved", len(result.Blocks))
	}

	// The branch choice MUST be deterministic and stable per author: for as long
	// as a source holds the same blocks, every tip and every range follows the
	// same branch. Here that is checked twice — repeated requests to one server,
	// and a second server holding the same blocks in the opposite insertion order
	// (spec/07-transport.md, "tip"; todos/086).
	for range 3 {
		again, err := forkClient.Tip(t.Context(), pub, "")
		if err != nil {
			t.Fatalf("Tip: %v", err)
		}
		if again.Block.Digest() != tip.Block.Digest() {
			t.Fatalf("tip moved between requests: %s then %s", tip.Block.Digest(), again.Block.Digest())
		}
	}
	reversed := memStore(t, append([]*block.Block{branches[1], branches[0]}, genesis)...)
	otherClient, _ := serve(t, ServerConfig{Store: reversed})
	other, err := otherClient.Tip(t.Context(), pub, "")
	if err != nil {
		t.Fatalf("Tip from the second server: %v", err)
	}
	if other.Block.Digest() != tip.Block.Digest() {
		t.Errorf("two servers holding the same blocks report %s and %s", tip.Block.Digest(), other.Block.Digest())
	}

	// siblings shows both, in ascending digest order, with no winner chosen.
	siblings, err := forkClient.Siblings(t.Context(), pub, &prev)
	if err != nil {
		t.Fatalf("Siblings: %v", err)
	}
	want := digests(branches)
	slices.SortFunc(want, func(a, b cid.Digest) int { return strings.Compare(string(a[:]), string(b[:])) })
	if !slices.Equal(digests(siblings), want) {
		t.Errorf("the sibling set is %v, want both branches ascending %v", digests(siblings), want)
	}
}

// post issues a POST against a test server.
func post(t *testing.T, ts *httptest.Server, path, contentType string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+path, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// assertProblem checks that an error body is RFC 9457 problem details under the
// right media type, and returns it (spec/07-transport.md, "Bodies and content
// types").
func assertProblem(t *testing.T, resp *http.Response, status int) Problem {
	t.Helper()
	if ct := resp.Header.Get("Content-Type"); ct != MediaTypeProblem {
		t.Errorf("error Content-Type = %q, want %q", ct, MediaTypeProblem)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the problem body: %v", err)
	}
	var p Problem
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("the error body is not JSON: %v (%q)", err, raw)
	}
	if p.Status != status {
		t.Errorf("problem status member = %d, want %d", p.Status, status)
	}
	if p.Type != ProblemTypeBlank && p.Type != ProblemNotHeld && p.Type != ProblemOperationNotOffered {
		t.Errorf("problem type = %q, want about:blank or one of the two the profile defines", p.Type)
	}
	if p.Title == "" {
		t.Error("the problem has no title")
	}
	return p
}
