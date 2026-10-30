"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { joinNames } from "@/components/copy";
import { multiple, shareCount, signedPct, times, usd } from "@/components/format";
import { colorVar } from "@/components/Swatch";
import { EstimateTag, estimateNote, newestFiling } from "@/components/Sourced";
import { useTip, useTipHandlers } from "@/components/tip/TipProvider";
import type { QuarterSources, SecSource, Source } from "@/api/types";
import type { Chart, ChartPoint, ShareEvent } from "@/engine";
import {
  bandSegments,
  bottom,
  dayLabel,
  dayTime,
  height,
  labelSlots,
  left,
  linePath,
  monthTicks,
  nearestIndex,
  pointsPath,
  priceTick,
  right,
  shadePath,
  spanLabel,
  top,
  width,
  xScale,
  yScale,
} from "./geometry";
import type { HoverStore } from "./hover";
import type { OverlayKey } from "./overlays";

/** Marker is a share-count event with the filing that reports its share count, when known. */
export interface Marker {
  event: ShareEvent;
  href: string | null;
  /** The sources of the share counts before and after the change; null without one. */
  sources: { from: Source | null; to: Source | null };
}

export interface PricePlotProps {
  chart: Chart;
  ticker: string;
  shown: ReadonlySet<OverlayKey>;
  /** The multiple of the band to lift; null lifts none. */
  lifted: number | null;
  /** The multiple of the band under the pointer or the focus; null when it leaves. */
  onBand: (multiple: number | null) => void;
  hover: HoverStore;
  markers: readonly Marker[];
  /** The price provider of the daily closes; null when the history names none. */
  feed: string | null;
  /** The sources of the history's quarters, oldest first. */
  quarters: readonly QuarterSources[];
}

type Y = (v: number) => number;

const overlayNames: Record<OverlayKey, string> = {
  eps: "TTM EPS",
  pe: "P/E",
  r12: "the 12-month price change",
  g12: "the 12-month EPS growth",
  bands: "P/E bands",
  sh: "share-change markers",
};

function draw(chart: Chart, bandsOn: boolean) {
  const pts = chart.points;
  const x1 = right(bandsOn);
  const ts = pts.map((p) => dayTime(p.date));
  const t0 = ts[0] ?? 0;
  const t1 = ts.at(-1) ?? t0;
  const x = xScale(t0, t1, x1);
  const xs = ts.map(x);
  const yp = yScale(chart.priceAxis);
  const ye = chart.epsRange.value === null ? null : yScale(chart.epsRange.value);
  const ype = chart.peRange.value === null ? null : yScale(chart.peRange.value);
  const yc = yScale(chart.changeRange);
  const eps = pts.map((p) => p.ttmEps);

  const price = linePath(
    xs,
    pts.map((p) => p.close),
    yp,
    false,
  );
  const lastX = xs.at(-1) ?? left;
  const last = pts.at(-1);

  const axis: { y: number; label: string }[] = [];
  const { min, max, step } = chart.priceAxis;
  if (step > 0) {
    for (let k = 0; k <= 100; k++) {
      const v = min + k * step;
      if (v > max + step * 1e-6) {
        break;
      }
      axis.push({ y: yp(v), label: priceTick(v) });
    }
  }

  const bands = chart.bands.value;
  let bandLines: { multiple: number; d: string }[] = [];
  let shade = "";
  let labels: { multiple: number; y: number }[] = [];
  if (bandsOn && bands !== null) {
    const runs = new Map(
      bands.multiples.map((b) => [b.multiple, bandSegments(xs, eps, b.multiple, yp)]),
    );
    bandLines = bands.multiples.map((b) => ({
      multiple: b.multiple,
      d: pointsPath(runs.get(b.multiple) ?? []),
    }));
    const pair = bands.current.value;
    if (pair !== null) {
      shade = shadePath(runs.get(pair.upper) ?? [], runs.get(pair.lower) ?? []);
    }
    // A band whose TTM EPS stops being positive ends before the right edge.
    const ends: { multiple: number; y: number }[] = [];
    for (const [multiple, r] of runs) {
      const end = r.at(-1)?.at(-1);
      if (end !== undefined && end[0] === lastX) {
        ends.push({ multiple, y: end[1] });
      }
    }
    labels = labelSlots(ends);
  }

  return {
    x1,
    xs,
    yp,
    ye,
    ype,
    yc,
    axis,
    ticks: monthTicks(t0, t1, chart.range).map((k) => ({ x: x(k.t), label: k.label })),
    price,
    fill:
      price === ""
        ? ""
        : `${price}L${lastX.toFixed(1)},${String(bottom)}L${String(left)},${String(bottom)}Z`,
    lastDot: last === undefined ? null : { x: lastX, y: yp(last.close) },
    eps: ye === null ? "" : linePath(xs, eps, ye, true),
    pe:
      ype === null
        ? ""
        : linePath(
            xs,
            pts.map((p) => p.pe.value),
            ype,
            false,
          ),
    r12: linePath(
      xs,
      pts.map((p) => p.priceChange12m.value),
      yc,
      false,
    ),
    g12: linePath(
      xs,
      pts.map((p) => p.epsGrowth12m.value),
      yc,
      true,
    ),
    zero: yc(0),
    bandLines,
    shade,
    labels,
    markerX: (e: ShareEvent) => x(dayTime(e.periodEnd)),
  };
}

