// custom_report_sink_test.go —— 报表聚合下沉后 service→仓储读写契约回归
// （AV-04c 批次7 刀2）。
//
// custom_report.go 五段数据源聚合下沉 opsrepo.CustomReportRepository、applyConds
// 下沉 opsrepo.ApplyConds 后，本文件锁 service 侧路径：BuildReportFilterSQL 解析的
// 过滤谓词在仓储聚合中仍然生效（status 过滤只命中目标行）、users/agents 分支可用、
// 仓储注入构造与查询结果等价。
package service

import (
	"context"
	"testing"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	sysrepo "hivemtk-user/internal/repository"
)

// 过滤谓词经下沉后的 ApplyConds 生效：sessions 报表带 status 过滤只统计目标行。
func TestQueryReportData_Sessions_FilterStillApplied(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	svc := setupCustomReportService(t, db)
	ctx := context.Background()

	if err := db.Create(&sysmodel.CustomerSession{SessionID: "sink-s1", UserID: "u1", Status: sysmodel.SessionStatusPending, MessageCount: 2}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&sysmodel.CustomerSession{SessionID: "sink-s2", UserID: "u2", Status: sysmodel.SessionStatusClosed, MessageCount: 9}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	report := &model.CustomReport{
		Name:       "过滤会话报表",
		DataSource: "sessions",
		Dimensions: `[{"field":"status","label":"状态"}]`,
		Metrics:    `[{"field":"message_count","label":"消息数"}]`,
		Filters:    `[{"field":"status","operator":"eq","value":"pending"}]`,
		ChartType:  "table",
		IsPublic:   true,
	}
	data, err := svc.QueryReportData(ctx, report, nil)
	if err != nil {
		t.Fatalf("QueryReportData: %v", err)
	}
	if data.Total != 1 || len(data.Data) != 1 {
		t.Fatalf("status=pending 过滤后应只剩 1 组，got total=%d rows=%v", data.Total, data.Data)
	}
	row := data.Data[0]
	if row["status"] != string(sysmodel.SessionStatusPending) {
		t.Fatalf("维度值应为 pending，got %+v", row)
	}
	if row["message_count"] != int64(2) {
		t.Fatalf("过滤后的消息和应为 2（closed 的 9 不计入），got %+v", row)
	}
}

// users 数据源：churn_risk 分支 + 过滤（report_filter users 白名单仅 churn_risk）。
func TestQueryReportData_Users_ChurnRiskBranch(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	svc := setupCustomReportService(t, db)
	ctx := context.Background()

	for _, risk := range []string{"low", "low", "high"} {
		if err := db.Create(&sysmodel.Customer{Name: "sink-u" + risk, ChurnRisk: risk}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	report := &model.CustomReport{
		Name:       "风险层报表",
		DataSource: "users",
		Dimensions: `[{"field":"churn_risk","label":"风险层"}]`,
		Metrics:    `[{"field":"user_count","label":"用户数"}]`,
		Filters:    `[{"field":"churn_risk","operator":"eq","value":"high"}]`,
		ChartType:  "table",
		IsPublic:   true,
	}
	data, err := svc.QueryReportData(ctx, report, nil)
	if err != nil {
		t.Fatalf("QueryReportData: %v", err)
	}
	if data.Total != 1 {
		t.Fatalf("churn_risk=high 过滤后应只剩 1 组，got %d", data.Total)
	}
	if data.Data[0]["count"] != int64(1) {
		t.Fatalf("high 组应只有 1 行，got %+v", data.Data[0])
	}
}

// agents 数据源：空坐席名排除 + 计数排序，经 service→仓储全链路。
func TestQueryReportData_Agents_Branch(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	svc := setupCustomReportService(t, db)
	ctx := context.Background()

	seeds := []*sysmodel.CustomerSession{
		{SessionID: "sink-a1", AgentName: "甲"},
		{SessionID: "sink-a2", AgentName: "甲"},
		{SessionID: "sink-a3", AgentName: "乙"},
		{SessionID: "sink-a4"}, // 空名：排除
	}
	for _, s := range seeds {
		if err := db.Create(s).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	report := &model.CustomReport{
		Name:       "坐席报表",
		DataSource: "agents",
		Dimensions: `[{"field":"agent_name","label":"坐席"}]`,
		Metrics:    `[{"field":"session_count","label":"会话数"}]`,
		ChartType:  "table",
		IsPublic:   true,
	}
	data, err := svc.QueryReportData(ctx, report, nil)
	if err != nil {
		t.Fatalf("QueryReportData: %v", err)
	}
	if len(data.Data) != 2 {
		t.Fatalf("空名坐席应被排除（期望 2 组），got %+v", data.Data)
	}
	if data.Data[0]["agent_name"] != "甲" || data.Data[0]["session_count"] != int64(2) {
		t.Fatalf("首位期望 甲×2，got %+v", data.Data[0])
	}
}

// 仓储注入构造（替代 NewCustomReportServiceWithDB）：与查询结果等价。
func TestNewCustomReportServiceWithRepos_QueryEquivalence(t *testing.T) {
	db := setupCustomReportServiceTestDB(t)
	svc := NewCustomReportServiceWithRepos(
		opsrepo.NewCustomReportRepositoryWithDB(db),
		sysrepo.NewCustomerSessionRepositoryWithDB(db),
		sysrepo.NewClueRepositoryWithDB(db),
		sysrepo.NewUserRFMRepositoryWithDB(db),
	)
	if svc.reportRepo == nil || svc.sessionRepo == nil || svc.clueRepo == nil || svc.userRfmRepo == nil {
		t.Fatal("注入构造后四仓储应就绪")
	}

	if err := db.Create(&sysmodel.Clue{SourceID: "sink-c1", Account: "a1", Type: 1, Name: "n1"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	report := &model.CustomReport{
		Name:       "线索报表",
		DataSource: "clues",
		Dimensions: `[{"field":"type","label":"类型"}]`,
		Metrics:    `[{"field":"clue_count","label":"线索数"}]`,
		ChartType:  "pie",
		IsPublic:   true,
	}
	data, err := svc.QueryReportData(context.Background(), report, nil)
	if err != nil {
		t.Fatalf("QueryReportData: %v", err)
	}
	if len(data.Data) != 1 || data.Data[0]["type"] != "1" || data.Data[0]["clue_count"] != int64(1) {
		t.Fatalf("注入构造的 clues 聚合期望 type=1×1，got %+v", data.Data)
	}
}
