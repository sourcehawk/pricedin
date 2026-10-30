"use client";

import type { ReactNode } from "react";
import type { SecSource, Source, SourcedNumber } from "@/api/types";
import { useTipHandlers } from "./tip/TipProvider";

// Names of the derivation methods that the API reports for a derived value.
const methods: Record<string, string> = {
  ttm: "Sum of the last four quarters",
  recurring_dividends_12m: "Recurring dividends with an ex-date in the last 12 months",
  price_over_ttm_eps: "Share price divided by TTM EPS",
  ytd_difference: "Year to date minus the quarters before",
  fy_difference: "Fiscal year minus the first three quarters",
  weighted_average_q4: "Fourth quarter from the yearly weighted average",
  reported: "As reported",
  not_available: "No data was ever fetched",
  derived: "Computed from other reported values",
};

const providers: Record<string, string> = { massive: "Massive" };
const providerName = (p: string): string => providers[p] ?? p;

function secSources(s: Source): SecSource[] {
  switch (s.kind) {
    case "sec":
      return [s];
    case "derived":
      return s.inputs.flatMap(secSources);
    default:
      return [];
  }
}

/** filingLink returns the filing a value links to: its own, or the newest filing among its inputs at any depth. */
export function filingLink(s: Source): string | null {
  let newest: SecSource | null = null;
  for (const f of secSources(s)) {
    if (newest === null || f.filed > newest.filed) {
      newest = f;
    }
  }
  return newest?.url ?? null;
}

function SourceLine({ s }: { s: Source }) {
  switch (s.kind) {
    case "sec":
      return (
        <>
          Form {s.form}, filed {s.filed}, period ending {s.periodEnd}
        </>
      );
    case "price":
      return (
        <>
          Close on {s.closeDate} from {providerName(s.provider)}, adjusted for splits
        </>
      );
    case "dividend":
      return (
        <>
          Cash dividend from {providerName(s.provider)}, ex-date {s.exDate}
        </>
      );
    case "derived":
      return <>{s.label ?? methods[s.method] ?? s.method}</>;
    case "user":
      return <>You typed this</>;
  }
}

function SourceTip({ value }: { value: SourcedNumber }) {
  const s = value.source;
  return (
    <>
      <b>
        <SourceLine s={s} />
        {s.kind === "derived" && s.estimate ? " (estimate)" : ""}
      </b>
      {s.kind === "derived" && s.inputs.length > 0 && (
        <ul className="tip-list">
          {s.inputs.map((i, k) => (
            <li key={k}>
              <SourceLine s={i} />
            </li>
          ))}
        </ul>
      )}
      {s.kind === "sec" && <div className="tm">Accession {s.accession}</div>}
      <div className="tm">
        {value.asOfLabel === undefined ? "As of" : capitalize(value.asOfLabel)} {value.asOf}
      </div>
    </>
  );
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/**
 * Sourced shows a number with its source: hovering or focusing it shows
 * the source and the as-of date, and a value from a filing links to that
 * filing. user marks a value the user typed; without a value or user it
 * shows only the number.
 */
export function Sourced({
  value,
  user = false,
  children,
}: {
  value: SourcedNumber | null | undefined;
  user?: boolean;
  children: ReactNode;
}) {
  const handlers = useTipHandlers(() =>
    user ? <b>You typed this</b> : value ? <SourceTip value={value} /> : null,
  );
  if (!user && !value) {
    return <span>{children}</span>;
  }
  const href = !user && value ? filingLink(value.source) : null;
  if (href !== null) {
    return (
      <a className="sourced" href={href} target="_blank" rel="noopener noreferrer" {...handlers}>
        {children}
      </a>
    );
  }
  return (
    <span className="sourced" tabIndex={0} {...handlers}>
      {children}
    </span>
  );
}
