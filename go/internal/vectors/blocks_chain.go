package vectors

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// The `invalid` section of blocks.json is a decoder's business: every case
// there is refused by the bytes alone. This file builds the other half —
// rejections that depend on what the node already holds, which is where rules
// 3, 4, 5, 6, the own-chain half of rule 10 and the scan limit live.
//
// A case is a small store to replay and one block that MUST be rejected once it
// has been replayed. Each is verified here against the reference validator, and
// the setup blocks are verified valid, so the file can never ask another
// implementation for a verdict this one does not reach.

// Two further authors, so that a refs graph can be several hops deep: a block
// may not name one of its own chain (rule 10), so every hop crosses to another
// key.
const (
	seedCarol = 0x04
	seedDave  = 0x05
)

// The rules a chain-relative case may violate.
const (
	ruleChainIntegrity = "spec/02-block-format.md, Validation rule 3 (chain integrity)"
	ruleOperations     = "spec/02-block-format.md, Validation rule 4 (operation validity)"
	ruleDataModel      = "spec/02-block-format.md, Validation rule 5 (data model conformance)"
	ruleRefVisibility  = "spec/02-block-format.md, Validation rule 6 (public/private reference rules)"
	ruleScanLimit      = "spec/05-processing-model.md, Scan limit (rejected under Validation rule 4)"
)

// The timestamps of the chain-relative scenarios. They sit after the ones of
// the `chain` section so that no case here is confusable with a block of that
// scenario, and they are monotonic within each case.
const (
	tsSetup    = 1740067500
	tsSetupTwo = 1740067560
	tsRejected = 1740067620
)

// A chainRejection is one case before it is rendered: the blocks to replay, the
// block that must then be rejected, and the rule it breaks. ruleNum is the
// numbered rule the reference validator must name, which is what ties the
// case's prose label to a checked fact.
type chainRejection struct {
	name    string
	rule    string
	ruleNum int
	reason  string
	setup   []*block.Block
	bad     *block.Block
	// scanLimit is the limit a consumer must configure for the case to fail;
	// zero means the default, and the case does not depend on the limit.
	scanLimit int
}

func invalidInChainCases() ([]InvalidInChainCase, error) {
	builders := []func() (chainRejection, error){
		rejectPrevOfAnotherChain,
		rejectAppendAfterRotation,
		rejectOwnChainReference,
		rejectUnreachableBond,
		rejectUnreferencedChain,
		rejectUnreachableMetaBond,
		rejectForwardReference,
		rejectUnreachableScalarUnit,
		rejectScanLimitExceeded,
		rejectBondResolvingToAnAtom,
		rejectUnitResolvingToABond,
		rejectFillerCountMismatch,
		rejectPublicNamingPrivate,
	}
	cases := make([]InvalidInChainCase, 0, len(builders))
	for _, build := range builders {
		c, err := build()
		if err != nil {
			return nil, fmt.Errorf("vectors: building the %s case: %w", c.name, err)
		}
		if err := verifyChainRejection(c); err != nil {
			return nil, fmt.Errorf("vectors: the %s case: %w", c.name, err)
		}
		setup := make([]string, 0, len(c.setup))
		for _, b := range c.setup {
			setup = append(setup, hexOf(b.Bytes()))
		}
		cases = append(cases, InvalidInChainCase{
			Name:      c.name,
			Rule:      c.rule,
			Reason:    c.reason,
			Setup:     setup,
			Bytes:     hexOf(c.bad.Bytes()),
			ScanLimit: c.scanLimit,
		})
	}
	return cases, nil
}

