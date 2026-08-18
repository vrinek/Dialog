/**
 * The Dialog data model: atoms, bonds, molecules and the fillers a molecule is
 * built from.
 *
 * Implements `spec/01-data-model.md` in full — the three entity maps, the
 * template-variable grammar, the five filler types and the canonical timestamp
 * profile — over the dCBOR profile of `spec/03-encoding.md` (`./dcbor.ts`) and
 * the digest/CID rules of the same document (`./cid.ts`). The five standard
 * meta-bonds of `spec/06-meta-bonds.md` are at the bottom of the file: they are
 * ordinary bonds, and only their digests distinguish a meta-molecule.
 *
 * Two rules shape everything here. An entity's identifier is the hash of its
 * bytes, so a constructor that accepts an invalid entity mints an identifier no
 * other implementation will produce; and a decoder that accepts bytes it does
 * not fully account for computes a digest for a structure another
 * implementation refuses (spec/03-encoding.md, rule 8). Every constructor
 * therefore validates, and every decoder is strict: unknown keys, missing keys,
 * wrong value shapes and out-of-profile timestamps are rejections.
 *
 * Browser-safe: no `node:` imports, no Node-only globals.
 */

import { DIGEST_SIZE, cid, cidToText, digest } from "./cid.ts";
import { Decimal, type DcborValue, decode, encode } from "./dcbor.ts";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/**
 * The class of data-model violation an error reports. The names follow the
 * structure of spec/01-data-model.md, so a caller (and the conformance suite)
 * can tell one rejection from another.
 */
export type EntityErrorCode =
  /** an atom's description is missing, not text, or empty. */
  | "description"
  /** a bond's template is missing, not text, empty, variable-free, or repeats
   * a variable name. */
  | "template"
  /** an internal reference is not a raw 32-byte digest. */
  | "digest"
  /** a molecule's filler list is missing, not a list, or empty. */
  | "fillers"
  /** a filler's type tag is not one of 0 to 4. */
  | "filler-type"
  /** a filler's value does not match the shape its type tag names. */
  | "filler-value"
  /** a type 3 filler's IPFS URI is empty. */
  | "ipfs-uri"
  /** a scalar filler's value is neither a quantity nor a datetime range. */
  | "scalar"
  /** a timestamp is outside Dialog's canonical RFC 3339 profile. */
  | "timestamp"
  /** a datetime range's `from` is later than its `to`. */
  | "range"
  /** a molecule's filler count differs from its bond's variable count. */
  | "filler-count"
  /** the value decoded is not the entity map it was read as. */
  | "shape";

/** A rejection by an entity constructor, encoder or decoder. */
export class EntityError extends Error {
  readonly code: EntityErrorCode;

  constructor(code: EntityErrorCode, message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "EntityError";
    this.code = code;
  }
}

// ---------------------------------------------------------------------------
// The model
// ---------------------------------------------------------------------------

/** `atom = { "description" => tstr }` — a single, unambiguous entity. */
export interface Atom {
  readonly kind: "atom";
  readonly description: string;
}

/** `bond = { "template" => tstr }` — a relationship template, a sentence with
 * named variables. */
export interface Bond {
  readonly kind: "bond";
  readonly template: string;
}

/** `molecule = { "bond" => bstr .size 32, "fillers" => [+ filler] }` — a
 * complete statement: a bond with its blanks filled in.
 *
 * The bond is named by its raw 32-byte digest, never by a CID
 * (spec/03-encoding.md, "Internal references"), which is why a molecule alone
 * cannot check its filler count against the template — see
 * {@link checkFillerCount}. */
export interface Molecule {
  readonly kind: "molecule";
  readonly bond: Uint8Array;
  readonly fillers: readonly Filler[];
}

/** An atom, a bond or a molecule. */
export type Entity = Atom | Bond | Molecule;

/** The type tag of a filler (spec/01-data-model.md, "Filler types"). */
export const FillerType = {
  /** 0 — the digest of an atom. */
  Atom: 0,
  /** 1 — the digest of a bond. */
  Bond: 1,
  /** 2 — the digest of a molecule. */
  Molecule: 2,
  /** 3 — a non-empty IPFS content identifier, as text. */
  IpfsUri: 3,
  /** 4 — a scalar: a quantity with an optional unit, or a datetime range. */
  Scalar: 4,
} as const;

/** One of the five filler type tags. */
export type FillerTypeTag = (typeof FillerType)[keyof typeof FillerType];

