# Runbook

This page tells you how to deploy PricedIn the first time and how to operate it after that. It assumes the hosting of `docs/architecture.md`: the server on Fly.io, the web app on Vercel, Postgres on Neon, and raw files in a Tigris bucket. Any Postgres 18 and any S3-compatible store work too. Only the names of their consoles differ.

In the commands, `pricedin-api` is the name of the Fly app, and `pricedin.vercel.app` is the domain of the web app. Replace both with your own names.

## Settings and secrets

The server reads its settings from environment variables. `deploy/fly/server.toml` sets those that are not secret. Set the others as Fly secrets (`flyctl secrets set NAME=value --app pricedin-api`):

| Secret | Value |
| --- | --- |
| `PRICEDIN_DATABASE_URL` | The app role's URL. A pooler host is permitted here |
| `PRICEDIN_DATABASE_LOCK_URL` | The app role's URL on the direct host, never a pooler |
| `PRICEDIN_OWNER_EMAIL` | Your login email |
| `PRICEDIN_OWNER_PASSWORD_HASH` | The output of `pricedin auth hash-password` |
| `PRICEDIN_PUBLIC_ORIGIN` | The origin of the web app, such as `https://pricedin.vercel.app` |
| `PRICEDIN_PROXY_SECRET` | The output of `openssl rand -hex 32`. Set the same value on the Vercel project (step 6). The web app sends it with the browser's address, and the server uses that address only when the value matches |
| `PRICEDIN_SEC_USER_AGENT` | A name and your contact email, such as `PricedIn you@example.com` |
| `PRICEDIN_MASSIVE_API_KEY` | Your Massive API key |
| `PRICEDIN_BLOB_S3_BUCKET` | The name of the Tigris bucket |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | The keys of the bucket. `flyctl storage create` sets them |

The owner role's URL is never a Fly secret. The Fly app holds only the app role's credentials, so a fault in the server, or a person with access to a machine, cannot change the schema or disable the trigger of `facts`. Only `pricedin migrate` uses the owner's URL, from your machine or from the deploy workflow.

The deploy workflow (`.github/workflows/deploy.yml`) needs these GitHub repository secrets. A job whose secrets are missing deploys nothing and passes:

| Secret | Value |
| --- | --- |
| `FLY_API_TOKEN` | A deploy token of the Fly app |
| `PRICEDIN_DATABASE_MIGRATE_URL` | The owner role's URL on the direct host. The server job runs `pricedin migrate up` with it before each deploy |
| `VERCEL_TOKEN` | A Vercel access token |
| `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID` | The IDs of the Vercel team and project |

If the Fly app has another name than the one in `server.toml`, set it as the repository variable `FLY_APP`.

## First deploy

Do these steps in this sequence. Run the commands from the root of the repository. You need `flyctl`, `psql`, `openssl`, Docker, and Node.js with pnpm on your machine. The Vercel CLI is a devDependency of the repository: `pnpm install` installs the version of the lockfile, and `pnpm exec vercel` runs it. Log in to Fly.io with `flyctl auth login`, and to Vercel with `pnpm exec vercel login`.

Steps 1 to 8 give a working login with data. Steps 9 and 10 are for scripts and for deploys from GitHub.

Several steps read the owner's URL from the shell variable `OWNER_URL`. A shell variable lives only in its own shell. In each new terminal, set it again with `read -rs OWNER_URL && export OWNER_URL`, then paste the URL.

### 1. Make the accounts

1. Make an account on Fly.io, Vercel, Neon and Massive.
2. In Massive, subscribe to the Stocks Starter plan and get an API key.
3. Build the server image on your machine. Some steps below run it:

   ```sh
   docker build --tag pricedin-server:dev server
   ```

   Build it again after each `git pull`, so that the migrations of the image match the code that you deploy.

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

4. Make the app role:

   ```sh
   psql "$OWNER_URL" -f deploy/sql/roles.sql
   ```

   Make the app role only with this script. A role that you make in the Neon console is a member of `neon_superuser`, and the script refuses it.

5. Make a password for the app role, and set it. The first command prints 32 hexadecimal characters (128 random bits), which need no encoding in a URL. The script asks for the password: paste it.

   ```sh
   openssl rand -hex 16
   psql "$OWNER_URL" -f deploy/sql/app-password.sql
   ```

   Neon accepts only passwords in plain text, and hashes them itself. Thus do not use psql's `\password` command on Neon: it sends a hash. On a Postgres that you run yourself, `\password pricedin_app` works too.

6. Write the app role's URL: the owner URL with `pricedin_app` and its password in place of the owner's. Make two copies:
   - `PRICEDIN_DATABASE_LOCK_URL` on the direct host.
   - `PRICEDIN_DATABASE_URL` on the direct host, or on the `-pooler` host. One server needs no pooler.

