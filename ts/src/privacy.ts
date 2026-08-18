/**
 * Private blocks: the payload a `private-block`'s `enc` carries, the AAD
 * that binds that ciphertext to the block's plaintext fields, and the
 * per-recipient content-key wrap that lets a chosen reader decrypt it.
 *
 * Implements `spec/04-cryptography.md` in full for the private-block path:
 * "Private block encryption" (the plaintext payload, the AAD construction,
 * the XChaCha20-Poly1305 seal and open), "Key management" and "Wrapped key
 * format" (the per-recipient wrap, pinned to 72 bytes), and
 * "Ed25519-to-X25519 conversion" (the five-step public procedure and the
 * four-step private procedure, with the rejection rules the spec states:
 * a non-canonical *y*, *y* = 1, and — downstream, at the point spec/04 places
 * it — the all-zero agreement result of a small-order key).
 *
 * `./block.ts` owns everything about a private block that does not require
 * the content key: its `enc` and `nonce` fields are opaque bytes there, with
 * only a size floor checked. This module is the one place that reads or
 * writes what `enc` actually holds.
 *
 * Browser-safe: XChaCha20-Poly1305 comes from @noble/ciphers, X25519 from
 * @noble/curves, SHA-512 and HKDF-SHA-256 from @noble/hashes; no `node:`
 * imports and no Node-only globals.
 */

import { xchacha20poly1305 } from "@noble/ciphers/chacha.js";
import { x25519 } from "@noble/curves/ed25519";
import { hkdf } from "@noble/hashes/hkdf";
import { sha256, sha512 } from "@noble/hashes/sha2";

import {
  type EntityOperation,
  type Operation,
  type PrivateBlock,
  BlockError,
  NONCE_SIZE,
  PROTOCOL_VERSION,
  PUBLIC_KEY_SIZE,
  operationFromValue,
  operationValue,
  seedOf,
  unsignedPrivateBlock,
} from "./block.ts";
import { type DcborValue, DcborError, decode, encode } from "./dcbor.ts";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Size of a private chain's content key (spec/04, "Encryption scheme"). */
export const CONTENT_KEY_SIZE = 32;
/** Size of an X25519 private or public key, and of the shared secret and
 * wrapping key an agreement and HKDF produce from them — all 32 bytes. */
export const X25519_KEY_SIZE = 32;
/** Size of the wrapping key HKDF-SHA-256 derives (spec/04, "Key management"). */
export const WRAPPING_KEY_SIZE = 32;
/** Size of the nonce a key wrap uses, the same XChaCha20 nonce size as a
 * block's own (spec/04, "Wrapped key format"). */
export const WRAP_NONCE_SIZE = 24;
/** The fixed size of every wrapped key: 24-byte nonce, 32-byte ciphertext,
 * 16-byte Poly1305 tag (spec/04, "Wrapped key format"). */
export const WRAPPED_KEY_SIZE = WRAP_NONCE_SIZE + CONTENT_KEY_SIZE + 16;

/** The HKDF info string of spec/04, "Key management", step 4. */
export const KEY_WRAP_INFO = "dialog-v1-key-wrap";
const KEY_WRAP_INFO_BYTES = new TextEncoder().encode(KEY_WRAP_INFO);
/**
 * The zero-length salt spec/04 specifies. Passed explicitly rather than left
 * `undefined`: @noble/hashes' `hkdf` treats an omitted salt as `hash.outputLen`
 * zero bytes (the RFC 5869 default for "no salt provided"), which is not what
 * spec/04, "Key management" step 4 asks for ("salt: empty (zero-length byte
 * string)"). The two happen to produce the same HMAC key here — HMAC zero-pads
 * a short key up to the block size, and both a zero-length and an all-zero
 * `outputLen`-length salt zero-pad to the same block-size key — but that is an
 * accident of an all-zero salt, not a general equivalence, so it is not relied
 * on.
 */
const EMPTY_SALT = new Uint8Array(0);

