// @vitest-environment node
import { compareJson, toJson, unknownKeys, type Schema } from "./json";

// The case runner must compare as the Go runner does, or the two engines
// could pass the same file with different outputs.
describe("compareJson", () => {
  it("accepts numbers within the tolerance", () => {
    expect(compareJson({ a: 1 }, { a: 1 + 1e-10 }, 1e-9)).toEqual([]);
  });

  it("rejects numbers beyond the tolerance", () => {
    expect(compareJson({ a: 1 }, { a: 1.1 }, 1e-9)).toEqual(["$.a: want 1, got 1.1"]);
  });

  it("matches null only with null", () => {
    expect(compareJson({ a: null }, { a: null }, 0)).toEqual([]);
    expect(compareJson({ a: null }, { a: 0 }, 0)).toHaveLength(1);
    expect(compareJson({ a: 0 }, { a: null }, 0)).toHaveLength(1);
  });

  it("tells a missing key from a null one", () => {
    expect(compareJson({ value: null, reason: "x" }, { value: null }, 0)).toEqual([
      '$.reason: missing, want "x"',
    ]);
    expect(compareJson({ value: 1 }, { value: 1, reason: "x" }, 0)).toEqual([
      '$.reason: unexpected field with "x"',
    ]);
  });

  it("rejects arrays of another length", () => {
    expect(compareJson([1, 2], [1], 0)).toHaveLength(1);
  });

  it("compares strings and booleans exactly", () => {
    expect(compareJson(["good", true], ["good", false], 0)).toEqual(["$[1]: want true, got false"]);
  });
});

describe("toJson", () => {
  it("drops undefined keys as JSON does", () => {
    expect(toJson({ a: 1, b: undefined })).toEqual({ a: 1 });
  });

  it("fails on NaN and Infinity instead of writing null", () => {
    expect(() => toJson({ a: NaN })).toThrow(/non-finite/);
    expect(() => toJson([Infinity])).toThrow(/non-finite/);
  });
});

describe("unknownKeys", () => {
  const quarter: Schema = { object: { label: "leaf", eps: "leaf" } };
  const schema: Schema = {
    object: {
      price: "leaf",
      eps: "leaf",
      nested: { array: quarter },
      maybe: { object: { value: "leaf", reason: "leaf" } },
    },
  };

  it("names keys that match no field exactly, also in nested objects and arrays", () => {
    const raw = {
      Price: 1,
      eps: 2,
      nested: [{ label: "a" }, { Label: "x" }],
      maybe: { value: null, Reason: "y" },
    };
    expect(unknownKeys(raw, schema).sort()).toEqual([
      "$.Price",
      "$.maybe.Reason",
      "$.nested[1].Label",
    ]);
  });

  it("accepts absent keys and null values", () => {
    expect(unknownKeys({ eps: null, nested: null }, schema)).toEqual([]);
  });
});