The app role can read and write the tables, but it cannot change the schema or the rows of `facts`. Never give `serve` or the jobs the owner URL. `roles.sql` refuses an app role that is the owner, a member of the owner, a superuser, or a member of `pg_write_all_data` or `neon_superuser`.

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
     PRICEDIN_OWNER_EMAIL='you@example.com' \
     PRICEDIN_OWNER_PASSWORD_HASH='$argon2id$...' \
     PRICEDIN_PUBLIC_ORIGIN='https://pricedin.vercel.app' \
     PRICEDIN_PROXY_SECRET='<output of openssl rand -hex 32>' \
     PRICEDIN_SEC_USER_AGENT='PricedIn you@example.com' \
     PRICEDIN_MASSIVE_API_KEY='...' \
     PRICEDIN_BLOB_S3_BUCKET='<BUCKET_NAME from step 3>'
   ```

   `--stage` stores the secrets for the next deploy. Do not paste secrets into a shared terminal log.

### 5. Deploy the server

Before each deploy, apply the migrations of the version that you deploy, as the owner. Then deploy:

```sh
docker run --rm -e PRICEDIN_DATABASE_MIGRATE_URL="$OWNER_URL" pricedin-server:dev migrate up
flyctl deploy server --config "$PWD/deploy/fly/server.toml" --ha=false --app pricedin-api
```

The deploy workflow (step 10) runs the same two commands. `server` is the build context of the image. flyctl changes into that folder before it reads `--config`, so the config path must be absolute. Fly starts one machine for `serve` (512 MB) and one for `scheduler` (1 GB). Make sure that the API answers:

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
   pnpm exec vercel link
   ```

2. In the project settings on vercel.com, set these values:
   - Root Directory: `web`
   - Framework Preset: Next.js
   - Node.js Version: the major version of `nodejs` in `.tool-versions`, or the newest that Vercel offers
   - Environment variable `PRICEDIN_API_URL` = `https://pricedin-api.fly.dev`, for Production. The proxy of the web app (`web/src/proxy.ts`) reads it for each request, not at build time. Vercel gives a changed value to the next deployment.
   - Environment variable `PRICEDIN_PROXY_SECRET` = the value of the Fly secret from step 4, for Production, of the type Sensitive. Do not give it the prefix `NEXT_PUBLIC_`: a variable with that prefix goes into the browser's code.

3. Deploy:

   ```sh
   pnpm exec vercel deploy --prod
   ```

`web/vercel.json` turns off the deployments from Git pushes, so that only you or the workflow deploy. The browser calls `/api/*` on the Vercel domain, and the proxy of the web app rewrites those requests to Fly. The session cookie thus belongs to the Vercel domain, and it is `Secure`.

If the domain differs from `PRICEDIN_PUBLIC_ORIGIN`, set the secret again. A wrong origin makes each login answer 403 `forbidden-origin`.

### 7. Log in and confirm the client address

1. Open the web app. It shows the login page (`/login`). Log in with `PRICEDIN_OWNER_EMAIL` and your password.
2. Find the address that the server recorded for your session:

   ```sh
   psql "$OWNER_URL" -c 'SELECT ip, created_at FROM sessions ORDER BY created_at DESC LIMIT 1'
   ```

3. Make sure that `ip` is your own public address.

The login rate limit counts attempts per address. A login through the web app counts against the address that Vercel's edge gives to the web app. The web app sends that address with `PRICEDIN_PROXY_SECRET`. If `ip` is an address of Vercel, the server did not accept the secret. Then make sure that the Fly secret and the Vercel variable hold the same value, and deploy the web app again.

The Fly hostname also accepts requests that do not come through Vercel. Such a request counts against its own address from `Fly-Client-IP`. A copy of `X-Forwarded-For` or `X-PricedIn-Client-IP` that it sends has no effect without the secret.

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

To load one company only, use `flyctl ssh console --app pricedin-api -C '/pricedin ingest company GRMN'`. In the web app, pick the company in the stock search and choose "Load this company", or use "Load or refresh" on the data page.

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
3. Read the team and project IDs in `.vercel/project.json` (`orgId` and `projectId`), which `pnpm exec vercel link` wrote.
4. Add the four values as repository secrets on GitHub: `FLY_API_TOKEN`, `VERCEL_TOKEN`, `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID`.
5. Add the owner role's URL (`$OWNER_URL`) as the repository secret `PRICEDIN_DATABASE_MIGRATE_URL`.

