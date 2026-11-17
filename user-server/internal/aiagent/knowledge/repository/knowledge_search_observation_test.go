package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestKnowledgeSearchObservation_RequiresAuthoritativeTenantAndTiming(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeSearchLog{})
	repo := NewKnowledgeSearchLogRepository(database)
	started := time.Now()
	completed := started.Add(time.Millisecond)
	valid := model.KnowledgeSearchLog{
		TenantID: 18, ProductID: "cb35812c-cd41-4e23-b4a8-1d28929291b7", Query: "不可写入的合成客户正文",
		StartedAt: &started, CompletedAt: &completed, DurationMeasured: true, TimingVersion: 1, LatencyMs: 1,
	}
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		mutate func(*model.KnowledgeSearchLog)
	}{
		{name: "缺少上下文"},
		{name: "不同租户", ctx: tenant.OnlyTenant(context.Background(), 19)},
		{name: "缺少测量", ctx: tenant.OnlyTenant(context.Background(), 18), mutate: func(row *model.KnowledgeSearchLog) { row.DurationMeasured = false }},
		{name: "反向时间", ctx: tenant.OnlyTenant(context.Background(), 18), mutate: func(row *model.KnowledgeSearchLog) { row.CompletedAt = &started; row.StartedAt = &completed }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := valid
			if tc.mutate != nil {
				tc.mutate(&row)
			}
			if err := repo.CreateObservation(tc.ctx, &row); err == nil {
				t.Fatal("归属或测量不明时不得留下伪观测")
			}
		})
	}
	if err := repo.CreateObservation(tenant.OnlyTenant(context.Background(), 18), &valid); err != nil {
		t.Fatal(err)
	}
	var rows []model.KnowledgeSearchLog
	if err := database.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TenantID != 18 || rows[0].ProductID != valid.ProductID || rows[0].Query != "" {
		t.Fatal("只有有效元数据可写入，客户正文必须移除")
	}
	if valid.Query == "" {
		t.Fatal("仓储不得修改调用方载荷")
	}
}
