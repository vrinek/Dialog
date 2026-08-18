/**
 * Conformance: `vectors/privacy.json` in full — the private-block path of
 * spec/04-cryptography.md.
 *
 * Each section is checked the way the file is built to be checked: the
 * `payload` plaintext and the `aead` AAD are rebuilt from the published JSON
 * value model and re-encoded byte for byte; the `enc` ciphertext is
 * reproduced by sealing that plaintext, under the pinned content key and
 * nonce, with the genesis AAD; the `x25519` conversions are reproduced from
 * the pinned seeds; the `key_wrap` shared secret, wrapping key and 72-byte
 * wrapped key are reproduced end to end, and the wrap is opened again to
 * recover the content key; and the `private_block` case is rebuilt, its
 * signature verified and re-derived, its identifiers recomputed, and its
 * payload opened and byte-compared against the `payload` section.
 *
 * The second half is rejection: tampering with `enc`, the nonce, or any
 * AAD-covered field breaks authentication; the wrong content key and the
 * wrong recipient both fail; a wrapped key of the wrong length is rejected
 * before any decryption is attempted; and a plaintext that authenticates but
 * decodes to something non-canonical, or to a shape a payload cannot have, is
 * still rejected, by the strict dCBOR decode and the payload's own checks.
 */

import assert from "node:assert/strict";
import test from "node:test";

import { xchacha20poly1305 } from "@noble/ciphers/chacha.js";

import {
  type PrivateBlock,
  blockCid,
  blockCidText,
  blockDigest,
  blockFromValue,
  createAtom,
  decodeBlock,
  encodeBlock,
  signBlock,
  signingBytes,
  signingInput,
  verifyBlockSignature,
} from "../src/block.ts";
import { type DcborValue, encode } from "../src/dcbor.ts";
import { bytesToHex, hexToBytes } from "../src/hex.ts";
import {
  CONTENT_KEY_SIZE,
  PrivacyError,
  WRAPPED_KEY_SIZE,
  blockAad,
  decodePrivatePayload,
  deriveWrappingKey,
  ed25519PrivateKeyToX25519,
  ed25519PublicKeyToX25519,
  encodePrivateAad,
  encodePrivatePayload,
  openPrivateBlock,
  sealPrivateBlock,
  unwrapContentKey,
  unwrapContentKeyWithKey,
  wrapContentKey,
  wrapContentKeyWithKey,
  wrappingKeyBetween,
  x25519SharedSecret,
} from "../src/privacy.ts";
import { buildValue, loadVectors, section } from "./vectors.ts";

const vectors = loadVectors("privacy.json");

/** Case counts as `vectors/README.md` records them. */
const EXPECTED_CASE_COUNTS: Record<string, number> = {
  payload: 1,
  aead: 4,
  x25519: 3,
  key_wrap: 2,
  private_block: 1,
};

test("the vector file is the one vectors/README.md describes", () => {
  assert.equal(vectors.vectors, "dialog-conformance/1");
  assert.equal(vectors.area, "privacy");
  for (const [name, count] of Object.entries(EXPECTED_CASE_COUNTS)) {
    assert.equal(section(vectors, name).cases.length, count, `${name} case count`);
  }
  assert.equal(
    vectors.sections.length,
    Object.keys(EXPECTED_CASE_COUNTS).length,
    "the file has exactly the sections README.md lists",
  );
});

/** The seed of one of the file's test keys. */
function seedOfKey(name: string): Uint8Array {
  const key = vectors.inputs?.keys?.find((candidate) => candidate.name === name);
  if (key === undefined) throw new Error(`no test key named ${name}`);
  return hexToBytes(key.seed);
}

/** The published public key of one of the file's test keys. */
function publicKeyOf(name: string): Uint8Array {
  const key = vectors.inputs?.keys?.find((candidate) => candidate.name === name);
  if (key === undefined) throw new Error(`no test key named ${name}`);
  return hexToBytes(key.public_key);
}

