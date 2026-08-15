// Package graph implements Dialog's Layer 2 — "what we know" — as specified in
// spec/05-processing-model.md, "Layer 2 — Ontology graph accumulation".
//
// A Graph is the single, unified ontology graph a node builds by extracting the
// operations of every valid block it holds. Each entity is stored once, keyed by
// its digest, and tagged with one authorship record per (author, block) that
// published it:
//
//	Ingest(block)          -> every operation's entity, tagged with the block's
//	                          pub key and the block's digest (provenance)
//	IngestPayload(block, p) -> the same, for a private block whose payload the
//	                          caller has decrypted
//
// # The caller validates
//
// Ingest and IngestPayload take a block the caller has ALREADY VALIDATED. This
// package runs no validation of its own: it does not check signatures, chain
// linkage, reference reachability or filler counts, all of which are
// block.Validate's and block.ValidatePayload's business, and all of which need
// blocks this package does not hold.
//
// The contract is therefore normative and it is the caller's to keep: "A stored
// but unvalidated block MUST NOT be made available for L2 processing: its
// operations contribute nothing to the ontology graph until the block is
// validated" (spec/05-processing-model.md, "Block reception"). Accumulation is
// defined over "each valid block in L1 — that is, each block whose validation
// succeeded, never one that is stored but unvalidated". Ingesting a block whose
// validation has not succeeded — or has not been attempted, or failed — puts
// data in L2 that the protocol says is not there, and no later query can tell
// the difference. The reference wiring is:
//
//	report, err := block.Validate(b, store, opts)   // or privacy.OpenAndValidate
//	if err != nil {
//	        return err                              // invalid: reject, do not ingest
//	}
//	if err := g.Ingest(b); err != nil {
//	        return err
//	}
//
// What this package does reject is input it cannot use at all: a nil block, a
// private block handed to Ingest without its decrypted payload, and a payload
// that is not a well-formed one. Those are programming errors, not validation.
//
// # Append-only
//
// "L2 is append-only. Entities MUST NOT be removed or modified once added"
// (spec/05-processing-model.md, "Accumulation rules"). There is accordingly no
// way to remove or change anything through this API: the only mutators are the
// two Ingest methods, every accessor returns a copy or an immutable value, and
// the entity types themselves (entity.Atom, entity.Bond, entity.Molecule) have
// no mutating methods.
//
// Re-publication accumulates authorship rather than entities: "If an entity with
// the same CID already exists in L2 (because the same content was published by a
// different author, or re-published by the same author), the new authorship
// record is added alongside the existing one. The entity itself is not
// duplicated." Ingesting the same block twice is a no-op — the (author, block)
// pair is already recorded — so a node that re-processes its store rebuilds the
// same graph.
//
// # No interpretation
//
// "L2 performs no interpretation of data. Meta-molecules (e.g., 'X is true',
// 'A is the same as B') are stored as regular molecules in L2. Their special
// semantics are only applied during L2→L3 processing"
// (spec/05-processing-model.md, "No interpretation"). This package therefore
// never looks at a molecule's bond digest: a meta-molecule is a molecule, with
// the same authorship records and the same queries as any other. Equivalence
// closure, truth assertion and retraction, contradiction and supersession are
// L3 semantics and belong to the accept package (spec/06-meta-bonds.md).
//
// The same holds for filtering. L2 accumulates everything the caller ingests,
// subscribed authors and foreign chains alike: "Foreign chain data is present in
// L2 for validation context but is not automatically promoted to L3", and "L2 is
// unaffected — it accumulates all data pulled at L1 without filtering".
//
// # Determinism and concurrency
//
// Every slice this package returns is sorted — entities by digest, authorship
// records by (author, block) — so two graphs holding the same data answer every
// query identically, whatever order the blocks arrived in. No map iteration
// order is ever observable through the API.
//
// A Graph is safe for concurrent use by multiple goroutines, like
// block.MemStore: an RWMutex guards it, ingestion takes the write lock and every
// query takes the read lock. The values it hands out are copies, so a caller may
// hold on to them past the call.
package graph

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// An Entity is one of Dialog's three content-addressed primitives, as stored in
// the graph. Exactly three types satisfy it — entity.Atom, entity.Bond and
// entity.Molecule (spec/01-data-model.md) — and all three are immutable values,
// so an Entity handed out by a query cannot be used to change what the graph
// holds.
//
// Entry.Kind says which of the three an Entity is without a type switch;
// Entry.Atom, Entry.Bond and Entry.Molecule recover the concrete value.
type Entity interface {
	// Digest returns SHA-256(dCBOR(entity)), the entity's identity and the key
	// it is stored under.
	Digest() cid.Digest
	// CID returns the entity's external 36-byte content identifier.
	CID() cid.CID
	// Bytes returns the entity's canonical dCBOR encoding.
	Bytes() []byte
	// Value returns the entity as a dCBOR value.
	Value() dcbor.Value
	// String renders the entity for logs and test failures.
	String() string
}

