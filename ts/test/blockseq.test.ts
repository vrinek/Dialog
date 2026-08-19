/**
 * The block sequence of `spec/07-transport.md`: RFC 8742 framing (none), the
 * ordering each operation fixes, and the file form.
 *
 * The committed demo chains are the ground truth for the file form: the
 * profile states that concatenating one author's `.block` files in the order
 * the index lists them yields, byte for byte, the range response for that
 * author's whole chain from the genesis position. That equality is a test
 * rather than a claim, and it is run here against the committed directory.
 */

import assert from "node:assert/strict";
import test from "node:test";

import { encodeBlock } from "../src/block.ts";
import {
  BlockSequenceError,
  checkRangeOrder,
  checkSiblingOrder,
  compareBytes,
  decodeBlockSequence,
  encodeBlockSequence,
} from "../src/blockseq.ts";
import { cidToText } from "../src/cid.ts";
import { encode } from "../src/dcbor.ts";
import { digestToCidText } from "../src/transport.ts";
import { bytesToHex } from "../src/hex.ts";
import { ALICE, BOB, chainOf, demoChains, publicBlock } from "./chains.ts";
import { createAtom } from "../src/block.ts";

test("an empty sequence is a zero-length byte string meaning none", () => {
  assert.equal(encodeBlockSequence([]).length, 0);
  assert.deepEqual(decodeBlockSequence(new Uint8Array(0)), []);
});

test("a sequence is the blocks concatenated, with no framing of any kind", () => {
  const chain = chainOf(ALICE, 3);
  const bytes = encodeBlockSequence(chain);
  assert.equal(
    bytes.length,
    chain.reduce((total, item) => total + item.bytes.length, 0),
    "no length prefix, no wrapper and no separator",
  );

  const read = decodeBlockSequence(bytes);
  assert.equal(read.length, 3);
  for (const [index, item] of read.entries()) {
    assert.deepEqual(item.bytes, chain[index]!.bytes);
    assert.deepEqual(item.digest, chain[index]!.digest, "each block re-hashed on arrival");
  }
});

test("the reader decodes items until the input is exhausted, whatever their sizes", () => {
  // The demo chains' blocks run from 334 to 2855 bytes, so the boundaries are
  // found by decoding and never by a fixed stride.
  const chain = demoChains()[0]!;
  const read = decodeBlockSequence(encodeBlockSequence(chain.items));
  assert.equal(read.length, chain.items.length);
  assert.deepEqual(
    read.map((item) => item.bytes.length),
    chain.index.map((entry) => entry.size),
  );
});

test("a truncated final item is an error, never the end of the sequence", () => {
  const chain = chainOf(ALICE, 2);
  const bytes = encodeBlockSequence(chain);
  const truncated = bytes.subarray(0, bytes.length - 1);
  assert.throws(
    () => decodeBlockSequence(truncated),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "item",
  );
});

test("a sequence holding anything but blocks is malformed as a whole", () => {
  const chain = chainOf(ALICE, 1);
  // A perfectly well-formed dCBOR item that is not a block.
  const intruder = encode(1n);
  for (const bytes of [
    encodeBlockSequence([intruder]),
    encodeBlockSequence([chain[0]!.bytes, intruder]),
    encodeBlockSequence([intruder, chain[0]!.bytes]),
  ]) {
    assert.throws(
      () => decodeBlockSequence(bytes),
      (error: unknown) => error instanceof BlockSequenceError && error.code === "item",
      "a prefix of good blocks does not rescue the sequence",
    );
  }
});

test("an item that is not the block's canonical encoding is refused", () => {
  const item = chainOf(ALICE, 1)[0]!;
  // Re-encode the block's map with a non-canonical head: the dCBOR decoder
  // catches it, and the sequence reader reports the item at fault.
  const tampered = Uint8Array.from(item.bytes);
  tampered[0] = 0xb8; // map(n) written with a one-byte argument instead of inline
  assert.throws(
    () => decodeBlockSequence(tampered),
    (error: unknown) => error instanceof BlockSequenceError && error.index === 0,
  );
});

