/**
 * The client half of `spec/07-transport.md`: the five read operations and
 * `announce`, and above all the **client rules** — the obligations that make a
 * profile carrying no trust safe.
 *
 * The client is run against this implementation's own server, in process, which
 * is what makes the profile testable at all: a source that lies is a server
 * built here to lie, and every one below is.
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  BlockStore,
  createAtom,
  createBond,
  createMolecule,
  encodeBlock,
  newPublicBlock,
  operationDigest,
  signBlock,
  unsignedPublicBlock,
} from "../src/block.ts";
import { encodeBlockSequence, sequenceItem } from "../src/blockseq.ts";
import { atomFiller } from "../src/entity.ts";
import { authorKeyToText } from "../src/cid.ts";
import { bytesToHex } from "../src/hex.ts";
import {
  BLOCK_SEQUENCE_TYPE,
  CBOR_SEQUENCE_TYPE,
  DEFAULT_BASE_PATH,
  DialogClient,
  DialogServer,
  JSON_TYPE,
  PROBLEM_ANNOUNCE_REFUSED,
  PROBLEM_NOT_HELD,
  PROBLEM_TYPE,
  TransportError,
  digestToCidText,
} from "../src/transport.ts";
import { ALICE, ALICE_PUB, BOB, chainOf, demoChain, publicBlock } from "./chains.ts";

const ORIGIN = "http://mirror.example";
const BASE = `${ORIGIN}${DEFAULT_BASE_PATH}`;

function storeOf(...sequences: readonly (readonly { bytes: Uint8Array }[])[]): BlockStore {
  const store = new BlockStore();
  for (const sequence of sequences) {
    for (const item of sequence) store.add(item.bytes);
  }
  return store;
}

/** A client wired straight into a server, with no socket in between. */
function clientFor(
  server: DialogServer,
  options: { store?: BlockStore; label?: string } = {},
): DialogClient {
  return new DialogClient({
    baseUrl: BASE,
    fetch: (input, init) => server.handle(new Request(input as string, init)),
    ...options,
  });
}

/** A client against a source that answers whatever the test says it does. */
function clientAgainst(
  answer: (request: Request) => Response | Promise<Response>,
  options: { store?: BlockStore } = {},
): DialogClient {
  return new DialogClient({
    baseUrl: BASE,
    fetch: async (input, init) => answer(new Request(input as string, init)),
    ...options,
  });
}

function sequenceResponse(bytes: Uint8Array, type = BLOCK_SEQUENCE_TYPE): Response {
  return new Response(bytes, { status: 200, headers: { "Content-Type": type } });
}

// ---------------------------------------------------------------------------
// The read operations
// ---------------------------------------------------------------------------

test("tip: the block, its digest computed here, and an ETag verified rather than believed", async () => {
  const atlas = demoChain("atlas");
  const server = new DialogServer({ store: storeOf(atlas.items) });
  const answer = await clientFor(server).tip(atlas.pub);

  assert.equal(answer.status, "block");
  assert.deepEqual(answer.digest, atlas.items.at(-1)!.digest);
  assert.deepEqual(answer.declaredTip, atlas.items.at(-1)!.digest);
  assert.equal(answer.etagVerified, true);
});

test("tip: a 404 is not-held and carries the profile's problem type", async () => {
  const server = new DialogServer({ store: new BlockStore() });
  const answer = await clientFor(server).tip(ALICE_PUB);
  assert.equal(answer.status, "not-held");
  assert.equal(answer.problem?.type, PROBLEM_NOT_HELD);
  assert.equal(answer.problem?.status, 404);
});

test("tip: If-None-Match is how a client polls a chain for a few dozen bytes", async () => {
  const atlas = demoChain("atlas");
  const server = new DialogServer({ store: storeOf(atlas.items) });
  const client = clientFor(server);

  const first = await client.tip(atlas.pub);
  const again = await client.tip(atlas.pub, { ifNoneMatch: first.etag! });
  assert.equal(again.status, "not-modified");
  assert.deepEqual(again.declaredTip, atlas.items.at(-1)!.digest);
});

