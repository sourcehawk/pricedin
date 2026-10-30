# HTTP API

The API gives the data and the calculations behind the web app: stock profiles, the engine input, the derived quarters with their filings, and full evaluations. Every number carries its source and its as-of date. The API never gives buy or sell advice.

The contract is the OpenAPI 3.1 document `server/api/openapi.yaml`. A running server also serves it at `GET /api/v1/openapi.yaml` (with authentication). This page shows how to use the API. The document has every field.

The examples use the local fixture stack (`make up-fixtures` and `make seed`), so their prices are synthetic. Against a deployment, use the Fly URL, such as `https://pricedin-api.fly.dev`, in place of `http://localhost:8080`.

## Authenticate

Every `/api/v1` request needs one of two credentials.

### API token (scripts)

The owner makes each token with the `pricedin` command, on the machine of the stack or of the deployment. A token does not expire. The command shows it one time:

```sh
# Local fixture stack
docker compose -f docker-compose.yml -f docker-compose.fixtures.yml --profile fixtures \
  exec server /pricedin auth token create my-script
# Deployment
flyctl ssh console --app pricedin-api -C '/pricedin auth token create my-script'
```

Send it in the `Authorization` header:

```sh
export PRICEDIN_TOKEN='<token>'
curl -H "Authorization: Bearer $PRICEDIN_TOKEN" http://localhost:8080/api/v1/status
```

A request with a bearer token needs no `Origin` header, and the login rate limit does not apply to it. `pricedin auth token list` shows the tokens, and `pricedin auth token revoke <name>` ends one.

### Session (browser)

The web app logs in with the owner's email and password, and gets the cookie `pricedin_session`. With curl:

```sh
curl -c cookies.txt -H 'Content-Type: application/json' -H 'Origin: http://localhost:3000' \
  -d '{"email":"you@example.com","password":"..."}' http://localhost:8080/api/auth/login
```

```json
{"email":"you@example.com","createdAt":"2026-10-30T11:28:04.848296915Z","expiresAt":"2026-11-29T11:28:04.848296915Z"}
```

A login, a logout, and each `POST` with the cookie must send an `Origin` header that is in `PRICEDIN_PUBLIC_ORIGIN`. A `GET` with the cookie needs none. Each client address can try to log in 5 times a minute. `GET /api/auth/session` returns the current session, and `POST /api/auth/logout` ends it. A session lasts `PRICEDIN_SESSION_TTL` (30 days by default).

## Rules for all requests

- Rates and returns are fractions: `0.15` is 15%. Money is US dollars. Per-share values and closes are split-adjusted to today's share count. Dates are `YYYY-MM-DD`.
- A `POST` must send `Content-Type: application/json`. A request body never holds `null`: leave out an optional field instead.
- A value that the engine cannot compute is `{"value": null, "reason": "<code>"}`. A value that exists is `{"value": <value>}`. For example, `needs_positive_eps` means that the TTM EPS is zero or negative, so the required growth has no value. The schema `Reason` of the contract lists every code.
- Each response has an `X-Request-ID` header.

## Endpoints

| Method and path | What it returns |
| --- | --- |
| `GET /api/v1/search?q=&limit=` | Current tickers that start with `q`, or whose company name contains it |
| `GET /api/v1/status` | The newest run of each ingestion job, the newest close, and the number of loaded companies |
| `GET /api/v1/stocks/{ticker}` | The profile: company, latest close, TTM EPS, TTM dividend, P/E, freshness |
| `GET /api/v1/stocks/{ticker}/history?range=` | The engine input: every quarter, and the daily closes with the TTM EPS known on each day |
| `GET /api/v1/stocks/{ticker}/fundamentals` | Every derived quarter with each metric and its filings, the confirmed splits, and the conflicts |
| `POST /api/v1/stocks/{ticker}/evaluation` | The full engine output for the levers in the body |
| `POST /api/v1/stocks/{ticker}/load` | Loads or refreshes one company from SEC and Massive, and returns its new profile |
| `GET /api/v1/openapi.yaml` | The contract |
| `GET /healthz`, `GET /readyz` | The liveness and readiness probes. They need no authentication |

### Search

```sh
curl -H "Authorization: Bearer $PRICEDIN_TOKEN" 'http://localhost:8080/api/v1/search?q=ms&limit=5'
```