Then start a deploy from the Actions tab (workflow "Deploy"), or with `gh workflow run deploy.yml -f target=all`. The `target` input is `all`, `server` or `web`. The server job builds the server image, runs `migrate up` with it as the owner, and then deploys to Fly. It needs `FLY_API_TOKEN` and `PRICEDIN_DATABASE_MIGRATE_URL`. The web job needs the three Vercel secrets. A job that skips its deploy shows the notice "No server deploy" or "No web deploy" with the names of the missing secrets.

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

- the data page of the web app (`/data`), or `GET /api/v1/status`: the newest run of each job, the newest close, and the number of loaded companies. The data page also loads or refreshes one company ("Load or refresh");
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

The job `reprocess` loads these files into the database again. It uses the parsers and the store methods of the other jobs, and it sends no request to SEC or Massive. Then it rebuilds the quarters of the companies whose data changed. Use it after a fix to a parser or to the normalization of the data:

1. Deploy the server with the fix.
2. Run the job on the scheduler machine. Its `companyfacts.zip` files are more than 1 GB each, and the job copies each one to a temporary file:

   ```sh
   flyctl ssh console --app pricedin-api --machine <id> -C '/pricedin jobs run --wait reprocess'
   ```

Put the options of the job after `reprocess`:

| Option | Effect |
| --- | --- |
| `--prefix sec`, `--prefix prices` | Reads only the SEC files, or only the Massive files. |
| `--since YYYY-MM-DD` | Reads only the files retrieved on or after this day. |
| `--cik N` | Reads only the files of one company: its SEC files, its entries in the archives, and the files of the symbols that the database maps to it. It does not read the ticker maps, and it marks no grouped day as loaded. |

How the job loads the files:

- It reads every version of each file, oldest retrieval day first. On each day, it loads the files in the order of the jobs: the SEC ticker map, submissions, company facts, the archives, then the Massive ticker list, splits and dividends, grouped daily bars and the bars of one symbol.
- Facts and filings are never changed or removed, so each version can add rows. For the other data, such as company details, splits, dividends and bars, the job writes each value one time, in its newest version. Thus a second run writes nothing.
- It skips a version with the same content as the version of the retrieval day before. A ticker map is the exception: the last map of such a run gives the day of the sync.
- It loads a ticker map only from the day of the last sync of its source. The dated symbol map cannot go back in time. To load the older maps again, reprocess into an empty database.
- If another job stores a newer response while `reprocess` runs, `reprocess` loads that response again after the older ones. The newest value stays in the database.
- It keeps the grouped daily bars of the covered symbols, and of each symbol that has splits, dividends or bars of its own in the blob store.
- It counts a response as `incomplete` and skips it when a part is not stored: a page, an extra submissions file, or the dividends that the job stores with the splits. It also skips a Massive ticker list of more than one page when a page has two versions on one day, because the pages of the two lists cannot be paired.
- The daily filing indexes, the split-adjusted bars and the ticker details hold no data to load.

Like the other jobs, `reprocess` records an `ingest_runs` row with its counts, and it is safe to run again. If a file fails, the job loads the other files, and the run ends as failed with the first errors.

After a change to the derivation rules only, `fundamentals-rebuild all` is enough. It reads only the stored facts and provider splits.

To rebuild a database at another site, copy the raw files out of the bucket:

```sh
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \
  aws s3 sync s3://<bucket> ./pricedin-raw --endpoint-url https://fly.storage.tigris.dev
```

Then set `PRICEDIN_BLOB_BACKEND=fs` and `PRICEDIN_BLOB_DIR=./pricedin-raw`, apply the migrations to the empty database, and run `reprocess`. The blob store does not record which companies were loaded on demand. To keep them current, load each one again with `ingest company <ticker>`. Then run `daily`.

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

### Proxy secret

1. Make a new value with `openssl rand -hex 32`.
2. Set it on Fly: `flyctl secrets set --app pricedin-api PRICEDIN_PROXY_SECRET='...'`.
3. Set the same value in the Vercel project, and deploy the web app again.

Between step 2 and step 3, the server does not accept the old value. Then each login through the web app counts against the address of Vercel, and logins can get 429 `rate-limited` for a short time.

### Database passwords

- App role: make a new password with `openssl rand -hex 16`, and set it with `psql "$OWNER_URL" -f deploy/sql/app-password.sql`. The server cannot connect until the next step is done, so do it at once. Then set `PRICEDIN_DATABASE_URL` and `PRICEDIN_DATABASE_LOCK_URL` again, in one `flyctl secrets set` command.
- Owner role: reset its password in the Neon console. Then update the GitHub secret `PRICEDIN_DATABASE_MIGRATE_URL`, and your own copy of the URL.

### Bucket keys, deploy tokens