test("range: the whole chain from the genesis position, caught up at the tip", async () => {
  const atlas = demoChain("atlas");
  const server = new DialogServer({ store: storeOf(atlas.items) });
  const answer = await clientFor(server).range(atlas.pub);

  assert.equal(answer.items.length, 6);
  assert.equal(answer.caughtUp, true, "the last block hashes to the Dialog-Tip value");
  assert.deepEqual(
    answer.items.map((item) => bytesToHex(item.digest)),
    atlas.items.map((item) => bytesToHex(item.digest)),
  );

  const page = await clientFor(server).range(atlas.pub, null, { limit: 2 });
  assert.equal(page.items.length, 2);
  assert.equal(page.caughtUp, false, "a range that ended at a limit is not caught up");
});

test("range: the client checks the range property itself, and a skip breaks the walk", async () => {
  const chain = chainOf(ALICE, 4);
  // A source that skips a block it holds. Nothing about the blocks is forged —
  // each one is genuine — which is the point: omission is the whole attack
  // surface, and within a range it is free to detect.
  const client = clientAgainst(() =>
    sequenceResponse(encodeBlockSequence([chain[0]!, chain[2]!, chain[3]!])),
  );
  await assert.rejects(client.range(ALICE_PUB), /does not name block 0 as its predecessor/);

  // And a range that does not begin at the position the client asked about.
  const shifted = clientAgainst(() => sequenceResponse(encodeBlockSequence(chain.slice(1))));
  await assert.rejects(shifted.range(ALICE_PUB), /does not name the position/);
});

test("range: another author's blocks in the response are refused", async () => {
  const mine = chainOf(ALICE, 1);
  const theirs = chainOf(BOB, 1);
  const client = clientAgainst(() => sequenceResponse(encodeBlockSequence([mine[0]!, theirs[0]!])));
  await assert.rejects(client.range(ALICE_PUB), /another author/);
});

test("block: a response hashing to something else is a failed fetch, not a block", async () => {
  const chain = chainOf(ALICE, 2);
  // The source answers a genuine, valid, correctly signed block — just not the
  // one that was asked for.
  const client = clientAgainst(() => sequenceResponse(encodeBlockSequence([chain[1]!])));
  const answer = await client.block(chain[0]!.digest);
  assert.equal(answer.failed, "digest-mismatch");
  assert.equal(answer.item, undefined);
});

test("block: a 404 is a fetch that did not succeed, never a finding about the reference", async () => {
  const server = new DialogServer({ store: new BlockStore() });
  const answer = await clientFor(server).block(chainOf(ALICE, 1)[0]!.digest);
  assert.equal(answer.failed, "not-held");
  assert.equal(answer.problem?.type, PROBLEM_NOT_HELD);
});

test("blocks: the subset held comes back, and the rest is named as missing", async () => {
  const atlas = demoChain("atlas");
  const absent = chainOf(ALICE, 2);
  const server = new DialogServer({ store: storeOf(atlas.items) });
  const wanted = [atlas.items[3]!, absent[0]!, atlas.items[0]!, absent[1]!];

  const answer = await clientFor(server).blocks(wanted.map((item) => item.digest));
  assert.deepEqual(
    answer.items.map((item) => bytesToHex(item.digest)),
    [atlas.items[3]!, atlas.items[0]!].map((item) => bytesToHex(item.digest)),
  );
  assert.deepEqual(
    answer.missing.map(bytesToHex),
    absent.map((item) => bytesToHex(item.digest)),
  );
  assert.equal(answer.orderRespected, true);
});

test("blocks: a repeated digest is asked for once, and an empty list costs no request", async () => {
  const atlas = demoChain("atlas");
  let requests = 0;
  const server = new DialogServer({ store: storeOf(atlas.items) });
  const client = new DialogClient({
    baseUrl: BASE,
    fetch: (input, init) => {
      requests++;
      return server.handle(new Request(input as string, init));
    },
  });

  const digest = atlas.items[0]!.digest;
  const answer = await client.blocks([digest, digest]);
  assert.equal(answer.items.length, 1);
  assert.equal(requests, 1, "a request MUST NOT name the same digest twice");

  await client.blocks([]);
  assert.equal(requests, 1);
});

