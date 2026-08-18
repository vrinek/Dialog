package block

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/vrinek/Dialog/go/cid"
)

// A Verdict is what a node decided about a block when the block was offered to
// it: the three-valued outcome of spec/02-block-format.md, "Validation".
//
// Only two of the three are ever recorded. A rejected block is not stored — the
// node learned that it is wrong, and keeping it serves nothing — so there is no
// invalid verdict to read back, and the absence of a verdict means the store
// does not hold the block at all.
type Verdict int

const (
	// VerdictUnknown is the zero value, and what a store answers for a digest
	// it does not hold. It is not an undecided verdict: nothing was decided
	// because nothing was offered.
	VerdictUnknown Verdict = iota
	// VerdictValid is an accepted block. It stays accepted: a verdict moves in
	// one direction (spec/02-block-format.md, "Validation", "A verdict moves in
	// one direction"), so a store that holds this never recomputes it, and what
	// a validation left unchecked never becomes a defect later.
	VerdictValid
	// VerdictUnvalidated is a block held while a block validating it requires
	// is one the node cannot read: *stored but unvalidated*
	// (spec/05-processing-model.md, "Block reception"). It is neither valid nor
	// invalid, MUST NOT reach L2, MUST NOT serve as another block's rule 3
	// predecessor, and is the one verdict that is expected to move.
	VerdictUnvalidated
)

func (v Verdict) String() string {
	switch v {
	case VerdictValid:
		return "valid"
	case VerdictUnvalidated:
		return "stored but unvalidated"
	default:
		return "unknown"
	}
}

// A Verdicts source records the verdict it reached for each block it holds, so
// that a block's validity is a lookup rather than a re-derivation. It is the
// induction of spec/02-block-format.md, "Validation", carried by the store:
// every block in it was validated when it was received, so rule 3 asks the
// store what it accepted instead of walking an ancestry again.
//
// Validate consults it for rule 3, which requires a predecessor the node holds
// *and has accepted as valid*. It does not consult it for rule 4: a definition
// may be read from any block a source hands over, whatever that block's verdict
// (spec/05-processing-model.md, "Resolution procedure", "Resolution reads
// blocks, not verdicts").
//
// A Source that does not implement it is used under the older contract, which
// is not weaker but differently placed: its caller undertakes to offer it only
// blocks it has validated, and rule 3 is a lookup among those.
type Verdicts interface {
	// Verdict returns what the source decided about the block, and — for an
	// accepted one — the report that acceptance produced. The report may be nil
	// for a source that keeps none.
	Verdict(d cid.Digest) (Verdict, *Report)
}

// An Admission is what a ValidatingStore did with a block offered to it.
type Admission struct {
	// Digest identifies the block.
	Digest cid.Digest
	// Verdict is VerdictValid or VerdictUnvalidated. A rejected block produces
	// an error and no Admission.
	Verdict Verdict
	// Duplicate reports that the store already held the block with an accepted
	// verdict, which it returned rather than recomputing. It is how a caller
	// sees that the one-direction rule was applied rather than that the block
	// happened to validate twice.
	Duplicate bool
	// Report is the validation report of an accepted block, and nil otherwise.
	Report *Report
	// Pending is why the verdict is undecided, and nil otherwise. It carries
	// what the block is waiting for: Awaiting names the block whose arrival
	// would settle it, and errors.As with a *PendingError names the block a
	// decryption key is wanted for.
	Pending error
}

// A ValidatingStore is a node's Layer 1 storage: a MemStore that validates each
// block as it arrives and records the verdict, rather than a set of blocks a
// caller has undertaken to have validated elsewhere.
//
// It is the model spec/05-processing-model.md, "Block reception", describes, and
// the one ts/'s BlockStore implements. Three things follow from carrying the
// verdict, and none of them is available to a caller that validates by hand:
//
//   - An accepted verdict is never recomputed. A block offered twice is a
//     lookup, and Add says so with Admission.Duplicate. This is what makes the
//     one-direction rule hold by construction: a store that grows a private
//     block an accepted block's refs happen to name cannot turn that block's
//     rules 6 and 10 into a rejection, because nothing revisits the question
//     (spec/02-block-format.md, "Validation", "A verdict moves in one
//     direction").
//   - Rule 3 is exact. A block held as stored but unvalidated MUST NOT be
//     treated as another block's predecessor, and the store is what knows the
//     difference between a block it holds and a block it accepted.
//   - An undecided block is revalidated when what it awaits arrives. It is
//     filed under the block its verdict is waiting for, and Add re-offers it
//     when that block is admitted.
//
// What the store does *not* do is hold blocks back from reference resolution. A
// block held as stored but unvalidated still defines the entities its
// operations create, and Block hands it over like any other: a definition is
// self-certifying, and rule 4 reads blocks rather than verdicts
// (spec/05-processing-model.md, "Resolution procedure", "Resolution reads
// blocks, not verdicts").
//
// A rejected block is not stored. The exception is a block that was already
// held as undecided and turns out invalid when it is revalidated: its bytes
// stay where they are, its verdict stays undecided, and the rejection is
// surfaced to the caller that offers it again — a store cannot un-receive
// something, and the alternative is to record a rejection nobody asked for.
//
// A ValidatingStore is safe for concurrent use.
type ValidatingStore struct {
	blocks *MemStore
	opts   *Options

	mu       sync.Mutex
	verdicts map[cid.Digest]entry
	waiting  map[cid.Digest][]cid.Digest
}

