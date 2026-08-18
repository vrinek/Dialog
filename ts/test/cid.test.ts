/**
 * Conformance: the digest and CID halves of `vectors/entities.json`, plus the
 * CID rules of spec/03-encoding.md that the vectors do not exercise
 * (parameter validation and the text form's input rules).
 *
 * The entity *structure* rules of spec/01-data-model.md are phase 2; what is
 * checked here is everything reachable with dcbor + cid: the canonical bytes
 * of each case, and the digest, CID and CID text form computed from them.
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  AUTHOR_KEY_TEXT_LENGTH,
  CID_PREFIX,
  CID_SIZE,
  CidError,
  authorKeyFromText,
  authorKeyToText,
  cid,
  cidFromDigest,
  cidFromText,
  cidToText,
  digest,
  digestFromCid,
  multihash,
  validateCid,
} from "../src/cid.ts";
import { decode, encode } from "../src/dcbor.ts";
import { bytesToHex, hexToBytes } from "../src/hex.ts";
import { buildValue, loadVectors } from "./vectors.ts";

const entities = loadVectors("entities.json");

/** Case counts as `vectors/README.md` records them. */
const EXPECTED_CASE_COUNTS: Record<string, number> = {
  atoms: 5,
  bonds: 2,
  meta_bonds: 5,
  molecules: 4,
  fillers: 12,
  invalid: 38,
};

test("the vector file is the one this suite was written against", () => {
  assert.equal(entities.vectors, "dialog-conformance/1");
  assert.equal(entities.area, "entities");
  for (const [name, count] of Object.entries(EXPECTED_CASE_COUNTS)) {
    const found = entities.sections.find((s) => s.name === name);
    assert.ok(found !== undefined, `section ${name}`);
    assert.equal(found.cases.length, count, `case count of ${name}`);
  }
});

// The invalid section holds no value model and no encoding — its bytes are
// what a decoder must refuse, which is the entity layer's business
// (test/entity.test.ts), not the CID layer's.
for (const entitySection of entities.sections.filter((s) => s.name !== "invalid")) {
  test(`vectors/entities.json: ${entitySection.name}`, async (t) => {
    for (const vector of entitySection.cases) {
      await t.test(vector.name, () => {
        assert.ok(vector.value !== undefined, "case has a value model");
        assert.ok(vector.dcbor !== undefined, "case has an encoding");
        const value = buildValue(vector.value);
        const bytes = hexToBytes(vector.dcbor);

        assert.equal(bytesToHex(encode(value)), vector.dcbor, "encode(value)");
        assert.deepEqual(decode(bytes), value, "decode(dcbor)");

        if (vector.digest === undefined) {
          // Fillers are hashed as part of their molecule and have no
          // identifier of their own (vectors/README.md, "Case shapes").
          assert.equal(vector.cid, undefined);
          assert.equal(vector.cid_text, undefined);
          return;
        }

        // digest = SHA-256(dCBOR(entity))
        assert.equal(bytesToHex(digest(bytes)), vector.digest, "digest");
        // CID = 01 71 12 20 || digest
        assert.ok(vector.cid !== undefined, "case has a CID");
        assert.equal(bytesToHex(cid(bytes)), vector.cid, "cid");
        assert.equal(
          bytesToHex(cidFromDigest(hexToBytes(vector.digest))),
          vector.cid,
          "cidFromDigest(digest)",
        );
        // The CID's parameters and the digest it carries.
        const cidBytes = hexToBytes(vector.cid);
        validateCid(cidBytes);
        assert.equal(bytesToHex(digestFromCid(cidBytes)), vector.digest, "digestFromCid");
        // text(CID) = "b" || base32-lower-nopad(36 bytes)
        assert.ok(vector.cid_text !== undefined, "case has a CID text form");
        assert.equal(cidToText(cidBytes), vector.cid_text, "cidToText");
        assert.deepEqual(cidFromText(vector.cid_text), cidBytes, "cidFromText");
        assert.ok(vector.cid_text.startsWith("bafyrei"), "every Dialog CID text starts with bafyrei");
        assert.equal(vector.cid_text.length, 59, "the text form is 59 characters");
      });
    }
  });
}

test("the worked example of spec/03-encoding.md, 'Encoding an atom'", () => {
  const atom = new Map([["description", "France"]]);
  const bytes = encode(atom);
  assert.equal(bytesToHex(bytes), "a16b6465736372697074696f6e664672616e6365");
  assert.equal(
    bytesToHex(digest(bytes)),
    "e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842",
  );
  assert.equal(
    bytesToHex(cid(bytes)),
    "01711220e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842",
  );
  assert.equal(
    cidToText(cid(bytes)),
    "bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii",
  );
});

