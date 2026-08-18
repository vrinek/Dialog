/**
 * Conformance: `vectors/entities.json` in full, plus the rules of
 * spec/01-data-model.md that no vector pins.
 *
 * Every case is built the way a foreign implementation builds it — from the
 * published JSON value model, through this implementation's constructors —
 * then encoded and compared byte for byte, hashed, turned into a CID and its
 * text form, and decoded back from the canonical bytes.
 *
 * The rejection tests below are hand-written, because `entities.json` carries
 * no `invalid` section (see todos/058). They follow the MUSTs of
 * spec/01-data-model.md one by one.
 */

import assert from "node:assert/strict";
import test from "node:test";

import { Decimal, type DcborValue, encode } from "../src/dcbor.ts";
import {
  CONTRADICTION,
  EQUIVALENCE,
  type Atom,
  type Bond,
  type Filler,
  type Molecule,
  type Quantity,
  type ScalarValue,
  EntityError,
  META_BOND_TEMPLATES,
  SUPERSESSION,
  STANDARD_META_BONDS,
  TRUTH_ASSERTION,
  TRUTH_RETRACTION,
  atomFiller,
  bondFiller,
  bondVariables,
  checkFillerCount,
  datetimeRange,
  decodeAtom,
  decodeBond,
  decodeEntity,
  decodeFiller,
  decodeMolecule,
  encodeEntity,
  encodeFiller,
  entityCid,
  entityCidText,
  entityDigest,
  ipfsFiller,
  isTimestamp,
  metaBond,
  metaBondForDigest,
  metaBondOf,
  moleculeFiller,
  newAtom,
  newBond,
  newMolecule,
  newMoleculeForBond,
  quantity,
  scalarFiller,
  templateVariables,
  validateTimestamp,
} from "../src/entity.ts";
import { bytesToHex, hexToBytes } from "../src/hex.ts";
import { buildValue, loadVectors, section, type ValueModel, type VectorCase } from "./vectors.ts";

const entities = loadVectors("entities.json");

/** Case counts as `vectors/README.md` records them. */
const EXPECTED_CASE_COUNTS: Record<string, number> = {
  atoms: 5,
  bonds: 2,
  meta_bonds: 5,
  molecules: 3,
  fillers: 11,
};

test("vectors/entities.json is the file this suite was written against", () => {
  assert.equal(entities.vectors, "dialog-conformance/1");
  assert.equal(entities.area, "entities");
  assert.deepEqual(
    entities.sections.map((s) => s.name),
    Object.keys(EXPECTED_CASE_COUNTS),
  );
  for (const [name, count] of Object.entries(EXPECTED_CASE_COUNTS)) {
    assert.equal(section(entities, name).cases.length, count, `case count of ${name}`);
  }
});

// ---------------------------------------------------------------------------
// Rebuilding the published value model, which is what a foreign implementation
// is given: a JSON structure and the bytes it must produce from it.
// ---------------------------------------------------------------------------

function entry(model: ValueModel, key: string): ValueModel | undefined {
  return model.entries?.find((e) => e.key === key)?.value;
}

function required(model: ValueModel, key: string): ValueModel {
  const found = entry(model, key);
  assert.ok(found !== undefined, `value model has the key ${JSON.stringify(key)}`);
  return found;
}

function moleculeFromModel(model: ValueModel): Molecule {
  assert.equal(model.type, "map");
  const bond = required(model, "bond");
  assert.equal(bond.type, "bytes");
  const fillers = required(model, "fillers");
  assert.equal(fillers.type, "array");
  return newMolecule(hexToBytes(bond.bytes ?? ""), (fillers.items ?? []).map(fillerFromModel));
}

function fillerFromModel(model: ValueModel): Filler {
  assert.equal(model.type, "map");
  const tag = required(model, "type");
  assert.equal(tag.type, "uint");
  const value = required(model, "value");
  switch (tag.number) {
    case "0":
      return atomFiller(hexToBytes(value.bytes ?? ""));
    case "1":
      return bondFiller(hexToBytes(value.bytes ?? ""));
    case "2":
      return moleculeFiller(hexToBytes(value.bytes ?? ""));
    case "3":
      return ipfsFiller(value.text ?? "");
    case "4":
      return scalarFiller(scalarFromModel(value));
    default:
      throw new Error(`unknown filler type ${tag.number}`);
  }
}

