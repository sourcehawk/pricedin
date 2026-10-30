# PricedIn

PricedIn shows how much future earnings growth a stock price already assumes. It answers one question before any deep research starts: what must this company deliver for me to earn my target return, and has it ever delivered that?

PricedIn never tells you to buy or sell. It shows which stocks deserve research, and why.

v1 is in development. The product brief is in `CLAUDE.md`, the design in `docs/superpowers/specs/`, and the behavior reference in `prototype/pricedin.html`.

## Develop

### Prerequisites

- [asdf](https://asdf-vm.com/). It installs the tool versions in `.tool-versions`: Go, Node.js, pnpm, golangci-lint, actionlint and shellcheck.
- Docker with Compose v2.

### First setup

```sh
make tools deps
cp .env.example .env
make up
```

`make tools` installs the pinned tools. `make deps` installs the Go modules, the npm packages and the Playwright browser. `make up` builds the images and starts the stack in the background:

- the web app on http://localhost:3000
- the API on http://localhost:8080 (`/healthz` and `/readyz`)
- the metrics on http://localhost:9090/metrics
- Postgres on `localhost:5432`

If one of these ports is in use, change it in `.env` (`PRICEDIN_WEB_PORT`, `PRICEDIN_API_PORT`, `PRICEDIN_METRICS_PORT`, `PRICEDIN_DB_PORT`). `make down` stops the stack. `make logs` follows its logs.

### Tests and checks

| Target | What it runs |
| --- | --- |
| `make lint` | golangci-lint, ESLint, the TypeScript type check, Prettier, actionlint and shellcheck |
| `make test` | The Go unit tests (`make test-server`) and the web unit tests (`make test-web`) |
| `make test-integration` | The Go tests with the `integration` build tag |
| `make test-e2e` | Builds the images, starts the Compose stack, runs Playwright, and stops the stack |
| `make ci` | Everything that CI runs, in the CI order |

`make` without a target lists every target. CI calls the same targets, so a green `make ci` predicts a green CI run.

### Layout

| Path | Content |
| --- | --- |
| `server/` | Go module: the `pricedin` command, the API, and later the ingestion jobs and the engine |
| `web/` | Next.js app in TypeScript |
| `e2e/` | Playwright tests that run against the Compose stack |
| `prototype/` | The prototype, the reference for behavior, calculations, copy and layout |
| `docs/` | The design, the plan and later the architecture and runbooks |
| `scripts/` | Shell scripts that the Makefile calls |
