package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// equivalentsInput is the argument set of dialog_equivalents.
type equivalentsInput struct {
	Digest string `json:"digest" jsonschema:"the entity whose equivalence class to return, as a 64-character hex digest or a CID text form"`
}

// equivalentsOutput is what dialog_equivalents returns as structured content.
type equivalentsOutput struct {
	Entity     entityRef        `json:"entity"`
	Size       int              `json:"size"`
	Class      []entityRef      `json:"class"`
	DeclaredBy []declarationOut `json:"declared_by,omitempty"`
	Subscribed []string         `json:"subscribed"`
}

// Equivalents answers the question a name raises: is this the same thing under
// another word.
//
// A class holds what subscribed authors declared equivalent, transitively, and
// nothing more. Equivalence does not compose through a molecule's parts
// (spec/06-meta-bonds.md, "Equivalence"), so two molecules built from
// equivalent bonds and equivalent fillers stay two classes until somebody
// declares the molecules themselves the same — which is worth saying out loud,
// because it is the opposite of what a reader expects.
func (s *Server) Equivalents(_ context.Context, _ *mcp.CallToolRequest, in equivalentsInput) (*mcp.CallToolResult, equivalentsOutput, error) {
	v, subs := s.snapshot()
	d, err := s.entity(v, subs, in.Digest)
	if err != nil {
		return nil, equivalentsOutput{}, err
	}

	class := v.EquivalenceClass(d)
	out := equivalentsOutput{
		Entity:     s.ref(v, d),
		Size:       len(class),
		Class:      s.refs(v, class),
		DeclaredBy: s.declarations(v, v.EquivalenceDeclarations(d)),
		Subscribed: subs,
	}

	return text(s.equivalentsText(out)), out, nil
}

func (s *Server) equivalentsText(out equivalentsOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", out.Entity.line())

	if out.Size <= 1 {
		fmt.Fprintf(&b, "Equivalence class of one: no subscribed author has declared this the "+
			"same as anything else, with %s subscribed.\n", joinNames(out.Subscribed))
		b.WriteString("That is the ordinary case. Equivalence in Dialog is declared, never " +
			"inferred: two molecules assembled from equivalent bonds and equivalent fillers are " +
			"still two statements until somebody publishes an equivalence between the molecules " +
			"themselves.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "Equivalence class of %d, with %s subscribed. L3 treats these as "+
		"interchangeable: a truth assertion, a contradiction or a supersession naming any one of "+
		"them is a statement about all of them.\n\n", out.Size, joinNames(out.Subscribed))
	for i, r := range out.Class {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.line())
	}
	writeDeclarations(&b, "Declared by:", out.DeclaredBy)
	b.WriteString("\nNothing is merged away: every member keeps its own digest and its own " +
		"authorship, and the class is what carries a truth state.\n")
	return b.String()
}
