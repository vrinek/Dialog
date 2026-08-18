package difftest

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/vrinek/Dialog/go/dcbor"
)

// Generation bounds. They are small on purpose: the encode target asserts a
// byte-for-byte agreement, and an agreement is found or lost in the first few
// items of a tree. A large tree costs execution rate, which is the fuzzer's
// only budget, and buys nothing a small one does not already cover — the
// interesting cases are the *boundaries* (head sizes, key order, the decimal
// canonicalization), and those are in the value pools below rather than in the
// size of the tree.
const (
	// genMaxNodes bounds the whole tree, which is what guarantees termination:
	// the entropy is read cyclically, so nothing else does.
	genMaxNodes = 96
	// genMaxDepth bounds nesting. Dialog structures nest about six levels
	// (spec/03-encoding.md rule 10, informative note), so this is the shape of
	// a real document. Deeper trees, including the one at exactly MaxDepth,
	// are covered by the table tests rather than by the fuzzer, which would
	// have to guess a 64-item chain.
	genMaxDepth = 6
	// genMaxItems bounds an array's length and a map's entry count. 24 is one
	// past the largest count a one-byte head can hold, so a generated
	// container reaches the head-size boundary.
	genMaxItems = 25
)

// Generate builds a dcbor.Value from arbitrary bytes.
//
// It is total and deterministic: any input produces a value, the same input
// always produces the same value, and every value it produces is one
// dcbor.Encode accepts — canonical decimals, unique map keys, valid UTF-8,
// nesting inside MaxDepth. That last property is what makes the encode target
// a comparison rather than an error check: a generated value that dcbor.Encode
// refuses is a bug in this file, and the target says so.
//
// The entropy is consumed cyclically, so a short input is not a short tree; it
// is a repetitive one, which is a fine thing for a fuzzer to start from.
func Generate(entropy []byte) dcbor.Value {
	g := &generator{src: entropy, budget: genMaxNodes}
	return g.value(0)
}

type generator struct {
	src    []byte
	pos    int
	budget int
}

// next returns the next entropy byte, wrapping around at the end. An empty
// input reads as an endless run of zeros.
func (g *generator) next() byte {
	if len(g.src) == 0 {
		return 0
	}
	b := g.src[g.pos%len(g.src)]
	g.pos++
	return b
}

// intn returns a value in [0, n).
func (g *generator) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(g.next()) % n
}

// uint64 reads eight entropy bytes as one integer.
func (g *generator) uint64() uint64 {
	var b [8]byte
	for i := range b {
		b[i] = g.next()
	}
	return binary.BigEndian.Uint64(b[:])
}

// The value kinds, in the order the generator selects them. The scalar kinds
// come first so that a truncated selection — at the depth or budget bound —
// is a scalar.
const (
	kindUint = iota
	kindNeg
	kindDecimal
	kindText
	kindBytes
	kindNull
	kindArray
	kindMap
	kindCount
)

func (g *generator) value(depth int) dcbor.Value {
	g.budget--
	kinds := kindCount
	if depth >= genMaxDepth || g.budget <= 0 {
		kinds = kindArray // the scalar kinds are the ones below kindArray
	}

	switch g.intn(kinds) {
	case kindUint:
		return dcbor.Uint(g.argument())
	case kindNeg:
		return dcbor.Neg(g.argument())
	case kindDecimal:
		return g.decimal()
	case kindText:
		return dcbor.Text(g.text())
	case kindBytes:
		return dcbor.Bytes(g.bytes())
	case kindNull:
		return dcbor.Null
	case kindArray:
		n := g.intn(genMaxItems)
		items := make(dcbor.Array, 0, n)
		for range n {
			items = append(items, g.value(depth+1))
		}
		return items
	default: // kindMap
		return g.mapValue(depth)
	}
}

// argumentEdges are the CBOR argument values where the head changes size, and
// their neighbours. An encoder that gets one of these wrong writes a
// different number of bytes than the oracle does, which is the single most
// likely way for two independently written codecs to disagree.
var argumentEdges = []uint64{
	0, 1, 22, 23, 24, 25,
	254, 255, 256, 257,
	65534, 65535, 65536, 65537,
	4294967294, 4294967295, 4294967296, 4294967297,
	math.MaxInt64 - 1, math.MaxInt64, math.MaxInt64 + 1,
	math.MaxUint64 - 1, math.MaxUint64,
}

// argument returns a CBOR head argument: usually one of the boundary values,
// sometimes a uniformly random 64-bit integer.
func (g *generator) argument() uint64 {
	b := g.next()
	if b < 0xc0 {
		return argumentEdges[int(b)%len(argumentEdges)]
	}
	return g.uint64()
}