const contentKey = hexToBytes(vectors.inputs!.content_key!);
const blockNonce = hexToBytes(vectors.inputs!.block_nonce!);

test("the fixed content key and block nonce are the sizes their fields declare", () => {
  assert.equal(contentKey.length, CONTENT_KEY_SIZE);
  assert.equal(blockNonce.length, 24);
});

const privatePayload = { refs: [], ts: 1740067200n, ops: [createAtom("My private note")] };

// ---------------------------------------------------------------------------
// payload
// ---------------------------------------------------------------------------

const payloadCase = section(vectors, "payload").cases[0]!;

test("the payload plaintext reproduces the pinned bytes, and round-trips", () => {
  assert.equal(bytesToHex(encode(buildValue(payloadCase.value!))), payloadCase.hex);
  assert.equal(bytesToHex(encodePrivatePayload(privatePayload)), payloadCase.hex);

  const decoded = decodePrivatePayload(hexToBytes(payloadCase.hex!));
  assert.deepEqual(decoded.refs, []);
  assert.equal(decoded.ts, 1740067200n);
  assert.deepEqual(decoded.ops, [{ op: "create_atom", description: "My private note" }]);
});

// ---------------------------------------------------------------------------
// aead
// ---------------------------------------------------------------------------

const aeadCases = section(vectors, "aead").cases;
const aadGenesisCase = aeadCases.find((c) => c.name === "aad_genesis")!;
const aadLinkedCase = aeadCases.find((c) => c.name === "aad_linked")!;
const nonceCase = aeadCases.find((c) => c.name === "nonce")!;
const encCase = aeadCases.find((c) => c.name === "enc")!;

test("the genesis AAD (prev: null) reproduces the pinned bytes", () => {
  assert.equal(bytesToHex(encode(buildValue(aadGenesisCase.value!))), aadGenesisCase.hex);
  const aad = encodePrivateAad({ v: 1n, pub: publicKeyOf("author"), prev: null });
  assert.equal(bytesToHex(aad), aadGenesisCase.hex);
});

test("the linked AAD (a non-null prev) reproduces the pinned bytes", () => {
  assert.equal(bytesToHex(encode(buildValue(aadLinkedCase.value!))), aadLinkedCase.hex);
  const prev = hexToBytes("f1deac9d3f383e2236bdad4b60111441b89c74fafba5b1f5d22c6351b5fb999b");
  const aad = encodePrivateAad({ v: 1n, pub: publicKeyOf("author"), prev });
  assert.equal(bytesToHex(aad), aadLinkedCase.hex);
});

test("the block nonce is the pinned 24 bytes", () => {
  assert.equal(bytesToHex(blockNonce), nonceCase.hex);
});

test("enc reproduces the pinned ciphertext, sealed from the payload and the genesis AAD", () => {
  const aad = encodePrivateAad({ v: 1n, pub: publicKeyOf("author"), prev: null });
  const enc = xchacha20poly1305(contentKey, blockNonce, aad).encrypt(encodePrivatePayload(privatePayload));
  assert.equal(bytesToHex(enc), encCase.hex);

  const unsigned = sealPrivateBlock(
    { pub: publicKeyOf("author"), prev: null, ts: privatePayload.ts, ops: privatePayload.ops },
    contentKey,
    blockNonce,
  );
  assert.equal(bytesToHex(unsigned.enc), encCase.hex, "sealPrivateBlock reproduces enc");
  assert.equal(bytesToHex(unsigned.nonce), nonceCase.hex, "sealPrivateBlock carries the nonce through");
});

// ---------------------------------------------------------------------------
// x25519
// ---------------------------------------------------------------------------

for (const c of section(vectors, "x25519").cases) {
  test(`x25519 conversion ${c.name} reproduces the pinned private and public keys`, () => {
    const seed = hexToBytes(c.seed!);
    const edPub = hexToBytes(c.ed25519_public_key!);
    assert.equal(bytesToHex(ed25519PrivateKeyToX25519(seed)), c.x25519_private_key, "private conversion");
    assert.equal(bytesToHex(ed25519PublicKeyToX25519(edPub)), c.x25519_public_key, "public conversion");
  });
}

