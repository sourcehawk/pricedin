import type { ExitPeMode, Levers, Quarter } from "./inputs";
import { Series } from "./series";
import { at, median, none, roundHalf, some, type Maybe, type MaybeNumber } from "./value";

/**
 * Typical is the typical P/E: the median of the quarter-end P/Es over the
 * look-back, with today's P/E (the price and TTM EPS levers) as the last
 * point, also when the history has no quarter.
 */
export interface Typical {
  pe: MaybeNumber;
  /** The requested months rounded to a multiple of 3, from 6 to 36. A shorter history uses every quarter it has. */
  months: number;
  /** The P/Es in the median; quarters without a positive TTM EPS or a close are left out. */
  points: number;
}

const defaultTypicalMonths = 36;

/** typicalPe returns the typical P/E over the requested months; zero months means 36. */
export function typicalPe(
  quarters: readonly Quarter[],
  price: number,
  eps: number,
  months: number,
): Typical {
  return typical(new Series(quarters), price, eps, months);
}

export function typical(s: Series, price: number, eps: number, requested: number): Typical {
  const months = requested === 0 ? defaultTypicalMonths : requested;
  let m = Math.round(months / 3) * 3;
  m = Math.min(36, Math.max(6, m));
  const pes: number[] = [];
  for (let i = Math.max(0, s.last - m / 3); i <= s.last; i++) {
    let p = price;
    let e: MaybeNumber = some(eps);
    if (i !== s.last) {
      p = at(s.q, i).close ?? 0;
      e = at(s.ttm, i);
    }
    if (p !== 0 && e.value !== null && e.value > 0) {
      pes.push(p / e.value);
    }
  }
  if (s.last < 0 && price !== 0 && eps > 0) {
    pes.push(price / eps);
  }
  return { pe: median(pes), months: m, points: pes.length };
}

/** ExitPeRule names the rule that set an exit P/E. */
export type ExitPeRule =
  /** The premium of today's P/E over 15x shrinks as growth fades toward 6%. */
  | "fade"
  /** Slow growth and a P/E more than 5% above the typical P/E; the estimate is the typical P/E. */
  | "slowGrowerTypical"
  /** Slow growth; the estimate keeps today's P/E. */
  | "slowGrowerKeep"
  /** Today's P/E is 15x or less; the estimate keeps it. */
  | "lowPeKeep"
  | "today"
  | "trailing"
  | "custom";

/** Estimate is the estimated exit P/E with the numbers that explain it. */
export interface Estimate {
  pe: MaybeNumber;
  /** Null when pe is missing. */
  rule: ExitPeRule | null;
  /** The weighted trend the estimate starts from. */
  g0: MaybeNumber;
  gExit: MaybeNumber;
}

const peBase = 15;
const growthFloor = 0.06;
const fadeHalf = 3;

/**
 * estimatedExitPe estimates the exit P/E after years. weighted is the
 * weighted trend and typicalPe the typical P/E; either may be null. Without
 * a weighted trend the stock counts as a slow grower. The result is rounded
 * to 0.5 and is at least 6.
 */
export function estimatedExitPe(
  price: number,
  eps: number,
  years: number,
  weighted: number | null,
  typicalPe: number | null,
): Estimate {
  const fade = Math.pow(0.5, years / fadeHalf);
  const g0: MaybeNumber = weighted === null ? none("insufficient_history") : some(weighted);
  const gExit: MaybeNumber =
    weighted === null
      ? none("insufficient_history")
      : some(growthFloor + (weighted - growthFloor) * fade);
  const now = price / eps;
  if (!(eps > 0) || !(now > 0)) {
    return { pe: none("needs_positive_eps"), rule: null, g0, gExit };
  }
  const slow = weighted === null || weighted <= growthFloor;
  let pe: number;
  let rule: ExitPeRule;
  if (slow && typicalPe !== null && now > typicalPe * 1.05) {
    pe = typicalPe;
    rule = "slowGrowerTypical";
  } else if (slow) {
    pe = now;
    rule = "slowGrowerKeep";
  } else if (now <= peBase) {
    pe = now;
    rule = "lowPeKeep";
  } else {
    // The fraction of the excess growth left at the exit is the fade
    // factor itself; (gExit-floor)/(g0-floor) loses it near the floor.
    pe = peBase + (now - peBase) * fade;
    rule = "fade";
  }
  return { pe: some(Math.max(6, roundHalf(pe))), rule, g0, gExit };
}

