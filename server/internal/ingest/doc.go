// Package ingest loads SEC filings and provider prices into the database:
// the jobs that `pricedin jobs run` starts, the daily chain that `pricedin
// scheduler` runs, and the on-demand load of one company that the API
// calls.
//
// Every job stores raw responses in the blob store before it parses them
// (through Recorder, which the clients call), records one ingest_runs row,
// and is safe to run again: a second run with the same upstream data
// changes nothing.
//
// Concurrency: each job holds a Postgres advisory lock for its name while
// it runs, so two processes never run the same job at once; the second one
// gets ErrAlreadyRunning. SEC allows 10 requests a second for one contact,
// and each process has its own rate limiter, so every SEC request also
// holds the advisory lock "sec-api": the scheduler, a `jobs run` and an
// on-demand load in the API process take turns, one company or one file
// at a time, and a process that takes the lock over from another one starts
// no sooner than a second after that one released it. The download of companyfacts.zip by sec-bulk does not
// hold the lock: it is one long request, and one request beside the others
// at the limit is an accepted excess.
//
// Connections: a process uses its query pool, which `pricedin` sizes to at
// least 8 connections, and a lock pool of at most 12 (four concurrent jobs
// with up to three locks each). The locks hold database sessions, so the
// lock pool needs a direct connection, not a pooler in transaction mode;
// PRICEDIN_DATABASE_LOCK_URL can name one apart from the query pool's.
package ingest
