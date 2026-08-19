/**
 * The transport profile of `spec/07-transport.md`: six operations over the
 * block sequence of {@link ./blockseq.ts}, bound to HTTP, in both directions —
 * a client that issues them against a base URL and a server that answers them
 * out of a block store.
 *
 * ## What the profile is
 *
 * Every Dialog block is self-authenticating: the signature, the author key, the
 * chain link and the content address are all inside the bytes. A source
 * therefore **cannot lie about a block's contents**; it can only **lie by
 * omission** — withholding a block, a branch of a fork, or a tip. That single
 * fact is why there is no authentication here, no session, no trusted party and
 * no client identifier: none of them would buy a client anything it does not
 * already hold inside the block.
 *
 * What is here instead is the layout of blocks in a byte stream, six ways of
 * asking for some, the obligation to verify everything on arrival, and the two
 * properties the channel genuinely cannot give — completeness and freshness —
 * left as gaps rather than papered over.
 *
 * ## Shape of this module
 *
 * - **Constants and parsing.** The paths, the media types, the three problem
 *   types, and the canonical spellings of a position and of `limit`. Exactly
 *   one spelling of each is admitted; a variant is a 400 and never normalized,
 *   and a percent-encoded octet is a second spelling like any other, so the
 *   canonical-form rules are applied to the request target **as received**.
 * - **{@link walkChain}.** The constructive definition of a tip, shared by the
 *   server (which answers `tip` and `range` from it) and the client (which
 *   finds where its own copy of a chain stops). It walks forward from the
 *   genesis position, so a store with a hole reports the block before the hole
 *   and serves a range that ends at the same block.
 * - **{@link DialogServer}.** A function from a web-standard `Request` to a
 *   web-standard `Response`, framework-free, so it runs under Node's `http`
 *   through a twenty-line adapter and in any fetch-compatible runtime.
 * - **{@link DialogClient}.** The five read operations and `announce` against a
 *   base URL, over `fetch`, with every client rule of the profile applied: each
 *   block re-hashed and validated into a {@link BlockStore}, the range property
 *   checked by the client rather than asserted by the server, a failed fetch
 *   left undecided and never invalid, and the problem details surfaced as they
 *   arrived.
 * - **{@link syncChain} and {@link syncChainFromSources}.** The multi-source
 *   rule: the same chain from two sources into one store, which is what makes a
 *   fork detectable at all.
 */

import {
  type AcceptResult,
  type Block,
  BlockError,
  type BlockStore,
  type ForkDetection,
  type StoredBlock,
} from "./block.ts";
import {
  BlockSequenceError,
  type DecodeSequenceOptions,
  type SequenceItem,
  checkRangeOrder,
  checkSiblingOrder,
  compareBytes,
  decodeBlockSequence,
  encodeBlockSequence,
} from "./blockseq.ts";
import {
  authorKeyFromText,
  authorKeyToText,
  cidFromDigest,
  cidFromText,
  cidToText,
  digestFromCid,
} from "./cid.ts";
import { bytesToHex } from "./hex.ts";

export {
  BlockSequenceError,
  type SequenceItem,
  checkRangeOrder,
  checkSiblingOrder,
  decodeBlockSequence,
  encodeBlockSequence,
} from "./blockseq.ts";

// ---------------------------------------------------------------------------
// The binding's constants
// ---------------------------------------------------------------------------

/** The default path prefix. A server MAY be mounted at any base URL, and a
 * client is configured with the whole base URL rather than with a host. The
 * `v1` names the version of the *profile*, not the protocol version in a
 * block's `v` field. */
export const DEFAULT_BASE_PATH = "/dialog/v1";

/** The media type of a block sequence. The `+cbor-seq` structured syntax
 * suffix is RFC 8742's. A server MUST NOT serve a block sequence under any
 * other type. */
export const BLOCK_SEQUENCE_TYPE = "application/dialog-blocks+cbor-seq";

/** The generic CBOR-sequence type a plain file server offering a directory of
 * chain files will send. A client MUST accept it as equivalent, since its bytes
 * are the same bytes. */
export const CBOR_SEQUENCE_TYPE = "application/cbor-seq";

/** The type of a `blocks` request body and of an `announce` receipt. */
export const JSON_TYPE = "application/json";

/** The type of every error body (RFC 9457). */
export const PROBLEM_TYPE = "application/problem+json";

/** The header carrying the CID text form of the tip a server holds, defined
 * for `tip` and `range` and for no other operation. */
export const TIP_HEADER = "Dialog-Tip";

/** This source does not hold what was asked for. Another source may hold it,
 * and this one may hold it later. */
export const PROBLEM_NOT_HELD = "urn:dialog:problem:not-held";

/** This server does not implement the OPTIONAL operation at that path. Asking
 * again will not change the answer; another server may offer it. */
export const PROBLEM_OPERATION_NOT_OFFERED = "urn:dialog:problem:operation-not-offered";

/** 403. This source takes announces and refuses **this** one, by a policy of
 * its own. Nothing was judged and nothing was stored; the blocks are not
 * implicated. Another source may take them, and this one may take them later. */
export const PROBLEM_ANNOUNCE_REFUSED = "urn:dialog:problem:announce-refused";

/** RFC 9457's "the status code and nothing more". */
export const PROBLEM_BLANK = "about:blank";

/** The digest count a conforming server MUST accept in one `blocks` request,
 * so that the scan limit's default of 256 foreign blocks fits in one
 * exchange (spec/05, "Scan limit"; spec/07, `blocks`). */
export const MIN_FETCH_DIGESTS = 256;

/** The body a `fetch` init accepts, named without depending on the DOM lib. */
type RequestBody = NonNullable<RequestInit["body"]>;

/** The six operations, named. */
export type Operation = "tip" | "range" | "block" | "blocks" | "siblings" | "announce";

// ---------------------------------------------------------------------------
// Problem details
// ---------------------------------------------------------------------------

/** An RFC 9457 problem document, as this profile uses one. */
export interface ProblemDetails {
  /** One of the profile's three types — `urn:dialog:problem:not-held`,
   * `urn:dialog:problem:operation-not-offered`,
   * `urn:dialog:problem:announce-refused` — or `about:blank`. A client MUST
   * branch on the status code and MUST work against a server that sends none of
   * the three. */
  readonly type: string;
  /** For people. A client MUST NOT parse it. */
  readonly title?: string;
  readonly status?: number;
  /** For people. A client MUST NOT parse it. */
  readonly detail?: string;
}

/** A request this profile could not answer. */
export class TransportError extends Error {
  readonly status: number;
  readonly operation?: Operation;
  /** The problem document the server sent, surfaced as it arrived. Absent when
   * the failure was local or the body was not a problem document. */
  readonly problem?: ProblemDetails;

  constructor(
    message: string,
    options: ErrorOptions & {
      readonly status: number;
      readonly operation?: Operation;
      readonly problem?: ProblemDetails;
    },
  ) {
    super(message, options);
    this.name = "TransportError";
    this.status = options.status;
    if (options.operation !== undefined) this.operation = options.operation;
    if (options.problem !== undefined) this.problem = options.problem;
  }

  /** Whether the server said it does not hold what was asked for, as opposed to
   * not offering the operation. Absence is a fact about the source and never
   * evidence that the thing does not exist. */
  get notHeld(): boolean {
    return this.status === 404 && this.problem?.type !== PROBLEM_OPERATION_NOT_OFFERED;
  }

  /** Whether the server said it does not implement the OPTIONAL operation at
   * that path. Asking again will not change the answer. */
  get operationNotOffered(): boolean {
    return this.status === 404 && this.problem?.type === PROBLEM_OPERATION_NOT_OFFERED;
  }

  /**
   * Whether the source refused this announce by a policy of its own.
   *
   * It carries **no receipt**: nothing was judged, so there are no dispositions
   * to report, and a client MUST NOT read any verdict about the announced
   * blocks from it. The blocks are not implicated — another source may take
   * them, and this one may take them once whatever policy provoked the refusal
   * has changed — which is what distinguishes it from the 404 a server that
   * does not implement `announce` at all answers.
   */
  get announceRefused(): boolean {
    return this.status === 403;
  }
}

// ---------------------------------------------------------------------------
// Canonical spellings
// ---------------------------------------------------------------------------

/**
 * Parse `limit`: one or more ASCII digits, the first of which is not `0`, with
 * no sign, no decimal point and no whitespace.
 *
 * `01`, `+1`, `1.0` and `1e3` are malformed, and so is `%201` — twice over, by
 * the whitespace rule and by the general percent-encoding rule of the HTTP
 * binding, which makes any percent-encoded octet of a value this profile
 * defines a second spelling and therefore a 400. The value is read from the
 * *raw* query string, undecoded, which is what makes the encoded variants
 * visible at all — see {@link parseQuery}.
 */
