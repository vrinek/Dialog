package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/go/accept"
)

// subscriptionsInput is the argument set of dialog_subscriptions.
type subscriptionsInput struct {
	Action  string   `json:"action,omitempty" jsonschema:"get to report the current subscription set, set to replace it. Defaults to get"`
	Authors []string `json:"authors,omitempty" jsonschema:"the author names to subscribe to, replacing the whole set. Required when action is set; an empty list is allowed and yields an empty view"`
}

// subscriptionsOutput is what dialog_subscriptions returns as structured
// content.
type subscriptionsOutput struct {
	Action     string   `json:"action"`
	Available  []string `json:"available"`
	Subscribed []string `json:"subscribed"`
	Previous   []string `json:"previous,omitempty"`
	Entities   int      `json:"entities"`
	Conflicts  int      `json:"conflicts"`
	Summary    string   `json:"summary"`

	EntityDelta          int           `json:"entity_delta,omitempty"`
	ConflictsAppeared    []conflictOut `json:"conflicts_appeared,omitempty"`
	ConflictsDisappeared []conflictOut `json:"conflicts_disappeared,omitempty"`
}

// Subscriptions is the tool the demo turns on.
//
// L3 is one user's view, and the subscription set is the input that decides
// what is in it: "Only data from subscribed authors passes from L2 to L3"
// (spec/05-processing-model.md, "Filtering rules"). Truth is therefore
// subscription-relative, and this is where that stops being an abstract claim —
// drop the author who disagreed and the disagreement is gone, not because
// anything was resolved but because the other side is no longer in the room.
//
// A view is a snapshot and accept.Build is a pure function, so replacing the
// set is a rebuild rather than a patch, and the answer can report exactly what
// changed by comparing the two views.
func (s *Server) Subscriptions(_ context.Context, _ *mcp.CallToolRequest, in subscriptionsInput) (*mcp.CallToolResult, subscriptionsOutput, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		action = "get"
		if in.Authors != nil {
			action = "set"
		}
	}

	switch action {
	case "get":
		v, subs := s.snapshot()
		out := subscriptionsOutput{
			Action:     "get",
			Available:  s.node.Authors(),
			Subscribed: subs,
			Entities:   v.Len(),
			Conflicts:  len(v.Conflicts()),
		}
		out.Summary = fmt.Sprintf("Subscribed to %d of the %d %s this node holds chains for: %s. "+
			"The view holds %d entities and surfaces %d %s.",
			len(out.Subscribed), len(out.Available),
			plural(len(out.Available), "author", "authors"), joinNames(out.Subscribed),
			out.Entities, out.Conflicts, plural(out.Conflicts, "conflict", "conflicts"))
		return text(s.getText(out)), out, nil

	case "set":
		return s.setSubscriptions(in)

	default:
		return nil, subscriptionsOutput{}, fmt.Errorf("%q is not an action for "+
			"dialog_subscriptions: it takes get, which reports the current set, and set, which "+
			"replaces it", in.Action)
	}
}

func (s *Server) setSubscriptions(in subscriptionsInput) (*mcp.CallToolResult, subscriptionsOutput, error) {
	if in.Authors == nil {
		return nil, subscriptionsOutput{}, fmt.Errorf("setting subscriptions needs an authors "+
			"list; this node holds chains for %s, and an empty list is allowed and yields an "+
			"empty view", joinNames(s.node.Authors()))
	}
	authors, err := s.resolveAuthors(in.Authors)
	if err != nil {
		return nil, subscriptionsOutput{}, err
	}
	before, after, previous, err := s.resubscribe(authors)
	if err != nil {
		return nil, subscriptionsOutput{}, err
	}

	out := subscriptionsOutput{
		Action:      "set",
		Available:   s.node.Authors(),
		Subscribed:  authors,
		Previous:    previous,
		Entities:    after.Len(),
		Conflicts:   len(after.Conflicts()),
		EntityDelta: after.Len() - before.Len(),
	}
	appeared, disappeared := diffConflicts(before.Conflicts(), after.Conflicts())
	for _, c := range appeared {
		out.ConflictsAppeared = append(out.ConflictsAppeared, s.conflict(after, c))
	}
	for _, c := range disappeared {
		out.ConflictsDisappeared = append(out.ConflictsDisappeared, s.conflict(before, c))
	}
	out.Summary = setSummary(out, before.Len(), len(before.Conflicts()))

	return text(s.setText(out)), out, nil
}

