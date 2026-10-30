# PricedIn

PricedIn shows how much future earnings growth a stock price already assumes. It answers one question before any deep research starts: what must this company deliver for me to earn my target return, and has it ever delivered that?

PricedIn never tells you to buy or sell. It shows which stocks deserve research, and why.

v1 is in development. The product brief is in `CLAUDE.md`, the design in `docs/superpowers/specs/`, and the behavior reference in `prototype/pricedin.html`.

## Develop

### Prerequisites

- [asdf](https://asdf-vm.com/) 0.16 or later. It installs the tool versions in `.tool-versions`: Go, Node.js, pnpm, golangci-lint, actionlint and shellcheck.
- Docker with Compose v2.

### First setup

```sh
make tools deps
cp .env.example .env
make up
```

`make tools` adds the asdf plugins that are missing and installs the pinned tools. `make deps` installs the Go modules, the npm packages and the Playwright browser. `make up` builds the images and starts the stack in the background:

- the web app on http://localhost:3000
- the API on http://localhost:8080 (`/healthz` and `/readyz`)
- the metrics on http://localhost:9090/metrics
- Postgres on `localhost:5432`

Before the server starts, the `migrate` service applies the database migrations. The server keeps raw downloads in the `blob-data` volume.

If one of these ports is in use, change it in `.env` (`PRICEDIN_WEB_PORT`, `PRICEDIN_API_PORT`, `PRICEDIN_METRICS_PORT`, `PRICEDIN_DB_PORT`). `make down` stops the stack. `make logs` follows its logs.

### Load data without SEC and Massive

The fixture server answers at the SEC and Massive paths from the files in `testdata/`, so you need no API key and no network access to those services:

```sh
make up-fixtures
make seed
```

`make up-fixtures` starts the stack with the fixture server and the scheduler (Compose files `docker-compose.yml` and `docker-compose.fixtures.yml`, profile `fixtures`). `make seed` then runs the jobs that load the five fixture companies: NVDA, AAPL, KO, GRMN and ADM. `make down-fixtures` stops that stack.

### Load real data

The ingestion jobs need `PRICEDIN_SEC_USER_AGENT` (a name and a contact email address) for SEC, and `PRICEDIN_MASSIVE_API_KEY` for prices. Set them in `.env`. While you develop, narrow the jobs to a few tickers, or they load every NYSE and Nasdaq company:

```sh
PRICEDIN_UNIVERSE=NVDA,KO
```

| Command | What it does |
| --- | --- |
| `pricedin jobs list` | Lists the jobs and the latest run of each |
| `pricedin jobs run <job> [args]` | Runs one job. For a first import, run `sec-tickers`, `sec-bulk`, `price-reference`, `prices-backfill` and `fundamentals-rebuild all`, in that order |
| `pricedin ingest company <ticker>` | Loads one company: its SEC filings and facts, its splits, dividends and daily prices, and its quarters. It works for any ticker in the SEC ticker map, also outside `PRICEDIN_UNIVERSE` |
| `pricedin scheduler` | Runs the `daily` job every day at `PRICEDIN_SCHEDULE_DAILY` (UTC, default 01:30) |

In the Compose stack, put `docker compose exec server /pricedin` before each command, such as `docker compose exec server /pricedin ingest company GRMN`. On your host, run `go run ./cmd/pricedin` in `server/` instead. Every job is safe to run again. When another process runs the same job, `jobs run` skips it, or waits for it with `--wait`. `docker compose --profile scheduler up` also starts the scheduler.

### Tests and checks

| Target | What it runs |
| --- | --- |
| `make lint` | golangci-lint, ESLint, the TypeScript type check, Prettier, actionlint, shellcheck, and a check that the engine cases are current |
| `make test` | The Go unit tests (`make test-server`) and the web unit tests (`make test-web`) |
| `make test-integration` | The Go tests with the `integration` build tag |
| `make test-e2e` | Builds the images, starts a separate Compose stack against the fixture server (project `pricedin-e2e`, web on port 3100, API on port 8180), runs Playwright, and removes that stack. The stack of `make up` is not touched |
| `make generate-engine-cases` | Writes the shared engine cases in `testdata/engine/` from the prototype oracle in `tools/engine-oracle/` |
| `make generate-price-fixtures` | Writes the synthetic Massive fixtures in `testdata/prices/massive/`, with closes that follow the earnings in the SEC fixtures |
| `make ci` | Everything that CI runs, in the CI order |

`make migrate` applies the migrations to the database of the Compose stack. `make migrate MIGRATE_ARGS=status` lists them, and `MIGRATE_ARGS=down` rolls back the latest one. For another database, set `PRICEDIN_DATABASE_URL` and run `go run ./cmd/pricedin migrate up` in `server/`.

`make` without a target lists every target. CI calls the same targets, so a green `make ci` predicts a green CI run.

### Layout

| Path | Content |
| --- | --- |
| `server/` | Go module: the `pricedin` command, the API, the calculation engine (`internal/engine`), the ingestion jobs (`internal/ingest`) and the fixture server (`cmd/fixtureserver`) |
| `web/` | Next.js app in TypeScript, with the browser copy of the calculation engine (`src/engine`) |
| `e2e/` | Playwright tests that run against the Compose stack |
| `prototype/` | The prototype, the reference for behavior, calculations, copy and layout |
| `docs/` | The design, the plan and later the architecture and runbooks |
| `scripts/` | Shell scripts that the Makefile calls |
| `testdata/` | Test data that more than one part reads, such as the SEC fixtures in `testdata/sec/` and the shared engine cases in `testdata/engine/` |
| `tools/` | Programs that make test data, such as `tools/sec-fixtures/` and the engine oracle in `tools/engine-oracle/` |