export function parseLimit(raw: string, options: { readonly max?: number } = {}): number {
  if (!/^[1-9][0-9]*$/.test(raw)) {
    throw new TransportError(`limit is not a canonical positive decimal integer: ${raw}`, {
      status: 400,
    });
  }
  const value = Number(raw);
  const max = options.max ?? 1_000_000_000;
  if (!Number.isSafeInteger(value) || value > max) {
    throw new TransportError(`limit is too large to be a plausible count of blocks: ${raw}`, {
      status: 400,
    });
  }
  return value;
}

/**
 * Parse a position: the CID text form of the block occupying it, or the
 * **genesis position**, which is named by the *absence* of the parameter.
 *
 * The literal string `null` MUST NOT be used and is rejected, as is an empty
 * value: exactly one spelling of a position is admitted, for the same reason
 * exactly one spelling of a CID is.
 */
export function parsePosition(raw: string | undefined, what: string): Uint8Array | null {
  if (raw === undefined) return null;
  try {
    return digestFromCid(cidFromText(raw));
  } catch (error) {
    throw new TransportError(`${what} is not a canonical CID text form: ${raw}`, {
      status: 400,
      cause: error,
    });
  }
}

/** The CID text form of a raw 32-byte digest, which is how a digest inside a
 * `refs` or `prev` field is written into a URL. Hexadecimal MUST NOT appear in
 * a path or a query parameter: it is a byte dump, not an identifier. */
export function digestToCidText(digest: Uint8Array): string {
  return cidToText(cidFromDigest(digest));
}

/** The digest a CID text form names. */
export function digestFromCidText(text: string): Uint8Array {
  return digestFromCid(cidFromText(text));
}

/**
 * Split a raw query string into its parameters **without percent-decoding**.
 *
 * "One spelling means one byte sequence, and percent-encoding is a second
 * spelling": every value this profile puts in a query string is drawn from an
 * alphabet that needs no encoding — base32 for a position, ASCII digits for
 * `limit` — so a request target that percent-encodes any octet of one is
 * malformed and MUST be rejected with 400. A server applies the canonical-form
 * rules to the target **as received**, not to a decoded copy of it, which is
 * what a decoder here would produce: `%201` would become a `limit` with
 * whitespace and `%62afyrei…` a second spelling of one immutable resource,
 * which is the alias the canonical text forms exist to prevent and which a
 * cache keys twice.
 *
 * A parameter the operation defines, given more than once, is malformed and is
 * reported as such by the caller, since the profile does not say which value
 * would win; every other parameter is ignored, repeated or not.
 */
export function parseQuery(search: string): Map<string, string[]> {
  const out = new Map<string, string[]>();
  const raw = search.startsWith("?") ? search.slice(1) : search;
  if (raw === "") return out;
  for (const pair of raw.split("&")) {
    if (pair === "") continue;
    const eq = pair.indexOf("=");
    const key = eq === -1 ? pair : pair.slice(0, eq);
    const value = eq === -1 ? "" : pair.slice(eq + 1);
    const values = out.get(key) ?? [];
    values.push(value);
    out.set(key, values);
  }
  return out;
}

/** One value of a parameter the operation defines, or `undefined`. A parameter
 * given more than once is a 400. */
function oneParameter(query: Map<string, string[]>, name: string): string | undefined {
  const values = query.get(name);
  if (values === undefined) return undefined;
  if (values.length > 1) {
    throw new TransportError(`the ${name} query parameter is given ${values.length} times`, {
      status: 400,
    });
  }
  return values[0];
}

// ---------------------------------------------------------------------------
// The constructive tip
// ---------------------------------------------------------------------------

/** What serving needs of a store: a block by digest, and the blocks held at a
 * position. Both are what {@link BlockStore} already answers. */
export interface ServeSource {
  get(digest: Uint8Array): StoredBlock | undefined;
  siblings(pub: Uint8Array, prev: Uint8Array | null): readonly StoredBlock[];
}

/** Which branch a source serves where it holds more than one block at a
 * position. The choice MUST be deterministic and stable per author. */
export type BranchChoice = (candidates: readonly StoredBlock[]) => StoredBlock;

/**
 * The reference branch rule: the block with the lowest digest, comparing
 * bytewise — the same order a sibling set is sorted in.
 *
 * It is a function of the blocks alone, so it is stable across requests, across
 * restarts and across two sources holding the same blocks, and it costs no
 * stored state. A source MAY choose on any other ground; what is normative is
 * determinism and stability, not this rule.
 */
export const lowestDigestBranch: BranchChoice = (candidates) => {
  let chosen = candidates[0]!;
  for (const candidate of candidates.slice(1)) {
    if (compareBytes(candidate.digest, chosen.digest) < 0) chosen = candidate;
  }
  return chosen;
};

/** Options for {@link walkChain}. */
export interface WalkOptions {
  /** Where to start. `null`, the default, is the genesis position; the block
   * naming the position is *not* included, the blocks after it are. */
  readonly after?: Uint8Array | null;
  /** The most blocks to return. */
  readonly limit?: number;
  /** Which branch to follow at a fork. Defaults to
   * {@link lowestDigestBranch}. */
  readonly chooseBranch?: BranchChoice;
}

/**
 * Walk a source's own store forward along one author's chain: the block at the
 * position, then the block whose `prev` names that block, and so on.
 *
 * This is the **constructive** definition of a tip, and both server answers are
 * read off it: the tip is the last block the walk reaches from the genesis
 * position, and a range is the walk from the requested position. Two
 * consequences fall out rather than needing to be checked:
 *
 * - A source holding no block at the genesis position holds **no tip**,
 *   whatever else of that chain it holds.
 * - A source with a **hole** has as its tip the last block before the hole, and
 *   serves a range that ends at the same block — it cannot serve across a hole
 *   or report a tip it cannot serve a range to, because the walk stops in both
 *   answers.
 *
 * The walk reads every block the source *holds*, whatever verdict the source
 * reached about it (server rule 7): it is a claim about **connectivity and not
 * about validity**, so a source that has validated none of the blocks it holds
 * still has a well-defined tip. Withholding one would be the source deciding a
 * validity question on the client's behalf, which is exactly what the client
 * rules forbid the client from delegating.
 */
export function walkChain(
  source: ServeSource,
  pub: Uint8Array,
  options: WalkOptions = {},
): StoredBlock[] {
  const choose = options.chooseBranch ?? lowestDigestBranch;
  const limit = options.limit ?? Number.POSITIVE_INFINITY;
  const out: StoredBlock[] = [];
  const seen = new Set<string>();
  let position: Uint8Array | null = options.after ?? null;

  while (out.length < limit) {
    const candidates: readonly StoredBlock[] = source.siblings(pub, position);
    if (candidates.length === 0) break;
    const chosen = choose(candidates);
    const key = bytesToHex(chosen.digest);
    if (seen.has(key)) break;
    seen.add(key);
    out.push(chosen);
    position = chosen.digest;
  }
  return out;
}

/** The tip a source holds for an author, as `tip` defines one: the last block
 * of the walk from the genesis position, or `undefined` where the walk cannot
 * start. */
export function sourceTip(
  source: ServeSource,
  pub: Uint8Array,
  chooseBranch?: BranchChoice,
): StoredBlock | undefined {
  const walked = walkChain(source, pub, chooseBranch === undefined ? {} : { chooseBranch });
  return walked.at(-1);
}

// ---------------------------------------------------------------------------
// Content negotiation
// ---------------------------------------------------------------------------

/** The media type of a `Content-Type` header, lowercased, without its
 * parameters. */
export function mediaType(header: string | null): string {
  if (header === null) return "";
  const semicolon = header.indexOf(";");
  return (semicolon === -1 ? header : header.slice(0, semicolon)).trim().toLowerCase();
}

/**
 * Whether an `Accept` header admits a media type, by RFC 9110's precedence:
 * the most specific matching range decides, and `q=0` excludes.
 *
 * An absent `Accept` admits everything.
 */
export function acceptsType(accept: string | null, type: string): boolean {
  if (accept === null || accept.trim() === "") return true;
  const [wantType, wantSubtype] = type.split("/") as [string, string];
  let best = -1;
  let quality = 0;
  for (const entry of accept.split(",")) {
    const parts = entry.split(";");
    const range = (parts[0] ?? "").trim().toLowerCase();
    if (range === "") continue;
    let q = 1;
    for (const parameter of parts.slice(1)) {
      const [name, value] = parameter.split("=");
      if ((name ?? "").trim().toLowerCase() !== "q") continue;
      const parsed = Number((value ?? "").trim());
      if (Number.isFinite(parsed)) q = parsed;
    }
    const [rangeType, rangeSubtype] = range.split("/");
    let specificity: number;
    if (range === "*/*") specificity = 0;
    else if (rangeSubtype === "*" && rangeType === wantType) specificity = 1;
    else if (rangeType === wantType && rangeSubtype === wantSubtype) specificity = 2;
    else continue;
    if (specificity > best) {
      best = specificity;
      quality = q;
    }
  }
  if (best === -1) return false;
  return quality > 0;
}

