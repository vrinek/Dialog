/**
 * Digests, content identifiers, and author key text.
 *
 * Implements `spec/03-encoding.md`, "Content identifiers (CIDs)", "Text
 * representation", "Internal references", "Multihash format" and "Text
 * representation of author keys" (cross-referenced from
 * `spec/04-cryptography.md`, "Key encoding").
 *
 * A digest is the raw 32-byte SHA-256 of an entity's dCBOR encoding and is the
 * only form a reference takes inside a Dialog structure. A CID is that digest
 * behind the four fixed prefix bytes `01 71 12 20` (CIDv1, dag-cbor, SHA-256,
 * 32 bytes) and is used only where an external identifier is meant; written
 * down, it is always its multibase base32 text form.
 *
 * An author's Ed25519 public key is, inside Dialog's CBOR structures, 32 raw
 * bytes with no prefix. Written down outside them it takes a text form of its
 * own, built from the same multibase base32 alphabet and code as a CID, but
 * behind the `ed25519-pub` multicodec prefix (`0xed 0x01`) rather than a CID's
 * four-byte prefix.
 *
 * Browser-safe: SHA-256 comes from @noble/hashes, not from `node:crypto`.
 */

import { sha256 } from "@noble/hashes/sha2";

/** Length of a digest: SHA-256, 32 bytes. */
export const DIGEST_SIZE = 32;
/** Length of a CID: 4 prefix bytes + 32 digest bytes. */
export const CID_SIZE = 36;
/** Length of a multihash: 2 prefix bytes + 32 digest bytes. */
export const MULTIHASH_SIZE = 34;

/** CIDv1. */
export const CID_VERSION = 0x01;
/** Content codec: dag-cbor. */
export const CODEC_DAG_CBOR = 0x71;
/** Multihash function code: SHA-256. */
export const HASH_SHA256 = 0x12;

/** The four fixed bytes every Dialog CID starts with. */
export const CID_PREFIX: Uint8Array = Uint8Array.of(
  CID_VERSION,
  CODEC_DAG_CBOR,
  HASH_SHA256,
  DIGEST_SIZE,
);

/** The class of violation a CID rejection reports. */
export type CidErrorCode =
  /** the byte string is not the fixed size a digest or CID has. */
  | "size"
  /** the CID version byte is not 1. */
  | "version"
  /** the content codec is not dag-cbor. */
  | "codec"
  /** the hash function is not SHA-256. */
  | "hash"
  /** the multihash digest length is not 32. */
  | "digest-length"
  /** the text form is not multibase base32, lowercase and unpadded. */
  | "text"
  /** the multicodec prefix of an author key's text form is not `ed25519-pub`. */
  | "multicodec";

/** A rejection by a CID constructor or parser. */
export class CidError extends Error {
  readonly code: CidErrorCode;

  constructor(code: CidErrorCode, message: string) {
    super(message);
    this.name = "CidError";
    this.code = code;
  }
}

/** The digest of an entity: SHA-256 over its dCBOR encoding. */
export function digest(dcborBytes: Uint8Array): Uint8Array {
  return sha256(dcborBytes);
}

/** The CID of an entity: `01 71 12 20 || SHA-256(dCBOR(entity))`. */
export function cid(dcborBytes: Uint8Array): Uint8Array {
  return cidFromDigest(digest(dcborBytes));
}

/** Wrap a 32-byte digest in the fixed CID prefix. */
export function cidFromDigest(digestBytes: Uint8Array): Uint8Array {
  checkDigest(digestBytes);
  const out = new Uint8Array(CID_SIZE);
  out.set(CID_PREFIX, 0);
  out.set(digestBytes, CID_PREFIX.length);
  return out;
}

/** The digest inside a CID, after checking every fixed parameter. */
export function digestFromCid(cidBytes: Uint8Array): Uint8Array {
  validateCid(cidBytes);
  return cidBytes.slice(CID_PREFIX.length);
}

/** The multihash form of a digest: `12 20 || digest` (34 bytes). */
export function multihash(digestBytes: Uint8Array): Uint8Array {
  checkDigest(digestBytes);
  const out = new Uint8Array(MULTIHASH_SIZE);
  out[0] = HASH_SHA256;
  out[1] = DIGEST_SIZE;
  out.set(digestBytes, 2);
  return out;
}

