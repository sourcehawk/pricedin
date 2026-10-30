# PricedIn v1 design

Date: 2026-10-30. Status: approved by the owner through a standing grant to decide the open points.

## Goal

PricedIn v1 is a private, cloud-hosted web application with an HTTP API. It shows, for any US stock that files with the SEC, how much yearly EPS growth the current price requires for the owner's target return. It shows the company's own record next to that requirement.

`CLAUDE.md` is the product brief. `prototype/pricedin.html` is the behavior specification for calculations, copy and layout. This spec records the decisions that the brief leaves open. Where this spec and the brief agree, the brief is not repeated.

## Non-goals for v1

- Public access. One owner logs in. Nobody else gets an account.
- The screener, sector context, MCP server and backtester. The schema and the API leave room for them (see "Room for extensions").
- Intraday prices, analyst estimates, forward P/E.
- Delisted companies. The price source covers current symbols only. The backtester needs a different source later.
- A deployment that the agent runs. The repository is deploy-ready, and the owner runs the first deploy.

## Repository layout

One repository holds every part. Each top-level directory has one owner concern.

| Path | Content |
| --- | --- |
| `server/` | Go module `github.com/sourcehawk/pricedin/server`: the API, the ingestion jobs, the calculation engine, the migrations. |
| `web/` | Next.js application in TypeScript, with the TypeScript engine port. |
| `e2e/` | Playwright tests that run against the full Docker Compose stack. |
| `testdata/` | Data that more than one part reads: the shared engine cases, SEC fixtures, synthetic price fixtures. |
| `prototype/` | The prototype, kept as the reference. |
| `deploy/` | Fly.io configuration for the server and the scheduled jobs. |
| `docs/` | Architecture, data sources, API, runbooks, ADRs. |

`server/` and `web/` each have their own `Dockerfile`. A pnpm workspace at the root holds `web` and `e2e`.

## Tooling

- `.tool-versions` pins every tool: Go, Node.js (current LTS), pnpm, golangci-lint, and the linters for shell and workflows. asdf installs them locally. CI reads the same file.
- A root `Makefile` is the only entry point for builds, linters and tests. CI calls `make` targets and nothing else, so a green local `make ci` predicts a green CI run.
- Docker Compose runs the full stack locally: Postgres, the server, the scheduler and the web application. A `fixtures` profile adds a fixture server that imitates SEC and the price provider, for offline development and end-to-end tests.
- Renovate keeps every dependency current: Go modules, npm packages, GitHub Actions (pinned to commit digests), Docker images (pinned to digests) and `.tool-versions`.
- GitHub Actions validates every pull request: linters, unit tests, integration tests against a real Postgres, the engine parity cases in both languages, image builds, and the end-to-end suite.

## Server

### Process model

One binary, `pricedin`, has subcommands:

- `serve`: the HTTP API.
- `migrate`: applies the database migrations. `serve` does not migrate by itself, so that a deploy controls when the schema changes.
- `jobs run <job>`: runs one ingestion job and exits. A platform scheduler calls it.
- `scheduler`: a small loop that runs the daily jobs at fixed UTC times. Docker Compose uses it locally. A Postgres advisory lock makes sure that only one copy of a job runs at a time, wherever it starts.
- `ingest company <ticker>`: loads one company on demand.
- `auth hash-password` and `auth token create|list|revoke`: owner credentials and API tokens.

The server uses the standard library HTTP router, `pgx` for Postgres, `goose` for migrations, and `log/slog` for structured logs. It exposes Prometheus metrics on a separate port.

### Calculation engine

The engine is a pure Go package with no I/O. It ports every calculation from the prototype and the brief:

- required growth, projections, exit P/E needed, exit price and EPS needed;
- the growth baselines (weighted, today, trailing, custom flat, custom year by year) and the trajectory fill;
- the exit P/E modes (estimated with the slow-grower rule, today, trailing typical P/E, custom);
- the verdict and the return-grid tones;
- the return grid, the earnings paths, "how demanding over time", and the bear, base and bull cases;
- share-count events, the price-earnings link and the P/E bands.

