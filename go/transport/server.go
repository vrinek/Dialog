package transport

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A Store is what a [Server] serves from: blocks by digest, and the blocks that
// claim a position in an author's chain.
//
// Both of the package's stores satisfy it — a [block.MemStore] and a
// [block.ValidatingStore] — and so does anything else that can answer the two
// questions. Nothing more is needed, because the six operations are exactly
// those two questions asked in different shapes: a tip is the end of a forward
// walk, a range is a bounded forward walk, a sibling set is one position's
// answer unfiltered, and a block is a digest lookup.
//
// A server serves what it holds, whatever verdict it has reached about it. It
// does not validate on the way out and it does not filter by verdict: a block it
// holds as *stored but unvalidated* is still a block another node may be able to
// decide about, and withholding it would make this server the reason a valid
// block cannot be validated elsewhere. Every client re-validates everything
// anyway, so withholding costs the client a detection and saves it nothing —
// and at siblings the block a source cannot yet judge is precisely the one whose
// omission would hide a fork (spec/07-transport.md, "Server rules", rule 7;
// "Verification obligations"; todos/091).
//
// The interface is therefore the two questions and no third one about validity,
// which is also what makes a store that validates nothing — a mirror holding
// bytes it was given — a conforming source of this profile. The tip walk over it
// is a claim about connectivity, not about validity.
type Store interface {
	block.Source
	block.Siblings
}

// ServerConfig configures a [Server]. The zero value of every field is a
// documented default, so a read-only mirror is ServerConfig{Store: s}.
type ServerConfig struct {
	// Store is what the server serves. It is required.
	Store Store
	// Announce, when set, implements the profile's one write operation. Left
	// nil, the announce path is not served at all and the server is a read-only
	// mirror, which is conforming (spec/07-transport.md, "The six operations").
	Announce Announcer
	// Prefix is the path prefix every operation is mounted under. Empty means
	// DefaultPrefix. A server MAY be mounted at any base URL; a client is
	// configured with the whole base URL rather than with a host.
	Prefix string
	// DefaultRangeLimit is how many blocks a range returns when the client names
	// no limit. Zero means DefaultRangeLimit.
	DefaultRangeLimit int
	// MaxRangeLimit caps the limit a client may ask for. Zero means
	// MaxRangeLimit. A server MAY cap a limit and MUST NOT exceed it; capping is
	// not a partial answer presented as complete, because a range is a
	// contiguous prefix and a client continues from the last block it received.
	MaxRangeLimit int
	// MaxDigests caps a blocks request. Zero means MaxBatchDigests. It MUST NOT
	// be set below MinBatchDigests: a conforming server accepts a request naming
	// at least the scan limit's default, so that a worst-case honest validation
	// fits in one exchange.
	MaxDigests int
	// MaxBodyBytes caps a request body. Zero means MaxBodyBytes.
	MaxBodyBytes int64
}

// The server's default bounds. A server MUST bound what a request can cost it,
// and the bounds are policy rather than protocol (spec/07-transport.md,
// "Resource limits").
const (
	// DefaultRangeLimit is how many blocks a range returns when the client asks
	// for no particular number.
	DefaultRangeLimit = 128
	// MaxRangeLimit is the largest limit a client may ask for.
	MaxRangeLimit = 1024
	// MaxBatchDigests is the largest blocks request the server accepts. It is
	// four times MinBatchDigests, which is the floor conformance requires.
	MaxBatchDigests = 4 * MinBatchDigests
	// MaxBodyBytes is the largest request body the server reads.
	MaxBodyBytes int64 = 16 << 20
)

