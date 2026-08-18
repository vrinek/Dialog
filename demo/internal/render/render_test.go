package render_test

import (
	"strings"
	"testing"

	"github.com/vrinek/Dialog/demo/chains"
	"github.com/vrinek/Dialog/demo/internal/content"
	"github.com/vrinek/Dialog/demo/internal/render"
	"github.com/vrinek/Dialog/demo/internal/replay"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/graph"
)

func node(t *testing.T) *replay.Node {
	t.Helper()
	n, err := replay.Load(chains.FS)
	if err != nil {
		t.Fatalf("replaying the committed chains: %v", err)
	}
	return n
}

// TestSentencesOfTheDataset renders one molecule of every shape the demo
// publishes. These are the strings an assistant quotes, so they are pinned
// here in full rather than pattern-matched.
func TestSentencesOfTheDataset(t *testing.T) {
	r := render.New(node(t).Graph)

	poland, ok := content.CountryByName("Poland")
	if !ok {
		t.Fatal("Poland is not in the dataset")
	}
	valdoria, ok := content.CountryByName(content.DisputedCountry)
	if !ok {
		t.Fatalf("%s is not in the dataset", content.DisputedCountry)
	}
	atlasClaim := content.CapitalMolecule(valdoria.Capital, valdoria.Name)

	for _, c := range []struct {
		what   string
		digest cid.Digest
		want   string
	}{
		{"an atom", content.Atom("Paris").Digest(), "Paris"},
		{"a bond", content.Bond(content.TemplateCapitalOf).Digest(), `"_A_ is the capital of _B_"`},
		{
			"a two-atom molecule",
			content.CapitalMolecule("Paris", "France").Digest(),
			"Paris is the capital of France",
		},
		{
			"a molecule with a scalar carrying a unit and a datetime range",
			content.PopulationMolecule(poland.Name, poland.Population).Digest(),
			"Poland had a population of 36600000 people during " +
				content.CensusFrom + " to " + content.CensusTo,
		},
		{
			"a molecule with a decimal fraction",
			content.AreaMolecule(valdoria).Digest(),
			"Valdoria has an area of 1240.5 square kilometres",
		},
		{
			"a molecule with a whole-number scalar",
			content.AreaMolecule(poland).Digest(),
			"Poland has an area of 312696 square kilometres",
		},
		{
			"a truth assertion over a molecule",
			content.TruthAssertion(atlasClaim.Digest()).Digest(),
			"«Miravel is the capital of Valdoria» is true",
		},
		{
			"a truth retraction over a molecule",
			content.TruthRetraction(atlasClaim.Digest()).Digest(),
			"«Miravel is the capital of Valdoria» is untrue",
		},
		{
			"an equivalence between atoms",
			content.AtomEquivalence("Holland", "Netherlands").Digest(),
			"Holland is the same as Netherlands",
		},
		{
			"an equivalence between bonds",
			content.BondEquivalence(content.TemplateCapitalCityOf, content.TemplateCapitalOf).Digest(),
			`"_A_ is the capital city of _B_" is the same as "_A_ is the capital of _B_"`,
		},
		{
			"a contradiction between molecules",
			content.Contradiction(
				content.CapitalMolecule(content.RivalCapital, valdoria.Name).Digest(),
				atlasClaim.Digest()).Digest(),
			"«Port Casta is the capital of Valdoria» contradicts «Miravel is the capital of Valdoria»",
		},
		{
			"a supersession between molecules",
			content.Supersession(
				content.PopulationMolecule(poland.Name, content.PolandRevisions[0]).Digest(),
				content.PopulationMolecule(poland.Name, poland.Population).Digest()).Digest(),
			"«Poland had a population of 36620000 people during " +
				content.CensusFrom + " to " + content.CensusTo + "» supersedes " +
				"«Poland had a population of 36600000 people during " +
				content.CensusFrom + " to " + content.CensusTo + "»",
		},
	} {
		if got := r.Text(c.digest); got != c.want {
			t.Errorf("%s renders as\n  %q\nwant\n  %q", c.what, got, c.want)
		}
	}
}

// TestEveryEntityOfTheGraphRenders is the property the MCP server's search
// depends on: nothing in the dataset renders as a placeholder, because the
// graph holds every entity every molecule names.
func TestEveryEntityOfTheGraphRenders(t *testing.T) {
	g := node(t).Graph
	r := render.New(g)
	for _, e := range g.Entries() {
		text := r.Text(e.Digest())
		if text == "" {
			t.Errorf("%s renders as the empty string", e.Digest())
		}
		if strings.Contains(text, "unpublished") || strings.Contains(text, "unrecognized") {
			t.Errorf("%s renders with a placeholder: %q", e.Digest(), text)
		}
	}
}

