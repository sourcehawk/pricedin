-- Fiscal quarters derived from facts. This table is rebuilt per company and
-- is not a record: facts stay the source of truth.

-- +goose Up
CREATE TABLE quarterly_fundamentals (
    cik            bigint NOT NULL REFERENCES companies (cik),
    fiscal_year    integer NOT NULL,
    fiscal_quarter smallint NOT NULL CHECK (fiscal_quarter BETWEEN 1 AND 4),
    metric         text NOT NULL,
    value          numeric NOT NULL,
    method         text NOT NULL CHECK (method IN (
        'reported', 'ytd_difference', 'fy_difference', 'weighted_average_q4', 'derived'
    )),
    period_start   date NOT NULL,
    period_end     date NOT NULL,
    -- The latest filing date of the inputs.
    filed          date NOT NULL,
    -- One object per input: accession, form, filed, concept.
    inputs         jsonb NOT NULL DEFAULT '[]',
    built_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cik, fiscal_year, fiscal_quarter, metric)
);

-- +goose Down
DROP TABLE quarterly_fundamentals;
