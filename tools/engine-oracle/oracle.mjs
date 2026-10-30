// The engine oracle: every engine function with the v1 input and output
// shapes, computed by the prototype's own code in prototype.mjs.
//
// This file converts v1 inputs (fractions, nullable values, dates) into the
// prototype's structures (percent levers, quarter tables, timestamps), calls
// the verbatim prototype code, and converts the result back. Where v1
// deliberately differs from the prototype, the v1 rule is written here and
// marked with "v1:", and NOTES holds the sentence that the shared cases
// carry as their note.

import * as P from "./prototype.mjs";

export const NOTES = {
  nullData:
    "v1: missing quarterly data gives null with a reason; the prototype has no missing data.",
  dividendsReachTarget:
    "v1: when dividends alone reach the target, required growth is null with reason dividends_reach_target; the prototype returns -1 and shows 'none'.",
  trailingHistory:
    "v1: the trailing look-back is limited by the available history; the prototype caps it at 45 months, the limit of its 19 demo quarters.",
  typicalMedian:
    "v1: typical P/E is the conventional median (the mean of the two middle values for an even count); the prototype takes the upper middle value.",
  typicalHistory:
    "v1: the typical P/E window starts at the first quarter when the history is shorter than the look-back.",
  noTrend:
    "v1: without a weighted trend the estimate treats the stock as a slow grower and reports g0 and gExit as null; the prototype computes with null as 0.",
  defaultScenNoTrend:
    "v1: without a weighted trend the default cases center on 5% growth as the prototype intends; the prototype's isFinite(null) check lets null through as 0.",
  exactRow:
    "v1: the return grid has a row at the exact baseline growth, tagged with its baseline, and the YOU cell sits in it; the prototype marks the nearest row.",
  scaleEveryPoint:
    "v1: the earnings-path scale covers every plotted point, including intermediate points and negative TTM EPS; the prototype uses only the end points and clamps the minimum at 0.",
  zeroWidth: "v1: a scale with a zero-width domain widens to a visible range.",
  g12: "v1: 12-month EPS growth compares the TTM EPS known on a day with the TTM EPS known 365 days earlier; the prototype uses the TTM of four quarters earlier, which is the same value without a reporting lag.",
  dailyPe: "v1: the daily P/E is null when TTM EPS is not positive; the prototype divides anyway.",
  medianRequired:
    "v1: the typical required growth over the period is the conventional median, like the typical P/E.",
  demandingCount:
    "v1: the count of quarters where the price asked for more than was delivered compares only quarters with both values.",
  pairwise:
    "v1: correlations skip quarters where either change is missing; the prototype has no missing data.",
  barScale:
    "v1: bar scales include negative values and zero; the prototype assumes positive values.",
  baseCheck:
    "v1: a change against a non-positive base is null with reason non_positive_base; the prototype divides anyway.",
  yearRatesPad:
    "v1: missing year-by-year rates repeat the last rate, or the flat rate when there is none; filling from the trajectory is a UI action.",
  growthRange:
    "v1: a growth of -100% or lower, or above 1000%, gives null with reason growth_out_of_range for values that compound it.",
  trajectoryBounds:
    "v1: the trajectory rates stay within -50% to 200%, the bounds of a custom rate, so that they can fill the custom rates; the prototype lets the cap at the highest reading go below -50%.",
  fadeFactor:
    "v1: the estimated exit P/E uses the fade factor 0.5^(years/3) directly; the prototype's (gExit - 6%) / (g0 - 6%) gives 0 near the floor by cancellation.",
  bandDigits:
    "v1: band multiples keep enough decimals for their step; the prototype rounds to 2 decimals, which makes the bands of a P/E below 0.01 all zero.",
  targetTolerance:
    "v1: a return within 1e-9 below the target meets it, so a projection that lands exactly on the target is not a miss by rounding.",
  bandSteps:
    "v1: the band step search widens its powers of ten when the P/E span needs it, and the bands stop at 50; the prototype searches 0.1 to 5000 only, which never ends for a near-zero EPS.",
  peChangeNeedsEps:
    "v1: the 12-month P/E change needs a positive TTM EPS today; the prototype divides by zero when EPS fell to zero.",
  scenarioRange:
    "v1: a case with growth of -100% or lower, or a non-positive exit P/E, has no result, and the worst return skips it.",
  exitPePositive:
    "v1: an exit P/E in use of zero or less is missing with reason needs_positive_eps.",
  constantSeries:
    "v1: a correlation of a constant series is missing with reason zero_variance; the prototype divides rounding noise.",
};

// ---------------------------------------------------------------- helpers

export const val = (v) => ({ value: v });
export const none = (reason) => ({ value: null, reason });
const fin = (v) => v != null && Number.isFinite(v);
const has = (m) => m && m.value != null;
const nanToNull = (v) => (fin(v) ? v : null);

const DAY = 864e5;
export const toMs = (date) => Date.parse(`${date}T00:00:00Z`);
export const toDate = (ms) => new Date(ms).toISOString().slice(0, 10);

// The conventional median of a sorted copy of finite values.
function median(values) {
  const v = values.filter(fin).sort((a, b) => a - b);
  if (!v.length) return null;
  const h = Math.floor(v.length / 2);
  return v.length % 2 ? v[h] : (v[h - 1] + v[h]) / 2;
}

// v1: widen a zero-width domain.
function widen(lo, hi) {
  if (hi > lo) return [lo, hi];
  const d = Math.abs(hi) > 0 ? Math.abs(hi) * 0.1 : 1;
  return [lo - d, hi + d];
}

function axis(min, max, step) {
  const [a, b] = widen(min, max);
  return { min: a, max: b, step };
}
function range(min, max) {
  const [a, b] = widen(min, max);
  return { min: a, max: b };
}

// ------------------------------------------------------- quarterly series

// The per-quarter series of v1: the prototype's series with null wherever an
// input is missing (v1), each as a {value, reason}.
export function series(quarters) {
  const Q = quarters.map((q) => [q.label, q.eps, q.close]);
  const base = P.loadData(Q);
  const L = Q.length - 1;
  const eps = quarters.map((q) => q.eps);
  const ttm = base.ttm.map((t, i) => {
    if (i < 3) return none("insufficient_history");
    for (let k = 0; k < 4; k++) if (eps[i - k] == null) return none("missing_data");
    return val(t);
  });
  const yoy = base.qyoy.map((y, i) => {
    if (i < 4) return none("insufficient_history");
    if (eps[i] == null || eps[i - 4] == null) return none("missing_data");
    if (!(eps[i - 4] > 0)) return none("needs_positive_eps");
    return val(y);
  });
  const ttmYoy = base.tyoy.map((y, i) => {
    if (i < 7) return none("insufficient_history");
    if (!has(ttm[i])) return none(ttm[i].reason);
    if (!has(ttm[i - 4])) return none(ttm[i - 4].reason);
    if (!(ttm[i - 4].value > 0)) return none("needs_positive_eps");
    return val(y);
  });
  // The prototype code reads these arrays with null for "no value".
  const ttmN = ttm.map((m) => m.value);
  const qyoyN = yoy.map((m) => m.value);
  const tyoyN = ttmYoy.map((m) => m.value);
  // The weighted trend of loadData (line 738) is wtdAt(L) on these series;
  // its fallback to RECENT only applies when RECENT is missing too.
  const ctx = { S: {}, Q, L, ttm: ttmN, qyoy: qyoyN, WTD: null, RECENT: L >= 0 ? qyoyN[L] : null };
  const WTD = L >= 0 ? nanToNull(P.makeMath(ctx).wtdAt(L)) : null;
  const math = P.makeMath({ ...ctx, WTD });
  const weightedTrend = quarters.map((_, i) => {
    const w = math.wtdAt(i);
    return fin(w) ? val(w) : none("insufficient_history");
  });
  return { Q, L, ttm, yoy, ttmYoy, weightedTrend, ttmN, qyoyN, tyoyN, WTD, math };
}

