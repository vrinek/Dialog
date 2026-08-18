package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/demo/chains"
	"github.com/vrinek/Dialog/demo/internal/content"
	"github.com/vrinek/Dialog/demo/internal/replay"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// server replays the committed chains and starts a server over them, with all
// three authors subscribed. Every test here starts this way, and each gets its
// own server: the subscription set is process state, and a test that changes it
// must not change another's world.
func server(t *testing.T) *Server {
	t.Helper()
	n, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the committed chains: %v", err)
	}
	s, err := NewServer(n)
	if err != nil {
		t.Fatalf("starting the server: %v", err)
	}
	return s
}

// resultText is the prose half of a tool result — what an assistant quotes.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("the handler returned no result")
	}
	var b strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("a tool answered with %T; every answer here is text", c)
		}
		b.WriteString(tc.Text)
	}
	if b.Len() == 0 {
		t.Fatal("the handler returned an empty answer")
	}
	return b.String()
}

// The dataset's landmarks, recomputed from the content package: a digest is a
// function of the statement, so the tests know where to look without asking the
// chain builder.
func disputedClaims(t *testing.T) (atlasClaim, rivalClaim cid.Digest) {
	t.Helper()
	c, ok := content.CountryByName(content.DisputedCountry)
	if !ok {
		t.Fatalf("the disputed country %q is not in the dataset", content.DisputedCountry)
	}
	return content.CapitalMolecule(c.Capital, c.Name).Digest(),
		content.CapitalMolecule(content.RivalCapital, c.Name).Digest()
}

func mustContain(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not mention %q; it said:\n%s", what, want, body)
		}
	}
}

// TestRegisterExposesTheSixTools checks the half of the server the handlers
// cannot: that AddTool accepted every handler's signature and inferred an input
// schema from it. The transport is in-memory, so this is still a unit test —
// nothing is spawned, and no bytes cross a pipe.
func TestRegisterExposesTheSixTools(t *testing.T) {
	ctx := context.Background()
	m := mcp.NewServer(&mcp.Implementation{Name: "dialog", Version: version}, nil)
	server(t).Register(m)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := m.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting the server: %v", err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %s has no description; the model reads it to decide when to call", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has no input schema", tool.Name)
		}
	}
	slices.Sort(names)
	want := []string{
		"dialog_conflicts", "dialog_equivalents", "dialog_lookup",
		"dialog_provenance", "dialog_subscriptions", "dialog_truth",
	}
	if !slices.Equal(names, want) {
		t.Errorf("the server exposes %v, want %v", names, want)
	}
}

