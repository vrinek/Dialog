package block

import (
	"errors"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// DefaultScanLimit is the number of foreign blocks reference resolution will
// scan for one block before giving up: the value
// spec/05-processing-model.md, "Scan limit", asks every implementation to
// default to, so that a block gets the same verdict from every
// default-configured node. A hostile author can otherwise chain refs deeply
// enough to make one block cost an unbounded traversal, and a block that
// honestly needs more than this many CID-providing blocks is pathological.
const DefaultScanLimit = 256

// ErrScanLimit reports that reference resolution reached the configured scan
// limit before every digest resolved. spec/05-processing-model.md, "Scan
// limit", requires the block to be treated as invalid in that case, so the
// error arrives wrapped in a *RuleError for rule 4; errors.Is separates it
// from a genuinely unresolvable reference.
var ErrScanLimit = errors.New("block: the foreign block scan limit")

// ErrUndecryptable reports that reference resolution needed the operations of a
// private block the caller holds and supplied no key for. Like ErrNotFound it
// means the node has not been able to decide, not that the block is wrong
// (spec/05-processing-model.md, "Undecryptable reference handling"), so it
// arrives wrapped in a rule 4 *RuleError and answers true to IsUnvalidated;
// errors.Is separates the two causes for a caller that wants to fetch a block
// in one case and seek a key in the other.
var ErrUndecryptable = errors.New("block: no decryption key for a block resolution must read")

// IsUnvalidated reports whether a validation error means the node has not been
// able to decide, rather than that the block is wrong.
//
// Validation has three outcomes, not two (spec/02-block-format.md,
// "Validation" rule 4; spec/05-processing-model.md, "Block reception"): a
// block is valid, invalid, or *stored but unvalidated* — held, neither valid
// nor invalid, because a block validating it requires is one the node cannot
// read. The third is what this reports, whichever rule and whichever cause
// reached it: rule 3's missing predecessor, rule 4's unobtainable refs target
// (ErrNotFound), and rule 4's held-but-undecryptable one (ErrUndecryptable)
// are one verdict.
//
// A caller MUST NOT record such a block as rejected. It may keep the bytes and
// validate again when the missing block or the missing key arrives, or discard
// them and ask again; what it may not do is let a block it could not fetch, or
// a key it was not given, decide that another author's block is invalid. A
// source that withholds one foreign block would otherwise be able to
// invalidate a block that is in fact valid, and a key the node has not *yet*
// been made a recipient of would decide a question that is the same block's
// whatever this node holds.
//
// The scan limit is the deliberate opposite (ErrScanLimit): a bound the node
// chose, reached against blocks it holds and read, and therefore a definitive
// rejection.
func IsUnvalidated(err error) bool {
	return err != nil && (errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnaccepted) || errors.Is(err, ErrUndecryptable))
}

// ErrUnaccepted reports that rule 3's predecessor is held but has not been
// accepted as valid: the source carries verdicts (see Verdicts), and the one it
// holds for that block is not VerdictValid. Rule 3 requires a predecessor the
// node holds *and has accepted as valid* (spec/02-block-format.md, "Validation"
// rule 3), and a stored but unvalidated block MUST NOT be treated as another
// block's predecessor (spec/05-processing-model.md, "Block reception").
//
// Like ErrNotFound it means the node has not decided rather than that the block
// is wrong, so IsUnvalidated answers true to it. What would settle it is that
// block's own verdict rather than its arrival, which is why it is a separate
// sentinel: a caller fetches in one case and waits in the other.
//
// A source that carries no verdicts cannot raise it. There, rule 3 is a lookup
// among blocks the caller has undertaken to have validated, which is the
// contract a bare MemStore is used under.
var ErrUnaccepted = errors.New("block: the previous block has not been accepted as valid")

// A PendingError names the one block that left a verdict undecided: the block
// whose arrival, or whose decryption key, would let validation finish.
//
// It carries no message of its own — Error returns the wrapped error's, which
// already names the block in words. The type exists so that a store can file a
// stored-but-unvalidated block under what it is waiting for and re-validate it
// when that arrives, without parsing a sentence. errors.As reaches it through
// the *RuleError, and errors.Is still finds ErrNotFound or ErrUndecryptable
// beneath it, so IsUnvalidated is unaffected.
type PendingError struct {
	// Block is the block validation could not read.
	Block cid.Digest
	// Key reports which of the two causes it is. False: the source does not
	// hold Block, and holding it would settle the verdict. True: the source
	// holds Block and could not read it — a private block no Decrypter opened —
	// so no arrival will settle it and what is wanted is a key
	// (spec/05-processing-model.md, "Undecryptable reference handling").
	Key bool
	// Err is the error this stands in for, wrapping ErrNotFound or
	// ErrUndecryptable.
	Err error
}

