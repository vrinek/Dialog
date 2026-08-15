//go:build ruleguard

// Package gorules holds the project-specific static-analysis rules that
// gocritic's ruleguard checker runs; see go/.golangci.yml.
//
// Nothing compiles this file. The `ruleguard` build constraint keeps it out
// of every build and out of `go build ./...`, `go vet ./...` and the test
// binaries; ruleguard reads it as data and interprets the matcher. The dsl
// import is nonetheless real enough that ruleguard has to type-check it, which
// is why go.mod requires github.com/quasilyte/go-ruleguard/dsl — a dependency
// of the analysis, never of the library, and never linked into anything.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// mapIterationOrder is the determinism guard.
//
// Dialog is a content-addressed protocol: an entity's identity is the hash of
// its canonical dCBOR bytes, so the encoder MUST emit map entries in a fixed
// order (spec/03-encoding.md, "Deterministic encoding"). Go randomises map
// iteration order on purpose, which makes `range` over a map the one
// construct in the language that can produce a different byte string on every
// run — and a bug that a test suite passes ninety-nine times out of a hundred.
//
// So it is banned outright in the packages that produce or consume canonical
// bytes, in the vector generator, whose output is committed and diffed, and in
// the graph package, whose every query MUST answer in an order that does not
// depend on the order blocks arrived in (spec/05-processing-model.md).
// Where iteration really is order-independent — building a set, or
// collecting keys that are sorted immediately afterwards — say so with a
// `//nolint:gocritic // reason` and the reason it cannot matter. Tests are
// exempt: they compare against fixed expectations, and an ordering bug there
// fails loudly rather than silently.
func mapIterationOrder(m dsl.Matcher) {
	m.Match(
		`for $_, $_ := range $x { $*_ }`,
		`for $_ := range $x { $*_ }`,
		`for range $x { $*_ }`,
	).
		Where(m["x"].Type.Underlying().Is(`map[$_]$_`) &&
			m.File().PkgPath.Matches(`github\.com/vrinek/Dialog/go/(dcbor|cid|entity|block|privacy|graph|internal/vectors|internal/vectorfile)$`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`range over a map: iteration order is randomised, so canonical bytes must never depend on it (spec/03-encoding.md). Sort the keys and range over the sorted slice, or explain with //nolint:gocritic why order cannot matter here.`)
}
