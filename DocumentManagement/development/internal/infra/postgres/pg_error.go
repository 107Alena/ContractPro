package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL error codes (from SQLSTATE).
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// isPgUniqueViolation returns true if err wraps a pgconn.PgError with
// SQLSTATE 23505 (unique_violation).
func isPgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// isPgFKViolation returns true if err wraps a pgconn.PgError with
// SQLSTATE 23503 (foreign_key_violation).
func isPgFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation
}

// nullableString converts an empty Go string to nil (SQL NULL).
// Non-empty strings are returned as a pointer.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// fromNullableString converts a nullable string pointer back to a Go string.
// nil → "", otherwise dereferences.
func fromNullableString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
