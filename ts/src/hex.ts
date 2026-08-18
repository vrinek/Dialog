/**
 * Lowercase hexadecimal, the form every byte string takes in the
 * specification's examples and in `vectors/`.
 *
 * Hex is a byte dump, never a wire or text format: a CID written down is its
 * multibase base32 form (spec/03-encoding.md, "Text representation").
 */

/** Lowercase hex of a byte string, with no separators. */
export function bytesToHex(bytes: Uint8Array): string {
  let out = "";
  for (const b of bytes) out += b.toString(16).padStart(2, "0");
  return out;
}

/** Bytes of a hex string. Rejects odd lengths and non-hex characters; accepts
 * either case, since hex is an input convenience and not a canonical form. */
export function hexToBytes(hex: string): Uint8Array {
  if (hex.length % 2 !== 0) {
    throw new Error(`hex string has an odd length (${hex.length})`);
  }
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    const byte = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
    if (!Number.isInteger(byte) || !/^[0-9a-fA-F]{2}$/.test(hex.slice(i * 2, i * 2 + 2))) {
      throw new Error(`invalid hex at byte ${i}: ${JSON.stringify(hex.slice(i * 2, i * 2 + 2))}`);
    }
    out[i] = byte;
  }
  return out;
}
