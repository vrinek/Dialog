/**
 * Conformance: `vectors/blocks.json` in full, plus the validation rules of
 * spec/02-block-format.md that no vector pins.
 *
 * The chain section is rebuilt the way a foreign implementation rebuilds it —
 * from the published JSON value model, through this implementation's decoder —
 * and then checked in both directions: the block re-encodes byte for byte, its
 * signing bytes and signing input match the pinned ones, the pinned signature
 * verifies under the pinned key, the key is re-derived from its seed and the
 * signature reproduced by signing again, and the digest, CID and CID text come
 * out as published. The five blocks are then replayed in order into a store and
 * validated one by one, which is what the vector file is for.
 *
 * The `forks` section is the one condition rule 9 requires a node to detect,
 * and the `invalid` section is the half that decides which blocks exist at all:
 * every case must be rejected, and rejected by the rule class its `rule` field
 * names, not by accident. The `invalid_in_chain` section is the other half of
 * that: blocks that decode and verify and are wrong only against a store, each
 * case replayed into a store of its own — rules 3, 4, 5, 6, the own-chain half
 * of rule 10, and the scan limit at the limit its case names.
 *
 * The hand-written tests at the bottom cover what the vectors still leave
 * unpinned — an ambiguous succession, a non-monotonic timestamp, the counting
 * unit of the scan limit — each built from this implementation's own signed
 * blocks.
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  BlockError,
  type BlockErrorCode,
  type Block,
  BlockStore,
  DEFAULT_SCAN_LIMIT,
  EMPTY_SOURCE,
  PROTOCOL_VERSION,
  SIGNING_DOMAIN_SEPARATOR,
  blockCid,
  blockCidText,
  blockDigest,
  blockFromValue,
  createAtom,
  createBond,
  createMolecule,
  decodeBlock,
  encodeBlock,
  newPublicBlock,
  operationDigest,
  operationReferences,
  publicKeyFromSeed,
  signBlock,
  signingBytes,
  signingInput,
  unsignedPrivateBlock,
  unsignedPublicBlock,
  unsignedRotationBlock,
  validateBlock,
  verifyBlockSignature,
} from "../src/block.ts";
import { authorKeyFromText, authorKeyToText } from "../src/cid.ts";
import { atomFiller, moleculeFiller, quantity, scalarFiller } from "../src/entity.ts";
import { bytesToHex, hexToBytes } from "../src/hex.ts";
import { buildValue, loadVectors, section, type VectorCase } from "./vectors.ts";

const blocks = loadVectors("blocks.json");

/** Case counts as `vectors/README.md` records them. */
const EXPECTED_CASE_COUNTS: Record<string, number> = {
  chain: 5,
  forks: 1,
  fork_block: 1,
  invalid: 23,
  invalid_in_chain: 12,
};

test("the vector file is the one vectors/README.md describes", () => {
  assert.equal(blocks.vectors, "dialog-conformance/1");
  assert.equal(blocks.area, "blocks");
  for (const [name, count] of Object.entries(EXPECTED_CASE_COUNTS)) {
    assert.equal(section(blocks, name).cases.length, count, `${name} case count`);
  }
  assert.equal(
    blocks.sections.length,
    Object.keys(EXPECTED_CASE_COUNTS).length,
    "the file has exactly the sections README.md lists",
  );
});

/** The seed of one of the file's test keys. */
function seedOfKey(name: string): Uint8Array {
  const key = blocks.inputs?.keys?.find((candidate) => candidate.name === name);
  if (key === undefined) throw new Error(`no test key named ${name}`);
  return hexToBytes(key.seed);
}

/** The published public key of one of the file's test keys. */
function publicKeyOf(name: string): Uint8Array {
  const key = blocks.inputs?.keys?.find((candidate) => candidate.name === name);
  if (key === undefined) throw new Error(`no test key named ${name}`);
  return hexToBytes(key.public_key);
}

test("every test key is the one its seed derives", () => {
  const keys = blocks.inputs?.keys ?? [];
  assert.equal(keys.length, 5);
  for (const key of keys) {
    assert.equal(
      bytesToHex(publicKeyFromSeed(hexToBytes(key.seed))),
      key.public_key,
      `${key.name}'s public key`,
    );
    // The 64-byte private key form is the seed followed by the public key.
    assert.equal(key.private_key, key.seed + key.public_key);
    // public_key_text is derived from public_key, not independent
    // (spec/03-encoding.md, "Text representation of author keys").
    assert.equal(
      authorKeyToText(hexToBytes(key.public_key)),
      key.public_key_text,
      `${key.name}'s public key text`,
    );
    assert.deepEqual(
      authorKeyFromText(key.public_key_text),
      hexToBytes(key.public_key),
      `${key.name}'s public key text parses back to the key bytes`,
    );
  }
});

// ---------------------------------------------------------------------------
// The chain, and the forking block, block by block
// ---------------------------------------------------------------------------

