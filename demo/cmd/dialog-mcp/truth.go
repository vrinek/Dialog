package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// truthInput is the argument set of dialog_truth.
type truthInput struct {
	Digest string `json:"digest" jsonschema:"the entity to report on, as the 64-character hex digest dialog_lookup returns, or as a CID text form"`
}

// truthOutput is what dialog_truth returns as structured content.
type truthOutput struct {
	Entity                  entityRef        `json:"entity"`
	Truth                   string           `json:"truth"`
	Assertions              []assertionOut   `json:"assertions"`
	Superseded              bool             `json:"superseded"`
	SupersededBy            []entityRef      `json:"superseded_by,omitempty"`
	Current                 []entityRef      `json:"current,omitempty"`
	Supersedes              []entityRef      `json:"supersedes,omitempty"`
	SupersessionDeclaredBy  []declarationOut `json:"supersession_declared_by,omitempty"`
	Contradicts             []entityRef      `json:"contradicts,omitempty"`
	ContradictionDeclaredBy []declarationOut `json:"contradiction_declared_by,omitempty"`
	Equivalents             []entityRef      `json:"equivalents,omitempty"`
	EquivalenceDeclaredBy   []declarationOut `json:"equivalence_declared_by,omitempty"`
	MetaBond                string           `json:"meta_bond,omitempty"`
	Malformed               bool             `json:"malformed,omitempty"`
	Withdrawn               bool             `json:"withdrawn,omitempty"`
	Subscribed              []string         `json:"subscribed"`
}

// An assertionOut is one truth meta-molecule as it bears on the queried
// molecule: who published it, what it says, and where in that author's chain it
// sits, which is what decides their last word.
type assertionOut struct {
	Author      string `json:"author"`
	Stance      string `json:"stance"`
	Says        string `json:"says"`
	Latest      bool   `json:"latest"`
	Block       string `json:"block"`
	BlockDigest string `json:"block_digest"`
	Meta        string `json:"meta"`
	Subject     string `json:"subject"`
	SubjectText string `json:"subject_text,omitempty"`
}

// Truth answers the question an assistant grounding a claim actually has: may
// I say this, and on whose authority.
//
// It reports the state and the whole record behind it. The protocol requires
// both — conflicts MUST be surfaced and conflicting assertions MUST NOT be
// silently discarded (spec/06-meta-bonds.md, "Conflict handling") — so a
// conflicted molecule comes back with both sides named and no winner.
func (s *Server) Truth(_ context.Context, _ *mcp.CallToolRequest, in truthInput) (*mcp.CallToolResult, truthOutput, error) {
	v, subs := s.snapshot()
	d, err := s.entity(v, subs, in.Digest)
	if err != nil {
		return nil, truthOutput{}, err
	}

	out := truthOutput{
		Entity:     s.ref(v, d),
		Truth:      v.Truth(d).String(),
		Superseded: v.IsSuperseded(d),
		Subscribed: subs,
	}
	for _, a := range v.Assertions(d) {
		out.Assertions = append(out.Assertions, s.assertion(a, d))
	}
	out.SupersededBy = s.refs(v, v.SupersededBy(d))
	out.Supersedes = s.refs(v, v.Supersedes(d))
	if out.Superseded {
		out.Current = s.refs(v, v.Current(d))
	}
	out.SupersessionDeclaredBy = s.declarations(v, v.SupersessionDeclarations(d))
	out.Contradicts = s.refs(v, v.Contradictions(d))
	out.ContradictionDeclaredBy = s.declarations(v, v.ContradictionDeclarations(d))
	if class := without(v.EquivalenceClass(d), d); len(class) > 0 {
		out.Equivalents = s.refs(v, class)
		out.EquivalenceDeclaredBy = s.declarations(v, v.EquivalenceDeclarations(d))
	}
	out.MetaBond, out.Malformed, out.Withdrawn = s.metaReading(v, d)

	return text(s.truthText(out)), out, nil
}

// assertion describes one assertion against the molecule that was asked about.
// The subject may be a different molecule: an assertion applies across an
// equivalence class, so an author can have decided this molecule's truth
// without ever naming it.
//
// The block is quoted as a position — "gazetteer block 4 of 4" — because that
// is what decides whose word is last: "the later assertion (by block order)
// takes precedence" (spec/06-meta-bonds.md, "Truth retraction"). L3 carries the
// position it decided by on the assertion itself, so this renders the working
// rather than recomputing it.
func (s *Server) assertion(a accept.Assertion, asked cid.Digest) assertionOut {
	says := "is true"
	if a.Stance == accept.Retracted {
		says = "is untrue"
	}
	out := assertionOut{
		Author:      s.authorName(a.Author),
		Stance:      a.Stance.String(),
		Says:        says,
		Latest:      a.Latest,
		Block:       s.positionLabel(a.Position),
		BlockDigest: a.Block.String(),
		Meta:        a.Meta.String(),
		Subject:     a.Subject.String(),
	}
	if a.Subject != asked {
		out.SubjectText = s.renderer.Text(a.Subject)
	}
	return out
}

