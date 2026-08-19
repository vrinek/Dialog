// Package interop defines the summary document the interop harness compares.
//
// Two implementations syncing the same blocks from the same servers must
// produce the identical document, so every field here is a fact about the
// blocks or about the client's own store: no request counts, no source URLs, no
// timings, no implementation name. What is left is what conformance means —
// which blocks the client ended up holding, in what chain order, with what
// verdicts, and which positions it holds more than one block at.
//
// The shape is documented for readers of other implementations in
// interop/README.md; this file is its Go definition, shared by the dialog-sync
// binary that produces one by syncing and the geninterop generator that
// computes the expected one from the fixtures alone.
package interop

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/internal/chains"
)

// A Summary is one sync run's report.
type Summary struct {
	Chains []Chain `json:"chains"`
	Totals Totals  `json:"totals"`
}

// A Chain is one author's chain as the client ended up holding it.
type Chain struct {
	// Author is the key in the canonical text form of spec/03-encoding.md.
	Author string `json:"author"`
	// AdvertisedTips is what each source claimed, in the order the sources were
	// given, and null where a source claimed none. It is a claim and not
	// evidence: two sources differing means one is behind, or they are on
	// different branches, or one is withholding, and the first and the third are
	// indistinguishable.
	AdvertisedTips []*string `json:"advertised_tips"`
	// Tip is the client's own constructive tip after the sync: the end of the
	// forward walk from the genesis position, lowest digest at a fork.
	Tip *string `json:"tip"`
	// Chain is that walk, genesis first.
	Chain []string `json:"chain"`
	// Blocks is every block of this author the store holds that is reachable
	// from the genesis position, ascending by digest — every branch of every
	// fork, not only the one the walk took.
	Blocks []string `json:"blocks"`
	// Accepted and Held are the verdicts over those blocks. Held is *stored but
	// unvalidated*: neither accepted nor refused, and another source may settle
	// it (spec/05-processing-model.md, "Block reception").
	Accepted int `json:"accepted"`
	Held     int `json:"held"`
	// Rejected is how many blocks a source handed over that a validation rule
	// showed wrong. They are not stored, so they appear in no other field.
	Rejected int `json:"rejected"`
	// Pursuits are the backward walks after a tip a source advertised and the
	// client did not hold, one entry per source that needed one.
	Pursuits []Pursuit `json:"pursuits"`
	// Forks are the positions the client holds more than one block of this
	// author at, ascending by the lowest digest in each — validation rule 9's
	// condition, in the client's own store, with no source having admitted to
	// anything.
	Forks []Fork `json:"forks"`
}

// A Pursuit is one backward walk after a tip a source advertised.
type Pursuit struct {
	// Source is the index of the source, in the order the sources were given.
	Source int `json:"source"`
	// Tip is the tip that was pursued.
	Tip string `json:"tip"`
	// End is how the walk ended: "held", "genesis" or "failed", the three names
	// spec/07-transport.md settles on.
	End string `json:"end"`
	// Fetched is how many blocks the walk obtained.
	Fetched int `json:"fetched"`
}

// A Fork is one position and every block claiming it.
type Fork struct {
	// Prev is the predecessor claimed, or null for the genesis position: two
	// genesis blocks are a fork in the strict sense of validation rule 9.
	Prev *string `json:"prev"`
	// Siblings are the blocks at that position, ascending by digest.
	Siblings []string `json:"siblings"`
}

// Totals are the chain summaries added up, so that a harness can assert the
// headline numbers without walking the document.
type Totals struct {
	Chains   int `json:"chains"`
	Blocks   int `json:"blocks"`
	Accepted int `json:"accepted"`
	Held     int `json:"held"`
	Rejected int `json:"rejected"`
	Forks    int `json:"forks"`
}

// New returns an empty summary, with every list present rather than null: a
// reader never has to tell absent from empty, and two documents differing only
// in that would not compare equal.
func New() *Summary { return &Summary{Chains: []Chain{}} }

// Add appends a chain and updates the totals.
func (s *Summary) Add(c Chain) {
	s.Chains = append(s.Chains, c)
	s.Totals.Chains++
	s.Totals.Blocks += len(c.Blocks)
	s.Totals.Accepted += c.Accepted
	s.Totals.Held += c.Held
	s.Totals.Rejected += c.Rejected
	s.Totals.Forks += len(c.Forks)
}