// ---------------------------------------------------------------------------
// The server
// ---------------------------------------------------------------------------

/**
 * Why a server refused an announce outright.
 *
 * A refusal by policy — quota, rate, acquaintance, disk — is **403** with the
 * problem type {@link PROBLEM_ANNOUNCE_REFUSED}, and it carries no receipt:
 * nothing was judged, so there are no dispositions to report, and a server that
 * answered 200 with every block `rejected` would be reporting a verdict it
 * never reached.
 */
export interface AnnounceRefusal {
  /** The status to answer with. Defaults to 403, which is the profile's code
   * for a refusal by policy; a server whose ground is rate or a temporary
   * condition rather than policy MAY answer 429 or 503 instead, and only the
   * 403 carries {@link PROBLEM_ANNOUNCE_REFUSED}. */
  readonly status?: number;
  /** For people. */
  readonly detail: string;
}

/** How a server is configured. */
export interface ServerOptions {
  /** The store the five read operations answer from, and `announce` writes to.
   * A {@link BlockStore} satisfies this. */
  readonly store: ServeSource;
  /** The path prefix. Defaults to {@link DEFAULT_BASE_PATH}. */
  readonly basePath?: string;
  /** Whether to offer `announce`. `announce` is OPTIONAL — a read-only mirror
   * is conforming — so this defaults to `false` and the path answers 404 with
   * `urn:dialog:problem:operation-not-offered` until it is turned on. Requires
   * a store with an `add` method. */
  readonly announce?: boolean;
  /** Which branch to serve at a fork. Whatever it is, it MUST be deterministic
   * and stable per author: `tip` and `range` MUST agree, and a source choosing
   * per request would name no single chain. Defaults to
   * {@link lowestDigestBranch}. */
  readonly chooseBranch?: BranchChoice;
  /** The largest `limit` the server honours. It MUST NOT exceed its own cap.
   * Defaults to 1024. */
  readonly maxRangeLimit?: number;
  /** How many blocks a range returns when `limit` is absent — "the server
   * chooses". Defaults to 256. */
  readonly defaultRangeLimit?: number;
  /** The most digests a `blocks` request may name. MUST be at least
   * {@link MIN_FETCH_DIGESTS}; a larger request MAY be refused with 413.
   * Defaults to 1024. */
  readonly maxFetchDigests?: number;
  /** The largest `blocks` request body, in bytes. Defaults to 1 MiB. */
  readonly maxFetchBytes?: number;
  /** The largest announce body, in bytes. Defaults to 16 MiB. */
  readonly maxAnnounceBytes?: number;
  /** Policy: refuse an announce outright, for reasons that are the server's
   * own — quota, rate, acquaintance, disk. */
  readonly refuseAnnounce?: (items: readonly SequenceItem[]) => AnnounceRefusal | undefined;
}

/** An announce receipt: what became of each submitted block, after the whole
 * sequence was processed. */
export interface AnnounceReceipt {
  /** Blocks the server validated and stored. A block it already held is
   * reported here too. */
  readonly accepted: string[];
  /** Blocks it is keeping as *stored but unvalidated*, pending their
   * ancestry. */
  readonly held: string[];
  /** Each block it refused, and why, in prose meant for a person. */
  readonly rejected: Record<string, string>;
}

interface Route {
  readonly operation: Operation | "events";
  readonly methods: readonly string[];
  readonly pub?: Uint8Array;
  readonly cid?: string;
}

/**
 * A minimal, framework-free server: a function from a web-standard `Request` to
 * a web-standard `Response`.
 *
 * It implements the five required operations and, when configured, `announce`.
 * It requires no authentication and no client identifier for the read
 * operations, which is normative rather than a convenience: a server that made
 * a client identify itself would make that client's requests linkable into a
 * durable identity, which is the opposite of what the profile's
 * subscription-privacy consideration asks for.
 *
 * **It serves what it holds, whatever verdict it has reached about it** (server
 * rule 7). No operation here consults {@link StoredBlock.valid}: a block held
 * as *stored but unvalidated* is answered by `block` and `blocks`, named by
 * `siblings`, and crossed by the `tip` and `range` walk. Withholding it would
 * cost the client a detection and save it nothing, since a client MUST validate
 * everything it receives regardless — and it bites hardest at `siblings`, where
 * the block withheld is one side of a fork the source has not been able to
 * judge, and where "I have not validated it yet" is identical on the wire to "I
 * do not have it". A server that validates nothing at all is conforming.
 */
export class DialogServer {
  private readonly store: ServeSource;
  private readonly basePath: string;
  private readonly offersAnnounce: boolean;
  private readonly chooseBranch: BranchChoice;
  private readonly maxRangeLimit: number;
  private readonly defaultRangeLimit: number;
  private readonly maxFetchDigests: number;
  private readonly maxFetchBytes: number;
  private readonly maxAnnounceBytes: number;
  private readonly refuseAnnounce: ServerOptions["refuseAnnounce"];

  constructor(options: ServerOptions) {
    this.store = options.store;
    this.basePath = trimTrailingSlash(options.basePath ?? DEFAULT_BASE_PATH);
    this.offersAnnounce = options.announce ?? false;
    this.chooseBranch = options.chooseBranch ?? lowestDigestBranch;
    this.maxRangeLimit = options.maxRangeLimit ?? 1024;
    this.defaultRangeLimit = options.defaultRangeLimit ?? 256;
    this.maxFetchDigests = options.maxFetchDigests ?? 1024;
    this.maxFetchBytes = options.maxFetchBytes ?? 1024 * 1024;
    this.maxAnnounceBytes = options.maxAnnounceBytes ?? 16 * 1024 * 1024;
    if (options.refuseAnnounce !== undefined) this.refuseAnnounce = options.refuseAnnounce;
    if (this.maxFetchDigests < MIN_FETCH_DIGESTS) {
      throw new RangeError(
        `a conforming server MUST accept a blocks request naming at least ${MIN_FETCH_DIGESTS} digests`,
      );
    }
    if (this.offersAnnounce && typeof (this.store as { add?: unknown }).add !== "function") {
      throw new TypeError("a server offering announce needs a store that can add blocks");
    }
  }

  /** Answer one request. Never throws: every failure becomes a problem
   * document. */
  async handle(request: Request): Promise<Response> {
    try {
      return await this.route(request);
    } catch (error) {
      if (error instanceof TransportError) {
        return problemResponse(error.status, error.message, error.problem?.type);
      }
      if (error instanceof BlockSequenceError) {
        return problemResponse(400, error.message);
      }
      return problemResponse(
        500,
        error instanceof Error ? error.message : "the server failed to answer",
      );
    }
  }

  /** The handler as a plain function, for anywhere that wants one. */
  get fetch(): (request: Request) => Promise<Response> {
    return (request) => this.handle(request);
  }

  private async route(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const route = this.match(url.pathname);
    if (route === undefined) {
      return problemResponse(404, "no operation of this profile is bound to that path");
    }

    const method = request.method.toUpperCase();
    const head = method === "HEAD";
    const allowed = route.methods.includes(method) || (head && route.methods.includes("GET"));
    if (!allowed) {
      // HEAD MUST be supported wherever GET is; any other method on a defined
      // path is a 405 with an Allow header.
      const allow = route.methods.includes("GET")
        ? [...route.methods, "HEAD"].join(", ")
        : route.methods.join(", ");
      const response = problemResponse(405, `${method} is not defined for that path`);
      response.headers.set("Allow", allow);
      return response;
    }

    const query = parseQuery(url.search);
    const response = await this.answer(request, route, query);
    return head ? headOf(response) : response;
  }

  private match(pathname: string): Route | undefined {
    if (pathname !== this.basePath && !pathname.startsWith(`${this.basePath}/`)) return undefined;
    const rest = pathname.slice(this.basePath.length);
    const segments = rest.split("/").filter((segment) => segment !== "");

    if (segments.length === 3 && segments[0] === "chains") {
      const pub = this.authorKey(segments[1]!);
      if (segments[2] === "tip") return { operation: "tip", methods: ["GET"], pub };
      if (segments[2] === "blocks") return { operation: "range", methods: ["GET"], pub };
      if (segments[2] === "siblings") return { operation: "siblings", methods: ["GET"], pub };
      return undefined;
    }
    if (segments.length === 2 && segments[0] === "blocks") {
      if (segments[1] === "fetch") return { operation: "blocks", methods: ["POST"] };
      return { operation: "block", methods: ["GET"], cid: segments[1]! };
    }
    if (segments.length === 1 && segments[0] === "announce") {
      return { operation: "announce", methods: ["POST"] };
    }
    if (segments.length === 1 && segments[0] === "events") {
      return { operation: "events", methods: ["GET"] };
    }
    return undefined;
  }

