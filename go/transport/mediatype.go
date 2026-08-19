package transport

import (
	"mime"
	"strconv"
	"strings"
)

// itoa is strconv.Itoa under a shorter name, for the header values this package
// writes by hand.
func itoa(n int) string { return strconv.Itoa(n) }

// mediaTypeIs reports whether a Content-Type header names the given type,
// ignoring parameters and case. An unparseable header names nothing.
func mediaTypeIs(header, want string) bool {
	if header == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return strings.EqualFold(mt, want)
}

// isBlockSeqType reports whether a media type is one of the two a block sequence
// may travel under.
//
// The equivalence holds in both directions: a client MUST accept the generic RFC
// 8742 type on a response, since a plain file server offering a directory of
// chain files sends it and its bytes are the same bytes, and a server MUST
// accept an announce body under either, since the file that body came from
// carries whatever type its file server attached. Nothing is confused by
// admitting both — the two types are the same bytes, and a block sequence
// carries no metadata to disagree about (spec/07-transport.md, "Bodies and
// content types"; todos/094).
func isBlockSeqType(header string) bool {
	return mediaTypeIs(header, MediaTypeBlocks) || mediaTypeIs(header, MediaTypeCBORSeq)
}

// acceptsBlockSeq reports whether an Accept header admits the one type this
// server can send for a body of blocks. An absent or empty header admits
// everything, which is what RFC 9110 says and what a curl user expects.
//
// A client that names a type this server cannot send gets 406 rather than a
// body under a type it said it would not read. Only the media range is read: a
// q-value of zero is not honoured, which errs toward answering.
func acceptsBlockSeq(header string) bool {
	if strings.TrimSpace(header) == "" {
		return true
	}
	for _, entry := range strings.Split(header, ",") {
		rang := strings.TrimSpace(entry)
		if i := strings.IndexByte(rang, ';'); i >= 0 {
			rang = strings.TrimSpace(rang[:i])
		}
		switch {
		case rang == "*/*", rang == "application/*":
			return true
		case strings.EqualFold(rang, MediaTypeBlocks), strings.EqualFold(rang, MediaTypeCBORSeq):
			return true
		}
	}
	return false
}

// There is deliberately no acceptsJSON beside these two. The bodies that are
// JSON — an announce receipt, and the problem details of every error — are the
// only thing their operations can answer with, so a server has nothing to
// negotiate and 406 has nothing to protect. Accept is evaluated on the five read
// operations and on none of the writes (spec/07-transport.md, "Bodies and
// content types"; "Status codes"; todos/094).
