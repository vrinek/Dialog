package vectors

import (
	"fmt"

	"github.com/vrinek/Dialog/go/entity"
)

// The fixed entities of this file. They are the worked examples of the
// specification wherever it gives one, and otherwise the smallest thing that
// exercises the shape.
const (
	franceDescription   = "France"
	parisDescription    = "Paris, the capital of France"
	capitalTemplate     = "_A_ is the capital of _B_"
	eiffelDescription   = "The Eiffel Tower"
	metreDescription    = "metre"
	heightTemplate      = "_A_ is _B_ tall"
	parisFranceDescr    = "Paris, France"
	ipfsURI             = "bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii"
	datetimeRangeFrom   = "2024-02-20T15:30:00Z"
	datetimeRangeTo     = "2024-02-21T15:30:00Z"
	scalarNumberValue   = 330
	scalarDecimalExpo   = -2
	scalarDecimalMantis = 314
)

func entitiesFile() (File, error) {
	doc, err := entitiesDocument()
	if err != nil {
		return File{}, err
	}
	return File{Name: "entities.json", Doc: doc}, nil
}

func entitiesDocument() (Document, error) {
	fillers, err := fillerCases()
	if err != nil {
		return Document{}, err
	}
	molecules, err := moleculeCases()
	if err != nil {
		return Document{}, err
	}
	return Document{
		Vectors:     Format,
		Area:        "entities",
		Description: "The three content-addressed primitives — atoms, bonds and molecules — and the fillers a molecule is built from. Every case gives the structure, its canonical dCBOR bytes, the 32-byte digest those bytes hash to, and the 36-byte CID with its canonical base32 text form.",
		Spec:        []string{"spec/01-data-model.md", "spec/03-encoding.md", "spec/06-meta-bonds.md"},
		Sections: []Section{
			{
				Name:        "atoms",
				Description: "atom = { \"description\" => tstr }. The digest of an atom is SHA-256 over these bytes and nothing else, so two atoms whose descriptions differ by one code point are two entities.",
				Cases:       atomCases(),
			},
			{
				Name:        "bonds",
				Description: "bond = { \"template\" => tstr }. The variables are the _X_ placeholders of the template, in the order they appear; a molecule's fillers are matched to them positionally.",
				Cases:       bondCases(),
			},
			{
				Name:        "meta_bonds",
				Description: "The five standard meta-bonds of spec/06-meta-bonds.md. The specification prints their templates and no digests; these are the identifiers by which every implementation recognizes a meta-molecule, so they are pinned here.",
				Cases:       metaBondCases(),
			},
			{
				Name:        "molecules",
				Description: "molecule = { \"bond\" => bstr .size 32, \"fillers\" => [+ filler] }. The bond is referenced by its raw digest, never by a CID.",
				Cases:       molecules,
			},
			{
				Name:        "fillers",
				Description: "One case per filler type, including every scalar shape: a plain integer, a negative integer, a decimal fraction, a number with a unit, and a datetime range. A filler carries no identifier of its own.",
				Cases:       fillers,
			},
		},
	}, nil
}

func atomCase(name, description, note string) EntityCase {
	a := entity.MustAtom(description)
	return EntityCase{
		Name:        name,
		Kind:        "atom",
		Note:        note,
		Description: description,
		Value:       describe(a.Value()),
		DCBOR:       hexOf(a.Bytes()),
		Digest:      a.Digest().String(),
		CID:         a.CID().HexString(),
		CIDText:     a.CID().String(),
	}
}

func bondCase(name, template, note string) EntityCase {
	b := entity.MustBond(template)
	return EntityCase{
		Name:      name,
		Kind:      "bond",
		Note:      note,
		Template:  template,
		Variables: b.Variables(),
		Value:     describe(b.Value()),
		DCBOR:     hexOf(b.Bytes()),
		Digest:    b.Digest().String(),
		CID:       b.CID().HexString(),
		CIDText:   b.CID().String(),
	}
}

func moleculeCase(name, note string, m entity.Molecule) EntityCase {
	return EntityCase{
		Name:    name,
		Kind:    "molecule",
		Note:    note,
		Value:   describe(m.Value()),
		DCBOR:   hexOf(m.Bytes()),
		Digest:  m.Digest().String(),
		CID:     m.CID().HexString(),
		CIDText: m.CID().String(),
	}
}

