/**
 * A client of the transport profile that syncs a list of author chains from a
 * list of sources and writes the summary document of `interop/README.md`, for
 * the cross-implementation harness.
 *
 * ```
 * node ts/scripts/sync.ts -source URL [-source URL …] -authors KEY[,KEY…] \
 *      [-from genesis|held] [-limit N] [-max-pursuit N] [-timeout MS]
 * ```
 *
 * Every author is synced from **every** source into **one** store, in the order
 * the authors were given, because that order is a real dependency: a later
 * chain's blocks name an earlier chain's in `refs`, and a client that already
 * holds what a reference resolves to spends no fetch on it
 * (`spec/07-transport.md`, "A full sync session", step 4). One store per run and
 * nothing kept between runs, so the document below is a statement about what
 * this exchange delivered rather than about what a previous one left behind.
 *
 * ## Where each source is asked from — `-from`
 *
 * A client meeting a source it has never asked before has two positions it can
 * ask the range from, and the position of *another* source's chain is not a
 * cursor for this one. The profile permits both, prefers the genesis position,
 * and asks a client to record which it used — which this flag and the run's
 * command line are (`spec/07-transport.md`, "First contact with a source").
 * Which of the two a client picks is invisible in a document until the sources
 * disagree, and there it decides whether the pursuit happens at all.
 *
 * `-from genesis`, the default, asks every source from the **genesis position**.
 * It is what a client with no per-source cursor can honestly do, since it holds
 * no position of a source's chain until that source has served it one, and it is
 * the move "Pursuing an advertised tip" names as an alternative a client MAY
 * use: "re-issuing the range from the genesis position costs one request and
 * re-downloads the shared prefix; it delivers the divergent blocks too, so rule
 * 9 fires on it as well". A source on another branch then answers with its whole
 * chain and the divergence arrives as ordinary blocks.
 *
 * `-from held` asks each source from where **this client's own chain** already
 * reaches: the constructive tip of `spec/07-transport.md`, "tip", recomputed
 * over the store before each source, and the genesis position when the store
 * holds nothing of that author — so the first source of a run is asked from the
 * genesis position either way. It asks for nothing the client already holds, and
 * that is exactly why it reaches the other case: a source serving a branch this
 * client's position is not on answers the empty range and the unreachable tip
 * that "Pursuing an advertised tip" is written for, and {@link syncChain}
 * pursues it, which is what puts two blocks with one `prev` in the store for
 * validation rule 9 to fire on.
 *
 * Neither choice changes which blocks the run ends up holding, in these
 * scenarios or in any where every source is honest: it changes how they arrive,
 * and therefore whether `pursuits` has anything in it.
 *
 * ## What the document says
 *
 * `chain` is the constructive tip walk of `spec/07-transport.md`, "tip", over
 * the client's own store; `blocks` is every block of that author reachable
 * *forward* from the genesis position — every branch of every fork, not only the
 * branch the walk took — so a block whose predecessor the store does not hold is
 * on no chain the client can name and appears nowhere. `accepted` and `held` are
 * the store's verdicts over exactly those blocks, `held` being the *stored but
 * unvalidated* state of `spec/05-processing-model.md`, "Block reception", which
 * is neither acceptance nor refusal. `forks` is read off the store's own
 * contents rather than off anything a source said, which is what validation rule
 * 9 requires and what the multi-source rule exists to make possible.
 *
 * Nothing else reaches stdout: the document is the whole of it, and every
 * diagnostic goes to stderr.
 */

import process from "node:process";

import { BlockStore, type StoredBlock } from "../src/block.ts";
import { compareBytes } from "../src/blockseq.ts";
import { authorKeyFromText, authorKeyToText } from "../src/cid.ts";
import { bytesToHex } from "../src/hex.ts";
import {
  DialogClient,
  type PursuitEnd,
  type SyncOptions,
  digestToCidText,
  syncChainFromSources,
  walkChain,
} from "../src/transport.ts";
import { commaSeparated, count, fail, one, parseFlags } from "./flags.ts";

/** How long one chain's sync, across all of its sources, may take when no
 * `-timeout` is given. The bound is the client's own, as every resource bound in
 * this profile is. */
const DEFAULT_TIMEOUT_MS = 30_000;

/** One pursuit of a tip a source advertised and the store did not hold. */
interface PursuitSummary {
  readonly source: number;
  readonly tip: string;
  readonly end: PursuitEnd;
  readonly fetched: number;
}

/** One position the client holds more than one block of the author at. */
interface ForkSummary {
  readonly prev: string | null;
  readonly siblings: string[];
}

/** One author chain, as the client's store ended up holding it. */
interface ChainSummary {
  readonly author: string;
  readonly advertised_tips: (string | null)[];
  readonly tip: string | null;
  readonly chain: string[];
  readonly blocks: string[];
  readonly accepted: number;
  readonly held: number;
  readonly rejected: number;
  readonly pursuits: PursuitSummary[];
  readonly forks: ForkSummary[];
}

/** The summary document. */
interface Summary {
  readonly chains: ChainSummary[];
  readonly totals: {
    readonly chains: number;
    readonly blocks: number;
    readonly accepted: number;
    readonly held: number;
    readonly rejected: number;
    readonly forks: number;
  };
}

await main();