func (e *PendingError) Error() string { return e.Err.Error() }

func (e *PendingError) Unwrap() error { return e.Err }

// Awaiting returns the block whose arrival would settle an undecided verdict.
//
// ok is false for any other error, and for the undecided verdict that is
// waiting on a decryption key rather than on a block: that block is already
// held, so no arrival will re-open the question and a store has nothing to file
// the waiting block under. Use errors.As with a *PendingError to see which
// block a key is wanted for.
func Awaiting(err error) (d cid.Digest, ok bool) {
	var pe *PendingError
	if !errors.As(err, &pe) || pe.Key {
		return cid.Digest{}, false
	}
	return pe.Block, true
}

// Options tunes validation. A nil *Options means the defaults.
type Options struct {
	// ScanLimit caps the distinct foreign blocks reference resolution scans
	// while resolving one block's references — the unit
	// spec/05-processing-model.md, "Scan limit", defines. Zero means
	// DefaultScanLimit; a negative value means no limit.
	ScanLimit int
	// Decrypter, when set, lets reference resolution read the operations of
	// private blocks the caller holds keys for. Without it a private block
	// resolution meets — an ancestor of the block being validated, or a block
	// its refs name — contributes no definitions, since this package holds no
	// keys, and a digest that needed one of those definitions leaves the
	// verdict undecided rather than invalid (ErrUndecryptable).
	Decrypter Decrypter
}

func (o *Options) scanLimit() int {
	if o == nil || o.ScanLimit == 0 {
		return DefaultScanLimit
	}
	return o.ScanLimit
}

func (o *Options) decrypter() Decrypter {
	if o == nil {
		return nil
	}
	return o.Decrypter
}

// A Decrypter supplies the decrypted payload of a private block to reference
// resolution. This package holds no keys; a caller that does implements this so
// that a chain's own private ancestors, and private blocks its refs name, can
// define the entities its operations reference.
//
// It is the mechanism behind spec/05-processing-model.md, "Undecryptable
// reference handling": a node that can read a block but not one the block
// depends on has not decided, MUST surface that undecided state and MUST NOT
// accept the block on partial validation. Returning ok false for a block the
// caller has no key for is what produces the ErrUndecryptable verdict, once a
// digest actually needs that block's operations.
type Decrypter interface {
	// DecryptPayload returns the payload of a private block. ok is false when
	// no key for the block is held, which is not an error; an error is a
	// failure to decrypt with a key that should have worked.
	DecryptPayload(b *Block) (p Payload, ok bool, err error)
}

// PrivateBlockNotice is the text of the warning Validate records on a private
// block: the four rules it cannot check are listed in the report's Unchecked
// field, and this says so in words. A caller that goes on to check them with
// ValidatePayload drops the warning, which is why the text is a constant
// rather than a literal.
const PrivateBlockNotice = "this is a private block: rules 4, 5, 6 and 10 can only be checked by a holder of the decryption key"

// A Warning is something a validator is asked to notice but not to reject —
// a non-monotonic timestamp, a rule it could not check, a SHOULD an author did
// not honour.
type Warning struct {
	// Rule is the numbered validation rule of spec/02-block-format.md the
	// warning belongs to, or 0 when it belongs to none.
	Rule int
	// Block is the block the warning is about.
	Block cid.Digest
	// Msg says what was noticed.
	Msg string
}

func (w Warning) String() string {
	if w.Rule == 0 {
		return w.Msg
	}
	return fmt.Sprintf("rule %d: %s", w.Rule, w.Msg)
}