function scalarFromModel(model: ValueModel): ScalarValue {
  assert.equal(model.type, "map");
  const from = entry(model, "from");
  if (from !== undefined) {
    return datetimeRange(from.text ?? "", required(model, "to").text ?? "");
  }
  const number = required(model, "value");
  const value =
    number.type === "decimal"
      ? new Decimal(BigInt(number.exponent ?? "0"), BigInt(number.mantissa ?? "0"))
      : BigInt(number.number ?? "0");
  const unit = entry(model, "unit");
  return unit === undefined ? quantity(value) : quantity(value, hexToBytes(unit.bytes ?? ""));
}

/** The digest, CID and CID text form a case pins, checked against the entity. */
function checkIdentifiers(entity: Atom | Bond | Molecule, vector: VectorCase): void {
  assert.ok(vector.digest !== undefined && vector.cid !== undefined);
  assert.ok(vector.cid_text !== undefined);
  assert.equal(bytesToHex(entityDigest(entity)), vector.digest, "digest");
  assert.equal(bytesToHex(entityCid(entity)), vector.cid, "cid");
  assert.equal(entityCidText(entity), vector.cid_text, "cid_text");
}

// ---------------------------------------------------------------------------
// Atoms
// ---------------------------------------------------------------------------

test("vectors/entities.json: atoms", async (t) => {
  for (const vector of section(entities, "atoms").cases) {
    await t.test(vector.name, () => {
      assert.equal(vector.kind, "atom");
      assert.ok(vector.description !== undefined && vector.dcbor !== undefined);
      const atom = newAtom(vector.description);

      assert.equal(bytesToHex(encodeEntity(atom)), vector.dcbor, "encode");
      // The same bytes from the published value model, with no constructor in
      // the way: the two paths must not diverge.
      assert.equal(bytesToHex(encode(buildValue(vector.value!))), vector.dcbor, "value model");
      checkIdentifiers(atom, vector);

      const bytes = hexToBytes(vector.dcbor);
      assert.deepEqual(decodeAtom(bytes), atom, "decode");
      assert.deepEqual(decodeEntity(bytes), atom, "decodeEntity");
    });
  }
});

// ---------------------------------------------------------------------------
// Bonds and the standard meta-bonds
// ---------------------------------------------------------------------------

for (const name of ["bonds", "meta_bonds"]) {
  test(`vectors/entities.json: ${name}`, async (t) => {
    for (const vector of section(entities, name).cases) {
      await t.test(vector.name, () => {
        assert.equal(vector.kind, "bond");
        assert.ok(vector.template !== undefined && vector.dcbor !== undefined);
        const bond = newBond(vector.template);

        assert.equal(bytesToHex(encodeEntity(bond)), vector.dcbor, "encode");
        assert.equal(bytesToHex(encode(buildValue(vector.value!))), vector.dcbor, "value model");
        checkIdentifiers(bond, vector);
        // The variables the vector lists, in template order.
        assert.deepEqual(bondVariables(bond), vector.variables, "variables");

        const bytes = hexToBytes(vector.dcbor);
        assert.deepEqual(decodeBond(bytes), bond, "decode");
        assert.deepEqual(decodeEntity(bytes), bond, "decodeEntity");
      });
    }
  });
}

test("the five standard meta-bonds carry the pinned templates and digests", () => {
  const cases = section(entities, "meta_bonds").cases;
  assert.equal(cases.length, STANDARD_META_BONDS.length);
  // The vectors' order is the order spec/06-meta-bonds.md numbers them.
  for (const [index, vector] of cases.entries()) {
    const meta = STANDARD_META_BONDS[index]!;
    assert.equal(meta.name, vector.name, "name");
    assert.equal(meta.template, vector.template, "template");
    assert.deepEqual([...meta.variables], vector.variables, "variables");
    assert.equal(bytesToHex(meta.digest), vector.digest, "digest");
    assert.equal(bytesToHex(meta.cid), vector.cid, "cid");
    assert.equal(meta.cidText, vector.cid_text, "cid_text");
    // Recognition works from the digest alone, which is all a molecule carries.
    assert.equal(metaBondForDigest(hexToBytes(vector.digest!)), meta);
    assert.equal(metaBond(meta.name), meta);
    assert.equal(META_BOND_TEMPLATES[meta.name], vector.template);
  }
  assert.deepEqual(
    STANDARD_META_BONDS,
    [EQUIVALENCE, TRUTH_ASSERTION, TRUTH_RETRACTION, CONTRADICTION, SUPERSESSION],
  );
  assert.equal(EQUIVALENCE.template, "_A_ is the same as _B_");
  assert.equal(TRUTH_ASSERTION.template, "_A_ is true");
  assert.equal(TRUTH_RETRACTION.template, "_A_ is untrue");
  assert.equal(CONTRADICTION.template, "_A_ contradicts _B_");
  assert.equal(SUPERSESSION.template, "_A_ supersedes _B_");
  // An ordinary bond is not a meta-bond.
  assert.equal(metaBondForDigest(entityDigest(newBond("_A_ is the capital of _B_"))), undefined);
});

