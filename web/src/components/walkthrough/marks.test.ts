import { afterEach, describe, expect, it } from "vitest";
import { clearLevers, clearScope, markScope, ringLevers } from "./marks";

afterEach(() => {
  document.body.innerHTML = "";
});

function panel(): HTMLElement {
  document.body.innerHTML = `
    <aside class="rail" data-walk-scroll>
      <div data-lever="targetReturn">target</div>
      <div data-lever="holdingYears">years</div>
    </aside>
    <section id="scope">
      <div data-walk="big sub"><b>14.8%</b> a year</div>
      <dl><dt>Weighted</dt><dd data-walk="weighted">12%</dd></dl>
      <div class="tour-card"><p>card text</p></div>
    </section>`;
  return document.getElementById("scope") as HTMLElement;
}

describe("markScope", () => {
  it("highlights each element whose data-walk lists a key", () => {
    const scope = panel();
    const found = markScope(scope, ["sub", "weighted"], null);
    expect(found.map((e) => e.textContent)).toEqual(["14.8% a year", "12%"]);
    expect(found.every((e) => e.hasAttribute("data-thl"))).toBe(true);
  });

  it("fades the other leaves of the scope but not the card", () => {
    const scope = panel();
    const card = scope.querySelector(".tour-card");
    markScope(scope, ["weighted"], card);
    expect(scope).toHaveAttribute("data-tour-on");
    expect(scope.querySelector("dt")).toHaveAttribute("data-tl");
    expect(scope.querySelector(".tour-card p")).not.toHaveAttribute("data-tl");
  });

  it("does not fade an element that holds a highlighted element", () => {
    const scope = panel();
    const p = document.createElement("p");
    p.innerHTML = 'Required <b data-walk="req">14.8%</b> a year';
    scope.prepend(p);
    markScope(scope, ["req"], null);
    expect(p).not.toHaveAttribute("data-tl");
    expect(scope.querySelector("dt")).toHaveAttribute("data-tl");
  });

  it("fades nothing when no element matches", () => {
    const scope = panel();
    expect(markScope(scope, ["missing"], null)).toEqual([]);
    expect(scope).not.toHaveAttribute("data-tour-on");
    expect(scope.querySelector("[data-tl]")).toBeNull();
  });

  it("ignores a key that would break the selector", () => {
    const scope = panel();
    expect(markScope(scope, ['x"]', "weighted"], null)).toHaveLength(1);
  });

  it("replaces the marks of the step before", () => {
    const scope = panel();
    markScope(scope, ["weighted"], null);
    markScope(scope, ["big"], null);
    expect(scope.querySelector("dd")).not.toHaveAttribute("data-thl");
  });
});

describe("clearScope", () => {
  it("removes every mark", () => {
    const scope = panel();
    markScope(scope, ["weighted"], null);
    clearScope(scope);
    expect(scope).not.toHaveAttribute("data-tour-on");
    expect(scope.querySelector("[data-tl], [data-thl]")).toBeNull();
  });
});

describe("ringLevers", () => {
  it("rings the levers a step refers to and only those", () => {
    panel();
    ringLevers(document, ["targetReturn"]);
    ringLevers(document, ["holdingYears"]);
    expect(document.querySelector('[data-lever="targetReturn"]')).not.toHaveAttribute("data-tside");
    expect(document.querySelector('[data-lever="holdingYears"]')).toHaveAttribute("data-tside");
  });

  it("scrolls the left panel, not the page, to a lever out of view", () => {
    panel();
    const rail = document.querySelector(".rail") as HTMLElement;
    Object.defineProperty(rail, "scrollHeight", { value: 900 });
    Object.defineProperty(rail, "clientHeight", { value: 300 });
    rail.getBoundingClientRect = () => ({ top: 0, bottom: 300 }) as DOMRect;
    const lever = document.querySelector('[data-lever="holdingYears"]') as HTMLElement;
    lever.getBoundingClientRect = () => ({ top: 500, bottom: 560 }) as DOMRect;
    ringLevers(document, ["holdingYears"]);
    expect(rail.scrollTop).toBe(480);
  });

  it("clearLevers removes the rings", () => {
    panel();
    ringLevers(document, ["targetReturn", "holdingYears"]);
    clearLevers(document);
    expect(document.querySelector("[data-tside]")).toBeNull();
  });
});