// A Report is what a successful validation produces: everything worth knowing
// that is not a rejection.
type Report struct {
	// Warnings are the SHOULDs and the unnoticed oddities, in the order they
	// were found.
	Warnings []Warning
	// Forks lists the chain forks detected while validating
	// (spec/02-block-format.md, "Validation" rule 9). A non-empty Forks is not
	// a rejection: detection is normative, handling is the caller's.
	Forks []Fork
	// Scanned counts the distinct foreign blocks resolution scanned through
	// the refs graph: the unit of spec/05-processing-model.md, "Scan limit". A
	// block reached twice counts once, an ancestor of the author's own chain
	// does not count, and neither does a refs entry the source does not hold or
	// one fetched only to check rules 6 and 10 against it.
	Scanned int
	// Unchecked lists the numbered rules that could not be evaluated — rules
	// 4, 5, 6 and 10 of a private block, whose refs and ops are inside its
	// ciphertext (spec/02-block-format.md: "For private blocks, validation of
	// rules 4, 5, 6, and 10 is only possible by entities that hold the
	// decryption key"). A caller that holds the key decrypts the payload and
	// passes it to ValidatePayload, which checks exactly these four.
	Unchecked []int
	// UncheckedRefs lists the refs entries the source did not hold, against
	// which rules 6 and 10 could therefore not be evaluated
	// (spec/02-block-format.md, "Validation" rule 6). It is informational and
	// nothing else: it names what the verdict does not cover, so that a caller
	// can ask for those blocks if it cares. The block is valid.
	//
	// It is not a reservation. An entry no validation of this block resolved is
	// permanently outside the block's validity: a caller that later holds one
	// of these blocks and finds it private, or of this author's own chain, MUST
	// NOT re-open the verdict of a block it has accepted — a verdict moves in
	// one direction, and nothing has to be undone in an append-only L2. What it
	// MAY do is surface the finding.
	UncheckedRefs []cid.Digest
}

func (r *Report) warn(rule int, d cid.Digest, format string, args ...any) {
	r.Warnings = append(r.Warnings, Warning{Rule: rule, Block: d, Msg: fmt.Sprintf(format, args...)})
}

// A RuleError reports a violation of one of the numbered validation rules of
// spec/02-block-format.md, "Validation". Callers that care which rule failed
// use errors.As.
type RuleError struct {
	// Rule is the rule number, 1 to 10.
	Rule int
	// Block is the block that failed it.
	Block cid.Digest
	// Err says what was wrong.
	Err error
}

func (e *RuleError) Error() string {
	return fmt.Sprintf("block %s: validation rule %d (%s): %v", e.Block, e.Rule, ruleName(e.Rule), e.Err)
}

func (e *RuleError) Unwrap() error { return e.Err }

// ruleName names the numbered rules of spec/02-block-format.md, "Validation".
func ruleName(rule int) string {
	switch rule {
	case 1:
		return "version check"
	case 2:
		return "signature check"
	case 3:
		return "chain integrity"
	case 4:
		return "operation validity"
	case 5:
		return "data model conformance"
	case 6:
		return "public/private reference rules"
	case 7:
		return "non-empty operations"
	case 8:
		return "deterministic encoding"
	case 9:
		return "fork detection"
	case 10:
		return "reference hygiene"
	default:
		return "unknown rule"
	}
}

func ruleErr(rule int, b *Block, format string, args ...any) error {
	return &RuleError{Rule: rule, Block: b.Digest(), Err: fmt.Errorf(format, args...)}
}

