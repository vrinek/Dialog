// Package accept implements Dialog's Layer 3 — "what we accept" — as specified
// in spec/05-processing-model.md, "Layer 3 — Truth distillation", and
// spec/06-meta-bonds.md.
//
// L3 is the application's source of truth. It takes the accumulated, unfiltered
// and uninterpreted graph of L2 (the graph package) and does three things to it:
//
//	filter      keep the entities a subscribed author published
//	interpret   read the standard meta-bonds' molecules as what they assert
//	surface     report every disagreement, and resolve none of them
//
//	view, err := accept.Build(g, store, accept.NewSubscriptions(alice, bob))
//
// # A view is a snapshot, recomputed rather than maintained
//
// Build is a pure function of its three inputs. A View holds no reference to the
// graph it was built from, nothing in this package writes to L2, and there is no
// incremental update path: when the graph grows, build another view. Correctness
// is the reason — a subjective truth assembled by a sequence of patches is very
// hard to argue about, and rebuilding is cheap — and it makes the determinism
// this package promises easy to keep: two views over the same graph and the same
// subscriptions answer every query identically, whatever order the blocks
// reached L2 in.
//
// # Filtering
//
// "Only data from subscribed authors passes from L2 to L3: for each entity in
// L2, check if any of its authors (from the authorship tags) is in the user's
// subscription list" (spec/05-processing-model.md, "Filtering rules"). That is
// the whole test, and it is applied per entity: an entity with two authorship
// records is in the view when either author is subscribed, and foreign-chain
// data loaded into L2 for validation context stays out unless its author is
// independently subscribed.
//
// The test is uniform. Public and private, own chain and foreign chain, plain
// molecule and meta-molecule are filtered alike; a user is always considered
// subscribed to chains signed by keys they hold, which is what makes their own
// private chains unconditional; and holding a private chain's content key is not
// a subscription (spec/05-processing-model.md, "Private chains"). See
// Subscriptions.
//
// Filtering is per entity and not transitive: a molecule whose author is
// subscribed is in the view even if the bond it names is not, because the bond's
// only author is somebody else. The view reports what it holds and does not
// prune around the gaps: "A view can therefore hold a molecule whose bond only
// an unsubscribed author ever published [...] This is expected and is not an
// error" (spec/05-processing-model.md, "Filtering rules"). L1 validation
// guarantees L2 holds the referenced entity, so an L3 implementation that has to
// render such a molecule reads the missing bond or filler from L2 on the
// application's behalf, which supplies the words and not the truth.
//
// # Meta-molecule application
//
// The five standard meta-bonds of spec/06-meta-bonds.md are recognized by their
// bond digest, and only from subscribed authors' molecules:
//
//	_A_ is the same as _B_   equivalence closure; see EquivalenceClass
//	_A_ is true              truth; see Truth and Assertions
//	_A_ is untrue            truth, and the later assertion by block order wins
//	_A_ contradicts _B_      surfaced when both molecules are in the view
//	_A_ supersedes _B_       B is marked; see IsSuperseded and Current
//
// A meta-molecule applies while it stands. Publication is backing, an explicit
// "«M» is true" from a publishing author is backing too, and that author's later
// "«M» is untrue" withdraws theirs, by the same block order that settles any
// other molecule; an equivalence, contradiction or supersession every one of its
// subscribed authors has retracted declares nothing, while another author's
// retraction withdraws nothing and is surfaced as the disagreement it is
// (spec/06-meta-bonds.md, "Withdrawing meta-molecules"). The truth meta-bonds
// are exempt, which is what stops the regress. See WithdrawnMetaMolecules.
//
// The equivalence closure comes first among the readings because the other four
// are read through it: "Implementations SHOULD treat equivalent entities as interchangeable when
// querying L3" (spec/06-meta-bonds.md, "Equivalence"), and this implementation
// takes that at its word — a truth assertion, a retraction, a contradiction or a
// supersession naming any member of a class is a statement about the class. So a
// class is what carries a truth state, what a supersession marks, and what a
// contradiction is surfaced between. Other strategies are conformant and the
// specification says so; this one is recorded there as the reference reading.
//
// A meta-molecule is itself an ordinary entity of the view: it is filtered like
// any other molecule and Lookup answers for it. What L3 adds is the reading. A
// molecule whose bond is a standard meta-bond but whose fillers do not fit that
// bond's template is not that assertion, and is read as nothing at all — see
// MalformedMetaMolecules.
//
// # Conflicts are surfaced, never resolved
//
// "Implementations MUST surface conflicts [...] to the application layer. The
// protocol does NOT require any specific conflict resolution strategy"
// (spec/05-processing-model.md, "Meta-molecule application"), and
// "Implementations MUST NOT silently discard conflicting assertions"
// (spec/06-meta-bonds.md, "Conflict handling"). So this package detects four
// kinds of disagreement, gives each of them a Conflict value naming the
// molecules and the authors on each side, and stops. Nothing here prefers an
// author, and nothing drops an assertion.
//
// # Determinism
//
// Every slice a View returns is in a fixed order — entities and molecules by
// digest, authors by public key, conflicts by kind and subject — and no map
// iteration order is observable through the API. The determinism guard of
// go/ruleguard/rules.go covers this package for the same reason it covers the
// graph package.
//
// A View is immutable once built and is therefore safe for concurrent use by
// any number of goroutines. Every accessor returns a copy.
package accept

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/graph"
)

