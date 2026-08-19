// Command dialog-serve serves a directory of Dialog blocks over the transport
// profile of spec/07-transport.md.
//
// It is the smallest conforming server there is: it loads every .block file
// under a directory into a [block.ValidatingStore], mounts
// [transport.NewServer] over it, and answers the profile's read operations at
// the usual base URL. It stores nothing, remembers nothing between runs, and
// requires no client to identify itself, which server rule 5 forbids anyway.
//
// Usage:
//
//	dialog-serve -chains DIR [-addr HOST:PORT] [-prefix PATH] [-announce]
//
//	-chains    a directory of .block files, searched recursively (required)
//	-addr      the address to listen on; the default asks the kernel for a
//	           free port on the loopback interface
//	-prefix    the path prefix the operations are mounted under, defaulting to
//	           the profile's /dialog/v1
//	-announce  also serve the profile's one write operation, which is optional:
//	           a read-only mirror is conforming, and is the default
//
// On startup it writes one line of JSON to stdout and flushes it, so that a
// script can learn the address the kernel gave it and the chains it is serving
// without parsing prose or guessing a port:
//
//	{"addr":"127.0.0.1:41751","base_url":"http://127.0.0.1:41751/dialog/v1",
//	 "blocks":14,"chains":[{"author":"b5ua…","tip":"bafyrei…","blocks":6}]}
//
// Then it serves until it is interrupted. The tip in that line is the profile's
// own, constructive tip — the end of the forward walk from the genesis position
// — and is null for an author whose genesis block this directory does not hold,
// because such a chain has no tip this server could serve a range to.
//
// The blocks are validated as they are loaded, and a block whose predecessor or
// whose refs the directory does not hold lands *stored but unvalidated*. It is
// served anyway: a source serves what it holds, whatever verdict it has reached
// about it, and withholding the one block a client cannot yet judge is exactly
// the omission that hides a fork (spec/07-transport.md, "Server rules", rule 7).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/internal/chains"
	"github.com/vrinek/Dialog/go/transport"
)

func main() {
	dir := flag.String("chains", "", "directory of .block files to serve, searched recursively")
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on; port 0 asks the kernel for a free one")
	prefix := flag.String("prefix", transport.DefaultPrefix, "path prefix the operations are mounted under")
	announce := flag.Bool("announce", false, "also serve the announce operation; a read-only mirror is the default")
	flag.Parse()

	if err := run(*dir, *addr, *prefix, *announce); err != nil {
		fmt.Fprintln(os.Stderr, "dialog-serve:", err)
		os.Exit(1)
	}
}

// startup is the one line of JSON the server writes before it begins serving.
type startup struct {
	Addr    string       `json:"addr"`
	BaseURL string       `json:"base_url"`
	Blocks  int          `json:"blocks"`
	Chains  []chainStart `json:"chains"`
}

// A chainStart is one author this server holds blocks of.
type chainStart struct {
	// Author is the author's key in the canonical text form.
	Author string `json:"author"`
	// Tip is the constructive tip, or null where the walk from the genesis
	// position reaches nothing.
	Tip *string `json:"tip"`
	// Blocks is how many blocks of this author the directory held.
	Blocks int `json:"blocks"`
}

func run(dir, addr, prefix string, announce bool) error {
	if dir == "" {
		return errors.New("-chains is required")
	}
	loaded, err := chains.Load(dir)
	if err != nil {
		return err
	}
	store := block.NewValidatingStore(nil)
	for _, b := range loaded {
		// An invalid block is refused and the loader says so; a block whose
		// ancestry has not been read yet is held, and settled when it arrives.
		if _, err := store.Add(b); err != nil {
			var fork *block.ForkError
			if !errors.As(err, &fork) {
				return fmt.Errorf("loading %s: %w", b.Digest(), err)
			}
		}
	}

	cfg := transport.ServerConfig{Store: store, Prefix: prefix}
	if announce {
		cfg.Announce = transport.StoreAnnouncer(store)
	}
	handler, err := transport.NewServer(cfg)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	line, err := json.Marshal(describe(ln.Addr().String(), prefix, loaded, store))
	if err != nil {
		return fmt.Errorf("describing the server: %w", err)
	}
	fmt.Println(string(line))

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

// describe builds the startup line: what this process is serving, and where.
func describe(addr, prefix string, loaded []*block.Block, store *block.ValidatingStore) startup {
	out := startup{Addr: addr, BaseURL: "http://" + addr + prefix, Chains: []chainStart{}}
	// One file per block is the convention, not a guarantee: count each digest
	// once, and only where the store took it.
	unique := make([]*block.Block, 0, len(loaded))
	seen := make(map[cid.Digest]bool, len(loaded))
	for _, b := range loaded {
		if !seen[b.Digest()] && store.Has(b.Digest()) {
			seen[b.Digest()] = true
			unique = append(unique, b)
		}
	}
	out.Blocks = len(unique)
	for _, pub := range chains.Authors(unique) {
		text, err := cid.AuthorKeyText(pub)
		if err != nil {
			continue
		}
		entry := chainStart{Author: text}
		for _, b := range unique {
			if b.PublicKey().Equal(pub) {
				entry.Blocks++
			}
		}
		if tip, ok := chains.Tip(store, pub); ok {
			s := tip.CID().String()
			entry.Tip = &s
		}
		out.Chains = append(out.Chains, entry)
	}
	return out
}