// An entry is one block's recorded verdict.
type entry struct {
	verdict Verdict
	report  *Report
	pending error
}

// NewValidatingStore returns an empty store that validates with opts. A nil
// *Options means the defaults; the store keeps the pointer, so a caller must
// not mutate what it passed.
func NewValidatingStore(opts *Options) *ValidatingStore {
	return &ValidatingStore{
		blocks:   NewMemStore(),
		opts:     opts,
		verdicts: make(map[cid.Digest]entry),
		waiting:  make(map[cid.Digest][]cid.Digest),
	}
}

// Add offers a block to the store: validates it, records the verdict, and keeps
// the block unless it is invalid.
//
// An invalid block is rejected — the error is the *RuleError Validate produced,
// and the block is not stored. A block the store cannot decide about is kept as
// stored but unvalidated and filed under what it is waiting for; that is not an
// error, and the Admission carries the reason in Pending. A block already held
// with an accepted verdict is returned as it stands, without being validated
// again.
//
// A fork is detected, not resolved: the forking block is stored, the fork is in
// the Admission's Report, and what to do about it is the caller's
// (spec/02-block-format.md, "Validation" rule 9).
func (s *ValidatingStore) Add(b *Block) (*Admission, error) {
	adm, err := s.admit(b)
	if err != nil || adm.Duplicate {
		return adm, err
	}
	// The blocks that were waiting for this one can be decided now — whichever
	// verdict this one got. An arrival settles a rule 4 dependency by being
	// readable, which a stored but unvalidated block is; only rule 3 waits for
	// the block to be accepted, and it goes on waiting if it was not. That is
	// the SHOULD of spec/05-processing-model.md, "Block reception",
	// "Revalidation on arrival" (todos/084), which this store satisfies for
	// every block it keeps — it keeps them all.
	s.settle(adm.Digest)
	return adm, nil
}

// AddAll offers several blocks in order, stopping at the first rejection. A
// block held as stored but unvalidated is not a rejection and does not stop it.
func (s *ValidatingStore) AddAll(blocks ...*Block) error {
	for _, b := range blocks {
		if _, err := s.Add(b); err != nil {
			return err
		}
	}
	return nil
}

// admit validates one block and records what it decided, without following the
// blocks that were waiting for it.
//
// The lock is not held across Validate, which reads this store back — rule 3
// asks it for the predecessor's verdict, and resolution asks it for blocks — so
// holding it there would deadlock. What that leaves open is two goroutines
// validating the same block at once against different states of the store, and
// record closes it: an accepted verdict is never written over.
func (s *ValidatingStore) admit(b *Block) (*Admission, error) {
	if b == nil {
		return nil, fmt.Errorf("block: Add called with a nil block")
	}
	d := b.Digest()
	if e, held := s.lookup(d); held && e.verdict == VerdictValid {
		// A verdict moves in one direction: an accepted block is accepted, and
		// re-validating it against a store that has grown since could only take
		// something away.
		return &Admission{Digest: d, Verdict: VerdictValid, Duplicate: true, Report: e.report}, nil
	}

	report, err := Validate(b, s, s.opts)
	if err != nil && !IsUnvalidated(err) {
		return nil, err
	}
	if storeErr := s.store(b); storeErr != nil {
		return nil, storeErr
	}
	if err != nil {
		awaited, waits := Awaiting(err)
		s.record(d, entry{verdict: VerdictUnvalidated, pending: err}, awaited, waits)
		return &Admission{Digest: d, Verdict: VerdictUnvalidated, Pending: err}, nil
	}
	s.record(d, entry{verdict: VerdictValid, report: report}, cid.Digest{}, false)
	return &Admission{Digest: d, Verdict: VerdictValid, Report: report}, nil
}

