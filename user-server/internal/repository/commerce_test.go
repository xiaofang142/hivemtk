package repository

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 所有数据入口无租户/无数据库fail-closed，事务回调绝不执行。
func TestCommerceRepository_FailClosed(t *testing.T) {
	r := NewCommerceRepository(nil)
	called := false
	e := r.Transact(context.Background(), CommerceKey{}, func(*CommerceAggregate) error { called = true; return nil })
	if !errors.Is(e, ErrCommerceStore) || called {
		t.Fatal("无租户执行了事务")
	}
	if _, e = r.List(context.Background(), 1, "p", "s", "", 10); !errors.Is(e, ErrCommerceStore) {
		t.Fatal(e)
	}
	if _, e = r.Due(context.Background(), time.Now(), 10); !errors.Is(e, ErrCommerceStore) {
		t.Fatal(e)
	}
}