/** Check that a byte string is a 32-byte digest. */
export function checkDigest(digestBytes: Uint8Array): void {
  if (digestBytes.length !== DIGEST_SIZE) {
    throw new CidError(
      "size",
      `a digest is ${DIGEST_SIZE} bytes, got ${digestBytes.length}`,
    );
  }
}

/**
 * Check every parameter a Dialog CID fixes: 36 bytes, version 1, dag-cbor,
 * SHA-256, digest length 32. Implementations MUST reject CIDs that use
 * different parameters (spec/03-encoding.md).
 */
export function validateCid(cidBytes: Uint8Array): void {
  if (cidBytes.length !== CID_SIZE) {
    throw new CidError("size", `a CID is ${CID_SIZE} bytes, got ${cidBytes.length}`);
  }
  if (cidBytes[0] !== CID_VERSION) {
    throw new CidError(
      "version",
      `CID version must be 1, got 0x${cidBytes[0]!.toString(16)}`,
    );
  }
  if (cidBytes[1] !== CODEC_DAG_CBOR) {
    throw new CidError(
      "codec",
      `content codec must be dag-cbor (0x71), got 0x${cidBytes[1]!.toString(16)}`,
    );
  }
  if (cidBytes[2] !== HASH_SHA256) {
    throw new CidError(
      "hash",
      `hash function must be SHA-256 (0x12), got 0x${cidBytes[2]!.toString(16)}`,
    );
  }
  if (cidBytes[3] !== DIGEST_SIZE) {
    throw new CidError(
      "digest-length",
      `digest length must be 32, got ${cidBytes[3]}`,
    );
  }
}

// ---------------------------------------------------------------------------
// Text representation: multibase base32, lowercase, unpadded
// ---------------------------------------------------------------------------

/** RFC 4648 base32, lowercase — the alphabet of multibase code `b`. */
const BASE32_ALPHABET = "abcdefghijklmnopqrstuvwxyz234567";
/** The multibase prefix of the canonical CID text form. */
export const MULTIBASE_BASE32_LOWER = "b";
/** Length of the text form of a Dialog CID: `b` + 58 base32 characters. */
export const CID_TEXT_LENGTH = 59;

/** The canonical text form of a CID: `"b" || base32-lower-nopad(36 bytes)`. */
export function cidToText(cidBytes: Uint8Array): string {
  validateCid(cidBytes);
  return MULTIBASE_BASE32_LOWER + base32Encode(cidBytes);
}

/**
 * Parse the canonical text form of a CID.
 *
 * The form is case-sensitive: uppercase base32 (multibase code `B`) and padded
 * forms are rejected, as is any string whose decoded bytes fail the parameter
 * validation. Bare hex is not a CID string.
 */
export function cidFromText(text: string): Uint8Array {
  if (text.length === 0) {
    throw new CidError("text", "empty string is not a CID");
  }
  const prefix = text[0]!;
  if (prefix !== MULTIBASE_BASE32_LOWER) {
    if (prefix === "B") {
      throw new CidError(
        "text",
        "uppercase base32 (multibase code B) is not the canonical CID text form",
      );
    }
    throw new CidError(
      "text",
      `multibase prefix must be "b" (base32, lowercase, unpadded), got ${JSON.stringify(prefix)}`,
    );
  }
  if (text.length !== CID_TEXT_LENGTH) {
    throw new CidError(
      "text",
      `the text form of a Dialog CID is ${CID_TEXT_LENGTH} characters, got ${text.length}`,
    );
  }
  const bytes = base32Decode(text.slice(1));
  validateCid(bytes);
  return bytes;
}

// ---------------------------------------------------------------------------
// Text representation of author keys
// ---------------------------------------------------------------------------

/** Length of a raw Ed25519 public key: 32 bytes. */
export const AUTHOR_KEY_SIZE = 32;
/** The multicodec prefix of an author key's text form: `ed25519-pub` (`0xed`),
 * written as its unsigned varint. `0xed` is 237, greater than 127, so unlike
 * every code used in a CID it is not a single-byte varint. */
export const AUTHOR_KEY_PREFIX: Uint8Array = Uint8Array.of(0xed, 0x01);
/** Length of a prefixed author key: 2 prefix bytes + 32 key bytes. */
export const PREFIXED_AUTHOR_KEY_SIZE = AUTHOR_KEY_PREFIX.length + AUTHOR_KEY_SIZE;
/** Length of the text form of an author key: `b` + 55 base32 characters. */
export const AUTHOR_KEY_TEXT_LENGTH = 56;