// An Authorship record is the tag spec/05-processing-model.md, "Accumulation
// rules", requires every entity added to L2 to carry: the author's public key
// (from the block's pub field) and the block's identity (provenance).
//
// One entity may carry many: the same content published by a different author,
// or re-published by the same author in a later block, adds a record rather than
// a second entity.
//
// Those two tags are "the minimum every entity MUST carry, not a closed list";
// an implementation MAY record further provenance beside them. This one records
// nothing more — in particular no timestamp, an untrusted, author-chosen field
// that a private block keeps inside its ciphertext. What the one L3 rule that
// needs more than the two tags asks for is block order, and the provenance tag
// is what yields it: the block it names has a position in its author's chain
// (spec/05-processing-model.md, "Accumulation rules" and "Assertion order").
type Authorship struct {
	// Author is the raw 32-byte Ed25519 public key of the block's pub field.
	Author ed25519.PublicKey
	// Block is the digest of the block the entity was extracted from. The
	// specification calls this tag "the block's CID"; a block's CID is its
	// digest behind the four-byte prefix, and inside structures Dialog carries
	// the digest (spec/03-encoding.md, "Internal references"). Use Block.CID()
	// for the external form.
	Block cid.Digest
}

// CID returns the provenance block's external content identifier.
func (a Authorship) CID() cid.CID { return a.Block.CID() }

func (a Authorship) String() string {
	return fmt.Sprintf("%x in block %s", authorPrefix(a.Author), a.Block)
}

// authorPrefix is the leading bytes of a key, for error messages and String.
func authorPrefix(pub ed25519.PublicKey) []byte {
	if len(pub) > 8 {
		return pub[:8]
	}
	return pub
}

// An authorKey is a public key in comparable form, so that it can key a map and
// be compared without allocating.
type authorKey [ed25519.PublicKeySize]byte

func (k authorKey) public() ed25519.PublicKey { return slices.Clone(k[:]) }

// a record is an Authorship in comparable form.
type record struct {
	author authorKey
	block  cid.Digest
}

// An Entry is one entity of the graph together with everything the graph knows
// about it: its kind, its value and its authorship records. It is a snapshot
// taken under the read lock, and holds nothing the graph can change afterwards.
type Entry struct {
	kind    block.EntityKind
	value   Entity
	authors []Authorship
}

// Digest returns the entity's digest, the key it is stored under.
func (e Entry) Digest() cid.Digest { return e.value.Digest() }

// CID returns the entity's external content identifier.
func (e Entry) CID() cid.CID { return e.value.CID() }

// Kind reports whether the entry holds an atom, a bond or a molecule.
func (e Entry) Kind() block.EntityKind { return e.kind }

// Entity returns the stored entity.
func (e Entry) Entity() Entity { return e.value }

// Atom returns the entry's atom. ok is false unless Kind is block.KindAtom.
func (e Entry) Atom() (a entity.Atom, ok bool) {
	a, ok = e.value.(entity.Atom)
	return a, ok
}

// Bond returns the entry's bond. ok is false unless Kind is block.KindBond.
func (e Entry) Bond() (b entity.Bond, ok bool) {
	b, ok = e.value.(entity.Bond)
	return b, ok
}