// TestLookupFindsWordsAndTemplates is the entry point to everything else: an
// assistant has words and the other tools want digests.
func TestLookupFindsWordsAndTemplates(t *testing.T) {
	s := server(t)

	// A place name finds the atom and every molecule built on it, and the
	// molecules read as sentences rather than as digests.
	res, out, err := s.Lookup(context.Background(), nil, lookupInput{Query: "paris"})
	if err != nil {
		t.Fatalf("looking up Paris: %v", err)
	}
	body := resultText(t, res)
	if out.Total < 2 {
		t.Fatalf("looking up Paris found %d entities, want the atom and at least one molecule:\n%s",
			out.Total, body)
	}
	var kinds []string
	for _, m := range out.Matches {
		kinds = append(kinds, m.Kind)
	}
	if !slices.Contains(kinds, "atom") || !slices.Contains(kinds, "molecule") {
		t.Errorf("looking up Paris found kinds %v, want an atom and a molecule", kinds)
	}
	mustContain(t, "the Paris lookup", body,
		"Paris is the capital of France",
		content.Atom("Paris").Digest().String(),
		content.CapitalMolecule("Paris", "France").Digest().String(),
		content.Atom("Paris").Digest().CID().String(),
		content.AuthorAtlas,
	)
	// The query is case-insensitive: the dataset spells it "Paris".
	if !strings.Contains(body, "Paris") {
		t.Errorf("a lower-case query did not match the capitalised description:\n%s", body)
	}

	// A bond template is found by its words too, and renders as the template.
	res, out, err = s.Lookup(context.Background(), nil, lookupInput{Query: "is the capital of", Kind: "bond"})
	if err != nil {
		t.Fatalf("looking up the capital bond: %v", err)
	}
	body = resultText(t, res)
	if out.Total != 1 {
		t.Fatalf("looking up the capital bond found %d bonds, want 1:\n%s", out.Total, body)
	}
	if got, want := out.Matches[0].Digest, content.Bond(content.TemplateCapitalOf).Digest().String(); got != want {
		t.Errorf("the capital bond is %s, want %s", got, want)
	}
	mustContain(t, "the bond lookup", body, `"`+content.TemplateCapitalOf+`"`, content.AuthorAtlas)

	// Restricting the kind restricts the answer.
	_, out, err = s.Lookup(context.Background(), nil, lookupInput{Query: "capital", Kind: "atom"})
	if err != nil {
		t.Fatalf("looking up atoms: %v", err)
	}
	for _, m := range out.Matches {
		if m.Kind != "atom" {
			t.Errorf("a kind-restricted lookup returned a %s", m.Kind)
		}
	}

	// The limit caps what is returned and never what is counted.
	_, out, err = s.Lookup(context.Background(), nil, lookupInput{Query: "capital", Limit: 2})
	if err != nil {
		t.Fatalf("looking up with a limit: %v", err)
	}
	if out.Returned != 2 || out.Total <= 2 {
		t.Errorf("a limit of 2 returned %d of %d matches, want 2 of more", out.Returned, out.Total)
	}
}

// TestLookupRejectsBadArguments: an error is an answer too, and a useless one
// is worse than no tool.
func TestLookupRejectsBadArguments(t *testing.T) {
	s := server(t)
	if _, _, err := s.Lookup(context.Background(), nil, lookupInput{Query: "  "}); err == nil {
		t.Error("an empty query was accepted")
	}
	_, _, err := s.Lookup(context.Background(), nil, lookupInput{Query: "Paris", Kind: "planet"})
	if err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	mustContain(t, "the unknown-kind error", err.Error(), "atoms, bonds and molecules")

	// A query nothing matches is an empty answer and not an error: the view
	// holds what it holds.
	res, out, err := s.Lookup(context.Background(), nil, lookupInput{Query: "Atlantis"})
	if err != nil {
		t.Fatalf("a query with no matches errored: %v", err)
	}
	if out.Total != 0 {
		t.Errorf("Atlantis matched %d entities", out.Total)
	}
	mustContain(t, "the empty lookup", resultText(t, res), "dialog_subscriptions")
}