/**
 * A filler, as the CDDL defines it: each type tag bound to the one value shape
 * it permits. A filler whose value does not match its tag MUST be rejected
 * (spec/01-data-model.md, "Filler types").
 */
export type Filler =
  | { readonly type: 0; readonly value: Uint8Array }
  | { readonly type: 1; readonly value: Uint8Array }
  | { readonly type: 2; readonly value: Uint8Array }
  | { readonly type: 3; readonly value: string }
  | { readonly type: 4; readonly value: ScalarValue };

/** A scalar filler's value: a quantity, or a datetime range. */
export type ScalarValue = Quantity | DatetimeRange;

/** A number, optionally carrying the digest of the atom that names its unit. */
export interface Quantity {
  readonly value: bigint | Decimal;
  readonly unit?: Uint8Array;
}

/** Two canonical timestamps, `from` no later than `to`. A range whose
 * endpoints are equal is how Dialog writes a single instant; there are no
 * plain dates. */
export interface DatetimeRange {
  readonly from: string;
  readonly to: string;
}

/** Whether a scalar value is a datetime range rather than a quantity. */
export function isDatetimeRange(scalar: ScalarValue): scalar is DatetimeRange {
  return "from" in scalar || "to" in scalar;
}

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

/**
 * An atom. The description MUST be a non-empty UTF-8 string; any difference in
 * it, however minor, produces a different atom.
 */
export function newAtom(description: string): Atom {
  const atom: Atom = { kind: "atom", description };
  validateAtom(atom);
  return atom;
}

/**
 * A bond. The template MUST be a non-empty UTF-8 string containing one or more
 * variables, with no variable name repeated (see {@link templateVariables}).
 */
export function newBond(template: string): Bond {
  const bond: Bond = { kind: "bond", template };
  validateBond(bond);
  return bond;
}

/**
 * A molecule from a bond *digest* and its fillers.
 *
 * The filler count cannot be checked here: the bond is named by a digest, and
 * the template it hashes is not in hand. That check belongs where the
 * specification puts it — at block validation, where every digest an operation
 * carries has been resolved (spec/02-block-format.md, "Validation" rule 5) —
 * and is available as {@link checkFillerCount} and {@link newMoleculeForBond}.
 */
export function newMolecule(bondDigest: Uint8Array, fillers: readonly Filler[]): Molecule {
  const molecule: Molecule = {
    kind: "molecule",
    bond: bondDigest,
    fillers: [...fillers],
  };
  validateMolecule(molecule);
  return molecule;
}

/**
 * A molecule from the bond itself, which lets the filler count be checked
 * against the template's variables straight away.
 */
export function newMoleculeForBond(bond: Bond, fillers: readonly Filler[]): Molecule {
  validateBond(bond);
  const molecule = newMolecule(entityDigest(bond), fillers);
  checkFillerCount(molecule, bond);
  return molecule;
}

/**
 * The rule of spec/01-data-model.md: the number of fillers MUST equal the
 * number of variables in the referenced bond template, and the fillers are
 * matched to the variables positionally.
 *
 * `bond` MUST be the bond the molecule names; a molecule holding another
 * bond's digest is rejected rather than checked against the wrong template.
 */
export function checkFillerCount(molecule: Molecule, bond: Bond): void {
  validateMolecule(molecule);
  validateBond(bond);
  const expected = entityDigest(bond);
  if (!bytesEqual(molecule.bond, expected)) {
    throw new EntityError(
      "digest",
      "the bond given is not the one this molecule names",
    );
  }
  const variables = templateVariables(bond.template);
  if (molecule.fillers.length !== variables.length) {
    throw new EntityError(
      "filler-count",
      `molecule has ${molecule.fillers.length} filler(s) but its bond template has ${variables.length} variable(s)`,
    );
  }
}

/** A type 0 filler: the digest of an atom. */
export function atomFiller(atomDigest: Uint8Array): Filler {
  const filler: Filler = { type: 0, value: atomDigest };
  validateFiller(filler);
  return filler;
}

/** A type 1 filler: the digest of a bond. */
export function bondFiller(bondDigest: Uint8Array): Filler {
  const filler: Filler = { type: 1, value: bondDigest };
  validateFiller(filler);
  return filler;
}

/** A type 2 filler: the digest of a molecule. */
export function moleculeFiller(moleculeDigest: Uint8Array): Filler {
  const filler: Filler = { type: 2, value: moleculeDigest };
  validateFiller(filler);
  return filler;
}