// Molecule returns the entry's molecule. ok is false unless Kind is
// block.KindMolecule.
//
// A meta-molecule is a molecule and is returned here like any other: L2 stores
// it without interpretation (spec/05-processing-model.md, "No interpretation").
// entity.LookupMetaBond on the molecule's bond digest is how L3 recognizes one.
func (e Entry) Molecule() (m entity.Molecule, ok bool) {
	m, ok = e.value.(entity.Molecule)
	return m, ok
}

// Authors returns a copy of the entry's authorship records, ordered by author
// key and then by block digest.
func (e Entry) Authors() []Authorship { return cloneAuthorships(e.authors) }

// AuthoredBy reports whether pub is among the entry's authors — the test
// spec/05-processing-model.md, "Filtering rules", asks of every entity when L3
// is built.
func (e Entry) AuthoredBy(pub ed25519.PublicKey) bool {
	return slices.ContainsFunc(e.authors, func(a Authorship) bool {
		return bytes.Equal(a.Author, pub)
	})
}

func (e Entry) String() string {
	return fmt.Sprintf("%s %s (%d author record(s))", e.kind, e.Digest(), len(e.authors))
}

// cloneAuthorships deep-copies a record slice, keys included.
func cloneAuthorships(src []Authorship) []Authorship {
	out := make([]Authorship, len(src))
	for i, a := range src {
		out[i] = Authorship{Author: slices.Clone(a.Author), Block: a.Block}
	}
	return out
}

// a node is the graph's internal record of one entity: the entity itself, which
// never changes, and the authorship records, which only ever grow.
type node struct {
	kind    block.EntityKind
	value   Entity
	authors []record // ascending by (author, block)
}

// entry renders the node as an Entry, copying everything a caller could hold on
// to. The caller holds the lock.
func (n *node) entry() Entry {
	authors := make([]Authorship, len(n.authors))
	for i, r := range n.authors {
		authors[i] = Authorship{Author: r.author.public(), Block: r.block}
	}
	return Entry{kind: n.kind, value: n.value, authors: authors}
}

// A Graph is Dialog's Layer 2: the accumulated, author-tagged ontology graph.
// The zero value is not usable; call New.
//
// It is append-only, safe for concurrent use, and answers every query in a
// deterministic order. See the package documentation for the contract Ingest
// expects of its caller.
type Graph struct {
	mu       sync.RWMutex
	entities map[cid.Digest]*node
	order    []cid.Digest                      // every entity digest, ascending
	byKind   map[block.EntityKind][]cid.Digest // ascending within each kind
	byAuthor map[authorKey][]cid.Digest        // ascending within each author
	authors  []authorKey                       // ascending
	// blocks maps an ingested block's digest to the digest of its decrypted
	// payload's canonical encoding, or the zero digest for a block whose
	// operations were in the clear. It is what makes re-ingestion a no-op, and
	// what catches a caller handing the same private block two different
	// payloads.
	blocks     map[cid.Digest]cid.Digest
	blockOrder []cid.Digest // ascending
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		entities: make(map[cid.Digest]*node),
		byKind:   make(map[block.EntityKind][]cid.Digest),
		byAuthor: make(map[authorKey][]cid.Digest),
		blocks:   make(map[cid.Digest]cid.Digest),
	}
}

// ErrNilBlock reports a nil block handed to Ingest or IngestPayload.
var ErrNilBlock = errors.New("graph: nil block")

// ErrPayloadRequired reports a private block handed to Ingest. Its operations
// are inside its ciphertext, so there is nothing to extract: decrypt it — with
// privacy.Open, or privacy.OpenAndValidate, which validates it at the same time
// — and call IngestPayload (spec/05-processing-model.md, "Private chains",
// step 2).
var ErrPayloadRequired = errors.New("graph: a private block can only be ingested with its decrypted payload")

// ErrPayloadMismatch reports the same private block ingested twice with two
// different payloads. A block's digest covers its ciphertext, and a ciphertext
// has exactly one plaintext, so the two cannot both be that block's payload:
// one of them did not come from decrypting this block.
var ErrPayloadMismatch = errors.New("graph: this block has already been ingested with a different payload")

