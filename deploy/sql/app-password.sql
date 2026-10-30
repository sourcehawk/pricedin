-- Sets the password of the app role, which roles.sql creates.
--
--   openssl rand -hex 16
--   psql "$OWNER_URL" -f deploy/sql/app-password.sql
--
-- The first command makes a password of 128 random bits, in characters
-- that need no encoding in a URL. psql asks for it and sends it to the
-- server as plain text, because Neon accepts only plain-text passwords and
-- hashes them itself. -v app_role=<name> names another role, and
-- -v pw=<password> skips the question.

\set ON_ERROR_STOP on
\if :{?app_role}
\else
  \set app_role pricedin_app
\endif
\if :{?pw}
\else
  \prompt 'Password of the app role: ' pw
\endif

ALTER ROLE :"app_role" PASSWORD :'pw';
\unset pw
