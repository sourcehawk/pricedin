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

// locker takes Postgres session advisory locks. A lock holds one pool
// connection until it is released, and the database releases it when that
// connection ends, also when the process dies.
type locker struct {
	pool *pgxpool.Pool
}

// try takes the lock name if it is free. ok is false when another session
// holds it. release must be called when ok is true.
func (l locker) try(ctx context.Context, name string) (release func(), ok bool, err error) {
	return l.take(ctx, name, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`)
}

// wait takes the lock name, and waits while another session holds it, until
// ctx ends.
func (l locker) wait(ctx context.Context, name string) (release func(), err error) {
	release, _, err = l.take(ctx, name, `SELECT true FROM pg_advisory_lock(hashtextextended($1, 0))`)
	return release, err
}

func (l locker) take(ctx context.Context, name, sql string) (func(), bool, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("ingest: lock %s: %w", name, err)
	}
	key := lockPrefix + name
	var ok bool
	if err := conn.QueryRow(ctx, sql, key).Scan(&ok); err != nil {
		// A canceled wait can leave the connection in an unknown state.
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		conn.Release()
		return nil, false, fmt.Errorf("ingest: lock %s: %w", name, err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, key); err != nil {
			// Closing the connection ends the session, which frees the lock.
			_ = conn.Conn().Close(unlockCtx)
		}
		conn.Release()
	}
	return release, true, nil
}