// ---------------------------------------------------------------------------
// Molecules
// ---------------------------------------------------------------------------

test("vectors/entities.json: molecules", async (t) => {
  for (const vector of section(entities, "molecules").cases) {
    await t.test(vector.name, () => {
      assert.equal(vector.kind, "molecule");
      assert.ok(vector.dcbor !== undefined);
      const molecule = moleculeFromModel(vector.value!);

      assert.equal(bytesToHex(encodeEntity(molecule)), vector.dcbor, "encode");
      assert.equal(bytesToHex(encode(buildValue(vector.value!))), vector.dcbor, "value model");
      checkIdentifiers(molecule, vector);

      const bytes = hexToBytes(vector.dcbor);
      assert.deepEqual(decodeMolecule(bytes), molecule, "decode");
      assert.deepEqual(decodeEntity(bytes), molecule, "decodeEntity");
    });
  }
});

test("the meta-molecule is recognized by its bond digest alone", () => {
  const cases = section(entities, "molecules").cases;
  const byName = (name: string): Molecule =>
    moleculeFromModel(cases.find((c) => c.name === name)!.value!);
  assert.equal(metaBondOf(byName("paris_equivalence")), EQUIVALENCE);
  assert.equal(metaBondOf(byName("paris_is_the_capital_of_france")), undefined);
  assert.equal(metaBondOf(byName("eiffel_tower_is_330_metres_tall")), undefined);
});

test("the molecules of the vectors rebuild from their atoms and bonds", () => {
  // Nothing but the description and template strings goes in; every digest is
  // computed. This is the whole chain of spec/01-data-model.md's examples.
  const paris = newAtom("Paris, the capital of France");
  const france = newAtom("France");
  const parisFrance = newAtom("Paris, France");
  const eiffel = newAtom("The Eiffel Tower");
  const metre = newAtom("metre");
  const capitalOf = newBond("_A_ is the capital of _B_");
  const height = newBond("_A_ is _B_ tall");

  const capital = newMoleculeForBond(capitalOf, [
    atomFiller(entityDigest(paris)),
    atomFiller(entityDigest(france)),
  ]);
  const tall = newMoleculeForBond(height, [
    atomFiller(entityDigest(eiffel)),
    scalarFiller(quantity(330n, entityDigest(metre))),
  ]);
  const equivalence = newMoleculeForBond(EQUIVALENCE.bond, [
    atomFiller(entityDigest(paris)),
    atomFiller(entityDigest(parisFrance)),
  ]);

  const digestOf = (name: string): string =>
    section(entities, "molecules").cases.find((c) => c.name === name)!.digest!;
  assert.equal(bytesToHex(entityDigest(capital)), digestOf("paris_is_the_capital_of_france"));
  assert.equal(bytesToHex(entityDigest(tall)), digestOf("eiffel_tower_is_330_metres_tall"));
  assert.equal(bytesToHex(entityDigest(equivalence)), digestOf("paris_equivalence"));

  // And the molecule digest a type 2 filler carries is the one the fillers
  // section pins.
  const fillers = section(entities, "fillers").cases;
  assert.equal(
    bytesToHex(encodeFiller(moleculeFiller(entityDigest(tall)))),
    fillers.find((c) => c.name === "molecule_reference")!.dcbor,
  );
});

// ---------------------------------------------------------------------------
// Fillers
// ---------------------------------------------------------------------------

