package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lockPrefix keeps the advisory lock names of the jobs apart from other
// advisory locks in the database.
const lockPrefix = "pricedin-ingest:"

// unlockTimeout limits the unlock after a job, which runs also when the
// job's context ended.
const unlockTimeout = 10 * time.Second

// The waits between two tries of a lock that another session holds.
const (
	firstLockPoll = 100 * time.Millisecond
	maxLockPoll   = 2 * time.Second
)

// handoffGap is the least time between the last request of one process
// under a handed-over lock and the first request of the next process: one
// second, the window of the SEC rate limit.
const handoffGap = time.Second

// locker takes Postgres session advisory locks. A held lock keeps one
// connection of its own pool until it is released, and the database frees
// the lock when that connection ends, also when the process dies. A wait
// holds no connection between its tries.
type locker struct {
	pool *pgxpool.Pool
	// holder names this process in ingest_lock_releases.
	holder string
}

// lock is one held advisory lock.
type lock struct {
	conn *pgxpool.Conn
	name string
}

// try takes the lock name if it is free, and returns nil when another
// session holds it.
func (l locker) try(ctx context.Context, name string) (*lock, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("ingest: lock %s: %w", name, err)
	}
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, lockPrefix+name).Scan(&ok); err != nil {
		conn.Release()
		return nil, fmt.Errorf("ingest: lock %s: %w", name, err)
	}
	if !ok {
		conn.Release()
		return nil, nil
	}
	return &lock{conn: conn, name: name}, nil
}

// wait takes the lock name, and tries again while another session holds
// it, until ctx ends.
func (l locker) wait(ctx context.Context, name string) (*lock, error) {
	pause := firstLockPoll
	for {
		lk, err := l.try(ctx, name)
		if err != nil || lk != nil {
			return lk, err
		}
		if err := sleepFor(ctx, pause); err != nil {
			return nil, fmt.Errorf("ingest: lock %s: %w", name, err)
		}
		pause = min(2*pause, maxLockPoll)
	}
}

// release frees the lock.
func (lk *lock) release(ctx context.Context) {
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	if _, err := lk.conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockPrefix+lk.name); err != nil {
		// Closing the connection ends the session, which frees the lock.
		_ = lk.conn.Conn().Close(unlockCtx)
	}
	lk.conn.Release()
}

// waitForHandoff sleeps until handoffGap after the last release of the lock
// by another process. It returns at once when this process released it
// last, because this process's own rate limiter spaces its requests.
func (l locker) waitForHandoff(ctx context.Context, lk *lock) error {
	var wait float64
	err := lk.conn.QueryRow(ctx, `
		SELECT coalesce(max(extract(epoch FROM released_at + make_interval(secs => $3) - clock_timestamp())), 0)
		FROM ingest_lock_releases WHERE name = $1 AND holder <> $2`,
		lk.name, l.holder, handoffGap.Seconds()).Scan(&wait)
	if err != nil {
		return fmt.Errorf("ingest: lock %s: %w", lk.name, err)
	}
	if wait <= 0 {
		return nil
	}
	return sleepFor(ctx, time.Duration(wait*float64(time.Second)))
}

// releaseHandedOff records the release time for waitForHandoff and frees
// the lock.
func (l locker) releaseHandedOff(ctx context.Context, lk *lock) {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	// A failed record costs only the gap; the unlock still runs.
	_, _ = lk.conn.Exec(recordCtx, `
		INSERT INTO ingest_lock_releases (name, holder, released_at) VALUES ($1, $2, clock_timestamp())
		ON CONFLICT (name) DO UPDATE SET holder = excluded.holder, released_at = excluded.released_at`,
		lk.name, l.holder)
	lk.release(ctx)
}