// verifyChainRejection is the mustReject of this section. A case must be three
// things at once: every setup block valid, the rejected block well-formed
// enough to decode — otherwise it belongs in the `invalid` section — and the
// rejection the one the case names.
func verifyChainRejection(c chainRejection) error {
	store := block.NewMemStore()
	opts := &block.Options{ScanLimit: c.scanLimit}
	for _, b := range c.setup {
		if err := store.Add(b); err != nil {
			return fmt.Errorf("storing the setup block %s: %w", b, err)
		}
		if _, err := block.Validate(b, store, opts); err != nil {
			return fmt.Errorf("the setup block %s does not validate: %w", b, err)
		}
	}
	if _, err := block.Decode(c.bad.Bytes()); err != nil {
		return fmt.Errorf("the rejected block does not decode (%w); a case of this section is a rejection by the store, not by the decoder", err)
	}
	if c.scanLimit != 0 {
		// A scan-limit case is a rejection by configuration: under the default
		// limit the very same block and store are valid, which is what makes
		// the case about the limit and nothing else.
		if _, err := block.Validate(c.bad, store, nil); err != nil {
			return fmt.Errorf("the block is rejected under the default scan limit too (%w), so the case does not isolate the limit", err)
		}
	}
	_, err := block.Validate(c.bad, store, opts)
	var ruleErr *block.RuleError
	switch {
	case err == nil:
		return errors.New("the reference validator accepts the block; a vector may never pin a rejection this implementation does not make")
	case !errors.As(err, &ruleErr):
		return fmt.Errorf("the reference validator rejects the block with an error that is not a numbered rule violation: %w", err)
	case ruleErr.Rule != c.ruleNum:
		return fmt.Errorf("the reference validator rejects the block under rule %d, but the case names rule %d", ruleErr.Rule, c.ruleNum)
	}
	return nil
}

// author returns a Builder for one of the scenario's seeds.
func author(seed byte) (*block.Builder, error) {
	return block.NewBuilder(seedKey(seed))
}

// rejectPrevOfAnotherChain: rule 3 — every block of a chain carries the same
// pub, so a prev naming another author's block is not a chain link at all.
func rejectPrevOfAnotherChain() (chainRejection, error) {
	c := chainRejection{
		name:    "prev_of_another_chain",
		rule:    ruleChainIntegrity,
		ruleNum: 3,
		reason:  "The rejected block is signed by Bob and its prev names a block of Alice's chain. Within a single chain all blocks MUST carry the same pub, so this is not a link but a claim on someone else's history. The block decodes and its signature verifies; only the store can refuse it.",
	}
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	genesis, err := alice.Public(tsSetup, nil, block.MustCreateAtom(franceDescription))
	if err != nil {
		return c, err
	}
	prev := genesis.Digest()
	bad, err := block.Sign(block.Content{
		Version: block.Version,
		Type:    block.TypePublic,
		Pub:     seedPub(seedBob),
		Prev:    &prev,
		TS:      tsRejected,
		Ops:     []block.Operation{block.MustCreateAtom(parisFranceDescr)},
	}, seedKey(seedBob))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{genesis}, bad
	return c, nil
}

// rejectAppendAfterRotation: rule 3 — a rotation block ends its key's chain,
// and no further block signed by that key may be accepted.
func rejectAppendAfterRotation() (chainRejection, error) {
	c := chainRejection{
		name:    "appended_after_rotation",
		rule:    ruleChainIntegrity,
		ruleNum: 3,
		reason:  "The rejected block is appended by Alice's key to the rotation block that ended her chain. The rotation block marks the key inactive: implementations MUST NOT accept a further block signed by it, whatever that block contains (spec/02-block-format.md, \"rotate_key\").",
	}
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	genesis, err := alice.Public(tsSetup, nil, block.MustCreateAtom(franceDescription))
	if err != nil {
		return c, err
	}
	rotation, err := alice.Rotation(tsSetupTwo, nil, seedPub(seedSuccessor))
	if err != nil {
		return c, err
	}
	prev := rotation.Digest()
	// The Builder refuses to sign after a rotation, which is the author-side
	// half of the same rule, so the block is signed directly.
	bad, err := block.Sign(block.Content{
		Version: block.Version,
		Type:    block.TypePublic,
		Pub:     seedPub(seedAlice),
		Prev:    &prev,
		TS:      tsRejected,
		Ops:     []block.Operation{block.MustCreateAtom(parisDescription)},
	}, seedKey(seedAlice))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{genesis, rotation}, bad
	return c, nil
}

// rejectOwnChainReference: rule 10's own-chain half — the author's ancestry is
// already a resolution path and MUST NOT be listed in refs.
func rejectOwnChainReference() (chainRejection, error) {
	c := chainRejection{
		name:    "refs_names_own_chain",
		rule:    ruleRefHygiene,
		ruleNum: 10,
		reason:  "The rejected block lists its own predecessor in refs. Every block of a chain carries the same pub, so the check is a comparison of the referenced block's pub with the referencing block's; it needs the referenced block, which is why the duplicate half of rule 10 is a decoder's business and this half is not.",
	}
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	genesis, err := alice.Public(tsSetup, nil, block.MustCreateBond(capitalTemplate))
	if err != nil {
		return c, err
	}
	bad, err := alice.Public(tsRejected, []cid.Digest{genesis.Digest()}, block.MustCreateAtom(franceDescription))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{genesis}, bad
	return c, nil
}

