/**
 * Dialog's deterministic CBOR profile.
 *
 * Implements `spec/03-encoding.md`, "Deterministic CBOR": the Core
 * Deterministic Encoding Requirements of RFC 8949 §4.2.1 plus the eight rules
 * that section adds, the text-string rules of "Text strings and Unicode", and
 * the canonicalization rules of "Decimal fractions".
 *
 * The profile is narrow on purpose: no floating-point values, no booleans, one
 * tag (4, decimal fractions), null as the only simple value. Anything outside
 * it is rejected rather than ignored, because a content-addressed format
 * cannot afford a decoder that accepts bytes it does not fully account for
 * (spec/03-encoding.md, rule 8 and its informative note).
 *
 * The code here is browser-safe: no `node:` imports, no Node-only globals.
 */

/** The value model this codec encodes and decodes.
 *
 * - integers are `bigint` (or a `number` that is a safe integer, on encode);
 *   `decode` always yields `bigint`, so `decode(encode(v))` returns integers
 *   as `bigint`.
 * - text strings are `string`, byte strings are `Uint8Array`.
 * - arrays are JS arrays, maps are `Map` keyed by text strings.
 * - decimal fractions (CBOR tag 4) are {@link Decimal}.
 * - `null` is the only simple value.
 */
export type DcborValue =
  | null
  | bigint
  | number
  | string
  | Uint8Array
  | Decimal
  | DcborValue[]
  | Map<string, DcborValue>;

/**
 * The class of profile violation an error reports. The names correspond to the
 * numbered rules of spec/03-encoding.md, "Deterministic CBOR", so that a
 * caller (and the conformance suite) can tell one rejection from another.
 */
export type DcborErrorCode =
  /** rule 1: integers and lengths use the shortest possible encoding. */
  | "shortest"
  /** rule 2: map keys are sorted by the bytewise order of their encoding. */
  | "map-key-order"
  /** rule 3: no duplicate map keys. */
  | "duplicate-key"
  /** rule 4: no indefinite-length items. */
  | "indefinite"
  /** rule 5: no floating-point values. */
  | "float"
  /** rule 6: no tags other than tag 4. */
  | "tag"
  /** rule 7: null is the only simple value. */
  | "simple"
  /** map keys are text strings. */
  | "map-key-type"
  /** text strings (and map keys) are well-formed UTF-8. */
  | "utf8"
  /** the shape, canonicalization or 64-bit bounds of a tag 4 decimal fraction. */
  | "decimal"
  /** an integer outside the range CBOR can express. */
  | "range"
  /** RFC 8949 §3: the input is not exactly one well-formed data item. */
  | "malformed"
  /** the value handed to the encoder is not part of the profile at all. */
  | "unsupported-type"
  /** nesting deeper than this decoder accepts (see {@link MAX_NESTING_DEPTH}). */
  | "depth";

/** A rejection by the dCBOR encoder or decoder. */
export class DcborError extends Error {
  readonly code: DcborErrorCode;

  constructor(code: DcborErrorCode, message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "DcborError";
    this.code = code;
  }
}

const INT64_MIN = -(1n << 63n);
const INT64_MAX = (1n << 63n) - 1n;
/** The largest argument a CBOR head can carry: 2^64-1. */
const MAX_ARGUMENT = (1n << 64n) - 1n;

/**
 * The deepest nesting this codec accepts. The specification fixes no bound;
 * every recursive decoder needs one so that hostile input fails as a rejection
 * rather than as a stack overflow. See todos/057.
 */
export const MAX_NESTING_DEPTH = 1024;

/**
 * A decimal fraction: `mantissa × 10^exponent`, CBOR tag 4, the only tag the
 * profile admits (spec/03-encoding.md, "Decimal fractions").
 *
 * The constructor enforces the canonicalization rules, so an instance always
 * has exactly one valid encoding: the exponent is negative, the mantissa is
 * neither zero nor divisible by 10, and both components lie in the signed
 * 64-bit range.
 */
export class Decimal {
  readonly exponent: bigint;
  readonly mantissa: bigint;

