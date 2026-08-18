/**
 * Properties of the codec that the vectors state but do not exhaust:
 * round-tripping in both directions over generated values, the truncation and
 * trailing-byte properties of every canonical vector encoding, and the values
 * the encoder itself must refuse.
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  Decimal,
  DcborError,
  MAX_NESTING_DEPTH,
  decode,
  encode,
  type DcborValue,
} from "../src/dcbor.ts";
import { bytesToHex, hexToBytes } from "../src/hex.ts";
import { loadVectors, section } from "./vectors.ts";

test("decode(encode(v)) === v and encode(decode(b)) === b over generated values", () => {
  const next = lcg(0x1d1a1067);
  for (let i = 0; i < 2000; i++) {
    const value = randomValue(next, 0);
    const bytes = encode(value);
    const decoded = decode(bytes);
    assert.deepEqual(decoded, value, `round trip of ${bytesToHex(bytes)}`);
    assert.equal(bytesToHex(encode(decoded)), bytesToHex(bytes), "re-encoding is stable");
  }
});

test("map key order is a property of the encoding, not of insertion", () => {
  const forwards = new Map<string, DcborValue>([
    ["v", 1n],
    ["ts", 1740067200n],
    ["ops", []],
    ["type", "public"],
  ]);
  const backwards = new Map<string, DcborValue>([...forwards].reverse());
  assert.equal(bytesToHex(encode(backwards)), bytesToHex(encode(forwards)));
  // "ops" < "ts" < "type" < "v": length first, then bytewise.
  assert.deepEqual([...decode(encode(backwards)) as Map<string, DcborValue>].map(([k]) => k), [
    "v",
    "ts",
    "ops",
    "type",
  ]);
});

test("every proper prefix of a canonical encoding is rejected", () => {
  const vectors = loadVectors("dcbor.json");
  for (const name of ["encoding_reference", "canonical", "decimal_fractions"]) {
    for (const vector of section(vectors, name).cases) {
      assert.ok(vector.dcbor !== undefined);
      const bytes = hexToBytes(vector.dcbor);
      for (let cut = 0; cut < bytes.length; cut++) {
        assert.throws(
          () => decode(bytes.subarray(0, cut)),
          DcborError,
          `${vector.name}: the first ${cut} bytes must not decode`,
        );
      }
      // A Dialog document is exactly one data item.
      const extended = new Uint8Array(bytes.length + 1);
      extended.set(bytes, 0);
      extended[bytes.length] = 0xf6;
      assert.throws(() => decode(extended), DcborError, `${vector.name}: trailing byte`);
    }
  }
});

test("the encoder refuses values outside the profile", () => {
  const cases: Array<{ value: unknown; code: string; what: string }> = [
    { value: true, code: "unsupported-type", what: "true" },
    { value: false, code: "unsupported-type", what: "false" },
    { value: undefined, code: "unsupported-type", what: "undefined" },
    { value: { description: "France" }, code: "unsupported-type", what: "a plain object" },
    { value: 3.14, code: "float", what: "a non-integer number" },
    { value: Number.NaN, code: "float", what: "NaN" },
    { value: Number.POSITIVE_INFINITY, code: "float", what: "infinity" },
    { value: 2 ** 60, code: "range", what: "an unsafe integer" },
    { value: 1n << 64n, code: "range", what: "2^64" },
    { value: -(1n << 64n) - 1n, code: "range", what: "-2^64-1" },
    { value: "\ud800", code: "utf8", what: "an unpaired high surrogate" },
    { value: "\udc00", code: "utf8", what: "an unpaired low surrogate" },
    { value: new Map([["ok", "\ud800"]]), code: "utf8", what: "a surrogate in a map value" },
  ];
  for (const { value, code, what } of cases) {
    assert.throws(
      () => encode(value as DcborValue),
      (e: unknown) => e instanceof DcborError && e.code === code,
      `${what} must be refused as ${code}`,
    );
  }
  // The extremes themselves do encode.
  assert.equal(bytesToHex(encode((1n << 64n) - 1n)), "1bffffffffffffffff");
  assert.equal(bytesToHex(encode(-(1n << 64n))), "3bffffffffffffffff");
  // A number that is a safe integer encodes exactly like its bigint.
  assert.equal(bytesToHex(encode(1740067200)), bytesToHex(encode(1740067200n)));
});

test("Decimal enforces the canonicalization rules at construction", () => {
  const int64Max = (1n << 63n) - 1n;
  const int64Min = -(1n << 63n);
  // Canonical.
  assert.equal(bytesToHex(encode(new Decimal(-2n, 314n))), "c4822119013a");
  assert.equal(bytesToHex(encode(new Decimal(-1n, int64Max))), "c482201b7fffffffffffffff");
  assert.equal(bytesToHex(encode(new Decimal(int64Min, 3n))), "c4823b7fffffffffffffff03");
  // Non-canonical.
  for (const [exponent, mantissa, what] of [
    [0n, 314n, "a zero exponent"],
    [2n, 314n, "a positive exponent"],
    [-1n, 0n, "a zero mantissa"],
    [-3n, 3140n, "a mantissa divisible by 10"],
    [-1n, int64Max + 1n, "a mantissa above 2^63-1"],
    [-1n, int64Min - 1n, "a mantissa below -2^63"],
    [int64Min - 1n, 3n, "an exponent below -2^63"],
  ] as Array<[bigint, bigint, string]>) {
    assert.throws(
      () => new Decimal(exponent, mantissa),
      (e: unknown) => e instanceof DcborError && e.code === "decimal",
      `${what} must be refused`,
    );
  }
});

test("nesting is bounded rather than exhausting the stack", () => {
  // Rule 10 counts containers, the outermost at depth 1, so n wrappings of a
  // non-container put the innermost container at depth n.
  const wrap = (inner: DcborValue, n: number): DcborValue => {
    let value = inner;
    for (let i = 0; i < n; i++) value = [value];
    return value;
  };
  const arrayHeads = (n: number): Uint8Array => new Uint8Array(n).fill(0x81);

  // At the bound, both directions work — with an array, a map and a decimal
  // fraction as the deepest container, since all three are one level. An
  // integer is not a container, so it takes one more wrapping to reach 64.
  const atTheLimit: DcborValue[] = [
    wrap(0n, MAX_NESTING_DEPTH),
    wrap(new Map<string, DcborValue>([["a", 1n]]), MAX_NESTING_DEPTH - 1),
    wrap(new Decimal(-2, 314n), MAX_NESTING_DEPTH - 1),
  ];
  for (const value of atTheLimit) {
    assert.deepEqual(decode(encode(value)), value);
  }

  // One container too many is a rejection on both sides.
  for (const tooDeep of [
    wrap(0n, MAX_NESTING_DEPTH + 1),
    wrap(new Map<string, DcborValue>([["a", 1n]]), MAX_NESTING_DEPTH),
    wrap(new Decimal(-2, 314n), MAX_NESTING_DEPTH),
  ]) {
    assert.throws(
      () => encode(tooDeep),
      (e: unknown) => e instanceof DcborError && e.code === "depth",
    );
  }

  // And on decode, at the boundary and far past it.
  assert.deepEqual(
    decode(new Uint8Array([...arrayHeads(MAX_NESTING_DEPTH - 1), 0x80])),
    wrap([], MAX_NESTING_DEPTH - 1),
  );
  for (const bytes of [
    new Uint8Array([...arrayHeads(MAX_NESTING_DEPTH), 0x80]),
    arrayHeads(MAX_NESTING_DEPTH * 100),
  ]) {
    assert.throws(
      () => decode(bytes),
      (e: unknown) => e instanceof DcborError && e.code === "depth",
    );
  }
});

// ---------------------------------------------------------------------------
// A small deterministic generator: no dependencies, same values every run.
// ---------------------------------------------------------------------------

function lcg(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return state / 0x1_0000_0000;
  };
}

function randomInt(next: () => number, bound: number): number {
  return Math.floor(next() * bound);
}

const CODE_POINTS = [0x41, 0x7a, 0xe9, 0x301, 0x3b1, 0x4e2d, 0x1f5ff, 0x10ffff, 0x20, 0x5f];

function randomText(next: () => number): string {
  const length = randomInt(next, 6);
  let out = "";
  for (let i = 0; i < length; i++) {
    out += String.fromCodePoint(CODE_POINTS[randomInt(next, CODE_POINTS.length)]!);
  }
  return out;
}

function randomInteger(next: () => number): bigint {
  const magnitude = [24n, 256n, 65536n, 1n << 32n, 1n << 64n][randomInt(next, 5)]!;
  const value = BigInt(Math.floor(next() * Number.MAX_SAFE_INTEGER)) % magnitude;
  return next() < 0.5 ? value : -1n - value;
}

function randomDecimal(next: () => number): Decimal {
  let mantissa = randomInteger(next) % ((1n << 62n) - 1n);
  if (mantissa === 0n) mantissa = 7n;
  while (mantissa % 10n === 0n) mantissa += 1n;
  const exponent = -1n - (BigInt(randomInt(next, 40)) % 40n);
  return new Decimal(exponent, mantissa);
}

function randomValue(next: () => number, depth: number): DcborValue {
  const scalarOnly = depth >= 4;
  const kind = randomInt(next, scalarOnly ? 5 : 7);
  switch (kind) {
    case 0:
      return null;
    case 1:
      return randomInteger(next);
    case 2:
      return randomText(next);
    case 3: {
      const bytes = new Uint8Array(randomInt(next, 40));
      for (let i = 0; i < bytes.length; i++) bytes[i] = randomInt(next, 256);
      return bytes;
    }
    case 4:
      return randomDecimal(next);
    case 5: {
      const items: DcborValue[] = [];
      for (let i = randomInt(next, 5); i > 0; i--) items.push(randomValue(next, depth + 1));
      return items;
    }
    default: {
      const map = new Map<string, DcborValue>();
      for (let i = randomInt(next, 5); i > 0; i--) {
        map.set(randomText(next), randomValue(next, depth + 1));
      }
      return map;
    }
  }
}