export function quarterSeries({ quarters }) {
  const s = series(quarters);
  return quarters.map((q, i) => ({
    ttmEps: s.ttm[i],
    yoy: s.yoy[i],
    ttmYoy: s.ttmYoy[i],
    weightedTrend: s.weightedTrend[i],
  }));
}

export function weightedTrend({ quarters, at }) {
  const s = series(quarters);
  if (at < 0 || at >= quarters.length) return none("insufficient_history");
  return s.weightedTrend[at];
}

// ------------------------------------------------------------- core math

const coreMath = () =>
  P.makeMath({ S: {}, Q: [], L: -1, ttm: [], qyoy: [], WTD: null, RECENT: null });

export function requiredGrowth({ price, eps, targetReturn, years, exitPe, dividend }) {
  const R = coreMath().req(price, eps, targetReturn, years, exitPe, dividend);
  if (!fin(R)) return none("needs_positive_eps");
  // v1: -1 means the dividends alone reach the target.
  if (R === -1) return none("dividends_reach_target");
  return val(R);
}

export function requirement(input) {
  const { price, eps, years, exitPe, dividend } = input;
  const growth = requiredGrowth(input);
  const dividends = dividend * years;
  if (!has(growth)) {
    const r = growth.reason;
    return {
      growth,
      exitPrice: none(r),
      priceChange: none(r),
      eps: none(r),
      dividends,
      tightFrom: none(r),
      undemandingFrom: none(r),
    };
  }
  // Lines 963-965 (renderAll): EPS and exit price needed.
  const R = growth.value,
    n = years,
    E = eps,
    pe = exitPe,
    Pp = price;
  const endE = E * Math.pow(1 + R, n),
    endP = endE * pe;
  return {
    growth,
    exitPrice: val(endP),
    priceChange: val(endP / Pp - 1),
    eps: val(endE),
    dividends,
    // The verdict bounds of tone() (line 847) around the requirement.
    tightFrom: val(R - 0.015),
    undemandingFrom: val(R + 0.015),
  };
}

export function project({ price, eps, growth, years, exitPe, dividend }) {
  const p = coreMath().proj(price, eps, growth, years, exitPe, dividend);
  return { exitPrice: p.px, totalReturn: p.cum, yearlyReturn: p.cagr };
}

export function exitPeNeeded({ price, eps, targetReturn, years, growth, dividend }) {
  if (!(eps > 0)) return none("needs_positive_eps");
  if (!inRange(growth)) return none("growth_out_of_range");
  // v1: a non-positive numerator means the dividends alone reach the target.
  if (!(price * Math.pow(1 + targetReturn, years) - dividend * years > 0))
    return none("dividends_reach_target");
  return val(coreMath().reqPe(price, eps, targetReturn, years, growth, dividend));
}

// v1: the engine compounds growth above -100% and up to 1000% a year.
const inRange = (g) => g > -1 && g <= 10;

export function tone({ diff }) {
  return coreMath().tone(diff);
}

const VERDICT = { good: "undemanding", warn: "tight", bad: "demanding" };
export function verdict({ gap }) {
  return VERDICT[coreMath().tone(gap)];
}

export function niceStep({ range: r, target }) {
  return P.niceStep(r, target);
}

// ---------------------------------------------------------------- baselines

// v1: the longest trailing look-back the history allows.
const maxTrailingMonths = (L) => Math.max(0, 3 * (L - 3));

export function trailingGrowth({ quarters, months }) {
  const s = series(quarters);
  const L = s.L;
  const maxMonths = maxTrailingMonths(L);
  // Line 789 (trlMonths), with the v1 upper limit from the history.
  let m = Math.round((+months || 12) / 3) * 3;
  m = Math.min(Math.max(6, maxMonths), Math.max(6, m));
  const q = m / 3;
  const from = L - q >= 0 ? s.ttm[L - q] : none("insufficient_history");
  const to = L >= 0 ? s.ttm[L] : none("insufficient_history");
  let value;
  if (L - q < 3) value = none("insufficient_history");
  else if (!has(from)) value = none(from.reason);
  else if (!has(to)) value = none(to.reason);
  else {
    // Line 791 (trlGrowth).
    const a = from.value,
      b = to.value;
    value = a > 0 && b > 0 ? val(Math.pow(b / a, 12 / m) - 1) : none("needs_positive_eps");
  }
  return { growth: value, months: m, maxMonths, fromTtm: from, toTtm: to };
}

export function trajectoryFill({ quarters, years }) {
  const s = series(quarters);
  if (s.WTD == null) return none("insufficient_history");
  const t = s.math.trajInfo(years);
  // v1: the rates stay within the bounds of a custom rate, -50% to 200%.
  const rates = t.rates.map((v) => Math.max(-50, Math.min(200, v)));
  return val({
    rates: rates.map((v) => v / 100),
    slope: t.slope,
    previous: fin(t.prev) ? val(t.prev) : none("insufficient_history"),
    highest: t.hi,
    capped: t.slope > 0 && rates.some((v) => Math.abs(v / 100 - t.hi) < 0.006),
  });
}

// v1: padding repeats the last rate or the flat rate.
export function yearRates({ custom, years }) {
  const S = {
    n: years,
    ygr: (custom.rates || []).map((v) => v * 100),
    ygrSrc: "user",
    custom: custom.rate * 100,
  };
  const math = P.makeMath({ S, Q: [], L: -1, ttm: [], qyoy: [], WTD: null, RECENT: null });
  return math.yearRates().map((v) => v / 100);
}

export function yearlyAverage({ rates }) {
  const S = { n: rates.length, ygr: rates.map((v) => v * 100), ygrSrc: "user", custom: 0 };
  const math = P.makeMath({ S, Q: [], L: -1, ttm: [], qyoy: [], WTD: null, RECENT: null });
  return math.yearlyAvg();
}

