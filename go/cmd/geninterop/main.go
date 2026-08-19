// Command geninterop writes the interop harness's fixtures and expectations.
//
// The harness in interop/ runs each implementation's server against the other's
// client, and asserts that both directions produce the same summary document.
// Two of the things it needs cannot be produced by the harness itself:
//
//   - **Fork fixtures.** demo/chains/ is three honest, linear chains, which is
//     the case a fork test is not. The two scenarios here are the two shapes of
//     divergence the profile has to be able to see: a chain that forks after a
//     shared prefix, and two chains claiming one author with no block in common
//     at all — the genesis fork of spec/07-transport.md, "Pursuing an advertised
//     tip", and validation rule 9's own condition at the genesis position.
//   - **Expectations.** What a conforming client MUST end up holding, computed
//     from the fixtures rather than recorded from a client, so that it is
//     something both implementations are measured against rather than something
//     one of them defines.
//
// Usage:
//
//	go run ./cmd/geninterop [-root DIR] [-check]
//
//	-root   the repository root, defaulting to the parent of go/
//	-check  rebuild everything in memory and compare, writing nothing; exits
//	        non-zero on any drift
//
// Everything it writes is generated and never hand-edited, under the rule
// vectors/ and demo/chains/ already live by: regeneration is byte-identical, and
// a diff means a digest moved. The keys are seeded constants — they sign nothing
// anybody relies on, and they are constants so that every byte of every fixture
// is reproducible.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/internal/chains"
	"github.com/vrinek/Dialog/go/internal/interop"
)

func main() {
	root := flag.String("root", "..", "the repository root")
	check := flag.Bool("check", false, "rebuild in memory and compare, writing nothing")
	flag.Parse()

	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, "geninterop:", err)
		os.Exit(1)
	}
}

// A file is one artifact this generator owns, by its path relative to the
// repository root.
type file struct {
	path string
	body []byte
}

func run(root string, check bool) error {
	files, err := build(root)
	if err != nil {
		return err
	}
	if check {
		return compare(root, files)
	}
	for _, f := range files {
		full := filepath.Join(root, f.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(full, f.body, 0o644); err != nil { // #nosec G306 -- committed fixtures are world-readable by design
			return err
		}
	}
	fmt.Printf("geninterop: wrote %d files under %s\n", len(files), filepath.Join(root, "interop"))
	return nil
}

// compare reports the first difference between what is on disk and what this
// generator would write.
func compare(root string, files []file) error {
	for _, f := range files {
		full := filepath.Join(root, f.path)
		have, err := os.ReadFile(full) // #nosec G304 -- the path is this generator's own
		if err != nil {
			return fmt.Errorf("%s is missing; run 'go run ./cmd/geninterop': %w", f.path, err)
		}
		if !bytes.Equal(have, f.body) {
			return fmt.Errorf("%s is not what the generator writes; run 'go run ./cmd/geninterop' and review the diff", f.path)
		}
	}
	fmt.Printf("geninterop: %d files are current\n", len(files))
	return nil
}

// build assembles every artifact: the two fixture scenarios, and the three
// expectations, one of which is over demo/chains rather than over a fixture of
// this generator's own.
func build(root string) ([]file, error) {
	var out []file

	forkA, forkB := forkScenario()
	out = append(out, blockFiles("interop/fixtures/fork-a", forkA)...)
	out = append(out, blockFiles("interop/fixtures/fork-b", forkB)...)
	forked, err := expect([][]*block.Block{forkA, forkB}, []ed25519.PublicKey{forkA[0].PublicKey()})
	if err != nil {
		return nil, err
	}
	out = append(out, file{path: "interop/expected/fork.json", body: forked})

	genesisA, genesisB := genesisScenario()
	out = append(out, blockFiles("interop/fixtures/genesis-a", genesisA)...)
	out = append(out, blockFiles("interop/fixtures/genesis-b", genesisB)...)
	split, err := expect([][]*block.Block{genesisA, genesisB}, []ed25519.PublicKey{genesisA[0].PublicKey()})
	if err != nil {
		return nil, err
	}
	out = append(out, file{path: "interop/expected/genesis.json", body: split})

	demo, pubs, err := demoChains(root)
	if err != nil {
		return nil, err
	}
	honest, err := expect([][]*block.Block{demo}, pubs)
	if err != nil {
		return nil, err
	}
	out = append(out, file{path: "interop/expected/demo.json", body: honest})

	return out, nil
}

// expect renders the summary a conforming client must produce.
func expect(sources [][]*block.Block, pubs []ed25519.PublicKey) ([]byte, error) {
	summary, err := interop.Expect(sources, pubs)
	if err != nil {
		return nil, err
	}
	return summary.JSON()
}

// blockFiles lays a set of blocks out as one file per block, numbered in the
// order given, which is chain order.
func blockFiles(dir string, blocks []*block.Block) []file {
	out := make([]file, 0, len(blocks))
	for i, b := range blocks {
		out = append(out, file{path: fmt.Sprintf("%s/%03d%s", dir, i, chains.Extension), body: b.Bytes()})
	}
	return out
}

// fixtureKey returns the private key of a fixture author. The seed is a
// constant: these keys sign fixtures and nothing else, and every byte they
// produce has to be reproducible.
func fixtureKey(seed byte) ed25519.PrivateKey {
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = seed
	}
	return ed25519.NewKeyFromSeed(raw)
}