// Validate checks one block against the ten numbered rules of
// spec/02-block-format.md, "Validation", in their stated order, reading the
// blocks it needs from src.
//
// The rules and where each is enforced:
//
//  1. Version check — here, and already at Decode.
//  2. Signature check — here, and at every constructor.
//  3. Chain integrity — here: prev exists, carries the same pub key, and is
//     not a rotation block (a rotation block ends its key's chain, and no
//     further block signed by that key may be accepted).
//  4. Operation validity — here: every entity digest an operation carries is
//     reachable from the same block, an ancestor, or the refs graph.
//  5. Data model conformance — here: each create_molecule's filler count
//     matches its bond's template, and each digest resolves to an entity of
//     the kind its position names.
//  6. Public/private reference rules — here: a public block's refs do not name
//     a private block. Public and rotation blocks may be named; only a private
//     block is excluded, being the one target a node without the decryption key
//     cannot validate.
//  7. Non-empty operations — at Decode and in Content.Validate.
//  8. Deterministic encoding — at Decode, which rejects any non-canonical
//     encoding, and by construction for a block this package built.
//  9. Fork detection — here, when src implements Siblings.
//  10. Reference hygiene — the duplicate half at Decode and in
//     Content.Validate, which need no other block; the own-chain half here,
//     when a referenced block is resolved.
//
// A rejection is a *RuleError naming the rule. A missing block is reported
// with ErrNotFound in the chain: errors.Is(err, ErrNotFound) distinguishes
// "this block is wrong" from "I do not have enough of the graph yet".
//
// Two rules can end that way, and they end that way for the same reason. Rule
// 3 does when the predecessor is not held; rule 4 does when reference
// resolution needs a block it cannot read — a refs entry, a block reached
// transitively through one, or an ancestor deeper in the author's own chain,
// either absent from the source or held as ciphertext no Decrypter opens
// (ErrUndecryptable, spec/05-processing-model.md, "Undecryptable reference
// handling") — and a digest is left unresolved for want of it. All of them
// mean the block is *stored but unvalidated* (spec/05-processing-model.md,
// "Block reception"): neither valid nor invalid, kept out of L2, and
// validatable again once the missing block, or the missing key, arrives.
// Neither a block nor a key this node lacks is evidence about the validity of
// the block that needs it, so a caller MUST NOT record such a block as
// rejected — which is what makes IsUnvalidated a question worth asking before
// acting on any error this returns.
//
// For a private block, rules 4, 5, 6 and 10 are listed in the report's
// Unchecked field rather than evaluated: refs and ops are inside enc, which
// this package treats as opaque. A caller holding the decryption key decrypts
// the payload (see the privacy package) and passes it to ValidatePayload; the
// two calls together are a complete validation of a private block.
func Validate(b *Block, src Source, opts *Options) (*Report, error) {
	if b == nil {
		return nil, fmt.Errorf("block: Validate called with a nil block")
	}
	report := &Report{}
	d := b.Digest()

	// Rule 1 — version check.
	if b.content.Version != Version {
		return nil, ruleErr(1, b, "unrecognized protocol version %d, want %d", b.content.Version, Version)
	}

	// Rule 2 — signature check.
	if err := b.Verify(); err != nil {
		return nil, &RuleError{Rule: 2, Block: d, Err: err}
	}

	// Rule 7 — non-empty operations. Structural, and already enforced; the
	// check is restated so that a Block built by some future path cannot slip
	// past it.
	if b.content.Type.hasPlaintextPayload() && len(b.content.Ops) == 0 {
		return nil, ruleErr(7, b, "the %q list is empty; a block must contain at least one operation", keyOps)
	}

	// Rule 3 — chain integrity.
	if _, err := validateLinkage(b, src, report); err != nil {
		return nil, err
	}

	// Rule 9 — fork detection. Detection is required; handling is the
	// caller's, so a fork lands in the report rather than in the error.
	if s, ok := src.(Siblings); ok {
		if f, found := detectFork(b, s); found {
			report.Forks = append(report.Forks, f)
		}
	} else {
		report.warn(9, d, "the source cannot list sibling blocks, so a chain fork at this position would go unnoticed; implement block.Siblings to detect one")
	}

	// Rules 4, 5, 6 and 10 need the operations and refs in the clear.
	if b.content.Type == TypePrivate {
		report.Unchecked = append(report.Unchecked, 4, 5, 6, 10)
		report.warn(0, d, "%s", PrivateBlockNotice)
		return report, nil
	}

	if err := validateReferences(b, b.content.Refs, b.content.Ops, src, opts, report); err != nil {
		return nil, err
	}
	return report, nil
}

