-- Daily bars, splits and dividends from the price provider. Closes are raw;
-- the view prices_adjusted divides them by the splits that came later.

-- +goose Up
CREATE TABLE prices_daily (
    symbol    text NOT NULL,
    date      date NOT NULL,
    open      numeric NOT NULL,
    high      numeric NOT NULL,
    low       numeric NOT NULL,
    close     numeric NOT NULL,
    volume    numeric NOT NULL,
    source    text NOT NULL,
    loaded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (symbol, date)
);

-- Missing-day checks look up dates across all symbols.
CREATE INDEX prices_daily_date ON prices_daily (date);

CREATE TABLE price_splits (
    symbol         text NOT NULL,
    -- The first trading day at the new share count.
    execution_date date NOT NULL,
    split_from     numeric NOT NULL CHECK (split_from > 0),
    split_to       numeric NOT NULL CHECK (split_to > 0),
    provider_id    text NOT NULL DEFAULT '',
    source         text NOT NULL,
    loaded_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (symbol, execution_date)
);

CREATE TABLE dividends (
    provider_id      text PRIMARY KEY,
    symbol           text NOT NULL,
    ex_dividend_date date NOT NULL,
    pay_date         date,
    declaration_date date,
    cash_amount      numeric NOT NULL,
    currency         text NOT NULL DEFAULT '',
    -- Payments a year; 0 for a one-time payment.
    frequency        integer NOT NULL DEFAULT 0,
    dividend_type    text NOT NULL DEFAULT '',
    source           text NOT NULL,
    loaded_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dividends_symbol_ex_date ON dividends (symbol, ex_dividend_date);

CREATE AGGREGATE numeric_product (numeric) (
    SFUNC = numeric_mul,
    STYPE = numeric,
    INITCOND = '1'
);

-- Split-adjusted bars. split_factor is the product of the ratios of every
-- split that executed after the bar's date and on or before the symbol's
-- newest bar: prices divide by it, and volume multiplies by it. Until a bar
-- at the new share count exists, every stored bar is at the old one, so an
-- announced split changes nothing yet.
CREATE VIEW prices_adjusted AS
SELECT
    p.symbol,
    p.date,
    p.open / f.factor   AS open,
    p.high / f.factor   AS high,
    p.low / f.factor    AS low,
    p.close / f.factor  AS close,
    p.volume * f.factor AS volume,
    p.close             AS raw_close,
    f.factor            AS split_factor,
    p.source
FROM prices_daily p
CROSS JOIN LATERAL (
    SELECT coalesce(numeric_product(s.split_to / s.split_from), 1) AS factor
    FROM price_splits s
    WHERE s.symbol = p.symbol AND s.execution_date > p.date
      AND s.execution_date <= (SELECT max(n.date) FROM prices_daily n WHERE n.symbol = p.symbol)
) f;

-- +goose Down
DROP VIEW prices_adjusted;
DROP AGGREGATE numeric_product (numeric);
DROP TABLE dividends;
DROP TABLE price_splits;
DROP TABLE prices_daily;
