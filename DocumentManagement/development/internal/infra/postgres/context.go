package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// DBTX — consumer-side interface for repository methods.
// ---------------------------------------------------------------------------

// DBTX is the common interface satisfied by both *pgxpool.Pool and pgx.Tx.
// Repository methods accept DBTX so they work transparently inside or outside
// a transaction — the caller does not need to know which one is active.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Compile-time proof that both pgxpool.Pool and pgx.Tx satisfy DBTX.
var (
	_ DBTX = (*pgxpool.Pool)(nil)
	_ DBTX = (pgx.Tx)(nil)
)

// ---------------------------------------------------------------------------
// Context keys and helpers.
// ---------------------------------------------------------------------------

// ctxKey is an unexported type used as context-value key to prevent collisions.
type ctxKey int

const (
	// txKey carries the active pgx.Tx inside the context.
	txKey ctxKey = iota

	// poolKey carries the *pgxpool.Pool inside the context.
	poolKey
)

// InjectPool stores the connection pool in the context.
// Call this once at the application wiring layer so that ConnFromCtx
// can fall back to the pool when no transaction is active.
func InjectPool(ctx context.Context, pool *pgxpool.Pool) context.Context {
	return context.WithValue(ctx, poolKey, pool)
}

// injectTx stores a transaction handle in the context.
// Used internally by the Transactor; not exported.
func injectTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey, tx)
}

// extractTx returns the pgx.Tx stored in the context, or nil.
func extractTx(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(txKey).(pgx.Tx)
	return tx
}

// extractPool returns the *pgxpool.Pool stored in the context, or nil.
func extractPool(ctx context.Context) *pgxpool.Pool {
	pool, _ := ctx.Value(poolKey).(*pgxpool.Pool)
	return pool
}

// ConnFromCtx extracts the active database connection from the context.
//
// Resolution order:
//  1. If the context carries a pgx.Tx (set by Transactor.WithTransaction), return it.
//  2. Otherwise return the *pgxpool.Pool (set by InjectPool at startup).
//  3. If neither is present, panic — this is a programming error (InjectPool was
//     not called during application startup).
//
// Repository methods should call this at the top of every public method:
//
//	conn := postgres.ConnFromCtx(ctx)
func ConnFromCtx(ctx context.Context) DBTX {
	if tx := extractTx(ctx); tx != nil {
		return tx
	}
	if pool := extractPool(ctx); pool != nil {
		return pool
	}
	panic("postgres.ConnFromCtx: no pool or transaction in context — did you forget InjectPool at startup?")
}

// HasTx returns true if the context already carries a transaction handle.
// Used by Transactor to implement join semantics for nested WithTransaction calls.
func HasTx(ctx context.Context) bool {
	return extractTx(ctx) != nil
}