```json
{"results":[{"symbol":"MSFT","name":"MICROSOFT CORP","exchange":"Nasdaq","cik":"0000789019","loaded":false}]}
```

`loaded: false` means that the company is known, but its data is not loaded. `POST …/load` loads it.

### Status

```sh
curl -H "Authorization: Bearer $PRICEDIN_TOKEN" http://localhost:8080/api/v1/status
```

Trimmed:

```json
{
  "ingestRuns": [
    {"id": 10, "job": "prices-backfill", "status": "succeeded", "abandoned": false,
     "startedAt": "2026-10-30T11:26:09.892804Z", "finishedAt": "2026-10-30T11:26:16.975742Z",
     "stats": {"bars": 6155, "days": 1237, "failed": 0, "loaded": 1231, "not_authorized": 6}, "error": ""}
  ]
}
```

### Profile

```sh
curl -H "Authorization: Bearer $PRICEDIN_TOKEN" http://localhost:8080/api/v1/stocks/GRMN
```

Trimmed. Each value has a `source` and an `asOf` date:

```json
{
  "ticker": {"symbol": "GRMN", "exchange": "NYSE"},
  "company": {"cik": "0001121788", "name": "GARMIN LTD", "sicCode": "3812", "fiscalYearEnd": "1226"},
  "loaded": true,
  "latestClose": {
    "value": 258.2,
    "source": {"kind": "price", "provider": "massive", "symbol": "GRMN", "closeDate": "2026-10-29", "rawClose": 258.2, "splitFactor": 1},
    "asOf": "2026-10-29", "asOfLabel": "as of close"
  },
  "ttmEps": {
    "value": 10.41,
    "source": {"kind": "derived", "method": "ttm", "label": "Q3 FY26", "inputs": [
      {"kind": "sec", "form": "10-Q", "accession": "0001193125-26-435070", "filed": "2026-10-28",
       "concept": "EarningsPerShareDiluted", "periodStart": "2026-06-28", "periodEnd": "2026-09-26", "reported": 2.8,
       "url": "https://www.sec.gov/Archives/edgar/data/1121788/000119312526435070/grmn-20260926.htm"}
    ]},
    "asOf": "2026-10-28"
  },
  "pe": {"value": 24.803073967339095, "source": {"kind": "derived", "method": "price_over_ttm_eps"}},
  "freshness": {"latestFiled": "2026-10-28", "latestClose": "2026-10-29"}
}
```

### History

`range` is `6M`, `1Y`, `3Y` (the default) or `max`. It limits the daily closes only. The quarters are always all of them.

```sh
curl -H "Authorization: Bearer $PRICEDIN_TOKEN" 'http://localhost:8080/api/v1/stocks/GRMN/history?range=1Y'
```

Trimmed. `history` is the engine input. `quarters` gives the sources of each quarter, in the same order. `prices` is the source of every daily close: the provider, its symbol, and the splits after the first close of the range. To get the provider's raw close of a day, multiply the close by the ratio of each split after that day:

```json
{
  "ticker": "GRMN",
  "range": "1Y",
  "history": {
    "quarters": [{"label": "Q3 FY26", "periodEnd": "2026-09-26", "eps": 2.8, "close": 249.77, "dilutedShares": 193617000,
                  "revenue": 1986076000, "operatingIncome": 611648000, "operatingCosts": 1374428000, "freeCashFlow": 293268000}],
    "daily": [{"date": "2026-10-29", "close": 258.2, "ttmEps": 10.41}]
  },
  "quarters": [{"label": "Q3 FY26", "periodEnd": "2026-09-26", "ttmEpsKnownOn": "2026-10-28",
                "eps": {"kind": "sec", "form": "10-Q", "accession": "0001193125-26-435070", "filed": "2026-10-28"}}],
  "prices": {"provider": "massive", "symbol": "GRMN", "splits": []},
  "freshness": {"latestFiled": "2026-10-28", "latestClose": "2026-10-29"}
}
```

### Evaluation

The body holds the levers. Each lever is optional, and an omitted lever takes its default: the latest close, the TTM EPS, the TTM dividend, a target return of 15%, 3 years, the estimated exit P/E, the weighted baseline, and the default bear, base and bull cases. The schema `LeversInput` of the contract lists the fields and their bounds. The company must be loaded: if the request answers 409, load it first (below).