func atomCases() []EntityCase {
	return []EntityCase{
		atomCase("france", franceDescription, "The worked example of spec/03-encoding.md, \"Encoding an atom\": every step of it, from the CBOR bytes to the base32 text form, is checked by this case."),
		atomCase("paris_the_capital_of_france", parisDescription, "The atom example of spec/01-data-model.md. Its description is 28 bytes, so the text string takes a one-byte length argument (78 1c)."),
		atomCase("paris_france", parisFranceDescr, "A second name for the same city, and a different entity. The two are related with the \"_A_ is the same as _B_\" meta-bond, never merged by an implementation."),
		atomCase("eiffel_tower", eiffelDescription, "Subject of the scalar molecule below."),
		atomCase("metre", metreDescription, "A unit atom. A scalar filler's optional unit field carries this digest, and that digest is subject to the same reachability rule as any other (spec/02-block-format.md, rule 4)."),
	}
}

func bondCases() []EntityCase {
	return []EntityCase{
		bondCase("capital_of", capitalTemplate, "The bond example of spec/01-data-model.md."),
		bondCase("height", heightTemplate, "A two-variable bond whose second filler is a scalar with a unit."),
	}
}

func metaBondCases() []EntityCase {
	notes := map[string]string{
		entity.TemplateEquivalence:     "Transitive equivalence between two entities of the same kind (spec/06-meta-bonds.md §1).",
		entity.TemplateTruthAssertion:  "Asserts a molecule (§2).",
		entity.TemplateTruthRetraction: "Retracts or denies a molecule (§3).",
		entity.TemplateContradiction:   "Declares two molecules contradictory (§4).",
		entity.TemplateSupersession:    "Versioning: molecule A replaces molecule B (§5).",
	}
	names := map[string]string{
		entity.TemplateEquivalence:     "equivalence",
		entity.TemplateTruthAssertion:  "truth_assertion",
		entity.TemplateTruthRetraction: "truth_retraction",
		entity.TemplateContradiction:   "contradiction",
		entity.TemplateSupersession:    "supersession",
	}
	var cases []EntityCase
	for _, b := range entity.StandardMetaBonds() {
		cases = append(cases, bondCase(names[b.Template()], b.Template(), notes[b.Template()]))
	}
	return cases
}

func moleculeCases() ([]EntityCase, error) {
	paris := entity.MustAtom(parisDescription)
	france := entity.MustAtom(franceDescription)
	capital := entity.MustBond(capitalTemplate)

	capitalOf, err := entity.NewMoleculeFor(capital, []entity.Filler{
		entity.AtomFiller(paris.Digest()),
		entity.AtomFiller(france.Digest()),
	})
	if err != nil {
		return nil, fmt.Errorf("vectors: capital-of molecule: %w", err)
	}

	height, err := heightMolecule()
	if err != nil {
		return nil, err
	}

	equivalence, err := entity.NewMoleculeFor(entity.MetaBondEquivalence, []entity.Filler{
		entity.AtomFiller(paris.Digest()),
		entity.AtomFiller(entity.MustAtom(parisFranceDescr).Digest()),
	})
	if err != nil {
		return nil, fmt.Errorf("vectors: equivalence meta-molecule: %w", err)
	}

	return []EntityCase{
		moleculeCase("paris_is_the_capital_of_france", "The molecule example of spec/01-data-model.md: \"[Paris, the capital of France] is the capital of [France]\". Two atom fillers, positionally matched to _A_ and _B_.", capitalOf),
		moleculeCase("eiffel_tower_is_330_metres_tall", "A molecule whose second filler is a scalar carrying a unit digest: \"[The Eiffel Tower] is [330 metre] tall\".", height),
		moleculeCase("paris_equivalence", "A meta-molecule: the two Paris atoms are declared the same (spec/06-meta-bonds.md, \"Declaring atom equivalence\"). A meta-molecule is an ordinary molecule; only its bond digest marks it.", equivalence),
	}, nil
}

