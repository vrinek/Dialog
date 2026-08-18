package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A Source is anything a node obtains blocks from through the five read
// operations of the profile: a server, a mirror, a directory of chain files
// behind an adapter, a fake in a test.
//
// It is the interface every client-side rule in this package is written
// against, because the profile's own definition of a source is that broad:
// "anything a node obtains blocks from — a server, a file, a directory, a
// removable disk, another node" (spec/07-transport.md, Terminology). The
// multi-source rule is only implementable against something this shape.
//
// Every method takes a context and no method establishes state a later one
// depends on. An implementation MUST verify nothing on the caller's behalf and
// MUST NOT hide a failure as an empty answer: "I do not have it" is
// [ErrNotHeld], and it is a fact about the source and nothing else.
type Source interface {
	// Tip returns the block at the author's tip position as this source holds
	// it. ifNoneMatch, when not empty, polls: a source that has not moved
	// answers with TipResult.Unchanged and no block.
	Tip(ctx context.Context, pub ed25519.PublicKey, ifNoneMatch string) (*TipResult, error)
	// Range returns a contiguous run of the author's chain beginning at the
	// block after the position, which is exclusive; a nil position is the
	// genesis position. limit of zero lets the source choose.
	Range(ctx context.Context, pub ed25519.PublicKey, after *cid.Digest, limit int) (*RangeResult, error)
	// Block returns one block by digest, or an error wrapping ErrNotHeld.
	Block(ctx context.Context, d cid.Digest) (*block.Block, error)
	// Blocks returns the subset of the named digests this source holds, in the
	// order requested. A digest the source does not hold is absent from the
	// result and is not an error.
	Blocks(ctx context.Context, digests []cid.Digest) ([]*block.Block, error)
	// Siblings returns every block this source holds from the author whose prev
	// names the position, in ascending digest order.
	Siblings(ctx context.Context, pub ed25519.PublicKey, prev *cid.Digest) ([]*block.Block, error)
}

// A TipResult is the answer to a tip request.
type TipResult struct {
	// Block is the tip block, or nil when the poll found nothing new.
	Block *block.Block
	// ETag is the entity tag the source sent, to be handed back to a later Tip
	// call as ifNoneMatch. It is a cache validator and not a fact about the
	// chain; the client verified the tip's identity by hashing the block.
	ETag string
	// Unchanged reports a 304: the source's tip is still the one the client
	// named, and no block was sent.
	Unchanged bool
}

// A RangeResult is the answer to a range request.
type RangeResult struct {
	// Blocks are the blocks of the range, in chain order, already checked for
	// contiguity against the requested position.
	Blocks []*block.Block
	// Tip is the tip the source reported alongside the range, or nil when it
	// reported none.
	//
	// It is a claim, not evidence. Comparing the last block of the range against
	// it is how a client learns whether the range ended at the tip or at a
	// limit — and that is all a client may do with it, because a source
	// withholding its newest blocks reports the older tip here too
	// (spec/07-transport.md, "A partial range"; todo 075).
	Tip *cid.Digest
}

// AtTip reports whether the last block of the range is the tip the source
// claimed. A client that is not at the tip continues by asking again with its
// position set to the digest of the last block it received, which is the
// profile's only continuation mechanism: no cursor, no session, no server-side
// state.
func (r *RangeResult) AtTip() bool {
	if r.Tip == nil || len(r.Blocks) == 0 {
		return false
	}
	return r.Blocks[len(r.Blocks)-1].Digest() == *r.Tip
}

// Last returns the digest of the last block of the range, which is the position
// a continuation asks after.
func (r *RangeResult) Last() (d cid.Digest, ok bool) {
	if len(r.Blocks) == 0 {
		return cid.Digest{}, false
	}
	return r.Blocks[len(r.Blocks)-1].Digest(), true
}

// ClientConfig configures a [Client]. The zero value of every field is a
// documented default.
type ClientConfig struct {
	// HTTP is the client to issue requests with. Nil means a client of this
	// package's own, with a timeout; http.DefaultClient is deliberately not the
	// default, because it has none.
	HTTP *http.Client
	// MaxBodyBytes bounds what a response may be. Zero means
	// DefaultMaxSeqBytes. A response body can be arbitrarily large and a client
	// MUST bound what it will read.
	MaxBodyBytes int64
	// UserAgent, when set, is sent with every request. It is optional and
	// carries no identity: the read operations require no client identifier,
	// and a client that supplies a distinctive one has made its requests
	// linkable, which is its own choice to make.
	UserAgent string
}