/** Every case that carries a complete block: the chain plus the fork block. */
const blockCases: VectorCase[] = [
  ...section(blocks, "chain").cases,
  ...section(blocks, "fork_block").cases,
];

for (const vector of blockCases) {
  test(`block ${vector.name} reproduces every byte the vectors pin`, () => {
    assert.ok(vector.value !== undefined && vector.block !== undefined);
    const block = blockFromValue(buildValue(vector.value));

    // The summary fields, which are the value model read back in prose.
    assert.equal(block.type, vector.type, "type");
    assert.equal(block.v, PROTOCOL_VERSION, "v");
    assert.equal(bytesToHex(block.pub), bytesToHex(publicKeyOf(vector.author!)), "pub");
    assert.equal(
      block.prev === null ? null : bytesToHex(block.prev),
      vector.prev ?? null,
      "prev",
    );
    if (block.type !== "private") {
      assert.deepEqual(block.refs.map(bytesToHex), vector.refs ?? [], "refs");
      assert.equal(block.ts, BigInt(vector.ts!), "ts");
    }

    // The complete encoding, signature included: this is what the digest
    // hashes (spec/02, "Block identification").
    assert.equal(bytesToHex(encodeBlock(block)), vector.block, "block bytes");

    // The signing input, before the signature: if these bytes are right and
    // the signature is not, the problem is in the signing procedure.
    assert.equal(bytesToHex(signingBytes(block)), vector.signing_bytes, "signing_bytes");
    assert.equal(bytesToHex(signingInput(block)), vector.signing_input, "signing_input");
    assert.equal(
      new TextDecoder().decode(signingInput(block).subarray(0, 15)),
      SIGNING_DOMAIN_SEPARATOR,
      "the domain separator prefixes the signing bytes",
    );

    // The pinned signature verifies, and re-signing with the seed reproduces
    // it: Ed25519 signing is deterministic.
    assert.equal(bytesToHex(block.sig), vector.signature, "signature");
    assert.ok(verifyBlockSignature(block), "the pinned signature verifies");
    const { sig: _omitted, ...unsigned } = block;
    const resigned = signBlock(unsigned, seedOfKey(vector.author!));
    assert.equal(bytesToHex(resigned.sig), vector.signature, "re-signed signature");
    assert.equal(bytesToHex(encodeBlock(resigned)), vector.block, "re-signed block bytes");

    // The identifiers.
    assert.equal(bytesToHex(blockDigest(block)), vector.digest, "digest");
    assert.equal(bytesToHex(blockCid(block)), vector.cid, "cid");
    assert.equal(blockCidText(block), vector.cid_text, "cid_text");

    // And the round trip from the bytes themselves.
    const decoded = decodeBlock(hexToBytes(vector.block));
    assert.equal(bytesToHex(encodeBlock(decoded)), vector.block, "decoded block re-encodes");
    assert.deepEqual(decoded, block, "decoding the bytes yields the same block");
  });
}

test("tampering with any pinned block breaks its signature", () => {
  for (const vector of blockCases) {
    const block = decodeBlock(hexToBytes(vector.block!));

    // The signature itself: one flipped bit, the tampered_signature case's
    // condition, applied to every block in the file.
    const sig = block.sig.slice();
    sig[0] ^= 0x01;
    assert.equal(verifyBlockSignature({ ...block, sig } as Block), false, vector.name);

    // And a field the signature covers. The timestamp is untrusted for
    // validation decisions and still inside the signed bytes.
    if (block.type !== "private") {
      const moved: Block = { ...block, ts: block.ts + 1n };
      assert.equal(verifyBlockSignature(moved), false, `${vector.name} with a moved ts`);
    }
  }
});

// ---------------------------------------------------------------------------
// Replaying the chain
// ---------------------------------------------------------------------------

/** The chain section, in the order the blocks are published. */
function chainCases(): VectorCase[] {
  return section(blocks, "chain").cases;
}

/** A store holding the chain up to and including `count` blocks. */
function replay(count = Number.POSITIVE_INFINITY): BlockStore {
  const store = new BlockStore();
  for (const [index, vector] of chainCases().entries()) {
    if (index >= count) break;
    const result = store.add(hexToBytes(vector.block!));
    assert.equal(result.status, "accepted", `${vector.name} accepted`);
  }
  return store;
}

test("the chain validates block by block, in the order it is published", () => {
  const store = new BlockStore();
  for (const vector of chainCases()) {
    const result = store.add(hexToBytes(vector.block!));
    assert.equal(result.status, "accepted", vector.name);
    assert.equal(bytesToHex(result.digest), vector.digest, `${vector.name} digest`);
    assert.equal(result.report?.fork, undefined, `${vector.name} is not a fork`);
    assert.deepEqual(result.report?.uncheckedRefs, [], `${vector.name} resolved every ref`);
  }
  assert.equal(store.size, 5);
  assert.equal(store.forks.length, 0);
});

