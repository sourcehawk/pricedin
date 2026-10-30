-- The app role: the Postgres role of `pricedin serve`, the jobs and the
-- scheduler. It reads and writes the tables, but it owns none of them. Only
-- an owner can disable a trigger, so the app cannot turn off the trigger
-- that keeps facts append-only. It can also only add rows to facts.
--
-- Run this script as the role that runs `pricedin migrate`, after the first
-- `pricedin migrate up`. Run it again whenever you like: it changes nothing
-- the second time.
--
--   psql "$OWNER_URL" -f deploy/sql/roles.sql
--   psql "$OWNER_URL" -f deploy/sql/app-password.sql
--
-- The second script sets the password of the role. -v app_role=<name>
-- gives the role another name. Make the role only with this script: a role
-- from the Neon console is a member of neon_superuser, which this script
-- refuses.
--
-- The default privileges below make each table that a later migration
-- creates readable and writable for the app role. A migration that adds an
-- append-only table must revoke UPDATE and DELETE on it, as this script
-- does for facts.

\set ON_ERROR_STOP on
\if :{?app_role}
\else
  \set app_role pricedin_app
\endif

-- psql does not put variables into the body of a DO block, so the block
-- reads the name from a setting of this session.
SELECT set_config('pricedin.app_role', :'app_role', false) AS app_role \gset

DO $$
DECLARE
    app   text := current_setting('pricedin.app_role');
    owner oid;
BEGIN
    IF to_regclass('public.facts') IS NULL THEN
        RAISE EXCEPTION 'the table facts does not exist: run pricedin migrate up as the owner first';
    END IF;
    SELECT relowner INTO owner FROM pg_class WHERE oid = 'public.facts'::regclass;

    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = app) THEN
        EXECUTE format('CREATE ROLE %I LOGIN', app);
    END IF;
    -- A member of the owner role has the owner's rights, and a superuser
    -- or a BYPASSRLS role passes every check.
    IF pg_has_role(app, owner, 'MEMBER') THEN
        RAISE EXCEPTION 'role % owns facts or is a member of its owner; the app role must be another role', app;
    END IF;
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = app AND (rolsuper OR rolbypassrls)) THEN
        RAISE EXCEPTION 'role % is a superuser or bypasses row security; the app role must be a plain role', app;
    END IF;
    -- pg_write_all_data has UPDATE and DELETE on every table, facts too.
    -- neon_superuser is the Neon role that can make roles and grant them.
    IF pg_has_role(app, 'pg_write_all_data', 'MEMBER') THEN
        RAISE EXCEPTION 'role % is a member of pg_write_all_data; the app role must be a plain role', app;
    END IF;
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'neon_superuser') THEN
        IF pg_has_role(app, 'neon_superuser', 'MEMBER') THEN
            RAISE EXCEPTION 'role % is a member of neon_superuser; make the app role with this script, not in the Neon console', app;
        END IF;
    END IF;

    -- TEMPORARY: the batch writes copy their rows into temporary tables.
    EXECUTE format('GRANT CONNECT, TEMPORARY ON DATABASE %I TO %I', current_database(), app);
    EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', app);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %I', app);
    EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %I', app);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %I', app);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %I', app);

    EXECUTE format('REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON facts FROM %I', app);
    -- The migration history belongs to `pricedin migrate`.
    IF to_regclass('public.goose_db_version') IS NOT NULL THEN
        EXECUTE format('REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON goose_db_version FROM %I', app);
    END IF;
END;
$$;