test("multihash is 12 20 || digest", () => {
  const d = digest(new Uint8Array(0));
  const mh = multihash(d);
  assert.equal(mh.length, 34);
  assert.equal(mh[0], 0x12);
  assert.equal(mh[1], 0x20);
  assert.deepEqual(mh.slice(2), d);
});

test("CID parameters are validated on parse", () => {
  const good = cid(encode(new Map([["description", "France"]])));
  validateCid(good);

  const wrongVersion = good.slice();
  wrongVersion[0] = 0x02;
  assert.throws(() => validateCid(wrongVersion), (e: unknown) => e instanceof CidError && e.code === "version");

  const wrongCodec = good.slice();
  wrongCodec[1] = 0x55; // raw
  assert.throws(() => validateCid(wrongCodec), (e: unknown) => e instanceof CidError && e.code === "codec");

  const wrongHash = good.slice();
  wrongHash[2] = 0x13; // sha2-512
  assert.throws(() => validateCid(wrongHash), (e: unknown) => e instanceof CidError && e.code === "hash");

  const wrongLength = good.slice();
  wrongLength[3] = 0x40;
  assert.throws(() => validateCid(wrongLength), (e: unknown) => e instanceof CidError && e.code === "digest-length");

  assert.throws(() => validateCid(good.slice(0, 35)), (e: unknown) => e instanceof CidError && e.code === "size");
  assert.throws(() => validateCid(new Uint8Array(CID_SIZE + 1)), (e: unknown) => e instanceof CidError && e.code === "size");
  assert.throws(() => cidFromDigest(new Uint8Array(31)), (e: unknown) => e instanceof CidError && e.code === "size");
});