// A Server answers the six operations of spec/07-transport.md over HTTP.
//
// It is an [http.Handler] and holds no state of its own: every operation is
// independent, no operation establishes state a later one depends on, and no
// operation requires an identifier for the client. That is not an
// implementation convenience but the profile's rule — a server that required a
// client to identify itself would make that client's requests linkable into a
// durable identity, which is the opposite of what the subscription-privacy
// consideration asks for (spec/07-transport.md, "Server rules", rule 5).
//
// A deployment MAY put whatever it likes in front of these endpoints — TLS, an
// allowlist, a VPN, a rate limiter — by wrapping the handler. Neither changes
// what a client must verify.
//
// A query parameter this profile does not define for the operation being
// invoked is ignored, whether it is given once or many times: a tracking
// parameter an intermediary appended, a parameter of a later version of this
// profile, a parameter of another operation. Only the parameters an operation
// defines are read, and only those are refused for being given twice. That is
// what makes a server which does not implement the long poll degrade to polling
// rather than refuse the request, and what lets a later parameter be added
// without breaking servers written against this version
// (spec/07-transport.md, "HTTP binding"; todos/095).
type Server struct {
	store    Store
	announce Announcer
	prefix   string

	defaultLimit int
	maxLimit     int
	maxDigests   int
	maxBody      int64

	mux *http.ServeMux
}

// NewServer builds a server from a configuration.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Store == nil {
		return nil, errors.New("transport: a server needs a store to serve from")
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = DefaultPrefix
	}
	prefix = "/" + strings.Trim(prefix, "/")
	if prefix == "/" {
		prefix = ""
	}
	s := &Server{
		store:        cfg.Store,
		announce:     cfg.Announce,
		prefix:       prefix,
		defaultLimit: pick(cfg.DefaultRangeLimit, DefaultRangeLimit),
		maxLimit:     pick(cfg.MaxRangeLimit, MaxRangeLimit),
		maxDigests:   pick(cfg.MaxDigests, MaxBatchDigests),
		maxBody:      pick64(cfg.MaxBodyBytes, MaxBodyBytes),
	}
	if s.maxDigests < MinBatchDigests {
		return nil, fmt.Errorf("transport: MaxDigests is %d; a conforming server accepts at least %d digests in one blocks request", s.maxDigests, MinBatchDigests)
	}
	// The cap wins over the default, so that a server configured with a small
	// MaxRangeLimit alone is configured with it: a server MUST NOT exceed the
	// limit it caps at, and that includes the limit it chooses for itself when
	// the client names none.
	s.defaultLimit = min(s.defaultLimit, s.maxLimit)

	// The patterns carry no method. Go's mux would answer a wrong method with a
	// bare 405 and a text body; the profile wants 405 with an Allow header and
	// problem details, so each handler dispatches on the method itself.
	s.mux = http.NewServeMux()
	s.mux.HandleFunc(prefix+"/chains/{author}/tip", s.handleTip)
	s.mux.HandleFunc(prefix+"/chains/{author}/blocks", s.handleRange)
	s.mux.HandleFunc(prefix+"/chains/{author}/siblings", s.handleSiblings)
	s.mux.HandleFunc(prefix+"/blocks/fetch", s.handleFetch)
	s.mux.HandleFunc(prefix+"/blocks/{cid}", s.handleBlock)
	if s.announce != nil {
		s.mux.HandleFunc(prefix+"/announce", s.handleAnnounce)
	} else {
		// An OPTIONAL operation this server does not offer answers 404 with the
		// problem type that says so, rather than falling through to the generic
		// "no resource here": this profile has no discovery document, so the
		// status code and its type are the only way a client learns that an
		// optional operation is absent (spec/07-transport.md, "The six
		// operations"; "Status codes"; todos/087).
		s.mux.HandleFunc(prefix+"/announce", notOffered("announce"))
	}
	// The event stream is the other OPTIONAL thing with a path of its own, and
	// this server implements none.
	s.mux.HandleFunc(prefix+"/events", notOffered("the tip event stream"))
	s.mux.HandleFunc("/", s.handleUnknown)
	return s, nil
}

func pick(v, dflt int) int {
	if v == 0 {
		return dflt
	}
	return v
}

func pick64(v, dflt int64) int64 {
	if v == 0 {
		return dflt
	}
	return v
}

// ServeHTTP implements [http.Handler].
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.canonicalTarget(w, r) {
		return
	}
	s.mux.ServeHTTP(w, r)
}