// validateLinkage is rule 3, plus the timestamp SHOULD that hangs off it. It
// returns the predecessor block, or nil for a genesis block.
//
// Validity is inductive from the genesis block, and the induction is carried by
// the source: a block the source holds was validated when it was received, so
// rule 3 is a lookup among accepted blocks and not a re-validation of the
// ancestry (spec/02-block-format.md, "Validation"; spec/05-processing-model.md,
// "Block reception"). Validating a chain of n blocks therefore costs n
// validations, which is what ValidateChain does, walking from the genesis block
// forward.
//
// A block whose predecessor the source does not hold is neither valid nor
// invalid: it is "stored but unvalidated" in the terms of
// spec/05-processing-model.md, and its operations must not reach L2. That is
// the ErrNotFound wrapped in the rule 3 error — errors.Is separates "I cannot
// validate this yet" from "this block is wrong".
func validateLinkage(b *Block, src Source, report *Report) (*Block, error) {
	prevDigest, ok := b.Prev()
	if !ok {
		// Genesis: prev MUST be null for the first block of a chain, and this
		// is the only block for which it may be.
		return nil, nil
	}
	prev, err := src.Block(prevDigest)
	if err != nil {
		inner := fmt.Errorf("previous block %s: %w", prevDigest, err)
		if errors.Is(err, ErrNotFound) {
			// The predecessor's arrival is what would settle this, and a
			// verdict-carrying store files the block under it (see Awaiting).
			inner = &PendingError{Block: prevDigest, Err: inner}
		}
		return nil, &RuleError{Rule: 3, Block: b.Digest(), Err: inner}
	}
	// Rule 3 wants a predecessor the node holds *and has accepted as valid*. A
	// source that records verdicts can answer that; one that does not is being
	// used under the contract that everything in it was validated when it
	// arrived, which is the induction the specification describes and a bare
	// MemStore leaves to its caller.
	//
	// This is the one place validation reads a verdict rather than a block, and
	// it is the deliberate counterpart of rule 4, which reads blocks whatever
	// their verdict (see the resolver's doc comment): rule 3 asks whether an
	// author's chain is intact, rule 4 asks what a digest names.
	if v, ok := src.(Verdicts); ok {
		if verdict, _ := v.Verdict(prevDigest); verdict != VerdictValid {
			return nil, &RuleError{Rule: 3, Block: b.Digest(), Err: &PendingError{
				Block: prevDigest,
				Err: fmt.Errorf("previous block %s is held but its verdict is %s, and a block that has not been accepted as valid must not be treated as another block's predecessor: %w",
					prevDigest, verdict, ErrUnaccepted),
			}}
		}
	}
	if !prev.SameAuthor(b) {
		return nil, ruleErr(3, b, "previous block %s is signed by %x, not by this block's author %x; within a single chain all blocks carry the same %q",
			prevDigest, prev.content.Pub[:8], b.content.Pub[:8], keyPub)
	}
	if prev.content.Type == TypeRotation {
		return nil, ruleErr(3, b, "previous block %s is a rotation block, which ends this key's chain; no further block signed by %x may be accepted",
			prevDigest, b.content.Pub[:8])
	}
	// The timestamp is untrusted and MUST NOT decide validity; a non-monotonic
	// one is a warning (spec/02-block-format.md, the ts field).
	if b.content.Type != TypePrivate && prev.content.Type != TypePrivate && b.content.TS < prev.content.TS {
		report.warn(0, b.Digest(), "timestamp %d is earlier than the previous block's %d; timestamps in a chain should not go backwards", b.content.TS, prev.content.TS)
	}
	return prev, nil
}

// detectFork implements rule 9: another stored block of the same author
// claiming the same predecessor is a fork.
func detectFork(b *Block, s Siblings) (Fork, bool) {
	d := b.Digest()
	siblings := s.BlocksWithPrev(b.PublicKey(), b.content.Prev)
	others := make([]cid.Digest, 0, len(siblings))
	for _, sib := range siblings {
		if sib != d {
			others = append(others, sib)
		}
	}
	if len(others) == 0 {
		return Fork{}, false
	}
	f := Fork{Pub: b.PublicKey(), Blocks: append(others, d)}
	slices.SortFunc(f.Blocks, func(x, y cid.Digest) int { return slices.Compare(x[:], y[:]) })
	if p, ok := b.Prev(); ok {
		f.Prev = &p
	}
	return f, true
}

