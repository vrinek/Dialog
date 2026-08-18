// Package publish builds the demo's three author chains from the curated
// dataset in the content package.
//
// It is the author side of Dialog: one block.Builder per author, signing
// blocks onto a chain, with every block stored and validated the moment it is
// signed. Nothing here is a shortcut around the protocol — a block that would
// not survive block.Validate on a node is an error here, at the moment its
// author produced it.
//
// The output is deterministic. The keys come from fixed seeds, the timestamps
// from a fixed base, Ed25519 signatures are deterministic, and the dCBOR
// encoding is canonical, so Build produces the same bytes on every run, on
// every machine. demo/chains holds those bytes, and cmd/genchains checks that
// they are still the ones this package produces.
//
// # Chain layout
//
// atlas, six blocks:
//
//  0. the three bonds of the dataset and the two unit atoms
//  1. an atom per country and per capital
//  2. a "<capital> is the capital of <country>" molecule per country
//  3. a population molecule per country
//  4. an area molecule per country
//  5. the "_A_ is true" bond, and atlas's assertion that its own claim about
//     the capital of Valdoria is true
//
// gazetteer, four blocks:
//
//  0. the four meta-bonds it uses, its own "capital city" bond, and its own
//     atoms
//  1. three atom equivalences and one bond equivalence
//  2. two molecules in its own wording, and an explicit molecule equivalence
//     for one of them
//  3. the rival capital claim, the retraction of atlas's claim, the assertion
//     of its own, and the contradiction between them
//
// errata, four blocks:
//
//  0. the "_A_ supersedes _B_" bond, the first corrected population figure for
//     Poland, and its supersession of atlas's figure
//  1. the second corrected figure and its supersession of the first
//  2. a population claim about Valdoria and errata's assertion that it is true
//  3. errata's own retraction of that assertion, two blocks later
//
// # Why the order matters
//
// A block may only reference an entity that is reachable from itself, from an
// ancestor in its own chain, or through its refs (spec/02-block-format.md,
// "Validation" rule 4), and within a block only from an operation that
// precedes it. atlas publishes first because the other two reference its
// entities; errata references gazetteer's genesis block for the two truth
// meta-bonds rather than re-publishing them, which is what a refs entry is
// for.
package publish