test("siblings: the whole set at a position, checked for order on arrival", async () => {
  const [genesis] = chainOf(ALICE, 1);
  const left = publicBlock(ALICE, { prev: genesis!.digest, ts: 2, ops: [createAtom("left")] });
  const right = publicBlock(ALICE, { prev: genesis!.digest, ts: 3, ops: [createAtom("right")] });
  const server = new DialogServer({ store: storeOf([genesis!, left, right]) });

  const answer = await clientFor(server).siblings(ALICE_PUB, genesis!.digest);
  assert.equal(answer.items.length, 2);

  // A source that sends the set in the wrong order is refused: the order is
  // fixed so that two sources holding the same set produce the same bytes.
  const unsorted = [left, right].sort((a, b) => bytesToHex(b.digest).localeCompare(bytesToHex(a.digest)));
  const liar = clientAgainst(() => sequenceResponse(encodeBlockSequence(unsorted)));
  await assert.rejects(liar.siblings(ALICE_PUB, genesis!.digest), /ascending/);
});

// ---------------------------------------------------------------------------
// Media types and bounds
// ---------------------------------------------------------------------------

test("a client accepts application/cbor-seq as equivalent and refuses anything else", async () => {
  const chain = chainOf(ALICE, 1);
  const generic = clientAgainst(() =>
    sequenceResponse(encodeBlockSequence(chain), CBOR_SEQUENCE_TYPE),
  );
  const answer = await generic.block(chain[0]!.digest);
  assert.deepEqual(answer.item?.digest, chain[0]!.digest);

  for (const type of ["application/octet-stream", JSON_TYPE, "text/plain"]) {
    const wrong = clientAgainst(() => sequenceResponse(encodeBlockSequence(chain), type));
    await assert.rejects(wrong.block(chain[0]!.digest), /not a block sequence/);
  }
});

test("a client bounds what it will read from a response", async () => {
  const chain = chainOf(ALICE, 4);
  const client = new DialogClient({
    baseUrl: BASE,
    fetch: async () => sequenceResponse(encodeBlockSequence(chain)),
    maxResponseBlocks: 2,
  });
  await assert.rejects(client.range(ALICE_PUB), /bound of 2/);
});

test("an error status is surfaced with the problem document as it arrived", async () => {
  const client = clientAgainst(
    () =>
      new Response(
        JSON.stringify({
          type: "about:blank",
          title: "Rate limited",
          status: 429,
          detail: "ask again in a minute",
        }),
        { status: 429, headers: { "Content-Type": PROBLEM_TYPE, "Retry-After": "60" } },
      ),
  );
  await assert.rejects(client.range(ALICE_PUB), (error: unknown) => {
    assert.ok(error instanceof TransportError);
    assert.equal(error.status, 429);
    assert.equal(error.operation, "range");
    assert.equal(error.problem?.detail, "ask again in a minute");
    assert.equal(error.notHeld, false);
    return true;
  });
});

test("a client works against a server that sends none of the three problem types", async () => {
  const bare = clientAgainst(() => new Response(null, { status: 404 }));
  // The status code is the whole of it: a 404 for a block is a failed fetch
  // whatever the body says or does not say.
  assert.equal((await bare.block(chainOf(ALICE, 1)[0]!.digest)).failed, "not-held");
  assert.equal((await bare.tip(ALICE_PUB)).status, "not-held");

  // And a 403 with no problem document at all is still a refusal of this
  // announce and still carries no verdict about the blocks.
  const refusing = clientAgainst(() => new Response(null, { status: 403 }));
  await assert.rejects(refusing.announce(chainOf(ALICE, 1)), (error: unknown) => {
    assert.ok(error instanceof TransportError);
    assert.equal(error.announceRefused, true);
    assert.equal(error.problem, undefined);
    return true;
  });
});

// ---------------------------------------------------------------------------
// Validation into a store
// ---------------------------------------------------------------------------

test("every block a client receives is validated into its store", async () => {
  const atlas = demoChain("atlas");
  const server = new DialogServer({ store: storeOf(atlas.items) });
  const store = new BlockStore();
  const answer = await clientFor(server, { store }).range(atlas.pub);

  assert.equal(answer.ingested?.accepted.length, 6);
  assert.equal(answer.ingested?.held.length, 0);
  assert.equal(answer.ingested?.rejected.length, 0);
  assert.equal(store.validBlocks().length, 6);
});

