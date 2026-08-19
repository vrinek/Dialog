/**
 * Sync: what the profile is actually for.
 *
 * Three scenarios, all of them TS-to-TS in process — this implementation's
 * server answering this implementation's client, with a downstream
 * {@link BlockStore} whose state is asserted afterwards rather than the
 * responses alone:
 *
 * - a **cold sync** of the three committed demo chains, from an empty store to
 *   fourteen validated blocks;
 * - a block left **undecided** because the blocks it needs are on a source this
 *   client has not asked, and **settled** when a second source is asked — the
 *   third outcome of validation rule 4, and the reason a failed fetch is never
 *   an invalidity;
 * - the **multi-source rule**: two sources each serving one branch of a fork,
 *   neither admitting to it, and the fork appearing at the client that asks
 *   both. Fork detection is a reachability property, not a query;
 * - **pursuing an advertised tip**, which is what that comparison consists of on
 *   the wire: an empty range and a `Dialog-Tip` the client cannot reach, walked
 *   backward by digest to the divergence — ending at a block the client holds,
 *   or at a **genesis block** when the two chains share nothing at all, or, when
 *   the walk fails, recorded as fetches that did not succeed and as nothing
 *   else. Those three ends are the profile's own, and `genesis` is neither of
 *   the other two.
 */

import assert from "node:assert/strict";
import test from "node:test";

import { BlockStore, createAtom } from "../src/block.ts";
import { encodeBlockSequence } from "../src/blockseq.ts";
import { bytesToHex } from "../src/hex.ts";
import {
  DEFAULT_BASE_PATH,
  DialogClient,
  DialogServer,
  PROBLEM_NOT_HELD,
  PROBLEM_TYPE,
  pursuitEnd,
  resolveReferences,
  sourceTip,
  syncChain,
  syncChainFromSources,
} from "../src/transport.ts";
import { ALICE, ALICE_PUB, chainOf, demoChain, demoChains, publicBlock } from "./chains.ts";

const BASE = `http://mirror.example${DEFAULT_BASE_PATH}`;

function storeOf(items: readonly { bytes: Uint8Array }[]): BlockStore {
  const store = new BlockStore();
  for (const item of items) store.add(item.bytes);
  return store;
}

function sourceOver(
  store: BlockStore,
  label: string,
  options: { downstream?: BlockStore } = {},
): DialogClient {
  const server = new DialogServer({ store });
  return new DialogClient({
    baseUrl: BASE,
    label,
    fetch: (input, init) => server.handle(new Request(input as string, init)),
    ...(options.downstream === undefined ? {} : { store: options.downstream }),
  });
}

// ---------------------------------------------------------------------------
// Cold sync of the committed chains
// ---------------------------------------------------------------------------

test("a cold sync of the three committed chains reaches fourteen validated blocks", async () => {
  const chains = demoChains();
  const upstream = new BlockStore();
  for (const chain of chains) for (const item of chain.items) upstream.add(item.bytes);

  const downstream = new BlockStore();
  const client = sourceOver(upstream, "mirror.example", { downstream });

  const results = [];
  for (const chain of chains) {
    results.push(await syncChain(client, downstream, chain.pub));
  }

  // Fourteen blocks, three requests: each chain's whole range fits in one page
  // and the last block hashes to the Dialog-Tip value, so nothing is continued.
  assert.deepEqual(
    results.map((result) => result.requests),
    [1, 1, 1],
  );
  assert.equal(
    results.reduce((total, result) => total + result.received, 0),
    14,
  );
  assert.ok(results.every((result) => result.caughtUp));
  assert.ok(results.every((result) => result.rejected.length === 0));
  assert.ok(
    results.every((result) => result.pursuit === undefined),
    "each source's tip is a block the store reached forward from the genesis position",
  );

  // The downstream store's own state, which is the point of the exercise.
  assert.equal(downstream.size, 14);
  assert.equal(downstream.validBlocks().length, 14);
  assert.deepEqual(downstream.forks, []);
  assert.deepEqual(downstream.ambiguousSuccessions, []);
  for (const chain of chains) {
    assert.deepEqual(
      sourceTip(downstream, chain.pub)?.digest,
      chain.items.at(-1)!.digest,
      `${chain.name}'s tip`,
    );
  }

  // And the downstream node is now a source: it serves the same bytes.
  const mirror = new DialogServer({ store: downstream });
  for (const chain of chains) {
    const response = await mirror.handle(
      new Request(`${BASE}/chains/${chain.keyText}/blocks`),
    );
    assert.deepEqual(new Uint8Array(await response.arrayBuffer()), encodeBlockSequence(chain.files));
  }
});

