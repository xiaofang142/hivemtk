package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"

	"github.com/stretchr/testify/require"
)

func resetIdentityMaxAttemptsProvider() {
	identityMaxAttemptsProvider = func() int { return DefaultIdentityMaxAttempts }
}

func TestIdentityMaxAttemptsSeam(t *testing.T) {
	resetIdentityMaxAttemptsProvider()
	t.Cleanup(resetIdentityMaxAttemptsProvider)

	t.Run("未注入时返回兜底值 8", func(t *testing.T) {
		require.Equal(t, 8, identityMaxAttempts())
	})

	t.Run("注入后生效", func(t *testing.T) {
		SetIdentityMaxAttemptsProvider(func() int { return 3 })
		require.Equal(t, 3, identityMaxAttempts())
	})

	t.Run("注入非法值回落兜底（0 次重试等于一次都不试）", func(t *testing.T) {
		SetIdentityMaxAttemptsProvider(func() int { return 0 })
		require.Equal(t, DefaultIdentityMaxAttempts, identityMaxAttempts())
	})

	t.Run("setter 传 nil 是空操作而非复位", func(t *testing.T) {
		SetIdentityMaxAttemptsProvider(func() int { return 3 })
		SetIdentityMaxAttemptsProvider(nil)
		require.Equal(t, 3, identityMaxAttempts(),
			"传 nil 应该什么都不做（防装配顺序错时把兜底值顶掉）")
	})
}

// failingIdentityRepo 复用同包 fakeCustRepo 已实现的整个 CustomerRepository 接口，
// 只把 FindByIdentity 换成「永远报错 + 计数」，用来观察重试次数。
type failingIdentityRepo struct {
	*fakeCustRepo
	calls int
}

func newFailingIdentityRepo() *failingIdentityRepo {
	return &failingIdentityRepo{fakeCustRepo: newFakeCustRepo()}
}

func (f *failingIdentityRepo) FindByIdentity(context.Context, string, string, string, string, string) (*model.Customer, error) {
	f.calls++
	return nil, errors.New("模拟仓储冲突")
}

// TestFindExistingWithRetryHonorsAttemptLimit 行为级断言：
// 仓储每次查找都报错时，重试次数由参数决定 —— 配 2 就只试 2 次，配 5 就试 5 次。
// 这条钉的是「读取点真的走了 seam」，比只测 identityMaxAttempts() 本身强。
func TestFindExistingWithRetryHonorsAttemptLimit(t *testing.T) {
	resetIdentityMaxAttemptsProvider()
	t.Cleanup(resetIdentityMaxAttemptsProvider)

	repo := newFailingIdentityRepo()
	svc := &CustomerIdentityService{repo: repo}
	ids := identity.Identifiers{Phone: "13800000000"}

	SetIdentityMaxAttemptsProvider(func() int { return 2 })
	_, err := svc.findExistingWithRetry(t.Context(), ids)
	require.Error(t, err)
	require.Equal(t, 2, repo.calls, "配 2 次上限时只应重试 2 次")

	repo.calls = 0
	SetIdentityMaxAttemptsProvider(func() int { return 5 })
	_, err = svc.findExistingWithRetry(t.Context(), ids)
	require.Error(t, err)
	require.Equal(t, 5, repo.calls, "配 5 次上限时应重试 5 次")
}