test("the foreign reference resolves by scanning exactly one foreign block", () => {
  const store = replay(2);
  const bob = chainCases()[2]!;
  const result = store.add(hexToBytes(bob.block!));
  assert.equal(result.status, "accepted");
  // Alice's genesis block defines the bond and the atom Bob's molecule needs;
  // Alice's second block is never fetched, exactly as spec/05's example says.
  assert.equal(result.report?.scanned, 1);
});

test("a block whose predecessor has not arrived is stored but unvalidated", () => {
  const store = new BlockStore();
  const [genesis, second] = chainCases();
  const held = store.add(hexToBytes(second!.block!));
  assert.equal(held.status, "unvalidated");
  assert.equal(held.pending?.code, "unvalidated");
  assert.equal(store.get(hexToBytes(second!.digest!))?.valid, false);

  // Rule 3 reads the store: an unvalidated block is not a predecessor.
  assert.equal(store.add(hexToBytes(genesis!.block!)).status, "accepted");
  assert.equal(
    store.get(hexToBytes(second!.digest!))?.valid,
    true,
    "the held block validates once its ancestor arrives",
  );
});

test("the successor genesis block is recognized as the rotation's successor", () => {
  const store = replay();
  const rotation = chainCases()[3]!;
  const successor = chainCases()[4]!;
  assert.equal(store.successions.length, 1);
  const succession = store.successions[0]!;
  assert.equal(bytesToHex(succession.rotation), rotation.digest);
  assert.equal(bytesToHex(succession.genesis), successor.digest);
  assert.equal(bytesToHex(succession.oldPub), bytesToHex(publicKeyOf("alice")));
  assert.equal(bytesToHex(succession.newPub), bytesToHex(publicKeyOf("alice_successor")));
  assert.equal(store.ambiguousSuccessions.length, 0);
  assert.equal(
    bytesToHex(store.rotationOf(publicKeyOf("alice"))!.digest),
    rotation.digest,
    "alice's key is marked inactive by the rotation block",
  );
});

test("no block is accepted for a key after its rotation block", () => {
  const store = replay();
  const fork = section(blocks, "fork_block").cases[0]!;
  // The forking block is signed by Alice and is well-formed, but Alice's chain
  // ended: rule 3 turns it away before rule 9 ever looks at it.
  assert.throws(
    () => store.add(hexToBytes(fork.block!)),
    (error: unknown) => error instanceof BlockError && error.code === "chain",
  );
});

// ---------------------------------------------------------------------------
// Forks (rule 9)
// ---------------------------------------------------------------------------

test("two blocks of one chain sharing a prev are detected as a fork", () => {
  const forkCase = section(blocks, "forks").cases[0]!;
  const forkBlock = section(blocks, "fork_block").cases[0]!;
  const store = replay(2);
  const result = store.add(hexToBytes(forkBlock.block!));

  assert.equal(result.status, "accepted", "the forking block is valid in itself");
  const fork = result.report?.fork;
  assert.ok(fork !== undefined, "the fork is detected");
  assert.equal(bytesToHex(fork.pub), bytesToHex(publicKeyOf(forkCase.author!)));
  assert.equal(fork.prev === null ? null : bytesToHex(fork.prev), forkCase.prev);
  assert.deepEqual(fork.blocks.map(bytesToHex), forkCase.blocks);
  assert.equal(store.forks.length, 1, "the store records the fork it detected");
});

test("fork handling is policy: a store may reject the forking block instead", () => {
  const forkBlock = section(blocks, "fork_block").cases[0]!;
  const store = new BlockStore({ forkPolicy: "reject" });
  for (const vector of chainCases().slice(0, 2)) store.add(hexToBytes(vector.block!));
  assert.throws(
    () => store.add(hexToBytes(forkBlock.block!)),
    (error: unknown) => error instanceof BlockError && error.code === "fork",
  );
  assert.equal(store.forks.length, 1, "detection happens whatever the policy is");
});

test("a source that cannot answer sibling queries reports rule 9 as unperformed", () => {
  const genesis = decodeBlock(hexToBytes(chainCases()[0]!.block!));
  const report = validateBlock(genesis, EMPTY_SOURCE);
  assert.equal(report.forkDetectionPerformed, false);
  assert.equal(report.fork, undefined);
});

// ---------------------------------------------------------------------------
// The invalid section
// ---------------------------------------------------------------------------

/**
 * The rule class each `rule` string names, as this implementation reports it.
 * The longer rule numbers come first: "rule 10" contains "rule 1".
 */
const RULE_CLASSES: Array<{ names: string; code: BlockErrorCode }> = [
  { names: "Validation rule 10", code: "reference-hygiene" },
  { names: "Validation rule 1 ", code: "version" },
  { names: "Validation rule 2", code: "signature" },
  { names: "Validation rule 7", code: "empty-ops" },
  { names: "Validation rule 8", code: "encoding" },
  { names: "Validation dispatch", code: "type" },
  { names: "Rotation block", code: "rotation" },
  { names: "Private block", code: "field" },
  { names: "Internal references", code: "field" },
  { names: "Filler types", code: "data-model" },
];