One function, `Evaluate`, composes all of them from one input: the stock's history and the lever values. The history holds the fiscal quarters (split-adjusted EPS, quarter-end close, diluted shares, the financials) and the daily split-adjusted closes, each with the TTM EPS that was known on that day. The levers include the chart range, because the P/E bands and the price-earnings link depend on the visible period. The evaluation endpoint returns its output. The web application runs the TypeScript port of the same function.

### Corrections to the prototype

The prototype has a few defects. v1 corrects them, and the shared test cases carry the corrected rule with a note:

- Typical P/E is the conventional median: for an even number of quarter-end P/Es, the average of the two middle values. The prototype takes the upper middle value, which biases the typical P/E, the trailing exit P/E and the slow-grower rule upward.
- The return grid always has a row at the exact baseline growth, tagged with its baseline. The YOU cell sits in that row, so its return equals the return in the summary. The prototype marks the nearest row, whose return can differ from the summary.
- Chart scales cover every plotted point, also the intermediate points of a year-by-year path that is not monotonic.
- A chart scale with a zero-width domain (for example a constant share count) widens to a visible range instead of dividing by zero.
- In the price chart, the hit areas of the P/E bands sit above the pointer-capture layer, so hovering a band line lifts it. In the prototype only the band labels react.
- The bear, base and bull walkthrough names the cases that actually meet the target. The prototype infers them from how many cases pass, which is wrong once the owner edits the cases (for example, only Bear passes). Tests cover reordered outcomes.

"Not applicable" results are explicit. A function that cannot produce a number returns no value and a reason code (for example `needs_positive_eps`), never NaN or a magic number. JSON carries this as `null` plus a `reason` field.

### Engine parity

`testdata/engine/cases.json` holds the shared test cases: a function name, the inputs, the expected output and a tolerance. The Go and TypeScript test suites both run every case. A change to one engine without the other fails CI.

The expected values come from an oracle, not from either port. A script loads the prototype's own calculation functions and evaluates them on fixed inputs. The brief's GRMN reference values are cases too. Where v1 changes the prototype on purpose, the case names the change and the expected value comes from the brief's formula.

### Data model

Postgres 18. The schema has these tables:

- `companies`: CIK, name, SIC code and description, fiscal year end, state of incorporation.
- `tickers`: symbol, CIK, exchange, valid-from date, valid-to date. A symbol that moves to another CIK closes its old row and opens a new one.
- `filings`: accession number, CIK, form type, filing date, report date, primary document. This gives every figure a link to its filing.
- `facts`: one row per reported XBRL value: CIK, taxonomy, concept, unit, period start, period end, value, form type, fiscal year and period of the filing, frame, filing date, accession number, load time. A trigger rejects `UPDATE` and `DELETE` on this table. An amended filing adds new rows. A unique key on the value's identity makes a repeated load a no-op.
- `stock_splits`: the splits found in SEC data: CIK, effective date, ratio, source, and the evidence (an accession number or a note).
- `quarterly_fundamentals`: the derived quarters, rebuilt from `facts` after each load. Each value keeps the method that produced it (reported, year-to-date difference, fourth-quarter difference) and the filing date and accession of every input.
- `prices_daily`: symbol, date, open, high, low, raw close, volume, source, load time. The closes are raw. The view `prices_adjusted` computes the split factor of each bar from `price_splits`.
- `price_splits` and `dividends`: the provider's split events and cash dividends per symbol, with their dates and the provider's identifier.
- `ingest_runs`: one row per job run with start, end, status, counts and error text.
- `sessions` and `api_tokens`: owner authentication. Both store hashes, never the secrets.

