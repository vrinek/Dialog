#!/usr/bin/env bash
#
# Run Dialog's two implementations against each other over the transport profile
# of spec/07-transport.md, in both directions, and assert that they agree.
#
# For every scenario (a set of chain directories) and every server layout (whose
# implementation is behind each of those directories), the harness starts the
# servers, runs *both* clients against them, checks each client's summary against
# the expectation generated from the fixture bytes, and checks the two clients'
# summaries against each other. The crossings — Go servers with the TypeScript
# client, and the reverse — are what this directory exists for; the two
# same-implementation runs cost one process each and tell a real interop failure
# apart from a plain bug.
#
# See interop/README.md for what is asserted and why the summary document holds
# what it holds.
#
# Usage: interop/run.sh [-keep DIR]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEEP=""
while [ $# -gt 0 ]; do
  case "$1" in
    -keep) KEEP="$2"; shift 2 ;;
    -h|--help) sed -n '2,20p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "interop: unknown argument $1" >&2; exit 2 ;;
  esac
done

if [ -n "$KEEP" ]; then
  mkdir -p "$KEEP"
  WORK="$(cd "$KEEP" && pwd)"
else
  WORK="$(mktemp -d)"
fi

PIDS=()
cleanup() {
  for pid in ${PIDS+"${PIDS[@]}"}; do
    kill "$pid" 2>/dev/null || true
  done
  for pid in ${PIDS+"${PIDS[@]}"}; do
    wait "$pid" 2>/dev/null || true
  done
  [ -n "$KEEP" ] || rm -rf "$WORK"
}
trap cleanup EXIT

CHECKS=0
pass() {
  CHECKS=$((CHECKS + 1))
  echo "  ok   $*"
}

# ---------------------------------------------------------------------------
# Build both sides.
# ---------------------------------------------------------------------------

echo "interop: building"
BIN="$WORK/bin"
mkdir -p "$BIN"
(cd "$ROOT/go" && go build -o "$BIN/dialog-serve" ./cmd/dialog-serve && go build -o "$BIN/dialog-sync" ./cmd/dialog-sync)
if [ ! -d "$ROOT/ts/node_modules" ]; then
  (cd "$ROOT/ts" && npm ci)
fi

# The fixtures and the expectations are generated and never hand-edited; a run
# against stale ones would assert against the wrong digests.
(cd "$ROOT/go" && go run ./cmd/geninterop -check)

# ---------------------------------------------------------------------------
# Starting a server and running a client, for either implementation.
# ---------------------------------------------------------------------------

# serve IMPL DIR TAG — start a server and set SERVE_URL to the base URL it
# announces. It sets a variable rather than printing one because a command
# substitution runs in a subshell, and the pid of a server started there would
# never reach the list this script kills on exit.
SERVE_URL=""
serve() {
  local impl="$1" dir="$2" tag="$3"
  local out="$WORK/$tag.startup" err="$WORK/$tag.stderr"
  : >"$out"
  case "$impl" in
    go) "$BIN/dialog-serve" -chains "$dir" >"$out" 2>"$err" & ;;
    ts) node "$ROOT/ts/scripts/serve.ts" -chains "$dir" >"$out" 2>"$err" & ;;
    *) echo "interop: no such implementation: $impl" >&2; exit 2 ;;
  esac
  PIDS+=($!)
  if ! SERVE_URL="$(node "$ROOT/interop/harness.mjs" wait-url "$out")"; then
    echo "interop: the $impl server over $dir did not start" >&2
    cat "$err" >&2
    exit 1
  fi
}

# sync IMPL FROM OUT AUTHORS URL… — run a client and write its summary to OUT.
#
# FROM is where a source not asked before is asked from: "genesis", or "held" —
# the position the client's own chain already reaches. The profile permits both
# and prefers the genesis position (spec/07-transport.md, "First contact with a
# source"), and they are not the same exchange: from
# the genesis position the divergence arrives in the range itself, and from the
# held position the source answers an empty range and a tip the client cannot
# reach, which is what the backward walk is for.
sync_from() {
  local impl="$1" from="$2" out="$3" authors="$4"
  shift 4
  local args=()
  for url in "$@"; do args+=(-source "$url"); done
  case "$impl" in
    go) "$BIN/dialog-sync" "${args[@]}" -authors "$authors" -from "$from" >"$out" ;;
    ts) node "$ROOT/ts/scripts/sync.ts" "${args[@]}" -authors "$authors" -from "$from" >"$out" ;;
    *) echo "interop: no such implementation: $impl" >&2; exit 2 ;;
  esac
}

# ---------------------------------------------------------------------------
# The scenarios: a name, an expectation, and the directories one server each is
# started over. The order of the directories is the order the client is given
# the sources in, which is the order advertised_tips is reported in.
# ---------------------------------------------------------------------------

scenario_dirs() {
  case "$1" in
    demo) echo "$ROOT/demo/chains" ;;
    fork) echo "$ROOT/interop/fixtures/fork-a $ROOT/interop/fixtures/fork-b" ;;
    genesis) echo "$ROOT/interop/fixtures/genesis-a $ROOT/interop/fixtures/genesis-b" ;;
    *) echo "interop: no such scenario: $1" >&2; exit 2 ;;
  esac
}