function expectedCode(rule: string): BlockErrorCode {
  const found = RULE_CLASSES.find((entry) => rule.includes(entry.names));
  if (found === undefined) throw new Error(`no rule class for ${JSON.stringify(rule)}`);
  return found.code;
}

for (const vector of section(blocks, "invalid").cases) {
  test(`invalid block ${vector.name} is rejected by the rule it names`, () => {
    assert.ok(vector.bytes !== undefined && vector.rule !== undefined);
    let error: unknown;
    try {
      const block = decodeBlock(hexToBytes(vector.bytes));
      validateBlock(block, EMPTY_SOURCE);
      assert.fail(`accepted: ${vector.reason}`);
    } catch (thrown) {
      error = thrown;
    }
    assert.ok(error instanceof BlockError, `${vector.name}: ${String(error)}`);
    assert.equal(
      error.code,
      expectedCode(vector.rule),
      `${vector.name} (${vector.rule}): ${error.message}`,
    );
  });
}

test("no invalid block reaches a store", () => {
  const store = replay();
  for (const vector of section(blocks, "invalid").cases) {
    assert.throws(() => store.add(hexToBytes(vector.bytes!)), `${vector.name} was stored`);
  }
  assert.equal(store.size, 5, "the store is unchanged");
});

// ---------------------------------------------------------------------------
// The invalid_in_chain section: the rejections that need a store
// ---------------------------------------------------------------------------

/**
 * The rule class each chain-relative case names. "Scan limit" comes first: its
 * label also names rule 4, which is the rule the rejection falls under, but
 * this implementation reports the limit itself.
 */
const CHAIN_RULE_CLASSES: Array<{ names: string; code: BlockErrorCode }> = [
  { names: "Scan limit", code: "scan-limit" },
  { names: "Validation rule 10", code: "reference-hygiene" },
  { names: "Validation rule 3", code: "chain" },
  { names: "Validation rule 4", code: "reachability" },
  { names: "Validation rule 5", code: "data-model" },
  { names: "Validation rule 6", code: "reference-visibility" },
];

function expectedChainCode(rule: string): BlockErrorCode {
  const found = CHAIN_RULE_CLASSES.find((entry) => rule.includes(entry.names));
  if (found === undefined) throw new Error(`no rule class for ${JSON.stringify(rule)}`);
  return found.code;
}

for (const vector of section(blocks, "invalid_in_chain").cases) {
  test(`in a chain, ${vector.name} is rejected by the rule it names`, () => {
    assert.ok(vector.bytes !== undefined && vector.rule !== undefined);
    const options = vector.scan_limit === undefined ? {} : { scanLimit: vector.scan_limit };
    const store = new BlockStore(options);
    for (const [index, setup] of (vector.setup ?? []).entries()) {
      const accepted = store.add(hexToBytes(setup));
      assert.equal(accepted.status, "accepted", `${vector.name}: setup[${index}]`);
    }

    // The case is a rejection by the store, never by the decoder: these bytes
    // decode and their signature verifies.
    const block = decodeBlock(hexToBytes(vector.bytes));
    assert.ok(verifyBlockSignature(block), `${vector.name} is correctly signed`);

    if (vector.scan_limit !== undefined) {
      // A case that names a limit pins the limit, not the block: under the
      // default the very same block against the very same store is valid.
      const permissive = new BlockStore();
      for (const setup of vector.setup ?? []) permissive.add(hexToBytes(setup));
      assert.equal(
        permissive.add(hexToBytes(vector.bytes)).status,
        "accepted",
        `${vector.name} is valid under the default scan limit`,
      );
    }

    let error: unknown;
    try {
      store.add(hexToBytes(vector.bytes));
      assert.fail(`accepted: ${vector.reason}`);
    } catch (thrown) {
      error = thrown;
    }
    assert.ok(error instanceof BlockError, `${vector.name}: ${String(error)}`);
    assert.equal(
      error.code,
      expectedChainCode(vector.rule),
      `${vector.name} (${vector.rule}): ${error.message}`,
    );
  });
}

// ---------------------------------------------------------------------------
// The rules the vectors do not pin
// ---------------------------------------------------------------------------

const ALICE = seedOfKey("alice");
const BOB = seedOfKey("bob");
const ALICE_PUB = publicKeyFromSeed(ALICE);
const BOB_PUB = publicKeyFromSeed(BOB);

/** A signed public block, for the rules no vector exercises. */
function publicBlock(
  seed: Uint8Array,
  fields: {
    prev?: Uint8Array | null;
    refs?: readonly Uint8Array[];
    ts?: number;
    ops: Parameters<typeof unsignedPublicBlock>[0]["ops"];
  },
): Block {
  return signBlock(
    unsignedPublicBlock({
      pub: publicKeyFromSeed(seed),
      prev: fields.prev ?? null,
      refs: fields.refs ?? [],
      ts: fields.ts ?? 1740000000,
      ops: fields.ops,
    }),
    seed,
  );
}

const CAPITAL_OF = createBond("_A_ is the capital of _B_");
const PARIS = createAtom("Paris");
const FRANCE = createAtom("France");

