package block

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"strings"

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
// hand. That order is the induction of spec/02-block-format.md, "Validation":
// validity is defined from the genesis block forward, and each block is
// validated once, so the whole chain costs n validations rather than n².
//
// The chain must be complete: a prev the source does not hold is an error
// wrapping ErrNotFound. Such a tip is not invalid — it is stored but
// unvalidated until its ancestry arrives (spec/05-processing-model.md, "Block
// reception") — and errors.Is(err, ErrNotFound) is how a caller tells the two
// apart.
//
// A chain ends when a rotation block is published, so a rotation block may
// appear only as the tip; a block linked onto one is rejected by rule 3. The
// successor key's chain is a separate Chain, tied to this one by
// ValidateSuccession.
//
// # An accepted verdict is read, not recomputed
//
// A verdict moves in one direction (spec/02-block-format.md, "Validation", "A
// verdict moves in one direction"), and this function is the natural way to ask
// for a second validation of a block that already has a verdict — a node
// re-checking its store on startup, say. Re-validating a block against a store
// that has grown since cannot make it more valid, and it can make it less: an
// entry the first validation left unchecked because the block was not held
// might now resolve to a private block, and rules 6 and 10 would reject a block
// the node has already accepted, contributed to L2 and served.
//
// So it does not re-validate one. When src carries verdicts (see Verdicts,
// which ValidatingStore implements), a block the source has accepted is taken
// as accepted and its recorded report is folded in unchanged. A source that
// carries no verdicts cannot be asked, and there the obligation stays with the
// caller: it MUST NOT downgrade a block it has accepted on a rule 6 or 10
// finding for an entry the accepting validation reported in
// Report.UncheckedRefs.
func ValidateChain(tip cid.Digest, src Source, opts *Options) (*Chain, error) {
	blocks, err := walk(tip, src)
	if err != nil {
		return nil, err
	}
	chain := &Chain{Pub: blocks[0].PublicKey(), Blocks: blocks, Report: &Report{}}
	for _, b := range blocks {
		report, err := accepted(b, src, opts)
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

// accepted returns the verdict a source already holds for a block, and
// validates the block only when there is none to read (see ValidateChain, "An
// accepted verdict is read, not recomputed").
//
// A nil report from a source that keeps none is turned into an empty one, so
// that a caller folding reports together never has to test for it.
func accepted(b *Block, src Source, opts *Options) (*Report, error) {
	if v, ok := src.(Verdicts); ok {
		if verdict, report := v.Verdict(b.Digest()); verdict == VerdictValid {
			if report == nil {
				report = &Report{}
			}
			return report, nil
		}
	}
	return Validate(b, src, opts)
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
// (spec/02-block-format.md, "rotate_key", "Verifiable succession", and
// spec/05-processing-model.md, "Chain succession").
//
// Five things are required, and each is an error: rotation must be a rotation
// block, genesis must be a genesis block (its prev is null), genesis must be a
// public block, genesis must be signed by the key the rotate_key operation
// names, and genesis must list the rotation block's digest in its refs. The
// last is what makes the succession checkable from the blocks alone — the new
// key's own signature covers the reference — and a chain that omits it is not
// the successor of that rotation, whatever key signed it.
//
// The successor's genesis block MUST be public (spec/02-block-format.md,
// "Verifiable succession"): every node is asked to act on the reference, and a
// private block's refs are inside its enc field, so a node without the
// decryption key would be acting on evidence it cannot read. Confidential
// succession is deferred to a future protocol version; the blocks after the
// genesis block may be private.
//
// A public genesis block naming a rotation block in its refs is what rule 6
// permits: the rule excludes private targets only (spec/02-block-format.md,
// "Validation" rule 6).
//
// Of the three block types, the only one the type check turns away in practice
// is the private one: a rotation block is never a genesis block
// (spec/02-block-format.md, "Rotation block"), so it never reaches the genesis
// position at all and its case is refused a step earlier, by IsGenesis. The
// check keeps the type in the error message rather than reporting a missing
// prev.
//
// A rotation block naming its own key is impossible here: Content.Validate
// rejects one, so no such *Block exists.
func ValidateSuccession(rotation, genesis *Block) (*Report, error) {
	report := &Report{}
	op, ok := rotation.RotateKey()
	if !ok {
		return nil, fmt.Errorf("block: %s is a %s block, not a rotation block; only a rotation block hands a chain over", rotation.CID(), rotation.content.Type)
	}
	if !genesis.IsGenesis() {
		return nil, fmt.Errorf("block: %s is not a genesis block, so it cannot begin the successor chain; its %q field must be null", genesis.CID(), keyPrev)
	}
	if genesis.content.Type != TypePublic {
		if genesis.content.Type == TypePrivate {
			return nil, fmt.Errorf("block: %s is a private block, so it cannot begin a successor chain; its %q are inside %q and a node without the decryption key cannot read the reference the succession rests on",
				genesis.CID(), keyRefs, keyEnc)
		}
		return nil, fmt.Errorf("block: %s is a %s block, so it cannot begin a successor chain; the genesis block of a successor chain must be a %s block",
			genesis.CID(), genesis.content.Type, TypePublic)
	}
	if !slices.Equal(genesis.content.Pub, op.NewPublicKey()) {
		return nil, fmt.Errorf("block: the rotation block hands over to %x, but the successor genesis block is signed by %x",
			op.NewPublicKey()[:8], genesis.content.Pub[:8])
	}
	if !slices.Contains(genesis.content.Refs, rotation.Digest()) {
		return nil, fmt.Errorf("block: the genesis block of the successor chain does not reference the rotation block %s in %q; a chain that omits the reference is not the successor of that rotation",
			rotation.Digest(), keyRefs)
	}
	return report, nil
}

// Successors finds the genesis blocks that claim to continue a rotation
// block's chain: the stored blocks that name it in refs, are public genesis
// blocks, and are signed by the key its rotate_key operation appoints. The type
// is part of the question, not an afterthought — only a public block can begin
// a successor chain (spec/02-block-format.md, "Verifiable succession") — so a
// block of another type claiming the position is no successor and is not
// counted as one.
//
// Only one chain can succeed a rotation. More than one is an ambiguous
// succession: a fork in the strict sense of rule 9, since all of them claim the
// genesis position of the successor key's chain, but one held to a stricter
// rule than rule 9's. Every claimant is returned, together with the fork
// between them, and no claimant is preferred to another — "the node MUST
// surface the conflict and MUST NOT pick a successor on its own"
// (spec/02-block-format.md, "Verifiable succession", and
// spec/05-processing-model.md, "Chain succession (key rotation)"). A caller
// holding a fork here MUST NOT treat any one of the successors as the
// successor: not for auto-subscription, not for block order across the
// junction, not for anything else. Accept-first-seen, which rule 9 leaves open
// for an ordinary fork, is not available for this one. Holding and serving all
// of them is not a choice between them.
//
// src must implement Referrers; a source that cannot list the blocks referring
// to a digest cannot answer the question, and says so rather than reporting no
// successor.
func Successors(rotation *Block, src Source) (successors []cid.Digest, fork *Fork, err error) {
	op, ok := rotation.RotateKey()
	if !ok {
		return nil, nil, fmt.Errorf("block: %s is a %s block, not a rotation block; only a rotation block hands a chain over", rotation.CID(), rotation.content.Type)
	}
	r, ok := src.(Referrers)
	if !ok {
		return nil, nil, fmt.Errorf("block: the source cannot list the blocks referring to %s; implement block.Referrers to detect a successor chain", rotation.Digest())
	}
	newPub := op.NewPublicKey()
	for _, d := range r.BlocksReferencing(rotation.Digest()) {
		candidate, err := src.Block(d)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, nil, err
		}
		if candidate.IsGenesis() && candidate.content.Type == TypePublic && slices.Equal(candidate.content.Pub, newPub) {
			successors = append(successors, d)
		}
	}
	slices.SortFunc(successors, func(x, y cid.Digest) int { return slices.Compare(x[:], y[:]) })
	if len(successors) > 1 {
		fork = &Fork{Pub: newPub, Blocks: slices.Clone(successors)}
	}
	return successors, fork, nil
}

// An AmbiguousSuccessionError reports a rotation block that more than one
// genesis block claims to continue. It names the rotation block and every
// claimant, so that a caller can surface the conflict; it names no winner,
// because there is none to name — "the node MUST surface the conflict and MUST
// NOT pick a successor on its own" (spec/05-processing-model.md, "Chain
// succession (key rotation)").
//
// It is an error rather than a warning because of what the caller asked for: a
// history is a claim that these chains succeed one another, and a node that
// cannot show which chain succeeds a rotation cannot affirm that claim. The
// chains are not invalid, and each of them still validates on its own with
// ValidateChain. What is unavailable is the junction.
type AmbiguousSuccessionError struct {
	// Rotation is the rotation block the claimants are claiming.
	Rotation cid.Digest
	// Successors are the genesis blocks claiming it, ascending by digest.
	Successors []cid.Digest
}

func (e *AmbiguousSuccessionError) Error() string {
	claimants := make([]string, len(e.Successors))
	for i, d := range e.Successors {
		claimants[i] = d.String()
	}
	return fmt.Sprintf("block: the succession of rotation block %s is ambiguous: %d genesis blocks claim it (%s); the conflict is surfaced and no successor is chosen",
		e.Rotation, len(e.Successors), strings.Join(claimants, ", "))
}

// ValidateHistory validates a succession of chains for one author, given the
// tip of each in order, oldest key first. Each chain is validated with
// ValidateChain, and each junction with ValidateSuccession: every chain but
// the last must end in a rotation block, and the next chain's genesis block
// must be signed by the key that rotation names.
//
// A junction more than one genesis block claims is not a history this function
// affirms: it returns an *AmbiguousSuccessionError naming every claimant, since
// validating the succession the caller named would be picking a successor for
// it. Each chain still validates on its own with ValidateChain.
//
// Like ValidateChain, it reads an accepted verdict rather than recomputing it
// when the source carries verdicts.
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
			// A second genesis block claiming the same rotation makes the
			// succession ambiguous, and joining the caller's choice of chain
			// onto the rotation would be picking a successor on its behalf.
			// The fork is surfaced instead, naming every claimant, and the
			// history is not affirmed (spec/02-block-format.md, "Verifiable
			// succession"; spec/05-processing-model.md, "Chain succession (key
			// rotation)"). A source that cannot read the refs graph backwards
			// cannot be asked the question at all — it does not implement
			// Referrers — and there the junction rests on the evidence there
			// is, the pair of blocks in hand.
			if _, ok := src.(Referrers); ok {
				successors, fork, err := Successors(rotation, src)
				if err != nil {
					return nil, err
				}
				if fork != nil {
					return nil, &AmbiguousSuccessionError{Rotation: rotation.Digest(), Successors: successors}
				}
			}
		}
		chains = append(chains, chain)
	}
	return chains, nil
}
