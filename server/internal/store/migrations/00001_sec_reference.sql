-- SEC reference data: companies, the symbols that map to them, and their
-- filings.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE companies (
    cik                    bigint PRIMARY KEY CHECK (cik > 0),
    name                   text NOT NULL,
    sic                    text NOT NULL DEFAULT '',
    sic_description        text NOT NULL DEFAULT '',
    -- Month and day the fiscal year ends, as MMDD; empty when unknown.
    fiscal_year_end        text NOT NULL DEFAULT '' CHECK (fiscal_year_end ~ '^([0-9]{4})?$'),
    state_of_incorporation text NOT NULL DEFAULT '',
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- Ticker search matches name substrings with ILIKE.
CREATE INDEX companies_name_trgm ON companies USING gin (name gin_trgm_ops);

-- Each source (SEC, the price provider) keeps its own dated mapping.
CREATE TABLE tickers (
    symbol     text NOT NULL CHECK (symbol <> ''),
    cik        bigint NOT NULL REFERENCES companies (cik),
    exchange   text NOT NULL DEFAULT '',
    valid_from date NOT NULL,
    valid_to   date CHECK (valid_to >= valid_from),
    source     text NOT NULL CHECK (source <> ''),
    PRIMARY KEY (source, symbol, valid_from)
);

CREATE UNIQUE INDEX tickers_one_open_row ON tickers (source, symbol) WHERE valid_to IS NULL;
CREATE INDEX tickers_symbol ON tickers (symbol) WHERE valid_to IS NULL;
CREATE INDEX tickers_cik ON tickers (cik);

CREATE TABLE filings (
    accession        text PRIMARY KEY,
    cik              bigint NOT NULL REFERENCES companies (cik),
    form             text NOT NULL,
    filed            date NOT NULL,
    report_date      date,
    primary_document text NOT NULL DEFAULT '',
    loaded_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX filings_cik_filed ON filings (cik, filed);

-- +goose Down
DROP TABLE filings;
DROP TABLE tickers;
DROP TABLE companies;
DROP EXTENSION IF EXISTS pg_trgm;