// JSON renders the document as the harness compares it: two-space indentation
// and a trailing newline, so that a regenerated expectation is byte-identical to
// the committed one and a diff is readable.
func (s *Summary) JSON() ([]byte, error) {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("interop: encoding the summary: %w", err)
	}
	return append(body, '\n'), nil
}

// Expect computes the summary a conforming client MUST produce after syncing
// the given authors, in the given order, from the given sources.
//
// It is analytic rather than a recording: each source's advertised tip is the
// constructive walk over the blocks that source holds, and the client's chain,
// blocks, verdicts and forks are the same questions asked of the union — because
// a client that obtains every chain from every source ends up holding the union,
// and a client that does not is the thing being tested. Nothing in it comes from
// running a client, which is what lets it be the expectation both clients are
// measured against.
//
// Pursuits are empty by construction: a pursuit needs the client to already hold
// a position the source's walk does not pass through, which a client meeting
// these sources for the first time does not have. See interop/README.md, "What
// this does not prove", and todo 098.
func Expect(sources [][]*block.Block, pubs []ed25519.PublicKey) (*Summary, error) {
	union := block.NewMemStore()
	per := make([]*block.MemStore, 0, len(sources))
	for _, held := range sources {
		mem := block.NewMemStore()
		for _, b := range held {
			if err := addFlagging(mem, b); err != nil {
				return nil, err
			}
			if err := addFlagging(union, b); err != nil {
				return nil, err
			}
		}
		per = append(per, mem)
	}

	// The union, validated in the order a client would offer it: author by
	// author in the order asked for, and within an author every block after the
	// one it names as its predecessor.
	store := block.NewValidatingStore(nil)
	for _, pub := range pubs {
		for _, d := range chains.Reachable(union, pub) {
			b, err := union.Block(d)
			if err != nil {
				return nil, fmt.Errorf("interop: %w", err)
			}
			if _, err := store.Add(b); err != nil {
				return nil, fmt.Errorf("interop: the fixtures hold an invalid block %s: %w", d, err)
			}
		}
	}

	out := New()
	for _, pub := range pubs {
		author, err := cid.AuthorKeyText(pub)
		if err != nil {
			return nil, fmt.Errorf("interop: %w", err)
		}
		entry := Chain{
			Author:         author,
			AdvertisedTips: []*string{},
			Chain:          []string{},
			Blocks:         []string{},
			Pursuits:       []Pursuit{},
			Forks:          []Fork{},
		}
		for _, mem := range per {
			tip, ok := chains.Tip(mem, pub)
			if !ok {
				entry.AdvertisedTips = append(entry.AdvertisedTips, nil)
				continue
			}
			entry.AdvertisedTips = append(entry.AdvertisedTips, Text(&tip))
		}
		for _, d := range chains.Walk(store, pub) {
			entry.Chain = append(entry.Chain, d.CID().String())
		}
		if n := len(entry.Chain); n > 0 {
			entry.Tip = &entry.Chain[n-1]
		}
		for _, d := range chains.All(store, pub) {
			entry.Blocks = append(entry.Blocks, d.CID().String())
			switch verdict, _ := store.Verdict(d); verdict {
			case block.VerdictValid:
				entry.Accepted++
			case block.VerdictUnvalidated:
				entry.Held++
			case block.VerdictUnknown:
				return nil, fmt.Errorf("interop: %s is reachable and not held", d)
			}
		}
		for _, f := range store.Forks() {
			if !f.Pub.Equal(pub) {
				continue
			}
			siblings := make([]string, 0, len(f.Blocks))
			for _, d := range f.Blocks {
				siblings = append(siblings, d.CID().String())
			}
			entry.Forks = append(entry.Forks, Fork{Prev: Text(f.Prev), Siblings: siblings})
		}
		out.Add(entry)
	}
	return out, nil
}

// addFlagging stores a block, letting a fork through: a set of blocks that holds
// one holds it on purpose, and the store's policy is accept-and-flag.
func addFlagging(mem *block.MemStore, b *block.Block) error {
	err := mem.Add(b)
	var fork *block.ForkError
	if err == nil || errors.As(err, &fork) {
		return nil
	}
	return fmt.Errorf("interop: storing %s: %w", b.Digest(), err)
}

// Text renders a digest as its CID text form, or null where there is none.
func Text(d *cid.Digest) *string {
	if d == nil {
		return nil
	}
	s := d.CID().String()
	return &s
}
