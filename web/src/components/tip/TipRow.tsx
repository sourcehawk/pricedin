import type { ReactNode } from "react";
import { colorVar } from "@/components/Swatch";

/**
 * TipRow is one label and value row of a tooltip. With color, the row
 * leads with a sample of a chart line's color, dashed for a dashed line,
 * and shows the value large. The tooltip's background is the ink color, so
 * a line drawn in ink takes the surface color here.
 */
export function TipRow({
  label,
  value,
  color,
  dash = false,
}: {
  label: ReactNode;
  value: ReactNode;
  color?: string;
  dash?: boolean;
}) {
  if (color === undefined) {
    return (
      <div className="row">
        <span>{label}</span>
        <span>{value}</span>
      </div>
    );
  }
  return (
    <div className="prow">
      <span>
        <i className={dash ? "sw dash" : "sw"} style={colorVar(color)} />
        {label}
      </span>
      <strong>{value}</strong>
    </div>
  );
}
