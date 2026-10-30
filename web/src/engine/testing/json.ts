import { readFileSync } from "node:fs";
import path from "node:path";

/** A decoded JSON value. */
export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };

/** The repository's testdata/engine directory, shared with the Go engine. */
export const engineTestdata = path.join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "..",
  "testdata",
  "engine",
);

/** readJson reads and decodes one file of testdata/engine. */
export function readJson(file: string): unknown {
  return JSON.parse(readFileSync(path.join(engineTestdata, file), "utf8"));
}

/**
 * toJson returns value as it travels over the wire: what JSON.stringify
 * writes, decoded again. It throws on NaN and Infinity, which Go cannot
 * marshal, so that such a value fails a test instead of turning into null.
 */
export function toJson(value: unknown): Json {
  const text = JSON.stringify(value, (key, v: unknown) => {
    if (typeof v === "number" && !Number.isFinite(v)) {
      throw new Error(`non-finite number ${String(v)} at key "${key}"`);
    }
    return v;
  });
  return JSON.parse(text) as Json;
}

/**
 * compareJson compares decoded JSON the way the Go case runner does:
 * numbers within tol, null only with null, objects with the same keys,
 * arrays of the same length. It returns one line per difference.
 */
export function compareJson(want: Json, got: Json, tol: number, at = "$"): string[] {
  const diffs: string[] = [];
  walk(at, want, got, tol, diffs);
  return diffs;
}

function walk(at: string, want: Json, got: Json, tol: number, diffs: string[]): void {
  if (typeof want === "number") {
    if (typeof got !== "number" || Math.abs(want - got) > tol) {
      diffs.push(`${at}: want ${String(want)}, got ${JSON.stringify(got)}`);
    }
    return;
  }
  if (Array.isArray(want)) {
    if (!Array.isArray(got) || got.length !== want.length) {
      diffs.push(`${at}: want an array of ${String(want.length)}, got ${JSON.stringify(got)}`);
      return;
    }
    want.forEach((w, i) => {
      walk(`${at}[${String(i)}]`, w, got[i] ?? null, tol, diffs);
    });
    return;
  }
  if (want !== null && typeof want === "object") {
    if (got === null || typeof got !== "object" || Array.isArray(got)) {
      diffs.push(`${at}: want an object, got ${JSON.stringify(got)}`);
      return;
    }
    const keys = [...new Set([...Object.keys(want), ...Object.keys(got)])].sort();
    for (const k of keys) {
      const w = want[k];
      const g = got[k];
      if (w === undefined) {
        diffs.push(`${at}.${k}: unexpected field with ${JSON.stringify(g)}`);
      } else if (g === undefined) {
        diffs.push(`${at}.${k}: missing, want ${JSON.stringify(w)}`);
      } else {
        walk(`${at}.${k}`, w, g, tol, diffs);
      }
    }
    return;
  }
  if (want !== got) {
    diffs.push(`${at}: want ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
  }
}

/**
 * Schema is the shape of a case input: "leaf" for a value whose keys are
 * not checked (a number, a string, a date), an array of one schema, or an
 * object with its exact JSON field names.
 */
export type Schema = "leaf" | { array: Schema } | { object: Readonly<Record<string, Schema>> };

/**
 * unknownKeys returns the path of each object key in raw that is not the
 * exact name of a field of schema, as the Go runner's unknownKeys does.
 * Absent keys and null values pass.
 */
export function unknownKeys(raw: unknown, schema: Schema, at = "$"): string[] {
  if (schema === "leaf" || raw === null || typeof raw !== "object") {
    return [];
  }
  if ("array" in schema) {
    return Array.isArray(raw)
      ? raw.flatMap((v: unknown, i) => unknownKeys(v, schema.array, `${at}[${String(i)}]`))
      : [];
  }
  if (Array.isArray(raw)) {
    return [];
  }
  return Object.entries(raw).flatMap(([k, v]: [string, unknown]) => {
    const field = Object.hasOwn(schema.object, k) ? schema.object[k] : undefined;
    return field === undefined ? [`${at}.${k}`] : unknownKeys(v, field, `${at}.${k}`);
  });
}