test("rule 4: a digest that resolves nowhere is not reachable", () => {
  const orphan = publicBlock(ALICE, {
    ops: [createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });
  assert.throws(
    () => validateBlock(orphan, new BlockStore()),
    (error: unknown) => error instanceof BlockError && error.code === "reachability",
  );
});

test("rule 4: an ancestor of the author's own chain resolves a digest", () => {
  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ops: [PARIS, FRANCE, CAPITAL_OF] });
  store.add(genesis);
  const second = publicBlock(ALICE, {
    prev: blockDigest(genesis),
    ops: [createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });
  assert.equal(store.add(second).status, "accepted");
});

test("rule 4: a scalar filler's unit is a digest like any other", () => {
  const metre = createAtom("metre");
  const tall = createBond("_A_ is _B_ tall");
  const withUnit = createMolecule(operationDigest(tall), [
    atomFiller(operationDigest(PARIS)),
    scalarFiller(quantity(330n, operationDigest(metre))),
  ]);
  // The unit is one of the digests operationReferences enumerates, and there
  // is no exempt position.
  assert.equal(operationReferences(withUnit).length, 3);
  assert.ok(operationReferences(withUnit).some((ref) => ref.role === "unit"));

  const store = new BlockStore();
  const missingUnit = publicBlock(ALICE, { ops: [PARIS, tall, withUnit] });
  assert.throws(
    () => store.add(missingUnit),
    (error: unknown) => error instanceof BlockError && error.code === "reachability",
  );
  assert.equal(
    store.add(publicBlock(ALICE, { ops: [PARIS, metre, tall, withUnit] })).status,
    "accepted",
  );
});

test("rule 4: an operation may only use entities earlier operations created", () => {
  const molecule = createMolecule(operationDigest(CAPITAL_OF), [
    atomFiller(operationDigest(PARIS)),
    atomFiller(operationDigest(FRANCE)),
  ]);
  // The molecule comes before the bond it uses.
  const outOfOrder = publicBlock(ALICE, { ops: [PARIS, FRANCE, molecule, CAPITAL_OF] });
  assert.throws(
    () => validateBlock(outOfOrder, new BlockStore()),
    (error: unknown) => error instanceof BlockError && error.code === "reachability",
  );
});

