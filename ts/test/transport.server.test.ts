/**
 * The server half of `spec/07-transport.md`'s HTTP binding: the six paths, the
 * media types, the headers, the status codes, and the one spelling of every
 * identifier the binding admits.
 *
 * These are conformance tables read straight off the profile. Where the
 * specification names a value — a media type, a header, a status code, a
 * malformed spelling — the value is asserted here rather than paraphrased.
 */

import assert from "node:assert/strict";
import test from "node:test";

import { BlockStore, createAtom } from "../src/block.ts";
import { compareBytes, decodeBlockSequence, encodeBlockSequence } from "../src/blockseq.ts";
import {
  BLOCK_SEQUENCE_TYPE,
  DEFAULT_BASE_PATH,
  DialogServer,
  JSON_TYPE,
  MIN_FETCH_DIGESTS,
  PROBLEM_BLANK,
  PROBLEM_NOT_HELD,
  PROBLEM_OPERATION_NOT_OFFERED,
  PROBLEM_TYPE,
  TIP_HEADER,
  digestToCidText,
  mediaType,
} from "../src/transport.ts";
import { authorKeyToText as authorText } from "../src/cid.ts";
import { bytesToHex } from "../src/hex.ts";
import { ALICE, chainOf, demoChain, demoChains, publicBlock } from "./chains.ts";

const ORIGIN = "http://mirror.example";

/** A store holding the three committed demo chains, atlas first — the order in
 * which nothing is ever held for want of an ancestor. */
function demoStore(): BlockStore {
  const store = new BlockStore();
  for (const chain of demoChains()) {
    for (const item of chain.items) store.add(item.bytes);
  }
  return store;
}

function serverOver(store: BlockStore, options: { announce?: boolean } = {}): DialogServer {
  return new DialogServer({ store, ...(options.announce === undefined ? {} : options) });
}

function url(path: string): string {
  return `${ORIGIN}${DEFAULT_BASE_PATH}${path}`;
}

async function get(
  server: DialogServer,
  path: string,
  init: RequestInit = {},
): Promise<Response> {
  return server.handle(new Request(url(path), init));
}

async function sequenceOf(response: Response) {
  return decodeBlockSequence(new Uint8Array(await response.arrayBuffer()));
}

async function problemOf(response: Response): Promise<Record<string, unknown>> {
  assert.equal(mediaType(response.headers.get("Content-Type")), PROBLEM_TYPE);
  return (await response.json()) as Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// tip
// ---------------------------------------------------------------------------

test("tip answers the block itself, with a strong ETag and Dialog-Tip naming it", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const response = await get(server, `/chains/${atlas.keyText}/tip`);

  assert.equal(response.status, 200);
  assert.equal(mediaType(response.headers.get("Content-Type")), BLOCK_SEQUENCE_TYPE);
  assert.equal(response.headers.get("Cache-Control"), "no-cache");

  const tip = atlas.items.at(-1)!;
  const cid = digestToCidText(tip.digest);
  assert.equal(response.headers.get(TIP_HEADER), cid);
  assert.equal(response.headers.get("ETag"), `"${cid}"`);
  // The example of spec/07: atlas's tip is 334 bytes and this CID.
  assert.equal(cid, "bafyreifvr7u624ffnnmymo5cvo4ipzx26l6bwmxcnlsmwijqgmcododv4e");

  const items = await sequenceOf(response);
  assert.equal(items.length, 1);
  assert.deepEqual(items[0]!.digest, tip.digest, "the client hashes the bytes it received");
  assert.equal(items[0]!.bytes.length, 334);
});

test("tip answers 304 to a matching If-None-Match, and 200 to a stale one", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const etag = `"${digestToCidText(atlas.items.at(-1)!.digest)}"`;
  const stale = `"${digestToCidText(atlas.items[0]!.digest)}"`;

  for (const header of [etag, `W/${etag}`, "*", `${stale}, ${etag}`]) {
    const response = await get(server, `/chains/${atlas.keyText}/tip`, {
      headers: { "If-None-Match": header },
    });
    assert.equal(response.status, 304, header);
    // A 304 SHOULD carry the header alongside the ETag; a client MUST NOT
    // require it there.
    assert.equal(response.headers.get("ETag"), etag);
    assert.equal(response.headers.get(TIP_HEADER), digestToCidText(atlas.items.at(-1)!.digest));
  }

  const response = await get(server, `/chains/${atlas.keyText}/tip`, {
    headers: { "If-None-Match": stale },
  });
  assert.equal(response.status, 200);
});

