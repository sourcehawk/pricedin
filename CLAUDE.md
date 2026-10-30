# PricedIn

PricedIn shows how much future earnings growth a stock price already assumes. It answers one question before any deep research starts: what must this company deliver for me to earn my target return, and has it ever delivered that?

Product principle: show what must happen for an investment to succeed, not what the stock is supposedly worth. PricedIn never tells a user to buy or sell. It shows which stocks deserve research, and why.

## Status

v1 is in development. The repository has these parts:

- `server/`: Go module `github.com/sourcehawk/pricedin/server`. The `pricedin` command (`cmd/pricedin`) serves `/healthz`, `/readyz` and the metrics. `internal/config` reads the `PRICEDIN_` variables. `internal/domain` holds the types that later packages share.
- `web/`: Next.js app (App Router, TypeScript) with a placeholder page that uses the prototype's design tokens and fonts.
- `e2e/`: Playwright tests that run against the Docker Compose stack.
- `Makefile`: the only entry point for tools, lint, tests, builds, images and the local stack. CI calls only `make` targets. `make` lists them.
- `docker-compose.yml`: Postgres, the server and the web app. `.env.example` lists every variable.

The prototype `prototype/pricedin.html` is a single self-contained HTML page with inline CSS and JavaScript.

- All company data in it is invented demo data for eight tickers (GRMN, MSFT, GOOGL, AMD, PLTR, COST, KO, NKE). Do not reuse any of these numbers.
- Treat the prototype as the specification for behavior, calculations, copy and layout. It is not production code. Port its logic into tested modules.
- Open it in a browser to see every feature described below.

## Target for v1

- Cloud hosted, with an HTTP API.
- All data is real and comes from reliable sources. No demo data, no analyst estimates.
- Every number carries its source and its as-of date.
- One calculation engine serves the web UI, the API, and later the screener and the MCP tools. MCP (Model Context Protocol) is the standard that lets AI agents call tools.
- v1 is a private deployment for its owner only, behind a login. The price source (Massive Stocks Starter) allows personal use only. Before anyone else can see the site, replace the price source with a licensed one. See "Price data".

## Glossary

- EPS: earnings per share. Use diluted GAAP EPS as reported in filings.
- TTM: trailing twelve months, the sum of the last four reported quarters.
- P/E: share price divided by TTM EPS. Every P/E in PricedIn uses TTM EPS. Do not show a forward P/E anywhere.
- Exit P/E: the P/E that the user expects at the sale date.
- Required growth: the average yearly EPS growth (compound) that turns today's price into the target return at the exit P/E.
- Growth baseline: the growth rate the user compares against the requirement.
- YoY: a quarter compared with the same quarter one year earlier.
- pp: percentage points.

## Calculation engine

Put these in one pure module with no UI or network code. Write unit tests for every function.

The engine uses six inputs:

- price `P`
- TTM EPS `E`
- target return `r`
- holding period `n` in years
- exit P/E `pe`
- yearly dividend per share `D`, counted as cash and not reinvested

- Required growth: `g_req = ((P·(1+r)^n − D·n) / (E·pe))^(1/n) − 1`. If `E ≤ 0` or `pe ≤ 0`, the result is not applicable. Show "needs positive EPS".
- Projection at growth `g`: exit price `px = E·(1+g)^n·pe`. Total return `(px + D·n)/P − 1`. Yearly return (CAGR, compound annual growth rate) `((px + D·n)/P)^(1/n) − 1`.
- Exit P/E needed at growth `g`: `(P·(1+r)^n − D·n) / (E·(1+g)^n)`.
- Exit share price needed and EPS needed: `E·(1+g_req)^n·pe` and `E·(1+g_req)^n`.

Growth baselines:

- Weighted (default): average YoY growth of quarterly diluted EPS over the last 8 quarters. The weight of a quarter halves every 2 quarters back, so `w_k = 0.5^(k/2)` for `k = 0..7`.
- Today: YoY growth of the latest quarter only.
- Trailing: compound yearly growth of TTM EPS over N months. N defaults to 12, steps of 3 months, and is limited by the available history.
- Custom: one rate for every year, or one rate per year. Year-by-year rates combine into a compound average: `(Π(1+g_i))^(1/n) − 1`. The EPS path follows the yearly rates, not the average.
- Trajectory fill: takes the change in the weighted trend over the last 4 quarters, continues it, and halves it every year. The rate is never above the highest weighted reading of the last 8 quarters and never below −50%. It fills the Custom year-by-year rates. The user can then edit them.

