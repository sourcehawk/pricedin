# Data sources

PricedIn uses two sources. SEC EDGAR gives the filings and their reported values. Massive (formerly Polygon.io) gives the daily share prices, the splits and the dividends. PricedIn uses no analyst estimates.

This page tells you what PricedIn reads from each source, what it stores, the rules that turn filings into quarters, and the limits of the data. `docs/runbook.md` tells you how to run the jobs.

## SEC EDGAR

SEC data is public and free, and it needs no key.

### Endpoints

| Endpoint | What PricedIn reads |
| --- | --- |
| `https://www.sec.gov/files/company_tickers_exchange.json` | Ticker, CIK and exchange of every listed company |
| `https://data.sec.gov/submissions/CIK##########.json` | The filings of one company, its SIC code and its fiscal year end |
| `https://data.sec.gov/api/xbrl/companyfacts/CIK##########.json` | The XBRL facts of one company |
| `https://www.sec.gov/Archives/edgar/daily-index/YYYY/QTRn/master.YYYYMMDD.idx` | The filings of one day, for the daily job |
| `https://www.sec.gov/Archives/edgar/daily-index/xbrl/companyfacts.zip` | The facts of every company in one archive (more than 1 GB), for the first import |

The covered universe is every company with a current ticker on NYSE or Nasdaq. `PRICEDIN_UNIVERSE` narrows it to a list of tickers. The owner can also load any other company in the SEC ticker map on demand.

### Rules of access

SEC enforces two rules, and blocks clients that break them:

- Each request sends a `User-Agent` with a name and a contact email address. `PRICEDIN_SEC_USER_AGENT` holds it. Every command that calls SEC refuses to start without it.
- A client sends at most 10 requests a second. PricedIn sends at most 9 a second from each process. All SEC requests also hold the advisory lock `sec-api`, so two processes take turns and do not add up.

The client tries again after HTTP 429 and 5xx answers, with a growing wait. The download of `companyfacts.zip` is one long request and does not hold the lock.

### What PricedIn stores

PricedIn stores only the XBRL concepts that it uses:

| Use | Concepts |
| --- | --- |
| EPS | `EarningsPerShareDiluted` |
| Revenue, in this order of priority | `Revenues`, `RevenueFromContractWithCustomerExcludingAssessedTax`, `RevenueFromContractWithCustomerIncludingAssessedTax`, `SalesRevenueNet`, `SalesRevenueGoodsNet` |
| Operating income | `OperatingIncomeLoss` |
| Operating cash flow | `NetCashProvidedByUsedInOperatingActivities`, `NetCashProvidedByUsedInOperatingActivitiesContinuingOperations` |
| Capital spending | `PaymentsToAcquirePropertyPlantAndEquipment`, `PaymentsToAcquireProductiveAssets` |
| Diluted shares | `WeightedAverageNumberOfDilutedSharesOutstanding` |
| Dividends per share | `CommonStockDividendsPerShareDeclared`, `CommonStockDividendsPerShareCashPaid` |
| Stock splits | `StockholdersEquityNoteStockSplitConversionRatio1` |
| Other | `NetIncomeLoss`, `EntityCommonStockSharesOutstanding` |

Each fact row keeps the value, its unit, its period, the form type, the filing date and the accession number. An amended filing adds new rows. No row changes, and no row is removed.

### From facts to quarters

The package `server/internal/fundamentals` turns facts into fiscal quarters. These are its rules:

- A period of 80 to 100 days is a quarter. Periods of about six, nine and twelve months are year-to-date or full-year values.
- For each period, the value from the latest filing wins, because a later filing restates earlier periods for splits and corrections.
- If a filing does not report a quarter directly, the quarter comes from differences: Q2 = H1 − Q1, Q3 = 9M − H1, Q4 = FY − 9M. Cash flows in 10-Q filings are always year to date, so they always come from differences.
- Diluted shares are a weighted average. A reported quarterly count always wins. Otherwise Q4 is the difference weighted by the lengths of the periods, and PricedIn marks it as an estimate.
- Free cash flow is operating cash flow minus capital spending. Operating costs are revenue minus operating income.
- TTM values are the sum of the last four quarters.
- Labels follow the company's fiscal calendar, such as "Q3 FY26".

Splits:

- A split counts only with dated evidence: a split event from the price provider, or the split-ratio concept with its period. If the provider has an event within 180 days of the SEC date, the provider's date wins.
- Per-share values from before a split, which no later filing restated, are divided by the ratio. Share counts are multiplied by it.
- If the SEC data and the provider disagree about a split, the evaluation still works, and the data page lists the conflict.

### Known limits of the derivation