test("tip answers 404 with not-held, and no Dialog-Tip, for an author with no tip", async () => {
  const unknown = chainOf(ALICE, 1)[0]!;
  const server = serverOver(demoStore());
  const response = await get(server, `/chains/${authorText(unknown.block.pub)}/tip`);

  assert.equal(response.status, 404);
  assert.equal(response.headers.get(TIP_HEADER), null);
  const problem = await problemOf(response);
  assert.equal(problem["type"], PROBLEM_NOT_HELD);
  assert.equal(problem["status"], 404);
});

// ---------------------------------------------------------------------------
// range
// ---------------------------------------------------------------------------

test("a whole-chain range is the author's .block files concatenated, byte for byte", async () => {
  const server = serverOver(demoStore());
  for (const chain of demoChains()) {
    const response = await get(server, `/chains/${chain.keyText}/blocks`);
    assert.equal(response.status, 200);
    assert.equal(mediaType(response.headers.get("Content-Type")), BLOCK_SEQUENCE_TYPE);
    assert.equal(
      response.headers.get(TIP_HEADER),
      digestToCidText(chain.items.at(-1)!.digest),
    );
    const body = new Uint8Array(await response.arrayBuffer());
    assert.deepEqual(
      body,
      encodeBlockSequence(chain.files),
      `${chain.name}: the range response is the concatenated block files`,
    );
    assert.equal(body.length, chain.index.reduce((total, entry) => total + entry.size, 0));
  }
});

test("the position is exclusive, and a range continues from the last block received", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());

  const first = await get(server, `/chains/${atlas.keyText}/blocks?limit=2`);
  const page = await sequenceOf(first);
  assert.equal(page.length, 2);
  assert.deepEqual(page[0]!.digest, atlas.items[0]!.digest);

  const after = digestToCidText(page.at(-1)!.digest);
  const second = await get(server, `/chains/${atlas.keyText}/blocks?after=${after}&limit=2`);
  const next = await sequenceOf(second);
  assert.deepEqual(
    next.map((item) => bytesToHex(item.digest)),
    atlas.items.slice(2, 4).map((item) => bytesToHex(item.digest)),
    "the block naming the position is not in the response; the blocks after it are",
  );
});

test("a range at the tip is an empty sequence, and still carries Dialog-Tip", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const tip = atlas.items.at(-1)!;
  const response = await get(
    server,
    `/chains/${atlas.keyText}/blocks?after=${digestToCidText(tip.digest)}`,
  );
  assert.equal(response.status, 200);
  assert.equal((await sequenceOf(response)).length, 0);
  assert.equal(response.headers.get(TIP_HEADER), digestToCidText(tip.digest));
});

test("a range for an author with no tip is 200, empty, and omits the header", async () => {
  const unknown = chainOf(ALICE, 1)[0]!;
  const server = serverOver(demoStore());
  const response = await get(server, `/chains/${authorText(unknown.block.pub)}/blocks`);
  assert.equal(response.status, 200);
  assert.equal((await sequenceOf(response)).length, 0);
  // Omitted rather than given some empty or null value: the header's value is a
  // CID text form and this profile mints no second spelling of a position.
  assert.equal(response.headers.get(TIP_HEADER), null);
});

test("a server MUST NOT return more blocks than the requested maximum, and MAY return fewer", async () => {
  const atlas = demoChain("atlas");
  const store = demoStore();
  const generous = new DialogServer({ store });
  const capped = new DialogServer({ store, maxRangeLimit: 2 });

  assert.equal((await sequenceOf(await get(generous, `/chains/${atlas.keyText}/blocks?limit=3`))).length, 3);
  assert.equal(
    (await sequenceOf(await get(capped, `/chains/${atlas.keyText}/blocks?limit=6`))).length,
    2,
    "a server MUST NOT exceed its own cap",
  );
});

test("a store with a hole reports no tip and serves no range across the hole", async () => {
  // Blocks 3, 4 and 5 of a chain whose first three never arrived.
  const atlas = demoChain("atlas");
  const store = new BlockStore();
  for (const item of atlas.items.slice(3)) store.add(item.bytes);
  const server = serverOver(store);

  const tip = await get(server, `/chains/${atlas.keyText}/tip`);
  assert.equal(tip.status, 404, "the walk cannot start, so there is no tip");

  const range = await get(server, `/chains/${atlas.keyText}/blocks`);
  assert.equal(range.status, 200);
  assert.equal((await sequenceOf(range)).length, 0);
  assert.equal(range.headers.get(TIP_HEADER), null);

  // block and blocks make no claim about a chain, and a store with a hole can
  // answer them honestly.
  const held = atlas.items[4]!;
  const byDigest = await get(server, `/blocks/${digestToCidText(held.digest)}`);
  assert.equal(byDigest.status, 200);
  assert.deepEqual((await sequenceOf(byDigest))[0]!.digest, held.digest);
});

