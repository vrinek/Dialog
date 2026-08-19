/**
 * A server of the transport profile over a directory of `.block` files, for the
 * cross-implementation harness of `interop/README.md`.
 *
 * ```
 * node ts/scripts/serve.ts -chains DIR [-addr HOST:PORT] [-prefix PATH] [-announce]
 * ```
 *
 * Every `.block` file under the directory, recursively and in ascending path
 * order, is decoded as exactly one block — `spec/07-transport.md`, "As a file":
 * a block sequence of length one, with no framing of any kind, so the file's
 * bytes are the block's bytes and its digest is taken over them unchanged. Any
 * `index.json` beside them is ignored and every digest is recomputed: the index
 * "is a local convenience — it carries no authority, and a reader that trusted
 * it would still have to validate every block".
 *
 * What is served is whatever the store then holds, with no filter by verdict
 * (server rule 7): a block held as *stored but unvalidated* is answered by
 * `block`, named by `siblings`, and crossed by the `tip` and `range` walk,
 * because withholding it would cost a client a detection and save it nothing.
 * Loading is the one place a block can be turned away, and a block a validation
 * rule shows wrong is a fault in the directory rather than something to serve
 * around: it stops the program with a diagnostic, rather than leaving a hole
 * every block after it would fail rule 3 against.
 *
 * One line of JSON goes to stdout before the first request can arrive, so that a
 * script can learn the port the kernel gave it. Its `tip` per chain is the
 * constructive tip of `spec/07-transport.md`, "tip" — the end of the forward
 * walk from the genesis position, taking the lowest digest where the store holds
 * more than one block at a position — and it is `null` for an author whose
 * genesis block the directory does not hold, since "a source that holds no block
 * at the genesis position holds no tip for that author, whatever else of that
 * chain it holds".
 *
 * `announce` is offered only under `-announce`. A read-only mirror is conforming
 * and is the default; without the flag the path answers 404 with
 * `urn:dialog:problem:operation-not-offered`, which is a fact about this server
 * that asking again will not change.
 */

import { readFileSync, readdirSync } from "node:fs";
import { createServer as createHttpServer } from "node:http";
import path from "node:path";
import process from "node:process";

import { BlockStore } from "../src/block.ts";
import { BLOCK_FILE_EXTENSION, compareBytes } from "../src/blockseq.ts";
import { authorKeyToText } from "../src/cid.ts";
import { bytesToHex } from "../src/hex.ts";
import { nodeListener } from "../src/node-http.ts";
import {
  DEFAULT_BASE_PATH,
  DialogServer,
  digestToCidText,
  sourceTip,
} from "../src/transport.ts";
import { fail, one, parseFlags } from "./flags.ts";

/** The startup line: what this process is serving, and where. */
interface StartupLine {
  readonly addr: string;
  readonly base_url: string;
  readonly blocks: number;
  readonly chains: readonly {
    readonly author: string;
    readonly tip: string | null;
    readonly blocks: number;
  }[];
}

main();

function main(): void {
  const flags = parseFlags(process.argv.slice(2), { boolean: ["announce"] });
  const chains = one(flags, "chains");
  if (chains === undefined) fail("serve: -chains DIR is required");
  const prefix = one(flags, "prefix") ?? DEFAULT_BASE_PATH;
  const [host, port] = splitAddress(one(flags, "addr") ?? "127.0.0.1:0");

  const store = new BlockStore();
  const perAuthor = load(store, chains);

  const server = createHttpServer(
    nodeListener(
      new DialogServer({
        store,
        basePath: prefix,
        announce: flags.boolean.has("announce"),
      }).fetch,
    ),
  );

  server.on("error", (error: Error) => fail(`serve: ${error.message}`));
  server.listen(port, host, () => {
    const bound = server.address();
    const addr =
      bound === null || typeof bound === "string"
        ? `${host}:${port}`
        : `${host === "" ? bound.address : host}:${bound.port}`;
    const line: StartupLine = {
      addr,
      base_url: `http://${addr}${prefix}`,
      blocks: store.size,
      chains: [...perAuthor.values()]
        .sort((a, b) => compareBytes(a.pub, b.pub))
        .map((chain) => {
          const tip = sourceTip(store, chain.pub);
          return {
            author: authorKeyToText(chain.pub),
            tip: tip === undefined ? null : digestToCidText(tip.digest),
            blocks: chain.blocks,
          };
        }),
    };
    process.stdout.write(`${JSON.stringify(line)}\n`);
  });

  // SIGINT and SIGTERM are how the harness stops a server it started, and a
  // server that is asked to stop has done nothing wrong: it exits 0.
  for (const signal of ["SIGINT", "SIGTERM"] as const) {
    process.on(signal, () => {
      server.close();
      server.closeAllConnections();
      process.exit(0);
    });
  }
}

/** What one author's blocks in the directory amount to. */
interface HeldChain {
  readonly pub: Uint8Array;
  blocks: number;
}

/**
 * Read every `.block` file under a directory into the store, in ascending path
 * order, and count what arrived per author.
 *
 * The order matters only for the verdicts, never for what is served: a chain
 * whose blocks arrive genesis-first is validated as it loads, and one that
 * arrives out of order is held unvalidated until the missing predecessor turns
 * up, which is the same store either way (`spec/05-processing-model.md`, "Block
 * reception", "Revalidation on arrival").
 */
function load(store: BlockStore, directory: string): Map<string, HeldChain> {
  const perAuthor = new Map<string, HeldChain>();
  for (const file of blockFiles(directory)) {
    const bytes = new Uint8Array(readFileSync(file));
    let pub: Uint8Array;
    try {
      const result = store.add(bytes);
      pub = store.get(result.digest)!.block.pub;
    } catch (error) {
      fail(`serve: ${file}: ${error instanceof Error ? error.message : String(error)}`);
    }
    const key = bytesToHex(pub);
    const chain = perAuthor.get(key) ?? { pub, blocks: 0 };
    chain.blocks++;
    perAuthor.set(key, chain);
  }
  if (perAuthor.size === 0) fail(`serve: no ${BLOCK_FILE_EXTENSION} files under ${directory}`);
  return perAuthor;
}

/** Every `.block` file under a directory, recursively, in ascending path
 * order. */
function blockFiles(directory: string): string[] {
  const found: string[] = [];
  const walk = (at: string): void => {
    for (const entry of readdirSync(at, { withFileTypes: true })) {
      const full = path.join(at, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.isFile() && entry.name.endsWith(BLOCK_FILE_EXTENSION)) found.push(full);
    }
  };
  try {
    walk(directory);
  } catch (error) {
    fail(`serve: ${error instanceof Error ? error.message : String(error)}`);
  }
  return found.sort();
}

/** `HOST:PORT`, with the port after the last colon so that a bracketed IPv6
 * host survives. */
function splitAddress(addr: string): [string, number] {
  const colon = addr.lastIndexOf(":");
  if (colon === -1) fail(`serve: -addr is HOST:PORT, not ${addr}`);
  const host = addr.slice(0, colon);
  const port = Number(addr.slice(colon + 1));
  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    fail(`serve: -addr has no port: ${addr}`);
  }
  return [host, port];
}
