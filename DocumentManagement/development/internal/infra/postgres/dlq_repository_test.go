package postgres

import (
	"context"
	"testing"

	"contractpro/document-management/internal/domain/port"
)

func TestDLQRepository_InterfaceCompliance(t *testing.T) {
	var _ port.DLQRepository = (*DLQRepository)(nil)
}

func TestNewDLQRepository(t *testing.T) {
	r := NewDLQRepository()
	if r == nil {
		t.Fatal("expected non-nil DLQRepository")
	}
}

func TestDLQRepository_Insert_NilContext(t *testing.T) {
	r := NewDLQRepository()
	// ConnFromCtx panics on nil context — this verifies repository doesn't
	// silently swallow the call. In production, ctx always carries a pool.
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from ConnFromCtx with no pool")
		}
	}()
	_ = r.Insert(context.Background(), nil)
}

func TestDLQRepository_FindByFilter_NilContext(t *testing.T) {
	r := NewDLQRepository()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from ConnFromCtx with no pool")
		}
	}()
	_, _ = r.FindByFilter(context.Background(), port.DLQFilterParams{})
}

func TestDLQRepository_IncrementReplayCount_NilContext(t *testing.T) {
	r := NewDLQRepository()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from ConnFromCtx with no pool")
		}
	}()
	_ = r.IncrementReplayCount(context.Background(), "some-id")
}
