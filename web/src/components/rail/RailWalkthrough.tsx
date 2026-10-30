"use client";

import type { Evaluation } from "@/engine";
import { pct, usd, years } from "@/components/format";
import { Walkthrough, type WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import { useStock } from "@/state/StockProvider";
import { baselineName, typicalText } from "./notes";

const exitPe = (ev: Evaluation): string => `${(ev.exitPe.inUse.value?.pe ?? 0).toFixed(1)}×`;
const peNow = (ev: Evaluation): string => `${(ev.peNow.value ?? 0).toFixed(1)}×`;

/** RailWalkthrough walks through the inputs of the left panel; its card shows at the top of the main column. */
export function RailWalkthrough() {
  const { state, levers } = useStock();
  const sym = state.ticker ?? "The stock";
  const r = state.targetReturn;
  const n = state.holdingYears;
  const dividend = levers?.dividend ?? 0;

  const steps: WalkthroughStep[] = [
    {
      title: "Target return",
      levers: ["targetReturn"],
      body: (
        <p>
          Set the yearly return that makes the risk worth it to you. A broad index fund of US stocks
          (the S&amp;P 500) has returned about 10% a year over long periods, with large swings along
          the way. One stock carries more risk than an index, so many investors ask for more, often
          12% to 15%. A higher target makes more stocks look expensive. A lower one accepts returns
          you could get from an index with less effort.
        </p>
      ),
      you: (ev) => (
        <>
          Your target is <b>{pct(r, 1)}</b>, {r > 0.1 ? "above" : r < 0.1 ? "below" : "equal to"}{" "}
          the S&amp;P 500&apos;s long-run 10%. At {pct(r, 1)} {sym} requires{" "}
          <b>{pct(ev.required.growth.value)}</b> growth; at 10% it would require{" "}
          <b>{pct(ev.sensitivity.value?.marketTarget.requiredGrowth.value)}</b>.
        </>
      ),
    },
    {
      title: "Holding period",
      levers: ["holdingYears"],
      body: (
        <p>
          Use how long you would really hold. Over 1 or 2 years, the exit P/E decides most of the
          result, and that depends on market mood. Over 3 to 5 years, earnings growth and the P/E
          both matter. Over 10 years, growth dominates, but a 10-year growth forecast is much less
          certain.
        </p>
      ),
      you: (ev) => (
        <>
          With a {exitPe(ev)} exit, {sym} requires{" "}
          <b>{pct(ev.sensitivity.value?.oneYear.requiredGrowth.value)}</b> a year over 1 year,{" "}
          <b>{pct(ev.required.growth.value)}</b> over your {years(n)}, and{" "}
          <b>{pct(ev.sensitivity.value?.fiveYears.requiredGrowth.value)}</b> over 5 years.
        </>
      ),
    },
    {
      title: "Exit P/E: when a company usually gets a high P/E",
      levers: ["exitPe"],
      body: (
        <p>
          Investors pay a high P/E (price divided by yearly earnings) when they expect earnings to
          grow fast for many years. Common reasons: steady, repeat revenue such as subscriptions,
          high profit margins, little need for new factories or equipment, a strong position against
          competitors, and low interest rates. A P/E can also look high for a bad reason: earnings
          dropped for a while, so the price is divided by an unusually small number.
        </p>
      ),
      you: (ev) => (
        <>
          {sym} trades at <b>{peNow(ev)}</b> today, against its {typicalText(ev)}.
        </>
      ),
    },
    {
      title: "Exit P/E: when a company usually gets a low P/E",
      levers: ["exitPe"],
      body: (
        <p>
          Slow or shrinking growth, heavy debt, earnings that swing with the economy, businesses
          that need a lot of new investment to grow, and high interest rates all push the P/E down.
          Be careful with cyclical companies near a peak: the P/E looks low because earnings are
          unusually high, and those earnings may fall.
        </p>
      ),
      you: (ev) => {
        const w = ev.baselines.weighted.value;
        return (
          <>
            {sym}&apos;s weighted EPS trend is <b>{pct(w)}</b>
            {w !== null && w < 0.06
              ? ": slow growth, which usually earns a lower P/E."
              : ", so the market can justify a higher P/E as long as that growth lasts."}
          </>
        );
      },
    },
    {
      title: "Exit P/E: what to pick",
      levers: ["exitPe"],
      body: (
        <p>
          As growth slows, the P/E usually falls. That is why the Estimated setting lowers
          today&apos;s P/E as growth fades. Assume a higher exit P/E than today only with a clear
          reason. Compare with the stock&apos;s own typical P/E (set next to Typical P/E, 36 months
          by default) and with the P/E bands in the price chart. For reference, the S&amp;P 500 has
          averaged roughly 16× earnings over its long history and has often traded near 20× or
          higher in recent decades.
        </p>
      ),
      you: (ev) => (
        <>
          Your exit P/E is <b>{exitPe(ev)}</b>. Today the stock trades at {peNow(ev)}, against a{" "}
          {typicalText(ev)}.
        </>
      ),
    },
    {
      title: "Growth baseline",
      levers: ["baseline"],
      body: (
        <p>
          The baseline is the growth you compare with what the price requires. Weighted, the
          default, follows recent execution but smooths out single quarters. Today reacts first when
          growth changes, but one unusual quarter can move it. Trailing gives a plain growth rate
          over the number of months you choose. Use Custom when you know something the record does
          not show. Year by year lets you model a slowdown, for example 15%, 12%, 9%. Few companies
          grow EPS above 15% to 20% a year for a long time; growth usually fades toward the growth
          of the whole economy, around 4% to 6% a year before inflation.
        </p>
      ),
      you: (ev) => (
        <>
          Weighted <b>{pct(ev.baselines.weighted.value)}</b>, today{" "}
          <b>{pct(ev.baselines.today.value)}</b>, trailing{" "}
          <b>{pct(ev.baselines.trailing.growth.value)}</b>. You use the {baselineName(ev)},{" "}
          <b>{pct(ev.baselines.growth.value)}</b>.
        </>
      ),
    },
    {
      title: "Dividends",
      levers: ["dividend"],
      body: (
        <p>
          The yearly dividend per share counts as cash you receive, not reinvested. The page fills
          it with the company&apos;s recurring dividends of the last 12 months. Set it to 0 to judge
          the price return only.
        </p>
      ),
      you: () =>
        dividend > 0 ? (
          <>
            The page counts <b>{usd(dividend)}</b> a year in dividends.
          </>
        ) : (
          <>The dividend is 0, so the page shows the price return only.</>
        ),
      takeaway: (ev) => (
        <>
          <strong>{sym} in one line:</strong> for {pct(r, 1)} a year over {years(n)} with a{" "}
          {exitPe(ev)} exit, the price requires {pct(ev.required.growth.value)} EPS growth a year,
          against your {baselineName(ev)} of {pct(ev.baselines.growth.value)}.
        </>
      ),
    },
  ];

  return (
    <Walkthrough
      id="inputs"
      name="the inputs"
      launcher="Walk me through these inputs"
      slot="main"
      intro="Every result on this page follows from these inputs. These rules of thumb help you pick values you can defend."
      watchOut="These are rules of thumb, not laws. Interest rates, the industry and the whole market change what counts as a normal P/E. Test a few values on each lever and see whether the verdict changes."
      steps={steps}
    />
  );
}