// TestTruthOfTheDisputedClaim is the demo's centre: two subscribed authors
// disagree about the capital of Valdoria, and L3 reports the disagreement with
// both sides named and no winner.
func TestTruthOfTheDisputedClaim(t *testing.T) {
	s := server(t)
	atlasClaim, rivalClaim := disputedClaims(t)

	res, out, err := s.Truth(context.Background(), nil, truthInput{Digest: atlasClaim.String()})
	if err != nil {
		t.Fatalf("asking for the truth of atlas's capital claim: %v", err)
	}
	body := resultText(t, res)

	if out.Truth != accept.Conflicted.String() {
		t.Errorf("atlas's claim is %q, want %q", out.Truth, accept.Conflicted)
	}
	if len(out.Assertions) != 2 {
		t.Fatalf("the claim carries %d assertions, want 2:\n%s", len(out.Assertions), body)
	}
	stances := map[string]string{}
	for _, a := range out.Assertions {
		stances[a.Author] = a.Says
		if !a.Latest {
			t.Errorf("%s's only assertion is not marked as their last word", a.Author)
		}
		if !strings.HasPrefix(a.Block, a.Author+" block ") {
			t.Errorf("%s's assertion is placed in %q, want a block of their own chain", a.Author, a.Block)
		}
	}
	if got := stances[content.AuthorAtlas]; got != "is true" {
		t.Errorf("atlas says %q, want %q", got, "is true")
	}
	if got := stances[content.AuthorGazetteer]; got != "is untrue" {
		t.Errorf("gazetteer says %q, want %q", got, "is untrue")
	}

	// The prose says the same thing, names both authors, and does not resolve.
	mustContain(t, "the truth answer", body,
		"Miravel is the capital of Valdoria",
		"conflicted",
		content.AuthorAtlas,
		content.AuthorGazetteer,
		"is true",
		"is untrue",
		"dialog_conflicts",
		atlasClaim.String(),
	)

	// The contradiction is reported from this side too, with the rival claim
	// rendered rather than named by digest alone.
	if len(out.Contradicts) != 1 {
		t.Fatalf("the claim is declared to contradict %d molecules, want 1", len(out.Contradicts))
	}
	if got := out.Contradicts[0].Digest; got != rivalClaim.String() {
		t.Errorf("the claim contradicts %s, want the rival claim %s", got, rivalClaim)
	}
	mustContain(t, "the truth answer", body, "Port Casta is the capital of Valdoria")

	// Neither side is dropped: the rival claim has its own entry, asserted,
	// because only gazetteer spoke about it.
	_, rival, err := s.Truth(context.Background(), nil, truthInput{Digest: rivalClaim.String()})
	if err != nil {
		t.Fatalf("asking for the truth of the rival claim: %v", err)
	}
	if rival.Truth != accept.Asserted.String() {
		t.Errorf("the rival claim is %q, want %q", rival.Truth, accept.Asserted)
	}
}

// TestTruthReportsSupersession: errata's corrections leave atlas's original
// Poland figure in the view, marked and pointed at its replacement.
func TestTruthReportsSupersession(t *testing.T) {
	s := server(t)
	poland, ok := content.CountryByName("Poland")
	if !ok {
		t.Fatal("Poland is not in the dataset")
	}
	original := content.PopulationMolecule(poland.Name, poland.Population).Digest()
	newest := content.PopulationMolecule(poland.Name, content.PolandRevisions[1]).Digest()

	res, out, err := s.Truth(context.Background(), nil, truthInput{Digest: original.String()})
	if err != nil {
		t.Fatalf("asking for the truth of the original Poland figure: %v", err)
	}
	if !out.Superseded {
		t.Error("the original Poland figure is not marked superseded")
	}
	if len(out.Current) != 1 || out.Current[0].Digest != newest.String() {
		t.Errorf("the current version is %v, want the newest figure %s", out.Current, newest)
	}

	// The correction is attributed: who replaced this figure, and where they
	// said so (accept.View.SupersessionDeclarations, todos/067).
	if len(out.SupersessionDeclaredBy) != 1 {
		t.Fatalf("the supersession is declared by %d meta-molecules, want 1",
			len(out.SupersessionDeclaredBy))
	}
	backing := out.SupersessionDeclaredBy[0].Backing
	if len(backing) != 1 || backing[0].Author != content.AuthorErrata {
		t.Fatalf("the supersession is backed by %v, want errata alone", backing)
	}
	if !strings.HasPrefix(backing[0].Block, content.AuthorErrata+" block ") {
		t.Errorf("the supersession was published in %q, want a block of errata's chain", backing[0].Block)
	}
	mustContain(t, "the supersession answer", resultText(t, res),
		"36600000 people", "36621000 people", "deprecated, not deleted",
		"declared by "+content.AuthorErrata+", in "+backing[0].Block)
}

