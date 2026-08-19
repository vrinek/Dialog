/**
 * Blocks: the unit of data at Layer 1 — a signed, append-only container of
 * ontology operations.
 *
 * Implements `spec/02-block-format.md` in full — the three block types, the
 * four operations, chain linking and the ten numbered validation rules — with
 * the signing procedure of `spec/04-cryptography.md`, "Block signing", and the
 * demand-driven resolution procedure of `spec/05-processing-model.md`,
 * "Foreign chain loading".
 *
 * Two properties shape the code. A block's identity is the hash of its complete
 * encoding, signature included (spec/02, "Block identification"), so anything
 * that reaches the encoder must already be valid: a lenient constructor mints a
 * digest no other implementation will produce. And a block's validity is
 * defined inductively from the genesis block forward (spec/02, "Validation"),
 * so validation is a question asked of a *store*: {@link validateBlock} takes a
 * {@link BlockSource} and looks its predecessor up among the blocks the node has
 * already accepted, rather than re-deriving an ancestor's validity.
 *
 * Private blocks are handled structurally here — `enc` and `nonce` are opaque
 * bytes with a size floor. Decryption, and with it the rules that only a holder
 * of the content key can check (4, 5, 6 and 10), belongs to `./privacy.ts`.
 *
 * Browser-safe: Ed25519 comes from @noble/curves, SHA-256 from @noble/hashes;
 * no `node:` imports and no Node-only globals.
 */

import { ed25519 } from "@noble/curves/ed25519";

import { DIGEST_SIZE, cid, cidToText, digest } from "./cid.ts";
import { type DcborValue, decode, encode } from "./dcbor.ts";
import {
  EntityError,
  type Entity,
  type Filler,
  entityDigest,
  fillerFromValue,
  fillerValue,
  newAtom,
  newBond,
  newMolecule,
  templateVariables,
} from "./entity.ts";
import { bytesToHex } from "./hex.ts";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** The protocol version this implementation speaks. A block carrying any other
 * value is rejected by validation rule 1. */
export const PROTOCOL_VERSION = 1n;

/** Size of an Ed25519 public key, and of the `new_pub` a rotation names. */
export const PUBLIC_KEY_SIZE = 32;
/** Size of an Ed25519 signature. */
export const SIGNATURE_SIZE = 64;
/** Size of an Ed25519 private key in its seed form (spec/04, "Key encoding"). */
export const SEED_SIZE = 32;
/** Size of the XChaCha20 nonce a private block carries. */
export const NONCE_SIZE = 24;
/**
 * The smallest `enc` a private block may carry: the 16-byte Poly1305 tag every
 * XChaCha20-Poly1305 ciphertext ends with (spec/02, "Private block"). Anything
 * shorter cannot be the output of the AEAD, so the block is structurally
 * invalid and is rejected without attempting decryption.
 */
export const MIN_ENC_SIZE = 16;

/**
 * The domain separator prepended to a block's signing bytes
 * (spec/04-cryptography.md, "Signing procedure"): 15 bytes, UTF-8.
 */
export const SIGNING_DOMAIN_SEPARATOR = "dialog-v1-block";

const DOMAIN_SEPARATOR_BYTES = new TextEncoder().encode(SIGNING_DOMAIN_SEPARATOR);

/**
 * The default bound on the number of foreign blocks a single validation scans
 * while resolving references: the value spec/05-processing-model.md, "Scan
 * limit", asks every implementation to default to, so that the same block gets
 * the same verdict from every default-configured node.
 *
 * One unit is one *distinct foreign block scanned* — fetched through the refs
 * graph and read for the definitions its operations carry. A block the graph
 * names twice costs one unit; an ancestor reached through `prev` costs none;
 * a `refs` entry the store does not hold, or one fetched only to check rules 6
 * and 10 against it, costs none either. The limit is configurable per
 * validation ({@link ValidateOptions.scanLimit}) and per store
 * ({@link BlockStoreOptions.scanLimit}); a lower one accepts a subset of what
 * this one accepts.
 */
export const DEFAULT_SCAN_LIMIT = 256;

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/**
 * The class of block rule an error reports. The names follow the numbered
 * validation rules of spec/02-block-format.md, so a caller (and the conformance
 * suite) can tell one rejection from another.
 */
export type BlockErrorCode =
  /** rule 1: the `v` field is not a recognized protocol version. */
  | "version"
  /** "Validation dispatch": the `type` field, or a field set that belongs to
   * another block type — a public block carrying `enc`, a private block
   * carrying plaintext `ops`, a `rotate_key` outside a rotation block. */
  | "type"
  /** a field's value is not the type or the size its definition gives: a `prev`
   * that is not a 32-byte digest, a 12-byte nonce, an `enc` below the tag
   * size. */
  | "field"
  /** rule 8: the closed-map rule — an undeclared key, or a missing declared
   * one — and any other departure from canonical dCBOR. */
  | "encoding"
  /** rule 7: the `ops` list is empty. */
  | "empty-ops"
  /** the rotation-block constraints of spec/02, "Rotation block": exactly one
   * `rotate_key` operation, a non-null `prev`, a `new_pub` that differs. */
  | "rotation"
  /** rule 5: an operation violates the data model of spec/01, or a digest it
   * carries resolves to an entity of another kind. */
  | "data-model"
  /** rule 2: the signature does not verify under the block's `pub`. */
  | "signature"
  /** rule 3: chain integrity — a `pub` that differs from the chain's, a block
   * appended after the chain's rotation block. */
  | "chain"
  /** neither valid nor invalid: a block validating this one requires is one
   * this node cannot read — its predecessor (rule 3), or a block reference
   * resolution must read to decide rule 4, whether it is not held or is held as
   * ciphertext with no key for it (spec/05, "Block reception", "Undecryptable
   * reference handling"). The block may be kept as **stored but unvalidated**
   * and re-checked when the missing block, or the missing key, arrives. Neither
   * a block nor a key this node lacks is evidence that the block needing it is
   * invalid. */
  | "unvalidated"
  /** rule 4: an entity digest an operation carries is not reachable. */
  | "reachability"
  /** the scan limit was reached before every digest resolved; the block is
   * invalid for unresolvable references (spec/05, "Scan limit"). */
  | "scan-limit"
  /** rule 6: a public block's `refs` name a private block. */
  | "reference-visibility"
  /** rule 10: a `refs` list that repeats a digest or names a block of the
   * author's own chain. */
  | "reference-hygiene"
  /** rule 9: two blocks of one chain claim the same predecessor. */
  | "fork"
  /** the succession rules of spec/02, "rotate_key": a successor genesis block
   * that is not public, or an ambiguous succession. */
  | "succession";

/** Options for a {@link BlockError}: the standard `cause`, plus what an
 * `unvalidated` verdict is waiting for — a block, or a key. */
export interface BlockErrorOptions extends ErrorOptions {
  /** The block whose absence left the verdict undecided. Only an `unvalidated`
   * error carries one, and it is what a store keys its held blocks by so that
   * they can be re-validated when it arrives. */
  readonly awaiting?: Uint8Array;
  /** The block resolution needed, held, and could not read: what is wanted is
   * a decryption key rather than a block, so no arrival will settle the verdict
   * and a store has nothing to file the held block under (spec/05,
   * "Undecryptable reference handling"). */
  readonly undecryptable?: Uint8Array;
}

/** A rejection by a block constructor, decoder or validator. */
export class BlockError extends Error {
  readonly code: BlockErrorCode;
  /** For an `unvalidated` verdict, the digest of the block that has not
   * arrived; `undefined` for every other code. */
  readonly awaiting?: Uint8Array;
  /** For an `unvalidated` verdict reached because a block resolution needed is
   * held as ciphertext this node has no key for, that block's digest. The
   * situation is the application's to act on — the fix is to obtain the key —
   * which is why it is surfaced rather than folded into the message. */
  readonly undecryptable?: Uint8Array;

  constructor(code: BlockErrorCode, message: string, options?: BlockErrorOptions) {
    super(message, options);
    this.name = "BlockError";
    this.code = code;
    if (options?.awaiting !== undefined) this.awaiting = options.awaiting;
    if (options?.undecryptable !== undefined) this.undecryptable = options.undecryptable;
  }
}

