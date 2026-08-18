package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/demo/internal/render"
	"github.com/vrinek/Dialog/demo/internal/replay"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A Server is the state behind the six tools: the replayed node, which never
// changes after startup, and the subscription set with the view built for it,
// which dialog_subscriptions replaces.
//
// # One session per process
//
// The subscription set is the server's, not the client's. The MCP stdio
// transport gives one client one process, so that is a session; a server
// reached over a transport that multiplexes sessions would have to key this
// state by session, and this one does not. Changing subscriptions through the
// tool therefore changes what every later call in the process sees, which is
// exactly what makes the demo's point that truth is subscription-relative
// legible in a conversation — and exactly what would surprise a second client.
//
// A Server is safe for concurrent use: the node and the renderer are immutable,
// and the mutable pair (subscriptions, view) is swapped under a mutex. A view
// is immutable once built, so a call that took the view before a resubscription
// finishes its answer against a consistent snapshot.
type Server struct {
	node     *replay.Node
	renderer *render.Renderer
	blocks   map[cid.Digest]blockRef

	mu   sync.RWMutex
	subs []string
	view *accept.View
}

// A blockRef is where a block sits in its author's chain: what an assertion's
// provenance tag means once it is read as a position rather than 32 bytes
// (spec/05-processing-model.md, "Assertion order").
type blockRef struct {
	Author string
	Height int // 1-based, genesis first
	Of     int // the chain's length
}

// NewServer replays nothing: it takes a node that is already loaded and starts
// with every author of it subscribed, which is the view a first-time user of
// the demo should meet. Dropping one is what dialog_subscriptions is for.
func NewServer(n *replay.Node) (*Server, error) {
	if n == nil {
		return nil, errors.New("dialog-mcp: a server needs a replayed node")
	}
	s := &Server{
		node:     n,
		renderer: render.New(n.Graph),
		blocks:   make(map[cid.Digest]blockRef),
		subs:     n.Authors(),
	}
	for _, c := range n.Chains {
		for i, b := range c.Blocks {
			s.blocks[b.Digest()] = blockRef{Author: c.Author, Height: i + 1, Of: len(c.Blocks)}
		}
	}
	v, err := n.View(s.subs...)
	if err != nil {
		return nil, err
	}
	s.view = v
	return s, nil
}

// Register adds the six tools to an MCP server. Every handler is a method of
// this Server, so the tests call them directly and the transport is not in the
// way of testing what they say.
func (s *Server) Register(m *mcp.Server) {
	mcp.AddTool(m, &mcp.Tool{
		Name:  "dialog_lookup",
		Title: "Find Dialog entities by their words",
		Description: "Search the current Dialog view for atoms, bonds and molecules whose text " +
			"contains a substring, case-insensitively. An atom matches on its description, a bond " +
			"on its template, and a molecule on the sentence its bond and fillers spell. Every " +
			"match carries its digest and CID, so it can be passed to the other tools, and the " +
			"authors who published it, so a claim built on it can be attributed.",
	}, s.Lookup)

	mcp.AddTool(m, &mcp.Tool{
		Name:  "dialog_truth",
		Title: "What the view holds about one molecule",
		Description: "Report the truth state of a molecule and the whole record behind it: every " +
			"truth assertion by every subscribed author, which block each was published in, " +
			"whether the molecule has been superseded and by what, what it is declared to " +
			"contradict, and whether it is itself one of the five standard meta-molecules — and " +
			"if so whether its authors have withdrawn it. Dialog surfaces disagreement and never " +
			"resolves it: a conflicted molecule has no winner here.",
	}, s.Truth)

	mcp.AddTool(m, &mcp.Tool{
		Name:  "dialog_conflicts",
		Title: "Every disagreement the view surfaces",
		Description: "List the conflicts the current subscription set produces, grouped by kind: " +
			"truth disagreements, declared contradictions, supersession cycles and ambiguous key " +
			"successions. Each side is rendered as a sentence and attributed to the authors who " +
			"hold it. Nothing here is resolved; the protocol forbids resolving it.",
	}, s.Conflicts)

	mcp.AddTool(m, &mcp.Tool{
		Name:  "dialog_equivalents",
		Title: "The equivalence class of an entity",
		Description: "Return everything a subscribed author has declared to be the same entity as " +
			"this one, transitively, together with the equivalence meta-molecules that declared " +
			"it and who published them. Equivalence is declared and never derived: two molecules " +
			"built from equivalent parts are not equivalent until somebody says so.",
	}, s.Equivalents)

	mcp.AddTool(m, &mcp.Tool{
		Name:  "dialog_provenance",
		Title: "Who published an entity, and in which block",
		Description: "Return the L2 authorship records of an entity: one per author and block that " +
			"published it, unsubscribed authors included, because who published a thing is a fact " +
			"about it and not a matter of subscription. Re-publication accumulates records rather " +
			"than entities, so an entity two authors published has two.",
	}, s.Provenance)

	mcp.AddTool(m, &mcp.Tool{
		Name:  "dialog_subscriptions",
		Title: "Read or replace the subscribed authors",
		Description: "Report the authors this session is subscribed to, or replace the set. " +
			"Subscriptions are what decides which entities reach the view and therefore what is " +
			"true in it: replacing the set rebuilds the view, and the answer says what changed — " +
			"how many entities came or went, and which conflicts appeared or disappeared. The " +
			"setting is per server process and outlives the call.",
	}, s.Subscriptions)
}

