#!/usr/bin/env node
// The parts of the interop harness that are easier to write than to shell out
// to: waiting for a server's startup line, reading the authors out of an
// expectation, and comparing two summary documents.
//
// It is Node because Node is already a dependency of the harness — one of the
// two implementations is written in it — and because comparing two JSON
// documents in a shell script means comparing their formatting, which is not
// what is being asserted. The comparison here is over the parsed documents, so
// two clients that agree about every block and disagree about whitespace still
// pass, and two that agree about whitespace and disagree about a digest still
// fail.
//
// Usage:
//   node harness.mjs wait-url FILE [TIMEOUT_MS]   print a server's base URL
//   node harness.mjs authors FILE                 print an expectation's authors
//   node harness.mjs compare A B [LABEL_A LABEL_B]   assert two documents agree
//   node harness.mjs compare-startup A B [LABEL_A LABEL_B]   the same, for two
//       servers' startup lines, whose address and base URL are the two fields
//       that are allowed to differ — the kernel chose them
//   node harness.mjs compare-store A B [LABEL_A LABEL_B]   the same, ignoring
//       every chain's pursuits: what the client ended up holding, whether or not
//       it took the backward walk to get there
//   node harness.mjs expect-pursuit FILE END   assert a pursuit ended that way

import { readFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

const [command, ...rest] = process.argv.slice(2);

/** Read a JSON document, or undefined while it is still being written. */
function readJSON(path) {
  let text;
  try {
    text = readFileSync(path, "utf8");
  } catch {
    return undefined;
  }
  const line = text.split("\n", 1)[0];
  if (line.trim() === "") return undefined;
  try {
    return JSON.parse(line);
  } catch {
    // A partially written line. The caller polls.
    return undefined;
  }
}

/**
 * Wait for a server's startup line and print the base URL it announces.
 *
 * A server started with the default address asks the kernel for a free port, so
 * the port is not knowable in advance and the startup line is the only place it
 * is written down. Polling the file is what turns "the server is up" into
 * something a script can wait for without sleeping an arbitrary amount.
 */
async function waitURL(path, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const startup = readJSON(path);
    if (startup?.base_url !== undefined) return startup.base_url;
    if (Date.now() > deadline) {
      throw new Error(`no startup line in ${path} after ${timeoutMs}ms`);
    }
    await sleep(50);
  }
}

/** The authors of an expectation, in the order it lists them — which is the
 * order they must be synced in, because a later chain's blocks reference an
 * earlier chain's. */
function authors(path) {
  const expected = JSON.parse(readFileSync(path, "utf8"));
  return expected.chains.map((chain) => chain.author).join(",");
}

/** A stable rendering of a document, for a readable diff: the keys sorted, so
 * that two documents differing only in key order render identically. */
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, canonical(value[key])]),
    );
  }
  return value;
}

/** Every path at which two documents differ, in prose a person can act on. */
function differences(a, b, path = "", found = []) {
  if (JSON.stringify(canonical(a)) === JSON.stringify(canonical(b))) return found;
  const both = a !== null && b !== null && typeof a === "object" && typeof b === "object";
  if (!both) {
    found.push(`${path || "the document"}: ${JSON.stringify(a)} vs ${JSON.stringify(b)}`);
    return found;
  }
  const keys = [...new Set([...Object.keys(a), ...Object.keys(b)])];
  for (const key of keys) {
    const where = Array.isArray(a) ? `${path}[${key}]` : path === "" ? key : `${path}.${key}`;
    if (!(key in a) || !(key in b)) {
      found.push(`${where}: ${key in a ? "missing on the right" : "missing on the left"}`);
      continue;
    }
    differences(a[key], b[key], where, found);
  }
  return found;
}

/**
 * Compare two summary documents and exit non-zero on any difference.
 *
 * This is the assertion the directory exists for. The documents hold no request
 * counts, no source URLs and no implementation name, so a difference between two
 * conforming clients is impossible by construction: whatever it names is either
 * a bug in one of them or a question the specification does not answer.
 */
function compare(pathA, pathB, labelA, labelB, strip = [], pursuits = true) {
  const a = JSON.parse(readFileSync(pathA, "utf8"));
  const b = JSON.parse(readFileSync(pathB, "utf8"));
  for (const key of strip) {
    delete a[key];
    delete b[key];
  }
  if (!pursuits) {
    for (const doc of [a, b]) for (const chain of doc.chains) delete chain.pursuits;
  }
  const found = differences(a, b);
  if (found.length === 0) return;
  console.error(`the summaries from ${labelA} and ${labelB} differ:`);
  for (const line of found) console.error(`  ${line}`);
  console.error(`\n${labelA}:\n${JSON.stringify(canonical(a), null, 2)}`);
  console.error(`\n${labelB}:\n${JSON.stringify(canonical(b), null, 2)}`);
  process.exit(1);
}

/**
 * Assert that the client took the backward walk, and that it ended the way this
 * scenario's divergence says it must.
 *
 * A pursuit that ends as `failed` is a failed fetch and nothing more — it is not
 * evidence of a fork or of an invalidity — but between two servers on loopback
 * that are both serving what they hold, it is a bug, so it fails here.
 */
function expectPursuit(path, end) {
  const doc = JSON.parse(readFileSync(path, "utf8"));
  const pursuits = doc.chains.flatMap((chain) => chain.pursuits);
  if (pursuits.length === 0) {
    console.error(`${path}: no pursuit at all, wanted one ending as "${end}"`);
    process.exit(1);
  }
  for (const pursuit of pursuits) {
    if (pursuit.end !== end) {
      console.error(
        `${path}: a pursuit ended as "${pursuit.end}", wanted "${end}": ${JSON.stringify(pursuit)}`,
      );
      process.exit(1);
    }
  }
}

switch (command) {
  case "wait-url":
    console.log(await waitURL(rest[0], Number(rest[1] ?? 20000)));
    break;
  case "authors":
    console.log(authors(rest[0]));
    break;
  case "compare":
    compare(rest[0], rest[1], rest[2] ?? rest[0], rest[3] ?? rest[1]);
    break;
  case "compare-store":
    // What the client ended up holding, whether it got there by a range from
    // the genesis position or by the backward walk from an advertised tip. The
    // two routes are both blessed and must deliver the same blocks; that they
    // do is the assertion, and the pursuits are compared separately.
    compare(rest[0], rest[1], rest[2] ?? rest[0], rest[3] ?? rest[1], [], false);
    break;
  case "expect-pursuit":
    expectPursuit(rest[0], rest[1]);
    break;
  case "compare-startup":
    // Two servers over the same directory must report the same blocks and the
    // same tips: the tip is constructive, and which branch of a fork it follows
    // is fixed by the profile's reference rule rather than left to the server.
    // The address is the kernel's answer to each of them separately.
    compare(rest[0], rest[1], rest[2] ?? rest[0], rest[3] ?? rest[1], ["addr", "base_url"]);
    break;
  default:
    console.error("usage: harness.mjs wait-url|authors|compare …");
    process.exit(2);
}