// ---------------------------------------------------------------------------
// block and blocks
// ---------------------------------------------------------------------------

test("a block response is immutable and cacheable for a year", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const wanted = atlas.items[3]!;
  const response = await get(server, `/blocks/${digestToCidText(wanted.digest)}`);

  assert.equal(response.status, 200);
  assert.equal(mediaType(response.headers.get("Content-Type")), BLOCK_SEQUENCE_TYPE);
  assert.equal(response.headers.get("Cache-Control"), "public, max-age=31536000, immutable");
  const items = await sequenceOf(response);
  assert.equal(items.length, 1);
  assert.deepEqual(items[0]!.bytes, wanted.bytes);
});

test("a block the source does not hold is 404 not-held, never it does not exist", async () => {
  const absent = chainOf(ALICE, 1)[0]!;
  const server = serverOver(demoStore());
  const response = await get(server, `/blocks/${digestToCidText(absent.digest)}`);
  assert.equal(response.status, 404);
  assert.equal((await problemOf(response))["type"], PROBLEM_NOT_HELD);
});

test("blocks returns the subset held, in the order requested", async () => {
  // The profile's own example: errata's genesis block names three digests, all
  // of them atlas's, and the three go out in one request.
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const absent = chainOf(ALICE, 1)[0]!;
  const wanted = [atlas.items[3]!, atlas.items[0]!, absent, atlas.items[1]!];

  const response = await server.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: JSON.stringify({ digests: wanted.map((item) => digestToCidText(item.digest)) }),
    }),
  );
  assert.equal(response.status, 200);
  assert.equal(mediaType(response.headers.get("Content-Type")), BLOCK_SEQUENCE_TYPE);
  const items = await sequenceOf(response);
  assert.deepEqual(
    items.map((item) => bytesToHex(item.digest)),
    [atlas.items[3]!, atlas.items[0]!, atlas.items[1]!].map((item) => bytesToHex(item.digest)),
    "digests the source does not hold are simply not in the response",
  );
});

test("a conforming server accepts a blocks request naming at least 256 digests", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const digests = [digestToCidText(atlas.items[0]!.digest)];
  // 255 digests nobody holds, plus one that is held: 256 in all, the scan
  // limit's whole default budget in one exchange.
  for (const item of chainOf(ALICE, MIN_FETCH_DIGESTS - 1, { label: "filler" })) {
    digests.push(digestToCidText(item.digest));
  }
  assert.equal(digests.length, MIN_FETCH_DIGESTS);

  const response = await server.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: JSON.stringify({ digests }),
    }),
  );
  assert.equal(response.status, 200);
  assert.equal((await sequenceOf(response)).length, 1);

  // And a server MAY refuse a larger one with 413.
  const small = new DialogServer({ store: demoStore(), maxFetchDigests: MIN_FETCH_DIGESTS });
  const overflow = await small.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: JSON.stringify({ digests: [...digests, digestToCidText(atlas.items[1]!.digest)] }),
    }),
  );
  assert.equal(overflow.status, 413);

  assert.throws(
    () => new DialogServer({ store: demoStore(), maxFetchDigests: 255 }),
    RangeError,
    "a server that would not accept 256 is not conforming",
  );
});

test("a blocks request naming the same digest twice is refused", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const cid = digestToCidText(atlas.items[0]!.digest);
  const response = await server.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: JSON.stringify({ digests: [cid, cid] }),
    }),
  );
  assert.equal(response.status, 400);
});

