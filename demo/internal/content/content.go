// Package content holds the grounding demo's curated dataset and expresses it
// in Dialog's data model.
//
// It is the single source of truth for the demo: the three authors and their
// signing keys, the facts each of them publishes, and the entities those facts
// encode to. Everything here is a compile-time constant, and every entity is
// derived from those constants by the entity package, so the digest of any
// statement in the dataset can be recomputed by anyone holding this file —
// which is how the tests find the molecules they make assertions about without
// asking the chain builder where it put them.
//
// # What is real and what is invented
//
// Ten of the eleven countries are real, and their capitals are correct. Their
// population and area figures are approximate and are here to exercise the
// scalar filler types (an integer with a unit, a decimal fraction with a unit,
// a datetime range), not to be cited.
//
// The eleventh country, Valdoria, is FICTIONAL, and so is everything published
// about it: its capital Miravel, the rival claim that Port Casta is its
// capital, and its population. The demo needs one capital that two authors
// genuinely disagree about — that is the whole point of the conflict-surfacing
// machinery — and inventing a country is the honest way to get one without
// publishing a false claim about a real place.
//
// The correction chain errata publishes over Poland's population is invented
// too: the revised figures are made up so that a supersession chain has
// something to correct.
//
// # The three authors
//
//   - atlas     publishes the primary facts: countries, capitals, populations
//     and areas, and asserts its own claim about Valdoria's capital.
//   - gazetteer publishes naming variants as equivalences, the rival claim
//     about Valdoria's capital, a retraction of atlas's claim,
//     and an explicit contradiction between the two.
//   - errata    publishes corrections: a supersession chain over Poland's
//     population, an assertion it later retracts itself, and an equivalence it
//     gets wrong and withdraws.
package content

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// The three authors of the demo, named rather than keyed, so that
// subscriptions can be spelled out in prose.
const (
	// AuthorAtlas publishes the primary facts.
	AuthorAtlas = "atlas"
	// AuthorGazetteer publishes naming equivalences and the rival capital
	// claim.
	AuthorGazetteer = "gazetteer"
	// AuthorErrata publishes corrections.
	AuthorErrata = "errata"
)

// Authors lists the demo's authors in publication order: atlas first, because
// the other two reference its entities, then gazetteer, then errata.
var Authors = []string{AuthorAtlas, AuthorGazetteer, AuthorErrata}

// keyLabel is the string an author's Ed25519 seed is derived from. The keys of
// this demo are deliberately reproducible — the chains under demo/chains are
// committed and must regenerate byte for byte — which means they are also
// public. They sign nothing outside this repository.
func keyLabel(author string) string { return "dialog-demo/author/" + author }

// Seed returns the fixed 32-byte Ed25519 seed of an author:
//
//	seed(author) = SHA-256("dialog-demo/author/" || author)
//
// It panics for an author the demo does not have, which is a programming error
// rather than an input.
func Seed(author string) [ed25519.SeedSize]byte {
	if !known(author) {
		panic(fmt.Sprintf("content: no such author %q", author))
	}
	return sha256.Sum256([]byte(keyLabel(author)))
}

// Key returns an author's signing key. See Seed: it is not a secret.
func Key(author string) ed25519.PrivateKey {
	seed := Seed(author)
	return ed25519.NewKeyFromSeed(seed[:])
}

// PublicKey returns an author's public key — the identity their chain is
// published under, and the value a subscription names.
func PublicKey(author string) ed25519.PublicKey {
	pub, ok := Key(author).Public().(ed25519.PublicKey)
	if !ok {
		panic("content: an Ed25519 private key did not yield an Ed25519 public key")
	}
	return pub
}

func known(author string) bool {
	for _, a := range Authors {
		if a == author {
			return true
		}
	}
	return false
}