// Ingest adds the entities created by a public or rotation block's operations,
// each tagged with the block's author and the block's digest
// (spec/05-processing-model.md, "Accumulation rules").
//
// THE BLOCK MUST ALREADY HAVE BEEN VALIDATED. See the package documentation:
// this method validates nothing, and a block that is stored but unvalidated MUST
// NOT reach it.
//
// Ingesting the same block twice is a no-op: "A node MAY record which blocks it
// has already processed, so that re-processing its store is idempotent"
// (spec/05-processing-model.md, "Accumulation rules"). The record is bookkeeping
// and changes nothing about what the graph contains.
//
// A rotation block carries a single rotate_key operation, which creates no
// entity (spec/02-block-format.md, "rotate_key"), so ingesting one records the
// block and adds nothing to the graph: "a rotation block contributes nothing to
// the ontology graph. A node's whole response to a rotation block is the L1
// procedure of 'Chain succession (key rotation)'" (spec/05-processing-model.md,
// "Accumulation rules"). Marking the old key inactive and following the
// succession are that L1 procedure, not L2's business.
//
// A private block is refused with ErrPayloadRequired; use IngestPayload.
func (g *Graph) Ingest(b *block.Block) error {
	if b == nil {
		return fmt.Errorf("%w: Ingest needs a block to extract operations from", ErrNilBlock)
	}
	if b.Type() == block.TypePrivate {
		return fmt.Errorf("%w: %s", ErrPayloadRequired, b.CID())
	}
	return g.ingest(b, b.Ops(), cid.Digest{})
}

// IngestPayload adds the entities created by a private block's decrypted
// operations, each tagged with the block's author — which is in the clear, in
// the block's pub field — and the block's digest
// (spec/05-processing-model.md, "Private chains", step 2: "If the node holds the
// decryption key, the enc field is decrypted to recover refs, ts, and ops, and
// the operations are added to the graph").
//
// THE BLOCK MUST ALREADY HAVE BEEN VALIDATED, payload included: the ordinary way
// to obtain the p argument is privacy.OpenAndValidate, which decrypts the block
// and runs every validation rule, the four that need the plaintext included.
//
// Ingesting the same block twice is a no-op. Handing the same block a second,
// different payload is ErrPayloadMismatch: a block's digest covers its
// ciphertext and a ciphertext has one plaintext, so the second payload is not
// this block's.
//
// A public or rotation block is refused: its operations are already in the
// clear, so Ingest takes it.
func (g *Graph) IngestPayload(b *block.Block, p block.Payload) error {
	if b == nil {
		return fmt.Errorf("%w: IngestPayload needs a block to tag the payload's operations with", ErrNilBlock)
	}
	if b.Type() != block.TypePrivate {
		return fmt.Errorf("graph: %s is a %s block; its refs, ts and ops are already in the clear, so Ingest takes it", b.CID(), b.Type())
	}
	// Encode validates the payload on the way, so a malformed one is refused
	// here rather than half-ingested, and the encoding identifies the payload
	// for the idempotency check below.
	encoded, err := p.Encode()
	if err != nil {
		return fmt.Errorf("graph: block %s: %w", b.CID(), err)
	}
	return g.ingest(b, p.Ops, cid.SumDigest(encoded))
}

// a creation is one entity an operation creates, extracted before the lock is
// taken.
type creation struct {
	digest cid.Digest
	kind   block.EntityKind
	value  Entity
}

