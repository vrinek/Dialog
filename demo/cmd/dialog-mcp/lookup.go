package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// defaultLookupLimit keeps an answer quotable. The demo's whole graph is under
// a hundred entities, so this is a shape for larger datasets rather than a
// restriction anybody here will feel.
const defaultLookupLimit = 25

// lookupInput is the argument set of dialog_lookup.
type lookupInput struct {
	Query string `json:"query" jsonschema:"the substring to look for, matched case-insensitively against atom descriptions, bond templates and the rendered sentence of each molecule"`
	Kind  string `json:"kind,omitempty" jsonschema:"restrict the search to one kind of entity: atom, bond or molecule. Empty searches all three"`
	Limit int    `json:"limit,omitempty" jsonschema:"the maximum number of matches to return. Defaults to 25; the total number of matches is reported either way"`
}

// lookupOutput is what dialog_lookup returns as structured content.
type lookupOutput struct {
	Query      string      `json:"query"`
	Kind       string      `json:"kind,omitempty"`
	Subscribed []string    `json:"subscribed"`
	Total      int         `json:"total"`
	Returned   int         `json:"returned"`
	Matches    []entityRef `json:"matches"`
}

// Lookup is the entry point to everything else: an assistant has words, and
// the other five tools want digests.
//
// The search is over the view rather than the graph, so what it finds is what
// this session's subscriptions admit — an entity only an unsubscribed author
// published is not there to be found, which is the filtering rule of
// spec/05-processing-model.md doing its job and not a lookup failure.
func (s *Server) Lookup(_ context.Context, _ *mcp.CallToolRequest, in lookupInput) (*mcp.CallToolResult, lookupOutput, error) {
	v, subs := s.snapshot()

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, lookupOutput{}, fmt.Errorf("dialog_lookup needs a query: a word or phrase to " +
			"look for, such as a place name or part of a bond template")
	}
	kinds, err := parseKind(in.Kind)
	if err != nil {
		return nil, lookupOutput{}, err
	}
	limit := in.Limit
	switch {
	case limit < 0:
		return nil, lookupOutput{}, fmt.Errorf("a limit of %d is not a number of matches", limit)
	case limit == 0:
		limit = defaultLookupLimit
	}

	needle := strings.ToLower(query)
	var matches []cid.Digest
	for _, kind := range kinds {
		for _, d := range v.DigestsOfKind(kind) {
			if strings.Contains(strings.ToLower(s.renderer.Text(d)), needle) {
				matches = append(matches, d)
			}
		}
	}

	out := lookupOutput{
		Query:      query,
		Kind:       strings.ToLower(strings.TrimSpace(in.Kind)),
		Subscribed: subs,
		Total:      len(matches),
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out.Matches = s.refs(v, matches)
	out.Returned = len(out.Matches)

	return text(s.lookupText(out)), out, nil
}

func (s *Server) lookupText(out lookupOutput) string {
	var b strings.Builder
	scope := "atoms, bonds and molecules"
	if out.Kind != "" {
		scope = out.Kind + "s"
	}
	if out.Total == 0 {
		fmt.Fprintf(&b, "No %s in the view say %q, with %s subscribed.\n",
			scope, out.Query, joinNames(out.Subscribed))
		b.WriteString("Nothing was hidden: the view holds what the subscribed authors published, " +
			"and none of it matches. Try a shorter substring, or dialog_subscriptions to widen " +
			"the view.")
		return b.String()
	}
	fmt.Fprintf(&b, "%d %s in the view %s %q, with %s subscribed",
		out.Total, plural(out.Total, "entity", "entities"), plural(out.Total, "says", "say"),
		out.Query, joinNames(out.Subscribed))
	if out.Returned < out.Total {
		fmt.Fprintf(&b, "; the first %d follow", out.Returned)
	}
	b.WriteString(".\n")
	for i, ref := range out.Matches {
		fmt.Fprintf(&b, "\n%d. %s\n", i+1, ref.line())
	}
	return b.String()
}

// parseKind turns the optional kind argument into the kinds to search, in the
// order the answer lists them.
func parseKind(arg string) ([]block.EntityKind, error) {
	all := []block.EntityKind{block.KindAtom, block.KindBond, block.KindMolecule}
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
		return all, nil
	case "atom", "atoms":
		return []block.EntityKind{block.KindAtom}, nil
	case "bond", "bonds":
		return []block.EntityKind{block.KindBond}, nil
	case "molecule", "molecules":
		return []block.EntityKind{block.KindMolecule}, nil
	default:
		return nil, fmt.Errorf("%q is not a kind of Dialog entity: there are atoms, bonds and "+
			"molecules, and an empty kind searches all three", arg)
	}
}
