import type { CSSProperties } from "react";

// Copy never names a color: it shows one of these samples instead.

/** colorVar sets the --c color that color samples, legend keys, line samples and chips read. */
export function colorVar(color: string): CSSProperties {
  return { "--c": color } as CSSProperties;
}

/** Swatch is a small square sample of an area's or a marker's color. */
export function Swatch({ color }: { color: string }) {
  return <i className="sq" style={colorVar(color)} aria-hidden="true" />;
}

/** LineSample is a short sample of a chart line, dashed for a dashed line. */
export function LineSample({ color, dash = false }: { color: string; dash?: boolean }) {
  return <span className={dash ? "lg d" : "lg"} style={colorVar(color)} aria-hidden="true" />;
}
