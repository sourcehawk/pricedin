# Runbook

This page tells you how to deploy PricedIn the first time and how to operate it after that. It assumes the hosting of `docs/architecture.md`: the server on Fly.io, the web app on Vercel, Postgres on Neon, and raw files in a Tigris bucket. Any Postgres 18 and any S3-compatible store work too. Only the names of their consoles differ.

In the commands, `pricedin-api` is the name of the Fly app, and `pricedin.vercel.app` is the domain of the web app. Replace both with your own names.

## Settings and secrets

The server reads its settings from environment variables. `deploy/fly/server.toml` sets those that are not secret. Set the others as Fly secrets (`flyctl secrets set NAME=value --app pricedin-api`):

| Secret | Value |
| --- | --- |
| `PRICEDIN_DATABASE_URL` | The app role's URL. A pooler host is permitted here |
| `PRICEDIN_DATABASE_LOCK_URL` | The app role's URL on the direct host, never a pooler |
| `PRICEDIN_DATABASE_MIGRATE_URL` | The owner role's URL on the direct host |
| `PRICEDIN_OWNER_EMAIL` | Your login email |
| `PRICEDIN_OWNER_PASSWORD_HASH` | The output of `pricedin auth hash-password` |
| `PRICEDIN_PUBLIC_ORIGIN` | The origin of the web app, such as `https://pricedin.vercel.app` |
| `PRICEDIN_SEC_USER_AGENT` | A name and your contact email, such as `PricedIn you@example.com` |
| `PRICEDIN_MASSIVE_API_KEY` | Your Massive API key |
| `PRICEDIN_BLOB_S3_BUCKET` | The name of the Tigris bucket |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | The keys of the bucket. `flyctl storage create` sets them |

The deploy workflow (`.github/workflows/deploy.yml`) needs these GitHub repository secrets. A job whose secrets are missing deploys nothing and passes:

| Secret | Value |
| --- | --- |
| `FLY_API_TOKEN` | A deploy token of the Fly app |
| `VERCEL_TOKEN` | A Vercel access token |
| `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID` | The IDs of the Vercel team and project |

If the Fly app has another name than the one in `server.toml`, set it as the repository variable `FLY_APP`.

## First deploy

Do these steps in this sequence. Run the commands from the root of the repository. You need `flyctl`, the Vercel CLI (`npx vercel`), `psql` and Docker on your machine. Log in to Fly.io with `flyctl auth login`, and to Vercel with `npx vercel login`.

Steps 1 to 8 give a working login with data. Steps 9 and 10 are for scripts and for deploys from GitHub.

### 1. Make the accounts

1. Make an account on Fly.io, Vercel, Neon and Massive.
2. In Massive, subscribe to the Stocks Starter plan and get an API key.
3. Build the server image on your machine. Some steps below run it:

   ```sh
   make images
   ```

### 2. Make the database

1. In Neon, make a project with Postgres 18. Choose a region near the Fly region in `server.toml` (`primary_region`).
2. Get the connection string of the owner role (in Neon, `neondb_owner`). Use the direct host, without `-pooler` in its name. The migrations and the advisory locks need a session that stays open, and a pooler in transaction mode does not keep one. Keep the string in a shell variable for the next commands:

   ```sh
   read -rs OWNER_URL && export OWNER_URL
   ```
3. Make the tables as the owner:

   ```sh
   docker run --rm -e PRICEDIN_DATABASE_MIGRATE_URL="$OWNER_URL" pricedin-server:dev migrate up
   ```

4. Make the app role and set its password. psql asks for the password, and sends only its hash:

   ```sh
   psql "$OWNER_URL" -f deploy/sql/roles.sql
   psql "$OWNER_URL" -c '\password pricedin_app'
   ```

5. Write the app role's URL: the owner URL with `pricedin_app` and its password in place of the owner's. Make two copies:
   - `PRICEDIN_DATABASE_LOCK_URL` on the direct host.
   - `PRICEDIN_DATABASE_URL` on the direct host, or on the `-pooler` host. One server needs no pooler.