test("rule 4: a refs entry the store does not hold leaves the verdict undecided", () => {
  // spec/02, rule 4, third outcome: resolution needed a block the node does
  // not hold, so it has not decided. The block is stored but unvalidated —
  // never invalid, because a source that withheld one foreign block would
  // otherwise be able to reject a block that is in fact valid.
  const provider = publicBlock(ALICE, { ops: [CAPITAL_OF] });
  const dependent = publicBlock(BOB, {
    refs: [blockDigest(provider)],
    ops: [PARIS, FRANCE, createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });

  const store = new BlockStore();
  const held = store.add(dependent);
  assert.equal(held.status, "unvalidated");
  assert.equal(held.pending?.code, "unvalidated");
  assert.deepEqual(held.pending?.awaiting, blockDigest(provider));
  assert.equal(store.get(blockDigest(dependent))?.valid, false);

  // The missing block arrives; nothing about the held block changes; it is
  // valid.
  assert.equal(store.add(provider).status, "accepted");
  assert.equal(
    store.get(blockDigest(dependent))?.valid,
    true,
    "the held block validates once the block its refs name arrives",
  );
});

test("rule 4: a block reached transitively through refs is the same undecided verdict", () => {
  // Carol defines the bond, Alice's block names Carol's, Bob's names Alice's.
  // Bob's own refs entry is held; the block it leads to is not.
  const CAROL = seedOfKey("carol");
  const carol = publicBlock(CAROL, { ops: [CAPITAL_OF] });
  const alice = publicBlock(ALICE, { refs: [blockDigest(carol)], ops: [PARIS, FRANCE] });
  const bob = publicBlock(BOB, {
    refs: [blockDigest(alice)],
    ops: [createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });

  const store = new BlockStore();
  store.add(alice);
  const held = store.add(bob);
  assert.equal(held.status, "unvalidated");
  assert.deepEqual(held.pending?.awaiting, blockDigest(carol));

  store.add(carol);
  assert.equal(store.get(blockDigest(bob))?.valid, true);
});

test("rule 4: a digest nothing defines stays invalid when nothing was missing", () => {
  // The other failing outcome: resolution read every block it asked for — the
  // block itself, which has neither ancestors nor refs — so the digest is
  // provably absent and the rejection is definitive.
  const orphan = publicBlock(ALICE, {
    ops: [PARIS, FRANCE, createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });
  const store = new BlockStore();
  assert.throws(
    () => store.add(orphan),
    (error: unknown) => error instanceof BlockError && error.code === "reachability",
  );
  assert.equal(store.get(blockDigest(orphan)), undefined, "an invalid block is not held");
});

test("rule 4: a refs entry resolution never needed does not make the block undecided", () => {
  // Every digest resolves from the block's own operations, so the entry it
  // also names is never fetched. Outcome 3 is reached only when the missing
  // block could have mattered (spec/05, "Resolution procedure").
  const unrelated = publicBlock(ALICE, { ops: [createAtom("an entity nobody needs")] });
  const b = publicBlock(BOB, {
    refs: [blockDigest(unrelated)],
    ops: [CAPITAL_OF, PARIS, FRANCE, createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });
  const store = new BlockStore();
  const result = store.add(b);
  assert.equal(result.status, "accepted");
  assert.equal(result.report?.scanned, 0);
  assert.deepEqual(result.report?.uncheckedRefs.map(bytesToHex), [
    bytesToHex(blockDigest(unrelated)),
  ]);
});

test("a non-monotonic ts is warned about, never rejected", () => {
  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ts: 1740000060, ops: [PARIS] });
  store.add(genesis);
  const backdated = publicBlock(ALICE, {
    prev: blockDigest(genesis),
    ts: 1740000000,
    ops: [FRANCE],
  });
  const result = store.add(backdated);
  assert.equal(result.status, "accepted", "a timestamp is never a validity decision");
  assert.deepEqual(result.report?.nonMonotonicTimestamp, {
    previous: 1740000060n,
    current: 1740000000n,
  });
  // And the chain in the vectors is monotonic, so no warning is raised there.
  const pinned = new BlockStore();
  for (const vector of chainCases()) {
    const accepted = pinned.add(hexToBytes(vector.block!));
    assert.equal(accepted.report?.nonMonotonicTimestamp, undefined, vector.name);
  }
});

test("rule 5: every digest resolves to an entity of the kind its position names", () => {
  const wrongBond = publicBlock(ALICE, {
    ops: [
      PARIS,
      FRANCE,
      CAPITAL_OF,
      // The bond field naming an atom.
      createMolecule(operationDigest(PARIS), [atomFiller(operationDigest(FRANCE))]),
    ],
  });
  assert.throws(
    () => validateBlock(wrongBond, new BlockStore()),
    (error: unknown) => error instanceof BlockError && error.code === "data-model",
  );

  const wrongFiller = publicBlock(ALICE, {
    ops: [
      PARIS,
      FRANCE,
      CAPITAL_OF,
      // A type 2 filler naming an atom.
      createMolecule(operationDigest(CAPITAL_OF), [
        moleculeFiller(operationDigest(PARIS)),
        atomFiller(operationDigest(FRANCE)),
      ]),
    ],
  });
  assert.throws(
    () => validateBlock(wrongFiller, new BlockStore()),
    (error: unknown) => error instanceof BlockError && error.code === "data-model",
  );
});

test("rule 5: the filler count MUST equal the bond's variable count", () => {
  const tooFew = publicBlock(ALICE, {
    ops: [PARIS, CAPITAL_OF, createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
    ])],
  });
  assert.throws(
    () => validateBlock(tooFew, new BlockStore()),
    (error: unknown) =>
      error instanceof BlockError &&
      error.code === "data-model" &&
      error.message.includes("variable"),
  );
});

test("rule 6: a public block's refs MUST NOT name a private block", () => {
  const store = new BlockStore();
  const secret = signBlock(
    unsignedPrivateBlock({
      pub: BOB_PUB,
      prev: null,
      enc: new Uint8Array(48).fill(7),
      nonce: new Uint8Array(24).fill(9),
    }),
    BOB,
  );
  store.add(secret);
  const dependent = publicBlock(ALICE, { refs: [blockDigest(secret)], ops: [PARIS] });
  assert.throws(
    () => store.add(dependent),
    (error: unknown) => error instanceof BlockError && error.code === "reference-visibility",
  );
});

test("rule 6: an entry the node does not hold leaves the rule unchecked", () => {
  const unknown = new Uint8Array(32).fill(0xab);
  const block = publicBlock(ALICE, { refs: [unknown], ops: [PARIS] });
  const report = validateBlock(block, new BlockStore());
  assert.deepEqual(report.uncheckedRefs.map(bytesToHex), [bytesToHex(unknown)]);
});

test("rule 10: refs MUST NOT name a block of the author's own chain", () => {
  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ops: [PARIS, FRANCE, CAPITAL_OF] });
  store.add(genesis);
  const selfReferential = publicBlock(ALICE, {
    prev: blockDigest(genesis),
    refs: [blockDigest(genesis)],
    ops: [createAtom("Lyon")],
  });
  assert.throws(
    () => store.add(selfReferential),
    (error: unknown) => error instanceof BlockError && error.code === "reference-hygiene",
  );
});

test("rule 10: a repeated refs entry is refused when the block is built", () => {
  const ref = new Uint8Array(32).fill(3);
  assert.throws(
    () =>
      unsignedPublicBlock({
        pub: ALICE_PUB,
        prev: null,
        refs: [ref, ref],
        ts: 1740000000,
        ops: [PARIS],
      }),
    (error: unknown) => error instanceof BlockError && error.code === "reference-hygiene",
  );
});

