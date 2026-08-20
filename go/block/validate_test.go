package block

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// isRule reports whether err is a violation of the given numbered validation
// rule of spec/02-block-format.md.
func isRule(err error, rule int) bool {
	var re *RuleError
	return errors.As(err, &re) && re.Rule == rule
}

// mustValidate fails the test if the block does not validate.
func mustValidate(t *testing.T, b *Block, src Source, opts *Options) *Report {
	t.Helper()
	report, err := Validate(b, src, opts)
	if err != nil {
		t.Fatalf("Validate(%s) = %v, want nil", b, err)
	}
	return report
}

// TestChainHappyPath is genesis → append → append, validated block by block
// and then as a whole chain.
func TestChainHappyPath(t *testing.T) {
	store := NewMemStore()
	author := mustBuilder(t, 1)

	genesis, err := author.Public(1000, nil, MustCreateBond("_A_ is the capital of _B_"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	second, err := author.Public(2000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	third, err := author.Public(3000, nil, MustCreateAtom("Paris, the capital of France"))
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	store.MustAdd(genesis, second, third)

	for _, b := range []*Block{genesis, second, third} {
		mustValidate(t, b, store, nil)
	}

	chain, err := ValidateChain(third.Digest(), store, nil)
	if err != nil {
		t.Fatalf("ValidateChain: %v", err)
	}
	if chain.Len() != 3 {
		t.Errorf("chain length = %d, want 3", chain.Len())
	}
	if chain.Genesis().Digest() != genesis.Digest() || chain.Tip().Digest() != third.Digest() {
		t.Error("the chain does not run from the genesis block to the tip")
	}
	if len(chain.Report.Forks) != 0 {
		t.Errorf("forks = %v, want none", chain.Report.Forks)
	}
	if _, rotated := chain.Rotation(); rotated {
		t.Error("a chain that has not rotated must not report a rotation block")
	}
}

// TestWrongPrevFails covers the two ways rule 3 is broken: a predecessor the
// source does not hold, and a predecessor signed by somebody else.
func TestWrongPrevFails(t *testing.T) {
	store := NewMemStore()
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	store.MustAdd(genesis)

	t.Run("unknown predecessor", func(t *testing.T) {
		missing := cid.Digest{9: 9}
		b, err := Sign(Content{
			Version: Version, Type: TypePublic, Prev: &missing, TS: 2000,
			Ops: []Operation{MustCreateAtom("Paris, the capital of France")},
		}, testKey(t, 1))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		_, err = Validate(b, store, nil)
		if !isRule(err, 3) || !errors.Is(err, ErrNotFound) {
			t.Fatalf("Validate = %v, want a rule 3 violation wrapping ErrNotFound", err)
		}
	})

	t.Run("predecessor of another author", func(t *testing.T) {
		prev := genesis.Digest()
		b, err := Sign(Content{
			Version: Version, Type: TypePublic, Prev: &prev, TS: 2000,
			Ops: []Operation{MustCreateAtom("Paris, the capital of France")},
		}, testKey(t, 2)) // a different key linking onto author 1's chain
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := Validate(b, store, nil); !isRule(err, 3) {
			t.Fatalf("Validate = %v, want a rule 3 violation", err)
		}
	})
}

// TestForkDetection covers rule 9 in both positions a fork can take: two
// blocks claiming the same predecessor, and two genesis blocks, which claim
// the same (null) one.
func TestForkDetection(t *testing.T) {
	store := NewMemStore()
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	store.MustAdd(genesis)

	prev := genesis.Digest()
	left, err := Sign(Content{Version: Version, Type: TypePublic, Prev: &prev, TS: 2000, Ops: []Operation{MustCreateAtom("left")}}, testKey(t, 1))
	if err != nil {
		t.Fatalf("left: %v", err)
	}
	right, err := Sign(Content{Version: Version, Type: TypePublic, Prev: &prev, TS: 2000, Ops: []Operation{MustCreateAtom("right")}}, testKey(t, 1))
	if err != nil {
		t.Fatalf("right: %v", err)
	}
	if !IsFork(left, right) {
		t.Error("two distinct blocks by one author claiming the same prev are a fork")
	}
	if IsFork(left, left) {
		t.Error("a block is not a fork of itself")
	}

	if err := store.Add(left); err != nil {
		t.Fatalf("storing the first block of the pair: %v", err)
	}
	var forkErr *ForkError
	if err := store.Add(right); !errors.As(err, &forkErr) {
		t.Fatalf("storing the second block = %v, want a *ForkError", err)
	}
	if len(forkErr.Fork.Blocks) != 2 || forkErr.Fork.Prev == nil || *forkErr.Fork.Prev != prev {
		t.Errorf("fork = %+v, want the two blocks at prev %s", forkErr.Fork, prev)
	}

	// Detection does not make a block invalid: handling is the caller's
	// (spec/02-block-format.md, "Validation" rule 9).
	report := mustValidate(t, right, store, nil)
	if len(report.Forks) != 1 {
		t.Fatalf("report.Forks = %v, want the one fork", report.Forks)
	}
	if len(store.Forks()) != 1 {
		t.Errorf("store.Forks() = %v, want one", store.Forks())
	}

	// Two genesis blocks by the same author fork at the genesis position.
	other := mustBuilder(t, 2)
	g1, err := other.Public(1000, nil, MustCreateAtom("one"))
	if err != nil {
		t.Fatalf("g1: %v", err)
	}
	g2, err := Sign(Content{Version: Version, Type: TypePublic, TS: 1000, Ops: []Operation{MustCreateAtom("two")}}, testKey(t, 2))
	if err != nil {
		t.Fatalf("g2: %v", err)
	}
	if err := store.Add(g1); err != nil {
		t.Fatalf("storing g1: %v", err)
	}
	if err := store.Add(g2); !errors.As(err, &forkErr) {
		t.Fatalf("storing a second genesis block = %v, want a *ForkError", err)
	}
	if forkErr.Fork.Prev != nil {
		t.Errorf("a genesis fork must report a nil prev, got %s", forkErr.Fork.Prev)
	}
}

// TestReachability walks every path rule 4 allows and every way it can fail
// (spec/02-block-format.md, "Validation" rule 4;
// spec/05-processing-model.md, "Resolution procedure").
func TestReachability(t *testing.T) {
	bondTemplate := "_A_ is the capital of _B_"
	bond := entity.MustBond(bondTemplate)
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	t.Run("same block", func(t *testing.T) {
		store := NewMemStore()
		author := mustBuilder(t, 1)
		b, err := author.Public(1, nil,
			MustCreateBond(bondTemplate),
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b)
		if report := mustValidate(t, b, store, nil); report.Scanned != 0 {
			t.Errorf("scanned %d foreign block(s), want 0", report.Scanned)
		}
	})

	t.Run("ancestor", func(t *testing.T) {
		store := NewMemStore()
		author := mustBuilder(t, 1)
		genesis, err := author.Public(1, nil, MustCreateBond(bondTemplate), MustCreateAtom(paris.Description()))
		if err != nil {
			t.Fatalf("genesis: %v", err)
		}
		middle, err := author.Public(2, nil, MustCreateAtom("an unrelated entity"))
		if err != nil {
			t.Fatalf("middle: %v", err)
		}
		tip, err := author.Public(3, nil, MustCreateAtom(france.Description()), MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("tip: %v", err)
		}
		store.MustAdd(genesis, middle, tip)
		if report := mustValidate(t, tip, store, nil); report.Scanned != 0 {
			t.Errorf("scanned %d foreign block(s); the author's own chain needs none", report.Scanned)
		}
	})

	t.Run("refs one hop", func(t *testing.T) {
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, []cid.Digest{provider.Digest()},
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(provider, b)
		if report := mustValidate(t, b, store, nil); report.Scanned != 1 {
			t.Errorf("scanned %d foreign block(s), want 1", report.Scanned)
		}
	})

	t.Run("refs transitive", func(t *testing.T) {
		// Carol publishes the bond. Alice publishes the atoms in a block that
		// references Carol's. Bob references only Alice's block, so the bond
		// resolves by recursing into that block's own refs — step 5 of the
		// resolution procedure. Each hop crosses to another author's chain,
		// which is the only kind of ref there is: a block may not list one of
		// its own chain (rule 10).
		store := NewMemStore()
		carol := mustBuilder(t, 3)
		first, err := carol.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		alice := mustBuilder(t, 1)
		second, err := alice.Public(2, []cid.Digest{first.Digest()},
			MustCreateAtom(paris.Description()), MustCreateAtom(france.Description()))
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(3, []cid.Digest{second.Digest()}, MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(first, second, b)
		report := mustValidate(t, b, store, nil)
		if report.Scanned != 2 {
			t.Errorf("scanned %d foreign block(s), want 2 (the ref and the block it references)", report.Scanned)
		}

		// The same block with the scan limit set below what resolution needs
		// is invalid (spec/05-processing-model.md, "Scan limit"). The limit is
		// a bound the node chose, reached against blocks it holds, so it is a
		// definitive rejection and not the undecided verdict a missing block
		// produces.
		_, err = Validate(b, store, &Options{ScanLimit: 1})
		if !isRule(err, 4) || !errors.Is(err, ErrScanLimit) {
			t.Errorf("Validate with a scan limit of 1 = %v, want a rule 4 violation wrapping ErrScanLimit", err)
		}
		if IsUnvalidated(err) {
			t.Errorf("Validate with a scan limit of 1 = %v, want a definitive rejection and not a stored-but-unvalidated verdict", err)
		}
	})

	t.Run("unknown digest", func(t *testing.T) {
		store := NewMemStore()
		author := mustBuilder(t, 1)
		b, err := author.Public(1, nil,
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers)) // the bond is nowhere
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b)
		// Every block resolution could read was read — the block itself, and it
		// has neither ancestors nor refs — so the digest is provably absent and
		// the rejection is definitive.
		_, err = Validate(b, store, nil)
		if !isRule(err, 4) {
			t.Errorf("Validate = %v, want a rule 4 violation", err)
		}
		if IsUnvalidated(err) {
			t.Errorf("Validate = %v, want an invalid block and not an undecided one: nothing was missing from the store", err)
		}
	})

	t.Run("defined only in an unreferenced foreign chain", func(t *testing.T) {
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, nil, // no refs: the bond is visible to the store, not to the block
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(provider, b)
		// The store holds the defining block; the block does not name it. That
		// is a completed resolution over everything reachable *from the block*,
		// so it is a rejection and not an undecided verdict.
		_, err = Validate(b, store, nil)
		if !isRule(err, 4) {
			t.Errorf("Validate = %v, want a rule 4 violation: prev is not a resolution path and refs is empty", err)
		}
		if IsUnvalidated(err) {
			t.Errorf("Validate = %v, want an invalid block: nothing resolution needed was missing", err)
		}
	})
}

// TestUnobtainableReferenceIsUndecided is the third outcome of rule 4
// (spec/02-block-format.md, "Validation" rule 4; spec/05-processing-model.md,
// "Resolution procedure"): resolution that needs a block the source does not
// hold has not shown the block invalid, it has failed to decide. The block is
// stored but unvalidated, exactly as one whose prev has not arrived, and it
// validates unchanged once the missing block turns up.
func TestUnobtainableReferenceIsUndecided(t *testing.T) {
	bondTemplate := "_A_ is the capital of _B_"
	bond := entity.MustBond(bondTemplate)
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	t.Run("a refs entry the source does not hold", func(t *testing.T) {
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, []cid.Digest{provider.Digest()},
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b) // the provider is never stored

		_, err = Validate(b, store, nil)
		if !isRule(err, 4) || !IsUnvalidated(err) {
			t.Fatalf("Validate = %v, want a rule 4 error wrapping ErrNotFound: the node cannot decide, so the block is stored but unvalidated", err)
		}
		// The verdict is a function of what the store holds and nothing else:
		// asking again with nothing changed answers the same.
		if _, again := Validate(b, store, nil); again.Error() != err.Error() {
			t.Errorf("a second Validate over the same store = %v, want the same verdict as %v", again, err)
		}

		// The missing block arrives; nothing about the block changes; it is
		// valid. A source that withheld one foreign block never got to make it
		// invalid.
		store.MustAdd(provider)
		if report := mustValidate(t, b, store, nil); report.Scanned != 1 {
			t.Errorf("scanned %d foreign block(s) after the provider arrived, want 1", report.Scanned)
		}
	})

	t.Run("a transitively referenced block the source does not hold", func(t *testing.T) {
		// Carol defines the bond, Alice's block names Carol's, Bob's names
		// Alice's. Bob's refs entry is held; the block it leads to is not, and
		// the digest is unresolved for want of a block one hop further out.
		store := NewMemStore()
		carol := mustBuilder(t, 3)
		first, err := carol.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		alice := mustBuilder(t, 1)
		second, err := alice.Public(2, []cid.Digest{first.Digest()},
			MustCreateAtom(paris.Description()), MustCreateAtom(france.Description()))
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(3, []cid.Digest{second.Digest()}, MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(second, b) // Carol's block is never stored

		_, err = Validate(b, store, nil)
		if !isRule(err, 4) || !IsUnvalidated(err) {
			t.Fatalf("Validate = %v, want a rule 4 error wrapping ErrNotFound", err)
		}
		store.MustAdd(first)
		mustValidate(t, b, store, nil)
	})

	t.Run("an ancestor deeper in the author's own chain", func(t *testing.T) {
		// Rule 3 checks the immediate predecessor; a gap further back surfaces
		// only when a digest needs what the missing ancestor defined, and it is
		// the same undecided verdict.
		store := NewMemStore()
		author := mustBuilder(t, 1)
		genesis, err := author.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("genesis: %v", err)
		}
		middle, err := author.Public(2, nil, MustCreateAtom("an unrelated entity"))
		if err != nil {
			t.Fatalf("middle: %v", err)
		}
		tip, err := author.Public(3, nil,
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("tip: %v", err)
		}
		store.MustAdd(middle, tip) // the genesis block, which defines the bond, is missing

		_, err = Validate(tip, store, nil)
		if !isRule(err, 4) || !IsUnvalidated(err) {
			t.Fatalf("Validate = %v, want a rule 4 error wrapping ErrNotFound", err)
		}
		store.MustAdd(genesis)
		mustValidate(t, tip, store, nil)
	})

	t.Run("a refs entry resolution never needed", func(t *testing.T) {
		// The block resolves every digest from its own operations, so the entry
		// it also names is never fetched. Resolution completed; the verdict is
		// valid, not undecided (spec/05, "Resolution procedure": outcome 3 is
		// reached only when the missing block could have mattered).
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Public(1, nil, MustCreateAtom("an entity nobody needs"))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, []cid.Digest{provider.Digest()},
			MustCreateBond(bondTemplate),
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b) // the provider is never stored
		report := mustValidate(t, b, store, nil)
		if report.Scanned != 0 {
			t.Errorf("scanned %d foreign block(s), want 0: no digest needed one", report.Scanned)
		}
	})
}