// TestTruthReportsAWithdrawnMetaMolecule is the worked case of
// spec/06-meta-bonds.md, "Withdrawing meta-molecules": errata declares its two
// corrected Poland figures the same statement and retracts that equivalence a
// block later. The meta-molecule is still an entity of the view, its truth
// state records the retraction, and it declares nothing.
func TestTruthReportsAWithdrawnMetaMolecule(t *testing.T) {
	s := server(t)
	wrong := content.RetractedEquivalence().Digest()

	res, out, err := s.Truth(context.Background(), nil, truthInput{Digest: wrong.String()})
	if err != nil {
		t.Fatalf("asking for the truth of the withdrawn equivalence: %v", err)
	}
	body := resultText(t, res)

	if !out.Withdrawn {
		t.Errorf("the retracted equivalence is not reported as withdrawn:\n%s", body)
	}
	if out.Truth != accept.Retracted.String() {
		t.Errorf("the retracted equivalence is %q, want %q", out.Truth, accept.Retracted)
	}
	if out.MetaBond != entity.TemplateEquivalence {
		t.Errorf("the withdrawn molecule is read as the meta-bond %q, want %q",
			out.MetaBond, entity.TemplateEquivalence)
	}
	mustContain(t, "the withdrawn-equivalence answer", body,
		"is the same as", "withdrawn", "retracted", "declares nothing", content.AuthorErrata)

	// And it unifies nothing: the two figures it named are two classes.
	_, first, err := s.Equivalents(context.Background(), nil, equivalentsInput{
		Digest: content.PopulationMolecule("Poland", content.PolandRevisions[0]).Digest().String(),
	})
	if err != nil {
		t.Fatalf("asking for the class of the first correction: %v", err)
	}
	if first.Size != 1 {
		t.Errorf("the first correction is in a class of %d, want 1: the equivalence was withdrawn",
			first.Size)
	}
}

// TestTruthRejectsUnknownAndUnadmittedDigests: the two ways a digest can fail
// are different facts and get different answers.
func TestTruthRejectsUnknownAndUnadmittedDigests(t *testing.T) {
	s := server(t)

	if _, _, err := s.Truth(context.Background(), nil, truthInput{Digest: "not-a-digest"}); err == nil {
		t.Error("a malformed digest was accepted")
	} else {
		mustContain(t, "the malformed-digest error", err.Error(), "64-character hex digest", "bafyrei")
	}

	absent := content.Atom("Atlantis").Digest()
	if _, _, err := s.Truth(context.Background(), nil, truthInput{Digest: absent.String()}); err == nil {
		t.Error("a digest L2 has never held was accepted")
	} else {
		mustContain(t, "the unknown-entity error", err.Error(), "L2 graph", "dialog_lookup")
	}

	// An entity in L2 that this view does not admit is a third answer, and the
	// one that says what to do about it.
	if _, _, err := s.Subscriptions(context.Background(), nil, subscriptionsInput{
		Action: "set", Authors: []string{content.AuthorAtlas},
	}); err != nil {
		t.Fatalf("dropping gazetteer: %v", err)
	}
	_, rival := disputedClaims(t)
	_, _, err := s.Truth(context.Background(), nil, truthInput{Digest: rival.String()})
	if err == nil {
		t.Fatal("a molecule outside the view was answered for")
	}
	mustContain(t, "the not-in-view error", err.Error(),
		"in L2 but not in this view", content.AuthorGazetteer, "dialog_subscriptions")
}