import (
	"crypto/ed25519"
	"fmt"

	"github.com/vrinek/Dialog/demo/internal/content"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// A Chain is one author's blocks, genesis first.
type Chain struct {
	// Author is the demo name of the author, one of content.Authors.
	Author string
	// Pub is the key every block of the chain is signed with.
	Pub ed25519.PublicKey
	// Blocks are the chain's blocks in publication order.
	Blocks []*block.Block
}

// Build signs, stores and validates the demo's three chains.
//
// The returned store holds every block, which is what makes the chains
// validatable: gazetteer's and errata's blocks reference atlas's, and a
// reference resolves only against blocks a node holds. The chains are returned
// in publication order.
func Build() ([]Chain, *block.MemStore, error) {
	w := &world{store: block.NewMemStore()}
	atlas, err := w.atlas()
	if err != nil {
		return nil, nil, fmt.Errorf("publish: atlas: %w", err)
	}
	gazetteer, err := w.gazetteer(atlas)
	if err != nil {
		return nil, nil, fmt.Errorf("publish: gazetteer: %w", err)
	}
	errata, err := w.errata(atlas, gazetteer)
	if err != nil {
		return nil, nil, fmt.Errorf("publish: errata: %w", err)
	}
	return []Chain{atlas, gazetteer, errata}, w.store, nil
}

// A world is the node the demo's authors publish into: one block store, and a
// counter that stamps each block with the next timestamp.
type world struct {
	store *block.MemStore
	n     int
}

// publish signs one block onto an author's chain, stores it, and validates it
// exactly as a receiving node would.
//
// Validation happens here rather than at load time as well, so that a mistake
// in the dataset — an operation referencing an entity no reachable block
// defines, a filler count that does not match its bond — is an error where it
// was made. The loader validates again from the committed bytes; both paths run
// the same rules.
func (w *world) publish(b *block.Builder, refs []cid.Digest, ops ...block.Operation) (*block.Block, error) {
	blk, err := b.Public(content.TS(w.n), refs, ops...)
	if err != nil {
		return nil, fmt.Errorf("signing block %d: %w", w.n, err)
	}
	w.n++
	if err := w.store.Add(blk); err != nil {
		return nil, fmt.Errorf("storing block %s: %w", blk.Digest(), err)
	}
	report, err := block.Validate(blk, w.store, nil)
	if err != nil {
		return nil, fmt.Errorf("validating block %s: %w", blk.Digest(), err)
	}
	// The demo builds a complete world: every referenced block is held, no
	// chain forks, nothing is private. Any warning at all means the dataset
	// grew a hole, so it is an error rather than a note.
	if len(report.Warnings) > 0 {
		return nil, fmt.Errorf("block %s: %v", blk.Digest(), report.Warnings)
	}
	if len(report.Forks) > 0 {
		return nil, fmt.Errorf("block %s forks the chain: %v", blk.Digest(), report.Forks)
	}
	return blk, nil
}

func builder(author string) (*block.Builder, error) {
	b, err := block.NewBuilder(content.Key(author))
	if err != nil {
		return nil, fmt.Errorf("builder for %s: %w", author, err)
	}
	return b, nil
}

// createMolecule turns a molecule of the content package into the operation
// that publishes it. The molecule was built with entity.MustMolecule against
// its bond, so its filler count is already right; this cannot fail for a
// molecule that exists.
func createMolecule(m entity.Molecule) block.Operation {
	op, err := block.NewCreateMolecule(m.Bond(), m.Fillers())
	if err != nil {
		panic(fmt.Sprintf("publish: create_molecule for %s: %v", m.Digest(), err))
	}
	return op
}

func createAtom(description string) block.Operation {
	return block.MustCreateAtom(description)
}

func createBond(template string) block.Operation {
	return block.MustCreateBond(template)
}

// atlas publishes the primary facts.
func (w *world) atlas() (Chain, error) {
	b, err := builder(content.AuthorAtlas)
	if err != nil {
		return Chain{}, err
	}
	chain := Chain{Author: content.AuthorAtlas, Pub: b.PublicKey()}
	add := func(refs []cid.Digest, ops ...block.Operation) error {
		blk, err := w.publish(b, refs, ops...)
		if err != nil {
			return err
		}
		chain.Blocks = append(chain.Blocks, blk)
		return nil
	}

	// Block 0 — the vocabulary: the three bonds, and the two atoms that name
	// the units its scalars carry. A unit is the digest of an atom, so it has
	// to exist as an entity before a scalar can name it.
	if err := add(nil,
		createBond(content.TemplateCapitalOf),
		createBond(content.TemplatePopulation),
		createBond(content.TemplateArea),
		createAtom(content.UnitPeople),
		createAtom(content.UnitSquareKilometres),
	); err != nil {
		return Chain{}, err
	}

	// Block 1 — the atoms: one per country and one per capital.
	atoms := make([]block.Operation, 0, 2*len(content.Countries))
	for _, c := range content.Countries {
		atoms = append(atoms, createAtom(c.Name), createAtom(c.Capital))
	}
	if err := add(nil, atoms...); err != nil {
		return Chain{}, err
	}

	// Block 2 — the capitals.
	capitals := make([]block.Operation, 0, len(content.Countries))
	for _, c := range content.Countries {
		capitals = append(capitals, createMolecule(content.CapitalMolecule(c.Capital, c.Name)))
	}
	if err := add(nil, capitals...); err != nil {
		return Chain{}, err
	}

	// Block 3 — the populations: an integer with a unit, and a datetime range.
	populations := make([]block.Operation, 0, len(content.Countries))
	for _, c := range content.Countries {
		populations = append(populations, createMolecule(content.PopulationMolecule(c.Name, c.Population)))
	}
	if err := add(nil, populations...); err != nil {
		return Chain{}, err
	}

	// Block 4 — the areas: one of them a decimal fraction.
	areas := make([]block.Operation, 0, len(content.Countries))
	for _, c := range content.Countries {
		areas = append(areas, createMolecule(content.AreaMolecule(c)))
	}
	if err := add(nil, areas...); err != nil {
		return Chain{}, err
	}

	// Block 5 — atlas stands behind its claim about the capital of Valdoria.
	// The meta-bond is an ordinary bond and has to be published before a
	// molecule can name it: the standard meta-bonds are content-addressed
	// templates, not entities every node already holds.
	disputed, ok := content.CountryByName(content.DisputedCountry)
	if !ok {
		return Chain{}, fmt.Errorf("the disputed country %q is not in the dataset", content.DisputedCountry)
	}
	claim := content.CapitalMolecule(disputed.Capital, disputed.Name)
	if err := add(nil,
		createBond(entity.TemplateTruthAssertion),
		createMolecule(content.TruthAssertion(claim.Digest())),
	); err != nil {
		return Chain{}, err
	}
	return chain, nil
}

// gazetteer publishes naming variants, a rival capital claim, and the
// contradiction between that claim and atlas's.
func (w *world) gazetteer(atlas Chain) (Chain, error) {
	b, err := builder(content.AuthorGazetteer)
	if err != nil {
		return Chain{}, err
	}
	chain := Chain{Author: content.AuthorGazetteer, Pub: b.PublicKey()}
	add := func(refs []cid.Digest, ops ...block.Operation) error {
		blk, err := w.publish(b, refs, ops...)
		if err != nil {
			return err
		}
		chain.Blocks = append(chain.Blocks, blk)
		return nil
	}

	// Block 0 — gazetteer's vocabulary. It publishes the four meta-bonds it
	// uses itself: "_A_ is true" is already an entity of atlas's chain, and
	// re-publishing it adds an authorship record rather than a second entity
	// (spec/05-processing-model.md, "Accumulation rules"), which is worth
	// seeing in the demo.
	vocabulary := []block.Operation{
		createBond(entity.TemplateEquivalence),
		createBond(entity.TemplateTruthAssertion),
		createBond(entity.TemplateTruthRetraction),
		createBond(entity.TemplateContradiction),
		createBond(content.TemplateCapitalCityOf),
	}
	for _, v := range content.NameVariants {
		vocabulary = append(vocabulary, createAtom(v.Variant))
	}
	vocabulary = append(vocabulary, createAtom(content.RivalCapital))
	if err := add(nil, vocabulary...); err != nil {
		return Chain{}, err
	}

	// Block 1 — the equivalences. The atom variants name atoms atlas
	// published, and the bond equivalence names atlas's bond, so this block
	// references the two atlas blocks that define them.
	equivalences := make([]block.Operation, 0, len(content.NameVariants)+1)
	for _, v := range content.NameVariants {
		equivalences = append(equivalences, createMolecule(content.AtomEquivalence(v.Variant, v.Canonical)))
	}
	equivalences = append(equivalences, createMolecule(
		content.BondEquivalence(content.TemplateCapitalCityOf, content.TemplateCapitalOf)))
	if err := add([]cid.Digest{atlas.digest(0), atlas.digest(1)}, equivalences...); err != nil {
		return Chain{}, err
	}

	// Block 2 — the same relation in gazetteer's own words, twice.
	//
	// The two molecules have the same shape: gazetteer's bond, an atom of its
	// own that it has declared equivalent to one of atlas's, and an atom of
	// atlas's. The difference is what follows them:
	//
	//   - Lisboa's molecule is declared equivalent to atlas's Lisbon molecule
	//     explicitly, so L3 unifies the two.
	//   - Amsterdam's is not. Its bond is equivalent to atlas's bond and its
	//     country atom is equivalent to atlas's country atom, and the
	//     specification's bond-equivalence example says molecules using either
	//     template are treated as expressing the same relationship — but no
	//     implementation is told to derive molecule equivalence from the
	//     equivalence of the parts, and the reference implementation does not.
	//     See todos/063; the demo keeps both cases so the difference is
	//     testable.
	lisboa := content.CapitalCityMolecule("Lisboa", "Portugal")
	lisbon := content.CapitalMolecule("Lisbon", "Portugal")
	amsterdam := content.CapitalCityMolecule("Amsterdam", "Holland")
	if err := add([]cid.Digest{atlas.digest(1), atlas.digest(2)},
		createMolecule(lisboa),
		createMolecule(content.MoleculeEquivalence(lisboa.Digest(), lisbon.Digest())),
		createMolecule(amsterdam),
	); err != nil {
		return Chain{}, err
	}

	// Block 3 — the dispute. gazetteer publishes its own claim about the
	// capital of Valdoria, says atlas's claim is untrue, says its own is true,
	// and declares the two contradictory. The first two of those produce a
	// truth disagreement between two subscribed authors; the last produces a
	// contradiction. They are different conflicts and the demo surfaces both.
	disputed, ok := content.CountryByName(content.DisputedCountry)
	if !ok {
		return Chain{}, fmt.Errorf("the disputed country %q is not in the dataset", content.DisputedCountry)
	}
	atlasClaim := content.CapitalMolecule(disputed.Capital, disputed.Name)
	rivalClaim := content.CapitalMolecule(content.RivalCapital, disputed.Name)
	if err := add([]cid.Digest{atlas.digest(0), atlas.digest(1), atlas.digest(2)},
		createMolecule(rivalClaim),
		createMolecule(content.TruthRetraction(atlasClaim.Digest())),
		createMolecule(content.TruthAssertion(rivalClaim.Digest())),
		createMolecule(content.Contradiction(rivalClaim.Digest(), atlasClaim.Digest())),
	); err != nil {
		return Chain{}, err
	}
	return chain, nil
}

// errata publishes corrections: a supersession chain, and a retraction of its
// own earlier assertion.
func (w *world) errata(atlas, gazetteer Chain) (Chain, error) {
	b, err := builder(content.AuthorErrata)
	if err != nil {
		return Chain{}, err
	}
	chain := Chain{Author: content.AuthorErrata, Pub: b.PublicKey()}
	add := func(refs []cid.Digest, ops ...block.Operation) error {
		blk, err := w.publish(b, refs, ops...)
		if err != nil {
			return err
		}
		chain.Blocks = append(chain.Blocks, blk)
		return nil
	}

	poland, ok := content.CountryByName("Poland")
	if !ok {
		return Chain{}, fmt.Errorf("Poland is not in the dataset")
	}
	original := content.PopulationMolecule(poland.Name, poland.Population)
	first := content.PopulationMolecule(poland.Name, content.PolandRevisions[0])
	second := content.PopulationMolecule(poland.Name, content.PolandRevisions[1])

	// Block 0 — the supersession bond, the first correction, and the
	// supersession of atlas's original figure. The refs name the atlas blocks
	// that define the bond and unit atom (0), the country atom (1) and the
	// original molecule (3).
	if err := add([]cid.Digest{atlas.digest(0), atlas.digest(1), atlas.digest(3)},
		createBond(entity.TemplateSupersession),
		createMolecule(first),
		createMolecule(content.Supersession(first.Digest(), original.Digest())),
	); err != nil {
		return Chain{}, err
	}

	// Block 1 — the second correction supersedes the first. The first
	// correction is in this chain's own ancestry, so only the atlas blocks
	// defining the bond, the unit and the country atom are referenced.
	if err := add([]cid.Digest{atlas.digest(0), atlas.digest(1)},
		createMolecule(second),
		createMolecule(content.Supersession(second.Digest(), first.Digest())),
	); err != nil {
		return Chain{}, err
	}

	// Block 2 — a claim errata will change its mind about. The truth
	// meta-bonds are not re-published here: errata references gazetteer's
	// genesis block, which defines them, and a reference to another author's
	// block is exactly what refs is for.
	disputed, ok := content.CountryByName(content.DisputedCountry)
	if !ok {
		return Chain{}, fmt.Errorf("the disputed country %q is not in the dataset", content.DisputedCountry)
	}
	flipped := content.PopulationMolecule(disputed.Name, content.FlippedPopulation)
	if err := add([]cid.Digest{atlas.digest(0), atlas.digest(1), gazetteer.digest(0)},
		createMolecule(flipped),
		createMolecule(content.TruthAssertion(flipped.Digest())),
	); err != nil {
		return Chain{}, err
	}

	// Block 3 — errata retracts it. Same author, later block, so block order
	// settles it: the molecule is Retracted and nothing is in conflict
	// (spec/06-meta-bonds.md, "Truth retraction").
	if err := add([]cid.Digest{gazetteer.digest(0)},
		createMolecule(content.TruthRetraction(flipped.Digest())),
	); err != nil {
		return Chain{}, err
	}
	return chain, nil
}

// digest returns the digest of the n-th block of a chain, which is how another
// author names it in refs.
func (c Chain) digest(n int) cid.Digest { return c.Blocks[n].Digest() }
