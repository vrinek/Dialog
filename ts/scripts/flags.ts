/**
 * The command-line parsing the two interop scripts share.
 *
 * `interop/README.md` gives both programs the same flags in the same spelling
 * as the Go binaries they are run against — a single leading dash, a value in
 * the next argument or after an `=`. Nothing here is protocol; it exists so that
 * the harness is a table rather than a special case per implementation, and a
 * flag it does not recognize is an error rather than something to guess at.
 */

import process from "node:process";

/** What a command line amounted to: every value of every flag, in the order it
 * was given, and the boolean flags that were present. */
export interface Flags {
  readonly values: Map<string, string[]>;
  readonly boolean: Set<string>;
}

/** Parse `-name value`, `-name=value` and, for the flags named as boolean,
 * `-name` alone. A doubled dash is admitted as the same flag, since both
 * spellings reach Go's `flag` package unchanged. */
export function parseFlags(
  argv: readonly string[],
  options: { readonly boolean?: readonly string[] } = {},
): Flags {
  const flags: Flags = { values: new Map(), boolean: new Set() };
  const booleans = new Set(options.boolean ?? []);

  for (let index = 0; index < argv.length; index++) {
    const argument = argv[index]!;
    if (!argument.startsWith("-")) fail(`unexpected argument: ${argument}`);
    const body = argument.startsWith("--") ? argument.slice(2) : argument.slice(1);
    const equals = body.indexOf("=");
    const name = equals === -1 ? body : body.slice(0, equals);
    if (name === "") fail(`unexpected argument: ${argument}`);

    if (booleans.has(name)) {
      flags.boolean.add(name);
      continue;
    }
    let value: string;
    if (equals !== -1) {
      value = body.slice(equals + 1);
    } else {
      const next = argv[index + 1];
      if (next === undefined) fail(`-${name} takes a value`);
      value = next;
      index++;
    }
    const values = flags.values.get(name) ?? [];
    values.push(value);
    flags.values.set(name, values);
  }
  return flags;
}

/** Every value of a repeatable flag, flattened over both spellings the harness
 * uses: the flag given several times, and one value list separated by commas.
 * Empty entries are dropped, so a trailing comma is not a source URL. */
export function commaSeparated(flags: Flags, name: string): string[] {
  return (flags.values.get(name) ?? [])
    .flatMap((value) => value.split(","))
    .map((entry) => entry.trim())
    .filter((entry) => entry !== "");
}

/** A flag given at most once. */
export function one(flags: Flags, name: string): string | undefined {
  const values = flags.values.get(name);
  if (values === undefined) return undefined;
  if (values.length > 1) fail(`-${name} is given more than once`);
  return values[0];
}

/** A count flag: the same one spelling of a positive integer the profile admits
 * in a query string (`spec/07-transport.md`, the `limit` bullet). */
export function count(flags: Flags, name: string): number | undefined {
  const raw = one(flags, name);
  if (raw === undefined) return undefined;
  if (!/^[1-9][0-9]*$/.test(raw)) fail(`-${name} is a positive integer, not ${raw}`);
  const value = Number(raw);
  if (!Number.isSafeInteger(value)) fail(`-${name} is too large: ${raw}`);
  return value;
}

/** Report on stderr and stop. Diagnostics never go to stdout: the summary
 * document, and the server's startup line, are the whole of what these programs
 * write there. */
export function fail(message: string): never {
  process.stderr.write(`${message}\n`);
  process.exit(1);
}