/**
 * A type 3 filler: an IPFS content identifier as text. It is not an internal
 * reference; its format beyond being non-empty is IPFS's, not Dialog's.
 */
export function ipfsFiller(uri: string): Filler {
  const filler: Filler = { type: 3, value: uri };
  validateFiller(filler);
  return filler;
}

/** A type 4 filler: a quantity or a datetime range. */
export function scalarFiller(value: ScalarValue): Filler {
  const filler: Filler = { type: 4, value };
  validateFiller(filler);
  return filler;
}

/**
 * A quantity: an integer or a decimal fraction, optionally with the digest of
 * the atom naming its unit. Whole numbers are plain integers, never decimal
 * fractions (spec/03-encoding.md, "Decimal fractions").
 */
export function quantity(value: bigint | number | Decimal, unit?: Uint8Array): Quantity {
  const number = typeof value === "number" ? safeInteger(value) : value;
  const q: Quantity = unit === undefined ? { value: number } : { value: number, unit };
  validateScalar(q);
  return q;
}

/** A datetime range. Both endpoints are validated, and `from` MUST NOT be
 * later than `to`; the two MAY be equal. */
export function datetimeRange(from: string, to: string): DatetimeRange {
  const range: DatetimeRange = { from, to };
  validateScalar(range);
  return range;
}

function safeInteger(value: number): bigint {
  if (!Number.isSafeInteger(value)) {
    throw new EntityError(
      "filler-value",
      `${value} is not a safe integer; pass a bigint or a Decimal`,
    );
  }
  return BigInt(value);
}

// ---------------------------------------------------------------------------
// The template-variable grammar
// ---------------------------------------------------------------------------

/**
 * The variables of a bond template, in the order they appear.
 *
 * The grammar is `variable = "_" 1*UCALPHA "_"` with a leftmost-longest match:
 * scan left to right, and when an underscore is encountered, consume the
 * longest run of uppercase ASCII letters followed by a closing underscore.
 * Every other underscore is literal text. The closing underscore is consumed,
 * so it cannot also open the next variable — which is what makes `_A_B_` the
 * variable `A` followed by the literal `B_`, while `_A__B_` is two variables
 * (spec/01-data-model.md, "Bonds", and its disambiguation table).
 *
 * Names are returned as written and may repeat; {@link validateBond} is what
 * refuses a template that repeats one.
 */
export function templateVariables(template: string): string[] {
  const variables: string[] = [];
  let i = 0;
  while (i < template.length) {
    if (template[i] !== "_") {
      i++;
      continue;
    }
    let end = i + 1;
    while (end < template.length && isUpperAlpha(template.charCodeAt(end))) end++;
    if (end > i + 1 && end < template.length && template[end] === "_") {
      variables.push(template.slice(i + 1, end));
      i = end + 1;
      continue;
    }
    i++;
  }
  return variables;
}

function isUpperAlpha(code: number): boolean {
  return code >= 0x41 && code <= 0x5a;
}

/** The variables of a bond, in template order. */
export function bondVariables(bond: Bond): string[] {
  validateBond(bond);
  return templateVariables(bond.template);
}

// ---------------------------------------------------------------------------
// The timestamp profile
// ---------------------------------------------------------------------------

/**
 * The lexical form of a Dialog timestamp: `YYYY-MM-DDTHH:MM:SSZ`, exactly 20
 * characters, uppercase `T` and `Z`, no fractional seconds, no numeric offset,
 * seconds `00`-`59` (spec/01-data-model.md, "Datetime ranges", rules 1 to 5).
 */
const TIMESTAMP_PATTERN =
  /^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]Z$/;

/** The length of a Dialog timestamp: `YYYY-MM-DDTHH:MM:SSZ`. */
export const TIMESTAMP_LENGTH = 20;

/**
 * Check a timestamp against the canonical profile, including the calendar rule
 * (rule 6): the day MUST exist in that month of that year in the **proleptic
 * Gregorian calendar**, whose leap-year rule applies to every year, before 1582
 * as well as after. `1500-02-29T00:00:00Z` is therefore not a Dialog timestamp
 * and `1600-02-29T00:00:00Z` is one; the year `0000` is permitted and is a leap
 * year like any other year divisible by 400.
 */