// rejectUnreachableBond: rule 4 — a bond digest no block anywhere defines.
func rejectUnreachableBond() (chainRejection, error) {
	c := chainRejection{
		name:    "unreachable_bond",
		rule:    ruleOperations,
		ruleNum: 4,
		reason:  "The create_molecule names a bond that no block defines: not this one, not an ancestor — it is a genesis block — and not through refs, which is empty. The store holds another author's block, and holding blocks is not reachability.",
	}
	capital := entity.MustBond(capitalTemplate)
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	setup, err := alice.Public(tsSetup, nil, block.MustCreateAtom(eiffelDescription))
	if err != nil {
		return c, err
	}
	molecule, err := block.NewCreateMolecule(capital.Digest(), []entity.Filler{
		entity.AtomFiller(entity.MustAtom(parisDescription).Digest()),
		entity.AtomFiller(entity.MustAtom(franceDescription).Digest()),
	})
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, nil,
		block.MustCreateAtom(parisDescription),
		block.MustCreateAtom(franceDescription),
		molecule)
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{setup}, bad
	return c, nil
}

// rejectUnreferencedChain: rule 4 — the digest is defined in a block the node
// holds, and the block does not name it in refs.
func rejectUnreferencedChain() (chainRejection, error) {
	c := chainRejection{
		name:    "bond_defined_in_an_unreferenced_chain",
		rule:    ruleOperations,
		ruleNum: 4,
		reason:  "The bond exists and the node holds the block that created it, but the rejected block's refs is empty. Reachability is a property of the block, not of the node's store: prev is not a resolution path into another author's chain, and an entity a node happens to know is not a dependency the block declared.",
	}
	capital := entity.MustBond(capitalTemplate)
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	provider, err := alice.Public(tsSetup, nil, block.MustCreateBond(capitalTemplate))
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, nil,
		block.MustCreateAtom(parisDescription),
		block.MustCreateAtom(franceDescription),
		block.MustCreateMolecule(capital, []entity.Filler{
			entity.AtomFiller(entity.MustAtom(parisDescription).Digest()),
			entity.AtomFiller(entity.MustAtom(franceDescription).Digest()),
		}))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{provider}, bad
	return c, nil
}

// rejectUnreachableMetaBond: rule 4 in the one position an implementation is
// tempted to treat as ambient — the digest of a standard meta-bond.
func rejectUnreachableMetaBond() (chainRejection, error) {
	c := chainRejection{
		name:    "unreachable_meta_bond",
		rule:    ruleOperations,
		ruleNum: 4,
		reason:  "The create_molecule names the standard meta-bond \"_A_ is true\", which nothing in this chain ever created: the block publishes no create_bond for it, its ancestor published the molecule being asserted and not the meta-bond, and refs is empty. The standard meta-bonds are not implicitly present in any chain and no digest is exempt from rule 4 (spec/06-meta-bonds.md, \"Meta-molecules are regular molecules\"), so an implementation that recognizes the five meta-bond digests and lets them resolve accepts this block. Its valid counterpart is the chain block bob_meta_molecule, which is this block plus the create_bond.",
	}
	capital := entity.MustBond(capitalTemplate)
	fillers := []entity.Filler{
		entity.AtomFiller(entity.MustAtom(parisDescription).Digest()),
		entity.AtomFiller(entity.MustAtom(franceDescription).Digest()),
	}
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	setup, err := alice.Public(tsSetup, nil,
		block.MustCreateAtom(parisDescription),
		block.MustCreateAtom(franceDescription),
		block.MustCreateBond(capitalTemplate),
		block.MustCreateMolecule(capital, fillers))
	if err != nil {
		return c, err
	}
	bad, err := alice.Public(tsRejected, nil,
		block.MustCreateMolecule(entity.MetaBondTruthAssertion, []entity.Filler{
			entity.MoleculeFiller(entity.MustMolecule(capital, fillers).Digest()),
		}))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{setup}, bad
	return c, nil
}