// heightMolecule builds "[The Eiffel Tower] is [330 metre] tall".
func heightMolecule() (entity.Molecule, error) {
	unit, err := entity.IntScalar(scalarNumberValue).WithUnit(entity.MustAtom(metreDescription).Digest())
	if err != nil {
		return entity.Molecule{}, fmt.Errorf("vectors: metre scalar: %w", err)
	}
	scalar, err := entity.ScalarFiller(unit)
	if err != nil {
		return entity.Molecule{}, fmt.Errorf("vectors: scalar filler: %w", err)
	}
	m, err := entity.NewMoleculeFor(entity.MustBond(heightTemplate), []entity.Filler{
		entity.AtomFiller(entity.MustAtom(eiffelDescription).Digest()),
		scalar,
	})
	if err != nil {
		return entity.Molecule{}, fmt.Errorf("vectors: height molecule: %w", err)
	}
	return m, nil
}

func fillerCase(name, note string, f entity.Filler) FillerCase {
	return FillerCase{
		Name:  name,
		Type:  uint64(f.Type()),
		Note:  note,
		Value: describe(f.Value()),
		DCBOR: hexOf(f.Bytes()),
	}
}

func fillerCases() ([]FillerCase, error) {
	france := entity.MustAtom(franceDescription).Digest()
	capital := entity.MustBond(capitalTemplate).Digest()
	metre := entity.MustAtom(metreDescription).Digest()
	molecule, err := heightMolecule()
	if err != nil {
		return nil, err
	}

	scalars := []struct {
		name, note string
		build      func() (entity.Scalar, error)
	}{
		{"scalar_integer", "A unitless whole number. Whole numbers are plain CBOR integers, never decimal fractions.", func() (entity.Scalar, error) {
			return entity.IntScalar(scalarNumberValue), nil
		}},
		{"scalar_negative_integer", "A negative whole number, major type 1.", func() (entity.Scalar, error) {
			return entity.IntScalar(-40), nil
		}},
		{"scalar_decimal_fraction", "3.14 as tag 4 [-2, 314] (spec/03-encoding.md, \"Decimal fractions\").", func() (entity.Scalar, error) {
			return entity.DecimalScalar(scalarDecimalExpo, scalarDecimalMantis)
		}},
		{"scalar_with_unit", "330 metres. The unit field carries the digest of the atom naming the unit, and that atom MUST be reachable from the block (spec/02-block-format.md, rule 4).", func() (entity.Scalar, error) {
			return entity.IntScalar(scalarNumberValue).WithUnit(metre)
		}},
		{"scalar_decimal_with_unit", "3.14 metres: a decimal fraction and a unit together. The map keys sort unit before value.", func() (entity.Scalar, error) {
			s, err := entity.DecimalScalar(scalarDecimalExpo, scalarDecimalMantis)
			if err != nil {
				return entity.Scalar{}, err
			}
			return s.WithUnit(metre)
		}},
		{"scalar_datetime_range", "A datetime range. Both endpoints are UTC RFC 3339 date-times at second precision, and a range never carries a unit.", func() (entity.Scalar, error) {
			return entity.NewDatetimeRange(datetimeRangeFrom, datetimeRangeTo)
		}},
		{"scalar_datetime_instant", "A range whose endpoints are equal — the protocol's way of writing a single instant.", func() (entity.Scalar, error) {
			return entity.NewDatetimeRange(datetimeRangeFrom, datetimeRangeFrom)
		}},
	}

	ipfs, err := entity.IPFSFiller(ipfsURI)
	if err != nil {
		return nil, fmt.Errorf("vectors: ipfs filler: %w", err)
	}
	cases := []FillerCase{
		fillerCase("atom_reference", "Type 0: the raw 32-byte digest of an atom.", entity.AtomFiller(france)),
		fillerCase("bond_reference", "Type 1: the digest of a bond.", entity.BondFiller(capital)),
		fillerCase("molecule_reference", "Type 2: the digest of a molecule. This is how a meta-molecule names the molecule it asserts.", entity.MoleculeFiller(molecule.Digest())),
		fillerCase("ipfs_uri", "Type 3: an IPFS content identifier as text. It is not an internal reference; its format is IPFS's, not Dialog's.", ipfs),
	}
	for _, s := range scalars {
		sc, err := s.build()
		if err != nil {
			return nil, fmt.Errorf("vectors: %s: %w", s.name, err)
		}
		f, err := entity.ScalarFiller(sc)
		if err != nil {
			return nil, fmt.Errorf("vectors: %s: %w", s.name, err)
		}
		cases = append(cases, fillerCase(s.name, s.note, f))
	}
	return cases, nil
}