  /** The author key of a path segment, in the canonical text form and no
   * other: a server that accepted a variant would be minting aliases. */
  private authorKey(segment: string): Uint8Array {
    try {
      return authorKeyFromText(segment);
    } catch (error) {
      throw new TransportError(`the author key is not in the canonical text form: ${segment}`, {
        status: 400,
        cause: error,
      });
    }
  }

  private async answer(
    request: Request,
    route: Route,
    query: Map<string, string[]>,
  ): Promise<Response> {
    switch (route.operation) {
      case "tip":
        return this.tip(request, route.pub!);
      case "range":
        return this.range(request, route.pub!, query);
      case "siblings":
        return this.siblings(request, route.pub!, query);
      case "block":
        return this.block(request, route.cid!);
      case "blocks":
        return this.blocks(request);
      case "announce":
        return this.announce(request);
      case "events":
        // The event stream is OPTIONAL and this server does not offer it. A
        // client reads the 404 as a fact about this source and never as a
        // statement that the operation does not exist.
        return problemResponse(
          404,
          "this server does not offer the tip event stream",
          PROBLEM_OPERATION_NOT_OFFERED,
        );
    }
  }

  private tip(request: Request, pub: Uint8Array): Response {
    // A server that does not implement long polling MUST ignore `wait` and
    // answer immediately, which degrades to polling.
    this.requireAccepts(request, BLOCK_SEQUENCE_TYPE);
    const tip = sourceTip(this.store, pub, this.chooseBranch);
    if (tip === undefined) {
      // The author is unknown to this source, in the same sense as every other
      // 404 in this profile: it holds nothing of that chain, or nothing at its
      // start. The response carries no Dialog-Tip.
      return problemResponse(
        404,
        "this source holds no tip for that author",
        PROBLEM_NOT_HELD,
      );
    }

    const cid = digestToCidText(tip.digest);
    const etag = `"${cid}"`;
    if (ifNoneMatchSatisfied(request.headers.get("If-None-Match"), etag)) {
      const notModified = new Response(null, { status: 304 });
      notModified.headers.set("ETag", etag);
      notModified.headers.set(TIP_HEADER, cid);
      notModified.headers.set("Cache-Control", "no-cache");
      return notModified;
    }

    const response = sequenceResponse(encodeBlockSequence([tip.bytes]));
    response.headers.set("ETag", etag);
    response.headers.set(TIP_HEADER, cid);
    response.headers.set("Cache-Control", "no-cache");
    return response;
  }

  private range(request: Request, pub: Uint8Array, query: Map<string, string[]>): Response {
    this.requireAccepts(request, BLOCK_SEQUENCE_TYPE);
    const after = parsePosition(oneParameter(query, "after"), "after");
    const rawLimit = oneParameter(query, "limit");
    const requested =
      rawLimit === undefined
        ? this.defaultRangeLimit
        : parseLimit(rawLimit, { max: 1_000_000_000 });
    // A source MUST NOT return more blocks than the requested maximum, and MAY
    // return fewer for any reason, including a cap of its own.
    const limit = Math.min(requested, this.maxRangeLimit);

    const blocks = walkChain(this.store, pub, {
      after,
      limit,
      chooseBranch: this.chooseBranch,
    });
    const response = sequenceResponse(encodeBlockSequence(blocks.map((held) => held.bytes)));
    response.headers.set("Cache-Control", "no-cache");
    const tip = sourceTip(this.store, pub, this.chooseBranch);
    // Where the server holds no tip the header is omitted rather than given
    // some empty or null value: its value is a CID text form, and this profile
    // mints no second spelling of a position.
    if (tip !== undefined) response.headers.set(TIP_HEADER, digestToCidText(tip.digest));
    return response;
  }

  private siblings(request: Request, pub: Uint8Array, query: Map<string, string[]>): Response {
    this.requireAccepts(request, BLOCK_SEQUENCE_TYPE);
    const prev = parsePosition(oneParameter(query, "prev"), "prev");
    // Every block the source holds at the named position, with no winner
    // chosen, ordered by ascending digest so that two sources holding the same
    // set produce the same bytes.
    const held = [...this.store.siblings(pub, prev)].sort((a, b) =>
      compareBytes(a.digest, b.digest),
    );
    const response = sequenceResponse(encodeBlockSequence(held.map((block) => block.bytes)));
    response.headers.set("Cache-Control", "no-cache");
    return response;
  }

  private block(request: Request, segment: string): Response {
    this.requireAccepts(request, BLOCK_SEQUENCE_TYPE);
    let wanted: Uint8Array;
    try {
      wanted = digestFromCidText(segment);
    } catch (error) {
      throw new TransportError(`the CID is not in the canonical text form: ${segment}`, {
        status: 400,
        cause: error,
      });
    }
    const held = this.store.get(wanted);
    if (held === undefined) {
      return problemResponse(404, "this source does not hold that block", PROBLEM_NOT_HELD);
    }
    const response = sequenceResponse(encodeBlockSequence([held.bytes]));
    // The response for a given CID can never change, because the CID is the
    // hash of the response.
    response.headers.set("Cache-Control", "public, max-age=31536000, immutable");
    return response;
  }

  private async blocks(request: Request): Promise<Response> {
    this.requireAccepts(request, BLOCK_SEQUENCE_TYPE);
    this.requireContentType(request, [JSON_TYPE]);
    const body = await readBody(request, this.maxFetchBytes);
    let parsed: unknown;
    try {
      parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(body)) as unknown;
    } catch (error) {
      throw new TransportError("the request body is not JSON", { status: 400, cause: error });
    }
    const digests = fetchRequestDigests(parsed);
    if (digests.length > this.maxFetchDigests) {
      throw new TransportError(
        `this server accepts at most ${this.maxFetchDigests} digests in one blocks request`,
        { status: 413 },
      );
    }

    const seen = new Set<string>();
    const found: Uint8Array[] = [];
    for (const text of digests) {
      let digest: Uint8Array;
      try {
        digest = digestFromCidText(text);
      } catch (error) {
        throw new TransportError(`a digest is not a canonical CID text form: ${text}`, {
          status: 400,
          cause: error,
        });
      }
      const key = bytesToHex(digest);
      // A request MUST NOT name the same digest twice; this source rejects such
      // a request rather than answering it as though the duplicate were absent.
      if (seen.has(key)) {
        throw new TransportError(`the digest ${text} is named twice`, { status: 400 });
      }
      seen.add(key);
      const held = this.store.get(digest);
      // Digests the source does not hold are simply not in the response, and
      // the response says nothing about why.
      if (held !== undefined) found.push(held.bytes);
    }

    const response = sequenceResponse(encodeBlockSequence(found));
    // POST /blocks/fetch is the one request in this profile that looks unsafe
    // and is not: a GET with an argument list too long for a URL. It has no
    // side effects and is idempotent, so a server MAY make its response
    // cacheable — but not immutable, unlike a single block's: the answer is the
    // subset this source *holds*, and a source's store grows.
    response.headers.set("Cache-Control", "no-cache");
    return response;
  }

  private async announce(request: Request): Promise<Response> {
    if (!this.offersAnnounce) {
      return problemResponse(
        404,
        "this server does not offer announce; it is a read-only mirror",
        PROBLEM_OPERATION_NOT_OFFERED,
      );
    }
    // The equivalence of the two block-sequence types holds in both directions:
    // an announce body's Content-Type MUST be one of them, and anything else —
    // or nothing at all — is 415. Admitting the generic type is what makes "a
    // chain file offered to a server is a valid announce body" true of a file
    // whose type came from a file server rather than from a Dialog client.
    //
    // `Accept` is deliberately not evaluated here. This operation's only
    // response bodies are JSON, so there is nothing for a 406 to protect, and a
    // server enforcing it uniformly would refuse writes over a header naming a
    // type the response was never going to have — including the standing
    // `Accept: application/dialog-blocks+cbor-seq` of a client that speaks this
    // profile. 406 is defined for the five read operations.
    this.requireContentType(request, [BLOCK_SEQUENCE_TYPE, CBOR_SEQUENCE_TYPE]);
    const body = await readBody(request, this.maxAnnounceBytes);
    const items = decodeBlockSequence(body);

    const refusal = this.refuseAnnounce?.(items);
    if (refusal !== undefined) {
      // A refusal by policy is 403 and carries no receipt: nothing was judged,
      // and a client reads it as a fact about this source and about nothing
      // else. It is distinct from the 404 a server that does not implement
      // announce at all answers, which asking again will not change.
      const status = refusal.status ?? 403;
      return problemResponse(
        status,
        refusal.detail,
        status === 403 ? PROBLEM_ANNOUNCE_REFUSED : PROBLEM_BLANK,
      );
    }

    const store = this.store as ServeSource & { add(input: Uint8Array | Block): AcceptResult };
    const rejected: Record<string, string> = {};
    for (const item of items) {
      try {
        store.add(item.bytes);
      } catch (error) {
        rejected[digestToCidText(item.digest)] =
          error instanceof Error ? error.message : "the source refused the block";
      }
    }

    // A disposition is decided after the whole sequence, not as each block is
    // offered: a block settled by a later block of the same sequence is
    // reported by its final state.
    const accepted: string[] = [];
    const held: string[] = [];
    const reported = new Set<string>();
    for (const item of items) {
      const cid = digestToCidText(item.digest);
      if (reported.has(cid)) continue;
      reported.add(cid);
      if (cid in rejected) continue;
      const stored = this.store.get(item.digest);
      if (stored === undefined) {
        rejected[cid] = "the source did not keep the block";
      } else if (stored.valid) {
        accepted.push(cid);
      } else {
        held.push(cid);
      }
    }

    const receipt: AnnounceReceipt = { accepted, held, rejected };
    return jsonResponse(200, receipt);
  }

  private requireAccepts(request: Request, type: string): void {
    if (!acceptsType(request.headers.get("Accept"), type)) {
      throw new TransportError(`this server can only send ${type}`, { status: 406 });
    }
  }

  private requireContentType(request: Request, types: readonly string[]): void {
    const type = mediaType(request.headers.get("Content-Type"));
    if (!types.includes(type)) {
      throw new TransportError(
        `the request body's media type is ${type === "" ? "absent" : type}, not ${types.join(" or ")}`,
        { status: 415 },
      );
    }
  }
}

