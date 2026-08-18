package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// provenanceInput is the argument set of dialog_provenance.
type provenanceInput struct {
	Digest string `json:"digest" jsonschema:"the entity whose authorship records to return, as a 64-character hex digest or a CID text form"`
}

// provenanceOutput is what dialog_provenance returns as structured content.
type provenanceOutput struct {
	Entity     entityRef           `json:"entity"`
	Records    []provenanceRecord  `json:"records"`
	Authors    []string            `json:"authors"`
	InView     bool                `json:"in_view"`
	Subscribed []string            `json:"subscribed"`
	Chains     []provenanceChainer `json:"chains,omitempty"`
}

// A provenanceRecord is one authorship tag: the two things
// spec/05-processing-model.md, "Accumulation rules", requires every entity in
// L2 to carry — who published it, and in which block.
type provenanceRecord struct {
	Author      string `json:"author"`
	Subscribed  bool   `json:"subscribed"`
	Block       string `json:"block"`
	BlockDigest string `json:"block_digest"`
	BlockCID    string `json:"block_cid"`
}

// A provenanceChainer is one author's summary across the records, for an entity
// the same author published more than once.
type provenanceChainer struct {
	Author string `json:"author"`
	Blocks int    `json:"blocks"`
}

// Provenance reads L2 directly, not the view.
//
// Who published a thing is a fact about it and not a matter of subscription, so
// this answers for entities the current subscription set filters out, and it
// names authors the session is not subscribed to. That is the honest boundary:
// the view decides what is true here, and provenance decides who is answerable
// for it.
//
// Re-publication accumulates records rather than entities — "the new authorship
// record is added alongside the existing one. The entity itself is not
// duplicated" — so an entity two authors published has two records and one
// digest.
func (s *Server) Provenance(_ context.Context, _ *mcp.CallToolRequest, in provenanceInput) (*mcp.CallToolResult, provenanceOutput, error) {
	v, subs := s.snapshot()

	d, err := parseDigest(in.Digest)
	if err != nil {
		return nil, provenanceOutput{}, err
	}
	records := s.node.Graph.Provenance(d)
	if len(records) == 0 {
		return nil, provenanceOutput{}, fmt.Errorf("no entity with digest %s is in this node's "+
			"L2 graph, so nothing published it; use dialog_lookup to find an entity by its words", d)
	}

	out := provenanceOutput{
		Entity:     s.ref(v, d),
		InView:     v.Has(d),
		Subscribed: subs,
	}
	byAuthor := map[string]int{}
	for _, r := range records {
		name := s.authorName(r.Author)
		out.Records = append(out.Records, provenanceRecord{
			Author:      name,
			Subscribed:  slices.Contains(subs, name),
			Block:       s.blockLabel(r.Block),
			BlockDigest: r.Block.String(),
			BlockCID:    r.CID().String(),
		})
		if !slices.Contains(out.Authors, name) {
			out.Authors = append(out.Authors, name)
		}
		byAuthor[name]++
	}
	for _, name := range out.Authors {
		if byAuthor[name] > 1 {
			out.Chains = append(out.Chains, provenanceChainer{Author: name, Blocks: byAuthor[name]})
		}
	}

	return text(s.provenanceText(out)), out, nil
}

func (s *Server) provenanceText(out provenanceOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", out.Entity.line())

	fmt.Fprintf(&b, "L2 holds %d authorship %s for it, by %s.\n",
		len(out.Records), plural(len(out.Records), "record", "records"), joinNames(out.Authors))
	for _, r := range out.Records {
		state := "not subscribed in this session"
		if r.Subscribed {
			state = "subscribed"
		}
		fmt.Fprintf(&b, "  - %s (%s), in %s\n    block digest %s\n    block cid    %s\n",
			r.Author, state, r.Block, r.BlockDigest, r.BlockCID)
	}
	if len(out.Records) > len(out.Authors) {
		b.WriteString("\nOne of these authors published it more than once. Re-publication adds a " +
			"record and not an entity: the content is addressed by its digest, so publishing it " +
			"again is publishing the same entity.\n")
	} else if len(out.Authors) > 1 {
		b.WriteString("\nTwo authors published the same content independently, and it is one " +
			"entity with two records rather than two entities: content addressing makes agreement " +
			"identity.\n")
	}
	if out.InView {
		fmt.Fprintf(&b, "\nIt is in the current view (%s subscribed).\n", joinNames(out.Subscribed))
	} else {
		fmt.Fprintf(&b, "\nIt is NOT in the current view: this session is subscribed to %s, and "+
			"none of them published it. L2 knows about it; L3 does not admit it.\n",
			joinNames(out.Subscribed))
	}
	return b.String()
}
