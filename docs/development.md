# Development

This page tells you how to work on PricedIn on your machine: the tools, the local stack, the tests, the migrations and the generated files. `docs/architecture.md` explains how the parts fit together.

## Tools

To run the stack, you need only Docker with Compose v2, `make` and `git`: the README's quick start builds everything in containers. To change the code and run the tests and linters, you also need the tools below on your machine.

- [asdf](https://asdf-vm.com/) 0.16 or later. It installs the versions in `.tool-versions`: Go, Node.js, pnpm, golangci-lint, actionlint and shellcheck.
- Docker with Compose v2.

Install the tools and the dependencies once:

```sh
make tools deps
cp .env.example .env
```

`make tools` adds each missing asdf plugin and installs the pinned versions. `make deps` installs the Go modules, the npm packages and the Playwright browser. `make` without a target lists every target.

## The local stack

Compose runs the stack. There are two variants:

| Command | Stack | Data |
| --- | --- | --- |
| `make up` | Postgres, the migrations, the API server and the web app (Compose project `pricedin`) | Real data, if you set the keys below |
| `make up-fixtures` | The same, plus the scheduler and the fixture server (project `pricedin-fixtures`) | Fixture data from `testdata/` |

Both variants use these addresses:

- the web app on http://localhost:3000
- the API on http://localhost:8080 (`/healthz` and `/readyz`)
- the metrics on http://localhost:9090/metrics
- Postgres on `localhost:5432`

The two projects have their own volumes, so fixture data never mixes with real data. They use the same host ports, so stop one stack before you start the other. If a port is in use, change it in `.env` (`PRICEDIN_WEB_PORT`, `PRICEDIN_API_PORT`, `PRICEDIN_METRICS_PORT`, `PRICEDIN_DB_PORT`).

Before the server starts, the `migrate` service applies the migrations. The server keeps raw downloads in the `blob-data` volume.

| Command | What it does |
| --- | --- |
| `make down` or `make down-fixtures` | Stops the stack and keeps its volumes |
| `make logs` | Follows the logs of the stack of `make up` |
| `make migrate` | Applies the migrations to the database of `make up` (`MIGRATE_ARGS=status` lists them, `MIGRATE_ARGS=down` rolls back the latest one) |

### Fixture data

The fixture server answers at the SEC and Massive paths with the files in `testdata/`. The fixture stack needs no key and no network access to those services.

```sh
make up-fixtures
make seed
```

`make seed` runs the jobs of a first import (`sec-tickers`, `sec-bulk`, `price-reference`, `prices-backfill` and `fundamentals-rebuild all`) for the five fixture companies: NVDA, AAPL, KO, GRMN and ADM. The SEC fixtures are trimmed real filings. The price fixtures are synthetic, and their closes follow the earnings in the SEC fixtures.

### Real data

The jobs need `PRICEDIN_SEC_USER_AGENT` (a name and a contact email address) for SEC, and `PRICEDIN_MASSIVE_API_KEY` for prices. Set them in `.env`. While you develop, narrow the jobs to a few tickers, or they load every NYSE and Nasdaq company:

```sh
PRICEDIN_UNIVERSE=NVDA,KO
```

Then run the jobs in the stack of `make up`:

```sh
docker compose exec server /pricedin ingest company GRMN
```

On your host, run `go run ./cmd/pricedin <command>` in `server/` instead. `docker compose --profile scheduler up` also starts the scheduler. `docs/runbook.md` lists the jobs.

### Owner login and API tokens

Without an owner, no login succeeds. To log in to the web app, add `PRICEDIN_OWNER_EMAIL` and `PRICEDIN_OWNER_PASSWORD_HASH` to `.env`, then start the stack again. Make the hash with the server image of the stack:

```sh
docker run --rm -it pricedin-server:dev auth hash-password
```

Put the hash in single quotes in `.env`, because it holds `$` characters. Compose allows logins from `http://localhost:<PRICEDIN_WEB_PORT>`.

The web app has three pages: the login page (`/login`), the stock page (`/`) with the stock search and the levers in the left panel and the panels in the main column, and the data page (`/data`) with the freshness of the data, the latest ingest runs and an action that loads one company. `web/src/proxy.ts` sends a visitor without a session cookie to the login page, and sends each `/api/*` request to `PRICEDIN_API_URL`, which Compose sets to `http://server:8080`.

For a script, make an API token:

```sh
docker compose exec server /pricedin auth token create local
```

In the fixture stack, name its Compose files and profile:

```sh
docker compose -f docker-compose.yml -f docker-compose.fixtures.yml --profile fixtures exec server /pricedin auth token create local
```

`docs/api.md` shows how to call the API with it.

## Tests and checks

| Target | What it runs |
| --- | --- |
| `make lint` | golangci-lint, ESLint, the TypeScript type check, Prettier, actionlint, shellcheck, and a check that the engine cases are current |
| `make test` | The Go unit tests (`make test-server`), the web unit tests (`make test-web`), and the tests of the shell scripts in `scripts/` (`make test-scripts`) |
| `make test-integration` | The Go tests with the `integration` build tag. They start Postgres in a container (Testcontainers), so Docker must run |
| `make test-e2e` | Builds the images, starts a separate Compose stack against the fixture server (project `pricedin-e2e`, web on port 3100, API on port 8180), seeds it, runs Playwright, and removes that stack. The stack of `make up` is not touched |
| `make ci` | Everything that CI runs, in the CI order |

CI calls the same targets, so a green `make ci` predicts a green CI run. On a pull request, CI runs only the test jobs that the changed paths can affect, and `scripts/ci-changes.sh` sets which paths affect which job. The lint job always runs, a push to main runs every job, and CI builds the images in the end-to-end job only, not in a job of its own.

Integration tests are in files named `*_integration_test.go`. A package that uses the database calls `pgtest.Main` from its `TestMain`, and each test gets a fresh database from `pgtest.NewURL` or `pgtest.NewPool`.

### End-to-end tests

The Playwright tests in `e2e/tests/` drive the web app of a Compose stack against the fixture server. `scripts/e2e.sh` starts that stack with `docker-compose.e2e.yml` on top of the fixture files, without the scheduler and without `.env`. It creates the owner account from `PRICEDIN_E2E_OWNER_EMAIL` and `PRICEDIN_E2E_OWNER_PASSWORD`, and loads the fixture companies with the jobs of `make seed`. It leaves ADM out, so that a test can load it on demand. Each test runs on a desktop and a phone viewport, in light and dark color schemes. A request to a host outside the stack fails the test.

To work on the tests, keep the stack between runs:

```sh
make e2e-up                                   # build the images, start and seed the stack
make e2e-run ARGS="levers --project=desktop-light"
make e2e-down                                 # remove the stack and its volumes
```

Playwright reads each file filter in `ARGS` as a regular expression and matches it against the full path of each test file. If a filter also matches a part of the checkout's path, for example `price-chart` in a worktree named `price-chart-flows`, it selects every file. Use a filter that only the file name contains, such as `price-chart.spec`. Add `--list` to see which tests a filter selects without running them.

The test that loads ADM fails on a second run against the same stack, because ADM is then loaded. Start a new stack for it.

## Add a migration

Migrations are SQL files in `server/internal/store/migrations`, applied by goose in the order of their numbers.

1. Add a file with the number after the highest in that folder, such as `00008_<what>.sql`.
2. Write a `-- +goose Up` section and a `-- +goose Down` section. Put a function body between `-- +goose StatementBegin` and `-- +goose StatementEnd`.
3. Start the stack with `make up`. Then run `make migrate`, then `make migrate MIGRATE_ARGS=down`, then `make migrate` again, to make sure that both directions work.
4. Run `make test-integration`. The template database of the tests applies every migration.

In production, the deploy workflow applies the new migrations as the owner of the tables before it deploys the server (`docs/runbook.md`). The server connects as the app role, which owns no table. The default privileges of `deploy/sql/roles.sql` give the app role read and write access to each new table. If rows of a new table must never change, give it triggers that reject `UPDATE`, `DELETE` and `TRUNCATE`, as `00002_facts.sql` does for `facts`. The app role cannot disable a trigger.

Never change a migration that is on `main`. Production has applied it.

## Generated files

| Files | Command | When |
| --- | --- | --- |
| `web/src/api/schema.d.ts`, the TypeScript types of the API | `make generate-api-types` | After a change to `server/api/openapi.yaml`. Commit the new file. `make lint` (`lint-api-types`), and thus CI, fails while the committed types are stale |
| `testdata/engine/cases.json` and `evaluation-example.json` | `make generate-engine-cases` | After a change to the oracle in `tools/engine-oracle/`, the script that runs the prototype's math to get the expected engine values. Commit the new files. `make lint`, and thus CI, fails while the committed cases are stale |
| `testdata/prices/massive/` | `make generate-price-fixtures` | After a change to `tools/price-fixtures/` or to the SEC fixtures, whose earnings set the synthetic closes |
| `testdata/sec/` | `go run .` in `tools/sec-fixtures/` (see its README) | Only to download the SEC files again. It needs `PRICEDIN_SEC_USER_AGENT` |

`make generate` runs the first three.

## Layout

| Path | Content |
| --- | --- |
| `server/` | Go module: the `pricedin` command, the API, the calculation engine (`internal/engine`), the ingestion jobs (`internal/ingest`) and the fixture server (`cmd/fixtureserver`) |
| `web/` | Next.js app in TypeScript, with the browser copy of the calculation engine (`src/engine`) |
| `e2e/` | Playwright tests that run against the Compose stack |
| `deploy/` | Fly.io settings and the SQL script of the app role |
| `prototype/` | The prototype, the reference for behavior, calculations, copy and layout |
| `docs/` | These pages, and the design, the plan and the state of v1 in `docs/superpowers/` |
| `scripts/` | Shell scripts that the Makefile and the workflows call |
| `testdata/` | Test data that more than one part reads |
| `tools/` | Programs that make test data |