test("vectors/entities.json: fillers", async (t) => {
  for (const vector of section(entities, "fillers").cases) {
    await t.test(vector.name, () => {
      assert.ok(vector.dcbor !== undefined && vector.type !== undefined);
      const filler = fillerFromModel(vector.value!);
      assert.equal(filler.type, vector.type, "type tag");

      assert.equal(bytesToHex(encodeFiller(filler)), vector.dcbor, "encode");
      assert.equal(bytesToHex(encode(buildValue(vector.value!))), vector.dcbor, "value model");
      assert.deepEqual(decodeFiller(hexToBytes(vector.dcbor)), filler, "decode");
    });
  }
});

// ---------------------------------------------------------------------------
// The template-variable grammar (spec/01-data-model.md, "Bonds")
// ---------------------------------------------------------------------------

test("the disambiguation table of spec/01-data-model.md", () => {
  assert.deepEqual(templateVariables("_AB_"), ["AB"]);
  assert.deepEqual(templateVariables("_A_B_"), ["A"]);
  assert.deepEqual(templateVariables("_A__B_"), ["A", "B"]);
  assert.deepEqual(templateVariables("type_of"), []);
  assert.deepEqual(templateVariables("_a_"), []);
});

test("leftmost-longest matching of the variable grammar", () => {
  assert.deepEqual(templateVariables("_A_ is the capital of _B_"), ["A", "B"]);
  assert.deepEqual(templateVariables("_A_ is _B_ tall"), ["A", "B"]);
  // The longest run of uppercase letters wins.
  assert.deepEqual(templateVariables("_ABC_ likes _D_"), ["ABC", "D"]);
  // A leading underscore that opens nothing is literal text.
  assert.deepEqual(templateVariables("__A_"), ["A"]);
  // An unterminated opener is literal text.
  assert.deepEqual(templateVariables("_ABC"), []);
  assert.deepEqual(templateVariables("_A_B"), ["A"]);
  // Digits, lowercase letters and spaces are not variable names.
  assert.deepEqual(templateVariables("_A1_"), []);
  assert.deepEqual(templateVariables("_Ab_"), []);
  assert.deepEqual(templateVariables("_ _"), []);
  assert.deepEqual(templateVariables("__"), []);
  assert.deepEqual(templateVariables("_"), []);
  assert.deepEqual(templateVariables(""), []);
  // Non-ASCII uppercase is not UCALPHA (%x41-5A).
  assert.deepEqual(templateVariables("_Α_"), []); // U+0391 GREEK CAPITAL ALPHA
  // Three variables in a row, sharing their underscores.
  assert.deepEqual(templateVariables("_A__B__C_"), ["A", "B", "C"]);
});

test("a bond template MUST contain at least one variable and repeat none", () => {
  for (const template of ["", "hello", "type_of", "_a_", "_A", "A_", "_1_", "_ _"]) {
    assert.throws(
      () => newBond(template),
      (e: unknown) => e instanceof EntityError && e.code === "template",
      `template ${JSON.stringify(template)}`,
    );
  }
  for (const template of ["_A__A_", "_A_ equals _A__A_", "_A__B__A_"]) {
    assert.throws(
      () => newBond(template),
      (e: unknown) =>
        e instanceof EntityError && e.code === "template" && /repeats the variable/.test(e.message),
      `template ${JSON.stringify(template)}`,
    );
  }
  // The near-misses are fine: different names, however similar.
  assert.deepEqual(bondVariables(newBond("_A__AB_")), ["A", "AB"]);
});

// ---------------------------------------------------------------------------
// The timestamp profile (spec/01-data-model.md, "Datetime ranges")
// ---------------------------------------------------------------------------

