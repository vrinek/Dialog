package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A Receipt is what a source did with each block of an announce: the three-way
// report of spec/07-transport.md, "announce". Every submitted block appears in
// exactly one of the three.
//
// A receipt carries no authority in either direction. The announcer asserted
// nothing a block does not already say, and the source endorses nothing by
// accepting one — it is the same bytes moving the other way. Acceptance means
// only that the source validated the block and stored it.
type Receipt struct {
	// Accepted are the blocks the source validated and stored, and the blocks it
	// already held as valid.
	Accepted []cid.Digest
	// Held are the blocks the source is keeping as *stored but unvalidated*
	// pending their ancestry (spec/05-processing-model.md, "Block reception").
	// They are neither accepted nor refused: the source has not decided.
	Held []cid.Digest
	// Rejected names each block the source refused and why, in prose meant for a
	// person. It is a slice rather than a map so that the order a receipt is
	// built in is the order it is read in.
	Rejected []Rejection
	// Deferred reports that the source answered 202: the announce was taken for
	// later processing and the receipt is incomplete or absent. It is a fact
	// about the response and not part of the receipt's wire shape, so it is set
	// by the client and never encoded.
	Deferred bool
}

// A Rejection is one refused block and the reason, which is a diagnostic and not
// an interface: a client MUST NOT parse it.
type Rejection struct {
	Digest cid.Digest
	Reason string
}

// Len returns the number of blocks the receipt accounts for.
func (r *Receipt) Len() int { return len(r.Accepted) + len(r.Held) + len(r.Rejected) }

// receiptJSON is the wire shape of a receipt: one JSON object mapping each
// submitted block's CID to what became of it (spec/07-transport.md, "Bodies and
// content types"). All three members are always present, empty where nothing
// fell into them, so a reader never has to tell absent from empty.
type receiptJSON struct {
	Accepted []string          `json:"accepted"`
	Held     []string          `json:"held"`
	Rejected map[string]string `json:"rejected"`
}

// MarshalJSON renders the receipt in the profile's shape.
func (r *Receipt) MarshalJSON() ([]byte, error) {
	w := receiptJSON{
		Accepted: cidStrings(r.Accepted),
		Held:     cidStrings(r.Held),
		Rejected: make(map[string]string, len(r.Rejected)),
	}
	for _, rej := range r.Rejected {
		w.Rejected[rej.Digest.CID().String()] = rej.Reason
	}
	body, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("transport: encoding the announce receipt: %w", err)
	}
	return body, nil
}

// UnmarshalJSON reads a receipt a server sent.
func (r *Receipt) UnmarshalJSON(data []byte) error {
	var w receiptJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("transport: parsing the announce receipt: %w", err)
	}
	accepted, err := parseCIDs(w.Accepted, "accepted")
	if err != nil {
		return err
	}
	held, err := parseCIDs(w.Held, "held")
	if err != nil {
		return err
	}
	*r = Receipt{Accepted: accepted, Held: held, Deferred: r.Deferred}
	// The rejected member is a JSON object, and a map has no order. Sorting the
	// keys is what makes reading a receipt deterministic, which matters because
	// a caller may log or compare one.
	for _, key := range slices.Sorted(maps.Keys(w.Rejected)) {
		c, err := cid.ParseCIDString(key)
		if err != nil {
			return fmt.Errorf("transport: the receipt's rejected member is keyed by %q, which is not a CID: %w", key, err)
		}
		r.Rejected = append(r.Rejected, Rejection{Digest: c.Digest(), Reason: w.Rejected[key]})
	}
	return nil
}

func cidStrings(digests []cid.Digest) []string {
	out := make([]string, 0, len(digests))
	for _, d := range digests {
		out = append(out, d.CID().String())
	}
	return out
}

func parseCIDs(texts []string, member string) ([]cid.Digest, error) {
	out := make([]cid.Digest, 0, len(texts))
	for _, s := range texts {
		c, err := cid.ParseCIDString(s)
		if err != nil {
			return nil, fmt.Errorf("transport: the receipt's %s member holds %q, which is not a CID: %w", member, s, err)
		}
		out = append(out, c.Digest())
	}
	return out, nil
}

// An Announcer is a source that takes blocks in — the one operation that moves
// blocks toward a source rather than away from it.
//
// It is optional: a [Server] built without one is a read-only mirror, which the
// profile makes conforming (spec/07-transport.md, "The six operations").
//
// An implementation MUST validate every block before storing it, exactly as it
// would a block from any other origin, and MUST NOT store as valid a block whose
// predecessor it does not hold and has not validated. It MAY refuse an announce
// entirely for reasons that are its own policy — quota, rate, acquaintance,
// disk — by returning an error, which the server reports as a refusal of the
// request rather than of any one block.
type Announcer interface {
	// Announce offers a block sequence and reports what became of each block.
	// The blocks of one author arrive in chain order.
	Announce(ctx context.Context, blocks []*block.Block) (*Receipt, error)
}

// StoreAnnouncer makes a [block.ValidatingStore] the sink of an announce
// endpoint: every block is offered to the store, which validates it on arrival
// and records the verdict, and the receipt is that verdict read back.
//
// The two passes are deliberate. Blocks are offered first, in the order they
// arrived, and the dispositions are read afterwards — because a block held as
// undecided is settled by the arrival of the block it was waiting for, which may
// be later in the same sequence. A receipt built block by block would report a
// block as held that the store went on to accept before the response was even
// written.
func StoreAnnouncer(store *block.ValidatingStore) Announcer { return &storeAnnouncer{store: store} }

type storeAnnouncer struct{ store *block.ValidatingStore }

func (a *storeAnnouncer) Announce(ctx context.Context, blocks []*block.Block) (*Receipt, error) {
	rejected := make(map[cid.Digest]string, 0)
	for _, b := range blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := a.store.Add(b); err != nil {
			// An invalid block is refused and not stored. The reason is the
			// rule that failed, in the words the validator produced, which is
			// what the profile asks a receipt to carry.
			rejected[b.Digest()] = err.Error()
		}
	}
	receipt := &Receipt{}
	for _, b := range blocks {
		d := b.Digest()
		if reason, refused := rejected[d]; refused {
			receipt.Rejected = append(receipt.Rejected, Rejection{Digest: d, Reason: reason})
			continue
		}
		switch verdict, _ := a.store.Verdict(d); verdict {
		case block.VerdictValid:
			receipt.Accepted = append(receipt.Accepted, d)
		case block.VerdictUnvalidated:
			receipt.Held = append(receipt.Held, d)
		case block.VerdictUnknown:
			// The store neither refused it nor holds it, which no path of
			// ValidatingStore.Add produces. Reporting it as rejected is the
			// honest answer: every submitted block MUST appear in exactly one
			// of the three members.
			receipt.Rejected = append(receipt.Rejected, Rejection{Digest: d, Reason: "the source did not retain this block"})
		}
	}
	return receipt, nil
}
