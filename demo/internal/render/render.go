// Package render turns Dialog entities into English.
//
// A digest is not an answer. An assistant grounding a claim in Dialog needs the
// sentence a molecule states, and a molecule states nothing on its own: it
// carries the digest of a bond and a list of fillers, and the words live in the
// atoms and the bond template those digests name. This package resolves them
// and substitutes the fillers into the template, so that
//
//	bond      "_A_ is the capital of _B_"
//	fillers   atom(Paris), atom(France)
//
// reads as "Paris is the capital of France", and a meta-molecule over it reads
// as "«Paris is the capital of France» is true".
//
// # Words come from L2, truth from L3
//
// A Renderer reads whatever Source it is given, and the useful source is the L2
// graph rather than an L3 view. L3 filtering is per entity and not transitive,
// so a view can hold a molecule whose bond only an unsubscribed author ever
// published; the specification's answer is that an implementation which has to
// render such a molecule "reads the missing bond or filler from L2 on the
// application's behalf, which supplies the words and not the truth"
// (spec/05-processing-model.md, "Filtering rules"). Rendering from L2 is
// therefore not a leak of unsubscribed content into the view: the view decides
// what exists and what is true, and this package only spells it.
//
// An entity the source does not hold at all is rendered as a placeholder naming
// its digest rather than dropped, because a sentence with a hole in it is
// honest and a missing sentence is not.
//
// # Nothing here is protocol
//
// These renderings are the demo's voice. No part of the specification says how
// a molecule should read to a person, none of this affects a digest, and an
// application that prefers other wording is free to it.
package render

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/graph"
)

// A Source is where a Renderer reads the entities a rendering needs. Both
// *graph.Graph and *accept.View satisfy it; see the package documentation for
// why the graph is usually the right one.
type Source interface {
	Lookup(d cid.Digest) (graph.Entry, bool)
}

// maxDepth bounds the recursion through molecule fillers. Content addressing
// makes a cycle impossible — a molecule would have to carry a digest computed
// over itself — so this guards against a Source that answers with something
// other than the entity it was asked for, and against a legitimately deep
// nesting rendering into an unreadable wall of guillemets.
const maxDepth = 8

// maxDecimalPlaces bounds the expansion of a decimal fraction into positional
// notation. A scalar's exponent is only bounded by the signed 64-bit range
// (spec/03-encoding.md, "Decimal fractions"), so an exponent of -2^40 would
// otherwise ask for a terabyte of zeros; past this many places the scientific
// form is both safe and more readable.
const maxDecimalPlaces = 40

// A Renderer renders the entities of one Source.
//
// It holds no state beyond the source, so it is safe for concurrent use
// whenever the source is, and both *graph.Graph and *accept.View are.
type Renderer struct{ src Source }

// New returns a Renderer reading from src.
func New(src Source) *Renderer { return &Renderer{src: src} }

// Text renders any entity by digest: an atom as its description, a bond as its
// quoted template, a molecule as the sentence its bond and fillers spell.
// A digest the source does not hold renders as a placeholder.
func (r *Renderer) Text(d cid.Digest) string { return r.text(d, 0) }

// Sentence renders a molecule the caller already holds. It is Text for a
// molecule, without requiring the source to hold the molecule itself — only
// the entities its fillers name.
func (r *Renderer) Sentence(m entity.Molecule) string { return r.molecule(m, 0) }

// Short is the abbreviated digest every placeholder and every log line uses:
// the first eight hex characters, which is enough to recognize a digest
// already on screen and never enough to be mistaken for one.
func Short(d cid.Digest) string { return d.String()[:8] }

func (r *Renderer) text(d cid.Digest, depth int) string {
	e, ok := r.src.Lookup(d)
	if !ok {
		return unknown("entity", d)
	}
	switch e.Kind() {
	case block.KindAtom:
		a, ok := e.Atom()
		if !ok {
			return unknown("atom", d)
		}
		return a.Description()
	case block.KindBond:
		b, ok := e.Bond()
		if !ok {
			return unknown("bond", d)
		}
		return quote(b.Template())
	case block.KindMolecule:
		m, ok := e.Molecule()
		if !ok {
			return unknown("molecule", d)
		}
		return r.molecule(m, depth)
	default:
		return unknown("entity", d)
	}
}

func (r *Renderer) molecule(m entity.Molecule, depth int) string {
	if depth > maxDepth {
		return unknown("molecule", m.Digest())
	}
	fillers := m.Fillers()
	e, ok := r.src.Lookup(m.Bond())
	if !ok {
		return r.bondless(m.Bond(), fillers, depth)
	}
	b, ok := e.Bond()
	if !ok {
		return r.bondless(m.Bond(), fillers, depth)
	}
	// A molecule whose filler count does not match its bond is not publishable
	// — block validation rule 5 rejects it (spec/02-block-format.md,
	// "Validation") — but a Renderer is given entities, not blocks, and says
	// what it sees rather than assuming the check was run.
	parts := splitTemplate(b.Template())
	if len(parts) != len(fillers)+1 {
		return fmt.Sprintf("%s with %s", quote(b.Template()), r.fillerList(fillers, depth))
	}
	var out strings.Builder
	for i, lit := range parts {
		out.WriteString(lit)
		if i < len(fillers) {
			out.WriteString(r.filler(fillers[i], depth+1))
		}
	}
	return out.String()
}

