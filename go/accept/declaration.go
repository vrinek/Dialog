package accept

import (
	"crypto/ed25519"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// A Backing is one subscribed author standing behind a meta-molecule: the
// authorship record that put it in the view, placed in that author's chain.
//
// "Publication is backing" (spec/06-meta-bonds.md, "Withdrawing
// meta-molecules"), so a Backing is an authorship record and not a separate
// statement. What decides whether it is still here is the author's later word:
// an author who published a meta-molecule and then asserted "«M» is untrue" has
// withdrawn their backing and has no Backing among a Declaration's, while an
// author who published it, retracted it and published or asserted it again
// backs it once more — the later-wins reading of block order that settles any
// other molecule (spec/05-processing-model.md, "Assertion order").
type Backing struct {
	// Author is the subscribed author who published the meta-molecule.
	Author ed25519.PublicKey
	// Block is the block they published it in — the provenance tag of
	// spec/05-processing-model.md, "Accumulation rules", step 3.
	Block cid.Digest
	// Position places that block in the author's chain. It is the position
	// this package decided the standing by.
	Position ChainPosition
}

func (b Backing) String() string {
	return fmt.Sprintf("%x in block %s (%s)", b.Author[:8], b.Block, b.Position)
}

func (b Backing) clone() Backing {
	b.Author = slices.Clone(b.Author)
	b.Position = b.Position.clone()
	return b
}

// A Declaration is one applied meta-molecule reported as the record it is:
// which molecule declared the reading, what it names, and which subscribed
// authors still stand behind it.
//
// It answers the question the readings themselves cannot — "who says these two
// are the same thing", "who says this figure replaces that one", "who says
// these two cannot both be true". L3 applies a meta-bond only because a
// subscribed author published a molecule saying so, and an application that
// reports the reading without the author has laundered an opinion into a fact.
// The authorship tags L2 keeps exist for exactly this
// (spec/05-processing-model.md, "Accumulation rules"), and the truth meta-bonds
// have always reported theirs through Assertion; these are the other three.
//
// Only standing declarations are ever returned. A meta-molecule every
// subscribed author who published it has retracted declares nothing
// ("Implementations MUST NOT apply them once every subscribed author who
// published it has withdrawn their backing", spec/06-meta-bonds.md,
// "Withdrawing meta-molecules"), so it appears in no Declaration at all — it is
// in WithdrawnMetaMolecules instead — and a single author's withdrawal removes
// that author's Backing while the declaration stands on whoever is left.
type Declaration struct {
	// Meta is the digest of the meta-molecule that declared it. It is an
	// entity of the view like any other: Lookup answers for it, and its own
	// authorship, truth state and equivalence class are readable.
	Meta cid.Digest
	// Template is the standard meta-bond it is built on — one of
	// entity.TemplateEquivalence, entity.TemplateSupersession and
	// entity.TemplateContradiction.
	Template string
	// A and B are the entities the meta-molecule's fillers name, in the order
	// the template reads them: "_A_ supersedes _B_" declares that A replaces
	// B, and "_A_ is the same as _B_" and "_A_ contradicts _B_" are symmetric.
	//
	// They are the digests the author wrote, not class identities. The reading
	// applies across their whole equivalence classes, which is why a
	// declaration answers for members it never names.
	A, B cid.Digest
	// Backing are the subscribed authors who still stand behind it, with the
	// blocks they published it in, ascending by author and then by block.
	// There is always at least one.
	Backing []Backing
}

// Authors returns the subscribed authors still backing the declaration,
// ascending by key and without repetition — the answer to "who says so".
func (d Declaration) Authors() []ed25519.PublicKey {
	var keys keySet
	for _, b := range d.Backing {
		if k, ok := keyOf(b.Author); ok {
			keys.add(k)
		}
	}
	return keys.public()
}

func (d Declaration) String() string {
	return fmt.Sprintf("%s %s: %s, %s (backed by %d author(s))",
		d.Meta, d.Template, d.A, d.B, len(d.Authors()))
}

func (d Declaration) clone() Declaration {
	out := d
	if d.Backing != nil {
		out.Backing = make([]Backing, len(d.Backing))
		for i, b := range d.Backing {
			out.Backing[i] = b.clone()
		}
	}
	return out
}

// compareDeclarations is the order every declaration list answers in: by the
// declaring meta-molecule, then by what it names. Two declarations cannot agree
// on all three without being the same molecule.
func compareDeclarations(x, y Declaration) int {
	if c := compareDigests(x.Meta, y.Meta); c != 0 {
		return c
	}
	if c := compareDigests(x.A, y.A); c != 0 {
		return c
	}
	return compareDigests(x.B, y.B)
}

// EquivalenceDeclarations returns the equivalences that bear on d's class: every
// standing "_A_ is the same as _B_" a subscribed author published naming a
// member of it, with the authors behind each. Ascending by meta-molecule.
//
// This is the provenance of EquivalenceClass. A class is the transitive closure
// of these declarations, so a class of n members is held together by at least
// n-1 of them, and every one is reported: which of them "caused" a particular
// merge is an artifact of the order the pairs were unioned in and not a fact
// about the data, so this package does not pretend to say. A declaration naming
// two members some other declaration had already unified is reported too, and
// is exactly as much of an opinion as the others.
//
// A class of one has no declarations. A digest the view does not hold has none
// either.
func (v *View) EquivalenceDeclarations(d cid.Digest) []Declaration {
	return v.declarationsOf(v.equivalenceDecls, d)
}

// SupersessionDeclarations returns the supersessions touching d's class in
// either direction: every standing "_A_ supersedes _B_" whose A or whose B is a
// member of it. Ascending by meta-molecule.
//
// This is the provenance of Supersedes, SupersededBy, IsSuperseded and Current.
// Read A and B to see which way an edge runs — d's class may be either end, and
// is both when a class supersedes itself.
func (v *View) SupersessionDeclarations(d cid.Digest) []Declaration {
	return v.declarationsOf(v.supersessionDecls, d)
}

// ContradictionDeclarations returns the contradictions touching d's class:
// every standing "_A_ contradicts _B_" a subscribed author published naming a
// member of it, with the authors behind each. Ascending by meta-molecule.
//
// This is the provenance of Contradictions, and the per-entity form of what
// Conflict.Meta and Conflict.Declarers report per surfaced conflict. The two
// agree by construction: a declaration is surfaced as a ConflictContradiction
// when both of the molecules it names are in the view, and reported here from
// either end.
func (v *View) ContradictionDeclarations(d cid.Digest) []Declaration {
	return v.declarationsOf(v.contradictionDecls, d)
}

// declarationsOf answers one of the three accessors, deep-copying so that a
// caller cannot reach into the view through the slices it hands out.
func (v *View) declarationsOf(index map[cid.Digest][]Declaration, d cid.Digest) []Declaration {
	if !v.Has(d) {
		return nil
	}
	src := index[v.class[d]]
	if len(src) == 0 {
		return nil
	}
	out := make([]Declaration, len(src))
	for i, decl := range src {
		out[i] = decl.clone()
	}
	return out
}

// declare files one claim's declaration against the class of each end it names
// that the view holds, once per class. It is called with the claims that
// survived applyStanding, so a withdrawn meta-molecule is never filed.
func (v *View) declare(index map[cid.Digest][]Declaration, cl claim, ends ...cid.Digest) {
	decl := Declaration{
		Meta:     cl.meta,
		Template: cl.template,
		A:        cl.a,
		B:        cl.b,
		Backing:  cl.backing,
	}
	var filed digestSet
	for _, end := range ends {
		if !v.Has(end) {
			continue
		}
		class := v.class[end]
		if filed.has(class) {
			continue
		}
		filed.add(class)
		i, _ := slices.BinarySearchFunc(index[class], decl, compareDeclarations)
		index[class] = slices.Insert(index[class], i, decl)
	}
}
