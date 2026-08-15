package accept

import (
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/graph"
)

// A claim is one meta-molecule of the view, read as the assertion its bond
// makes, together with the subscribed authors who published it and the blocks
// they published it in.
//
// A meta-molecule is an ordinary molecule at L1 and L2 and an ordinary entity
// of the view: it passes the filtering rule like anything else, and it is
// returned by Entries, EntriesOfKind and Lookup. A claim is the *reading* of
// one, which is what L3 adds (spec/06-meta-bonds.md, "Meta-molecules are
// regular molecules").
type claim struct {
	// meta is the meta-molecule's digest.
	meta cid.Digest
	// a and b are the entities its fillers name. A one-filler meta-bond —
	// "_A_ is true", "_A_ is untrue" — leaves b zero.
	a, b cid.Digest
	// typ is the filler type both fillers carry. For the equivalence bond it
	// is the type the pair is declared equivalent at; for every other
	// standard meta-bond it is entity.FillerMolecule.
	typ entity.FillerType
	// stance is Asserted for "_A_ is true" and Retracted for "_A_ is
	// untrue"; Unasserted for the meta-bonds that make no truth claim.
	stance TruthState
	// prov are the authorship records naming a subscribed author, ascending
	// by (author, block). An unsubscribed author's publication of the same
	// meta-molecule is not among them: an entity is in the view because *a*
	// subscribed author published it, and only subscribed authors' assertions
	// have effect.
	prov []graph.Authorship
}

// claims is every meta-molecule of the view, sorted into what it declares.
type claims struct {
	equivalences   []claim
	truths         []claim
	contradictions []claim
	supersessions  []claim
	// malformed are the molecules whose bond is a standard meta-bond but
	// whose fillers do not fit its template. They stay in the view as plain
	// molecules and mean nothing at L3; see View.MalformedMetaMolecules.
	malformed []cid.Digest
}

// read classifies the view's molecules. It walks the view in digest order, so
// the lists it fills are deterministic.
//
// A molecule is a meta-molecule when its bond field is the digest of a standard
// meta-bond: "An implementation recognizes a meta-molecule by checking whether
// the molecule's bond field matches the digest of any known meta-bond template"
// (spec/06-meta-bonds.md, "Meta-molecules are regular molecules").
//
// The fillers are then checked against that template, and a molecule that fails
// the check is recorded as malformed and read no further. Each of the five
// templates binds its fillers to a type — "_A_ is true" takes one molecule,
// "_A_ contradicts _B_" takes two, "_A_ is the same as _B_" takes two of the
// same type — and a molecule that carries anything else is not the assertion
// its bond names. Such a molecule is not impossible: block validation checks the
// number of fillers against the bond's variable count and the shape of each
// filler, and never the filler *types* a particular bond expects, so an author
// can publish "_A_ is the same as _B_" over an atom and a molecule, or "_A_ is
// true" over an atom, and the block is valid. L3 ignores it rather than guessing
// what was meant; it remains a molecule of the view like any other.
func read(v *View) claims {
	var c claims
	for _, d := range v.byKind[kindMolecule] {
		e := v.entries[d]
		m, ok := e.Molecule()
		if !ok { // unreachable: byKind[kindMolecule] holds molecules
			continue
		}
		bond, isMeta := entity.LookupMetaBond(m.Bond())
		if !isMeta {
			continue
		}
		prov := v.subscribedProvenance(e)
		if len(prov) == 0 { // unreachable: an entity is in the view because a subscribed author published it
			continue
		}
		cl, ok := readOne(d, bond.Template(), m.Fillers(), prov)
		if !ok {
			c.malformed = append(c.malformed, d)
			continue
		}
		switch bond.Template() {
		case entity.TemplateEquivalence:
			c.equivalences = append(c.equivalences, cl)
		case entity.TemplateTruthAssertion, entity.TemplateTruthRetraction:
			c.truths = append(c.truths, cl)
		case entity.TemplateContradiction:
			c.contradictions = append(c.contradictions, cl)
		case entity.TemplateSupersession:
			c.supersessions = append(c.supersessions, cl)
		}
	}
	return c
}

// readOne checks one meta-molecule's fillers against its template and returns
// the claim it makes. ok is false for a molecule whose fillers do not fit.
func readOne(d cid.Digest, template string, fillers []entity.Filler, prov []graph.Authorship) (claim, bool) {
	c := claim{meta: d, prov: prov}
	switch template {
	case entity.TemplateEquivalence:
		// "Declares transitive equivalence between two entities of the same
		// type. Both fillers MUST be the same type (both atoms, both bonds,
		// or both molecules)" (spec/06-meta-bonds.md, "Equivalence").
		if len(fillers) != 2 || fillers[0].Type() != fillers[1].Type() || !fillers[0].Type().IsRef() {
			return claim{}, false
		}
		a, aok := fillers[0].Ref()
		b, bok := fillers[1].Ref()
		if !aok || !bok {
			return claim{}, false
		}
		c.a, c.b, c.typ = a, b, fillers[0].Type()
		return c, true
	case entity.TemplateTruthAssertion, entity.TemplateTruthRetraction:
		// "Fillers: A = molecule (type 2)".
		if len(fillers) != 1 || fillers[0].Type() != entity.FillerMolecule {
			return claim{}, false
		}
		a, ok := fillers[0].Ref()
		if !ok {
			return claim{}, false
		}
		c.a, c.typ = a, entity.FillerMolecule
		c.stance = Asserted
		if template == entity.TemplateTruthRetraction {
			c.stance = Retracted
		}
		return c, true
	case entity.TemplateContradiction, entity.TemplateSupersession:
		// "Fillers: A = molecule (type 2), B = molecule (type 2)".
		if len(fillers) != 2 || fillers[0].Type() != entity.FillerMolecule || fillers[1].Type() != entity.FillerMolecule {
			return claim{}, false
		}
		a, aok := fillers[0].Ref()
		b, bok := fillers[1].Ref()
		if !aok || !bok {
			return claim{}, false
		}
		c.a, c.b, c.typ = a, b, entity.FillerMolecule
		return c, true
	default: // unreachable: LookupMetaBond returns one of the five
		return claim{}, false
	}
}

// a unionFind is the equivalence closure of spec/06-meta-bonds.md,
// "Equivalence": "If A is the same as B, and B is the same as C, then A, B, and
// C are all equivalent."
//
// The root of a class is its lexicographically smallest member, which makes the
// class identity a function of the class rather than of the order the pairs
// arrived in — the same requirement every other order in this package answers.
type unionFind struct {
	parent map[cid.Digest]cid.Digest
}

func newUnionFind() *unionFind {
	return &unionFind{parent: make(map[cid.Digest]cid.Digest)}
}

// find returns the root of d's class, adding d as its own class if it is new.
func (u *unionFind) find(d cid.Digest) cid.Digest {
	root, ok := u.parent[d]
	if !ok {
		u.parent[d] = d
		return d
	}
	for root != u.parent[root] {
		root = u.parent[root]
	}
	// Path compression, so that a long equivalence chain costs one walk.
	for cur := d; cur != root; {
		next := u.parent[cur]
		u.parent[cur] = root
		cur = next
	}
	return root
}

// union merges the classes of a and b. The smaller digest becomes the root.
func (u *unionFind) union(a, b cid.Digest) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if compareDigests(rb, ra) < 0 {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
}

// known reports whether d has ever been named in an equivalence.
func (u *unionFind) known(d cid.Digest) bool {
	_, ok := u.parent[d]
	return ok
}
