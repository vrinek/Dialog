package replay_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/vrinek/Dialog/demo/chains"
	"github.com/vrinek/Dialog/demo/internal/chainfile"
	"github.com/vrinek/Dialog/demo/internal/content"
	"github.com/vrinek/Dialog/demo/internal/replay"
	"github.com/vrinek/Dialog/go/accept"
	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// load replays the committed chains — the embedded copy of demo/chains — the
// way every test here starts.
func load(t *testing.T) *replay.Node {
	t.Helper()
	n, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the committed chains: %v", err)
	}
	return n
}

// view builds the L3 view for a set of subscribed authors, by demo name.
func view(t *testing.T, n *replay.Node, authors ...string) *accept.View {
	t.Helper()
	v, err := n.View(authors...)
	if err != nil {
		t.Fatalf("building the view for %v: %v", authors, err)
	}
	return v
}

// The dataset's landmarks, recomputed from the content package rather than
// looked up in the chains: the digest of a statement is a function of the
// statement, so a test knows where to look without the generator telling it.
func disputed(t *testing.T) content.Country {
	t.Helper()
	c, ok := content.CountryByName(content.DisputedCountry)
	if !ok {
		t.Fatalf("the disputed country %q is not in the dataset", content.DisputedCountry)
	}
	return c
}

func atlasCapitalClaim(t *testing.T) cid.Digest {
	t.Helper()
	c := disputed(t)
	return content.CapitalMolecule(c.Capital, c.Name).Digest()
}

func rivalCapitalClaim(t *testing.T) cid.Digest {
	t.Helper()
	return content.CapitalMolecule(content.RivalCapital, disputed(t).Name).Digest()
}

func polandFigures(t *testing.T) (original, first, second cid.Digest) {
	t.Helper()
	poland, ok := content.CountryByName("Poland")
	if !ok {
		t.Fatal("Poland is not in the dataset")
	}
	return content.PopulationMolecule(poland.Name, poland.Population).Digest(),
		content.PopulationMolecule(poland.Name, content.PolandRevisions[0]).Digest(),
		content.PopulationMolecule(poland.Name, content.PolandRevisions[1]).Digest()
}

func flippedClaim(t *testing.T) cid.Digest {
	t.Helper()
	return content.PopulationMolecule(disputed(t).Name, content.FlippedPopulation).Digest()
}