The app role can read and write the tables, but it cannot change the schema or the rows of `facts`. Never give `serve` or the jobs the owner URL. `roles.sql` refuses an app role that is the owner, a member of the owner, or a superuser.

### 3. Make the Fly app and the bucket

```sh
flyctl apps create pricedin-api
flyctl storage create --app pricedin-api
```

`flyctl storage create` makes a Tigris bucket and sets `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `BUCKET_NAME` as secrets of the app. The server reads the two keys. It needs the bucket name in its own variable (next step).

### 4. Set the secrets

1. Make the hash of your password. The command asks for the password two times:

   ```sh
   docker run --rm -it pricedin-server:dev auth hash-password
   ```

2. Set the secrets. Put each value in single quotes, because the hash holds `$` characters:

   ```sh
   flyctl secrets set --app pricedin-api --stage \
     PRICEDIN_DATABASE_URL='postgres://pricedin_app:...' \
     PRICEDIN_DATABASE_LOCK_URL='postgres://pricedin_app:...' \
     PRICEDIN_DATABASE_MIGRATE_URL='postgres://neondb_owner:...' \
     PRICEDIN_OWNER_EMAIL='you@example.com' \
     PRICEDIN_OWNER_PASSWORD_HASH='$argon2id$...' \
     PRICEDIN_PUBLIC_ORIGIN='https://pricedin.vercel.app' \
     PRICEDIN_SEC_USER_AGENT='PricedIn you@example.com' \
     PRICEDIN_MASSIVE_API_KEY='...' \
     PRICEDIN_BLOB_S3_BUCKET='<BUCKET_NAME from step 3>'
   ```

   `--stage` stores the secrets for the next deploy. Do not paste secrets into a shared terminal log.

### 5. Deploy the server

```sh
flyctl deploy server --config "$PWD/deploy/fly/server.toml" --ha=false --app pricedin-api
```

`server` is the build context of the image. flyctl changes into that folder before it reads `--config`, so the config path must be absolute. The release command applies any new migration as the owner. Then Fly starts one machine for `serve` (512 MB) and one for `scheduler` (1 GB). Make sure that the API answers:

```sh
curl https://pricedin-api.fly.dev/healthz
curl https://pricedin-api.fly.dev/readyz
```

Both must answer `{"status":"ok"}`. If `/readyz` answers 503, the server cannot reach the database: read `flyctl logs --app pricedin-api`.

When the scheduler starts, it runs the `daily` job at once if no daily run succeeded in the last day. On a new database, that is the case. Step 8 waits for that run. Before the first import, the run has little to do.

Make sure that the Massive key works:

```sh
flyctl ssh console --app pricedin-api -C '/pricedin prices verify'
```

It checks a known split, a list of dividends and a CIK, and prints only dates, ratios and results.

### 6. Deploy the web app

1. Make a Vercel project and link this repository to it:

   ```sh
   npx vercel link
   ```

2. In the project settings on vercel.com, set these values:
   - Root Directory: `web`
   - Framework Preset: Next.js
   - Node.js Version: the major version of `nodejs` in `.tool-versions`, or the newest that Vercel offers
   - Environment variable `PRICEDIN_API_URL` = `https://pricedin-api.fly.dev`, for Production. The build reads it.

3. Deploy:

   ```sh
   npx vercel deploy --prod
   ```

`web/vercel.json` turns off the deployments from Git pushes, so that only you or the workflow deploy. The browser calls `/api/*` on the Vercel domain, and the web app sends those requests on to Fly. The session cookie thus belongs to the Vercel domain, and it is `Secure`.

If the domain differs from `PRICEDIN_PUBLIC_ORIGIN`, set the secret again. A wrong origin makes each login answer 403 `forbidden-origin`.

### 7. Log in and confirm the client address