// ---------------------------------------------------------------------------
// key_wrap
// ---------------------------------------------------------------------------

for (const c of section(vectors, "key_wrap").cases) {
  test(`key wrap ${c.name} reproduces the shared secret, wrapping key and 72-byte wrapped key`, () => {
    const ownSeed = seedOfKey(c.own!);
    const peerPub = publicKeyOf(c.peer!);
    const ownX25519Private = ed25519PrivateKeyToX25519(ownSeed);
    const peerX25519Public = ed25519PublicKeyToX25519(peerPub);

    const sharedSecret = x25519SharedSecret(ownX25519Private, peerX25519Public);
    assert.equal(bytesToHex(sharedSecret), c.shared_secret, "shared_secret");

    const wrappingKey = deriveWrappingKey(sharedSecret);
    assert.equal(bytesToHex(wrappingKey), c.wrapping_key, "wrapping_key");
    assert.equal(
      bytesToHex(wrappingKeyBetween(ownSeed, peerPub)),
      c.wrapping_key,
      "wrappingKeyBetween end to end",
    );

    const nonce = hexToBytes(c.nonce!);
    const wrapped = wrapContentKeyWithKey(wrappingKey, contentKey, nonce);
    assert.equal(bytesToHex(wrapped), c.wrapped_key, "wrapped_key");
    assert.equal(wrapped.length, WRAPPED_KEY_SIZE);
    assert.equal(
      bytesToHex(wrapContentKey(ownSeed, peerPub, contentKey, nonce)),
      c.wrapped_key,
      "wrapContentKey end to end",
    );

    assert.equal(
      bytesToHex(unwrapContentKeyWithKey(wrappingKey, hexToBytes(c.wrapped_key!))),
      bytesToHex(contentKey),
      "unwrapContentKeyWithKey",
    );
  });
}

test("the recipient recovers the content key from the author's wrap, using their own identity", () => {
  const wrap = section(vectors, "key_wrap").cases.find((c) => c.name === "author_to_recipient")!;
  const opened = unwrapContentKey(seedOfKey("recipient"), publicKeyOf("author"), hexToBytes(wrap.wrapped_key!));
  assert.equal(bytesToHex(opened), bytesToHex(contentKey));
});