// names renders public keys as the demo's author names, so a failure says
// "gazetteer" rather than 32 bytes.
func names(t *testing.T, n *replay.Node, pubs []ed25519.PublicKey) []string {
	t.Helper()
	out := make([]string, 0, len(pubs))
	for _, p := range pubs {
		name, ok := n.AuthorName(p)
		if !ok {
			name = "unknown:" + string(p[:4])
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// TestReplayValidatesTheCommittedChains is the demo's central claim: the files
// under demo/chains are a world a Dialog node accepts. Load decodes every
// block, validates every chain from its genesis block forward, and ingests
// only what validated.
func TestReplayValidatesTheCommittedChains(t *testing.T) {
	n := load(t)
	want := []struct {
		author string
		blocks int
	}{
		{content.AuthorAtlas, 6},
		{content.AuthorGazetteer, 4},
		{content.AuthorErrata, 4},
	}
	if len(n.Chains) != len(want) {
		t.Fatalf("replayed %d chains, want %d", len(n.Chains), len(want))
	}
	total := 0
	for i, w := range want {
		c := n.Chains[i]
		if c.Author != w.author {
			t.Errorf("chain %d is %s, want %s", i, c.Author, w.author)
			continue
		}
		if len(c.Blocks) != w.blocks {
			t.Errorf("chain %s replayed %d blocks, want %d", c.Author, len(c.Blocks), w.blocks)
		}
		if !bytes.Equal(c.Pub, content.PublicKey(w.author)) {
			t.Errorf("chain %s is signed by an unexpected key", c.Author)
		}
		if len(c.Report.Warnings) != 0 {
			t.Errorf("chain %s validated with warnings: %v", c.Author, c.Report.Warnings)
		}
		if len(c.Report.Forks) != 0 {
			t.Errorf("chain %s forks: %v", c.Author, c.Report.Forks)
		}
		if len(c.Report.Unchecked) != 0 {
			t.Errorf("chain %s left rules %v unchecked", c.Author, c.Report.Unchecked)
		}
		total += len(c.Blocks)
	}
	if n.BlockCount() != total {
		t.Errorf("the store holds %d blocks, the chains hold %d", n.BlockCount(), total)
	}
	if n.Graph.BlockCount() != total {
		t.Errorf("L2 ingested %d blocks, L1 validated %d", n.Graph.BlockCount(), total)
	}
	if index := n.Index; index.Format != chainfile.Format {
		t.Errorf("the index declares format %q, want %q", index.Format, chainfile.Format)
	}
}

// TestGraphHoldsTheDataset checks L2: every entity of the dataset is there,
// exactly once, tagged with the author who published it.
func TestGraphHoldsTheDataset(t *testing.T) {
	n := load(t)
	g := n.Graph

	// The atoms: two units, a country and a capital for each row of the
	// dataset, gazetteer's naming variants, and its rival capital.
	wantAtoms := 2 + 2*len(content.Countries) + len(content.NameVariants) + 1
	// The bonds: the four templates of the dataset and all five standard
	// meta-bonds, each of which some author had to publish as an entity
	// before a molecule could name it.
	wantBonds := 4 + len(entity.StandardMetaBonds())
	// The molecules: atlas's three statements per country and its truth
	// assertion; gazetteer's four atom equivalences, one bond equivalence,
	// two molecules in its own wording, one molecule equivalence, its rival
	// claim and three meta-molecules; errata's two corrected figures with
	// their two supersessions, and the claim it asserts and retracts.
	wantMolecules := 3*len(content.Countries) + 1 +
		len(content.NameVariants) + 1 + 2 + 1 + 1 + 3 +
		len(content.PolandRevisions)*2 + 3

	for _, c := range []struct {
		kind block.EntityKind
		want int
	}{
		{block.KindAtom, wantAtoms},
		{block.KindBond, wantBonds},
		{block.KindMolecule, wantMolecules},
	} {
		if got := len(g.EntriesOfKind(c.kind)); got != c.want {
			t.Errorf("L2 holds %d %ss, want %d", got, c.kind, c.want)
		}
	}
	if got, want := g.Len(), wantAtoms+wantBonds+wantMolecules; got != want {
		t.Errorf("L2 holds %d entities, want %d", got, want)
	}

	// Authorship. Every operation of every block adds one authorship record,
	// so the records outnumber the entities by exactly the number of
	// re-publications — one, gazetteer having published the "_A_ is true"
	// bond atlas had already published.
	records := 0
	for _, a := range []struct {
		author string
		want   int
	}{
		{content.AuthorAtlas, 62},
		{content.AuthorGazetteer, 22},
		{content.AuthorErrata, 8},
	} {
		pub, ok := n.PublicKey(a.author)
		if !ok {
			t.Fatalf("no chain for %s", a.author)
		}
		got := len(g.EntriesByAuthor(pub))
		if got != a.want {
			t.Errorf("%s authored %d entities in L2, want %d", a.author, got, a.want)
		}
		records += got
	}
	if want := g.Len() + 1; records != want {
		t.Errorf("L2 holds %d authorship records over %d entities, want %d", records, g.Len(), want)
	}

	// The re-published entity itself: one entity, two authors.
	truthBond := content.Bond(entity.TemplateTruthAssertion)
	prov := g.Provenance(truthBond.Digest())
	if len(prov) != 2 {
		t.Fatalf("the %q bond has %d authorship records, want 2 (atlas published it, gazetteer re-published it)",
			entity.TemplateTruthAssertion, len(prov))
	}
	authors := make([]ed25519.PublicKey, 0, len(prov))
	for _, p := range prov {
		authors = append(authors, p.Author)
	}
	if got, want := names(t, n, authors), []string{content.AuthorAtlas, content.AuthorGazetteer}; !slices.Equal(got, want) {
		t.Errorf("the %q bond is authored by %v, want %v", entity.TemplateTruthAssertion, got, want)
	}

	// A plain fact is authored by exactly the author who published it.
	for _, c := range []struct {
		digest cid.Digest
		author string
		what   string
	}{
		{content.Atom("France").Digest(), content.AuthorAtlas, "the France atom"},
		{content.CapitalMolecule("Paris", "France").Digest(), content.AuthorAtlas, "Paris is the capital of France"},
		{content.Atom("Holland").Digest(), content.AuthorGazetteer, "the Holland atom"},
		{content.PopulationMolecule("Poland", content.PolandRevisions[1]).Digest(), content.AuthorErrata, "errata's second Poland figure"},
	} {
		prov := g.Provenance(c.digest)
		if len(prov) != 1 {
			t.Errorf("%s has %d authorship records, want 1", c.what, len(prov))
			continue
		}
		if got := names(t, n, []ed25519.PublicKey{prov[0].Author}); !slices.Equal(got, []string{c.author}) {
			t.Errorf("%s is authored by %v, want %s", c.what, got, c.author)
		}
	}
}

// TestTheCapitalDisputeSurfacesTwice is the conflict half of the demo. atlas
// says Miravel is the capital of Valdoria and stands behind it; gazetteer says
// that molecule is untrue, publishes Port Casta instead, and declares the two
// claims contradictory. Those are two different disagreements and L3 surfaces
// both, resolving neither.
func TestTheCapitalDisputeSurfacesTwice(t *testing.T) {
	n := load(t)
	v := view(t, n, content.Authors...)
	atlasClaim, rivalClaim := atlasCapitalClaim(t), rivalCapitalClaim(t)

	if got := v.Truth(atlasClaim); got != accept.Conflicted {
		t.Errorf("atlas's capital claim is %v, want %v", got, accept.Conflicted)
	}
	if got := v.Truth(rivalClaim); got != accept.Asserted {
		t.Errorf("gazetteer's capital claim is %v, want %v", got, accept.Asserted)
	}

	// Neither side is dropped: both assertions are on the record, and both are
	// their author's last word.
	assertions := v.Assertions(atlasClaim)
	if len(assertions) != 2 {
		t.Fatalf("atlas's claim carries %d assertions, want 2", len(assertions))
	}
	stances := map[string]accept.TruthState{}
	for _, a := range assertions {
		name, ok := n.AuthorName(a.Author)
		if !ok {
			t.Fatalf("assertion by an unknown author %x", a.Author[:8])
		}
		if !a.Latest {
			t.Errorf("%s's assertion is not their last word, but they published only one", name)
		}
		stances[name] = a.Stance
	}
	if got := stances[content.AuthorAtlas]; got != accept.Asserted {
		t.Errorf("atlas's stance is %v, want %v", got, accept.Asserted)
	}
	if got := stances[content.AuthorGazetteer]; got != accept.Retracted {
		t.Errorf("gazetteer's stance is %v, want %v", got, accept.Retracted)
	}

	conflicts := v.Conflicts()
	if len(conflicts) != 2 {
		t.Fatalf("the view surfaces %d conflicts, want 2 (a truth disagreement and a contradiction): %v",
			len(conflicts), conflicts)
	}

	disagreements := v.ConflictsOfKind(accept.ConflictTruthDisagreement)
	if len(disagreements) != 1 {
		t.Fatalf("the view surfaces %d truth disagreements, want 1", len(disagreements))
	}
	d := disagreements[0]
	if !slices.Contains(d.Molecules, atlasClaim) {
		t.Errorf("the truth disagreement is over %v, want it to name atlas's claim %s", d.Molecules, atlasClaim)
	}
	if got, want := names(t, n, d.Declarers), []string{content.AuthorAtlas, content.AuthorGazetteer}; !slices.Equal(got, want) {
		t.Errorf("the truth disagreement is between %v, want %v", got, want)
	}
	if len(d.Sides) != 2 {
		t.Fatalf("the truth disagreement has %d sides, want 2", len(d.Sides))
	}
	for _, s := range d.Sides {
		want := content.AuthorAtlas
		if s.Stance == accept.StanceUntrue {
			want = content.AuthorGazetteer
		}
		if got := names(t, n, s.Authors); !slices.Equal(got, []string{want}) {
			t.Errorf("the %q side is held by %v, want %s", s.Stance, got, want)
		}
	}

	contradictions := v.ConflictsOfKind(accept.ConflictContradiction)
	if len(contradictions) != 1 {
		t.Fatalf("the view surfaces %d contradictions, want 1", len(contradictions))
	}
	c := contradictions[0]
	wantMolecules := []cid.Digest{atlasClaim, rivalClaim}
	slices.SortFunc(wantMolecules, func(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) })
	if !slices.Equal(c.Molecules, wantMolecules) {
		t.Errorf("the contradiction is over %v, want the two capital claims %v", c.Molecules, wantMolecules)
	}
	if got, want := names(t, n, c.Declarers), []string{content.AuthorGazetteer}; !slices.Equal(got, want) {
		t.Errorf("the contradiction is declared by %v, want %v", got, want)
	}
	// The declaration is symmetric: each claim names the other.
	if got := v.Contradictions(rivalClaim); !slices.Equal(got, []cid.Digest{atlasClaim}) {
		t.Errorf("Contradictions(rival claim) = %v, want [%s]", got, atlasClaim)
	}
	if got := v.Contradictions(atlasClaim); !slices.Equal(got, []cid.Digest{rivalClaim}) {
		t.Errorf("Contradictions(atlas claim) = %v, want [%s]", got, rivalClaim)
	}

	// Nothing was discarded: both claims are still entities of the view.
	for _, d := range []cid.Digest{atlasClaim, rivalClaim} {
		if !v.Has(d) {
			t.Errorf("molecule %s is not in the view; a conflict must not remove either side", d)
		}
	}
}

// TestEquivalenceClasses covers gazetteer's naming work at all three levels
// the equivalence meta-bond reaches: atoms, bonds and molecules.
func TestEquivalenceClasses(t *testing.T) {
	n := load(t)
	v := view(t, n, content.Authors...)

	for _, nv := range content.NameVariants {
		a, b := content.Atom(nv.Variant).Digest(), content.Atom(nv.Canonical).Digest()
		if !v.Equivalent(a, b) {
			t.Errorf("%q and %q are not equivalent in L3", nv.Variant, nv.Canonical)
		}
		class := v.EquivalenceClass(a)
		if len(class) != 2 {
			t.Errorf("the class of %q has %d members, want 2: %v", nv.Variant, len(class), class)
		}
	}

	// Bond equivalence: gazetteer's wording of atlas's relation.
	ownBond := content.Bond(content.TemplateCapitalCityOf).Digest()
	atlasBond := content.Bond(content.TemplateCapitalOf).Digest()
	if !v.Equivalent(ownBond, atlasBond) {
		t.Errorf("%q and %q are not equivalent in L3", content.TemplateCapitalCityOf, content.TemplateCapitalOf)
	}

	// Molecule equivalence: the same fact stated with a different bond and a
	// different atom, declared equivalent explicitly.
	lisboa := content.CapitalCityMolecule("Lisboa", "Portugal").Digest()
	lisbon := content.CapitalMolecule("Lisbon", "Portugal").Digest()
	if !v.Equivalent(lisboa, lisbon) {
		t.Errorf("gazetteer's Lisboa molecule and atlas's Lisbon molecule are not equivalent in L3")
	}
	if got := v.EquivalenceClass(lisbon); len(got) != 2 {
		t.Errorf("the class of atlas's Lisbon molecule has %d members, want 2: %v", len(got), got)
	}

	// An entity nobody has declared equivalent to anything is a class of one.
	paris := content.Atom("Paris").Digest()
	if got := v.EquivalenceClass(paris); !slices.Equal(got, []cid.Digest{paris}) {
		t.Errorf("the class of the Paris atom is %v, want just itself", got)
	}
}

// TestEquivalenceDoesNotComposeThroughMoleculeStructure is the worked case of
// the rule spec/06-meta-bonds.md, "Equivalence", states: equivalence relates
// the entities a meta-molecule names, and no equivalence between two molecules
// is derived from an equivalence between their bonds or their fillers.
//
// gazetteer publishes two molecules of exactly the same shape: its own bond,
// its own atom for one filler, and atlas's atom for the other. For the Lisboa
// one it also publishes a molecule equivalence, and L3 unifies that pair; for
// the Amsterdam one it does not, relying on the fact that its bond is
// equivalent to atlas's bond and its "Holland" atom is equivalent to atlas's
// "Netherlands" atom — and that pair stays two classes. The difference is the
// cost of the rule and the reason the demo keeps both shapes side by side
// (todos/063).
func TestEquivalenceDoesNotComposeThroughMoleculeStructure(t *testing.T) {
	n := load(t)
	v := view(t, n, content.Authors...)

	gazetteers := content.CapitalCityMolecule("Amsterdam", "Holland").Digest()
	atlases := content.CapitalMolecule("Amsterdam", "Netherlands").Digest()
	if !v.Has(gazetteers) || !v.Has(atlases) {
		t.Fatal("both Amsterdam molecules should be in the view")
	}
	// The parts are equivalent...
	if !v.Equivalent(content.Bond(content.TemplateCapitalCityOf).Digest(), content.Bond(content.TemplateCapitalOf).Digest()) {
		t.Fatal("the two capital bonds should be equivalent")
	}
	if !v.Equivalent(content.Atom("Holland").Digest(), content.Atom("Netherlands").Digest()) {
		t.Fatal("Holland and the Netherlands should be equivalent")
	}
	// ...and the molecules built from them are not.
	if v.Equivalent(gazetteers, atlases) {
		t.Errorf("the two Amsterdam molecules are equivalent; spec/06-meta-bonds.md, \"Equivalence\", says an equivalence between molecules is declared and never derived from their parts")
	}
	if got := v.EquivalenceClass(gazetteers); !slices.Equal(got, []cid.Digest{gazetteers}) {
		t.Errorf("gazetteer's Amsterdam molecule is in a class of %v, want just itself", got)
	}
}

// TestSupersessionChain follows errata's corrections: two revised figures for
// Poland, each superseding the one before it, so that only the newest is
// current and the two older ones are marked.
func TestSupersessionChain(t *testing.T) {
	n := load(t)
	v := view(t, n, content.Authors...)
	original, first, second := polandFigures(t)

	for _, c := range []struct {
		digest cid.Digest
		what   string
	}{
		{original, "atlas's original figure"},
		{first, "errata's first correction"},
	} {
		if !v.IsSuperseded(c.digest) {
			t.Errorf("%s is not marked superseded", c.what)
		}
		if got := v.Current(c.digest); !slices.Equal(got, []cid.Digest{second}) {
			t.Errorf("Current(%s) = %v, want the newest figure [%s]", c.what, got, second)
		}
	}
	if v.IsSuperseded(second) {
		t.Error("errata's second correction is marked superseded, but nothing replaces it")
	}
	if got := v.Current(second); !slices.Equal(got, []cid.Digest{second}) {
		t.Errorf("Current(newest figure) = %v, want itself", got)
	}

	// The chain is a chain: each step names only its immediate predecessor.
	if got := v.SupersededBy(original); !slices.Equal(got, []cid.Digest{first}) {
		t.Errorf("SupersededBy(original) = %v, want [%s]", got, first)
	}
	if got := v.SupersededBy(first); !slices.Equal(got, []cid.Digest{second}) {
		t.Errorf("SupersededBy(first correction) = %v, want [%s]", got, second)
	}

	// Superseded is not deleted: both older figures are still in the view,
	// with their authors, and only the newest is in the accepted set.
	accepted := v.Accepted()
	for _, c := range []struct {
		digest cid.Digest
		want   bool
		what   string
	}{
		{original, false, "atlas's original figure"},
		{first, false, "errata's first correction"},
		{second, true, "errata's second correction"},
	} {
		if !v.Has(c.digest) {
			t.Errorf("%s is not in the view; a superseded molecule is deprecated, not removed", c.what)
		}
		if got := slices.Contains(accepted, c.digest); got != c.want {
			t.Errorf("Accepted() contains %s = %v, want %v", c.what, got, c.want)
		}
	}
}

// TestSameAuthorFlipResolvesToRetracted covers the one case block order
// settles on its own: one author asserting a molecule true and, two blocks
// later, retracting it. The later assertion wins, both are on the record, and
// nothing is in conflict.
func TestSameAuthorFlipResolvesToRetracted(t *testing.T) {
	n := load(t)
	v := view(t, n, content.Authors...)
	flip := flippedClaim(t)

	if got := v.Truth(flip); got != accept.Retracted {
		t.Errorf("errata's flipped claim is %v, want %v", got, accept.Retracted)
	}
	assertions := v.Assertions(flip)
	if len(assertions) != 2 {
		t.Fatalf("the flipped claim carries %d assertions, want 2", len(assertions))
	}
	var latest []accept.Assertion
	for _, a := range assertions {
		if name, _ := n.AuthorName(a.Author); name != content.AuthorErrata {
			t.Errorf("the flipped claim carries an assertion by %s, want only errata", name)
		}
		if a.Latest {
			latest = append(latest, a)
		}
	}
	if len(latest) != 1 {
		t.Fatalf("%d of errata's assertions are its last word, want 1", len(latest))
	}
	if latest[0].Stance != accept.Retracted {
		t.Errorf("errata's last word is %v, want %v", latest[0].Stance, accept.Retracted)
	}

	// One author changing their mind is not a disagreement.
	for _, c := range v.Conflicts() {
		if slices.Contains(c.Molecules, flip) {
			t.Errorf("the flipped claim is in a %v conflict; block order settles a single author's assertions", c.Kind)
		}
	}
	// And a retracted molecule is out of the accepted set but still in the view.
	if !v.Has(flip) {
		t.Error("the flipped claim is not in the view; a retraction flags, it does not delete")
	}
	if slices.Contains(v.Accepted(), flip) {
		t.Error("the flipped claim is in Accepted(), but its author retracted it")
	}
}

// TestSubscriptionsDecideWhatIsTrue is the demo's point about L3: the same L2
// graph yields a different truth for a different subscription set. Dropping
// gazetteer takes away the author who disagreed with atlas and the author who
// declared the naming variants, so the dispute and the equivalence classes both
// disappear.
func TestSubscriptionsDecideWhatIsTrue(t *testing.T) {
	n := load(t)
	all := view(t, n, content.Authors...)
	without := view(t, n, content.AuthorAtlas, content.AuthorErrata)

	if got := len(all.Conflicts()); got != 2 {
		t.Fatalf("with everyone subscribed the view surfaces %d conflicts, want 2", got)
	}
	if got := without.Conflicts(); len(got) != 0 {
		t.Errorf("without gazetteer the view still surfaces %v; both conflicts were gazetteer's", got)
	}

	// atlas's claim is no longer disputed by anyone, so it is simply true.
	atlasClaim := atlasCapitalClaim(t)
	if got := without.Truth(atlasClaim); got != accept.Asserted {
		t.Errorf("without gazetteer, atlas's capital claim is %v, want %v", got, accept.Asserted)
	}
	if got := without.Assertions(atlasClaim); len(got) != 1 {
		t.Errorf("without gazetteer, atlas's claim carries %d assertions, want 1", len(got))
	}
	// The rival claim is not in the view at all: its only author is gone.
	if without.Has(rivalCapitalClaim(t)) {
		t.Error("without gazetteer, its rival capital claim is still in the view")
	}

	// The equivalence classes collapse to singletons: the variants were
	// gazetteer's atoms, and the equivalences were gazetteer's molecules.
	for _, nv := range content.NameVariants {
		variant, canonical := content.Atom(nv.Variant).Digest(), content.Atom(nv.Canonical).Digest()
		if without.Has(variant) {
			t.Errorf("without gazetteer, its %q atom is still in the view", nv.Variant)
		}
		if got := without.EquivalenceClass(canonical); !slices.Equal(got, []cid.Digest{canonical}) {
			t.Errorf("without gazetteer, the class of %q is %v, want just itself", nv.Canonical, got)
		}
	}
	lisbon := content.CapitalMolecule("Lisbon", "Portugal").Digest()
	if got := without.EquivalenceClass(lisbon); !slices.Equal(got, []cid.Digest{lisbon}) {
		t.Errorf("without gazetteer, the class of atlas's Lisbon molecule is %v, want just itself", got)
	}

	// errata's work is untouched: it never depended on gazetteer's opinions,
	// only on gazetteer's *blocks*, for the truth meta-bonds it referenced.
	original, _, second := polandFigures(t)
	if got := without.Current(original); !slices.Equal(got, []cid.Digest{second}) {
		t.Errorf("without gazetteer, the current Poland figure is %v, want [%s]", got, second)
	}
	if got := without.Truth(flippedClaim(t)); got != accept.Retracted {
		t.Errorf("without gazetteer, errata's flipped claim is %v, want %v", got, accept.Retracted)
	}

	// Subscribing to nobody is an empty view rather than an error.
	empty := view(t, n)
	if empty.Len() != 0 {
		t.Errorf("the view for no subscriptions holds %d entities, want 0", empty.Len())
	}
	// And an author the demo does not have is an error rather than silence.
	if _, err := n.View("cartographer"); err == nil {
		t.Error("subscribing to an unknown author should be an error")
	}
}

// TestLoadRejectsTamperedChains checks that nothing on the loading path trusts
// the files: the index guards the bytes, and the bytes are validated even when
// the index is edited to agree with them.
func TestLoadRejectsTamperedChains(t *testing.T) {
	const target = "atlas/002.block"

	t.Run("a corrupted block file", func(t *testing.T) {
		fsys := copyFS(t)
		raw := slices.Clone(fsys[target].Data)
		raw[len(raw)/2] ^= 0xff
		fsys[target] = &fstest.MapFile{Data: raw}
		if _, err := replay.Load(fsys); err == nil {
			t.Fatal("a chain with a corrupted block loaded without error")
		}
	})

	t.Run("a corrupted block file the index agrees with", func(t *testing.T) {
		fsys := copyFS(t)
		raw := slices.Clone(fsys[target].Data)
		raw[len(raw)/2] ^= 0xff
		fsys[target] = &fstest.MapFile{Data: raw}

		var index chainfile.Index
		if err := json.Unmarshal(fsys[chainfile.IndexName].Data, &index); err != nil {
			t.Fatalf("parsing the index: %v", err)
		}
		patched := false
		for i := range index.Chains {
			for j := range index.Chains[i].Blocks {
				if index.Chains[i].Blocks[j].File != target {
					continue
				}
				d := cid.SumDigest(raw)
				index.Chains[i].Blocks[j].Digest = d.String()
				index.Chains[i].Blocks[j].Size = len(raw)
				patched = true
			}
		}
		if !patched {
			t.Fatalf("the index does not list %s", target)
		}
		encoded, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			t.Fatalf("encoding the index: %v", err)
		}
		fsys[chainfile.IndexName] = &fstest.MapFile{Data: append(encoded, '\n')}

		if _, err := replay.Load(fsys); err == nil {
			t.Fatal("a chain whose block does not decode loaded without error")
		}
	})

	t.Run("a block the index does not list", func(t *testing.T) {
		fsys := copyFS(t)
		fsys["atlas/999.block"] = &fstest.MapFile{Data: slices.Clone(fsys[target].Data)}
		if _, err := replay.Load(fsys); err == nil {
			t.Fatal("a chain directory with an unlisted block loaded without error")
		}
	})
}

// copyFS copies the embedded chain directory into a writable in-memory one.
func copyFS(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	err := fs.WalkDir(chains.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(chains.FS, p)
		if err != nil {
			return err
		}
		out[p] = &fstest.MapFile{Data: raw}
		return nil
	})
	if err != nil {
		t.Fatalf("copying the embedded chains: %v", err)
	}
	return out
}
