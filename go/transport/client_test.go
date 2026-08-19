package transport

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// lying serves a handler that answers everything with one canned response. It is
// how the client's verification obligations are tested: the point of those
// obligations is that they hold against a source that is not telling the truth
// (spec/07-transport.md, "Verification obligations").
func lying(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	client, err := NewClient(ts.URL+DefaultPrefix, &ClientConfig{HTTP: ts.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// blockSeq writes a block sequence response by hand.
func blockSeq(w http.ResponseWriter, blocks ...*block.Block) {
	w.Header().Set("Content-Type", MediaTypeBlocks)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(EncodeSeq(blocks))
}

// TestClientIdentifiesBlocksByHashing: a block response whose bytes hash to
// something other than the requested digest is a failed fetch, not a block
// (spec/07-transport.md, "Verification obligations", rule 1).
func TestClientIdentifiesBlocksByHashing(t *testing.T) {
	_, wanted := testChain(t, 20, 1)
	_, substituted := testChain(t, 21, 1)

	client := lying(t, func(w http.ResponseWriter, _ *http.Request) { blockSeq(w, substituted[0]) })

	_, err := client.Block(t.Context(), wanted[0].Digest())
	if err == nil {
		t.Fatal("the client accepted a block that is not the one it asked for")
	}
	// It is a fetch that did not succeed, which is the verdict-preserving
	// outcome: the client has not learned that anything is invalid.
	if !errors.Is(err, ErrNotHeld) || !errors.Is(err, block.ErrNotFound) {
		t.Errorf("err = %v, want a failed fetch wrapping ErrNotFound", err)
	}
}

// TestClientChecksTheRangeProperty: a client MUST verify the range property for
// itself, by checking that each block's prev names the block before it and that
// the first block's prev names the position it asked about (rule 2).
func TestClientChecksTheRangeProperty(t *testing.T) {
	pub, blocks := testChain(t, 22, 4)

	skipping := lying(t, func(w http.ResponseWriter, _ *http.Request) {
		blockSeq(w, blocks[0], blocks[2], blocks[3]) // block 1 withheld
	})
	if _, err := skipping.Range(t.Context(), pub, nil, 0); err == nil {
		t.Error("the client accepted a range with a skipped block")
	}

	misplaced := lying(t, func(w http.ResponseWriter, _ *http.Request) {
		blockSeq(w, blocks...) // the whole chain, for a request that asked after block 1
	})
	after := blocks[1].Digest()
	if _, err := misplaced.Range(t.Context(), pub, &after, 0); err == nil {
		t.Error("the client accepted a range that does not begin where it asked")
	}
}

// TestClientRejectsUnrequestedBlocks: a batch response is matched by re-hashing,
// never by position, so a block nobody asked for has no place to be matched to.
func TestClientRejectsUnrequestedBlocks(t *testing.T) {
	_, wanted := testChain(t, 23, 1)
	_, extra := testChain(t, 24, 1)

	client := lying(t, func(w http.ResponseWriter, _ *http.Request) { blockSeq(w, wanted[0], extra[0]) })
	if _, err := client.Blocks(t.Context(), []cid.Digest{wanted[0].Digest()}); err == nil {
		t.Error("the client accepted a batch carrying a block it did not ask for")
	}

	// And it refuses to send a request naming one digest twice.
	d := wanted[0].Digest()
	if _, err := client.Blocks(t.Context(), []cid.Digest{d, d}); err == nil {
		t.Error("the client sent a request naming a digest twice")
	}
}

// TestClientRefusesTheWrongMediaType: a server MUST NOT serve a block sequence
// under any other type, and a body under another type is not a sequence this
// client decodes.
func TestClientRefusesTheWrongMediaType(t *testing.T) {
	pub, blocks := testChain(t, 25, 1)
	client := lying(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(EncodeSeq(blocks))
	})
	if _, err := client.Tip(t.Context(), pub, ""); err == nil {
		t.Error("the client decoded a block sequence served under another media type")
	}

	// The generic RFC 8742 type is the one it MUST accept as equivalent.
	generic := lying(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", MediaTypeCBORSeq)
		_, _ = w.Write(EncodeSeq(blocks))
	})
	if _, err := generic.Tip(t.Context(), pub, ""); err != nil {
		t.Errorf("the client refused %s, which it must accept: %v", MediaTypeCBORSeq, err)
	}
}

// TestClientRefusesATipFromAnotherAuthor: the tip of a chain is a block of that
// chain, and the client reads the author from the block rather than from the URL
// it used.
func TestClientRefusesATipFromAnotherAuthor(t *testing.T) {
	pub, _ := testChain(t, 26, 1)
	_, other := testChain(t, 27, 1)
	client := lying(t, func(w http.ResponseWriter, _ *http.Request) { blockSeq(w, other[0]) })
	if _, err := client.Tip(t.Context(), pub, ""); err == nil {
		t.Error("the client accepted another author's block as this author's tip")
	}
}

// TestClientReadsTheTipClaim: the Dialog-Tip header is a claim the client may
// use for one purpose — deciding whether to ask for more — and a malformed one
// is a server that cannot be taken at its word about contiguity either.
func TestClientReadsTheTipClaim(t *testing.T) {
	pub, blocks := testChain(t, 28, 2)
	client := lying(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(HeaderTip, "not-a-cid")
		blockSeq(w, blocks...)
	})
	if _, err := client.Range(t.Context(), pub, nil, 0); err == nil {
		t.Error("the client accepted a malformed Dialog-Tip")
	}

	silent := lying(t, func(w http.ResponseWriter, _ *http.Request) { blockSeq(w, blocks...) })
	result, err := silent.Range(t.Context(), pub, nil, 0)
	if err != nil {
		t.Fatalf("a range without a tip claim: %v", err)
	}
	if result.Tip != nil || result.AtTip() {
		t.Error("a range with no tip claim reported one")
	}
}