1. Open the web app and log in.
2. Find the address that the server recorded for your session:

   ```sh
   psql "$OWNER_URL" -c 'SELECT ip, created_at FROM sessions ORDER BY created_at DESC LIMIT 1'
   ```

3. Make sure that `ip` is your own public address.

The login rate limit counts attempts per address. Vercel writes the client address into `X-Forwarded-For`, and the Fly proxy adds the address of Vercel after it. Thus `server.toml` takes the second entry from the right (`PRICEDIN_TRUSTED_PROXY_HOPS = "2"`). If `ip` is an address of Vercel, the count is wrong: fix it in `server.toml` and deploy again.

The Fly hostname also accepts requests that do not come through Vercel. Such a request can choose the address that the rate limit sees. A long password protects the login against that. The Argon2id hash makes each guess slow.

### 8. Load the data

The first import loads every NYSE and Nasdaq company. To start smaller, set a list of tickers first, such as `flyctl secrets set --app pricedin-api PRICEDIN_UNIVERSE=NVDA,KO,GRMN`. Run the import on the scheduler machine (1 GB), not on the API machine (512 MB):

1. Find the ID of the scheduler machine. It is the row whose process group is `scheduler`:

   ```sh
   flyctl machines list --app pricedin-api
   ```

2. Give it more memory for the import. The `sec-bulk` job reads an archive of more than 1 GB:

   ```sh
   flyctl scale memory 2048 --process-group scheduler --app pricedin-api
   ```

3. Run the jobs in this sequence. If the scheduler runs the same job at that moment, for example as a step of its first `daily` run, `--wait` waits for that run to end, and then runs the job:

   ```sh
   flyctl ssh console --app pricedin-api --machine <id> -C '/pricedin jobs run --wait sec-tickers'
   flyctl ssh console --app pricedin-api --machine <id> -C '/pricedin jobs run --wait sec-bulk'
   flyctl ssh console --app pricedin-api --machine <id> -C '/pricedin jobs run --wait price-reference'
   flyctl ssh console --app pricedin-api --machine <id> -C '/pricedin jobs run --wait prices-backfill'
   flyctl ssh console --app pricedin-api --machine <id> -C '/pricedin jobs run --wait fundamentals-rebuild all'
   ```

4. Set the memory back:

   ```sh
   flyctl scale memory 1024 --process-group scheduler --app pricedin-api
   ```

Durations, estimated from the rate limits for about 6,000 companies:

| Job | What limits it | Expected duration |
| --- | --- | --- |
| `sec-tickers` | One request | Seconds |
| `sec-bulk` | The download of `companyfacts.zip`, then one submissions request per company at 9 a second | 15 to 30 minutes |
| `price-reference` | Two requests per symbol (splits and dividends) at 20 a second | About 10 minutes |
| `prices-backfill` | One request per trading day for five years (about 1,260) at 20 a second, and the database writes | 5 to 20 minutes |
| `fundamentals-rebuild all` | The derivation of each company | Several minutes |

If an SSH session ends, run the same command again. Each job continues where the database shows that it stopped, and adds nothing twice.

The full universe needs several gigabytes of database storage. Make sure that the database plan has that much.

To load one company only, use `flyctl ssh console --app pricedin-api -C '/pricedin ingest company GRMN'`, or open the company in the web app and load it there.

### 9. Make an API token

```sh
flyctl ssh console --app pricedin-api -C '/pricedin auth token create laptop'
```

The command shows the token one time. Keep it in a password manager. `docs/api.md` shows how to use it.

### 10. Set up the deploy workflow

1. Make a deploy token of the Fly app:

   ```sh
   flyctl tokens create deploy --app pricedin-api
   ```

2. Make an access token on vercel.com (Account Settings, Tokens).
3. Read the team and project IDs in `.vercel/project.json` (`orgId` and `projectId`), which `npx vercel link` wrote.
4. Add the four values as repository secrets on GitHub: `FLY_API_TOKEN`, `VERCEL_TOKEN`, `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID`.