test("the canonical timestamp profile accepts only its one spelling", () => {
  for (const good of [
    "2024-02-20T15:30:00Z",
    "0000-01-01T00:00:00Z",
    "9999-12-31T23:59:59Z",
    "2024-02-29T00:00:00Z", // a leap day
    "2000-02-29T00:00:00Z", // divisible by 400
    "1600-02-29T00:00:00Z", // before the Gregorian reform, and still a leap year
    "0000-02-29T00:00:00Z", // year zero is divisible by 400
  ]) {
    assert.ok(isTimestamp(good), good);
    validateTimestamp(good);
  }

  const bad: Array<[string, string]> = [
    ["2024-02-20t15:30:00Z", "lowercase date-time separator"],
    ["2024-02-20T15:30:00z", "lowercase offset designator"],
    ["2024-02-20T15:30:00+00:00", "numeric offset"],
    ["2024-02-20T15:30:00-00:00", "negative zero offset"],
    ["2024-02-20T15:30:00", "no offset at all"],
    ["2024-02-20T15:30:00.000Z", "fractional seconds"],
    ["2024-02-20T15:30:00.0Z", "a single fractional digit"],
    ["2024-02-20T23:59:60Z", "the leap second"],
    ["2024-02-20T24:00:00Z", "hour 24"],
    ["2024-02-20T15:60:00Z", "minute 60"],
    ["2024-13-01T00:00:00Z", "month 13"],
    ["2024-00-01T00:00:00Z", "month 00"],
    ["2024-01-00T00:00:00Z", "day 00"],
    ["2024-01-32T00:00:00Z", "day 32"],
    ["2024-02-20 15:30:00Z", "a space instead of T"],
    ["2024-2-20T15:30:00Z", "an unpadded month"],
    ["24-02-20T15:30:00Z", "a two-digit year"],
    ["+2024-02-20T15:30:00Z", "an expanded year"],
    ["20240220T153000Z", "the basic ISO 8601 format"],
    ["2024-02-20", "a plain date — Dialog has none"],
    [" 2024-02-20T15:30:00Z", "leading whitespace"],
    ["2024-02-20T15:30:00Z ", "trailing whitespace"],
    ["2024-02-20T15:30:00Z\n", "a trailing newline"],
    ["", "the empty string"],
  ];
  for (const [timestamp, why] of bad) {
    assert.equal(isTimestamp(timestamp), false, why);
    assert.throws(
      () => validateTimestamp(timestamp),
      (e: unknown) => e instanceof EntityError && e.code === "timestamp",
      why,
    );
  }
});

test("the calendar rule is the proleptic Gregorian one", () => {
  const notADate = [
    "1500-02-29T00:00:00Z", // a leap day in the Julian calendar then in use
    "1900-02-29T00:00:00Z", // divisible by 100, not by 400
    "2023-02-29T00:00:00Z",
    "2100-02-29T00:00:00Z",
    "2024-02-30T00:00:00Z",
    "2024-04-31T00:00:00Z",
    "2024-06-31T00:00:00Z",
    "2024-09-31T00:00:00Z",
    "2024-11-31T00:00:00Z",
  ];
  for (const timestamp of notADate) {
    assert.throws(
      () => validateTimestamp(timestamp),
      (e: unknown) =>
        e instanceof EntityError && e.code === "timestamp" && /not a real date/.test(e.message),
      timestamp,
    );
  }
  // Every 31-day month accepts its 31st, and February accepts the 28th always.
  for (const month of ["01", "03", "05", "07", "08", "10", "12"]) {
    assert.ok(isTimestamp(`2023-${month}-31T00:00:00Z`), month);
  }
  for (const month of ["04", "06", "09", "11"]) {
    assert.ok(isTimestamp(`2023-${month}-30T00:00:00Z`), month);
    assert.equal(isTimestamp(`2023-${month}-31T00:00:00Z`), false, month);
  }
  assert.ok(isTimestamp("1900-02-28T00:00:00Z"));
});

test("a datetime range is ordered, and may be an instant", () => {
  const instant = datetimeRange("2024-02-20T15:30:00Z", "2024-02-20T15:30:00Z");
  assert.deepEqual(instant, { from: "2024-02-20T15:30:00Z", to: "2024-02-20T15:30:00Z" });
  datetimeRange("2024-02-20T15:30:00Z", "2024-02-21T15:30:00Z");
  // A whole day, the way spec/01-data-model.md writes "Thursday, Feb 20, 2026".
  datetimeRange("2026-02-20T00:00:00Z", "2026-02-20T23:59:59Z");

  assert.throws(
    () => datetimeRange("2024-02-21T15:30:00Z", "2024-02-20T15:30:00Z"),
    (e: unknown) => e instanceof EntityError && e.code === "range",
  );
  // One second of inversion is still inversion, and the comparison is the
  // bytewise one the specification prescribes.
  assert.throws(
    () => datetimeRange("2024-02-20T15:30:01Z", "2024-02-20T15:30:00Z"),
    (e: unknown) => e instanceof EntityError && e.code === "range",
  );
  assert.throws(
    () => datetimeRange("2024-02-20T15:30:00", "2024-02-20T15:30:00Z"),
    (e: unknown) => e instanceof EntityError && e.code === "timestamp",
  );
});

