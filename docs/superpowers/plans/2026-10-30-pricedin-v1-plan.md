# PricedIn v1 implementation plan

Spec: `docs/superpowers/specs/2026-10-30-pricedin-v1-design.md`. Product brief: `CLAUDE.md`. Behavior reference: `prototype/pricedin.html`.

Every PR targets `main` (`sub_pr_target: main`). Each PR passes CI and a Copilot review loop before it merges. Items are numbered by their tracking issue (sub-issues of epic #1). The planning docs have no issue of their own.

## Waves

| Wave | PRs | Depends on |
| --- | --- | --- |
| 0 | Planning docs | none |
| 1 | #2 Foundation | planning docs |
| 2 | #3 Go engine, #4 Store, #5 SEC client, #6 Price provider | #2 |
| 3 | #7 TypeScript engine, #8 Quarter derivation | #3 (for #7), #5 (for #8) |
| 4 | #9 Ingestion jobs, #10 HTTP API and auth | #4, #5, #6, #8 (for #9); #3, #4, #8 (for #10) |
| 5 | #11 Web shell, #12 Deploy and docs | #7, #10 (for #11); #9, #10 (for #12) |
| 6 | #13 Priced-in panel and return grid, #14 Price chart, #15 Earnings panels and cases, #16 Financials | #11 |
| 7 | #17 End-to-end suite | all |

## Planning docs (`docs/pricedin-v1-plan`)

Spec, this plan, the state file, and the `CLAUDE.md` price-data update (Massive replaces Stooq).

## #2: Foundation (`chore/project-foundation`)

Build the skeleton that every later PR extends. No product logic.

- `.tool-versions`: golang 1.27.3, nodejs 26.13.0, pnpm 12.16.1, golangci-lint 2.15.0, actionlint, shellcheck. Use the newest versions at the time of the PR.
- `Makefile` targets (the only interface CI uses): `help`, `tools`, `deps`, `generate`, `fmt`, `lint`, `test` (= `test-server` + `test-web`), `test-server` (unit), `test-integration` (Go tests with the `integration` build tag), `test-web`, `test-e2e`, `build`, `images`, `up`, `down`, `logs`, `migrate`, `ci` (everything CI runs, in CI order). Each target prints what it runs.
- `server/`: Go module, `cmd/pricedin` with a subcommand framework (cobra) and `serve` that answers `GET /healthz` and `GET /readyz` (readyz pings Postgres), graceful shutdown, slog JSON logging, a metrics port. `internal/config` (environment parsing with `PRICEDIN_` prefix, validation, tests). `internal/domain` with the shared types in "Contracts" below. `.golangci.yml` with a strict, explicit linter set.
- `web/`: Next.js (newest), TypeScript strict, ESLint (flat config), Prettier, Vitest with one sample test, a placeholder page that uses the prototype's design tokens and fonts. Root `pnpm-workspace.yaml` with `web` and `e2e`.
- `e2e/`: Playwright project with one smoke test: the web page loads and `GET /healthz` answers through the stack.
- `server/Dockerfile` and `web/Dockerfile`: multi-stage, non-root, small final images, base images pinned by digest.
- `docker-compose.yml`: `db` (Postgres 18, health check, named volume), `migrate` (one-shot), `server`, `web`. Every service reads `.env`. `.env.example` lists every variable with a comment.
- `.github/workflows/ci.yml`: on pull requests and pushes to `main`. Jobs: `lint`, `test-server`, `test-integration`, `test-web`, `build` (images with Buildx, no push), `e2e` (Compose stack, Playwright, report artifact on failure). Tool versions come from `.tool-versions` (`go-version-file`, `node-version-file`). Every action pinned to a full commit SHA with the version in a comment. Least-privilege `permissions`. Concurrency cancels superseded runs.
- `renovate.json`: extends `config:best-practices`; enables the `asdf`, `gomod`, `npm`, `github-actions`, `dockerfile`, `docker-compose` managers; `postUpdateOptions: ["gomodTidy", "pnpmDedupe"]`; groups for Next.js and React, Playwright, golangci-lint (tool version and action together), Go toolchain; weekly schedule; lock-file maintenance; automerge for patch updates of dev tooling only after CI passes.
- `.editorconfig`, `.gitattributes`, `.github/pull_request_template.md`, `CODEOWNERS`.
- `README.md` with a "Develop" section: prerequisites (asdf, Docker), `make tools deps up`, the test targets.

Acceptance: `make ci` passes locally and in GitHub Actions. `docker compose up` serves the placeholder page on port 3000 and the API on port 8080.

## #3: Go calculation engine (`feat/go-engine`)

Package `server/internal/engine`. Pure functions, no I/O, no logging. The input carries the daily split-adjusted closes with the TTM EPS known on each day, and the chart range. Apply the spec's "Corrections to the prototype".

- Port every calculation listed in the spec section "Calculation engine", with the exact rules in `CLAUDE.md` and the prototype functions `req`, `proj`, `reqPe`, `wtdAt`, `trajInfo`, `yearRates`, `yearlyAvg`, `trlGrowth`, `basePath`, `estPe`, `peTrailing`, `exitPe`, `tone`, `defaultScen`, plus the grid, paths, "how demanding over time", share-count events, price-earnings link and P/E bands from the render code (`renderAll` and the chart functions in `prototype/pricedin.html`).
- `Evaluate(History, Levers) Evaluation` composes them.
- Shared cases: `testdata/engine/cases.json` (format in "Contracts"). The expected values come from an oracle: `tools/engine-oracle/` holds the prototype's math transcribed verbatim into a Node module (each function cites its line in the prototype) and a script that writes the cases file. `make generate-engine-cases` reruns it. The brief's three GRMN reference values are hand-written cases. Deliberate differences from the prototype are cases with a `note`.
- Go tests: table tests for each function, plus a runner that executes every shared case.

## #4: Store and blob storage (`feat/database-store`)

- `server/internal/store/migrations/*.sql` (goose, embedded): every table in the spec's data model. The append-only trigger on `facts`. Indexes for the read paths (facts by CIK and concept, prices by symbol and date, tickers by symbol).
- `server/internal/store`: a `Store` with methods grouped by table. Batch inserts with `pgx.CopyFrom` into a temporary table, then `INSERT … ON CONFLICT DO NOTHING`, so repeated loads are no-ops. Transactions through a `WithTx` helper.
- `server/internal/store/pgtest`: a Testcontainers helper that starts Postgres 18 once per package and gives each test a fresh database from a migrated template.
- `server/internal/blob`: a `Store` interface (`Put`, `Get`, `List`, `Exists`) with a filesystem implementation and an S3-compatible implementation (aws-sdk-go-v2). Keys follow `sec/…`, `prices/…`.
- `pricedin migrate up|down|status`.
- Integration tests for every store method and for the append-only rule.

## #5: SEC client and parsers (`feat/sec-client`)

- `server/internal/sec`: HTTP client with the configured User-Agent, a token-bucket limit of 10 requests a second, retries with backoff for 429 and 5xx, and context cancellation. A missing User-Agent is a configuration error.
- Parsers for `company_tickers_exchange.json`, the submissions JSON (including the `filings.recent` arrays and the extra files), company facts JSON (only the concepts PricedIn uses), and the bulk ZIP archives (streaming, one company at a time).
- Output types are the `domain` types.
- Fixtures in `testdata/sec/`: trimmed real files for NVDA (10-for-1 split in 2024, fiscal year ends in January), AAPL (fiscal year ends in September, 4-for-1 split in 2020), KO (calendar year, dividends), GRMN (calendar year), and one company with an amended filing (10-Q/A or 10-K/A). `tools/sec-fixtures/` holds the script that fetched and trimmed them.
- Tests with `httptest` for the client behavior and fixture tests for the parsers.

## #6: Price provider and Massive adapter (`feat/massive-prices`)

- `server/internal/prices`: the `Provider` interface (see "Contracts"), and a fake provider in `prices/pricestest` for other packages' tests.
- `server/internal/prices/massive`: the adapter. Bearer-token auth, rate limit and retries, pagination (`next_url`), grouped daily, per-ticker daily bars, splits, dividends, tickers with CIK. An injected recorder receives every response body, page by page, before decoding (contract `raw-recorder`).
- Fixtures in `testdata/prices/massive/`: synthetic responses in the real JSON shape, generated by `tools/price-fixtures/` from the SEC fixture companies, so that the end-to-end stack has consistent prices. They never contain real provider data.
- `pricedin prices verify`: calls the live API (only when a key is set) and prints the split check for NVDA around 2024-06-10, the dividend check for KO, and the ticker-to-CIK check for GRMN. Its output goes into `docs/data-sources.md` in #12.

## #7: TypeScript engine (`feat/ts-engine`)

- `web/src/engine/`: the same functions and `evaluate` with the same names in camelCase and the same JSON shapes. No React, no DOM.
- Vitest runs every case in `testdata/engine/cases.json` with the same runner semantics as Go.
- CI already runs both suites. Add a check that both runners know every `fn` in the cases file.

## #8: Quarter derivation (`feat/quarter-derivation`)

- `server/internal/fundamentals`: `Derive(facts []domain.Fact, opts Options) (Series, error)` with the rules in the spec sections "Deriving quarters" and "Stock splits". `Options.AsOf` gives the point-in-time variant.
- Split detection from the ratio concept and from restatements.
- Each value records its method and its inputs (accession numbers and filing dates).
- Fixture tests: for each fixture company, the derived quarterly diluted EPS and revenue match the values in the companies' own 10-Q and 10-K filings. Put the expected values in a readable table in the test, with the filing each value comes from.

## #9: Ingestion jobs (`feat/ingestion-jobs`)

- `server/internal/ingest`: jobs `sec-tickers`, `sec-bulk` (first import from the archives), `sec-daily` (new filings since the last run), `prices-backfill`, `prices-daily`, `price-reference` (splits, dividends, ticker map), `fundamentals-rebuild`, and `company` (one company on demand). Every job writes raw responses to the blob store first, records an `ingest_runs` row, and is safe to run twice.
- `pricedin jobs run <job>`, `pricedin ingest company <ticker>`, and `pricedin scheduler` (daily jobs at fixed UTC times, Postgres advisory locks).
- Universe: companies with a current NYSE or Nasdaq ticker. `PRICEDIN_UNIVERSE` can narrow it to a list of tickers for development.
- `server/cmd/fixtureserver`: serves `testdata/sec` and `testdata/prices` at SEC-like and Massive-like paths, for Compose and end-to-end tests.
- Compose: a `scheduler` service, and a `fixtures` profile that runs the fixture server and points the server at it.
- Integration tests: each job against the fixture server and a real Postgres, run twice to prove idempotency.
- `make seed`: loads a short list of tickers into the local stack.

## #10: HTTP API and authentication (`feat/http-api`)

- `server/api/openapi.yaml`: OpenAPI 3.1 for every endpoint in the spec, with the provenance schema.
- `server/internal/auth`: Argon2id password check, sessions, API tokens, login rate limit, origin check. `pricedin auth hash-password`, `pricedin auth token create|list|revoke`.
- `server/internal/stocks`: the service that reads the store, builds the engine `History` (split-adjusted EPS and quarter-end closes), and assembles profiles, prices, fundamentals and evaluations with sources.
- `server/internal/httpapi`: handlers, problem-details errors, request IDs, access logs, timeouts.
- Tests: handler tests validate every response against the OpenAPI document (kin-openapi). Integration tests run the API against a seeded database.

## #11: Web shell (`feat/web-shell`)

- Generated API types from the OpenAPI document (`openapi-typescript`, committed, checked for drift in CI) and a typed client (`openapi-fetch`).
- Login page, route guard, logout, a session-expired path.
- Layout: top bar (brand, company, price, TTM EPS, P/E, "as of close" date), left panel (stock search, starting point, every lever with its text field, Reset inputs), main column with empty panel slots.
- Lever state: one reducer with the prototype's rules (a new stock resets every input except target return and holding period; Reset resets all). `localStorage` persistence with safe fallbacks.
- The walkthrough framework: a `Walkthrough` component and a step API that every panel uses (see "Contracts"), and the left panel's own walkthrough.
- A data page: data freshness per source, the latest ingest runs, and a "Load or refresh this company" action.
- Component tests for the reducer and the levers.

## #12: Deploy and docs (`chore/deploy-and-docs`)

- `deploy/fly/server.toml` and the scheduled-job machine, `deploy/fly/README` steps, Vercel project settings in `web/vercel.json`, a manual `deploy.yml` workflow that needs secrets and does nothing without them.
- `docs/architecture.md`, `docs/data-sources.md` (with the verify output), `docs/api.md`, `docs/runbook.md` (first deploy, rotate secrets, rerun a job, reprocess from the blob store, restore).
- README: what PricedIn is, a screenshot, quick start, links to the docs.

## #13 to #16: Panels

Each panel PR ports one part of the prototype's UI into React components under `web/src/components/panels/<panel>/`, with its inline walkthrough, its hover behavior, and component tests. Each one reads the shared `evaluate` output and does not compute engine values itself.

- #13 (`feat/priced-in-panel`): the priced-in panel (required growth, exit price and EPS needed, verdict, baselines, gap, quarterly YoY chips, growth scale) and the return grid (YOU cell, row tags, row outline on hover).
- #14 (`feat/price-chart`): the price chart: daily close, overlays for TTM EPS, P/E, 12-month changes, P/E bands and share-count markers, chips as legend and toggle with hovered values, ranges 6M, 1Y and 3Y, the band caption, and the side-by-side layout with the summary at 1600 px and wider.
- #15 (`feat/earnings-panels`): earnings trajectory, how demanding over time, and the bear, base and bull cases.
- #16 (`feat/financials-panel`): financials (revenue, operating costs, operating income, EPS, free cash flow, diluted shares), the Compare view and the correlation table, and links to filings.

## #17: End-to-end suite (`test/end-to-end-flows`)

- Playwright tests against the Compose stack with the `fixtures` profile: run the ingestion jobs, log in, search and pick a stock, read the priced-in numbers and compare them with the evaluation API, move each lever, switch modes, check the return grid and the charts, run each walkthrough, check the source links, log out.
- Run on desktop and phone viewports, in light and dark color schemes.

## Contracts

| Name | Producer | Consumers | Shape | Realization |
| --- | --- | --- | --- | --- |
| `domain-types` | #2 | #4, #5, #6, #8, #9, #10 | `server/internal/domain`: `CIK` (int64, `String()` pads to 10 digits), `Date` (civil date, JSON and SQL as `YYYY-MM-DD`), `Company`, `Ticker`, `Filing`, `Fact`, `PriceBar`, `Split`, `Dividend`. | pre-merge stub (in #2) |
| `engine-io` | #3 | #7, #10, #11, #13 to #16 | Go `engine.History`, `engine.Levers`, `engine.Evaluation`, JSON in camelCase. Rates and returns are fractions (0.15), never percent. A value that cannot be computed is `null` with a sibling `reason` code. | data-only (the cases file pins it) |
| `engine-cases` | #3 | #7 | `testdata/engine/cases.json`: `{ "version": 1, "cases": [ { "name", "fn", "input", "expected", "tolerance", "note"? } ] }`. `fn` is the TypeScript function name. Numbers compare with absolute tolerance; `null` must match `null`. | data-only |
| `raw-recorder` | #5, #6 | #9 | Each client package declares `type Recorder interface { Record(ctx context.Context, key string, body []byte) error }` and calls it with every response body before decoding; a non-nil error fails the request. Keys hold the retrieval date and the first 16 hex digits of the body's SHA-256, so distinct responses never collide and an identical body is stored once: `sec/<path>/<yyyy-mm-dd>/<sha16>-<file>`, `prices/massive/<endpoint>/<params>/<yyyy-mm-dd>/<sha16>-<page>.json`. The recorder builds the final key from the client's base key, so both clients use one key scheme. A nil recorder records nothing. Bulk archives stream to the blob store instead. | data-only |
| `price-provider` | #6 | #9, #10 | `prices.Provider` with `Name() string`, `GroupedDaily(ctx, date) ([]domain.PriceBar, error)`, `DailyBars(ctx, symbol, from, to) ([]domain.PriceBar, error)`, `Splits(ctx, symbol) ([]domain.Split, error)`, `Dividends(ctx, symbol) ([]domain.Dividend, error)`, `Tickers(ctx) ([]ProviderTicker, error)`. Bars are raw (not split-adjusted). | data-only (sequential) |
| `fundamentals-series` | #8 | #9, #10 | `fundamentals.Series{Quarters []Quarter; Splits []domain.Split}`; `Quarter` has fiscal year, fiscal quarter, label, period start and end, and a `Value` per metric with `Method`, `Inputs []SourceRef`, `Filed`. | data-only (sequential) |
| `openapi` | #10 | #11 to #17 | `server/api/openapi.yaml` | data-only (sequential) |
| `walkthrough-api` | #11 | #13 to #16 | `<Walkthrough steps={…}>` where a step has `title`, `body`, `you` (a line with the user's numbers), `highlight` (element keys), `levers` (lever keys to ring); the last step has `takeaway`. Elements opt in with `data-walk="<key>"`. | data-only (sequential) |
| `fixture-server-paths` | #9 | #17 | SEC paths as on `www.sec.gov` and `data.sec.gov`, Massive paths as on `api.massive.com`, under one base URL each. `PRICEDIN_SEC_BASE_URL`, `PRICEDIN_SEC_DATA_BASE_URL`, `PRICEDIN_MASSIVE_BASE_URL`. | data-only |

## Conventions

- Layout: Go code in `server/internal/<package>`, one concern per package, commands in `server/cmd`. Web code in `web/src/{app,components,engine,api,state,styles}`. Shared test data in `testdata/<area>/`. Scripts that produce test data in `tools/<name>/`.
- Go: `context.Context` first; errors wrapped with `%w` and a short lowercase prefix; no package-level state except constants; `slog` loggers passed in; table tests with the standard library and `go-cmp`; integration tests in `*_integration_test.go` behind the `integration` build tag.
- TypeScript: strict mode, named exports, no `any`. React components in PascalCase files. Engine names mirror Go (`RequiredGrowth` and `requiredGrowth`).
- Configuration: environment variables only, all with the `PRICEDIN_` prefix, all listed in `.env.example` and parsed in `internal/config`.
- Units: rates and returns are fractions in code and JSON. Money is US dollars as `float64`/`number`. Dates are `YYYY-MM-DD`. Share counts are whole shares.
- Vocabulary in code and copy: target return, holding period, exit P/E, typical P/E, required growth, baseline (weighted trend, today, trailing, custom), verdict (undemanding, tight, demanding), return grid, walkthrough. Never "fair value", "buy" or "sell".
- Commits and PR titles: Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`, `refactor:`, `ci:`, `build:`).
- Every PR keeps `make ci` green and adds tests for what it adds.