export function baselinePath({ eps, growth, quarters: k, yearlyRates }) {
  const yearly = Array.isArray(yearlyRates);
  const S = yearly
    ? {
        mode: "custom",
        yearly: true,
        n: yearlyRates.length,
        ygr: yearlyRates.map((v) => v * 100),
        ygrSrc: "user",
        custom: 0,
      }
    : { mode: "wtd" };
  const math = P.makeMath({ S, Q: [], L: -1, ttm: [], qyoy: [], WTD: null, RECENT: null });
  return math.basePath(eps, growth, k);
}

export function growthScale({ values }) {
  const v = values.map((x) => (x == null ? NaN : x));
  const s = P.rulerScale(v[0], v[1], v[2], v[3], v[4]);
  return axis(s.lo, s.hi, s.step);
}

// ---------------------------------------------------------------- exit P/E

export function typicalPe({ quarters, price, eps, months }) {
  const s = series(quarters);
  const L = s.L;
  // Line 827 (peTrlMonths).
  let m = Math.round((+months || 36) / 3) * 3;
  m = Math.min(36, Math.max(6, m));
  const q = m / 3;
  // Line 829 (peTrailing), v1: the window starts at the first quarter at most.
  const v = [];
  for (let i = Math.max(0, L - q); i <= L; i++) {
    const Pp = i === L ? price : s.Q[i][2],
      E = i === L ? eps : s.ttmN[i];
    if (Pp && E > 0) v.push(Pp / E);
  }
  // v1: the conventional median.
  const med = median(v);
  return {
    pe: med == null ? none("insufficient_history") : val(med),
    months: m,
    points: v.length,
  };
}

// Lines 815-825 (estPe), with the typical P/E and the weighted trend as
// parameters. v1: a missing trend counts as slow growth (see NOTES.noTrend).
export function estimatedExitPe({ price, eps, years, weightedTrend: wtd, typicalPe: TP }) {
  const PE_BASE = 15,
    G_FLOOR = 0.06,
    HALF = 3;
  const now = price / eps,
    n = years,
    g0 = wtd;
  const g0v = g0 == null ? none("insufficient_history") : val(g0);
  const gExitRaw = g0 == null ? null : G_FLOOR + (g0 - G_FLOOR) * Math.pow(0.5, n / HALF);
  const gExit = gExitRaw == null ? none("insufficient_history") : val(gExitRaw);
  // v1: zero EPS gives an infinite P/E, which the prototype lets through.
  if (!(eps > 0) || !(now > 0))
    return { pe: none("needs_positive_eps"), rule: null, g0: g0v, gExit };
  const slow = g0 == null || g0 <= G_FLOOR;
  let pe, rule;
  if (slow && TP != null && now > TP * 1.05) {
    pe = TP;
    rule = "slowGrowerTypical";
  } else if (slow || now <= PE_BASE) {
    pe = now;
    rule = slow ? "slowGrowerKeep" : "lowPeKeep";
  } else {
    // v1: the fraction of the excess growth left at the exit is the fade
    // factor itself; the prototype's (gExit - floor) / (g0 - floor) loses it
    // to cancellation near the floor.
    pe = PE_BASE + (now - PE_BASE) * Math.pow(0.5, n / HALF);
    rule = "fade";
  }
  return { pe: val(Math.max(6, Math.round(pe * 2) / 2)), rule, g0: g0v, gExit };
}

// ------------------------------------------------------------------ the grid

const TAGS = { weighted: "weighted", today: "today", trailing: "trailing", custom: "custom" };

export function returnGrid({
  price: Pp,
  eps: E,
  targetReturn: r,
  years: n,
  dividend: D,
  exitPe: pe,
  required,
  baseline,
  weighted,
  today,
  trailing,
}) {
  if (!(E > 0)) return none("needs_positive_eps");
  if (!(pe > 0)) return none("needs_positive_eps");
  const num = (m) => (has(m) ? m.value : NaN);
  const R = num(required),
    g = num(baseline.growth),
    WTD = num(weighted),
    RECENT = num(today),
    LT = num(trailing);
  const math = coreMath();
  const h = P.heatmap(R, g, pe, RECENT, WTD, LT, Pp, E, math.proj, n, D, r);
  // v1: insert the exact baseline row.
  const regular = h.rows;
  let rows = regular.map((growth) => ({ growth, exact: false }));
  const gOk = fin(g) && inRange(g);
  if (gOk) {
    const same = rows.find((x) => Math.abs(x.growth - g) < 1e-9);
    if (same) same.exact = true;
    else {
      rows.push({ growth: g, exact: true });
      rows.sort((a, b) => a.growth - b.growth);
    }
  }
  const values = rows.map((x) => x.growth);
  const tagsAt = rows.map(() => []);
  const youRow = gOk ? rows.findIndex((x) => x.exact) : -1;
  const place = (kind, v) => {
    if (kind === baseline.mode) {
      if (youRow >= 0) tagsAt[youRow].push(kind);
      return;
    }
    if (fin(v)) tagsAt[h.near(values, v)].push(kind);
  };
  place("required", R);
  place("weighted", WTD);
  place("today", RECENT);
  place("trailing", LT);
  if (baseline.mode === "custom") place("custom", g);
  const outRows = rows.map((x, i) => ({
    growth: x.growth,
    epsAtExit: E * Math.pow(1 + x.growth, n),
    exact: x.exact,
    tags: tagsAt[i],
    cells: h.cols.map((c) => {
      // Lines 1202-1204 (drawHeatmap).
      const p = math.proj(Pp, E, x.growth, n, c, D),
        diff = p.cagr - r;
      return {
        exitPrice: p.px,
        totalReturn: p.cum,
        yearlyReturn: p.cagr,
        vsTarget: diff,
        tone: math.tone(diff),
      };
    }),
  }));
  // The "Move down a column" step of the grid walkthrough, on the regular rows.
  let columnSensitivity = none(
    has(baseline.growth) ? "growth_out_of_range" : baseline.growth.reason,
  );
  if (gOk) {
    const selR = h.near(regular, g);
    const at = (ri) => math.proj(Pp, E, regular[ri], n, h.cols[h.selC], D).cagr;
    const i = Math.min(Math.max(selR, 1), regular.length - 1);
    columnSensitivity = val({
      growthStep: regular[i] - regular[i - 1],
      returnChange: at(i) - at(i - 1),
    });
  }
  return val({
    step: h.step,
    columns: h.cols.map((c, i) => ({ exitPe: c, today: h.today[i] })),
    rows: outRows,
    you:
      youRow >= 0
        ? val({ row: youRow, column: h.selC })
        : none(has(baseline.growth) ? "growth_out_of_range" : baseline.growth.reason),
    columnSensitivity,
  });
}

// ------------------------------------------------------------- labels

// v1: the label of the quarter k quarters after a "Q<q> FY<yy>" label.
export function labelAfter(label, k) {
  const m = /^Q([1-4]) FY(\d{2})$/.exec(label || "");
  if (!m) return null;
  const idx = Number(m[1]) - 1 + k;
  const q = (((idx % 4) + 4) % 4) + 1;
  const y = (Number(m[2]) + Math.floor(idx / 4)) % 100;
  return `Q${q} FY${String(y).padStart(2, "0")}`;
}

