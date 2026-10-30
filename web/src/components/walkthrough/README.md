# Walkthrough

Each panel has an inline walkthrough. It writes its steps into the panel. It highlights the parts that a step explains and fades the rest of the panel. It rings the levers that a step refers to in the left panel. It never opens a modal and never scrolls the page. One walkthrough runs at a time.

## Use it in a panel

1. Mark the panel as the scope of the walkthrough with `data-walk-scope`. The `Panel` frame in `components/panels/Panel.tsx` does this for you.
2. Mark each element that a step explains with `data-walk="<key>"`. One element can carry more than one key, separated by spaces: `data-walk="required exit"`. A key uses letters, digits and `_ . : -`.
3. Put `<Walkthrough>` where the launcher goes, at the end of the panel. While it runs, the step card takes the place of the launcher.

```tsx
import { Panel } from "@/components/panels/Panel";
import { Walkthrough, type WalkthroughStep } from "@/components/walkthrough/Walkthrough";
import { pct } from "@/components/format";
import { useEvaluation } from "@/state/StockProvider";

export function PricedInPanel() {
  const { evaluation: ev, state } = useEvaluation();
  const steps: WalkthroughStep[] = [
    {
      title: "The big number",
      body: <p>The average yearly EPS growth that gives you your target return.</p>,
      highlight: ["required"],
      you: (e) => <>{state.ticker} must grow EPS <b>{pct(e.required.growth.value)}</b> a year.</>,
    },
    {
      title: "Move the levers",
      body: <p>A higher target return or a lower exit P/E raises the requirement.</p>,
      highlight: ["required"],
      levers: ["targetReturn", "exitPe"],
      takeaway: (e) => <><strong>In one line:</strong> the price requires {pct(e.required.growth.value)}.</>,
    },
  ];
  return (
    <Panel id="priced-in" title="Priced in">
      <div data-walk="required">{pct(ev.required.growth.value)}</div>
      <Walkthrough
        id="priced-in"
        name="the priced-in summary"
        launcher="Walk me through the priced-in summary with my numbers"
        intro="This panel answers one question: ..."
        watchOut="EPS can jump for reasons that do not repeat."
        steps={steps}
      />
    </Panel>
  );
}
```

## API

`<Walkthrough>` props:

| Prop | Meaning |
| --- | --- |
| `id` | Unique on the page. |
| `name` | What it walks through. The card's label is "Walkthrough of `name`". |
| `launcher` | The text of the launcher button. |
| `steps` | The steps, in order. |
| `intro` | Optional. Shown on the first step, under the title. |
| `watchOut` | Optional. Shown on the last step, after "Watch out.". |
| `slot` | Optional. The name of a `<WalkthroughSlot>` that shows the card instead of the panel. The left panel uses the slot `main` at the top of the main column. |
| `onStart` | Optional. Runs when the walkthrough starts. The function that it returns runs when the walkthrough ends. Use it to change a view for the walkthrough, such as to show every chart overlay, and to put the view back. |

A `WalkthroughStep`:

| Field | Meaning |
| --- | --- |
| `title` | The step's title. |
| `body` | The explanation. Define each term at first use. Do not name colors in text: show a color sample. |
| `you` | Optional. `(ev: Evaluation) => ReactNode`: one line with the user's own numbers. It shows only when an evaluation exists. If it throws, the line is left out. |
| `highlight` | Optional. The `data-walk` keys of the elements that the step explains. Without a key that matches, nothing fades. |
| `levers` | Optional. The `LeverKey`s to ring: `price`, `ttmEps`, `dividend`, `targetReturn`, `holdingYears`, `exitPe`, `typicalPeMonths` (the typical P/E look-back), `baseline`, `trailingMonths` (the trailing growth look-back), `customGrowth` (the flat custom rate) and `yearRates` (the custom rates year by year). The last two exist only while the custom baseline shows them; a key with no element rings nothing. |
| `takeaway` | Optional. A node, or `(ev) => ReactNode`. Give it on the last step: the one-line take-away. |

`useWalkthrough()` returns `{ active, start(id), go(step), end() }` for a control outside the component.

## Behavior

- The step card has the step number, Back, Next (Finish on the last step), the step dots and a close button. The arrow keys move between steps, except in a text field. Escape ends the walkthrough.
- Focus moves to Next on each step and back to the launcher at the end, without a scroll.
- The marks are data attributes that no component renders: `data-tour-on` on the scope, `data-thl` on each highlighted element, `data-tl` on each other leaf of the scope (it fades), and `data-tside` on each ringed lever. The styles in `styles/globals.css` select these attributes; do not put them in a panel's markup. React keeps an attribute that it does not render, so a re-render that changes `className` keeps the marks. A `MutationObserver` on the page marks the nodes that any component adds or replaces while a step shows, within the same task. A mark is not reapplied when a component sets one of these attributes itself.
- To make a lever visible, the left panel scrolls inside itself (`data-walk-scroll`). The page never scrolls.
- The motion stops when the user prefers reduced motion.