/** Report an entity-layer rejection as the data-model rule it is (rule 5). */
function asBlockError(cause: unknown, what: string): never {
  if (cause instanceof EntityError) {
    throw new BlockError("data-model", `${what}: ${cause.message}`, { cause });
  }
  throw cause;
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

/** `{"op": "create_atom", "description": tstr}`. */
export interface CreateAtomOperation {
  readonly op: "create_atom";
  readonly description: string;
}

/** `{"op": "create_bond", "template": tstr}`. */
export interface CreateBondOperation {
  readonly op: "create_bond";
  readonly template: string;
}

/** `{"op": "create_molecule", "bond": bstr .size 32, "fillers": [+ filler]}`. */
export interface CreateMoleculeOperation {
  readonly op: "create_molecule";
  readonly bond: Uint8Array;
  readonly fillers: readonly Filler[];
}

/** `{"op": "rotate_key", "new_pub": bstr .size 32}` — the fourth operation,
 * which may appear only in a rotation block. */
export interface RotateKeyOperation {
  readonly op: "rotate_key";
  readonly newPub: Uint8Array;
}

/** The three operations that create an entity, and so the three a public or
 * private block may carry. */
export type EntityOperation =
  | CreateAtomOperation
  | CreateBondOperation
  | CreateMoleculeOperation;

/** Any of the four operations. */
export type Operation = EntityOperation | RotateKeyOperation;

/** A `create_atom` operation. The description is validated as an atom's. */
export function createAtom(description: string): CreateAtomOperation {
  try {
    newAtom(description);
  } catch (cause) {
    asBlockError(cause, "create_atom");
  }
  return { op: "create_atom", description };
}

/** A `create_bond` operation. The template is validated as a bond's. */
export function createBond(template: string): CreateBondOperation {
  try {
    newBond(template);
  } catch (cause) {
    asBlockError(cause, "create_bond");
  }
  return { op: "create_bond", template };
}

/**
 * A `create_molecule` operation, naming its bond by digest.
 *
 * The filler *count* is not checked here and cannot be: the template that would
 * say how many variables there are is not in hand. That check belongs where the
 * specification puts it — validation rule 5, once the bond digest has been
 * resolved — and is made by {@link validateBlock}.
 */
export function createMolecule(
  bond: Uint8Array,
  fillers: readonly Filler[],
): CreateMoleculeOperation {
  try {
    newMolecule(bond, fillers);
  } catch (cause) {
    asBlockError(cause, "create_molecule");
  }
  return { op: "create_molecule", bond, fillers: [...fillers] };
}

/**
 * A `rotate_key` operation. `newPub` MUST be a 32-byte Ed25519 public key; that
 * it differs from the rotating key's own `pub` is a relation between the
 * operation and its block, and is checked by the rotation block's constructor.
 */
export function rotateKey(newPub: Uint8Array): RotateKeyOperation {
  checkBytes(newPub, PUBLIC_KEY_SIZE, "a rotate_key operation's new_pub");
  return { op: "rotate_key", newPub: newPub.slice() };
}

/** Whether an operation creates an entity, as opposed to rotating a key. */
export function isEntityOperation(op: Operation): op is EntityOperation {
  return op.op !== "rotate_key";
}

/**
 * The entity an operation creates. `rotate_key` creates none
 * (spec/05, "Accumulation rules"), and is rejected here.
 */
export function operationEntity(op: EntityOperation): Entity {
  switch (op.op) {
    case "create_atom":
      return newAtom(op.description);
    case "create_bond":
      return newBond(op.template);
    case "create_molecule":
      return newMolecule(op.bond, op.fillers);
    default:
      throw new BlockError(
        "type",
        `${String((op as { op: unknown }).op)} creates no entity`,
      );
  }
}

/**
 * The identifier of the entity an operation creates: `SHA-256(dCBOR(entity))`,
 * exactly as spec/02 spells it out for each of the three operations.
 */
export function operationDigest(op: EntityOperation): Uint8Array {
  return entityDigest(operationEntity(op));
}

/** The dCBOR value of an operation. */
export function operationValue(op: Operation): DcborValue {
  const map = new Map<string, DcborValue>();
  map.set("op", op.op);
  switch (op.op) {
    case "create_atom":
      map.set("description", op.description);
      return map;
    case "create_bond":
      map.set("template", op.template);
      return map;
    case "create_molecule":
      map.set("bond", op.bond);
      map.set(
        "fillers",
        op.fillers.map((filler) => fillerValue(filler)),
      );
      return map;
    case "rotate_key":
      map.set("new_pub", op.newPub);
      return map;
    default:
      throw new BlockError(
        "encoding",
        `${String((op as { op: unknown }).op)} is not one of the four operation types`,
      );
  }
}

/** Check an operation against its definition, including the data-model rules
 * of spec/01 that its fields are subject to. */
export function validateOperation(op: Operation): void {
  switch (op.op) {
    case "create_atom":
      try {
        newAtom(op.description);
      } catch (cause) {
        asBlockError(cause, "create_atom");
      }
      return;
    case "create_bond":
      try {
        newBond(op.template);
      } catch (cause) {
        asBlockError(cause, "create_bond");
      }
      return;
    case "create_molecule":
      try {
        newMolecule(op.bond, op.fillers);
      } catch (cause) {
        asBlockError(cause, "create_molecule");
      }
      return;
    case "rotate_key":
      checkBytes(op.newPub, PUBLIC_KEY_SIZE, "a rotate_key operation's new_pub");
      return;
    default:
      throw new BlockError(
        "encoding",
        `${JSON.stringify(String((op as { op: unknown }).op))} is not one of the four operation types`,
      );
  }
}

// ---------------------------------------------------------------------------
// Blocks
// ---------------------------------------------------------------------------

/** One of the three block types. */
export type BlockType = "public" | "private" | "rotation";

/** `public-block` — the ordinary block: plaintext operations, in the clear for
 * every node. */
export interface PublicBlock {
  readonly type: "public";
  readonly v: bigint;
  readonly pub: Uint8Array;
  readonly sig: Uint8Array;
  readonly prev: Uint8Array | null;
  readonly refs: readonly Uint8Array[];
  readonly ts: bigint;
  readonly ops: readonly EntityOperation[];
}

/** `rotation-block` — the last block of a key's chain. Its `prev` is never
 * null: a rotation block is never a genesis block. */
export interface RotationBlock {
  readonly type: "rotation";
  readonly v: bigint;
  readonly pub: Uint8Array;
  readonly sig: Uint8Array;
  readonly prev: Uint8Array;
  readonly refs: readonly Uint8Array[];
  readonly ts: bigint;
  readonly ops: readonly [RotateKeyOperation];
}

/** `private-block` — `refs`, `ts` and `ops` encrypted together into `enc`;
 * only the chain management fields stay in the clear. */
export interface PrivateBlock {
  readonly type: "private";
  readonly v: bigint;
  readonly pub: Uint8Array;
  readonly sig: Uint8Array;
  readonly prev: Uint8Array | null;
  readonly enc: Uint8Array;
  readonly nonce: Uint8Array;
}

/** Any block. */
export type Block = PublicBlock | PrivateBlock | RotationBlock;

/** A block without its signature — what the signing input is built from
 * (spec/04, "Signature input"). */
export type UnsignedBlock =
  | Omit<PublicBlock, "sig">
  | Omit<PrivateBlock, "sig">
  | Omit<RotationBlock, "sig">;

/** A public or rotation block: the two types whose `refs`, `ts` and `ops` are
 * in the clear. */
export type PlaintextBlock = PublicBlock | RotationBlock;

/** Whether a block's `refs`, `ts` and `ops` are readable without a key. */
export function isPlaintextBlock(block: Block | UnsignedBlock): block is PlaintextBlock {
  return block.type === "public" || block.type === "rotation";
}

/** The fields a public block is built from. `v` defaults to the protocol
 * version and `refs` to the empty list. */
export interface PublicBlockFields {
  readonly pub: Uint8Array;
  readonly prev: Uint8Array | null;
  readonly refs?: readonly Uint8Array[];
  readonly ts: bigint | number;
  readonly ops: readonly EntityOperation[];
  readonly v?: bigint | number;
}

/** The fields a rotation block is built from. Its single operation is given as
 * the new public key. */
export interface RotationBlockFields {
  readonly pub: Uint8Array;
  readonly prev: Uint8Array;
  readonly refs?: readonly Uint8Array[];
  readonly ts: bigint | number;
  readonly newPub: Uint8Array;
  readonly v?: bigint | number;
}

/** The fields a private block is built from. `enc` and `nonce` are opaque
 * here; producing them is `./privacy.ts`'s business. */
export interface PrivateBlockFields {
  readonly pub: Uint8Array;
  readonly prev: Uint8Array | null;
  readonly enc: Uint8Array;
  readonly nonce: Uint8Array;
  readonly v?: bigint | number;
}

/** An unsigned public block, ready for {@link signBlock}. */
export function unsignedPublicBlock(fields: PublicBlockFields): Omit<PublicBlock, "sig"> {
  const block: Omit<PublicBlock, "sig"> = {
    type: "public",
    v: toVersion(fields.v),
    pub: fields.pub,
    prev: fields.prev,
    refs: fields.refs === undefined ? [] : [...fields.refs],
    ts: toTimestamp(fields.ts),
    ops: [...fields.ops],
  };
  validateBlockStructure(block);
  return block;
}

/** An unsigned rotation block, ready for {@link signBlock}. */
export function unsignedRotationBlock(fields: RotationBlockFields): Omit<RotationBlock, "sig"> {
  const block: Omit<RotationBlock, "sig"> = {
    type: "rotation",
    v: toVersion(fields.v),
    pub: fields.pub,
    prev: fields.prev,
    refs: fields.refs === undefined ? [] : [...fields.refs],
    ts: toTimestamp(fields.ts),
    ops: [rotateKey(fields.newPub)],
  };
  validateBlockStructure(block);
  return block;
}

/** An unsigned private block, ready for {@link signBlock}. */
export function unsignedPrivateBlock(fields: PrivateBlockFields): Omit<PrivateBlock, "sig"> {
  const block: Omit<PrivateBlock, "sig"> = {
    type: "private",
    v: toVersion(fields.v),
    pub: fields.pub,
    prev: fields.prev,
    enc: fields.enc,
    nonce: fields.nonce,
  };
  validateBlockStructure(block);
  return block;
}

/** A complete public block from fields and a signature. */
export function newPublicBlock(
  fields: PublicBlockFields & { readonly sig: Uint8Array },
): PublicBlock {
  const block: PublicBlock = { ...unsignedPublicBlock(fields), sig: fields.sig };
  validateBlockStructure(block);
  return block;
}

/** A complete rotation block from fields and a signature. */
export function newRotationBlock(
  fields: RotationBlockFields & { readonly sig: Uint8Array },
): RotationBlock {
  const block: RotationBlock = { ...unsignedRotationBlock(fields), sig: fields.sig };
  validateBlockStructure(block);
  return block;
}

/** A complete private block from fields and a signature. */
export function newPrivateBlock(
  fields: PrivateBlockFields & { readonly sig: Uint8Array },
): PrivateBlock {
  const block: PrivateBlock = { ...unsignedPrivateBlock(fields), sig: fields.sig };
  validateBlockStructure(block);
  return block;
}

function toVersion(v: bigint | number | undefined): bigint {
  if (v === undefined) return PROTOCOL_VERSION;
  const value = typeof v === "number" ? BigInt(v) : v;
  return value;
}

function toTimestamp(ts: bigint | number): bigint {
  if (typeof ts === "number") {
    if (!Number.isSafeInteger(ts)) {
      throw new BlockError("field", `${ts} is not a safe integer; pass a bigint`);
    }
    return BigInt(ts);
  }
  return ts;
}

// ---------------------------------------------------------------------------
// Structural validation
// ---------------------------------------------------------------------------

/**
 * Every check a block can be given on its own, in the order spec/02 numbers
 * them: the version (rule 1), the type and the field set its definition
 * declares ("Validation dispatch", rule 8), the size of every fixed-width
 * field, the non-empty `ops` list (rule 7), the structural half of rule 10 —
 * no duplicate `refs` — the data-model rules each operation is subject to
 * (rule 5), and the rotation-block constraints.
 *
 * The rules a block cannot answer alone — the signature (rule 2), chain
 * integrity (rule 3), reachability (rule 4), the resolved half of rules 5, 6
 * and 10, and fork detection (rule 9) — are {@link validateBlock}'s.
 */
export function validateBlockStructure(block: Block | UnsignedBlock): void {
  // Rule 1: version check.
  if (typeof block.v !== "bigint") {
    throw new BlockError("field", "the v field MUST be an unsigned integer");
  }
  if (block.v !== PROTOCOL_VERSION) {
    throw new BlockError(
      "version",
      `protocol version ${block.v} is not recognized; this implementation speaks version ${PROTOCOL_VERSION}`,
    );
  }

  // "Validation dispatch": the type field selects the structure.
  if (
    block.type !== "public" &&
    block.type !== "private" &&
    block.type !== "rotation"
  ) {
    throw new BlockError(
      "type",
      `block type ${JSON.stringify(String((block as { type: unknown }).type))} is not one of "public", "private" or "rotation"`,
    );
  }

  checkBytes(block.pub, PUBLIC_KEY_SIZE, "a block's pub");
  if ("sig" in block) {
    checkBytes(block.sig, SIGNATURE_SIZE, "a block's sig");
  }
  if (block.prev !== null) {
    checkBytes(block.prev, DIGEST_SIZE, "a block's prev");
  }

  if (block.type === "private") {
    if (!(block.enc instanceof Uint8Array)) {
      throw new BlockError("field", "a private block's enc MUST be a byte string");
    }
    if (block.enc.length < MIN_ENC_SIZE) {
      throw new BlockError(
        "field",
        `a private block's enc is ${block.enc.length} bytes; anything below ${MIN_ENC_SIZE}, the size of the Poly1305 tag, cannot be the output of the AEAD`,
      );
    }
    checkBytes(block.nonce, NONCE_SIZE, "a private block's nonce");
    return;
  }

  // Public and rotation blocks: refs, ts, ops in the clear.
  if (!Array.isArray(block.refs)) {
    throw new BlockError("field", "a block's refs MUST be a list");
  }
  const seen = new Set<string>();
  for (const [index, ref] of block.refs.entries()) {
    checkBytes(ref, DIGEST_SIZE, `refs[${index}]`);
    const hex = bytesToHex(ref);
    // Rule 10, the structural half: a digest MUST NOT appear twice. The check
    // needs no other block, so it is made when the block is decoded.
    if (seen.has(hex)) {
      throw new BlockError(
        "reference-hygiene",
        `refs names ${hex} more than once; a repeated entry denotes the same dependency twice and changes nothing but the block's bytes`,
      );
    }
    seen.add(hex);
  }
  if (typeof block.ts !== "bigint") {
    throw new BlockError("field", "a block's ts MUST be an unsigned integer");
  }
  if (block.ts < 0n) {
    throw new BlockError("field", "a block's ts MUST be an unsigned integer");
  }

  if (!Array.isArray(block.ops)) {
    throw new BlockError("field", "a block's ops MUST be a list");
  }
  // Rule 7: non-empty operations.
  if (block.ops.length === 0) {
    throw new BlockError(
      "empty-ops",
      "a block MUST contain at least one operation (the CDDL reads [+ operation])",
    );
  }
  for (const [index, op] of block.ops.entries()) {
    try {
      validateOperation(op);
    } catch (cause) {
      if (cause instanceof BlockError) {
        throw new BlockError(cause.code, `ops[${index}]: ${cause.message}`, { cause });
      }
      throw cause;
    }
  }

  if (block.type === "public") {
    for (const [index, op] of block.ops.entries()) {
      if (op.op === "rotate_key") {
        throw new BlockError(
          "type",
          `ops[${index}] is a rotate_key operation, which may appear only in a rotation block: a chain ends where the type field says it ends`,
        );
      }
    }
    return;
  }

  // Rotation block: exactly one rotate_key operation, a non-null prev, and a
  // new key that differs from the one signing it.
  if (block.ops.length !== 1 || block.ops[0]?.op !== "rotate_key") {
    throw new BlockError(
      "rotation",
      `a rotation block MUST contain exactly one rotate_key operation and no other operation (found ${block.ops.length})`,
    );
  }
  if (block.prev === null) {
    throw new BlockError(
      "rotation",
      "a rotation block's prev MUST NOT be null: a rotation block ends the chain it sits at the end of, and ending presupposes a chain to end",
    );
  }
  if (bytesEqual(block.ops[0].newPub, block.pub)) {
    throw new BlockError(
      "rotation",
      "new_pub equals the rotation block's own pub: a chain would end in favour of itself, which no node can act on",
    );
  }
}

function checkBytes(value: unknown, size: number, what: string): void {
  if (!(value instanceof Uint8Array)) {
    throw new BlockError("field", `${what} MUST be a byte string`);
  }
  if (value.length !== size) {
    const hint =
      size === DIGEST_SIZE && value.length === 36
        ? " — this is the length of a CID, and an internal reference is the raw digest, not the CID"
        : "";
    throw new BlockError(
      "field",
      `${what} MUST be ${size} bytes, got ${value.length}${hint}`,
    );
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

/**
 * The dCBOR value of a block, signature included — the encoding a block's
 * digest and CID are computed over (spec/02, "Block identification").
 */
export function blockValue(block: Block): DcborValue {
  validateBlockStructure(block);
  const map = unsignedMap(block);
  map.set("sig", block.sig);
  return map;
}

/**
 * The dCBOR value of a block *without* its signature: the signing input's
 * fields, as spec/04, "Signature input" writes them out. The same function
 * serves a signed block, since the signature is simply left out.
 */
export function unsignedBlockValue(block: Block | UnsignedBlock): DcborValue {
  validateBlockStructure(block);
  return unsignedMap(block);
}

function unsignedMap(block: Block | UnsignedBlock): Map<string, DcborValue> {
  const map = new Map<string, DcborValue>();
  map.set("v", block.v);
  map.set("type", block.type);
  map.set("pub", block.pub);
  map.set("prev", block.prev);
  if (block.type === "private") {
    map.set("enc", block.enc);
    map.set("nonce", block.nonce);
    return map;
  }
  map.set("refs", [...block.refs]);
  map.set("ts", block.ts);
  map.set(
    "ops",
    block.ops.map((op) => operationValue(op)),
  );
  return map;
}

/** The canonical encoding of a block: dCBOR over its map, signature included. */
export function encodeBlock(block: Block): Uint8Array {
  return encode(blockValue(block));
}

/** `signing_bytes = dCBOR(block without "sig" field)`. */
export function signingBytes(block: Block | UnsignedBlock): Uint8Array {
  return encode(unsignedBlockValue(block));
}

/** `signing_input = "dialog-v1-block" || signing_bytes`. */
export function signingInput(block: Block | UnsignedBlock): Uint8Array {
  const bytes = signingBytes(block);
  const out = new Uint8Array(DOMAIN_SEPARATOR_BYTES.length + bytes.length);
  out.set(DOMAIN_SEPARATOR_BYTES, 0);
  out.set(bytes, DOMAIN_SEPARATOR_BYTES.length);
  return out;
}

/** `SHA-256(dCBOR(block))` — the form `prev` and `refs` name a block by. */
export function blockDigest(block: Block): Uint8Array {
  return digest(encodeBlock(block));
}

/** `01 71 12 20 || SHA-256(dCBOR(block))` — the block's external identifier. */
export function blockCid(block: Block): Uint8Array {
  return cid(encodeBlock(block));
}

/** The canonical text form of a block's CID. */
export function blockCidText(block: Block): string {
  return cidToText(blockCid(block));
}

// ---------------------------------------------------------------------------
// Signing (spec/04-cryptography.md, "Block signing")
// ---------------------------------------------------------------------------

/**
 * The Ed25519 public key of a 32-byte seed. An Ed25519 private key is a seed,
 * not a scalar (spec/04, "Ed25519-to-X25519 conversion"); the 64-byte form many
 * libraries use is the seed followed by this key, and {@link seedOf} takes it
 * apart.
 */
export function publicKeyFromSeed(seed: Uint8Array): Uint8Array {
  return ed25519.getPublicKey(seedOf(seed));
}

/**
 * The 32-byte seed of an Ed25519 private key, accepting either the seed itself
 * or the 64-byte seed-and-public-key form.
 */
export function seedOf(privateKey: Uint8Array): Uint8Array {
  if (!(privateKey instanceof Uint8Array)) {
    throw new BlockError("field", "an Ed25519 private key MUST be a byte string");
  }
  if (privateKey.length === SEED_SIZE) return privateKey;
  if (privateKey.length === SEED_SIZE + PUBLIC_KEY_SIZE) {
    return privateKey.subarray(0, SEED_SIZE);
  }
  throw new BlockError(
    "field",
    `an Ed25519 private key is ${SEED_SIZE} bytes (the seed) or ${SEED_SIZE + PUBLIC_KEY_SIZE} (seed and public key), got ${privateKey.length}`,
  );
}

/**
 * Sign a block: `Ed25519_Sign(private_key, "dialog-v1-block" || dCBOR(block
 * without "sig"))`.
 *
 * The signing key MUST be the one the block's `pub` field names — a block
 * signed by another key is rejected by validation rule 2, and producing one is
 * never what a caller means.
 */
export function signBlock(block: UnsignedBlock, privateKey: Uint8Array): Block {
  const seed = seedOf(privateKey);
  const derived = ed25519.getPublicKey(seed);
  if (!bytesEqual(derived, block.pub)) {
    throw new BlockError(
      "signature",
      "the private key given does not match the block's pub field",
    );
  }
  const sig = ed25519.sign(signingInput(block), seed);
  const signed = { ...block, sig } as Block;
  validateBlockStructure(signed);
  return signed;
}

/**
 * Verify a block's signature (rule 2): rebuild the signing input from the
 * block's own fields and check `sig` against `pub`.
 */
export function verifyBlockSignature(block: Block): boolean {
  try {
    return ed25519.verify(block.sig, signingInput(block), block.pub);
  } catch {
    // A malformed key or signature encoding is a failed verification, not a
    // crash: the bytes came off the wire.
    return false;
  }
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

const PUBLIC_KEYS = ["v", "type", "pub", "sig", "prev", "refs", "ts", "ops"];
const PRIVATE_KEYS = ["v", "type", "pub", "sig", "prev", "enc", "nonce"];

/**
 * Decode a block from its canonical bytes.
 *
 * The dCBOR decoder rejects anything non-canonical (rule 8's encoding half),
 * and this function adds the closed-map rule and the structural validation
 * above, so a block that comes back from here is one whose bytes re-encode
 * identically and whose structure spec/02 admits.
 */
export function decodeBlock(bytes: Uint8Array): Block {
  return blockFromValue(decode(bytes));
}

/** Build a block from an already-decoded dCBOR value. */
export function blockFromValue(value: DcborValue): Block {
  const map = expectMap(value, "a block");

  // Rule 1 comes first, as spec/02 numbers it: a field a later version
  // introduces arrives in a block whose v this version does not recognize.
  const v = map.get("v");
  if (typeof v !== "bigint") {
    throw new BlockError("field", "the v field MUST be an unsigned integer");
  }
  if (v !== PROTOCOL_VERSION) {
    throw new BlockError(
      "version",
      `protocol version ${v} is not recognized; this implementation speaks version ${PROTOCOL_VERSION}`,
    );
  }

  const type = map.get("type");
  if (typeof type !== "string") {
    throw new BlockError("type", "the type field MUST be a text string");
  }
  if (type !== "public" && type !== "private" && type !== "rotation") {
    throw new BlockError(
      "type",
      `block type ${JSON.stringify(type)} is not one of "public", "private" or "rotation"`,
    );
  }

  // "Validation dispatch": a field belonging to another type is a dispatch
  // failure, not merely an undeclared key — it says which structure the author
  // meant and contradicts the type field.
  if (type === "private") {
    for (const key of ["ops", "refs", "ts"]) {
      if (map.has(key)) {
        throw new BlockError(
          "type",
          `a private block carries no plaintext ${JSON.stringify(key)}: refs, ts and ops live inside enc`,
        );
      }
    }
  } else {
    for (const key of ["enc", "nonce"]) {
      if (map.has(key)) {
        throw new BlockError(
          "type",
          `a ${type} block carries no ${JSON.stringify(key)} field, which belongs to a private block`,
        );
      }
    }
  }

  expectKeys(map, type === "private" ? PRIVATE_KEYS : PUBLIC_KEYS, `a ${type} block`);

  const pub = expectBytes(map.get("pub"), "pub");
  const sig = expectBytes(map.get("sig"), "sig");
  const prevValue = map.get("prev") as DcborValue;
  const prev = prevValue === null ? null : expectBytes(prevValue, "prev");

  if (type === "private") {
    return newPrivateBlock({
      v,
      pub,
      sig,
      prev,
      enc: expectBytes(map.get("enc"), "enc"),
      nonce: expectBytes(map.get("nonce"), "nonce"),
    });
  }

  const refsValue = map.get("refs") as DcborValue;
  if (!Array.isArray(refsValue)) {
    throw new BlockError("field", "a block's refs MUST be an array");
  }
  const refs = refsValue.map((ref, index) => expectBytes(ref, `refs[${index}]`));

  const ts = map.get("ts");
  if (typeof ts !== "bigint") {
    throw new BlockError("field", "a block's ts MUST be an unsigned integer");
  }

  const opsValue = map.get("ops") as DcborValue;
  if (!Array.isArray(opsValue)) {
    throw new BlockError("field", "a block's ops MUST be an array");
  }
  const ops = opsValue.map((op, index) => {
    try {
      return operationFromValue(op);
    } catch (cause) {
      if (cause instanceof BlockError) {
        throw new BlockError(cause.code, `ops[${index}]: ${cause.message}`, { cause });
      }
      throw cause;
    }
  });

  if (type === "rotation") {
    if (ops.length !== 1 || ops[0]?.op !== "rotate_key") {
      throw new BlockError(
        "rotation",
        `a rotation block MUST contain exactly one rotate_key operation and no other operation (found ${ops.length})`,
      );
    }
    if (prev === null) {
      throw new BlockError(
        "rotation",
        "a rotation block's prev MUST NOT be null: a rotation block ends the chain it sits at the end of, and ending presupposes a chain to end",
      );
    }
    return newRotationBlock({
      v,
      pub,
      sig,
      prev,
      refs,
      ts,
      newPub: ops[0].newPub,
    });
  }

  const entityOps: EntityOperation[] = [];
  for (const [index, op] of ops.entries()) {
    if (op.op === "rotate_key") {
      throw new BlockError(
        "type",
        `ops[${index}] is a rotate_key operation, which may appear only in a rotation block: a chain ends where the type field says it ends`,
      );
    }
    entityOps.push(op);
  }
  return newPublicBlock({ v, pub, sig, prev, refs, ts, ops: entityOps });
}

const OPERATION_KEYS: Record<string, readonly string[]> = {
  create_atom: ["op", "description"],
  create_bond: ["op", "template"],
  create_molecule: ["op", "bond", "fillers"],
  rotate_key: ["op", "new_pub"],
};

/** Build an operation from an already-decoded dCBOR value. */
export function operationFromValue(value: DcborValue): Operation {
  const map = expectMap(value, "an operation");
  const op = map.get("op");
  if (typeof op !== "string") {
    throw new BlockError("encoding", "an operation's op field MUST be a text string");
  }
  const keys = OPERATION_KEYS[op];
  if (keys === undefined) {
    throw new BlockError(
      "encoding",
      `${JSON.stringify(op)} is not one of the four operation types; an operation map carries exactly the keys the definition for its op declares`,
    );
  }
  expectKeys(map, keys, `a ${op} operation`);

  switch (op) {
    case "create_atom": {
      const description = map.get("description");
      if (typeof description !== "string") {
        throw new BlockError(
          "data-model",
          "a create_atom operation's description MUST be a text string",
        );
      }
      return createAtom(description);
    }
    case "create_bond": {
      const template = map.get("template");
      if (typeof template !== "string") {
        throw new BlockError(
          "data-model",
          "a create_bond operation's template MUST be a text string",
        );
      }
      return createBond(template);
    }
    case "create_molecule": {
      const bond = map.get("bond");
      if (!(bond instanceof Uint8Array)) {
        throw new BlockError(
          "data-model",
          "a create_molecule operation's bond MUST be a byte string",
        );
      }
      const fillers = map.get("fillers");
      if (!Array.isArray(fillers)) {
        throw new BlockError(
          "data-model",
          "a create_molecule operation's fillers MUST be an array",
        );
      }
      const built = fillers.map((filler, index) => {
        try {
          return fillerFromValue(filler);
        } catch (cause) {
          if (cause instanceof EntityError) {
            throw new BlockError("data-model", `filler ${index}: ${cause.message}`, { cause });
          }
          throw cause;
        }
      });
      return createMolecule(bond, built);
    }
    default: {
      const newPub = map.get("new_pub");
      if (!(newPub instanceof Uint8Array)) {
        throw new BlockError(
          "field",
          "a rotate_key operation's new_pub MUST be a byte string",
        );
      }
      return rotateKey(newPub);
    }
  }
}

function expectMap(value: DcborValue, what: string): Map<string, DcborValue> {
  if (!(value instanceof Map)) {
    throw new BlockError("encoding", `${what} is a CBOR map`);
  }
  return value;
}

function expectBytes(value: DcborValue | undefined, what: string): Uint8Array {
  if (!(value instanceof Uint8Array)) {
    throw new BlockError("field", `the ${what} field MUST be a byte string`);
  }
  return value;
}

/**
 * The closed-map rule (spec/03, rule 8; spec/02, "Validation dispatch"): a block
 * map carries exactly the keys the definition for its `type` declares, and an
 * operation map exactly the keys the definition for its `op` declares. An
 * undeclared key is a rejection, never something to ignore — a decoder that
 * ignored it would hash bytes it did not account for.
 */
function expectKeys(
  map: Map<string, DcborValue>,
  declared: readonly string[],
  what: string,
): void {
  for (const key of declared) {
    if (!map.has(key)) {
      throw new BlockError(
        "encoding",
        `${what} is missing the key ${JSON.stringify(key)}, which its definition declares`,
      );
    }
  }
  for (const key of map.keys()) {
    if (!declared.includes(key)) {
      throw new BlockError(
        "encoding",
        `${what} carries the key ${JSON.stringify(key)}, which its definition does not declare`,
      );
    }
  }
}

// ---------------------------------------------------------------------------
// The block source: what a node holds
// ---------------------------------------------------------------------------

/** A block a node holds, with the bytes it arrived as and whether the node has
 * accepted it as valid. */
export interface StoredBlock {
  readonly digest: Uint8Array;
  readonly bytes: Uint8Array;
  readonly block: Block;
  /**
   * False for a **stored but unvalidated** block: one a block validating it
   * requires is one the node cannot read, so the node has not decided.
   *
   * Two things read it, and one deliberately does not. Rule 3 reads it: such a
   * block MUST NOT be treated as another block's predecessor. A node's L2 reads
   * it: only a valid block's operations enter the ontology graph (spec/05,
   * "Accumulation rules"). Reference resolution does not — a definition is read
   * from any block the store holds and can read, whatever its verdict (spec/05,
   * "Resolution procedure", "Resolution reads blocks, not verdicts", and
   * {@link Resolver}).
   */
  readonly valid: boolean;
}

/**
 * What validation asks of a node's storage.
 *
 * {@link get} is the whole of the required interface — rule 3 is a lookup among
 * accepted blocks, and reference resolution is a walk over digests. The two
 * optional methods carry the rules that are questions about a *chain* rather
 * than about a block: without {@link siblings} a validator cannot detect a fork
 * (rule 9), and without {@link rotationOf} it cannot know that a key's chain
 * has already ended. A source that omits them has those rules reported as
 * unchecked in the {@link ValidationReport} rather than silently passed.
 */
export interface BlockSource {
  /** The block of that digest, or `undefined` if the node does not hold it. */
  get(digest: Uint8Array): StoredBlock | undefined;
  /** Held blocks by this author naming this predecessor (`null` for the
   * genesis position). */
  siblings?(pub: Uint8Array, prev: Uint8Array | null): readonly StoredBlock[];
  /** The rotation block that ended this author's chain, if the node holds
   * one. */
  rotationOf?(pub: Uint8Array): StoredBlock | undefined;
}

/** A source that holds nothing: validation of a genesis block needs no more. */
export const EMPTY_SOURCE: BlockSource = { get: () => undefined };

// ---------------------------------------------------------------------------
// Reference resolution (spec/05-processing-model.md, "Resolution procedure")
// ---------------------------------------------------------------------------

/** One entity digest an operation carries, and the kind its position names. */
export interface Reference {
  readonly digest: Uint8Array;
  /** The kind of entity the digest MUST resolve to (rule 5). */
  readonly kind: Entity["kind"];
  /** Which position of the operation carries it. */
  readonly role: "bond" | "filler" | "unit";
  /** How the position is named in a diagnostic. */
  readonly what: string;
}

/**
 * The entity digests an operation carries, exhaustively (spec/02, rule 4): a
 * `create_molecule`'s `bond`, each of its filler values of type 0, 1 or 2, and
 * the optional `unit` inside each of its scalar filler values. There is no
 * exempt position.
 */
export function operationReferences(op: Operation): readonly Reference[] {
  if (op.op !== "create_molecule") return [];
  const refs: Reference[] = [
    { digest: op.bond, kind: "bond", role: "bond", what: "the bond" },
  ];
  for (const [index, filler] of op.fillers.entries()) {
    const what = `filler ${index}`;
    switch (filler.type) {
      case 0:
        refs.push({ digest: filler.value, kind: "atom", role: "filler", what });
        break;
      case 1:
        refs.push({ digest: filler.value, kind: "bond", role: "filler", what });
        break;
      case 2:
        refs.push({ digest: filler.value, kind: "molecule", role: "filler", what });
        break;
      case 4: {
        const scalar = filler.value;
        if (!("from" in scalar) && scalar.unit !== undefined) {
          refs.push({
            digest: scalar.unit,
            kind: "atom",
            role: "unit",
            what: `${what}'s unit`,
          });
        }
        break;
      }
      default:
        break;
    }
  }
  return refs;
}

/**
 * The demand-driven resolution of spec/05: the same block first, then the
 * author's own ancestors through `prev`, then the blocks named in `refs` and,
 * transitively, the blocks *their* `refs` name — each stage entered only when
 * the one before it left a digest unresolved, and the last one bounded by the
 * scan limit.
 *
 * ## It reads blocks, not verdicts
 *
 * {@link StoredBlock.valid} is consulted nowhere here, and that is the rule
 * rather than an omission: rule 4's three branches name blocks the node holds
 * and can read, never valid blocks (spec/05, "Resolution procedure",
 * "Resolution reads blocks, not verdicts"). A definition may be taken from a
 * block held as stored but unvalidated, from one that forked its author's
 * chain, from one that will turn out invalid when the rest of that chain
 * arrives.
 *
 * Two things make that sound, and both hold here. Every block a
 * {@link BlockStore} hands over has passed the checks its own bytes support —
 * canonical dCBOR, the field set its `type` declares, and a signature that
 * verifies against its `pub` — because {@link BlockStore.add} runs them before
 * it stores anything, and a block that fails them throws rather than being
 * held. And a definition is self-certifying: {@link Resolver.indexBlock} keys
 * every entity under {@link entityDigest} of the entity it reconstructs from
 * the operation, which is SHA-256 over the entity's own canonical dCBOR
 * (spec/01, "Content addressing"). No block asserts a digest and none is
 * believed, so the source block's chain standing cannot change which entity a
 * digest names.
 *
 * What the permission does not touch: only a valid block's operations reach L2,
 * rules 6 and 10 are checked against the referenced block as written, and rule
 * 3 still requires a predecessor the node accepted as valid — which is why
 * {@link checkChainIntegrity} reads `valid` and this class does not.
 */
class Resolver {
  private readonly index = new Map<string, Entity>();
  private readonly source: BlockSource;
  private readonly block: PlaintextBlock;
  private readonly limit: number;
  private ancestorsLoaded = false;
  private queue: Uint8Array[] = [];
  private readonly queued = new Set<string>();
  private scanned = 0;
  private gap: Uint8Array | undefined;
  private unreadable: Uint8Array | undefined;

  constructor(block: PlaintextBlock, source: BlockSource, limit: number) {
    this.block = block;
    this.source = source;
    this.limit = limit;
    for (const ref of block.refs) this.enqueue(ref);
  }

  /**
   * How many distinct foreign blocks the resolution has scanned so far — the
   * unit spec/05-processing-model.md, "Scan limit", counts. A digest is
   * enqueued at most once, so a block the graph names repeatedly is scanned,
   * and counted, once; a block the store does not hold is never scanned at
   * all.
   */
  get scanCount(): number {
    return this.scanned;
  }

  /**
   * The first block resolution needed and the source did not hold — an
   * ancestor of the author's own chain, a `refs` entry, or a block reached
   * transitively through one — or `undefined` when resolution read everything
   * it asked for.
   *
   * It is what separates the two failing outcomes of spec/05, "Resolution
   * procedure": with no gap, an unresolved digest is *provably absent* from the
   * reachable set and the block is invalid; with a gap, the node has not been
   * able to decide and the block is stored but unvalidated. The distinction is
   * only consulted when a digest fails to resolve, so a block that resolves
   * everything before reaching the missing block is unaffected.
   */
  get missingBlock(): Uint8Array | undefined {
    return this.gap;
  }

  /**
   * The first block resolution needed, *held*, and could not read: a private
   * block whose operations are inside its `enc`. This package does no
   * decryption, so every private block resolution meets is one of these.
   *
   * It is the third cause of *stored but unvalidated* (spec/05, "Block
   * reception"; "Undecryptable reference handling"): an unresolved digest with
   * this recorded means the node has not decided, exactly as a missing block
   * would, because the same block is decidable for a node that holds the key
   * and a key this one was not given is not evidence about the block that needs
   * it. What settles it is a key, not an arrival, which is why it is reported
   * separately from {@link missingBlock}.
   */
  get unreadableBlock(): Uint8Array | undefined {
    return this.unreadable;
  }

  /** Record an entity created by an earlier operation of the same block. */
  define(op: EntityOperation): void {
    const entity = operationEntity(op);
    this.index.set(bytesToHex(entityDigest(entity)), entity);
  }

  /** The entity of that digest, or `undefined` if it is not reachable. */
  resolve(digest: Uint8Array): Entity | undefined {
    const key = bytesToHex(digest);
    const own = this.index.get(key);
    if (own !== undefined) return own;
    if (!this.ancestorsLoaded) {
      this.loadAncestors();
      const found = this.index.get(key);
      if (found !== undefined) return found;
    }
    while (this.queue.length > 0) {
      this.scanNext();
      const found = this.index.get(key);
      if (found !== undefined) return found;
    }
    return undefined;
  }

  /** Stage 3: ancestor blocks of the author's own chain, reached via `prev`.
   * They were validated when they arrived, so the walk is a lookup. */
  private loadAncestors(): void {
    this.ancestorsLoaded = true;
    let prev = this.block.prev;
    const visited = new Set<string>();
    while (prev !== null) {
      const key = bytesToHex(prev);
      if (visited.has(key)) return;
      visited.add(key);
      const held = this.source.get(prev);
      if (held === undefined) {
        // The chain stops here as far as this source is concerned. Rule 3 has
        // already checked the immediate predecessor, so this is a gap deeper
        // in, and it matters only if a digest fails to resolve.
        this.gap ??= prev;
        return;
      }
      if (!isPlaintextBlock(held.block)) this.unreadable ??= held.digest;
      this.indexBlock(held.block);
      prev = held.block.prev;
    }
  }

  /** Stages 4 and 5: one foreign block, then its own `refs`. */
  private scanNext(): void {
    const next = this.queue.shift();
    if (next === undefined) return;
    const held = this.source.get(next);
    if (held === undefined) {
      // Not held: nothing to scan, nothing counted against the limit, and
      // nothing learned. Another entry may still define the digest; if none
      // does, this gap is what makes the verdict undecided instead of a
      // rejection.
      this.gap ??= next;
      return;
    }
    this.scanned++;
    if (this.scanned > this.limit) {
      throw new BlockError(
        "scan-limit",
        `reference resolution scanned more than ${this.limit} foreign blocks without resolving every digest; the block is treated as invalid for unresolvable references`,
      );
    }
    if (isPlaintextBlock(held.block)) {
      this.indexBlock(held.block);
      for (const ref of held.block.refs) this.enqueue(ref);
    } else {
      // Held and unreadable: its operations, and the `refs` that would carry
      // resolution further, are inside `enc`. Nothing is learned, and if a
      // digest goes unresolved the verdict is undecided rather than a
      // rejection (spec/05, "Undecryptable reference handling").
      this.unreadable ??= held.digest;
    }
  }

  private enqueue(digest: Uint8Array): void {
    const key = bytesToHex(digest);
    if (this.queued.has(key)) return;
    this.queued.add(key);
    this.queue.push(digest);
  }

  /** Index every entity a block's operations create, under the digest
   * {@link entityDigest} computes from the entity's own canonical bytes. That
   * recomputation is what makes a definition self-certifying, and it is why one
   * may be read from a block whose validity nobody has established (see this
   * class's doc comment). A private block whose `enc` this node cannot read
   * contributes nothing. */
  private indexBlock(block: Block): void {
    if (!isPlaintextBlock(block)) return;
    for (const op of block.ops) {
      if (!isEntityOperation(op)) continue;
      const entity = operationEntity(op);
      this.index.set(bytesToHex(entityDigest(entity)), entity);
    }
  }
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

/** Two blocks of one chain claiming the same predecessor (rule 9). */
export interface ForkDetection {
  readonly pub: Uint8Array;
  /** The predecessor both blocks name; `null` when both claim the genesis
   * position, which is the shape an ambiguous succession takes. */
  readonly prev: Uint8Array | null;
  /** The digests of every block in the fork, the new one last. */
  readonly blocks: readonly Uint8Array[];
}

/** A successor chain's genesis block naming the rotation block it succeeds
 * (spec/02, "Verifiable succession"). */
export interface Succession {
  /** The digest of the rotation block. */
  readonly rotation: Uint8Array;
  /** The digest of the successor chain's genesis block. */
  readonly genesis: Uint8Array;
  /** The key whose chain the rotation ended. */
  readonly oldPub: Uint8Array;
  /** The key the successor chain is signed by, which the rotation named. */
  readonly newPub: Uint8Array;
}

/** What a validation established beyond "valid": the conditions the
 * specification requires a node to *surface* rather than reject. */
export interface ValidationReport {
  readonly digest: Uint8Array;
  /** Rule 9. Present when the node already holds another block of this chain
   * with the same `prev`; handling is implementation-scoped. */
  readonly fork?: ForkDetection;
  /** Present when this block is the genesis block of a chain succeeding a
   * rotation the node holds. */
  readonly succession?: Succession;
  /**
   * `refs` entries the node does not hold, for which rules 6 and 10 are
   * reported as unchecked (spec/05, "Public/private reference rules").
   *
   * Informational, and nothing more: it names what this verdict does not cover
   * so that an application can ask for those blocks if it cares. The block is
   * valid. An entry no validation of the block resolved is outside its validity
   * for good — a node that later holds one and finds it private, or of the
   * author's own chain, MUST NOT re-open a verdict it has accepted (spec/02,
   * "Validation", "A verdict moves in one direction"), which is why
   * {@link BlockStore} never re-validates a block it has stored as valid.
   */
  readonly uncheckedRefs: readonly Uint8Array[];
  /** How many distinct foreign blocks reference resolution scanned — the unit
   * of spec/05, "Scan limit". */
  readonly scanned: number;
  /**
   * Present when this block's `ts` is earlier than its predecessor's. The
   * field SHOULD be monotonic and implementations SHOULD warn when it is not
   * (spec/02, the `ts` row), but a timestamp is self-reported and untrusted:
   * this is a warning and never a validity decision.
   */
  readonly nonMonotonicTimestamp?: { readonly previous: bigint; readonly current: bigint };
  /** False when the source cannot answer sibling queries, so rule 9 could not
   * be evaluated. */
  readonly forkDetectionPerformed: boolean;
  /** True for a private block, whose rules 4, 5, 6 and 10 only a holder of the
   * decryption key can check. */
  readonly encrypted: boolean;
}

/** Options for {@link validateBlock}. */
export interface ValidateOptions {
  /** The bound on the distinct foreign blocks resolution may scan
   * (spec/05, "Scan limit"). Defaults to {@link DEFAULT_SCAN_LIMIT}. */
  readonly scanLimit?: number;
}

/**
 * Validate a block against what a node holds: every rule of spec/02,
 * "Validation", in the order the specification numbers them.
 *
 * Throws {@link BlockError} on an invalid block. The one code that does not
 * mean "invalid" is `unvalidated`: a block validating this one requires is one
 * the node cannot read, so it has not been able to decide, and the block may be
 * kept as **stored but unvalidated** (spec/05, "Block reception"). Two rules
 * reach that verdict — rule 3, when the predecessor is missing or is itself
 * unvalidated, and rule 4, when reference resolution needs a block the source
 * does not hold, or one it holds and cannot read. The error's `awaiting` names
 * the block whose arrival would settle it; its `undecryptable` names the block
 * a decryption key is wanted for, this package having none. Neither a block a
 * source withholds nor a key this node was not given can make a block invalid.
 *
 * A fork (rule 9) and a chain succession are *surfaced*, not rejected: the
 * specification requires detection and leaves the handling to the
 * implementation, so both appear in the returned {@link ValidationReport}.
 */
export function validateBlock(
  block: Block,
  source: BlockSource = EMPTY_SOURCE,
  options: ValidateOptions = {},
): ValidationReport {
  const limit = options.scanLimit ?? DEFAULT_SCAN_LIMIT;

  // Rules 1, 7, 8 and 10's structural half, the dispatch rules and the
  // rotation-block constraints.
  validateBlockStructure(block);
  const bytes = encodeBlock(block);
  const selfDigest = digest(bytes);

  // Rule 2: signature check.
  if (!verifyBlockSignature(block)) {
    throw new BlockError(
      "signature",
      "the sig field is not a valid Ed25519 signature over the block's signing input, verified against pub",
    );
  }

  // Rule 3: chain integrity.
  const predecessor = checkChainIntegrity(block, selfDigest, source);

  // The `ts` SHOULD be monotonic within a chain; a node SHOULD warn when it is
  // not, and MUST NOT decide validity on it.
  let nonMonotonicTimestamp: { previous: bigint; current: bigint } | undefined;
  if (
    predecessor !== undefined &&
    isPlaintextBlock(block) &&
    isPlaintextBlock(predecessor.block) &&
    block.ts < predecessor.block.ts
  ) {
    nonMonotonicTimestamp = { previous: predecessor.block.ts, current: block.ts };
  }

  const uncheckedRefs: Uint8Array[] = [];
  let scanned = 0;

  if (isPlaintextBlock(block)) {
    // Rules 6 and 10, evaluated on each referenced block the node holds. An
    // entry it does not hold leaves both unchecked: resolution is
    // demand-driven, and a node is not obliged to fetch a block for the sole
    // purpose of reading its type. Reading a block's type here is not scanning
    // it: no operation is read, so it costs no unit of the scan limit until
    // resolution reaches it below.
    //
    // What is left unchecked stays outside this block's validity: the rules
    // bind for the entries this validation resolved, and a block accepted
    // without one of them is not re-opened when it arrives later (spec/02,
    // "Validation", "A verdict moves in one direction").
    for (const ref of block.refs) {
      const held = source.get(ref);
      if (held === undefined) {
        uncheckedRefs.push(ref);
        continue;
      }
      if (block.type === "public" && held.block.type === "private") {
        throw new BlockError(
          "reference-visibility",
          `refs names the private block ${bytesToHex(ref)}: a public block MUST NOT depend on content a node without a decryption key cannot read`,
        );
      }
      if (bytesEqual(held.block.pub, block.pub)) {
        throw new BlockError(
          "reference-hygiene",
          `refs names ${bytesToHex(ref)}, a block of the author's own chain: such a block is already a resolution path under rule 4`,
        );
      }
    }

    // Rules 4 and 5, operation by operation and in order: an operation may
    // reference entities created by earlier operations of the same block.
    const resolver = new Resolver(block, source, limit);
    for (const [index, op] of block.ops.entries()) {
      for (const reference of operationReferences(op)) {
        const entity = resolver.resolve(reference.digest);
        if (entity === undefined) {
          // Rule 4's verdict is three-valued (spec/02, "Validation" rule 4).
          // A digest that fails to resolve is *unresolvable* only when
          // resolution read every block it asked for; if a block it needed is
          // not held, the node has not decided, and the block is stored but
          // unvalidated rather than invalid. The absence of a block is not
          // evidence about the validity of the block that names it.
          const missing = resolver.missingBlock;
          if (missing !== undefined) {
            throw new BlockError(
              "unvalidated",
              `ops[${index}]: ${reference.what} names ${bytesToHex(reference.digest)}, which did not resolve because the block ${bytesToHex(missing)} is not held: the block is neither valid nor invalid, and is stored but unvalidated until that block arrives`,
              { awaiting: missing },
            );
          }
          // The same verdict for the same reason when the block resolution
          // needed is held and unreadable: what is missing is a key, and a
          // capability this node lacks is not evidence about another author's
          // block (spec/05, "Undecryptable reference handling").
          const unreadable = resolver.unreadableBlock;
          if (unreadable !== undefined) {
            throw new BlockError(
              "unvalidated",
              `ops[${index}]: ${reference.what} names ${bytesToHex(reference.digest)}, which did not resolve because the private block ${bytesToHex(unreadable)} could not be read: the block is neither valid nor invalid, and is stored but unvalidated until a decryption key for it is held`,
              { undecryptable: unreadable },
            );
          }
          throw new BlockError(
            "reachability",
            `ops[${index}]: ${reference.what} names ${bytesToHex(reference.digest)}, which is not reachable from this block — not defined here, in an ancestor of this chain, or in a block reachable through refs`,
          );
        }
        if (entity.kind !== reference.kind) {
          throw new BlockError(
            "data-model",
            `ops[${index}]: ${reference.what} MUST resolve to ${article(reference.kind)}, but ${bytesToHex(reference.digest)} is ${article(entity.kind)}`,
          );
        }
        // The filler-count rule of spec/01, checked here because this is the
        // layer that has resolved the bond (spec/02, rule 5).
        if (reference.role === "bond" && entity.kind === "bond" && op.op === "create_molecule") {
          const variables = templateVariables(entity.template);
          if (op.fillers.length !== variables.length) {
            throw new BlockError(
              "data-model",
              `ops[${index}]: the molecule carries ${op.fillers.length} filler(s) but its bond template has ${variables.length} variable(s)`,
            );
          }
        }
      }
      if (isEntityOperation(op)) resolver.define(op);
    }
    scanned = resolver.scanCount;
  }

  // Rule 9: fork detection.
  const forkDetectionPerformed = typeof source.siblings === "function";
  const fork = forkDetectionPerformed
    ? detectFork(block, selfDigest, source)
    : undefined;

  const succession = detectSuccession(block, selfDigest, source);

  return {
    digest: selfDigest,
    ...(fork === undefined ? {} : { fork }),
    ...(succession === undefined ? {} : { succession }),
    ...(nonMonotonicTimestamp === undefined ? {} : { nonMonotonicTimestamp }),
    uncheckedRefs,
    scanned,
    forkDetectionPerformed,
    encrypted: block.type === "private",
  };
}

function article(kind: Entity["kind"]): string {
  return kind === "atom" ? "an atom" : kind === "bond" ? "a bond" : "a molecule";
}

/**
 * Rule 3. A non-null `prev` MUST reference a block the node holds and has
 * accepted as valid, carrying the same `pub`; and no block may be appended to a
 * chain a rotation block has ended (spec/02, "rotate_key").
 */
function checkChainIntegrity(
  block: Block,
  selfDigest: Uint8Array,
  source: BlockSource,
): StoredBlock | undefined {
  const rotation = source.rotationOf?.(block.pub);
  if (rotation !== undefined && !bytesEqual(rotation.digest, selfDigest)) {
    throw new BlockError(
      "chain",
      `this key's chain ended at the rotation block ${bytesToHex(rotation.digest)}; the key is inactive and no further block signed by it is accepted`,
    );
  }

  if (block.prev === null) return undefined;

  const predecessor = source.get(block.prev);
  if (predecessor === undefined) {
    throw new BlockError(
      "unvalidated",
      `the predecessor ${bytesToHex(block.prev)} is not held: the block is neither valid nor invalid, and is stored but unvalidated until its ancestry arrives`,
      { awaiting: block.prev },
    );
  }
  if (!predecessor.valid) {
    throw new BlockError(
      "unvalidated",
      `the predecessor ${bytesToHex(block.prev)} is itself stored but unvalidated; it MUST NOT be treated as this block's predecessor`,
      { awaiting: block.prev },
    );
  }
  if (!bytesEqual(predecessor.block.pub, block.pub)) {
    throw new BlockError(
      "chain",
      "prev names a block of another author's chain: within a single chain, all blocks MUST have the same pub field",
    );
  }
  if (predecessor.block.type === "rotation") {
    throw new BlockError(
      "chain",
      "prev names a rotation block, which is the last block of its chain: the new key begins a separate chain",
    );
  }
  return predecessor;
}

/** Rule 9: another held block of this chain claiming the same predecessor. */
function detectFork(
  block: Block,
  selfDigest: Uint8Array,
  source: BlockSource,
): ForkDetection | undefined {
  const siblings = source.siblings?.(block.pub, block.prev) ?? [];
  const others = siblings.filter((sibling) => !bytesEqual(sibling.digest, selfDigest));
  if (others.length === 0) return undefined;
  return {
    pub: block.pub,
    prev: block.prev,
    blocks: [...others.map((sibling) => sibling.digest), selfDigest],
  };
}

/**
 * The succession of spec/02, "Verifiable succession": a genesis block that is
 * public, is signed by the key a rotation block named, and lists that rotation
 * block's digest in its `refs`. All three are required, and the block's own
 * signature is what makes the pair evidence rather than an assertion.
 *
 * A private genesis block cannot be checked here — its `refs` are inside `enc`
 * — which is exactly why the specification requires the successor's genesis
 * block to be public. A node that decrypts one and finds the reference MUST
 * reject it; that check belongs with decryption.
 */
function detectSuccession(
  block: Block,
  selfDigest: Uint8Array,
  source: BlockSource,
): Succession | undefined {
  if (block.prev !== null) return undefined;
  if (!isPlaintextBlock(block)) return undefined;
  if (block.type !== "public") return undefined;
  for (const ref of block.refs) {
    const held = source.get(ref);
    if (held === undefined) continue;
    const referenced = held.block;
    if (referenced.type !== "rotation") continue;
    if (!bytesEqual(referenced.ops[0].newPub, block.pub)) continue;
    return {
      rotation: held.digest,
      genesis: selfDigest,
      oldPub: referenced.pub,
      newPub: block.pub,
    };
  }
  return undefined;
}

// ---------------------------------------------------------------------------
// An in-memory store
// ---------------------------------------------------------------------------

/** What {@link BlockStore.add} did with a block. */
export type AcceptStatus =
  /** Validated and stored; its operations may reach L2. */
  | "accepted"
  /** Held, but neither valid nor invalid: a block validating it requires — its
   * ancestry, or one its references resolve through — has not arrived. */
  | "unvalidated"
  /** Already held, byte for byte; nothing changed. */
  | "duplicate";

/** The outcome of offering a block to a store. */
export interface AcceptResult {
  readonly status: AcceptStatus;
  readonly digest: Uint8Array;
  /** Present when the block was validated. */
  readonly report?: ValidationReport;
  /** Present when the block was held unvalidated: its `awaiting` names the
   * block whose arrival would settle the verdict, or its `undecryptable` the
   * block a decryption key is wanted for — a block already held, which is why
   * no arrival will re-validate it and the caller re-offers the block once it
   * has the key. */
  readonly pending?: BlockError;
}

/** How a store responds to a fork (rule 9). Detection is normative; the
 * response is implementation-scoped. */
export type ForkPolicy =
  /** Store the block and record the fork, leaving the choice to the
   * application. */
  | "flag"
  /** Refuse the block that forks the chain. */
  | "reject";

/** Options for a {@link BlockStore}. */
export interface BlockStoreOptions {
  readonly forkPolicy?: ForkPolicy;
  readonly scanLimit?: number;
}

/**
 * A node's Layer 1 storage, in memory: enough to validate a chain, resolve
 * references across chains, and surface the two conditions the specification
 * requires a node to surface — a fork and an ambiguous succession.
 *
 * The store carries the induction of spec/02, "Validation": a block is
 * validated when it is offered, and its `valid` flag is what rule 3 reads when
 * the next block arrives. A block the store could not decide about — because
 * its predecessor has not arrived, because reference resolution needed a block
 * the store does not hold, or because it needed one the store holds and cannot
 * read — is kept as **stored but unvalidated**, and re-validated when the
 * missing block arrives. The third cause has no arrival to wait for: what it
 * wants is a decryption key, so the block is simply held and the caller offers
 * it again once it has one. No cause is ever recorded as a rejection: neither a
 * block the store does not have nor a key it was not given says anything about
 * the validity of the block that needs it.
 */
export class BlockStore implements BlockSource {
  private readonly blocks = new Map<string, StoredBlock>();
  /** pub|prev → digests, the index rule 9 is a lookup in. */
  private readonly positions = new Map<string, Uint8Array[]>();
  /** pub → the rotation block that ended that chain. */
  private readonly rotations = new Map<string, Uint8Array>();
  /** rotation digest → the genesis blocks claiming to succeed it. */
  private readonly successors = new Map<string, Uint8Array[]>();
  /** digest of a block that has not arrived → blocks held unvalidated awaiting
   * it, whether as their predecessor (rule 3) or as a block their reference
   * resolution needs (rule 4). */
  private readonly pending = new Map<string, Uint8Array[]>();
  private readonly detectedForks: ForkDetection[] = [];
  private readonly detectedSuccessions: Succession[] = [];
  private readonly forkPolicy: ForkPolicy;
  private readonly scanLimit: number;

  constructor(options: BlockStoreOptions = {}) {
    this.forkPolicy = options.forkPolicy ?? "flag";
    this.scanLimit = options.scanLimit ?? DEFAULT_SCAN_LIMIT;
  }

  /** Every fork the store has detected, in the order it detected them. */
  get forks(): readonly ForkDetection[] {
    return this.detectedForks;
  }

  /** Every succession the store has seen a genesis block claim. */
  get successions(): readonly Succession[] {
    return this.detectedSuccessions;
  }

  /** The rotations for which the store holds more than one claimed successor
   * genesis block. The succession is ambiguous and MUST be surfaced; a node
   * MUST NOT pick a successor on its own. */
  get ambiguousSuccessions(): readonly Succession[] {
    const ambiguous: Succession[] = [];
    for (const [rotation, genesisDigests] of this.successors) {
      if (genesisDigests.length < 2) continue;
      for (const succession of this.detectedSuccessions) {
        if (bytesToHex(succession.rotation) === rotation) ambiguous.push(succession);
      }
    }
    return ambiguous;
  }

  /** Every block the store holds, valid or not. */
  get size(): number {
    return this.blocks.size;
  }

  get(digestBytes: Uint8Array): StoredBlock | undefined {
    return this.blocks.get(bytesToHex(digestBytes));
  }

  /** Whether the store holds a block of that digest. */
  has(digestBytes: Uint8Array): boolean {
    return this.blocks.has(bytesToHex(digestBytes));
  }

  siblings(pub: Uint8Array, prev: Uint8Array | null): readonly StoredBlock[] {
    const digests = this.positions.get(positionKey(pub, prev)) ?? [];
    const out: StoredBlock[] = [];
    for (const held of digests) {
      const stored = this.blocks.get(bytesToHex(held));
      if (stored !== undefined) out.push(stored);
    }
    return out;
  }

  rotationOf(pub: Uint8Array): StoredBlock | undefined {
    const rotation = this.rotations.get(bytesToHex(pub));
    return rotation === undefined ? undefined : this.blocks.get(bytesToHex(rotation));
  }

  /*
   * There is deliberately no `tip` method here, and one should not be added.
   *
   * A tip is defined constructively (spec/07-transport.md, "tip"): the end of a
   * forward walk from the genesis position through the blocks the store holds,
   * crossing them whatever verdict it has reached about any of them, and
   * following the lowest digest where it holds more than one block at a
   * position. `walkChain` and `sourceTip` in `transport.ts` are that walk, and
   * it is the only definition of a tip in this codebase.
   *
   * The definition a store's own index answers cheaply — the block of that `pub`
   * that nothing else names as a predecessor — is the one the profile refuses by
   * name, and it lived here until todo 097. It differs in three ways and each of
   * them matters: it reports a tip across a hole that `range` cannot reach
   * (server rule 1), it withholds a block whose verdict is undecided (server
   * rule 7), and it picks a fork's branch by insertion order rather than
   * deterministically and stably. A caller wanting "which of my blocks does
   * nothing name as a predecessor" wants a plural, unfiltered answer under a
   * name that is not `tip`; `siblings` is the primitive it builds on.
   */

  /** Every valid block the store holds, in insertion order. */
  validBlocks(): readonly StoredBlock[] {
    return [...this.blocks.values()].filter((stored) => stored.valid);
  }

  /**
   * Offer a block to the store: validate it and, if it is valid, keep it.
   *
   * Invalid blocks throw. A block the store cannot decide about — its ancestry
   * has not arrived, or reference resolution needs a block it does not hold or
   * cannot read — is kept as stored but unvalidated, and re-validated when the
   * block it is waiting for is added. A verdict waiting on a key instead waits
   * for the caller to offer the block again.
   */
  add(input: Uint8Array | Block): AcceptResult {
    const block = input instanceof Uint8Array ? decodeBlock(input) : input;
    const bytes = input instanceof Uint8Array ? input : encodeBlock(block);
    if (input instanceof Uint8Array) {
      // Rule 8: the bytes a block arrives as MUST be its canonical encoding.
      const canonical = encodeBlock(block);
      if (!bytesEqual(canonical, bytes)) {
        throw new BlockError(
          "encoding",
          "the block's bytes are not its canonical dCBOR encoding",
        );
      }
    }
    const selfDigest = digest(bytes);
    const key = bytesToHex(selfDigest);
    const existing = this.blocks.get(key);
    if (existing !== undefined && existing.valid) {
      return { status: "duplicate", digest: selfDigest };
    }

    let report: ValidationReport;
    try {
      report = validateBlock(block, this, { scanLimit: this.scanLimit });
    } catch (error) {
      if (error instanceof BlockError && error.code === "unvalidated") {
        this.hold(selfDigest, bytes, block, error.awaiting);
        // An arrival wakes the blocks waiting for it whatever verdict it got
        // itself. Reference resolution reads blocks and not verdicts (spec/05,
        // "Resolution procedure", "Resolution reads blocks, not verdicts"), so
        // a rule 4 waiter needed this block to be *readable*, which it now is:
        // holding it undecided and leaving the waiter to wait would make the
        // waiter's verdict depend on a chain standing rule 4 never asked
        // about. Rule 3 is the one rule that wants its block *accepted*, and a
        // waiter woken for it simply fails rule 3 again and is re-filed under
        // the same block, at the cost of one validation (todos/083). The
        // specification requires this of a store that keeps held blocks, which
        // this one does: spec/05-processing-model.md, "Block reception",
        // "Revalidation on arrival" (todos/084).
        this.retryPending(selfDigest);
        return { status: "unvalidated", digest: selfDigest, pending: error };
      }
      throw error;
    }

    if (report.fork !== undefined) {
      this.detectedForks.push(report.fork);
      if (this.forkPolicy === "reject") {
        throw new BlockError(
          "fork",
          `this block forks the chain at ${report.fork.prev === null ? "the genesis position" : bytesToHex(report.fork.prev)}`,
        );
      }
    }

    this.store(selfDigest, bytes, block, true);
    if (report.succession !== undefined) {
      this.detectedSuccessions.push(report.succession);
      const rotationKey = bytesToHex(report.succession.rotation);
      const claimed = this.successors.get(rotationKey) ?? [];
      claimed.push(selfDigest);
      this.successors.set(rotationKey, claimed);
    }
    this.retryPending(selfDigest);
    return { status: "accepted", digest: selfDigest, report };
  }

  /**
   * Hold a block the store could not decide about, filed under the block it is
   * waiting for: its predecessor when rule 3 could not be evaluated, or the
   * block reference resolution could not obtain when rule 4 could not. Both are
   * the same **stored but unvalidated** state, so both are held the same way
   * and re-validated by the same arrival.
   *
   * A verdict left undecided by an *unreadable* block — held, and encrypted to
   * keys this node has none of — is the same state with nothing to file it
   * under: the block it needs is already here, and only a key will settle it.
   * It is held, kept out of L2 and never recorded as invalid, and the caller
   * that obtains the key offers the block again.
   */
  private hold(
    selfDigest: Uint8Array,
    bytes: Uint8Array,
    block: Block,
    awaiting: Uint8Array | undefined,
  ): void {
    this.store(selfDigest, bytes, block, false);
    if (awaiting === undefined) return;
    const key = bytesToHex(awaiting);
    const waiting = this.pending.get(key) ?? [];
    if (!waiting.some((held) => bytesEqual(held, selfDigest))) waiting.push(selfDigest);
    this.pending.set(key, waiting);
  }

  private store(
    selfDigest: Uint8Array,
    bytes: Uint8Array,
    block: Block,
    valid: boolean,
  ): void {
    const key = bytesToHex(selfDigest);
    const known = this.blocks.has(key);
    this.blocks.set(key, { digest: selfDigest, bytes, block, valid });
    if (!known) {
      const position = positionKey(block.pub, block.prev);
      const siblings = this.positions.get(position) ?? [];
      siblings.push(selfDigest);
      this.positions.set(position, siblings);
    }
    if (valid && block.type === "rotation") {
      this.rotations.set(bytesToHex(block.pub), selfDigest);
    }
  }

  /** Re-validate the blocks that were waiting for this one. */
  private retryPending(selfDigest: Uint8Array): void {
    const key = bytesToHex(selfDigest);
    const waiting = this.pending.get(key);
    if (waiting === undefined) return;
    this.pending.delete(key);
    for (const held of waiting) {
      const stored = this.blocks.get(bytesToHex(held));
      if (stored === undefined || stored.valid) continue;
      try {
        this.add(stored.bytes);
      } catch {
        // A block that turns out to be invalid stays where it is: the store
        // holds its bytes and never marks it valid. Surfacing the rejection is
        // the caller's business at the point it offered the block.
      }
    }
  }
}

function positionKey(pub: Uint8Array, prev: Uint8Array | null): string {
  return `${bytesToHex(pub)}|${prev === null ? "genesis" : bytesToHex(prev)}`;
}