// ---------------------------------------------------------------------------
// Rejections: the MUSTs of spec/01-data-model.md, by hand
// ---------------------------------------------------------------------------

/** dCBOR bytes of a map, so that a malformed entity can be handed to a
 * decoder. The encoder itself has no schema, so anything can be built. */
function bytesOf(entries: Array<[string, DcborValue]>): Uint8Array {
  return encode(new Map(entries));
}

function rejects(bytes: Uint8Array, code: string, what: string, decoder = decodeEntity): void {
  assert.throws(
    () => decoder(bytes),
    (e: unknown) => {
      assert.ok(e instanceof EntityError, `${what}: ${String(e)}`);
      assert.equal(e.code, code, `${what}: ${e.message}`);
      return true;
    },
    what,
  );
}

const DIGEST_A = new Uint8Array(32).fill(0xa1);
const DIGEST_B = new Uint8Array(32).fill(0xb2);

test("an atom's description MUST be a non-empty text string", () => {
  assert.throws(
    () => newAtom(""),
    (e: unknown) => e instanceof EntityError && e.code === "description",
  );
  rejects(bytesOf([["description", ""]]), "description", "empty description", decodeAtom);
  rejects(bytesOf([["description", 1n]]), "description", "integer description", decodeAtom);
  rejects(bytesOf([["description", null]]), "description", "null description", decodeAtom);
});

test("entity maps are closed: exactly the keys their definition declares", () => {
  rejects(bytesOf([["description", "France"], ["extra", 1n]]), "shape", "atom with an extra key");
  rejects(bytesOf([]), "shape", "an empty map is no entity");
  rejects(bytesOf([["desc", "France"]]), "shape", "a misspelled key");
  rejects(bytesOf([["template", "_A_ x"], ["description", "France"]]), "shape", "two entities at once", decodeBond);
  rejects(bytesOf([["bond", DIGEST_A]]), "shape", "a molecule with no fillers key");
  rejects(
    bytesOf([["bond", DIGEST_A], ["fillers", [fillerMap(0n, DIGEST_B)]], ["ts", 1n]]),
    "shape",
    "a molecule with an extra key",
  );
  rejects(encode("France"), "shape", "a bare text string is no entity");
  rejects(encode([1n]), "shape", "an array is no entity");
});

/** A filler map, built without the constructors so that invalid ones exist. */
function fillerMap(type: bigint, value: DcborValue): DcborValue {
  return new Map<string, DcborValue>([
    ["type", type],
    ["value", value],
  ]);
}

function moleculeBytes(bond: DcborValue, fillers: DcborValue[]): Uint8Array {
  return encode(
    new Map<string, DcborValue>([
      ["bond", bond],
      ["fillers", fillers],
    ]),
  );
}

test("a molecule's bond is a raw 32-byte digest and its fillers are non-empty", () => {
  rejects(moleculeBytes(DIGEST_A, []), "fillers", "an empty filler list", decodeMolecule);
  rejects(moleculeBytes(DIGEST_A, [] as DcborValue[]), "fillers", "[+ filler] means one or more");
  rejects(
    moleculeBytes(new Uint8Array(31), [fillerMap(0n, DIGEST_B)]),
    "digest",
    "a 31-byte bond reference",
  );
  rejects(
    moleculeBytes(hexToBytes("01711220" + "a1".repeat(32)), [fillerMap(0n, DIGEST_B)]),
    "digest",
    "a CID where a digest belongs",
  );
  rejects(
    moleculeBytes("not a digest", [fillerMap(0n, DIGEST_B)]),
    "digest",
    "a text bond reference",
  );
  rejects(
    encode(new Map<string, DcborValue>([["bond", DIGEST_A], ["fillers", DIGEST_B]])),
    "fillers",
    "fillers that are not a list",
  );
  assert.throws(
    () => newMolecule(new Uint8Array(31), [atomFiller(DIGEST_A)]),
    (e: unknown) => e instanceof EntityError && e.code === "digest",
  );
  assert.throws(
    () => newMolecule(DIGEST_A, []),
    (e: unknown) => e instanceof EntityError && e.code === "fillers",
  );
});

