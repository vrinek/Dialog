package vectors

import (
	"fmt"
	"strings"

	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// The fixed entities of this file. They are the worked examples of the
// specification wherever it gives one, and otherwise the smallest thing that
// exercises the shape.
const (
	franceDescription = "France"
	parisDescription  = "Paris, the capital of France"
	capitalTemplate   = "_A_ is the capital of _B_"
	eiffelDescription = "The Eiffel Tower"
	metreDescription  = "metre"
	heightTemplate    = "_A_ is _B_ tall"
	parisFranceDescr  = "Paris, France"
	ipfsURI           = "bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii"
	datetimeRangeFrom = "2024-02-20T15:30:00Z"
	datetimeRangeTo   = "2024-02-21T15:30:00Z"
	// The calendar pair spec/01-data-model.md, "Datetime ranges" rule 6, works
	// out in prose: 29 February 1600 exists in the proleptic Gregorian
	// calendar and 29 February 1500 does not, though the Julian calendar then
	// in civil use had it. The first is the valid range below; the second is
	// the timestamp_julian_leap_day case of the invalid section.
	leapDayFrom         = "1600-02-29T00:00:00Z"
	leapDayTo           = "1600-02-29T23:59:59Z"
	julianLeapDay       = "1500-02-29T00:00:00Z"
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
	invalid, err := invalidEntityCases()
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
			{
				Name:        "invalid",
				Description: "Encodings the data model refuses. Every case is well-formed dCBOR — except where it says otherwise — that spec/01-data-model.md rejects, so a decoder that accepts one has entities its peers cannot read. The \"kind\" field names the decoder the bytes are handed to (atom, bond, molecule or filler); each case must be rejected by that decoder.",
				Cases:       invalid,
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

	// "_A_ is true" over an atom, where the meta-bond's Fillers line declares a
	// molecule. The Fillers lines are L3 recognition criteria and not
	// validity rules (spec/06-meta-bonds.md, "Meta-molecules are regular
	// molecules"), so this is a valid molecule — and this case is what pins
	// that reading in bytes.
	truthOfAnAtom, err := entity.NewMoleculeFor(entity.MetaBondTruthAssertion, []entity.Filler{
		entity.AtomFiller(paris.Digest()),
	})
	if err != nil {
		return nil, fmt.Errorf("vectors: truth-of-an-atom meta-molecule: %w", err)
	}

	return []EntityCase{
		moleculeCase("paris_is_the_capital_of_france", "The molecule example of spec/01-data-model.md: \"[Paris, the capital of France] is the capital of [France]\". Two atom fillers, positionally matched to _A_ and _B_.", capitalOf),
		moleculeCase("eiffel_tower_is_330_metres_tall", "A molecule whose second filler is a scalar carrying a unit digest: \"[The Eiffel Tower] is [330 metre] tall\".", height),
		moleculeCase("paris_equivalence", "A meta-molecule: the two Paris atoms are declared the same (spec/06-meta-bonds.md, \"Declaring atom equivalence\"). A meta-molecule is an ordinary molecule; only its bond digest marks it.", equivalence),
		moleculeCase("truth_of_an_atom", "\"_A_ is true\" filled with an atom, where the meta-bond declares a molecule. It is a valid molecule and MUST be accepted: a meta-bond's Fillers line is a recognition criterion applied at L2→L3, not a validity rule (spec/06-meta-bonds.md, \"Meta-molecules are regular molecules\"). L3 reads no truth assertion from it and surfaces it as a plain molecule.", truthOfAnAtom),
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
		{"scalar_datetime_leap_day", "29 February 1600, a whole day. The calendar is the proleptic Gregorian one (spec/01-data-model.md, \"Datetime ranges\" rule 6), so this date exists although the Julian calendar in civil use in 1600 numbered its days differently; the invalid section's timestamp_julian_leap_day is the other half of the pair the specification works out.", func() (entity.Scalar, error) {
			return entity.NewDatetimeRange(leapDayFrom, leapDayTo)
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

// The rules the invalid cases cite. The three the block file already defines —
// ruleFillerTypes, ruleInternalRef and ruleDecimal — are reused as they stand.
const (
	ruleAtoms     = "spec/01-data-model.md, Atoms"
	ruleBonds     = "spec/01-data-model.md, Bonds"
	ruleMolecules = "spec/01-data-model.md, Molecules"
	ruleScalars   = "spec/01-data-model.md, Scalars"
	ruleDatetime  = "spec/01-data-model.md, Datetime ranges"
	ruleClosedMap = "spec/03-encoding.md, Deterministic CBOR rule 8 (closed maps)"
)

// An invalidEntity is one case of the entities file's invalid section: bytes
// the data model refuses, and the decoder that must refuse them.
type invalidEntity struct {
	name, kind, rule, reason string
	// value is encoded to produce the case's bytes. It is nil for the one case
	// whose bytes this module's encoder will not produce, which sets raw.
	value dcbor.Value
	raw   string
}

// invalidEntityCases builds the invalid section: one or more cases for every
// rejection rule of spec/01-data-model.md, plus the closed-map rule as it
// applies to an entity map, a filler and a scalar filler's value.
//
// Every case is checked against the reference decoder before it is emitted, so
// the file cannot come to pin a rejection this implementation does not make.
func invalidEntityCases() ([]InvalidCase, error) {
	france := entity.MustAtom(franceDescription).Digest()
	capital := entity.MustBond(capitalTemplate).Digest()
	metre := entity.MustAtom(metreDescription).Digest()
	digest := dcbor.Bytes(france.Bytes())
	short := dcbor.Bytes(france.Bytes()[:31])
	asCID := dcbor.Bytes(france.CID().Bytes())

	// A filler map, built without the constructors so that invalid ones exist.
	filler := func(typ uint64, v dcbor.Value) dcbor.Value {
		return dcbor.Map{
			{Key: "type", Value: dcbor.Uint(typ)},
			{Key: "value", Value: v},
		}
	}
	scalar := func(entries ...dcbor.MapEntry) dcbor.Value {
		return filler(4, dcbor.Map(entries))
	}
	timestamps := func(from, to string) dcbor.Value {
		return scalar(
			dcbor.MapEntry{Key: "from", Value: dcbor.Text(from)},
			dcbor.MapEntry{Key: "to", Value: dcbor.Text(to)},
		)
	}
	molecule := func(entries ...dcbor.MapEntry) dcbor.Value { return dcbor.Map(entries) }
	bondField := dcbor.MapEntry{Key: "bond", Value: dcbor.Bytes(capital.Bytes())}
	oneFiller := dcbor.MapEntry{Key: "fillers", Value: dcbor.Array{filler(0, digest)}}

	// The one case the encoder refuses to build: a decimal fraction with a
	// zero exponent, which is not the canonical spelling of a whole number
	// (spec/03-encoding.md, "Decimal fractions"). It is derived from the
	// canonical 3.14 filler by replacing the exponent, so that the surrounding
	// bytes are the encoder's own.
	canonical, err := entity.DecimalScalar(scalarDecimalExpo, scalarDecimalMantis)
	if err != nil {
		return nil, fmt.Errorf("vectors: canonical decimal: %w", err)
	}
	canonicalFiller, err := entity.ScalarFiller(canonical)
	if err != nil {
		return nil, fmt.Errorf("vectors: canonical decimal filler: %w", err)
	}
	const canonicalTag4, nonCanonicalTag4 = "c4822119013a", "c4820019013a"
	nonCanonicalDecimal := strings.Replace(hexOf(canonicalFiller.Bytes()), canonicalTag4, nonCanonicalTag4, 1)
	if !strings.HasSuffix(nonCanonicalDecimal, nonCanonicalTag4) {
		return nil, fmt.Errorf("vectors: the canonical 3.14 filler no longer ends in %s", canonicalTag4)
	}

	list := []invalidEntity{
		// Atoms: the description MUST be a non-empty text string, and the map
		// carries that key and no other.
		{name: "atom_empty_description", kind: "atom", rule: ruleAtoms,
			reason: "An atom whose description is the empty string. \"The description MUST be a non-empty UTF-8 string\".",
			value:  dcbor.Map{{Key: "description", Value: dcbor.Text("")}}},
		{name: "atom_description_not_text", kind: "atom", rule: ruleAtoms,
			reason: "An atom whose description is the integer 1. The CDDL binds it to tstr.",
			value:  dcbor.Map{{Key: "description", Value: dcbor.Uint(1)}}},
		{name: "atom_unknown_key", kind: "atom", rule: ruleClosedMap,
			reason: "An atom map carrying a \"lang\" key its definition does not declare. An unknown key is a rejection, never something to ignore: the digest is taken over these bytes.",
			value: dcbor.Map{
				{Key: "description", Value: dcbor.Text(franceDescription)},
				{Key: "lang", Value: dcbor.Text("fr")},
			}},
		{name: "atom_missing_description", kind: "atom", rule: ruleClosedMap,
			reason: "The empty map: the one key an atom's definition requires is absent.",
			value:  dcbor.Map{}},

		// Bonds: a non-empty template with at least one variable, each name
		// used once.
		{name: "bond_empty_template", kind: "bond", rule: ruleBonds,
			reason: "A bond whose template is the empty string.",
			value:  dcbor.Map{{Key: "template", Value: dcbor.Text("")}}},
		{name: "bond_template_without_a_variable", kind: "bond", rule: ruleBonds,
			reason: "\"is the capital of\" declares no variable, and \"The template MUST be a non-empty UTF-8 string containing one or more variables\".",
			value:  dcbor.Map{{Key: "template", Value: dcbor.Text("is the capital of")}}},
		{name: "bond_template_lowercase_variables", kind: "bond", rule: ruleBonds,
			reason: "\"_a_ is the same as _b_\" looks like a two-variable template and is not one: UCALPHA is %x41-5A, so lowercase letters match no variable and this template declares none.",
			value:  dcbor.Map{{Key: "template", Value: dcbor.Text("_a_ is the same as _b_")}}},
		{name: "bond_template_repeats_a_variable", kind: "bond", rule: ruleBonds,
			reason: "\"_A_ is the capital of _A_\" uses the name A twice. \"Variable names MUST be unique within a bond template\", since fillers are matched to variables positionally.",
			value:  dcbor.Map{{Key: "template", Value: dcbor.Text("_A_ is the capital of _A_")}}},

		// Molecules.
		{name: "molecule_missing_fillers", kind: "molecule", rule: ruleClosedMap,
			reason: "A molecule map with a bond and no fillers key.",
			value:  molecule(bondField)},
		{name: "molecule_unknown_key", kind: "molecule", rule: ruleClosedMap,
			reason: "A molecule map carrying a \"ts\" key. A molecule states a relationship and records no time; the block does that.",
			value:  molecule(bondField, oneFiller, dcbor.MapEntry{Key: "ts", Value: dcbor.Uint(0)})},
		{name: "molecule_empty_fillers", kind: "molecule", rule: ruleMolecules,
			reason: "An empty fillers array. The CDDL says [+ filler]: one or more.",
			value:  molecule(bondField, dcbor.MapEntry{Key: "fillers", Value: dcbor.Array{}})},
		{name: "molecule_bond_is_a_cid", kind: "molecule", rule: ruleInternalRef,
			reason: "The bond field carries the 36-byte CIDv1 of the bond instead of its raw 32-byte digest. Inside a Dialog structure every reference is a digest.",
			value:  molecule(dcbor.MapEntry{Key: "bond", Value: asCID}, oneFiller)},

		// Fillers: the type tag and the value are not independent.
		{name: "filler_type_out_of_range", kind: "filler", rule: ruleFillerTypes,
			reason: "A filler of type 5. The v1 vocabulary is types 0 to 4, and \"implementations MUST reject any type tag other than 0 to 4\".",
			value:  filler(5, digest)},
		{name: "filler_type_not_an_integer", kind: "filler", rule: ruleFillerTypes,
			reason: "A filler whose type tag is the text string \"0\".",
			value: dcbor.Map{
				{Key: "type", Value: dcbor.Text("0")},
				{Key: "value", Value: digest},
			}},
		{name: "filler_reference_wrong_size", kind: "filler", rule: ruleFillerTypes,
			reason: "A type 0 filler whose value is 31 bytes. Types 0, 1 and 2 carry bstr .size 32.",
			value:  filler(0, short)},
		{name: "filler_reference_is_a_cid", kind: "filler", rule: ruleInternalRef,
			reason: "A type 1 filler carrying a 36-byte CIDv1. The CID appears at the API boundary, never inside an entity.",
			value:  filler(1, asCID)},
		{name: "filler_reference_is_text", kind: "filler", rule: ruleFillerTypes,
			reason: "A type 2 filler whose value is a text string. The tag names the value shape, and molecule references are byte strings.",
			value:  filler(2, dcbor.Text(franceDescription))},
		{name: "filler_ipfs_uri_empty", kind: "filler", rule: ruleFillerTypes,
			reason: "A type 3 filler carrying the empty string. \"An empty content identifier addresses nothing, and implementations MUST reject it.\"",
			value:  filler(3, dcbor.Text(""))},
		{name: "filler_ipfs_uri_is_bytes", kind: "filler", rule: ruleFillerTypes,
			reason: "A type 3 filler carrying a byte string. An IPFS URI is tstr — type 3 is not an internal reference.",
			value:  filler(3, digest)},
		{name: "filler_scalar_is_an_integer", kind: "filler", rule: ruleFillerTypes,
			reason: "A type 4 filler whose value is the bare integer 330. A scalar filler's value is the scalar-value map, even for a unitless number.",
			value:  filler(4, dcbor.Uint(scalarNumberValue))},
		{name: "filler_unknown_key", kind: "filler", rule: ruleClosedMap,
			reason: "A filler map carrying a \"unit\" key. The unit belongs inside a scalar filler's value, and a filler map declares exactly type and value.",
			value: dcbor.Map{
				{Key: "type", Value: dcbor.Uint(0)},
				{Key: "unit", Value: dcbor.Bytes(metre.Bytes())},
				{Key: "value", Value: digest},
			}},
		{name: "filler_missing_value", kind: "filler", rule: ruleClosedMap,
			reason: "A filler map with a type and no value.",
			value:  dcbor.Map{{Key: "type", Value: dcbor.Uint(0)}}},

		// Scalars: a number with an optional unit, or a datetime range, and
		// never a mixture of the two.
		{name: "scalar_unit_without_value", kind: "filler", rule: ruleScalars,
			reason: "A scalar value carrying a unit and no value. The unit is optional in the CDDL; the value is not.",
			value:  scalar(dcbor.MapEntry{Key: "unit", Value: dcbor.Bytes(metre.Bytes())})},
		{name: "scalar_unit_wrong_size", kind: "filler", rule: ruleScalars,
			reason: "A scalar whose unit is 31 bytes. A unit is the SHA-256 digest of the atom naming it: bstr .size 32.",
			value: scalar(
				dcbor.MapEntry{Key: "unit", Value: short},
				dcbor.MapEntry{Key: "value", Value: dcbor.Uint(scalarNumberValue)},
			)},
		{name: "scalar_value_is_text", kind: "filler", rule: ruleScalars,
			reason: "A scalar whose value is the text string \"330\". A scalar is an integer or a tag 4 decimal fraction.",
			value:  scalar(dcbor.MapEntry{Key: "value", Value: dcbor.Text("330")})},
		{name: "scalar_unknown_key", kind: "filler", rule: ruleClosedMap,
			reason: "A scalar value carrying a \"scale\" key its definition does not declare.",
			value: scalar(
				dcbor.MapEntry{Key: "scale", Value: dcbor.Uint(10)},
				dcbor.MapEntry{Key: "value", Value: dcbor.Uint(scalarNumberValue)},
			)},
		{name: "scalar_non_canonical_decimal", kind: "filler", rule: ruleDecimal,
			reason: "A scalar carrying #6.4([0, 314]). The exponent MUST be negative — a whole number is a plain integer — so these bytes are refused at the dCBOR layer, before the entity layer sees them.",
			raw:    nonCanonicalDecimal},
		{name: "scalar_range_missing_endpoint", kind: "filler", rule: ruleScalars,
			reason: "A datetime range with a from and no to. Both endpoints are required.",
			value:  scalar(dcbor.MapEntry{Key: "from", Value: dcbor.Text(datetimeRangeFrom)})},
		{name: "scalar_range_with_a_unit", kind: "filler", rule: ruleScalars,
			reason: "A datetime range carrying a unit. The two shapes of scalar-value are exclusive: a range is not a quantity.",
			value: scalar(
				dcbor.MapEntry{Key: "from", Value: dcbor.Text(datetimeRangeFrom)},
				dcbor.MapEntry{Key: "to", Value: dcbor.Text(datetimeRangeTo)},
				dcbor.MapEntry{Key: "unit", Value: dcbor.Bytes(metre.Bytes())},
			)},

		// The timestamp profile: one instant, one encoding. Rules 1 to 6 of
		// spec/01-data-model.md, "Datetime ranges", then the ordering rule.
		{name: "timestamp_plain_date", kind: "filler", rule: ruleDatetime,
			reason: "Rule 1 (form). \"2024-02-20\" is a date, and there are no plain dates in Dialog: a day is the range from 00:00:00Z to 23:59:59Z.",
			value:  timestamps("2024-02-20", datetimeRangeTo)},
		{name: "timestamp_numeric_offset", kind: "filler", rule: ruleDatetime,
			reason: "Rule 2 (UTC only). The offset +00:00 denotes the same instant as Z and MUST NOT be used; a second spelling would be a second digest for one statement.",
			value:  timestamps("2024-02-20T15:30:00+00:00", datetimeRangeTo)},
		{name: "timestamp_lowercase_separator", kind: "filler", rule: ruleDatetime,
			reason: "Rule 3 (uppercase). The date-time separator is a lowercase t, which RFC 3339 permits and Dialog does not.",
			value:  timestamps("2024-02-20t15:30:00Z", datetimeRangeTo)},
		{name: "timestamp_lowercase_designator", kind: "filler", rule: ruleDatetime,
			reason: "Rule 3 (uppercase), in the to endpoint: the offset designator is a lowercase z.",
			value:  timestamps(datetimeRangeFrom, "2024-02-21T15:30:00z")},
		{name: "timestamp_fractional_seconds", kind: "filler", rule: ruleDatetime,
			reason: "Rule 4 (second precision). A fractional-second part MUST NOT be present, not even the zero one written here.",
			value:  timestamps("2024-02-20T15:30:00.000Z", datetimeRangeTo)},
		{name: "timestamp_leap_second", kind: "filler", rule: ruleDatetime,
			reason: "Rule 5 (no leap second). The seconds value 60 that RFC 3339 permits is not a Dialog timestamp.",
			value:  timestamps("2024-02-20T23:59:60Z", datetimeRangeTo)},
		{name: "timestamp_not_a_real_date", kind: "filler", rule: ruleDatetime,
			reason: "Rule 6 (a real date). February 2024 has 29 days, so 2024-02-30 denotes no instant. A parser that rolls it forward to 1 March mints a digest for a statement no other implementation holds.",
			value:  timestamps("2024-02-30T00:00:00Z", datetimeRangeTo)},
		{name: "timestamp_julian_leap_day", kind: "filler", rule: ruleDatetime,
			reason: "Rule 6 (a real date), in the proleptic Gregorian calendar: 1500 is divisible by 100 and not by 400, so 29 February 1500 does not exist, though the Julian calendar then in civil use had it. Its counterpart, 1600-02-29, is the valid scalar_datetime_leap_day case.",
			value:  timestamps(julianLeapDay, datetimeRangeTo)},
		{name: "range_from_after_to", kind: "filler", rule: ruleDatetime,
			reason: "A range running backwards: \"from\" MUST NOT be later than \"to\". Timestamps are fixed-width and most-significant-first, so this is a string comparison and needs no date parsing.",
			value:  timestamps(datetimeRangeTo, datetimeRangeFrom)},
	}

	out := make([]InvalidCase, 0, len(list))
	for _, c := range list {
		encoded := c.raw
		if encoded == "" {
			b, err := dcbor.Encode(c.value)
			if err != nil {
				return nil, fmt.Errorf("vectors: invalid entity %s: %w", c.name, err)
			}
			encoded = hexOf(b)
		}
		if err := mustReject(c.kind, encoded); err != nil {
			return nil, fmt.Errorf("vectors: invalid entity %s: %w", c.name, err)
		}
		out = append(out, InvalidCase{Name: c.name, Kind: c.kind, Rule: c.rule, Reason: c.reason, Bytes: encoded})
	}
	return out, nil
}

// mustReject checks that the reference decoder for kind refuses the bytes of an
// invalid case. A vector may never pin a rejection this implementation does not
// make: the file would then require of other implementations what its own
// generator does not do.
func mustReject(kind, encoded string) error {
	b := mustHexBytes(encoded)
	var err error
	switch kind {
	case "atom":
		_, err = entity.DecodeAtom(b)
	case "bond":
		_, err = entity.DecodeBond(b)
	case "molecule":
		_, err = entity.DecodeMolecule(b)
	case "filler":
		_, err = entity.DecodeFiller(b)
	default:
		return fmt.Errorf("kind %q is not one of atom, bond, molecule or filler", kind)
	}
	if err == nil {
		return fmt.Errorf("the reference decoder accepts these bytes as %s", kind)
	}
	return nil
}
