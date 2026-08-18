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
	// Type is a URI identifying the problem type. This profile defines none, so
	// it is always "about:blank", which RFC 9457 makes the status code's own
	// meaning.
	Type string `json:"type"`
	// Title is a short, human-readable summary.
	Title string `json:"title"`
	// Status is the HTTP status code, repeated in the body as RFC 9457 allows
	// so that a logged body is self-contained.
	Status int `json:"status"`
	// Detail is a human-readable explanation of this occurrence.
	Detail string `json:"detail,omitempty"`
}

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

// writeProblem sends an RFC 9457 error body. It is the only way this package's
// server writes an error, so that no path can answer with a bare status and an
// empty body.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := Problem{Type: "about:blank", Title: problemTitle(status), Status: status, Detail: detail}
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
