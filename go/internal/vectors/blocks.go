package vectors

import (
	"bytes"
	"crypto/ed25519"
	"fmt"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// The scenario's fixed inputs. Every key is derived from a seed of 32 equal
// bytes, and every timestamp is a constant, so the whole chain — signatures
// included — is reproducible. Ed25519 signing is deterministic (RFC 8032), so
// no randomness enters anywhere in this file.
const (
	seedAlice     = 0x01
	seedBob       = 0x02
	seedSuccessor = 0x03

	tsGenesis   = 1740067200 // 2025-02-20T16:00:00Z, the timestamp of the spec's examples
	tsSecond    = 1740067260
	tsBob       = 1740067320
	tsRotation  = 1740067380
	tsSuccessor = 1740067440
	tsFork      = 1740067261
)

// The block rules a vector may violate, named as spec/02-block-format.md
// numbers them.
const (
	ruleVersion     = "spec/02-block-format.md, Validation rule 1 (version check)"
	ruleSignature   = "spec/02-block-format.md, Validation rule 2 (signature check)"
	ruleNonEmptyOps = "spec/02-block-format.md, Validation rule 7 (non-empty operations)"
	ruleEncoding    = "spec/02-block-format.md, Validation rule 8 (deterministic encoding, including the closed-map rule)"
	ruleForks       = "spec/02-block-format.md, Validation rule 9 (fork detection)"
	ruleRefHygiene  = "spec/02-block-format.md, Validation rule 10 (reference hygiene)"
	ruleDispatch    = "spec/02-block-format.md, Validation dispatch"
	ruleRotation    = "spec/02-block-format.md, Rotation block"
	ruleFillerTypes = "spec/01-data-model.md, Filler types"
	ruleInternalRef = "spec/03-encoding.md, Internal references"
)

func seedKey(seed byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func seedPub(seed byte) ed25519.PublicKey {
	pub, ok := seedKey(seed).Public().(ed25519.PublicKey)
	if !ok { // unreachable for a key of the right size
		panic("vectors: seed key does not yield an Ed25519 public key")
	}
	return pub
}

func keyCase(name string, seed byte) KeyCase {
	priv := seedKey(seed)
	return KeyCase{
		Name:       name,
		Seed:       hexOf(priv.Seed()),
		PrivateKey: hexOf(priv),
		PublicKey:  hexOf(seedPub(seed)),
	}
}

func blocksFile() (File, error) {
	doc, err := blocksDocument()
	if err != nil {
		return File{}, err
	}
	return File{Name: "blocks.json", Doc: doc}, nil
}

func blocksDocument() (Document, error) {
	chain, fork, err := scenario()
	if err != nil {
		return Document{}, err
	}
	invalid, err := invalidBlockCases()
	if err != nil {
		return Document{}, err
	}
	return Document{
		Vectors: Format,
		Area:    "blocks",
		Description: "A complete, deterministic chain scenario — genesis, an appended block, a foreign-reference block by a second author, and a rotation with its successor genesis — plus a fork and the blocks a conforming implementation MUST reject. " +
			"Ed25519 signing is deterministic, so every signature here is reproducible from the seeds in inputs.",
		Spec:   []string{"spec/02-block-format.md", "spec/03-encoding.md", "spec/04-cryptography.md"},
		Inputs: blockInputs(),
		Sections: []Section{
			{
				Name:        "chain",
				Description: "The scenario, in the order the blocks are published. A consumer that stores them in order and validates each one MUST accept all five; the digests link them, so any encoding difference shows up as a broken prev.",
				Cases:       chain,
			},
			{
				Name:        "forks",
				Description: "Two blocks of one chain sharing a prev. Detection is normative; what a node does about it is not.",
				Cases:       []ForkCase{fork.summary},
			},
			{
				Name:        "fork_block",
				Description: "The forking block in full. It is a well-formed, correctly signed block: what makes it a fork is the chain it arrives into, not anything inside it.",
				Cases:       []BlockCase{fork.block},
			},
			{
				Name:        "invalid",
				Description: "Blocks a conforming implementation MUST reject, each naming the rule it violates. Every one of them is canonical dCBOR and — except where the case is about the signature — correctly signed, so the rejection can only come from the named rule.",
				Cases:       invalid,
			},
		},
	}, nil
}

func blockInputs() BlockInputs {
	return BlockInputs{
		Note: "Every key comes from a seed of 32 equal bytes. These are test keys with published private material and MUST NOT be used for anything but conformance testing.",
		Keys: []KeyCase{
			keyCase("alice", seedAlice),
			keyCase("bob", seedBob),
			keyCase("alice_successor", seedSuccessor),
		},
	}
}

// blockCase renders one signed block.
func blockCase(name, description, author string, b *block.Block) (BlockCase, error) {
	var prev *string
	if d, ok := b.Prev(); ok {
		s := d.String()
		prev = &s
	}
	refs := []string{}
	for _, r := range b.Refs() {
		refs = append(refs, r.String())
	}
	full, err := dcbor.Decode(b.Bytes())
	if err != nil {
		return BlockCase{}, fmt.Errorf("vectors: block %s does not decode: %w", name, err)
	}
	var enc, nonce string
	if e, ok := b.Enc(); ok {
		enc = hexOf(e)
	}
	if n, ok := b.Nonce(); ok {
		nonce = hexOf(n)
	}
	return BlockCase{
		Name:         name,
		Description:  description,
		Author:       author,
		Type:         string(b.Type()),
		Prev:         prev,
		Refs:         refs,
		TS:           b.TS(),
		Enc:          enc,
		Nonce:        nonce,
		Value:        describe(full),
		SigningBytes: hexOf(b.SigningBytes()),
		SigningInput: hexOf(b.SigningInput()),
		Signature:    hexOf(b.Signature()),
		Block:        hexOf(b.Bytes()),
		Digest:       b.Digest().String(),
		CID:          b.CID().HexString(),
		CIDText:      b.CID().String(),
	}, nil
}

// forkResult carries the forking block and the summary that names it.
type forkResult struct {
	block   BlockCase
	summary ForkCase
}

// scenario builds and validates the chain. Validation is not decoration: a
// vector set that pins an invalid chain would teach every implementation that
// reads it the wrong thing, so the generator refuses to emit one.
func scenario() ([]BlockCase, forkResult, error) {
	france := entity.MustAtom(franceDescription)
	paris := entity.MustAtom(parisDescription)
	capital := entity.MustBond(capitalTemplate)
	eiffel := entity.MustAtom(eiffelDescription)
	metre := entity.MustAtom(metreDescription)
	height := entity.MustBond(heightTemplate)
	parisFrance := entity.MustAtom(parisFranceDescr)

	alice, err := block.NewBuilder(seedKey(seedAlice))
	if err != nil {
		return nil, forkResult{}, err
	}

	// 1. Genesis: the four entity operations of the specification's examples.
	// The molecule comes last because its bond and both its fillers are
	// defined by earlier operations in the same block — the first branch of
	// rule 4's reachability.
	genesis, err := alice.Public(tsGenesis, nil,
		block.MustCreateAtom(franceDescription),
		block.MustCreateAtom(parisDescription),
		block.MustCreateBond(capitalTemplate),
		block.MustCreateMolecule(capital, []entity.Filler{
			entity.AtomFiller(paris.Digest()),
			entity.AtomFiller(france.Digest()),
		}),
	)
	if err != nil {
		return nil, forkResult{}, fmt.Errorf("vectors: genesis: %w", err)
	}

	// 2. An appended block: prev links it to the genesis block, and its
	// molecule carries a scalar filler whose unit digest is an atom the same
	// block defines.
	unitScalar, err := entity.IntScalar(scalarNumberValue).WithUnit(metre.Digest())
	if err != nil {
		return nil, forkResult{}, err
	}
	scalarFiller, err := entity.ScalarFiller(unitScalar)
	if err != nil {
		return nil, forkResult{}, err
	}
	second, err := alice.Public(tsSecond, nil,
		block.MustCreateAtom(eiffelDescription),
		block.MustCreateAtom(metreDescription),
		block.MustCreateBond(heightTemplate),
		block.MustCreateMolecule(height, []entity.Filler{
			entity.AtomFiller(eiffel.Digest()),
			scalarFiller,
		}),
	)
	if err != nil {
		return nil, forkResult{}, fmt.Errorf("vectors: second block: %w", err)
	}

	// 3. A second author, referencing the first author's genesis block. The
	// bond and one filler are defined there; the other filler is defined by
	// an earlier operation of this same block.
	bob, err := block.NewBuilder(seedKey(seedBob))
	if err != nil {
		return nil, forkResult{}, err
	}
	foreign, err := bob.Public(tsBob, []cid.Digest{genesis.Digest()},
		block.MustCreateAtom(parisFranceDescr),
		block.MustCreateMolecule(capital, []entity.Filler{
			entity.AtomFiller(parisFrance.Digest()),
			entity.AtomFiller(france.Digest()),
		}),
	)
	if err != nil {
		return nil, forkResult{}, fmt.Errorf("vectors: foreign-reference block: %w", err)
	}

	// 4. The rotation block that ends Alice's chain, naming her next key.
	rotation, err := alice.Rotation(tsRotation, nil, seedPub(seedSuccessor))
	if err != nil {
		return nil, forkResult{}, fmt.Errorf("vectors: rotation block: %w", err)
	}

	// 5. The successor chain's genesis block, which must be public and must
	// name the rotation block in refs (spec/02-block-format.md, "Verifiable
	// succession").
	successor, err := block.NewBuilder(seedKey(seedSuccessor))
	if err != nil {
		return nil, forkResult{}, err
	}
	if err := successor.Succeeds(rotation); err != nil {
		return nil, forkResult{}, err
	}
	successorGenesis, err := successor.Public(tsSuccessor, nil, block.MustCreateAtom("Lyon"))
	if err != nil {
		return nil, forkResult{}, fmt.Errorf("vectors: successor genesis: %w", err)
	}

	// The fork: a second block claiming the same predecessor as block 2,
	// signed by the same key. The Builder will not produce one — it advances
	// its own tip — so it is signed directly.
	prev := genesis.Digest()
	forkBlock, err := block.Sign(block.Content{
		Version: block.Version,
		Type:    block.TypePublic,
		Pub:     seedPub(seedAlice),
		Prev:    &prev,
		TS:      tsFork,
		Ops:     []block.Operation{block.MustCreateAtom("Marseille")},
	}, seedKey(seedAlice))
	if err != nil {
		return nil, forkResult{}, fmt.Errorf("vectors: fork block: %w", err)
	}

	if err := validateScenario(genesis, second, foreign, rotation, successorGenesis); err != nil {
		return nil, forkResult{}, err
	}

	descriptions := []struct{ name, description, author string }{
		{"alice_genesis", "Alice's genesis block: prev is null, and the four operations define two atoms, a bond and the molecule that uses them. The molecule's references resolve inside this same block.", "alice"},
		{"alice_second", "The next block of Alice's chain: prev is the digest of alice_genesis. Its molecule carries a scalar filler with a unit, whose digest is the metre atom this block defines.", "alice"},
		{"bob_foreign_reference", "Bob's genesis block, referencing Alice's genesis block in refs. Its molecule uses a bond and an atom defined there and an atom defined by its own first operation.", "bob"},
		{"alice_rotation", "The rotation block that ends Alice's chain. Exactly one rotate_key operation, prev not null, and new_pub naming a different key.", "alice"},
		{"alice_successor_genesis", "The successor chain's genesis block. It is public and names the rotation block in refs, which is what makes the succession verifiable to a node with no keys.", "alice_successor"},
	}
	blocks := []*block.Block{genesis, second, foreign, rotation, successorGenesis}
	cases := make([]BlockCase, 0, len(blocks))
	for i, b := range blocks {
		c, err := blockCase(descriptions[i].name, descriptions[i].description, descriptions[i].author, b)
		if err != nil {
			return nil, forkResult{}, err
		}
		cases = append(cases, c)
	}

	forkCase, err := blockCase("alice_fork", "A second block claiming alice_genesis as its predecessor. It is valid in isolation; against a store that already holds alice_second it is a fork.", "alice", forkBlock)
	if err != nil {
		return nil, forkResult{}, err
	}
	return cases, forkResult{
		block: forkCase,
		summary: ForkCase{
			Name:        "alice_second_vs_alice_fork",
			Description: "Two blocks signed by Alice carry the same prev. A node that holds both MUST detect the fork; whether it rejects, flags or keeps the first is its own policy.",
			Rule:        ruleForks,
			Author:      "alice",
			Prev:        genesis.Digest().String(),
			Blocks:      []string{cases[1].Digest, forkCase.Digest},
		},
	}, nil
}

// validateScenario runs the full validation the scenario claims to satisfy.
func validateScenario(blocks ...*block.Block) error {
	store := block.NewMemStore()
	for _, b := range blocks {
		if err := store.Add(b); err != nil {
			return fmt.Errorf("vectors: storing %s: %w", b, err)
		}
		report, err := block.Validate(b, store, nil)
		if err != nil {
			return fmt.Errorf("vectors: the scenario block %s does not validate: %w", b, err)
		}
		if len(report.Forks) != 0 {
			return fmt.Errorf("vectors: the scenario forked at %s", b)
		}
	}
	return nil
}

// blockMap builds a block map by hand, bypassing the constructors so that a
// vector can violate one rule on purpose. Entry order is irrelevant:
// dcbor.Encode sorts the keys.
func blockMap(typ block.Type, pub ed25519.PublicKey, prev *cid.Digest, refs []cid.Digest, ts uint64, ops []dcbor.Value) dcbor.Map {
	refList := dcbor.Array{}
	for _, r := range refs {
		refList = append(refList, dcbor.Bytes(r.Bytes()))
	}
	opList := dcbor.Array{}
	for _, op := range ops {
		opList = append(opList, op)
	}
	prevValue := dcbor.Value(dcbor.Null)
	if prev != nil {
		prevValue = dcbor.Bytes(prev.Bytes())
	}
	return dcbor.Map{
		{Key: "v", Value: dcbor.Uint(block.Version)},
		{Key: "type", Value: dcbor.Text(string(typ))},
		{Key: "pub", Value: dcbor.Bytes(pub)},
		{Key: "prev", Value: prevValue},
		{Key: "refs", Value: refList},
		{Key: "ts", Value: dcbor.Uint(ts)},
		{Key: "ops", Value: opList},
	}
}

// signRaw signs a hand-built block map with priv and returns the hex of the
// complete block. The signature covers exactly what a conforming author would
// sign — the map without "sig", behind the domain separator — so a vector
// built this way fails on its own rule and not on rule 2.
func signRaw(priv ed25519.PrivateKey, m dcbor.Map) (string, error) {
	signing, err := dcbor.Encode(m)
	if err != nil {
		return "", fmt.Errorf("vectors: encoding a hand-built block: %w", err)
	}
	sig := ed25519.Sign(priv, append([]byte(block.DomainSeparator), signing...))
	full := append(dcbor.Map{}, m...)
	full = append(full, dcbor.MapEntry{Key: "sig", Value: dcbor.Bytes(sig)})
	encoded, err := dcbor.Encode(full)
	if err != nil {
		return "", fmt.Errorf("vectors: encoding a hand-built block: %w", err)
	}
	return hexOf(encoded), nil
}

// without returns m with the named key removed.
func without(m dcbor.Map, key string) dcbor.Map {
	out := dcbor.Map{}
	for _, e := range m {
		if e.Key != key {
			out = append(out, e)
		}
	}
	return out
}

// with returns m with an entry added or replaced.
func with(m dcbor.Map, key string, v dcbor.Value) dcbor.Map {
	out := without(m, key)
	return append(out, dcbor.MapEntry{Key: key, Value: v})
}

func invalidBlockCases() ([]InvalidCase, error) {
	alice, aliceKey := seedPub(seedAlice), seedKey(seedAlice)
	prev := entity.MustAtom("a previous block").Digest() // any 32 bytes; the cases below never resolve it
	franceOp := block.MustCreateAtom(franceDescription).Value()
	valid := blockMap(block.TypePublic, alice, nil, nil, tsGenesis, []dcbor.Value{franceOp})

	// A correctly signed genesis block, used as the base for the mutations and
	// as the source of the tampered signature.
	genesis, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePublic, Pub: alice,
		TS: tsGenesis, Ops: []block.Operation{block.MustCreateAtom(franceDescription)},
	}, aliceKey)
	if err != nil {
		return nil, err
	}
	tampered := genesis.Bytes()
	// Flip a bit of the signature. The block stays canonical dCBOR and every
	// other rule still holds, so rule 2 is the only thing that can reject it.
	sigStart := bytes.Index(tampered, []byte{0x58, 0x40})
	if sigStart < 0 {
		return nil, fmt.Errorf("vectors: no 64-byte byte string in the block encoding")
	}
	tampered[sigStart+2] ^= 0x01

	rotationOp := block.MustRotateKey(seedPub(seedSuccessor)).Value()
	privateBase := dcbor.Map{
		{Key: "v", Value: dcbor.Uint(block.Version)},
		{Key: "type", Value: dcbor.Text(string(block.TypePrivate))},
		{Key: "pub", Value: dcbor.Bytes(alice)},
		{Key: "prev", Value: dcbor.Null},
		{Key: "enc", Value: dcbor.Bytes(bytes.Repeat([]byte{0xaa}, 48))},
		{Key: "nonce", Value: dcbor.Bytes(bytes.Repeat([]byte{0x33}, block.NonceSize))},
	}

	type spec struct {
		name, rule, reason string
		m                  dcbor.Map
		priv               ed25519.PrivateKey
	}
	specs := []spec{
		{"unknown_version", ruleVersion, "The v field is 2. A field a later version introduces arrives in a block this version rejects on its version, never as an extra key.",
			with(valid, "v", dcbor.Uint(2)), aliceKey},
		{"unknown_block_type", ruleDispatch, "The type field is \"secret\". There are exactly three block types.",
			with(valid, "type", dcbor.Text("secret")), aliceKey},
		{"empty_ops", ruleNonEmptyOps, "The ops list is empty.",
			with(valid, "ops", dcbor.Array{}), aliceKey},
		{"unknown_block_key", ruleEncoding, "The block map carries a \"note\" key its definition does not declare. An unknown key is a rejection, never something to ignore: a decoder that ignored it would hash bytes it did not account for.",
			with(valid, "note", dcbor.Text("hello")), aliceKey},
		{"missing_block_key", ruleEncoding, "The block map omits the declared ts key.",
			without(valid, "ts"), aliceKey},
		{"public_block_with_enc", ruleDispatch, "A public block carrying an enc field, which belongs to a private block.",
			with(valid, "enc", dcbor.Bytes(bytes.Repeat([]byte{0xaa}, 48))), aliceKey},
		{"unknown_operation", ruleEncoding, "An operation whose op value is \"delete_atom\". There are exactly four operation types.",
			with(valid, "ops", dcbor.Array{dcbor.Map{
				{Key: "op", Value: dcbor.Text("delete_atom")},
				{Key: "description", Value: dcbor.Text(franceDescription)},
			}}), aliceKey},
		{"unknown_operation_key", ruleEncoding, "A create_atom operation carrying an extra \"lang\" key. The closed-map rule governs operation maps too.",
			with(valid, "ops", dcbor.Array{dcbor.Map{
				{Key: "op", Value: dcbor.Text(block.OpCreateAtom)},
				{Key: "description", Value: dcbor.Text(franceDescription)},
				{Key: "lang", Value: dcbor.Text("fr")},
			}}), aliceKey},
		{"rotate_key_in_public_block", ruleDispatch, "A rotate_key operation in a public block. It may appear only in a rotation block, so that a chain ends where the type field says it ends.",
			with(valid, "ops", dcbor.Array{rotationOp}), aliceKey},
		{"rotation_block_as_genesis", ruleRotation, "A rotation block with a null prev. A rotation block ends a chain, and there is no chain to end.",
			with(with(valid, "type", dcbor.Text(string(block.TypeRotation))), "ops", dcbor.Array{rotationOp}), aliceKey},
		{"rotation_block_two_operations", ruleRotation, "A rotation block carrying a second operation. It MUST contain exactly one rotate_key operation and no others.",
			with(with(with(valid, "type", dcbor.Text(string(block.TypeRotation))), "prev", dcbor.Bytes(prev.Bytes())), "ops", dcbor.Array{rotationOp, franceOp}), aliceKey},
		{"rotation_to_the_same_key", ruleRotation, "new_pub equals the block's own pub. A chain would end in favour of itself.",
			with(with(with(valid, "type", dcbor.Text(string(block.TypeRotation))), "prev", dcbor.Bytes(prev.Bytes())), "ops", dcbor.Array{dcbor.Map{
				{Key: "op", Value: dcbor.Text(block.OpRotateKey)},
				{Key: "new_pub", Value: dcbor.Bytes(alice)},
			}}), aliceKey},
		{"duplicate_refs", ruleRefHygiene, "The refs list names the same digest twice. The duplicate half of rule 10 is structural and is checked at decoding.",
			with(valid, "refs", dcbor.Array{dcbor.Bytes(prev.Bytes()), dcbor.Bytes(prev.Bytes())}), aliceKey},
		{"cid_in_prev", ruleInternalRef, "The prev field carries a 36-byte CIDv1 instead of the raw 32-byte digest. Internal references are digests; the CID appears only at the API boundary.",
			with(valid, "prev", dcbor.Bytes(prev.CID().Bytes())), aliceKey},
		{"filler_type_out_of_range", ruleFillerTypes, "A filler of type 5. The v1 vocabulary is types 0 to 4.",
			with(valid, "ops", dcbor.Array{dcbor.Map{
				{Key: "op", Value: dcbor.Text(block.OpCreateMolecule)},
				{Key: "bond", Value: dcbor.Bytes(prev.Bytes())},
				{Key: "fillers", Value: dcbor.Array{dcbor.Map{
					{Key: "type", Value: dcbor.Uint(5)},
					{Key: "value", Value: dcbor.Bytes(prev.Bytes())},
				}}},
			}}), aliceKey},
		{"filler_value_shape_mismatch", ruleFillerTypes, "A type 0 filler carrying text. Each type tag is bound to exactly one value shape.",
			with(valid, "ops", dcbor.Array{dcbor.Map{
				{Key: "op", Value: dcbor.Text(block.OpCreateMolecule)},
				{Key: "bond", Value: dcbor.Bytes(prev.Bytes())},
				{Key: "fillers", Value: dcbor.Array{dcbor.Map{
					{Key: "type", Value: dcbor.Uint(0)},
					{Key: "value", Value: dcbor.Text("France")},
				}}},
			}}), aliceKey},
		{"empty_fillers", ruleFillerTypes, "A create_molecule operation with an empty fillers list; the CDDL requires at least one.",
			with(valid, "ops", dcbor.Array{dcbor.Map{
				{Key: "op", Value: dcbor.Text(block.OpCreateMolecule)},
				{Key: "bond", Value: dcbor.Bytes(prev.Bytes())},
				{Key: "fillers", Value: dcbor.Array{}},
			}}), aliceKey},
		{"private_block_with_plaintext_ops", ruleDispatch, "A private block carrying a plaintext ops field. Its operations live inside enc.",
			with(privateBase, "ops", dcbor.Array{franceOp}), aliceKey},
		{"private_block_short_enc", "spec/02-block-format.md, Private block", "An enc field shorter than the 16-byte Poly1305 tag cannot be the output of the AEAD.",
			with(privateBase, "enc", dcbor.Bytes(bytes.Repeat([]byte{0xaa}, 8))), aliceKey},
		{"private_block_wrong_nonce_size", "spec/02-block-format.md, Private block", "A 12-byte nonce. XChaCha20 takes 24 bytes.",
			with(privateBase, "nonce", dcbor.Bytes(bytes.Repeat([]byte{0x33}, 12))), aliceKey},
		{"private_block_missing_nonce", ruleEncoding, "A private block with no nonce field.",
			without(privateBase, "nonce"), aliceKey},
		{"signed_by_another_key", ruleSignature, "A block claiming Alice's pub and signed by Bob's key.",
			valid, seedKey(seedBob)},
	}

	cases := []InvalidCase{{
		Name:   "tampered_signature",
		Rule:   ruleSignature,
		Reason: "A valid genesis block with one bit of its sig field flipped.",
		Bytes:  hexOf(tampered),
	}}
	for _, s := range specs {
		encoded, err := signRaw(s.priv, s.m)
		if err != nil {
			return nil, err
		}
		cases = append(cases, InvalidCase{Name: s.name, Rule: s.rule, Reason: s.reason, Bytes: encoded})
	}
	return cases, nil
}
