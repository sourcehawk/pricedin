import type { CSSProperties } from "react";
import styles from "./PriceChart.module.css";

/** Swatch is a small color sample, shown in text instead of a color name. */
export function Swatch({ color }: { color: string }) {
  return (
    <i className={styles.swatch} style={{ "--c": color } as CSSProperties} aria-hidden="true" />
  );
}