// kindMolecule is block.KindMolecule, named once so that the meta-bond code
// reads as being about molecules rather than about a package constant.
const kindMolecule = block.KindMolecule

// A View is one user's Layer 3: the entities of an L2 graph that their
// subscriptions admit, with the standard meta-bonds applied and every
// disagreement surfaced.
//
// The zero value is not usable; call Build. A View never changes after Build
// returns.
type View struct {
	subs *Subscriptions

	entries map[cid.Digest]graph.Entry
	order   []cid.Digest                      // every in-view digest, ascending
	byKind  map[block.EntityKind][]cid.Digest // ascending within each kind

	// class maps an in-view digest to the identity of its equivalence class,
	// and classMembers maps that identity to the class's in-view members,
	// ascending. A digest that no equivalence names is its own class.
	class        map[cid.Digest]cid.Digest
	classMembers map[cid.Digest][]cid.Digest

	// Every reading below is keyed by class identity rather than by digest:
	// equivalent entities are interchangeable, so a meta-molecule naming one
	// member of a class is read as a statement about the class
	// (spec/06-meta-bonds.md, "Equivalence"). A class of one — the common
	// case — makes that the same thing as keying by digest.
	truth      map[cid.Digest]TruthState
	assertions map[cid.Digest][]Assertion

	supersedes        map[cid.Digest][]cid.Digest // class -> the classes it replaces
	supersededBy      map[cid.Digest][]cid.Digest // class -> the classes that replace it
	supersessionEdges []edge                      // between classes
	contradicts       map[cid.Digest][]cid.Digest // class -> the classes declared to contradict it

	malformed []cid.Digest
	withdrawn []cid.Digest
	conflicts []Conflict
}

// ErrNoGraph reports a Build with no graph to filter.
var ErrNoGraph = errors.New("accept: Build needs an L2 graph")

// ErrNoSource reports a Build with no L1 block source. One is always required:
// the graph's authorship tags name the block an assertion was published in, and
// turning that into a position in the author's chain — the block order
// spec/05-processing-model.md, "Assertion order", defines — means reading the
// chain.
var ErrNoSource = errors.New("accept: Build needs the L1 block source the graph was built from")