/** A block carrying a signature that is genuine over *another* block: it
 * decodes, it hashes to its own digest, and validation rule 2 refuses it.
 * Nothing a source does can make it valid, and no source could have made a
 * genuine block look like this. */
function forgedBlock() {
  const genuine = publicBlock(ALICE, { ops: [createAtom("a block Alice did sign")] });
  return sequenceItem(
    newPublicBlock({
      pub: ALICE_PUB,
      prev: null,
      refs: [],
      ts: 1740000000,
      ops: [createAtom("a block Alice did not sign")],
      sig: genuine.block.sig,
    }),
  );
}

test("an invalid block is the block's own fault, and it never reaches the store", async () => {
  const forged = forgedBlock();
  const store = new BlockStore();
  // The source answers honestly: the bytes hash to exactly the digest that was
  // asked for. The block is still refused, by the client's own validation.
  const client = clientAgainst(() => sequenceResponse(encodeBlockSequence([forged])), { store });

  const answer = await client.blocks([forged.digest]);
  assert.equal(answer.items.length, 1, "it hashes to what was asked for");
  assert.equal(answer.ingested?.rejected.length, 1);
  assert.match(answer.ingested!.rejected[0]!.reason, /signature/);
  assert.equal(store.size, 0);
});

// ---------------------------------------------------------------------------
// announce
// ---------------------------------------------------------------------------

/** Alice's block names a bond and two atoms Bob's block defines: the one shape
 * in which a block of a legitimate announce is held pending another block of
 * the same announce. */
function crossReferencedPair(): { alice: { bytes: Uint8Array; digest: Uint8Array }; bob: { bytes: Uint8Array; digest: Uint8Array } } {
  const bond = createBond("_A_ is the capital of _B_");
  const paris = createAtom("Paris");
  const france = createAtom("France");
  const bob = publicBlock(BOB, { ops: [bond, paris, france] });
  const alice = publicBlock(ALICE, {
    refs: [bob.digest],
    ops: [
      createMolecule(operationDigest(bond), [
        atomFiller(operationDigest(paris)),
        atomFiller(operationDigest(france)),
      ]),
    ],
  });
  return { alice, bob };
}

test("announce: a block held for want of a definition is accepted once the definition arrives", async () => {
  const { alice, bob } = crossReferencedPair();
  const store = new BlockStore();
  const client = clientFor(new DialogServer({ store, announce: true }));

  const first = await client.announce([alice.bytes]);
  assert.deepEqual(first.receipt?.held, [digestToCidText(alice.digest)]);
  assert.deepEqual(first.receipt?.accepted, []);
  assert.deepEqual(first.receipt?.rejected, {});

  const second = await client.announce([bob.bytes]);
  assert.deepEqual(second.receipt?.accepted, [digestToCidText(bob.digest)]);
  assert.equal(store.get(alice.digest)?.valid, true, "the arrival settled the held block");
});

test("announce: a disposition is decided after the whole sequence, not as each block is offered", async () => {
  const { alice, bob } = crossReferencedPair();
  const store = new BlockStore();
  const client = clientFor(new DialogServer({ store, announce: true }));

  // Alice's block is offered before the block that settles it. Read block by
  // block it is held; read after the whole sequence — which is what the profile
  // requires — it is accepted.
  const receipt = (await client.announce([alice.bytes, bob.bytes])).receipt!;
  assert.deepEqual(receipt.held, []);
  assert.deepEqual(new Set(receipt.accepted), new Set([alice, bob].map((b) => digestToCidText(b.digest))));

  // And announcing the same sequence twice produces the same receipt, which a
  // block-by-block reading does not guarantee.
  const again = (await client.announce([alice.bytes, bob.bytes])).receipt!;
  assert.deepEqual(new Set(again.accepted), new Set(receipt.accepted));
  assert.deepEqual(again.held, []);
});