// rejectForwardReference: rule 4 — within a block, an operation may reference
// only what an *earlier* operation created.
func rejectForwardReference() (chainRejection, error) {
	c := chainRejection{
		name:    "forward_reference_in_same_block",
		rule:    ruleOperations,
		ruleNum: 4,
		reason:  "The create_molecule comes first and the operations that define its bond and its fillers come after it. Operations are ordered and the order is the evaluation order: an operation may reference what an earlier operation created, never what a later one will. Setup is empty — the store cannot help a block that is wrong about itself.",
	}
	capital := entity.MustBond(capitalTemplate)
	molecule := block.MustCreateMolecule(capital, []entity.Filler{
		entity.AtomFiller(entity.MustAtom(parisDescription).Digest()),
		entity.AtomFiller(entity.MustAtom(franceDescription).Digest()),
	})
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	bad, err := alice.Public(tsRejected, nil,
		molecule,
		block.MustCreateBond(capitalTemplate),
		block.MustCreateAtom(parisDescription),
		block.MustCreateAtom(franceDescription))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{}, bad
	return c, nil
}

// rejectUnreachableScalarUnit: rule 4 in the position an implementation is most
// likely to exempt — "There is no exempt position".
func rejectUnreachableScalarUnit() (chainRejection, error) {
	c := chainRejection{
		name:    "unreachable_scalar_unit",
		rule:    ruleOperations,
		ruleNum: 4,
		reason:  "Every digest in the block resolves except the unit inside a scalar filler, whose atom is defined in a chain the block does not reference. The unit is an internal reference like any other and rule 4 covers it: \"There is no exempt position — every digest an operation carries is subject to this rule.\"",
	}
	metre := entity.MustAtom(metreDescription)
	height := entity.MustBond(heightTemplate)
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	provider, err := alice.Public(tsSetup, nil, block.MustCreateAtom(metreDescription))
	if err != nil {
		return c, err
	}
	scalar, err := entity.IntScalar(scalarNumberValue).WithUnit(metre.Digest())
	if err != nil {
		return c, err
	}
	filler, err := entity.ScalarFiller(scalar)
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, nil,
		block.MustCreateBond(heightTemplate),
		block.MustCreateAtom(eiffelDescription),
		block.MustCreateMolecule(height, []entity.Filler{
			entity.AtomFiller(entity.MustAtom(eiffelDescription).Digest()),
			filler,
		}))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{provider}, bad
	return c, nil
}

// rejectScanLimitExceeded: the bound on rule 4's recursion. The block is valid
// under the default limit and invalid under the one the case names, which is
// the whole point of writing the counting unit down.
func rejectScanLimitExceeded() (chainRejection, error) {
	c := chainRejection{
		name:      "scan_limit_exceeded",
		rule:      ruleScanLimit,
		ruleNum:   4,
		scanLimit: 2,
		reason:    "The bond resolves three foreign blocks deep: the rejected block names Alice's block, which names Dave's, which names Carol's, where the bond was created. Validate it with the scan_limit of this case, 2, and resolution must stop before the third block and treat the block as invalid for unresolvable references. Under the default limit of 256 the same block against the same store is valid — that is what the case isolates. One unit is one distinct foreign block scanned, so an implementation counting digests or recursion levels will disagree here.",
	}
	capital := entity.MustBond(capitalTemplate)
	carol, err := author(seedCarol)
	if err != nil {
		return c, err
	}
	first, err := carol.Public(tsSetup, nil, block.MustCreateBond(capitalTemplate))
	if err != nil {
		return c, err
	}
	dave, err := author(seedDave)
	if err != nil {
		return c, err
	}
	second, err := dave.Public(tsSetup, []cid.Digest{first.Digest()}, block.MustCreateAtom(eiffelDescription))
	if err != nil {
		return c, err
	}
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	third, err := alice.Public(tsSetupTwo, []cid.Digest{second.Digest()}, block.MustCreateAtom(metreDescription))
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, []cid.Digest{third.Digest()},
		block.MustCreateAtom(parisDescription),
		block.MustCreateAtom(franceDescription),
		block.MustCreateMolecule(capital, []entity.Filler{
			entity.AtomFiller(entity.MustAtom(parisDescription).Digest()),
			entity.AtomFiller(entity.MustAtom(franceDescription).Digest()),
		}))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{first, second, third}, bad
	return c, nil
}

