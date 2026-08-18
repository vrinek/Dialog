package accept

import (
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// an edge is one declared relation between two equivalence classes of the view,
// named by their class identities. Where nothing is equivalent to anything —
// the ordinary case — a class is one molecule and an edge is the relation
// between the two molecules a meta-molecule names.
type edge struct{ a, b cid.Digest }

// applySupersession builds the supersession graph of the view and surfaces its
// cycles.
//
// "Declares that molecule A replaces molecule B. Used for corrections and
// versioning. L3 semantics: If both A and B are in L3, implementations SHOULD
// present A and hide or deprecate B" (spec/06-meta-bonds.md, "Supersession").
//
// Both halves of that condition are load-bearing here. An edge is recorded only
// when both molecules are in the view: a supersession naming a molecule
// filtering left out has nothing to hide, and takes effect on a later rebuild
// that finds the molecule present (spec/05-processing-model.md, "Meta-molecule
// application"). And B is marked rather than removed — the view still holds it,
// Lookup still answers for it, and IsSuperseded and Current are how an
// application acts on the mark. Nothing is dropped, because L3 hides what it is
// told to hide and forgets nothing.
//
// The edges run between equivalence classes rather than between the two
// molecules named: equivalent entities are interchangeable, so replacing one
// member of a class replaces the class (spec/06-meta-bonds.md, "Equivalence").
// A class declared to supersede itself — "A supersedes B" where A and B are
// equivalent — is a cycle of one, and is surfaced as such, because no member of
// it can be the current version of anything.
func applySupersession(v *View, c claims) {
	var (
		nodes digestSet
		metas = make(map[edge]*digestSet)
		by    = make(map[edge]*keySet)
	)
	for _, cl := range c.supersessions {
		if !v.Has(cl.a) || !v.Has(cl.b) {
			continue
		}
		e := edge{a: v.class[cl.a], b: v.class[cl.b]}
		nodes.add(e.a)
		nodes.add(e.b)
		if _, ok := metas[e]; !ok {
			metas[e], by[e] = &digestSet{}, &keySet{}
			v.supersedes[e.a] = insertDigest(v.supersedes[e.a], e.b)
			v.supersededBy[e.b] = insertDigest(v.supersededBy[e.b], e.a)
			v.supersessionEdges = append(v.supersessionEdges, e)
		}
		metas[e].add(cl.meta)
		for _, a := range cl.prov {
			if k, ok := keyOf(a.Author); ok {
				by[e].add(k)
			}
		}
	}
	slices.SortFunc(v.supersessionEdges, compareEdges)

	for _, classes := range cycles(nodes.list(), v.supersedes) {
		var (
			meta      digestSet
			declarers keySet
			inCycle   digestSet
		)
		for _, d := range classes {
			inCycle.add(d)
		}
		for _, e := range v.supersessionEdges {
			if !inCycle.has(e.a) || !inCycle.has(e.b) {
				continue
			}
			for _, d := range metas[e].list() {
				meta.add(d)
			}
			for _, k := range by[e].keys {
				declarers.add(k)
			}
		}
		v.conflicts = append(v.conflicts, Conflict{
			Kind:      ConflictSupersessionCycle,
			Molecules: v.membersOf(classes),
			Meta:      meta.list(),
			Declarers: declarers.public(),
		})
	}
}

// applyContradictions records the declared contradictions of the view and
// surfaces every one whose two molecules are both in it.
//
// "Declares that two molecules are contradictory — they cannot both be true.
// L3 semantics: If both molecules are present in L3 (asserted by subscribed
// authors), the implementation MUST surface the contradiction to the
// application layer. Resolution strategy is implementation-scoped"
// (spec/06-meta-bonds.md, "Contradiction").
//
// Presence is read as the filtering rule reads it — an entity is in L3 when a
// subscribed author published it — rather than as requiring a truth assertion
// on top; surfacing is a MUST, and the narrower reading would suppress
// contradictions the application is entitled to see
// (spec/05-processing-model.md, "Meta-molecule application"). Which of the two
// molecules is true, if either, stays with the application: this package
// surfaces and stops.
//
// Like supersession, a contradiction is recorded between equivalence classes:
// what contradicts a molecule contradicts everything interchangeable with it
// (spec/06-meta-bonds.md, "Equivalence"). Two members of one class declared
// contradictory is a class that contradicts itself, which is surfaced as a
// contradiction with a single side.
func applyContradictions(v *View, c claims) {
	var (
		pairs []edge
		metas = make(map[edge]*digestSet)
		by    = make(map[edge]*keySet)
	)
	for _, cl := range c.contradictions {
		if !v.Has(cl.a) || !v.Has(cl.b) {
			continue
		}
		// A contradiction is symmetric — neither molecule is the subject —
		// so the pair is stored in one canonical direction.
		e := edge{a: v.class[cl.a], b: v.class[cl.b]}
		if compareDigests(e.b, e.a) < 0 {
			e.a, e.b = e.b, e.a
		}
		if _, ok := metas[e]; !ok {
			metas[e], by[e] = &digestSet{}, &keySet{}
			pairs = append(pairs, e)
			if e.a != e.b {
				v.contradicts[e.a] = insertDigest(v.contradicts[e.a], e.b)
				v.contradicts[e.b] = insertDigest(v.contradicts[e.b], e.a)
			} else {
				// A class declared to contradict itself, from a molecule
				// contradicting itself or two equivalent molecules
				// contradicting each other. It cannot be true, and the
				// application is told so rather than told nothing.
				v.contradicts[e.a] = insertDigest(v.contradicts[e.a], e.a)
			}
		}
		metas[e].add(cl.meta)
		for _, a := range cl.prov {
			if k, ok := keyOf(a.Author); ok {
				by[e].add(k)
			}
		}
	}
	slices.SortFunc(pairs, compareEdges)
	for _, e := range pairs {
		classes := []cid.Digest{e.a}
		if e.a != e.b {
			classes = append(classes, e.b)
		}
		sides := make([]Side, 0, len(classes))
		for _, class := range classes {
			members := v.classMembers[class]
			sides = append(sides, Side{Molecules: slices.Clone(members), Authors: v.authorsOfAll(members)})
		}
		v.conflicts = append(v.conflicts, Conflict{
			Kind:      ConflictContradiction,
			Molecules: v.membersOf(classes),
			Sides:     sides,
			Meta:      metas[e].list(),
			Declarers: by[e].public(),
		})
	}
}

// cycles returns the sets of equivalence classes that supersede each other in a
// loop, in ascending order of their smallest member. A molecule in one of them
// can never be the current version of anything, itself included, which is a
// disagreement among the authors who wrote the edges and not a state an ordering
// can settle.
//
// It is Tarjan's strongly-connected-components algorithm over the supersession
// graph: a component of more than one molecule is a cycle, and a component of
// one is a cycle only if the molecule supersedes itself. nodes is ascending and
// each adjacency list is ascending, so the components and their members come out
// in a fixed order whatever order the meta-molecules arrived in.
func cycles(nodes []cid.Digest, adjacency map[cid.Digest][]cid.Digest) [][]cid.Digest {
	type state struct {
		index, low int
		onStack    bool
	}
	var (
		found  [][]cid.Digest
		info   = make(map[cid.Digest]*state, len(nodes))
		stack  []cid.Digest
		nextID int
	)
	// The recursion is as deep as the longest supersession chain. Go grows a
	// goroutine's stack on demand, so a long chain costs memory rather than a
	// crash.
	var visit func(d cid.Digest)
	visit = func(d cid.Digest) {
		s := &state{index: nextID, low: nextID, onStack: true}
		nextID++
		info[d] = s
		stack = append(stack, d)
		for _, next := range adjacency[d] {
			ns, seen := info[next]
			switch {
			case !seen:
				visit(next)
				if ns = info[next]; ns.low < s.low {
					s.low = ns.low
				}
			case ns.onStack:
				if ns.index < s.low {
					s.low = ns.index
				}
			}
		}
		if s.low != s.index {
			return
		}
		var component []cid.Digest
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			info[top].onStack = false
			component = insertDigest(component, top)
			if top == d {
				break
			}
		}
		if len(component) > 1 || slices.Contains(adjacency[d], d) {
			found = append(found, component)
		}
	}
	for _, d := range nodes {
		if _, seen := info[d]; !seen {
			visit(d)
		}
	}
	slices.SortFunc(found, func(a, b []cid.Digest) int { return slices.CompareFunc(a, b, compareDigests) })
	return found
}

// compareEdges orders edges by their two endpoints.
func compareEdges(x, y edge) int {
	if c := compareDigests(x.a, y.a); c != 0 {
		return c
	}
	return compareDigests(x.b, y.b)
}