// canonicalTarget refuses a request target that percent-encodes any octet of a
// path segment under this server's prefix.
//
// One spelling means one byte sequence, and percent-encoding is a second
// spelling. Every segment of every path this profile defines is either a fixed
// literal or an identifier in a canonical text form, and both alphabets — base32
// for an author key and a CID — need no percent-encoding at all, so a target
// that carries some is a malformed request rather than a spelling to normalize:
// bafyrei… and %62afyrei… are two byte strings naming one immutable resource,
// which a cache keys twice and which is the alias the canonical forms exist to
// prevent (spec/07-transport.md, "HTTP binding"; todos/090).
//
// The check is here rather than in each handler because Go's mux matches on the
// decoded path, so by the time a handler runs the encoding is invisible: the
// escaped path is the only place the second spelling still exists. Paths outside
// this server's prefix are left alone — they are nothing this profile defines,
// and they answer 404 for being nowhere rather than 400 for being spelled oddly.
func (s *Server) canonicalTarget(w http.ResponseWriter, r *http.Request) bool {
	rest, under := strings.CutPrefix(r.URL.EscapedPath(), s.prefix)
	if !under || !strings.ContainsRune(rest, '%') {
		return true
	}
	writeProblem(w, r, http.StatusBadRequest,
		"the request target percent-encodes part of a path this profile defines; an author key and a CID are base32 and need no encoding, and two spellings of one resource are two names for it")
	return false
}

// rawQuery parses the query string without percent-decoding it, into the values
// each name was given, in the order they appeared.
//
// Not decoding is the whole of it. The query values this profile defines are
// base32 identifiers and ASCII digits, so a well-behaved client never sends a
// percent sign; leaving the bytes as they arrived is what makes limit=%31 and
// after=%62afyrei… fail their own canonical-form checks instead of being
// silently normalized into the values they decode to (spec/07-transport.md,
// "HTTP binding"; todos/090). A percent-encoded octet in a parameter this
// profile does not define is nobody's business and is ignored with the rest of
// that parameter (todos/095).
//
// A malformed pair is kept rather than dropped, for the same reason: what the
// operation does not define, it ignores, and what it does define it checks
// itself.
func rawQuery(r *http.Request) map[string][]string {
	values := make(map[string][]string)
	for q := r.URL.RawQuery; q != ""; {
		var pair string
		pair, q, _ = strings.Cut(q, "&")
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		values[name] = append(values[name], value)
	}
	return values
}

// handleUnknown answers a path this server does not define at all. It is a 404
// in the profile's sense — this source does not have it — under the blank
// problem type, because nothing more specific is true of a path this profile
// never defined.
func (s *Server) handleUnknown(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("this server serves no resource at %s", r.URL.Path))
}

// notOffered answers the path of an OPTIONAL operation this server does not
// implement, for every method: the resource is not here at all, so there is no
// method that would be right and no Allow header to send.
//
// The distinction it draws is the one a client acts on. A "not held" 404 may be
// answered by another source or by this one later; an operation this server does
// not offer will not appear by asking again (spec/07-transport.md, "Status
// codes"; todos/087).
func notOffered(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeTypedProblem(w, r, http.StatusNotFound, ProblemOperationNotOffered,
			"this server does not offer "+operation+", which the profile makes optional")
	}
}

// readMethod admits GET and HEAD and refuses everything else with the Allow
// header the profile requires. HEAD MUST be supported wherever GET is.
func (s *Server) readMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	writeProblem(w, r, http.StatusMethodNotAllowed, fmt.Sprintf("%s is not defined for this path; it answers GET and HEAD", r.Method))
	return false
}

// postMethod admits POST alone. The two POST operations are not safe to answer
// for GET: one carries a body too long for a URL, and the other carries blocks.
func (s *Server) postMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	w.Header().Set("Allow", "POST")
	writeProblem(w, r, http.StatusMethodNotAllowed, fmt.Sprintf("%s is not defined for this path; it answers POST", r.Method))
	return false
}

// author reads the {author} path segment. The canonical text form is exact in
// both directions, and a server that accepted a variant spelling would be
// minting aliases for one identity, so anything else is 400 rather than
// normalized (spec/07-transport.md, "HTTP binding"; spec/03-encoding.md,
// Security Considerations).
func (s *Server) author(w http.ResponseWriter, r *http.Request) (ed25519.PublicKey, bool) {
	pub, err := cid.ParseAuthorKeyText(r.PathValue("author"))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return ed25519.PublicKey(pub), true
}