// Build computes the L3 view of a graph for one subscription set.
//
// g is the L2 graph. src is the L1 store the graph's blocks came from; Build
// reads it to place assertions in their author's block order, and needs every
// block the graph names as the provenance of a subscribed author's
// meta-molecule — the truth assertions, and the equivalences, contradictions
// and supersessions whose standing is read from the same order — together with
// those blocks' ancestors. A block L2 holds is by
// definition one L1 validated and holds, so a block missing from src is an
// inconsistency between the two layers and is reported as an error wrapping
// block.ErrNotFound. subs may be nil or empty, which yields an empty view: an
// entity reaches L3 because an author was subscribed to, and nobody was.
//
// Build reads its inputs and writes to none of them. The returned View is a
// snapshot: later ingestion into g, and later changes to subs, do not touch it.
func Build(g *graph.Graph, src block.Source, subs *Subscriptions) (*View, error) {
	if g == nil {
		return nil, ErrNoGraph
	}
	if src == nil {
		return nil, ErrNoSource
	}
	v := &View{
		subs:         subs.snapshot(),
		entries:      make(map[cid.Digest]graph.Entry),
		byKind:       make(map[block.EntityKind][]cid.Digest),
		class:        make(map[cid.Digest]cid.Digest),
		classMembers: make(map[cid.Digest][]cid.Digest),
		truth:        make(map[cid.Digest]TruthState),
		assertions:   make(map[cid.Digest][]Assertion),
		supersedes:   make(map[cid.Digest][]cid.Digest),
		supersededBy: make(map[cid.Digest][]cid.Digest),
		contradicts:  make(map[cid.Digest][]cid.Digest),
	}

	// 1. Filtering. graph.Entries answers in digest order, so every index
	// this loop fills is ascending without being sorted.
	for _, e := range g.Entries() {
		if len(v.subscribedProvenance(e)) == 0 {
			continue
		}
		d := e.Digest()
		v.entries[d] = e
		v.order = append(v.order, d)
		v.byKind[e.Kind()] = append(v.byKind[e.Kind()], d)
	}

	// 2. Reading the meta-molecules, which are among the entities just
	// filtered: they are ordinary molecules and pass or fail like any other.
	c := read(v)
	v.malformed = c.malformed

	// 3. Which of them still stand. A meta-molecule its own authors have all
	// retracted is not applied, and deciding that comes before applying
	// anything — the equivalence closure of step 4 is one of the things it
	// decides (spec/06-meta-bonds.md, "Withdrawing meta-molecules").
	order := newBlockOrder(src)
	if err := applyStanding(v, &c, order); err != nil {
		return nil, err
	}

	// 4. The equivalence closure, first among the readings, because every
	// other one lands on a class rather than on a bare digest.
	v.closeEquivalences(c)

	// 5. Truth, which is the only reading that needs block order.
	if err := applyTruth(v, c, order); err != nil {
		return nil, err
	}

	// 6. Supersession and contradiction, neither of which needs an order.
	applySupersession(v, c)
	applyContradictions(v, c)

	// 7. The conflicts found while placing blocks in their order: an
	// ambiguous succession is an ambiguous order.
	v.conflicts = append(v.conflicts, order.conflicts...)
	slices.SortFunc(v.conflicts, compareConflicts)
	return v, nil
}

// closeEquivalences unions every well-formed equivalence a subscribed author
// declared and records the resulting classes.
//
// "Declares transitive equivalence between two entities of the same type [...]
// If A is the same as B, and B is the same as C, then A, B, and C are all
// equivalent" (spec/06-meta-bonds.md, "Equivalence"). The closure is therefore
// symmetric and transitive, and it is built only from subscribed authors'
// molecules — which is also the mitigation the specification names for a
// malicious equivalence: "the assertion only affects users who subscribe to that
// author" (spec/06-meta-bonds.md, "Security Considerations").
//
// A pair whose two fillers are of different types is not an equivalence the
// specification defines, and read has already refused it.
//
// The closure is over the declared pairs and over nothing else. No equivalence
// between two molecules is derived from an equivalence between their bonds or
// between the entities filling them: "Two molecules whose bonds are declared
// equivalent, and whose fillers are declared equivalent position by position,
// are therefore two classes and not one" (spec/06-meta-bonds.md,
// "Equivalence"). An author who means two molecules to be one statement
// publishes a molecule-level equivalence.
func (v *View) closeEquivalences(c claims) {
	uf := newUnionFind()
	for _, cl := range c.equivalences {
		uf.union(cl.a, cl.b)
	}
	for _, d := range v.order {
		root := d
		if uf.known(d) {
			root = uf.find(d)
		}
		v.class[d] = root
		v.classMembers[root] = append(v.classMembers[root], d)
	}
}

// subscribedProvenance returns the entry's authorship records whose author is
// subscribed, ascending by (author, block). It is the filtering test of
// spec/05-processing-model.md, "Filtering rules", and the answer to "which of
// these authors' assertions count".
func (v *View) subscribedProvenance(e graph.Entry) []graph.Authorship {
	var out []graph.Authorship
	for _, a := range e.Authors() {
		if v.subs.Contains(a.Author) {
			out = append(out, a)
		}
	}
	return out
}