test("a third party cannot open a wrap made for someone else", () => {
  const wrap = section(vectors, "key_wrap").cases.find((c) => c.name === "author_to_recipient")!;
  assert.throws(
    () => unwrapContentKey(seedOfKey("third_party"), publicKeyOf("author"), hexToBytes(wrap.wrapped_key!)),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("the intended recipient cannot open a wrap made for a different recipient", () => {
  const wrap = section(vectors, "key_wrap").cases.find((c) => c.name === "author_to_third_party")!;
  assert.throws(
    () => unwrapContentKey(seedOfKey("recipient"), publicKeyOf("author"), hexToBytes(wrap.wrapped_key!)),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

// ---------------------------------------------------------------------------
// private_block
// ---------------------------------------------------------------------------

const privateBlockCase = section(vectors, "private_block").cases[0]!;

function pinnedBlock(): PrivateBlock {
  return decodeBlock(hexToBytes(privateBlockCase.block!)) as PrivateBlock;
}

test("the private block case reproduces every byte the vectors pin, and opens", () => {
  assert.ok(privateBlockCase.value !== undefined);
  const block = pinnedBlock();

  assert.equal(block.type, "private");
  assert.equal(bytesToHex(block.pub), bytesToHex(publicKeyOf("author")));
  assert.equal(block.prev, null);
  assert.equal(bytesToHex(block.enc), privateBlockCase.enc);
  assert.equal(bytesToHex(block.nonce), privateBlockCase.nonce);

  // Rebuilding from the published value model gives the identical block.
  const fromValue = blockFromValue(buildValue(privateBlockCase.value!));
  assert.deepEqual(fromValue, block);

  assert.equal(bytesToHex(encodeBlock(block)), privateBlockCase.block, "block bytes");
  assert.equal(bytesToHex(signingBytes(block)), privateBlockCase.signing_bytes);
  assert.equal(bytesToHex(signingInput(block)), privateBlockCase.signing_input);
  assert.equal(bytesToHex(block.sig), privateBlockCase.signature);
  assert.ok(verifyBlockSignature(block), "the pinned signature verifies");

  const { sig: _omitted, ...unsigned } = block;
  const resigned = signBlock(unsigned, seedOfKey("author"));
  assert.equal(bytesToHex(resigned.sig), privateBlockCase.signature, "re-signed signature");

  assert.equal(bytesToHex(blockDigest(block)), privateBlockCase.digest);
  assert.equal(bytesToHex(blockCid(block)), privateBlockCase.cid);
  assert.equal(blockCidText(block), privateBlockCase.cid_text);

  // The AAD read straight off the block matches the aad_genesis case.
  assert.equal(bytesToHex(blockAad(block)), aadGenesisCase.hex);

  // Opening it with the content key reproduces the payload section's value.
  const payload = openPrivateBlock(block, contentKey);
  assert.deepEqual(payload.refs, []);
  assert.equal(payload.ts, 1740067200n);
  assert.deepEqual(payload.ops, [{ op: "create_atom", description: "My private note" }]);
  assert.equal(
    bytesToHex(encodePrivatePayload(payload)),
    payloadCase.hex,
    "the opened payload re-encodes to the pinned plaintext",
  );

  // Building it from scratch (seal + sign) reproduces the same complete block.
  const rebuiltUnsigned = sealPrivateBlock(
    { pub: publicKeyOf("author"), prev: null, ts: privatePayload.ts, ops: privatePayload.ops },
    contentKey,
    blockNonce,
  );
  const rebuilt = signBlock(rebuiltUnsigned, seedOfKey("author"));
  assert.equal(bytesToHex(encodeBlock(rebuilt)), privateBlockCase.block, "seal + sign reproduces the block");
});

// ---------------------------------------------------------------------------
// Rejections
// ---------------------------------------------------------------------------

test("flipping a bit in enc breaks authentication", () => {
  const block = pinnedBlock();
  const enc = block.enc.slice();
  enc[0] ^= 0x01;
  assert.throws(
    () => openPrivateBlock({ ...block, enc }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("flipping a bit in the nonce breaks authentication", () => {
  const block = pinnedBlock();
  const nonce = block.nonce.slice();
  nonce[0] ^= 0x01;
  assert.throws(
    () => openPrivateBlock({ ...block, nonce }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("changing v, an AAD-covered field, breaks authentication", () => {
  const block = pinnedBlock();
  assert.throws(
    () => openPrivateBlock({ ...block, v: 2n }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("changing pub, an AAD-covered field, breaks authentication", () => {
  const block = pinnedBlock();
  const pub = publicKeyOf("recipient");
  assert.throws(
    () => openPrivateBlock({ ...block, pub }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("changing prev, an AAD-covered field, breaks authentication", () => {
  const block = pinnedBlock();
  const prev = new Uint8Array(32).fill(0x01);
  assert.throws(
    () => openPrivateBlock({ ...block, prev }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("the wrong content key fails to open the block", () => {
  const block = pinnedBlock();
  const wrongKey = new Uint8Array(32).fill(0xaa);
  assert.throws(
    () => openPrivateBlock(block, wrongKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("a wrapped key of the wrong length is rejected before any decryption", () => {
  const wrap = section(vectors, "key_wrap").cases[0]!;
  const wrappingKey = hexToBytes(wrap.wrapping_key!);
  const properlyWrapped = hexToBytes(wrap.wrapped_key!);

  for (const bad of [properlyWrapped.slice(0, -1), new Uint8Array([...properlyWrapped, 0x00])]) {
    assert.throws(
      () => unwrapContentKeyWithKey(wrappingKey, bad),
      (error: unknown) => error instanceof PrivacyError && error.code === "wrap-length",
      `length ${bad.length}`,
    );
  }
});

test("strict decode rejects a decrypted plaintext that is not canonical dCBOR", () => {
  // A payload whose ts is encoded in a non-shortest form: major type 0,
  // additional info 24 (a following 1-byte argument) for the value 5, where
  // the shortest encoding is the single byte 0x05. The map's key order (ts,
  // ops, refs) and every other byte stay canonical, so this is exactly one
  // rule 1 ("shortest") violation and nothing else.
  const good = encodePrivatePayload({ refs: [], ts: 5n, ops: [createAtom("x")] });
  // "ts" is a 2-byte-length text-string key (3 bytes: 0x62 0x74 0x73) right
  // after the 1-byte map header, so its value starts at offset 4.
  assert.equal(good[0], 0xa3, "3-entry map header");
  assert.equal(good[4], 0x05, "ts encodes as the single shortest-form byte");
  const nonShortest = new Uint8Array(good.length + 1);
  nonShortest.set(good.subarray(0, 4), 0);
  nonShortest.set([0x18, 0x05], 4);
  nonShortest.set(good.subarray(5), 6);

  const block = pinnedBlock();
  const aad = blockAad(block);
  const enc = xchacha20poly1305(contentKey, blockNonce, aad).encrypt(nonShortest);

  assert.throws(
    () => openPrivateBlock({ ...block, enc, nonce: blockNonce }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "payload",
  );
});

test("a private block's payload rejects a rotate_key operation", () => {
  const opMap = new Map<string, DcborValue>();
  opMap.set("op", "rotate_key");
  opMap.set("new_pub", new Uint8Array(32).fill(0x09));
  const map = new Map<string, DcborValue>();
  map.set("refs", []);
  map.set("ts", 1n);
  map.set("ops", [opMap]);
  const plaintext = encode(map);

  const block = pinnedBlock();
  const aad = blockAad(block);
  const enc = xchacha20poly1305(contentKey, blockNonce, aad).encrypt(plaintext);
  assert.throws(
    () => openPrivateBlock({ ...block, enc, nonce: blockNonce }, contentKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "payload",
  );
});

// ---------------------------------------------------------------------------
// Ed25519-to-X25519 conversion rejections
//
// vectors/privacy.json pins no invalid case for any of the three rejection
// rules spec/04, "Ed25519-to-X25519 conversion" states (non-canonical y,
// y = 1, and the all-zero agreement result of a small-order key) — see
// todos/062. These are hand-built from the prose, the way
// vectors/entities.json's rejection rules were before entities.json grew an
// `invalid` section (todos/058).
// ---------------------------------------------------------------------------

test("a non-canonical y (y >= p) is rejected before the division", () => {
  // p = 2^255 - 19. Encode y = p itself, little-endian, sign bit (byte 31's
  // MSB) left clear: p's low 255 bits are all that step 2 reads.
  const p = (1n << 255n) - 19n;
  const bytes = new Uint8Array(32);
  let v = p;
  for (let i = 0; i < 32; i++) {
    bytes[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  assert.throws(
    () => ed25519PublicKeyToX25519(bytes),
    (error: unknown) => error instanceof PrivacyError && error.code === "conversion",
  );
});

test("y = 1 (the identity point) is rejected", () => {
  const bytes = new Uint8Array(32);
  bytes[0] = 1;
  assert.throws(
    () => ed25519PublicKeyToX25519(bytes),
    (error: unknown) => error instanceof PrivacyError && error.code === "conversion",
  );
});

test("an all-zero X25519 agreement (a small-order key) is rejected", () => {
  const privateKey = ed25519PrivateKeyToX25519(seedOfKey("author"));
  // u = 0 is one of RFC 7748 §5.2's low-order points; the agreement with it is
  // all-zero however the private scalar is chosen.
  assert.throws(
    () => x25519SharedSecret(privateKey, new Uint8Array(32)),
    (error: unknown) => error instanceof PrivacyError && error.code === "agreement",
  );
});