// position reads a position from a query parameter: absent means the genesis
// position, and every other spelling of "the start of the chain" is refused.
//
// The literal string null MUST be rejected. Exactly one spelling of a position
// is admitted, for the same reason exactly one spelling of a CID is: two
// spellings of one thing are two names for it, and this profile mints no
// aliases — which is also why the value is read undecoded, so that a
// percent-encoded CID is the second spelling it is (see rawQuery).
func (s *Server) position(w http.ResponseWriter, r *http.Request, param string) (*cid.Digest, bool) {
	values, present := rawQuery(r)[param]
	if !present {
		return nil, true
	}
	if len(values) > 1 {
		writeProblem(w, r, http.StatusBadRequest, fmt.Sprintf("%s was given %d times; a request names one position", param, len(values)))
		return nil, false
	}
	if values[0] == "" || values[0] == "null" {
		writeProblem(w, r, http.StatusBadRequest, fmt.Sprintf("%s=%q is not a position; omit the parameter to name the genesis position", param, values[0]))
		return nil, false
	}
	c, err := cid.ParseCIDString(values[0])
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, err.Error())
		return nil, false
	}
	d := c.Digest()
	return &d, true
}

// limit reads the optional maximum block count of a range. A server MAY cap it
// and MUST NOT exceed it.
//
// It has exactly one spelling — one or more ASCII digits, the first not zero,
// with no sign, no decimal point, no whitespace and no percent-encoded variant
// of any of those — and every other is 400, including a value too large to be a
// plausible count of blocks, which the round trip through strconv catches. A
// parameter given more than once is malformed for the same reason `after` given
// twice is: two values, and no rule anywhere saying which wins
// (spec/07-transport.md, "HTTP binding"; todos/089, 090).
func (s *Server) limit(w http.ResponseWriter, r *http.Request) (int, bool) {
	values, present := rawQuery(r)["limit"]
	if !present {
		return s.defaultLimit, true
	}
	if len(values) > 1 {
		writeProblem(w, r, http.StatusBadRequest, fmt.Sprintf("limit was given %d times", len(values)))
		return 0, false
	}
	n, err := strconv.Atoi(values[0])
	if err != nil || n <= 0 || values[0] != strconv.Itoa(n) {
		writeProblem(w, r, http.StatusBadRequest, fmt.Sprintf("limit=%q is not a positive decimal integer", values[0]))
		return 0, false
	}
	return min(n, s.maxLimit), true
}

// handleTip serves the tip operation: the block that occupies the tip position
// of an author's chain in this server's store.
//
// The response is the block itself, not a statement of its digest, so the
// client computes the identity from the bytes and the server cannot misreport
// it. What the server does choose is which tip to show, which is the freshness
// gap and is not fixable here (spec/07-transport.md, "tip"; todo 075).
func (s *Server) handleTip(w http.ResponseWriter, r *http.Request) {
	if !s.readMethod(w, r) {
		return
	}
	pub, ok := s.author(w, r)
	if !ok {
		return
	}
	if !acceptsBlockSeq(r.Header.Get("Accept")) {
		writeProblem(w, r, http.StatusNotAcceptable, "this server sends "+MediaTypeBlocks)
		return
	}
	tip, ok := s.tipOf(pub)
	if !ok {
		writeTypedProblem(w, r, http.StatusNotFound, ProblemNotHeld, "this source holds no tip for that author")
		return
	}

	// A strong ETag whose value is the tip block's CID, which is what makes
	// If-None-Match polling cost a few dozen bytes (spec/07-transport.md,
	// "Caching"). The client verifies it by re-hashing the body rather than
	// believing it.
	etag := `"` + tip.CID().String() + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set(HeaderTip, tip.CID().String())
	// no-cache asks a shared cache to revalidate rather than answer: a tip is
	// the one thing in this profile that changes.
	h.Set("Cache-Control", "no-cache")
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.writeBlocks(w, r, []*block.Block{tip})
}

// matchesETag reports whether an If-None-Match header names the given strong
// entity tag. The wildcard matches any existing representation, per RFC 9110.
func matchesETag(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		// A weak validator never matches a strong one for a body this client
		// is going to hash; W/ is not stripped, it disqualifies.
		if candidate == etag {
			return true
		}
	}
	return false
}

