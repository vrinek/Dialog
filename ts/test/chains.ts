/**
 * Fixtures for the transport tests.
 *
 * Two sources of blocks:
 *
 * - **`demo/chains/`**, the grounding demo's committed chain directory: three
 *   author chains, one `.block` file per block, with an index naming each
 *   file's digest, CID and size. Real committed data, and the degenerate case
 *   of the profile's file form rather than an exception to it — each `.block`
 *   file is a one-block sequence, and concatenating one author's files in index
 *   order is that author's whole-chain range response, byte for byte.
 * - **Blocks built here** from the test keys, for the cases no committed chain
 *   holds: a fork, a chain whose middle block is withheld, a block a source
 *   refuses.
 *
 * Test-only: `node:` imports are allowed here, as in `vectors.ts`.
 */

import { readFileSync } from "node:fs";

import {
  createAtom,
  decodeBlock,
  publicKeyFromSeed,
  signBlock,
  unsignedPublicBlock,
} from "../src/block.ts";
import { type SequenceItem, sequenceItem } from "../src/blockseq.ts";
import { authorKeyFromText } from "../src/cid.ts";
import { hexToBytes } from "../src/hex.ts";

// ---------------------------------------------------------------------------
// The committed demo chains
// ---------------------------------------------------------------------------

interface IndexBlock {
  index: number;
  file: string;
  digest: string;
  cid: string;
  ops: number;
  size: number;
}

interface IndexChain {
  author: string;
  public_key: string;
  blocks: IndexBlock[];
}

interface ChainIndex {
  format: string;
  chains: IndexChain[];
}

/** One committed author chain, as the index names it and as its files hold
 * it. */
export interface DemoChain {
  readonly name: string;
  /** The author key, decoded from the index's canonical text form. */
  readonly pub: Uint8Array;
  readonly keyText: string;
  /** The blocks in chain order, each read from its own `.block` file. */
  readonly items: SequenceItem[];
  /** The index's own entries, for the assertions that check this loader read
   * what the index says it did. */
  readonly index: readonly IndexBlock[];
  /** The `.block` files' bytes, in index order, unconcatenated. */
  readonly files: Uint8Array[];
}

const CHAINS_DIR = new URL("../../demo/chains/", import.meta.url);

/** The three committed chains, in the index's order — atlas first, which is
 * the order in which nothing is ever held for want of an ancestor. */
export function demoChains(): DemoChain[] {
  const index = JSON.parse(readFileSync(new URL("index.json", CHAINS_DIR), "utf8")) as ChainIndex;
  return index.chains.map((chain) => {
    const files = chain.blocks.map(
      (block) => new Uint8Array(readFileSync(new URL(block.file, CHAINS_DIR))),
    );
    return {
      name: chain.author,
      pub: authorKeyFromText(chain.public_key),
      keyText: chain.public_key,
      items: files.map((bytes, position) => ({
        bytes,
        block: decodeBlock(bytes),
        digest: hexToBytes(chain.blocks[position]!.digest),
      })),
      index: chain.blocks,
      files,
    };
  });
}

/** The chain of that author name. */
export function demoChain(name: string): DemoChain {
  const chain = demoChains().find((candidate) => candidate.name === name);
  if (chain === undefined) throw new Error(`no committed chain named ${name}`);
  return chain;
}

// ---------------------------------------------------------------------------
// Blocks built from test keys
// ---------------------------------------------------------------------------

interface KeyVector {
  name: string;
  seed: string;
}

const VECTOR_KEYS = JSON.parse(
  readFileSync(new URL("../../vectors/blocks.json", import.meta.url), "utf8"),
) as { inputs: { keys: KeyVector[] } };

/** The seed of one of `vectors/blocks.json`'s test keys. */
export function seedOfKey(name: string): Uint8Array {
  const key = VECTOR_KEYS.inputs.keys.find((candidate) => candidate.name === name);
  if (key === undefined) throw new Error(`no test key named ${name}`);
  return hexToBytes(key.seed);
}

export const ALICE = seedOfKey("alice");
export const BOB = seedOfKey("bob");
export const ALICE_PUB = publicKeyFromSeed(ALICE);
export const BOB_PUB = publicKeyFromSeed(BOB);

/** A signed public block over the test keys. */
export function publicBlock(
  seed: Uint8Array,
  fields: {
    readonly prev?: Uint8Array | null;
    readonly refs?: readonly Uint8Array[];
    readonly ts?: number;
    readonly ops: Parameters<typeof unsignedPublicBlock>[0]["ops"];
  },
): SequenceItem {
  return sequenceItem(
    signBlock(
      unsignedPublicBlock({
        pub: publicKeyFromSeed(seed),
        prev: fields.prev ?? null,
        refs: fields.refs ?? [],
        ts: fields.ts ?? 1740000000,
        ops: fields.ops,
      }),
      seed,
    ),
  );
}

/**
 * A chain of `length` blocks by one key, each carrying one atom naming its
 * position, optionally beginning after an existing block.
 */
export function chainOf(
  seed: Uint8Array,
  length: number,
  options: { readonly label?: string; readonly after?: SequenceItem } = {},
): SequenceItem[] {
  const label = options.label ?? "block";
  const out: SequenceItem[] = [];
  let prev = options.after?.digest ?? null;
  for (let position = 0; position < length; position++) {
    const item = publicBlock(seed, {
      prev,
      ts: 1740000000 + position,
      ops: [createAtom(`${label} ${position}`)],
    });
    out.push(item);
    prev = item.digest;
  }
  return out;
}