// ingest is the whole of accumulation. payload is the digest of the decrypted
// payload's canonical encoding, or the zero digest for a block whose operations
// were in the clear.
func (g *Graph) ingest(b *block.Block, ops []block.Operation, payload cid.Digest) error {
	created, err := creations(ops)
	if err != nil {
		return fmt.Errorf("graph: block %s: %w", b.CID(), err)
	}
	pub := b.PublicKey()
	if len(pub) != ed25519.PublicKeySize { // unreachable: a *Block has been through Content.Validate
		return fmt.Errorf("graph: block %s has a %d-byte public key, want %d", b.CID(), len(pub), ed25519.PublicKeySize)
	}
	var author authorKey
	copy(author[:], pub)
	tag := record{author: author, block: b.Digest()}

	g.mu.Lock()
	defer g.mu.Unlock()

	if stored, ok := g.blocks[tag.block]; ok {
		if stored != payload {
			return fmt.Errorf("%w: %s", ErrPayloadMismatch, b.CID())
		}
		return nil // already ingested: same block, same entities, same tag
	}
	// Nothing is written until every entity has been checked, so a refused
	// block leaves the graph exactly as it was.
	for _, c := range created {
		if n, ok := g.entities[c.digest]; ok && n.kind != c.kind {
			return fmt.Errorf("graph: block %s creates %s %s, but that digest is already held as %s; L2 is append-only and an entity's kind cannot change",
				b.CID(), c.kind, c.digest, n.kind)
		}
	}
	for _, c := range created {
		g.add(c, tag)
	}
	g.blocks[tag.block] = payload
	g.blockOrder = insertDigest(g.blockOrder, tag.block)
	return nil
}

// add stores one entity and its authorship tag. The caller holds the write
// lock and has checked that the digest, if held, is held as the same kind.
func (g *Graph) add(c creation, tag record) {
	n, ok := g.entities[c.digest]
	if !ok {
		// A new entity. The value is stored once and never replaced: two
		// entities with the same digest have the same canonical bytes, so a
		// re-publication carries nothing the graph does not already hold
		// (spec/05-processing-model.md, "Accumulation rules").
		n = &node{kind: c.kind, value: c.value}
		g.entities[c.digest] = n
		g.order = insertDigest(g.order, c.digest)
		g.byKind[c.kind] = insertDigest(g.byKind[c.kind], c.digest)
	}
	if i, found := slices.BinarySearchFunc(n.authors, tag, compareRecords); !found {
		n.authors = slices.Insert(n.authors, i, tag)
	}
	g.authors = insertAuthor(g.authors, tag.author)
	g.byAuthor[tag.author] = insertDigest(g.byAuthor[tag.author], c.digest)
}

// creations extracts the entity each operation creates, in the order the
// operations appear in the block (spec/05-processing-model.md, "Accumulation
// rules", steps 1 and 2). The digest and kind come from the operation's own
// Creates method, which is where the block package computes them; a rotate_key
// operation reports that it creates nothing and contributes no entity.
func creations(ops []block.Operation) ([]creation, error) {
	out := make([]creation, 0, len(ops))
	for i, op := range ops {
		if op == nil { // unreachable: Content.Validate and Payload.Validate refuse a nil operation
			return nil, fmt.Errorf("operation %d is nil", i)
		}
		d, kind, ok := op.Creates()
		if !ok {
			continue // rotate_key: no entity
		}
		var value Entity
		switch o := op.(type) {
		case block.CreateAtom:
			value = o.Atom()
		case block.CreateBond:
			value = o.Bond()
		case block.CreateMolecule:
			value = o.Molecule()
		default:
			// Unreachable: block.Operation is a closed set of four types and
			// the fourth creates no entity. A new operation type that creates
			// one must be added above rather than silently dropped.
			return nil, fmt.Errorf("operation %d is a %s, whose entity this package does not know how to extract", i, op.Op())
		}
		if value.Digest() != d { // unreachable: both come from the same entity value
			return nil, fmt.Errorf("operation %d creates %s but its entity hashes to %s", i, d, value.Digest())
		}
		out = append(out, creation{digest: d, kind: kind, value: value})
	}
	return out, nil
}

// Lookup returns the entry for an entity digest. ok is false when the graph
// holds no such entity.
func (g *Graph) Lookup(d cid.Digest) (e Entry, ok bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.entities[d]
	if !ok {
		return Entry{}, false
	}
	return n.entry(), true
}

// Has reports whether the graph holds an entity with this digest.
func (g *Graph) Has(d cid.Digest) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.entities[d]
	return ok
}

// Kind returns the kind of the entity with this digest. ok is false when the
// graph holds no such entity.
func (g *Graph) Kind(d cid.Digest) (k block.EntityKind, ok bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.entities[d]
	if !ok {
		return 0, false
	}
	return n.kind, true
}