// metaReading reports how L3 reads the molecule itself: which standard
// meta-bond it is built on, whether its fillers fit that bond's template, and
// whether every subscribed author who published it has since withdrawn it.
func (s *Server) metaReading(v *accept.View, d cid.Digest) (metaBond string, malformed, withdrawn bool) {
	e, ok := v.Lookup(d)
	if !ok {
		return "", false, false
	}
	m, ok := e.Molecule()
	if !ok {
		return "", false, false
	}
	b, ok := entity.LookupMetaBond(m.Bond())
	if !ok {
		return "", false, false
	}
	return b.Template(),
		slices.Contains(v.MalformedMetaMolecules(), d),
		slices.Contains(v.WithdrawnMetaMolecules(), d)
}

func (s *Server) truthText(out truthOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", out.Entity.line())

	if out.Entity.Kind != block.KindMolecule.String() {
		fmt.Fprintf(&b, "This is %s %s, and only a molecule can be true or untrue: the truth "+
			"meta-bonds take a molecule filler (spec/06-meta-bonds.md, %q).\n",
			article(out.Entity.Kind), out.Entity.Kind, "Truth assertion")
		writeEquivalents(&b, out)
		return b.String()
	}

	fmt.Fprintf(&b, "Truth: %s.\n", out.Truth)
	switch out.Truth {
	case accept.Unasserted.String():
		b.WriteString("No subscribed author has published a truth meta-molecule about it. " +
			"Publication is not an assertion of truth; it is a statement made.\n")
	case accept.Conflicted.String():
		b.WriteString("Subscribed authors disagree, and Dialog does not resolve that: both " +
			"positions stand, and neither has been discarded. See dialog_conflicts.\n")
	case accept.Retracted.String():
		b.WriteString("A subscribed author's last word is that it is untrue, with nobody " +
			"subscribed holding the opposite.\n")
	case accept.Asserted.String():
		b.WriteString("A subscribed author stands behind it, and nobody subscribed contradicts them.\n")
	}

	if len(out.Assertions) == 0 {
		b.WriteString("\nAssertions: none.\n")
	} else {
		fmt.Fprintf(&b, "\nAssertions (%d):\n", len(out.Assertions))
		for _, a := range out.Assertions {
			fmt.Fprintf(&b, "  - %s says it %s, in %s", a.Author, a.Says, a.Block)
			if a.Latest {
				b.WriteString(" (their last word)")
			} else {
				b.WriteString(" (superseded by their own later assertion)")
			}
			if a.SubjectText != "" {
				fmt.Fprintf(&b, "\n    said of the equivalent molecule «%s»", a.SubjectText)
			}
			fmt.Fprintf(&b, "\n    meta-molecule %s\n", a.Meta)
		}
	}

	if out.Superseded {
		b.WriteString("\nSuperseded: yes.\n")
		for _, r := range out.SupersededBy {
			fmt.Fprintf(&b, "  - replaced by «%s» (%s)\n", r.Text, r.Digest)
		}
		for _, r := range out.Current {
			fmt.Fprintf(&b, "  - the current version is «%s» (%s)\n", r.Text, r.Digest)
		}
		b.WriteString("  A superseded molecule is deprecated, not deleted; it is still here.\n")
	} else {
		b.WriteString("\nSuperseded: no.\n")
	}
	for _, r := range out.Supersedes {
		fmt.Fprintf(&b, "  It supersedes «%s» (%s).\n", r.Text, r.Digest)
	}
	writeDeclarations(&b, "The supersession was declared by:", out.SupersessionDeclaredBy)

	if len(out.Contradicts) > 0 {
		b.WriteString("\nDeclared to contradict:\n")
		for _, r := range out.Contradicts {
			fmt.Fprintf(&b, "  - «%s» (%s)\n", r.Text, r.Digest)
		}
		writeDeclarations(&b, "The contradiction was declared by:", out.ContradictionDeclaredBy)
	}

	writeEquivalents(&b, out)
	writeDeclarations(&b, "The equivalence was declared by:", out.EquivalenceDeclaredBy)

	if out.MetaBond != "" {
		fmt.Fprintf(&b, "\nThis is itself a meta-molecule, on the standard bond %q.\n", out.MetaBond)
		switch {
		case out.Malformed:
			b.WriteString("  Its fillers do not fit that bond's template, so L3 reads it as " +
				"nothing at all rather than guessing what was meant.\n")
		case out.Withdrawn:
			b.WriteString("  Every subscribed author who published it has since retracted it, " +
				"so it declares nothing. It is still an entity of the view, and its truth " +
				"state records the retraction.\n")
		default:
			b.WriteString("  It applies: L3 reads it as the statement it makes.\n")
		}
	}

	fmt.Fprintf(&b, "\nSubscribed: %s.\n", joinNames(out.Subscribed))
	return b.String()
}

func writeEquivalents(b *strings.Builder, out truthOutput) {
	if len(out.Equivalents) == 0 {
		b.WriteString("\nEquivalence: nobody has declared it the same as anything else.\n")
		return
	}
	fmt.Fprintf(b, "\nEquivalence: declared the same as %d other %s, which an assertion about "+
		"any one of them reaches:\n", len(out.Equivalents),
		plural(len(out.Equivalents), "entity", "entities"))
	for _, r := range out.Equivalents {
		fmt.Fprintf(b, "  - %s (%s)\n", r.Text, r.Digest)
	}
}

// without returns the digests other than d, keeping the order.
func without(digests []cid.Digest, d cid.Digest) []cid.Digest {
	var out []cid.Digest
	for _, x := range digests {
		if x != d {
			out = append(out, x)
		}
	}
	return out
}

func article(word string) string {
	if word == "" {
		return "a"
	}
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}