test("rule 3: a chain's blocks all carry the same pub", () => {
  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ops: [PARIS] });
  store.add(genesis);
  const impostor = publicBlock(BOB, { prev: blockDigest(genesis), ops: [FRANCE] });
  assert.throws(
    () => store.add(impostor),
    (error: unknown) => error instanceof BlockError && error.code === "chain",
  );
});

test("rule 2: a block signed by another key is rejected", () => {
  const block = publicBlock(ALICE, { ops: [PARIS] });
  const forged: Block = { ...block, pub: BOB_PUB } as Block;
  assert.equal(verifyBlockSignature(forged), false);
  assert.throws(
    () => validateBlock(forged, new BlockStore()),
    (error: unknown) => error instanceof BlockError && error.code === "signature",
  );
  assert.throws(
    () => signBlock({ ...unsignedPublicBlock({ pub: ALICE_PUB, prev: null, ts: 1, ops: [PARIS] }) }, BOB),
    (error: unknown) => error instanceof BlockError && error.code === "signature",
  );
});

test("the scan limit turns an unresolvable refs graph into a rejection", () => {
  // A chain of foreign blocks, each referencing the next, with the entity the
  // last one defines needed by the block under validation.
  const store = new BlockStore();
  const target = publicBlock(BOB, { ops: [PARIS, FRANCE, CAPITAL_OF] });
  store.add(target);
  let previous = blockDigest(target);
  const hops = 4;
  for (let i = 0; i < hops; i++) {
    const seed = new Uint8Array(32).fill(0x40 + i);
    const hop = publicBlock(seed, { refs: [previous], ops: [createAtom(`hop ${i}`)] });
    store.add(hop);
    previous = blockDigest(hop);
  }
  const molecule = createMolecule(operationDigest(CAPITAL_OF), [
    atomFiller(operationDigest(PARIS)),
    atomFiller(operationDigest(FRANCE)),
  ]);
  const dependent = publicBlock(ALICE, { refs: [previous], ops: [molecule] });

  assert.equal(validateBlock(dependent, store).scanned, hops + 1);
  assert.throws(
    () => validateBlock(dependent, store, { scanLimit: 2 }),
    (error: unknown) => error instanceof BlockError && error.code === "scan-limit",
  );
  assert.ok(DEFAULT_SCAN_LIMIT > hops + 1, "the default admits an honest graph");
});

test("the scan limit counts distinct foreign blocks, once each", () => {
  // Carol's block defines the bond and is named by two different blocks of the
  // refs graph, so resolution meets it twice and scans it once. Counting
  // fetches, or digests, or recursion levels gives another number here.
  const store = new BlockStore();
  const provider = publicBlock(new Uint8Array(32).fill(0x04), { ops: [CAPITAL_OF] });
  store.add(provider);
  const first = publicBlock(new Uint8Array(32).fill(0x05), {
    refs: [blockDigest(provider)],
    ops: [createAtom("one")],
  });
  const second = publicBlock(new Uint8Array(32).fill(0x06), {
    refs: [blockDigest(provider)],
    ops: [createAtom("two")],
  });
  store.add(first);
  store.add(second);

  const dependent = publicBlock(ALICE, {
    refs: [blockDigest(first), blockDigest(second)],
    ops: [
      PARIS,
      FRANCE,
      createMolecule(operationDigest(CAPITAL_OF), [
        atomFiller(operationDigest(PARIS)),
        atomFiller(operationDigest(FRANCE)),
      ]),
    ],
  });
  assert.equal(validateBlock(dependent, store).scanned, 3, "three blocks, four names");
  assert.equal(validateBlock(dependent, store, { scanLimit: 3 }).scanned, 3);
  assert.throws(
    () => validateBlock(dependent, store, { scanLimit: 2 }),
    (error: unknown) => error instanceof BlockError && error.code === "scan-limit",
  );
});

test("a refs entry resolution never needs costs no unit of the scan limit", () => {
  // The entry is still fetched — rules 6 and 10 are checked against it — but
  // nothing reads its operations, so it is not scanned.
  const store = new BlockStore();
  const provider = publicBlock(BOB, { ops: [createAtom("an entity nobody here needs")] });
  store.add(provider);
  const dependent = publicBlock(ALICE, {
    refs: [blockDigest(provider)],
    ops: [
      PARIS,
      FRANCE,
      CAPITAL_OF,
      createMolecule(operationDigest(CAPITAL_OF), [
        atomFiller(operationDigest(PARIS)),
        atomFiller(operationDigest(FRANCE)),
      ]),
    ],
  });
  assert.equal(validateBlock(dependent, store).scanned, 0);
  assert.equal(store.add(dependent).status, "accepted");
});

test("an ancestor of the author's own chain is not a foreign block", () => {
  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ops: [PARIS, FRANCE, CAPITAL_OF] });
  store.add(genesis);
  const tip = publicBlock(ALICE, {
    prev: blockDigest(genesis),
    ops: [createMolecule(operationDigest(CAPITAL_OF), [
      atomFiller(operationDigest(PARIS)),
      atomFiller(operationDigest(FRANCE)),
    ])],
  });
  // A limit of zero still admits it: the ancestry is not scanned.
  const report = validateBlock(tip, store, { scanLimit: 0 });
  assert.equal(report.scanned, 0);
});