test("a blocks request under the wrong media type is 415, and a malformed one 400", async () => {
  const server = serverOver(demoStore());
  const body = JSON.stringify({ digests: [] });

  const wrongType = await server.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": BLOCK_SEQUENCE_TYPE },
      body,
    }),
  );
  assert.equal(wrongType.status, 415);

  for (const bad of ["[]", "{}", '{"digests": "x"}', '{"digests": [1]}', "not json"]) {
    const response = await server.handle(
      new Request(url("/blocks/fetch"), {
        method: "POST",
        headers: { "Content-Type": JSON_TYPE },
        body: bad,
      }),
    );
    assert.equal(response.status, 400, bad);
  }

  const notCid = await server.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: JSON.stringify({ digests: ["5693c25cc1b72b71fca62897d451d821a3e6dc4738c76c42d98d333a96d91fa9"] }),
    }),
  );
  assert.equal(notCid.status, 400, "hexadecimal MUST NOT appear as an identifier");
});

// ---------------------------------------------------------------------------
// siblings and the fork branch
// ---------------------------------------------------------------------------

test("siblings names every block held at a position, in ascending digest order, with no winner", async () => {
  const [genesis] = chainOf(ALICE, 1);
  const left = publicBlock(ALICE, { prev: genesis!.digest, ts: 2, ops: [createAtom("left")] });
  const right = publicBlock(ALICE, { prev: genesis!.digest, ts: 3, ops: [createAtom("right")] });
  const store = new BlockStore();
  for (const item of [genesis!, left, right]) store.add(item.bytes);
  const server = serverOver(store);
  const author = authorText(genesis!.block.pub);

  const response = await get(
    server,
    `/chains/${author}/siblings?prev=${digestToCidText(genesis!.digest)}`,
  );
  assert.equal(response.status, 200);
  const items = await sequenceOf(response);
  const expected = [left, right].sort((a, b) => compareBytes(a.digest, b.digest));
  assert.deepEqual(
    items.map((item) => bytesToHex(item.digest)),
    expected.map((item) => bytesToHex(item.digest)),
  );

  // Including the one it would itself serve from range and tip, so the client
  // sees a set rather than a difference.
  const tip = await get(server, `/chains/${author}/tip`);
  const served = (await sequenceOf(tip))[0]!;
  assert.ok(items.some((item) => bytesToHex(item.digest) === bytesToHex(served.digest)));

  // The genesis position asks the question for the start of the chain.
  const atGenesis = await get(server, `/chains/${author}/siblings`);
  assert.deepEqual(
    (await sequenceOf(atGenesis)).map((item) => bytesToHex(item.digest)),
    [bytesToHex(genesis!.digest)],
  );
});

test("the fork branch is deterministic and stable, and tip and range agree on it", async () => {
  const [genesis] = chainOf(ALICE, 1);
  const left = publicBlock(ALICE, { prev: genesis!.digest, ts: 2, ops: [createAtom("left")] });
  const right = publicBlock(ALICE, { prev: genesis!.digest, ts: 3, ops: [createAtom("right")] });
  const store = new BlockStore();
  for (const item of [genesis!, left, right]) store.add(item.bytes);
  const server = serverOver(store);
  const author = authorText(genesis!.block.pub);
  const lowest = compareBytes(left.digest, right.digest) < 0 ? left : right;

  for (let attempt = 0; attempt < 3; attempt++) {
    const tip = await get(server, `/chains/${author}/tip`);
    assert.equal(tip.headers.get(TIP_HEADER), digestToCidText(lowest.digest));
    const range = await sequenceOf(await get(server, `/chains/${author}/blocks`));
    assert.deepEqual(
      range.map((item) => bytesToHex(item.digest)),
      [genesis!, lowest].map((item) => bytesToHex(item.digest)),
      "a range follows one branch only and never interleaves",
    );
  }
});

// ---------------------------------------------------------------------------
// Methods, negotiation and the optional operations
// ---------------------------------------------------------------------------

test("HEAD is supported wherever GET is, with the headers and no body", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  for (const path of [
    `/chains/${atlas.keyText}/tip`,
    `/chains/${atlas.keyText}/blocks`,
    `/chains/${atlas.keyText}/siblings`,
    `/blocks/${digestToCidText(atlas.items[0]!.digest)}`,
  ]) {
    const head = await get(server, path, { method: "HEAD" });
    const body = await get(server, path);
    assert.equal(head.status, body.status, path);
    assert.equal(head.headers.get("Content-Type"), body.headers.get("Content-Type"));
    assert.equal(head.headers.get("Content-Length"), body.headers.get("Content-Length"));
    assert.equal((await head.arrayBuffer()).byteLength, 0);
  }
});