// TestClientMapsNotFound: 404 is ErrNotHeld and, through it,
// block.ErrNotFound — so a failed fetch reaching validation leaves the block
// that named it stored but unvalidated rather than invalid.
func TestClientMapsNotFound(t *testing.T) {
	_, blocks := testChain(t, 29, 1)
	client := lying(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "this source does not hold that block")
	})
	_, err := client.Block(t.Context(), blocks[0].Digest())
	if !errors.Is(err, ErrNotHeld) || !errors.Is(err, block.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotHeld and ErrNotFound", err)
	}
	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want a StatusError carrying 404", err)
	}
	if status.Problem == nil || status.Problem.Status != http.StatusNotFound {
		t.Errorf("the problem details did not survive: %+v", status.Problem)
	}
}

// TestClientBoundsTheResponse: a client MUST bound what it will read.
func TestClientBoundsTheResponse(t *testing.T) {
	pub, blocks := testChain(t, 30, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		blockSeq(w, blocks...)
	}))
	t.Cleanup(ts.Close)
	client, err := NewClient(ts.URL+DefaultPrefix, &ClientConfig{HTTP: ts.Client(), MaxBodyBytes: 16})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Range(t.Context(), pub, nil, 0); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

// TestNewClientRejectsANonURL: a client is configured with the whole base URL.
func TestNewClientRejectsANonURL(t *testing.T) {
	for _, base := range []string{"", "example.com/dialog/v1", "ftp://example.com", "://"} {
		if _, err := NewClient(base, nil); err == nil {
			t.Errorf("NewClient(%q) succeeded", base)
		}
	}
	c, err := NewClient("https://mirror.example/dialog/v1/", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.BaseURL() != "https://mirror.example/dialog/v1" {
		t.Errorf("BaseURL = %q, want the trailing slash gone", c.BaseURL())
	}
	if !strings.Contains(c.String(), "mirror.example") {
		t.Errorf("String = %q, want a client that names itself by its base URL", c.String())
	}
}

// TestAnnounceRoundTrip covers the receipt in all three of its members:
// accepted, held, and rejected, with every submitted block in exactly one of
// them (spec/07-transport.md, "Bodies and content types").
func TestAnnounceRoundTrip(t *testing.T) {
	_, alice := testChain(t, 31, 3)
	store := block.NewValidatingStore(nil)
	client, _ := serve(t, ServerConfig{Store: store, Announce: StoreAnnouncer(store)})

	// The genesis block and its successor: both accepted, in chain order.
	receipt, err := client.Announce(t.Context(), alice[:2])
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(receipt.Accepted) != 2 || len(receipt.Held) != 0 || len(receipt.Rejected) != 0 {
		t.Fatalf("receipt = %+v, want two accepted", receipt)
	}
	if receipt.Accepted[0] != alice[0].Digest() {
		t.Errorf("the receipt names %s first, want the genesis block", receipt.Accepted[0])
	}

	// A block whose predecessor this source does not hold: held, not refused.
	// A server MUST NOT store as valid a block whose predecessor it has not
	// validated, and it MUST NOT call the block invalid either.
	_, bob := testChain(t, 32, 3)
	held, err := client.Announce(t.Context(), bob[2:])
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(held.Held) != 1 || held.Held[0] != bob[2].Digest() {
		t.Fatalf("receipt = %+v, want the orphan block held", held)
	}

	// A block that is invalid on its own terms: rejected, with a reason in
	// prose meant for a person.
	orphanMolecule := invalidBlock(t, 33)
	refused, err := client.Announce(t.Context(), []*block.Block{orphanMolecule})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(refused.Rejected) != 1 || refused.Rejected[0].Digest != orphanMolecule.Digest() {
		t.Fatalf("receipt = %+v, want the block rejected", refused)
	}
	if refused.Rejected[0].Reason == "" {
		t.Error("a rejection carries no reason")
	}

	// A block the server already held is reported as accepted.
	again, err := client.Announce(t.Context(), alice[:1])
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(again.Accepted) != 1 {
		t.Errorf("re-announcing a held block = %+v, want it accepted", again)
	}

	// And a held block settles when its ancestry follows, which the receipt
	// reports for the whole sequence rather than block by block.
	settled, err := client.Announce(t.Context(), bob[:2])
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(settled.Accepted) != 2 {
		t.Errorf("receipt = %+v, want both accepted", settled)
	}
	if !store.Accepted(bob[2].Digest()) {
		t.Error("the block held earlier was not settled by its ancestry arriving")
	}
}

// TestAnnounceDispositionsAreDecidedAfterTheSequence: a block settled by a later
// block of the same announce is reported by its final state. The announcer's
// interleaving is legal — each author's own blocks are in chain order — and a
// receipt built block by block would report the first block as held, a verdict
// the source had already moved past when it wrote the response
// (spec/07-transport.md, "announce"; todos/088).
func TestAnnounceDispositionsAreDecidedAfterTheSequence(t *testing.T) {
	_, definition, _, use := definitionAndUse(t, 35, 36)
	store := block.NewValidatingStore(nil)
	client, _ := serve(t, ServerConfig{Store: store, Announce: StoreAnnouncer(store)})

	// The using block first, then the chain that defines what it names: it is
	// undecidable when offered and accepted by the time the sequence is done.
	sequence := []*block.Block{use, definition[0], definition[1]}
	receipt, err := client.Announce(t.Context(), sequence)
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(receipt.Accepted) != 3 || len(receipt.Held) != 0 || len(receipt.Rejected) != 0 {
		t.Fatalf("receipt = %+v, want all three accepted", receipt)
	}
	if receipt.Accepted[0] != use.Digest() {
		t.Errorf("the receipt names %s first, want the using block %s", receipt.Accepted[0], use.Digest())
	}

	// And the same sequence announced again gives the same receipt, which is the
	// other half of what deciding after the sequence buys.
	again, err := client.Announce(t.Context(), sequence)
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if !slices.Equal(again.Accepted, receipt.Accepted) || len(again.Held) != 0 || len(again.Rejected) != 0 {
		t.Errorf("re-announcing the same sequence = %+v, want the first receipt %+v", again, receipt)
	}
}

// TestAnnounceMediaTypes: an announce body is a block sequence, and the two
// types a block sequence travels under are equivalent in a request as in a
// response — anything else is 415. Accept is not evaluated here at all, because
// the only body this operation answers with is JSON and the server produces it
// whatever the request asked for (spec/07-transport.md, "Bodies and content
// types"; todos/094).
func TestAnnounceMediaTypes(t *testing.T) {
	_, blocks := testChain(t, 61, 2)
	store := block.NewValidatingStore(nil)
	_, ts := serve(t, ServerConfig{Store: store, Announce: StoreAnnouncer(store)})
	path := DefaultPrefix + "/announce"
	body := EncodeSeq(blocks[:1])

	t.Run("the generic RFC 8742 type is admitted", func(t *testing.T) {
		// This is the chain file a plain file server handed over, offered as an
		// announce body under the type that file server gave it.
		resp := post(t, ts, path, MediaTypeCBORSeq, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != MediaTypeJSON {
			t.Errorf("receipt Content-Type = %q, want %q", ct, MediaTypeJSON)
		}
	})

	t.Run("the profile's own type is admitted", func(t *testing.T) {
		if resp := post(t, ts, path, MediaTypeBlocks, body); resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	for _, contentType := range []string{MediaTypeJSON, "application/octet-stream", "text/plain", ""} {
		resp := post(t, ts, path, contentType, body)
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type: %q = %d, want 415", contentType, resp.StatusCode)
			continue
		}
		assertProblem(t, resp, http.StatusUnsupportedMediaType)
	}

	t.Run("Accept is not evaluated", func(t *testing.T) {
		// The standing Accept header of a client that speaks this profile names
		// the block-sequence type, which no announce response ever carries. A
		// server enforcing 406 uniformly would refuse a write it would
		// otherwise take.
		for _, accept := range []string{MediaTypeBlocks + ", " + MediaTypeCBORSeq, "text/plain", "image/png"} {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+path, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			req.Header.Set("Content-Type", MediaTypeBlocks)
			req.Header.Set("Accept", accept)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("POST %s: %v", path, err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Accept: %q = %d, want 200", accept, resp.StatusCode)
			}
		}
	})
}

// invalidBlock builds a block that is invalid on its own terms: a molecule whose
// bond is defined nowhere, with no refs to explain the gap, which rule 4 refuses
// definitively rather than leaving undecided.
func invalidBlock(t *testing.T, seed byte) *block.Block {
	t.Helper()
	author := testBuilder(t, seed)
	bond := mustBond(t, "_A_ is the capital of _B_")
	paris := mustAtom(t, "Paris, the capital of France")
	france := mustAtom(t, "France")
	b, err := author.Public(1, nil,
		block.MustCreateAtom(paris.Description()),
		block.MustCreateAtom(france.Description()),
		block.MustCreateMolecule(bond, fillersOf(paris, france)))
	if err != nil {
		t.Fatalf("building the invalid block: %v", err)
	}
	return b
}