test("a reader bounds what it will read", () => {
  const bytes = encodeBlockSequence(chainOf(ALICE, 4));
  assert.throws(
    () => decodeBlockSequence(bytes, { maxBlocks: 2 }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "limit",
  );
  assert.throws(
    () => decodeBlockSequence(bytes, { maxBytes: 10 }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "limit",
  );
});

// ---------------------------------------------------------------------------
// Ordering
// ---------------------------------------------------------------------------

test("a range is in chain order, genesis-ward first, from the position asked about", () => {
  const chain = chainOf(ALICE, 4);
  checkRangeOrder(chain, { pub: chain[0]!.block.pub, after: null });
  checkRangeOrder(chain.slice(2), { after: chain[1]!.digest });

  assert.throws(
    () => checkRangeOrder(chain.slice(1), { after: null }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "order",
    "the first block must name the position the client asked about",
  );
  assert.throws(
    () => checkRangeOrder([chain[0]!, chain[2]!], { after: null }),
    (error: unknown) => error instanceof BlockSequenceError && error.index === 1,
    "a skipped block breaks the prev walk at the point of the skip",
  );
  assert.throws(
    () => checkRangeOrder([chain[1]!, chain[0]!], { after: chain[0]!.digest }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "order",
    "a reordered range breaks it too",
  );
});

test("a range carrying another author's block is refused", () => {
  const mine = chainOf(ALICE, 1);
  const theirs = chainOf(BOB, 1);
  assert.throws(
    () => checkRangeOrder([mine[0]!, theirs[0]!], { pub: mine[0]!.block.pub, after: null }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "order",
  );
});

test("a sibling set is ordered by ascending bytewise digest", () => {
  const one = publicBlock(ALICE, { ops: [createAtom("one")] });
  const two = publicBlock(ALICE, { ops: [createAtom("two")] });
  const sorted = [one, two].sort((a, b) => compareBytes(a.digest, b.digest));
  checkSiblingOrder(sorted, { pub: one.block.pub, prev: null });
  assert.throws(
    () => checkSiblingOrder([...sorted].reverse(), { pub: one.block.pub, prev: null }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "order",
  );
  assert.throws(
    () => checkSiblingOrder([sorted[0]!, sorted[0]!]),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "order",
    "the order is strict: a repeated member is not ascending",
  );
});

test("a sibling set member naming another position is refused", () => {
  const chain = chainOf(ALICE, 2);
  assert.throws(
    () => checkSiblingOrder([chain[1]!], { prev: null }),
    (error: unknown) => error instanceof BlockSequenceError && error.code === "order",
  );
});

// ---------------------------------------------------------------------------
// As a file
// ---------------------------------------------------------------------------

test("each committed .block file is a one-block sequence whose digest the index pins", () => {
  for (const chain of demoChains()) {
    for (const [position, bytes] of chain.files.entries()) {
      const read = decodeBlockSequence(bytes);
      assert.equal(read.length, 1, `${chain.name}/${position} is one block`);
      const entry = chain.index[position]!;
      assert.equal(bytesToHex(read[0]!.digest), entry.digest);
      assert.equal(digestToCidText(read[0]!.digest), entry.cid);
      assert.equal(bytes.length, entry.size);
      assert.deepEqual(
        encodeBlock(read[0]!.block),
        bytes,
        "the committed bytes are the block's canonical encoding",
      );
    }
  }
});

test("concatenating an author's .block files in index order is a valid whole-chain range", () => {
  for (const chain of demoChains()) {
    const concatenated = encodeBlockSequence(chain.files);
    const read = decodeBlockSequence(concatenated);
    // The concatenation is a range response body: chain order, genesis-ward
    // first, from the genesis position.
    checkRangeOrder(read, { pub: chain.pub, after: null });
    assert.equal(read.length, chain.files.length);
    assert.deepEqual(
      read.map((item) => cidToText(cidOf(item.digest))),
      chain.index.map((entry) => entry.cid),
    );
    assert.equal(
      concatenated.length,
      chain.index.reduce((total, entry) => total + entry.size, 0),
    );
  }
});

function cidOf(digest: Uint8Array): Uint8Array {
  // The fixed prefix of spec/03: 0x01 0x71 0x12 0x20 || digest.
  const cid = new Uint8Array(36);
  cid.set([0x01, 0x71, 0x12, 0x20], 0);
  cid.set(digest, 4);
  return cid;
}
