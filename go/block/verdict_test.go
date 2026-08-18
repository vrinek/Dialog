package block

import (
	"errors"
	"slices"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// mustAdd offers a block to a ValidatingStore and fails the test if it is
// rejected. A rejection is an error; an undecided verdict is not.
func mustAdd(t *testing.T, s *ValidatingStore, b *Block) *Admission {
	t.Helper()
	adm, err := s.Add(b)
	if err != nil {
		t.Fatalf("Add(%s) = %v, want no rejection", b, err)
	}
	return adm
}

// TestValidatingStoreRecordsVerdicts is the shape of the store: a block is
// validated when it is offered, the verdict is recorded, and offering it again
// reads that verdict instead of computing another one
// (spec/05-processing-model.md, "Block reception").
func TestValidatingStoreRecordsVerdicts(t *testing.T) {
	store := NewValidatingStore(nil)
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1000, nil, MustCreateBond("_A_ is the capital of _B_"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	second, err := author.Public(2000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if v, _ := store.Verdict(genesis.Digest()); v != VerdictUnknown {
		t.Errorf("verdict before the block arrives = %s, want %s", v, VerdictUnknown)
	}

	adm := mustAdd(t, store, genesis)
	if adm.Verdict != VerdictValid || adm.Duplicate || adm.Report == nil {
		t.Fatalf("Add(genesis) = %+v, want a fresh valid verdict with a report", adm)
	}
	if !store.Accepted(genesis.Digest()) {
		t.Error("the store does not report the genesis block as accepted")
	}

	// Rule 3 is a lookup in what the store accepted, so the second block
	// validates against a verdict rather than against a re-derivation.
	if adm := mustAdd(t, store, second); adm.Verdict != VerdictValid {
		t.Fatalf("Add(second) = %+v, want a valid verdict", adm)
	}

	// The same block again: read, not recomputed.
	again := mustAdd(t, store, genesis)
	if again.Verdict != VerdictValid || !again.Duplicate {
		t.Errorf("Add(genesis) twice = %+v, want the recorded verdict marked as a duplicate", again)
	}
	if store.Len() != 2 {
		t.Errorf("Len = %d, want 2", store.Len())
	}
}

// TestAcceptedVerdictSurvivesTheStoreGrowing is the regression test for the
// flip todos/081 found: a block accepted with a refs entry the store did not
// hold must stay accepted when that entry arrives and turns out to be a private
// block, which rules 6 and 10 would reject on a fresh validation
// (spec/02-block-format.md, "Validation", "A verdict moves in one direction").
func TestAcceptedVerdictSurvivesTheStoreGrowing(t *testing.T) {
	alice := mustBuilder(t, 1)
	secret, err := alice.Private(testCiphertext("alice"), make([]byte, NonceSize))
	if err != nil {
		t.Fatalf("private: %v", err)
	}
	bob := mustBuilder(t, 2)
	b, err := bob.Public(1, []cid.Digest{secret.Digest()}, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	store := NewValidatingStore(nil)
	adm := mustAdd(t, store, b) // Alice's block is not held yet
	if adm.Verdict != VerdictValid {
		t.Fatalf("Add(b) = %+v, want a valid verdict", adm)
	}
	if !slices.Equal(adm.Report.UncheckedRefs, []cid.Digest{secret.Digest()}) {
		t.Fatalf("UncheckedRefs = %v, want the entry the store did not hold", adm.Report.UncheckedRefs)
	}

	// Alice's block arrives, and it is private — the entry Bob's verdict never
	// covered. A bare Validate against the grown store now finds a rule 6
	// violation (TestUncheckedRefsAreInformational pins that), and the store
	// must not act on it.
	mustAdd(t, store, secret)
	if _, err := Validate(b, store, nil); !isRule(err, 6) {
		t.Fatalf("the premise of this test has changed: Validate over the grown store = %v, want a rule 6 finding", err)
	}
	if !store.Accepted(b.Digest()) {
		t.Error("the store downgraded a block it had accepted; a verdict moves in one direction")
	}
	if adm := mustAdd(t, store, b); !adm.Duplicate || adm.Verdict != VerdictValid {
		t.Errorf("re-offering the accepted block = %+v, want the recorded verdict", adm)
	}

	// ValidateChain is the documented way to ask for a second validation, and
	// it is the path todos/081 found: over a verdict-carrying store it reads
	// what was accepted rather than recomputing it.
	chain, err := ValidateChain(b.Digest(), store, nil)
	if err != nil {
		t.Fatalf("ValidateChain over the grown store = %v, want the accepted verdict", err)
	}
	if chain.Len() != 1 {
		t.Errorf("chain = %s, want the one block", chain)
	}
}

// TestUnvalidatedBlockIsNotAPredecessor covers the half of rule 3 only a
// verdict-carrying store can check: a block it holds without having accepted it
// MUST NOT be treated as another block's predecessor
// (spec/05-processing-model.md, "Block reception").
func TestUnvalidatedBlockIsNotAPredecessor(t *testing.T) {
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	second, err := author.Public(2, nil, MustCreateAtom("Paris, the capital of France"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	third, err := author.Public(3, nil, MustCreateAtom("Lyon"))
	if err != nil {
		t.Fatalf("third: %v", err)
	}

	store := NewValidatingStore(nil)
	held := mustAdd(t, store, second) // its predecessor has not arrived
	if held.Verdict != VerdictUnvalidated {
		t.Fatalf("Add(second) = %+v, want an undecided verdict", held)
	}
	if d, ok := Awaiting(held.Pending); !ok || d != genesis.Digest() {
		t.Errorf("Awaiting = %s, %v; want the missing genesis block %s", d, ok, genesis.Digest())
	}

	// The tip's own predecessor is held — and unaccepted, which is the point.
	onward := mustAdd(t, store, third)
	if onward.Verdict != VerdictUnvalidated || !errors.Is(onward.Pending, ErrUnaccepted) {
		t.Fatalf("Add(third) = %+v, want an undecided verdict wrapping ErrUnaccepted", onward)
	}

	// The missing block arrives, and the two held blocks are decided in order:
	// nothing about either changed.
	mustAdd(t, store, genesis)
	for _, b := range []*Block{second, third} {
		if !store.Accepted(b.Digest()) {
			t.Errorf("%s was not accepted once its ancestry arrived", b.Digest())
		}
	}
	if _, err := ValidateChain(third.Digest(), store, nil); err != nil {
		t.Errorf("ValidateChain = %v, want the complete chain", err)
	}
}

// TestPendingReferenceIsRevalidatedOnArrival is the rule 4 half of the same
// mechanism: a block whose reference resolution ran out of blocks it could read
// is held, filed under the block it needs, and decided when that block arrives.
//
// The block it needs is admitted as *undecided* itself — its own ancestry is
// missing — and that is enough, because resolution reads blocks rather than
// verdicts (spec/05-processing-model.md, "Resolution procedure").
func TestPendingReferenceIsRevalidatedOnArrival(t *testing.T) {
	bondTemplate := "_A_ is the capital of _B_"
	bond := entity.MustBond(bondTemplate)
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	alice := mustBuilder(t, 1)
	if _, err := alice.Public(1, nil, MustCreateAtom("a block nobody sent")); err != nil {
		t.Fatalf("undelivered: %v", err)
	}
	provider, err := alice.Public(2, nil, MustCreateBond(bondTemplate))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	bob := mustBuilder(t, 2)
	b, err := bob.Public(3, []cid.Digest{provider.Digest()},
		MustCreateAtom(paris.Description()),
		MustCreateAtom(france.Description()),
		MustCreateMolecule(bond, fillers))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	store := NewValidatingStore(nil)
	held := mustAdd(t, store, b)
	if held.Verdict != VerdictUnvalidated {
		t.Fatalf("Add(b) = %+v, want an undecided verdict", held)
	}
	if d, ok := Awaiting(held.Pending); !ok || d != provider.Digest() {
		t.Errorf("Awaiting = %s, %v; want the unheld refs entry %s", d, ok, provider.Digest())
	}

	// Alice's block cannot be accepted — its own predecessor is missing — and
	// Bob's block becomes valid on it all the same.
	if adm := mustAdd(t, store, provider); adm.Verdict != VerdictUnvalidated {
		t.Fatalf("Add(provider) = %+v, want an undecided verdict of its own", adm)
	}
	if !store.Accepted(b.Digest()) {
		t.Error("the block waiting on the definition was not accepted once the block carrying it arrived")
	}
	if store.Accepted(provider.Digest()) {
		t.Error("the block that carried the definition must still be undecided: its own ancestry is missing")
	}
}

// TestInvalidBlockIsNotStored: a rejection is the one verdict the store does
// not record, because it does not keep the block.
func TestInvalidBlockIsNotStored(t *testing.T) {
	bond := entity.MustBond("_A_ is the capital of _B_")
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	author := mustBuilder(t, 1)
	orphan, err := author.Public(1, nil,
		MustCreateAtom(paris.Description()),
		MustCreateAtom(france.Description()),
		MustCreateMolecule(bond, fillers)) // the bond is nowhere, and nothing is missing
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	store := NewValidatingStore(nil)
	adm, err := store.Add(orphan)
	if !isRule(err, 4) || IsUnvalidated(err) {
		t.Fatalf("Add(orphan) = %+v, %v; want a definitive rule 4 rejection", adm, err)
	}
	if store.Has(orphan.Digest()) {
		t.Error("a rejected block was stored")
	}
	if v, _ := store.Verdict(orphan.Digest()); v != VerdictUnknown {
		t.Errorf("verdict of a rejected block = %s, want %s", v, VerdictUnknown)
	}
}

// TestValidatingStoreDetectsForks: detection is normative, handling is the
// caller's, so a fork is stored and reported exactly as MemStore's is
// (spec/02-block-format.md, "Validation" rule 9).
func TestValidatingStoreDetectsForks(t *testing.T) {
	store := NewValidatingStore(nil)
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	mustAdd(t, store, genesis)

	prev := genesis.Digest()
	left, err := Sign(Content{Version: Version, Type: TypePublic, Prev: &prev, TS: 2,
		Ops: []Operation{MustCreateAtom("Paris, the capital of France")}}, testKey(t, 1))
	if err != nil {
		t.Fatalf("left: %v", err)
	}
	right, err := Sign(Content{Version: Version, Type: TypePublic, Prev: &prev, TS: 3,
		Ops: []Operation{MustCreateAtom("Lyon")}}, testKey(t, 1))
	if err != nil {
		t.Fatalf("right: %v", err)
	}

	mustAdd(t, store, left)
	adm := mustAdd(t, store, right)
	if len(adm.Report.Forks) != 1 {
		t.Fatalf("forks in the report = %v, want the one this block creates", adm.Report.Forks)
	}
	if len(store.Forks()) != 1 {
		t.Errorf("store.Forks() = %v, want one", store.Forks())
	}
	if !store.Accepted(right.Digest()) {
		t.Error("the forking block was not stored; this store flags a fork, it does not reject it")
	}
}