- A Q4 EPS from FY − 9M differs a little from a true fourth-quarter EPS when the share count changes during the year.
- A Q4 share count from the weighted difference is an estimate.
- PricedIn does not mix concepts within one period. A company that used a concept outside the list above for an old period has no value for that period.
- SEC split dates are approximate. The provider's date replaces them when the provider has the event.
- Some filings tag a share count in thousands or millions. If only the count times 1,000 or 1,000,000 fits the other counts of the company, the derivation uses that product, and the data page lists a conflict.

## Massive

Massive gives the daily bars, splits and dividends. PricedIn uses the Stocks Starter plan. The adapter in `server/internal/prices/massive` is the only code that knows the provider. The rest of the code uses the interface `prices.Provider`.

### Plan and endpoints

The Stocks Starter plan gives:

- five years of daily history, and no data older than that;
- no call limit (PricedIn sends at most 20 requests a second, `PRICEDIN_MASSIVE_RATE_LIMIT`);
- a license for personal use only.

| Endpoint | What PricedIn reads |
| --- | --- |
| `/v2/aggs/grouped/locale/us/market/stocks/{date}` | One bar for each US ticker on one day, for the backfill and the daily job |
| `/v2/aggs/ticker/{symbol}/range/1/day/{from}/{to}` | The bars of one symbol, for an on-demand load |
| `/stocks/v1/splits` | Split events per symbol |
| `/stocks/v1/dividends` | Cash dividends per symbol, with ex-dividend dates |
| `/v3/reference/tickers` | The provider's ticker list, with CIK numbers |

PricedIn asks for raw bars (`adjusted=false`) and stores them as they are. Massive answers `NOT_AUTHORIZED` for a day that is not over yet. Thus PricedIn asks for a day only after 20:00 New York time.

### Rules for prices

- Stored closes are raw. The view `prices_adjusted` divides each close by the splits that came after it.
- The yearly dividend is the sum of the recurring dividends with an ex-dividend date in the last twelve months. PricedIn stores special dividends, but does not count them as yearly income.
- Massive spells a class of shares with a dot (`BRK.B`), and SEC uses a hyphen (`BRK-B`). PricedIn stores the SEC spelling and changes it only for the calls to Massive.
- The page shows the latest close, labeled "as of close" with its date. PricedIn uses no intraday quotes.

### Checks against the live API

`pricedin prices verify` calls the live API with the owner's key. It prints only dates, ratios and results, because the license does not permit PricedIn to publish the provider's prices. Results of the run on 2026-10-30:

| Check | Result |
| --- | --- |
| Split event for NVDA on 2024-06-10, from 1 to 10 shares | Present, ratio 10: pass |
| Closes before 2024-06-10 divided by 10 by `prices.AdjustForSplits`, equal to the provider's own adjusted closes (2024-06-05 to 2024-06-07, factor 10) | Pass |
| Closes from 2024-06-10 not changed (2024-06-10 to 2024-06-12, factor 1) | Pass |
| The last four KO dividends (ex-dates 2025-12-01, 2026-03-13, 2026-06-15, 2026-09-15): four a year, type recurring | Pass |
| The provider's CIK of GRMN equals the SEC value 0001121788 | Pass |

Other facts measured on 2026-10-30:

- The provider's adjusted closes are adjusted for splits, not for dividends. PricedIn needs exactly that, and it computes the adjustment itself from the raw closes and the split events.
- A request for data older than five years gets `NOT_AUTHORIZED`.
- A grouped request for a weekend or a market holiday answers OK with no bars.

## Licensing

SEC data is in the public domain. The SEC fixtures in `testdata/sec/` are trimmed copies of real filings.

The Massive Stocks Starter plan allows personal use only, like the free plans of most price vendors. Exchanges own trade prices, and every vendor resells them under license. Thus:

- v1 is a private deployment for its owner, behind a login.
- The price fixtures in `testdata/prices/` are synthetic. No value in them comes from Massive.
- Before anyone else can see the site or call the API, replace the price plan with one that has display rights. The adapter interface makes this a change of one package. `CLAUDE.md` ("Price data") lists the vendors and the prices of their plans.

## Limits for backtesting

The data model keeps facts point in time, but other data does not keep its history:

- Prices, provider splits and dividends are stored in place. If the provider corrects a bar or a split, PricedIn writes the correction over the old row. A backtest sees today's corrections, not the values that were known then.
- The SIC code and the name of a company are also stored in place.
- The plan gives five years of history. A backtest cannot go further back.
- The provider covers current symbols only. Delisted companies are missing, so a backtest over this data has survivorship bias. The backtester needs another price source.
- The raw responses in the blob store keep each version of a provider response by date. A later tool can use them to rebuild what was known on a date, for the period after PricedIn started to record.