// setSummary is the sentence the whole tool exists for: what changed, in
// numbers, in one line.
func setSummary(out subscriptionsOutput, wasEntities, wasConflicts int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Subscribed to %s; was %s. Entities: %d → %d (%+d). Conflicts: %d → %d.",
		joinNames(out.Subscribed), joinNames(out.Previous),
		wasEntities, out.Entities, out.EntityDelta, wasConflicts, out.Conflicts)
	if n := len(out.ConflictsDisappeared); n > 0 {
		fmt.Fprintf(&b, " %d %s disappeared.", n, plural(n, "conflict", "conflicts"))
	}
	if n := len(out.ConflictsAppeared); n > 0 {
		fmt.Fprintf(&b, " %d %s appeared.", n, plural(n, "conflict", "conflicts"))
	}
	if len(out.ConflictsDisappeared) == 0 && len(out.ConflictsAppeared) == 0 {
		b.WriteString(" No conflict appeared or disappeared.")
	}
	return b.String()
}

func (s *Server) getText(out subscriptionsOutput) string {
	var b strings.Builder
	b.WriteString(out.Summary + "\n\n")
	for _, name := range out.Available {
		mark := "  "
		if slices.Contains(out.Subscribed, name) {
			mark = "* "
		}
		fmt.Fprintf(&b, "%s%s\n", mark, s.authorLine(name))
	}
	b.WriteString("\nSubscriptions are local configuration and are never published on-chain. " +
		"They decide what reaches L3: an entity is in the view because one of its authors is " +
		"subscribed, and truth here means truth for this set of authors.\n")
	return b.String()
}

func (s *Server) setText(out subscriptionsOutput) string {
	var b strings.Builder
	b.WriteString(out.Summary + "\n")

	if len(out.ConflictsDisappeared) > 0 {
		b.WriteString("\nGone:\n")
		for _, c := range out.ConflictsDisappeared {
			fmt.Fprintf(&b, "  - %s\n", c.Summary)
		}
		b.WriteString("  Nothing was resolved. The authors who disagreed are no longer both in " +
			"the view, so there is no longer a disagreement to surface — the assertions are still " +
			"in L2, and re-subscribing brings them back.\n")
	}
	if len(out.ConflictsAppeared) > 0 {
		b.WriteString("\nNew:\n")
		for _, c := range out.ConflictsAppeared {
			fmt.Fprintf(&b, "  - %s\n", c.Summary)
		}
	}
	b.WriteString("\nNow subscribed:\n")
	for _, name := range out.Available {
		mark := "  "
		if slices.Contains(out.Subscribed, name) {
			mark = "* "
		}
		fmt.Fprintf(&b, "%s%s\n", mark, s.authorLine(name))
	}
	b.WriteString("\nThis setting belongs to the server process and outlives the call: every " +
		"later dialog_lookup, dialog_truth, dialog_conflicts and dialog_equivalents answers " +
		"against this set until it is changed again.\n")
	return b.String()
}

// authorLine names an author and the chain they publish.
func (s *Server) authorLine(name string) string {
	c, ok := s.node.Chain(name)
	if !ok {
		return name
	}
	return fmt.Sprintf("%s — %d blocks, key %x…, tip %s",
		name, len(c.Blocks), c.Pub[:4], c.Tip())
}

// diffConflicts compares two views' conflicts by identity: kind plus the
// molecules, blocks and meta-molecules involved, which is what makes two
// Conflict values the same conflict (see accept's compareConflicts).
func diffConflicts(before, after []accept.Conflict) (appeared, disappeared []accept.Conflict) {
	was := make([]string, 0, len(before))
	for _, c := range before {
		was = append(was, conflictKey(c))
	}
	is := make([]string, 0, len(after))
	for _, c := range after {
		is = append(is, conflictKey(c))
	}
	for i, c := range after {
		if !slices.Contains(was, is[i]) {
			appeared = append(appeared, c)
		}
	}
	for i, c := range before {
		if !slices.Contains(is, was[i]) {
			disappeared = append(disappeared, c)
		}
	}
	return appeared, disappeared
}

func conflictKey(c accept.Conflict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|", c.Kind)
	for _, d := range c.Molecules {
		b.WriteString(d.String())
	}
	b.WriteString("|")
	for _, d := range c.Blocks {
		b.WriteString(d.String())
	}
	b.WriteString("|")
	for _, d := range c.Meta {
		b.WriteString(d.String())
	}
	return b.String()
}