// Block timestamps. A block's ts field is untrusted and decides nothing, but
// it is part of the signed bytes, so it has to be fixed for the chains to
// regenerate identically. The demo's blocks are stamped one hour apart from
// midnight UTC on 2026-01-01, in publication order across all three chains.
const (
	// GenesisTS is the Unix timestamp of the first block, 2026-01-01T00:00:00Z.
	GenesisTS uint64 = 1767225600
	// BlockInterval is the number of seconds between one block's timestamp and
	// the next.
	BlockInterval uint64 = 3600
)

// TS returns the timestamp of the n-th block published in the demo, counting
// from zero across every chain.
func TS(n int) uint64 { return GenesisTS + BlockInterval*uint64(n) } //nolint:gosec // n is a small non-negative block index.

// The bond templates of the dataset. The five standard meta-bond templates are
// not repeated here; they are entity.TemplateEquivalence and its siblings.
const (
	// TemplateCapitalOf is atlas's relation between a capital and its country.
	TemplateCapitalOf = "_A_ is the capital of _B_"
	// TemplatePopulation carries two scalars: a count with a unit, and the
	// datetime range the count is for.
	TemplatePopulation = "_A_ had a population of _B_ during _C_"
	// TemplateArea carries a scalar with a unit, a decimal fraction where the
	// figure is not whole.
	TemplateArea = "_A_ has an area of _B_"
	// TemplateCapitalCityOf is gazetteer's own wording of TemplateCapitalOf.
	// The two are declared equivalent.
	TemplateCapitalCityOf = "_A_ is the capital city of _B_"
)

// The unit atoms. A scalar's unit is the digest of an atom naming it
// (spec/01-data-model.md, "Scalars"), so a unit is an ordinary entity that
// somebody has to publish; atlas does.
const (
	// UnitPeople is the unit of a population figure.
	UnitPeople = "people"
	// UnitSquareKilometres is the unit of an area figure.
	UnitSquareKilometres = "square kilometres"
)

// The census period every population figure in the dataset is stated for.
const (
	CensusFrom = "2024-01-01T00:00:00Z"
	CensusTo   = "2024-12-31T23:59:59Z"
)

// A Country is one row of the dataset.
type Country struct {
	// Name is the atom description atlas publishes for the country.
	Name string
	// Capital is the atom description atlas publishes for its capital.
	Capital string
	// Population is the figure atlas states for the census period, in people.
	Population int64
	// AreaMantissa and AreaExponent give the area in square kilometres as
	// mantissa × 10^exponent. An exponent of zero is a whole number, which
	// Dialog encodes as a plain integer rather than a decimal fraction
	// (spec/03-encoding.md, "Decimal fractions"); the one fractional area in
	// the dataset is there to exercise the other encoding.
	AreaMantissa int64
	AreaExponent int64
	// Fictional marks a country that does not exist. See the package
	// documentation: exactly one row is fictional, and it is the one the demo's
	// disagreements are about.
	Fictional bool
}

// Countries is the dataset atlas publishes, in publication order.
//
// The population and area figures are approximate. Valdoria is invented; see
// the package documentation.
var Countries = []Country{
	{Name: "France", Capital: "Paris", Population: 68_400_000, AreaMantissa: 551_695},
	{Name: "Germany", Capital: "Berlin", Population: 83_500_000, AreaMantissa: 357_596},
	{Name: "Spain", Capital: "Madrid", Population: 48_800_000, AreaMantissa: 505_990},
	{Name: "Italy", Capital: "Rome", Population: 58_900_000, AreaMantissa: 302_073},
	{Name: "Portugal", Capital: "Lisbon", Population: 10_600_000, AreaMantissa: 92_212},
	{Name: "Netherlands", Capital: "Amsterdam", Population: 17_900_000, AreaMantissa: 41_850},
	{Name: "Belgium", Capital: "Brussels", Population: 11_800_000, AreaMantissa: 30_689},
	{Name: "Austria", Capital: "Vienna", Population: 9_160_000, AreaMantissa: 83_879},
	{Name: "Greece", Capital: "Athens", Population: 10_400_000, AreaMantissa: 131_957},
	{Name: "Poland", Capital: "Warsaw", Population: 36_600_000, AreaMantissa: 312_696},
	{Name: "Valdoria", Capital: "Miravel", Population: 248_000, AreaMantissa: 12_405, AreaExponent: -1, Fictional: true},
}

