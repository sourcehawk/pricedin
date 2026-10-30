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
// at a time. Within a process, the jobs send SEC requests from one
// goroutine at a time.
package ingest