test("the text form is case-sensitive, unpadded lowercase base32", () => {
  const text = "bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii";
  const bytes = cidFromText(text);
  assert.equal(bytes.length, CID_SIZE);
  assert.deepEqual(bytes.slice(0, 4), CID_PREFIX);

  // Uppercase base32 is multibase code B, not this form.
  assert.throws(() => cidFromText(text.toUpperCase()), (e: unknown) => e instanceof CidError && e.code === "text");
  // A lowercase b prefix with uppercase payload is not the alphabet either.
  assert.throws(
    () => cidFromText("b" + text.slice(1).toUpperCase()),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Padding is not part of the form.
  assert.throws(() => cidFromText(text + "="), (e: unknown) => e instanceof CidError && e.code === "text");
  // Another multibase prefix, a missing prefix, and the empty string.
  assert.throws(() => cidFromText("z" + text.slice(1)), (e: unknown) => e instanceof CidError && e.code === "text");
  assert.throws(() => cidFromText(text.slice(1)), (e: unknown) => e instanceof CidError && e.code === "text");
  assert.throws(() => cidFromText(""), (e: unknown) => e instanceof CidError && e.code === "text");
  // Bare hex is a byte dump, not a CID string.
  assert.throws(
    () => cidFromText(bytesToHex(bytes)),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Truncated and over-long forms.
  assert.throws(() => cidFromText(text.slice(0, 58)), (e: unknown) => e instanceof CidError && e.code === "text");
  assert.throws(() => cidFromText(text + "a"), (e: unknown) => e instanceof CidError && e.code === "text");
  // A character outside the alphabet (0, 1, 8 and 9 are not in RFC 4648 base32).
  assert.throws(
    () => cidFromText("b0" + text.slice(2)),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Non-zero padding bits in the final character: 36 bytes leave two spare
  // bits, so only characters whose low two bits are zero can end the form.
  const trailing = text.slice(0, 58) + "b";
  assert.throws(() => cidFromText(trailing), (e: unknown) => e instanceof CidError && e.code === "text");
  // Text of the right shape whose bytes fail parameter validation.
  const wrongCodec = bytes.slice();
  wrongCodec[1] = 0x55;
  assert.throws(
    () => cidFromText(cidToTextUnchecked(wrongCodec)),
    (e: unknown) => e instanceof CidError && e.code === "codec",
  );
});

/** The text form of arbitrary 36 bytes, bypassing the encoder's own check, so
 * that the parser's parameter validation can be exercised. */
function cidToTextUnchecked(bytes: Uint8Array): string {
  const valid = bytes.slice();
  valid.set(CID_PREFIX, 0);
  const text = cidToText(valid);
  // Re-encode with the invalid parameter byte in place.
  const alphabet = "abcdefghijklmnopqrstuvwxyz234567";
  let out = "";
  let buffer = 0;
  let bits = 0;
  for (const byte of bytes) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += alphabet[(buffer >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += alphabet[(buffer << (5 - bits)) & 31];
  assert.equal(out.length, text.length - 1);
  return "b" + out;
}

// ---------------------------------------------------------------------------
// Text representation of author keys (spec/03-encoding.md, "Text
// representation of author keys"; spec/04-cryptography.md, "Key encoding")
// ---------------------------------------------------------------------------

test("the worked example of spec/03-encoding.md, 'Text representation of author keys'", () => {
  const publicKey = hexToBytes(
    "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c",
  );
  const text = authorKeyToText(publicKey);
  assert.equal(text, "b5uayvchd3v2at4mv7vjnwlj4xjoxfsthbg7r3fasdpzxjcabwqhw6xa");
  assert.deepEqual(authorKeyFromText(text), publicKey);
});

test("author key text round-trips over several keys", () => {
  const keys = [
    new Uint8Array(32).fill(0x00),
    new Uint8Array(32).fill(0xff),
    hexToBytes("8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"),
    hexToBytes("8139770ea87d175f56a35466c34c7ecccb8d8a91b4ee37a25df60f5b8fc9b394"),
  ];
  for (const key of keys) {
    const text = authorKeyToText(key);
    assert.equal(text.length, AUTHOR_KEY_TEXT_LENGTH, "56 characters");
    assert.ok(text.startsWith("b5ua"), "every author key text starts with b5ua");
    assert.deepEqual(authorKeyFromText(text), key);
  }
});

test("the author key text form is 56 characters long", () => {
  // The length the specification states, against the constant the module
  // derives from the prefixed key size.
  assert.equal(AUTHOR_KEY_TEXT_LENGTH, 56);
});

test("author key text form rejects every MUST-reject case", () => {
  const publicKey = hexToBytes(
    "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c",
  );
  const text = authorKeyToText(publicKey);
  assert.equal(text, "b5uayvchd3v2at4mv7vjnwlj4xjoxfsthbg7r3fasdpzxjcabwqhw6xa");

  // Missing/wrong multibase prefix.
  assert.throws(
    () => authorKeyFromText(text.slice(1)),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  assert.throws(
    () => authorKeyFromText("z" + text.slice(1)),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Uppercase multibase code B.
  assert.throws(
    () => authorKeyFromText("B" + text.slice(1)),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Any uppercase character in the body.
  assert.throws(
    () => authorKeyFromText("b" + text.slice(1, -1) + text.slice(-1).toUpperCase()),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  assert.throws(
    () => authorKeyFromText(text.toUpperCase()),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Base32 padding.
  assert.throws(
    () => authorKeyFromText(text + "="),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Decoded length not 34 bytes (too short / too long).
  assert.throws(
    () => authorKeyFromText(text.slice(0, 55)),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  assert.throws(
    () => authorKeyFromText(text + "a"),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  // Multicodec prefix is not 0xed 0x01: re-encode a 34-byte value with a
  // different first byte.
  const wrongCodec = new Uint8Array(34);
  wrongCodec.set([0xed, 0x02], 0); // wrong second byte
  wrongCodec.set(publicKey, 2);
  const wrongCodecText = "b" + base32EncodeForTest(wrongCodec);
  assert.throws(
    () => authorKeyFromText(wrongCodecText),
    (e: unknown) => e instanceof CidError && e.code === "multicodec",
  );
  const wrongCodec2 = new Uint8Array(34);
  wrongCodec2.set([0x71, 0x01], 0); // wrong first byte (dag-cbor codec, not ed25519-pub)
  wrongCodec2.set(publicKey, 2);
  const wrongCodecText2 = "b" + base32EncodeForTest(wrongCodec2);
  assert.throws(
    () => authorKeyFromText(wrongCodecText2),
    (e: unknown) => e instanceof CidError && e.code === "multicodec",
  );
});

test("a CID text string is rejected by the author key parser, and vice versa", () => {
  const cidText = "bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii";
  const keyText = "b5uayvchd3v2at4mv7vjnwlj4xjoxfsthbg7r3fasdpzxjcabwqhw6xa";
  assert.throws(
    () => authorKeyFromText(cidText),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
  assert.throws(
    () => cidFromText(keyText),
    (e: unknown) => e instanceof CidError && e.code === "text",
  );
});

test("authorKeyToText rejects a key that is not 32 bytes", () => {
  assert.throws(
    () => authorKeyToText(new Uint8Array(31)),
    (e: unknown) => e instanceof CidError && e.code === "size",
  );
  assert.throws(
    () => authorKeyToText(new Uint8Array(33)),
    (e: unknown) => e instanceof CidError && e.code === "size",
  );
});

/** base32-lower-nopad encoding, independent of the module under test, so the
 * multicodec-prefix rejection test can build malformed input. */
function base32EncodeForTest(bytes: Uint8Array): string {
  const alphabet = "abcdefghijklmnopqrstuvwxyz234567";
  let out = "";
  let buffer = 0;
  let bits = 0;
  for (const byte of bytes) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += alphabet[(buffer >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += alphabet[(buffer << (5 - bits)) & 31];
  return out;
}