async function main(): Promise<void> {
  const flags = parseFlags(process.argv.slice(2));
  const sources = commaSeparated(flags, "source");
  const authors = commaSeparated(flags, "authors");
  if (sources.length === 0) fail("sync: -source URL is required");
  if (authors.length === 0) fail("sync: -authors KEY[,KEY…] is required");
  const pageSize = count(flags, "limit");
  const maxPursuit = count(flags, "max-pursuit");
  const timeout = count(flags, "timeout") ?? DEFAULT_TIMEOUT_MS;
  const from = one(flags, "from") ?? "genesis";
  if (from !== "genesis" && from !== "held") {
    fail(`sync: -from is genesis or held, not ${from}`);
  }

  const store = new BlockStore();
  const clients = sources.map(
    (baseUrl) => new DialogClient({ baseUrl, store, label: baseUrl }),
  );

  const chains: ChainSummary[] = [];
  for (const text of authors) {
    let pub: Uint8Array;
    try {
      pub = authorKeyFromText(text);
    } catch (error) {
      fail(`sync: ${text} is not an author key in the canonical text form: ${reason(error)}`);
    }

    // `held` is {@link syncChain}'s own default — the constructive tip of what
    // the store holds, recomputed per source, and the genesis position when it
    // holds nothing — so the flag leaves the option out rather than naming a
    // position the store has not been consulted about yet.
    const options: SyncOptions = {
      ...(from === "genesis" ? { from: null } : {}),
      signal: AbortSignal.timeout(timeout),
      ...(pageSize === undefined ? {} : { pageSize }),
      ...(maxPursuit === undefined ? {} : { maxPursuit }),
    };
    const synced = await syncChainFromSources(clients, store, pub, options);
    // A source that did not answer says nothing about the chain, and this
    // program says nothing about a run it could not make: a source it could not
    // reach at all is an error rather than a document with a hole in it.
    if (synced.failures.length > 0) {
      fail(
        synced.failures
          .map((failure) => `sync: ${failure.source}: ${failure.reason}`)
          .join("\n"),
      );
    }

    const advertised: (string | null)[] = [];
    const pursuits: PursuitSummary[] = [];
    let rejected = 0;
    for (const [index, result] of synced.perSource.entries()) {
      advertised.push(
        result.declaredTip === undefined ? null : digestToCidText(result.declaredTip),
      );
      rejected += result.rejected.length;
      if (result.pursuit !== undefined) {
        pursuits.push({
          source: index,
          tip: digestToCidText(result.pursuit.tip),
          end: result.pursuit.end,
          fetched: result.pursuit.fetched.length,
        });
      }
    }
    chains.push(summarize(store, pub, advertised, pursuits, rejected));
  }

  const summary: Summary = {
    chains,
    totals: {
      chains: chains.length,
      blocks: sum(chains, (chain) => chain.blocks.length),
      accepted: sum(chains, (chain) => chain.accepted),
      held: sum(chains, (chain) => chain.held),
      rejected: sum(chains, (chain) => chain.rejected),
      forks: sum(chains, (chain) => chain.forks.length),
    },
  };
  process.stdout.write(`${JSON.stringify(summary, null, 2)}\n`);
}

/** What the store holds of one author, as the document reports it. */
function summarize(
  store: BlockStore,
  pub: Uint8Array,
  advertised: (string | null)[],
  pursuits: PursuitSummary[],
  rejected: number,
): ChainSummary {
  const chain = walkChain(store, pub);
  const { blocks, forks } = reachable(store, pub);
  const accepted = blocks.filter((held) => held.valid).length;
  return {
    author: authorKeyToText(pub),
    advertised_tips: advertised,
    tip: chain.length === 0 ? null : digestToCidText(chain.at(-1)!.digest),
    chain: chain.map((held) => digestToCidText(held.digest)),
    blocks: blocks
      .map((held) => held.digest)
      .sort(compareBytes)
      .map(digestToCidText),
    accepted,
    held: blocks.length - accepted,
    rejected,
    pursuits,
    forks,
  };
}

/**
 * Every block of an author the store can reach forward from the genesis
 * position, and every position it holds more than one of them at.
 *
 * The walk takes every branch rather than the one a tip walk would choose,
 * because a fork's other side is exactly what the client is here to see: a
 * position with more than one block in it is validation rule 9's condition, read
 * off the store's own contents. A block whose predecessor the store does not
 * hold is never reached, which is the same statement as "it is on no chain the
 * client can name".
 */
function reachable(
  store: BlockStore,
  pub: Uint8Array,
): { blocks: StoredBlock[]; forks: ForkSummary[] } {
  const blocks: StoredBlock[] = [];
  const forks: { prev: Uint8Array | null; siblings: Uint8Array[] }[] = [];
  const seen = new Set<string>();
  const positions: (Uint8Array | null)[] = [null];

  while (positions.length > 0) {
    const prev = positions.shift()!;
    const at = [...store.siblings(pub, prev)].sort((a, b) => compareBytes(a.digest, b.digest));
    if (at.length > 1) forks.push({ prev, siblings: at.map((held) => held.digest) });
    for (const held of at) {
      const key = bytesToHex(held.digest);
      if (seen.has(key)) continue;
      seen.add(key);
      blocks.push(held);
      positions.push(held.digest);
    }
  }

  return {
    blocks,
    forks: forks
      .sort((a, b) => compareBytes(a.siblings[0]!, b.siblings[0]!))
      .map((fork) => ({
        prev: fork.prev === null ? null : digestToCidText(fork.prev),
        siblings: fork.siblings.map(digestToCidText),
      })),
  };
}

function sum<T>(items: readonly T[], of: (item: T) => number): number {
  return items.reduce((total, item) => total + of(item), 0);
}

function reason(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