/** The server as a plain handler function. */
export function createServer(options: ServerOptions): (request: Request) => Promise<Response> {
  return new DialogServer(options).fetch;
}

function fetchRequestDigests(parsed: unknown): string[] {
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new TransportError("a blocks request body is one JSON object", { status: 400 });
  }
  const digests = (parsed as { digests?: unknown }).digests;
  if (!Array.isArray(digests)) {
    throw new TransportError("a blocks request body has a digests array", { status: 400 });
  }
  for (const entry of digests) {
    if (typeof entry !== "string") {
      throw new TransportError("every digest is the CID text form of a block", { status: 400 });
    }
  }
  return digests as string[];
}

async function readBody(request: Request, max: number): Promise<Uint8Array> {
  const declared = request.headers.get("Content-Length");
  if (declared !== null && /^[0-9]+$/.test(declared) && Number(declared) > max) {
    throw new TransportError(`the request body is larger than this server accepts (${max} bytes)`, {
      status: 413,
    });
  }
  const body = new Uint8Array(await request.arrayBuffer());
  if (body.length > max) {
    throw new TransportError(`the request body is larger than this server accepts (${max} bytes)`, {
      status: 413,
    });
  }
  return body;
}

function sequenceResponse(bytes: Uint8Array): Response {
  const response = new Response(bytes, { status: 200 });
  response.headers.set("Content-Type", BLOCK_SEQUENCE_TYPE);
  response.headers.set("Content-Length", String(bytes.length));
  return response;
}

function jsonResponse(status: number, body: unknown): Response {
  const bytes = new TextEncoder().encode(JSON.stringify(body));
  const response = new Response(bytes, { status });
  response.headers.set("Content-Type", JSON_TYPE);
  response.headers.set("Content-Length", String(bytes.length));
  return response;
}

function problemResponse(status: number, detail: string, type = PROBLEM_BLANK): Response {
  const problem: ProblemDetails = {
    type,
    title: problemTitle(status),
    status,
    detail,
  };
  const bytes = new TextEncoder().encode(JSON.stringify(problem));
  const response = new Response(bytes, { status });
  response.headers.set("Content-Type", PROBLEM_TYPE);
  response.headers.set("Content-Length", String(bytes.length));
  return response;
}

function problemTitle(status: number): string {
  switch (status) {
    case 400:
      return "Malformed request";
    case 403:
      return "Announce refused";
    case 404:
      return "Not held";
    case 405:
      return "Method not allowed";
    case 406:
      return "Not acceptable";
    case 413:
      return "Body too large";
    case 415:
      return "Unsupported media type";
    case 429:
      return "Rate limited";
    case 503:
      return "Temporarily unable";
    default:
      return "Error";
  }
}

/** A HEAD response: the headers of the GET, and no body. */
function headOf(response: Response): Response {
  return new Response(null, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  });
}

/**
 * RFC 9110's `If-None-Match`: a list of entity-tags compared *weakly*, or `*`,
 * which matches any current representation.
 */
function ifNoneMatchSatisfied(header: string | null, etag: string): boolean {
  if (header === null) return false;
  const candidates = header.split(",").map((entry) => entry.trim());
  if (candidates.includes("*")) return true;
  const opaque = weakTag(etag);
  return candidates.some((candidate) => weakTag(candidate) === opaque);
}

function weakTag(etag: string): string {
  return etag.startsWith("W/") ? etag.slice(2) : etag;
}

function trimTrailingSlash(path: string): string {
  return path.endsWith("/") && path.length > 1 ? path.slice(0, -1) : path;
}

// ---------------------------------------------------------------------------
// The client
// ---------------------------------------------------------------------------

/** How a client is configured. */
export interface ClientOptions {
  /** The whole base URL, learned out of band — a client is configured with
   * this rather than with a host, since a server MAY be mounted anywhere. */
  readonly baseUrl: string;
  /** The store every received block is validated into. Optional: a caller that
   * wants the bytes without the L1 bookkeeping can leave it out, and validate
   * itself. */
  readonly store?: BlockStore;
  /** The fetch implementation. Defaults to the runtime's. */
  readonly fetch?: typeof globalThis.fetch;
  /** Headers added to every request. */
  readonly headers?: Readonly<Record<string, string>>;
  /** The most bytes the client will read from one response. A response body
   * can be arbitrarily large, and a client MUST bound what it reads. Defaults
   * to 32 MiB. */
  readonly maxResponseBytes?: number;
  /** The most blocks the client will read from one response. Defaults to
   * 65536. */
  readonly maxResponseBlocks?: number;
  /** A label for this source, used when several are compared. Defaults to the
   * base URL. */
  readonly label?: string;
}

/** What a store did with the blocks of one response. */
export interface IngestReport {
  readonly accepted: Uint8Array[];
  /** Blocks the store could not decide about: their ancestry has not arrived,
   * or a block their references resolve through has not. Undecided is not
   * invalid. */
  readonly held: Uint8Array[];
  /** Blocks the store found invalid, with the reason. A source cannot make a
   * valid block invalid, so this is always the block's own fault. */
  readonly rejected: { readonly digest: Uint8Array; readonly reason: string }[];
}

/** The answer to `tip`. */
export interface TipResponse {
  /** `block` — here is the tip; `not-held` — this source holds no tip for that
   * author, which is a fact about the source and nothing else; `not-modified` —
   * the value the client already holds is current. */
  readonly status: "block" | "not-held" | "not-modified";
  readonly item?: SequenceItem;
  /** The digest of the block returned, computed from its bytes. The source
   * cannot misreport a tip's identity; it can only choose which tip to show. */
  readonly digest?: Uint8Array;
  readonly etag?: string;
  /** Whether the `ETag` names the block that arrived — verified rather than
   * believed. */
  readonly etagVerified?: boolean;
  /** The `Dialog-Tip` the response carried, as a digest. */
  readonly declaredTip?: Uint8Array;
  readonly ingested?: IngestReport;
  readonly problem?: ProblemDetails;
}

/** The answer to `range`. */
export interface RangeResponse {
  readonly items: SequenceItem[];
  /** The `Dialog-Tip` the response carried, as a digest. Absent means "this
   * source claims no tip for this author", which is not an error. */
  readonly declaredTip?: Uint8Array;
  /**
   * Whether this range ended at the tip the source reports, rather than at a
   * limit. It is what the client compares to decide whether to ask for more,
   * and it is a **claim, not evidence**: a server withholding its newest blocks
   * reports the older tip here too, and nothing detects that.
   */
  readonly caughtUp: boolean;
  readonly ingested?: IngestReport;
}

/** The answer to `block`. */
export interface BlockResponse {
  readonly item?: SequenceItem;
  /** Why there is no block. `not-held` is a fact about this source; a
   * `digest-mismatch` response is a failed fetch and not a block. Neither is a
   * finding that anything is invalid. */
  readonly failed?: "not-held" | "digest-mismatch";
  readonly ingested?: IngestReport;
  readonly problem?: ProblemDetails;
}