test("any other method on a defined path is 405 with an Allow header", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore(), { announce: true });
  const cases: [string, string, string][] = [
    [`/chains/${atlas.keyText}/tip`, "POST", "GET, HEAD"],
    [`/chains/${atlas.keyText}/blocks`, "DELETE", "GET, HEAD"],
    [`/blocks/${digestToCidText(atlas.items[0]!.digest)}`, "PUT", "GET, HEAD"],
    ["/blocks/fetch", "GET", "POST"],
    ["/announce", "GET", "POST"],
  ];
  for (const [path, method, allow] of cases) {
    const response = await get(server, path, { method });
    assert.equal(response.status, 405, `${method} ${path}`);
    assert.equal(response.headers.get("Allow"), allow);
  }
});

test("an Accept that excludes the only type the server can send is 406", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const path = `/chains/${atlas.keyText}/tip`;

  for (const accept of ["application/json", "text/html", `${BLOCK_SEQUENCE_TYPE};q=0`]) {
    const response = await get(server, path, { headers: { Accept: accept } });
    assert.equal(response.status, 406, accept);
  }
  for (const accept of [
    BLOCK_SEQUENCE_TYPE,
    "*/*",
    "application/*",
    `text/html;q=0.9, ${BLOCK_SEQUENCE_TYPE}`,
  ]) {
    const response = await get(server, path, { headers: { Accept: accept } });
    assert.equal(response.status, 200, accept);
  }
});

test("an OPTIONAL operation this server does not offer answers 404 with its own problem type", async () => {
  const server = serverOver(demoStore());
  const announce = await server.handle(
    new Request(url("/announce"), {
      method: "POST",
      headers: { "Content-Type": BLOCK_SEQUENCE_TYPE },
      body: encodeBlockSequence([]),
    }),
  );
  assert.equal(announce.status, 404);
  assert.equal((await problemOf(announce))["type"], PROBLEM_OPERATION_NOT_OFFERED);

  const events = await get(server, "/events?author=b5ua");
  assert.equal(events.status, 404);
  assert.equal((await problemOf(events))["type"], PROBLEM_OPERATION_NOT_OFFERED);
});

test("a path no operation is bound to is 404, with about:blank", async () => {
  const server = serverOver(demoStore());
  for (const path of ["/", "/chains", "/chains/x/y/z", "/whatever"]) {
    const response = await get(server, path);
    assert.equal(response.status, 404, path);
    assert.equal((await problemOf(response))["type"], PROBLEM_BLANK);
  }
  const outside = await server.handle(new Request(`${ORIGIN}/elsewhere`));
  assert.equal(outside.status, 404);
});

// ---------------------------------------------------------------------------
// One spelling of everything
// ---------------------------------------------------------------------------

test("a non-canonical author key or CID is 400 rather than normalized", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const cid = digestToCidText(atlas.items[0]!.digest);

  const authorSpellings = [
    atlas.keyText.toUpperCase(),
    `B${atlas.keyText.slice(1)}`,
    atlas.keyText.slice(0, -1),
    `${atlas.keyText}=`,
    // Hexadecimal is a byte dump, not an identifier.
    bytesToHex(atlas.pub),
    // A percent-encoded first character decodes to the same string and is a
    // second spelling of it; this server refuses to mint the alias.
    `%62${atlas.keyText.slice(1)}`,
    cid,
  ];
  for (const spelling of authorSpellings) {
    for (const suffix of ["tip", "blocks", "siblings"]) {
      const response = await get(server, `/chains/${spelling}/${suffix}`);
      assert.equal(response.status, 400, `${spelling} ${suffix}`);
    }
  }

  const cidSpellings = [
    cid.toUpperCase(),
    `B${cid.slice(1)}`,
    `${cid}=`,
    bytesToHex(atlas.items[0]!.digest),
    `%62${cid.slice(1)}`,
    atlas.keyText,
  ];
  for (const spelling of cidSpellings) {
    assert.equal((await get(server, `/blocks/${spelling}`)).status, 400, spelling);
  }
});

test("a position has exactly one spelling, and the genesis position is the absence of one", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());

  // Omitted denotes the genesis position.
  assert.equal((await sequenceOf(await get(server, `/chains/${atlas.keyText}/blocks`))).length, 6);

  for (const spelling of ["null", "", "genesis", "0"]) {
    assert.equal(
      (await get(server, `/chains/${atlas.keyText}/blocks?after=${spelling}`)).status,
      400,
      `after=${spelling}`,
    );
    assert.equal(
      (await get(server, `/chains/${atlas.keyText}/siblings?prev=${spelling}`)).status,
      400,
      `prev=${spelling}`,
    );
  }
});