Raw downloads go to a blob store before any parsing: SEC JSON and archives, and the provider's price, split and dividend responses, page by page. The SEC client and the price adapter take an injected recorder that receives every response body before it is decoded. A recorder failure fails the request, so no value is stored without its raw source. Each key holds the retrieval date and a hash of the body, so a refreshed response never overwrites an earlier one. The blob store has two implementations: a local directory (Compose and tests) and S3-compatible storage (production, for example Tigris on Fly.io or Cloudflare R2). A full reprocess reads only the blob store.

### SEC ingestion

- The client sends the configured `User-Agent` with a contact address, limits itself to 10 requests a second, and retries 429 and 5xx responses with backoff. A missing `User-Agent` is a startup error.
- Sources: `company_tickers_exchange.json` (ticker, CIK and exchange), the submissions file for each company (filings, SIC code, fiscal year end), and company facts for each company. The bulk archives (`companyfacts.zip`, `submissions.zip`) serve the first full import.
- Only the concepts that PricedIn uses are stored: diluted EPS, the revenue concepts, operating income, operating cash flow, capital spending, diluted weighted shares, dividends declared per share, and the stock split ratio.
- The covered universe is every company with a current ticker on NYSE or Nasdaq. A single company can also load on demand, the first time the owner opens it.
- The daily job reads the list of recent filings, then fetches company facts only for covered companies that filed.

### Deriving quarters

A pure package turns facts into fiscal quarters. The rules:

- A period of about three months (80 to 100 days) is a quarter. Periods of about six, nine and twelve months are year-to-date or full-year values.
- For each period, the value from the latest filing wins, because a later filing restates earlier periods for splits and corrections. The point-in-time variant takes an as-of date and ignores filings after it.
- When a quarter is not reported directly, it comes from year-to-date differences: Q2 = H1 − Q1, Q3 = 9M − H1, Q4 = FY − 9M. Cash-flow values in 10-Q filings are always year-to-date, so they always take this path.
- Diluted shares are a weighted average, so plain subtraction does not apply. A reported quarterly count always wins. Otherwise Q4 is the duration-weighted difference: (FY × FY days − Σ Qi × Qi days) / Q4 days, using the actual period lengths. The value is marked as an estimate. When the inputs do not cover the full year or give a non-positive result, Q4 shares are unavailable.
- Revenue takes, for each period, the first available concept in a fixed priority list.
- Free cash flow is operating cash flow minus capital spending. Operating costs are revenue minus operating income.
- TTM values add the last four quarters. TTM dividends per share add the last four quarterly dividends.
- Labels use the company's fiscal calendar ("Q3 FY26"). The period end date travels with every quarter.

### Stock splits

Per-share history must be split-adjusted, or every P/E history breaks.

- A split is confirmed only by explicit, dated evidence: a split event from the price provider (execution date and ratio), or the `StockholdersEquityNoteStockSplitConversionRatio1` concept with its period. A clean ratio between an original and a restated per-share value does not prove a split and does not date it. Restatement ratios only reconcile: they check that a confirmed split explains the restated values, and a mismatch goes to the data page as a conflict.
- Per-share values (EPS, dividends) from periods that end before a confirmed split, and that no later filing restated, are divided by the ratio. Share counts are multiplied by it.
- Prices use the provider's split events. When the SEC evidence and the provider disagree on a split, the evaluation keeps working and the data page lists the conflict.

### Prices

The brief chose Stooq. On 2026-10-30 the owner replaced it with Massive (formerly Polygon.io), Stocks Starter plan, for these reasons:

- Since April 2026 Stooq requires an API key that only a person can get, behind a CAPTCHA. Its download pages also run a JavaScript browser check, and its bulk archives need a browser session. A scheduled job cannot fetch Stooq data without getting around that check, which PricedIn must not do.
- Massive gives the same data through a normal API key: daily bars for five years, split and dividend reference data, and a ticker list with CIK numbers. The Starter plan has no call limit. Like Stooq, it is licensed for personal use only.

Facts checked against the live API on 2026-10-30:

- `adjusted=false` returns raw closes. Check: the Nvidia close of 2024-06-07 divided by the close of 2024-06-10, the day of its 10-for-1 split, is within 1% of 10. Result: pass. With `adjusted=true`, the same ratio is within 1% of 1, because the earlier closes are divided by 10. Result: pass. Adjusted closes are not dividend-adjusted.
- The splits endpoint lists the Nvidia split of 2024-06-10 (`split_from` 1, `split_to` 10).
- The grouped daily endpoint returns one bar for each of about 12,600 US tickers for one date in one request.
- Data older than five years is refused on this plan.

The design:

- A `prices.Provider` interface hides the provider. The rest of the code never imports a provider package. The Massive adapter is the only implementation in v1, and a fixture adapter serves tests.
- PricedIn stores raw closes and the provider's split events. Split-adjusted closes come from those two at read time, so a new split never rewrites stored rows. The view `prices_adjusted` gives each bar its cumulative split factor.
- Backfill: grouped daily requests for every trading day of the last five years (about 1,260 requests, rate-limited). PricedIn needs at least three years.
- Daily job: after the US close, request the grouped daily bars for each missing trading day, newest gap first. A missed day is filled on the next run. Running it twice adds nothing.
- Loading one company on demand uses the per-ticker daily bars endpoint.
- Dividends: the provider's cash dividends, with ex-dividend dates, give the yearly dividend per share (the regular dividends with an ex-date in the last twelve months). Special dividends are stored but not counted as yearly income.
- Symbol mapping: the provider's ticker list carries the CIK. PricedIn joins it with the SEC ticker map and keeps both with dates.
- The page shows the latest close, labeled "as of close" with its date.

### Evaluation and sources

Every number in the API carries its provenance:

```json
{ "value": 10.8, "source": { "kind": "sec", "form": "10-Q", "accession": "0001045810-26-000075",
  "filed": "2026-08-26", "periodEnd": "2026-07-26", "url": "https://www.sec.gov/Archives/..." }, "asOf": "2026-08-26" }
```

Derived values list their inputs. A value the owner typed has `kind: "user"`. Prices have `kind: "price"` with the provider name and the close date.

### HTTP API

Versioned under `/api/v1`. An OpenAPI 3.1 document in `server/api/openapi.yaml` is the contract. Server tests validate responses against it. The web application generates its TypeScript types from it, and CI fails when the generated file is stale.

- `GET /api/v1/search?q=&limit=`: tickers and names, with a flag for loaded data.
- `GET /api/v1/status`: data freshness: the newest run of each ingestion job, the newest close, and how many companies are loaded.
- `GET /api/v1/openapi.yaml`: the contract itself.
- `GET /api/v1/stocks/{ticker}`: profile: company, SIC sector, latest close, TTM EPS, TTM dividend, P/E, freshness. A company that is known but not loaded answers with `loaded: false`; the other stock endpoints answer 409 for it.
- `GET /api/v1/stocks/{ticker}/history?range=`: the engine input: every derived quarter, and the daily closes of the range, each with the TTM EPS that was known on that day, with the sources of the quarter values. The browser runs the engine on it.
- `GET /api/v1/stocks/{ticker}/fundamentals`: the derived quarters, with sources, the confirmed splits and the conflicts.
- `POST /api/v1/stocks/{ticker}/evaluation`: takes the lever values, returns the full `Evaluate` output with sources and as-of dates.
- `POST /api/v1/stocks/{ticker}/load`: loads or refreshes one company on demand.
- `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`.
- `GET /healthz` and `GET /readyz` outside the version prefix.

Errors use RFC 9457 problem details.

### Authentication