  constructor(exponent: bigint | number, mantissa: bigint | number) {
    const e = toBigInt(exponent, "exponent");
    const m = toBigInt(mantissa, "mantissa");
    if (e < INT64_MIN || e > INT64_MAX) {
      throw new DcborError(
        "decimal",
        `decimal fraction exponent ${e} is outside the signed 64-bit range`,
      );
    }
    if (m < INT64_MIN || m > INT64_MAX) {
      throw new DcborError(
        "decimal",
        `decimal fraction mantissa ${m} is outside the signed 64-bit range`,
      );
    }
    if (e >= 0n) {
      throw new DcborError(
        "decimal",
        `decimal fraction exponent ${e} is not negative: a whole number MUST be encoded as a plain integer`,
      );
    }
    if (m === 0n) {
      throw new DcborError(
        "decimal",
        "decimal fraction mantissa is zero: zero MUST be encoded as the integer 0",
      );
    }
    if (m % 10n === 0n) {
      throw new DcborError(
        "decimal",
        `decimal fraction mantissa ${m} is divisible by 10: trailing zeros MUST be absorbed into the exponent`,
      );
    }
    this.exponent = e;
    this.mantissa = m;
  }

  /** The value in scientific notation, for diagnostics only. */
  toString(): string {
    return `${this.mantissa}e${this.exponent}`;
  }
}

function toBigInt(v: bigint | number, what: string): bigint {
  if (typeof v === "number") {
    if (!Number.isSafeInteger(v)) {
      throw new DcborError(
        "decimal",
        `decimal fraction ${what} ${v} is not a safe integer; pass a bigint`,
      );
    }
    return BigInt(v);
  }
  return v;
}

// ---------------------------------------------------------------------------
// Encoding
// ---------------------------------------------------------------------------

class ByteWriter {
  private buf = new Uint8Array(64);
  private len = 0;

  private ensure(n: number): void {
    if (this.len + n <= this.buf.length) return;
    let cap = this.buf.length * 2;
    while (cap < this.len + n) cap *= 2;
    const next = new Uint8Array(cap);
    next.set(this.buf.subarray(0, this.len));
    this.buf = next;
  }

  byte(b: number): void {
    this.ensure(1);
    this.buf[this.len++] = b;
  }

  write(bytes: Uint8Array): void {
    this.ensure(bytes.length);
    this.buf.set(bytes, this.len);
    this.len += bytes.length;
  }

  take(): Uint8Array {
    return this.buf.slice(0, this.len);
  }
}

const TEXT_ENCODER = new TextEncoder();
const TEXT_DECODER = new TextDecoder("utf-8", { fatal: true });

/** Encode one value as Dialog dCBOR. */
export function encode(value: DcborValue): Uint8Array {
  const w = new ByteWriter();
  encodeInto(w, value, 0);
  return w.take();
}

function encodeInto(w: ByteWriter, value: DcborValue, depth: number): void {
  if (depth > MAX_NESTING_DEPTH) {
    throw new DcborError("depth", `nesting deeper than ${MAX_NESTING_DEPTH} items`);
  }
  if (value === null) {
    w.byte(0xf6);
    return;
  }
  if (typeof value === "bigint") {
    encodeInteger(w, value);
    return;
  }
  if (typeof value === "number") {
    if (!Number.isInteger(value)) {
      throw new DcborError(
        "float",
        `the profile admits no floating-point values (got ${value})`,
      );
    }
    if (!Number.isSafeInteger(value)) {
      throw new DcborError(
        "range",
        `${value} is not a safe integer; pass a bigint to encode it exactly`,
      );
    }
    encodeInteger(w, BigInt(value));
    return;
  }
  if (typeof value === "string") {
    const bytes = encodeText(value);
    writeHead(w, 3, BigInt(bytes.length));
    w.write(bytes);
    return;
  }
  if (value instanceof Uint8Array) {
    writeHead(w, 2, BigInt(value.length));
    w.write(value);
    return;
  }
  if (value instanceof Decimal) {
    w.byte(0xc4);
    w.byte(0x82);
    encodeInteger(w, value.exponent);
    encodeInteger(w, value.mantissa);
    return;
  }
  if (Array.isArray(value)) {
    writeHead(w, 4, BigInt(value.length));
    for (const item of value) encodeInto(w, item, depth + 1);
    return;
  }
  if (value instanceof Map) {
    encodeMap(w, value, depth);
    return;
  }
  throw new DcborError(
    "unsupported-type",
    `${describe(value)} is not part of Dialog's CBOR profile`,
  );
}