test("announce: a block that fails validation is rejected, in prose meant for a person", async () => {
  const tampered = forgedBlock().bytes;
  const store = new BlockStore();
  const client = clientFor(new DialogServer({ store, announce: true }));
  const receipt = (await client.announce([tampered])).receipt!;

  const cid = Object.keys(receipt.rejected)[0]!;
  assert.equal(Object.keys(receipt.rejected).length, 1);
  assert.match(receipt.rejected[cid]!, /signature/);
  assert.deepEqual(receipt.accepted, []);
  assert.deepEqual(receipt.held, []);
  assert.equal(store.size, 0);

  // Every submitted block appears in exactly one of the three.
  const submitted = new Set([cid]);
  assert.deepEqual(
    new Set([...receipt.accepted, ...receipt.held, ...Object.keys(receipt.rejected)]),
    submitted,
  );
});

test("announce: a body that is not a block sequence is a 400 and never a partial receipt", async () => {
  const store = new BlockStore();
  const client = clientFor(new DialogServer({ store, announce: true }));
  const chain = chainOf(ALICE, 1);
  const truncated = chain[0]!.bytes.subarray(0, chain[0]!.bytes.length - 3);

  await assert.rejects(client.announce([truncated]), (error: unknown) => {
    assert.ok(error instanceof TransportError);
    assert.equal(error.status, 400);
    return true;
  });
  assert.equal(store.size, 0, "a sequence is malformed as a whole");
});

test("announce: a server that does not offer it says so, and asking again will not help", async () => {
  const client = clientFor(new DialogServer({ store: new BlockStore() }));
  await assert.rejects(client.announce(chainOf(ALICE, 1)), (error: unknown) => {
    assert.ok(error instanceof TransportError);
    assert.equal(error.operationNotOffered, true);
    assert.equal(error.notHeld, false);
    return true;
  });
});

test("announce: a source may refuse one outright, for reasons that are its own policy", async () => {
  const store = new BlockStore();
  const client = clientFor(
    new DialogServer({
      store,
      announce: true,
      // Quota, rate, acquaintance, disk: a refusal by policy is 403 with the
      // profile's own problem type.
      refuseAnnounce: () => ({ detail: "this server takes announces from acquaintances only" }),
    }),
  );
  const announced = chainOf(ALICE, 1);
  await assert.rejects(client.announce(announced), (error: unknown) => {
    assert.ok(error instanceof TransportError);
    assert.equal(error.status, 403);
    assert.equal(error.announceRefused, true);
    assert.equal(error.problem?.type, PROBLEM_ANNOUNCE_REFUSED);
    // Distinct from the 404 of a server that does not offer announce at all,
    // which is a fact about the server rather than about the request.
    assert.equal(error.operationNotOffered, false);
    assert.equal(error.notHeld, false);
    return true;
  });

  // It carries no receipt: nothing was judged, so there are no dispositions to
  // report and the client reads no verdict about the blocks it announced.
  assert.equal(store.size, 0);
  assert.equal(store.has(announced[0]!.digest), false);

  // And the blocks are not implicated: another source takes them.
  const elsewhere = new BlockStore();
  const other = clientFor(new DialogServer({ store: elsewhere, announce: true }));
  const receipt = (await other.announce(announced)).receipt!;
  assert.deepEqual(receipt.accepted, [digestToCidText(announced[0]!.digest)]);
});

test("announce: acceptance is not endorsement, and the same bytes come back out", async () => {
  const chain = chainOf(ALICE, 3);
  const upstream = new BlockStore();
  const client = clientFor(new DialogServer({ store: upstream, announce: true }));
  await client.announce(chain.map((item) => item.bytes));

  const range = await clientFor(new DialogServer({ store: upstream })).range(ALICE_PUB);
  assert.deepEqual(
    encodeBlockSequence(range.items),
    encodeBlockSequence(chain),
    "it is the same bytes moving the other way",
  );
});

// ---------------------------------------------------------------------------
// Over a real socket
// ---------------------------------------------------------------------------

