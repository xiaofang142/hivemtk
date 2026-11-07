// custom_report_agg_test.go —— 报表数据源聚合查询下沉仓储后的 SQL 契约回归
// （AV-04c 批次7 刀2）。
//
// ops/service/custom_report.go 的五段聚合 SQL（sessions/messages/clues/users/
// agents）与 dimField→SQL 表达式映射下沉本包 Aggregate*ByDim/AggregateAgentSessions，
// report_filter 的 applyConds 下沉 ApplyConds（谓词组装逐字保留）。本文件锁仓储侧
// 契约：维度表达式映射（含 default 与具名分支）、过滤谓词生效、排序与排除规则，
// 全部与下沉前 service 内联查询逐字段一致。
package repository

import (
	"context"
	"testing"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// ApplyConds：空 conds 原样返回；非空按 AND 合并生效。
func TestApplyConds(t *testing.T) {
	db := testutil.NewTestDB(t, &sysmodel.CustomerSession{})
	ctx := context.Background()

	if err := db.WithContext(ctx).Create(&sysmodel.CustomerSession{SessionID: "agg-c1", UserID: "u1", Status: sysmodel.SessionStatusPending}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.WithContext(ctx).Create(&sysmodel.CustomerSession{SessionID: "agg-c2", UserID: "u2", Status: sysmodel.SessionStatusClosed}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 空 conds：不过滤，两行都计入。
	var all []CustomReportSessionAggRow
	q := ApplyConds(db.WithContext(ctx).Table("customer_sessions").
		Select("status::text AS dim_value, COUNT(*) AS session_count, 0 AS message_sum").
		Group("status"), nil, nil)
	if err := q.Scan(&all).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("空 conds 应不过滤（期望 2 组），got %d", len(all))
	}

	// 单条 conds：只命中 pending。
	var filtered []CustomReportSessionAggRow
	q = ApplyConds(db.WithContext(ctx).Table("customer_sessions").
		Select("status::text AS dim_value, COUNT(*) AS session_count, 0 AS message_sum").
		Group("status"), []string{"status = ?"}, []any{"pending"})
	if err := q.Scan(&filtered).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(filtered) != 1 || filtered[0].DimValue != "pending" || filtered[0].SessionCount != 1 {
		t.Fatalf("过滤后应只剩 pending×1，got %+v", filtered)
	}

	// 多条 conds：AND 合并（两条互斥条件 ⇒ 空集）。
	var none []CustomReportSessionAggRow
	q = ApplyConds(db.WithContext(ctx).Table("customer_sessions").
		Select("status::text AS dim_value, COUNT(*) AS session_count, 0 AS message_sum").
		Group("status"),
		[]string{"status = ?", "status = ?"}, []any{"pending", "closed"})
	if err := q.Scan(&none).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("AND 合并的互斥条件应为空集，got %+v", none)
	}
}

// AggregateSessionsByDim：default 分支落 status 列；platform 分支不静默落到 status
// （曾把平台分布画成会话状态分布的判例）；agent_name 分支空名归「未分配」。
func TestAggregateSessionsByDim(t *testing.T) {
	db := testutil.NewTestDB(t, &sysmodel.CustomerSession{})
	ctx := context.Background()
	repo := NewCustomReportRepositoryWithDB(db)

	seeds := []*sysmodel.CustomerSession{
		{SessionID: "agg-s1", UserID: "u1", Status: sysmodel.SessionStatusPending, MessageCount: 2, Platform: sysmodel.PlatformWeChat},
		{SessionID: "agg-s2", UserID: "u2", Status: sysmodel.SessionStatusPending, MessageCount: 3, Platform: sysmodel.PlatformWeChat},
		{SessionID: "agg-s3", UserID: "u3", Status: sysmodel.SessionStatusClosed, MessageCount: 5, Platform: sysmodel.PlatformDouyin},
	}
	for _, s := range seeds {
		if err := db.WithContext(ctx).Create(s).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// default（dimField 未知值）：按 status 分组，消息数求和。
	rows, err := repo.AggregateSessionsByDim(ctx, "unknown_dim", nil, nil)
	if err != nil {
		t.Fatalf("AggregateSessionsByDim: %v", err)
	}
	byStatus := map[string]CustomReportSessionAggRow{}
	for _, r := range rows {
		byStatus[r.DimValue] = r
	}
	if got := byStatus["pending"]; got.SessionCount != 2 || got.MessageSum != 5 {
		t.Fatalf("pending 组期望 会话2/消息5，got %+v", got)
	}
	if got := byStatus["closed"]; got.SessionCount != 1 || got.MessageSum != 5 {
		t.Fatalf("closed 组期望 会话1/消息5，got %+v", got)
	}

	// platform 分支：按平台分组而非 status。
	rows, err = repo.AggregateSessionsByDim(ctx, "platform", nil, nil)
	if err != nil {
		t.Fatalf("AggregateSessionsByDim(platform): %v", err)
	}
	byPlatform := map[string]int64{}
	for _, r := range rows {
		byPlatform[r.DimValue] = r.SessionCount
	}
	if byPlatform["wechat"] != 2 || byPlatform["douyin"] != 1 {
		t.Fatalf("platform 分组期望 wechat×2/douyin×1，got %+v", byPlatform)
	}

	// agent_name 分支：AgentName 为空的行归「未分配」。
	if err := db.WithContext(ctx).Create(&sysmodel.CustomerSession{SessionID: "agg-s4", UserID: "u4", Status: sysmodel.SessionStatusPending, MessageCount: 1}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err = repo.AggregateSessionsByDim(ctx, "agent_name", nil, nil)
	if err != nil {
		t.Fatalf("AggregateSessionsByDim(agent_name): %v", err)
	}
	found := false
	for _, r := range rows {
		if r.DimValue == "未分配" {
			found = true
			if r.SessionCount != 4 {
				t.Fatalf("未分配组期望 4 行（四条种子均空坐席名），got %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("agent_name 分支应把空名归入「未分配」")
	}
}

// AggregateMessagesByDim：default 分支按 COALESCE(content_type,'unknown') 分组；
// platform 分支按平台分组。
func TestAggregateMessagesByDim(t *testing.T) {
	db := testutil.NewTestDB(t, &sysmodel.UnifiedMessage{})
	ctx := context.Background()
	repo := NewCustomReportRepositoryWithDB(db)

	seeds := []*sysmodel.UnifiedMessage{
		{MessageID: "agg-m1", ContentType: "text"},
		{MessageID: "agg-m2", ContentType: "text"},
		{MessageID: "agg-m3", ContentType: "image"},
		{MessageID: "agg-m4", Platform: sysmodel.PlatformDouyin},
	}
	for _, m := range seeds {
		if err := db.WithContext(ctx).Create(m).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// NULL content_type 的行走 COALESCE 兜底（struct Create 会把零值写成 ''，
	// 覆盖不到 NULL 分支，须显式造 NULL 行）。
	if err := db.WithContext(ctx).Model(&sysmodel.UnifiedMessage{}).Create(map[string]any{"message_id": "agg-m5", "content_type": nil, "platform": sysmodel.PlatformWeChat}).Error; err != nil {
		t.Fatalf("seed null content_type: %v", err)
	}

	rows, err := repo.AggregateMessagesByDim(ctx, "", nil, nil)
	if err != nil {
		t.Fatalf("AggregateMessagesByDim: %v", err)
	}
	got := map[string]int64{}
	for _, r := range rows {
		got[r.DimValue] = r.Count
	}
	if got["text"] != 2 || got["image"] != 1 || got["unknown"] != 1 {
		t.Fatalf("content_type 分组期望 text×2/image×1/unknown(NULL 兜底)×1，got %+v", got)
	}

	rows, err = repo.AggregateMessagesByDim(ctx, "platform", nil, nil)
	if err != nil {
		t.Fatalf("AggregateMessagesByDim(platform): %v", err)
	}
	got = map[string]int64{}
	for _, r := range rows {
		got[r.DimValue] += r.Count
	}
	// struct Create 把零值 platform 写成 ''（列 NOT NULL），只有显式 NULL 才落 unknown。
	if got[""] != 3 || got["douyin"] != 1 || got["wechat"] != 1 {
		t.Fatalf("platform 分组期望 \"\u00d73/douyin\u00d71/wechat\u00d71，got %+v", got)
	}
}

// AggregateCluesByDim：default 分支按 type 分组；level 分支空值归 warm。
func TestAggregateCluesByDim(t *testing.T) {
	db := testutil.NewTestDB(t, &sysmodel.Clue{})
	ctx := context.Background()
	repo := NewCustomReportRepositoryWithDB(db)

	seeds := []*sysmodel.Clue{
		{SourceID: "s1", Account: "a1", Type: 1, Name: "n1"},
		{SourceID: "s2", Account: "a2", Type: 1, Name: "n2"},
		{SourceID: "s3", Account: "a3", Type: 2, Name: "n3"},
	}
	for _, c := range seeds {
		if err := db.WithContext(ctx).Create(c).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rows, err := repo.AggregateCluesByDim(ctx, "type", nil, nil)
	if err != nil {
		t.Fatalf("AggregateCluesByDim: %v", err)
	}
	got := map[string]int64{}
	for _, r := range rows {
		got[r.DimValue] = r.Count
	}
	if got["1"] != 2 || got["2"] != 1 {
		t.Fatalf("type 分组期望 1×2/2×1，got %+v", got)
	}

	if err := db.WithContext(ctx).Create(&sysmodel.Clue{SourceID: "s4", Account: "a4", Type: 1, Name: "n4", Level: "hot"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err = repo.AggregateCluesByDim(ctx, "level", nil, nil)
	if err != nil {
		t.Fatalf("AggregateCluesByDim(level): %v", err)
	}
	got = map[string]int64{}
	for _, r := range rows {
		got[r.DimValue] = r.Count
	}
	if got["warm"] != 3 || got["hot"] != 1 {
		t.Fatalf("level 分组期望 warm×3（空 level 兜底）/hot×1，got %+v", got)
	}
}

// AggregateUsersByDim：default 与 date 分支按 DATE(created_at) 分组；churn_risk
// 分支按客户风险层分组。
func TestAggregateUsersByDim(t *testing.T) {
	db := testutil.NewTestDB(t, &sysmodel.Customer{})
	ctx := context.Background()
	repo := NewCustomReportRepositoryWithDB(db)

	for _, risk := range []string{"low", "low", "high"} {
		if err := db.WithContext(ctx).Create(&sysmodel.Customer{ChurnRisk: risk}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rows, err := repo.AggregateUsersByDim(ctx, "churn_risk", nil, nil)
	if err != nil {
		t.Fatalf("AggregateUsersByDim: %v", err)
	}
	got := map[string]int64{}
	for _, r := range rows {
		got[r.DimValue] = r.Count
	}
	if got["low"] != 2 || got["high"] != 1 {
		t.Fatalf("churn_risk 分组期望 low×2/high×1，got %+v", got)
	}

	rows, err = repo.AggregateUsersByDim(ctx, "created_at", nil, nil)
	if err != nil {
		t.Fatalf("AggregateUsersByDim(created_at): %v", err)
	}
	if len(rows) != 1 || rows[0].Count != 3 {
		t.Fatalf("date 分组期望同日 3 行聚一组，got %+v", rows)
	}
}

// AggregateAgentSessions：排除空/NULL agent_name，按 session_count DESC 排序，
// 平均响应时长 COALESCE 0。
func TestAggregateAgentSessions(t *testing.T) {
	db := testutil.NewTestDB(t, &sysmodel.CustomerSession{})
	ctx := context.Background()
	repo := NewCustomReportRepositoryWithDB(db)

	seeds := []*sysmodel.CustomerSession{
		{SessionID: "agg-a1", UserID: "u1", AgentName: "甲", AvgResponseTime: 10},
		{SessionID: "agg-a2", UserID: "u2", AgentName: "甲", AvgResponseTime: 30},
		{SessionID: "agg-a3", UserID: "u3", AgentName: "乙", AvgResponseTime: 5},
		{SessionID: "agg-a4", UserID: "u4"}, // 空名：排除
	}
	for _, s := range seeds {
		if err := db.WithContext(ctx).Create(s).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rows, err := repo.AggregateAgentSessions(ctx)
	if err != nil {
		t.Fatalf("AggregateAgentSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("空名坐席应被排除（期望 2 组），got %+v", rows)
	}
	if rows[0].AgentName != "甲" || rows[0].SessionCount != 2 || rows[0].AvgResponseTime != 20 {
		t.Fatalf("首位期望 甲×2/均值20，got %+v", rows[0])
	}
	if rows[1].AgentName != "乙" || rows[1].SessionCount != 1 || rows[1].AvgResponseTime != 5 {
		t.Fatalf("次位期望 乙×1/均值5，got %+v", rows[1])
	}
}