Then start a deploy from the Actions tab (workflow "Deploy"), or with `gh workflow run deploy.yml -f target=all`. The `target` input is `all`, `server` or `web`. The server job needs `FLY_API_TOKEN`, and the web job needs the three Vercel secrets. A job that skips its deploy shows the notice "No server deploy" or "No web deploy" with the names of the missing secrets.

## Daily operation

The scheduler runs the `daily` job each day at 01:30 UTC (`PRICEDIN_SCHEDULE_DAILY`). That time is after the US close in summer and in winter. The job runs these steps:

1. `sec-tickers`: loads the SEC ticker map.
2. `sec-daily`: reads the daily filing indexes since the last run, loads the covered companies that filed, and tries the earlier failures again.
3. `price-reference`: loads the splits and dividends of the covered symbols.
4. `prices-daily`: loads the bars of each trading day since the newest loaded one.
5. `fundamentals-rebuild pending`: derives the quarters again for each company whose facts or splits changed.

If the scheduler was down, it runs the job when it starts again, because no daily run succeeded in the last day. The job fills every day that it missed. A step that fails does not stop the steps after it, and the run is recorded as failed.

`flyctl ssh console` without `--machine` connects to one of the app's machines. Both machines have the same image and secrets, so short commands work on either one.

To see the state of the data:

- the data page of the web app, or `GET /api/v1/status`: the newest run of each job, the newest close, and the number of loaded companies;
- `flyctl ssh console --app pricedin-api -C '/pricedin jobs list'`: each job with its latest run;
- `flyctl logs --app pricedin-api`: the logs of both machines, in JSON;
- the metrics on port 9090 of the API machine, which Fly collects (Fly dashboard, Metrics).

## Run a job again

Every job is safe to run again:

```sh
flyctl ssh console --app pricedin-api -C '/pricedin jobs run --wait <job> [args]'
```

`flyctl ssh console --app pricedin-api -C '/pricedin jobs list'` lists the jobs and their arguments. Without `--wait`, a job that another process runs now is skipped, and the command exits with success. To load one company again, use `ingest company <ticker>`.

If a process died during a run, the run stays `running` until the next run of the same lock starts. That run marks it as failed with the error `abandoned`.

## Reprocess from the blob store

The blob store keeps every raw response under a key with its retrieval date and a hash of its body:

| Key | Content |
| --- | --- |
| `sec/tickers/...`, `sec/submissions/...`, `sec/companyfacts/...`, `sec/daily-index/...` | SEC responses |
| `sec/archives/companyfacts.zip/...` | The bulk archives of `sec-bulk` |
| `prices/massive/grouped/...`, `aggs/...`, `splits/...`, `dividends/...`, `tickers/...` | Massive responses, page by page |

What you can reprocess now:

- **Quarters.** After a change to the derivation rules, deploy the new server, then run `fundamentals-rebuild all`. It reads only the stored facts and provider splits, and makes no request to SEC or Massive.
- **Everything from the sources.** Load the data again with the jobs of step 8. The jobs add only what is new, so a run over a full database changes nothing.

v1 has no command that reads the raw files back into the database. The parsers of `server/internal/sec` and `server/internal/prices/massive` read stored bodies, so such a command can use them later. Until then, copy the raw files out of the bucket, for example to rebuild a database at another site:

```sh
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \
  aws s3 sync s3://<bucket> ./pricedin-raw --endpoint-url https://fly.storage.tigris.dev
```

## Rotate secrets

Rotate a secret when it leaks, and when a person who knew it leaves. After each `flyctl secrets set` without `--stage`, Fly restarts the machines with the new value.

### Owner password

1. Make a new hash with `docker run --rm -it pricedin-server:dev auth hash-password`.
2. Set it:

   ```sh
   flyctl secrets set --app pricedin-api PRICEDIN_OWNER_PASSWORD_HASH='$argon2id$...'
   ```