/** The answer to `blocks`. */
export interface BlocksResponse {
  readonly items: SequenceItem[];
  /** The digests this source did not return. They were not scanned, so they
   * cost nothing against the scan limit, and they are not evidence of
   * anything. */
  readonly missing: Uint8Array[];
  /** Blocks the source returned that were not asked for; kept out of the
   * answer. */
  readonly unrequested: Uint8Array[];
  /** Whether the source returned the blocks in the order the request named
   * them, as the profile requires. A client identifies each block by re-hashing
   * it, so a violation costs nothing but is worth knowing. */
  readonly orderRespected: boolean;
  readonly ingested?: IngestReport;
}

/** The answer to `siblings`. */
export interface SiblingsResponse {
  readonly items: SequenceItem[];
  readonly ingested?: IngestReport;
}

/** The answer to `announce`. */
export interface AnnounceResponse {
  readonly status: number;
  /** Absent or incomplete on a 202, whose receipt is incomplete or absent by
   * definition. */
  readonly receipt?: AnnounceReceipt;
  readonly accepted: Uint8Array[];
  readonly held: Uint8Array[];
  readonly rejected: { readonly digest: Uint8Array; readonly reason: string }[];
}

/** Per-request options every operation takes. */
export interface RequestOptions {
  readonly signal?: AbortSignal;
}

/**
 * A client of this profile against one source.
 *
 * Every client rule of the profile is applied here rather than left to the
 * caller: each block is re-hashed and identified by the digest the client
 * computes, never by the position it held in a sequence or the URL it came
 * from; the range property is checked by the client rather than asserted by the
 * server; a fetch that fails leaves a verdict undecided and never invalid; and
 * transport-level authenticity is never taken for validation, because TLS
 * protects the request pattern and not the data.
 */
export class DialogClient {
  readonly baseUrl: string;
  readonly label: string;
  private readonly store: BlockStore | undefined;
  private readonly doFetch: typeof globalThis.fetch;
  private readonly headers: Readonly<Record<string, string>>;
  private readonly bounds: DecodeSequenceOptions;

  constructor(options: ClientOptions) {
    this.baseUrl = trimTrailingSlash(options.baseUrl);
    this.label = options.label ?? this.baseUrl;
    this.store = options.store;
    this.doFetch = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.headers = options.headers ?? {};
    this.bounds = {
      maxBytes: options.maxResponseBytes ?? 32 * 1024 * 1024,
      maxBlocks: options.maxResponseBlocks ?? 65536,
    };
  }

  /** `GET {base}/chains/{author}/tip` — "has this chain moved?" */
  async tip(
    pub: Uint8Array,
    options: RequestOptions & {
      /** The `ETag` of the tip the client already holds; a match answers 304. */
      readonly ifNoneMatch?: string;
      /** Long polling. A server that does not implement it ignores the
       * parameter and answers immediately, which degrades to polling. */
      readonly wait?: number;
    } = {},
  ): Promise<TipResponse> {
    const query =
      options.wait === undefined ? "" : `?wait=${encodeCanonicalCount(options.wait, "wait")}`;
    const headers: Record<string, string> = { Accept: BLOCK_SEQUENCE_TYPE };
    if (options.ifNoneMatch !== undefined) headers["If-None-Match"] = options.ifNoneMatch;
    const response = await this.request(
      "tip",
      `/chains/${authorKeyToText(pub)}/tip${query}`,
      { headers },
      options.signal,
    );

    if (response.status === 304) {
      const etag = response.headers.get("ETag");
      return {
        status: "not-modified",
        ...(etag === null ? {} : { etag }),
        ...declaredTipOf(response),
      };
    }
    if (response.status === 404) {
      const problem = await problemOf(response);
      return { status: "not-held", ...(problem === undefined ? {} : { problem }) };
    }
    await this.expectOk(response, "tip");

    const items = await this.readSequence(response, "tip");
    if (items.length !== 1) {
      throw new TransportError(`a tip response carries one block, not ${items.length}`, {
        status: response.status,
        operation: "tip",
      });
    }
    const item = items[0]!;
    const etag = response.headers.get("ETag");
    const cid = digestToCidText(item.digest);
    return {
      status: "block",
      item,
      digest: item.digest,
      ...(etag === null ? {} : { etag, etagVerified: weakTag(etag) === `"${cid}"` }),
      ...declaredTipOf(response),
      ...this.ingest(items),
    };
  }

  /** `GET {base}/chains/{author}/blocks?after={cid}&limit={n}` — a contiguous
   * range beginning at the block after the position. The position is
   * exclusive, and `null` is the genesis position. */
  async range(
    pub: Uint8Array,
    after: Uint8Array | null = null,
    options: RequestOptions & { readonly limit?: number } = {},
  ): Promise<RangeResponse> {
    const parameters: string[] = [];
    if (after !== null) parameters.push(`after=${digestToCidText(after)}`);
    if (options.limit !== undefined) {
      parameters.push(`limit=${encodeCanonicalCount(options.limit, "limit")}`);
    }
    const query = parameters.length === 0 ? "" : `?${parameters.join("&")}`;
    const response = await this.request(
      "range",
      `/chains/${authorKeyToText(pub)}/blocks${query}`,
      { headers: { Accept: BLOCK_SEQUENCE_TYPE } },
      options.signal,
    );
    await this.expectOk(response, "range");

    const items = await this.readSequence(response, "range");
    // The range property is the client's own work: a source that skips a block
    // produces a break the client sees immediately.
    checkRangeOrder(items, { pub, after });
    const declared = declaredTipOf(response);
    const last = items.at(-1);
    const caughtUp =
      declared.declaredTip === undefined
        ? items.length === 0
        : last !== undefined && bytesEqual(last.digest, declared.declaredTip);
    return { items, ...declared, caughtUp, ...this.ingest(items) };
  }

  /** `GET {base}/blocks/{cid}` — one block, for demand-driven resolution of one
   * `refs` entry. */
  async block(digest: Uint8Array, options: RequestOptions = {}): Promise<BlockResponse> {
    const response = await this.request(
      "block",
      `/blocks/${digestToCidText(digest)}`,
      { headers: { Accept: BLOCK_SEQUENCE_TYPE } },
      options.signal,
    );
    if (response.status === 404) {
      // A 404 for a refs entry is a fetch that did not succeed; it is not a
      // finding that the reference is unresolvable.
      const problem = await problemOf(response);
      return { failed: "not-held", ...(problem === undefined ? {} : { problem }) };
    }
    await this.expectOk(response, "block");

    const items = await this.readSequence(response, "block");
    if (items.length !== 1) {
      throw new TransportError(`a block response carries one block, not ${items.length}`, {
        status: response.status,
        operation: "block",
      });
    }
    const item = items[0]!;
    // A response whose bytes hash to something other than the requested digest
    // is a failed fetch, not a block.
    if (!bytesEqual(item.digest, digest)) return { failed: "digest-mismatch" };
    return { item, ...this.ingest(items) };
  }

  /** `POST {base}/blocks/fetch` — the subset of a digest list the source holds.
   * The scan limit's default of 256 is a count of blocks and not of round
   * trips, so a client resolving a block's `refs` SHOULD ask once. */
  async blocks(
    digests: readonly Uint8Array[],
    options: RequestOptions = {},
  ): Promise<BlocksResponse> {
    const wanted: Uint8Array[] = [];
    const seen = new Set<string>();
    for (const digest of digests) {
      const key = bytesToHex(digest);
      // A request MUST NOT name the same digest twice.
      if (seen.has(key)) continue;
      seen.add(key);
      wanted.push(digest);
    }
    if (wanted.length === 0) {
      return { items: [], missing: [], unrequested: [], orderRespected: true };
    }

    const body = JSON.stringify({ digests: wanted.map(digestToCidText) });
    const response = await this.request(
      "blocks",
      "/blocks/fetch",
      {
        method: "POST",
        headers: { Accept: BLOCK_SEQUENCE_TYPE, "Content-Type": JSON_TYPE },
        body,
      },
      options.signal,
    );
    await this.expectOk(response, "blocks");

    const returned = await this.readSequence(response, "blocks");
    const wantedKeys = wanted.map((digest) => bytesToHex(digest));
    const items: SequenceItem[] = [];
    const unrequested: Uint8Array[] = [];
    const received = new Set<string>();
    let cursor = 0;
    let orderRespected = true;
    for (const item of returned) {
      const key = bytesToHex(item.digest);
      const index = wantedKeys.indexOf(key);
      if (index === -1) {
        unrequested.push(item.digest);
        continue;
      }
      if (index < cursor) orderRespected = false;
      cursor = index;
      if (received.has(key)) continue;
      received.add(key);
      items.push(item);
    }
    const missing = wanted.filter((digest) => !received.has(bytesToHex(digest)));
    return { items, missing, unrequested, orderRespected, ...this.ingest(items) };
  }

