package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// RunStatus is the state of an ingest run.
type RunStatus string

// The states of an ingest run.
const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
)

// IngestRun is one run of an ingestion job.
type IngestRun struct {
	ID         int64
	Job        string
	StartedAt  time.Time
	FinishedAt *time.Time // nil while running
	Status     RunStatus
	// Stats holds the counts the job reported, such as rows added.
	Stats map[string]int64
	Error string
}

// StartIngestRun records that job started now and returns the run's ID.
func (s *Store) StartIngestRun(ctx context.Context, job string) (int64, error) {
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO ingest_runs (job) VALUES ($1) RETURNING id`, job).Scan(&id); err != nil {
		return 0, fmt.Errorf("start ingest run of %s: %w", job, err)
	}
	return id, nil
}

// FinishIngestRun records that the run ended now with status, which must be
// RunSucceeded or RunFailed. It returns ErrNotFound when no running run has
// the ID, also when the run already finished.
func (s *Store) FinishIngestRun(ctx context.Context, id int64, status RunStatus, stats map[string]int64, errText string) error {
	if status != RunSucceeded && status != RunFailed {
		return fmt.Errorf("finish ingest run %d: status %q is not a final status", id, status)
	}
	if stats == nil {
		stats = map[string]int64{}
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE ingest_runs SET finished_at = now(), status = $2, stats = $3, error = $4
		WHERE id = $1 AND status = 'running'`, id, string(status), stats, errText)
	if err != nil {
		return fmt.Errorf("finish ingest run %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("finish ingest run %d: no running run", id)
	}
	return nil
}

const ingestRunColumns = `id, job, started_at, finished_at, status, stats, error`

func scanIngestRun(row pgx.Row) (IngestRun, error) {
	var r IngestRun
	var finished pgtype.Timestamptz
	var status string
	err := row.Scan(&r.ID, &r.Job, &r.StartedAt, &finished, &status, &r.Stats, &r.Error)
	r.FinishedAt, r.Status = fromPgTimePtr(finished), RunStatus(status)
	return r, err
}

// GetIngestRun returns the run with the ID, or ErrNotFound.
func (s *Store) GetIngestRun(ctx context.Context, id int64) (IngestRun, error) {
	r, err := scanIngestRun(s.db.QueryRow(ctx, `SELECT `+ingestRunColumns+` FROM ingest_runs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return IngestRun{}, notFound("get ingest run %d", id)
	}
	if err != nil {
		return IngestRun{}, fmt.Errorf("get ingest run %d: %w", id, err)
	}
	return r, nil
}

// ListLatestIngestRuns returns the newest run of each job, ordered by job.
func (s *Store) ListLatestIngestRuns(ctx context.Context) ([]IngestRun, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (job) `+ingestRunColumns+` FROM ingest_runs
		ORDER BY job, started_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list latest ingest runs: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IngestRun, error) { return scanIngestRun(row) })
	if err != nil {
		return nil, fmt.Errorf("list latest ingest runs: %w", err)
	}
	return out, nil
}