// A Client speaks the HTTP binding against one base URL.
//
// It implements [Source], and it verifies for itself everything the profile says
// a client must verify (spec/07-transport.md, "Verification obligations"):
//
//   - every block is re-hashed, and identified by the digest computed from its
//     bytes — never by its position in a sequence, never by the URL it came
//     from, never by anything the source said about it;
//   - a range's contiguity is checked against the position that was asked
//     about, so a skipped block is a break the client sees at the point of the
//     skip;
//   - a sibling set is checked to be one author's, at one position, in the
//     order the profile fixes.
//
// What a Client does not do is validate blocks against the protocol's rules.
// That is [block.Validate]'s work and belongs to the store the blocks are
// offered to; see [Syncer], which does both in the order the profile requires —
// validate on receipt, before the block is stored or its operations reach L2.
type Client struct {
	base    string
	http    *http.Client
	maxBody int64
	agent   string
}

// DefaultTimeout bounds a request a [Client] makes with the HTTP client it
// builds for itself.
const DefaultTimeout = 30 * time.Second

// NewClient returns a client for a base URL — the whole base URL, including the
// profile's path prefix, because a server MAY be mounted anywhere and a client
// is configured with the base URL rather than with a host.
func NewClient(baseURL string, cfg *ClientConfig) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("transport: the base URL %q is not a URL: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		// A server SHOULD be reachable over TLS and MAY be reachable over
		// plaintext HTTP. Neither changes what a client must verify, so both
		// are admitted here and neither is treated as authentication.
		return nil, fmt.Errorf("transport: the base URL %q is not http or https", baseURL)
	}
	c := &Client{base: strings.TrimRight(u.String(), "/")}
	if cfg != nil {
		c.http, c.maxBody, c.agent = cfg.HTTP, cfg.MaxBodyBytes, cfg.UserAgent
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: DefaultTimeout}
	}
	if c.maxBody == 0 {
		c.maxBody = DefaultMaxSeqBytes
	}
	return c, nil
}

// BaseURL returns the base URL the client was built for.
func (c *Client) BaseURL() string { return c.base }

// String makes a client name itself in a report that compares sources.
func (c *Client) String() string { return c.base }

// Tip implements [Source].
func (c *Client) Tip(ctx context.Context, pub ed25519.PublicKey, ifNoneMatch string) (*TipResult, error) {
	author, err := authorPath(pub)
	if err != nil {
		return nil, err
	}
	req, err := c.request(ctx, http.MethodGet, c.base+"/chains/"+author+"/tip", nil, "", MediaTypeBlocks)
	if err != nil {
		return nil, err
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := c.send(req, "tip")
	if err != nil {
		return nil, err
	}
	defer drain(resp)

	if resp.StatusCode == http.StatusNotModified {
		return &TipResult{ETag: resp.Header.Get("ETag"), Unchanged: true}, nil
	}
	if err := expect(resp, "tip", http.StatusOK); err != nil {
		return nil, err
	}
	blocks, err := c.readSeq(resp, "tip")
	if err != nil {
		return nil, err
	}
	if len(blocks) != 1 {
		return nil, fmt.Errorf("transport: tip %s: the response carries %d blocks, want exactly one", resp.Request.URL, len(blocks))
	}
	if !bytes.Equal(blocks[0].PublicKey(), pub) {
		return nil, fmt.Errorf("transport: tip %s: the block is signed by another author", resp.Request.URL)
	}
	return &TipResult{Block: blocks[0], ETag: resp.Header.Get("ETag")}, nil
}

// Range implements [Source].
func (c *Client) Range(ctx context.Context, pub ed25519.PublicKey, after *cid.Digest, limit int) (*RangeResult, error) {
	author, err := authorPath(pub)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if after != nil {
		// A position is a CID in a URL. A block digest inside a prev or refs
		// field is 32 raw bytes and has no text form of its own; hexadecimal
		// MUST NOT appear in a path or a query parameter, because it is a byte
		// dump and not an identifier (spec/07-transport.md, "HTTP binding").
		q.Set("after", after.CID().String())
	}
	if limit > 0 {
		q.Set("limit", itoa(limit))
	}
	req, err := c.request(ctx, http.MethodGet, c.base+"/chains/"+author+"/blocks"+query(q), nil, "", MediaTypeBlocks)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req, "range")
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if err := expect(resp, "range", http.StatusOK); err != nil {
		return nil, err
	}
	blocks, err := c.readSeq(resp, "range")
	if err != nil {
		return nil, err
	}
	// The range property is checked by the client, not asserted by the server.
	if err := CheckRange(blocks, after); err != nil {
		return nil, fmt.Errorf("transport: range %s: %w", resp.Request.URL, err)
	}
	if len(blocks) > 0 && !bytes.Equal(blocks[0].PublicKey(), pub) {
		return nil, fmt.Errorf("transport: range %s: the blocks are signed by another author", resp.Request.URL)
	}
	tip, err := tipHeader(resp)
	if err != nil {
		return nil, err
	}
	return &RangeResult{Blocks: blocks, Tip: tip}, nil
}