// ------------------------------------------------------------ the paths

// v1: the history windows start 12 quarters before the latest one (the
// prototype's fixed index 6 is that quarter in its 19-quarter demo data).
const windowStart = (L) => Math.max(0, L - 12);

export function earningsPaths({
  quarters,
  eps: E,
  years: n,
  baseline: g,
  yearlyRates,
  required: R,
}) {
  if (!(E > 0)) return none("needs_positive_eps");
  const s = series(quarters);
  const L = s.L;
  const history = [];
  for (let i = windowStart(L); i <= L; i++) {
    history.push({
      label: quarters[i].label,
      periodEnd: quarters[i].periodEnd,
      ttmEps: s.ttm[i],
      ttmYoy: s.ttmYoy[i],
    });
  }
  const F = 4 * n;
  const gOk = has(g) && inRange(g.value);
  const bp = (k) => baselinePath({ eps: E, growth: g.value, quarters: k, yearlyRates });
  // Line 1250 (drawTraj): the required path; v1 drops it when there is no requirement.
  const fr = (k) => E * Math.pow(1 + R.value, k / 4);
  const gReason = has(g) ? "growth_out_of_range" : g.reason;
  const last = L >= 0 ? quarters[L].label : null;
  const future = [];
  for (let k = 0; k <= F; k++) {
    const b = gOk ? val(bp(k)) : none(gReason);
    const q = has(R) ? val(fr(k)) : none(R.reason);
    future.push({
      quartersAhead: k,
      label: k === 0 ? last : labelAfter(last, k),
      baseline: b,
      required: q,
      gap: has(b) && has(q) ? val(b.value - q.value) : none(has(b) ? q.reason : b.reason),
    });
  }
  const end = future[F];
  // Line 1263 (drawTraj): red when the required path ends above the baseline.
  const gapTone = has(end.gap) ? val(fr(F) > bp(F) ? "bad" : "good") : none(end.gap.reason);
  // v1: the scale covers every plotted point (lines 1251-1253 use only the end points).
  const vals = history
    .map((x) => x.ttmEps.value)
    .concat(
      future.map((x) => x.baseline.value),
      future.map((x) => x.required.value),
      [E],
    )
    .filter(fin);
  const lo0 = Math.min(...vals),
    hi0 = Math.max(...vals);
  let mn = lo0 - Math.abs(lo0) * 0.08;
  const mx = hi0 + Math.abs(hi0) * 0.04;
  const st = P.niceStep(mx - mn, 5);
  mn = Math.floor(mn / st) * st;
  return val({ history, future, gapTone, scale: axis(mn, mx, st) });
}

export function howDemanding({
  quarters,
  price,
  eps,
  targetReturn: r,
  years: n,
  exitPe,
  dividend: D,
}) {
  const s = series(quarters);
  const L = s.L;
  const points = [];
  for (let i = windowStart(L); i <= L; i++) {
    const today = i === L;
    // Line 1288 (drawHist).
    const Pp = today ? price : s.Q[i][2];
    const Em = today ? val(eps) : s.ttm[i];
    const pm = Pp == null ? none("missing_data") : val(Pp);
    let req;
    if (!has(exitPe)) req = none(exitPe.reason);
    else if (Pp == null) req = none("missing_data");
    else if (!has(Em)) req = none(Em.reason);
    else
      req = requiredGrowth({
        price: Pp,
        eps: Em.value,
        targetReturn: r,
        years: n,
        exitPe: exitPe.value,
        dividend: D,
      });
    const pe = !has(pm)
      ? none("missing_data")
      : !has(Em)
        ? none(Em.reason)
        : Em.value > 0
          ? val(Pp / Em.value)
          : none("needs_positive_eps");
    points.push({
      label: quarters[i].label,
      periodEnd: quarters[i].periodEnd,
      today,
      price: pm,
      ttmEps: Em,
      pe,
      required: req,
      delivered: s.ttmYoy[i],
    });
  }
  // Lines 1290-1293 (drawHist).
  const all = points.flatMap((p) => [p.required.value, p.delivered.value]).filter(fin);
  let mn = Math.min(0, ...all),
    mx = Math.max(0.05, ...all);
  const st = P.niceStep(mx - mn, 5);
  mn = Math.floor(mn / st) * st;
  mx = Math.ceil(mx / st) * st;
  const reqs = points.map((p) => p.required.value);
  const med = median(reqs);
  let peak = none("insufficient_history");
  const finite = reqs.filter(fin);
  if (finite.length) {
    // v1: values within 1e-9 of the top count as a tie, and the first one
    // wins, so that rounding noise cannot move the peak.
    const top = Math.max(...finite);
    const index = reqs.findIndex((v) => fin(v) && v >= top - 1e-9);
    peak = val({ index, growth: reqs[index] });
  }
  let above = 0,
    compared = 0;
  points.forEach((p) => {
    if (has(p.required) && has(p.delivered)) {
      compared++;
      if (p.required.value > p.delivered.value) above++;
    }
  });
  return {
    points,
    scale: axis(mn, mx, st),
    typicalRequired: med == null ? none("insufficient_history") : val(med),
    peak,
    requiredAboveDelivered: above,
    compared,
  };
}

// ------------------------------------------------------------ scenarios

export function defaultScenarios({ weightedTrend: wtd, exitPe }) {
  // v1: a missing trend uses 5% (see NOTES.defaultScenNoTrend).
  const math = P.makeMath({
    S: {},
    Q: [],
    L: -1,
    ttm: [],
    qyoy: [],
    WTD: wtd == null ? NaN : wtd,
    RECENT: null,
  });
  return math
    .defaultScen(fin(exitPe) ? exitPe : 20)
    .map((c) => ({ name: c.name, growth: c.g / 100, exitPe: c.pe }));
}

export function scenarios({ price: Pp, eps: E, targetReturn: r, years: n, dividend: D, cases }) {
  const math = coreMath();
  const out = cases.map((c) => {
    if (!(E > 0)) return { ...c, result: none("needs_positive_eps") };
    if (!inRange(c.growth)) return { ...c, result: none("growth_out_of_range") };
    if (!(c.exitPe > 0)) return { ...c, result: none("needs_positive_eps") };
    // Lines 1323-1324 (drawScen).
    const p = math.proj(Pp, E, c.growth, n, c.exitPe, D),
      t = math.tone(p.cagr - r);
    const fe = E * Math.pow(1 + c.growth, n);
    return {
      ...c,
      result: val({
        eps: fe,
        exitPrice: p.px,
        totalReturn: p.cum,
        yearlyReturn: p.cagr,
        vsTarget: p.cagr - r,
        tone: t,
        meetsTarget: p.cagr >= r - 1e-9,
      }),
    };
  });
  const res = out.filter((c) => has(c.result));
  return {
    cases: out,
    meetingTarget: res.filter((c) => c.result.value.meetsTarget).map((c) => c.name),
    worstYearlyReturn: res.length
      ? val(Math.min(...res.map((c) => c.result.value.yearlyReturn)))
      : none(
          out.length ? out[0].result.reason : E > 0 ? "insufficient_history" : "needs_positive_eps",
        ),
  };
}