// lookup reads a recorded verdict.
func (s *ValidatingStore) lookup(d cid.Digest) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.verdicts[d]
	return e, ok
}

// record writes a verdict, and — for an undecided one — files the block under
// what it is waiting for. An accepted verdict is never written over: that is
// the one-direction rule, and it is also what keeps two concurrent validations
// of the same block from downgrading it.
func (s *ValidatingStore) record(d cid.Digest, e entry, awaited cid.Digest, waits bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, held := s.verdicts[d]; held && existing.verdict == VerdictValid {
		return
	}
	s.verdicts[d] = e
	if waits {
		s.file(awaited, d)
	}
}

// store puts the block in the underlying MemStore. A fork there is not an error
// at this level: Validate has already detected it and put it in the report,
// which is where the caller reads it. Nothing else is expected, and a fault in
// the storage layer is passed on rather than mistaken for a verdict.
func (s *ValidatingStore) store(b *Block) error {
	var fork *ForkError
	if err := s.blocks.Add(b); err != nil && !errors.As(err, &fork) {
		return err
	}
	return nil
}

// file records that the block d is waiting for the block awaited. The caller
// holds the lock.
func (s *ValidatingStore) file(awaited, d cid.Digest) {
	held := s.waiting[awaited]
	if !slices.Contains(held, d) {
		s.waiting[awaited] = append(held, d)
	}
}

// settle revalidates the blocks that were waiting for the block that just
// arrived, and then the blocks waiting for whichever of those became valid.
//
// A block that is still undecided is filed again under whatever it is waiting
// for now, and a block that turns out invalid keeps its bytes and its undecided
// verdict: the caller that offered it has already been told, and re-offering it
// is how a rejection is surfaced.
func (s *ValidatingStore) settle(arrived cid.Digest) {
	queue := []cid.Digest{arrived}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		for _, held := range s.take(d) {
			b, err := s.blocks.Block(held)
			if err != nil {
				continue
			}
			adm, err := s.admit(b)
			if err != nil || adm.Verdict != VerdictValid {
				continue
			}
			queue = append(queue, held)
		}
	}
}

// take removes and returns the blocks filed as waiting for d.
func (s *ValidatingStore) take(d cid.Digest) []cid.Digest {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.waiting[d]
	delete(s.waiting, d)
	return held
}

// Verdict implements Verdicts.
func (s *ValidatingStore) Verdict(d cid.Digest) (Verdict, *Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.verdicts[d]
	return e.verdict, e.report
}

// Accepted reports whether the store has accepted the block as valid — the
// question rule 3 asks, and the one a caller asks before letting a block's
// operations reach L2.
func (s *ValidatingStore) Accepted(d cid.Digest) bool {
	verdict, _ := s.Verdict(d)
	return verdict == VerdictValid
}

// Pending returns why the store has not decided about a block, or nil for a
// block it accepted or does not hold.
func (s *ValidatingStore) Pending(d cid.Digest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verdicts[d].pending
}

// Block implements Source. It hands over every block the store holds, accepted
// or not: reference resolution reads blocks rather than verdicts, and a caller
// that wants only accepted blocks asks Verdict (see the type's doc comment).
func (s *ValidatingStore) Block(d cid.Digest) (*Block, error) { return s.blocks.Block(d) }

// Has reports whether the store holds the block, whatever its verdict.
func (s *ValidatingStore) Has(d cid.Digest) bool { return s.blocks.Has(d) }

// Len returns the number of blocks held, accepted and undecided together.
func (s *ValidatingStore) Len() int { return s.blocks.Len() }

// BlocksWithPrev implements Siblings.
func (s *ValidatingStore) BlocksWithPrev(pub ed25519.PublicKey, prev *cid.Digest) []cid.Digest {
	return s.blocks.BlocksWithPrev(pub, prev)
}

// BlocksReferencing implements Referrers.
func (s *ValidatingStore) BlocksReferencing(d cid.Digest) []cid.Digest {
	return s.blocks.BlocksReferencing(d)
}

// Forks returns every fork the store holds, in the order MemStore.Forks defines.
func (s *ValidatingStore) Forks() []Fork { return s.blocks.Forks() }

// Tips returns the digests of the author's stored blocks that no other stored
// block names as its predecessor.
func (s *ValidatingStore) Tips(pub ed25519.PublicKey) []cid.Digest { return s.blocks.Tips(pub) }