// validateReferences is rules 4, 5, 6 and 10 for a block whose refs and ops are
// at hand — a public or rotation block's own fields, or the payload a holder of
// the decryption key has recovered from a private block, which is why the two
// are parameters and not read off the block (see ValidatePayload).
func validateReferences(b *Block, refs []cid.Digest, ops []Operation, src Source, opts *Options, report *Report) error {
	r := &resolver{
		block:     b,
		src:       src,
		limit:     opts.scanLimit(),
		decrypter: opts.decrypter(),
		defs:      make(map[cid.Digest]record),
		cache:     make(map[cid.Digest]*Block),
		report:    report,
	}
	if d, ok := b.Prev(); ok {
		r.nextAncestor = &d
	}

	// Rules 6 and 10 are both properties of a referenced block rather than of
	// this one, so they share a single pass over refs: a public block's refs
	// must not name a private block, and no block may reference a block of its
	// author's own chain — which, every block of a chain carrying the same pub,
	// is a comparison of the two keys (spec/02-block-format.md, "The refs
	// list"). Both are evaluated as a referenced block is resolved; an entry
	// the source does not hold is reported as unchecked rather than rejected,
	// which is what demand-driven resolution leaves possible.
	// A block fetched in this pass has not been scanned: its operations are
	// read only if resolution reaches it below, which is where it counts
	// against the scan limit (spec/05-processing-model.md, "Scan limit").
	//
	// An entry left unchecked here is outside this block's validity for good.
	// The two rules bind for the entries a validation resolved, and a caller
	// that later holds one of the others MUST NOT re-open a verdict it has
	// accepted on the strength of it (spec/02-block-format.md, "Validation", "A
	// verdict moves in one direction"; report.UncheckedRefs is what it needs to
	// tell the two apart).
	for _, ref := range refs {
		target, err := r.fetch(ref)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				report.UncheckedRefs = append(report.UncheckedRefs, ref)
				if b.content.Type == TypePublic {
					report.warn(6, b.Digest(), "referenced block %s is not held by the source, so neither its type (rule 6) nor its author (rule 10) could be checked", ref)
				} else {
					report.warn(10, b.Digest(), "referenced block %s is not held by the source, so its author could not be checked", ref)
				}
				continue
			}
			return err
		}
		// Rule 6 excludes exactly one kind of target: the private block, whose
		// operations a node without the decryption key cannot read. A rotation
		// block is in the clear for every node, so a public block may name one —
		// which is what a successor chain's genesis block does
		// (spec/02-block-format.md, "Validation" rule 6, and "rotate_key",
		// "Verifiable succession").
		if b.content.Type == TypePublic && target.content.Type == TypePrivate {
			return ruleErr(6, b, "refs entry %s is a private block; a public block's %q must not name one, since a node without the decryption key cannot validate what it depends on", ref, keyRefs)
		}
		if target.SameAuthor(b) {
			return ruleErr(10, b, "refs entry %s is signed by this block's own author %x, so it is a block of this chain; the author's own ancestry is already a resolution path and must not be listed in %q",
				ref, b.content.Pub[:8], keyRefs)
		}
	}
	r.queue = slices.Clone(refs)

	// Rule 4 — every entity digest an operation carries must be reachable, and
	// within this block only from an operation that precedes it.
	for i, op := range ops {
		for _, ref := range op.References() {
			rec, err := r.resolve(ref.Digest)
			if err != nil {
				return &RuleError{Rule: 4, Block: b.Digest(), Err: fmt.Errorf("operation %d (%s) %s: %w", i, op.Op(), ref.Field, err)}
			}
			// Rule 5 — the data model binds each position to an entity kind.
			if rec.kind != ref.Kind {
				return ruleErr(5, b, "operation %d (%s) %s names %s %s, but that digest is %s %s",
					i, op.Op(), ref.Field, ref.Kind, ref.Digest, article(rec.kind), rec.kind)
			}
		}
		// Rule 5 — the filler count MUST equal the bond's variable count.
		if cm, ok := op.(CreateMolecule); ok {
			bond := r.defs[cm.molecule.Bond()].bond
			if err := cm.molecule.ValidateAgainst(bond); err != nil {
				return &RuleError{Rule: 5, Block: b.Digest(), Err: fmt.Errorf("operation %d (%s): %w", i, op.Op(), err)}
			}
		}
		// Only now do this operation's entities become visible: within a block
		// an operation may reference what an *earlier* operation created.
		r.define(op)
	}
	report.Scanned = r.scanned
	return nil
}

func article(k EntityKind) string {
	if k == KindAtom {
		return "an"
	}
	return "a"
}

// A record is what a digest resolved to: the kind of entity, and — for a bond,
// whose template the filler-count rule needs — the entity itself.
type record struct {
	kind EntityKind
	bond entity.Bond
}

// A resolver answers "is this entity digest reachable from this block?" the
// way spec/05-processing-model.md, "Resolution procedure", describes: the
// block itself, then the author's own ancestors through prev, then the refs
// graph, fetched on demand and traversed transitively until the digest is
// found or the scan limit is reached.
//
// It is demand-driven in both directions: ancestors are walked one block at a
// time and the refs graph is a breadth-first queue, and both keep their place
// between lookups, so a block whose first operation resolves from its own
// chain never fetches a foreign block at all.
//
// # It reads blocks, not verdicts
//
// A definition is taken from any block the Source hands over, whatever that
// block's own validity: a block held while its ancestry is missing, a block
// that forked its author's chain, a block that will turn out invalid when the
// rest of its chain arrives. That is what spec/05-processing-model.md,
// "Resolution procedure", "Resolution reads blocks, not verdicts", permits, and
// it is sound here for two reasons that are properties of this package rather
// than of any store.
//
// The first is that the block's self-contained checks have already run. A
// *Block exists only through Decode, Sign or Assemble, and all three go through
// Content.Validate and verify the Ed25519 signature against the block's own pub
// field, Decode having first insisted on canonical dCBOR and the exact field set
// of the block's type. There is no way to obtain a *Block whose bytes, structure
// or signature are in doubt, so every block a Source can hand out is
// structurally sound in the sense the rule requires.
//
// The second is that a definition is self-certifying. define below indexes an
// operation under the digest op.Creates() computes — Atom.Digest, Bond.Digest
// or Molecule.Digest, each of them SHA-256 over the entity's canonical dCBOR
// (spec/01-data-model.md, "Content addressing") — so the digest a lookup
// matches is one this package derived from the entity's own bytes. No block
// asserts a digest and none is believed; the source block's chain standing
// cannot change what those bytes hash to.
//
// What that permission does not touch: only a valid block's operations may
// reach L2 (spec/05-processing-model.md, "Accumulation rules"), which is the
// graph package's caller's business and not this one's; rules 6 and 10 are
// checked against the referenced block exactly as written; and rule 3 still
// requires a predecessor the caller has accepted as valid.
type resolver struct {
	block     *Block
	src       Source
	limit     int
	decrypter Decrypter
	report    *Report

	defs  map[cid.Digest]record
	cache map[cid.Digest]*Block

	nextAncestor  *cid.Digest
	ancestorGap   *cid.Digest // the ancestor the source does not hold
	refsGap       *cid.Digest // the first refs-graph block the source does not hold
	keyGap        *cid.Digest // the first block held as ciphertext no key was supplied for
	privateWarned bool

	queue   []cid.Digest
	visited map[cid.Digest]bool
	scanned int
}