// --------------------------------------------------------- share counts

export function shareCountEvents({ quarters }) {
  const out = [];
  for (let i = 1; i < quarters.length; i++) {
    const a = quarters[i - 1].dilutedShares,
      b = quarters[i].dilutedShares;
    if (a == null || b == null || !(a > 0)) continue;
    const ev = P.shareEvents([a, b]);
    if (!ev.length) continue;
    const c = ev[0].c;
    out.push({
      label: quarters[i].label,
      periodEnd: quarters[i].periodEnd,
      from: a,
      to: b,
      change: c,
      kind: c > 0 ? "dilution" : "buyback",
    });
  }
  return out;
}

// ------------------------------------------------------------ price chart

const LINK = { strong: "strong", moderate: "moderate", weak: "weak", negative: "negative" };

// v1: a series whose values are all equal does not vary, whatever rounding
// noise its mean leaves.
const constant = (v) => v.every((x) => x === v[0]);

export function correlation({ xs, ys }) {
  if (xs.length < 20) return none("too_few_points");
  if (constant(xs) || constant(ys)) return none("zero_variance");
  const c = P.corr(xs, ys);
  if (!fin(c)) return none("zero_variance");
  return val({ correlation: c, class: LINK[P.corrWord(c)] });
}

// The daily points with their 12-month changes and P/E (lines 992-995),
// with the v1 rules for g12 and the P/E.
function enrichDaily(daily) {
  const a = daily.map((d) => ({ t: toMs(d.date), p: d.close, e: d.ttmEps, q: -1 }));
  P.enrich(a, []);
  let j = 0;
  return a.map((d, i) => {
    const t = d.t - 365 * DAY;
    while (j < a.length - 1 && a[j + 1].t <= t) j++;
    let g12;
    if (!(a[0].t <= t)) g12 = none("insufficient_history");
    else if (d.e == null || a[j].e == null) g12 = none("missing_data");
    else if (!(a[j].e > 0)) g12 = none("needs_positive_eps");
    else g12 = val(d.e / a[j].e - 1);
    const pe =
      d.e == null
        ? none("missing_data")
        : d.e > 0 && fin(d.p / d.e)
          ? val(d.p / d.e)
          : none("needs_positive_eps");
    const r12 = d.r12 == null ? none("insufficient_history") : val(d.r12);
    return {
      t: d.t,
      date: daily[i].date,
      close: d.p,
      ttmEps: d.e,
      pe,
      priceChange12m: r12,
      epsGrowth12m: g12,
    };
  });
}

// Lines 1043-1048 and 1115 (drawPx, bandNear) with the v1 step search: the
// powers of ten widen from the prototype's -1..3 until the largest step
// gives at most 5 bands and the smallest at least 3, and the bands stop at
// 50 or when a step no longer moves the multiple.
function bandsV1(D) {
  const pes = D.map((d) => d.p / d.e).filter((v) => v > 0 && isFinite(v)),
    pLo = Math.min(...pes) * 0.9,
    pHi = Math.max(...pes) * 1.05;
  let kLo = -1,
    kHi = 3;
  while ((pHi - pLo) / (5 * Math.pow(10, kHi)) > 5 && kHi < 400) kHi++;
  while ((pHi - pLo) / Math.pow(10, kLo) < 3 && kLo > -400) kLo--;
  let stp = 1,
    best = 1e9,
    digits = 2;
  for (let k = kLo; k <= kHi; k++)
    for (const f of [1, 2, 2.5, 5]) {
      const c = f * Math.pow(10, k),
        n = Math.floor(pHi / c) - Math.ceil(pLo / c) + 1;
      if (n < 3) continue;
      const sc = Math.abs(n - 5);
      if (sc < best) {
        best = sc;
        stp = c;
        // v1: enough decimals for the step (the prototype keeps 2).
        digits = Math.max(2, f === 2.5 ? 1 - k : -k);
      }
    }
  const ms = [];
  for (
    let m = Math.ceil(pLo / stp) * stp;
    m <= pHi + 1e-9 && ms.length < 50 && m + stp > m;
    m += stp
  )
    if (m > 0) ms.push(+m.toFixed(digits));
  const LST = D[D.length - 1],
    nowPe = LST.p / LST.e,
    mu = ms.find((m) => m >= nowPe),
    md = [...ms].reverse().find((m) => m <= nowPe);
  const near = ms.length
    ? ms.reduce((b, m) => (Math.abs(m - nowPe) < Math.abs(b - nowPe) ? m : b), ms[0])
    : null;
  return { ms, nowPe, mu, md, first: D[0].p / D[0].e, near };
}

// Line 1046 (drawPx): a band's price on one day is the day's TTM EPS times
// the multiple.
export function bandPrice({ ttmEps, multiple }) {
  if (ttmEps == null) return none("missing_data");
  if (!(ttmEps > 0)) return none("needs_positive_eps");
  return val(ttmEps * multiple);
}

export function peBands({ points }) {
  // points: [{close, ttmEps}] of the visible range.
  const D = points.map((d) => ({
    p: d.close,
    e: d.ttmEps != null && d.ttmEps > 0 ? d.ttmEps : NaN,
  }));
  if (!D.some((d) => fin(d.p / d.e))) return none("needs_positive_eps");
  const b = bandsV1(D);
  const LST = points[points.length - 1];
  const nowOk = fin(b.nowPe);
  const firstOk = fin(b.first);
  const nowReason = LST.ttmEps == null ? "missing_data" : "needs_positive_eps";
  const multiples = b.ms.map((m) => {
    if (!nowOk) return { multiple: m, priceToday: none(nowReason), vsPrice: none(nowReason) };
    const rel = P.bandRelation({ e: LST.ttmEps, p: LST.close }, m);
    return {
      multiple: m,
      priceToday: val(LST.ttmEps * m),
      vsPrice: val(rel === "right at" ? "at" : rel),
    };
  });
  let current = none(nowOk ? "not_applicable" : nowReason);
  if (nowOk && b.mu && b.md && b.mu !== b.md) current = val({ lower: b.md, upper: b.mu });
  let crossed = [];
  if (nowOk && firstOk) {
    // The "Use the P/E bands" walkthrough step (line 1667).
    const a = b.first,
      c = b.nowPe,
      lo = Math.min(a, c),
      hi = Math.max(a, c);
    crossed = b.ms.filter((m) => m > lo + 0.3 && m < hi - 0.3);
  }
  return val({
    multiples,
    firstPe: firstOk
      ? val(b.first)
      : none(points[0].ttmEps == null ? "missing_data" : "needs_positive_eps"),
    currentPe: nowOk ? val(b.nowPe) : none(nowReason),
    current,
    nearest: nowOk && b.ms.length ? val(b.near) : none(nowOk ? "not_applicable" : nowReason),
    crossed,
  });
}