// handleRange serves the range operation: a contiguous run of one author's
// chain, beginning at the block after the requested position.
func (s *Server) handleRange(w http.ResponseWriter, r *http.Request) {
	if !s.readMethod(w, r) {
		return
	}
	pub, ok := s.author(w, r)
	if !ok {
		return
	}
	after, ok := s.position(w, r, "after")
	if !ok {
		return
	}
	limit, ok := s.limit(w, r)
	if !ok {
		return
	}
	if !acceptsBlockSeq(r.Header.Get("Accept")) {
		writeProblem(w, r, http.StatusNotAcceptable, "this server sends "+MediaTypeBlocks)
		return
	}

	blocks := s.walk(pub, after, limit)
	h := w.Header()
	// The tip is reported alongside the range so that the client can tell a
	// range that ended at the tip from one this server truncated, without a
	// second request per page. Where this source holds no tip for the author the
	// header is omitted rather than given an empty or null value: its value is a
	// CID text form, and a second spelling of a position is what this profile
	// refuses everywhere (spec/07-transport.md, "HTTP binding", "Where the
	// server holds no tip"; todos/085).
	if tip, held := s.tipOf(pub); held {
		h.Set(HeaderTip, tip.CID().String())
	}
	h.Set("Cache-Control", "no-cache")
	s.writeBlocks(w, r, blocks)
}

// handleSiblings serves the sibling set at one position: every block this
// source holds from that author whose prev names that position.
//
// The server includes every such block it holds — including the one it would
// itself serve from range and tip, so that the client sees a set rather than a
// difference — and chooses no winner. A one-member answer is not a statement
// that the chain does not fork; it is a statement about this source
// (spec/07-transport.md, "siblings").
func (s *Server) handleSiblings(w http.ResponseWriter, r *http.Request) {
	if !s.readMethod(w, r) {
		return
	}
	pub, ok := s.author(w, r)
	if !ok {
		return
	}
	prev, ok := s.position(w, r, "prev")
	if !ok {
		return
	}
	if !acceptsBlockSeq(r.Header.Get("Accept")) {
		writeProblem(w, r, http.StatusNotAcceptable, "this server sends "+MediaTypeBlocks)
		return
	}
	blocks := s.at(pub, prev)
	SortSiblings(blocks)
	w.Header().Set("Cache-Control", "no-cache")
	s.writeBlocks(w, r, blocks)
}

// handleBlock serves one block by CID. The response for a given CID can never
// change, because the CID is the hash of the response, so it is served
// immutable: a CDN, a corporate proxy or a local cache can then answer
// foreign-block resolution, which is exactly the traffic the scan limit makes
// hot (spec/07-transport.md, "Caching").
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	if !s.readMethod(w, r) {
		return
	}
	c, err := cid.ParseCIDString(r.PathValue("cid"))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if !acceptsBlockSeq(r.Header.Get("Accept")) {
		writeProblem(w, r, http.StatusNotAcceptable, "this server sends "+MediaTypeBlocks)
		return
	}
	b, err := s.store.Block(c.Digest())
	if err != nil {
		if errors.Is(err, block.ErrNotFound) {
			writeTypedProblem(w, r, http.StatusNotFound, ProblemNotHeld, "this source does not hold that block")
			return
		}
		writeProblem(w, r, http.StatusServiceUnavailable, "the store could not be read")
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", `"`+c.String()+`"`)
	s.writeBlocks(w, r, []*block.Block{b})
}

// fetchRequest is the body of a blocks request: one JSON object naming the
// digests, in the CID text form (spec/07-transport.md, "Bodies and content
// types").
type fetchRequest struct {
	Digests []string `json:"digests"`
}

