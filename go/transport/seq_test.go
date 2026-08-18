package transport

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// TestSeqIsConcatenation is the format in one assertion: a block sequence is the
// blocks' own bytes, joined, with nothing added
// (spec/07-transport.md, "The block sequence").
func TestSeqIsConcatenation(t *testing.T) {
	_, blocks := testChain(t, 1, 3)

	var want []byte
	for _, b := range blocks {
		want = append(want, b.Bytes()...)
	}
	got := EncodeSeq(blocks)
	if !bytes.Equal(got, want) {
		t.Fatalf("the sequence is %d bytes, the blocks are %d: a sequence has no framing", len(got), len(want))
	}
	if SeqLen(blocks) != len(want) {
		t.Errorf("SeqLen = %d, want %d", SeqLen(blocks), len(want))
	}

	back, err := DecodeSeq(got)
	if err != nil {
		t.Fatalf("DecodeSeq: %v", err)
	}
	if !slices.Equal(digests(back), digests(blocks)) {
		t.Errorf("the sequence round-tripped to %v, want %v", digests(back), digests(blocks))
	}
	for i, b := range back {
		if !bytes.Equal(b.Bytes(), blocks[i].Bytes()) {
			t.Errorf("block %d came back with different bytes", i)
		}
	}
}

// TestEmptySeq: an empty sequence is a zero-length byte string, and it is a
// valid answer meaning "none".
func TestEmptySeq(t *testing.T) {
	if got := EncodeSeq(nil); len(got) != 0 {
		t.Errorf("the empty sequence is %d bytes, want 0", len(got))
	}
	blocks, err := DecodeSeq(nil)
	if err != nil || len(blocks) != 0 {
		t.Errorf("DecodeSeq(nil) = %v, %v; want no blocks and no error", blocks, err)
	}
}

// TestTruncatedFinalItemIsAnError is rule 3 of the format, and it is the rule
// that keeps a cut connection from reading as a short chain.
func TestTruncatedFinalItemIsAnError(t *testing.T) {
	_, blocks := testChain(t, 2, 2)
	full := EncodeSeq(blocks)

	for _, cut := range []int{1, len(blocks[0].Bytes()) + 1, len(full) - 1} {
		if _, err := DecodeSeq(full[:cut]); err == nil {
			t.Errorf("DecodeSeq of %d bytes of a %d-byte sequence succeeded; a truncated final item is an error, not an end", cut, len(full))
		}
	}
}

// TestNonBlockItemFailsTheWholeSequence: a sequence containing anything that is
// not a block is malformed as a whole, so one bad item is not skipped.
func TestNonBlockItemFailsTheWholeSequence(t *testing.T) {
	_, blocks := testChain(t, 3, 1)
	// 0xa0 is an empty CBOR map: well-formed dCBOR, and not a block.
	seq := append(EncodeSeq(blocks), 0xa0)
	if _, err := DecodeSeq(seq); err == nil {
		t.Fatal("a sequence with a non-block item decoded; it is malformed as a whole")
	}
	if _, err := DecodeSeq([]byte{0xa0}); err == nil {
		t.Fatal("a sequence of one non-block item decoded")
	}
}

// TestReadSeqBound: a client MUST bound what it will read, because a response
// body can be arbitrarily large (spec/07-transport.md, Security Considerations).
func TestReadSeqBound(t *testing.T) {
	_, blocks := testChain(t, 4, 2)
	seq := EncodeSeq(blocks)

	if _, err := ReadSeq(bytes.NewReader(seq), int64(len(seq)-1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("ReadSeq over the bound = %v, want ErrTooLarge", err)
	}
	got, err := ReadSeq(bytes.NewReader(seq), int64(len(seq)))
	if err != nil {
		t.Fatalf("ReadSeq at exactly the bound: %v", err)
	}
	if len(got) != len(blocks) {
		t.Errorf("ReadSeq returned %d blocks, want %d", len(got), len(blocks))
	}
}

// TestFileIsTheSameBytes: a range response saved to disk is a valid chain file
// and a chain file is a valid announce body, because they are one artifact
// (spec/07-transport.md, "As a file").
func TestFileIsTheSameBytes(t *testing.T) {
	_, blocks := testChain(t, 5, 3)
	dir := t.TempDir()
	name := filepath.Join(dir, "chain"+FileExtension)
	if err := WriteFile(name, blocks); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	raw, err := os.ReadFile(name) //nolint:gosec // the path is the test's own temporary directory.
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !bytes.Equal(raw, EncodeSeq(blocks)) {
		t.Error("the file is not the sequence's bytes")
	}
	back, err := ReadFile(name)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !slices.Equal(digests(back), digests(blocks)) {
		t.Errorf("ReadFile gave %v, want %v", digests(back), digests(blocks))
	}

	// A file holding exactly one block is the same format at length one, which
	// is what makes the demo's committed .block files one-block sequences.
	single := filepath.Join(dir, "one"+BlockFileExtension)
	if err := WriteFile(single, blocks[:1]); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	one, err := ReadFile(single)
	if err != nil || len(one) != 1 || one[0].Digest() != blocks[0].Digest() {
		t.Errorf("the one-block file read back as %v, %v", digests(one), err)
	}
}

// TestReadFileFS reads a chain file out of a file system, which is how an
// embedded directory of committed blocks is read by the same code as a disk.
func TestReadFileFS(t *testing.T) {
	_, blocks := testChain(t, 6, 2)
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "c.dialog"), blocks); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	back, err := ReadFileFS(os.DirFS(dir), "c.dialog")
	if err != nil {
		t.Fatalf("ReadFileFS: %v", err)
	}
	if !slices.Equal(digests(back), digests(blocks)) {
		t.Errorf("ReadFileFS gave %v, want %v", digests(back), digests(blocks))
	}
}