// Len returns the number of distinct entities in the graph. Re-publication of an
// entity that is already held does not change it.
func (g *Graph) Len() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.entities)
}

// Entries returns every entity in the graph, ordered by digest.
func (g *Graph) Entries() []Entry {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.entriesOf(g.order)
}

// EntriesOfKind returns every atom, bond or molecule in the graph, ordered by
// digest.
func (g *Graph) EntriesOfKind(k block.EntityKind) []Entry {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.entriesOf(g.byKind[k])
}

// EntriesByAuthor returns every entity the graph holds an authorship record for
// naming pub, ordered by digest. It is the query spec/05-processing-model.md,
// "Filtering rules", is written in terms of: an entity passes to L3 when any of
// its authors is subscribed.
//
// An author who published an entity someone else had already published is an
// author of it here: authorship accumulates, and the graph does not rank the
// records it holds.
func (g *Graph) EntriesByAuthor(pub ed25519.PublicKey) []Entry {
	if len(pub) != ed25519.PublicKeySize {
		return nil
	}
	var author authorKey
	copy(author[:], pub)

	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.entriesOf(g.byAuthor[author])
}

// entriesOf renders a digest list, which is already in ascending order. The
// caller holds the lock.
func (g *Graph) entriesOf(digests []cid.Digest) []Entry {
	if len(digests) == 0 {
		return nil
	}
	out := make([]Entry, 0, len(digests))
	for _, d := range digests {
		if n, ok := g.entities[d]; ok {
			out = append(out, n.entry())
		}
	}
	return out
}

// Provenance returns the authorship records of an entity — who published it and
// in which block — ordered by author key and then by block digest. It is empty
// for a digest the graph does not hold.
func (g *Graph) Provenance(d cid.Digest) []Authorship {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.entities[d]
	if !ok {
		return nil
	}
	return n.entry().authors
}

// Authors returns every author the graph holds an entity for, ordered by key.
//
// An author whose only ingested block is a rotation block is not among them: a
// rotate_key operation creates no entity, so there is nothing to be the author
// of.
func (g *Graph) Authors() []ed25519.PublicKey {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]ed25519.PublicKey, len(g.authors))
	for i, a := range g.authors {
		out[i] = a.public()
	}
	return out
}

// HasBlock reports whether the block with this digest has been ingested.
func (g *Graph) HasBlock(d cid.Digest) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.blocks[d]
	return ok
}

// Blocks returns the digests of every ingested block, in ascending order. A
// rotation block is among them, having been ingested, though it contributed no
// entity.
func (g *Graph) Blocks() []cid.Digest {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return slices.Clone(g.blockOrder)
}

// BlockCount returns the number of blocks ingested.
func (g *Graph) BlockCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.blocks)
}

func (g *Graph) String() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return fmt.Sprintf("graph(%d entities from %d block(s), %d author(s))", len(g.entities), len(g.blocks), len(g.authors))
}

// compareDigests orders digests bytewise.
func compareDigests(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) }

// compareAuthors orders public keys bytewise.
func compareAuthors(a, b authorKey) int { return bytes.Compare(a[:], b[:]) }

// compareRecords orders authorship records by author and then by block.
func compareRecords(a, b record) int {
	if c := compareAuthors(a.author, b.author); c != 0 {
		return c
	}
	return compareDigests(a.block, b.block)
}

// insertDigest inserts d into an ascending slice, keeping it ascending and free
// of duplicates. The indexes are maintained this way rather than sorted on
// demand so that no query has to range over a map: iteration order is
// randomised, and it MUST NOT be observable through this API.
func insertDigest(s []cid.Digest, d cid.Digest) []cid.Digest {
	i, found := slices.BinarySearchFunc(s, d, compareDigests)
	if found {
		return s
	}
	return slices.Insert(s, i, d)
}

// insertAuthor inserts a into an ascending slice, keeping it ascending and free
// of duplicates.
func insertAuthor(s []authorKey, a authorKey) []authorKey {
	i, found := slices.BinarySearchFunc(s, a, compareAuthors)
	if found {
		return s
	}
	return slices.Insert(s, i, a)
}