/**
 * The canonical text form of an author's Ed25519 public key:
 * `"b" || base32-lower-nopad(0xed 0x01 || 32-byte key)`.
 */
export function authorKeyToText(publicKey: Uint8Array): string {
  if (publicKey.length !== AUTHOR_KEY_SIZE) {
    throw new CidError(
      "size",
      `an author's Ed25519 public key is ${AUTHOR_KEY_SIZE} bytes, got ${publicKey.length}`,
    );
  }
  const prefixed = new Uint8Array(PREFIXED_AUTHOR_KEY_SIZE);
  prefixed.set(AUTHOR_KEY_PREFIX, 0);
  prefixed.set(publicKey, AUTHOR_KEY_PREFIX.length);
  return MULTIBASE_BASE32_LOWER + base32Encode(prefixed);
}

/**
 * Parse the canonical text form of an author's Ed25519 public key, returning
 * the raw 32-byte key.
 *
 * The form is case-sensitive: uppercase base32 (multibase code `B`) and
 * padded forms are rejected, as is any string whose decoded bytes do not
 * begin with the `ed25519-pub` multicodec prefix `0xed 0x01`. Bare hex is not
 * an author key string, and a CID's text form (59 characters, `bafyrei…`) is
 * not one either — this form is 56 characters and begins `b5ua`.
 */
export function authorKeyFromText(text: string): Uint8Array {
  if (text.length === 0) {
    throw new CidError("text", "empty string is not an author key");
  }
  const prefix = text[0]!;
  if (prefix !== MULTIBASE_BASE32_LOWER) {
    if (prefix === "B") {
      throw new CidError(
        "text",
        "uppercase base32 (multibase code B) is not the canonical author key text form",
      );
    }
    throw new CidError(
      "text",
      `multibase prefix must be "b" (base32, lowercase, unpadded), got ${JSON.stringify(prefix)}`,
    );
  }
  if (text.length !== AUTHOR_KEY_TEXT_LENGTH) {
    throw new CidError(
      "text",
      `the text form of an author key is ${AUTHOR_KEY_TEXT_LENGTH} characters, got ${text.length}`,
    );
  }
  const bytes = base32Decode(text.slice(1));
  if (bytes.length !== PREFIXED_AUTHOR_KEY_SIZE) {
    throw new CidError(
      "size",
      `a prefixed author key is ${PREFIXED_AUTHOR_KEY_SIZE} bytes, got ${bytes.length}`,
    );
  }
  if (bytes[0] !== AUTHOR_KEY_PREFIX[0] || bytes[1] !== AUTHOR_KEY_PREFIX[1]) {
    throw new CidError(
      "multicodec",
      `author key must start with the ed25519-pub multicodec prefix 0xed 0x01, got 0x${bytes[0]!.toString(16)} 0x${bytes[1]!.toString(16)}`,
    );
  }
  return bytes.slice(AUTHOR_KEY_PREFIX.length);
}

function base32Encode(bytes: Uint8Array): string {
  let out = "";
  let buffer = 0;
  let bits = 0;
  for (const byte of bytes) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += BASE32_ALPHABET[(buffer >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += BASE32_ALPHABET[(buffer << (5 - bits)) & 31];
  return out;
}

function base32Decode(text: string): Uint8Array {
  const out: number[] = [];
  let buffer = 0;
  let bits = 0;
  for (let i = 0; i < text.length; i++) {
    const character = text[i]!;
    const index = BASE32_ALPHABET.indexOf(character);
    if (index < 0) {
      throw new CidError(
        "text",
        character === "="
          ? "padded base32 is not the canonical CID text form"
          : `invalid base32 character ${JSON.stringify(character)} at position ${i + 1}`,
      );
    }
    buffer = (buffer << 5) | index;
    bits += 5;
    if (bits >= 8) {
      out.push((buffer >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  if (bits >= 5) {
    throw new CidError("text", "base32 string has a superfluous trailing character");
  }
  if (bits > 0 && (buffer & ((1 << bits) - 1)) !== 0) {
    throw new CidError("text", "base32 string has non-zero padding bits");
  }
  return Uint8Array.from(out);
}
