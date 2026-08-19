/**
 * A client of the transport profile that syncs a list of author chains from a
 * list of sources and writes the summary document of `interop/README.md`, for
 * the cross-implementation harness.
 *
 * ```
 * node ts/scripts/sync.ts -source URL [-source URL …] -authors KEY[,KEY…] \
 *      [-limit N] [-max-pursuit N] [-timeout MS]
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
 * ## Where each source is asked from
 *
 * The range walk starts each source at the **genesis position**, not at the
 * client's own tip. The distinction only shows itself where the sources
 * disagree, and there it is the whole point. The store's constructive tip is the
 * end of the branch *this client* chose; asking a second source from it presumes
 * that source's chain runs through that branch, which at a fork it does not, and
 * the source then answers the empty range and unreachable tip of "Pursuing an
 * advertised tip". Asking from the genesis position instead is the move that
 * section calls out as an alternative a client MAY use — "re-issuing the range
 * from the genesis position costs one request and re-downloads the shared
 * prefix; it delivers the divergent blocks too, so rule 9 fires on it as well" —
 * and it is what a client with no per-source cursor can honestly do, since it
 * holds no position of a source's chain until that source has served it one.
 * The pursuit remains implemented and obligatory in {@link syncChain} for the
 * case that does reach it, which is a source that stops short of the tip it
 * advertises.
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
import { commaSeparated, count, fail, parseFlags } from "./flags.ts";

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

    const options: SyncOptions = {
      from: null,
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
