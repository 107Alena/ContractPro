package postgres

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// mockTx — configurable mock that implements pgx.Tx so it can be injected
// into context via injectTx and retrieved by ConnFromCtx.
// Only Exec/Query/QueryRow are functional; all other Tx methods panic.
// ---------------------------------------------------------------------------

type mockTx struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row

	mu    sync.Mutex
	calls []mockCall
}

type mockCall struct {
	Method string
	SQL    string
	Args   []any
}

func (m *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.recordCall("Exec", sql, args)
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag(""), nil
}

func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	m.recordCall("Query", sql, args)
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	m.recordCall("QueryRow", sql, args)
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{err: pgx.ErrNoRows}
}

func (m *mockTx) recordCall(method, sql string, args []any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Method: method, SQL: sql, Args: args})
}

func (m *mockTx) getCalls() []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mockCall(nil), m.calls...)
}

// Tx-only methods — unused but required to satisfy pgx.Tx interface.
func (m *mockTx) Begin(context.Context) (pgx.Tx, error)   { panic("mockTx: Begin not implemented") }
func (m *mockTx) Commit(context.Context) error             { return nil }
func (m *mockTx) Rollback(context.Context) error           { return nil }
func (m *mockTx) Conn() *pgx.Conn                         { return nil }
func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("mockTx: CopyFrom not implemented")
}
func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("mockTx: SendBatch not implemented")
}
func (m *mockTx) LargeObjects() pgx.LargeObjects {
	panic("mockTx: LargeObjects not implemented")
}
func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("mockTx: Prepare not implemented")
}

// Compile-time proof that mockTx satisfies pgx.Tx (and therefore DBTX).
var _ pgx.Tx = (*mockTx)(nil)

// ctxWithMockTx creates a context carrying the mock transaction so that
// ConnFromCtx returns it.
func ctxWithMockTx(mock *mockTx) context.Context {
	return injectTx(context.Background(), mock)
}

// ---------------------------------------------------------------------------
// mockRow — implements pgx.Row.
// ---------------------------------------------------------------------------

type mockRow struct {
	scanFn func(dest ...any) error
	err    error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return r.err
}

// ---------------------------------------------------------------------------
// mockRows — implements pgx.Rows.
// ---------------------------------------------------------------------------

type mockRows struct {
	scanFns []func(dest ...any) error
	idx     int
	errFn   func() error
	closed  bool
}

func (r *mockRows) Next() bool {
	return r.idx < len(r.scanFns)
}

func (r *mockRows) Scan(dest ...any) error {
	if r.idx >= len(r.scanFns) {
		return pgx.ErrNoRows
	}
	fn := r.scanFns[r.idx]
	r.idx++
	return fn(dest...)
}

func (r *mockRows) Close()                                          { r.closed = true }
func (r *mockRows) CommandTag() pgconn.CommandTag                   { return pgconn.NewCommandTag("") }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription    { return nil }
func (r *mockRows) RawValues() [][]byte                             { return nil }
func (r *mockRows) Conn() *pgx.Conn                                { return nil }
func (r *mockRows) Values() ([]any, error)                         { return nil, nil }

func (r *mockRows) Err() error {
	if r.errFn != nil {
		return r.errFn()
	}
	return nil
}