// handleFetch serves the batch fetch. It is the one request in this profile
// that looks unsafe and is not: a GET with an argument list too long for a URL.
// It has no side effects and is idempotent.
func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	if !s.postMethod(w, r) {
		return
	}
	if !mediaTypeIs(r.Header.Get("Content-Type"), MediaTypeJSON) {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "a blocks request is "+MediaTypeJSON)
		return
	}
	if !acceptsBlockSeq(r.Header.Get("Accept")) {
		writeProblem(w, r, http.StatusNotAcceptable, "this server sends "+MediaTypeBlocks)
		return
	}
	raw, ok := s.body(w, r)
	if !ok {
		return
	}
	var req fetchRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "the request body is not a blocks request: "+err.Error())
		return
	}
	if len(req.Digests) > s.maxDigests {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, fmt.Sprintf("the request names %d digests; this server accepts %d", len(req.Digests), s.maxDigests))
		return
	}

	seen := make(map[cid.Digest]bool, len(req.Digests))
	blocks := make([]*block.Block, 0, len(req.Digests))
	for _, text := range req.Digests {
		c, err := cid.ParseCIDString(text)
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, err.Error())
			return
		}
		if seen[c.Digest()] {
			// A request MUST NOT name the same digest twice. A source MAY
			// reject such a request or answer as though the duplicate were
			// absent; this one rejects, so that the response's length is the
			// request's length minus what is not held and nothing else.
			writeProblem(w, r, http.StatusBadRequest, "the request names "+text+" twice")
			return
		}
		seen[c.Digest()] = true
		b, err := s.store.Block(c.Digest())
		if err != nil {
			// Digests the source does not hold are simply not in the response,
			// and the response says nothing about why.
			continue
		}
		blocks = append(blocks, b)
	}
	// The set can grow as this source receives blocks, so the answer is not
	// immutable the way one block's is.
	w.Header().Set("Cache-Control", "no-cache")
	s.writeBlocks(w, r, blocks)
}

// handleAnnounce serves the one operation that moves blocks toward this source.
func (s *Server) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	if !s.postMethod(w, r) {
		return
	}
	// An announce body is a block sequence, and the two types a block sequence
	// travels under are equivalent in this direction as in the other: a chain
	// file offered to a server is a valid announce body, and the type on that
	// file is whatever the file server that handed it over attached. Anything
	// else — including no type at all — is 415 (spec/07-transport.md, "Bodies
	// and content types"; todos/094).
	if !isBlockSeqType(r.Header.Get("Content-Type")) {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "an announce body is "+MediaTypeBlocks+" or "+MediaTypeCBORSeq)
		return
	}
	// Accept is not evaluated here. This operation's only response bodies are
	// JSON — a receipt, or problem details — and the server produces them
	// whatever the request asked for, so there is nothing for a 406 to protect;
	// a client of this profile carries Accept: application/dialog-blocks+cbor-seq
	// in its standing headers, and refusing a write over a header naming a type
	// the response was never going to have would cost the announce for nothing.
	raw, ok := s.body(w, r)
	if !ok {
		return
	}
	blocks, err := DecodeSeq(raw)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	receipt, err := s.announce.Announce(r.Context(), blocks)
	if err != nil {
		// A source MAY refuse an announce entirely, for reasons that are its
		// own policy — quota, acquaintance, disk. It is a refusal of the
		// request and not a finding about any block in it, so the answer is
		// 403 under the type that says so, and there is no receipt: nothing was
		// judged, and answering 200 with every block rejected would be
		// reporting a verdict this source never reached (spec/07-transport.md,
		// "announce"; todos/092).
		if errors.Is(err, ErrAnnounceRefused) {
			writeTypedProblem(w, r, http.StatusForbidden, ProblemAnnounceRefused, err.Error())
			return
		}
		// Anything else is this source being unable rather than unwilling, and
		// a client may try again.
		writeProblem(w, r, http.StatusServiceUnavailable, err.Error())
		return
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "the receipt could not be encoded")
		return
	}
	h := w.Header()
	h.Set("Content-Type", MediaTypeJSON)
	h.Set("Content-Length", itoa(len(body)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// body reads a request body under the server's bound, answering 413 for one
// that exceeds it. An announce body and a fetch body are both attacker-chosen
// sizes, and each costs the attacker one request.
func (s *Server) body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, s.maxBody+1))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "the request body could not be read")
		return nil, false
	}
	if int64(len(raw)) > s.maxBody {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, fmt.Sprintf("this server reads at most %d bytes of request body", s.maxBody))
		return nil, false
	}
	return raw, true
}