Each session remembers the owner email and password hash that started it. Thus a new password or a new email ends every session that started before the change.

### Sessions

PricedIn has no session signing key. A session is a random token, and the database keeps its hash. To end every session at once, for example after you lose a device:

```sh
flyctl ssh console --app pricedin-api -C '/pricedin auth sessions revoke-all'
```

This also ends your own session, so log in again after it. API tokens stay valid.

### API tokens

1. Make the new token: `flyctl ssh console --app pricedin-api -C '/pricedin auth token create <new-name>'`.
2. Put it into the scripts that use the old one.
3. Revoke the old token: `flyctl ssh console --app pricedin-api -C '/pricedin auth token revoke <old-name>'`.

`/pricedin auth token list` shows each token with its last use.

### Massive API key

1. Make a new key in the Massive dashboard.
2. Set it: `flyctl secrets set --app pricedin-api PRICEDIN_MASSIVE_API_KEY='...'`.
3. Delete the old key in the Massive dashboard.

### Database passwords

- App role: run `psql "$OWNER_URL" -c '\password pricedin_app'`. The server cannot connect until the next step is done, so do it at once. Then set `PRICEDIN_DATABASE_URL` and `PRICEDIN_DATABASE_LOCK_URL` again, in one `flyctl secrets set` command.
- Owner role: reset its password in the Neon console. Then set `PRICEDIN_DATABASE_MIGRATE_URL` again.

### Bucket keys, deploy tokens