test("a filler's value MUST match the shape its type tag names", () => {
  // Type tags outside 0..4.
  for (const tag of [5n, 6n, 255n, -1n]) {
    rejects(
      moleculeBytes(DIGEST_A, [fillerMap(tag, DIGEST_B)]),
      "filler-type",
      `filler type ${tag}`,
    );
  }
  rejects(
    moleculeBytes(DIGEST_A, [new Map<string, DcborValue>([["type", "0"], ["value", DIGEST_B]])]),
    "filler-type",
    "a text type tag",
  );
  // Types 0, 1, 2: a 32-byte digest and nothing else.
  for (const tag of [0n, 1n, 2n]) {
    rejects(
      moleculeBytes(DIGEST_A, [fillerMap(tag, new Uint8Array(31))]),
      "digest",
      `type ${tag} with a 31-byte value`,
    );
    rejects(
      moleculeBytes(DIGEST_A, [fillerMap(tag, "bafyrei…")]),
      "filler-value",
      `type ${tag} with a text value`,
    );
    rejects(
      moleculeBytes(DIGEST_A, [fillerMap(tag, 1n)]),
      "filler-value",
      `type ${tag} with an integer value`,
    );
  }
  // Type 3: a non-empty text string.
  rejects(moleculeBytes(DIGEST_A, [fillerMap(3n, "")]), "ipfs-uri", "an empty IPFS URI");
  rejects(moleculeBytes(DIGEST_A, [fillerMap(3n, DIGEST_B)]), "filler-value", "type 3 with bytes");
  // Type 4: a scalar value map.
  rejects(moleculeBytes(DIGEST_A, [fillerMap(4n, DIGEST_B)]), "shape", "type 4 with bytes");
  rejects(moleculeBytes(DIGEST_A, [fillerMap(4n, 330n)]), "shape", "type 4 with a bare integer");
  // A filler map is closed too.
  rejects(
    moleculeBytes(DIGEST_A, [
      new Map<string, DcborValue>([["type", 0n], ["unit", DIGEST_B], ["value", DIGEST_B]]),
    ]),
    "shape",
    "a filler with an extra key",
  );
  rejects(
    moleculeBytes(DIGEST_A, [new Map<string, DcborValue>([["type", 0n]])]),
    "shape",
    "a filler with no value",
  );
  rejects(moleculeBytes(DIGEST_A, [DIGEST_B]), "shape", "a filler that is not a map");
  // The constructors refuse the same things.
  assert.throws(
    () => ipfsFiller(""),
    (e: unknown) => e instanceof EntityError && e.code === "ipfs-uri",
  );
  assert.throws(
    () => atomFiller(new Uint8Array(36)),
    (e: unknown) => e instanceof EntityError && e.code === "digest",
  );
});

test("a scalar filler's value is a quantity or a datetime range, never both", () => {
  const scalar = (entries: Array<[string, DcborValue]>): Uint8Array =>
    moleculeBytes(DIGEST_A, [fillerMap(4n, new Map(entries))]);
  const NOW = "2024-02-20T15:30:00Z";
  const LATER = "2024-02-21T15:30:00Z";

  // Valid shapes, for contrast.
  decodeMolecule(scalar([["value", 330n]]));
  decodeMolecule(scalar([["unit", DIGEST_B], ["value", 330n]]));
  decodeMolecule(scalar([["value", new Decimal(-2n, 314n)]]));
  decodeMolecule(scalar([["from", NOW], ["to", LATER]]));

  rejects(scalar([]), "shape", "an empty scalar map");
  rejects(scalar([["unit", DIGEST_B]]), "shape", "a unit with no value");
  rejects(scalar([["from", NOW]]), "shape", "a range with no end");
  rejects(scalar([["to", LATER]]), "shape", "a range with no start");
  rejects(scalar([["unit", DIGEST_B], ["from", NOW], ["to", LATER]]), "shape", "a range with a unit");
  rejects(scalar([["value", 1n], ["from", NOW], ["to", LATER]]), "shape", "both shapes at once");
  rejects(scalar([["scale", 1n], ["value", 1n]]), "shape", "an undeclared scalar key");
  rejects(scalar([["value", "330"]]), "scalar", "a text quantity");
  rejects(scalar([["value", null]]), "scalar", "a null quantity");
  rejects(scalar([["value", [1n]]]), "scalar", "an array quantity");
  rejects(scalar([["unit", "metre"], ["value", 1n]]), "digest", "a text unit");
  rejects(scalar([["unit", new Uint8Array(31)], ["value", 1n]]), "digest", "a 31-byte unit");
  rejects(scalar([["from", NOW], ["to", "2024-02-21T15:30:00"]]), "timestamp", "an unzoned end");
  rejects(scalar([["from", "1500-02-29T00:00:00Z"], ["to", LATER]]), "timestamp", "a Julian leap day");
  rejects(scalar([["from", LATER], ["to", NOW]]), "range", "an inverted range");
  rejects(scalar([["from", 1n], ["to", 2n]]), "timestamp", "integer endpoints");
});