// Block implements [Source].
func (c *Client) Block(ctx context.Context, d cid.Digest) (*block.Block, error) {
	req, err := c.request(ctx, http.MethodGet, c.base+"/blocks/"+d.CID().String(), nil, "", MediaTypeBlocks)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req, "block")
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if err := expect(resp, "block", http.StatusOK); err != nil {
		return nil, err
	}
	blocks, err := c.readSeq(resp, "block")
	if err != nil {
		return nil, err
	}
	if len(blocks) != 1 {
		return nil, fmt.Errorf("transport: block %s: the response carries %d blocks, want exactly one", resp.Request.URL, len(blocks))
	}
	// A response whose bytes hash to something other than the requested digest
	// is a failed fetch, not a block (spec/07-transport.md, "Verification
	// obligations", rule 1). It is reported as not held, because that is what
	// the client learned: this source did not hand over that block.
	if blocks[0].Digest() != d {
		return nil, fmt.Errorf("transport: block %s: the response hashes to %s: %w", resp.Request.URL, blocks[0].Digest(), ErrNotHeld)
	}
	return blocks[0], nil
}

// Blocks implements [Source]. It is the operation the validation path needs: the
// scan limit counts blocks and not round trips, so a client resolving a block's
// refs issues one of these naming every digest it still needs, rather than one
// request per digest (spec/07-transport.md, "Interaction with the scan limit").
func (c *Client) Blocks(ctx context.Context, digests []cid.Digest) ([]*block.Block, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	wanted := make(map[cid.Digest]bool, len(digests))
	texts := make([]string, 0, len(digests))
	for _, d := range digests {
		if wanted[d] {
			// A request MUST NOT name the same digest twice.
			return nil, fmt.Errorf("transport: blocks: %s was named twice", d)
		}
		wanted[d] = true
		texts = append(texts, d.CID().String())
	}
	body, err := json.Marshal(fetchRequest{Digests: texts})
	if err != nil {
		return nil, fmt.Errorf("transport: blocks: encoding the request: %w", err)
	}
	req, err := c.request(ctx, http.MethodPost, c.base+"/blocks/fetch", body, MediaTypeJSON, MediaTypeBlocks)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req, "blocks")
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if err := expect(resp, "blocks", http.StatusOK); err != nil {
		return nil, err
	}
	blocks, err := c.readSeq(resp, "blocks")
	if err != nil {
		return nil, err
	}
	// A client MUST NOT identify a returned block by its position in the
	// sequence; it identifies each block by re-hashing it. Checking membership
	// rather than order is the whole of that rule.
	seen := make(map[cid.Digest]bool, len(blocks))
	for _, b := range blocks {
		d := b.Digest()
		if !wanted[d] {
			return nil, fmt.Errorf("transport: blocks %s: the response carries %s, which was not asked for", resp.Request.URL, d)
		}
		if seen[d] {
			return nil, fmt.Errorf("transport: blocks %s: the response carries %s twice", resp.Request.URL, d)
		}
		seen[d] = true
	}
	return blocks, nil
}

// Siblings implements [Source].
func (c *Client) Siblings(ctx context.Context, pub ed25519.PublicKey, prev *cid.Digest) ([]*block.Block, error) {
	author, err := authorPath(pub)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if prev != nil {
		q.Set("prev", prev.CID().String())
	}
	req, err := c.request(ctx, http.MethodGet, c.base+"/chains/"+author+"/siblings"+query(q), nil, "", MediaTypeBlocks)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req, "siblings")
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if err := expect(resp, "siblings", http.StatusOK); err != nil {
		return nil, err
	}
	blocks, err := c.readSeq(resp, "siblings")
	if err != nil {
		return nil, err
	}
	if err := CheckSiblings(blocks, pub, prev); err != nil {
		return nil, fmt.Errorf("transport: siblings %s: %w", resp.Request.URL, err)
	}
	return blocks, nil
}