test("a paged sync resumes from the digest of the last block it received", async () => {
  const atlas = demoChain("atlas");
  const downstream = new BlockStore();
  const client = sourceOver(storeOf(atlas.items), "paged", { downstream });

  const result = await syncChain(client, downstream, atlas.pub, { pageSize: 2 });
  // Six blocks in pages of two: three full pages, and a fourth request only if
  // the third did not already hash to the tip. It does, so three.
  assert.equal(result.requests, 3);
  assert.equal(result.received, 6);
  assert.equal(result.caughtUp, true);
  assert.equal(downstream.validBlocks().length, 6);

  // Syncing again costs one request and moves nothing: the position is the
  // digest of a block the client holds, so a resume needs no state at all.
  const again = await syncChain(client, downstream, atlas.pub, { pageSize: 2 });
  assert.equal(again.requests, 1);
  assert.equal(again.received, 0);
  assert.equal(again.caughtUp, true);
  assert.equal(downstream.size, 6);
});

// ---------------------------------------------------------------------------
// Undecided, then settled by a second source
// ---------------------------------------------------------------------------

test("a block whose references no source it asked holds is undecided, never invalid", async () => {
  // errata's blocks name entities atlas defines. A client that syncs errata
  // from a source serving errata alone cannot resolve them.
  const errata = demoChain("errata");
  const atlas = demoChain("atlas");
  const downstream = new BlockStore();
  const errataOnly = sourceOver(storeOf(errata.items), "errata.example", {
    downstream,
  });

  const first = await syncChain(errataOnly, downstream, errata.pub);
  assert.equal(first.received, 4);
  assert.equal(first.accepted, 0);
  assert.equal(first.held, 4, "stored but unvalidated: the node has not been able to decide");
  assert.equal(first.rejected.length, 0, "a fetch that failed is not an invalidity");
  assert.equal(downstream.validBlocks().length, 0);
  assert.equal(downstream.size, 4);
  // The source is caught up: it gave everything it has. Undecided is not
  // "incomplete sync".
  assert.equal(first.caughtUp, true);

  // A second source holds atlas. Nothing about errata's blocks changes; what
  // changes is what the node can read. Two of the four settle on arrival —
  // errata's first two name atlas's entities and nothing else.
  const atlasSource = sourceOver(storeOf(atlas.items), "atlas.example", { downstream });
  await syncChain(atlasSource, downstream, atlas.pub);
  assert.equal(downstream.validBlocks().length, 8);
  assert.equal(downstream.get(errata.items[0]!.digest)?.valid, true);
  assert.equal(downstream.get(errata.items[1]!.digest)?.valid, true);
  assert.equal(downstream.get(errata.items[2]!.digest)?.valid, false, "it also names gazetteer's");

  // A third source holds gazetteer, and the last two settle. No block was ever
  // rejected, at any point, for a reference the node could not obtain.
  const gazetteer = demoChain("gazetteer");
  const gazetteerSource = sourceOver(storeOf(gazetteer.items), "gazetteer.example", {
    downstream,
  });
  await syncChain(gazetteerSource, downstream, gazetteer.pub);

  assert.equal(downstream.validBlocks().length, 14);
  for (const item of errata.items) {
    assert.equal(downstream.get(item.digest)?.valid, true, bytesToHex(item.digest));
  }
});

test("a batch resolution asks the sources in turn and leaves the rest undecided", async () => {
  const atlas = demoChain("atlas");
  const errata = demoChain("errata");
  const downstream = new BlockStore();
  for (const item of errata.items) downstream.add(item.bytes);

  // The three digests errata's genesis block names in refs, plus one nobody has.
  const genesis = errata.items[0]!.block;
  assert.ok("refs" in genesis);
  const wanted = [...genesis.refs, chainOf(ALICE, 1)[0]!.digest];
  assert.equal(wanted.length, 4);

  const empty = sourceOver(new BlockStore(), "empty.example", { downstream });
  const atlasSource = sourceOver(storeOf(atlas.items), "atlas.example", { downstream });
  const result = await resolveReferences([empty, atlasSource], downstream, wanted);

  assert.equal(result.resolved.length, 3);
  assert.equal(result.unresolved.length, 1, "a digest no source returned was not scanned");
  assert.deepEqual(
    result.perSource.map((entry) => [entry.source, entry.returned]),
    [
      ["empty.example", 0],
      ["atlas.example", 3],
    ],
  );
  // The block that named the unresolvable digest is not invalid; it is held.
  assert.equal(downstream.get(errata.items[0]!.digest)?.valid, true);
});