  /** `GET {base}/chains/{author}/siblings?prev={cid}` — every block this source
   * holds at that position. A one-member set is not a statement that the chain
   * does not fork. */
  async siblings(
    pub: Uint8Array,
    prev: Uint8Array | null = null,
    options: RequestOptions = {},
  ): Promise<SiblingsResponse> {
    const query = prev === null ? "" : `?prev=${digestToCidText(prev)}`;
    const response = await this.request(
      "siblings",
      `/chains/${authorKeyToText(pub)}/siblings${query}`,
      { headers: { Accept: BLOCK_SEQUENCE_TYPE } },
      options.signal,
    );
    await this.expectOk(response, "siblings");
    const items = await this.readSequence(response, "siblings");
    checkSiblingOrder(items, { pub, prev });
    return { items, ...this.ingest(items) };
  }

  /** `POST {base}/announce` — the one operation that moves blocks toward a
   * source. It carries no authority in either direction: the announcer asserts
   * nothing a block does not already say, and the source endorses nothing by
   * accepting one. */
  async announce(
    blocks: Iterable<Block | Uint8Array | SequenceItem>,
    options: RequestOptions = {},
  ): Promise<AnnounceResponse> {
    const body = encodeBlockSequence(blocks);
    const response = await this.request(
      "announce",
      "/announce",
      {
        method: "POST",
        headers: { Accept: JSON_TYPE, "Content-Type": BLOCK_SEQUENCE_TYPE },
        body,
      },
      options.signal,
    );
    await this.expectOk(response, "announce");

    if (response.status === 202) {
      const receipt = await optionalReceipt(response);
      return {
        status: 202,
        ...(receipt === undefined ? {} : { receipt }),
        ...receiptDigests(receipt),
      };
    }
    const receipt = await optionalReceipt(response);
    if (receipt === undefined) {
      throw new TransportError("the announce receipt is not a JSON object", {
        status: response.status,
        operation: "announce",
      });
    }
    return { status: response.status, receipt, ...receiptDigests(receipt) };
  }

  private async request(
    operation: Operation,
    path: string,
    init: { method?: string; headers?: Record<string, string>; body?: RequestBody },
    signal: AbortSignal | undefined,
  ): Promise<Response> {
    const headers = { ...this.headers, ...(init.headers ?? {}) };
    try {
      return await this.doFetch(`${this.baseUrl}${path}`, {
        method: init.method ?? "GET",
        headers,
        ...(init.body === undefined ? {} : { body: init.body }),
        ...(signal === undefined ? {} : { signal }),
      });
    } catch (error) {
      throw new TransportError(
        `the ${operation} request to ${this.label} did not complete: ${
          error instanceof Error ? error.message : String(error)
        }`,
        { status: 0, operation, cause: error },
      );
    }
  }

  private async expectOk(response: Response, operation: Operation): Promise<void> {
    if (response.ok) return;
    const problem = await problemOf(response);
    throw new TransportError(
      `${operation} answered ${response.status}${problem?.detail === undefined ? "" : `: ${problem.detail}`}`,
      {
        status: response.status,
        operation,
        ...(problem === undefined ? {} : { problem }),
      },
    );
  }

  private async readSequence(response: Response, operation: Operation): Promise<SequenceItem[]> {
    const type = mediaType(response.headers.get("Content-Type"));
    // A client MUST accept application/cbor-seq as equivalent, since a plain
    // file server offering a directory of chain files sends the generic type
    // and its bytes are the same bytes.
    if (type !== BLOCK_SEQUENCE_TYPE && type !== CBOR_SEQUENCE_TYPE) {
      throw new TransportError(
        `${operation} answered ${type === "" ? "no media type" : type}, not a block sequence`,
        { status: response.status, operation },
      );
    }
    const bytes = new Uint8Array(await response.arrayBuffer());
    return decodeBlockSequence(bytes, this.bounds);
  }

  /** Validate every block into the store, in the order it arrived, and report
   * what the store made of each. */
  private ingest(items: readonly SequenceItem[]): { ingested?: IngestReport } {
    if (this.store === undefined) return {};
    return { ingested: ingestBlocks(this.store, items) };
  }
}

/**
 * Offer blocks to a store in order, and report what became of each.
 *
 * A block the store cannot decide about is *held*, never rejected: neither a
 * block a source withholds nor a key this node was not given can make a block
 * invalid. A block the store finds invalid is the block's own fault, and is
 * reported without stopping the rest.
 */
export function ingestBlocks(store: BlockStore, items: readonly SequenceItem[]): IngestReport {
  const accepted: Uint8Array[] = [];
  const held: Uint8Array[] = [];
  const rejected: { digest: Uint8Array; reason: string }[] = [];
  for (const item of items) {
    try {
      const result = store.add(item.bytes);
      if (result.status === "unvalidated") held.push(item.digest);
      else accepted.push(item.digest);
    } catch (error) {
      rejected.push({
        digest: item.digest,
        reason: error instanceof BlockError ? `${error.code}: ${error.message}` : String(error),
      });
    }
  }
  return { accepted, held, rejected };
}

function declaredTipOf(response: Response): { declaredTip?: Uint8Array } {
  const header = response.headers.get(TIP_HEADER);
  // An absent Dialog-Tip means "this source claims no tip for this author". A
  // client MUST NOT treat it as an error or as a protocol violation.
  if (header === null) return {};
  try {
    return { declaredTip: digestFromCidText(header.trim()) };
  } catch {
    // The header is a claim and nothing more; a client MUST NOT act on it
    // except to decide whether to ask for more, so an unreadable one is
    // dropped rather than made fatal.
    return {};
  }
}

async function problemOf(response: Response): Promise<ProblemDetails | undefined> {
  const type = mediaType(response.headers.get("Content-Type"));
  if (type !== PROBLEM_TYPE && type !== JSON_TYPE) return undefined;
  try {
    const parsed = (await response.json()) as Record<string, unknown>;
    if (typeof parsed !== "object" || parsed === null) return undefined;
    const problem: ProblemDetails = {
      type: typeof parsed["type"] === "string" ? parsed["type"] : PROBLEM_BLANK,
      ...(typeof parsed["title"] === "string" ? { title: parsed["title"] } : {}),
      ...(typeof parsed["status"] === "number" ? { status: parsed["status"] } : {}),
      ...(typeof parsed["detail"] === "string" ? { detail: parsed["detail"] } : {}),
    };
    return problem;
  } catch {
    return undefined;
  }
}