# The server layouts: which implementation is behind each directory. "mixed" is
# one of each, and needs at least two servers to mean anything.
scenario_layouts() {
  case "$1" in
    demo) echo "go ts" ;;
    *) echo "go ts mixed" ;;
  esac
}

layout_impl() { # LAYOUT INDEX
  case "$1" in
    go|ts) echo "$1" ;;
    mixed) [ "$2" -eq 0 ] && echo go || echo ts ;;
  esac
}

# How the backward walk after an advertised tip must end in each scenario, once
# the client is asking from the position it already holds. Empty where there is
# nothing to pursue: one source cannot disagree with itself.
scenario_pursuit() {
  case "$1" in
    demo) echo "" ;;
    fork) echo "held" ;;
    genesis) echo "genesis" ;;
  esac
}

for scenario in demo fork genesis; do
  expected="$ROOT/interop/expected/$scenario.json"
  authors="$(node "$ROOT/interop/harness.mjs" authors "$expected")"
  read -r -a dirs <<<"$(scenario_dirs "$scenario")"

  for layout in $(scenario_layouts "$scenario"); do
    echo "interop: $scenario, servers=$layout"
    urls=()
    for i in "${!dirs[@]}"; do
      impl="$(layout_impl "$layout" "$i")"
      serve "$impl" "${dirs[$i]}" "$scenario.$layout.$i"
      urls+=("$SERVE_URL")
    done

    for client in go ts; do
      out="$WORK/$scenario.$layout.$client.json"
      sync_from "$client" genesis "$out" "$authors" "${urls[@]}"
      node "$ROOT/interop/harness.mjs" compare "$expected" "$out" \
        "the expectation" "the $client client against $layout servers"
      pass "$scenario: the $client client against $layout servers holds what it must"
    done

    # The assertion this directory exists for: the two clients, over the same
    # servers, produced the same document.
    node "$ROOT/interop/harness.mjs" compare \
      "$WORK/$scenario.$layout.go.json" "$WORK/$scenario.$layout.ts.json" \
      "the Go client" "the TypeScript client"
    pass "$scenario: the two clients agree over $layout servers"

    # The same servers again, asked from the position each client already holds.
    # A source on another branch then answers an empty range and a tip the client
    # cannot reach, which is the case "Pursuing an advertised tip" is written
    # for: the divergence arrives by the backward walk instead of by the range.
    # The blocks that end up in the store are the same either way — that is the
    # assertion — and the walk's end is the third one.
    want_end="$(scenario_pursuit "$scenario")"
    for client in go ts; do
      out="$WORK/$scenario.$layout.$client.held.json"
      sync_from "$client" held "$out" "$authors" "${urls[@]}"
      node "$ROOT/interop/harness.mjs" compare-store "$expected" "$out" \
        "the expectation" "the $client client asking $layout servers from where it is"
      pass "$scenario: the $client client asking $layout servers from where it is holds the same blocks"
      if [ -n "$want_end" ]; then
        node "$ROOT/interop/harness.mjs" expect-pursuit "$out" "$want_end"
        pass "$scenario: the $client client's pursuit of a $layout server's tip ended as $want_end"
      fi
    done

    node "$ROOT/interop/harness.mjs" compare \
      "$WORK/$scenario.$layout.go.held.json" "$WORK/$scenario.$layout.ts.held.json" \
      "the Go client" "the TypeScript client"
    pass "$scenario: the two clients agree over $layout servers, pursuits and all"
  done

  # And across the crossings: the Go client against TypeScript servers, and the
  # TypeScript client against Go servers, are the two directions of the same
  # exchange and must not differ either.
  node "$ROOT/interop/harness.mjs" compare \
    "$WORK/$scenario.go.ts.json" "$WORK/$scenario.ts.go.json" \
    "TypeScript client ← Go servers" "Go client ← TypeScript servers"
  pass "$scenario: the two directions agree"
  node "$ROOT/interop/harness.mjs" compare \
    "$WORK/$scenario.go.ts.held.json" "$WORK/$scenario.ts.go.held.json" \
    "TypeScript client pursuing a Go server" "Go client pursuing a TypeScript server"
  pass "$scenario: the two directions agree when each pursues the other's tip"

  # The servers themselves, before any client asked them anything: over the same
  # directory they must report the same blocks and the same tips, because a tip
  # is the end of a forward walk and which branch of a fork it follows is fixed
  # by the profile rather than left to the server.
  for i in "${!dirs[@]}"; do
    node "$ROOT/interop/harness.mjs" compare-startup \
      "$WORK/$scenario.go.$i.startup" "$WORK/$scenario.ts.$i.startup" \
      "the Go server over $(basename "${dirs[$i]}")" "the TypeScript server over $(basename "${dirs[$i]}")"
    pass "$scenario: the two servers over $(basename "${dirs[$i]}") report the same chains"
  done
done

echo
echo "interop: $CHECKS checks passed"