// TestConflictsListsBothDisagreements: the capital dispute is two different
// disagreements — atlas and gazetteer contradicting each other about the truth
// of one molecule, and gazetteer declaring the two molecules contradictory —
// and L3 surfaces both, resolving neither.
func TestConflictsListsBothDisagreements(t *testing.T) {
	s := server(t)
	atlasClaim, rivalClaim := disputedClaims(t)

	res, out, err := s.Conflicts(context.Background(), nil, conflictsInput{})
	if err != nil {
		t.Fatalf("listing conflicts: %v", err)
	}
	body := resultText(t, res)

	if out.Total != 2 {
		t.Fatalf("the view surfaces %d conflicts, want 2:\n%s", out.Total, body)
	}
	kinds := map[string]conflictGroup{}
	for _, g := range out.Groups {
		kinds[g.Kind] = g
	}
	disagreement, ok := kinds[accept.ConflictTruthDisagreement.String()]
	if !ok || disagreement.Count != 1 {
		t.Fatalf("the view surfaces %d truth disagreements, want 1", disagreement.Count)
	}
	contradiction, ok := kinds[accept.ConflictContradiction.String()]
	if !ok || contradiction.Count != 1 {
		t.Fatalf("the view surfaces %d contradictions, want 1", contradiction.Count)
	}

	// Both sides of the truth disagreement are attributed.
	sides := map[string][]string{}
	for _, side := range disagreement.Conflicts[0].Sides {
		sides[side.Stance] = side.Authors
	}
	if got := sides[accept.StanceTrue]; !slices.Equal(got, []string{content.AuthorAtlas}) {
		t.Errorf("the %q side is held by %v, want atlas", accept.StanceTrue, got)
	}
	if got := sides[accept.StanceUntrue]; !slices.Equal(got, []string{content.AuthorGazetteer}) {
		t.Errorf("the %q side is held by %v, want gazetteer", accept.StanceUntrue, got)
	}

	// The contradiction names both molecules and its declarer.
	var declared []string
	for _, r := range contradiction.Conflicts[0].Molecules {
		declared = append(declared, r.Digest)
	}
	for _, want := range []cid.Digest{atlasClaim, rivalClaim} {
		if !slices.Contains(declared, want.String()) {
			t.Errorf("the contradiction does not name %s", want)
		}
	}
	if got := contradiction.Conflicts[0].Declarers; !slices.Equal(got, []string{content.AuthorGazetteer}) {
		t.Errorf("the contradiction is declared by %v, want gazetteer", got)
	}

	mustContain(t, "the conflicts answer", body,
		"truth disagreement", "contradiction",
		"Miravel is the capital of Valdoria", "Port Casta is the capital of Valdoria",
		content.AuthorAtlas, content.AuthorGazetteer,
		"resolves none of them",
	)

	// Filtering by kind narrows it, and an unknown kind is an informative error.
	_, one, err := s.Conflicts(context.Background(), nil, conflictsInput{Kind: "contradiction"})
	if err != nil {
		t.Fatalf("listing contradictions: %v", err)
	}
	if one.Total != 1 {
		t.Errorf("filtering to contradictions gave %d conflicts, want 1", one.Total)
	}
	if _, _, err := s.Conflicts(context.Background(), nil, conflictsInput{Kind: "argument"}); err == nil {
		t.Error("an unknown conflict kind was accepted")
	}
}

// TestEquivalentsOfHolland: gazetteer publishes "Holland" and declares it the
// same as atlas's "Netherlands". The class holds both, each keeps its own
// digest and its own author, and the equivalence that declared it is named.
func TestEquivalentsOfHolland(t *testing.T) {
	s := server(t)
	holland := content.Atom("Holland").Digest()
	netherlands := content.Atom("Netherlands").Digest()

	res, out, err := s.Equivalents(context.Background(), nil, equivalentsInput{Digest: holland.String()})
	if err != nil {
		t.Fatalf("asking for Holland's equivalence class: %v", err)
	}
	body := resultText(t, res)

	if out.Size != 2 {
		t.Fatalf("Holland is in a class of %d, want 2:\n%s", out.Size, body)
	}
	var digests, authors []string
	for _, r := range out.Class {
		digests = append(digests, r.Digest)
		authors = append(authors, r.Authors...)
	}
	for _, want := range []cid.Digest{holland, netherlands} {
		if !slices.Contains(digests, want.String()) {
			t.Errorf("the class does not hold %s", want)
		}
	}
	for _, want := range []string{content.AuthorAtlas, content.AuthorGazetteer} {
		if !slices.Contains(authors, want) {
			t.Errorf("the class does not attribute anything to %s; it has %v", want, authors)
		}
	}

	// The declaration itself is named, with the author still standing behind
	// it and the block they published it in — accept.View.EquivalenceDeclarations,
	// which is what todos/067 added.
	if len(out.DeclaredBy) != 1 {
		t.Fatalf("the class is declared by %d meta-molecules, want 1", len(out.DeclaredBy))
	}
	decl := out.DeclaredBy[0]
	if got, want := decl.Meta.Digest,
		content.AtomEquivalence("Holland", "Netherlands").Digest().String(); got != want {
		t.Errorf("the class is declared by %s, want gazetteer's equivalence %s", got, want)
	}
	if decl.Template != entity.TemplateEquivalence {
		t.Errorf("the declaration reads %q, want %q", decl.Template, entity.TemplateEquivalence)
	}
	if len(decl.Backing) != 1 || decl.Backing[0].Author != content.AuthorGazetteer {
		t.Fatalf("the declaration is backed by %v, want gazetteer alone", decl.Backing)
	}
	if !strings.HasPrefix(decl.Backing[0].Block, content.AuthorGazetteer+" block ") {
		t.Errorf("the declaration was published in %q, want a block of gazetteer's chain",
			decl.Backing[0].Block)
	}
	mustContain(t, "the equivalents answer", body,
		"Holland", "Netherlands", "Holland is the same as Netherlands",
		content.AuthorAtlas, content.AuthorGazetteer, "interchangeable",
		"declared by "+content.AuthorGazetteer+", in "+decl.Backing[0].Block)

	// An entity nobody has spoken about is a class of one, and says so.
	res, one, err := s.Equivalents(context.Background(), nil,
		equivalentsInput{Digest: content.Atom("Paris").Digest().String()})
	if err != nil {
		t.Fatalf("asking for Paris's equivalence class: %v", err)
	}
	if one.Size != 1 {
		t.Errorf("Paris is in a class of %d, want 1", one.Size)
	}
	mustContain(t, "the singleton class answer", resultText(t, res), "declared, never inferred")
}