test("the same server and client work over Node's http, with the runtime's own fetch", async () => {
  const { createServer } = await import("node:http");
  const { nodeListener } = await import("../src/node-http.ts");

  const atlas = demoChain("atlas");
  const dialog = new DialogServer({ store: storeOf(atlas.items), announce: true });
  const http = createServer(nodeListener(dialog.fetch));
  await new Promise<void>((resolve) => http.listen(0, "127.0.0.1", resolve));
  const address = http.address();
  if (address === null || typeof address === "string") throw new Error("no port");

  try {
    const store = new BlockStore();
    const client = new DialogClient({
      baseUrl: `http://127.0.0.1:${address.port}${DEFAULT_BASE_PATH}`,
      store,
    });

    const tip = await client.tip(atlas.pub);
    assert.equal(tip.status, "block");
    assert.equal(tip.etagVerified, true);

    const notModified = await client.tip(atlas.pub, { ifNoneMatch: tip.etag! });
    assert.equal(notModified.status, "not-modified");

    const range = await client.range(atlas.pub);
    assert.equal(range.items.length, 6);
    assert.equal(range.caughtUp, true);
    assert.equal(store.validBlocks().length, 6);

    const byDigest = await client.block(atlas.items[2]!.digest);
    assert.deepEqual(byDigest.item?.bytes, atlas.items[2]!.bytes);

    const batch = await client.blocks(atlas.items.map((item) => item.digest));
    assert.equal(batch.items.length, 6);

    const siblings = await client.siblings(atlas.pub, null);
    assert.equal(siblings.items.length, 1);

    const mine = chainOf(ALICE, 1);
    const receipt = await client.announce(mine.map((item) => item.bytes));
    assert.deepEqual(receipt.receipt?.accepted, [digestToCidText(mine[0]!.digest)]);

    const missing = await client.block(chainOf(BOB, 1)[0]!.digest);
    assert.equal(missing.failed, "not-held");

    // Nothing between the socket and the server percent-decodes the request
    // target: the canonical form is applied to it as received, so an encoded
    // octet is a 400 out here too and never a second URL for one block.
    const base = `http://127.0.0.1:${address.port}${DEFAULT_BASE_PATH}`;
    const cid = atlas.index[0]!.cid;
    assert.equal((await fetch(`${base}/blocks/${cid}`)).status, 200);
    assert.equal((await fetch(`${base}/blocks/%62${cid.slice(1)}`)).status, 400);
    assert.equal(
      (await fetch(`${base}/chains/${atlas.keyText}/blocks?limit=%201`)).status,
      400,
    );
  } finally {
    await new Promise<void>((resolve) => http.close(() => resolve()));
  }
});

test("a client names a chain by its author key in the canonical text form", async () => {
  const atlas = demoChain("atlas");
  let path = "";
  const client = new DialogClient({
    baseUrl: BASE,
    fetch: async (input) => {
      path = new URL(input as string).pathname;
      return new Response(null, { status: 404 });
    },
  });
  await client.tip(atlas.pub);
  assert.equal(path, `${DEFAULT_BASE_PATH}/chains/${atlas.keyText}/tip`);
  assert.equal(authorKeyToText(atlas.pub), atlas.keyText);

  await client.block(atlas.items[0]!.digest);
  assert.equal(path, `${DEFAULT_BASE_PATH}/blocks/${atlas.index[0]!.cid}`);
});

test("a client re-signs nothing and re-encodes nothing: the bytes it announces are the bytes it holds", async () => {
  const chain = chainOf(ALICE, 1);
  let body = new Uint8Array(0);
  const client = new DialogClient({
    baseUrl: BASE,
    fetch: async (_input, init) => {
      body = new Uint8Array(await new Request("http://x/", init).arrayBuffer());
      return new Response(JSON.stringify({ accepted: [], held: [], rejected: {} }), {
        status: 200,
        headers: { "Content-Type": JSON_TYPE },
      });
    },
  });
  // Announcing a block object rather than bytes encodes it canonically, which
  // is the same bytes its digest and signature are over.
  const block = signBlock(
    unsignedPublicBlock({ pub: ALICE_PUB, prev: null, refs: [], ts: 1740000000, ops: [createAtom("block 0")] }),
    ALICE,
  );
  await client.announce([block]);
  assert.deepEqual(body, encodeBlock(block));
  assert.deepEqual(body, chain[0]!.bytes);
});
