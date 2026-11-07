package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AV-04c 批次4 回归（ops/service/web_vital.go getdb 收口）：
//
// Report 原 `_db.GetDB().WithContext(ctx).Create(rec)`（service 直取 DB 句柄
// 宽口径），下沉 WebVitalRepository 后连接与写入口径不变——同表
// web_vital_records、同 Create、错误原样透传。两个用例分别钉住：
//  1. 注入仓储路径：行落到注入库（单测注入语义）；
//  2. 默认构造路径：NewWebVitalService() 解析**当前**全局句柄（SetTestDB 换库
//     后仍写进换上的库，证明没有握着装配顺序错误的旧连接）。

func TestWebVitalService_Report_InjectedRepo(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebVitalRecord{})
	svc := NewWebVitalServiceWithRepo(repository.NewWebVitalRepositoryWithDB(database))

	rec := &model.WebVitalRecord{TenantID: 1, Metric: "LCP", Value: 1234.5, Rating: "good", Page: "/dashboard", SessionID: "sess-av04c"}
	require.NoError(t, svc.Report(context.Background(), rec))

	var n int64
	require.NoError(t, database.Model(&model.WebVitalRecord{}).Count(&n).Error)
	assert.EqualValues(t, 1, n)

	var got model.WebVitalRecord
	require.NoError(t, database.First(&got, "session_id = ?", "sess-av04c").Error)
	assert.Equal(t, "LCP", got.Metric)
	assert.InDelta(t, 1234.5, got.Value, 1e-9)
}

func TestWebVitalService_Report_DefaultCtorResolvesCurrentGlobalHandle(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebVitalRecord{})
	db.SetTestDB(database)

	svc := NewWebVitalService()
	rec := &model.WebVitalRecord{TenantID: 1, Metric: "CLS", Value: 0.08, Rating: "good", Page: "/", SessionID: "sess-av04c-def"}
	require.NoError(t, svc.Report(context.Background(), rec))

	var n int64
	require.NoError(t, database.Model(&model.WebVitalRecord{}).Count(&n).Error)
	assert.EqualValues(t, 1, n, "默认构造必须解析到当前全局句柄（SetTestDB 后的库）")
}

// TestWebVitalRecord_NeedsImprovementRatingFitsColumn 观测回归（列宽 32 的来历）：
// rating=needs-improvement（17 字符）必须能整行落库，不因列宽截断 500。
// 下沉仓储时若动了 Create 口径（如包一层裁剪），这里先红。
func TestWebVitalRecord_NeedsImprovementRatingFitsColumn(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebVitalRecord{})
	db.SetTestDB(database)

	svc := NewWebVitalService()
	rec := &model.WebVitalRecord{TenantID: 1, Metric: "FCP", Value: 2100, Rating: "needs-improvement", Page: "/", SessionID: "sess-av04c-rating"}
	require.NoError(t, svc.Report(context.Background(), rec))
}
