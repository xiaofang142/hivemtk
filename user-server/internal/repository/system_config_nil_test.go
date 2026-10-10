package repository

import (
	"context"
	"errors"
	_db "hivemtk-user/internal/pkg/db"
	"testing"
)

// 没有配置行（gorm.ErrRecordNotFound）与"库根本没指对"（句柄是 nil）是两回事：
// 前者是业务状态，调用方按默认值兜底；后者是装配问题，运维调什么都不生效。
// 合成一种返回的话，"改了上传上限没反应"会被显示成"没配置过"，永远查不到根因。

func TestSystemConfigRepoNilHandleIsErrorNotPanic(t *testing.T) {
	repo := &systemConfigRepo{}

	_, err := repo.GetConfig(context.Background())
	if !errors.Is(err, ErrSystemConfigStoreUnavailable) {
		t.Fatalf("want ErrSystemConfigStoreUnavailable, got %v", err)
	}
}

func TestSystemConfigRepoNilReceiverIsErrorNotPanic(t *testing.T) {
	var repo *systemConfigRepo

	_, err := repo.GetConfig(context.Background())
	if !errors.Is(err, ErrSystemConfigStoreUnavailable) {
		t.Fatalf("want ErrSystemConfigStoreUnavailable, got %v", err)
	}
}

// 走真实构造路径：全局句柄为 nil 时，NewSystemConfigRepository().GetConfig 必须报哨兵错。
func TestSystemConfigRepoNilGlobalHandleIsErrorNotPanic(t *testing.T) {
	_db.SetTestDB(nil)
	t.Cleanup(func() { _db.SetTestDB(nil) })

	repo := NewSystemConfigRepository()
	if _, err := repo.GetConfig(context.Background()); !errors.Is(err, ErrSystemConfigStoreUnavailable) {
		t.Fatalf("want ErrSystemConfigStoreUnavailable, got %v", err)
	}
}
