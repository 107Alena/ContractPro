package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestIsPgUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unique violation",
			err:  &pgconn.PgError{Code: "23505"},
			want: true,
		},
		{
			name: "FK violation",
			err:  &pgconn.PgError{Code: "23503"},
			want: false,
		},
		{
			name: "wrapped unique violation",
			err:  errors.Join(errors.New("wrap"), &pgconn.PgError{Code: "23505"}),
			want: true,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("some error"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPgUniqueViolation(tt.err))
		})
	}
}

func TestIsPgFKViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "FK violation",
			err:  &pgconn.PgError{Code: "23503"},
			want: true,
		},
		{
			name: "unique violation",
			err:  &pgconn.PgError{Code: "23505"},
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("some error"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPgFKViolation(tt.err))
		})
	}
}


func TestNullableString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		isNil bool
		value string
	}{
		{name: "empty string", input: "", isNil: true},
		{name: "non-empty string", input: "hello", isNil: false, value: "hello"},
		{name: "whitespace only", input: " ", isNil: false, value: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nullableString(tt.input)
			if tt.isNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.value, *result)
			}
		})
	}
}

func TestFromNullableString(t *testing.T) {
	tests := []struct {
		name  string
		input *string
		want  string
	}{
		{name: "nil pointer", input: nil, want: ""},
		{name: "non-nil pointer", input: strPtr("hello"), want: "hello"},
		{name: "empty string pointer", input: strPtr(""), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fromNullableString(tt.input))
		})
	}
}

func strPtr(s string) *string {
	return &s
}