export function validateTimestamp(timestamp: string, what = "timestamp"): void {
  if (typeof timestamp !== "string") {
    throw new EntityError("timestamp", `${what} MUST be a text string`);
  }
  if (!TIMESTAMP_PATTERN.test(timestamp)) {
    throw new EntityError(
      "timestamp",
      `${what} ${JSON.stringify(timestamp)} is not a Dialog timestamp: the only form is YYYY-MM-DDTHH:MM:SSZ — UTC, uppercase T and Z, second precision, no fractional part, no leap second`,
    );
  }
  const year = Number(timestamp.slice(0, 4));
  const month = Number(timestamp.slice(5, 7));
  const day = Number(timestamp.slice(8, 10));
  const length = daysInMonth(year, month);
  if (day > length) {
    throw new EntityError(
      "timestamp",
      `${what} ${JSON.stringify(timestamp)} is not a real date: ${monthName(month)} ${year} has ${length} days in the proleptic Gregorian calendar`,
    );
  }
}

/** Whether a string is a Dialog timestamp. */
export function isTimestamp(timestamp: string): boolean {
  try {
    validateTimestamp(timestamp);
    return true;
  } catch {
    return false;
  }
}

const MONTH_LENGTHS = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
const MONTH_NAMES = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

function daysInMonth(year: number, month: number): number {
  if (month === 2 && isLeapYear(year)) return 29;
  return MONTH_LENGTHS[month - 1]!;
}

function monthName(month: number): string {
  return MONTH_NAMES[month - 1]!;
}

/** The proleptic Gregorian leap-year rule, applied to every year including
 * those before 1582 and the year 0. */
