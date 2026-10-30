import type { CSSProperties } from "react";

/** colorVar sets the --c color that color samples, legend keys, line samples and chips read. */
export function colorVar(color: string): CSSProperties {
  return { "--c": color } as CSSProperties;
}
