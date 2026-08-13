package entity

import (
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// The five standard meta-bond templates of spec/06-meta-bonds.md. Every
// implementation MUST support them, and recognizes a meta-molecule by
// comparing its bond digest against theirs.
const (
	// TemplateEquivalence declares transitive equivalence between two
	// entities of the same type (both atoms, both bonds, or both molecules).
	TemplateEquivalence = "_A_ is the same as _B_"
	// TemplateTruthAssertion asserts that a molecule is true according to the
	// publishing author.
	TemplateTruthAssertion = "_A_ is true"
	// TemplateTruthRetraction asserts that a molecule is not true according
	// to the publishing author.
	TemplateTruthRetraction = "_A_ is untrue"
	// TemplateContradiction declares that two molecules cannot both be true.
	TemplateContradiction = "_A_ contradicts _B_"
	// TemplateSupersession declares that molecule A replaces molecule B.
	TemplateSupersession = "_A_ supersedes _B_"
)

// The five standard meta-bonds, as bonds. They are ordinary bonds — content
// addressed over their template exactly like any other — and are singled out
// only by the L3 semantics spec/06-meta-bonds.md gives their molecules.
//
// These variables must not be reassigned; treat them as constants.
var (
	// MetaBondEquivalence is "_A_ is the same as _B_".
	MetaBondEquivalence = MustBond(TemplateEquivalence)
	// MetaBondTruthAssertion is "_A_ is true".
	MetaBondTruthAssertion = MustBond(TemplateTruthAssertion)
	// MetaBondTruthRetraction is "_A_ is untrue".
	MetaBondTruthRetraction = MustBond(TemplateTruthRetraction)
	// MetaBondContradiction is "_A_ contradicts _B_".
	MetaBondContradiction = MustBond(TemplateContradiction)
	// MetaBondSupersession is "_A_ supersedes _B_".
	MetaBondSupersession = MustBond(TemplateSupersession)
)

// standardMetaBonds is the v1 library, in the order spec/06-meta-bonds.md
// lists them.
var standardMetaBonds = []Bond{
	MetaBondEquivalence,
	MetaBondTruthAssertion,
	MetaBondTruthRetraction,
	MetaBondContradiction,
	MetaBondSupersession,
}

// metaBondsByDigest indexes the standard library by bond digest, which is how
// a meta-molecule is recognized: the molecule's bond field is compared
// against the digest of each known meta-bond template
// (spec/06-meta-bonds.md, "Meta-molecules are regular molecules").
var metaBondsByDigest = func() map[cid.Digest]Bond {
	m := make(map[cid.Digest]Bond, len(standardMetaBonds))
	for _, b := range standardMetaBonds {
		m[b.Digest()] = b
	}
	return m
}()

// StandardMetaBonds returns the v1 standard meta-bond library, in
// specification order.
func StandardMetaBonds() []Bond { return slices.Clone(standardMetaBonds) }

// LookupMetaBond returns the standard meta-bond with the given digest, if
// there is one. It is how an implementation decides whether a molecule is a
// meta-molecule: pass Molecule.Bond.
func LookupMetaBond(d cid.Digest) (Bond, bool) {
	b, ok := metaBondsByDigest[d]
	return b, ok
}

// IsMetaBond reports whether d is the digest of a standard meta-bond.
func IsMetaBond(d cid.Digest) bool {
	_, ok := metaBondsByDigest[d]
	return ok
}