// authorsOfAll returns the subscribed authors of a set of in-view entities,
// ascending and without repetition — the authors behind one side of a conflict,
// which is a whole equivalence class and not a single molecule.
func (v *View) authorsOfAll(digests []cid.Digest) []ed25519.PublicKey {
	var keys keySet
	for _, d := range digests {
		e, ok := v.entries[d]
		if !ok {
			continue
		}
		for _, a := range v.subscribedProvenance(e) {
			if k, ok := keyOf(a.Author); ok {
				keys.add(k)
			}
		}
	}
	return keys.public()
}

// Subscriptions returns the subscribed authors this view was built for,
// ascending by key.
func (v *View) Subscriptions() []ed25519.PublicKey { return v.subs.Keys() }

// Len returns the number of entities in the view.
func (v *View) Len() int { return len(v.order) }

// Has reports whether the view holds an entity with this digest.
func (v *View) Has(d cid.Digest) bool {
	_, ok := v.entries[d]
	return ok
}

// Lookup returns the L2 entry for an in-view entity — the entity itself and its
// full authorship, unsubscribed authors included, because who published a thing
// is a fact about it and not a matter of taste. ok is false for a digest the
// view does not hold.
func (v *View) Lookup(d cid.Digest) (e graph.Entry, ok bool) {
	e, ok = v.entries[d]
	return e, ok
}

// Digests returns every in-view entity's digest, ascending.
func (v *View) Digests() []cid.Digest { return slices.Clone(v.order) }

// Entries returns every entity of the view, ordered by digest.
func (v *View) Entries() []graph.Entry { return v.entriesOf(v.order) }

// EntriesOfKind returns the view's atoms, bonds or molecules, ordered by
// digest.
func (v *View) EntriesOfKind(k block.EntityKind) []graph.Entry { return v.entriesOf(v.byKind[k]) }

// DigestsOfKind returns the digests of the view's atoms, bonds or molecules,
// ascending.
func (v *View) DigestsOfKind(k block.EntityKind) []cid.Digest { return slices.Clone(v.byKind[k]) }

func (v *View) entriesOf(digests []cid.Digest) []graph.Entry {
	if len(digests) == 0 {
		return nil
	}
	out := make([]graph.Entry, 0, len(digests))
	for _, d := range digests {
		out = append(out, v.entries[d])
	}
	return out
}

// Truth returns what the view holds about a molecule: Unasserted unless a
// subscribed author published a truth meta-molecule about it or about something
// equivalent to it, and Conflicted rather than a winner when they disagree.
//
// A digest the view does not hold is Unasserted. So is an atom or a bond: the
// truth meta-bonds take a molecule filler, and a molecule is the only thing they
// can be said of (spec/06-meta-bonds.md, "Truth assertion").
func (v *View) Truth(d cid.Digest) TruthState {
	if !v.Has(d) {
		return Unasserted
	}
	return v.truth[v.class[d]]
}

// Assertions returns every truth assertion bearing on a molecule — its own and
// those of everything equivalent to it — ordered by author, then block, then
// meta-molecule. The Latest flag marks the assertions that decided the state.
//
// Retracted and out-voted assertions are here too: the protocol forbids
// discarding them ("Implementations MUST NOT silently discard conflicting
// assertions", spec/06-meta-bonds.md, "Conflict handling"), and an application
// resolving a conflict its own way needs the whole record to do it from.
func (v *View) Assertions(d cid.Digest) []Assertion {
	if !v.Has(d) {
		return nil
	}
	src := v.assertions[v.class[d]]
	out := make([]Assertion, len(src))
	for i, a := range src {
		out[i] = a
		out[i].Author = slices.Clone(a.Author)
	}
	slices.SortFunc(out, func(x, y Assertion) int {
		if c := compareKeys(x.Author, y.Author); c != 0 {
			return c
		}
		if c := compareDigests(x.Block, y.Block); c != 0 {
			return c
		}
		return compareDigests(x.Meta, y.Meta)
	})
	return out
}

