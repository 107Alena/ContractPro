package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that Transactor satisfies port.Transactor.
var _ port.Transactor = (*Transactor)(nil)

// Transactor implements the unit-of-work pattern over a pgxpool.Pool.
//
// Usage in application services:
//
//	err := transactor.WithTransaction(ctx, func(txCtx context.Context) error {
//	    // All repository calls using txCtx share the same DB transaction.
//	    if err := docRepo.Insert(txCtx, doc); err != nil { return err }
//	    if err := auditRepo.Insert(txCtx, record); err != nil { return err }
//	    return nil
//	})
//
// Nested calls: if ctx already carries a transaction (from an outer
// WithTransaction), the inner call reuses it (join semantics) instead of
// opening a nested transaction. This prevents accidental savepoint complexity
// and keeps the commit/rollback responsibility with the outermost caller.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor creates a Transactor backed by the given pool.
func NewTransactor(pool *pgxpool.Pool) *Transactor {
	return &Transactor{pool: pool}
}

// WithTransaction executes fn inside a database transaction.
//
// Behaviour:
//   - If fn returns nil, the transaction is committed.
//   - If fn returns an error, the transaction is rolled back and the error
//     is returned to the caller.
//   - If Begin, Commit, or Rollback itself fails, a *port.DomainError with
//     code DATABASE_FAILED (retryable) is returned.
//   - If the context already carries a transaction (nested call), fn is
//     executed directly on the existing transaction — no new TX is opened.
//
// The transaction handle is injected into ctx via context value. Repository
// methods retrieve it with ConnFromCtx(ctx).
func (t *Transactor) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	// Join semantics: reuse an existing transaction.
	if HasTx(ctx) {
		return fn(ctx)
	}

	tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return port.NewDatabaseError("begin transaction", err)
	}

	// Safety net: ensures rollback on panic inside fn.
	// After a successful Commit this is a no-op (returns pgx.ErrTxClosed).
	defer tx.Rollback(context.Background()) //nolint:errcheck

	txCtx := injectTx(ctx, tx)

	if fnErr := fn(txCtx); fnErr != nil {
		// Explicit rollback; ignore error — deferred rollback is the backstop,
		// and the caller needs the original business error, not infrastructure noise.
		_ = tx.Rollback(context.Background())
		return fnErr
	}

	if err := tx.Commit(ctx); err != nil {
		return port.NewDatabaseError("commit transaction", err)
	}

	return nil
}