function isLeapYear(year: number): boolean {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

/** Check an atom: a non-empty, well-formed UTF-8 description. */
export function validateAtom(atom: Atom): void {
  checkText(atom.description, "atom description", "description");
  if (atom.description.length === 0) {
    throw new EntityError("description", "an atom's description MUST NOT be empty");
  }
}

/** Check a bond: a non-empty template with at least one variable and no
 * repeated variable name. */
export function validateBond(bond: Bond): void {
  checkText(bond.template, "bond template", "template");
  if (bond.template.length === 0) {
    throw new EntityError("template", "a bond's template MUST NOT be empty");
  }
  const variables = templateVariables(bond.template);
  if (variables.length === 0) {
    throw new EntityError(
      "template",
      `bond template ${JSON.stringify(bond.template)} contains no variable: a template MUST contain one or more _NAME_ variables`,
    );
  }
  const seen = new Set<string>();
  for (const name of variables) {
    if (seen.has(name)) {
      throw new EntityError(
        "template",
        `bond template ${JSON.stringify(bond.template)} repeats the variable _${name}_; variable names MUST be unique within a template`,
      );
    }
    seen.add(name);
  }
}

/** Check a molecule: a 32-byte bond digest and one or more valid fillers. */
export function validateMolecule(molecule: Molecule): void {
  checkReference(molecule.bond, "a molecule's bond");
  if (!Array.isArray(molecule.fillers)) {
    throw new EntityError("fillers", "a molecule's fillers MUST be a list");
  }
  if (molecule.fillers.length === 0) {
    throw new EntityError(
      "fillers",
      "a molecule MUST carry at least one filler (the CDDL reads [+ filler])",
    );
  }
  for (const [index, filler] of molecule.fillers.entries()) {
    try {
      validateFiller(filler);
    } catch (cause) {
      if (cause instanceof EntityError) {
        throw new EntityError(cause.code, `filler ${index}: ${cause.message}`, { cause });
      }
      throw cause;
    }
  }
}

/** Check a filler: a type tag of 0 to 4, carrying the one value shape that tag
 * permits. */
export function validateFiller(filler: Filler): void {
  switch (filler.type) {
    case 0:
    case 1:
    case 2:
      checkReference(filler.value, `a type ${filler.type} filler's value`);
      return;
    case 3:
      checkText(filler.value, "a type 3 filler's value", "filler-value");
      if (filler.value.length === 0) {
        throw new EntityError(
          "ipfs-uri",
          "a type 3 filler's IPFS content identifier MUST NOT be empty: an empty identifier addresses nothing",
        );
      }
      return;
    case 4:
      validateScalar(filler.value);
      return;
    default:
      throw new EntityError(
        "filler-type",
        `filler type ${String((filler as { type: unknown }).type)} is not one of 0 (atom), 1 (bond), 2 (molecule), 3 (IPFS URI) or 4 (scalar)`,
      );
  }
}

/** Check a scalar value: a quantity with an optional unit digest, or a
 * datetime range whose endpoints are canonical and ordered. */
export function validateScalar(scalar: ScalarValue): void {
  if (scalar === null || typeof scalar !== "object") {
    throw new EntityError("scalar", "a scalar filler's value MUST be a map");
  }
  const hasFrom = "from" in scalar;
  const hasTo = "to" in scalar;
  const hasValue = "value" in scalar;
  const hasUnit = "unit" in scalar && (scalar as Quantity).unit !== undefined;
  if (hasFrom || hasTo) {
    if (hasValue || hasUnit) {
      throw new EntityError(
        "scalar",
        "a scalar filler's value is either a quantity or a datetime range, never both",
      );
    }
    if (!hasFrom || !hasTo) {
      throw new EntityError(
        "scalar",
        `a datetime range carries both "from" and "to"`,
      );
    }
    const range = scalar as DatetimeRange;
    validateTimestamp(range.from, `the "from" endpoint`);
    validateTimestamp(range.to, `the "to" endpoint`);
    // Every timestamp is fixed-width, zero-padded, UTC and
    // most-significant-first, so the bytewise comparison of the two strings is
    // their chronological comparison; no date parsing is required.
    if (range.from > range.to) {
      throw new EntityError(
        "range",
        `datetime range is inverted: "from" (${range.from}) is later than "to" (${range.to})`,
      );
    }
    return;
  }
  if (!hasValue) {
    throw new EntityError(
      "scalar",
      `a scalar filler's value MUST be a quantity ("value", optionally "unit") or a datetime range ("from" and "to")`,
    );
  }
  const q = scalar as Quantity;
  if (typeof q.value !== "bigint" && !(q.value instanceof Decimal)) {
    throw new EntityError(
      "scalar",
      "a quantity's value MUST be an integer or a decimal fraction (CBOR tag 4)",
    );
  }
  if (q.unit !== undefined) checkReference(q.unit, "a scalar's unit");
}

/** Check any entity. */
export function validateEntity(entity: Entity): void {
  switch (entity.kind) {
    case "atom":
      validateAtom(entity);
      return;
    case "bond":
      validateBond(entity);
      return;
    case "molecule":
      validateMolecule(entity);
      return;
    default:
      throw new EntityError(
        "shape",
        `${String((entity as { kind: unknown }).kind)} is not an atom, a bond or a molecule`,
      );
  }
}

/** An internal reference is a raw 32-byte digest, never a CID
 * (spec/03-encoding.md, "Internal references"). */
function checkReference(value: unknown, what: string): void {
  if (!(value instanceof Uint8Array)) {
    throw new EntityError("digest", `${what} MUST be a byte string`);
  }
  if (value.length !== DIGEST_SIZE) {
    const hint =
      value.length === 36
        ? " — this is the length of a CID, and an internal reference is the raw digest, not the CID"
        : "";
    throw new EntityError(
      "digest",
      `${what} MUST be a ${DIGEST_SIZE}-byte digest, got ${value.length} bytes${hint}`,
    );
  }
}

/** A text field that is a string and can be encoded as UTF-8. */
function checkText(value: unknown, what: string, code: EntityErrorCode): void {
  if (typeof value !== "string") {
    throw new EntityError(code, `${what} MUST be a text string`);
  }
  for (let i = 0; i < value.length; i++) {
    const unit = value.charCodeAt(i);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const low = value.charCodeAt(i + 1);
      if (!(low >= 0xdc00 && low <= 0xdfff)) {
        throw new EntityError(code, `${what} has an unpaired high surrogate at index ${i}`);
      }
      i++;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      throw new EntityError(code, `${what} has an unpaired low surrogate at index ${i}`);
    }
  }
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

// ---------------------------------------------------------------------------
// Encoding
// ---------------------------------------------------------------------------

/** The dCBOR value of an atom: `{"description": ...}`. */
export function atomValue(atom: Atom): DcborValue {
  validateAtom(atom);
  return new Map<string, DcborValue>([["description", atom.description]]);
}

/** The dCBOR value of a bond: `{"template": ...}`. */
export function bondValue(bond: Bond): DcborValue {
  validateBond(bond);
  return new Map<string, DcborValue>([["template", bond.template]]);
}

/** The dCBOR value of a molecule: `{"bond": ..., "fillers": [...]}`. */
export function moleculeValue(molecule: Molecule): DcborValue {
  validateMolecule(molecule);
  return new Map<string, DcborValue>([
    ["bond", molecule.bond],
    ["fillers", molecule.fillers.map((filler) => fillerValue(filler))],
  ]);
}

/** The dCBOR value of a filler: `{"type": ..., "value": ...}`. */
export function fillerValue(filler: Filler): DcborValue {
  validateFiller(filler);
  const map = new Map<string, DcborValue>();
  map.set("type", BigInt(filler.type));
  map.set("value", filler.type === 4 ? scalarValue(filler.value) : filler.value);
  return map;
}

/** The dCBOR value of a scalar: a quantity map or a datetime range map. */
export function scalarValue(scalar: ScalarValue): DcborValue {
  validateScalar(scalar);
  const map = new Map<string, DcborValue>();
  if (isDatetimeRange(scalar)) {
    map.set("from", scalar.from);
    map.set("to", scalar.to);
    return map;
  }
  if (scalar.unit !== undefined) map.set("unit", scalar.unit);
  map.set("value", scalar.value);
  return map;
}

/** The dCBOR value of any entity. */
export function entityValue(entity: Entity): DcborValue {
  switch (entity.kind) {
    case "atom":
      return atomValue(entity);
    case "bond":
      return bondValue(entity);
    case "molecule":
      return moleculeValue(entity);
    default:
      validateEntity(entity);
      throw new EntityError("shape", "unreachable");
  }
}

/** The canonical encoding of an entity: dCBOR over its map. */
export function encodeEntity(entity: Entity): Uint8Array {
  return encode(entityValue(entity));
}

/** The canonical encoding of a filler. A filler is hashed as part of its
 * molecule and has no identifier of its own. */
export function encodeFiller(filler: Filler): Uint8Array {
  return encode(fillerValue(filler));
}

// ---------------------------------------------------------------------------
// Identifiers
// ---------------------------------------------------------------------------

/** `SHA-256(dCBOR(entity))` — the form every reference inside a Dialog
 * structure takes. */
export function entityDigest(entity: Entity): Uint8Array {
  return digest(encodeEntity(entity));
}

/** `0x01 0x71 0x12 0x20 || SHA-256(dCBOR(entity))` — the external identifier. */
export function entityCid(entity: Entity): Uint8Array {
  return cid(encodeEntity(entity));
}

/** The canonical text form of an entity's CID: `"b" || base32-lower-nopad`. */
export function entityCidText(entity: Entity): string {
  return cidToText(entityCid(entity));
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

/**
 * Decode an atom from its canonical bytes. The map carries exactly the key its
 * definition declares (spec/03-encoding.md, rule 8), and the description MUST
 * be a non-empty text string.
 */
export function decodeAtom(bytes: Uint8Array): Atom {
  return atomFromValue(decode(bytes));
}

/** Decode a bond from its canonical bytes. */
export function decodeBond(bytes: Uint8Array): Bond {
  return bondFromValue(decode(bytes));
}

/** Decode a molecule from its canonical bytes. */
export function decodeMolecule(bytes: Uint8Array): Molecule {
  return moleculeFromValue(decode(bytes));
}

/** Decode a filler from its canonical bytes. */
export function decodeFiller(bytes: Uint8Array): Filler {
  return fillerFromValue(decode(bytes));
}

/**
 * Decode an entity of any kind, recognized by the key its map carries:
 * `description` an atom, `template` a bond, `bond` a molecule.
 */
export function decodeEntity(bytes: Uint8Array): Entity {
  const value = decode(bytes);
  const map = expectMap(value, "an entity");
  if (map.has("description")) return atomFromValue(value);
  if (map.has("template")) return bondFromValue(value);
  if (map.has("bond")) return moleculeFromValue(value);
  throw new EntityError(
    "shape",
    `a map with the keys ${keyList(map)} is not an atom, a bond or a molecule`,
  );
}

/** Build an atom from an already-decoded dCBOR value. */
export function atomFromValue(value: DcborValue): Atom {
  const map = expectMap(value, "an atom");
  expectKeys(map, ["description"], [], "an atom");
  const description = map.get("description");
  if (typeof description !== "string") {
    throw new EntityError("description", "an atom's description MUST be a text string");
  }
  return newAtom(description);
}

/** Build a bond from an already-decoded dCBOR value. */
export function bondFromValue(value: DcborValue): Bond {
  const map = expectMap(value, "a bond");
  expectKeys(map, ["template"], [], "a bond");
  const template = map.get("template");
  if (typeof template !== "string") {
    throw new EntityError("template", "a bond's template MUST be a text string");
  }
  return newBond(template);
}

/** Build a molecule from an already-decoded dCBOR value. */
export function moleculeFromValue(value: DcborValue): Molecule {
  const map = expectMap(value, "a molecule");
  expectKeys(map, ["bond", "fillers"], [], "a molecule");
  const bond = map.get("bond");
  if (!(bond instanceof Uint8Array)) {
    throw new EntityError("digest", "a molecule's bond MUST be a byte string");
  }
  const fillers = map.get("fillers");
  if (!Array.isArray(fillers)) {
    throw new EntityError("fillers", "a molecule's fillers MUST be an array");
  }
  return newMolecule(
    bond,
    fillers.map((filler, index) => {
      try {
        return fillerFromValue(filler);
      } catch (cause) {
        if (cause instanceof EntityError) {
          throw new EntityError(cause.code, `filler ${index}: ${cause.message}`, { cause });
        }
        throw cause;
      }
    }),
  );
}

/** Build a filler from an already-decoded dCBOR value. */
export function fillerFromValue(value: DcborValue): Filler {
  const map = expectMap(value, "a filler");
  expectKeys(map, ["type", "value"], [], "a filler");
  const tag = map.get("type");
  if (typeof tag !== "bigint") {
    throw new EntityError("filler-type", "a filler's type MUST be an integer");
  }
  const raw = map.get("value") as DcborValue;
  switch (tag) {
    case 0n:
    case 1n:
    case 2n: {
      if (!(raw instanceof Uint8Array)) {
        throw new EntityError(
          "filler-value",
          `a type ${tag} filler's value MUST be a ${DIGEST_SIZE}-byte digest, not ${describe(raw)}`,
        );
      }
      const type = Number(tag) as 0 | 1 | 2;
      const filler: Filler = { type, value: raw };
      validateFiller(filler);
      return filler;
    }
    case 3n: {
      if (typeof raw !== "string") {
        throw new EntityError(
          "filler-value",
          `a type 3 filler's value MUST be a text string, not ${describe(raw)}`,
        );
      }
      return ipfsFiller(raw);
    }
    case 4n:
      return scalarFiller(scalarFromValue(raw));
    default:
      throw new EntityError(
        "filler-type",
        `filler type ${tag} is not one of 0 (atom), 1 (bond), 2 (molecule), 3 (IPFS URI) or 4 (scalar)`,
      );
  }
}

/** Build a scalar value from an already-decoded dCBOR value. */
export function scalarFromValue(value: DcborValue): ScalarValue {
  const map = expectMap(value, "a scalar filler's value");
  if (map.has("from") || map.has("to")) {
    expectKeys(map, ["from", "to"], [], "a datetime range");
    const from = map.get("from");
    const to = map.get("to");
    if (typeof from !== "string" || typeof to !== "string") {
      throw new EntityError("timestamp", "a datetime range's endpoints MUST be text strings");
    }
    return datetimeRange(from, to);
  }
  expectKeys(map, ["value"], ["unit"], "a scalar quantity");
  const number = map.get("value") as DcborValue;
  if (typeof number !== "bigint" && !(number instanceof Decimal)) {
    throw new EntityError(
      "scalar",
      `a quantity's value MUST be an integer or a decimal fraction, not ${describe(number)}`,
    );
  }
  const unit = map.get("unit");
  if (unit === undefined) return quantity(number);
  if (!(unit instanceof Uint8Array)) {
    throw new EntityError("digest", "a scalar's unit MUST be a byte string");
  }
  return quantity(number, unit);
}

function expectMap(value: DcborValue, what: string): Map<string, DcborValue> {
  if (!(value instanceof Map)) {
    throw new EntityError("shape", `${what} is a CBOR map, not ${describe(value)}`);
  }
  return value;
}

/**
 * The closed-map rule of spec/03-encoding.md (rule 8): a map carries exactly
 * the key set its definition declares — every required key, no key the
 * definition does not declare, and optional keys only where the CDDL marks
 * them with `?`.
 */
function expectKeys(
  map: Map<string, DcborValue>,
  required: readonly string[],
  optional: readonly string[],
  what: string,
): void {
  for (const key of required) {
    if (!map.has(key)) {
      throw new EntityError("shape", `${what} is missing the key ${JSON.stringify(key)}`);
    }
  }
  for (const key of map.keys()) {
    if (!required.includes(key) && !optional.includes(key)) {
      throw new EntityError(
        "shape",
        `${what} carries the key ${JSON.stringify(key)}, which its definition does not declare`,
      );
    }
  }
}

function keyList(map: Map<string, DcborValue>): string {
  return [...map.keys()].map((key) => JSON.stringify(key)).join(", ");
}

function describe(value: DcborValue): string {
  if (value === null) return "null";
  if (typeof value === "bigint" || typeof value === "number") return `the integer ${value}`;
  if (typeof value === "string") return "a text string";
  if (value instanceof Uint8Array) return `a ${value.length}-byte byte string`;
  if (value instanceof Decimal) return "a decimal fraction";
  if (Array.isArray(value)) return "an array";
  if (value instanceof Map) return "a map";
  return "a value outside the profile";
}

// ---------------------------------------------------------------------------
// The standard meta-bond library (spec/06-meta-bonds.md)
// ---------------------------------------------------------------------------

/** The name of one of the five standard meta-bonds, as `vectors/entities.json`
 * spells it. */
export type MetaBondName =
  | "equivalence"
  | "truth_assertion"
  | "truth_retraction"
  | "contradiction"
  | "supersession";

/**
 * A standard meta-bond: an ordinary bond whose molecules carry special
 * semantics during L2→L3 processing. An implementation recognizes a
 * meta-molecule by comparing the molecule's `bond` field — a 32-byte digest,
 * not a CID — against these digests.
 */
export interface MetaBond {
  readonly name: MetaBondName;
  /** The bond itself; its template is the identifier's whole input. */
  readonly bond: Bond;
  readonly template: string;
  /** The template's variables, in order. */
  readonly variables: readonly string[];
  /** `SHA-256(dCBOR({"template": ...}))`. */
  readonly digest: Uint8Array;
  /** The 36-byte external identifier. */
  readonly cid: Uint8Array;
  /** The canonical text form of the CID. */
  readonly cidText: string;
}

/** The templates of the five standard meta-bonds, verbatim. */
export const META_BOND_TEMPLATES = {
  equivalence: "_A_ is the same as _B_",
  truth_assertion: "_A_ is true",
  truth_retraction: "_A_ is untrue",
  contradiction: "_A_ contradicts _B_",
  supersession: "_A_ supersedes _B_",
} as const satisfies Record<MetaBondName, string>;

function makeMetaBond(name: MetaBondName): MetaBond {
  const bond = newBond(META_BOND_TEMPLATES[name]);
  const cidBytes = entityCid(bond);
  return {
    name,
    bond,
    template: bond.template,
    variables: Object.freeze(templateVariables(bond.template)),
    digest: entityDigest(bond),
    cid: cidBytes,
    cidText: cidToText(cidBytes),
  };
}

/** §1 — `_A_ is the same as _B_`: transitive equivalence between two entities
 * of the same kind. */
export const EQUIVALENCE: MetaBond = makeMetaBond("equivalence");
/** §2 — `_A_ is true`: the publishing author asserts a molecule. */
export const TRUTH_ASSERTION: MetaBond = makeMetaBond("truth_assertion");
/** §3 — `_A_ is untrue`: the publishing author denies a molecule. */
export const TRUTH_RETRACTION: MetaBond = makeMetaBond("truth_retraction");
/** §4 — `_A_ contradicts _B_`: two molecules cannot both be true. */
export const CONTRADICTION: MetaBond = makeMetaBond("contradiction");
/** §5 — `_A_ supersedes _B_`: molecule A replaces molecule B. */
export const SUPERSESSION: MetaBond = makeMetaBond("supersession");

/** The five standard meta-bonds, in the order spec/06-meta-bonds.md numbers
 * them. Implementations MUST support all five. */
export const STANDARD_META_BONDS: readonly MetaBond[] = Object.freeze([
  EQUIVALENCE,
  TRUTH_ASSERTION,
  TRUTH_RETRACTION,
  CONTRADICTION,
  SUPERSESSION,
]);

/** The standard meta-bond of that name. */
export function metaBond(name: MetaBondName): MetaBond {
  const found = STANDARD_META_BONDS.find((meta) => meta.name === name);
  if (found === undefined) {
    throw new EntityError("template", `no standard meta-bond is named ${JSON.stringify(name)}`);
  }
  return found;
}

/** The standard meta-bond a 32-byte bond digest names, or `undefined` if the
 * digest belongs to an ordinary bond. */
export function metaBondForDigest(bondDigest: Uint8Array): MetaBond | undefined {
  return STANDARD_META_BONDS.find((meta) => bytesEqual(meta.digest, bondDigest));
}

/**
 * The standard meta-bond a molecule is built from, or `undefined` for an
 * ordinary molecule. A meta-molecule is a regular molecule in every other
 * respect; only its bond digest marks it.
 */
export function metaBondOf(molecule: Molecule): MetaBond | undefined {
  return metaBondForDigest(molecule.bond);
}
