/**
 * The block sequence: the one serialization of `spec/07-transport.md`.
 *
 * A **block sequence** is a CBOR sequence
 * ([RFC 8742](https://datatracker.ietf.org/doc/html/rfc8742)) — the canonical
 * dCBOR encoding of zero or more blocks, concatenated, with no framing, no
 * length prefix, no wrapper and no separator. It is every response body, every
 * request body that carries blocks, and every file. A range response saved to
 * disk is a chain file; a chain file offered to a server is an announce body;
 * the demo's per-author `.block` files concatenated in index order are that
 * author's whole-chain range response, byte for byte.
 *
 * The sequence carries **no metadata**: no count, no author, no position, no
 * timestamp and no signature of its own. Everything a reader needs is inside
 * the blocks, which is what makes a saved response and a hand-carried file the
 * same artifact — and what makes the reader's obligation absolute: every block
 * is re-hashed and validated here, never identified by where it sat in a
 * sequence.
 *
 * Ordering is a property of the *operation* that produced a sequence rather
 * than of the format, so the order checks live in named functions
 * ({@link checkRangeOrder}, {@link checkSiblingOrder}) that a caller applies
 * according to what it asked for.
 */

import { type Block, decodeBlock, encodeBlock } from "./block.ts";
import { digest } from "./cid.ts";
import { decodeFirst } from "./dcbor.ts";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/** What made a byte string not a block sequence. */
export type BlockSequenceErrorCode =
  /** An item is not the well-formed dCBOR encoding of a block, or the final
   * item is truncated. Either way the sequence is malformed as a whole
   * (spec/07, "The block sequence", rules 1 and 3). */
  | "item"
  /** The sequence exceeded a bound the reader set: a byte count or a block
   * count. A reader MUST bound what it will read (spec/07, Security
   * Considerations, "Resource exhaustion"). */
  | "limit"
  /** The sequence is well-formed but not in the order the operation that
   * produced it fixes (spec/07, "The block sequence", "Ordering"). */
  | "order";

/** A byte string offered as a block sequence that is not one. */
export class BlockSequenceError extends Error {
  readonly code: BlockSequenceErrorCode;
  /** The index of the offending item, where one item is at fault. */
  readonly index?: number;

  constructor(
    code: BlockSequenceErrorCode,
    message: string,
    options?: ErrorOptions & { readonly index?: number },
  ) {
    super(message, options);
    this.name = "BlockSequenceError";
    this.code = code;
    if (options?.index !== undefined) this.index = options.index;
  }
}

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

/**
 * One block of a sequence: the bytes it arrived as, the block they decode to,
 * and the digest of those bytes.
 *
 * The digest is computed here and never taken from anything a source said. A
 * client identifies a block by this value alone — never by the position the
 * block held in the sequence, never by the URL it was fetched from (spec/07,
 * "Verification obligations", rule 1).
 */
export interface SequenceItem {
  readonly bytes: Uint8Array;
  readonly block: Block;
  readonly digest: Uint8Array;
}

/** The item a block makes: its canonical encoding and that encoding's digest. */
export function sequenceItem(block: Block): SequenceItem {
  const bytes = encodeBlock(block);
  return { bytes, block, digest: digest(bytes) };
}

// ---------------------------------------------------------------------------
// Encoding
// ---------------------------------------------------------------------------

/**
 * Concatenate blocks into a block sequence: no framing, no separator, nothing
 * between one block's last byte and the next block's first.
 *
 * A member given as bytes is written through unchanged, which is what a source
 * relaying blocks it holds does — it MUST NOT re-encode them, since the bytes
 * are what the digest and the signature are over. A member given as a block is
 * encoded canonically.
 */