/** *p* = 2^255 − 19, the field of Curve25519 (spec/04,
 * "Ed25519-to-X25519 conversion"). */
const CURVE25519_P = (1n << 255n) - 19n;

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/** The class of rejection a private-block or key-wrap operation reports. */
export type PrivacyErrorCode =
  /** a byte string is not the size its definition gives. */
  | "field"
  /** the Ed25519-to-X25519 conversion rejected its input: a non-canonical
   * *y*, or *y* = 1. */
  | "conversion"
  /** an X25519 agreement produced the all-zero result of a small-order key
   * (RFC 7748 §6.1). */
  | "agreement"
  /** a wrapped key is not exactly {@link WRAPPED_KEY_SIZE} bytes. */
  | "wrap-length"
  /** AEAD authentication failed: wrong key, or a tampered ciphertext, nonce
   * or AAD-covered field. */
  | "aead"
  /** the (correctly authenticated) decrypted plaintext is not valid dCBOR, or
   * not the shape a private block's payload must have. */
  | "payload";

/** A rejection by this module. */
export class PrivacyError extends Error {
  readonly code: PrivacyErrorCode;

  constructor(code: PrivacyErrorCode, message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "PrivacyError";
    this.code = code;
  }
}

function checkBytes(value: unknown, size: number, what: string): Uint8Array {
  if (!(value instanceof Uint8Array)) {
    throw new PrivacyError("field", `${what} MUST be a byte string`);
  }
  if (value.length !== size) {
    throw new PrivacyError("field", `${what} MUST be ${size} bytes, got ${value.length}`);
  }
  return value;
}

/** {@link seedOf}, reporting its rejection as a {@link PrivacyError}: this
 * module never throws a `./block.ts` error type. */
function toSeed(privateKey: Uint8Array): Uint8Array {
  try {
    return seedOf(privateKey);
  } catch (cause) {
    if (cause instanceof BlockError) {
      throw new PrivacyError("field", cause.message, { cause });
    }
    throw cause;
  }
}

// ---------------------------------------------------------------------------
// Ed25519-to-X25519 conversion (spec/04, "Ed25519-to-X25519 conversion")
// ---------------------------------------------------------------------------

/** Reduce into `[0, p)`: bigint `%` can return a negative result. */
function mod(x: bigint): bigint {
  const r = x % CURVE25519_P;
  return r >= 0n ? r : r + CURVE25519_P;
}

/** Modular exponentiation mod *p*, for the modular inverse below. */
function fpow(base: bigint, exponent: bigint): bigint {
  let result = 1n;
  let b = mod(base);
  let e = exponent;
  while (e > 0n) {
    if (e & 1n) result = mod(result * b);
    b = mod(b * b);
    e >>= 1n;
  }
  return result;
}

/** Modular inverse via Fermat's little theorem: x^(p-2) mod p. */
function finv(x: bigint): bigint {
  return fpow(x, CURVE25519_P - 2n);
}

function leBytesToBigInt(bytes: Uint8Array): bigint {
  let value = 0n;
  for (let i = bytes.length - 1; i >= 0; i--) {
    value = (value << 8n) | BigInt(bytes[i]!);
  }
  return value;
}