- Tigris: make new keys in the Tigris dashboard (`flyctl storage dashboard`), set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` again, then delete the old keys.
- Fly deploy token: make a new one with `flyctl tokens create deploy`, update `FLY_API_TOKEN` on GitHub, then revoke the old one (`flyctl tokens list`, `flyctl tokens revoke`).
- Vercel token: make a new one on vercel.com, update `VERCEL_TOKEN` on GitHub, then delete the old one.

## Restore the database

A restore brings back the database as it was at an earlier point, with its schema, its role passwords, its sessions and its API tokens. Thus the steps below also bring the schema up to date, set the app role's password again, and end the sessions and the tokens that the restore can bring back.

Neon keeps the history of each branch for a time that depends on the plan. To go back to an earlier point:

1. Stop both machines, so that no request and no job uses the database during the restore:

   ```sh
   flyctl scale count app=0 scheduler=0 --app pricedin-api
   ```

2. In the Neon console, restore the branch to a time before the fault. Or make a new branch from that time, if you want to keep the current state. A restore in place keeps the host. A new branch has a new host: set `OWNER_URL` to the owner URL of the new branch, so that the next steps change the new branch.
3. Apply the migrations that came after the restored point, with the image of the version that runs now:

   ```sh
   docker run --rm -e PRICEDIN_DATABASE_MIGRATE_URL="$OWNER_URL" pricedin-server:dev migrate up
   ```

4. Run `psql "$OWNER_URL" -f deploy/sql/roles.sql`. It changes nothing when the grants are already there.
5. Set the app role's password again, because the restore can bring back an older one. Use the password of the current `PRICEDIN_DATABASE_URL`, or make a new one with `openssl rand -hex 16`:

   ```sh
   psql "$OWNER_URL" -f deploy/sql/app-password.sql
   ```

6. End every session, because the restore can bring back sessions that ended after the restored point:

   ```sh
   psql "$OWNER_URL" -c 'DELETE FROM sessions'
   ```

7. If the host or the app role's password changed, set `PRICEDIN_DATABASE_URL` and `PRICEDIN_DATABASE_LOCK_URL` again with `--stage`, and update the GitHub secret `PRICEDIN_DATABASE_MIGRATE_URL`.
8. Start the machines again with `flyctl scale count app=1 scheduler=1 --app pricedin-api`.
9. Revoke again each API token that you revoked after the restored point. `/pricedin auth token list` shows each token as active or revoked. If you do not know which tokens to revoke, revoke all of them and make new ones.
10. Run `flyctl ssh console --app pricedin-api -C '/pricedin jobs run --wait daily'`. The jobs reload what the restore removed: the daily cursors are from the restored point, and each job adds only missing rows.

From another Postgres, use a dump. Make it as the owner, and restore it as the owner into an empty database. Stop both machines first (step 1 above):

```sh
pg_dump "$OWNER_URL" --format=custom --file=pricedin.dump
pg_restore --dbname="$NEW_OWNER_URL" --no-owner --no-acl pricedin.dump
docker run --rm -e PRICEDIN_DATABASE_MIGRATE_URL="$NEW_OWNER_URL" pricedin-server:dev migrate up
psql "$NEW_OWNER_URL" -f deploy/sql/roles.sql
psql "$NEW_OWNER_URL" -f deploy/sql/app-password.sql
psql "$NEW_OWNER_URL" -c 'DELETE FROM sessions'
```

`pg_restore` makes the tables and adds the rows. `--no-owner` makes the role of `$NEW_OWNER_URL` the owner of each table, so use the owner role there, never the app role. `--no-acl` leaves out the grants of the dump, because they name `pricedin_app`, which a new server does not have yet. `roles.sql` then makes the role and its grants, and `app-password.sql` gives the role a password: a dump holds no passwords. The trigger of `facts` rejects `UPDATE`, `DELETE` and `TRUNCATE`, but not the rows that the restore adds.

Then do steps 7 to 10 above with the URLs of the new server.

## Update the deploy tools

- The Vercel CLI is a devDependency in `package.json`. Renovate proposes each new version, and the lockfile pins it.
- Renovate does not update flyctl, because the workflow pins it with a checksum. To update it, pick a release on https://github.com/superfly/flyctl/releases that is at least two weeks old. Copy the line for `flyctl_<version>_Linux_x86_64.tar.gz` from its file `flyctl_<version>_checksums.txt`. Then set `FLYCTL_VERSION` and `FLYCTL_SHA256` in `.github/workflows/deploy.yml` in one commit.

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
- The whole range of the request is older than five years. The plan gives no older data. (A range that only starts before the window returns the bars inside it.) `PRICEDIN_PRICES_BACKFILL_DAYS` (default 1260) keeps the backfill inside the window.
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
