// Package chains embeds the demo's committed author chains so that a program
// can replay them without knowing where the repository is checked out.
//
// The bytes are the ones cmd/genchains produced; see the chainfile package for
// the layout and internal/replay for the loader that turns them back into a
// node's L1 store, L2 graph and L3 views. Nothing here interprets them: this
// package is a file system and nothing else.
package chains

import "embed"

// FS holds the chain directory: index.json and one file per block.
//
//go:embed index.json */*.block
var FS embed.FS
