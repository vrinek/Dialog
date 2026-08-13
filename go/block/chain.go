package block

import (
	"crypto/ed25519"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// A Chain is one key's validated author chain: the linear sequence of blocks
// from the genesis block to the tip, all signed by the same key
// (spec/02-block-format.md, "Chain linking").
type Chain struct {
	// Pub is the key that signed every block of the chain.
	Pub ed25519.PublicKey
	// Blocks are the chain's blocks, genesis first and tip last.
	Blocks []*Block
	// Report holds the warnings and forks found while validating the chain.
	Report *Report
}

// Genesis returns the first block of the chain.
func (c *Chain) Genesis() *Block { return c.Blocks[0] }

// Tip returns the last block of the chain.
func (c *Chain) Tip() *Block { return c.Blocks[len(c.Blocks)-1] }

// Len returns the number of blocks in the chain.
func (c *Chain) Len() int { return len(c.Blocks) }

// Rotation returns the rotation block that ended the chain. ok is false for a
// chain that is still open — one whose tip is not a rotation block.
func (c *Chain) Rotation() (b *Block, ok bool) {
	tip := c.Tip()
	if tip.content.Type != TypeRotation {
		return nil, false
	}
	return tip, true
}

// NextPublicKey returns the key the chain's rotation block hands over to. ok
// is false for a chain that has not rotated.
func (c *Chain) NextPublicKey() (pub ed25519.PublicKey, ok bool) {
	rotation, ok := c.Rotation()
	if !ok {
		return nil, false
	}
	op, ok := rotation.RotateKey()
	if !ok {
		return nil, false
	}
	return op.NewPublicKey(), true
}

func (c *Chain) String() string {
	return fmt.Sprintf("chain(%x, %d block(s), tip %s)", c.Pub[:8], len(c.Blocks), c.Tip().CID())
}

// ValidateChain validates a whole author chain, from the genesis block up to
// the block named by tip.
//
// It walks prev from the tip to the genesis block, then validates each block
// in publication order with Validate, so that every rule — signature, linkage,
// reachability, fork detection — is checked with the ancestors already in
// hand. The chain must be complete: a prev the source does not hold is an
// error wrapping ErrNotFound.
//
// A chain ends when a rotation block is published, so a rotation block may
// appear only as the tip; a block linked onto one is rejected by rule 3. The
// successor key's chain is a separate Chain, tied to this one by
// ValidateSuccession.
func ValidateChain(tip cid.Digest, src Source, opts *Options) (*Chain, error) {
	blocks, err := walk(tip, src)
	if err != nil {
		return nil, err
	}
	chain := &Chain{Pub: blocks[0].PublicKey(), Blocks: blocks, Report: &Report{}}
	for _, b := range blocks {
		report, err := Validate(b, src, opts)
		if err != nil {
			return nil, err
		}
		chain.Report.Warnings = append(chain.Report.Warnings, report.Warnings...)
		chain.Report.Forks = append(chain.Report.Forks, report.Forks...)
		chain.Report.Scanned += report.Scanned
		for _, rule := range report.Unchecked {
			if !slices.Contains(chain.Report.Unchecked, rule) {
				chain.Report.Unchecked = append(chain.Report.Unchecked, rule)
			}
		}
	}
	return chain, nil
}

// walk follows prev from tip to genesis and returns the blocks in publication
// order.
func walk(tip cid.Digest, src Source) ([]*Block, error) {
	var blocks []*Block
	seen := make(map[cid.Digest]bool)
	next := &tip
	for next != nil {
		d := *next
		if seen[d] {
			// Impossible for honest content-addressed blocks — a cycle would
			// need a SHA-256 preimage — but a Source is an interface, and a
			// buggy one must not spin this loop forever.
			return nil, fmt.Errorf("block: the chain reaching %s revisits block %s; prev links must form a list, not a cycle", tip, d)
		}
		seen[d] = true
		b, err := fetchChecked(src, d)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
		next = b.content.Prev
	}
	slices.Reverse(blocks)
	return blocks, nil
}

// fetchChecked reads a block and confirms the source handed back the block
// that was asked for. A Source is an interface; this keeps a mismatched store
// from silently rewriting history.
func fetchChecked(src Source, d cid.Digest) (*Block, error) {
	b, err := src.Block(d)
	if err != nil {
		return nil, err
	}
	if got := b.Digest(); got != d {
		return nil, fmt.Errorf("block: the source returned block %s for digest %s", got, d)
	}
	return b, nil
}

// ValidateSuccession checks the link between a chain that ended with a
// rotation block and the genesis block of the key that continues it
// (spec/02-block-format.md, "rotate_key", and spec/05-processing-model.md,
// "Chain succession").
//
// Three things are required and are errors: rotation must be a rotation block,
// genesis must be a genesis block (its prev is null), and genesis must be
// signed by the key the rotate_key operation names. The fourth — that the
// genesis block reference the rotation block in refs — is a SHOULD in the
// specification, so a genesis block that omits it is valid and gets a warning.
// Without that reference nothing on the wire ties the two chains together, and
// the report says so.
func ValidateSuccession(rotation, genesis *Block) (*Report, error) {
	report := &Report{}
	op, ok := rotation.RotateKey()
	if !ok {
		return nil, fmt.Errorf("block: %s is a %s block, not a rotation block; only a rotation block hands a chain over", rotation.CID(), rotation.content.Type)
	}
	if !genesis.IsGenesis() {
		return nil, fmt.Errorf("block: %s is not a genesis block, so it cannot begin the successor chain; its %q field must be null", genesis.CID(), keyPrev)
	}
	if !slices.Equal(genesis.content.Pub, op.NewPublicKey()) {
		return nil, fmt.Errorf("block: the rotation block hands over to %x, but the successor genesis block is signed by %x",
			op.NewPublicKey()[:8], genesis.content.Pub[:8])
	}
	if !slices.Contains(genesis.content.Refs, rotation.Digest()) {
		report.warn(0, genesis.Digest(), "the genesis block of the successor chain does not reference the rotation block %s in %q; key succession is unverifiable without it (spec/02-block-format.md makes the reference a SHOULD)",
			rotation.Digest(), keyRefs)
	}
	return report, nil
}

// ValidateHistory validates a succession of chains for one author, given the
// tip of each in order, oldest key first. Each chain is validated with
// ValidateChain, and each junction with ValidateSuccession: every chain but
// the last must end in a rotation block, and the next chain's genesis block
// must be signed by the key that rotation names.
func ValidateHistory(tips []cid.Digest, src Source, opts *Options) ([]*Chain, error) {
	if len(tips) == 0 {
		return nil, fmt.Errorf("block: ValidateHistory needs at least one chain tip")
	}
	chains := make([]*Chain, 0, len(tips))
	for i, tip := range tips {
		chain, err := ValidateChain(tip, src, opts)
		if err != nil {
			return nil, fmt.Errorf("block: chain %d of %d: %w", i+1, len(tips), err)
		}
		if i > 0 {
			previous := chains[i-1]
			rotation, ok := previous.Rotation()
			if !ok {
				return nil, fmt.Errorf("block: chain %d of %d does not end with a rotation block, so chain %d cannot succeed it", i, len(tips), i+1)
			}
			report, err := ValidateSuccession(rotation, chain.Genesis())
			if err != nil {
				return nil, err
			}
			chain.Report.Warnings = append(chain.Report.Warnings, report.Warnings...)
		}
		chains = append(chains, chain)
	}
	return chains, nil
}
