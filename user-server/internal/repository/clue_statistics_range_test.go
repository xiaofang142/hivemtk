package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 统计接口扩展：验证时间窗过滤生效、六维聚合字段齐全且 truncated=false。
func TestClueRepository_LeadMiningStatsWithRange(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Clue{})
	repo := NewClueRepositoryWithDB(database)
	ctx := context.Background()

	rows := []*model.Clue{
		{SourceID: "sr1", Account: "a1", Type: 8, Name: "in", LeadRole: "demand", Status: "new", SourceChannel: "wechat", IsGroup: true, CreateTime: 1000},
		{SourceID: "sr2", Account: "a2", Type: 8, Name: "in2", LeadRole: "supply", Status: "notified", SourceChannel: "qq", IsGroup: false, CreateTime: 2000},
		{SourceID: "sr3", Account: "a3", Type: 8, Name: "out", LeadRole: "broker", Status: "new", SourceChannel: "qq", IsGroup: true, CreateTime: 9000},
	}
	for _, row := range rows {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("写入夹具失败 %s: %v", row.Name, err)
		}
	}

	stats, err := repo.GetLeadMiningStatsWithRange(ctx, 1000, 2000)
	if err != nil {
		t.Fatalf("时间窗聚合失败: %v", err)
	}
	if got := stats["total"]; got != int64(2) {
		t.Fatalf("时间窗内总数期望 2，实际 %v", got)
	}
	if stats["truncated"] != false {
		t.Fatalf("服务端聚合必须 truncated=false，实际 %v", stats["truncated"])
	}
	for _, key := range []string{"by_type", "by_lead_role", "by_status", "by_channel", "by_conversation"} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("统计缺少维度 %s：%+v", key, stats)
		}
	}
	all, err := repo.GetLeadMiningStats(ctx)
	if err != nil || all["total"] != int64(3) {
		t.Fatalf("全量聚合期望 3 条：%v %+v", err, all)
	}
}

func TestClueRepository_ClueStatisticsWithRange(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Clue{})
	repo := NewClueRepositoryWithDB(database)
	ctx := context.Background()
	for _, row := range []*model.Clue{
		{SourceID: "ts1", Account: "b1", Type: 8, Name: "in", CreateTime: 100},
		{SourceID: "ts2", Account: "b2", Type: 8, Name: "out", CreateTime: 900},
	} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("写入夹具失败: %v", err)
		}
	}
	inWindow, err := repo.GetClueStatisticsWithRange(ctx, 0, 500)
	if err != nil || len(inWindow) != 1 || inWindow[0]["total"] != int64(1) {
		t.Fatalf("时间窗内按类型聚合期望 1 条：%v %+v", err, inWindow)
	}
	all, err := repo.GetClueStatisticsWithRange(ctx, 0, 0)
	if err != nil || len(all) != 1 || all[0]["total"] != int64(2) {
		t.Fatalf("无边界聚合期望 2 条：%v %+v", err, all)
	}
}
