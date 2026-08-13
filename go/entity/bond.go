package entity

import (
	"bytes"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// keyTemplate is the sole key of a bond (spec/01-data-model.md, "Bonds").
const keyTemplate = "template"

// A Bond is a relationship template — a sentence with named variables:
//
//	bond = { "template" => tstr }
//
// The template MUST be a non-empty UTF-8 string containing one or more
// variables, and variable names MUST be unique within it
// (spec/01-data-model.md, "Bonds"). Variables are parsed with the grammar of
// ParseTemplateVariables.
//
// A bond is content-addressed exactly like an atom, over its template bytes.
// The number of variables it declares is what a molecule's filler count has
// to match, which is why Bond carries the parsed variable list.
//
// The zero Bond is not a bond; Bytes, Digest and CID panic on it. Build one
// with NewBond, MustBond or DecodeBond.
type Bond struct {
	template  string
	variables []string
	enc       []byte // canonical dCBOR; nil only in the zero value
}

// NewBond returns the bond with the given template.
//
// It is an error if the template is empty, is not valid UTF-8, contains no
// variable, or repeats a variable name.
func NewBond(template string) (Bond, error) {
	if template == "" {
		return Bond{}, fmt.Errorf("entity: bond template is empty; it must be a non-empty UTF-8 string with at least one variable")
	}
	if !utf8.ValidString(template) {
		return Bond{}, fmt.Errorf("entity: bond template is not valid UTF-8: %q", template)
	}
	vars := ParseTemplateVariables(template)
	if len(vars) == 0 {
		return Bond{}, fmt.Errorf("entity: bond template %q declares no variables; it must contain at least one _NAME_ variable", template)
	}
	seen := make(map[string]bool, len(vars))
	for _, v := range vars {
		if seen[v] {
			return Bond{}, fmt.Errorf("entity: bond template %q repeats the variable _%s_; variable names must be unique within a template", template, v)
		}
		seen[v] = true
	}
	return Bond{template: template, variables: vars, enc: dcbor.MustEncode(bondValue(template))}, nil
}

// MustBond is NewBond, panicking on error. It is meant for templates known to
// be valid at the call site (tests, constants, the standard meta-bonds).
func MustBond(template string) Bond {
	b, err := NewBond(template)
	if err != nil {
		panic(err)
	}
	return b
}

// DecodeBond parses and validates the canonical dCBOR encoding of a bond.
//
// The input must be exactly the map `{"template": tstr}` in canonical form,
// and the template must satisfy every rule NewBond enforces.
func DecodeBond(b []byte) (Bond, error) {
	m, err := decodeEntityMap(b, "bond", keyTemplate)
	if err != nil {
		return Bond{}, err
	}
	template, err := textField(m, keyTemplate, "bond")
	if err != nil {
		return Bond{}, err
	}
	return NewBond(template)
}

// Template returns the bond's template string.
func (b Bond) Template() string { return b.template }

// Variables returns a copy of the bond's variable names, in the order they
// appear in the template. A molecule's fillers are positionally matched to
// this order (spec/01-data-model.md, "Molecules").
func (b Bond) Variables() []string { return slices.Clone(b.variables) }

// VariableCount returns the number of variables in the template, which is the
// number of fillers a molecule using this bond must carry.
func (b Bond) VariableCount() int { return len(b.variables) }

// Value returns the bond as a dCBOR value.
func (b Bond) Value() dcbor.Value { return bondValue(b.template) }

// Bytes returns a copy of the bond's canonical dCBOR encoding.
func (b Bond) Bytes() []byte { return bytes.Clone(b.encoding()) }

// Digest returns SHA-256(dCBOR(bond)), the form a molecule's bond field and
// every bond reference takes.
func (b Bond) Digest() cid.Digest { return cid.SumDigest(b.encoding()) }

// CID returns the bond's external 36-byte content identifier.
func (b Bond) CID() cid.CID { return b.Digest().CID() }

// String renders the bond for logs and test failures.
func (b Bond) String() string {
	if b.enc == nil {
		return "bond(invalid)"
	}
	return fmt.Sprintf("bond(%q, %s)", b.template, b.CID())
}

func (b Bond) encoding() []byte {
	if b.enc == nil {
		panic("entity: zero-value Bond has no encoding; build bonds with NewBond or DecodeBond")
	}
	return b.enc
}

func bondValue(template string) dcbor.Value {
	return dcbor.Map{{Key: keyTemplate, Value: dcbor.Text(template)}}
}

// ParseTemplateVariables returns the variable names of a bond template, in
// order of appearance and with repeats preserved, per the grammar of
// spec/01-data-model.md, "Bonds":
//
//	variable = "_" 1*UCALPHA "_"
//	UCALPHA  = %x41-5A          ; A-Z
//
// Parsing is leftmost-longest: scanning left to right, an underscore opens a
// candidate variable, the longest following run of uppercase ASCII letters is
// consumed, and the candidate is a variable only if that run is non-empty and
// is immediately followed by a closing underscore. The closing underscore is
// consumed with the variable, so it can also open the next one. Every other
// underscore is literal text.
//
// The rule needs no backtracking: a run of uppercase letters is maximal, so
// if the character after it is not an underscore, no shorter run could end in
// one either. That is what makes the specification's disambiguation cases
// come out as written — "_AB_" is one variable, "_A_B_" is the variable A
// followed by the literal "B_", "_A__B_" is the variables A and B, "type_of"
// and "_a_" hold no variables at all.
//
// Scanning is byte-wise, which is safe because '_' and 'A'-'Z' are ASCII and
// cannot occur inside a multi-byte UTF-8 sequence.
func ParseTemplateVariables(template string) []string {
	var vars []string
	for i := 0; i < len(template); {
		if template[i] != '_' {
			i++
			continue
		}
		j := i + 1
		for j < len(template) && template[j] >= 'A' && template[j] <= 'Z' {
			j++
		}
		if j > i+1 && j < len(template) && template[j] == '_' {
			vars = append(vars, template[i+1:j])
			i = j + 1
			continue
		}
		i++
	}
	return vars
}
