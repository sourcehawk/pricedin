-- Reported XBRL values, point in time. Rows are never changed or removed:
-- an amended filing adds rows under its own accession number.

-- +goose Up
CREATE TABLE facts (
    id            bigserial PRIMARY KEY,
    cik           bigint NOT NULL REFERENCES companies (cik),
    taxonomy      text NOT NULL,
    concept       text NOT NULL,
    unit          text NOT NULL,
    -- NULL for an instant value, such as a balance.
    period_start  date,
    period_end    date NOT NULL,
    value         numeric NOT NULL,
    form          text NOT NULL,
    fiscal_year   integer NOT NULL,
    fiscal_period text NOT NULL,
    -- The calendar frame SEC had assigned when the row loaded. SEC can move
    -- a frame to a later filing, so it is not part of the identity.
    frame         text NOT NULL DEFAULT '',
    filed         date NOT NULL,
    accession     text NOT NULL,
    loaded_at     timestamptz NOT NULL DEFAULT now()
);

-- The identity of a reported value. SEC lists the same value for the same
-- period more than once in one filing; those are one row. The value is part
-- of the key, so that two different values for one period in one filing
-- both stay.
CREATE UNIQUE INDEX facts_identity ON facts (
    cik, taxonomy, concept, unit,
    coalesce(period_start, '-infinity'::date), period_end,
    accession, value
);

CREATE INDEX facts_cik_concept ON facts (cik, concept, period_end);
-- The latest filing date of a company reads the end of this index.
CREATE INDEX facts_cik_filed ON facts (cik, filed);

-- +goose StatementBegin
CREATE FUNCTION facts_reject_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'facts is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'restrict_violation',
              HINT = 'An amended filing adds new rows.';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER facts_append_only
    BEFORE UPDATE OR DELETE ON facts
    FOR EACH ROW EXECUTE FUNCTION facts_reject_change();

CREATE TRIGGER facts_no_truncate
    BEFORE TRUNCATE ON facts
    FOR EACH STATEMENT EXECUTE FUNCTION facts_reject_change();

-- Splits that SEC data confirms, with the evidence for each.
CREATE TABLE stock_splits (
    cik            bigint NOT NULL REFERENCES companies (cik),
    effective_date date NOT NULL,
    -- New shares for each old share: 10 for a 10-for-1 split.
    ratio          numeric NOT NULL CHECK (ratio > 0),
    source         text NOT NULL,
    evidence       text NOT NULL DEFAULT '',
    PRIMARY KEY (cik, effective_date, source)
);

-- +goose Down
-- The lock holds until the rollback commits, so no fact can arrive between
-- the check and the DROP.
-- +goose StatementBegin
DO $$
BEGIN
    LOCK TABLE facts IN ACCESS EXCLUSIVE MODE;
    IF current_setting('pricedin.allow_drop_facts', true) IS DISTINCT FROM 'on'
       AND EXISTS (SELECT 1 FROM facts) THEN
        RAISE EXCEPTION 'facts holds rows, and this rollback drops them; roll back with --force to drop them anyway';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE stock_splits;
DROP TABLE facts;
DROP FUNCTION facts_reject_change();
