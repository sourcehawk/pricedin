"use client";

import type { ReactNode } from "react";
import { pct, signedPct, times, usd, years } from "@/components/format";
import { baselineName } from "@/components/rail/notes";
import { InfoTip } from "@/components/tip/InfoTip";
import { Walkthrough, type WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import type { Evaluation, Paths } from "@/engine";
import { useEvaluation } from "@/state/StockProvider";
import { Panel } from "../Panel";
import { endLabels, FromLevers, linePath, swatch, TipRow, useColumns, YAxis } from "./smallChart";

const W = 520;
const H = 270;
const x0 = 48;
const x1 = W - 66;
const y0 = 14;
const y1 = H - 30;

const ink = "var(--ink)";
const muted = "var(--muted)";
const accent = "var(--accent)";

/** pathsOf returns the trajectory; a walkthrough line that needs it is left out without one. */
function pathsOf(ev: Evaluation): Paths {
  if (ev.earningsPaths.value === null) {
    throw new Error("no trajectory");
  }
  return ev.earningsPaths.value;
}

function yearsOut(k: number): string {
  return `${String(k / 4)} ${k === 4 ? "year" : "years"} out`;
}

function signedUsd(v: number | null): string {
  return v === null ? "n/a" : `${v >= 0 ? "+" : ""}${usd(v)}`;
}

/** EarningsTrajectoryPanel shows TTM EPS by quarter, then the baseline path and the path the price requires. */
export function EarningsTrajectoryPanel() {
  const { evaluation: ev, levers, profile, provenance } = useEvaluation();
  const ticker = profile.ticker.symbol;
  const n = levers.holdingYears;

  const steps: WalkthroughStep[] = [
    {
      title: (
        <>
          <span className="lg" style={swatch(ink)} /> The reported EPS line
        </>
      ),
      body: (
        <p>
          The solid line is TTM EPS as the company reported it: earnings per share over each past 12
          months (TTM, trailing twelve months), one point per quarter. It ends at today.
        </p>
      ),
      highlight: ["hist"],
      you: (e) => {
        const p = pathsOf(e);
        const first = p.history[0];
        return (
          <>
            TTM EPS went from <b>{usd(first?.ttmEps.value)}</b> in {first?.label} to{" "}
            <b>{usd(p.future[0]?.baseline.value ?? levers.ttmEps)}</b> today.
          </>
        );
      },
    },
    {
      title: (
        <>
          <span className="lg d" style={swatch(muted)} /> The baseline line
        </>
      ),
      body: (
        <p>
          This dashed line continues from today at your growth baseline from the left panel. It
          shows where EPS ends up if the company keeps growing the way you assume.
        </p>
      ),
      highlight: ["base"],
      levers: ["baseline"],
      you: (e) => (
        <>
          At your {baselineName(e)} of <b>{pct(e.baselines.growth.value)}</b>, EPS reaches{" "}
          <b>{usd(pathsOf(e).future.at(-1)?.baseline.value)}</b> in {years(n)}.
        </>
      ),
    },
    {
      title: (
        <>
          <span className="lg d" style={swatch(accent)} /> The required line
        </>
      ),
      body: (
        <p>
          This dashed line is the EPS path that today&apos;s price needs for your target return at
          your exit P/E. Its end point is the EPS the company must reach by the time you sell.
        </p>
      ),
      highlight: ["req"],
      levers: ["targetReturn", "exitPe"],
      you: (e) => (
        <>
          The price needs EPS to reach <b>{usd(e.required.eps.value)}</b> in {years(n)}:{" "}
          <b>{pct(e.required.growth.value)}</b> growth a year.
        </>
      ),
    },
    {
      title: "Compare the slopes",
      body: (
        <p>
          Look at how steep the reported line was over the last year, then at the required path. If
          the required path is much steeper, the price assumes faster growth than the company has
          shown recently.
        </p>
      ),
      highlight: ["hist", "req"],
      you: (e) => {
        const last = pathsOf(e).history.at(-1)?.ttmYoy.value ?? null;
        const r = e.required.growth.value;
        if (last === null || r === null) {
          return null;
        }
        return (
          <>
            Over the last year TTM EPS grew <b>{signedPct(last)}</b>. The required path climbs{" "}
            <b>{pct(r)}</b> a year,{" "}
            {r > last ? "steeper than the recent record" : "no steeper than the recent record"}.
          </>
        );
      },
    },
    {
      title: "Read the shading",
      body: (
        <p>
          The shaded area is the gap between the two dashed lines.
          <span className="vl">
            <i className="sq" style={swatch("var(--bad)")} />
            The company grows too slowly for your target, so you would earn less than you want.
          </span>
          <span className="vl">
            <i className="sq" style={swatch("var(--good)")} />
            The company grows fast enough, so you would earn more than you want.
          </span>
          The wider the gap at the right edge, the bigger the difference.
        </p>
      ),
      highlight: ["shade", "req", "base"],
      you: (e) => shadingLine(e, ticker, levers.targetReturn),
    },
    {
      title: "Question the color",
      body: (
        <p>
          The color is only as good as your two assumptions, marked in the left panel. A big
          favorable area can come from an exit P/E set too high or a growth baseline that is too
          optimistic. It can also mean the market sees a risk you do not, such as a slowdown or
          earnings at a peak. A big unfavorable area can come from an exit P/E set too low or a
          baseline dragged down by one weak quarter. It can also mean the market expects an
          acceleration, or earnings that are only temporarily low. Either way, ask why the market
          disagrees with you.
        </p>
      ),
      highlight: ["shade"],
      levers: ["exitPe", "baseline"],
      you: (e) => colorLine(e),
    },
    {
      title: "Check for bends in the history",
      body: (
        <p>
          A reported line that flattens near today signals slowing growth, even if the longer
          trailing growth still looks strong.
        </p>
      ),
      highlight: ["hist"],
      you: (e) => {
        const h = pathsOf(e).history;
        const now = h.at(-1)?.ttmYoy.value ?? null;
        const before = h.at(-4)?.ttmYoy.value ?? null;
        if (now === null || before === null) {
          return null;
        }
        const shape =
          now < before - 0.02
            ? "the line is flattening."
            : now > before + 0.02
              ? "the line is getting steeper."
              : "the slope is steady.";
        return (
          <>
            TTM EPS growth went from <b>{signedPct(before, 0)}</b> three quarters ago to{" "}
            <b>{signedPct(now, 0)}</b> now: {shape}
          </>
        );
      },
      takeaway: (e) => {
        const gap = pathsOf(e).future.at(-1)?.gap.value ?? null;
        if (gap === null) {
          return null;
        }
        return (
          <>
            <strong>{ticker} in one line:</strong> at your {baselineName(e)}, EPS ends{" "}
            {usd(Math.abs(gap))} per share {gap < 0 ? "short of" : "above"} what the price needs.
          </>
        );
      },
    },
  ];

  return (
    <Panel
      id="earnings-trajectory"
      className="chart"
      title="Earnings trajectory"
      sub={
        <>
          TTM EPS by quarter, then two paths: your baseline continuing, and the path the price
          requires.
          <InfoTip label="About shading">
            <span className="sw" style={swatch("var(--bad)")} /> Shading: the required path ends
            above the baseline, so the price needs more growth than the trend gives.{" "}
            <span className="sw" style={swatch("var(--good)")} /> Shading: the baseline beats it.
            Hover the chart for values.
          </InfoTip>
        </>
      }
    >
      <div className="keys">
        <span style={swatch(ink)}>Reported TTM EPS</span>
        <span className="dash" style={swatch(muted)}>
          Baseline continues
        </span>
        <span className="dash" style={swatch(accent)}>
          Required path
        </span>
      </div>
      {ev.earningsPaths.value === null ? (
        <p className="note">Needs positive EPS to project.</p>
      ) : (
        <>
          <TrajectoryChart paths={ev.earningsPaths.value} years={n} />
          <p className="note">
            Projections start from today&apos;s TTM EPS of{" "}
            <FromLevers provenance={provenance.ttmEps}>{usd(levers.ttmEps)}</FromLevers> and
            compound quarterly.
          </p>
        </>
      )}
      <Walkthrough
        id="earnings-trajectory"
        name="the earnings trajectory"
        launcher="Walk me through the earnings trajectory with my numbers"
        steps={steps}
        watchOut="Only the end point of each path matters for the return. The smooth curves are one way to get there. Real earnings arrive in uneven steps."
      />
    </Panel>
  );
}

function shadingLine(e: Evaluation, ticker: string, targetReturn: number): ReactNode {
  const p = pathsOf(e);
  const gap = p.future.at(-1)?.gap.value ?? null;
  const at = e.atBaseline.value;
  const pe = e.exitPe.inUse.value?.pe ?? null;
  const target = pct(targetReturn);
  if (gap === null || at === null) {
    return null;
  }
  const name = baselineName(e);
  const g = pct(e.baselines.growth.value);
  if (gap < 0) {
    return (
      <>
        <strong>Short for {ticker}:</strong> at your {name} of {g}, EPS ends <b>{usd(-gap)}</b> per
        share short of what the price needs. In plain terms, if growth stays at that pace and the
        market pays {times(pe)} when you sell, you earn about <b>{signedPct(at.yearlyReturn)}</b> a
        year instead of your {target}. <strong>Take-away:</strong> for your target, the company must
        grow faster than you assume, or the market must pay a higher P/E than {times(pe)}.
      </>
    );
  }
  return (
    <>
      <strong>Ahead for {ticker}:</strong> at your {name} of {g}, EPS ends <b>{usd(gap)}</b> per
      share above what the price needs. In plain terms, if growth stays at that pace and the market
      pays {times(pe)} when you sell, you earn about <b>{signedPct(at.yearlyReturn)}</b> a year,
      more than your {target} target. <strong>Take-away:</strong> the price does not ask for more
      growth than you expect, which leaves room for things to go a bit worse.
    </>
  );
}

function colorLine(e: Evaluation): ReactNode {
  const p = pathsOf(e);
  const flip = e.atBaseline.value?.exitPeNeeded.value ?? null;
  const r = e.required.growth.value;
  const g = e.baselines.growth.value;
  const pe = e.exitPe.inUse.value?.pe ?? null;
  if (flip === null || r === null || g === null) {
    return null;
  }
  const good = p.gapTone.value === "good";
  const dq = p.history.at(-1)?.ttmYoy.value ?? null;
  const lead = (
    <>
      The color flips at a <b>{times(flip)}</b> exit P/E (yours is {times(pe)}) or at{" "}
      <b>{pct(r)}</b> growth (yours is {pct(g)}).{" "}
    </>
  );
  if (good) {
    return (
      <>
        {lead}So it holds unless the market pays less than {times(flip)} or growth drops below{" "}
        {pct(r)}. Ask why the market is more cautious than you.
      </>
    );
  }
  return (
    <>
      {lead}
      {dq !== null && dq < 0 ? (
        <>
          Earnings fell <b>{pct(-dq)}</b> over the last year, so part of the gap may come from
          temporarily low earnings: a recovery would close it fast.{" "}
        </>
      ) : (
        <>
          So the price assumes either faster growth than {pct(g)} or a P/E of at least {times(flip)}{" "}
          when you sell.{" "}
        </>
      )}
      Ask what the market expects that the numbers do not show yet.
    </>
  );
}

function TrajectoryChart({ paths, years: n }: { paths: Paths; years: number }) {
  const { history, future, scale } = paths;
  const { history: sources } = useEvaluation();
  const known = new Map(sources.quarters.map((q) => [q.periodEnd, q.ttmEpsKnownOn]));
  const F = future.length - 1;
  const tot = Math.max(1, history.length - 1 + F);
  const X = (i: number) => x0 + (i / tot) * (x1 - x0);
  const Y = (v: number) => y1 - ((v - scale.min) / (scale.max - scale.min)) * (y1 - y0);
  const pt = (i: number, v: number | null): readonly [number, number] | null =>
    v === null ? null : [X(i), Y(v)];
  const today = history.length - 1;

  const tipFor = (i: number): ReactNode => {
    if (i <= today) {
      const q = history[i];
      if (q === undefined) {
        return null;
      }
      const on = known.get(q.periodEnd) ?? null;
      return (
        <>
          <b>
            {q.label}
            {i === today ? " (today)" : ""}
          </b>
          <TipRow label="Reported TTM EPS" value={usd(q.ttmEps.value)} />
          {q.ttmYoy.value !== null && (
            <TipRow label="vs a year earlier" value={signedPct(q.ttmYoy.value)} />
          )}
          {on !== null && <div className="tm">Known from the filings on {on}</div>}
        </>
      );
    }
    const f = future[i - today];
    if (f === undefined) {
      return null;
    }
    return (
      <>
        <b>
          {f.label === null ? "" : `${f.label} · `}
          {yearsOut(f.quartersAhead)}
        </b>
        <TipRow label="Baseline EPS" value={usd(f.baseline.value)} />
        {f.required.value !== null && (
          <>
            <TipRow label="Required EPS" value={usd(f.required.value)} />
            <TipRow label="Gap" value={signedUsd(f.gap.value)} />
          </>
        )}
      </>
    );
  };
  const { chartProps, layer, live } = useColumns(
    { count: tot + 1, x: X, width: (x1 - x0) / tot, y0, y1, start: today },
    tipFor,
    paths,
  );

  const hp = history.map((q, i) => pt(i, q.ttmEps.value));
  const bp = future.map((f, k) => pt(today + k, f.baseline.value));
  const rp = future.map((f, k) => pt(today + k, f.required.value));
  const end = future.at(-1);
  const endBase = end?.baseline.value ?? null;
  const endReq = end?.required.value ?? null;
  const tone = paths.gapTone.value;
  const shade =
    tone !== null && bp.every((p) => p !== null) && rp.every((p) => p !== null)
      ? `${linePath(bp)} L${rp
          .slice()
          .reverse()
          .map((p) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`)
          .join(" L")} Z`
      : null;
  const xn = X(today);
  const step = scale.step;
  const decimals = step < 1 ? (step < 0.1 ? 2 : 1) : 0;
  const yearTicks = [...history.map((q) => q.label), ...future.slice(1).map((f) => f.label)];
  const [yb, yr] = endLabels(Y(endBase ?? 0), Y(endReq ?? 0));

  const first = history[0];
  const label =
    `TTM EPS by quarter from ${first?.label ?? ""} to ${history.at(-1)?.label ?? ""}, ` +
    `then two paths over ${years(n)}: the baseline path reaches ${usd(endBase)} and ` +
    (endReq === null
      ? "the required path is not available."
      : `the required path reaches ${usd(endReq)}.`);

  return (
    <>
      <svg viewBox={`0 0 ${String(W)} ${String(H)}`} role="img" aria-label={label} {...chartProps}>
        <YAxis axis={scale} y={Y} x0={x0} x1={x1} format={(v) => usd(v, decimals)} />
        <rect x={xn} y={y0} width={x1 - xn} height={y1 - y0} fill="var(--sunk)" />
        <line x1={xn} y1={y0} x2={xn} y2={y1} stroke={muted} strokeDasharray="2 3" />
        <text x={xn + 4} y={y0 + 10} textAnchor="start">
          today
        </text>
        {yearTicks.map((l, i) => {
          const m = l === null ? null : /^Q1 FY(\d{2})$/.exec(l);
          if (m === null || (tot > 30 && Number(m[1]) % 2 === 1)) {
            return null;
          }
          return (
            <text key={i} x={X(i)} y={y1 + 16} textAnchor="middle">
              FY{m[1]}
            </text>
          );
        })}
        {shade !== null && tone !== null && (
          <path
            className={`L-shade ${tone}`}
            data-walk="shade"
            d={shade}
            fill={`color-mix(in srgb, var(--${tone}) 12%, transparent)`}
          />
        )}
        <path data-walk="hist" d={linePath(hp)} fill="none" stroke={ink} strokeWidth={2.2} />
        <path
          data-walk="base"
          d={linePath(bp)}
          fill="none"
          stroke={muted}
          strokeWidth={2}
          strokeDasharray="5 4"
        />
        {endReq !== null && (
          <path
            data-walk="req"
            d={linePath(rp)}
            fill="none"
            stroke={accent}
            strokeWidth={2.2}
            strokeDasharray="5 4"
          />
        )}
        {hp.map(
          (p, i) =>
            p !== null && (
              <circle
                key={i}
                data-walk="hist"
                cx={p[0]}
                cy={p[1]}
                r={i === today ? 3.5 : 2.2}
                fill={ink}
              />
            ),
        )}
        {future.map((f, k) => {
          if (k === 0 || k % 4 !== 0) {
            return null;
          }
          const b = bp[k];
          const r = rp[k];
          return (
            <g key={k}>
              {b && <circle data-walk="base" cx={b[0]} cy={b[1]} r={2.6} fill={muted} />}
              {r && <circle data-walk="req" cx={r[0]} cy={r[1]} r={2.6} fill={accent} />}
            </g>
          );
        })}
        {endBase !== null && (
          <text data-walk="base" x={x1 + 6} y={yb} style={{ fill: muted }}>
            {usd(endBase)}
          </text>
        )}
        {endReq !== null && (
          <text data-walk="req" x={x1 + 6} y={yr} style={{ fill: accent, fontWeight: 500 }}>
            {usd(endReq)}
          </text>
        )}
        {layer}
      </svg>
      {live}
    </>
  );
}
