package block

import (
	"errors"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// DefaultScanLimit is the number of foreign blocks reference resolution will
// fetch for one block before giving up. spec/05-processing-model.md, "Scan
// limit", makes the limit optional but asks for a safe default: a hostile
// author can otherwise chain refs deeply enough to make one block cost an
// unbounded traversal. A block that honestly needs more than this many
// CID-providing blocks is pathological.
const DefaultScanLimit = 256

// ErrScanLimit reports that reference resolution reached the configured scan
// limit before every digest resolved. spec/05-processing-model.md, "Scan
// limit", requires the block to be treated as invalid in that case, so the
// error arrives wrapped in a *RuleError for rule 4; errors.Is separates it
// from a genuinely unresolvable reference.
var ErrScanLimit = errors.New("block: the foreign block scan limit")

// Options tunes validation. A nil *Options means the defaults.
type Options struct {
	// ScanLimit caps the foreign blocks fetched through the refs graph while
	// resolving one block's references. Zero means DefaultScanLimit; a
	// negative value means no limit.
	ScanLimit int
}

func (o *Options) scanLimit() int {
	if o == nil || o.ScanLimit == 0 {
		return DefaultScanLimit
	}
	return o.ScanLimit
}

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
	// Scanned counts the foreign blocks fetched through the refs graph.
	Scanned int
	// Unchecked lists the numbered rules that could not be evaluated — rules
	// 4, 5 and 6 of a private block, whose refs and ops this package cannot
	// read (spec/02-block-format.md: "For private blocks, validation of rules
	// 4, 5, and 6 is only possible by entities that hold the decryption key").
	Unchecked []int
}

func (r *Report) warn(rule int, d cid.Digest, format string, args ...any) {
	r.Warnings = append(r.Warnings, Warning{Rule: rule, Block: d, Msg: fmt.Sprintf(format, args...)})
}

// A RuleError reports a violation of one of the numbered validation rules of
// spec/02-block-format.md, "Validation". Callers that care which rule failed
// use errors.As.
type RuleError struct {
	// Rule is the rule number, 1 to 9.
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
	default:
		return "unknown rule"
	}
}

func ruleErr(rule int, b *Block, format string, args ...any) error {
	return &RuleError{Rule: rule, Block: b.Digest(), Err: fmt.Errorf(format, args...)}
}

// Validate checks one block against the nine numbered rules of
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
//  6. Public/private reference rules — here: a public block's refs list only
//     public blocks.
//  7. Non-empty operations — at Decode and in Content.Validate.
//  8. Deterministic encoding — at Decode, which rejects any non-canonical
//     encoding, and by construction for a block this package built.
//  9. Fork detection — here, when src implements Siblings.
//
// A rejection is a *RuleError naming the rule. A missing block is reported
// with ErrNotFound in the chain: errors.Is(err, ErrNotFound) distinguishes
// "this block is wrong" from "I do not have enough of the graph yet".
//
// For a private block, rules 4, 5 and 6 are listed in the report's Unchecked
// field rather than evaluated: refs and ops are inside enc, which this package
// treats as opaque.
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
	prev, err := validateLinkage(b, src, report)
	if err != nil {
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

	// Rules 4, 5 and 6 need the operations and refs in the clear.
	if b.content.Type == TypePrivate {
		report.Unchecked = append(report.Unchecked, 4, 5, 6)
		report.warn(0, d, "this is a private block: rules 4, 5 and 6 can only be checked by a holder of the decryption key")
		return report, nil
	}

	if err := validateReferences(b, prev, src, opts, report); err != nil {
		return nil, err
	}
	return report, nil
}