// TestProvenanceOfTheTwicePublishedBond: atlas publishes the "_A_ is true"
// bond, and gazetteer publishes it again to use it. Content addressing makes
// that one entity with two authorship records, not two entities.
func TestProvenanceOfTheTwicePublishedBond(t *testing.T) {
	s := server(t)
	truthBond := content.Bond(entity.TemplateTruthAssertion).Digest()

	res, out, err := s.Provenance(context.Background(), nil, provenanceInput{Digest: truthBond.String()})
	if err != nil {
		t.Fatalf("asking for the provenance of the truth bond: %v", err)
	}
	body := resultText(t, res)

	if len(out.Records) != 2 {
		t.Fatalf("the truth bond has %d authorship records, want 2:\n%s", len(out.Records), body)
	}
	if !slices.Equal(out.Authors, []string{content.AuthorAtlas, content.AuthorGazetteer}) &&
		!slices.Equal(out.Authors, []string{content.AuthorGazetteer, content.AuthorAtlas}) {
		t.Errorf("the truth bond is published by %v, want atlas and gazetteer", out.Authors)
	}
	for _, r := range out.Records {
		if !r.Subscribed {
			t.Errorf("%s is not marked subscribed, but the server starts subscribed to everyone", r.Author)
		}
		if !strings.HasPrefix(r.Block, r.Author+" block ") {
			t.Errorf("%s's record is placed in %q, want a block of their own chain", r.Author, r.Block)
		}
	}
	mustContain(t, "the provenance answer", body,
		content.AuthorAtlas, content.AuthorGazetteer,
		`"`+entity.TemplateTruthAssertion+`"`,
		"one entity with two records")

	// An entity with a single publisher reads as one record.
	_, one, err := s.Provenance(context.Background(), nil,
		provenanceInput{Digest: content.Atom("Paris").Digest().String()})
	if err != nil {
		t.Fatalf("asking for the provenance of the Paris atom: %v", err)
	}
	if len(one.Records) != 1 || one.Records[0].Author != content.AuthorAtlas {
		t.Errorf("the Paris atom has records %v, want one by atlas", one.Records)
	}
}

