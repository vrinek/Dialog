package transport

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// testKey returns a deterministic Ed25519 key. The seed is a constant because
// every byte in these tests has to be reproducible; nothing here is a key
// anybody signs with.
func testKey(t *testing.T, seed byte) ed25519.PrivateKey {
	t.Helper()
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = seed
	}
	return ed25519.NewKeyFromSeed(raw)
}

// testBuilder returns a builder over the key with that seed.
func testBuilder(t *testing.T, seed byte) *block.Builder {
	t.Helper()
	b, err := block.NewBuilder(testKey(t, seed))
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	return b
}

// testChain builds n public blocks of one author, each creating one atom, and
// returns them genesis first.
func testChain(t *testing.T, seed byte, n int) (ed25519.PublicKey, []*block.Block) {
	t.Helper()
	author := testBuilder(t, seed)
	blocks := make([]*block.Block, 0, n)
	for i := range n {
		b, err := author.Public(uint64(1000+i), nil, block.MustCreateAtom(atomText(seed, i)))
		if err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
		blocks = append(blocks, b)
	}
	return blocks[0].PublicKey(), blocks
}

// atomText gives every test atom a description of its own, so that no two
// blocks of two chains collide by accident.
func atomText(seed byte, i int) string {
	return string(rune('A'+seed)) + " block " + itoa(i)
}

// memStore returns a MemStore holding the blocks.
func memStore(t *testing.T, blocks ...*block.Block) *block.MemStore {
	t.Helper()
	s := block.NewMemStore()
	for _, b := range blocks {
		var fork *block.ForkError
		// A fixture that stores a fork stores it on purpose: the store's policy
		// is accept-and-flag, and several of these tests are about what a
		// server does when it holds one.
		if err := s.Add(b); err != nil && !errors.As(err, &fork) {
			t.Fatalf("storing %s: %v", b.Digest(), err)
		}
	}
	return s
}

// serve mounts a server over a store on an httptest server and returns a client
// for it. The server is closed when the test ends, which is also what proves no
// request outlives its test.
func serve(t *testing.T, cfg ServerConfig) (*Client, *httptest.Server) {
	t.Helper()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = DefaultPrefix
	}
	client, err := NewClient(ts.URL+prefix, &ClientConfig{HTTP: ts.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, ts
}

// get issues a raw request against a test server, for the conformance checks
// that are about headers and status codes rather than about blocks.
func get(t *testing.T, ts *httptest.Server, method, path string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	for key, values := range header {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// authorText renders a key the way a URL carries it.
func authorText(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	s, err := cid.AuthorKeyText(pub)
	if err != nil {
		t.Fatalf("author key: %v", err)
	}
	return s
}

// mustBond, mustAtom and fillersOf build the entities a molecule needs, so that
// a test can say what a block asserts rather than how the entity package spells
// it.
func mustBond(t *testing.T, template string) entity.Bond {
	t.Helper()
	return entity.MustBond(template)
}

func mustAtom(t *testing.T, description string) entity.Atom {
	t.Helper()
	return entity.MustAtom(description)
}

func fillersOf(atoms ...entity.Atom) []entity.Filler {
	out := make([]entity.Filler, 0, len(atoms))
	for _, a := range atoms {
		out = append(out, entity.AtomFiller(a.Digest()))
	}
	return out
}

// definitionAndUse builds two chains that only make sense together: the first
// author's second block defines a bond, and the second author's block names it
// in refs and asserts a molecule that needs it. It is the shape demand-driven
// resolution exists for — a block whose validity depends on a block of a chain
// this node may not follow.
func definitionAndUse(t *testing.T, defSeed, useSeed byte) (defPub ed25519.PublicKey, definition []*block.Block, usePub ed25519.PublicKey, use *block.Block) {
	t.Helper()
	template := "_A_ is the capital of _B_"
	bond := mustBond(t, template)
	paris := mustAtom(t, "Paris, the capital of France")
	france := mustAtom(t, "France")

	author := testBuilder(t, defSeed)
	genesis, err := author.Public(1, nil, block.MustCreateAtom("the definer's first block"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	provider, err := author.Public(2, nil, block.MustCreateBond(template))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	user := testBuilder(t, useSeed)
	b, err := user.Public(3, []cid.Digest{provider.Digest()},
		block.MustCreateAtom(paris.Description()),
		block.MustCreateAtom(france.Description()),
		block.MustCreateMolecule(bond, fillersOf(paris, france)))
	if err != nil {
		t.Fatalf("the using block: %v", err)
	}
	definerKey, userKey := genesis.PublicKey(), b.PublicKey()
	return definerKey, []*block.Block{genesis, provider}, userKey, b
}

// digests maps blocks to their digests, for comparing two sequences by identity.
func digests(blocks []*block.Block) []cid.Digest {
	out := make([]cid.Digest, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Digest())
	}
	return out
}
