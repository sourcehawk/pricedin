-- State of the ingestion jobs, and the parts of the derived quarters that
-- the API serves without running the derivation again.

-- +goose Up
ALTER TABLE quarterly_fundamentals
    ADD COLUMN label       text NOT NULL DEFAULT '',
    ADD COLUMN estimate    boolean NOT NULL DEFAULT false,
    -- The date on which the filings first gave the quarter. NULL for rows
    -- built before this column existed.
    ADD COLUMN first_filed date;

-- The reconciliation problems of the derivation, which the data page lists.
-- Rebuilt with quarterly_fundamentals.
CREATE TABLE fundamental_conflicts (
    cik          bigint NOT NULL REFERENCES companies (cik),
    ord          integer NOT NULL,
    kind         text NOT NULL,
    concept      text NOT NULL DEFAULT '',
    period_start date,
    period_end   date,
    original     jsonb,
    restated     jsonb,
    observed     numeric NOT NULL DEFAULT 0,
    expected     numeric NOT NULL DEFAULT 0,
    split        jsonb,
    PRIMARY KEY (cik, ord)
);

-- The trading days whose grouped daily bars were loaded. A day that a
-- per-symbol load filled for one company still lacks the other symbols, so
-- the bars alone cannot tell which days are complete.
CREATE TABLE price_grouped_loads (
    date      date PRIMARY KEY,
    -- The number of bars the provider returned, for all its symbols.
    bars      integer NOT NULL CHECK (bars >= 0),
    loaded_at timestamptz NOT NULL DEFAULT now()
);

-- The symbols whose splits and dividends were fetched from the provider at
-- least once, so that no splits means none, not unknown.
CREATE TABLE price_reference_checks (
    symbol          text PRIMARY KEY,
    first_checked_at timestamptz NOT NULL DEFAULT now()
);

-- Where a job that works through dates continued last time, such as the
-- last daily index that sec-daily read.
CREATE TABLE ingest_cursors (
    job        text PRIMARY KEY,
    position   date NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The companies whose derived quarters must be built again, because their
-- facts or provider splits changed. The write that changes an input marks
-- the company in the same transaction; a rebuild clears the mark only when
-- version is still the one it read, so a change during the rebuild keeps it.
CREATE TABLE fundamentals_pending (
    cik       bigint PRIMARY KEY REFERENCES companies (cik),
    version   bigint NOT NULL DEFAULT 1,
    marked_at timestamptz NOT NULL DEFAULT now()
);

-- The last release of an advisory lock that the processes hand over, such
-- as the SEC lock: the next holder from another process waits until a
-- second after it, so that two rate limiters never overlap.
CREATE TABLE ingest_lock_releases (
    name        text PRIMARY KEY,
    holder      text NOT NULL,
    released_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE ingest_lock_releases;
DROP TABLE fundamentals_pending;
DROP TABLE ingest_cursors;
DROP TABLE price_reference_checks;
DROP TABLE price_grouped_loads;
DROP TABLE fundamental_conflicts;
ALTER TABLE quarterly_fundamentals
    DROP COLUMN first_filed,
    DROP COLUMN estimate,
    DROP COLUMN label;