// TestScanLimitCountingUnit pins what the scan limit counts, which
// spec/05-processing-model.md, "Scan limit", defines normatively: one distinct
// foreign block scanned per block validated. Two implementations that count
// anything else — digests resolved, recursion levels, fetches including
// repeats — reject different blocks at the same setting, which is the whole
// reason the unit is written down.
func TestScanLimitCountingUnit(t *testing.T) {
	bondTemplate := "_A_ is the capital of _B_"
	bond := entity.MustBond(bondTemplate)
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	if DefaultScanLimit != 256 {
		t.Errorf("DefaultScanLimit = %d, want the 256 spec/05-processing-model.md, \"Scan limit\", asks every implementation to default to", DefaultScanLimit)
	}

	t.Run("a refs entry resolution never needs is not scanned", func(t *testing.T) {
		// The entry is still fetched — rules 6 and 10 are checked against it —
		// but nothing reads its operations, so it costs no unit.
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Public(1, nil, MustCreateAtom("an entity nobody here needs"))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, []cid.Digest{provider.Digest()},
			MustCreateBond(bondTemplate),
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(provider, b)
		if report := mustValidate(t, b, store, nil); report.Scanned != 0 {
			t.Errorf("scanned %d foreign block(s), want 0: every digest resolves inside the block itself", report.Scanned)
		}
		// The entry was still fetched and checked: that a rule 6 rejection
		// lands on a block whose operations resolve without any scanning is
		// what TestPublicBlockMustNotReferencePrivate pins.
	})

	t.Run("a block the graph names twice costs one unit", func(t *testing.T) {
		// carol's block defines the bond and is named by two different blocks
		// of the refs graph, so resolution meets it twice. It is scanned once.
		store := NewMemStore()
		carol := mustBuilder(t, 3)
		provider, err := carol.Public(1, nil, MustCreateBond(bondTemplate))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		alice := mustBuilder(t, 1)
		first, err := alice.Public(2, []cid.Digest{provider.Digest()}, MustCreateAtom("an unrelated entity"))
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		dave := mustBuilder(t, 4)
		second, err := dave.Public(3, []cid.Digest{provider.Digest()}, MustCreateAtom("another unrelated entity"))
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Public(4, []cid.Digest{first.Digest(), second.Digest()},
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(provider, first, second, b)

		// first, second and provider: three distinct blocks, four names.
		report := mustValidate(t, b, store, nil)
		if report.Scanned != 3 {
			t.Errorf("scanned %d foreign block(s), want 3 distinct blocks — the provider is named twice and counts once", report.Scanned)
		}
		if _, err := Validate(b, store, &Options{ScanLimit: 3}); err != nil {
			t.Errorf("Validate with a scan limit of 3 = %v, want acceptance: three distinct blocks are scanned", err)
		}
		_, err = Validate(b, store, &Options{ScanLimit: 2})
		if !isRule(err, 4) || !errors.Is(err, ErrScanLimit) {
			t.Errorf("Validate with a scan limit of 2 = %v, want a rule 4 violation wrapping ErrScanLimit", err)
		}
	})

	t.Run("an ancestor is not a foreign block", func(t *testing.T) {
		// The author's own chain is walked through prev, which the limit does
		// not bound: a limit of zero still admits a block that resolves from
		// its own ancestry.
		store := NewMemStore()
		author := mustBuilder(t, 1)
		genesis, err := author.Public(1, nil, MustCreateBond(bondTemplate), MustCreateAtom(paris.Description()))
		if err != nil {
			t.Fatalf("genesis: %v", err)
		}
		tip, err := author.Public(2, nil, MustCreateAtom(france.Description()), MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("tip: %v", err)
		}
		store.MustAdd(genesis, tip)
		report, err := Validate(tip, store, &Options{ScanLimit: -1})
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if report.Scanned != 0 {
			t.Errorf("scanned %d foreign block(s), want 0: ancestors are not foreign", report.Scanned)
		}
	})
}

// TestDataModelConformance covers rule 5: the filler count against the bond's
// template, and the entity kind each position names.
func TestDataModelConformance(t *testing.T) {
	t.Run("filler count mismatch", func(t *testing.T) {
		bond := entity.MustBond("_A_ is the capital of _B_") // two variables
		atom := entity.MustAtom("France")
		op, err := NewCreateMolecule(bond.Digest(), []entity.Filler{entity.AtomFiller(atom.Digest())})
		if err != nil {
			t.Fatalf("NewCreateMolecule: %v", err)
		}
		store := NewMemStore()
		author := mustBuilder(t, 1)
		b, err := author.Public(1, nil, MustCreateBond(bond.Template()), MustCreateAtom(atom.Description()), op)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b)
		if _, err := Validate(b, store, nil); !isRule(err, 5) {
			t.Errorf("Validate = %v, want a rule 5 violation", err)
		}
	})

	t.Run("filler names the wrong kind of entity", func(t *testing.T) {
		// A type 0 filler must carry an atom digest; this one carries a bond's.
		subject := entity.MustBond("_A_ is a bond")
		bond := entity.MustBond("_A_ is heavy")
		op, err := NewCreateMolecule(bond.Digest(), []entity.Filler{entity.AtomFiller(subject.Digest())})
		if err != nil {
			t.Fatalf("NewCreateMolecule: %v", err)
		}
		store := NewMemStore()
		author := mustBuilder(t, 1)
		b, err := author.Public(1, nil, MustCreateBond(subject.Template()), MustCreateBond(bond.Template()), op)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b)
		if _, err := Validate(b, store, nil); !isRule(err, 5) {
			t.Errorf("Validate = %v, want a rule 5 violation", err)
		}
	})

	t.Run("scalar unit atom", func(t *testing.T) {
		bond := entity.MustBond("_A_ is the mass")
		kilogram := entity.MustAtom("kilogram")
		scalar, err := entity.IntScalar(70).WithUnit(kilogram.Digest())
		if err != nil {
			t.Fatalf("WithUnit: %v", err)
		}
		filler, err := entity.ScalarFiller(scalar)
		if err != nil {
			t.Fatalf("ScalarFiller: %v", err)
		}
		op, err := NewCreateMoleculeFor(bond, []entity.Filler{filler})
		if err != nil {
			t.Fatalf("NewCreateMoleculeFor: %v", err)
		}

		// Reachable: the unit atom is created earlier in the same block.
		store := NewMemStore()
		author := mustBuilder(t, 1)
		ok, err := author.Public(1, nil, MustCreateBond(bond.Template()), MustCreateAtom(kilogram.Description()), op)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(ok)
		mustValidate(t, ok, store, nil)

		// Unreachable: the unit atom is nowhere. A scalar's unit is an internal
		// reference like any other, so rule 4 covers it
		// (spec/02-block-format.md, "create_molecule": "the optional unit field
		// inside each scalar filler's value ... MUST resolve to an atom").
		other := NewMemStore()
		author2 := mustBuilder(t, 2)
		bad, err := author2.Public(1, nil, MustCreateBond(bond.Template()), op)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		other.MustAdd(bad)
		if _, err := Validate(bad, other, nil); !isRule(err, 4) {
			t.Errorf("Validate = %v, want a rule 4 violation for the unreachable unit atom", err)
		}

		// Reachable but the wrong kind: the unit digest names a bond, and a
		// unit is an atom. That is rule 5, the same rule a type 0 filler
		// pointing at a bond breaks.
		unitIsBond := entity.MustBond("_A_ is not a unit")
		scalarOnBond, err := entity.IntScalar(70).WithUnit(unitIsBond.Digest())
		if err != nil {
			t.Fatalf("WithUnit: %v", err)
		}
		fillerOnBond, err := entity.ScalarFiller(scalarOnBond)
		if err != nil {
			t.Fatalf("ScalarFiller: %v", err)
		}
		opOnBond, err := NewCreateMoleculeFor(bond, []entity.Filler{fillerOnBond})
		if err != nil {
			t.Fatalf("NewCreateMoleculeFor: %v", err)
		}
		third := NewMemStore()
		author3 := mustBuilder(t, 3)
		wrongKind, err := author3.Public(1, nil,
			MustCreateBond(bond.Template()), MustCreateBond(unitIsBond.Template()), opOnBond)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		third.MustAdd(wrongKind)
		if _, err := Validate(wrongKind, third, nil); !isRule(err, 5) {
			t.Errorf("Validate = %v, want a rule 5 violation: a unit digest must resolve to an atom", err)
		}
	})
}

