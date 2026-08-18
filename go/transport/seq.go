package transport

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// DefaultMaxSeqBytes bounds what [ReadSeq] will read from a stream it does not
// control. A response body can be arbitrarily large, and a client MUST bound
// what it will read (spec/07-transport.md, Security Considerations, "Resource
// exhaustion"). Sixteen megabytes is far past any honest chain page and far
// short of an allocation that hurts.
const DefaultMaxSeqBytes int64 = 16 << 20

// ErrTooLarge reports a body that exceeded the caller's bound before it was
// decoded. It is a refusal to read, not a finding about the bytes.
var ErrTooLarge = errors.New("transport: the block sequence is larger than the reader accepts")

// EncodeSeq renders blocks as a block sequence: the canonical dCBOR encoding of
// each block, concatenated, with no framing, no length prefix, no wrapper and no
// separator (spec/07-transport.md, "The block sequence").
//
// Each block's encoding is exactly the bytes its digest is taken over and its
// signature covers, so this function neither computes nor adds anything: the
// sequence carries no count, no author, no position and no signature of its own.
// Ordering is the caller's, because ordering is a property of the operation that
// produced the sequence and not of the format.
func EncodeSeq(blocks []*block.Block) []byte {
	return AppendSeq(nil, blocks...)
}

// AppendSeq appends the blocks' encodings to dst and returns the extended
// slice, in the manner of [append].
func AppendSeq(dst []byte, blocks ...*block.Block) []byte {
	for _, b := range blocks {
		dst = append(dst, b.Bytes()...)
	}
	return dst
}

// SeqLen returns the length in bytes of the sequence EncodeSeq would produce.
// A server uses it to set Content-Length without encoding twice, which is also
// what lets it answer HEAD without building a body.
func SeqLen(blocks []*block.Block) int {
	n := 0
	for _, b := range blocks {
		n += len(b.Bytes())
	}
	return n
}

// WriteSeq writes the blocks as a block sequence.
func WriteSeq(w io.Writer, blocks []*block.Block) (int64, error) {
	var written int64
	for _, b := range blocks {
		n, err := w.Write(b.Bytes())
		written += int64(n)
		if err != nil {
			return written, fmt.Errorf("transport: writing a block sequence: %w", err)
		}
	}
	return written, nil
}

// DecodeSeq parses a block sequence.
//
// Reading is strict, and every strictness is one of the format's rules
// (spec/07-transport.md, "The block sequence"):
//
//   - Every item must be a well-formed dCBOR encoding of a block. A sequence
//     containing anything else is malformed as a whole, so one bad item fails
//     the call rather than being skipped.
//   - Items are decoded one after another until the input is exhausted. There
//     is no count to check against and none to trust.
//   - A truncated final item is an error, not the end of the sequence. This is
//     the difference between a short read and a complete answer, and a reader
//     that guessed would turn a cut connection into a silently short chain.
//
// An empty input is a sequence of zero blocks and not an error: it is the valid
// answer meaning "none".
//
// Each item is decoded twice — once to find where it ends, since a CBOR sequence
// has no framing and the only thing that says an item's length is decoding it,
// and once through [block.Decode], which is the single entry point that performs
// the whole L1 structural check including the signature. The second pass is what
// makes the block's bytes exactly the bytes its digest is taken over.
func DecodeSeq(seq []byte) ([]*block.Block, error) {
	var blocks []*block.Block
	for off := 0; off < len(seq); {
		_, n, err := dcbor.DecodePrefix(seq[off:])
		if err != nil {
			return nil, fmt.Errorf("transport: block %d of the sequence, at byte %d: %w", len(blocks), off, err)
		}
		b, err := block.Decode(seq[off : off+n])
		if err != nil {
			return nil, fmt.Errorf("transport: block %d of the sequence, at byte %d: %w", len(blocks), off, err)
		}
		blocks = append(blocks, b)
		off += n
	}
	return blocks, nil
}

// ReadSeq reads a block sequence from r, refusing to read more than max bytes.
// A max of zero means DefaultMaxSeqBytes; a negative max means no bound, which
// is only ever right for a source the caller controls.
func ReadSeq(r io.Reader, max int64) ([]*block.Block, error) {
	if max == 0 {
		max = DefaultMaxSeqBytes
	}
	var (
		raw []byte
		err error
	)
	if max < 0 {
		raw, err = io.ReadAll(r)
	} else {
		// One byte past the bound: reading it is how an oversized body is told
		// from one that exactly fits.
		raw, err = io.ReadAll(io.LimitReader(r, max+1))
		if err == nil && int64(len(raw)) > max {
			return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, max)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("transport: reading a block sequence: %w", err)
	}
	return DecodeSeq(raw)
}