test("limit has exactly one spelling", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const path = `/chains/${atlas.keyText}/blocks`;

  for (const spelling of ["01", "+1", "-1", "1.0", "%201", " 1", "1e3", "0", "", "one", "1_000"]) {
    const response = await get(server, `${path}?limit=${spelling}`);
    assert.equal(response.status, 400, `limit=${spelling}`);
  }
  // And a value too large to be a plausible count of blocks.
  assert.equal((await get(server, `${path}?limit=99999999999999999999`)).status, 400);

  for (const spelling of ["1", "6", "1024"]) {
    assert.equal((await get(server, `${path}?limit=${spelling}`)).status, 200, spelling);
  }
});

test("a query parameter given more than once is malformed", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const cid = digestToCidText(atlas.items[0]!.digest);
  const blocks = `/chains/${atlas.keyText}/blocks`;

  assert.equal((await get(server, `${blocks}?after=${cid}&after=${cid}`)).status, 400);
  assert.equal((await get(server, `${blocks}?limit=1&limit=2`)).status, 400);
  assert.equal(
    (await get(server, `/chains/${atlas.keyText}/siblings?prev=${cid}&prev=${cid}`)).status,
    400,
  );
});

test("a server that does not implement long polling ignores wait and answers immediately", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const response = await get(server, `/chains/${atlas.keyText}/tip?wait=30`);
  assert.equal(response.status, 200);
  assert.equal(
    response.headers.get(TIP_HEADER),
    digestToCidText(atlas.items.at(-1)!.digest),
    "the parameter degrades to polling rather than being refused",
  );
});


test("a parameter the operation does not define is ignored, as the long-poll rule requires", async () => {
  // The only thing the profile says about a parameter a server does not
  // implement is that `wait` MUST be ignored, so that long polling degrades to
  // polling. This server ignores every parameter the matched operation does not
  // define, which is the reading that keeps that rule true without a special
  // case — see todos/095.
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const cid = digestToCidText(atlas.items[0]!.digest);

  const range = await get(server, `/chains/${atlas.keyText}/blocks?prev=${cid}&page=3`);
  assert.equal(range.status, 200);
  assert.equal(
    (await sequenceOf(range)).length,
    6,
    "prev names nothing on a range, so the range is from the genesis position",
  );

  const siblings = await get(server, `/chains/${atlas.keyText}/siblings?after=${cid}`);
  assert.equal(siblings.status, 200);
  assert.equal((await sequenceOf(siblings)).length, 1, "the genesis position's sibling set");
});

test("a blocks response is cacheable but not immutable: the subset a source holds grows", async () => {
  const atlas = demoChain("atlas");
  const server = serverOver(demoStore());
  const response = await server.handle(
    new Request(url("/blocks/fetch"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: JSON.stringify({ digests: [digestToCidText(atlas.items[0]!.digest)] }),
    }),
  );
  assert.equal(response.headers.get("Cache-Control"), "no-cache");
});

test("an announce body under the generic CBOR-sequence type is accepted", async () => {
  // A chain file offered to a server is a valid announce body, and a plain file
  // server labels one with the generic type — see todos/094.
  const store = new BlockStore();
  const server = new DialogServer({ store, announce: true });
  const chain = chainOf(ALICE, 2);

  const response = await server.handle(
    new Request(url("/announce"), {
      method: "POST",
      headers: { "Content-Type": "application/cbor-seq" },
      body: encodeBlockSequence(chain),
    }),
  );
  assert.equal(response.status, 200);
  assert.equal(mediaType(response.headers.get("Content-Type")), JSON_TYPE);
  assert.equal(store.validBlocks().length, 2);

  const wrong = await server.handle(
    new Request(url("/announce"), {
      method: "POST",
      headers: { "Content-Type": JSON_TYPE },
      body: encodeBlockSequence(chain),
    }),
  );
  assert.equal(wrong.status, 415);
});

test("an announce larger than the server accepts is 413, and nothing is stored", async () => {
  const store = new BlockStore();
  const server = new DialogServer({ store, announce: true, maxAnnounceBytes: 32 });
  const response = await server.handle(
    new Request(url("/announce"), {
      method: "POST",
      headers: { "Content-Type": BLOCK_SEQUENCE_TYPE },
      body: encodeBlockSequence(chainOf(ALICE, 1)),
    }),
  );
  assert.equal(response.status, 413);
  assert.equal(store.size, 0);
});
