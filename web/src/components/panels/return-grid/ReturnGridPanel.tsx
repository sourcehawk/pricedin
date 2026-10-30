"use client";

import {
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import type { Evaluation, Grid, GridOutcome, GridTag } from "@/engine";
import { pct, pp, signedPct, times, usd } from "@/components/format";
import { baselineName } from "@/components/rail/notes";
import { Seg } from "@/components/rail/Levers";
import { InfoTip } from "@/components/tip/InfoTip";
import { useTipHandlers } from "@/components/tip/TipProvider";
import { Walkthrough } from "@/components/walkthrough/Walkthrough";
import { useWalkthrough } from "@/components/walkthrough/WalkthroughProvider";
import { useEvaluation, useView } from "@/state/StockProvider";
import { Panel } from "../Panel";
import {
  exampleRow,
  growthLabel,
  peLabel,
  tagOrder,
  tagStyles,
  taggedRow,
  toneClass,
  type GridView,
} from "./layout";
import styles from "./ReturnGrid.module.css";
import { gridSteps } from "./steps";

const walkId = "return-grid";
/** The steps that box one cell: where the example row meets the user's column, and the YOU cell. */
const meetStep = 2;
const youStep = 3;

const views: readonly { value: GridView; label: string }[] = [
  { value: "cagr", label: "Annual return" },
  { value: "cum", label: "Total return" },
  { value: "px", label: "Future price" },
];

/** cellShade is the value of a cell's --shade: the strength of its tone's color, as a CSS percentage. */
function cellShade(tone: "good" | "warn" | "bad", diff: number): string {
  const k = (x: number, span: number) => Math.min(Math.max(x / span, 0), 1);
  switch (tone) {
    case "good":
      return `${String(Math.round(22 + k(diff - 0.015, 0.12) * 48))}%`;
    case "bad":
      return `${String(Math.round(20 + k(-diff - 0.015, 0.15) * 48))}%`;
    default:
      return "38%";
  }
}

function cellText(c: GridOutcome, view: GridView): string {
  switch (view) {
    case "cum":
      return signedPct(c.totalReturn, 0);
    case "px":
      return `$${Math.round(c.exitPrice).toLocaleString("en-US")}`;
    default:
      return signedPct(c.yearlyReturn, 0);
  }
}

/** tagClass is the class list of a row tag; the custom tag's style is this panel's own. */
function tagClass(t: GridTag): string {
  return t === "custom" ? `tag ${styles.custom ?? ""}` : `tag ${tagStyles[t].className}`;
}

const swatch = (color: string) => <i style={{ background: color }} aria-hidden="true" />;

/** useOutline returns the props that outline the row of tag while the element is hovered or focused, with its tooltip. */
function useOutline(
  tag: GridTag | null,
  setOutlined: (t: GridTag | null) => void,
  tip: () => ReactNode,
) {
  const h = useTipHandlers(tip);
  const on = () => {
    setOutlined(tag);
  };
  const off = () => {
    setOutlined(null);
  };
  return {
    ...h,
    tabIndex: 0,
    onPointerEnter: (e: React.PointerEvent) => {
      on();
      h.onPointerEnter(e);
    },
    onPointerLeave: (e: React.PointerEvent) => {
      off();
      h.onPointerLeave(e);
    },
    onFocus: (e: React.FocusEvent) => {
      on();
      h.onFocus(e);
    },
    onBlur: (e: React.FocusEvent) => {
      off();
      h.onBlur(e);
    },
  };
}

function RowTag({
  tag,
  tip,
  setOutlined,
}: {
  tag: GridTag;
  tip: () => ReactNode;
  setOutlined: (t: GridTag | null) => void;
}) {
  const props = useOutline(tag, setOutlined, tip);
  return (
    <span className={tagClass(tag)} {...props}>
      {tagStyles[tag].label}
    </span>
  );
}

function LegendEntry({
  tag,
  tip,
  setOutlined,
  children,
}: {
  tag: GridTag | null;
  tip: () => ReactNode;
  setOutlined: (t: GridTag | null) => void;
  children: ReactNode;
}) {
  const props = useOutline(tag, setOutlined, tip);
  return <span {...props}>{children}</span>;
}

function Cell({
  cell,
  className,
  walk,
  style,
  text,
  tip,
  focusable,
  r,
  c,
}: {
  cell: GridOutcome;
  className: string;
  walk: string;
  style: CSSProperties;
  text: string;
  tip: () => ReactNode;
  focusable: boolean;
  r: number;
  c: number;
}) {
  const h = useTipHandlers(tip);
  return (
    <td
      className={className}
      data-walk={walk}
      data-r={r}
      data-c={c}
      data-tone={cell.tone}
      style={style}
      tabIndex={focusable ? 0 : -1}
      {...h}
    >
      {text}
    </td>
  );
}

interface Box {
  key: string;
  tag: GridTag;
  left: number;
  top: number;
  width: number;
  height: number;
}

/** useRowBoxes measures the outline of each tagged row of the table, again whenever the table or its wrapper changes size. */
function useRowBoxes(grid: Grid, view: GridView) {
  const wrap = useRef<HTMLDivElement>(null);
  const table = useRef<HTMLTableElement>(null);
  const [boxes, setBoxes] = useState<Box[]>([]);
  useLayoutEffect(() => {
    const w = wrap.current;
    const t = table.current;
    if (w === null || t === null) {
      return;
    }
    const measure = () => {
      const body = t.tBodies[0];
      const wb = w.getBoundingClientRect();
      const out: Box[] = [];
      grid.rows.forEach((row, ri) => {
        const cells = body?.rows[ri]?.querySelectorAll("td");
        const first = cells?.[0];
        const last = cells?.[cells.length - 1];
        tagOrder
          .filter((tag) => row.tags.includes(tag))
          .forEach((tag, k) => {
            const a = first?.getBoundingClientRect();
            const b = last?.getBoundingClientRect();
            // Two tags on one row get nested outlines.
            const off = 2 + k * 3;
            out.push({
              key: `${String(ri)}-${tag}`,
              tag,
              left: (a?.left ?? 0) - wb.left + w.scrollLeft - off,
              top: (a?.top ?? 0) - wb.top + w.scrollTop - off,
              width: (b?.right ?? 0) - (a?.left ?? 0) + off * 2,
              height: (a?.height ?? 0) + off * 2,
            });
          });
      });
      setBoxes(out);
    };
    measure();
    if (typeof ResizeObserver === "undefined") {
      return;
    }
    const ro = new ResizeObserver(measure);
    // The table keeps its own width and is centered, so a wider wrapper moves it without resizing it.
    ro.observe(t);
    ro.observe(w);
    return () => {
      ro.disconnect();
    };
  }, [grid, view]);
  return { wrap, table, boxes };
}

function onArrows(e: KeyboardEvent<HTMLTableElement>) {
  const td = e.target instanceof HTMLElement ? e.target.closest("td[data-r]") : null;
  const moves: Record<string, [number, number]> = {
    ArrowUp: [-1, 0],
    ArrowDown: [1, 0],
    ArrowLeft: [0, -1],
    ArrowRight: [0, 1],
  };
  const m = moves[e.key];
  if (!(td instanceof HTMLElement) || m === undefined) {
    return;
  }
  const r = Number(td.dataset.r) + m[0];
  const c = Number(td.dataset.c) + m[1];
  const to = e.currentTarget.querySelector(`td[data-r="${String(r)}"][data-c="${String(c)}"]`);
  // The walkthrough also listens to the arrow keys: preventDefault tells it this one is used.
  e.preventDefault();
  if (to instanceof HTMLElement) {
    to.focus();
  }
}

function tagTip(ev: Evaluation, grid: Grid, tag: GridTag, r: number): ReactNode {
  const pe = grid.columns[grid.you.value?.column ?? -1]?.exitPe ?? ev.exitPe.inUse.value?.pe;
  const peText = pe === undefined ? "n/a" : `${peLabel(pe)}×`;
  const ri = taggedRow(grid, tag);
  const row = grid.rows[ri];
  const youCol = grid.you.value?.column ?? grid.columns.findIndex((c) => c.exitPe === pe);
  const at = signedPct(row?.cells[youCol]?.yearlyReturn);
  const outlined = `The outlined row (${growthLabel(grid, ri)})`;
  const b = ev.baselines;
  switch (tag) {
    case "required":
      return (
        <>
          <b>Req: required growth</b>The price needs {pct(ev.required.growth.value)} EPS growth a
          year for your {pct(r)} target at a {peText} exit. {outlined} is the nearest one. In your
          column, rows above it fall short and rows below beat your target.
        </>
      );
    case "weighted":
      return (
        <>
          <b>Wtd: weighted trend</b>EPS grew {pct(b.weighted.value)} a year over the last 8
          quarters, with the latest quarters counting most. {outlined} shows what you earn if that
          pace continues: {at} a year at your {peText} exit.
        </>
      );
    case "today":
      return (
        <>
          <b>Today: latest quarter</b>EPS grew {pct(b.today.value)} in the latest quarter, compared
          with the same quarter a year earlier. {outlined} shows what you earn if that pace
          continues: {at} a year at your {peText} exit.
        </>
      );
    case "trailing":
      return (
        <>
          <b>Trl: trailing growth</b>TTM EPS grew {pct(b.trailing.growth.value)} a year on average
          over the last {String(b.trailing.months)} months. {outlined} shows what you earn if the
          company returns to that pace: {at} a year at your {peText} exit.
        </>
      );
    case "custom":
      return (
        <>
          <b>Custom: your own rate</b>Your custom growth of {pct(b.custom.value)} a year. {outlined}{" "}
          holds your YOU cell: {at} a year at your {peText} exit.
        </>
      );
  }
}

/**
 * ReturnGridPanel is the return grid: the yearly return for each EPS growth
 * rate (rows) and exit P/E (columns), toned against the target return. The
 * owner's own case is the YOU cell; the rows of the requirement and the
 * baselines carry tags that outline their row on hover.
 */
export function ReturnGridPanel() {
  const { evaluation: ev, state, levers } = useEvaluation();
  const [view, setView] = useView<GridView>("grid.view", "cagr");
  const [outlined, setOutlined] = useState<GridTag | null>(null);
  const walk = useWalkthrough();
  const sym = state.ticker ?? "The stock";
  const r = state.targetReturn;
  const n = state.holdingYears;
  const grid = ev.returnGrid.value;

  const steps = gridSteps({ sym, r, n, price: levers.price, eps: levers.ttmEps });
  const walkthrough = (
    <Walkthrough
      id={walkId}
      name="the return grid"
      launcher="Walk me through this grid with my numbers"
      intro="The grid shows what you would earn under many combinations of earnings growth and the P/E you sell at, so you can see how much each one matters."
      watchOut="Colors compare each return with your target, not with zero, so a cell that falls short can still be a gain. Growth is an average: a bad year followed by strong recovery can land in the same cell as steady growth, but it is harder to hold through."
      steps={steps}
    />
  );

  return (
    <Panel
      id="return-grid"
      title="Return grid"
      sub={
        <>
          {String(n)}-year outcomes for {sym} from {usd(levers.price)} and {usd(levers.ttmEps)} EPS.
          Colors compare each yearly return with your {pct(r)} target, not with zero.
          <InfoTip label="About the return grid">
            Rows: average yearly EPS growth over the holding period. Columns: the P/E you sell at.
            Each cell is the return from today&apos;s price. Hover or tap a cell for the full
            calculation. The cell marked YOU is your baseline growth at your exit P/E. Hover a row
            tag (Req, Wtd, Today, Trl) to outline that row and see what it means.
          </InfoTip>
        </>
      }
      actions={
        <Seg
          label="What the cells show"
          options={views}
          value={view}
          onChange={setView}
          className={`seg ${styles.views ?? ""}`}
        />
      }
    >
      {grid === null ? (
        <p className="note">
          {ev.returnGrid.reason === "needs_positive_eps"
            ? "The return grid needs positive EPS."
            : "The return grid needs an exit P/E above zero."}
        </p>
      ) : (
        <GridTable
          ev={ev}
          grid={grid}
          view={view}
          r={r}
          n={n}
          eps={levers.ttmEps}
          outlined={outlined}
          setOutlined={setOutlined}
          box={
            walk.active?.id === walkId
              ? walk.active.step === meetStep
                ? "meet"
                : walk.active.step === youStep
                  ? "you"
                  : null
              : null
          }
        />
      )}
      {grid !== null && <Legend ev={ev} grid={grid} r={r} setOutlined={setOutlined} />}
      {walkthrough}
    </Panel>
  );
}

function GridTable({
  ev,
  grid,
  view,
  r,
  n,
  eps,
  outlined,
  setOutlined,
  box,
}: {
  ev: Evaluation;
  grid: Grid;
  view: GridView;
  r: number;
  n: number;
  eps: number;
  outlined: GridTag | null;
  setOutlined: (t: GridTag | null) => void;
  /** The cell the walkthrough boxes: the example row in the user's column, or the YOU cell. */
  box: "meet" | "you" | null;
}) {
  const { wrap, table, boxes } = useRowBoxes(grid, view);
  const you = grid.you.value;
  const youCol = you?.column ?? -1;
  const ex = exampleRow(grid);
  const peNow = ev.peNow.value;
  const dividends = ev.required.dividends;

  const cellTip = (ri: number, ci: number): ReactNode => {
    const row = grid.rows[ri];
    const col = grid.columns[ci];
    const c = row?.cells[ci];
    if (row === undefined || col === undefined || c === undefined) {
      return null;
    }
    const pe = peLabel(col.exitPe);
    const multiple = col.today
      ? "This column sells at roughly today's multiple. "
      : peNow !== null && col.exitPe < peNow
        ? `Multiple shrinks from ${times(peNow)}, which costs return. `
        : `Multiple expands from ${times(peNow)}, which adds return. `;
    return (
      <>
        <b>
          {pct(row.growth)} EPS growth a year, sell at {pe}×
        </b>
        <div className="row">
          <span>EPS now → in {String(n)}Y</span>
          <span>
            {usd(eps)} → {usd(row.epsAtExit)}
          </span>
        </div>
        <div className="row">
          <span>Share price at exit</span>
          <span>
            {usd(row.epsAtExit)} × {pe} = {usd(c.exitPrice)}
          </span>
        </div>
        {dividends > 0 && (
          <div className="row">
            <span>Dividends received</span>
            <span>{usd(dividends)}</span>
          </div>
        )}
        <div className="row">
          <span>Total return</span>
          <span>{signedPct(c.totalReturn)}</span>
        </div>
        <div className="row">
          <span>Per year</span>
          <span>{signedPct(c.yearlyReturn)}</span>
        </div>
        <div className="row">
          <span>vs {pct(r)} target</span>
          <span>{pp(c.vsTarget)}</span>
        </div>
        <div className="tm">
          {multiple}Growth is an average: any path to {usd(row.epsAtExit)} gives the same result.
        </div>
      </>
    );
  };

  return (
    <div className="hm-wrap" ref={wrap}>
      <table
        className={`hm ${styles.grid ?? ""}`}
        ref={table}
        onKeyDown={onArrows}
        aria-label="Returns for each EPS growth rate (rows) and exit P/E (columns)"
      >
        <thead>
          <tr>
            <th className="corner" scope="col" data-walk="you-col">
              Exit P/E
            </th>
            {grid.columns.map((c, ci) => (
              <th
                key={c.exitPe}
                scope="col"
                className={ci === youCol ? "colsel" : undefined}
                data-walk={ci === youCol ? "you-col" : undefined}
              >
                {peLabel(c.exitPe)}×
                {c.today && (
                  <>
                    <br />
                    <span style={{ fontSize: "9.5px" }}>today</span>
                  </>
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {grid.rows.map((row, ri) => {
            const isYouRow = you?.row === ri;
            const rowKeys = row.tags.map((t) => `row-${t}`);
            const headKeys = [
              ...rowKeys,
              ...(ri === ex ? ["ex-row"] : []),
              ...(isYouRow ? ["you-cell"] : []),
            ];
            const tags = tagOrder.filter((t) => row.tags.includes(t));
            return (
              <tr key={row.growth}>
                <th scope="row" data-walk={headKeys.join(" ") || undefined}>
                  {tags.length > 0 && (
                    <span className="tags">
                      {tags.map((t) => (
                        <RowTag
                          key={t}
                          tag={t}
                          setOutlined={setOutlined}
                          tip={() => tagTip(ev, grid, t, r)}
                        />
                      ))}
                    </span>
                  )}
                  {growthLabel(grid, ri)}
                </th>
                {row.cells.map((c, ci) => {
                  const isYou = isYouRow && ci === youCol;
                  const tone = toneClass(c.tone);
                  const keys = [
                    ...rowKeys,
                    `tone-${tone}`,
                    ...(ri === ex ? ["ex-row"] : []),
                    ...(ci === youCol ? ["you-col"] : []),
                    ...(isYouRow ? ["you-row"] : []),
                    ...(isYou ? ["you-cell"] : []),
                  ];
                  const boxed =
                    (box === "meet" && ri === ex && ci === youCol) || (box === "you" && isYou);
                  const cls = [tone, isYou ? "sel" : "", boxed ? "tmeet" : ""]
                    .filter((x) => x !== "")
                    .join(" ");
                  return (
                    <Cell
                      key={ci}
                      cell={c}
                      r={ri}
                      c={ci}
                      className={cls}
                      walk={keys.join(" ")}
                      style={{ "--shade": cellShade(tone, c.vsTarget) } as CSSProperties}
                      text={cellText(c, view)}
                      tip={() => cellTip(ri, ci)}
                      focusable={you === null ? ri === 0 && ci === 0 : isYou}
                    />
                  );
                })}
              </tr>
            );
          })}
        </tbody>
        <tfoot>
          <tr>
            <th className="corner" scope="row" data-walk="ex-row">
              EPS growth
            </th>
            <td colSpan={grid.columns.length} />
          </tr>
        </tfoot>
      </table>
      {boxes.map((b) => (
        <div
          key={b.key}
          className={`rowmark ${styles.mark ?? ""}${outlined === b.tag ? " on" : ""}`}
          data-row-tag={b.tag}
          aria-hidden="true"
          style={{
            color: tagStyles[b.tag].color,
            left: b.left,
            top: b.top,
            width: b.width,
            height: b.height,
          }}
        />
      ))}
    </div>
  );
}

function Legend({
  ev,
  grid,
  r,
  setOutlined,
}: {
  ev: Evaluation;
  grid: Grid;
  r: number;
  setOutlined: (t: GridTag | null) => void;
}) {
  const ab = ev.atBaseline.value;
  const pe = ev.exitPe.inUse.value?.pe;
  const present = tagOrder.filter((t) => taggedRow(grid, t) >= 0);
  return (
    <div className="legend">
      <span>
        {swatch("color-mix(in srgb, var(--bad) 55%, var(--surface))")}Below target by over 1.5 pp
      </span>
      <span>{swatch("color-mix(in srgb, var(--warn) 45%, var(--surface))")}Near target</span>
      <span>{swatch("color-mix(in srgb, var(--good) 55%, var(--surface))")}Above target</span>
      {grid.you.value !== null && (
        <LegendEntry
          tag={null}
          setOutlined={setOutlined}
          tip={() => (
            <>
              <b>YOU: your case</b>Your growth baseline ({baselineName(ev)},{" "}
              {pct(ev.baselines.growth.value)}) at your exit P/E of{" "}
              {pe === undefined ? "n/a" : `${peLabel(pe)}×`}. That earns{" "}
              {signedPct(ab?.yearlyReturn)} a year against your {pct(r)} target. Hover any cell to
              see its calculation.
            </>
          )}
        >
          <span className="tag you">YOU</span>your case
        </LegendEntry>
      )}
      {present.map((t) => {
        const s = tagStyles[t];
        return (
          <LegendEntry key={t} tag={t} setOutlined={setOutlined} tip={() => tagTip(ev, grid, t, r)}>
            <span className={tagClass(t)}>{s.label}</span>
            {s.legend}
          </LegendEntry>
        );
      })}
    </div>
  );
}