// TestPublicBlockMustNotReferencePrivate covers rule 6.
func TestPublicBlockMustNotReferencePrivate(t *testing.T) {
	store := NewMemStore()
	alice := mustBuilder(t, 1)
	private, err := alice.Private(testCiphertext("alice"), make([]byte, NonceSize))
	if err != nil {
		t.Fatalf("private: %v", err)
	}
	bob := mustBuilder(t, 2)
	public, err := bob.Public(1, []cid.Digest{private.Digest()}, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("public: %v", err)
	}
	store.MustAdd(private, public)
	if _, err := Validate(public, store, nil); !isRule(err, 6) {
		t.Errorf("Validate = %v, want a rule 6 violation", err)
	}
}

// TestUncheckedRefsAreInformational is rules 6 and 10's binding scope
// (spec/02-block-format.md, "Validation" rule 6, and "A verdict moves in one
// direction"): an entry the source does not hold leaves both rules unchecked,
// the block is valid, and the entry is outside that verdict for good.
func TestUncheckedRefsAreInformational(t *testing.T) {
	store := NewMemStore()
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
	store.MustAdd(b) // Alice's block is not held yet

	// The verdict is *valid*, not a reservation: an unchecked entry means the
	// node has not found the block unsound, which is no reason to withhold it.
	// Rule 4's third outcome is the asymmetric one — there the node cannot show
	// the block sound — and it is why this block reaches L2 and that one
	// does not.
	report := mustValidate(t, b, store, nil)
	if !slices.Equal(report.UncheckedRefs, []cid.Digest{secret.Digest()}) {
		t.Errorf("UncheckedRefs = %v, want the entry the source does not hold", report.UncheckedRefs)
	}
	unchecked := false
	for _, w := range report.Warnings {
		if w.Rule == 6 && strings.Contains(w.Msg, secret.Digest().String()) {
			unchecked = true
		}
	}
	if !unchecked {
		t.Errorf("warnings = %v, want one naming the entry rules 6 and 10 could not be checked against", report.Warnings)
	}

	// The node later obtains Alice's block — while validating something else,
	// or because it subscribed to her chain — and it turns out to be private.
	// The verdict Bob's block already has is not re-opened: rules 6 and 10 bind
	// for the entries a validation resolved, and this entry was not among them,
	// so the caller keeps what it accepted and nothing has to be undone in an
	// append-only L2. This package validates against a source and records no
	// verdicts, so honouring that is the caller's: what it needs is
	// UncheckedRefs above, which names exactly what its verdict did not cover.
	store.MustAdd(secret)
	if _, err := Validate(b, store, nil); !isRule(err, 6) {
		t.Errorf("Validate over the grown store = %v, want the rule 6 finding a node that resolves the entry now makes", err)
	}
}