// EquivalenceClass returns the in-view entities equivalent to d, ascending, d
// among them. A molecule nobody has declared equivalent to anything is a class
// of one; a digest the view does not hold has no class and returns nil.
//
// "Implementations SHOULD treat equivalent entities as interchangeable when
// querying L3. The specific deduplication strategy (merge, prefer one, show
// both) is implementation-scoped" (spec/06-meta-bonds.md, "Equivalence"). This
// implementation shows both: nothing is merged away, every member keeps its own
// digest and its own authorship, and the class is what carries a truth state.
//
// A class holds what subscribed authors declared equivalent, transitively, and
// nothing more. Equivalence does not compose through a molecule's parts
// (spec/06-meta-bonds.md, "Equivalence"), so two molecules built from
// equivalent bonds and equivalent fillers are in two classes until somebody
// declares the molecules themselves equivalent.
func (v *View) EquivalenceClass(d cid.Digest) []cid.Digest {
	if !v.Has(d) {
		return nil
	}
	return slices.Clone(v.classMembers[v.class[d]])
}

// Equivalent reports whether two in-view entities are in the same equivalence
// class. It is reflexive over the entities the view holds.
func (v *View) Equivalent(a, b cid.Digest) bool {
	if !v.Has(a) || !v.Has(b) {
		return false
	}
	return v.class[a] == v.class[b]
}

// Supersedes returns the in-view molecules d replaces, ascending — the direct
// edges only. Follow them with Supersedes again for a chain.
//
// Equivalence widens both ends of the relation: a supersession declared over any
// member of d's class replaces every member of the class it names.
func (v *View) Supersedes(d cid.Digest) []cid.Digest {
	if !v.Has(d) {
		return nil
	}
	return v.membersOf(v.supersedes[v.class[d]])
}

// SupersededBy returns the in-view molecules that replace d, ascending — the
// direct edges only.
func (v *View) SupersededBy(d cid.Digest) []cid.Digest {
	if !v.Has(d) {
		return nil
	}
	return v.membersOf(v.supersededBy[v.class[d]])
}

// IsSuperseded reports whether any in-view molecule replaces d, or anything
// equivalent to d. A superseded molecule stays in the view — "present A and hide
// or deprecate B" (spec/06-meta-bonds.md, "Supersession") — and this is the mark
// an application hides or deprecates it by.
func (v *View) IsSuperseded(d cid.Digest) bool {
	return v.Has(d) && len(v.supersededBy[v.class[d]]) > 0
}

// Current follows the supersession chain forward from d and returns the
// molecules at the end of it: those that replace d, directly or transitively,
// and are themselves replaced by nothing. A molecule nothing supersedes is its
// own current version, so Current returns d itself — with everything equivalent
// to it, the class being what a supersession lands on.
//
// Two molecules can supersede the same one, which is why this returns a slice:
// the protocol does not say two corrections of one statement are a conflict, and
// this package does not invent one. A molecule caught in a supersession cycle
// has no current version at all and returns nil; View.Conflicts says why.
//
// The result is ascending, and empty for a digest the view does not hold.
func (v *View) Current(d cid.Digest) []cid.Digest {
	if !v.Has(d) {
		return nil
	}
	var (
		out     digestSet
		visited = map[cid.Digest]bool{}
		walk    func(class cid.Digest)
	)
	walk = func(class cid.Digest) {
		if visited[class] {
			return
		}
		visited[class] = true
		next := v.supersededBy[class]
		if len(next) == 0 {
			for _, m := range v.classMembers[class] {
				out.add(m)
			}
			return
		}
		for _, n := range next {
			walk(n)
		}
	}
	walk(v.class[d])
	return out.list()
}

// Contradictions returns the in-view molecules a subscribed author declared to
// contradict d, ascending — those declared to contradict anything equivalent to
// d among them. The declaration is symmetric, so each of the two molecules names
// the other.
func (v *View) Contradictions(d cid.Digest) []cid.Digest {
	if !v.Has(d) {
		return nil
	}
	return v.membersOf(v.contradicts[v.class[d]])
}