/** ExitPe is an exit P/E with the rule that set it. */
export interface ExitPe {
  pe: number;
  rule: ExitPeRule;
}

/** ExitPeView is everything the page shows about the exit P/E. */
export interface ExitPeView {
  mode: ExitPeMode;
  /** Missing with needs_positive_eps when zero or less. */
  inUse: Maybe<ExitPe>;
  /** The estimated exit P/E, whatever the mode. */
  estimate: Maybe<ExitPe>;
  g0: MaybeNumber;
  gExit: MaybeNumber;
  typical: Typical;
  /** "above" (more than 10% above the typical P/E), "below" (more than 10% below) or "close". */
  peNowVsTypical: Maybe<"above" | "below" | "close">;
  /** "lower", "higher" or "same" (within 0.25) for the exit P/E in use against today's P/E. */
  vsNow: Maybe<"lower" | "higher" | "same">;
  /** The top of the exit P/E slider. */
  sliderMax: number;
}

export function exitPeView(s: Series, l: Levers, peNow: MaybeNumber): ExitPeView {
  const typ = typical(s, l.price, l.ttmEps, l.exitPe.trailingMonths ?? 0);
  const est = estimatedExitPe(l.price, l.ttmEps, l.holdingYears, s.weighted().value, typ.pe.value);
  const estimate: Maybe<ExitPe> =
    est.pe.value === null || est.rule === null
      ? none("needs_positive_eps")
      : some({ pe: est.pe.value, rule: est.rule });
  let inUse: Maybe<ExitPe>;
  switch (l.exitPe.mode) {
    case "today":
      inUse =
        l.ttmEps > 0
          ? some({ pe: roundHalf(l.price / l.ttmEps), rule: "today" })
          : none("needs_positive_eps");
      break;
    case "trailing":
      inUse =
        typ.pe.value === null ? typ.pe : some({ pe: roundHalf(typ.pe.value), rule: "trailing" });
      break;
    case "custom":
      inUse = some({ pe: l.exitPe.custom ?? 0, rule: "custom" });
      break;
    default:
      inUse = estimate;
  }
  if (inUse.value !== null && !(inUse.value.pe > 0)) {
    inUse = none("needs_positive_eps");
  }
  let peNowVsTypical: ExitPeView["peNowVsTypical"];
  if (peNow.value === null) {
    peNowVsTypical = peNow;
  } else if (typ.pe.value === null) {
    peNowVsTypical = typ.pe;
  } else {
    const now = peNow.value;
    const t = typ.pe.value;
    peNowVsTypical = some(now > t * 1.1 ? "above" : now < t * 0.9 ? "below" : "close");
  }
  let vsNow: ExitPeView["vsNow"];
  if (peNow.value === null) {
    vsNow = peNow;
  } else if (inUse.value === null) {
    vsNow = inUse;
  } else {
    const now = peNow.value;
    const pe = inUse.value.pe;
    vsNow = some(pe < now - 0.25 ? "lower" : pe > now + 0.25 ? "higher" : "same");
  }
  let top = l.ttmEps > 0 ? l.price / l.ttmEps : 0;
  if (inUse.value !== null && inUse.value.pe > top) {
    top = inUse.value.pe;
  }
  return {
    mode: l.exitPe.mode,
    inUse,
    estimate,
    g0: est.g0,
    gExit: est.gExit,
    typical: typ,
    peNowVsTypical,
    vsNow,
    sliderMax: Math.max(45, Math.min(400, Math.ceil((top * 1.3) / 5) * 5)),
  };
}