// TestMissingEntitiesRenderAsPlaceholders covers the other half: a source that
// does not hold what a molecule names still produces a sentence, with a hole in
// it where the words would have been. This is the case
// spec/05-processing-model.md, "Filtering rules", describes for an L3 view
// whose molecule names a bond only an unsubscribed author published — the
// renderer is given the graph for exactly that reason, and this is what it
// would say if it were not.
func TestMissingEntitiesRenderAsPlaceholders(t *testing.T) {
	r := render.New(emptySource{})
	m := content.CapitalMolecule("Paris", "France")

	text := r.Sentence(m)
	if !strings.Contains(text, render.Short(m.Bond())) {
		t.Errorf("a molecule with an unheld bond renders as %q, want it to name the bond %s",
			text, render.Short(m.Bond()))
	}
	for _, f := range m.Fillers() {
		d, ok := f.Ref()
		if !ok {
			continue
		}
		if !strings.Contains(text, render.Short(d)) {
			t.Errorf("a molecule with unheld fillers renders as %q, want it to name %s",
				text, render.Short(d))
		}
	}

	// The bond alone is enough for the sentence; the fillers are then the holes.
	r = render.New(onlyEntity(t, content.Bond(content.TemplateCapitalOf).Digest()))
	text = r.Sentence(m)
	if !strings.HasSuffix(text, " is the capital of "+placeholderFor(t, m, 1)) {
		t.Errorf("with only the bond held, the molecule renders as %q", text)
	}
}

// placeholderFor is what an unheld atom filler renders as.
func placeholderFor(t *testing.T, m entity.Molecule, i int) string {
	t.Helper()
	d, ok := m.Fillers()[i].Ref()
	if !ok {
		t.Fatalf("filler %d of the molecule is not a reference", i)
	}
	return "[an unpublished entity, " + render.Short(d) + "]"
}

// TestSplitTemplateMatchesTheParser pins the renderer's template scan to the
// library's: the number of pieces the substitution splits a template into must
// always be one more than the number of variables the entity package finds in
// it, including for the disambiguation cases of spec/01-data-model.md, "Bonds".
// If the two ever disagree, a molecule renders its fillers into the wrong
// slots, which is a lie rather than a formatting bug.
func TestSplitTemplateMatchesTheParser(t *testing.T) {
	for _, template := range []string{
		"_A_ is the capital of _B_",
		"_A_ had a population of _B_ during _C_",
		"_A_ is the same as _B_",
		"_A_ is true",
		"_AB_ relates to _CD_",
		"_A_B_ is odd",
		"_A__B_ is adjacent",
		"type_of _A_",
		"_a_ holds no variable",
		"no variables at all",
		"_A_ and _A_ again",
		"trailing _A_",
		"_A_ leading",
	} {
		bond, err := entity.NewBond(template)
		if err != nil {
			// Templates the library refuses are not renderable input; the scan
			// is still checked against the parser below.
			t.Logf("template %q is not a valid bond: %v", template, err)
		} else if bond.VariableCount() != len(entity.ParseTemplateVariables(template)) {
			t.Fatalf("the library disagrees with itself about %q", template)
		}
		want := len(entity.ParseTemplateVariables(template)) + 1
		if got := render.SplitTemplateForTest(template); len(got) != want {
			t.Errorf("splitting %q gives %d pieces %q, want %d", template, len(got), got, want)
		}
	}
}

// TestDecimalRendering covers the scalar shapes the dataset does not reach.
func TestDecimalRendering(t *testing.T) {
	for _, c := range []struct {
		exponent, mantissa int64
		want               string
	}{
		{-1, 12405, "1240.5"},
		{-3, 5, "0.005"},
		{-2, -125, "-1.25"},
		{-1, -5, "-0.5"},
		{0, 42, "42"},
		{2, 42, "4200"},
		{-64, 1, "1e-64"},
	} {
		if got := render.DecimalForTest(c.exponent, c.mantissa); got != c.want {
			t.Errorf("%d × 10^%d renders as %q, want %q", c.mantissa, c.exponent, got, c.want)
		}
	}
}

// emptySource holds nothing.
type emptySource struct{}

func (emptySource) Lookup(cid.Digest) (graph.Entry, bool) { return graph.Entry{}, false }

// oneEntity holds a single entity of the demo's graph and nothing else, so
// that part of a molecule resolves and the rest does not.
type oneEntity struct{ entry graph.Entry }

func (o oneEntity) Lookup(d cid.Digest) (graph.Entry, bool) {
	if d != o.entry.Digest() {
		return graph.Entry{}, false
	}
	return o.entry, true
}

func onlyEntity(t *testing.T, d cid.Digest) oneEntity {
	t.Helper()
	e, ok := node(t).Graph.Lookup(d)
	if !ok {
		t.Fatalf("the demo's graph does not hold %s", d)
	}
	return oneEntity{entry: e}
}
