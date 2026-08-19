/**
 * The twenty-line adapter that runs {@link ./transport.ts}'s server under
 * Node's `http`.
 *
 * The server itself is a function from a web-standard `Request` to a
 * web-standard `Response` and knows nothing about any runtime. This module is
 * the one place in `src/` that names a `node:` module, and it does so in a
 * type-only import, so nothing here reaches a browser bundle: a fetch-compatible
 * runtime (`Deno.serve`, `Bun.serve`, a Cloudflare worker, a service worker)
 * takes the handler directly and needs none of this.
 */

import type { IncomingMessage, ServerResponse } from "node:http";

/** Options for {@link nodeListener}. */
export interface NodeListenerOptions {
  /** The origin to resolve a request's path against. Defaults to the request's
   * `Host` header, or `http://localhost` when it has none. A URL is needed
   * because the web `Request` type has no relative form; nothing in this
   * profile reads the authority. */
  readonly origin?: string;
}

/**
 * Wrap a fetch-style handler as a Node request listener.
 *
 * ```ts
 * const server = new DialogServer({ store });
 * createServer(nodeListener(server.fetch)).listen(0);
 * ```
 */
export function nodeListener(
  handle: (request: Request) => Promise<Response>,
  options: NodeListenerOptions = {},
): (incoming: IncomingMessage, outgoing: ServerResponse) => void {
  return (incoming, outgoing) => {
    void (async () => {
      try {
        const response = await handle(await toRequest(incoming, options.origin));
        const headers: Record<string, string> = {};
        response.headers.forEach((value, key) => {
          headers[key] = value;
        });
        outgoing.writeHead(response.status, headers);
        if (response.body === null || incoming.method === "HEAD") {
          outgoing.end();
          return;
        }
        outgoing.end(Buffer.from(await response.arrayBuffer()));
      } catch (error) {
        outgoing.writeHead(500, { "Content-Type": "text/plain" });
        outgoing.end(error instanceof Error ? error.message : "internal error");
      }
    })();
  };
}

async function toRequest(incoming: IncomingMessage, origin?: string): Promise<Request> {
  const host = origin ?? `http://${incoming.headers.host ?? "localhost"}`;
  const url = new URL(incoming.url ?? "/", host);
  const headers = new Headers();
  for (const [name, value] of Object.entries(incoming.headers)) {
    if (value === undefined) continue;
    for (const one of Array.isArray(value) ? value : [value]) headers.append(name, one);
  }
  const method = (incoming.method ?? "GET").toUpperCase();
  if (method === "GET" || method === "HEAD") {
    return new Request(url, { method, headers });
  }
  const chunks: Buffer[] = [];
  for await (const chunk of incoming) chunks.push(chunk as Buffer);
  return new Request(url, { method, headers, body: Buffer.concat(chunks) });
}