// snapshot returns the current view and subscription set together, so that a
// handler's answer describes one state of the server rather than two.
func (s *Server) snapshot() (*accept.View, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view, slices.Clone(s.subs)
}

// resubscribe replaces the subscription set and returns the view before and
// after it. An unknown author name leaves the server untouched.
func (s *Server) resubscribe(authors []string) (before, after *accept.View, previous []string, err error) {
	// Build the new view before taking the write lock: accept.Build is a pure
	// function of the graph, the store and the set, so it needs nothing of the
	// server's mutable state, and a failure must not leave the server changed.
	v, err := s.node.View(authors...)
	if err != nil {
		return nil, nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before, previous = s.view, s.subs
	s.subs, s.view = authors, v
	return before, v, previous, nil
}

// authorName is the reverse lookup every attribution needs: a name a person can
// read, from the 32 bytes a chain is signed with.
func (s *Server) authorName(pub ed25519.PublicKey) string {
	if name, ok := s.node.AuthorName(pub); ok {
		return name
	}
	return "an author outside the demo (" + hex.EncodeToString(pub)[:16] + ")"
}

func (s *Server) authorNames(pubs []ed25519.PublicKey) []string {
	out := make([]string, 0, len(pubs))
	for _, p := range pubs {
		out = append(out, s.authorName(p))
	}
	return out
}

// blockLabel places a block in its author's chain. The provenance tag L2 keeps
// is a digest; the position is what makes "later" mean anything, and it is what
// a reader wants to be told.
func (s *Server) blockLabel(d cid.Digest) string {
	if b, ok := s.blocks[d]; ok {
		return fmt.Sprintf("%s block %d of %d", b.Author, b.Height, b.Of)
	}
	return "a block outside the demo (" + render.Short(d) + ")"
}

// An entityRef is how every response names an entity: what it is, what it says,
// who published it, where the view stands on it, and the two identifiers that
// get it back — the raw digest Dialog carries inside structures, and the CID
// text form it uses outside them (spec/03-encoding.md, "Internal references").
type entityRef struct {
	Kind       string   `json:"kind"`
	Text       string   `json:"text"`
	Digest     string   `json:"digest"`
	CID        string   `json:"cid"`
	Authors    []string `json:"authors"`
	InView     bool     `json:"in_view"`
	Truth      string   `json:"truth,omitempty"`
	Superseded bool     `json:"superseded,omitempty"`
	Withdrawn  bool     `json:"withdrawn,omitempty"`
}

// ref describes one entity against a view.
func (s *Server) ref(v *accept.View, d cid.Digest) entityRef {
	out := entityRef{
		Kind:    "unpublished",
		Text:    s.renderer.Text(d),
		Digest:  d.String(),
		CID:     d.CID().String(),
		Authors: s.publishers(d),
		InView:  v.Has(d),
	}
	kind, ok := s.node.Graph.Kind(d)
	if !ok {
		return out
	}
	out.Kind = kind.String()
	if kind != block.KindMolecule || !out.InView {
		return out
	}
	out.Truth = v.Truth(d).String()
	out.Superseded = v.IsSuperseded(d)
	out.Withdrawn = slices.Contains(v.WithdrawnMetaMolecules(), d)
	return out
}

func (s *Server) refs(v *accept.View, digests []cid.Digest) []entityRef {
	out := make([]entityRef, 0, len(digests))
	for _, d := range digests {
		out = append(out, s.ref(v, d))
	}
	return out
}

// publishers names every author L2 holds an authorship record for, in the
// graph's order (by key, then by block), without repeating an author who
// published the same entity twice.
func (s *Server) publishers(d cid.Digest) []string {
	var out []string
	for _, a := range s.node.Graph.Provenance(d) {
		name := s.authorName(a.Author)
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// line renders an entity as one line of prose: what it says, then how to ask
// about it again.
func (ref entityRef) line() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %s", ref.Kind, ref.Text)
	if ref.Truth != "" {
		fmt.Fprintf(&out, " [truth: %s", ref.Truth)
		if ref.Superseded {
			out.WriteString(", superseded")
		}
		if ref.Withdrawn {
			out.WriteString(", withdrawn")
		}
		out.WriteString("]")
	}
	if len(ref.Authors) > 0 {
		fmt.Fprintf(&out, "\n    published by %s", joinNames(ref.Authors))
	}
	fmt.Fprintf(&out, "\n    digest %s\n    cid    %s", ref.Digest, ref.CID)
	return out.String()
}

// entity resolves a digest argument against the view, with an error saying
// which of the three ways it failed: the identifier is not one, L2 has never
// heard of it, or no subscribed author published it and it is therefore not in
// this view — the last being a fact about the subscription set and the thing
// dialog_subscriptions exists to change.
func (s *Server) entity(v *accept.View, subs []string, arg string) (cid.Digest, error) {
	d, err := parseDigest(arg)
	if err != nil {
		return cid.Digest{}, err
	}
	if v.Has(d) {
		return d, nil
	}
	if !s.node.Graph.Has(d) {
		return cid.Digest{}, fmt.Errorf("no entity with digest %s is in this node's L2 graph; "+
			"use dialog_lookup to find an entity by its words", d)
	}
	return cid.Digest{}, fmt.Errorf("the entity %s (%s) is in L2 but not in this view: "+
		"it was published by %s, and this session is subscribed to %s. "+
		"Use dialog_subscriptions to change that, or dialog_provenance, which reads L2 directly",
		d, s.renderer.Text(d), joinNames(s.publishers(d)), joinNames(subs))
}

// parseDigest accepts the three ways a Dialog entity gets named in text: the
// raw digest as hex, the CID in its canonical base32 text form, and the CID as
// a hex byte dump, which is what the specification's own listings show.
func parseDigest(arg string) (cid.Digest, error) {
	t := strings.TrimSpace(arg)
	if t == "" {
		return cid.Digest{}, errors.New("no digest given: pass the 64-character hex digest of an " +
			"entity, or its CID text form")
	}
	if d, err := cid.ParseDigestHex(t); err == nil {
		return d, nil
	}
	if c, err := cid.ParseCIDString(t); err == nil {
		return c.Digest(), nil
	}
	if c, err := cid.ParseCIDHex(t); err == nil {
		return c.Digest(), nil
	}
	return cid.Digest{}, fmt.Errorf("%q is not a Dialog entity identifier: give either a "+
		"64-character hex digest, or a CID text form (59 characters beginning bafyrei)", t)
}

// authorNamesOf resolves the author names a tool was given, so that a
// misspelling is an error naming the demo's authors rather than a silently
// smaller view.
func (s *Server) resolveAuthors(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if _, ok := s.node.PublicKey(name); !ok {
			return nil, fmt.Errorf("%q is not an author of this demo; it publishes %s",
				raw, joinNames(s.node.Authors()))
		}
		if slices.Contains(out, name) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// metaDeclarations finds the in-view meta-molecules of one standard meta-bond
// whose fillers all name entities in the given set — the equivalences that put
// an equivalence class together, or the supersession that replaced one molecule
// with another.
//
// L3 applies these readings and does not report which meta-molecule produced
// which reading: View.Assertions gives the record for the truth meta-bonds and
// Conflict.Meta gives it for a surfaced contradiction, but an equivalence class
// and a supersession edge arrive unattributed. So the application scans for
// them, which is this function, and which is filed as todos/067.
func (s *Server) metaDeclarations(v *accept.View, metaBond cid.Digest, members []cid.Digest) []cid.Digest {
	withdrawn := v.WithdrawnMetaMolecules()
	var out []cid.Digest
	for _, d := range v.DigestsOfKind(block.KindMolecule) {
		e, ok := v.Lookup(d)
		if !ok {
			continue
		}
		m, ok := e.Molecule()
		if !ok || m.Bond() != metaBond || slices.Contains(withdrawn, d) {
			continue
		}
		named := 0
		for _, f := range m.Fillers() {
			ref, ok := f.Ref()
			if ok && slices.Contains(members, ref) {
				named++
			}
		}
		if named == len(m.Fillers()) && named > 0 {
			out = append(out, d)
		}
	}
	return out
}

// joinNames renders a list the way a sentence wants it.
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return "nobody"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// text wraps a rendered answer as the tool result an assistant reads. The typed
// output value beside it becomes the structured content; this is the part that
// gets quoted.
func text(body string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: body}}}
}