// rejectBondResolvingToAnAtom: rule 5 — each digest resolves to an entity of
// the kind its position names.
func rejectBondResolvingToAnAtom() (chainRejection, error) {
	c := chainRejection{
		name:    "bond_resolves_to_an_atom",
		rule:    ruleDataModel,
		ruleNum: 5,
		reason:  "The create_molecule's bond field names a digest that resolves — through refs, to Alice's block — to an atom. Reachability alone is satisfied, which is why this is rule 5 and not rule 4: the data model binds each position to a kind, and a molecule built on an atom is not a statement.",
	}
	france := entity.MustAtom(franceDescription)
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	provider, err := alice.Public(tsSetup, nil, block.MustCreateAtom(franceDescription))
	if err != nil {
		return c, err
	}
	molecule, err := block.NewCreateMolecule(france.Digest(), []entity.Filler{
		entity.AtomFiller(entity.MustAtom(parisFranceDescr).Digest()),
	})
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, []cid.Digest{provider.Digest()},
		block.MustCreateAtom(parisFranceDescr),
		molecule)
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{provider}, bad
	return c, nil
}

// rejectUnitResolvingToABond: rule 5 in the scalar filler's unit position.
func rejectUnitResolvingToABond() (chainRejection, error) {
	c := chainRejection{
		name:    "unit_resolves_to_a_bond",
		rule:    ruleDataModel,
		ruleNum: 5,
		reason:  "The unit inside a scalar filler resolves, through refs, to a bond. A unit MUST resolve to an atom: it names what the number is measured in. The digest is reachable, so an implementation that checks rule 4 on the unit position and stops there accepts this block.",
	}
	capital := entity.MustBond(capitalTemplate)
	height := entity.MustBond(heightTemplate)
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	provider, err := alice.Public(tsSetup, nil, block.MustCreateBond(capitalTemplate))
	if err != nil {
		return c, err
	}
	scalar, err := entity.IntScalar(scalarNumberValue).WithUnit(capital.Digest())
	if err != nil {
		return c, err
	}
	filler, err := entity.ScalarFiller(scalar)
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, []cid.Digest{provider.Digest()},
		block.MustCreateBond(heightTemplate),
		block.MustCreateAtom(eiffelDescription),
		block.MustCreateMolecule(height, []entity.Filler{
			entity.AtomFiller(entity.MustAtom(eiffelDescription).Digest()),
			filler,
		}))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{provider}, bad
	return c, nil
}

// rejectFillerCountMismatch: rule 5 — the filler count against a template only
// the refs graph can supply.
func rejectFillerCountMismatch() (chainRejection, error) {
	c := chainRejection{
		name:    "filler_count_mismatch",
		rule:    ruleDataModel,
		ruleNum: 5,
		reason:  "The molecule carries one filler and its bond's template, defined in the block Alice published and this block references, has two variables. The count cannot be checked without resolving the bond, which is why a decoder accepts these bytes and a node does not.",
	}
	capital := entity.MustBond(capitalTemplate)
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	provider, err := alice.Public(tsSetup, nil, block.MustCreateBond(capitalTemplate))
	if err != nil {
		return c, err
	}
	molecule, err := block.NewCreateMolecule(capital.Digest(), []entity.Filler{
		entity.AtomFiller(entity.MustAtom(parisFranceDescr).Digest()),
	})
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, []cid.Digest{provider.Digest()},
		block.MustCreateAtom(parisFranceDescr),
		molecule)
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{provider}, bad
	return c, nil
}

// rejectPublicNamingPrivate: rule 6 — a public block's refs MUST NOT name a
// private block.
func rejectPublicNamingPrivate() (chainRejection, error) {
	c := chainRejection{
		name:    "public_block_references_a_private_block",
		rule:    ruleRefVisibility,
		ruleNum: 6,
		reason:  "The rejected block is public and its refs name a private block of Alice's chain. Nothing about the bytes says so — the type is inside the referenced block — so the rejection arrives only once the node holds it. The setup's private block carries a fixed opaque ciphertext: no node without the key reads it, which is precisely the reason for the rule.",
	}
	alice, err := author(seedAlice)
	if err != nil {
		return c, err
	}
	genesis, err := alice.Public(tsSetup, nil, block.MustCreateAtom(franceDescription))
	if err != nil {
		return c, err
	}
	private, err := alice.Private(bytes.Repeat([]byte{0xaa}, 48), bytes.Repeat([]byte{0x33}, block.NonceSize))
	if err != nil {
		return c, err
	}
	bob, err := author(seedBob)
	if err != nil {
		return c, err
	}
	bad, err := bob.Public(tsRejected, []cid.Digest{private.Digest()}, block.MustCreateAtom(parisFranceDescr))
	if err != nil {
		return c, err
	}
	c.setup, c.bad = []*block.Block{genesis, private}, bad
	return c, nil
}