Exit P/E modes:

- Estimated (default): today's P/E premium over a 15× base shrinks as growth fades. Growth starts at the weighted trend and fades toward 6%, and half of the excess disappears every 3 years. `g_exit = 6% + (g0 − 6%)·0.5^(n/3)` and `pe = 15 + (pe_now − 15)·(g_exit − 6%)/(g0 − 6%)`. Floor 6×, rounded to 0.5.
- Slow-grower rule: if `g0 ≤ 6%` and today's P/E is more than 5% above the typical P/E, use the typical P/E. If `g0 ≤ 6%` otherwise, or if today's P/E is at or below 15×, keep today's P/E.
- Today: the current P/E.
- Trailing: the typical P/E, the median of quarter-end P/Es over N months (default 36) including today.
- Custom: a number the user types or drags.

Classifications and other rules:

- Verdict label: compares the baseline with the required growth. More than 1.5 pp above gives "Undemanding" (green). Within 1.5 pp gives "Tight" (amber). More than 1.5 pp below gives "Demanding" (red).
- Return grid cells: compare each yearly return with the target, not with zero, with the same ±1.5 pp thresholds.
- Share-count events: a quarter where diluted shares change by 2% or more against the quarter before. Increase means dilution, decrease means buybacks.
- Price-earnings link: the correlation of the 12-month price change with the 12-month TTM EPS growth over the chosen chart range. Strong is 0.6 or more, moderate is from 0.3 to 0.6, weak is below 0.3, and negative is −0.3 or less.
- P/E bands: lines at fixed multiples of TTM EPS. They cover only the P/E range the stock traded at in the visible period, about 5 bands.

Reference values for tests, from the prototype's GRMN demo profile:

- `P=270, E=10.80, r=15%, n=3, pe=20` gives a required growth of 23.9%.
- `P=270, E=10.80, r=10%, n=3, pe=22` gives 14.8%.
- At 12% EPS growth and 22× the exit price is $334 and the yearly return is about 7.3%.

## Inputs and defaults