function describe(value: unknown): string {
  if (typeof value === "boolean") return "a boolean";
  if (value === undefined) return "undefined";
  return `a value of type ${typeof value}`;
}

function encodeMap(w: ByteWriter, map: Map<string, DcborValue>, depth: number): void {
  const entries: Array<{ key: Uint8Array; value: DcborValue }> = [];
  for (const [key, value] of map) {
    if (typeof key !== "string") {
      throw new DcborError("map-key-type", "map keys MUST be text strings");
    }
    const kw = new ByteWriter();
    const bytes = encodeText(key);
    writeHead(kw, 3, BigInt(bytes.length));
    kw.write(bytes);
    entries.push({ key: kw.take(), value });
  }
  // Rule 2: bytewise lexicographic order of the encoded key.
  entries.sort((a, b) => compareBytes(a.key, b.key));
  for (let i = 1; i < entries.length; i++) {
    if (compareBytes(entries[i - 1]!.key, entries[i]!.key) === 0) {
      throw new DcborError("duplicate-key", "a map key appears more than once");
    }
  }
  writeHead(w, 5, BigInt(entries.length));
  for (const entry of entries) {
    w.write(entry.key);
    encodeInto(w, entry.value, depth + 1);
  }
}

function encodeInteger(w: ByteWriter, value: bigint): void {
  if (value >= 0n) {
    if (value > MAX_ARGUMENT) {
      throw new DcborError("range", `${value} exceeds the largest CBOR unsigned integer`);
    }
    writeHead(w, 0, value);
    return;
  }
  const argument = -1n - value;
  if (argument > MAX_ARGUMENT) {
    throw new DcborError("range", `${value} is below the smallest CBOR negative integer`);
  }
  writeHead(w, 1, argument);
}

/** Rule 1: the argument goes in the head byte when it fits, else in the
 * shortest of the 1-, 2-, 4- and 8-byte forms. */
function writeHead(w: ByteWriter, major: number, argument: bigint): void {
  const base = major << 5;
  if (argument < 24n) {
    w.byte(base | Number(argument));
  } else if (argument <= 0xffn) {
    w.byte(base | 24);
    writeArgument(w, argument, 1);
  } else if (argument <= 0xffffn) {
    w.byte(base | 25);
    writeArgument(w, argument, 2);
  } else if (argument <= 0xffffffffn) {
    w.byte(base | 26);
    writeArgument(w, argument, 4);
  } else {
    w.byte(base | 27);
    writeArgument(w, argument, 8);
  }
}

function writeArgument(w: ByteWriter, argument: bigint, size: number): void {
  for (let i = size - 1; i >= 0; i--) {
    w.byte(Number((argument >> BigInt(i * 8)) & 0xffn));
  }
}

/**
 * UTF-8 bytes of a text string, rejecting a JS string that cannot be one:
 * unpaired surrogates would otherwise be replaced by U+FFFD and silently
 * change the entity's digest.
 */
function encodeText(text: string): Uint8Array {
  for (let i = 0; i < text.length; i++) {
    const unit = text.charCodeAt(i);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const low = text.charCodeAt(i + 1);
      if (!(low >= 0xdc00 && low <= 0xdfff)) {
        throw new DcborError("utf8", `unpaired high surrogate at index ${i}`);
      }
      i++;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      throw new DcborError("utf8", `unpaired low surrogate at index ${i}`);
    }
  }
  return TEXT_ENCODER.encode(text);
}

function compareBytes(a: Uint8Array, b: Uint8Array): number {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    const d = a[i]! - b[i]!;
    if (d !== 0) return d;
  }
  return a.length - b.length;
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

interface Cursor {
  readonly buf: Uint8Array;
  pos: number;
}

interface Head {
  major: number;
  argument: bigint;
}

/**
 * Decode exactly one Dialog dCBOR data item.
 *
 * Every deviation from the profile is a rejection: non-shortest encodings,
 * indefinite lengths, floats, non-null simple values, tags other than a
 * canonical tag 4, non-text/unsorted/duplicate map keys, invalid UTF-8,
 * truncation and trailing bytes.
 */