// bondless renders a molecule whose bond the source does not hold. The words
// of the sentence are in that bond, so there is no sentence; naming the fillers
// is all that is left, and it is more than nothing.
func (r *Renderer) bondless(bond cid.Digest, fillers []entity.Filler, depth int) string {
	return fmt.Sprintf("%s with %s", unknown("bond", bond), r.fillerList(fillers, depth))
}

func (r *Renderer) fillerList(fillers []entity.Filler, depth int) string {
	if len(fillers) == 0 {
		return "no fillers"
	}
	out := make([]string, 0, len(fillers))
	for _, f := range fillers {
		out = append(out, r.filler(f, depth+1))
	}
	return strings.Join(out, ", ")
}

// filler renders one filler of a molecule (spec/01-data-model.md, "Filler
// types"). A molecule filler is wrapped in guillemets, so that a meta-molecule
// reads as a statement about a statement.
func (r *Renderer) filler(f entity.Filler, depth int) string {
	switch f.Type() {
	case entity.FillerAtom, entity.FillerBond:
		d, ok := f.Ref()
		if !ok {
			return unknown("entity", cid.Digest{})
		}
		return r.text(d, depth)
	case entity.FillerMolecule:
		d, ok := f.Ref()
		if !ok {
			return unknown("molecule", cid.Digest{})
		}
		if _, held := r.src.Lookup(d); !held {
			return unknown("molecule", d)
		}
		return "«" + r.text(d, depth) + "»"
	case entity.FillerIPFSURI:
		uri, ok := f.URI()
		if !ok {
			return "[an IPFS filler with no URI]"
		}
		return uri
	case entity.FillerScalar:
		s, ok := f.Scalar()
		if !ok {
			return "[a scalar filler with no scalar]"
		}
		return r.scalar(s, depth)
	default:
		return fmt.Sprintf("[a filler of unrecognized type %d]", uint64(f.Type()))
	}
}

// scalar renders a scalar filler: a number with the description of its unit
// atom after it, or the two endpoints of a datetime range
// (spec/01-data-model.md, "Scalars").
func (r *Renderer) scalar(s entity.Scalar, depth int) string {
	switch s.Kind() {
	case entity.ScalarDatetimeRange:
		from, to, ok := s.Range()
		if !ok {
			return "[a datetime range with no endpoints]"
		}
		return from + " to " + to
	case entity.ScalarNumber:
		v, ok := s.Number()
		if !ok {
			return "[a number scalar with no number]"
		}
		out := number(v)
		if unit, ok := s.Unit(); ok {
			out += " " + r.text(unit, depth)
		}
		return out
	default:
		return "[a scalar of unrecognized kind]"
	}
}

// number renders a dCBOR number: an integer as its digits, a decimal fraction
// in positional notation.
func number(v dcbor.Value) string {
	switch n := v.(type) {
	case dcbor.Uint:
		return strconv.FormatUint(uint64(n), 10)
	case dcbor.Neg:
		if i, ok := n.Int64(); ok {
			return strconv.FormatInt(i, 10)
		}
		// Neg covers -1..-2^64 and int64 does not reach the far end of it.
		return new(big.Int).Sub(big.NewInt(-1), new(big.Int).SetUint64(uint64(n))).String()
	case dcbor.Decimal:
		return decimal(n.Exponent, n.Mantissa)
	default:
		return fmt.Sprintf("[a %T where a number was expected]", v)
	}
}

// decimal renders mantissa × 10^exponent. The canonical form of
// spec/03-encoding.md, "Decimal fractions", always has a negative exponent —
// a whole number is a plain integer and never a tag 4 — but this handles the
// other signs rather than mis-rendering a value some other producer built.
func decimal(exponent, mantissa int64) string {
	digits := strconv.FormatInt(mantissa, 10)
	sign := ""
	if rest, cut := strings.CutPrefix(digits, "-"); cut {
		sign, digits = "-", rest
	}
	switch {
	case exponent == 0:
		return sign + digits
	case exponent > 0:
		if exponent > maxDecimalPlaces {
			break
		}
		return sign + digits + strings.Repeat("0", int(exponent))
	case exponent >= -maxDecimalPlaces:
		places := int(-exponent)
		if places >= len(digits) {
			digits = strings.Repeat("0", places-len(digits)+1) + digits
		}
		cut := len(digits) - places
		return sign + digits[:cut] + "." + digits[cut:]
	}
	return sign + digits + "e" + strconv.FormatInt(exponent, 10)
}

// splitTemplate splits a bond template into the literal text around its
// variables, so that the i-th filler goes between the i-th and (i+1)-th piece.
// The result always holds one more piece than the template holds variables.
//
// The scan is the grammar of spec/01-data-model.md, "Bonds", read exactly as
// entity.ParseTemplateVariables reads it: leftmost-longest, an underscore opens
// a candidate, the maximal following run of uppercase ASCII closes it only if
// an underscore follows, and every other underscore is literal text.
func splitTemplate(template string) []string {
	parts := []string{}
	start := 0
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
			parts = append(parts, template[start:i])
			i = j + 1
			start = i
			continue
		}
		i++
	}
	return append(parts, template[start:])
}

func quote(s string) string { return `"` + s + `"` }

func unknown(what string, d cid.Digest) string {
	return fmt.Sprintf("[an unpublished %s, %s]", what, Short(d))
}
