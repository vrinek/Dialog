/**
 * Loading `vectors/*.json` and rebuilding the JSON value model of
 * `vectors/README.md`, "The value model".
 *
 * Test-only: this is the one place `node:` imports are allowed.
 */

import { readFileSync } from "node:fs";

import { Decimal, type DcborValue } from "../src/dcbor.ts";
import { hexToBytes } from "../src/hex.ts";

export interface ValueModel {
  type: "uint" | "neg" | "text" | "bytes" | "array" | "map" | "decimal" | "null";
  number?: string;
  text?: string;
  bytes?: string;
  items?: ValueModel[];
  entries?: Array<{ key: string; value: ValueModel }>;
  exponent?: string;
  mantissa?: string;
}

export interface VectorCase {
  name: string;
  note?: string;
  rule?: string;
  reason?: string;
  value?: ValueModel;
  dcbor?: string;
  bytes?: string;
  digest?: string;
  cid?: string;
  cid_text?: string;
  kind?: string;
  type?: number | string;
  description?: string;
  template?: string;
  variables?: string[];
  // Block cases (`blocks.json`).
  author?: string;
  prev?: string | null;
  refs?: string[];
  ts?: number;
  enc?: string;
  nonce?: string;
  signing_bytes?: string;
  signing_input?: string;
  signature?: string;
  block?: string;
  blocks?: string[];
  // Chain-relative rejections (`blocks.json`, `invalid_in_chain`).
  setup?: string[];
  scan_limit?: number;
}

export interface VectorSection {
  name: string;
  description?: string;
  cases: VectorCase[];
}

/** A test key from a file's `inputs`, with its published private material. */
export interface VectorKey {
  name: string;
  seed: string;
  private_key: string;
  public_key: string;
}

export interface VectorFile {
  vectors: string;
  area: string;
  description: string;
  spec: string[];
  inputs?: { note?: string; keys?: VectorKey[] };
  sections: VectorSection[];
}

/** Load one of the committed conformance vector files. */
export function loadVectors(file: string): VectorFile {
  const url = new URL(`../../vectors/${file}`, import.meta.url);
  return JSON.parse(readFileSync(url, "utf8")) as VectorFile;
}

/** The section of that name, which must exist. */
export function section(vectors: VectorFile, name: string): VectorSection {
  const found = vectors.sections.find((s) => s.name === name);
  if (found === undefined) {
    throw new Error(`vector file has no section ${JSON.stringify(name)}`);
  }
  return found;
}

/** Rebuild a dCBOR value from the vectors' JSON value model. */
export function buildValue(model: ValueModel): DcborValue {
  switch (model.type) {
    case "uint":
    case "neg":
      return BigInt(required(model.number, "number"));
    case "text":
      return model.text ?? "";
    case "bytes":
      return hexToBytes(model.bytes ?? "");
    case "array":
      return (model.items ?? []).map(buildValue);
    case "map":
      return new Map((model.entries ?? []).map((e) => [e.key, buildValue(e.value)]));
    case "decimal":
      return new Decimal(
        BigInt(required(model.exponent, "exponent")),
        BigInt(required(model.mantissa, "mantissa")),
      );
    case "null":
      return null;
    default:
      throw new Error(`unknown value model type ${JSON.stringify(model.type)}`);
  }
}

function required(field: string | undefined, name: string): string {
  if (field === undefined) throw new Error(`value model is missing ${name}`);
  return field;
}
