package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/cid"
)

// conflictKinds are the four disagreements L3 detects, in the order
// View.Conflicts reports them.
var conflictKinds = []accept.ConflictKind{
	accept.ConflictTruthDisagreement,
	accept.ConflictContradiction,
	accept.ConflictSupersessionCycle,
	accept.ConflictAmbiguousSuccession,
}

// conflictsInput is the argument set of dialog_conflicts.
type conflictsInput struct {
	Kind string `json:"kind,omitempty" jsonschema:"restrict the answer to one kind of conflict: truth disagreement, contradiction, supersession cycle or ambiguous succession. Empty reports all four"`
}

// conflictsOutput is what dialog_conflicts returns as structured content.
type conflictsOutput struct {
	Subscribed []string        `json:"subscribed"`
	Total      int             `json:"total"`
	Groups     []conflictGroup `json:"groups"`
}

// A conflictGroup is the conflicts of one kind.
type conflictGroup struct {
	Kind      string        `json:"kind"`
	Count     int           `json:"count"`
	Conflicts []conflictOut `json:"conflicts"`
}

// A conflictOut is one surfaced disagreement.
type conflictOut struct {
	Kind      string      `json:"kind"`
	Summary   string      `json:"summary"`
	Molecules []entityRef `json:"molecules,omitempty"`
	Sides     []sideOut   `json:"sides,omitempty"`
	Meta      []entityRef `json:"meta,omitempty"`
	Blocks    []string    `json:"blocks,omitempty"`
	Declarers []string    `json:"declarers"`
}

// A sideOut is one party to a conflict.
type sideOut struct {
	Stance    string      `json:"stance,omitempty"`
	Molecules []entityRef `json:"molecules,omitempty"`
	Authors   []string    `json:"authors"`
}

// Conflicts is the tool that shows what Dialog does instead of resolving.
//
// "Implementations MUST surface conflicts [...] to the application layer. The
// protocol does NOT require any specific conflict resolution strategy"
// (spec/05-processing-model.md, "Meta-molecule application"). This is that
// surface: every disagreement, both sides of each, and the authors who hold
// them, so that an assistant reports the dispute rather than picking a winner.
func (s *Server) Conflicts(_ context.Context, _ *mcp.CallToolRequest, in conflictsInput) (*mcp.CallToolResult, conflictsOutput, error) {
	v, subs := s.snapshot()
	kinds, err := parseConflictKind(in.Kind)
	if err != nil {
		return nil, conflictsOutput{}, err
	}

	out := conflictsOutput{Subscribed: subs}
	for _, k := range kinds {
		found := v.ConflictsOfKind(k)
		if len(found) == 0 {
			continue
		}
		group := conflictGroup{Kind: k.String(), Count: len(found)}
		for _, c := range found {
			group.Conflicts = append(group.Conflicts, s.conflict(v, c))
		}
		out.Groups = append(out.Groups, group)
		out.Total += len(found)
	}

	return text(s.conflictsText(out, in.Kind)), out, nil
}

// conflict describes one Conflict value.
func (s *Server) conflict(v *accept.View, c accept.Conflict) conflictOut {
	out := conflictOut{
		Kind:      c.Kind.String(),
		Summary:   s.conflictSummary(c),
		Molecules: s.refs(v, c.Molecules),
		Meta:      s.refs(v, c.Meta),
		Declarers: s.authorNames(c.Declarers),
	}
	for _, side := range c.Sides {
		out.Sides = append(out.Sides, sideOut{
			Stance:    side.Stance,
			Molecules: s.refs(v, side.Molecules),
			Authors:   s.authorNames(side.Authors),
		})
	}
	for _, d := range c.Blocks {
		out.Blocks = append(out.Blocks, s.blockLabel(d)+" ("+d.String()+")")
	}
	return out
}

