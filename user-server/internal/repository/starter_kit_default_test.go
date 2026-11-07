package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
)

// AV-04c 批次4 回归（service/auth.go 开户赠送构造点收口）：
//
// auth.go 原 `NewStarterKitRepository(NewTenantRepository().GetDB())` 改为
// NewStarterKitRepositoryDefault()。两者必须同连接：NewTenantRepository 构造即
// 捕获全局连接、GetDB() 原样返回；Default 构造取 repository.GetDB()。测试用
// SetTestDB 换掉全局句柄后，Default 构造必须解析到**当前**全局句柄——种子行
// 能被 Default 构造的仓储读写到，即证明它没有握着别处（或 nil）的连接。

func TestStarterKitRepositoryDefault_ResolvesCurrentGlobalHandle(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SystemConfig{})
	db.SetTestDB(database)

	repo := NewStarterKitRepositoryDefault()
	ctx := tenant.WithSystemScope(context.Background())

	const tenantID = uint(770077)
	cfg := &model.SystemConfig{TenantID: tenantID, Name: "批次4默认构造回归站"}
	require.NoError(t, repo.CreateSiteConfig(ctx, cfg), "Default 构造的仓储应可写当前全局句柄")

	n, err := repo.CountSiteConfig(ctx, tenantID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "同一租户幂等检查应看到刚写入的行")

	// 注入式构造读到的行数必须一致：两条路径同库（连接同源的直接证据）。
	viaWithDB := NewStarterKitRepository(database)
	n2, err := viaWithDB.CountSiteConfig(ctx, tenantID)
	require.NoError(t, err)
	require.EqualValues(t, n, n2)
}