export function decode(bytes: Uint8Array): DcborValue {
  const cursor: Cursor = { buf: bytes, pos: 0 };
  const value = decodeItem(cursor, 0);
  if (cursor.pos !== bytes.length) {
    throw new DcborError(
      "malformed",
      `${bytes.length - cursor.pos} byte(s) follow the data item; a Dialog document is exactly one data item`,
    );
  }
  return value;
}

function decodeItem(c: Cursor, depth: number): DcborValue {
  if (depth > MAX_NESTING_DEPTH) {
    throw new DcborError("depth", `nesting deeper than ${MAX_NESTING_DEPTH} items`);
  }
  if (c.pos >= c.buf.length) {
    throw new DcborError("malformed", "unexpected end of input: no data item");
  }
  const major = c.buf[c.pos]! >> 5;
  // Major type 7 carries no argument in the rule-1 sense; its additional
  // information selects a simple value or a float, both rejected below.
  if (major === 7) return decodeMajor7(c);

  const head = readHead(c);
  switch (head.major) {
    case 0:
      return head.argument;
    case 1:
      return -1n - head.argument;
    case 2: {
      const n = itemLength(c, head.argument, "byte string");
      const bytes = c.buf.slice(c.pos, c.pos + n);
      c.pos += n;
      return bytes;
    }
    case 3: {
      const n = itemLength(c, head.argument, "text string");
      const bytes = c.buf.subarray(c.pos, c.pos + n);
      c.pos += n;
      return decodeText(bytes);
    }
    case 4: {
      const n = countLength(c, head.argument, "array");
      const items: DcborValue[] = [];
      for (let i = 0; i < n; i++) items.push(decodeItem(c, depth + 1));
      return items;
    }
    case 5:
      return decodeMap(c, countLength(c, head.argument, "map"), depth);
    default:
      // Major type 6: tags. Rule 6 admits tag 4 and nothing else.
      if (head.argument !== 4n) {
        throw new DcborError(
          "tag",
          `tag ${head.argument} is not permitted; tag 4 (decimal fraction) is the only tag in the profile`,
        );
      }
      return decodeDecimal(c);
  }
}

/** Read a head byte and its argument, enforcing rule 1 and rejecting
 * indefinite lengths (rule 4) and the reserved additional information. */
function readHead(c: Cursor): Head {
  if (c.pos >= c.buf.length) {
    throw new DcborError("malformed", "unexpected end of input: no head byte");
  }
  const initial = c.buf[c.pos]!;
  c.pos++;
  const major = initial >> 5;
  const info = initial & 31;
  if (info < 24) return { major, argument: BigInt(info) };
  if (info === 31) {
    if (major >= 2 && major <= 5) {
      throw new DcborError(
        "indefinite",
        `indefinite-length item (major type ${major}); all items MUST use definite lengths`,
      );
    }
    throw new DcborError(
      "malformed",
      `additional information 31 is not well-formed for major type ${major}`,
    );
  }
  if (info > 27) {
    throw new DcborError(
      "malformed",
      `additional information ${info} is reserved`,
    );
  }
  const size = 1 << (info - 24);
  if (c.pos + size > c.buf.length) {
    throw new DcborError(
      "malformed",
      `truncated ${size}-byte argument for major type ${major}`,
    );
  }
  let argument = 0n;
  for (let i = 0; i < size; i++) {
    argument = (argument << 8n) | BigInt(c.buf[c.pos + i]!);
  }
  c.pos += size;
  const minimum = size === 1 ? 24n : 1n << BigInt((size >> 1) * 8);
  if (argument < minimum) {
    throw new DcborError(
      "shortest",
      `${argument} is encoded in a ${size}-byte argument but fits a shorter one`,
    );
  }
  return { major, argument };
}

/** A byte- or text-string length, checked against what the input can hold. */
function itemLength(c: Cursor, argument: bigint, what: string): number {
  const remaining = BigInt(c.buf.length - c.pos);
  if (argument > remaining) {
    throw new DcborError(
      "malformed",
      `${what} declares ${argument} bytes but only ${remaining} remain`,
    );
  }
  return Number(argument);
}

/** An array or map element count. Every element takes at least one byte, so a
 * count larger than the bytes remaining cannot be satisfied. */
function countLength(c: Cursor, argument: bigint, what: string): number {
  const remaining = BigInt(c.buf.length - c.pos);
  if (argument > remaining) {
    throw new DcborError(
      "malformed",
      `${what} declares ${argument} entries but only ${remaining} bytes remain`,
    );
  }
  return Number(argument);
}