// Country returns the dataset row with the given name.
func CountryByName(name string) (Country, bool) {
	for _, c := range Countries {
		if c.Name == name {
			return c, true
		}
	}
	return Country{}, false
}

// A NameVariant is an alternative name gazetteer publishes for a country atlas
// already named, together with the atlas name it declares equivalent to it.
type NameVariant struct {
	// Variant is the atom gazetteer creates.
	Variant string
	// Canonical is the atom atlas created, which Variant is declared the same
	// as.
	Canonical string
}

// NameVariants are the naming equivalences gazetteer publishes. Every one of
// them is a real alternative name; none of them is a claim about the world
// beyond "these two strings name the same place".
var NameVariants = []NameVariant{
	{Variant: "Holland", Canonical: "Netherlands"},
	{Variant: "Hellas", Canonical: "Greece"},
	{Variant: "Deutschland", Canonical: "Germany"},
	{Variant: "Lisboa", Canonical: "Lisbon"},
}

// The capital dispute. atlas says Miravel; gazetteer says Port Casta, retracts
// atlas's molecule, and declares the two molecules contradictory. Both places
// are fictional, as is the country. See the package documentation.
const (
	// DisputedCountry is the country whose capital the two authors disagree
	// about.
	DisputedCountry = "Valdoria"
	// RivalCapital is gazetteer's candidate. atlas's is the Capital field of
	// the Valdoria row.
	RivalCapital = "Port Casta"
)

// PolandRevisions are the corrected population figures errata publishes for
// Poland, oldest correction first. Each supersedes the one before it, and the
// first supersedes atlas's original figure, so the chain is
//
//	PolandRevisions[1] supersedes PolandRevisions[0] supersedes atlas's figure
//
// and only the last of the three is current. The figures are invented.
var PolandRevisions = []int64{36_620_000, 36_621_000}

// RetractedEquivalence is the mistake errata makes and takes back: it declares
// its two corrected Poland figures the same statement. They are not — they are
// two revisions of one, and errata itself published the supersession that says
// so — and it retracts the equivalence in its next block.
//
// The point of keeping it in the dataset is what the retraction does. While it
// stood, the two figures would be one equivalence class, and the supersession
// between them would be a class replacing itself: a supersession cycle, with no
// current figure at the end of it. Withdrawn, it declares nothing, and the
// correction chain reads as it should (spec/06-meta-bonds.md, "Withdrawing
// meta-molecules").
func RetractedEquivalence() entity.Molecule {
	first := PopulationMolecule("Poland", PolandRevisions[0])
	second := PopulationMolecule("Poland", PolandRevisions[1])
	return MoleculeEquivalence(first.Digest(), second.Digest())
}

// FlippedPopulation is the Valdoria population figure errata asserts as true
// and, two blocks later, retracts. Both meta-molecules are the same author's,
// so block order settles it and the molecule's truth state is Retracted with no
// conflict (spec/06-meta-bonds.md, "Truth retraction"). The figure is invented,
// like everything else about Valdoria.
const FlippedPopulation int64 = 251_000

// Atom returns the atom for a description. The demo's descriptions are
// constants, so a malformed one is a bug and panics.
func Atom(description string) entity.Atom { return entity.MustAtom(description) }

// Bond returns the bond for a template. As with Atom, the templates are
// constants.
func Bond(template string) entity.Bond { return entity.MustBond(template) }

// CapitalMolecule is "<capital> is the capital of <country>", atlas's wording.
func CapitalMolecule(capital, country string) entity.Molecule {
	return entity.MustMolecule(Bond(TemplateCapitalOf), []entity.Filler{
		entity.AtomFiller(Atom(capital).Digest()),
		entity.AtomFiller(Atom(country).Digest()),
	})
}