// TestProvenanceAnswersOutsideTheView: L2 is the record of who said what, and
// subscriptions do not edit it.
func TestProvenanceAnswersOutsideTheView(t *testing.T) {
	s := server(t)
	if _, _, err := s.Subscriptions(context.Background(), nil, subscriptionsInput{
		Action: "set", Authors: []string{content.AuthorAtlas, content.AuthorErrata},
	}); err != nil {
		t.Fatalf("dropping gazetteer: %v", err)
	}

	holland := content.Atom("Holland").Digest()
	res, out, err := s.Provenance(context.Background(), nil, provenanceInput{Digest: holland.String()})
	if err != nil {
		t.Fatalf("asking for the provenance of an entity outside the view: %v", err)
	}
	if out.InView {
		t.Error("the Holland atom is reported in view, but gazetteer is not subscribed")
	}
	if !slices.Equal(out.Authors, []string{content.AuthorGazetteer}) {
		t.Errorf("the Holland atom is published by %v, want gazetteer", out.Authors)
	}
	if out.Records[0].Subscribed {
		t.Error("gazetteer is marked subscribed after being dropped")
	}
	mustContain(t, "the out-of-view provenance answer", resultText(t, res),
		"NOT in the current view", content.AuthorGazetteer)

	// A digest L2 has never held has no provenance, and says so.
	if _, _, err := s.Provenance(context.Background(), nil,
		provenanceInput{Digest: content.Atom("Atlantis").Digest().String()}); err == nil {
		t.Error("a digest L2 has never held was answered for")
	}
}

// TestSubscriptionsMakeTheConflictDisappear is the demo's point about L3: the
// same L2 graph yields a different truth for a different subscription set.
// Dropping gazetteer takes away the author who disagreed, and the disagreement
// goes with them — not because anything was resolved, but because the other
// side is no longer in the room.
func TestSubscriptionsMakeTheConflictDisappear(t *testing.T) {
	s := server(t)
	atlasClaim, _ := disputedClaims(t)

	res, before, err := s.Subscriptions(context.Background(), nil, subscriptionsInput{})
	if err != nil {
		t.Fatalf("reading the subscription set: %v", err)
	}
	if !slices.Equal(before.Subscribed, content.Authors) {
		t.Errorf("the server starts subscribed to %v, want %v", before.Subscribed, content.Authors)
	}
	if before.Conflicts != 2 {
		t.Fatalf("the starting view surfaces %d conflicts, want 2", before.Conflicts)
	}
	mustContain(t, "the subscriptions report", resultText(t, res),
		content.AuthorAtlas, content.AuthorGazetteer, content.AuthorErrata, "never published on-chain")

	res, after, err := s.Subscriptions(context.Background(), nil, subscriptionsInput{
		Action:  "set",
		Authors: []string{content.AuthorAtlas, content.AuthorErrata},
	})
	if err != nil {
		t.Fatalf("dropping gazetteer: %v", err)
	}
	body := resultText(t, res)

	if !slices.Equal(after.Subscribed, []string{content.AuthorAtlas, content.AuthorErrata}) {
		t.Errorf("the new set is %v, want atlas and errata", after.Subscribed)
	}
	if !slices.Equal(after.Previous, content.Authors) {
		t.Errorf("the previous set is reported as %v, want %v", after.Previous, content.Authors)
	}
	if after.Conflicts != 0 {
		t.Errorf("after dropping gazetteer the view still surfaces %d conflicts", after.Conflicts)
	}
	if len(after.ConflictsDisappeared) != 2 {
		t.Fatalf("%d conflicts disappeared, want both:\n%s", len(after.ConflictsDisappeared), body)
	}
	if len(after.ConflictsAppeared) != 0 {
		t.Errorf("%d conflicts appeared, want none", len(after.ConflictsAppeared))
	}
	if after.EntityDelta >= 0 {
		t.Errorf("the entity count changed by %+d; dropping an author should shrink the view",
			after.EntityDelta)
	}

	// The delta message is the answer an assistant quotes when the world it is
	// grounding in changes under it.
	wantDelta := fmt.Sprintf("Entities: %d → %d (%+d)",
		after.Entities-after.EntityDelta, after.Entities, after.EntityDelta)
	mustContain(t, "the subscription change summary", after.Summary,
		"Subscribed to atlas and errata; was atlas, gazetteer and errata",
		wantDelta,
		"Conflicts: 2 → 0",
		"2 conflicts disappeared",
	)
	mustContain(t, "the subscription change answer", body,
		after.Summary,
		"truth disagreement over",
		"contradiction between",
		"Nothing was resolved",
		"re-subscribing brings them back",
	)

	// And the claim that was conflicted is now simply true, on atlas's word.
	_, truth, err := s.Truth(context.Background(), nil, truthInput{Digest: atlasClaim.String()})
	if err != nil {
		t.Fatalf("asking for the truth of atlas's claim after the change: %v", err)
	}
	if truth.Truth != accept.Asserted.String() {
		t.Errorf("with gazetteer dropped, atlas's claim is %q, want %q", truth.Truth, accept.Asserted)
	}
	if len(truth.Assertions) != 1 {
		t.Errorf("with gazetteer dropped, the claim carries %d assertions, want 1", len(truth.Assertions))
	}
}

