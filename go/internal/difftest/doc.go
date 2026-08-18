// Package difftest differentially fuzzes Dialog's hand-rolled dCBOR codec
// (github.com/vrinek/Dialog/go/dcbor) against an independent CBOR
// implementation, github.com/fxamacker/cbor/v2, configured as strictly as its
// options allow.
//
// # Why this module exists separately
//
// The library module carries exactly one dependency, golang.org/x/crypto, and
// that is a property worth keeping: anyone who imports Dialog's Go
// implementation gets the protocol and an Ed25519 implementation and nothing
// else. An oracle is a second CBOR codec, which is precisely the thing the
// library must not depend on. So this is its own module — its own go.mod, a
// `replace` onto ../.., no presence in the library's dependency graph, and no
// presence in the module zip a `go/vX.Y.Z` tag publishes, since a directory
// with its own go.mod is excluded from the parent module.
//
// # What is compared
//
// Two properties, one per fuzz target:
//
//   - Encode agreement (FuzzEncodeAgreement). A random dcbor.Value tree is
//     built from the fuzzer's bytes, converted to the Go representation the
//     oracle uses, and encoded by both. The two byte strings MUST be
//     identical. This tests head sizes, integer shortest-form, map key
//     ordering and container framing against an implementation that shares no
//     code with ours.
//
//   - Decode agreement (FuzzDecodeAgreement). Arbitrary bytes are offered to
//     both decoders and the accept/reject decisions are compared. Dialog's
//     profile is deliberately *narrower* than generic deterministic CBOR
//     (spec/03-encoding.md, "Deterministic CBOR"), so the two decoders are not
//     expected to agree everywhere; what is asserted is the shape of the
//     disagreement. See [Allowlist].
//
// # The oracle is not a dCBOR implementation
//
// fxamacker/cbor implements RFC 8949, including its Core Deterministic
// Encoding profile (§4.2.1). It does not implement Dialog's profile and has no
// reason to: Dialog's profile is RFC 8949 §4.2.1 *plus* restrictions, and an
// oracle that already knew those restrictions would be a second copy of the
// thing under test rather than an independent witness. The value of an
// independent witness is exactly that it disagrees in known places; the
// allowlist in divergence.go enumerates them, and a disagreement outside it
// fails the fuzz target.
//
// Two mechanisms carry the oracle's side of the comparison, and the
// distinction matters when reading a failure:
//
//   - Its options (see [Oracle]) express the rules it has knobs for:
//     definite-length only, duplicate-key rejection, invalid UTF-8 rejection,
//     NaN and infinity rejection, bignum-tag rejection, and — through a
//     simple-value registry that rejects all 248 simple values except null —
//     rule 7 exactly.
//
//   - A canonicity round-trip carries the rest. A byte string is "accepted by
//     the oracle" only if it decodes *and* re-encoding the decoded value under
//     Core Deterministic reproduces the input byte for byte. That is what
//     makes the oracle reject non-shortest arguments, unsorted map keys and
//     anything else RFC 8949 §4.2.1 forbids, none of which its decoder checks
//     on its own.
//
// # Running it
//
// From this directory, with a Go toolchain on PATH:
//
//	go test -count=1 ./...            # replays the committed seed corpus
//	go test -run '^$' -fuzz FuzzEncodeAgreement -fuzztime=10m ./...
//	go test -run '^$' -fuzz FuzzDecodeAgreement -fuzztime=10m ./...
//
// Both targets are seeded from vectors/dcbor.json — read from the repository
// at test time, never copied — so the ordinary `go test` run already exercises
// every byte string the conformance vectors pin, valid and invalid alike.
package difftest
