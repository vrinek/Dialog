/**
 * Conformance: `vectors/dcbor.json`, every case of every section.
 *
 * The valid sections are checked in both directions — `encode(value)` must
 * produce the vector's bytes and `decode(bytes)` must produce the vector's
 * value — and the invalid section must be rejected byte string by byte string,
 * with the rejection naming the class of rule the vector says is violated.
 */

import assert from "node:assert/strict";
import test from "node:test";

import { DcborError, decode, encode, type DcborErrorCode } from "../src/dcbor.ts";
import { bytesToHex, hexToBytes } from "../src/hex.ts";
import { buildValue, loadVectors, section, type VectorCase } from "./vectors.ts";

const vectors = loadVectors("dcbor.json");

/** Case counts as `vectors/README.md` records them; a change here is a change
 * to the interop contract, not a cosmetic one. */
const EXPECTED_CASE_COUNTS: Record<string, number> = {
  encoding_reference: 10,
  canonical: 25,
  decimal_fractions: 6,
  invalid: 51,
};

test("the vector file is the one this suite was written against", () => {
  assert.equal(vectors.vectors, "dialog-conformance/1");
  assert.equal(vectors.area, "dcbor");
  for (const [name, count] of Object.entries(EXPECTED_CASE_COUNTS)) {
    assert.equal(section(vectors, name).cases.length, count, `case count of ${name}`);
  }
});

for (const name of ["encoding_reference", "canonical", "decimal_fractions"]) {
  test(`vectors/dcbor.json: ${name}`, async (t) => {
    for (const vector of section(vectors, name).cases) {
      await t.test(vector.name, () => {
        checkRoundTrip(vector);
      });
    }
  });
}

function checkRoundTrip(vector: VectorCase): void {
  assert.ok(vector.value !== undefined, "case has a value model");
  assert.ok(vector.dcbor !== undefined, "case has an encoding");
  const value = buildValue(vector.value);
  const bytes = hexToBytes(vector.dcbor);

  // Encode(value) MUST produce dcbor.
  assert.equal(bytesToHex(encode(value)), vector.dcbor, "encode(value)");
  // Decode(dcbor) MUST produce value.
  assert.deepEqual(decode(bytes), value, "decode(dcbor)");
  // And both directions compose.
  assert.deepEqual(decode(encode(value)), value, "decode(encode(value))");
  assert.equal(bytesToHex(encode(decode(bytes))), vector.dcbor, "encode(decode(dcbor))");
}

test("vectors/dcbor.json: invalid", async (t) => {
  for (const vector of section(vectors, "invalid").cases) {
    await t.test(vector.name, () => {
      assert.ok(vector.bytes !== undefined, "case has bytes");
      assert.ok(vector.rule !== undefined, "case names the rule it violates");
      const bytes = hexToBytes(vector.bytes);
      let error: unknown;
      assert.throws(
        () => {
          decode(bytes);
        },
        (thrown: unknown) => {
          error = thrown;
          return thrown instanceof DcborError;
        },
        `${vector.name} must be rejected (${vector.reason ?? vector.rule})`,
      );
      const expected = expectedCode(vector.rule);
      assert.equal(
        (error as DcborError).code,
        expected,
        `${vector.name}: rejected as ${(error as DcborError).code} (${(error as DcborError).message}), expected the ${expected} class for ${vector.rule}`,
      );
    });
  }
});

/** The error class a vector's `rule` string calls for. */
function expectedCode(rule: string): DcborErrorCode {
  if (rule.includes("Decimal fractions")) return "decimal";
  if (rule.includes("rule 1")) return "shortest";
  if (rule.includes("rule 2")) return "map-key-order";
  if (rule.includes("rule 3")) return "duplicate-key";
  if (rule.includes("rule 4")) return "indefinite";
  if (rule.includes("rule 5")) return "float";
  if (rule.includes("rule 6")) return "tag";
  if (rule.includes("rule 7")) return "simple";
  if (rule.includes("rule 9")) return "map-key-type";
  if (rule.includes("UTF-8")) return "utf8";
  if (rule.includes("RFC 8949")) return "malformed";
  throw new Error(`no error class is mapped to the rule ${JSON.stringify(rule)}`);
}
