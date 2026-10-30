/** Footer states the product principle and the method, as the prototype does. */
export function Footer() {
  return (
    <footer>
      <div className="principle">
        PricedIn measures the gap between the growth an investment requires and the growth the
        company has demonstrated.
      </div>
      <div>
        Method: required EPS growth = ((price × (1 + target)<sup>years</sup> − dividends × years) ÷
        (TTM EPS × exit P/E))<sup>1/years</sup> − 1. Dividends are counted as cash received, not
        reinvested. Taxes and fees are excluded.
      </div>
      <div>
        Estimated exit P/E: recent EPS growth fades toward 6% with a 3-year half-life. The premium
        of today&apos;s P/E over a 15× base multiple shrinks in proportion to the excess growth that
        fades. When the weighted trend is at or below 6% and today&apos;s P/E is more than 5% above
        the stock&apos;s typical P/E (the median over the months set next to Typical P/E, 36 by
        default), the estimate uses the typical P/E.
      </div>
      <div>
        Fundamentals come from SEC filings and prices from Massive. Every number shows its source
        and its as-of date when you point at it.
      </div>
    </footer>
  );
}