- Target return 15% (beats the S&P 500's long-run average of about 10%).
- Holding period 3 years, range 1 to 10.
- Exit P/E Estimated, growth baseline Weighted.
- Typical P/E look-back 36 months, trailing growth look-back 12 months. Both are always editable, whatever mode is selected.
- Dividend 0 in the prototype. In v1, fill it from the data source.
- Price and TTM EPS come from the selected stock and stay editable, so users can test their own numbers.
- A new stock pick resets every input except the target return and the holding period. "Reset inputs" resets all inputs for the current stock.
- Every lever value is also a text field.

## UI specification

The prototype defines these panels. Port each one.

1. Left panel: stock search, starting point, levers, input walkthrough. It scrolls on its own and ends at the bottom of the screen.
2. Priced-in panel (titled "Priced in"): required growth, exit price and EPS needed, verdict label, the three growth baselines, gap, quarterly YoY chips and growth scale.
3. Price chart: daily close, overlays for TTM EPS, P/E, 12-month changes, P/E bands and share-count markers. Each overlay has a chip that is both legend and toggle and shows the hovered value. Ranges are 6M, 1Y and 3Y. Hovering a band lifts it and shows its value in a caption above the chart, never in a tooltip.
4. Return grid: rows are growth rates, columns are exit P/Es. The user's own cell has a purple "YOU" badge. Row tags mark Req, Wtd, Today and Trl. Hovering a tag outlines its row.
5. Earnings trajectory: TTM EPS history, baseline path, required path, red or green gap shading.
6. How demanding over time: the required growth at each past quarter, using today's levers, against the delivered TTM EPS growth.
7. Bear, base and bull: editable cases with returns.
8. Financials: revenue, operating costs, operating income, EPS, free cash flow, diluted shares, and a Compare view with a quarter-to-quarter correlation table.

Rules for the UI:

- On screens 1600 px wide or more, the summary and the price chart sit side by side at equal height.
- The small charts snap their tooltip to the nearest column and draw a vertical guide line.
- Every panel has an inline walkthrough. It writes its steps into the panel, never into a modal, and never scrolls the page. Each step highlights the parts it explains, and fades the rest. Highlighted text gets a thin yellow ring and a soft glow, with no fill. Each step adds one line that uses the user's own numbers, and the last step adds a one-line take-away. A step that refers to a lever rings that lever in the left panel.
- Color meanings:
  - blue: price and required values
  - white or black: TTM EPS
  - orange: P/E
  - teal: weighted trend and a hovered band
  - purple: the "YOU" cell
  - red, amber and green: comparisons with the target
- Rising costs and rising share counts are never green.
- Copy is plain English for a beginner, and still exact for an experienced investor. Define each term at first use. Do not name colors in text. Show a color sample instead.

## Data sources for v1

Fundamentals come from SEC EDGAR. It is free, official and needs no key.

- Ticker to CIK map: `https://www.sec.gov/files/company_tickers.json`.
- Financial facts: `https://data.sec.gov/api/xbrl/companyfacts/CIK##########.json`. Use diluted EPS, revenue, operating income, operating cash flow, capital spending and diluted share count.
- Filing list and SIC industry code: `https://data.sec.gov/submissions/CIK##########.json`.
- Nightly bulk files exist for the initial import. After that, fetch changes incrementally.
- Send a descriptive `User-Agent` with a contact address, and stay under 10 requests a second. SEC enforces both rules.

Rules for fundamentals:

- Derive quarters from filings. Cash-flow values in 10-Q filings are year to date, so subtract the previous quarter. Q4 equals the fiscal year minus Q1 to Q3.
- Keep the form type, filing date and accession number for every value, and link each figure to its filing.
- Store data point in time. If a company amends a filing, keep both versions with their filing dates. Never overwrite a value. Later backtests need this to avoid look-ahead bias.
- Adjust per-share history for stock splits.
- Free cash flow is operating cash flow minus capital spending. Operating costs are revenue minus operating income.

Do not use analyst consensus estimates. The product deliberately avoids them.

## Price data

SEC filings contain no daily share prices. Prices come from a separate source.

Decision for v1: use Massive (formerly Polygon.io), Stocks Starter plan. The owner pays for it, and the API key is in `.env` as `PRICEDIN_MASSIVE_API_KEY`.

Stooq was the first choice. Since April 2026 its downloads need an API key that only a person can get behind a CAPTCHA, and its pages run a JavaScript browser check. Do not try to get around that check.

- Massive serves daily bars for the last five years. The plan refuses older data.
- `GET /v2/aggs/grouped/locale/us/market/stocks/{date}?adjusted=false` returns one daily bar for every US ticker (about 12,600) for one date.
- `GET /v2/aggs/ticker/{ticker}/range/1/day/{from}/{to}?adjusted=false` returns the daily bars of one ticker.
- `GET /v3/reference/splits` and `GET /v3/reference/dividends` give split events and cash dividends. `GET /v3/reference/tickers` gives the CIK of each ticker.
- Backfill: grouped daily bars for every trading day of the last five years. PricedIn needs at least 3 years, which is about 756 trading days.
- Daily job: after the US close, a scheduled job (cron or a cloud scheduler) fetches the grouped daily bars for every missing trading day. The job must tolerate delays and missed days, and must fill gaps on its next run.
- The page always shows the latest close, labeled "as of close" with its date. Live or delayed intraday quotes are out of scope, because the product compares growth over years and intraday prices change nothing that matters. Exchanges also charge extra for displaying them.
- Wrap Massive behind a price-provider adapter interface. The rest of the code must not know which provider it uses.

Facts checked on 2026-10-30:

- Splits: with `adjusted=false`, Nvidia closed at 1208.88 on 2024-06-07 and at 121.79 on 2024-06-10, the day of its 10-for-1 split. With `adjusted=true` the earlier closes are divided by 10. Store raw closes and the split events, and compute split-adjusted closes from them. Every P/E history depends on this.
- Dividends: adjusted closes are adjusted for splits only, not for dividends. Take the dividend per share from the dividends endpoint, as a separate value.
- Symbol mapping: join tickers to SEC companies through the CIK on the Massive ticker and the SEC ticker-to-CIK map. Symbols change over time, so keep the mapping with dates.
- Delisted companies: the dashboard does not need them, but the planned backtester does. Massive keeps delisted tickers, but the plan limits history to five years. Record this limit.

Licensing:

- Stooq, Yahoo Finance, the Massive Starter plan and the free tiers of most API providers allow personal or non-commercial use only. Exchanges own trade prices and every vendor resells them under license. That is why no free public archive of daily closes exists, unlike SEC filings.
- Google Finance has no public API. The `GOOGLEFINANCE` function in Google Sheets blocks access to historical data from outside Sheets. Do not use it.
- A public site or a public API needs a plan with display rights. A secondary source listed these prices in 2026, which you must confirm with each vendor: Twelve Data Venture from $149 a month allows display to end users. Intrinio sells a startup display right from $333 a month. Massive (formerly Polygon.io) requires its Stocks Business plan at $2,499 a month. Financial Modeling Prep and Alpha Vantage need a separate written agreement. Ask Tiingo and EODHD directly.
- The Massive Stocks Starter plan that v1 uses is for personal use only.

## Architecture

Stack decisions:

- Back end: Go. It runs the HTTP API, the ingestion jobs, and later the screener and the MCP server. An official Go SDK for MCP exists.
- Front end: Next.js with TypeScript. Most of the page is client-side work, because every lever change recalculates the grid, the charts and the walkthrough lines at once.
- Database: Postgres. Plain tables are enough. TimescaleDB is an option if the price table grows a lot.
- Raw files: keep the downloaded SEC files and price-provider responses in object storage, so that you can reprocess everything after the normalization logic improves.
- Python only for later research work, such as backtesting notebooks. Ingestion does not need it, because the SEC data is clean JSON.

The calculation engine exists twice:

- The Go engine is the source of truth. The API, the screener and the MCP server use it.
- A TypeScript port runs in the browser, so that lever changes update the page instantly, without a request to the server.
- Both engines run the same shared file of test cases: inputs and expected outputs in JSON. Both test suites run it in CI. A change to one engine without the other fails the build.

Data layout in Postgres:

- `companies`: CIK, name, SIC code, fiscal year end.
- `tickers`: symbol, CIK, valid-from and valid-to dates.
- `facts`: one row per reported value, with CIK, XBRL concept, unit, period start, period end, value, form type, filing date and accession number. Never update or delete a row. Amended filings add new rows.
- `prices_daily`: symbol, date, close, volume, split factor, source and load time.
- Derived quarterly series and TTM values: views or tables rebuilt from `facts`, with the filing date of each input.
- Later: tables for screener runs and their results.

Ingestion:

- SEC first import: download the bulk `companyfacts.zip` archive, which SEC rebuilds every night, and the ticker-to-CIK map.
- SEC daily job: read the list of new filings, fetch company facts only for the companies that filed, and add the new rows.
- Prices: the backfill and daily job described under "Price data".
- Every job is safe to run twice, so that a retry never duplicates data.

API:

- Read endpoints for a stock's profile, daily prices, quarterly fundamentals and evaluation.
- The evaluation endpoint takes the same inputs as the levers and returns every computed value, with sources and as-of dates.

Hosting, as a starting point: managed Postgres (for example Neon, Supabase or Amazon RDS), the Go service on Fly.io or Cloud Run with the platform scheduler for the daily jobs, and the Next.js app on Vercel.

## Planned extensions

Design the schema and the API so that these extensions fit without a rewrite.

- Screener: filter and rank the whole covered universe with the same engine. Typical filters: required growth below the weighted trend, gap in pp, revenue growth, positive free cash flow, margin trend, dilution, typical P/E. Return why each stock passed.
- Sector context: group companies by SIC code and compute medians of P/E, required growth and growth rates. If a group has fewer than about ten companies, fall back to the two-digit major group.
- MCP server: tools for agents that return structured data and sources, never buy or sell advice. Planned tools: `screen_stocks(filters, sort_by, limit)`, `evaluate_stock(ticker, assumptions)`, `compare_stocks(tickers, assumptions)`, `get_financial_history(ticker, period)` and `get_screening_evidence(ticker)`.
- Backtesting: rerun screens at past dates with point-in-time data, including delisted companies.

## Suggested first steps

1. Create the repository and copy `prototype/pricedin.html` into it.
2. Write the Go calculation engine with unit tests that reproduce the reference values above.
3. Write the shared JSON test cases, then the TypeScript port that passes them.
4. Build SEC ingestion for a few tickers. Make sure that the derived quarters match the companies' own filings.
5. Build the Massive adapter, the backfill and the daily job. Do the split, dividend and symbol tests from "Price data".
6. Expose the evaluation endpoint.
7. Port the UI panel by panel. Start with the priced-in panel and the return grid.
8. Before anyone else gets access, choose a licensed price provider and switch the adapter.
