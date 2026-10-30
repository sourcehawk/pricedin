import { expect, isPhone, test, type Source, type SecSource } from "./support/fixtures";
import { pickStock } from "./support/stock";

function filings(s: Source): SecSource[] {
  return s.kind === "sec" ? [s] : s.kind === "derived" ? s.inputs.flatMap(filings) : [];
}

test("a sourced figure shows its filings and links to the newest one on SEC EDGAR", async ({
  page,
  api,
}) => {
  await page.goto("/");
  await pickStock(page, "NV", "NVDA");
  const p = await api.profile("NVDA");
  if (p.ttmEps === null) {
    throw new Error("the NVDA fixture has a TTM EPS");
  }
  const inputs = filings(p.ttmEps.source);
  expect(inputs.length).toBeGreaterThan(0);
  const newest = inputs.reduce((a, b) => (b.filed > a.filed ? b : a));

  const eps = page.locator("header.top .stat", { hasText: "TTM EPS" }).locator("a.sourced");
  // A tap would follow the link; focus shows the same tooltip.
  if (isPhone(page)) {
    await eps.focus();
  } else {
    await eps.hover();
  }
  const tip = page.getByRole("tooltip");
  await expect(tip).toBeVisible();
  // The tooltip lists the value's own inputs; a derived input shows its label.
  const direct = p.ttmEps.source.kind === "derived" ? p.ttmEps.source.inputs : [p.ttmEps.source];
  for (const f of direct.filter((i) => i.kind === "sec")) {
    await expect(tip).toContainText(`Form ${f.form}, filed ${f.filed}`);
  }
  await expect(tip).toContainText(`As of ${p.ttmEps.asOf}`);

  const cik = String(Number(p.company.cik));
  const folder = newest.accession.replaceAll("-", "");
  await expect(eps).toHaveAttribute(
    "href",
    new RegExp(`^https://www\\.sec\\.gov/Archives/edgar/data/${cik}/${folder}/[^/]+$`),
  );
  await expect(eps).toHaveAttribute("target", "_blank");
});