// resolve finds the entity a digest names, extending the search as far as it
// must.
func (r *resolver) resolve(d cid.Digest) (record, error) {
	if rec, ok := r.defs[d]; ok {
		return rec, nil
	}
	// Step 3 of the resolution procedure: ancestor blocks in the same chain.
	for r.nextAncestor != nil {
		if err := r.extendAncestors(); err != nil {
			return record{}, err
		}
		if rec, ok := r.defs[d]; ok {
			return rec, nil
		}
	}
	// Steps 4 and 5: the refs graph, transitively.
	for len(r.queue) > 0 {
		if err := r.extendRefs(); err != nil {
			return record{}, err
		}
		if rec, ok := r.defs[d]; ok {
			return rec, nil
		}
	}
	// Three-valued rule 4 (spec/02-block-format.md, "Validation" rule 4;
	// spec/05-processing-model.md, "Resolution procedure"). Resolution has
	// failed, and which failure it is depends on whether it ran out of
	// definitions or ran out of blocks it could read. A gap in any of the three
	// directions — the author's own chain, the refs graph, or a block held as
	// ciphertext with no key for it — means the node has not decided, so the
	// error is one IsUnvalidated answers true to and the caller reads it as
	// "stored but unvalidated" rather than "invalid". Only a resolution that
	// completed against everything the source holds *and could read* proves a
	// digest absent.
	if r.ancestorGap != nil {
		return record{}, &PendingError{Block: *r.ancestorGap, Err: fmt.Errorf("entity %s could not be resolved: the author's chain is incomplete at block %s, which the source does not hold, so the block is stored but unvalidated rather than invalid: %w", d, r.ancestorGap, ErrNotFound)}
	}
	if r.refsGap != nil {
		return record{}, &PendingError{Block: *r.refsGap, Err: fmt.Errorf("entity %s could not be resolved: the refs graph is incomplete at block %s, which the source does not hold, so the block is stored but unvalidated rather than invalid: %w", d, r.refsGap, ErrNotFound)}
	}
	if r.keyGap != nil {
		return record{}, &PendingError{Block: *r.keyGap, Key: true, Err: fmt.Errorf("entity %s could not be resolved: block %s is private and no decryption key for it was supplied, so its operations could not be read; the block is stored but unvalidated rather than invalid, and a key would settle it: %w", d, r.keyGap, ErrUndecryptable)}
	}
	return record{}, fmt.Errorf("entity %s is not reachable from this block, from an ancestor in the author's chain, or from any block in the refs graph", d)
}

// extendAncestors folds one more ancestor's operations into the definitions.
func (r *resolver) extendAncestors() error {
	d := *r.nextAncestor
	ancestor, err := r.fetch(d)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// The chain stops here as far as this source is concerned. Rule 3
			// has already checked the immediate predecessor, so this is a gap
			// deeper in, and it only matters if a digest fails to resolve.
			r.ancestorGap, r.nextAncestor = &d, nil
			return nil
		}
		return err
	}
	ops := ancestor.content.Ops
	if ancestor.content.Type == TypePrivate {
		p, ok, err := r.decrypt(ancestor)
		if err != nil {
			return err
		}
		if ok {
			// A key holder validating its own private chain resolves through
			// its own earlier blocks, exactly as a public chain does.
			ops = p.Ops
		} else {
			// The block is held and unreadable, which is the readability cause
			// of "stored but unvalidated": if a digest then fails to resolve,
			// the verdict is undecided rather than invalid (see resolve).
			if r.keyGap == nil {
				r.keyGap = &d
			}
			if !r.privateWarned {
				r.report.warn(4, r.block.Digest(), "ancestor block %s is private and no decryption key for it was supplied, so the entities its operations define cannot satisfy a reference", d)
				r.privateWarned = true
			}
		}
	}
	for _, op := range ops {
		r.define(op)
	}
	r.nextAncestor = ancestor.content.Prev
	return nil
}