// ---------------------------------------------------------------------------
// The multi-source rule
// ---------------------------------------------------------------------------

/** One chain, forked at its second position, with each branch on its own
 * source and neither source admitting to the other. */
function forkedSources(downstream: BlockStore) {
  const [genesis] = chainOf(ALICE, 1);
  const left = publicBlock(ALICE, {
    prev: genesis!.digest,
    ts: 1740000100,
    ops: [createAtom("the branch one source serves")],
  });
  const right = publicBlock(ALICE, {
    prev: genesis!.digest,
    ts: 1740000200,
    ops: [createAtom("the branch the other serves")],
  });
  return {
    genesis: genesis!,
    left,
    right,
    first: sourceOver(storeOf([genesis!, left]), "first.example", { downstream }),
    second: sourceOver(storeOf([genesis!, right]), "second.example", { downstream }),
  };
}

test("neither source admits to the fork: each answers a one-member sibling set", async () => {
  const downstream = new BlockStore();
  const { genesis, first, second } = forkedSources(downstream);
  for (const source of [first, second]) {
    const answer = await source.siblings(ALICE_PUB, genesis.digest);
    assert.equal(
      answer.items.length,
      1,
      "a one-member set is what an honest server sends for an unforked chain and what a dishonest one sends for a forked one",
    );
  }
});

test("two sources with different branches produce a fork at the client", async () => {
  const downstream = new BlockStore();
  const { genesis, left, right, first, second } = forkedSources(downstream);

  const result = await syncChainFromSources([first, second], downstream, ALICE_PUB);

  assert.equal(result.tipsAgree, false, "the two sources name different tips");
  assert.equal(result.failures.length, 0);
  assert.equal(result.perSource.length, 2);
  const pursuit = result.perSource[1]!.pursuit!;
  assert.equal(
    pursuit.outcome,
    "held",
    "the second source named a tip the store did not hold, and the walk back reached one it did",
  );
  assert.deepEqual(pursuit.tip, right.digest);
  assert.deepEqual(pursuit.fetched, [right.digest]);
  assert.deepEqual(pursuit.reached, genesis.digest);

  // Rule 9's condition, at the client: two blocks of one chain claiming the
  // same predecessor, from one pub key.
  assert.equal(result.forks.length, 1);
  const fork = result.forks[0]!;
  assert.deepEqual(fork.prev, genesis.digest);
  assert.deepEqual(
    new Set(fork.blocks.map(bytesToHex)),
    new Set([left, right].map((item) => bytesToHex(item.digest))),
  );
  assert.deepEqual(downstream.forks, result.forks);
  assert.equal(downstream.size, 3);

  // And now the client's own store answers the sibling query honestly, which
  // neither source did.
  assert.equal(downstream.siblings(ALICE_PUB, genesis.digest).length, 2);
});

/**
 * One chain forked at its second position, each branch three blocks long and on
 * its own source, with the client having synced the first branch already.
 *
 * The second source then answers exactly what the profile says is the normal
 * second-source answer about a forked chain: an empty range after the position
 * the client holds, and a `Dialog-Tip` naming a block the client does not hold.
 */
function divergentSources(downstream: BlockStore) {
  const [genesis] = chainOf(ALICE, 1);
  const left = chainOf(ALICE, 3, { label: "left", after: genesis! });
  const right = chainOf(ALICE, 3, { label: "right", after: genesis! });
  return {
    genesis: genesis!,
    left,
    right,
    first: sourceOver(storeOf([genesis!, ...left]), "first.example", { downstream }),
    second: sourceOver(storeOf([genesis!, ...right]), "second.example", { downstream }),
  };
}

