// Package chainfile defines the on-disk shape of the committed demo chains and
// reads them back.
//
// # The format
//
// A chain directory holds one file per block plus an index:
//
//	index.json           the manifest below
//	atlas/000.block      the canonical dCBOR encoding of one signed block
//	atlas/001.block
//	gazetteer/000.block
//	...
//
// A .block file holds exactly the bytes block.Block.Bytes returns — the bytes
// that go on the wire and the bytes the block's digest is taken over. There is
// no envelope, no length prefix and no framing: a reader hands the whole file
// to block.Decode.
//
// index.json is the manifest. It exists so that a reader knows which files are
// blocks, which chain each belongs to and in what order, without parsing every
// file to find out; and so that a corrupted or substituted file is caught
// before it is decoded, by comparing the digest the index records against the
// digest of the bytes actually read. It carries no authority: every claim it
// makes about a block is checked against the block itself, and a reader that
// trusted it would still have to validate the chain.
//
// The JSON is written with two-space indentation and a trailing newline, and
// its fields are emitted in the order they are declared here, so a regenerated
// index is byte-identical to the committed one.
//
// This package does not know how the chains were built. Build takes decoded
// blocks and Read returns them, which is what lets the regeneration check
// compare the two sides without either of them being the other's authority.
package chainfile

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// Format is the value of the index's "format" field. It names the shape of the
// directory, not the protocol version, which is a block's v field.
const Format = "dialog-demo-chains/1"

// IndexName is the manifest's file name.
const IndexName = "index.json"

// A Chain is one author's blocks in memory: what Build writes out and what
// Read hands back.
type Chain struct {
	// Author is the demo name of the author, which is also the name of the
	// subdirectory their blocks live in.
	Author string
	// Pub is the key every block of the chain is signed with.
	Pub ed25519.PublicKey
	// Blocks are the chain's blocks, genesis first.
	Blocks []*block.Block
}

// An Index is the manifest of a chain directory.
type Index struct {
	// Format is Format.
	Format string `json:"format"`
	// Description says what the directory holds, in prose.
	Description string `json:"description"`
	// Generator is the command that wrote it.
	Generator string `json:"generator"`
	// Chains are the author chains, in publication order — the order they
	// must be replayed in, since a later chain's blocks reference an earlier
	// chain's.
	Chains []ChainEntry `json:"chains"`
}

// A ChainEntry is the manifest's record of one author chain.
type ChainEntry struct {
	// Author is the demo name of the author.
	Author string `json:"author"`
	// PublicKey is their Ed25519 public key in the canonical text form of
	// spec/03-encoding.md, "Text representation of author keys": 56
	// characters beginning "b5ua". The bytes behind it are the pub field of
	// every block of the chain, which stays 32 raw bytes on the wire; this is
	// how the key is written down when it names something, and here it names
	// the chain the entry files.
	PublicKey string `json:"public_key"`
	// Blocks are the chain's blocks, genesis first.
	Blocks []BlockEntry `json:"blocks"`
}

// A BlockEntry is the manifest's record of one block file.
type BlockEntry struct {
	// Index is the block's position in the chain, counting from zero.
	Index int `json:"index"`
	// File is the block's path, relative to the directory holding the index.
	File string `json:"file"`
	// Digest is SHA-256 of the file's bytes, hex-encoded: the block's
	// identity, and the value another block's prev or refs field carries.
	Digest string `json:"digest"`
	// CID is the same digest in the external base32 form.
	CID string `json:"cid"`
	// Ops is the number of operations the block carries.
	Ops int `json:"ops"`
	// Size is the length of the file in bytes.
	Size int `json:"size"`
}

// A File is one file of the chain directory: a path relative to the directory
// and the bytes it holds.
type File struct {
	Path  string
	Bytes []byte
}

// Build renders the chains as the files of a chain directory: the index first,
// then every block file in publication order.
//
// It is a pure function of its input, so writing the files and checking the
// committed ones against them are the same computation.
func Build(chains []Chain) ([]File, error) {
	index := Index{
		Format:      Format,
		Description: "The Dialog grounding demo's three author chains, one file per block.",
		Generator:   "go run ./cmd/genchains",
	}
	files := make([]File, 1, 1+countBlocks(chains))
	for _, c := range chains {
		key, err := cid.AuthorKeyText(c.Pub)
		if err != nil {
			return nil, fmt.Errorf("chainfile: chain %s: %w", c.Author, err)
		}
		entry := ChainEntry{Author: c.Author, PublicKey: key}
		for i, b := range c.Blocks {
			raw := b.Bytes()
			name := path.Join(c.Author, fmt.Sprintf("%03d.block", i))
			d := b.Digest()
			entry.Blocks = append(entry.Blocks, BlockEntry{
				Index:  i,
				File:   name,
				Digest: hex.EncodeToString(d[:]),
				CID:    b.CID().String(),
				Ops:    len(b.Ops()),
				Size:   len(raw),
			})
			files = append(files, File{Path: name, Bytes: raw})
		}
		index.Chains = append(index.Chains, entry)
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("chainfile: encoding the index: %w", err)
	}
	files[0] = File{Path: IndexName, Bytes: append(raw, '\n')}
	return files, nil
}

