package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// The worked examples of spec/02-block-format.md and spec/04-cryptography.md.
//
// Neither document prints block bytes, digests or signatures: every key and
// signature in them is a placeholder ("<32 bytes: author's Ed25519 public
// key>", "<64 bytes: Ed25519 signature>"), and a block's identity depends on
// both. What is computable is therefore (a) the exact byte layout of the
// signing input and of the block, which these tests build by hand from the
// CBOR encoding reference of spec/03-encoding.md and compare against the
// package's output, and (b) the entity digests the example operations define,
// which spec/01-data-model.md does print. The signatures are produced with a
// deterministic test key, so the byte strings below are stable.
const (
	// spec/04-cryptography.md, "Signing a public block": the timestamp of the
	// example block, 1740067200 = 0x67b75180.
	specTS    = 1740067200
	specTSHex = "1a67b75180"

	// Encoded map keys and text values, from spec/03-encoding.md's "CBOR
	// encoding reference" table.
	hexKeyV           = "6176"
	hexKeyTS          = "627473"
	hexKeyOps         = "636f7073"
	hexKeyPub         = "63707562"
	hexKeySig         = "63736967"
	hexKeyPrev        = "6470726576"
	hexKeyRefs        = "6472656673"
	hexKeyType        = "6474797065"
	hexKeyOp          = "626f70"
	hexKeyDescription = "6b6465736372697074696f6e"
	hexPublic         = "667075626c6963"
	hexCreateAtom     = "6b6372656174655f61746f6d"
	hexFrance         = "664672616e6365"

	// spec/01-data-model.md, "Examples", and spec/03-encoding.md, "Encoding an
	// atom" — the digests the example operations define.
	specFranceDigest   = "e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842"
	specParisDigest    = "6545050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360965411fd"
	specCapitalDigest  = "f295b89289597b4486784ad03d0be8bdab09a0d20070a893afa4f4d307811340"
	specMoleculeDigest = "f9f124b06af6aa7d5f2381462afdeaca628fe3ac8b994253e5c08a3f5d128afb"

	specParisDescription = "Paris, the capital of France"
	specCapitalTemplate  = "_A_ is the capital of _B_"
)

// TestSpecSigningInputPublicBlock reproduces the worked example of
// spec/04-cryptography.md, "Signing a public block", byte for byte.
//
// The expected signing bytes are assembled here from the encoding rules rather
// than taken from the package, so the test checks the two things the
// specification actually fixes: which fields the signature covers (everything
// but "sig") and in which order they are encoded (bytewise lexicographic order
// of the encoded keys, which puts them v, ts, ops, pub, prev, refs, type).
func TestSpecSigningInputPublicBlock(t *testing.T) {
	priv := testKey(t, 1)
	pub, _ := priv.Public().(ed25519.PublicKey)

	c := Content{
		Version: Version,
		Type:    TypePublic,
		Pub:     pub,
		Prev:    nil, // genesis
		Refs:    nil, // the example's empty refs list
		TS:      specTS,
		Ops:     []Operation{MustCreateAtom("France")},
	}

	// a7 — a map of seven keys: the block's eight fields minus "sig".
	wantSigning := "a7" +
		hexKeyV + "01" +
		hexKeyTS + specTSHex +
		hexKeyOps + "81" + "a2" + hexKeyOp + hexCreateAtom + hexKeyDescription + hexFrance +
		hexKeyPub + "5820" + hex.EncodeToString(pub) +
		hexKeyPrev + "f6" +
		hexKeyRefs + "80" +
		hexKeyType + hexPublic

	signingBytes, err := c.SigningBytes()
	if err != nil {
		t.Fatalf("SigningBytes: %v", err)
	}
	if got := hex.EncodeToString(signingBytes); got != wantSigning {
		t.Errorf("signing bytes =\n%s\nwant\n%s", got, wantSigning)
	}

	input, err := c.SigningInput()
	if err != nil {
		t.Fatalf("SigningInput: %v", err)
	}
	if got, want := hex.EncodeToString(input), hex.EncodeToString([]byte(DomainSeparator))+wantSigning; got != want {
		t.Errorf("signing input =\n%s\nwant\n%s", got, want)
	}

	// Step 4 of the example: the complete block is the same fields plus "sig".
	// The specification prints a placeholder signature, so the value is this
	// key's, and what is checked is the layout and that it verifies.
	b, err := Sign(c, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	wantBlock := "a8" +
		hexKeyV + "01" +
		hexKeyTS + specTSHex +
		hexKeyOps + "81" + "a2" + hexKeyOp + hexCreateAtom + hexKeyDescription + hexFrance +
		hexKeyPub + "5820" + hex.EncodeToString(pub) +
		hexKeySig + "5840" + hex.EncodeToString(b.Signature()) +
		hexKeyPrev + "f6" +
		hexKeyRefs + "80" +
		hexKeyType + hexPublic
	if got := hex.EncodeToString(b.Bytes()); got != wantBlock {
		t.Errorf("block encoding =\n%s\nwant\n%s", got, wantBlock)
	}
	if !ed25519.Verify(pub, input, b.Signature()) {
		t.Error("the example's signature does not verify over the example's signing input")
	}
	// spec/02-block-format.md, "Block identification": the CID is computed
	// from the complete encoding, signature included.
	if got, want := b.Digest(), cid.Digest(sha256.Sum256(b.Bytes())); got != want {
		t.Errorf("block digest = %s, want %s", got, want)
	}
}

// TestSpecGenesisBlockFourOperations reproduces the example of
// spec/02-block-format.md, "Genesis block with four operations", and checks
// each of the three reasons the specification gives for its validity.
func TestSpecGenesisBlockFourOperations(t *testing.T) {
	bond := MustCreateBond(specCapitalTemplate)
	paris := MustCreateAtom(specParisDescription)
	france := MustCreateAtom("France")
	molecule := MustCreateMolecule(bond.Bond(), []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})

	// The digests the example's operations define are the ones
	// spec/01-data-model.md prints for the same entities.
	checkDigest(t, "atom Paris", mustDigest(paris), specParisDigest)
	checkDigest(t, "atom France", mustDigest(france), specFranceDigest)
	checkDigest(t, "bond", mustDigest(bond), specCapitalDigest)
	checkDigest(t, "molecule", mustDigest(molecule), specMoleculeDigest)

	author := mustBuilder(t, 1)
	b, err := author.Public(specTS, nil, paris, france, bond, molecule)
	if err != nil {
		t.Fatalf("building the example block: %v", err)
	}

	store := NewMemStore()
	store.MustAdd(b)
	report, err := Validate(b, store, nil)
	if err != nil {
		t.Fatalf("the example block must be valid: %v", err)
	}
	// "prev is null (genesis block)".
	if !b.IsGenesis() {
		t.Error("the example block must be a genesis block")
	}
	// "The refs list is empty (no foreign dependencies)" — so nothing was
	// fetched to validate it.
	if len(b.Refs()) != 0 || report.Scanned != 0 {
		t.Errorf("refs = %v and %d foreign block(s) scanned; the example needs neither", b.Refs(), report.Scanned)
	}
	// "The create_molecule operation references the bond and atoms created by
	// earlier operations in the same block": moving it first must break it.
	reordered := mustBuilder(t, 1)
	bad, err := reordered.Public(specTS, nil, molecule, paris, france, bond)
	if err != nil {
		t.Fatalf("building the reordered block: %v", err)
	}
	badStore := NewMemStore()
	badStore.MustAdd(bad)
	if _, err := Validate(bad, badStore, nil); !isRule(err, 4) {
		t.Errorf("a molecule placed before the operations it depends on = %v, want a rule 4 violation", err)
	}
}