test("an empty range whose tip the client does not hold is pursued back to the fork", async () => {
  const downstream = new BlockStore();
  const { genesis, left, right, first, second } = divergentSources(downstream);

  const synced = await syncChain(first, downstream, ALICE_PUB);
  assert.equal(synced.caughtUp, true);
  assert.equal(downstream.size, 4);
  assert.equal(downstream.forks.length, 0, "one source, one branch, nothing to detect");

  // The second source: one range request, no blocks, and a tip the store does
  // not hold. A client that read that as "no new blocks" would walk away from
  // the fork it is one request from detecting.
  const result = await syncChain(second, downstream, ALICE_PUB);
  assert.equal(result.requests, 1);
  assert.deepEqual(result.declaredTip, right.at(-1)!.digest);

  // Instead it fetches the named block by digest and walks prev backward, one
  // block at a time, until it reaches a block it holds.
  const pursuit = result.pursuit!;
  assert.deepEqual(pursuit.tip, right.at(-1)!.digest);
  assert.deepEqual(
    pursuit.fetched.map(bytesToHex),
    [...right].reverse().map((item) => bytesToHex(item.digest)),
    "tip-ward first, one predecessor at a time",
  );
  assert.equal(pursuit.outcome, "held");
  assert.deepEqual(pursuit.reached, genesis.digest);
  assert.equal(result.received, 3);
  assert.equal(result.rejected.length, 0);

  // Reaching a block the client holds is the point of the exercise: two blocks
  // with one prev from one author, which is the condition rule 9 names. The
  // store surfaces it on its own contents, not on anything the transport said.
  assert.equal(downstream.forks.length, 1);
  const fork = downstream.forks[0]!;
  assert.deepEqual(fork.prev, genesis.digest);
  assert.deepEqual(
    new Set(fork.blocks.map(bytesToHex)),
    new Set([left[0]!, right[0]!].map((item) => bytesToHex(item.digest))),
  );

  // Both branches are in the store, whole, and neither source admitted to
  // anything.
  for (const item of [...left, ...right]) {
    assert.equal(downstream.get(item.digest)?.valid, true, bytesToHex(item.digest));
  }
  assert.equal(downstream.size, 7);
  assert.equal(downstream.siblings(ALICE_PUB, genesis.digest).length, 2);
});

test("a pursuit that fails is a failed fetch and nothing else", async () => {
  const downstream = new BlockStore();
  const { genesis, right, first } = divergentSources(downstream);
  await syncChain(first, downstream, ALICE_PUB);

  // A source that advertises a tip it will not serve is indistinguishable from
  // one that lost it, which is the freshness gap and is not fixable here.
  const withholding = storeOf([genesis, ...right]);
  const server = new DialogServer({ store: withholding });
  const liar = new DialogClient({
    baseUrl: BASE,
    label: "withholding.example",
    store: downstream,
    fetch: async (input, init) => {
      const url = new URL(input as string);
      if (url.pathname.startsWith(`${DEFAULT_BASE_PATH}/blocks/`)) {
        return new Response(JSON.stringify({ type: PROBLEM_NOT_HELD, status: 404 }), {
          status: 404,
          headers: { "Content-Type": PROBLEM_TYPE },
        });
      }
      return server.handle(new Request(input as string, init));
    },
  });

  const before = downstream.size;
  const refused = await syncChain(liar, downstream, ALICE_PUB);
  assert.deepEqual(refused.declaredTip, right.at(-1)!.digest);
  assert.equal(refused.pursuit?.outcome, "not-held");
  assert.deepEqual(refused.pursuit?.fetched, []);
  assert.equal(refused.caughtUp, false, "the store still does not hold the tip it was shown");
  // No verdict about any block follows from a failed pursuit.
  assert.equal(refused.rejected.length, 0);
  assert.equal(downstream.forks.length, 0);
  assert.equal(downstream.size, before);

  // The client's own bound is the other way a walk ends short, and it is the
  // same kind of nothing: the walk is over a chain of the source's choosing, of
  // a length the source controls.
  const second = sourceOver(withholding, "second.example", { downstream });
  const bounded = await syncChain(second, downstream, ALICE_PUB, { maxPursuit: 2 });
  assert.equal(bounded.pursuit?.outcome, "bounded");
  assert.equal(bounded.pursuit?.fetched.length, 2);
  assert.equal(bounded.rejected.length, 0);
  assert.equal(downstream.forks.length, 0, "the walk never reached the divergent position");
  // The two blocks it did fetch are held, undecided: their ancestry has not
  // arrived, which is not an invalidity either.
  for (const item of right.slice(1)) {
    assert.equal(downstream.get(item.digest)?.valid, false, bytesToHex(item.digest));
  }
});