// membersOf expands class identities into the in-view entities they stand for,
// ascending. It is how every class-keyed reading is answered in the digests a
// caller knows.
func (v *View) membersOf(classes []cid.Digest) []cid.Digest {
	if len(classes) == 0 {
		return nil
	}
	var out digestSet
	for _, c := range classes {
		for _, d := range v.classMembers[c] {
			out.add(d)
		}
	}
	return out.list()
}

// Conflicts returns every disagreement the view found, in a deterministic
// order: by kind, then by the molecules, blocks and meta-molecules involved.
//
// This is the surface the protocol requires — "Implementations MUST surface
// conflicts (e.g., when one subscribed author asserts 'X is true' and another
// asserts 'X is untrue') to the application layer" (spec/05-processing-model.md,
// "Meta-molecule application") — and it is the end of this package's
// involvement. Resolution is the application's.
func (v *View) Conflicts() []Conflict {
	if len(v.conflicts) == 0 {
		return nil
	}
	out := make([]Conflict, len(v.conflicts))
	for i, c := range v.conflicts {
		out[i] = c.clone()
	}
	return out
}

// ConflictsOfKind returns the conflicts of one kind, in the same order.
func (v *View) ConflictsOfKind(k ConflictKind) []Conflict {
	var out []Conflict
	for _, c := range v.conflicts {
		if c.Kind == k {
			out = append(out, c.clone())
		}
	}
	return out
}

// MalformedMetaMolecules returns the in-view molecules whose bond is a standard
// meta-bond but whose fillers do not fit its template, ascending.
//
// They are in the view like any other molecule and mean nothing at L3. Such a
// molecule is publishable: a meta-bond's Fillers line is a recognition criterion
// applied during L2→L3 processing and not a rule of block validity, so block
// validation checks the number of fillers against the bond's variable count and
// the shape of each filler, never the filler types a particular bond expects,
// and "_A_ is true" over an atom is a valid block (spec/06-meta-bonds.md,
// "Meta-molecules are regular molecules"). That section requires the semantics
// not to be applied and asks that such molecules be surfaced rather than
// dropped: L3 declines to guess what was meant, and lists them here.
func (v *View) MalformedMetaMolecules() []cid.Digest { return slices.Clone(v.malformed) }

// WithdrawnMetaMolecules returns the in-view equivalences, contradictions and
// supersessions that are not applied because every subscribed author who
// published one has retracted it, ascending.
//
// "Implementations MUST NOT apply them once every subscribed author who
// published it has withdrawn their backing [...] A meta-molecule that no longer
// applies is not removed" (spec/06-meta-bonds.md, "Withdrawing meta-molecules").
// So these molecules are in the view, Lookup answers for them, their truth state
// records the retraction that withdrew them — and they declare nothing. The
// truth meta-molecules are never here: they are not gated this way, because a
// retraction of a retraction is one author restating a position block order
// already settles.
func (v *View) WithdrawnMetaMolecules() []cid.Digest { return slices.Clone(v.withdrawn) }

// Accepted returns the molecules of the view that survive the meta-bonds:
// in the view, not Retracted, and not superseded by anything. Ascending.
//
// This is a convenience of the reference implementation, not a protocol
// concept. The protocol mandates recognizing the meta-bonds and surfacing
// conflicts; what an application then shows is its own business, and the
// specification only ever says SHOULD about hiding a retracted or superseded
// molecule (spec/06-meta-bonds.md, "Truth retraction" and "Supersession"). The
// choices baked in here are:
//
//   - Unasserted molecules are accepted. Most molecules carry no truth
//     meta-molecule at all, and reading their silence as a retraction would
//     empty the view.
//   - Conflicted molecules are accepted. The protocol refuses to resolve a
//     disagreement, so dropping one side would be resolving it; the conflict is
//     in Conflicts, and the application decides.
//   - Meta-molecules are molecules and are included.
//
// An application that wants a different rule has Truth, IsSuperseded and
// Conflicts to build it from.
func (v *View) Accepted() []cid.Digest {
	var out []cid.Digest
	for _, d := range v.byKind[kindMolecule] {
		if v.Truth(d) == Retracted || v.IsSuperseded(d) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (v *View) String() string {
	return fmt.Sprintf("view(%d entities from %d subscription(s), %d conflict(s))",
		len(v.order), v.subs.Len(), len(v.conflicts))
}