function decodeText(bytes: Uint8Array): string {
  try {
    return TEXT_DECODER.decode(bytes);
  } catch (cause) {
    throw new DcborError("utf8", "text string is not well-formed UTF-8", { cause });
  }
}

function decodeMap(c: Cursor, entries: number, depth: number): Map<string, DcborValue> {
  const map = new Map<string, DcborValue>();
  let previousKey: Uint8Array | null = null;
  for (let i = 0; i < entries; i++) {
    if (c.pos >= c.buf.length) {
      throw new DcborError("malformed", "unexpected end of input inside a map");
    }
    if (c.buf[c.pos]! >> 5 !== 3) {
      throw new DcborError(
        "map-key-type",
        `map keys MUST be text strings (found major type ${c.buf[c.pos]! >> 5})`,
      );
    }
    const keyStart = c.pos;
    const key = decodeItem(c, depth + 1) as string;
    const keyBytes = c.buf.subarray(keyStart, c.pos);
    if (previousKey !== null) {
      const order = compareBytes(previousKey, keyBytes);
      if (order === 0) {
        throw new DcborError("duplicate-key", `map key ${JSON.stringify(key)} appears twice`);
      }
      if (order > 0) {
        throw new DcborError(
          "map-key-order",
          `map key ${JSON.stringify(key)} is out of order; keys MUST be sorted bytewise by their encoding`,
        );
      }
    }
    map.set(key, decodeItem(c, depth + 1));
    previousKey = keyBytes;
  }
  return map;
}

/**
 * Tag 4 content: a definite-length array of exactly two shortest-form
 * integers, exponent first, both inside the signed 64-bit range and
 * canonical per spec/03-encoding.md, "Decimal fractions".
 *
 * Every violation inside the tag is reported as a decimal-fraction error, with
 * the underlying rule kept as the error's cause.
 */
function decodeDecimal(c: Cursor): Decimal {
  try {
    const head = readHead(c);
    if (head.major !== 4) {
      throw new DcborError(
        "decimal",
        `tag 4 content MUST be an array (found major type ${head.major})`,
      );
    }
    if (head.argument !== 2n) {
      throw new DcborError(
        "decimal",
        `tag 4 content MUST be an array of exactly two elements (found ${head.argument})`,
      );
    }
    const exponent = decodeDecimalComponent(c, "exponent");
    const mantissa = decodeDecimalComponent(c, "mantissa");
    return new Decimal(exponent, mantissa);
  } catch (cause) {
    if (cause instanceof DcborError && cause.code === "decimal") throw cause;
    const reason = cause instanceof Error ? cause.message : String(cause);
    throw new DcborError("decimal", `invalid decimal fraction: ${reason}`, { cause });
  }
}

function decodeDecimalComponent(c: Cursor, what: string): bigint {
  if (c.pos >= c.buf.length) {
    throw new DcborError("decimal", `unexpected end of input in place of the ${what}`);
  }
  const major = c.buf[c.pos]! >> 5;
  if (major !== 0 && major !== 1) {
    throw new DcborError(
      "decimal",
      `the ${what} of a decimal fraction MUST be an integer (found major type ${major})`,
    );
  }
  const head = readHead(c);
  const value = head.major === 0 ? head.argument : -1n - head.argument;
  if (value < INT64_MIN || value > INT64_MAX) {
    throw new DcborError(
      "decimal",
      `decimal fraction ${what} ${value} is outside the signed 64-bit range`,
    );
  }
  return value;
}

function decodeMajor7(c: Cursor): null {
  const info = c.buf[c.pos]! & 31;
  if (info === 22) {
    c.pos++;
    return null;
  }
  if (info >= 25 && info <= 27) {
    throw new DcborError(
      "float",
      `floating-point value (major type 7, additional information ${info}); the profile admits no floats`,
    );
  }
  if (info <= 24) {
    throw new DcborError(
      "simple",
      `simple value (major type 7, additional information ${info}); null is the only simple value in the profile`,
    );
  }
  if (info === 31) {
    throw new DcborError(
      "malformed",
      "break byte outside an indefinite-length item",
    );
  }
  throw new DcborError("malformed", `additional information ${info} is reserved`);
}
