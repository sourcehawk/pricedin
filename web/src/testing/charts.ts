// Queries for the hover columns of a small chart (components/charts).

/** columns returns the hover columns inside root, left to right. */
export function columns(root: Element): SVGRectElement[] {
  return [...root.querySelectorAll<SVGRectElement>("rect.vcol")];
}

/** column returns hover column i inside root, and fails the test without one. */
export function column(root: Element, i: number): SVGRectElement {
  const c = columns(root).at(i);
  if (c === undefined) {
    throw new Error(`no column ${String(i)}`);
  }
  return c;
}

/** guide returns the guide line inside root, and fails the test without one. */
export function guide(root: Element): SVGLineElement {
  const l = root.querySelector<SVGLineElement>("line.vx");
  if (l === null) {
    throw new Error("no guide line");
  }
  return l;
}