// TestCheckRange is the client's own check of the range property: the thing a
// client MUST verify for itself rather than take from the server
// (spec/07-transport.md, "Verification obligations", rule 2).
func TestCheckRange(t *testing.T) {
	_, blocks := testChain(t, 7, 4)

	if err := CheckRange(blocks, nil); err != nil {
		t.Fatalf("a whole chain from the genesis position: %v", err)
	}
	first := blocks[0].Digest()
	if err := CheckRange(blocks[1:], &first); err != nil {
		t.Fatalf("a range after the genesis block: %v", err)
	}

	// A skipped block is a break the client sees at the point of the skip.
	skipped := []*block.Block{blocks[0], blocks[2], blocks[3]}
	if err := CheckRange(skipped, nil); err == nil {
		t.Error("a range with a skipped block passed the contiguity check")
	}
	// A reordered range is the same break.
	if err := CheckRange([]*block.Block{blocks[1], blocks[0]}, nil); err == nil {
		t.Error("a reordered range passed the contiguity check")
	}
	// A range that does not begin where the client asked.
	if err := CheckRange(blocks, &first); err == nil {
		t.Error("a range beginning at the genesis block passed a check for a later position")
	}
	// Another author's block cannot be in this author's range.
	_, other := testChain(t, 8, 1)
	if err := CheckRange([]*block.Block{blocks[0], other[0]}, nil); err == nil {
		t.Error("a range mixing two authors passed the check")
	}
}

// TestCheckSiblings pins the sibling ordering: ascending bytewise digest, so
// that two sources holding the same set produce the same bytes.
func TestCheckSiblings(t *testing.T) {
	pub, _, siblings := forkedChain(t)
	SortSiblings(siblings)
	prev, _ := siblings[0].Prev()

	if err := CheckSiblings(siblings, pub, &prev); err != nil {
		t.Fatalf("a sorted sibling set: %v", err)
	}
	reversed := slices.Clone(siblings)
	slices.Reverse(reversed)
	if err := CheckSiblings(reversed, pub, &prev); err == nil {
		t.Error("a descending sibling set passed the order check")
	}
	if err := CheckSiblings(siblings, pub, nil); err == nil {
		t.Error("a sibling set at a later position passed a check for the genesis position")
	}
}

// forkedChain builds a genesis block and two blocks claiming it as their
// predecessor: a fork, and therefore a sibling set with two members
// (spec/02-block-format.md, "Validation" rule 9).
func forkedChain(t *testing.T) (ed25519.PublicKey, *block.Block, []*block.Block) {
	t.Helper()
	priv := testKey(t, 9)
	genesis, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePublic, TS: 1,
		Ops: []block.Operation{block.MustCreateAtom("the common ancestor")},
	}, priv)
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	prev := genesis.Digest()
	var branches []*block.Block
	for i, text := range []string{"the left branch", "the right branch"} {
		b, err := block.Sign(block.Content{
			Version: block.Version, Type: block.TypePublic, Prev: &prev, TS: uint64(2 + i),
			Ops: []block.Operation{block.MustCreateAtom(text)},
		}, priv)
		if err != nil {
			t.Fatalf("branch %d: %v", i, err)
		}
		branches = append(branches, b)
	}
	pub := genesis.PublicKey()
	return pub, genesis, branches
}

// TestDecodeSeqNamesTheItem: an error says which item of the sequence was wrong
// and where, because "the response is malformed" is not a diagnostic.
func TestDecodeSeqNamesTheItem(t *testing.T) {
	_, blocks := testChain(t, 10, 2)
	seq := append(EncodeSeq(blocks), 0xa0)
	_, err := DecodeSeq(seq)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "block 2 of the sequence") {
		t.Errorf("the error does not name the item: %v", err)
	}
}

// TestSeqCarriesNoMetadata: the sequence is the blocks and nothing else, so a
// reader learns the author, the position and the count from the blocks
// themselves. This is what makes a saved response and a hand-carried file the
// same artifact.
func TestSeqCarriesNoMetadata(t *testing.T) {
	pub, blocks := testChain(t, 11, 2)
	seq := EncodeSeq(blocks)
	if bytes.Contains(seq, []byte(itoa(len(blocks)))) && len(blocks) > 9 {
		t.Skip("the count would be coincidentally present")
	}
	back, err := DecodeSeq(seq)
	if err != nil {
		t.Fatalf("DecodeSeq: %v", err)
	}
	if !back[0].PublicKey().Equal(pub) {
		t.Error("the author is read from the block, and did not survive the round trip")
	}
	if _, ok := back[0].Prev(); ok {
		t.Error("the genesis block came back with a prev")
	}
	if d := cid.SumDigest(back[1].Bytes()); d != blocks[1].Digest() {
		t.Error("a block's identity is the hash of its own bytes, and the sequence moved them")
	}
}