async function optionalReceipt(response: Response): Promise<AnnounceReceipt | undefined> {
  if (mediaType(response.headers.get("Content-Type")) !== JSON_TYPE) return undefined;
  let parsed: unknown;
  try {
    parsed = (await response.json()) as unknown;
  } catch {
    return undefined;
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return undefined;
  const raw = parsed as Record<string, unknown>;
  const accepted = Array.isArray(raw["accepted"]) ? (raw["accepted"] as unknown[]) : [];
  const held = Array.isArray(raw["held"]) ? (raw["held"] as unknown[]) : [];
  const rejected =
    typeof raw["rejected"] === "object" && raw["rejected"] !== null
      ? (raw["rejected"] as Record<string, unknown>)
      : {};
  return {
    accepted: accepted.filter((entry): entry is string => typeof entry === "string"),
    held: held.filter((entry): entry is string => typeof entry === "string"),
    rejected: Object.fromEntries(
      Object.entries(rejected).map(([key, value]) => [key, String(value)]),
    ),
  };
}

function receiptDigests(receipt: AnnounceReceipt | undefined): {
  accepted: Uint8Array[];
  held: Uint8Array[];
  rejected: { digest: Uint8Array; reason: string }[];
} {
  if (receipt === undefined) return { accepted: [], held: [], rejected: [] };
  return {
    accepted: receipt.accepted.flatMap(safeDigest),
    held: receipt.held.flatMap(safeDigest),
    rejected: Object.entries(receipt.rejected).flatMap(([cid, reason]) =>
      safeDigest(cid).map((digest) => ({ digest, reason })),
    ),
  };
}

function safeDigest(cid: string): Uint8Array[] {
  try {
    return [digestFromCidText(cid)];
  } catch {
    return [];
  }
}

/** A count in a URL has the same one spelling a server admits. */
function encodeCanonicalCount(value: number, what: string): string {
  if (!Number.isSafeInteger(value) || value < 1) {
    throw new TransportError(`${what} must be a positive integer, not ${value}`, { status: 0 });
  }
  return String(value);
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  return a.length === b.length && compareBytes(a, b) === 0;
}

// ---------------------------------------------------------------------------
// Sync
// ---------------------------------------------------------------------------

/** What one chain sync from one source did. */
export interface ChainSyncResult {
  readonly source: string;
  readonly pub: Uint8Array;
  /** How many range requests it took. */
  readonly requests: number;
  readonly received: number;
  readonly accepted: number;
  /** Blocks the store could not decide about. Undecided is never invalid. */
  readonly held: number;
  readonly rejected: { readonly digest: Uint8Array; readonly reason: string }[];
  /**
   * Whether the store now holds the block this source names as its tip — a
   * claim, not evidence, since a server withholding its newest blocks reports
   * the older tip here too. A source claiming no tip is caught up vacuously:
   * there is nothing more it will give.
   */
  readonly caughtUp: boolean;
  /** Whether the sync had to go back to the genesis position because the source
   * named a tip the store could not reach forward from its own — the shape a
   * fork, or a source ahead on another branch, takes at the client. */
  readonly rescanned: boolean;
  /** The tip this source claimed, as a digest. */
  readonly declaredTip?: Uint8Array;
  /** Where the store's own copy of the chain now ends, walked constructively
   * from the genesis position. */
  readonly localTip?: Uint8Array;
}

/** Options for {@link syncChain}. */
export interface SyncOptions {
  /** Blocks per range request. */
  readonly pageSize?: number;
  /** A bound on the number of range requests, so that a source feeding an
   * endless chain cannot hold the client forever. Defaults to 1024. */
  readonly maxRequests?: number;
  readonly signal?: AbortSignal;
}

/**
 * Sync one author chain from one source into a store: `range` from where the
 * store's own copy ends, repeated with the position set to the digest of the
 * last block received, until the source's tip is reached or it stops.
 *
 * The continuation needs no cursor, no session and no server-side state — the
 * position *is* the digest of a block the client holds — so a client that stops
 * and resumes a week later on a different machine resumes correctly.
 *
 * ## When the source's tip is not reachable from the client's position
 *
 * A range that comes back empty means one of two things, and the emptiness does
 * not distinguish them: the client is already at the tip, or the source's store
 * stops there. The `Dialog-Tip` comparison settles the first. The case the
 * profile leaves unwritten is the third one that falls out of the same two
 * answers — an **empty range whose `Dialog-Tip` names a block the client does
 * not hold**, which is what a source on the other branch of a fork looks like
 * from a client that synced the first branch, and what a source ahead on a
 * chain the client holds a different version of looks like too.
 *
 * This implementation then re-asks from the **genesis position**, once, which
 * is the move the profile's own worked example makes ("the client tells them
 * apart by fetching the range from the second source too"): the second source's
 * range is either a prefix of the first's, an extension of it, or a walk that
 * diverges at some position — and in the third case the divergent blocks land
 * in the store, where validation rule 9 fires on them. The alternative, walking
 * the client's own chain backwards asking `siblings` at each position, costs a
 * request per block instead of one. See todos/093.
 */
export async function syncChain(
  client: DialogClient,
  store: BlockStore,
  pub: Uint8Array,
  options: SyncOptions = {},
): Promise<ChainSyncResult> {
  const maxRequests = options.maxRequests ?? 1024;
  let position = sourceTip(store, pub)?.digest ?? null;
  let requests = 0;
  let received = 0;
  let accepted = 0;
  let held = 0;
  const rejected: { digest: Uint8Array; reason: string }[] = [];
  let declaredTip: Uint8Array | undefined;
  let rescanned = false;

  while (requests < maxRequests) {
    const page = await client.range(pub, position, {
      ...(options.pageSize === undefined ? {} : { limit: options.pageSize }),
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    });
    requests++;
    if (page.declaredTip !== undefined) declaredTip = page.declaredTip;
    received += page.items.length;
    const report = page.ingested ?? ingestBlocks(store, page.items);
    accepted += report.accepted.length;
    held += report.held.length;
    rejected.push(...report.rejected);

    if (page.items.length > 0 && !page.caughtUp) {
      position = page.items.at(-1)!.digest;
      continue;
    }
    // The source named a tip this store cannot reach from where it was asking.
    if (
      !rescanned &&
      position !== null &&
      declaredTip !== undefined &&
      !store.has(declaredTip)
    ) {
      rescanned = true;
      position = null;
      continue;
    }
    break;
  }

  const localTip = sourceTip(store, pub)?.digest;
  return {
    source: client.label,
    pub,
    requests,
    received,
    accepted,
    held,
    rejected,
    // A source claiming no tip has nothing more to give, so it is caught up
    // vacuously; otherwise the question is whether the store now holds the
    // block the source named.
    caughtUp: declaredTip === undefined || store.has(declaredTip),
    rescanned,
    ...(declaredTip === undefined ? {} : { declaredTip }),
    ...(localTip === undefined ? {} : { localTip }),
  };
}

/** What the same chain from several sources revealed. */
export interface MultiSourceSyncResult {
  readonly pub: Uint8Array;
  readonly perSource: ChainSyncResult[];
  /** Whether every source that claimed a tip claimed the same one. Disagreement
   * means one source is behind, one is on another branch, or one is lying by
   * omission — and the first and third are indistinguishable. */
  readonly tipsAgree: boolean;
  /** Every fork the union of these sources revealed at the client. A source
   * serving one branch of a fork has every incentive not to admit to it, so
   * this is where rule 9 actually fires. */
  readonly forks: ForkDetection[];
  /** Sources that failed, and how. A source that did not answer says nothing
   * about the chain. */
  readonly failures: { readonly source: string; readonly reason: string }[];
}

/**
 * The multi-source rule: obtain a chain from more than one source, into one
 * store, and compare.
 *
 * **Fork detection is a reachability property, not a query.** Validation rule 9
 * requires a node to detect a fork when it *holds* two blocks with the same
 * `prev` from the same author; a node that only ever hears one version of a
 * chain satisfies it vacuously and forever. Two sources with different branches
 * produce a fork at the client even when neither admits to one, which is why
 * this function syncs them all into the same store and reads the forks off it.
 *
 * The `siblings` query at each divergent position is issued too, since that is
 * where an honest source names the whole set.
 */
export async function syncChainFromSources(
  clients: readonly DialogClient[],
  store: BlockStore,
  pub: Uint8Array,
  options: SyncOptions = {},
): Promise<MultiSourceSyncResult> {
  const before = store.forks.length;
  const perSource: ChainSyncResult[] = [];
  const failures: { source: string; reason: string }[] = [];

  for (const client of clients) {
    try {
      perSource.push(await syncChain(client, store, pub, options));
    } catch (error) {
      failures.push({
        source: client.label,
        reason: error instanceof Error ? error.message : String(error),
      });
    }
  }

  // Where the sources disagree, ask each of them the divergent position
  // directly: a source that holds both sides names both, and the union of
  // one-sided answers is a fork at the client either way.
  const forks = store.forks.slice(before);
  for (const fork of forks) {
    for (const client of clients) {
      try {
        await client.siblings(pub, fork.prev, {
          ...(options.signal === undefined ? {} : { signal: options.signal }),
        });
      } catch {
        // A source that cannot answer says nothing about the fork.
      }
    }
  }

  const claimed = perSource.flatMap((result) =>
    result.declaredTip === undefined ? [] : [result.declaredTip],
  );
  const tipsAgree = claimed.every((tip) => bytesEqual(tip, claimed[0]!));
  return { pub, perSource, tipsAgree, forks: store.forks.slice(before), failures };
}

/** What a batch resolution of `refs` obtained. */
export interface ResolveResult {
  /** Digests now in the store. */
  readonly resolved: Uint8Array[];
  /**
   * Digests no source returned. A digest nobody returned was **not scanned**,
   * so it costs nothing against the scan limit, and the block that named it has
   * not been shown invalid — it is *stored but unvalidated*, which is a third
   * outcome and not a rejection.
   */
  readonly unresolved: Uint8Array[];
  /** The sources tried, and what each contributed. */
  readonly perSource: { readonly source: string; readonly returned: number }[];
}

/**
 * Resolve a block's `refs` by batch fetch: one `blocks` request per source
 * naming every digest still needed, rather than a `block` request per digest.
 *
 * A client SHOULD retry an unresolved digest from another source, which is what
 * passing more than one client does. Nothing here ever reports a block as
 * invalid on the strength of a fetch that failed.
 */
export async function resolveReferences(
  clients: readonly DialogClient[],
  store: BlockStore,
  digests: readonly Uint8Array[],
  options: RequestOptions = {},
): Promise<ResolveResult> {
  const wanted = new Map<string, Uint8Array>();
  for (const digest of digests) {
    if (store.has(digest)) continue;
    wanted.set(bytesToHex(digest), digest);
  }
  const perSource: { source: string; returned: number }[] = [];

  for (const client of clients) {
    if (wanted.size === 0) break;
    let response: BlocksResponse;
    try {
      response = await client.blocks([...wanted.values()], options);
    } catch {
      perSource.push({ source: client.label, returned: 0 });
      continue;
    }
    if (response.ingested === undefined) ingestBlocks(store, response.items);
    for (const item of response.items) wanted.delete(bytesToHex(item.digest));
    perSource.push({ source: client.label, returned: response.items.length });
  }

  const unresolved = [...wanted.values()];
  const resolved = digests.filter((digest) => !wanted.has(bytesToHex(digest)));
  return { resolved, unresolved, perSource };
}