test("a rotation block ends a chain and a rotation is never a genesis block", () => {
  assert.throws(
    () =>
      unsignedRotationBlock({
        pub: ALICE_PUB,
        prev: null as unknown as Uint8Array,
        ts: 1740000000,
        newPub: BOB_PUB,
      }),
    (error: unknown) => error instanceof BlockError && error.code === "rotation",
  );
  assert.throws(
    () =>
      unsignedRotationBlock({
        pub: ALICE_PUB,
        prev: new Uint8Array(32).fill(1),
        ts: 1740000000,
        newPub: ALICE_PUB,
      }),
    (error: unknown) => error instanceof BlockError && error.code === "rotation",
  );

  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ops: [PARIS] });
  store.add(genesis);
  const rotation = signBlock(
    unsignedRotationBlock({
      pub: ALICE_PUB,
      prev: blockDigest(genesis),
      ts: 1740000060,
      newPub: BOB_PUB,
    }),
    ALICE,
  );
  store.add(rotation);
  assert.throws(
    () => store.add(publicBlock(ALICE, { prev: blockDigest(rotation), ops: [FRANCE] })),
    (error: unknown) => error instanceof BlockError && error.code === "chain",
  );
});

test("two genesis blocks claiming one rotation make the succession ambiguous", () => {
  const store = new BlockStore();
  const genesis = publicBlock(ALICE, { ops: [PARIS] });
  store.add(genesis);
  const rotation = signBlock(
    unsignedRotationBlock({
      pub: ALICE_PUB,
      prev: blockDigest(genesis),
      ts: 1740000060,
      newPub: BOB_PUB,
    }),
    ALICE,
  );
  store.add(rotation);

  const first = publicBlock(BOB, {
    refs: [blockDigest(rotation)],
    ts: 1740000120,
    ops: [createAtom("Lyon")],
  });
  const second = publicBlock(BOB, {
    refs: [blockDigest(rotation)],
    ts: 1740000180,
    ops: [createAtom("Marseille")],
  });
  store.add(first);
  const result = store.add(second);

  assert.equal(store.successions.length, 2);
  assert.equal(store.ambiguousSuccessions.length, 2, "the conflict is surfaced");
  // The two are also a fork in the strict sense of rule 9: distinct blocks
  // signed by the successor key, both claiming the genesis position.
  assert.notEqual(result.report?.fork, undefined);
  assert.equal(result.report?.fork?.prev, null);
});

test("a private block is validated structurally, and its rules stay unchecked", () => {
  const block = signBlock(
    unsignedPrivateBlock({
      pub: ALICE_PUB,
      prev: null,
      enc: new Uint8Array(16).fill(1),
      nonce: new Uint8Array(24).fill(2),
    }),
    ALICE,
  );
  const report = validateBlock(block, new BlockStore());
  assert.equal(report.encrypted, true);
  assert.equal(report.scanned, 0);
  assert.deepEqual(report.uncheckedRefs, []);
  assert.equal(bytesToHex(blockDigest(block)).length, 64);

  assert.throws(
    () =>
      unsignedPrivateBlock({
        pub: ALICE_PUB,
        prev: null,
        enc: new Uint8Array(15),
        nonce: new Uint8Array(24),
      }),
    (error: unknown) => error instanceof BlockError && error.code === "field",
  );
  assert.throws(
    () =>
      unsignedPrivateBlock({
        pub: ALICE_PUB,
        prev: null,
        enc: new Uint8Array(16),
        nonce: new Uint8Array(12),
      }),
    (error: unknown) => error instanceof BlockError && error.code === "field",
  );
});

test("a block's bytes MUST be its canonical encoding", () => {
  const store = new BlockStore();
  const genesis = decodeBlock(hexToBytes(chainCases()[0]!.block!));
  const bytes = encodeBlock(genesis);
  const padded = new Uint8Array(bytes.length + 1);
  padded.set(bytes);
  assert.throws(() => store.add(padded));
});

test("the constructors refuse an operation the data model refuses", () => {
  assert.throws(
    () => createAtom(""),
    (error: unknown) => error instanceof BlockError && error.code === "data-model",
  );
  assert.throws(
    () => createBond("a template with no variable"),
    (error: unknown) => error instanceof BlockError && error.code === "data-model",
  );
  assert.throws(
    () => createMolecule(new Uint8Array(32), []),
    (error: unknown) => error instanceof BlockError && error.code === "data-model",
  );
  assert.throws(
    () =>
      newPublicBlock({
        pub: ALICE_PUB,
        sig: new Uint8Array(64),
        prev: null,
        ts: 1740000000,
        ops: [],
      }),
    (error: unknown) => error instanceof BlockError && error.code === "empty-ops",
  );
});
