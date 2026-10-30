import type { ReactNode } from "react";
import type { Evaluation, Grid, GridOutcome } from "@/engine";
import { multiple, pct, points, signedPct, times, usd, years } from "@/components/format";
import { baselineLabel, baselineName, dividendsReach, orDividends } from "@/components/copy";
import { Swatch } from "@/components/Swatch";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import { exampleRow, exitColumn, tagStyles, toneClass } from "./layout";

/** StepContext holds the lever values that the steps quote. */
export interface StepContext {
  sym: string;
  /** The target return. */
  r: number;
  /** The holding period in years. */
  n: number;
  price: number;
  eps: number;
}

const tag = (t: "required" | "weighted" | "today" | "trailing") => (
  <span className={`tag ${tagStyles[t].className}`}>{tagStyles[t].label}</span>
);

/** The grid and the column of the exit P/E in use; a step that needs them shows no line without them. */
interface At {
  grid: Grid;
  col: number;
  pe: string;
  /** The cells of the user's column, one per row. */
  cells: GridOutcome[];
}

function at(ev: Evaluation): At | null {
  const grid = ev.returnGrid.value;
  if (grid === null) {
    return null;
  }
  const ci = exitColumn(grid, ev);
  const col = grid.columns[ci];
  if (col === undefined) {
    return null;
  }
  return {
    grid,
    col: ci,
    pe: multiple(col.exitPe),
    cells: grid.rows.map((r) => r.cells[ci]).filter((c) => c !== undefined),
  };
}

/** withGrid runs line with the grid of ev, and shows nothing without one. */
const withGrid =
  (line: (a: At, ev: Evaluation) => ReactNode) =>
  (ev: Evaluation): ReactNode => {
    const a = at(ev);
    return a === null ? null : line(a, ev);
  };

