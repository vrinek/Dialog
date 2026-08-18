package transport

import (
	"encoding/json"
	"io"
	"net/http"
)

// A Problem is an RFC 9457 problem details object, the body of every error this
// profile defines (spec/07-transport.md, "Bodies and content types").
//
// The title and detail members are for people. A client MUST branch on the
// status code and MUST NOT parse detail: the status code is the interface, and
// the prose is a diagnostic somebody reads at three in the morning.
type Problem struct {
	// Type is a URI identifying the problem type. It is one of the two the
	// profile defines — [ProblemNotHeld] and [ProblemOperationNotOffered], both
	// 404s — or "about:blank", which RFC 9457 makes the status code's own
	// meaning and which every other error here uses.
	Type string `json:"type"`
	// Title is a short, human-readable summary.
	Title string `json:"title"`
	// Status is the HTTP status code, repeated in the body as RFC 9457 allows
	// so that a logged body is self-contained.
	Status int `json:"status"`
	// Detail is a human-readable explanation of this occurrence.
	Detail string `json:"detail,omitempty"`
}

// The two problem types the profile defines, both of them 404s, because 404
// carries two different facts and a client's next move differs between them
// (spec/07-transport.md, "Status codes"; todos/087).
//
// They are URNs rather than URLs because they are identifiers and not links:
// nothing is published at them, a client MUST NOT dereference them, and a URL
// would tie a protocol identifier to a hostname somebody has to keep answering.
const (
	// ProblemTypeBlank is RFC 9457's "the status code and nothing more", which
	// every error outside the two below uses.
	ProblemTypeBlank = "about:blank"
	// ProblemNotHeld says this source does not hold what was asked for: the
	// block, or a tip for that author. Another source may hold it, and this one
	// may hold it later.
	ProblemNotHeld = "urn:dialog:problem:not-held"
	// ProblemOperationNotOffered says this server does not implement the
	// OPTIONAL operation at that path. Asking again, or with other arguments,
	// will not change the answer; another server may offer it.
	ProblemOperationNotOffered = "urn:dialog:problem:operation-not-offered"
)

// problemTitles gives each status code of the profile's table the title the
// profile's own prose gives it, so that two servers built on this package
// describe the same refusal the same way (spec/07-transport.md, "Status codes").
func problemTitle(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "malformed request"
	case http.StatusNotFound:
		// The natural HTTP reading of 404 is the wrong one here, and the title
		// is where that is said out loud: a source's absence of a block is a
		// fact about the source and says nothing about whether the block exists.
		return "not held by this source"
	case http.StatusMethodNotAllowed:
		return "wrong method for this path"
	case http.StatusNotAcceptable:
		return "no acceptable representation"
	case http.StatusRequestEntityTooLarge:
		return "request too large"
	case http.StatusUnsupportedMediaType:
		return "unsupported media type"
	case http.StatusTooManyRequests:
		return "rate limited"
	case http.StatusServiceUnavailable:
		return "temporarily unable"
	default:
		return http.StatusText(status)
	}
}

// titleFor gives a typed problem the title its type deserves, and falls back to
// the status code's own.
func titleFor(status int, problemType string) string {
	switch problemType {
	case ProblemNotHeld:
		return problemTitle(http.StatusNotFound)
	case ProblemOperationNotOffered:
		return "operation not offered by this server"
	default:
		return problemTitle(status)
	}
}

// writeProblem sends an RFC 9457 error body under the blank type, which is the
// status code and nothing more. It is how every error outside the profile's two
// defined types is written.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	writeTypedProblem(w, r, status, ProblemTypeBlank, detail)
}

// writeTypedProblem sends an RFC 9457 error body naming a problem type. It and
// writeProblem are the only ways this package's server writes an error, so that
// no path can answer with a bare status and an empty body.
func writeTypedProblem(w http.ResponseWriter, r *http.Request, status int, problemType, detail string) {
	p := Problem{Type: problemType, Title: titleFor(status, problemType), Status: status, Detail: detail}
	body, err := json.Marshal(p)
	if err != nil {
		// Problem has no field that can fail to marshal; the branch exists so
		// that a future one cannot silently produce an empty error body.
		body = []byte(`{"type":"about:blank","title":"internal error","status":500}`)
		status = http.StatusInternalServerError
	}
	h := w.Header()
	h.Set("Content-Type", MediaTypeProblem)
	h.Set("Content-Length", itoa(len(body)))
	// A problem body is a diagnostic about one request; caching one would let a
	// shared cache answer a later, different request with it.
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// parseProblem reads an error body a server sent. A body that is missing,
// oversized or unparseable is not itself an error: the status code is what the
// client branches on, and the problem details only ever improve the message.
func parseProblem(r *http.Response) *Problem {
	if ct := r.Header.Get("Content-Type"); !mediaTypeIs(ct, MediaTypeProblem) && !mediaTypeIs(ct, MediaTypeJSON) {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return nil
	}
	var p Problem
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	return &p
}
