// The marks a walkthrough step puts on the page. They are DOM classes, not
// React state, so a panel opts in with data attributes alone and never
// re-renders for a walkthrough.

/** Classes and attributes of the marks; the styles in globals.css use the same names. */
export const marks = {
  /** On the panel scope while a step highlights something in it. */
  scope: "tour-on",
  /** On each element a step highlights: the thin ring and soft glow. */
  highlight: "thl",
  /** On each lever a step refers to. */
  lever: "tside",
  /** On each leaf of the scope, which fades unless a highlighted element holds it. */
  fade: "data-tl",
} as const;

const keyPattern = /^[A-Za-z0-9_.:-]+$/;

function isLeaf(e: Element): boolean {
  if (e.children.length === 0) {
    return true;
  }
  return [...e.childNodes].some(
    (n) => n.nodeType === Node.TEXT_NODE && (n.textContent ?? "").trim() !== "",
  );
}

/**
 * markScope highlights the elements of scope whose data-walk lists one of
 * keys, and fades the rest of scope, except what keep holds (the step card)
 * and the elements that hold a highlighted element. It returns the
 * highlighted elements; with none, nothing fades. Keys that are not
 * letters, digits and _.:- are ignored.
 */
export function markScope(
  scope: Element,
  keys: readonly string[],
  keep: Element | null,
): Element[] {
  clearScope(scope);
  const found = keys
    .filter((k) => keyPattern.test(k))
    .flatMap((k) => [...scope.querySelectorAll(`[data-walk~="${k}"]`)]);
  if (found.length === 0) {
    return [];
  }
  scope.classList.add(marks.scope);
  for (const e of scope.querySelectorAll("*")) {
    // Opacity applies to a whole subtree: a faded parent fades its highlighted child.
    const holdsHighlight = found.some((h) => h !== e && e.contains(h));
    if ((keep === null || !keep.contains(e)) && !holdsHighlight && isLeaf(e)) {
      e.setAttribute(marks.fade, "");
    }
  }
  for (const e of found) {
    e.classList.add(marks.highlight);
  }
  return found;
}

/** clearScope removes every mark of markScope from scope. */
export function clearScope(scope: Element): void {
  scope.classList.remove(marks.scope);
  for (const e of scope.querySelectorAll(`[${marks.fade}]`)) {
    e.removeAttribute(marks.fade);
  }
  for (const e of scope.querySelectorAll(`.${marks.highlight}`)) {
    e.classList.remove(marks.highlight);
  }
}

/**
 * ringLevers rings the elements of root whose data-lever is one of keys
 * and returns them. When the first one is out of view inside a container
 * marked data-walk-scroll (the left panel), that container scrolls; the
 * page itself never scrolls.
 */
export function ringLevers(root: ParentNode, keys: readonly string[]): Element[] {
  clearLevers(root);
  const found = keys
    .filter((k) => keyPattern.test(k))
    .flatMap((k) => [...root.querySelectorAll(`[data-lever="${k}"]`)]);
  for (const e of found) {
    e.classList.add(marks.lever);
  }
  const first = found[0];
  const box = first?.closest("[data-walk-scroll]");
  if (first !== undefined && box && box.scrollHeight > box.clientHeight) {
    const b = box.getBoundingClientRect();
    const r = first.getBoundingClientRect();
    if (r.top < b.top || r.bottom > b.bottom) {
      box.scrollTop += r.top - b.top - 20;
    }
  }
  return found;
}

/** clearLevers removes every lever ring under root. */
export function clearLevers(root: ParentNode): void {
  for (const e of root.querySelectorAll(`.${marks.lever}`)) {
    e.classList.remove(marks.lever);
  }
}