test("a pursuit that reaches a genesis block is not a failure", async () => {
  // Two chains signed by one key with different genesis blocks: the source
  // serves one, the client already holds the other. They share no block at all,
  // so the backward walk from the source's tip has no held block to meet and
  // runs out of predecessors instead.
  const downstream = new BlockStore();
  const mine = chainOf(ALICE, 3, { label: "the chain the client holds" });
  const theirs = chainOf(ALICE, 3, { label: "the chain the source serves" });
  assert.notDeepEqual(mine[0]!.digest, theirs[0]!.digest, "two distinct genesis blocks");

  const holder = sourceOver(storeOf(mine), "held.example", { downstream });
  await syncChain(holder, downstream, ALICE_PUB);
  assert.equal(downstream.validBlocks().length, 3);
  assert.equal(downstream.forks.length, 0, "one chain, nothing to detect");

  const other = sourceOver(storeOf(theirs), "other.example", { downstream });
  const result = await syncChain(other, downstream, ALICE_PUB);

  // The normal second-source answer about a chain the client's position is not
  // on: an empty range, and a tip the client cannot reach.
  assert.equal(result.requests, 1);
  assert.deepEqual(result.declaredTip, theirs.at(-1)!.digest);

  const pursuit = result.pursuit!;
  assert.deepEqual(
    pursuit.fetched.map(bytesToHex),
    [...theirs].reverse().map((item) => bytesToHex(item.digest)),
    "the whole of the other chain, tip-ward first",
  );

  // The settled three-way name, and the finer kind beside it. Nothing failed:
  // every fetch succeeded and every block verified.
  assert.equal(pursuit.end, "genesis");
  assert.equal(pursuit.outcome, "genesis");
  assert.equal(pursuitEnd(pursuit.outcome), "genesis");
  assert.notEqual(pursuit.end, "failed", "the walk ran out of predecessors, it did not fail");
  assert.notEqual(pursuit.end, "held", "no block the client holds was ever met");
  assert.equal(pursuit.reached, undefined);
  assert.equal(result.rejected.length, 0, "no verdict about any block follows from the end");

  // Every block of both chains is in the store and valid: the walk's blocks are
  // offered to the store like any others.
  assert.equal(downstream.size, 6);
  assert.equal(downstream.validBlocks().length, 6);
  for (const item of [...mine, ...theirs]) {
    assert.equal(downstream.get(item.digest)?.valid, true, bytesToHex(item.digest));
  }

  // And rule 9's condition, with the two GENESIS blocks as the sibling pair:
  // two distinct blocks of one pub key claiming the genesis position, which
  // spec/02-block-format.md's "rotate_key" calls a fork in the strict sense.
  const genesisSiblings = downstream.siblings(ALICE_PUB, null);
  assert.deepEqual(
    new Set(genesisSiblings.map((stored) => bytesToHex(stored.digest))),
    new Set([mine[0]!, theirs[0]!].map((item) => bytesToHex(item.digest))),
  );
  assert.equal(downstream.forks.length, 1);
  const fork = downstream.forks[0]!;
  assert.equal(fork.prev, null, "the genesis position, named by the absence of a digest");
  assert.deepEqual(fork.pub, ALICE_PUB);
  assert.deepEqual(
    new Set(fork.blocks.map(bytesToHex)),
    new Set([mine[0]!, theirs[0]!].map((item) => bytesToHex(item.digest))),
  );
});

test("two sources that agree need nothing more", async () => {
  const atlas = demoChain("atlas");
  const downstream = new BlockStore();
  const upstream = storeOf(atlas.items);
  const result = await syncChainFromSources(
    [sourceOver(upstream, "one", { downstream }), sourceOver(upstream, "two", { downstream })],
    downstream,
    atlas.pub,
  );

  assert.equal(result.tipsAgree, true);
  assert.deepEqual(result.forks, []);
  assert.equal(result.perSource[1]!.received, 0, "the second source has nothing to add");
  assert.equal(downstream.validBlocks().length, 6);
});

test("a source that does not answer says nothing about the chain", async () => {
  const atlas = demoChain("atlas");
  const downstream = new BlockStore();
  const broken = new DialogClient({
    baseUrl: BASE,
    label: "offline.example",
    fetch: async () => {
      throw new Error("connection refused");
    },
    store: downstream,
  });
  const working = sourceOver(storeOf(atlas.items), "working.example", { downstream });

  const result = await syncChainFromSources([broken, working], downstream, atlas.pub);
  assert.deepEqual(
    result.failures.map((failure) => failure.source),
    ["offline.example"],
  );
  assert.equal(result.perSource.length, 1);
  assert.equal(downstream.validBlocks().length, 6, "the source that did answer still counts");
});