// sign builds one public block and signs it.
func sign(priv ed25519.PrivateKey, ts uint64, prev *cid.Digest, text string) *block.Block {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		panic("geninterop: an Ed25519 private key without an Ed25519 public key")
	}
	b, err := block.Sign(block.Content{
		Version: block.Version,
		Type:    block.TypePublic,
		Pub:     pub,
		Prev:    prev,
		TS:      ts,
		Ops:     []block.Operation{block.MustCreateAtom(text)},
	}, priv)
	if err != nil {
		panic("geninterop: signing a fixture block: " + err.Error())
	}
	return b
}

// forkScenario builds the ordinary fork: one author, a shared prefix of two
// blocks, and one divergent block on each side.
//
// Each side is a complete, honest chain from the genesis block to its own tip,
// so neither server is misbehaving and neither can be caught at it. Only a
// client that asks both, and compares, sees anything at all — which is the
// multi-source rule's whole content (spec/07-transport.md, "The multi-source
// rule"; spec/02-block-format.md, "Validation" rule 9).
func forkScenario() (a, b []*block.Block) {
	priv := fixtureKey(11)
	genesis := sign(priv, 1, nil, "the block both sides agree on")
	first := genesis.Digest()
	shared := sign(priv, 2, &first, "the last block both sides agree on")
	at := shared.Digest()
	left := sign(priv, 3, &at, "one branch")
	right := sign(priv, 4, &at, "the other branch")
	return []*block.Block{genesis, shared, left}, []*block.Block{genesis, shared, right}
}

// genesisScenario builds the fundamental fork: two chains under one key that
// share no block at all.
//
// This is what a source serving a second genesis block for one author looks like
// from a client holding the first, and the two genesis blocks are a sibling set
// at the genesis position — a fork in the strict sense of validation rule 9, and
// where the ambiguous succession of spec/02-block-format.md, "rotate_key", is
// detected (spec/07-transport.md, "Pursuing an advertised tip").
func genesisScenario() (a, b []*block.Block) {
	priv := fixtureKey(22)
	chain := func(ts uint64, text string) []*block.Block {
		genesis := sign(priv, ts, nil, text)
		at := genesis.Digest()
		return []*block.Block{genesis, sign(priv, ts+1, &at, text+", continued")}
	}
	return chain(1, "one claim on this author"), chain(100, "another claim on this author")
}

// demoChains reads demo/chains and returns its blocks and its authors in
// publication order.
//
// The order comes from the directory's own manifest, which records it because it
// is the order the chains must be replayed in: a later chain's blocks reference
// an earlier chain's, and a client that asks for them the other way round is
// resolving references to blocks nobody has offered it yet. The manifest carries
// no other authority here — the blocks are decoded from the files, and every
// digest is recomputed.
func demoChains(root string) ([]*block.Block, []ed25519.PublicKey, error) {
	dir := filepath.Join(root, "demo", "chains")
	blocks, err := chains.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.json")) // #nosec G304 -- a path this generator builds
	if err != nil {
		return nil, nil, fmt.Errorf("geninterop: %w", err)
	}
	var index struct {
		Chains []struct {
			PublicKey string `json:"public_key"`
		} `json:"chains"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, nil, fmt.Errorf("geninterop: reading the demo chain manifest: %w", err)
	}
	var pubs []ed25519.PublicKey
	for _, entry := range index.Chains {
		key, err := cid.ParseAuthorKeyText(entry.PublicKey)
		if err != nil {
			return nil, nil, fmt.Errorf("geninterop: the demo chain manifest names %q: %w", entry.PublicKey, err)
		}
		pubs = append(pubs, ed25519.PublicKey(key))
	}
	held := chains.Authors(blocks)
	if len(pubs) != len(held) {
		return nil, nil, errors.New("geninterop: the demo chain manifest names a different set of authors than its blocks are signed by")
	}
	for _, pub := range held {
		if !slices.ContainsFunc(pubs, func(k ed25519.PublicKey) bool { return k.Equal(pub) }) {
			return nil, nil, fmt.Errorf("geninterop: %x signs a demo block and is not in the manifest", pub[:8])
		}
	}
	return blocks, pubs, nil
}