export function priceChart({ daily, quarters, range: px }) {
  if (!daily.length) return none("insufficient_history");
  const all = enrichDaily(daily);
  const { D, end } = P.rangePoints(all, px);
  const points = D.map((d) => ({
    date: d.date,
    close: d.close,
    ttmEps: d.ttmEps,
    pe: d.pe,
    priceChange12m: d.priceChange12m,
    epsGrowth12m: d.epsGrowth12m,
  }));
  const sc = P.pxScales(
    D.map((d) => ({
      p: d.close,
      e: d.ttmEps,
      pe: d.pe.value,
      r12: d.priceChange12m.value,
      g12: d.epsGrowth12m.value,
    })),
  );
  const f = D[0],
    l = D[D.length - 1];
  // Lines 1082-1083 (pxSub) and 1141-1146 (guidePx).
  const peA = f.pe,
    peB = l.pe;
  const epsChange =
    f.ttmEps == null || l.ttmEps == null
      ? none("missing_data")
      : f.ttmEps > 0
        ? val(l.ttmEps / f.ttmEps - 1)
        : none("needs_positive_eps");
  let peTrend = none(has(peA) ? peB.reason : peA.reason);
  if (has(peA) && has(peB))
    peTrend = val(
      peB.value > peA.value * 1.05
        ? "expanded"
        : peB.value < peA.value * 0.95
          ? "contracted"
          : "steady",
    );
  // The "Compare the shapes" walkthrough step (line 1666).
  let share = none("not_applicable");
  if (f.ttmEps > 0 && l.ttmEps > 0) {
    const lp = Math.log(l.close / f.close),
      le = Math.log(l.ttmEps / f.ttmEps);
    if (lp > 0 && le > 0) share = val(le / lp);
  } else if (f.ttmEps == null || l.ttmEps == null) share = none("missing_data");
  // The "Compare the two 12-month" walkthrough step (line 1669).
  const r12 = l.priceChange12m,
    g12 = l.epsGrowth12m;
  let peChange = none(has(r12) ? g12.reason : r12.reason);
  if (has(r12) && has(g12)) {
    peChange =
      l.ttmEps > 0 ? val((1 + r12.value) / (1 + g12.value) - 1) : none("needs_positive_eps");
  }
  const both = D.filter((d) => has(d.priceChange12m) && has(d.epsGrowth12m));
  const link = correlation({
    xs: both.map((d) => d.priceChange12m.value),
    ys: both.map((d) => d.epsGrowth12m.value),
  });
  const startDate = D[0].date,
    endDate = toDate(end);
  const events = shareCountEvents({ quarters }).filter(
    (e) => e.periodEnd >= startDate && e.periodEnd <= endDate,
  );
  return val({
    range: px,
    start: startDate,
    end: endDate,
    points,
    priceAxis: axis(sc.price[0], sc.price[1], sc.price[2]),
    epsRange: sc.eps ? val(range(sc.eps[0], sc.eps[1])) : none("missing_data"),
    peRange: sc.pe ? val(range(sc.pe[0], sc.pe[1])) : none("needs_positive_eps"),
    changeRange: range(sc.change[0], sc.change[1]),
    priceChange: l.close / f.close - 1,
    high: sc.high,
    low: sc.low,
    epsChange,
    peStart: peA,
    peEnd: peB,
    peTrend,
    earningsShareOfGain: share,
    last12Months: { priceChange: r12, epsGrowth: g12, peChange },
    link,
    bands: peBands({ points: D }),
    shareCountEvents: events,
  });
}

// ------------------------------------------------------------- financials

export const METRICS = [
  "revenue",
  "operatingCosts",
  "operatingIncome",
  "eps",
  "freeCashFlow",
  "dilutedShares",
];
const PROTO_KEY = {
  revenue: "rev",
  operatingCosts: "opex",
  operatingIncome: "op",
  eps: "eps",
  freeCashFlow: "fcf",
  dilutedShares: "shares",
};
const BAD = {
  revenue: false,
  operatingCosts: true,
  operatingIncome: false,
  eps: false,
  freeCashFlow: false,
  dilutedShares: true,
};
const COLOR_TONE = {
  "var(--good)": "good",
  "var(--bad)": "bad",
  "var(--warn)": "warn",
  "var(--muted)": "neutral",
};
const TILE_TONE = { up: "good", down: "bad", muted: "neutral", warnc: "warn" };
const CMP_CLASS = {
  "strong, same direction": "strongSame",
  "moderate, same direction": "moderateSame",
  "weak or none": "weak",
  "moderate, opposite directions": "moderateOpposite",
  "strong, opposite directions": "strongOpposite",
};

const change = (a, b) =>
  a == null || b == null
    ? none("missing_data")
    : b > 0
      ? val(a / b - 1)
      : none("non_positive_base");