// TestSpecBlockWithForeignReference reproduces the example of
// spec/02-block-format.md, "Block with foreign reference": author B's block
// uses a bond created in a specific block of author A's chain, and lists that
// block in refs.
func TestSpecBlockWithForeignReference(t *testing.T) {
	store := NewMemStore()

	alice := mustBuilder(t, 1)
	aliceBlock, err := alice.Public(1740067200, nil, MustCreateBond(specCapitalTemplate))
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	store.MustAdd(aliceBlock)

	bond := entity.MustBond(specCapitalTemplate)
	paris := MustCreateAtom(specParisDescription)
	france := MustCreateAtom("France")
	molecule := MustCreateMolecule(bond, []entity.Filler{
		entity.AtomFiller(paris.Atom().Digest()),
		entity.AtomFiller(france.Atom().Digest()),
	})

	bob := mustBuilder(t, 2)
	bobGenesis, err := bob.Public(1740153600, nil, MustCreateAtom("Bob's first entity"))
	if err != nil {
		t.Fatalf("bob genesis: %v", err)
	}
	store.MustAdd(bobGenesis)
	bobBlock, err := bob.Public(1740153600, []cid.Digest{aliceBlock.Digest()}, paris, france, molecule)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	store.MustAdd(bobBlock)

	report, err := Validate(bobBlock, store, nil)
	if err != nil {
		t.Fatalf("the example block must be valid: %v", err)
	}
	if report.Scanned != 1 {
		t.Errorf("scanned %d foreign block(s), want exactly 1 — the ref points straight at the CID provider", report.Scanned)
	}

	// "The ref points directly to the CID-providing block": a third author
	// publishing the same molecule without listing Alice's block cannot reach
	// the bond, even though the block sits in the same store.
	carol := mustBuilder(t, 3)
	carolGenesis, err := carol.Public(1740153600, nil, MustCreateAtom("Carol's first entity"))
	if err != nil {
		t.Fatalf("carol genesis: %v", err)
	}
	unreferenced, err := carol.Public(1740153600, nil, paris, france, molecule)
	if err != nil {
		t.Fatalf("carol: %v", err)
	}
	store.MustAdd(carolGenesis, unreferenced)
	if _, err := Validate(unreferenced, store, nil); !isRule(err, 4) {
		t.Errorf("a molecule whose bond is only in an unreferenced foreign chain = %v, want a rule 4 violation", err)
	}
}

func checkDigest(t *testing.T, what string, got cid.Digest, want string) {
	t.Helper()
	if got.String() != want {
		t.Errorf("%s digest = %s, want %s", what, got, want)
	}
}
