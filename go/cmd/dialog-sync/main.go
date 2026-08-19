// Command dialog-sync obtains author chains from one or more sources over the
// transport profile of spec/07-transport.md and reports what it found.
//
// It is the client side of dialog-serve, and the Go half of the interop harness
// in interop/: it builds a [transport.Syncer] over a fresh
// [block.ValidatingStore], syncs each named author from every named source, and
// writes a JSON summary of the resulting store to stdout. Every block is
// validated on receipt, every response is re-hashed, and nothing the sources say
// about a block is believed — which is the whole point of the exercise.
//
// Usage:
//
//	dialog-sync -source URL [-source URL …] -authors KEY[,KEY…]
//
//	-source       a base URL, repeatable and also comma-separated. More than one
//	              is the profile's SHOULD, and the reason is detection rather
//	              than redundancy: a node that only ever hears one version of a
//	              chain from one source satisfies validation rule 9 vacuously.
//	-authors      author keys in the canonical text form of
//	              spec/03-encoding.md, comma-separated. The order is the order
//	              they are synced in, which matters where one chain's blocks
//	              reference another's.
//	-limit        the limit asked of each range request; 0 lets the source choose
//	-max-pursuit  the client's own bound on the backward walk after an advertised
//	              tip; 0 means the package default
//	-from         where a source not asked before is asked from: "genesis", the
//	              default, or "held" — the position this client's own chain
//	              already reaches. The profile permits both and says which is
//	              neither (todo 099); they differ in traffic and in whether the
//	              divergence arrives as a range or as a pursuit
//	-timeout      the whole run's deadline
//
// The summary's shape is documented in interop/README.md, which is also where
// the reason it contains no request counts, no source URLs and no timings is
// written down: two implementations syncing the same blocks from the same
// servers must produce the identical document, so it holds facts about the
// chains and about this client's store, and nothing about how either side is
// built.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/internal/chains"
	"github.com/vrinek/Dialog/go/internal/interop"
	"github.com/vrinek/Dialog/go/transport"
)

func main() {
	var sources multiFlag
	flag.Var(&sources, "source", "base URL of a source; repeatable, and also comma-separated")
	authors := flag.String("authors", "", "author keys in canonical text form, comma-separated, in sync order")
	limit := flag.Int("limit", 0, "limit asked of each range request; 0 lets the source choose")
	maxPursuit := flag.Int("max-pursuit", 0, "bound on the backward walk after an advertised tip; 0 is the default")
	from := flag.String("from", "genesis", `where a source not asked before is asked from: "genesis" or "held"`)
	timeout := flag.Duration("timeout", time.Minute, "deadline for the whole run")
	flag.Parse()

	if err := run(sources, *authors, *limit, *maxPursuit, *from, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "dialog-sync:", err)
		os.Exit(1)
	}
}

// A multiFlag is a repeatable string flag whose values may also be given
// comma-separated, so that a shell script can pass either shape.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*m = append(*m, part)
		}
	}
	return nil
}

func run(sources multiFlag, authors string, limit, maxPursuit int, from string, timeout time.Duration) error {
	if len(sources) == 0 {
		return errors.New("at least one -source is required")
	}
	if from != "genesis" && from != "held" {
		return fmt.Errorf("-from is %q, want \"genesis\" or \"held\"", from)
	}
	pubs, err := parseAuthors(authors)
	if err != nil {
		return err
	}
	clients := make([]transport.Source, 0, len(sources))
	for _, base := range sources {
		c, err := transport.NewClient(base, nil)
		if err != nil {
			return err
		}
		clients = append(clients, c)
	}

	store := block.NewValidatingStore(nil)
	syncer := transport.NewSyncer(store, clients...)
	syncer.PageLimit, syncer.MaxPursuit = limit, maxPursuit
	syncer.AskFromHeldPosition = from == "held"

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out := interop.New()
	for _, pub := range pubs {
		result, err := syncer.SyncChain(ctx, pub)
		if err != nil {
			return err
		}
		entry, err := describe(store, pub, result)
		if err != nil {
			return err
		}
		out.Add(entry)
	}

	body, err := out.JSON()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(body)
	return err
}

// parseAuthors reads the comma-separated author keys, in the order given, which
// is the order they are synced in.
func parseAuthors(list string) ([]ed25519.PublicKey, error) {
	var pubs []ed25519.PublicKey
	for _, text := range strings.Split(list, ",") {
		if text = strings.TrimSpace(text); text == "" {
			continue
		}
		raw, err := cid.ParseAuthorKeyText(text)
		if err != nil {
			return nil, fmt.Errorf("author %q: %w", text, err)
		}
		pubs = append(pubs, ed25519.PublicKey(raw))
	}
	if len(pubs) == 0 {
		return nil, errors.New("-authors is required, as one or more canonical author keys")
	}
	return pubs, nil
}

// describe reads one chain out of the store after the sync.
func describe(store *block.ValidatingStore, pub ed25519.PublicKey, result *transport.ChainSync) (interop.Chain, error) {
	author, err := cid.AuthorKeyText(pub)
	if err != nil {
		return interop.Chain{}, err
	}
	entry := interop.Chain{
		Author:         author,
		AdvertisedTips: []*string{},
		Chain:          []string{},
		Blocks:         []string{},
		Pursuits:       []interop.Pursuit{},
		Forks:          []interop.Fork{},
		Rejected:       len(result.Rejected),
	}
	for i, src := range result.Sources {
		entry.AdvertisedTips = append(entry.AdvertisedTips, interop.Text(src.Tip))
		if src.PursuitEnd != transport.PursuitNone && src.Tip != nil {
			entry.Pursuits = append(entry.Pursuits, interop.Pursuit{
				Source:  i,
				Tip:     src.Tip.CID().String(),
				End:     string(src.PursuitEnd),
				Fetched: src.Pursued,
			})
		}
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
			// The store answered for a digest it does not hold, which the walk
			// that produced this digest cannot produce. Nothing to count.
		}
	}
	for _, f := range result.Forks {
		siblings := make([]string, 0, len(f.Blocks))
		for _, d := range f.Blocks {
			siblings = append(siblings, d.CID().String())
		}
		entry.Forks = append(entry.Forks, interop.Fork{Prev: interop.Text(f.Prev), Siblings: siblings})
	}
	return entry, nil
}
