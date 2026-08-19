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
 *   both. Fork detection is a reachability property, not a query.
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
  assert.ok(results.every((result) => !result.rescanned));

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
  assert.equal(
    result.perSource[1]!.rescanned,
    true,
    "the second source named a tip the store could not reach forward from its own",
  );

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