- Tigris: make new keys in the Tigris dashboard (`flyctl storage dashboard`), set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` again, then delete the old keys.
- Fly deploy token: make a new one with `flyctl tokens create deploy`, update `FLY_API_TOKEN` on GitHub, then revoke the old one (`flyctl tokens list`, `flyctl tokens revoke`).
- Vercel token: make a new one on vercel.com, update `VERCEL_TOKEN` on GitHub, then delete the old one.

## Restore the database

A restore brings back the database as it was at an earlier point, with its schema, its sessions and its API tokens. Thus the steps below also bring the schema up to date, and end the sessions and the tokens that the restore can bring back.

Neon keeps the history of each branch for a time that depends on the plan. To go back to an earlier point:

1. Stop both machines, so that no request and no job uses the database during the restore:

   ```sh
   flyctl scale count app=0 scheduler=0 --app pricedin-api
   ```

2. In the Neon console, restore the branch to a time before the fault. Or make a new branch from that time, if you want to keep the current state. A new branch has a new host: set the three database URLs again, with `--stage`. A restore in place keeps the host.
3. Apply the migrations that came after the restored point, with the image of the version that runs now:

   ```sh
   docker run --rm -e PRICEDIN_DATABASE_MIGRATE_URL="$OWNER_URL" pricedin-server:dev migrate up
   ```

   A change of secrets does not run the release command, so this step is necessary.
4. Run `psql "$OWNER_URL" -f deploy/sql/roles.sql`. It changes nothing when the grants are already there.
5. Start the machines again with `flyctl scale count app=1 scheduler=1 --app pricedin-api`.
6. End every session, because the restore can bring back sessions that ended after the restored point:

   ```sh
   flyctl ssh console --app pricedin-api -C '/pricedin auth sessions revoke-all'
   ```

7. Revoke again each API token that you revoked after the restored point. `/pricedin auth token list` shows each token as active or revoked. If you do not know which tokens to revoke, revoke all of them and make new ones.
8. Run `flyctl ssh console --app pricedin-api -C '/pricedin jobs run --wait daily'`. The jobs reload what the restore removed: the daily cursors are from the restored point, and each job adds only missing rows.

From another Postgres, use a dump. Make it as the owner, and restore it as the owner into an empty database. Stop both machines first (step 1 above):

```sh
pg_dump "$OWNER_URL" --format=custom --file=pricedin.dump
pg_restore --dbname="$NEW_OWNER_URL" --no-owner --no-acl pricedin.dump
docker run --rm -e PRICEDIN_DATABASE_MIGRATE_URL="$NEW_OWNER_URL" pricedin-server:dev migrate up
psql "$NEW_OWNER_URL" -f deploy/sql/roles.sql
psql "$NEW_OWNER_URL" -c '\password pricedin_app'
```

`pg_restore` makes the tables and adds the rows. `--no-owner` makes the role of `$NEW_OWNER_URL` the owner of each table, so use the owner role there, never the app role. `--no-acl` leaves out the grants of the dump, because they name `pricedin_app`, which a new server does not have yet. `roles.sql` then makes the role and its grants, and `\password` gives the role a password: a dump holds no passwords. The trigger of `facts` rejects `UPDATE`, `DELETE` and `TRUNCATE`, but not the rows that the restore adds.

Then set the three database URLs to the new server, start the machines, and do steps 6 to 8 above.

## Troubleshooting

### SEC answers 403

SEC refuses requests without a declared `User-Agent`, and it blocks a client that sends more than 10 requests a second for about ten minutes.

1. Make sure that `PRICEDIN_SEC_USER_AGENT` holds a name and a real contact email address.
2. Make sure that no other program sends SEC requests from the same address.
3. Wait ten minutes, then run the job again.

A 403 for a daily index of a weekend or a holiday is normal. The client treats it as "no index for that day".

### Massive answers NOT_AUTHORIZED

Massive gives this answer for four causes:

- The key is wrong or the plan ended. Make sure that the subscription is active, and set the key again.
- The request is for data older than five years. The plan gives no older data. `PRICEDIN_PRICES_BACKFILL_DAYS` (default 1260) keeps the backfill inside the window.
- The request is for a day that is not over. PricedIn asks for a day only after 20:00 New York time, so this answer points to a wrong clock or a manual request.
- The plan does not include an endpoint that PricedIn uses. `docs/data-sources.md` lists the endpoints. `/pricedin prices verify` calls the splits, dividends, bars and tickers endpoints, and shows which check fails.

### Prices are stale

The newest close on the page is older than the last trading day:

1. Make sure that the scheduler machine runs: `flyctl status --app pricedin-api`. If it is stopped, start it with `flyctl machine start <id> --app pricedin-api`.
2. Look at the latest `daily` and `prices-daily` runs with `/pricedin jobs list`, or on the data page.
3. If a run failed, read its error, then run `prices-daily` again.

Before 20:00 New York time, the newest close is of the trading day before. That is correct.

### A failed ingest run on the data page

1. Read the error text of the run on the data page, or with `/pricedin jobs list`.
2. Read the logs of that time: `flyctl logs --app pricedin-api`. Each log line names its job.
3. Correct the cause (a secret, a quota, a source that was down).
4. Run the job again, as "Run a job again" says.

`sec-daily` tries each company that failed again, with a growing wait, up to a limit of attempts. The data page lists the companies that reached the limit. Load each of them with `ingest company <ticker>`.

### The blob store fails

Each job writes the raw response to the bucket before it stores a value. If the bucket cannot be reached, every job fails, and its error names the blob store. Make sure that `PRICEDIN_BLOB_S3_BUCKET`, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are set and that the keys are valid (`flyctl storage list`). Then run the jobs again.

### The jobs refuse to start: lock connection

The jobs check at startup that `PRICEDIN_DATABASE_LOCK_URL` keeps its session. Behind a pooler in transaction mode, such as a Neon `-pooler` host, they refuse to start. Set the lock URL to the direct host.

### Logins answer 403 or 429

- 403 `forbidden-origin`: the `Origin` of the browser is not in `PRICEDIN_PUBLIC_ORIGIN`. Set the origin of the web app, with `https://` and without a slash at the end.
- 429 `rate-limited`: more than 5 attempts a minute from one address. Wait for the time in the `Retry-After` header. If every login shares one limit, the hop count is wrong (step 7).
- 503 `login-disabled`: the owner email or the password hash is not set.