test("the filler count MUST equal the bond's variable count", () => {
  const bond = newBond("_A_ is the capital of _B_");
  const one = [atomFiller(DIGEST_A)];
  const two = [atomFiller(DIGEST_A), atomFiller(DIGEST_B)];
  const three = [...two, atomFiller(DIGEST_A)];

  newMoleculeForBond(bond, two);
  for (const fillers of [one, three]) {
    assert.throws(
      () => newMoleculeForBond(bond, fillers),
      (e: unknown) => e instanceof EntityError && e.code === "filler-count",
      `${fillers.length} fillers`,
    );
  }
  // The check is available separately, for the layer that has resolved the
  // bond (spec/02-block-format.md, "Validation" rule 5).
  const molecule = newMolecule(entityDigest(bond), two);
  checkFillerCount(molecule, bond);
  assert.throws(
    () => checkFillerCount(molecule, newBond("_A_ is true")),
    (e: unknown) => e instanceof EntityError && e.code === "digest",
    "a molecule is only checked against the bond it names",
  );
  // A one-variable bond takes exactly one filler.
  const truth = newMoleculeForBond(TRUTH_ASSERTION.bond, [moleculeFiller(DIGEST_A)]);
  assert.equal(metaBondOf(truth), TRUTH_ASSERTION);
  assert.throws(
    () => newMoleculeForBond(TRUTH_ASSERTION.bond, two),
    (e: unknown) => e instanceof EntityError && e.code === "filler-count",
  );
});

test("the encoders refuse an invalid entity assembled by hand", () => {
  // The interfaces are structural, so an object can be built without the
  // constructors; encoding one is where it is caught.
  assert.throws(
    () => encodeEntity({ kind: "atom", description: "" } as Atom),
    (e: unknown) => e instanceof EntityError && e.code === "description",
  );
  assert.throws(
    () => encodeEntity({ kind: "bond", template: "no variables" } as Bond),
    (e: unknown) => e instanceof EntityError && e.code === "template",
  );
  assert.throws(
    () => encodeEntity({ kind: "molecule", bond: DIGEST_A, fillers: [] } as Molecule),
    (e: unknown) => e instanceof EntityError && e.code === "fillers",
  );
  assert.throws(
    () => encodeFiller({ type: 3, value: "" } as Filler),
    (e: unknown) => e instanceof EntityError && e.code === "ipfs-uri",
  );
  assert.throws(
    () => encodeFiller({ type: 4, value: { value: 1.5 } as unknown as Quantity } as Filler),
    (e: unknown) => e instanceof EntityError && e.code === "scalar",
  );
  assert.throws(
    () => encodeEntity({ kind: "wormhole" } as unknown as Atom),
    (e: unknown) => e instanceof EntityError && e.code === "shape",
  );
});

test("a whole number is a plain integer and 3.14 is a canonical decimal", () => {
  // The canonicalization rules live in the dCBOR layer; what matters here is
  // that a scalar cannot smuggle a non-canonical decimal into an entity.
  assert.throws(() => quantity(new Decimal(0n, 3n)), /exponent 0 is not negative/);
  assert.throws(() => quantity(new Decimal(-2n, 3140n)), /divisible by 10/);
  assert.equal(
    bytesToHex(encodeFiller(scalarFiller(quantity(new Decimal(-2n, 314n))))),
    "a26474797065046576616c7565a16576616c7565c4822119013a",
  );
  assert.equal(
    bytesToHex(encodeFiller(scalarFiller(quantity(330)))),
    "a26474797065046576616c7565a16576616c756519014a",
  );
  assert.throws(
    () => quantity(3.14),
    (e: unknown) => e instanceof EntityError && e.code === "filler-value",
  );
});