// TestSubscriptionsRejectUnknownAuthorsAndLeaveTheViewAlone: a misspelled
// author would otherwise be a silently smaller world.
func TestSubscriptionsRejectUnknownAuthorsAndLeaveTheViewAlone(t *testing.T) {
	s := server(t)
	_, before, err := s.Subscriptions(context.Background(), nil, subscriptionsInput{})
	if err != nil {
		t.Fatalf("reading the subscription set: %v", err)
	}

	_, _, err = s.Subscriptions(context.Background(), nil, subscriptionsInput{
		Action: "set", Authors: []string{content.AuthorAtlas, "cartographer"},
	})
	if err == nil {
		t.Fatal("an unknown author was accepted")
	}
	mustContain(t, "the unknown-author error", err.Error(),
		"cartographer", content.AuthorAtlas, content.AuthorGazetteer, content.AuthorErrata)

	_, after, err := s.Subscriptions(context.Background(), nil, subscriptionsInput{})
	if err != nil {
		t.Fatalf("re-reading the subscription set: %v", err)
	}
	if !slices.Equal(after.Subscribed, before.Subscribed) || after.Entities != before.Entities {
		t.Errorf("a rejected change moved the view: %v/%d became %v/%d",
			before.Subscribed, before.Entities, after.Subscribed, after.Entities)
	}

	// An unknown action is an error; an omitted one with authors is a set.
	if _, _, err := s.Subscriptions(context.Background(), nil,
		subscriptionsInput{Action: "unsubscribe"}); err == nil {
		t.Error("an unknown action was accepted")
	}
	_, empty, err := s.Subscriptions(context.Background(), nil,
		subscriptionsInput{Authors: []string{}})
	if err != nil {
		t.Fatalf("subscribing to nobody: %v", err)
	}
	if empty.Action != "set" || empty.Entities != 0 {
		t.Errorf("subscribing to nobody gave action %q and %d entities, want a set and an empty view",
			empty.Action, empty.Entities)
	}
}

// TestDigestArgumentsAcceptBothForms: a digest is what Dialog carries inside
// structures and a CID is what it publishes outside them, and an assistant may
// have been handed either.
func TestDigestArgumentsAcceptBothForms(t *testing.T) {
	s := server(t)
	paris := content.Atom("Paris").Digest()

	_, byDigest, err := s.Equivalents(context.Background(), nil, equivalentsInput{Digest: paris.String()})
	if err != nil {
		t.Fatalf("by hex digest: %v", err)
	}
	_, byCID, err := s.Equivalents(context.Background(), nil, equivalentsInput{Digest: paris.CID().String()})
	if err != nil {
		t.Fatalf("by CID text form: %v", err)
	}
	if byDigest.Entity.Digest != byCID.Entity.Digest {
		t.Errorf("the two forms named different entities: %s and %s",
			byDigest.Entity.Digest, byCID.Entity.Digest)
	}
	_, spaced, err := s.Equivalents(context.Background(), nil,
		equivalentsInput{Digest: "  " + paris.String() + "\n"})
	if err != nil {
		t.Fatalf("by a digest with surrounding whitespace: %v", err)
	}
	if spaced.Entity.Digest != byDigest.Entity.Digest {
		t.Error("surrounding whitespace changed the answer")
	}
}
