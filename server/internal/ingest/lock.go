package ingest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lockPrefix keeps the advisory lock names of the jobs apart from other
// advisory locks in the database.
const lockPrefix = "pricedin-ingest:"

// unlockTimeout limits the unlock after a job, which runs also when the
// job's context ended.
const unlockTimeout = 10 * time.Second

// The wait between two tries of a lock that another session holds is
// random in this range.
const (
	minLockPoll = 100 * time.Millisecond
	maxLockPoll = 250 * time.Millisecond
)

// handoffGap is the least time between the last request of one process
// under a handed-over lock and the first request of the next process: one
// second, the window of the SEC rate limit.
const handoffGap = time.Second

// yieldPause is how long a holder that released a handed-over lock waits
// before it takes it again when another process waits for it: longer than
// maxLockPoll, so the waiter's next try comes first.
const yieldPause = 2 * maxLockPoll

// ErrSessionRequired means that the lock connection does not keep one
// database session between statements, as a pooler in transaction mode
// does, so advisory locks would leak.
var ErrSessionRequired = errors.New("ingest: the advisory locks need a direct (session) database connection; set PRICEDIN_DATABASE_LOCK_URL to a connection that does not go through a pooler in transaction mode")

// locker takes Postgres session advisory locks. A held lock keeps one
// connection of its own pool until it is released, and the database frees
// the lock when that connection ends, also when the process dies. A wait
// holds no connection between its tries.
type locker struct {
	pool *pgxpool.Pool
	// holder names this process in ingest_lock_releases.
	holder string
	logger *slog.Logger
}

// lock is one held advisory lock.
type lock struct {
	conn *pgxpool.Conn
	name string
}

// checkSession returns ErrSessionRequired when two statements on one
// connection of the lock pool reach different database sessions, or when
// a session lock does not hold from one statement to the next.
func (l locker) checkSession(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("ingest: lock connection: %w", err)
	}
	defer conn.Release()
	var first, second int32
	if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&first); err != nil {
		return fmt.Errorf("ingest: lock connection: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&second); err != nil {
		return fmt.Errorf("ingest: lock connection: %w", err)
	}
	if first != second {
		return ErrSessionRequired
	}
	key := lockPrefix + "session-check:" + l.holder
	var taken, released bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, key).Scan(&taken); err != nil {
		return fmt.Errorf("ingest: lock connection: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, key).Scan(&released); err != nil {
		return fmt.Errorf("ingest: lock connection: %w", err)
	}
	if taken && !released {
		return ErrSessionRequired
	}
	return nil
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
// it, until ctx ends. While it waits, it is listed in ingest_lock_waits.
func (l locker) wait(ctx context.Context, name string) (*lock, error) {
	lk, err := l.try(ctx, name)
	if err != nil || lk != nil {
		return lk, err
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	waiter := hex.EncodeToString(id)
	defer l.forgetWaiter(ctx, name, waiter)
	for {
		if _, err := l.pool.Exec(ctx, `
			INSERT INTO ingest_lock_waits (name, waiter, at) VALUES ($1, $2, clock_timestamp())
			ON CONFLICT (name, waiter) DO UPDATE SET at = excluded.at`, name, waiter); err != nil {
			return nil, fmt.Errorf("ingest: lock %s: %w", name, err)
		}
		pause := minLockPoll + mrand.N(maxLockPoll-minLockPoll+1) //nolint:gosec // jitter needs no cryptographic randomness
		if err := sleepFor(ctx, pause); err != nil {
			return nil, fmt.Errorf("ingest: lock %s: %w", name, err)
		}
		if lk, err := l.try(ctx, name); err != nil || lk != nil {
			return lk, err
		}
	}
}

func (l locker) forgetWaiter(ctx context.Context, name, waiter string) {
	cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	_, _ = l.pool.Exec(cleanCtx, `DELETE FROM ingest_lock_waits WHERE name = $1 AND waiter = $2`, name, waiter)
}

// release frees the lock.
func (l locker) release(ctx context.Context, lk *lock) {
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	var released bool
	err := lk.conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockPrefix+lk.name).Scan(&released)
	switch {
	case err != nil:
		// Closing the connection ends the session, which frees the lock.
		_ = lk.conn.Conn().Close(unlockCtx)
	case !released:
		l.logger.ErrorContext(ctx, "ingest: the lock was not held at its release; the lock connection may go through a pooler in transaction mode",
			"lock", lk.name)
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
// the lock. When another wait for the lock is listed, it pauses before it
// returns.
func (l locker) releaseHandedOff(ctx context.Context, lk *lock) {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	// A failed record costs only the gap; the unlock still runs.
	_, _ = lk.conn.Exec(recordCtx, `
		INSERT INTO ingest_lock_releases (name, holder, released_at) VALUES ($1, $2, clock_timestamp())
		ON CONFLICT (name) DO UPDATE SET holder = excluded.holder, released_at = excluded.released_at`,
		lk.name, l.holder)
	var waiting bool
	_ = lk.conn.QueryRow(recordCtx, `
		SELECT EXISTS (SELECT 1 FROM ingest_lock_waits
		               WHERE name = $1 AND at > clock_timestamp() - make_interval(secs => $2))`,
		lk.name, (2 * maxLockPoll).Seconds()).Scan(&waiting)
	l.release(ctx, lk)
	if waiting {
		_ = sleepFor(ctx, yieldPause)
	}
}