// Announce offers blocks to a source and returns what it did with each of them.
//
// Announcing asserts nothing a block does not already say, and the source
// endorses nothing by accepting one. The blocks of one author MUST be in chain
// order; blocks of several authors MAY be interleaved in any way that keeps each
// author's own blocks in chain order (spec/07-transport.md, "Ordering").
func (c *Client) Announce(ctx context.Context, blocks []*block.Block) (*Receipt, error) {
	req, err := c.request(ctx, http.MethodPost, c.base+"/announce", EncodeSeq(blocks), MediaTypeBlocks, MediaTypeJSON)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req, "announce")
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if err := expect(resp, "announce", http.StatusOK, http.StatusAccepted); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody))
	if err != nil {
		return nil, fmt.Errorf("transport: announce %s: reading the receipt: %w", resp.Request.URL, err)
	}
	receipt := &Receipt{Deferred: resp.StatusCode == http.StatusAccepted}
	if len(bytes.TrimSpace(raw)) == 0 {
		// 202 means the announce was taken for later processing and the receipt
		// is incomplete or absent. An empty body is then the honest answer and
		// not a malformed one.
		if receipt.Deferred {
			return receipt, nil
		}
		return nil, fmt.Errorf("transport: announce %s: the response carries no receipt", resp.Request.URL)
	}
	if err := json.Unmarshal(raw, receipt); err != nil {
		return nil, fmt.Errorf("transport: announce %s: %w", resp.Request.URL, err)
	}
	return receipt, nil
}

// request builds one request. Every request carries an Accept header naming the
// type the operation answers with; a client SHOULD send it, and a server that
// cannot honour it answers 406 rather than sending a type the client said it
// would not read.
func (c *Client) request(ctx context.Context, method, u string, body []byte, contentType, accept string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	// The URL is built from the base URL the caller configured and a path
	// segment that is a canonical author key or CID, both of which are fixed
	// alphabets checked before they get here. Requesting a URL the operator
	// named is what a transport client is for.
	req, err := http.NewRequestWithContext(ctx, method, u, reader) //nolint:gosec // the base URL is the caller's own configuration.
	if err != nil {
		return nil, fmt.Errorf("transport: building the request for %s: %w", u, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
		req.ContentLength = int64(len(body))
	}
	if accept == MediaTypeBlocks {
		// The generic RFC 8742 type is named too, because a plain file server
		// offering a directory of chain files sends it and its bytes are the
		// same bytes.
		req.Header.Set("Accept", MediaTypeBlocks+", "+MediaTypeCBORSeq)
	} else {
		req.Header.Set("Accept", accept)
	}
	if c.agent != "" {
		req.Header.Set("User-Agent", c.agent)
	}
	return req, nil
}

func (c *Client) send(req *http.Request, op string) (*http.Response, error) {
	resp, err := c.http.Do(req) //nolint:gosec // see request: the destination is the configured base URL.
	if err != nil {
		return nil, fmt.Errorf("transport: %s %s: %w", op, req.URL, err)
	}
	return resp, nil
}

// readSeq reads a block sequence response, checking the media type before the
// bytes. A server MUST NOT serve a block sequence under any other type, and a
// body under another type is not a sequence this client will decode.
func (c *Client) readSeq(resp *http.Response, op string) ([]*block.Block, error) {
	if ct := resp.Header.Get("Content-Type"); !isBlockSeqType(ct) {
		return nil, fmt.Errorf("transport: %s %s: the response is %q, want %s", op, resp.Request.URL, ct, MediaTypeBlocks)
	}
	blocks, err := ReadSeq(resp.Body, c.maxBody)
	if err != nil {
		return nil, fmt.Errorf("transport: %s %s: %w", op, resp.Request.URL, err)
	}
	return blocks, nil
}

// expect turns any status but the ones the operation defines into a
// [StatusError], which a caller branches on by code.
func expect(resp *http.Response, op string, want ...int) error {
	for _, status := range want {
		if resp.StatusCode == status {
			return nil
		}
	}
	return &StatusError{Op: op, URL: resp.Request.URL.String(), Status: resp.StatusCode, Problem: parseProblem(resp)}
}

// tipHeader reads the Dialog-Tip claim off a response. A malformed one is an
// error: the header is the server's own statement in a form the profile fixes,
// and a server that cannot spell it is not one whose contiguity claims are worth
// more.
func tipHeader(resp *http.Response) (*cid.Digest, error) {
	value := resp.Header.Get(HeaderTip)
	if value == "" {
		return nil, nil
	}
	c, err := cid.ParseCIDString(value)
	if err != nil {
		return nil, fmt.Errorf("transport: %s %s: %w", HeaderTip, resp.Request.URL, err)
	}
	d := c.Digest()
	return &d, nil
}

// drain closes a response body, reading the little that may be left so that the
// connection can be reused rather than dropped.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}

func authorPath(pub ed25519.PublicKey) (string, error) {
	text, err := cid.AuthorKeyText(pub)
	if err != nil {
		return "", fmt.Errorf("transport: %w", err)
	}
	return text, nil
}

func query(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}