type Drawing = ReturnType<typeof draw>;

/** The circles that follow the hovered day, one per line that has a value per day. */
function dotSpecs(g: Drawing): {
  key: OverlayKey | "price";
  color: string;
  y: Y | null;
  v: (p: ChartPoint) => number | null;
}[] {
  return [
    { key: "price", color: "var(--accent)", y: g.yp, v: (p) => p.close },
    { key: "eps", color: "var(--ink)", y: g.ye, v: (p) => p.ttmEps },
    { key: "pe", color: "var(--warn)", y: g.ype, v: (p) => p.pe.value },
    { key: "r12", color: "var(--accent)", y: g.yc, v: (p) => p.priceChange12m.value },
    { key: "g12", color: "var(--ink)", y: g.yc, v: (p) => p.epsGrowth12m.value },
  ];
}

function TipRow({
  color,
  dash = false,
  label,
  value,
}: {
  color: string;
  dash?: boolean;
  label: string;
  value: string;
}) {
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

/** DayTip is the tooltip of a hovered day; the tooltip's background is the ink color, so ink lines use the surface color. */
function DayTip({
  p,
  shown,
  feed,
  quarters,
}: {
  p: ChartPoint;
  shown: ReadonlySet<OverlayKey>;
  feed: string | null;
  quarters: readonly QuarterSources[];
}) {
  return (
    <>
      <b>{dayLabel(p.date)}</b>
      <TipRow color="var(--accent)" label="Close" value={usd(p.close)} />
      {shown.has("eps") && <TipRow color="var(--surface)" label="TTM EPS" value={usd(p.ttmEps)} />}
      {shown.has("pe") && <TipRow color="var(--warn)" label="P/E" value={times(p.pe.value)} />}
      {shown.has("r12") && (
        <TipRow
          color="var(--accent)"
          dash
          label="Price change, 12M"
          value={signedPct(p.priceChange12m.value)}
        />
      )}
      {shown.has("g12") && (
        <TipRow
          color="var(--surface)"
          dash
          label="EPS growth, 12M"
          value={signedPct(p.epsGrowth12m.value)}
        />
      )}
      <div className="tm">
        {feed === null
          ? "Daily close"
          : `Close from ${feed.charAt(0).toUpperCase()}${feed.slice(1)}`}
        , adjusted for splits. {ttmNote(p, quarters)}
      </div>
    </>
  );
}

/**
 * ttmNote names the quarters behind the TTM EPS of a day, as the engine
 * gives them, and the newest filing among them. The chart plots their latest
 * filed values, which can be amendments filed after that day.
 */
function ttmNote(p: ChartPoint, quarters: readonly QuarterSources[]): string {
  const last = p.ttmQuarters.at(-1);
  const q = quarters.find((x) => x.label === last);
  if (q === undefined || q.ttmEpsKnownOn === null) {
    return "TTM EPS adds up the last four reported quarters.";
  }
  const behind = quarters.filter((x) => p.ttmQuarters.includes(x.label));
  let newest: SecSource | null = null;
  for (const x of behind) {
    const f = x.eps === null ? null : newestFiling(x.eps);
    if (f !== null && (newest === null || f.filed > newest.filed)) {
      newest = f;
    }
  }
  const filing =
    newest === null ? "" : `; the newest is Form ${newest.form}, filed ${newest.filed}`;
  return `TTM EPS adds up ${joinNames(p.ttmQuarters)}, known from the filings on ${q.ttmEpsKnownOn}, with the values of the latest filings${filing}. P/E is the close over this TTM EPS.`;
}

/** dayLine is the day's values as one sentence, for the screen reader. */
function dayLine(p: ChartPoint, shown: ReadonlySet<OverlayKey>): string {
  const parts = [`close ${usd(p.close)}`];
  if (shown.has("eps")) {
    parts.push(`TTM EPS ${usd(p.ttmEps)}`);
  }
  if (shown.has("pe")) {
    parts.push(`P/E ${times(p.pe.value)}`);
  }
  if (shown.has("r12")) {
    parts.push(`price change 12M ${signedPct(p.priceChange12m.value)}`);
  }
  if (shown.has("g12")) {
    parts.push(`EPS growth 12M ${signedPct(p.epsGrowth12m.value)}`);
  }
  return `${dayLabel(p.date)}: ${parts.join(", ")}`;
}

function MarkerMark({ m, x }: { m: Marker; x: number }) {
  const e = m.event;
  const dilution = e.kind === "dilution";
  const color = dilution ? "var(--bad)" : "var(--good)";
  // The badge stays inside the view box; the guide stays on the event's day.
  const bx = Math.min(Math.max(x, 27), width - 27);
  const change = signedPct(e.change, 1);
  const tip = useTip();
  const [node, setNode] = useState<Element | null>(null);
  // A tap on a link opens it at once, so the first tap only shows the tip.
  const tapOpen = useRef<boolean | null>(null);
  const hideTip = tip.hide;
  const open = node !== null && tip.owner === node;
  // A tip that a tap opened stays until a tap elsewhere, also inside the chart.
  useEffect(() => {
    if (!open) {
      return;
    }
    const onDown = (ev: PointerEvent) => {
      if (!(ev.target instanceof Node) || !node.contains(ev.target)) {
        hideTip(node);
      }
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
    };
  }, [open, node, hideTip]);
  const handlers = useTipHandlers(() => (
    <>
      <b>
        {e.label}: {e.kind}
      </b>
      <div className="row">
        <span>Shares</span>
        <span>
          {shareCount(e.from)}
          <EstimateTag source={m.sources.from} /> → {shareCount(e.to)}
          <EstimateTag source={m.sources.to} />
        </span>
      </div>
      <div className="row">
        <span>Change</span>
        <span>{change}</span>
      </div>
      <div className="tm">
        {dilution
          ? "Each share now owns a smaller part of the profit or loss."
          : "Each remaining share owns a larger part of the profit or loss."}
        {m.href !== null && " Click, or tap again, to open the filing."}
      </div>
    </>
  ));
  const label = `${e.label}: ${e.kind}, ${shareCount(e.from)}${estimateNote(m.sources.from)} to ${shareCount(e.to)}${estimateNote(m.sources.to)} diluted shares, ${change}`;
  const body = (
    <>
      <line
        x1={x}
        x2={x}
        y1={top}
        y2={bottom}
        stroke={color}
        strokeWidth={1.2}
        strokeDasharray="2 3"
        pointerEvents="none"
      />
      {/* Ink on a light tint of the tone keeps the small text readable in both themes. */}
      <rect
        x={bx - 26}
        y={bottom - 22}
        width={52}
        height={17}
        rx={8.5}
        style={{ fill: `color-mix(in srgb, ${color} 22%, var(--surface))`, stroke: color }}
      />
      <text
        x={bx}
        y={bottom - 10}
        textAnchor="middle"
        style={{ fill: "var(--ink)", fontWeight: 600, fontSize: 10.5 }}
      >
        {change}
      </text>
    </>
  );
  const className = `pxev ${e.kind}`;
  if (m.href !== null) {
    return (
      <a
        href={m.href}
        target="_blank"
        rel="noopener noreferrer"
        className={className}
        data-walk="events"
        aria-label={label}
        {...handlers}
        ref={(el) => {
          handlers.ref(el);
          setNode(el);
        }}
        onPointerDown={(ev) => {
          tapOpen.current = ev.pointerType === "touch" ? tip.owner === node : null;
          handlers.onPointerDown(ev);
        }}
        onClick={(ev) => {
          if (tapOpen.current === false) {
            ev.preventDefault();
          }
          // The rule covers one tap; a later click from the keyboard has no pointerdown.
          tapOpen.current = null;
          handlers.onClick(ev);
        }}
      >
        {body}
      </a>
    );
  }
  return (
    <g
      className={className}
      data-walk="events"
      role="img"
      tabIndex={0}
      aria-label={label}
      {...handlers}
    >
      {body}
    </g>
  );
}

/**
 * PricePlot draws the price chart. Hovering it, or moving through the days
 * with the arrow keys, Home and End once it has the focus, writes the day
 * into hover, moves the guide line and shows the day's tooltip, without a
 * re-render. Hovering or focusing a band calls onBand.
 */
export function PricePlot({
  chart,
  ticker,
  shown,
  lifted,
  onBand,
  hover,
  markers,
  feed,
  quarters,
}: PricePlotProps) {
  const bandsOn = shown.has("bands");
  const g = useMemo(() => draw(chart, bandsOn), [chart, bandsOn]);
  const uid = useId().replace(/[^A-Za-z0-9_-]/g, "");
  const clip = `px-clip-${uid}`;
  const grad = `px-fill-${uid}`;
  const svg = useRef<SVGSVGElement>(null);
  const guide = useRef<SVGLineElement>(null);
  const dots = useRef<(SVGCircleElement | null)[]>([]);
  const tip = useTip();
  const specs = useMemo(() => dotSpecs(g), [g]);
  const nearest = chart.bands.value?.nearest.value ?? null;

  const live = useRef<HTMLParagraphElement>(null);
  const lastAt = useRef<{ x: number; y: number } | null>(null);
  const { hide: hideTip, show: showTip, move: moveTip } = tip;

  const clear = useCallback(() => {
    hover.set(null);
    guide.current?.setAttribute("visibility", "hidden");
    for (const d of dots.current) {
      d?.setAttribute("visibility", "hidden");
    }
    if (svg.current !== null) {
      hideTip(svg.current);
    }
  }, [hover, hideTip]);

  // The hovered index means another day once the days change.
  const days = `${ticker} ${chart.start} ${chart.end} ${String(chart.points.length)}`;
  useEffect(() => {
    clear();
  }, [days, clear]);

  // A tapped day or band stays after the finger lifts, until a tap elsewhere.
  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      const own = svg.current;
      if (own === null || !(e.target instanceof Node) || own.contains(e.target)) {
        return;
      }
      onBand(null);
      if (hover.get() !== null) {
        clear();
      }
    };
    document.addEventListener("pointerdown", onDown);
    return () => {
      document.removeEventListener("pointerdown", onDown);
    };
  }, [clear, hover, onBand]);

  const showDay = useCallback(
    (i: number, at: { x: number; y: number }, refresh = false) => {
      const p = chart.points[i];
      const cx = g.xs[i];
      const owner = svg.current;
      if (p === undefined || cx === undefined || owner === null) {
        return;
      }
      const changed = refresh || hover.get() !== i;
      hover.set(i);
      lastAt.current = at;
      guide.current?.setAttribute("x1", String(cx));
      guide.current?.setAttribute("x2", String(cx));
      guide.current?.setAttribute("visibility", "visible");
      specs.forEach((s, k) => {
        const d = dots.current[k];
        const v = s.v(p);
        const on = s.key === "price" || shown.has(s.key);
        if (d === null || d === undefined) {
          return;
        }
        if (!on || v === null || s.y === null) {
          d.setAttribute("visibility", "hidden");
          return;
        }
        d.setAttribute("cx", String(cx));
        d.setAttribute("cy", String(s.y(v)));
        d.setAttribute("visibility", "visible");
      });
      if (changed) {
        showTip(owner, <DayTip p={p} shown={shown} feed={feed} quarters={quarters} />, at);
      } else {
        moveTip(owner, at);
      }
    },
    [chart, g, specs, shown, feed, quarters, hover, showTip, moveTip],
  );

  // The tooltip sits at fixed screen coordinates, so a scroll of the page
  // drops the day. The plot's own scroll that reveals a day keeps it.
  const revealing = useRef(false);
  useEffect(() => {
    const onScroll = (e: Event) => {
      if (hover.get() === null) {
        return;
      }
      if (revealing.current && e.target === svg.current?.parentElement) {
        revealing.current = false;
        return;
      }
      clear();
    };
    document.addEventListener("scroll", onScroll, { capture: true, passive: true });
    return () => {
      document.removeEventListener("scroll", onScroll, { capture: true });
    };
  }, [clear, hover]);

  // New values for the same days, or other lines, redraw the selected day.
  useEffect(() => {
    const i = hover.get();
    if (i !== null && lastAt.current !== null) {
      showDay(i, lastAt.current, true);
    }
  }, [showDay, hover]);

  const onPointer = (e: React.PointerEvent<SVGSVGElement>) => {
    const target = e.target as Element;
    if (target.closest("[data-bm], .pxev") !== null) {
      clear();
      return;
    }
    const b = e.currentTarget.getBoundingClientRect();
    if (!(b.width > 0)) {
      return;
    }
    const vx = ((e.clientX - b.left) / b.width) * width;
    if (vx < left - 4 || vx > g.x1 + 4) {
      clear();
      return;
    }
    showDay(nearestIndex(g.xs, vx), { x: e.clientX, y: e.clientY });
  };

  const onKeyDown = (e: React.KeyboardEvent<SVGSVGElement>) => {
    if (e.target !== e.currentTarget) {
      return;
    }
    const n = g.xs.length;
    const cur = hover.get() ?? n - 1;
    let i: number;
    switch (e.key) {
      case "ArrowLeft":
        i = Math.max(0, cur - 1);
        break;
      case "ArrowRight":
        i = Math.min(n - 1, cur + 1);
        break;
      case "Home":
        i = 0;
        break;
      case "End":
        i = n - 1;
        break;
      case "Escape":
        if (hover.get() !== null) {
          e.preventDefault();
          clear();
        }
        return;
      default:
        return;
    }
    e.preventDefault();
    const cx = g.xs[i] ?? left;
    const box = e.currentTarget.parentElement;
    if (box !== null && box.scrollWidth > box.clientWidth) {
      const at = (cx / width) * e.currentTarget.getBoundingClientRect().width;
      if (at < box.scrollLeft + 20 || at > box.scrollLeft + box.clientWidth - 20) {
        const before = box.scrollLeft;
        box.scrollLeft = Math.max(0, at - box.clientWidth / 2);
        // An unchanged position fires no scroll event to reset the flag.
        revealing.current = box.scrollLeft !== before;
      }
    }
    const b = e.currentTarget.getBoundingClientRect();
    showDay(i, { x: b.left + (cx / width) * b.width, y: b.top + b.height * 0.3 });
    const p = chart.points[i];
    if (live.current !== null && p !== undefined) {
      live.current.textContent = dayLine(p, shown);
    }
  };

  const bandOf = (t: EventTarget): number | null => {
    const el = (t as Element).closest("[data-bm]");
    return el === null ? null : Number(el.getAttribute("data-bm"));
  };

  const names = (["eps", "pe", "r12", "g12", "bands", "sh"] as const)
    .filter((k) => shown.has(k))
    .map((k) => overlayNames[k]);
  const first = chart.points[0];
  const last = chart.points.at(-1);
  const summary =
    `${ticker} daily close ${spanLabel(chart.start, chart.end, chart.range)}, from ${usd(first?.close)} to ${usd(last?.close)}` +
    (names.length > 0 ? `, with ${joinNames(names)}` : "");

  const bandName = (m: number) => {
    const band = chart.bands.value?.multiples.find((b) => b.multiple === m);
    return `${multiple(m)} band: ${usd(band?.priceToday.value)} today`;
  };
  const walkBand = (m: number) => (m === nearest ? "bands band-near" : "bands");
  const bh = (m: number) => (m === lifted ? " bh" : "");
  const on = (k: OverlayKey) => shown.has(k);

  const content = (
    <>
      <defs>
        <linearGradient id={grad} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" stopOpacity={0.16} />
          <stop offset="1" stopColor="var(--accent)" stopOpacity={0} />
        </linearGradient>
        <clipPath id={clip}>
          <rect x={left} y={top} width={g.x1 - left} height={bottom - top} />
        </clipPath>
      </defs>
      <g role="img" aria-label={summary}>
        {g.axis.map((a) => (
          <g key={a.label}>
            <line x1={left} x2={g.x1} y1={a.y} y2={a.y} stroke="var(--line)" strokeWidth={1} />
            <text x={left - 6} y={a.y + 3.5} textAnchor="end">
              {a.label}
            </text>
          </g>
        ))}
        {g.ticks.map((k) => (
          <g key={k.label}>
            <line x1={k.x} x2={k.x} y1={bottom} y2={bottom + 4} stroke="var(--muted)" />
            <text x={k.x} y={bottom + 17} textAnchor={k.x > width - 24 ? "end" : "middle"}>
              {k.label}
            </text>
          </g>
        ))}
        <g clipPath={`url(#${clip})`}>
          {g.shade !== "" && (
            <path
              className="L-band"
              data-walk="bands"
              d={g.shade}
              fill="var(--accent)"
              fillOpacity={0.08}
            />
          )}
          {g.bandLines.map((b) => (
            <path
              key={b.multiple}
              className={`L-band bline${bh(b.multiple)}`}
              data-bm={b.multiple}
              data-walk={walkBand(b.multiple)}
              d={b.d}
              fill="none"
              stroke="var(--muted)"
              strokeWidth={1.2}
              strokeDasharray="3 3"
            />
          ))}
          <path className="L-price" data-walk="price" d={g.fill} fill={`url(#${grad})`} />
          {(on("r12") || on("g12")) && (
            <>
              <line
                x1={left}
                x2={g.x1}
                y1={g.zero}
                y2={g.zero}
                stroke="var(--muted)"
                strokeWidth={1}
                opacity={0.5}
              />
              <text x={g.x1 - 4} y={g.zero - 4} textAnchor="end" style={{ fontSize: 9.5 }}>
                0% (12M)
              </text>
            </>
          )}
          {on("r12") && (
            <path
              className="L-r12"
              data-walk="r12"
              d={g.r12}
              fill="none"
              stroke="var(--accent)"
              strokeWidth={1.3}
              strokeDasharray="4 3"
              opacity={0.8}
            />
          )}
          {on("g12") && (
            <path
              className="L-g12"
              data-walk="g12"
              d={g.g12}
              fill="none"
              stroke="var(--ink)"
              strokeWidth={1.5}
              strokeDasharray="4 3"
              opacity={0.8}
            />
          )}
          {on("pe") && g.pe !== "" && (
            <path
              className="L-pe"
              data-walk="pe"
              d={g.pe}
              fill="none"
              stroke="var(--warn)"
              strokeWidth={1.4}
              opacity={0.9}
            />
          )}
          {on("eps") && g.eps !== "" && (
            <path
              className="L-eps"
              data-walk="eps"
              d={g.eps}
              fill="none"
              stroke="var(--ink)"
              strokeWidth={1.8}
            />
          )}
          <path
            className="L-price"
            data-walk="price"
            d={g.price}
            fill="none"
            stroke="var(--accent)"
            strokeWidth={2}
            strokeLinejoin="round"
          />
        </g>
        {g.lastDot !== null && (
          <circle
            className="L-price"
            data-walk="price"
            cx={g.lastDot.x}
            cy={g.lastDot.y}
            r={3.5}
            fill="var(--accent)"
          />
        )}
      </g>
      <line
        ref={guide}
        data-testid="price-guide"
        x1={0}
        x2={0}
        y1={top}
        y2={bottom}
        stroke="var(--ink)"
        strokeWidth={1}
        opacity={0.35}
        visibility="hidden"
        pointerEvents="none"
      />
      {specs.map((s, k) => (
        <circle
          key={s.key}
          ref={(el) => {
            dots.current[k] = el;
          }}
          r={3.5}
          fill={s.color}
          stroke="var(--surface)"
          strokeWidth={1.5}
          visibility="hidden"
          pointerEvents="none"
        />
      ))}
      <rect
        data-testid="price-capture"
        x={left}
        y={top}
        width={g.x1 - left}
        height={bottom - top}
        fill="transparent"
      />
      {g.bandLines.length > 0 && (
        <g clipPath={`url(#${clip})`}>
          {g.bandLines.map((b) => {
            // A band without a label at the right edge takes the focus on its line.
            const focus = g.labels.every((l) => l.multiple !== b.multiple)
              ? {
                  role: "img",
                  tabIndex: 0,
                  "aria-label": bandName(b.multiple),
                  onFocus: () => {
                    onBand(b.multiple);
                  },
                  onBlur: () => {
                    onBand(null);
                  },
                }
              : {};
            return (
              <path
                key={b.multiple}
                className={`L-band bhit${bh(b.multiple)}`}
                data-bm={b.multiple}
                d={b.d}
                fill="none"
                stroke="transparent"
                strokeWidth={12}
                {...focus}
              />
            );
          })}
        </g>
      )}
      {g.labels.map((l) => {
        return (
          <g
            key={l.multiple}
            className={`blab${bh(l.multiple)}`}
            data-bm={l.multiple}
            data-walk={walkBand(l.multiple)}
            role="img"
            tabIndex={0}
            aria-label={bandName(l.multiple)}
            onFocus={() => {
              onBand(l.multiple);
            }}
            onBlur={() => {
              onBand(null);
            }}
          >
            <rect
              className="L-band"
              x={g.x1 + 4}
              y={l.y - 7}
              width={38}
              height={14}
              rx={3}
              fill="var(--surface)"
              stroke="var(--line)"
            />
            <text
              className="L-band"
              x={g.x1 + 23}
              y={l.y + 3.5}
              textAnchor="middle"
              style={{ fontSize: 10, fill: "var(--ink)" }}
            >
              {multiple(l.multiple)}
            </text>
          </g>
        );
      })}
      {on("sh") &&
        markers.map((m) => <MarkerMark key={m.event.periodEnd} m={m} x={g.markerX(m.event)} />)}
    </>
  );

  return (
    <>
      <svg
        ref={svg}
        viewBox={`0 0 ${String(width)} ${String(height)}`}
        role="group"
        aria-label={`${ticker} price chart. The arrow keys, Home and End move through the days.`}
        tabIndex={0}
        onPointerMove={onPointer}
        onPointerDown={(e) => {
          if (e.pointerType === "touch" && bandOf(e.target) === null) {
            onBand(null);
          }
          onPointer(e);
        }}
        onPointerCancel={() => {
          clear();
          onBand(null);
        }}
        onPointerLeave={(e) => {
          if (e.pointerType !== "touch") {
            clear();
            onBand(null);
          }
        }}
        onPointerOver={(e) => {
          const m = bandOf(e.target);
          if (m !== null) {
            onBand(m);
          }
        }}
        onPointerOut={(e) => {
          if (e.pointerType !== "touch" && bandOf(e.target) !== null) {
            onBand(null);
          }
        }}
        onKeyDown={onKeyDown}
        onBlur={(e) => {
          if (e.target === e.currentTarget) {
            clear();
          }
        }}
      >
        {content}
      </svg>
      <p ref={live} className="sr-only" role="status" />
    </>
  );
}