export function financials({ quarters }) {
  const w = quarters.slice(Math.max(0, quarters.length - 8));
  const FIN = {};
  METRICS.forEach((k) => (FIN[PROTO_KEY[k]] = w.map((q) => q[k] ?? null)));
  const rev = FIN.rev;
  const revYoy = rev.map((v, i) => (i >= 4 ? change(v, rev[i - 4]) : none("insufficient_history")));
  const metrics = {};
  METRICS.forEach((k) => {
    const d = FIN[PROTO_KEY[k]];
    const M = { bad: BAD[k] };
    const yoy = d.map((v, i) => (i >= 4 ? change(v, d[i - 4]) : none("insufficient_history")));
    const qoq = d.map((v, i) => (i >= 1 ? change(v, d[i - 1]) : none("insufficient_history")));
    // Line 1352 (drawFin): the color of a change; v1: no revenue change counts as no squeeze.
    const yoyTone = yoy.map((y, i) => {
      if (!has(y)) return none(y.reason);
      if (k === "operatingCosts") {
        const rv = revYoy[i];
        return val(has(rv) && y.value > rv.value + 0.005 ? "warn" : "neutral");
      }
      return val(COLOR_TONE[P.finColor(PROTO_KEY[k], y.value, null, FIN, M)]);
    });
    // Line 1364 (drawFin): the quarter-on-quarter color of the share count.
    const qoqTone = qoq.map((y) =>
      !has(y)
        ? none(y.reason)
        : val(
            Math.abs(y.value) < 1e-6
              ? "neutral"
              : COLOR_TONE[P.finColor("", y.value, null, FIN, M)],
          ),
    );
    const present = d.filter(fin);
    let scale;
    if (!present.length) scale = none("missing_data");
    else if (k === "dilutedShares") {
      const [a, b] = P.finSharesScale(present);
      scale = val(range(a, b));
    } else {
      // v1: bars cover negative values and zero (line 1367 assumes positive values).
      scale = val(
        range(Math.min(0, Math.min(...present) * 1.12), Math.max(0, P.finBarsMax(present))),
      );
    }
    metrics[k] = { values: d, yoy, yoyTone, qoq, qoqTone, scale };
  });
  const operatingMargin = w.map((q) =>
    q.operatingIncome == null || q.revenue == null
      ? none("missing_data")
      : q.revenue > 0
        ? val(q.operatingIncome / q.revenue)
        : none("non_positive_base"),
  );
  const costShareOfRevenue = w.map((q) =>
    q.operatingCosts == null || q.revenue == null
      ? none("missing_data")
      : q.revenue > 0
        ? val(q.operatingCosts / q.revenue)
        : none("non_positive_base"),
  );

  // Lines 1377-1390 (drawFin): the tiles compare the latest quarter with the
  // same quarter a year earlier.
  let tiles = none("insufficient_history");
  if (w.length >= 5) {
    const i = w.length - 1,
      j = i - 4;
    const tile = (k) => {
      const d = FIN[PROTO_KEY[k]];
      const chg = change(d[i], d[j]);
      let t = none(chg.reason);
      if (has(chg)) {
        if (k === "operatingCosts") {
          const rv = change(rev[i], rev[j]);
          t = val(has(rv) && chg.value > rv.value + 0.005 ? "warn" : "neutral");
        } else {
          t = val(TILE_TONE[P.tileClass(PROTO_KEY[k], chg.value, FIN, { bad: BAD[k] }, i, j)]);
        }
      }
      return { latest: d[i] == null ? none("missing_data") : val(d[i]), change: chg, tone: t };
    };
    const m = operatingMargin[i],
      mp = operatingMargin[j];
    tiles = val({
      label: w[i].label,
      compareLabel: w[j].label,
      revenue: tile("revenue"),
      operatingCosts: tile("operatingCosts"),
      operatingIncome: tile("operatingIncome"),
      operatingMargin: (() => {
        const change =
          has(m) && has(mp) ? val(m.value - mp.value) : none(has(m) ? mp.reason : m.reason);
        // Line 1388 (drawFin): tile('margin', ..., m-mp) colors the change.
        const t = has(change)
          ? val(TILE_TONE[P.tileClass("margin", change.value, FIN, { bad: false }, i, j)])
          : none(change.reason);
        return { latest: m, previous: mp, change, tone: t };
      })(),
      eps: tile("eps"),
      freeCashFlow: tile("freeCashFlow"),
      dilutedShares: tile("dilutedShares"),
    });
  }

  // Lines 1398 and 1419-1422 (drawCmp).
  const sinceStart = {},
    quarterOverQuarter = {};
  const changes = {};
  METRICS.forEach((k) => {
    const d = FIN[PROTO_KEY[k]];
    sinceStart[k] = d.map((x, i) =>
      x == null || d[0] == null
        ? none("missing_data")
        : d[0] > 0
          ? val(P.cmpSeries(d, true)[i])
          : none("non_positive_base"),
    );
    quarterOverQuarter[k] = metrics[k].qoq;
    changes[k] = metrics[k].qoq.slice(1).map((m) => m.value);
  });
  const correlations = [];
  for (let a = 0; a < METRICS.length; a++) {
    for (let b = a + 1; b < METRICS.length; b++) {
      const xa = changes[METRICS[a]],
        xb = changes[METRICS[b]];
      // v1: pairwise complete changes only.
      const idx = xa.map((_, i) => i).filter((i) => fin(xa[i]) && fin(xb[i]));
      const ca = idx.map((i) => xa[i]),
        cb = idx.map((i) => xb[i]);
      const r = !idx.length || constant(ca) || constant(cb) ? NaN : P.rr(ca, cb);
      correlations.push({
        a: METRICS[a],
        b: METRICS[b],
        result: fin(r)
          ? val({ correlation: r, class: CMP_CLASS[P.word(r)] })
          : none(idx.length ? "zero_variance" : "missing_data"),
      });
    }
  }
  return {
    quarters: w.map((q) => ({ label: q.label, periodEnd: q.periodEnd })),
    metrics,
    operatingMargin,
    costShareOfRevenue,
    tiles,
    compare: { sinceStart, quarterOverQuarter, correlations, changes: Math.max(0, w.length - 1) },
  };
}

export function compareScale({ values }) {
  const s = P.cmpScale(values.filter(fin));
  return axis(s[0], s[1], s[2]);
}

// ---------------------------------------------------------------- evaluate

const PE_MODE = { estimated: "est", today: "now", trailing: "trl", custom: "manual" };