/** gridSteps are the walkthrough steps of the return grid. */
export function gridSteps({ sym, r, n, price, eps }: StepContext): WalkthroughStep[] {
  const target = pct(r);
  const noGrowth = (
    <>
      Your dividends alone reach <b>{target}</b> a year, so the price requires no EPS growth.
    </>
  );
  return [
    {
      title: "Rows are EPS growth rates",
      highlight: ["ex-row"],
      body: (
        <p>
          Each row is one average yearly EPS growth rate over your holding period of {years(n)},
          read from the label on the left. The highlighted row is one example.
        </p>
      ),
      you: withGrid(({ grid }) => {
        const row = grid.rows[exampleRow(grid)];
        if (row === undefined) {
          return null;
        }
        return (
          <>
            Example: the <b>{pct(row.growth)}</b> row assumes EPS grows {pct(row.growth)} a year for{" "}
            {years(n)}, from <b>{usd(eps)}</b> to <b>{usd(row.epsAtExit)}</b>.
          </>
        );
      }),
    },
    {
      title: "Columns are exit P/E multiples",
      highlight: ["you-col"],
      body: (
        <p>
          Each column is one P/E at which you could sell at the end of your holding period,{" "}
          {years(n)} from now, read from the label at the top. The highlighted column is your exit
          P/E; its label at the top is marked.
        </p>
      ),
      you: withGrid(({ pe }, ev) => (
        <>
          Your column is <b>{pe}</b>: when you sell in {years(n)}, the share price is assumed to be{" "}
          {pe.replace("×", "")} times the EPS at that point. Today&apos;s P/E is{" "}
          {times(ev.peNow.value)}.
        </>
      )),
    },
    {
      title: "Where they meet is your return",
      highlight: ["ex-row", "you-col"],
      body: (
        <p>
          The cell where a row and a column cross is the yearly return from today&apos;s price for
          that growth rate and that exit P/E. The boxed cell is the return for the example row at
          your exit P/E. Hover over any cell in the grid, or tap it, to see the calculation.
        </p>
      ),
      you: withGrid(({ grid, col, pe }, ev) => {
        const row = grid.rows[exampleRow(grid)];
        const c = row?.cells[col];
        if (row === undefined || c === undefined) {
          return null;
        }
        const d = ev.required.dividends;
        return (
          <>
            Here: {pct(row.growth)} growth and a {pe} exit. EPS reaches {usd(row.epsAtExit)}, so the
            price is {usd(row.epsAtExit)} × {pe.replace("×", "")} = <b>{usd(c.exitPrice)}</b>
            {d > 0 ? `, plus ${usd(d)} in dividends` : ""}. From today&apos;s {usd(price)} that is{" "}
            <b>{signedPct(c.yearlyReturn)}</b> a year.
          </>
        );
      }),
    },
    {
      title: "YOU marks your own case",
      highlight: ["you-cell"],
      body: (
        <p>
          The cell marked YOU sits in the row for your growth baseline and the column for your exit
          P/E. It is the return you get if your own assumptions come true.
        </p>
      ),
      you: withGrid(({ pe }, ev) => {
        const ab = ev.atBaseline.value;
        if (ab === null) {
          return null;
        }
        return (
          <>
            Your case: <b>{pct(ev.baselines.growth.value)}</b> growth ({baselineName(ev)}) at{" "}
            <b>{pe}</b> earns <b>{signedPct(ab.yearlyReturn)}</b> a year,{" "}
            <b>{points(ab.vsTarget)} pp</b> {ab.vsTarget >= 0 ? "above" : "below"} your {target}{" "}
            target.
          </>
        );
      }),
    },
    {
      title: (
        <>
          <Swatch color="var(--bad)" />
          Cells that fall short of your target
        </>
      ),
      highlight: ["tone-bad"],
      body: (
        <p>
          In such a cell, that growth rate and exit P/E give you more than 1.5 points a year less
          than your target return. It does not mean a loss: the cell can still be a gain, just
          smaller than you asked for. The deeper the shade, the bigger the shortfall.
        </p>
      ),
      you: withGrid(({ grid, cells, pe }) => {
        const short = grid.rows.filter((_, i) => cells[i]?.tone === "bad");
        const close = grid.rows.find((_, i) => cells[i] !== undefined && cells[i].tone !== "bad");
        const last = short.at(-1);
        if (last === undefined) {
          return (
            <>
              Nothing in your {pe} column falls short by more than 1.5 pp: every growth rate shown
              gets you close to your target.
            </>
          );
        }
        return (
          <>
            In your {pe} column, every row up to <b>{pct(last.growth)}</b> growth falls short of
            your target by more than 1.5 pp.{" "}
            {close === undefined ? (
              "No row shown gets close to your target."
            ) : (
              <>
                You need roughly <b>{pct(close.growth)}</b> a year before the return gets close to
                your {target} target.
              </>
            )}
          </>
        );
      }),
    },
    {
      title: (
        <>
          <Swatch color="var(--good)" />
          Cells that beat your target
        </>
      ),
      highlight: ["tone-good"],
      body: (
        <p>
          In such a cell, you earn more than 1.5 points a year above your target return. The deeper
          the shade, the more room for error. Look at where these cells start in your column: if
          they only start well below your row, the stock needs faster growth than your baseline.
        </p>
      ),
      you: withGrid(({ grid, cells, pe }, ev) => {
        const first = grid.rows.find((_, i) => cells[i]?.tone === "good");
        const g = ev.baselines.growth.value;
        if (first === undefined) {
          return (
            <>
              No row in your {pe} column beats your target by more than 1.5 pp: even{" "}
              {pct(grid.rows.at(-1)?.growth)} growth does not.
            </>
          );
        }
        if (g !== null && first.growth <= g) {
          return (
            <>Your baseline row already beats your target: the stock beats it if growth holds.</>
          );
        }
        if (g === null) {
          return (
            <>
              Your column beats your target from <b>{pct(first.growth)}</b> growth.
            </>
          );
        }
        return (
          <>
            Your column beats your target from <b>{pct(first.growth)}</b> growth,{" "}
            <b>{points(first.growth - g)} points</b> above your {pct(g)} baseline.
          </>
        );
      }),
    },
    {
      title: (
        <>
          <Swatch color="var(--warn)" />
          Follow the cells near your target
        </>
      ),
      highlight: ["tone-warn"],
      body: (
        <p>
          These cells are combinations that land within 1.5 points of your target. Cells on the side
          of the shortfall fall short, cells on the other side beat it. A wide area of cells that
          beat your target around your own cell gives a margin of safety.
        </p>
      ),
      you: orDividends(
        noGrowth,
        withGrid(({ grid, cells }, ev) => {
          const near = grid.rows.filter(
            (_, i) => cells[i] !== undefined && toneClass(cells[i].tone) === "warn",
          );
          const R = pct(ev.required.growth.value);
          if (near.length === 0) {
            const crosses =
              cells.some((c) => c.tone === "bad") && cells.some((c) => c.tone === "good");
            return crosses ? (
              <>
                Your column jumps from falling short to beating your target between two rows. The
                exact requirement, {R}, falls between them.
              </>
            ) : (
              <>No row in your column lands within 1.5 pp of your target.</>
            );
          }
          return (
            <>
              In your column the {near.length > 1 ? "rows" : "row"} near your target{" "}
              {near.length > 1 ? "are" : "is"} <b>{near.map((x) => pct(x.growth)).join(" and ")}</b>
              . That is where the price lands you right at your target, close to the <b>{R}</b> the
              price requires.
            </>
          );
        }),
      ),
    },
    {
      title: "Question your cell's tone",
      highlight: ["you-cell"],
      levers: ["baseline", "exitPe"],
      body: (
        <p>
          The tone of your YOU cell rests on two assumptions, marked in the left panel: your growth
          baseline and your exit P/E. Check how far each one is from changing the tone. If a small
          change flips it, the result is fragile.
        </p>
      ),
      you: orDividends(
        noGrowth,
        withGrid(({ pe }, ev) => {
          const ab = ev.atBaseline.value;
          if (ab === null) {
            return null;
          }
          const tone = toneClass(ab.tone);
          const word = {
            bad: "falls short of your target",
            warn: "is near your target",
            good: "beats your target",
          }[tone];
          const color = { bad: "var(--bad)", warn: "var(--warn)", good: "var(--good)" }[tone];
          return (
            <>
              Your cell <Swatch color={color} />
              <b>{word}</b>: {signedPct(ab.yearlyReturn)} a year against your {target} target. You
              reach the target at <b>{pct(ev.required.growth.value)}</b> growth (yours is{" "}
              {pct(ev.baselines.growth.value)}) or at a <b>{times(ab.exitPeNeeded.value)}</b> exit
              P/E (yours is {pe}).{" "}
              {ab.fragile
                ? "One small change flips the tone, so treat the result as fragile."
                : "Neither is a small step, so the result is not fragile."}
            </>
          );
        }),
      ),
    },
    {
      title: "Move across a row to see what the P/E does",
      highlight: ["you-row"],
      body: (
        <p>
          The growth stays the same, only the selling P/E changes. If the cells to the left quickly
          fall short of your target, your return depends on the market keeping a high P/E.
        </p>
      ),
      you: withGrid(({ grid }, ev) => {
        const row = grid.rows[grid.you.value?.row ?? -1];
        const a = row?.cells[0];
        const b = row?.cells.at(-1);
        const first = grid.columns[0];
        const last = grid.columns.at(-1);
        if (!a || !b || !first || !last) {
          return null;
        }
        return (
          <>
            At your {pct(ev.baselines.growth.value)} baseline, selling at {multiple(first.exitPe)}{" "}
            earns <b>{signedPct(a.yearlyReturn)}</b> a year and selling at {multiple(last.exitPe)}{" "}
            earns <b>{signedPct(b.yearlyReturn)}</b> a year. The exit P/E alone moves your yearly
            return by <b>{points(b.yearlyReturn - a.yearlyReturn, 0)} points</b>.
          </>
        );
      }),
    },
    {
      title: "Move down a column to see what growth does",
      highlight: ["you-col"],
      body: (
        <p>
          The P/E stays the same, only growth changes. The row where the cells start to beat your
          target is roughly the growth the price requires at that P/E.
        </p>
      ),
      you: withGrid(({ grid, pe }) => {
        const s = grid.columnSensitivity.value;
        if (s === null) {
          return null;
        }
        return (
          <>
            In your {pe} column, each <b>{pct(s.growthStep)}</b> of extra growth a year adds about{" "}
            <b>{points(s.returnChange)} points</b> of yearly return.
          </>
        );
      }),
    },
    {
      title: <>{tag("required")} marks the required growth</>,
      highlight: ["row-required"],
      body: (
        <p>
          The required growth (Req) row is the growth rate closest to what today&apos;s price
          requires for your target at your exit P/E. In your column, rows above it fall short of
          your target and rows below it beat it.
        </p>
      ),
      you: orDividends(
        noGrowth,
        withGrid(({ pe }, ev) => {
          const gap = ev.gap.value;
          return (
            <>
              Today&apos;s price requires <b>{pct(ev.required.growth.value)}</b> EPS growth a year
              for your {target} target at {pe}. Your baseline is {pct(ev.baselines.growth.value)}
              {gap === null ? (
                "."
              ) : (
                <>
                  , <b>{points(gap)} pp</b> {gap >= 0 ? "above it." : "short of it."}
                </>
              )}
            </>
          );
        }),
      ),
    },
    {
      title: <>{tag("weighted")} marks the weighted trend</>,
      highlight: ["row-weighted"],
      body: (
        <p>
          Weighted (Wtd), Today and Trailing (Trl) are the growth baseline options from the left
          panel, placed on the grid so you can compare them. The weighted trend (Wtd) row is closest
          to the company&apos;s year-over-year EPS growth over the last 8 quarters, with the latest
          quarters counting most. If it sits above the required growth (Req) row, the current pace
          falls short of what the price needs.
        </p>
      ),
      you: (ev) => {
        const w = ev.baselines.weighted.value;
        const R = ev.required.growth.value;
        if (w === null || R === null) {
          return null;
        }
        return (
          <>
            The weighted trend is <b>{pct(w)}</b>,{" "}
            {w >= R ? (
              "enough for"
            ) : (
              <>
                <b>{points(R - w)} points</b> short of
              </>
            )}{" "}
            what the price requires.
          </>
        );
      },
    },
    {
      title: <>{tag("today")} marks the latest quarter</>,
      highlight: ["row-today"],
      body: (
        <p>
          The Today row is closest to the EPS growth of the latest quarter compared with the same
          quarter a year earlier. If it sits above the weighted trend (Wtd) row, growth has been
          slowing lately.
        </p>
      ),
      you: (ev) => {
        const t = ev.baselines.today.value;
        const w = ev.baselines.weighted.value;
        if (t === null || w === null) {
          return null;
        }
        return (
          <>
            The latest quarter grew <b>{pct(t)}</b>,{" "}
            {t < w - 0.005
              ? "below the weighted trend, so growth slowed recently"
              : t > w + 0.005
                ? "above the weighted trend, so growth picked up recently"
                : "in line with the weighted trend"}
            .
          </>
        );
      },
    },
    {
      title: <>{tag("trailing")} marks trailing growth</>,
      highlight: ["row-trailing"],
      levers: ["trailingMonths"],
      body: (
        <p>
          The trailing growth (Trl) row is closest to the average yearly growth of TTM EPS over the
          months set next to Trailing growth in the left panel (12 by default). A longer period
          gives a steadier number, so compare it with the weighted trend (Wtd) and Today to see
          whether growth is speeding up or slowing down.
        </p>
      ),
      you: (ev) => {
        const t = ev.baselines.trailing;
        const lt = t.growth.value;
        const w = ev.baselines.weighted.value;
        if (lt === null || w === null) {
          return null;
        }
        return (
          <>
            Over the last {String(t.months)} months TTM EPS grew <b>{pct(lt)}</b> a year,{" "}
            {lt > w + 0.005
              ? "faster than the weighted trend: growth was stronger earlier in the period"
              : lt < w - 0.005
                ? "slower than the weighted trend: growth has picked up"
                : "about the same as the weighted trend"}
            .
          </>
        );
      },
      takeaway: (ev) => {
        if (dividendsReach(ev)) {
          return (
            <>
              <strong>{sym} in one line:</strong> your dividends alone reach {target} a year, so the
              price requires no EPS growth.
            </>
          );
        }
        const gap = ev.gap.value;
        const pe = ev.exitPe.inUse.value?.pe;
        return (
          <>
            <strong>{sym} in one line:</strong> the price requires {pct(ev.required.growth.value)}{" "}
            EPS growth a year for {target} at a {multiple(pe)} exit. The weighted trend is{" "}
            {pct(ev.baselines.weighted.value)} and the latest quarter{" "}
            {pct(ev.baselines.today.value)}.{" "}
            {gap === null ? null : (
              <>
                Your {baselineLabel(ev)} of {pct(ev.baselines.growth.value)}{" "}
                {gap >= 0
                  ? "clears the bar."
                  : "falls short, so the price assumes growth speeds up."}
              </>
            )}
          </>
        );
      },
    },
  ];
}