This request asks for a 10% target return over 5 years, at an exit P/E of 22, against the trailing growth of 24 months:

```sh
curl -H "Authorization: Bearer $PRICEDIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"targetReturn": 0.10, "holdingYears": 5, "exitPe": {"mode": "custom", "custom": 22}, "baseline": {"mode": "trailing", "trailingMonths": 24}}' \
  http://localhost:8080/api/v1/stocks/GRMN/evaluation
```

The next response is for another request: the default levers (body `{}`). Trimmed:

```json
{
  "ticker": "GRMN",
  "levers": {"price": 258.2, "ttmEps": 10.41, "dividend": 3.68, "targetReturn": 0.15, "holdingYears": 3,
             "exitPe": {"mode": "estimated"}, "baseline": {"mode": "weighted"}, "chartRange": "3Y"},
  "sources": {"price": {"value": 258.2, "asOf": "2026-10-29", "asOfLabel": "as of close"}},
  "asOf": "2026-10-30",
  "evaluation": {
    "peNow": {"value": 24.803073967339095},
    "exitPe": {"mode": "estimated", "inUse": {"value": {"pe": 20, "rule": "fade"}}},
    "required": {"growth": {"value": 0.22384988694323638}, "exitPrice": {"value": 381.6499249999998}, "eps": {"value": 19.08249624999999}},
    "baselines": {"active": "weighted", "weighted": {"value": 0.264075662179214}, "today": {"value": 0.34615384615384603}},
    "gap": {"value": 0.040225775235977645},
    "verdict": {"value": "undemanding"}
  }
}
```

`required.growth` is the required growth: here 22.4% a year. The weighted baseline is 26.4%, so the gap is 4.0 pp and the verdict is "undemanding". The full output also holds the return grid, the earnings paths, "how demanding over time", the cases, the price chart and the financials. `testdata/engine/evaluation-example.json` shows a complete output.

### Load a company

```sh
curl -X POST -H "Authorization: Bearer $PRICEDIN_TOKEN" -H 'Content-Type: application/json' -d '{}' \
  http://localhost:8080/api/v1/stocks/KO/load
```

It returns the new profile. A load can take a minute, because it fetches from SEC and Massive.

## Errors

Each error is an RFC 9457 problem document with the content type `application/problem+json`:

```json
{"type":"urn:pricedin:problem:not-loaded","title":"Company data not loaded","status":409,
 "detail":"The data of MSFT is not loaded yet.","instance":"/api/v1/stocks/MSFT/history",
 "requestId":"1aceb92d908f9a63","loadable":true}
```

`type` tells a program what happened, and `detail` tells a person. `requestId` is the same value as the `X-Request-ID` header. Give it when you report a fault, because the server logs it.

| Status | `type` (after `urn:pricedin:problem:`) | Cause |
| --- | --- | --- |
| 400 | `invalid-request`, `invalid-ticker`, `invalid-levers` | The request does not match the contract, or a lever is out of bounds, such as `holdingYears must be 1 to 10, got 12` |
| 401 | `unauthenticated`, `invalid-credentials` | No valid token or session, or a wrong email or password at login |
| 403 | `forbidden-origin` | A login, or a `POST` with the cookie, without an allowed `Origin` |
| 404 | `unknown-ticker`, `not-found` | No company has that ticker, or no such path |
| 405 | `method-not-allowed` | The path exists, but not with that method. The `Allow` header lists the methods |
| 409 | `not-loaded` | The company is known, but not loaded. Send `POST …/load`, then the first request again |
| 413, 415 | `body-too-large`, `unsupported-media-type` | The body is too large, or it is not `application/json` |
| 422 | `missing-input` | The engine needs a lever that the stock cannot give, such as a price. `input` names it |
| 429 | `rate-limited` | Too many login attempts. The `Retry-After` header gives the wait |
| 500 | `internal` | An unexpected error on the server. The detail holds no internals: give the `requestId` when you report it |
| 501 | `load-unavailable` | The server cannot load companies, because it has no SEC user agent or no Massive key |
| 502 | `load-failed` | A load failed at SEC or Massive |
| 503 | `login-disabled` | No owner is configured |