export function evaluate({ history, levers }) {
  const quarters = history.quarters;
  const s = series(quarters);
  const L = s.L;
  const Pp = levers.price,
    E = levers.ttmEps,
    D = levers.dividend,
    r = levers.targetReturn,
    n = levers.holdingYears;
  const peNow = E > 0 ? val(Pp / E) : none("needs_positive_eps");

  // Baselines.
  const weighted = s.WTD == null ? none("insufficient_history") : val(s.WTD);
  const today = L >= 0 ? s.yoy[L] : none("insufficient_history");
  const trailing = trailingGrowth({ quarters, months: levers.baseline.trailingMonths });
  const mode = levers.baseline.mode;
  const custom = levers.baseline.custom || { yearly: false, rate: 0, rates: [] };
  let yearly = null,
    customValue = none("not_applicable"),
    yearByYear = none("not_applicable");
  if (mode === "custom") {
    if (custom.yearly) {
      yearly = yearRates({ custom, years: n });
      const avg = yearlyAverage({ rates: yearly });
      customValue = val(avg);
      // Lines 806-808 (buildYears).
      const simple = yearly.reduce((a, b) => a + b, 0) / yearly.length;
      yearByYear = val({
        rates: yearly,
        compoundAverage: avg,
        simpleAverage: simple,
        endEps:
          E > 0
            ? val(baselinePath({ eps: E, growth: avg, quarters: 4 * n, yearlyRates: yearly }))
            : none("needs_positive_eps"),
        showSimpleAverage: Math.abs(simple - avg) >= 0.002,
      });
    } else customValue = val(custom.rate);
  }
  const g = { weighted, today, trailing: trailing.growth, custom: customValue }[mode];

  // Exit P/E.
  const typical = typicalPe({ quarters, price: Pp, eps: E, months: levers.exitPe.trailingMonths });
  const est = estimatedExitPe({
    price: Pp,
    eps: E,
    years: n,
    weightedTrend: s.WTD,
    typicalPe: typical.pe.value,
  });
  const estimate = has(est.pe) ? val({ pe: est.pe.value, rule: est.rule }) : none(est.pe.reason);
  let inUse;
  const pm = levers.exitPe.mode;
  if (pm === "estimated") inUse = estimate;
  else if (pm === "today")
    inUse =
      E > 0 ? val({ pe: Math.round((Pp / E) * 2) / 2, rule: "today" }) : none("needs_positive_eps");
  else if (pm === "trailing")
    inUse = has(typical.pe)
      ? val({ pe: Math.round(typical.pe.value * 2) / 2, rule: "trailing" })
      : none(typical.pe.reason);
  else inUse = val({ pe: levers.exitPe.custom, rule: "custom" });
  if (has(inUse) && !(inUse.value.pe > 0)) inUse = none("needs_positive_eps");
  const pe = has(inUse) ? val(inUse.value.pe) : none(inUse.reason);
  let peNowVsTypical = none(has(peNow) ? typical.pe.reason : peNow.reason);
  if (has(peNow) && has(typical.pe)) {
    const now = peNow.value,
      t = typical.pe.value;
    peNowVsTypical = val(now > t * 1.1 ? "above" : now < t * 0.9 ? "below" : "close");
  }
  let vsNow = none(has(peNow) ? pe.reason : peNow.reason);
  if (has(peNow) && has(pe))
    vsNow = val(
      pe.value < peNow.value - 0.25 ? "lower" : pe.value > peNow.value + 0.25 ? "higher" : "same",
    );
  const exitPe = {
    mode: pm,
    inUse,
    estimate,
    g0: est.g0,
    gExit: est.gExit,
    typical,
    peNowVsTypical,
    vsNow,
    sliderMax: P.peSliderMax(E > 0, Pp / E, has(pe) ? pe.value : null),
  };

  // The requirement and the verdict.
  const required = has(pe)
    ? requirement({ price: Pp, eps: E, targetReturn: r, years: n, exitPe: pe.value, dividend: D })
    : (() => {
        const x = none(pe.reason);
        return {
          growth: x,
          exitPrice: x,
          priceChange: x,
          eps: x,
          dividends: D * n,
          tightFrom: x,
          undemandingFrom: x,
        };
      })();
  const R = required.growth;
  let gap, verdictV;
  if (R.reason === "dividends_reach_target") {
    gap = none("dividends_reach_target");
    verdictV = val("undemanding");
  } else if (!has(R)) {
    gap = none(R.reason);
    verdictV = none(R.reason);
  } else if (!has(g)) {
    gap = none(g.reason);
    verdictV = none(g.reason);
  } else {
    gap = val(g.value - R.value);
    verdictV = val(verdict({ gap: gap.value }));
  }

  // Values at the baseline (lines 974-977).
  let atBaseline;
  if (!(E > 0)) atBaseline = none("needs_positive_eps");
  else if (!has(pe)) atBaseline = none(pe.reason);
  else if (!has(g)) atBaseline = none(g.reason);
  else if (!inRange(g.value)) atBaseline = none("growth_out_of_range");
  else {
    const p = project({
      price: Pp,
      eps: E,
      growth: g.value,
      years: n,
      exitPe: pe.value,
      dividend: D,
    });
    const f = exitPeNeeded({
      price: Pp,
      eps: E,
      targetReturn: r,
      years: n,
      growth: g.value,
      dividend: D,
    });
    const fb = E * Math.pow(1 + g.value, n);
    atBaseline = val({
      exitPrice: p.exitPrice,
      totalReturn: p.totalReturn,
      yearlyReturn: p.yearlyReturn,
      vsTarget: p.yearlyReturn - r,
      tone: tone({ diff: p.yearlyReturn - r }),
      meetsTarget: p.yearlyReturn >= r - 1e-9,
      eps: fb,
      epsGap: has(required.eps) ? val(fb - required.eps.value) : none(required.eps.reason),
      exitPeNeeded: f,
      // The "Question your cell" walkthrough step (line 1601).
      fragile:
        (has(R) && Math.abs(g.value - R.value) < 0.02) ||
        (has(f) && Math.abs(f.value - pe.value) < 1.5),
    });
  }

  // The lever experiments of the walkthroughs (lines 1626-1627, 1644).
  let sensitivity;
  if (!(E > 0)) sensitivity = none("needs_positive_eps");
  else if (!has(pe)) sensitivity = none(pe.reason);
  else {
    const rq = (o) =>
      requiredGrowth({
        price: Pp,
        eps: E,
        targetReturn: r,
        years: n,
        exitPe: pe.value,
        dividend: D,
        ...o,
      });
    const lowerPe = Math.max(1, pe.value - 2),
      lowerTarget = Math.max(0, r - 0.02);
    sensitivity = val({
      lowerExitPe: { exitPe: lowerPe, requiredGrowth: rq({ exitPe: lowerPe }) },
      lowerTarget: { targetReturn: lowerTarget, requiredGrowth: rq({ targetReturn: lowerTarget }) },
      marketTarget: { targetReturn: 0.1, requiredGrowth: rq({ targetReturn: 0.1 }) },
      oneYear: { years: 1, requiredGrowth: rq({ years: 1 }) },
      fiveYears: { years: 5, requiredGrowth: rq({ years: 5 }) },
    });
  }

  const trajectory = trajectoryFill({ quarters, years: n });
  const yoyShift =
    L >= 3 && has(s.yoy[L]) && has(s.yoy[L - 3])
      ? val(s.yoy[L].value - s.yoy[L - 3].value)
      : none(
          L < 3 ? "insufficient_history" : !has(s.yoy[L]) ? s.yoy[L].reason : s.yoy[L - 3].reason,
        );

  const returnGridV = has(pe)
    ? returnGrid({
        price: Pp,
        eps: E,
        targetReturn: r,
        years: n,
        dividend: D,
        exitPe: pe.value,
        required: R,
        baseline: { mode, growth: g },
        weighted,
        today,
        trailing: trailing.growth,
      })
    : none(E > 0 ? pe.reason : "needs_positive_eps");

  const scenCases =
    levers.scenarios && levers.scenarios.length
      ? levers.scenarios
      : defaultScenarios({ weightedTrend: s.WTD, exitPe: has(pe) ? pe.value : null });
  const scen = scenarios({
    price: Pp,
    eps: E,
    targetReturn: r,
    years: n,
    dividend: D,
    cases: scenCases,
  });

  return {
    peNow,
    exitPe,
    baselines: {
      active: mode,
      growth: g,
      weighted,
      today,
      trailing,
      custom: customValue,
      yoyShift,
    },
    yearByYear,
    trajectory,
    required,
    gap,
    verdict: verdictV,
    atBaseline,
    sensitivity,
    quarters: quarters.map((q, i) => ({
      label: q.label,
      periodEnd: q.periodEnd,
      eps: q.eps,
      ttmEps: s.ttm[i],
      yoy: s.yoy[i],
      ttmYoy: s.ttmYoy[i],
      weightedTrend: s.weightedTrend[i],
    })),
    growthScale: growthScale({
      values: [R.value, today.value, weighted.value, trailing.growth.value, g.value],
    }),
    returnGrid: returnGridV,
    earningsPaths: earningsPaths({
      quarters,
      eps: E,
      years: n,
      baseline: g,
      yearlyRates: yearly,
      required: R,
    }),
    howDemanding: howDemanding({
      quarters,
      price: Pp,
      eps: E,
      targetReturn: r,
      years: n,
      exitPe: pe,
      dividend: D,
    }),
    scenarios: { defaults: !(levers.scenarios && levers.scenarios.length), ...scen },
    shareCountEvents: shareCountEvents({ quarters }),
    priceChart: priceChart({ daily: history.daily, quarters, range: levers.chartRange || "3Y" }),
    financials: financials({ quarters }),
  };
}