export function encodeBlockSequence(items: Iterable<Block | Uint8Array | SequenceItem>): Uint8Array {
  const parts: Uint8Array[] = [];
  let total = 0;
  for (const item of items) {
    const bytes =
      item instanceof Uint8Array ? item : "bytes" in item ? item.bytes : encodeBlock(item);
    parts.push(bytes);
    total += bytes.length;
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

/** Bounds a reader puts on a sequence before reading it. */
export interface DecodeSequenceOptions {
  /** The most blocks the reader will accept. Absent, no bound. */
  readonly maxBlocks?: number;
  /** The most bytes the reader will accept. Absent, no bound. */
  readonly maxBytes?: number;
}

/**
 * Read a block sequence: decode items one after another until the input is
 * exhausted.
 *
 * Every item MUST be a well-formed dCBOR encoding of a block; a sequence
 * containing anything else is malformed **as a whole**, so this throws rather
 * than returning a prefix. A truncated final item is an error and never the end
 * of the sequence. An empty input is a valid sequence of no blocks, which is
 * the answer meaning "none".
 *
 * Each item's bytes are checked to be the block's canonical encoding
 * (spec/02, validation rule 8) — the digest and the signature are over exactly
 * these bytes, so a re-encoding that differed would mean the sequence carried a
 * block under an identity that is not its own.
 */
export function decodeBlockSequence(
  bytes: Uint8Array,
  options: DecodeSequenceOptions = {},
): SequenceItem[] {
  if (options.maxBytes !== undefined && bytes.length > options.maxBytes) {
    throw new BlockSequenceError(
      "limit",
      `the sequence is ${bytes.length} bytes, over the reader's bound of ${options.maxBytes}`,
    );
  }

  const items: SequenceItem[] = [];
  let offset = 0;
  while (offset < bytes.length) {
    if (options.maxBlocks !== undefined && items.length >= options.maxBlocks) {
      throw new BlockSequenceError(
        "limit",
        `the sequence holds more than the reader's bound of ${options.maxBlocks} block(s)`,
        { index: items.length },
      );
    }
    const index = items.length;
    const rest = bytes.subarray(offset);

    let read: number;
    let block: Block;
    try {
      const item = decodeFirst(rest);
      read = item.read;
      block = decodeBlock(rest.subarray(0, read));
    } catch (error) {
      throw new BlockSequenceError(
        "item",
        `item ${index} of the sequence, at byte ${offset}, is not a block: ${
          error instanceof Error ? error.message : String(error)
        }`,
        { cause: error, index },
      );
    }

    const itemBytes = rest.slice(0, read);
    const canonical = encodeBlock(block);
    if (!bytesEqual(canonical, itemBytes)) {
      throw new BlockSequenceError(
        "item",
        `item ${index} of the sequence is not the canonical encoding of the block it decodes to`,
        { index },
      );
    }

    items.push({ bytes: itemBytes, block, digest: digest(itemBytes) });
    offset += read;
  }
  return items;
}

// ---------------------------------------------------------------------------
// Ordering (spec/07, "The block sequence", "Ordering")
// ---------------------------------------------------------------------------

/**
 * Check the range property of a sequence, which is the client's own work and
 * never something a source asserts (spec/07, "Verification obligations",
 * rule 2): the first block's `prev` names the position asked about, every later
 * block's `prev` is the digest of the block immediately before it, and every
 * block is the author's.
 *
 * A source that skips a block produces a break the client sees immediately, so
 * *within* a range completeness is free. Nothing here says the range reached
 * the chain's end — that is the `Dialog-Tip` comparison, and it is a claim
 * rather than evidence.
 */
export function checkRangeOrder(
  items: readonly SequenceItem[],
  expect: { readonly pub?: Uint8Array; readonly after: Uint8Array | null },
): void {
  let position = expect.after;
  for (const [index, item] of items.entries()) {
    if (expect.pub !== undefined && !bytesEqual(item.block.pub, expect.pub)) {
      throw new BlockSequenceError(
        "order",
        `block ${index} of the range is signed by another author than the one requested`,
        { index },
      );
    }
    const prev = item.block.prev;
    const linked =
      position === null ? prev === null : prev !== null && bytesEqual(prev, position);
    if (!linked) {
      throw new BlockSequenceError(
        "order",
        index === 0
          ? "the first block of the range does not name the position that was asked about"
          : `block ${index} of the range does not name block ${index - 1} as its predecessor`,
        { index },
      );
    }
    position = item.digest;
  }
}

/**
 * Check a sibling set: every block signed by the author asked about, every one
 * naming the position asked about, and the set in ascending bytewise digest
 * order.
 *
 * The order is fixed so that two sources holding the same set produce the same
 * bytes. A one-member set is not a statement that the chain does not fork; it
 * is a statement about this source (spec/07, `siblings`).
 */
export function checkSiblingOrder(
  items: readonly SequenceItem[],
  expect: { readonly pub?: Uint8Array; readonly prev?: Uint8Array | null } = {},
): void {
  for (const [index, item] of items.entries()) {
    if (expect.pub !== undefined && !bytesEqual(item.block.pub, expect.pub)) {
      throw new BlockSequenceError(
        "order",
        `block ${index} of the sibling set is signed by another author than the one requested`,
        { index },
      );
    }
    if (expect.prev !== undefined) {
      const prev = item.block.prev;
      const named =
        expect.prev === null ? prev === null : prev !== null && bytesEqual(prev, expect.prev);
      if (!named) {
        throw new BlockSequenceError(
          "order",
          `block ${index} of the sibling set does not name the position that was asked about`,
          { index },
        );
      }
    }
    if (index > 0 && compareBytes(items[index - 1]!.digest, item.digest) >= 0) {
      throw new BlockSequenceError(
        "order",
        "the sibling set is not in ascending bytewise digest order",
        { index },
      );
    }
  }
}

/** Ascending bytewise comparison, the order a sibling set is sorted in and the
 * order the reference fork-branch rule takes its minimum under. */
export function compareBytes(a: Uint8Array, b: Uint8Array): number {
  const shared = Math.min(a.length, b.length);
  for (let i = 0; i < shared; i++) {
    const difference = a[i]! - b[i]!;
    if (difference !== 0) return difference;
  }
  return a.length - b.length;
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  return a.length === b.length && compareBytes(a, b) === 0;
}

// ---------------------------------------------------------------------------
// As a file
// ---------------------------------------------------------------------------

/** The conventional extension of a chain file: a block sequence at rest. */
export const CHAIN_FILE_EXTENSION = ".dialog";

/** The conventional extension of a sequence holding exactly one block, which
 * is the same thing at length one. */
export const BLOCK_FILE_EXTENSION = ".block";
