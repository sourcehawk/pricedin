import type { ReactNode } from "react";
import { coefficient, multiple, pct, signedPct, times, usd } from "@/components/format";
import { LineSample, Swatch } from "@/components/Swatch";
import type { WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import type { Chart, Evaluation } from "@/engine";
import { spanLabel } from "./geometry";

/** span names the period of the chart, such as "over 3Y". */
function span(c: Chart): string {
  return spanLabel(c.start, c.end, c.range);
}

/** Span starts a sentence with the period of the chart, such as "Over 3Y". */
function Span({ c }: { c: Chart }) {
  const s = span(c);
  return <>{s.charAt(0).toUpperCase() + s.slice(1)}</>;
}

/** withChart gives a line only when the evaluation has a chart. */
function withChart(f: (c: Chart, ev: Evaluation) => ReactNode) {
  return (ev: Evaluation) => (ev.priceChart.value === null ? null : f(ev.priceChart.value, ev));
}

function ends(c: Chart) {
  const f = c.points[0];
  const l = c.points.at(-1);
  if (f === undefined || l === undefined) {
    throw new Error("a chart without points");
  }
  return { f, l };
}

const linkMeaning: Record<string, string> = {
  strong: "the price change closely tracked earnings growth.",
  moderate: "the price change tracked earnings growth in part.",
  weak: "the price change hardly tracked earnings growth.",
  negative: "the price change tended to move against earnings growth.",
};

const usual: Record<string, string> = {
  above: "more expensive than usual",
  below: "cheaper than usual",
  close: "close to its usual level",
};

/** why says in one sentence what the P/E change of the range means. */
function why(c: Chart): string {
  const share = c.earningsShareOfGain.value;
  switch (c.peTrend.value) {
    case "expanded":
      if (share !== null && share <= 1) {
        const e = Math.round(share * 100);
        return `The price did better than earnings: measured on a log scale, about ${String(e)}% of the price gain came from higher earnings and ${String(100 - e)}% from a higher P/E.`;
      }
      return "The price did better than earnings, so the stock got more expensive compared with its earnings.";
    case "contracted":
      return "Earnings did better than the price, so the stock got cheaper compared with its earnings.";
    case "steady":
      return "The P/E moved less than 5%, so the price roughly followed earnings.";
    default:
      return "";
  }
}

/** bandsStep is the index of the step that lifts the band nearest today's P/E. */
export const bandsStep = 6;

/** priceSteps are the steps of the price chart's walkthrough, in the prototype's order. */
export function priceSteps(ticker: string): WalkthroughStep[] {
  return [
    {
      title: (
        <>
          <LineSample color="var(--accent)" /> The price line
        </>
      ),
      body: (
        <p>
          The share price at each day&apos;s close. It is the only line with an axis, so read its
          value on the left.
        </p>
      ),
      highlight: ["price"],
      you: withChart((c) => {
        const { f, l } = ends(c);
        return (
          <>
            <Span c={c} /> the price went from <b>{usd(f.close)}</b> to <b>{usd(l.close)}</b>,{" "}
            <b>{signedPct(c.priceChange)}</b>. Every line is switched on for this walkthrough; your
            own choice comes back at the end.
          </>
        );
      }),
    },
    {
      title: (
        <>
          <LineSample color="var(--ink)" /> The TTM EPS line
        </>
      ),
      body: (
        <p>
          Earnings per share over the last 12 months (TTM EPS): the last four quarterly reports
          added up. It moves in steps, one each time a new report comes out. Like every line except
          the price, it is stretched to fill the chart, so compare its shape, not its height.
        </p>
      ),
      highlight: ["eps"],
      you: withChart((c) => {
        const { f, l } = ends(c);
        return (
          <>
            TTM EPS went from <b>{usd(f.ttmEps)}</b> to <b>{usd(l.ttmEps)}</b> {span(c)},{" "}
            <b>{signedPct(c.epsChange.value)}</b>.
          </>
        );
      }),
    },
    {
      title: (
        <>
          <LineSample color="var(--warn)" /> The P/E line
        </>
      ),
      body: (
        <p>
          The price divided by TTM EPS (P/E): how many dollars investors pay for one dollar of
          yearly earnings. When it rises, the stock got more expensive compared with its earnings.
        </p>
      ),
      highlight: ["pe"],
      you: withChart((c) => (
        <>
          The P/E went from <b>{times(c.peStart.value)}</b> to <b>{times(c.peEnd.value)}</b>{" "}
          {span(c)}.
        </>
      )),
    },
    {
      title: (
        <>
          <LineSample color="var(--accent)" dash />
          <LineSample color="var(--ink)" dash /> The 12-month change lines
        </>
      ),
      body: (
        <p>
          How much the price and TTM EPS changed compared with one year earlier. These two share one
          scale, so you can compare their heights. They are hidden by default: switch them on with
          their chips above the chart.
        </p>
      ),
      highlight: ["r12", "g12"],
      you: withChart((c) => {
        const m = c.last12Months;
        if (m.priceChange.value === null) {
          return null;
        }
        return (
          <>
            Over the last 12 months: price <b>{signedPct(m.priceChange.value)}</b>, TTM EPS{" "}
            <b>{signedPct(m.epsGrowth.value)}</b>.
          </>
        );
      }),
    },
    {
      title: "Compare the shapes of the price and TTM EPS lines",
      body: (
        <p>
          If they climb together, the price follows earnings. If the price climbs while the EPS line
          stays flat, investors pay more for the same earnings.
        </p>
      ),
      highlight: ["price", "eps"],
      you: withChart((c) => {
        const share = c.earningsShareOfGain.value;
        if (share === null) {
          return (
            <>
              <Span c={c} /> the price moved <b>{signedPct(c.priceChange)}</b> while earnings moved{" "}
              <b>{signedPct(c.epsChange.value)}</b>.
            </>
          );
        }
        if (share > 1) {
          return (
            <>
              <Span c={c} /> earnings grew faster than the price, so investors now pay less per
              dollar of earnings.
            </>
          );
        }
        const e = Math.round(share * 100);
        return (
          <>
            Measured on a log scale, about <b>{e}%</b> of the price gain {span(c)} came from higher
            earnings and <b>{100 - e}%</b> from investors paying more per dollar of earnings.
          </>
        );
      }),
    },
    {
      title: "Confirm with the P/E line",
      body: (
        <p>
          A rising P/E means the price grew faster than earnings, so the stock got more expensive. A
          falling P/E means earnings grew faster than the price, so the stock got cheaper, even if
          the price went up.
        </p>
      ),
      highlight: ["pe"],
      levers: ["price", "ttmEps", "typicalPeMonths"],
      you: (ev) => {
        const vs = ev.exitPe.peNowVsTypical.value;
        const t = ev.exitPe.typical;
        return (
          <>
            The P/E from your inputs (price ÷ TTM EPS in the left panel) is{" "}
            <b>{times(ev.peNow.value)}</b>, against its typical <b>{times(t.pe.value)}</b> (the
            median of the quarter-end P/Es of the last {t.months} months and the P/E from your
            inputs)
            {vs === null ? "." : `: ${usual[vs] ?? ""}.`} Change the months next to Typical P/E in
            the exit P/E lever to use a different period.
          </>
        );
      },
    },
    {
      title: "Use the P/E bands for levels",
      body: (
        <p>
          Each dotted line shows what the share price would be if investors paid one fixed P/E,
          labeled on the right, such as 20×. It is TTM EPS times that number, so the line steps up
          each time earnings rise. Read where the price sits among the lines: that is its P/E at
          that moment, and the shaded band is where it sits today. If the price stays with one line,
          it rose only because earnings rose. If it climbs from a lower line to a higher one,
          investors started paying more for each dollar of earnings.
        </p>
      ),
      highlight: ["band-near", "price", "bandcap"],
      you: withChart((c) => {
        const b = c.bands.value;
        const near = b?.nearest.value ?? null;
        const a = b?.firstPe.value ?? null;
        const z = b?.currentPe.value ?? null;
        if (b === null || near === null || a === null || z === null) {
          return null;
        }
        const crossed = b.crossed.map((m) => multiple(m)).join(", ");
        const rose = z > a;
        const priceUp = c.priceChange >= 0;
        return (
          <>
            The <LineSample color="var(--s3)" /> line is the <b>{multiple(near)}</b> band, the one
            closest to today&apos;s P/E. <Span c={c} /> the P/E went from <b>{times(a)}</b> to{" "}
            <b>{times(z)}</b>.{" "}
            {b.crossed.length === 0 ? (
              "No band line lies between the first and the last P/E."
            ) : (
              <>
                Between the first and the last day it moved {rose ? "up" : "down"} past the{" "}
                <b>{crossed}</b> {b.crossed.length > 1 ? "lines" : "line"}, so investors now pay{" "}
                {rose ? "more" : "less"} for each dollar of earnings.
                {rose && !priceUp ? " The price itself fell, but earnings fell faster." : ""}
                {!rose && priceUp ? " The price itself rose, but earnings rose faster." : ""}
              </>
            )}
          </>
        );
      }),
    },
    {
      title: "Check the share change markers",
      body: (
        <p>
          A marker shows a quarter in which the diluted share count changed by 2% or more against
          the quarter before. <Swatch color="var(--bad)" />
          This marker means the count rose: dilution, such as new shares or more options.{" "}
          <Swatch color="var(--good)" />
          This one means it fell, usually through buybacks. The count is a quarterly average, so a
          marker shows the change, not one transaction. After a dilution, the profit or loss is
          split over more shares, so each share owns a smaller part of it.
        </p>
      ),
      highlight: ["events", "eps"],
      you: withChart((c) => {
        const ev = c.shareCountEvents;
        if (ev.length === 0) {
          return "No quarter in this range changed the share count by 2% or more.";
        }
        return (
          <>
            {ev.length === 1 ? "One quarter" : `${String(ev.length)} quarters`} in this range
            changed the share count by 2% or more:{" "}
            <b>{ev.map((e) => `${e.label} ${signedPct(e.change, 1)}`).join(", ")}</b>.
          </>
        );
      }),
    },
    {
      title: "Compare the two 12-month lines",
      body: (
        <p>
          They are hidden by default, so turn them on with their chips when you want to see where
          price and earnings went separate ways. They share one scale, so you can compare their
          heights. When <LineSample color="var(--accent)" dash /> price change sits above{" "}
          <LineSample color="var(--ink)" dash /> EPS growth, the P/E went up over that year. When it
          sits below, the P/E went down. This holds while TTM EPS stays positive, because the P/E
          needs a positive TTM EPS. The faint line marked 0% (12M) is where there was no change.
        </p>
      ),
      highlight: ["r12", "g12"],
      you: withChart((c) => {
        const d = c.last12Months.peChange.value;
        if (d === null) {
          return null;
        }
        return (
          <>
            Which line is on top gives the direction of the P/E. Over the last 12 months the price{" "}
            {d >= 0 ? "outran" : "lagged"} earnings, so the P/E {d >= 0 ? "rose" : "fell"}{" "}
            <b>{pct(Math.abs(d))}</b>.
          </>
        );
      }),
    },
    {
      title: "Understand the correlation number",
      body: (
        <p>
          The number sits under the chart title, as “price–earnings link” with its value.
          Correlation measures how closely two lines move together, on a scale from −1 to +1. Here
          it compares the two 12-month lines: when earnings growth sped up or slowed down, did the
          price change follow?
          <span className="vl">
            <Swatch color="var(--good)" />
            Strong, 0.6 or more: the price change closely tracked earnings growth.
          </span>
          <span className="vl">
            <Swatch color="var(--warn)" />
            Moderate, 0.3 to 0.6: it tracked earnings growth in part.
          </span>
          <span className="vl">
            <Swatch color="var(--muted)" />
            Weak, below 0.3: it hardly tracked earnings growth, so look for other drivers, such as
            market mood, interest rates or hopes for the future.
          </span>
          <span className="vl">
            <Swatch color="var(--bad)" />
            Negative: the price change tended to move against earnings growth.
          </span>
        </p>
      ),
      highlight: ["r12", "g12", "link"],
      you: withChart((c) => {
        const link = c.link.value;
        if (link === null) {
          return "reason" in c.link && c.link.reason === "zero_variance"
            ? "One of the two 12-month lines does not change in this range, so the correlation cannot be measured."
            : "Not enough history in this range to measure it.";
        }
        return (
          <>
            <Span c={c} /> the correlation is <b>{coefficient(link.correlation)}</b>, {link.class}:{" "}
            {linkMeaning[link.class]}
          </>
        );
      }),
      takeaway: withChart((c) => (
        <>
          <strong>{ticker} in one line:</strong> {span(c)} the price changed{" "}
          {signedPct(c.priceChange)} and TTM EPS {signedPct(c.epsChange.value)}, so the P/E went
          from {times(c.peStart.value)} to {times(c.peEnd.value)}. {why(c)}
        </>
      )),
    },
  ];
}
