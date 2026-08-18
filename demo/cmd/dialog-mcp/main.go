// Command dialog-mcp exposes the grounding demo's Dialog chains to an AI
// assistant over the Model Context Protocol.
//
// This is the founding use case, wired up: an assistant asked about a European
// capital answers from content-addressed, author-attributed facts instead of
// from memory, cites the digest and the author of every claim it makes, reports
// a dispute as a dispute because L3 surfaces conflicts and resolves none of
// them, and can be shown that truth here is relative to a subscription set by
// changing the set mid-conversation.
//
// # Running it
//
//	go run ./cmd/dialog-mcp                      # the chains built into the binary
//	go run ./cmd/dialog-mcp -chains ./chains     # a chain directory on disk
//	DIALOG_MCP_CHAINS=./chains go run ./cmd/dialog-mcp
//
// The server speaks the MCP stdio transport, so it is started by its client and
// talks JSON-RPC over stdin and stdout. Nothing is ever written to stdout by
// this program itself — that would corrupt the protocol stream — and every
// diagnostic goes to stderr.
//
// The chains are embedded in the binary by default (the demo/chains package),
// which is why the command needs no working directory and no configuration to
// be useful. The -chains flag, or the DIALOG_MCP_CHAINS environment variable
// when the flag is absent, replaces them with a chain directory on disk: the
// one holding index.json. Whichever is used, the blocks take the full loading
// path — decoded, validated against the ten rules of spec/02-block-format.md,
// and ingested into L2 only if their chain validated — so a tampered directory
// fails to start rather than serving a lie.
//
// # Wiring it into a client
//
// An MCP client is configured with a command to run. For a client using the
// usual JSON configuration file:
//
//	{
//	  "mcpServers": {
//	    "dialog": { "command": "/path/to/dialog-mcp" }
//	  }
//	}
//
// # The tools
//
//	dialog_lookup         find entities by the words they contain
//	dialog_truth          the truth state of a molecule and the record behind it
//	dialog_conflicts      every disagreement the view surfaces, by kind
//	dialog_equivalents    the equivalence class of an entity
//	dialog_provenance     the L2 authorship records of an entity
//	dialog_subscriptions  read or replace the subscribed authors
//
// The subscription set is per process and outlives a call; see the Server type.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vrinek/Dialog/demo/chains"
	"github.com/vrinek/Dialog/demo/internal/replay"
)

// version is what the server reports as its implementation version. It is the
// demo's own, not the protocol's.
const version = "0.1.0"

// chainsEnv names the environment variable the chain directory can be given
// in, for a client that can set an environment but not an argument list.
const chainsEnv = "DIALOG_MCP_CHAINS"

func main() {
	dir := flag.String("chains", "", "chain directory to serve (the one holding index.json); "+
		"defaults to $"+chainsEnv+", and to the chains built into this binary")
	flag.Parse()

	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "dialog-mcp:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if dir == "" {
		dir = os.Getenv(chainsEnv)
	}
	node, source, err := load(dir)
	if err != nil {
		return err
	}
	s, err := NewServer(node)
	if err != nil {
		return err
	}

	// stderr, never stdout: stdout is the JSON-RPC stream.
	fmt.Fprintf(os.Stderr, "dialog-mcp: replayed %d blocks from %s over %d chains (%s); "+
		"L2 holds %d entities\n",
		node.BlockCount(), source, len(node.Chains), joinNames(node.Authors()), node.Graph.Len())

	m := mcp.NewServer(&mcp.Implementation{
		Name:    "dialog",
		Title:   "Dialog grounding demo",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: "This server answers from Dialog chains: a content-addressed, " +
			"author-attributed knowledge graph about European countries and their capitals. " +
			"Ground answers in it rather than in recall, and cite what it returns — every " +
			"response carries the digest of each entity and the names of the authors who " +
			"published it. Start with dialog_lookup to turn words into digests, then " +
			"dialog_truth for what the view holds about a molecule. Where the authors disagree, " +
			"say so and name both sides: Dialog surfaces conflicts and deliberately resolves " +
			"none of them, so an answer that picks a winner is the assistant's judgement and not " +
			"the data's. What is in the view depends on which authors are subscribed, which " +
			"dialog_subscriptions reports and can change.",
	})
	s.Register(m)

	// A client that has finished with the server closes the pipe, and Run
	// reports the shutdown that follows. That is the ordinary way this program
	// ends, not a failure, so it does not become a non-zero exit status.
	err = m.Run(context.Background(), &mcp.StdioTransport{})
	if isShutdown(err) {
		return nil
	}
	return err
}

// codeServerClosing is the JSON-RPC error code the SDK's transport layer
// returns when the connection is shutting down. The SDK exposes the wire error
// type (jsonrpc.Error) but not a sentinel for this code, so the code is what
// there is to match on.
const codeServerClosing = -32004

// isShutdown reports whether an error from Run is the connection ending rather
// than something going wrong.
func isShutdown(err error) bool {
	if err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, mcp.ErrConnectionClosed) ||
		errors.Is(err, context.Canceled) {
		return true
	}
	var wire *jsonrpc.Error
	return errors.As(err, &wire) && wire.Code == codeServerClosing
}

// load replays the chain directory, from disk if one was named and from the
// binary otherwise, and says which it was.
func load(dir string) (*replay.Node, string, error) {
	var (
		fsys   fs.FS = chains.FS
		source       = "the chains built into this binary"
	)
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, "", fmt.Errorf("reading the chain directory %s: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, "", fmt.Errorf("%s is not a directory; -chains takes the directory "+
				"holding index.json", dir)
		}
		fsys, source = os.DirFS(dir), dir
	}
	node, err := replay.Load(fsys)
	if err != nil {
		return nil, "", errors.New("the chains did not validate, so nothing is being served: " +
			err.Error())
	}
	return node, source, nil
}