// exponentEdges and mantissaEdges are the decimal-fraction components worth
// trying: the ends of the int64 range the specification bounds both components
// to, the head-size boundaries, and mantissas divisible by 10, which
// dcbor.NewDecimal has to strip into the exponent before the value is
// canonical.
var (
	exponentEdges = []int64{
		-1, -2, -3, -10, -23, -24, -25, -255, -256, -65536, -4294967296,
		math.MinInt64, math.MinInt64 + 1,
	}
	mantissaEdges = []int64{
		1, -1, 3, -3, 23, 24, 314, -314, 255, 256, 65535, 65536,
		10, 100, 3140, -3140, 1000000,
		math.MaxInt64, math.MinInt64, math.MinInt64 + 1,
	}
)

// decimal returns a canonical decimal fraction — or the integer one
// canonicalizes to, which is itself a case worth generating: a mantissa
// divisible by 10 whose zeros absorb the whole exponent is a whole number, and
// spec/03-encoding.md requires a whole number to be a plain integer.
func (g *generator) decimal() dcbor.Value {
	exponent := exponentEdges[g.intn(len(exponentEdges))]
	mantissa := mantissaEdges[g.intn(len(mantissaEdges))]
	v, err := dcbor.NewDecimal(exponent, mantissa)
	if err != nil {
		// Unreachable: NewDecimal only fails when canonicalization carries a
		// non-negative exponent, and every exponent above is negative. Falling
		// back keeps Generate total rather than asserting inside a generator.
		return dcbor.Uint(0)
	}
	return v
}

// textPieces are the UTF-8 sequences text strings are assembled from: one,
// two, three and four byte encodings, the boundary code points of each, and
// the plain ASCII a Dialog document is mostly made of.
var textPieces = []string{
	"", "a", "b", "z", "0", " ", "-", "_",
	"France", "Paris", "description", "type", "value",
	// The boundaries of each UTF-8 sequence length, and the code points either
	// side of the surrogate range — the classic place for a UTF-8 validator to
	// be wrong.
	"\u0000", "\u007f", "\u0080", "\u07ff", "\u0800", "\ud7ff", "\ue000", "\uffff",
	"\U00010000", "\U0001f600", "\U0010ffff",
	"é", "日本語", "𝄞",
}

// lengthTargets are the string lengths where a CBOR length head changes size,
// and their neighbours. 65536 is left out on purpose: a 64 KiB string costs
// far more fuzzing time than it buys, and the four-byte length head is
// covered by a table test instead.
var lengthTargets = []int{0, 1, 2, 22, 23, 24, 25, 254, 255, 256, 257}

// text assembles a valid UTF-8 string of roughly one of the boundary lengths.
func (g *generator) text() string {
	target := lengthTargets[g.intn(len(lengthTargets))]
	var b strings.Builder
	for b.Len() < target {
		piece := textPieces[g.intn(len(textPieces))]
		if piece == "" {
			piece = "a"
		}
		b.WriteString(piece)
	}
	return b.String()
}

// bytes returns a byte string of one of the boundary lengths, filled from the
// entropy.
func (g *generator) bytes() []byte {
	n := lengthTargets[g.intn(len(lengthTargets))]
	out := make([]byte, n)
	for i := range out {
		out[i] = g.next()
	}
	return out
}

// keyPieces are map keys chosen to stress the ordering rule. Dialog sorts by
// the bytewise lexicographic order of the *encoded* key, not of the string, so
// "b" sorts before "aa": the encoded forms are 61 62 and 62 61 61, and the head
// byte decides. The pool holds several pairs that separate the two, together
// with keys either side of each length-head boundary, so that a codec sorting
// by the raw string or measuring a key's length differently cannot agree by
// luck.
var keyPieces = []string{
	"", "a", "b", "z", "A", "Z", "0", "9", "-", "_",
	"aa", "ab", "ba", "bb", "aaa", "aab",
	"type", "value", "description", "prev", "refs", "ops", "v", "ts",
	"\u0080", "\u07ff", "é", "日", "𝄞",
	strings.Repeat("k", 22), strings.Repeat("k", 23), strings.Repeat("k", 24),
	strings.Repeat("k", 255), strings.Repeat("k", 256),
}

// mapValue builds a map with distinct text keys. Duplicates are dropped rather
// than retried: dcbor.Encode rejects a duplicate key, and a generator that
// produced one would be testing the harness instead of the codecs.
func (g *generator) mapValue(depth int) dcbor.Value {
	n := g.intn(genMaxItems)
	m := make(dcbor.Map, 0, n)
	seen := make(map[string]bool, n)
	for range n {
		key := keyPieces[g.intn(len(keyPieces))]
		if seen[key] {
			continue
		}
		seen[key] = true
		m = append(m, dcbor.MapEntry{Key: key, Value: g.value(depth + 1)})
	}
	return m
}