// writeBlocks sends a block sequence. It is the only way this package's server
// writes blocks, so that no path can serve them under another media type: a
// server MUST NOT serve a block sequence under any other type.
//
// An empty sequence is a zero-length body and a valid answer meaning "none",
// which is why this writes 200 and not 404 for an empty range or sibling set.
func (s *Server) writeBlocks(w http.ResponseWriter, r *http.Request, blocks []*block.Block) {
	h := w.Header()
	h.Set("Content-Type", MediaTypeBlocks)
	h.Set("Content-Length", itoa(SeqLen(blocks)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := WriteSeq(w, blocks); err != nil {
		// The status line is already sent; there is nothing to say but to stop
		// writing. The client sees a body shorter than Content-Length, which is
		// a truncated final item and therefore an error on its side — which is
		// exactly the rule that keeps a cut connection from reading as a short
		// chain.
		return
	}
}

// at returns the blocks this source holds at one position of an author's chain.
func (s *Server) at(pub ed25519.PublicKey, prev *cid.Digest) []*block.Block {
	digests := s.store.BlocksWithPrev(pub, prev)
	blocks := make([]*block.Block, 0, len(digests))
	for _, d := range digests {
		b, err := s.store.Block(d)
		if err != nil {
			continue
		}
		blocks = append(blocks, b)
	}
	return blocks
}

// branch chooses which block to serve at a position this source holds more than
// one block at — that is, at a fork.
//
// The choice MUST be deterministic and stable per author: for as long as a
// source holds the same blocks of a chain, every tip and every range it answers
// follows the same branch, and the two MUST agree (spec/07-transport.md, "tip";
// "range"; todos/086). Which branch is source policy, and this server takes the
// profile's reference rule — the lowest digest bytewise, the order siblings is
// sorted in. Being a function of the blocks alone, it is stable across requests,
// across restarts, and across two servers holding the same blocks, and it costs
// no stored state.
//
// Untangling a fork is the siblings operation's job, and a client that cares
// about forks does not learn about them from tip.
func branch(blocks []*block.Block) *block.Block {
	SortSiblings(blocks)
	return blocks[0]
}

// walk returns up to limit blocks of an author's chain, beginning at the block
// after the given position.
//
// It stops at a hole rather than serving across one: where this source holds a
// gap, the response ends before the gap, so that the client's prev walk
// terminates cleanly and the client can tell "the source stops here" from "the
// chain ends here" by asking tip (spec/07-transport.md, "Server rules", rule 2).
//
// The walk terminates because every step moves to a block whose prev is the
// current position, and a block's digest determines its prev: revisiting a
// position would mean two distinct blocks with one digest.
func (s *Server) walk(pub ed25519.PublicKey, after *cid.Digest, limit int) []*block.Block {
	out := make([]*block.Block, 0, min(limit, 64))
	pos := after
	for len(out) < limit {
		next := s.at(pub, pos)
		if len(next) == 0 {
			return out
		}
		b := branch(next)
		out = append(out, b)
		d := b.Digest()
		pos = &d
	}
	return out
}

// tipOf returns the block at the end of the chain this server can serve for an
// author: the last block of the forward walk from the genesis position.
//
// This is the profile's own definition of a tip, which is constructive for this
// reason: server rules 1 and 2 then hold by construction rather than as separate
// obligations, because the walk that answers tip is the walk that answers range
// and both stop at the same place (spec/07-transport.md, "tip"; "Server rules",
// rules 1 and 2; todos/086). A store holding blocks 3, 4 and 5 of a chain whose
// first three it never received therefore reports no tip at all and serves an
// empty range — those blocks are still served by digest, where no chain claim is
// made about them, and the hole is this server's problem to fix by fetching what
// it is missing.
func (s *Server) tipOf(pub ed25519.PublicKey) (*block.Block, bool) {
	var tip *block.Block
	pos := (*cid.Digest)(nil)
	for {
		next := s.at(pub, pos)
		if len(next) == 0 {
			return tip, tip != nil
		}
		tip = branch(next)
		d := tip.Digest()
		pos = &d
	}
}
