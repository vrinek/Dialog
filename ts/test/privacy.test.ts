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
 * The second half is rejection, and since todos/062 that half is pinned too:
 * the `invalid` section covers every rule spec/04-cryptography.md states in
 * prose — both X25519 conversion refusals, the small-order agreement, the
 * key-wrap length and tamper cases, and the AEAD/payload cases (a tampered
 * `enc`, nonce or AAD-covered field, the enc floor, a plaintext that
 * authenticates but is not canonical dCBOR, and a rotate_key operation inside
 * a private payload). A case's populated fields say which function it
 * exercises. A short "beyond the vectors" section keeps a few hand-written
 * cases that are additional instances of a pinned rule (the AAD's other two
 * covered fields, a wrong content key, a differently-shaped non-canonical
 * plaintext) rather than new rules.
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
import { authorKeyFromText, authorKeyToText } from "../src/cid.ts";
import { encode } from "../src/dcbor.ts";
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
  invalid: 13,
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

test("every test key's public_key_text is reproduced from public_key, and parses back to it", () => {
  const keys = vectors.inputs?.keys ?? [];
  assert.equal(keys.length, 3);
  for (const key of keys) {
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
// invalid
//
// Every rejection rule spec/04-cryptography.md states in prose (plus the two
// that in fact live in spec/02-block-format.md: the enc floor and the
// rotate_key scoping), pinned as bytes since todos/062 settled the gap
// entities.json and blocks.json had already closed at their own layers. A
// case's populated fields say which function it exercises — see
// vectors/README.md, "Privacy rejections", and vectorfile.PrivacyInvalidCase's
// doc comment in the Go reference implementation for the four shapes.
// ---------------------------------------------------------------------------

for (const c of section(vectors, "invalid").cases) {
  test(`invalid/${c.name} is rejected under its named rule`, () => {
    if (c.public_key !== undefined) {
      assert.throws(
        () => ed25519PublicKeyToX25519(hexToBytes(c.public_key!)),
        (error: unknown) => error instanceof PrivacyError && error.code === "conversion",
        c.reason,
      );
      return;
    }
    if (c.peer_public_key !== undefined) {
      assert.throws(
        () => wrappingKeyBetween(seedOfKey(c.own!), hexToBytes(c.peer_public_key!)),
        (error: unknown) => error instanceof PrivacyError && error.code === "agreement",
        c.reason,
      );
      return;
    }
    if (c.wrapped_key !== undefined) {
      const wrapped = hexToBytes(c.wrapped_key);
      assert.throws(
        () => unwrapContentKey(seedOfKey(c.peer!), publicKeyOf(c.own!), wrapped),
        (error: unknown) => {
          if (!(error instanceof PrivacyError)) return false;
          return c.rule!.includes("authentication") ? error.code === "aead" : error.code === "wrap-length";
        },
        c.reason,
      );
      return;
    }
    if (c.content_key !== undefined && c.block !== undefined) {
      const key = hexToBytes(c.content_key);
      let block: PrivateBlock;
      try {
        block = decodeBlock(hexToBytes(c.block)) as PrivateBlock;
      } catch {
        // Rejected before openPrivateBlock is ever reached — the enc-floor
        // case, which is structural (spec/02-block-format.md, "Private
        // block") and needs no key.
        return;
      }
      assert.throws(() => openPrivateBlock(block, key), c.reason);
      return;
    }
    assert.fail(`case ${c.name} names none of the shapes this test knows how to check`);
  });
}

// ---------------------------------------------------------------------------
// Rejections beyond the vectors
//
// The AAD covers three fields (v, pub, prev); the invalid section pins only
// prev. changing_pub and changing_v below are the same rule, exercised on the
// other two, so that the AAD binds all three and not just the one the vectors
// happen to pin. The wrong-content-key and non-shortest-integer cases are
// likewise additional instances of rules the vectors already pin once each.
// ---------------------------------------------------------------------------

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

test("the wrong content key fails to open the block", () => {
  const block = pinnedBlock();
  const wrongKey = new Uint8Array(32).fill(0xaa);
  assert.throws(
    () => openPrivateBlock(block, wrongKey),
    (error: unknown) => error instanceof PrivacyError && error.code === "aead",
  );
});

test("strict decode rejects a decrypted plaintext that is not canonical dCBOR (a non-shortest integer)", () => {
  // A payload whose ts is encoded in a non-shortest form: major type 0,
  // additional info 24 (a following 1-byte argument) for the value 5, where
  // the shortest encoding is the single byte 0x05. The map's key order (ts,
  // ops, refs) and every other byte stay canonical, so this is exactly one
  // rule 1 ("shortest") violation and nothing else — a different violation of
  // the same "strict decode after authentication" rule the invalid section's
  // non_canonical_plaintext case pins with an unsorted key order.
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