// conflictSummary is the one-line form: enough to say what the disagreement is
// about without unfolding it, which is what a change of subscriptions reports
// and what a list wants at its head.
func (s *Server) conflictSummary(c accept.Conflict) string {
	switch c.Kind {
	case accept.ConflictTruthDisagreement:
		return fmt.Sprintf("truth disagreement over «%s», between %s",
			s.sentence(c.Molecules), joinNames(s.authorNames(c.Declarers)))
	case accept.ConflictContradiction:
		sides := make([]string, 0, len(c.Sides))
		for _, side := range c.Sides {
			sides = append(sides, "«"+s.sentence(side.Molecules)+"»")
		}
		if len(sides) == 0 {
			sides = append(sides, "«"+s.sentence(c.Molecules)+"»")
		}
		return fmt.Sprintf("contradiction between %s, declared by %s",
			strings.Join(sides, " and "), joinNames(s.authorNames(c.Declarers)))
	case accept.ConflictSupersessionCycle:
		return fmt.Sprintf("supersession cycle among %d molecules, so none of them is current",
			len(c.Molecules))
	case accept.ConflictAmbiguousSuccession:
		return fmt.Sprintf("ambiguous key succession between %d blocks, so their author's block "+
			"order is ambiguous too", len(c.Blocks))
	default:
		return c.String()
	}
}

// sentence renders the first molecule of a set and says how many others there
// are, an equivalence class of one being the ordinary case.
func (s *Server) sentence(molecules []cid.Digest) string {
	if len(molecules) == 0 {
		return "no molecule"
	}
	out := s.renderer.Text(molecules[0])
	if n := len(molecules) - 1; n > 0 {
		out += fmt.Sprintf(" (and %d equivalent %s)", n, plural(n, "molecule", "molecules"))
	}
	return out
}

func (s *Server) conflictsText(out conflictsOutput, asked string) string {
	var b strings.Builder
	scope := ""
	if strings.TrimSpace(asked) != "" {
		scope = " of kind " + strings.TrimSpace(asked)
	}
	if out.Total == 0 {
		fmt.Fprintf(&b, "No conflicts%s, with %s subscribed.\n", scope, joinNames(out.Subscribed))
		b.WriteString("Nothing was resolved to get there: the subscribed authors simply do not " +
			"disagree. A different subscription set can produce one — see dialog_subscriptions.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d %s%s, with %s subscribed. Dialog surfaces these and resolves none of "+
		"them; the choice is the application's.\n",
		out.Total, plural(out.Total, "conflict", "conflicts"), scope, joinNames(out.Subscribed))

	for _, g := range out.Groups {
		fmt.Fprintf(&b, "\n%s (%d):\n", g.Kind, g.Count)
		for i, c := range g.Conflicts {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, c.Summary)
			sidesCarryMolecules := false
			for _, side := range c.Sides {
				// A truth disagreement's side is a stance; a contradiction's is
				// an equivalence class and the authors who published it.
				if side.Stance != "" {
					fmt.Fprintf(&b, "     %s: %s\n", side.Stance, joinNames(side.Authors))
				} else {
					fmt.Fprintf(&b, "     held by %s:\n", joinNames(side.Authors))
				}
				for _, r := range side.Molecules {
					sidesCarryMolecules = true
					fmt.Fprintf(&b, "       «%s» (%s)\n", r.Text, r.Digest)
				}
			}
			// A truth disagreement's sides are stances and name no molecules,
			// so the subject is listed once for the conflict as a whole.
			if !sidesCarryMolecules {
				for _, r := range c.Molecules {
					fmt.Fprintf(&b, "     molecule: «%s» (%s)\n", r.Text, r.Digest)
				}
			}
			for _, blk := range c.Blocks {
				fmt.Fprintf(&b, "     block: %s\n", blk)
			}
			for _, r := range c.Meta {
				fmt.Fprintf(&b, "     declared by «%s» (%s)\n", r.Text, r.Digest)
			}
		}
	}
	return b.String()
}

// parseConflictKind turns the optional kind argument into the kinds to report.
func parseConflictKind(arg string) ([]accept.ConflictKind, error) {
	want := strings.ToLower(strings.TrimSpace(arg))
	want = strings.ReplaceAll(want, "_", " ")
	if want == "" {
		return conflictKinds, nil
	}
	for _, k := range conflictKinds {
		if k.String() == want {
			return []accept.ConflictKind{k}, nil
		}
	}
	names := make([]string, 0, len(conflictKinds))
	for _, k := range conflictKinds {
		names = append(names, k.String())
	}
	return nil, fmt.Errorf("%q is not a kind of Dialog conflict; L3 detects %s, and an empty "+
		"kind reports all four", arg, joinNames(names))
}