// ReadFile reads a chain file: a block sequence at rest.
//
// Nothing is added and nothing is removed by writing a sequence to disk, so a
// range response saved to a file is a valid chain file and a chain file offered
// to a server is a valid announce body (spec/07-transport.md, "As a file"). The
// conventional extension is [FileExtension]; a file holding exactly one block
// may use [BlockFileExtension], which this function reads identically because it
// is the same format at length one.
func ReadFile(name string) ([]*block.Block, error) {
	raw, err := os.ReadFile(name) //nolint:gosec // reading the named file is the function.
	if err != nil {
		return nil, fmt.Errorf("transport: reading the chain file: %w", err)
	}
	blocks, err := DecodeSeq(raw)
	if err != nil {
		return nil, fmt.Errorf("%w (in %s)", err, name)
	}
	return blocks, nil
}

// ReadFileFS reads a chain file from a file system, which is what lets an
// embedded directory of committed blocks be read by the same code as a
// directory on disk.
func ReadFileFS(fsys fs.FS, name string) ([]*block.Block, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("transport: reading the chain file: %w", err)
	}
	blocks, err := DecodeSeq(raw)
	if err != nil {
		return nil, fmt.Errorf("%w (in %s)", err, name)
	}
	return blocks, nil
}

// WriteFile writes the blocks to a chain file. The bytes are the ones a range
// response would carry, which is the property that keeps offline exchange from
// being a parallel mechanism.
func WriteFile(name string, blocks []*block.Block) error {
	if err := os.WriteFile(name, EncodeSeq(blocks), 0o600); err != nil {
		return fmt.Errorf("transport: writing the chain file: %w", err)
	}
	return nil
}

// CheckRange verifies the range property of a sequence for itself: that the
// first block's prev names the position that was asked about, and that every
// later block's prev names the block immediately before it.
//
// This is the client obligation of spec/07-transport.md, "Verification
// obligations", rule 2, and it is why completeness *within* a range is free: a
// source that skips a block it holds produces a break the client sees at the
// point of the skip. It is also the server's own rule — a server MUST NOT skip a
// block within a range, reorder one, or serve across a hole in its own store —
// so both sides call it.
//
// after is the requested position: nil for the genesis position, which requires
// the first block to be a genesis block. Every block must be signed by the same
// author, since a range is a run of one author's chain.
func CheckRange(blocks []*block.Block, after *cid.Digest) error {
	want := after
	for i, b := range blocks {
		prev, ok := b.Prev()
		switch {
		case want == nil && ok:
			return fmt.Errorf("transport: block %d of the range names prev %s, but the range began at the genesis position", i, prev)
		case want != nil && !ok:
			return fmt.Errorf("transport: block %d of the range is a genesis block, but the range began after %s", i, *want)
		case want != nil && prev != *want:
			return fmt.Errorf("transport: block %d of the range names prev %s, want %s: the range is not contiguous", i, prev, *want)
		}
		if i > 0 && !b.SameAuthor(blocks[0]) {
			return fmt.Errorf("transport: block %d of the range is by another author: a range is one author's chain", i)
		}
		d := b.Digest()
		want = &d
	}
	return nil
}

// CheckSiblings verifies a sibling set for itself: every block signed by the
// named author, every block claiming the named position, no duplicates, and
// ascending bytewise digest order.
//
// The order is fixed by the profile so that two sources holding the same set
// produce the same bytes, which makes the response comparable rather than merely
// cacheable (spec/07-transport.md, "Ordering"). A client that finds the order
// wrong has found a server that is not conforming; it has not found a fork, and
// the set is still usable once re-sorted.
func CheckSiblings(blocks []*block.Block, pub ed25519.PublicKey, prev *cid.Digest) error {
	for i, b := range blocks {
		if !bytes.Equal(b.PublicKey(), pub) {
			return fmt.Errorf("transport: sibling %d is signed by another author", i)
		}
		p, ok := b.Prev()
		switch {
		case prev == nil && ok:
			return fmt.Errorf("transport: sibling %d names prev %s, but the genesis position was asked about", i, p)
		case prev != nil && !ok:
			return fmt.Errorf("transport: sibling %d is a genesis block, but position %s was asked about", i, *prev)
		case prev != nil && p != *prev:
			return fmt.Errorf("transport: sibling %d names prev %s, want %s", i, p, *prev)
		}
		if i > 0 {
			before, here := blocks[i-1].Digest(), b.Digest()
			if bytes.Compare(before[:], here[:]) >= 0 {
				return fmt.Errorf("transport: sibling %d (%s) does not follow %s: a sibling set is ordered by ascending digest", i, here, before)
			}
		}
	}
	return nil
}

// SortSiblings orders blocks by ascending bytewise comparison of their digests,
// which is the order a sibling set is served in.
func SortSiblings(blocks []*block.Block) {
	slices.SortFunc(blocks, func(a, b *block.Block) int {
		ad, bd := a.Digest(), b.Digest()
		return bytes.Compare(ad[:], bd[:])
	})
}