// keyText renders an author key in the canonical text form of
// spec/03-encoding.md, "Text representation of author keys", for a message that
// names a key. The fallback is unreachable for a key that came out of a decoded
// block — those are 32 bytes by construction — and exists so that reporting one
// failure never has to report a second one instead.
func keyText(pub []byte) string {
	s, err := cid.AuthorKeyText(pub)
	if err != nil {
		return fmt.Sprintf("%d bytes that are not an author key", len(pub))
	}
	return s
}

func countBlocks(chains []Chain) int {
	n := 0
	for _, c := range chains {
		n += len(c.Blocks)
	}
	return n
}

// Read reads a chain directory: it parses the index, decodes every block file
// it names, and checks each block against what the index claims about it.
//
// Decoding is block.Decode, so every file goes through the full L1 structural
// check — the dCBOR profile, the field set, the operation shapes, the Ed25519
// signature — before Read returns it. What Read does not do is validate the
// chain: linkage, reachability and fork detection need a store, and belong to
// the replay package.
func Read(fsys fs.FS) (Index, []Chain, error) {
	raw, err := fs.ReadFile(fsys, IndexName)
	if err != nil {
		return Index{}, nil, fmt.Errorf("chainfile: reading %s: %w", IndexName, err)
	}
	var index Index
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&index); err != nil {
		return Index{}, nil, fmt.Errorf("chainfile: parsing %s: %w", IndexName, err)
	}
	if index.Format != Format {
		return Index{}, nil, fmt.Errorf("chainfile: %s declares format %q, want %q", IndexName, index.Format, Format)
	}
	chains := make([]Chain, 0, len(index.Chains))
	for _, c := range index.Chains {
		chain, err := readChain(fsys, c)
		if err != nil {
			return Index{}, nil, err
		}
		chains = append(chains, chain)
	}
	if err := checkComplete(fsys, index); err != nil {
		return Index{}, nil, err
	}
	return index, chains, nil
}

func readChain(fsys fs.FS, c ChainEntry) (Chain, error) {
	pub, err := cid.ParseAuthorKeyText(c.PublicKey)
	if err != nil {
		return Chain{}, fmt.Errorf("chainfile: chain %s: parsing the public key: %w", c.Author, err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return Chain{}, fmt.Errorf("chainfile: chain %s: the public key is %d bytes, want %d", c.Author, len(pub), ed25519.PublicKeySize)
	}
	chain := Chain{Author: c.Author, Pub: pub}
	for i, entry := range c.Blocks {
		if entry.Index != i {
			return Chain{}, fmt.Errorf("chainfile: chain %s: block %d is indexed %d; the index must list a chain in order", c.Author, i, entry.Index)
		}
		want, err := cid.ParseDigestHex(entry.Digest)
		if err != nil {
			return Chain{}, fmt.Errorf("chainfile: chain %s block %d: parsing the digest: %w", c.Author, i, err)
		}
		raw, err := fs.ReadFile(fsys, entry.File)
		if err != nil {
			return Chain{}, fmt.Errorf("chainfile: reading %s: %w", entry.File, err)
		}
		if len(raw) != entry.Size {
			return Chain{}, fmt.Errorf("chainfile: %s is %d bytes, the index says %d", entry.File, len(raw), entry.Size)
		}
		if got := cid.SumDigest(raw); got != want {
			return Chain{}, fmt.Errorf("chainfile: %s hashes to %s, the index says %s", entry.File, got, want)
		}
		b, err := block.Decode(raw)
		if err != nil {
			return Chain{}, fmt.Errorf("chainfile: decoding %s: %w", entry.File, err)
		}
		if !bytes.Equal(b.PublicKey(), pub) {
			return Chain{}, fmt.Errorf("chainfile: %s is signed by %s, but the index files it under %s (%s)", entry.File, keyText(b.PublicKey()), c.Author, c.PublicKey)
		}
		if got := len(b.Ops()); got != entry.Ops {
			return Chain{}, fmt.Errorf("chainfile: %s carries %d operations, the index says %d", entry.File, got, entry.Ops)
		}
		chain.Blocks = append(chain.Blocks, b)
	}
	return chain, nil
}

// checkComplete reports a .block file the index does not list. An unlisted
// block would be silently ignored on replay, which is exactly the kind of
// difference between the committed bytes and the loaded world the index exists
// to rule out.
func checkComplete(fsys fs.FS, index Index) error {
	listed := make(map[string]bool)
	for _, c := range index.Chains {
		for _, b := range c.Blocks {
			listed[b.File] = true
		}
	}
	var stray []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".block" {
			return nil
		}
		if !listed[p] {
			stray = append(stray, p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("chainfile: walking the chain directory: %w", err)
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		return fmt.Errorf("chainfile: %v are not listed in %s", stray, IndexName)
	}
	return nil
}
