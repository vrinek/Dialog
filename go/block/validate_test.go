package block

import (
	"crypto/ed25519"
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
		// is invalid (spec/05-processing-model.md, "Scan limit").
		_, err = Validate(b, store, &Options{ScanLimit: 1})
		if !isRule(err, 4) || !errors.Is(err, ErrScanLimit) {
			t.Errorf("Validate with a scan limit of 1 = %v, want a rule 4 violation wrapping ErrScanLimit", err)
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
		if _, err := Validate(b, store, nil); !isRule(err, 4) {
			t.Errorf("Validate = %v, want a rule 4 violation", err)
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
		if _, err := Validate(b, store, nil); !isRule(err, 4) {
			t.Errorf("Validate = %v, want a rule 4 violation: prev is not a resolution path and refs is empty", err)
		}
	})

	t.Run("referenced block absent from the source", func(t *testing.T) {
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
		if _, err := Validate(b, store, nil); !isRule(err, 4) {
			t.Errorf("Validate = %v, want a rule 4 violation", err)
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
		if !ok || !ed25519.PublicKey(pub).Equal(newPub) {
			t.Errorf("NextPublicKey = %x, %v, want %x", pub, ok, newPub)
		}
		for _, w := range chains[1].Report.Warnings {
			t.Errorf("unexpected warning on a well-linked succession: %s", w)
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
		rot, err := mustBuilder(t, 1).Rotation(2000, nil, testPub(t, 3))
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

	t.Run("rotation to the same key", func(t *testing.T) {
		// new_pub MUST NOT equal pub (spec/02-block-format.md, "Rotation
		// block"), so no such block can be built or decoded.
		if _, err := mustBuilder(t, 7).Rotation(1000, nil, testPub(t, 7)); err == nil {
			t.Error("a rotation block naming its own key must be rejected")
		}
	})

	t.Run("two chains claiming one rotation", func(t *testing.T) {
		// Only one chain can succeed a rotation. A second genesis block
		// referencing the same rotation block makes the succession ambiguous,
		// which is surfaced the way rule 9's forks are: reported, not resolved.
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
		if !ed25519.PublicKey(fork.Pub).Equal(newPub) || len(fork.Blocks) != 2 {
			t.Errorf("fork = %+v, want the successor key's two genesis blocks", fork)
		}

		// ValidateHistory surfaces it in the successor chain's report rather
		// than choosing between the two.
		chains, err := ValidateHistory([]cid.Digest{rotation.Digest(), newGenesis.Digest()}, forked, nil)
		if err != nil {
			t.Fatalf("ValidateHistory: %v", err)
		}
		if len(chains[1].Report.Forks) == 0 {
			t.Error("the ambiguous succession is not in the successor chain's report")
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

// plainSource hides a store's Siblings implementation.
type plainSource struct{ inner Source }

func (p plainSource) Block(d cid.Digest) (*Block, error) { return p.inner.Block(d) }