- One owner. The owner's email and an Argon2id password hash come from the environment. `pricedin auth hash-password` makes the hash.
- A login creates a server-side session row and sets an `HttpOnly`, `Secure`, `SameSite=Lax` cookie that holds a random token. The table stores only the token's hash. Logout deletes the row.
- Login attempts are rate-limited per client address.
- State-changing requests (POST, PUT, PATCH, DELETE) must be JSON. When they use the session cookie, and for login, they must also carry an `Origin` header of the application's own origin. Read requests (GET) need no `Origin` header, because browsers omit it on same-origin GET requests. Requests with a valid bearer token need no `Origin` header, because scripts do not send one.
- API tokens let scripts and, later, agents call the API with `Authorization: Bearer`. The CLI creates and revokes them. The table stores only hashes.

## Web application

- Next.js App Router, TypeScript in strict mode, React. No CSS framework: the prototype's design tokens and styles move into CSS files, with light and dark themes.
- The browser calls the API through a Next.js rewrite of `/api/*`, so the session cookie stays first-party. A Next.js route guard sends a visitor without a session to `/login`.
- The TypeScript engine runs in the browser. Every lever change recalculates the page without a request.
- Lever state lives in one reducer. The last stock and the lever values persist in `localStorage` as a per-browser convenience.
- Charts are hand-written SVG components that port the prototype's charts. No chart library could reproduce the bands, chips and snapping guides of the prototype without fighting it.
- Every panel in the brief's UI specification is ported, with its inline walkthrough. The walkthrough is one shared component: it writes its steps into the panel, highlights the parts it explains, fades the rest, and rings the matching lever in the left panel. It never opens a modal and never scrolls the page.
- Each figure that comes from a filing links to that filing. Hovering a value shows its source and as-of date.

## Testing

- Go unit tests for every engine function, the quarter derivation, split detection, the SEC parsers, the price adapter and the API handlers.
- Go integration tests against a real Postgres in a container (Testcontainers), for the store, migrations, ingestion jobs and the API.
- Real SEC filings, trimmed, serve as fixtures: the derived quarters must match the companies' own reported figures. SEC data is public domain.
- Price fixtures are synthetic responses in the Massive JSON format. The provider's terms do not allow redistribution of its data, and this repository is public.
- TypeScript unit tests (Vitest) for the engine port and the reducer, plus component tests for the panels.
- Playwright end-to-end tests start the Compose stack with the fixture server, run the ingestion jobs, log in, and use the page: pick a stock, move levers, check the computed numbers against the evaluation API, and step through a walkthrough.

## Room for extensions

- Screener: `quarterly_fundamentals` and `prices_daily` hold everything a screen needs. `Evaluate` runs on any company without the UI.
- Sector context: `companies` keeps the SIC code.
- MCP server: the API already returns structured data with sources. An MCP server becomes another adapter over the same service layer.
- Backtesting: append-only facts and the as-of variant of the quarter derivation give point-in-time data. Delisted companies need another price source. Prices, provider splits, dividends and the SIC data of companies are overwritten in place, so a backtest sees today's corrections, not the values as they were known then.

## Risks

- The Massive Starter plan is for personal use. A plan with display rights is mandatory before anyone else sees the site. The adapter interface makes a switch cheap.
- Five years of price history limits the long look-backs. The levers need at most three years.
- XBRL tagging varies between companies. The derivation records how it got each number, and the fixture tests cover several filers with different fiscal calendars and a split.
- The Q4 EPS by subtraction differs slightly from a true fourth-quarter EPS when the share count changes during the year. This follows the brief.
- The role that owns the tables can disable the trigger that keeps `facts` append-only. Production therefore runs the server and the jobs as a role that does not own the tables.

## Alternatives considered

- SQL views for derived quarters: rejected. The rules (restatements, year-to-date differences, splits, as-of filtering) read and test better as a Go package. The results still land in a table for later screening.
- A chart library (Recharts, visx, ECharts): rejected for fidelity reasons, see above.
- A separate auth provider (Clerk, Auth0, Neon Auth): rejected for one owner. It adds an external dependency and cost, and the API still needs its own tokens.
- Evaluation only on the server: rejected by the brief. Lever changes must not wait for the network.