function bigIntToLeBytes(value: bigint, length: number): Uint8Array {
  const out = new Uint8Array(length);
  let v = value;
  for (let i = 0; i < length; i++) {
    out[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  return out;
}

/**
 * The public half of Ed25519-to-X25519 conversion (spec/04,
 * "Ed25519-to-X25519 conversion"): the birational map of RFC 7748 §4.1 from
 * an Edwards *y*-coordinate to a Montgomery *u*-coordinate.
 *
 * Steps 1-5 of the spec, in order:
 * 1. clear the sign-of-*x* bit (the MSB of byte 31);
 * 2. read the rest as little-endian *y*, rejecting *y* ≥ *p* (non-canonical);
 * 3. reject *y* = 1 (the identity point) before the division it would make
 *    undefined;
 * 4. compute *u* = (1 + *y*)(1 − *y*)⁻¹ mod *p*;
 * 5. encode *u* as 32 little-endian bytes.
 *
 * A small-order key is not rejected here — it converts to a small-order *u*
 * without error — because the spec places that rejection downstream, at the
 * all-zero result the agreement with it produces; see
 * {@link x25519SharedSecret}.
 */
export function ed25519PublicKeyToX25519(publicKey: Uint8Array): Uint8Array {
  checkBytes(publicKey, PUBLIC_KEY_SIZE, "an Ed25519 public key");

  // Step 1: clear the sign-of-x bit. It takes no part in the map.
  const cleared = publicKey.slice();
  cleared[31] = (cleared[31] ?? 0) & 0x7f;

  // Step 2: the remaining 255 bits, little-endian. y >= p is not canonical:
  // no Ed25519 public key produces such an encoding.
  const y = leBytesToBigInt(cleared);
  if (y >= CURVE25519_P) {
    throw new PrivacyError(
      "conversion",
      `the y-coordinate is not canonical (y = ${y} >= p); no Ed25519 public key produces this encoding`,
    );
  }

  // Step 3: y = 1 is the identity point. 1 - y has no inverse, and its
  // Montgomery image is the point at infinity, which has no u to encode.
  if (y === 1n) {
    throw new PrivacyError(
      "conversion",
      "y = 1 is the identity point: 1 - y has no inverse, so it has no Montgomery image",
    );
  }

  // Step 4: u = (1 + y) * (1 - y)^-1 mod p.
  const u = mod(mod(1n + y) * finv(mod(1n - y)));

  // Step 5: u, little-endian, 32 bytes.
  return bigIntToLeBytes(u, 32);
}

/**
 * The private half of Ed25519-to-X25519 conversion (spec/04,
 * "Ed25519-to-X25519 conversion"): an Ed25519 private key is a 32-byte seed,
 * not a scalar, and the X25519 private key is the scalar that seed expands
 * to (RFC 8032 §5.1.5).
 *
 * Steps 1-4 of the spec:
 * 1. *h* = SHA-512(seed);
 * 2. *s* = *h*[0..31], the lower half;
 * 3. clamp: `s[0] &= 248`, `s[31] &= 127`, `s[31] |= 64`;
 * 4. *s* is the X25519 private key.
 *
 * Accepts either the 32-byte seed or the 64-byte seed-and-public-key form
 * (`./block.ts`'s {@link seedOf} takes the seed apart). Using the seed itself
 * as the X25519 scalar — skipping this hash — is *not* this conversion: it
 * yields a valid-looking key that agrees with nobody.
 */
export function ed25519PrivateKeyToX25519(privateKey: Uint8Array): Uint8Array {
  const seed = toSeed(privateKey);
  const hash = sha512(seed);
  const scalar = hash.slice(0, 32);
  scalar[0] = (scalar[0] ?? 0) & 0xf8;
  scalar[31] = ((scalar[31] ?? 0) & 0x7f) | 0x40;
  return scalar;
}

/**
 * X25519 agreement between an X25519 private key and an X25519 public key
 * (spec/04, "Key management", step 3: `X25519(author_x25519_sk,
 * recipient_x25519_pk)`).
 *
 * A public key of small order converts to a *u* of small order, and the
 * agreement with it yields an all-zero result (RFC 7748 §6.1), which the spec
 * requires be rejected. @noble/curves' `x25519.getSharedSecret` already
 * refuses that all-zero product, so this function reports that refusal as a
 * {@link PrivacyError} rather than re-implementing the check.
 */
export function x25519SharedSecret(
  x25519PrivateKey: Uint8Array,
  x25519PublicKey: Uint8Array,
): Uint8Array {
  checkBytes(x25519PrivateKey, X25519_KEY_SIZE, "an X25519 private key");
  checkBytes(x25519PublicKey, X25519_KEY_SIZE, "an X25519 public key");
  try {
    return x25519.getSharedSecret(x25519PrivateKey, x25519PublicKey);
  } catch (cause) {
    throw new PrivacyError(
      "agreement",
      "the X25519 agreement produced the all-zero output of a small-order key; this MUST be rejected (RFC 7748 §6.1) and no wrapping key derived from it",
      { cause },
    );
  }
}

// ---------------------------------------------------------------------------
// Key wrap (spec/04, "Key management" and "Wrapped key format")
// ---------------------------------------------------------------------------

/**
 * `HKDF-SHA-256(salt: empty, ikm: shared_secret, info: "dialog-v1-key-wrap",
 * length: 32)` (spec/04, "Key management", step 4).
 */
export function deriveWrappingKey(sharedSecret: Uint8Array): Uint8Array {
  checkBytes(sharedSecret, X25519_KEY_SIZE, "an X25519 shared secret");
  return hkdf(sha256, sharedSecret, EMPTY_SALT, KEY_WRAP_INFO_BYTES, WRAPPING_KEY_SIZE);
}

/**
 * The wrapping key between two identities, named by their Ed25519 keys: the
 * whole of spec/04, "Key management", steps 1-4, run from one party's private
 * key and the other party's public key.
 *
 * Symmetric in the cryptographic sense, not the code path: the author calls
 * this with their own private key and the recipient's public key, and the
 * recipient calls it with their own private key and the author's public key,
 * and both derive the identical wrapping key — that identity is what makes
 * unwrapping possible.
 */
export function wrappingKeyBetween(
  ownEd25519PrivateKey: Uint8Array,
  peerEd25519PublicKey: Uint8Array,
): Uint8Array {
  const ownX25519Private = ed25519PrivateKeyToX25519(ownEd25519PrivateKey);
  const peerX25519Public = ed25519PublicKeyToX25519(peerEd25519PublicKey);
  const sharedSecret = x25519SharedSecret(ownX25519Private, peerX25519Public);
  return deriveWrappingKey(sharedSecret);
}

/**
 * Wrap a content key with an already-derived wrapping key: `wrap_nonce ||
 * XChaCha20Poly1305_Encrypt(wrapping_key, wrap_nonce, content_key, aad:
 * empty)` — the pinned 72-byte layout of spec/04, "Wrapped key format".
 *
 * `wrapNonce` MUST be freshly generated for every wrap, from a
 * cryptographically secure random source (spec/04, same section, which states
 * the requirement is stronger here than for block encryption: a wrapping key
 * is the same key for every wrap between one pair of identities, for every
 * chain and for all time). This function does not generate one, so that a
 * caller — and the conformance vectors — can pin the output to a fixed nonce.
 */
export function wrapContentKeyWithKey(
  wrappingKey: Uint8Array,
  contentKey: Uint8Array,
  wrapNonce: Uint8Array,
): Uint8Array {
  checkBytes(wrappingKey, WRAPPING_KEY_SIZE, "a wrapping key");
  checkBytes(contentKey, CONTENT_KEY_SIZE, "a content key");
  checkBytes(wrapNonce, WRAP_NONCE_SIZE, "a wrap nonce");
  const ciphertext = xchacha20poly1305(wrappingKey, wrapNonce).encrypt(contentKey);
  const wrapped = new Uint8Array(WRAPPED_KEY_SIZE);
  wrapped.set(wrapNonce, 0);
  wrapped.set(ciphertext, WRAP_NONCE_SIZE);
  return wrapped;
}

/**
 * Unwrap a content key with an already-derived wrapping key.
 *
 * Rejects any length but {@link WRAPPED_KEY_SIZE} *before* attempting
 * decryption (spec/04, "Wrapped key format": "Implementations MUST reject a
 * wrapped key of any other length without attempting to decrypt it: the
 * plaintext is a fixed-size key, so every conforming wrap has exactly this
 * size").
 */
export function unwrapContentKeyWithKey(wrappingKey: Uint8Array, wrapped: Uint8Array): Uint8Array {
  checkBytes(wrappingKey, WRAPPING_KEY_SIZE, "a wrapping key");
  if (!(wrapped instanceof Uint8Array)) {
    throw new PrivacyError("wrap-length", "a wrapped key MUST be a byte string");
  }
  if (wrapped.length !== WRAPPED_KEY_SIZE) {
    throw new PrivacyError(
      "wrap-length",
      `a wrapped key MUST be exactly ${WRAPPED_KEY_SIZE} bytes, got ${wrapped.length}`,
    );
  }
  const wrapNonce = wrapped.subarray(0, WRAP_NONCE_SIZE);
  const ciphertext = wrapped.subarray(WRAP_NONCE_SIZE);
  try {
    return xchacha20poly1305(wrappingKey, wrapNonce).decrypt(ciphertext);
  } catch (cause) {
    throw new PrivacyError(
      "aead",
      "the wrapped key did not authenticate under this wrapping key: wrong key pair, or tampered bytes",
      { cause },
    );
  }
}

/**
 * The author-side wrap, from Ed25519 identities directly: derive the wrapping
 * key between `authorEd25519PrivateKey` and `recipientEd25519PublicKey`, then
 * wrap. See {@link wrapContentKeyWithKey} for the nonce requirement.
 */
export function wrapContentKey(
  authorEd25519PrivateKey: Uint8Array,
  recipientEd25519PublicKey: Uint8Array,
  contentKey: Uint8Array,
  wrapNonce: Uint8Array,
): Uint8Array {
  const wrappingKey = wrappingKeyBetween(authorEd25519PrivateKey, recipientEd25519PublicKey);
  return wrapContentKeyWithKey(wrappingKey, contentKey, wrapNonce);
}

/**
 * The recipient-side unwrap, from Ed25519 identities directly: derive the
 * same wrapping key from `recipientEd25519PrivateKey` and
 * `authorEd25519PublicKey`, then unwrap.
 */
export function unwrapContentKey(
  recipientEd25519PrivateKey: Uint8Array,
  authorEd25519PublicKey: Uint8Array,
  wrapped: Uint8Array,
): Uint8Array {
  const wrappingKey = wrappingKeyBetween(recipientEd25519PrivateKey, authorEd25519PublicKey);
  return unwrapContentKeyWithKey(wrappingKey, wrapped);
}

// ---------------------------------------------------------------------------
// Private block payload (spec/04, "Private block encryption")
// ---------------------------------------------------------------------------

/**
 * The decrypted payload of a private block: the three fields spec/04,
 * "Private block encryption" encrypts together.
 *
 * `ops` excludes `rotate_key`: that operation may appear only in a rotation
 * block (spec/02, "Rotation block"), a distinct, always-plaintext block type,
 * so it can never be found inside a private block's decrypted payload either.
 */
export interface PrivatePayload {
  readonly refs: readonly Uint8Array[];
  readonly ts: bigint;
  readonly ops: readonly EntityOperation[];
}

/** `plaintext = dCBOR({"refs": ..., "ts": ..., "ops": ...})` (spec/04,
 * "Encryption procedure") — the dCBOR value of the payload, exactly as a
 * public block encodes the same three fields. */
export function privatePayloadValue(payload: PrivatePayload): DcborValue {
  const map = new Map<string, DcborValue>();
  map.set("refs", [...payload.refs]);
  map.set("ts", payload.ts);
  map.set(
    "ops",
    payload.ops.map((op) => operationValue(op)),
  );
  return map;
}

/** The dCBOR encoding of a private payload: what gets sealed into `enc`. */
export function encodePrivatePayload(payload: PrivatePayload): Uint8Array {
  return encode(privatePayloadValue(payload));
}

const PAYLOAD_KEYS = ["refs", "ts", "ops"];

/**
 * Decode a private block's plaintext after decryption: the closed-map rule
 * (exactly `refs`, `ts` and `ops`), the same field checks `./block.ts`
 * applies to a public block's fields, and the rejection of a `rotate_key`
 * operation, which a private block's payload may never carry.
 *
 * `decode` (spec/03-encoding.md, "Deterministic CBOR") already enforces the
 * full dCBOR profile on the plaintext bytes before this function inspects
 * their shape, so a non-canonical decrypted plaintext is rejected here too —
 * this is the strict decode the task calls for.
 */
export function decodePrivatePayload(plaintext: Uint8Array): PrivatePayload {
  let value: DcborValue;
  try {
    value = decode(plaintext);
  } catch (cause) {
    if (cause instanceof DcborError) {
      throw new PrivacyError("payload", `decrypted payload: ${cause.message}`, { cause });
    }
    throw cause;
  }

  if (!(value instanceof Map)) {
    throw new PrivacyError("payload", "a private block's decrypted payload MUST be a map");
  }
  for (const key of PAYLOAD_KEYS) {
    if (!value.has(key)) {
      throw new PrivacyError(
        "payload",
        `the decrypted payload is missing the key ${JSON.stringify(key)}`,
      );
    }
  }
  for (const key of value.keys()) {
    if (!PAYLOAD_KEYS.includes(key)) {
      throw new PrivacyError(
        "payload",
        `the decrypted payload carries the key ${JSON.stringify(key)}, which is not one of refs, ts, ops`,
      );
    }
  }

  const refsValue = value.get("refs");
  if (!Array.isArray(refsValue)) {
    throw new PrivacyError("payload", "the decrypted payload's refs MUST be an array");
  }
  const refs = refsValue.map((ref, index) => {
    if (!(ref instanceof Uint8Array)) {
      throw new PrivacyError("payload", `refs[${index}] MUST be a byte string`);
    }
    return ref;
  });

  const ts = value.get("ts");
  if (typeof ts !== "bigint") {
    throw new PrivacyError("payload", "the decrypted payload's ts MUST be an unsigned integer");
  }

  const opsValue = value.get("ops");
  if (!Array.isArray(opsValue)) {
    throw new PrivacyError("payload", "the decrypted payload's ops MUST be an array");
  }
  if (opsValue.length === 0) {
    throw new PrivacyError(
      "payload",
      "a block MUST contain at least one operation (the CDDL reads [+ operation])",
    );
  }
  const ops: EntityOperation[] = [];
  for (const [index, opValue] of opsValue.entries()) {
    let op: Operation;
    try {
      op = operationFromValue(opValue);
    } catch (cause) {
      if (cause instanceof BlockError) {
        throw new PrivacyError("payload", `ops[${index}]: ${cause.message}`, { cause });
      }
      throw cause;
    }
    if (op.op === "rotate_key") {
      throw new PrivacyError(
        "payload",
        `ops[${index}] is a rotate_key operation, which may appear only in a rotation block: a private block's type is never "rotation"`,
      );
    }
    ops.push(op);
  }

  return { refs, ts, ops };
}

// ---------------------------------------------------------------------------
// AAD (spec/04, "Private block encryption")
// ---------------------------------------------------------------------------

/**
 * The plaintext fields a private block's AAD binds the ciphertext to
 * (spec/04, "Private block encryption": "all plaintext block fields
 * excluding sig, enc, and nonce"). `type` is not a parameter — it is always
 * `"private"` here, since an AAD is computed for no other block type.
 */
export interface PrivateAadFields {
  readonly v: bigint;
  readonly pub: Uint8Array;
  readonly prev: Uint8Array | null;
}

/** `aad = dCBOR({"v": ..., "type": "private", "pub": ..., "prev": ...})`. */
export function privateAadValue(fields: PrivateAadFields): DcborValue {
  const map = new Map<string, DcborValue>();
  map.set("v", fields.v);
  map.set("type", "private");
  map.set("pub", fields.pub);
  map.set("prev", fields.prev);
  return map;
}

/** The dCBOR encoding of a private block's AAD. */
export function encodePrivateAad(fields: PrivateAadFields): Uint8Array {
  return encode(privateAadValue(fields));
}

/** The AAD of an already-built private block (or its unsigned form), read off
 * the block's own plaintext fields. */
export function blockAad(block: PrivateBlock | Omit<PrivateBlock, "sig">): Uint8Array {
  return encodePrivateAad({ v: block.v, pub: block.pub, prev: block.prev });
}

// ---------------------------------------------------------------------------
// Seal and open (spec/04, "Encryption procedure" and "Decryption procedure")
// ---------------------------------------------------------------------------

/** The fields a private block's payload is built from. `v` defaults to the
 * protocol version and `refs` to the empty list, matching `./block.ts`'s
 * other field-set interfaces. */
export interface SealPrivateBlockFields {
  readonly pub: Uint8Array;
  readonly prev: Uint8Array | null;
  readonly refs?: readonly Uint8Array[];
  readonly ts: bigint | number;
  readonly ops: readonly EntityOperation[];
  readonly v?: bigint | number;
}

function toVersion(v: bigint | number | undefined): bigint {
  if (v === undefined) return PROTOCOL_VERSION;
  return typeof v === "number" ? BigInt(v) : v;
}

function toTimestamp(ts: bigint | number): bigint {
  if (typeof ts === "number") {
    if (!Number.isSafeInteger(ts)) {
      throw new PrivacyError("field", `${ts} is not a safe integer; pass a bigint`);
    }
    return BigInt(ts);
  }
  return ts;
}

/**
 * Encrypt a private block's payload (spec/04, "Encryption procedure" and the
 * worked "Encrypting a private block" example): build the plaintext, the AAD
 * from the block's plaintext fields, seal with the content key and nonce, and
 * return the unsigned block. `./block.ts`'s `signBlock` completes it.
 *
 * `nonce` MUST be unique per block under the content key (spec/04, "Nonce
 * reuse"). This function does not generate one — the caller supplies it, so
 * that the conformance vectors' fixed nonce reproduces their pinned bytes.
 */
export function sealPrivateBlock(
  fields: SealPrivateBlockFields,
  contentKey: Uint8Array,
  nonce: Uint8Array,
): Omit<PrivateBlock, "sig"> {
  checkBytes(contentKey, CONTENT_KEY_SIZE, "a content key");
  checkBytes(nonce, NONCE_SIZE, "a private block's nonce");

  const v = toVersion(fields.v);
  const payload: PrivatePayload = {
    refs: fields.refs === undefined ? [] : [...fields.refs],
    ts: toTimestamp(fields.ts),
    ops: [...fields.ops],
  };
  const aad = encodePrivateAad({ v, pub: fields.pub, prev: fields.prev });
  const plaintext = encodePrivatePayload(payload);
  const enc = xchacha20poly1305(contentKey, nonce, aad).encrypt(plaintext);

  return unsignedPrivateBlock({ pub: fields.pub, prev: fields.prev, enc, nonce, v });
}

/**
 * Decrypt a private block's payload with the chain's content key (spec/04,
 * "Decryption procedure"): recompute the AAD from the block's own plaintext
 * fields, open `enc` under `nonce`, and strict-decode the result.
 *
 * Throws {@link PrivacyError} with code `"aead"` when authentication fails —
 * wrong key, a tampered `enc` or `nonce`, or a change to any AAD-covered field
 * (`v`, `pub` or `prev`) since sealing — and with code `"payload"` when the
 * (correctly authenticated) plaintext is not itself valid dCBOR or not the
 * shape a payload must have. Per spec/04: "If the AAD does not match during
 * decryption ..., the block MUST be rejected. If decryption fails for any
 * other reason ..., the block MUST also be rejected."
 */
export function openPrivateBlock(block: PrivateBlock, contentKey: Uint8Array): PrivatePayload {
  checkBytes(contentKey, CONTENT_KEY_SIZE, "a content key");
  const aad = blockAad(block);
  let plaintext: Uint8Array;
  try {
    plaintext = xchacha20poly1305(contentKey, block.nonce, aad).decrypt(block.enc);
  } catch (cause) {
    throw new PrivacyError(
      "aead",
      "the private block did not authenticate under this content key: wrong key, or a tampered enc, nonce or AAD-covered field",
      { cause },
    );
  }
  return decodePrivatePayload(plaintext);
}