// CapitalCityMolecule is "<capital> is the capital city of <country>",
// gazetteer's wording of the same relation.
func CapitalCityMolecule(capital, country string) entity.Molecule {
	return entity.MustMolecule(Bond(TemplateCapitalCityOf), []entity.Filler{
		entity.AtomFiller(Atom(capital).Digest()),
		entity.AtomFiller(Atom(country).Digest()),
	})
}

// PopulationMolecule is "<country> had a population of <people> during
// <census period>". The count carries the "people" unit and the period is a
// datetime range, so one molecule exercises both scalar shapes.
func PopulationMolecule(country string, people int64) entity.Molecule {
	count, err := entity.IntScalar(people).WithUnit(Atom(UnitPeople).Digest())
	if err != nil {
		panic(fmt.Sprintf("content: population scalar for %s: %v", country, err))
	}
	period, err := entity.NewDatetimeRange(CensusFrom, CensusTo)
	if err != nil {
		panic(fmt.Sprintf("content: census period: %v", err))
	}
	return entity.MustMolecule(Bond(TemplatePopulation), []entity.Filler{
		entity.AtomFiller(Atom(country).Digest()),
		scalarFiller(count),
		scalarFiller(period),
	})
}

// AreaMolecule is "<country> has an area of <area> square kilometres".
func AreaMolecule(c Country) entity.Molecule {
	area, err := entity.DecimalScalar(c.AreaExponent, c.AreaMantissa)
	if err != nil {
		panic(fmt.Sprintf("content: area scalar for %s: %v", c.Name, err))
	}
	area, err = area.WithUnit(Atom(UnitSquareKilometres).Digest())
	if err != nil {
		panic(fmt.Sprintf("content: area unit for %s: %v", c.Name, err))
	}
	return entity.MustMolecule(Bond(TemplateArea), []entity.Filler{
		entity.AtomFiller(Atom(c.Name).Digest()),
		scalarFiller(area),
	})
}

func scalarFiller(s entity.Scalar) entity.Filler {
	f, err := entity.ScalarFiller(s)
	if err != nil {
		panic(fmt.Sprintf("content: scalar filler: %v", err))
	}
	return f
}

// AtomEquivalence is "<a> is the same as <b>" over two atom descriptions.
func AtomEquivalence(a, b string) entity.Molecule {
	return metaMolecule(entity.MetaBondEquivalence,
		entity.AtomFiller(Atom(a).Digest()),
		entity.AtomFiller(Atom(b).Digest()))
}

// BondEquivalence is "<a> is the same as <b>" over two bond templates.
func BondEquivalence(a, b string) entity.Molecule {
	return metaMolecule(entity.MetaBondEquivalence,
		entity.BondFiller(Bond(a).Digest()),
		entity.BondFiller(Bond(b).Digest()))
}

// MoleculeEquivalence is "<a> is the same as <b>" over two molecule digests.
func MoleculeEquivalence(a, b cid.Digest) entity.Molecule {
	return metaMolecule(entity.MetaBondEquivalence,
		entity.MoleculeFiller(a), entity.MoleculeFiller(b))
}

// TruthAssertion is "<m> is true".
func TruthAssertion(m cid.Digest) entity.Molecule {
	return metaMolecule(entity.MetaBondTruthAssertion, entity.MoleculeFiller(m))
}

// TruthRetraction is "<m> is untrue".
func TruthRetraction(m cid.Digest) entity.Molecule {
	return metaMolecule(entity.MetaBondTruthRetraction, entity.MoleculeFiller(m))
}

// Contradiction is "<a> contradicts <b>".
func Contradiction(a, b cid.Digest) entity.Molecule {
	return metaMolecule(entity.MetaBondContradiction,
		entity.MoleculeFiller(a), entity.MoleculeFiller(b))
}

// Supersession is "<newer> supersedes <older>".
func Supersession(newer, older cid.Digest) entity.Molecule {
	return metaMolecule(entity.MetaBondSupersession,
		entity.MoleculeFiller(newer), entity.MoleculeFiller(older))
}

func metaMolecule(b entity.Bond, fillers ...entity.Filler) entity.Molecule {
	return entity.MustMolecule(b, fillers)
}