// validateLinkage is rule 3, plus the timestamp SHOULD that hangs off it. It
// returns the predecessor block, or nil for a genesis block.
func validateLinkage(b *Block, src Source, report *Report) (*Block, error) {
	prevDigest, ok := b.Prev()
	if !ok {
		// Genesis: prev MUST be null for the first block of a chain, and this
		// is the only block for which it may be.
		return nil, nil
	}
	prev, err := src.Block(prevDigest)
	if err != nil {
		return nil, &RuleError{Rule: 3, Block: b.Digest(), Err: fmt.Errorf("previous block %s: %w", prevDigest, err)}
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

// validateReferences is rules 4, 5 and 6 for a block whose operations are in
// the clear.
func validateReferences(b *Block, prev *Block, src Source, opts *Options, report *Report) error {
	r := &resolver{
		block:  b,
		src:    src,
		limit:  opts.scanLimit(),
		defs:   make(map[cid.Digest]record),
		cache:  make(map[cid.Digest]*Block),
		report: report,
	}
	if prev != nil {
		d, _ := b.Prev()
		r.nextAncestor = &d
	}

	// Rule 6 — a public block MUST only reference public blocks. The check
	// needs the referenced block, so it can only be made for refs the source
	// holds; a ref that is absent and never needed is reported as unchecked.
	if b.content.Type == TypePublic {
		for _, ref := range b.content.Refs {
			target, err := r.fetch(ref, true)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					report.warn(6, b.Digest(), "referenced block %s is not held by the source, so its type could not be checked", ref)
					continue
				}
				if errors.Is(err, ErrScanLimit) {
					return &RuleError{Rule: 4, Block: b.Digest(), Err: err}
				}
				return err
			}
			if target.content.Type == TypePrivate {
				return ruleErr(6, b, "refs entry %s is a private block; a public block must only reference public blocks", ref)
			}
		}
	}
	r.queue = slices.Clone(b.content.Refs)

	// Rule 4 — every entity digest an operation carries must be reachable, and
	// within this block only from an operation that precedes it.
	for i, op := range b.content.Ops {
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
type resolver struct {
	block  *Block
	src    Source
	limit  int
	report *Report

	defs  map[cid.Digest]record
	cache map[cid.Digest]*Block

	nextAncestor  *cid.Digest
	ancestorGap   *cid.Digest // the ancestor the source does not hold
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
	if r.ancestorGap != nil {
		return record{}, fmt.Errorf("entity %s is not reachable from this block; the author's chain is incomplete at block %s, which the source does not hold", d, r.ancestorGap)
	}
	return record{}, fmt.Errorf("entity %s is not reachable from this block, from an ancestor in the author's chain, or from any block in the refs graph", d)
}

// extendAncestors folds one more ancestor's operations into the definitions.
func (r *resolver) extendAncestors() error {
	d := *r.nextAncestor
	ancestor, err := r.fetch(d, false)
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
	if ancestor.content.Type == TypePrivate {
		if !r.privateWarned {
			r.report.warn(4, r.block.Digest(), "ancestor block %s is private, so the entities its operations define are not visible to this package and cannot satisfy a reference", d)
			r.privateWarned = true
		}
	}
	for _, op := range ancestor.content.Ops {
		r.define(op)
	}
	r.nextAncestor = ancestor.content.Prev
	return nil
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

	target, err := r.fetch(d, true)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// An unavailable CID provider is not itself a failure: another ref
			// may still define the digest. If none does, resolve reports the
			// unresolved digest.
			r.report.warn(4, r.block.Digest(), "referenced block %s is not held by the source", d)
			return nil
		}
		return err
	}
	if target.content.Type == TypePrivate {
		// The operations are encrypted; this package cannot read them, so the
		// block contributes no definitions. spec/05-processing-model.md,
		// "Undecryptable reference handling", makes that a validation error
		// once a digest actually needs it, which is what resolve reports.
		r.report.warn(4, r.block.Digest(), "referenced block %s is private; its operations are not visible to this package", d)
		return nil
	}
	for _, op := range target.content.Ops {
		r.define(op)
	}
	r.queue = append(r.queue, target.content.Refs...)
	return nil
}

// define records the entity an operation creates. The first definition wins;
// re-creating the same entity is idempotent, since the digest determines the
// content.
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

// fetch reads a block through the source and caches it. A foreign fetch — one
// that walks the refs graph rather than the author's own chain — counts
// against the scan limit the first time it happens, which is the count
// spec/05-processing-model.md, "Scan limit", bounds.
func (r *resolver) fetch(d cid.Digest, foreign bool) (*Block, error) {
	if b, ok := r.cache[d]; ok {
		return b, nil
	}
	if foreign && r.limit >= 0 && r.scanned >= r.limit {
		return nil, fmt.Errorf("%w of %d foreign block(s) was reached before every reference resolved", ErrScanLimit, r.limit)
	}
	b, err := r.src.Block(d)
	if err != nil {
		return nil, err
	}
	r.cache[d] = b
	if foreign {
		r.scanned++
	}
	return b, nil
}