// decrypt asks the caller's Decrypter for a private block's payload. Without
// one, no private block is readable, which is the honest answer for a package
// that holds no keys.
func (r *resolver) decrypt(b *Block) (Payload, bool, error) {
	if r.decrypter == nil {
		return Payload{}, false, nil
	}
	p, ok, err := r.decrypter.DecryptPayload(b)
	if err != nil {
		return Payload{}, false, fmt.Errorf("block: decrypting %s: %w", b.Digest(), err)
	}
	if !ok {
		return Payload{}, false, nil
	}
	if err := p.Validate(); err != nil {
		return Payload{}, false, err
	}
	return p, true, nil
}

// extendRefs folds one more block of the refs graph into the definitions and
// queues that block's own refs, which is the recursion of step 5.
func (r *resolver) extendRefs() error {
	d := r.queue[0]
	r.queue = r.queue[1:]
	if r.visited == nil {
		r.visited = make(map[cid.Digest]bool)
	}
	if r.visited[d] {
		return nil
	}
	r.visited[d] = true

	target, err := r.fetch(d)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// An unavailable CID provider is not itself a failure: another ref
			// may still define the digest. If none does, the verdict is not
			// determinable rather than invalid, which is what recording the gap
			// is for (see resolve). Nothing was scanned, so nothing is counted.
			if r.refsGap == nil {
				r.refsGap = &d
			}
			r.report.warn(4, r.block.Digest(), "referenced block %s is not held by the source", d)
			return nil
		}
		return err
	}
	// This block is about to be scanned — fetched and read for the definitions
	// its operations carry — which is the unit the scan limit counts
	// (spec/05-processing-model.md, "Scan limit"). The visited set above makes
	// it one unit however often the refs graph names it.
	if r.limit >= 0 && r.scanned >= r.limit {
		return fmt.Errorf("%w of %d foreign block(s) was reached before every reference resolved", ErrScanLimit, r.limit)
	}
	r.scanned++
	ops, refs := target.content.Ops, target.content.Refs
	if target.content.Type == TypePrivate {
		p, ok, err := r.decrypt(target)
		if err != nil {
			return err
		}
		if !ok {
			// The operations are encrypted and no key for them was supplied, so
			// the block contributes no definitions. spec/05-processing-model.md,
			// "Undecryptable reference handling", makes that the undecided
			// verdict once a digest actually needs it — the node holds the block
			// but cannot read it, and a key holder decides the same question
			// differently, so nothing here shows the referencing block wrong.
			// Recording the gap is what turns it into "stored but unvalidated"
			// (see resolve) rather than a rejection.
			if r.keyGap == nil {
				r.keyGap = &d
			}
			r.report.warn(4, r.block.Digest(), "referenced block %s is private and no decryption key for it was supplied; its operations are not visible", d)
			return nil
		}
		ops, refs = p.Ops, p.Refs
	}
	for _, op := range ops {
		r.define(op)
	}
	r.queue = append(r.queue, refs...)
	return nil
}

// define records the entity an operation creates, under the digest
// op.Creates() computes from the entity's canonical bytes. That recomputation
// is what makes a definition self-certifying, and it is why a definition may be
// read from a block whose own validity is undecided (see the resolver's doc
// comment).
//
// The first definition wins; re-creating the same entity is idempotent, since
// the digest determines the content.
func (r *resolver) define(op Operation) {
	d, kind, ok := op.Creates()
	if !ok {
		return
	}
	if _, exists := r.defs[d]; exists {
		return
	}
	rec := record{kind: kind}
	if cb, isBond := op.(CreateBond); isBond {
		rec.bond = cb.bond
	}
	r.defs[d] = rec
}

// fetch reads a block through the source and caches it. Fetching is not
// scanning: the scan limit counts the foreign blocks whose operations
// resolution reads, which is counted where that happens (see extendRefs).
func (r *resolver) fetch(d cid.Digest) (*Block, error) {
	if b, ok := r.cache[d]; ok {
		return b, nil
	}
	b, err := r.src.Block(d)
	if err != nil {
		return nil, err
	}
	r.cache[d] = b
	return b, nil
}
