// Package transport implements the optional interoperability profile of
// spec/07-transport.md: one serialization for "some blocks", six operations
// over it, and their binding to HTTP.
//
// # The transport carries no trust
//
// Every guarantee a client of this package gets comes from the blocks, not from
// the channel. A block's signature, its author key, its chain link and its
// content address are all inside the bytes the digest is taken over, so a source
// cannot lie about a block's contents; it can only lie by omission — withholding
// a tip, a branch of a fork, or a block another block's refs names. That single
// fact is why nothing here authenticates anything, and why every response this
// package's client accepts is re-hashed and re-validated before it is stored
// (spec/07-transport.md, "Verification obligations").
//
// The three parts:
//
//   - The block sequence (seq.go). An RFC 8742 CBOR sequence of block
//     encodings: concatenation, no framing, no metadata. It is the response
//     body, the request body and the file, which is what makes a saved response
//     and a hand-carried chain file the same artifact.
//   - The server (server.go). An [http.Handler] over anything that can answer
//     "which block is this digest" and "which blocks claim this position" —
//     a [block.MemStore] or a [block.ValidatingStore], both of which do.
//   - The client (client.go, sync.go). The six operations against one base URL,
//     and a [Syncer] that drives them into a [block.ValidatingStore] with every
//     block validated on receipt.
//
// # What this package does not do
//
// It defines no authorization, no session, no discovery and no peer protocol,
// because the profile defines none: a client learns a base URL the way it learns
// a podcast feed's, and a user who wants no third party runs a server. Freshness
// and completeness are not provided either — they cannot be, and the profile
// states them as gaps rather than papering over them (spec/07-transport.md,
// "What a server does not guarantee"). The client-side answer to both is to
// obtain each chain from more than one source, which [Syncer] does.
package transport

import (
	"fmt"
	"net/http"

	"github.com/vrinek/Dialog/go/block"
)

// DefaultPrefix is the path prefix the profile fixes for the HTTP binding. The
// v1 names the version of the transport profile, not the protocol version a
// block carries in its v field (spec/07-transport.md, "HTTP binding").
const DefaultPrefix = "/dialog/v1"

// The media types of the binding. A block sequence has one type of its own and
// one it MUST be accepted under, because a plain file server offering a
// directory of chain files sends the generic one and its bytes are the same
// bytes (spec/07-transport.md, "Bodies and content types").
const (
	// MediaTypeBlocks is the type of every body that carries blocks.
	MediaTypeBlocks = "application/dialog-blocks+cbor-seq"
	// MediaTypeCBORSeq is the generic RFC 8742 type a client MUST accept as
	// equivalent to MediaTypeBlocks.
	MediaTypeCBORSeq = "application/cbor-seq"
	// MediaTypeJSON is the type of a blocks request and an announce receipt.
	// Every body that does not carry blocks is JSON, so that a diagnostic a
	// person reads at three in the morning needs no decoder.
	MediaTypeJSON = "application/json"
	// MediaTypeProblem is the type of every error body (RFC 9457).
	MediaTypeProblem = "application/problem+json"
)

// HeaderTip is the response header carrying the CID text form of the tip the
// server holds for an author at the moment of the response. It is what lets a
// client tell a range that ended at the tip from one the server truncated,
// without a second request per page.
//
// It is a claim and not evidence. A server that withholds its newest blocks
// reports the older tip here too and nothing detects that, so a client MUST NOT
// act on the value except to decide whether to ask for more; the identity of
// every block it stores comes from re-hashing the block
// (spec/07-transport.md, "HTTP binding"; todo 075).
const HeaderTip = "Dialog-Tip"

// MinBatchDigests is the number of digests a conforming server MUST accept in
// one blocks request. It is the default scan limit of
// spec/05-processing-model.md, so that a worst-case honest validation's whole
// resolution budget fits in one exchange rather than in 256 round trips
// (spec/07-transport.md, "blocks"; "Resource limits").
const MinBatchDigests = 256

// FileExtension is the conventional extension of a file holding a block
// sequence. A file holding exactly one block may use BlockFileExtension, which
// is the same thing at length one (spec/07-transport.md, "As a file").
const (
	FileExtension      = ".dialog"
	BlockFileExtension = ".block"
)

// ErrNotHeld reports that a source does not hold what was asked for: the 404 of
// the binding, and the "I do not have it" of every operation.
//
// It is a fact about the source and about nothing else. It is never evidence
// that the block does not exist, that the author never published it, or that
// another source does not hold it, and a client MUST NOT treat it as such
// (spec/07-transport.md, "Status codes"; "What a server does not guarantee").
//
// It wraps [block.ErrNotFound], so a [Resolver] handing a failed fetch to
// validation produces the undecided verdict rather than a rejection.
var ErrNotHeld = fmt.Errorf("%w: the source does not hold it", block.ErrNotFound)

// A StatusError is an HTTP response a client could not use: any status other
// than the one the operation expects.
//
// A client MUST branch on the status code and MUST NOT parse the problem
// details' detail member, which is prose for a person
// (spec/07-transport.md, "Bodies and content types").
type StatusError struct {
	// Op names the operation that failed, in the profile's vocabulary.
	Op string
	// URL is the request URL, without a body.
	URL string
	// Status is the HTTP status code.
	Status int
	// Problem is the parsed RFC 9457 body, or nil when the server sent none or
	// sent one this client could not parse.
	Problem *Problem
}

// Unwrap makes a 404 the package's ErrNotHeld, and through it
// [block.ErrNotFound]: "I do not have it" is the answer to every question a
// source cannot answer from its own store, and the validator already knows what
// to do with a block it could not fetch — hold the block that named it as
// stored but unvalidated, and never call it invalid.
func (e *StatusError) Unwrap() error {
	if e.Status == http.StatusNotFound {
		return ErrNotHeld
	}
	return nil
}

func (e *StatusError) Error() string {
	if e.Problem != nil && e.Problem.Title != "" {
		return fmt.Sprintf("transport: %s %s: %d %s: %s", e.Op, e.URL, e.Status, e.Problem.Title, e.Problem.Detail)
	}
	return fmt.Sprintf("transport: %s %s: HTTP %d", e.Op, e.URL, e.Status)
}