// TestRefsHygiene covers rule 10 in both halves: a refs list names each
// dependency once, and never names a block of the author's own chain
// (spec/02-block-format.md, "The refs list").
func TestRefsHygiene(t *testing.T) {
	alice := mustBuilder(t, 1)
	provider, err := alice.Public(1, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	t.Run("duplicate entries", func(t *testing.T) {
		// The duplicate half needs no other block, so it is refused at the two
		// points that see the bytes alone: the author's constructor and the
		// decoder.
		d := provider.Digest()
		bob := mustBuilder(t, 2)
		if _, err := bob.Public(2, []cid.Digest{d, d}, MustCreateAtom("Paris, the capital of France")); err == nil {
			t.Error("a block listing the same dependency twice must not be signed")
		}

		m := validPublicMap(t, 2)
		for i, e := range m {
			if e.Key == keyRefs {
				m[i].Value = dcbor.Array{dcbor.Bytes(d.Bytes()), dcbor.Bytes(d.Bytes())}
			}
		}
		if b, err := Decode(rawBlock(t, testKey(t, 2), m)); err == nil {
			t.Errorf("Decode accepted %s, whose refs repeat an entry", b)
		}
	})

	t.Run("own-chain reference", func(t *testing.T) {
		// A block referencing its own predecessor: already reachable through
		// prev, so the reference is degenerate and rule 10 rejects it. The
		// author's key is what gives it away — every block of a chain carries
		// the same pub.
		store := NewMemStore()
		second, err := alice.Public(2, []cid.Digest{provider.Digest()}, MustCreateAtom("Paris, the capital of France"))
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		store.MustAdd(provider, second)
		if _, err := Validate(second, store, nil); !isRule(err, 10) {
			t.Errorf("Validate = %v, want a rule 10 violation for a reference into the author's own chain", err)
		}
	})

	t.Run("another author's block", func(t *testing.T) {
		store := NewMemStore()
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, []cid.Digest{provider.Digest()}, MustCreateAtom("Paris, the capital of France"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(provider, b)
		mustValidate(t, b, store, nil)
	})

	t.Run("unheld reference is unchecked, not rejected", func(t *testing.T) {
		// Resolution is demand-driven: an entry whose block the source does not
		// hold leaves rules 6 and 10 unevaluated rather than failing the block,
		// as long as nothing needed it to resolve a digest.
		store := NewMemStore()
		bob := mustBuilder(t, 2)
		b, err := bob.Public(2, []cid.Digest{{9: 9}}, MustCreateAtom("Paris, the capital of France"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		store.MustAdd(b)
		report := mustValidate(t, b, store, nil)
		found := false
		for _, w := range report.Warnings {
			if w.Rule == 6 || w.Rule == 10 {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one saying the referenced block could not be checked", report.Warnings)
		}
	})
}

// TestPrivateBlockValidation covers what a node without the decryption key can
// and cannot check (spec/02-block-format.md: rules 4, 5 and 6 need the key).
func TestPrivateBlockValidation(t *testing.T) {
	store := NewMemStore()
	author := mustBuilder(t, 1)
	genesis, err := author.Private(testCiphertext("one"), make([]byte, NonceSize))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	second, err := author.Private(testCiphertext("two"), make([]byte, NonceSize))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	store.MustAdd(genesis, second)

	report := mustValidate(t, second, store, nil)
	if !slices.Equal(report.Unchecked, []int{4, 5, 6, 10}) {
		t.Errorf("report.Unchecked = %v, want rules 4, 5, 6 and 10", report.Unchecked)
	}
	chain, err := ValidateChain(second.Digest(), store, nil)
	if err != nil {
		t.Fatalf("ValidateChain: %v", err)
	}
	if chain.Len() != 2 {
		t.Errorf("private chain length = %d, want 2", chain.Len())
	}
}

// TestNonMonotonicTimestampWarns covers the ts SHOULD: a block whose timestamp
// goes backwards is valid and noticed (spec/02-block-format.md, the ts field).
func TestNonMonotonicTimestampWarns(t *testing.T) {
	store := NewMemStore()
	author := mustBuilder(t, 1)
	genesis, err := author.Public(5000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	second, err := author.Public(1000, nil, MustCreateAtom("Paris, the capital of France"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	store.MustAdd(genesis, second)

	report := mustValidate(t, second, store, nil)
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w.Msg, "timestamp") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one about the timestamp going backwards", report.Warnings)
	}
}

// TestRotation covers the rotation block's own rules and the succession to the
// new key's chain (spec/02-block-format.md, "rotate_key").
func TestRotation(t *testing.T) {
	store := NewMemStore()
	oldKey := mustBuilder(t, 1)
	newPub := testPub(t, 2)

	genesis, err := oldKey.Public(1000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	rotation, err := oldKey.Rotation(2000, nil, newPub)
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	store.MustAdd(genesis, rotation)

	successor := mustBuilder(t, 2)
	if err := successor.Succeeds(rotation); err != nil {
		t.Fatalf("Succeeds: %v", err)
	}
	newGenesis, err := successor.Public(3000, nil, MustCreateAtom("Paris, the capital of France"))
	if err != nil {
		t.Fatalf("new genesis: %v", err)
	}
	store.MustAdd(newGenesis)

	t.Run("happy path", func(t *testing.T) {
		chains, err := ValidateHistory([]cid.Digest{rotation.Digest(), newGenesis.Digest()}, store, nil)
		if err != nil {
			t.Fatalf("ValidateHistory: %v", err)
		}
		if len(chains) != 2 {
			t.Fatalf("got %d chains, want 2", len(chains))
		}
		pub, ok := chains[0].NextPublicKey()
		if !ok || !pub.Equal(newPub) {
			t.Errorf("NextPublicKey = %x, %v, want %x", pub, ok, newPub)
		}
		for _, w := range chains[1].Report.Warnings {
			t.Errorf("unexpected warning on a well-linked succession: %s", w)
		}
		// Rule 6 excludes private targets only, so the mandatory reference from
		// a public genesis block to a rotation block is legal by construction
		// (spec/02-block-format.md, "Validation" rule 6).
		mustValidate(t, newGenesis, store, nil)
		// The rotation block itself survives the wire: prev is a digest, not
		// null, and that is the shape the decoder accepts.
		if _, err := Decode(rotation.Bytes()); err != nil {
			t.Errorf("Decode of a well-formed rotation block: %v", err)
		}
	})

	t.Run("appending after a rotation block", func(t *testing.T) {
		if _, err := oldKey.Public(4000, nil, MustCreateAtom("too late")); err == nil {
			t.Error("the builder must refuse to append after its rotation block")
		}
		// An author who bypasses the builder is caught by rule 3.
		prev := rotation.Digest()
		late, err := Sign(Content{Version: Version, Type: TypePublic, Prev: &prev, TS: 4000, Ops: []Operation{MustCreateAtom("too late")}}, testKey(t, 1))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := Validate(late, store, nil); !isRule(err, 3) {
			t.Errorf("Validate = %v, want a rule 3 violation", err)
		}
	})

	t.Run("successor genesis without the rotation reference", func(t *testing.T) {
		// The back-reference is a MUST: a chain whose genesis block omits it is
		// a valid chain, but it is not the successor of that rotation
		// (spec/02-block-format.md, "Verifiable succession").
		unlinked := mustBuilder(t, 3)
		g, err := unlinked.Public(3000, nil, MustCreateAtom("a chain of its own"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		// A chain has to exist before it can be handed over, so the rotation
		// block sits behind a genesis block of its own.
		rotating := mustBuilder(t, 1)
		if _, err := rotating.Public(1000, nil, MustCreateAtom("France")); err != nil {
			t.Fatalf("genesis: %v", err)
		}
		rot, err := rotating.Rotation(2000, nil, testPub(t, 3))
		if err != nil {
			t.Fatalf("rotation: %v", err)
		}
		if _, err := ValidateSuccession(rot, g); err == nil {
			t.Error("a successor chain whose genesis block does not reference the rotation block must be rejected")
		}
		// The block itself is untouched by this: it validates as the genesis
		// block of an unrelated author's chain.
		store := NewMemStore()
		store.MustAdd(g)
		mustValidate(t, g, store, nil)
	})

	t.Run("private successor genesis", func(t *testing.T) {
		// The genesis block of a successor chain MUST be a public block: every
		// node is asked to act on its reference to the rotation block, and a
		// private block's refs are inside enc, so a node without the decryption
		// key would be acting on evidence it cannot read
		// (spec/02-block-format.md, "Verifiable succession").
		hidden := mustBuilder(t, 2)
		if err := hidden.Succeeds(rotation); err != nil {
			t.Fatalf("Succeeds: %v", err)
		}
		if _, err := hidden.Private(testCiphertext("successor"), make([]byte, NonceSize)); err == nil {
			t.Error("the builder must refuse to open a successor chain with a private genesis block")
		}
		// An author who bypasses the builder is caught at validation.
		privateGenesis, err := Sign(Content{
			Version: Version, Type: TypePrivate,
			Enc: testCiphertext("successor"), Nonce: make([]byte, NonceSize),
		}, testKey(t, 2))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := ValidateSuccession(rotation, privateGenesis); err == nil {
			t.Error("a private block must not be accepted as the genesis block of a successor chain")
		} else if !strings.Contains(err.Error(), "private") {
			t.Errorf("ValidateSuccession = %v, want an error naming the block's type", err)
		}
		// The block itself is a perfectly good private genesis block; what it
		// cannot be is the successor of a rotation.
		own := NewMemStore()
		own.MustAdd(privateGenesis)
		mustValidate(t, privateGenesis, own, nil)
	})

	t.Run("a rotation block is never a genesis block", func(t *testing.T) {
		// A rotation block ends a chain, and ending presupposes a chain to end:
		// its prev MUST NOT be null (spec/02-block-format.md, "Rotation
		// block"). This is what settles the case of a rotation block in the
		// genesis position of a successor chain — an author who wants to
		// abandon a key immediately publishes its genesis block first.
		fresh := mustBuilder(t, 8)
		if _, err := fresh.Rotation(3000, nil, testPub(t, 9)); err == nil {
			t.Error("the builder must refuse to sign a rotation block as the first block of a chain")
		}
		if _, err := Sign(Content{
			Version: Version, Type: TypeRotation, TS: 3000,
			Ops: []Operation{MustRotateKey(testPub(t, 9))},
		}, testKey(t, 8)); err == nil {
			t.Error("Sign accepted a rotation block with a null prev")
		}
		// The same is true in the successor position, which is how issue #45
		// is closed: the type list of "Verifiable succession" turns away only
		// the private case, because no rotation block can reach the genesis
		// position at all.
		immediate := mustBuilder(t, 2)
		if err := immediate.Succeeds(rotation); err != nil {
			t.Fatalf("Succeeds: %v", err)
		}
		if _, err := immediate.Rotation(3000, nil, testPub(t, 8)); err == nil {
			t.Error("the builder must refuse to open a successor chain with a rotation genesis block")
		}
		// A rotation block that is not a genesis block is not a claimant to the
		// successor position either: Successors counts public genesis blocks,
		// and ValidateSuccession refuses anything whose prev is not null.
		prev := newGenesis.Digest()
		later, err := Sign(Content{
			Version: Version, Type: TypeRotation, Prev: &prev, TS: 3100,
			Refs: []cid.Digest{rotation.Digest()},
			Ops:  []Operation{MustRotateKey(testPub(t, 8))},
		}, testKey(t, 2))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := ValidateSuccession(rotation, later); err == nil {
			t.Error("a rotation block must not be accepted as the genesis block of a successor chain")
		}
		claimed := NewMemStore()
		claimed.MustAdd(genesis, rotation, newGenesis, later)
		successors, fork, err := Successors(rotation, claimed)
		if err != nil {
			t.Fatalf("Successors: %v", err)
		}
		if len(successors) != 1 || fork != nil {
			t.Errorf("Successors = %v, fork %v, want only the public genesis block", successors, fork)
		}
	})

	t.Run("rotation to the same key", func(t *testing.T) {
		// new_pub MUST NOT equal pub (spec/02-block-format.md, "Rotation
		// block"), so no such block can be built or decoded.
		if _, err := mustBuilder(t, 7).Rotation(1000, nil, testPub(t, 7)); err == nil {
			t.Error("a rotation block naming its own key must be rejected")
		}
	})

	t.Run("two chains claiming one rotation", func(t *testing.T) {
		// Only one chain can succeed a rotation. A second genesis block
		// referencing the same rotation block makes the succession ambiguous:
		// the conflict is surfaced with every claimant named, and no successor
		// is picked (spec/02-block-format.md, "Verifiable succession";
		// spec/05-processing-model.md, "Chain succession (key rotation)").
		rival := mustBuilder(t, 2)
		if err := rival.Succeeds(rotation); err != nil {
			t.Fatalf("Succeeds: %v", err)
		}
		other, err := rival.Public(3001, nil, MustCreateAtom("a rival successor"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		forked := NewMemStore()
		forked.MustAdd(genesis, rotation, newGenesis)
		var forkErr *ForkError
		if err := forked.Add(other); !errors.As(err, &forkErr) {
			t.Fatalf("storing a second successor genesis block = %v, want a *ForkError", err)
		}

		successors, fork, err := Successors(rotation, forked)
		if err != nil {
			t.Fatalf("Successors: %v", err)
		}
		if len(successors) != 2 || fork == nil {
			t.Fatalf("Successors = %v, fork %v, want both genesis blocks and a fork", successors, fork)
		}
		if !fork.Pub.Equal(newPub) || len(fork.Blocks) != 2 {
			t.Errorf("fork = %+v, want the successor key's two genesis blocks", fork)
		}

		// ValidateHistory refuses the junction rather than affirming the one
		// the caller named: validating that succession would be picking a
		// successor on the caller's behalf. The error names every claimant, so
		// the caller has the conflict to surface and no winner to read out of
		// it.
		var ambiguous *AmbiguousSuccessionError
		if _, err := ValidateHistory([]cid.Digest{rotation.Digest(), newGenesis.Digest()}, forked, nil); !errors.As(err, &ambiguous) {
			t.Fatalf("ValidateHistory over an ambiguous succession = %v, want an *AmbiguousSuccessionError", err)
		}
		if ambiguous.Rotation != rotation.Digest() {
			t.Errorf("the error names rotation block %s, want %s", ambiguous.Rotation, rotation.Digest())
		}
		if !slices.Contains(ambiguous.Successors, newGenesis.Digest()) || !slices.Contains(ambiguous.Successors, other.Digest()) {
			t.Errorf("the error names %v, want both claimants", ambiguous.Successors)
		}
		// The other order is refused just the same: neither claimant is the
		// one the ambiguity resolves to.
		if _, err := ValidateHistory([]cid.Digest{rotation.Digest(), other.Digest()}, forked, nil); !errors.As(err, &ambiguous) {
			t.Errorf("ValidateHistory over the rival claimant = %v, want an *AmbiguousSuccessionError", err)
		}
		// Each chain is still valid on its own; what is unavailable is the
		// junction between them.
		if _, err := ValidateChain(newGenesis.Digest(), forked, nil); err != nil {
			t.Errorf("the successor chain must still validate on its own: %v", err)
		}

		// A source that cannot read the refs graph backwards says so.
		if _, _, err := Successors(rotation, plainSource{forked}); err == nil {
			t.Error("Successors must report that a source without Referrers cannot answer")
		}
	})

	t.Run("successor genesis signed by the wrong key", func(t *testing.T) {
		stranger := mustBuilder(t, 4)
		g, err := stranger.Public(3000, nil, MustCreateAtom("not the successor"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if _, err := ValidateSuccession(rotation, g); err == nil {
			t.Error("a genesis block signed by a key the rotation does not name must be rejected")
		}
	})

	t.Run("successor block that is not a genesis block", func(t *testing.T) {
		second, err := successor.Public(4000, nil, MustCreateAtom("second block"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if _, err := ValidateSuccession(rotation, second); err == nil {
			t.Error("only a genesis block may begin the successor chain")
		}
	})

	t.Run("succeeding a block that is not a rotation block", func(t *testing.T) {
		if err := mustBuilder(t, 5).Succeeds(genesis); err == nil {
			t.Error("Succeeds must reject a block that is not a rotation block")
		}
		if _, err := ValidateSuccession(genesis, newGenesis); err == nil {
			t.Error("ValidateSuccession must reject a block that is not a rotation block")
		}
	})

	t.Run("succeeding a rotation that names another key", func(t *testing.T) {
		if err := mustBuilder(t, 6).Succeeds(rotation); err == nil {
			t.Error("Succeeds must reject a rotation block naming a different key")
		}
	})
}

// TestStoredButUnvalidated covers the inductive definition of validity: a
// block whose ancestry a node does not hold is neither valid nor invalid but
// stored but unvalidated, and it becomes valid — without anything about the
// block changing — as soon as the missing ancestor arrives
// (spec/02-block-format.md, "Validation"; spec/05-processing-model.md, "Block
// reception").
func TestStoredButUnvalidated(t *testing.T) {
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

	// The block's own bytes are beyond reproach: it decodes, so it is
	// structurally valid and correctly signed. Only its ancestry is missing.
	store := NewMemStore()
	store.MustAdd(second, third)
	if _, err := Decode(third.Bytes()); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// The immediate predecessor is held but was never validatable itself, and
	// the tip's own rule 3 check is a lookup, so the gap surfaces where the
	// induction actually breaks: at the block whose predecessor is absent.
	if _, err := Validate(second, store, nil); !isRule(err, 3) || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Validate(second) = %v, want a rule 3 violation wrapping ErrNotFound", err)
	}
	if _, err := ValidateChain(third.Digest(), store, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ValidateChain = %v, want an error wrapping ErrNotFound: the chain is not anchored to a genesis block", err)
	}

	// The missing ancestor arrives; nothing else changes; the chain validates.
	store.MustAdd(genesis)
	chain, err := ValidateChain(third.Digest(), store, nil)
	if err != nil {
		t.Fatalf("ValidateChain after the ancestor arrived: %v", err)
	}
	if chain.Len() != 3 || chain.Genesis().Digest() != genesis.Digest() {
		t.Errorf("chain = %s, want the three blocks from the genesis block to the tip", chain)
	}
}

// TestDefinitionFromAnUndecidedBlockResolves pins the ratified reading of rule
// 4's three branches: they name blocks the node holds and can read, never valid
// blocks (spec/05-processing-model.md, "Resolution procedure", "Resolution
// reads blocks, not verdicts"; spec/02-block-format.md, "Validation" rule 4).
//
// Alice's second block defines a bond. Its predecessor never arrives, so
// Alice's block is stored but unvalidated and stays that way. Bob names it in
// refs and resolves the bond through it, and Bob's block is valid: a definition
// is self-certifying — the bond's digest is SHA-256 over the bond's own
// canonical bytes, which this package recomputes — so Alice's chain standing
// cannot change which entity the digest names.
func TestDefinitionFromAnUndecidedBlockResolves(t *testing.T) {
	bondTemplate := "_A_ is the capital of _B_"
	bond := entity.MustBond(bondTemplate)
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	alice := mustBuilder(t, 1)
	undelivered, err := alice.Public(1, nil, MustCreateAtom("a block nobody sent"))
	if err != nil {
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

	store := NewMemStore()
	store.MustAdd(provider, b) // Alice's genesis block is not delivered

	// Alice's block is undecided, and names the block whose arrival would
	// settle it.
	_, err = Validate(provider, store, nil)
	if !isRule(err, 3) || !IsUnvalidated(err) {
		t.Fatalf("Validate(provider) = %v, want an undecided rule 3 verdict", err)
	}
	if d, ok := Awaiting(err); !ok || d != undelivered.Digest() {
		t.Errorf("Awaiting = %s, %v; want the undelivered predecessor %s", d, ok, undelivered.Digest())
	}

	// Bob's block is valid all the same, and the definition cost the one scan a
	// held block costs.
	report := mustValidate(t, b, store, nil)
	if report.Scanned != 1 {
		t.Errorf("scanned %d foreign block(s), want 1", report.Scanned)
	}

	// And it stays that way: the source block's verdict is still undecided, so
	// nothing about Bob's block rested on Alice's chain being intact.
	if _, err := Validate(provider, store, nil); !IsUnvalidated(err) {
		t.Errorf("Validate(provider) after the fact = %v, want the same undecided verdict", err)
	}
}

// TestValidateChainRejectsIncompleteChain checks that a chain missing a block
// is an ErrNotFound rather than a silent truncation.
func TestValidateChainRejectsIncompleteChain(t *testing.T) {
	store := NewMemStore()
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	second, err := author.Public(2, nil, MustCreateAtom("Paris, the capital of France"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	store.MustAdd(second) // the genesis block is missing
	_ = genesis
	if _, err := ValidateChain(second.Digest(), store, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("ValidateChain = %v, want an error wrapping ErrNotFound", err)
	}
}

// TestValidateWithoutSiblingsWarns checks that a Source which cannot answer
// the fork question says so rather than passing rule 9 silently.
func TestValidateWithoutSiblingsWarns(t *testing.T) {
	author := mustBuilder(t, 1)
	b, err := author.Public(1, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	report := mustValidate(t, b, plainSource{NewMemStore()}, nil)
	for _, w := range report.Warnings {
		if w.Rule == 9 {
			return
		}
	}
	t.Errorf("warnings = %v, want one for rule 9", report.Warnings)
}

// testDecrypter is a caller that happens to hold keys: it answers with the
// payload it was given for a block, and ok false — "no key here" — for any
// other, which is exactly the shape block.Decrypter asks for.
type testDecrypter map[cid.Digest]Payload

func (d testDecrypter) DecryptPayload(b *Block) (Payload, bool, error) {
	p, ok := d[b.Digest()]
	if !ok {
		return Payload{}, false, nil
	}
	return p.Clone(), true, nil
}

// TestUndecryptableReferenceIsUndecided is the readability cause of *stored but
// unvalidated* (spec/05-processing-model.md, "Undecryptable reference
// handling"): a block resolution needs, holds, and cannot read is the same
// verdict as one that never arrived. Validity is a property of the blocks — the
// same block is decidable for a key holder — so a key this node was not given
// is a capability it lacks and not evidence that another author's block is
// wrong.
func TestUndecryptableReferenceIsUndecided(t *testing.T) {
	bondTemplate := "_A_ is the capital of _B_"
	bond := entity.MustBond(bondTemplate)
	paris := entity.MustAtom("Paris, the capital of France")
	france := entity.MustAtom("France")
	fillers := []entity.Filler{entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest())}

	t.Run("a private refs target the node holds no key for", func(t *testing.T) {
		// Alice's private block defines the bond. Bob is a recipient of his own
		// chain's key and not of Alice's, so he holds her block as ciphertext:
		// rule 6 does not apply — a private block's refs MAY name a block of any
		// type — and the question is only whether he can read what he needs.
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Private(testCiphertext("alice"), make([]byte, NonceSize))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		providerPayload := Payload{TS: 1, Ops: []Operation{MustCreateBond(bondTemplate)}}

		bob := mustBuilder(t, 2)
		b, err := bob.Private(testCiphertext("bob"), make([]byte, NonceSize))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		payload := Payload{
			Refs: []cid.Digest{provider.Digest()},
			TS:   2,
			Ops: []Operation{
				MustCreateAtom(paris.Description()),
				MustCreateAtom(france.Description()),
				MustCreateMolecule(bond, fillers),
			},
		}
		store.MustAdd(provider, b)

		// Bob's own block is readable to him — Validate leaves rules 4, 5, 6 and
		// 10 to the key holder, and this is the key holder's pass.
		if report := mustValidate(t, b, store, nil); len(report.Unchecked) != 4 {
			t.Errorf("Unchecked = %v, want rules 4, 5, 6 and 10", report.Unchecked)
		}

		_, err = ValidatePayload(b, payload, store, nil)
		if !isRule(err, 4) || !IsUnvalidated(err) {
			t.Fatalf("ValidatePayload = %v, want a rule 4 error the node has not decided", err)
		}
		if !errors.Is(err, ErrUndecryptable) {
			t.Errorf("ValidatePayload = %v, want ErrUndecryptable: the block is held, and what is missing is a key", err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Errorf("ValidatePayload = %v, want the key cause and not the missing-block one: the source holds the block", err)
		}
		// The situation is surfaced, not swallowed: the caller is told which
		// block it could not read, since obtaining that key is the fix.
		if !strings.Contains(err.Error(), provider.Digest().String()) {
			t.Errorf("ValidatePayload = %v, want the undecryptable block named in the error", err)
		}
		// The verdict is a function of what the node can read and nothing else.
		if _, again := ValidatePayload(b, payload, store, nil); again.Error() != err.Error() {
			t.Errorf("a second ValidatePayload over the same store = %v, want the same verdict as %v", again, err)
		}

		// Alice wraps her content key for Bob. Nothing about either block
		// changes, and the block that was undecided is valid: the same question
		// a node without the key could not answer.
		opts := &Options{Decrypter: testDecrypter{provider.Digest(): providerPayload}}
		if report, err := ValidatePayload(b, payload, store, opts); err != nil {
			t.Errorf("ValidatePayload with the key = %v, want acceptance", err)
		} else if report.Scanned != 1 {
			t.Errorf("scanned %d foreign block(s), want 1", report.Scanned)
		}
	})

	t.Run("a private ancestor of the author's own chain", func(t *testing.T) {
		// A chain may mix block types. A node that holds the chain but not the
		// key to one of its earlier blocks cannot read what that block defined,
		// and the same undecided verdict follows — this time for a public block.
		store := NewMemStore()
		author := mustBuilder(t, 1)
		genesis, err := author.Private(testCiphertext("genesis"), make([]byte, NonceSize))
		if err != nil {
			t.Fatalf("genesis: %v", err)
		}
		genesisPayload := Payload{TS: 1, Ops: []Operation{MustCreateBond(bondTemplate)}}
		tip, err := author.Public(2, nil,
			MustCreateAtom(paris.Description()),
			MustCreateAtom(france.Description()),
			MustCreateMolecule(bond, fillers))
		if err != nil {
			t.Fatalf("tip: %v", err)
		}
		store.MustAdd(genesis, tip)

		_, err = Validate(tip, store, nil)
		if !isRule(err, 4) || !errors.Is(err, ErrUndecryptable) || !IsUnvalidated(err) {
			t.Fatalf("Validate = %v, want a rule 4 error wrapping ErrUndecryptable", err)
		}
		mustValidate(t, tip, store, &Options{Decrypter: testDecrypter{genesis.Digest(): genesisPayload}})
	})

	t.Run("an unreadable block no digest needs", func(t *testing.T) {
		// Outcome 3 is reached only when the unreadable block could have
		// mattered. Every digest here resolves inside the block, so the private
		// entry is scanned, contributes nothing, and decides nothing.
		store := NewMemStore()
		alice := mustBuilder(t, 1)
		provider, err := alice.Private(testCiphertext("alice"), make([]byte, NonceSize))
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		bob := mustBuilder(t, 2)
		b, err := bob.Private(testCiphertext("bob"), make([]byte, NonceSize))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		payload := Payload{
			Refs: []cid.Digest{provider.Digest()},
			TS:   2,
			Ops: []Operation{
				MustCreateBond(bondTemplate),
				MustCreateAtom(paris.Description()),
				MustCreateAtom(france.Description()),
				MustCreateMolecule(bond, fillers),
			},
		}
		store.MustAdd(provider, b)
		if _, err := ValidatePayload(b, payload, store, nil); err != nil {
			t.Errorf("ValidatePayload = %v, want acceptance: no digest needed the unreadable block", err)
		}
	})
}

// plainSource hides a store's Siblings implementation.
type plainSource struct{ inner Source }

func (p plainSource) Block(d cid.Digest) (*Block, error) { return p.inner.Block(d) }
