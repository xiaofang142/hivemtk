package service

// FA-42 契约锁：员工画像必须使用本人真实样本、订单按员工归属、观察窗口显式、
// 缺数据/查询故障独立标识，趋势由相邻窗口证据得出（不得固定 stable/up）。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const fa42Tenant = uint(71)

func fa42DB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	db := testutil.NewTestDB(t,
		&model.SessionMessage{},
		&model.CustomerSession{},
		&model.Order{},
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.RagSession{},
	)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	return db
}

func fa42Ctx() context.Context {
	return tenant.WithTenantID(context.Background(), fa42Tenant)
}

func fa42Session(t *testing.T, db *gorm.DB, sessionID, accountID string, staffID uint, at time.Time) {
	t.Helper()
	row := &model.CustomerSession{
		SessionID: sessionID,
		AccountID: accountID,
		UserID:    "u-" + sessionID,
		AgentID:   staffID,
		Status:    model.SessionStatusClosed,
		Rating:    5,
		CreatedAt: at,
	}
	if err := db.WithContext(fa42Ctx()).Create(row).Error; err != nil {
		t.Fatalf("seed session %s: %v", sessionID, err)
	}
}

func fa42Message(t *testing.T, db *gorm.DB, sessionID, senderType, senderID string, at time.Time) {
	t.Helper()
	row := &model.SessionMessage{
		SessionID:  sessionID,
		SenderType: senderType,
		SenderID:   senderID,
		Content:    "m",
		CreatedAt:  at,
	}
	if err := db.WithContext(fa42Ctx()).Create(row).Error; err != nil {
		t.Fatalf("seed message %s/%s: %v", sessionID, senderType, err)
	}
}

func fa42Order(t *testing.T, db *gorm.DB, accountID string, at time.Time) {
	t.Helper()
	row := &model.Order{
		AccountID:  accountID,
		CreateTime: at.Unix(),
		Price:      "100",
	}
	if err := db.WithContext(fa42Ctx()).Create(row).Error; err != nil {
		t.Fatalf("seed order %s: %v", accountID, err)
	}
}

func fa42Item(report *PersonaReport, tag string) PersonaItem {
	for _, it := range report.Items {
		if it.Tag == tag {
			return it
		}
	}
	return PersonaItem{}
}

// 两名员工业绩相反 + 订单归属 + 真实响应样本 + 相邻窗口趋势。
func TestFA42_PersonaAttributionAndTrend(t *testing.T) {
	db := fa42DB(t)
	now := time.Now()
	curCust := now.Add(-2 * time.Hour)
	curReply := curCust.Add(10 * time.Second)
	prevCust := now.AddDate(0, 0, -40)
	prevReply := prevCust.Add(200 * time.Second)

	// 员工 42：当前窗口 2 会话、2 订单（仅 acc42a 归属），响应 10s；上一窗口响应 200s。
	fa42Session(t, db, "s42a", "acc42a", 42, now.AddDate(0, 0, -3))
	fa42Session(t, db, "s42b", "acc42b", 42, now.AddDate(0, 0, -3))
	fa42Message(t, db, "s42a", "customer", "cust", curCust)
	fa42Message(t, db, "s42a", "agent", "42", curReply)
	fa42Order(t, db, "acc42a", now.Add(-time.Hour))
	fa42Order(t, db, "acc42a", now.Add(-2*time.Hour))
	fa42Session(t, db, "s42prev", "acc42prev", 42, prevCust.Add(-time.Hour))
	fa42Message(t, db, "s42prev", "customer", "cust", prevCust)
	fa42Message(t, db, "s42prev", "agent", "42", prevReply)

	// 员工 43：当前窗口 2 会话、0 自有订单（订单挂在 42 的账户上），响应 300s。
	fa42Session(t, db, "s43a", "acc43a", 43, now.AddDate(0, 0, -3))
	fa42Session(t, db, "s43b", "acc43b", 43, now.AddDate(0, 0, -3))
	fa42Message(t, db, "s43a", "customer", "cust", curCust)
	fa42Message(t, db, "s43a", "agent", "43", curCust.Add(300*time.Second))

	// 员工 44：乱序写入——先插回复行（时间较晚），再插客户消息行（时间较早）。
	fa42Session(t, db, "s44", "acc44", 44, now.AddDate(0, 0, -3))
	fa42Message(t, db, "s44", "agent", "44", now.Add(-3*time.Hour).Add(5*time.Second))
	fa42Message(t, db, "s44", "customer", "cust", now.Add(-3*time.Hour))

	// 员工 45：无任何会话，所有依赖样本的指标应为 no_data。
	svc := NewSalesPersonaService()
	if svc.repo == nil {
		t.Fatal("有库时仓储应装配")
	}

	report42, err := svc.BuildReport(fa42Ctx(), 42)
	if err != nil {
		t.Fatalf("BuildReport(42): %v", err)
	}
	if len(report42.Items) != 7 {
		t.Fatalf("应有 7 项, got %d", len(report42.Items))
	}
	if report42.WindowDays != 30 || report42.WindowFrom.IsZero() || report42.WindowTo.IsZero() {
		t.Fatalf("观察窗口未显式化: %+v", report42)
	}
	resp42 := fa42Item(report42, "response_speed")
	if resp42.Status != "ok" || resp42.Sample != 1 || resp42.Score != 100 || resp42.Trend != "up" {
		t.Fatalf("42 响应应 10s→100分/样本1/趋势up(上一窗口200s=60分): %+v", resp42)
	}
	conv42 := fa42Item(report42, "conversion")
	if conv42.Status != "ok" || conv42.Sample != 2 || conv42.Score != 100 {
		t.Fatalf("42 转化应 2会话2订单=100分(不混入 43/其他租户订单): %+v", conv42)
	}

	report43, err := svc.BuildReport(fa42Ctx(), 43)
	if err != nil {
		t.Fatalf("BuildReport(43): %v", err)
	}
	resp43 := fa42Item(report43, "response_speed")
	if resp43.Status != "ok" || resp43.Score != 60 || resp43.Sample != 1 {
		t.Fatalf("43 响应应 300s→60分/样本1: %+v", resp43)
	}
	conv43 := fa42Item(report43, "conversion")
	if conv43.Status != "ok" || conv43.Score != 0 {
		t.Fatalf("43 转化应 0(0 自有订单，不能算 42 的): %+v", conv43)
	}

	report44, err := svc.BuildReport(fa42Ctx(), 44)
	if err != nil {
		t.Fatalf("BuildReport(44): %v", err)
	}
	resp44 := fa42Item(report44, "response_speed")
	if resp44.Status != "ok" || resp44.Sample != 1 || resp44.Score != 100 {
		t.Fatalf("44 乱序写入应按 created_at 配对 5s→100分: %+v", resp44)
	}

	report45, err := svc.BuildReport(fa42Ctx(), 45)
	if err != nil {
		t.Fatalf("BuildReport(45): %v", err)
	}
	for _, tag := range []string{"response_speed", "conversion", "satisfaction", "followup"} {
		it := fa42Item(report45, tag)
		if it.Status != "no_data" {
			t.Errorf("45 %s 无样本应 no_data, got %+v", tag, it)
		}
		if it.Score != 0 {
			t.Errorf("45 %s 无样本不得凭空得分, got %+v", tag, it)
		}
	}
}

// 查询失败：ctx 取消时各指标必须标记为 error 且汇总到 Errors，不得静默吞成满分。
func TestFA42_QueryFailureIsExplicit(t *testing.T) {
	fa42DB(t)
	ctx, cancel := context.WithCancel(fa42Ctx())
	cancel()
	svc := NewSalesPersonaService()
	report, err := svc.BuildReport(ctx, 42)
	if err != nil {
		t.Fatalf("BuildReport 不应整体返回错误（故障以 item 标识）: %v", err)
	}
	if len(report.Errors) == 0 {
		t.Fatal("查询失败必须汇总到 report.Errors")
	}
	errCount := 0
	for _, it := range report.Items {
		if it.Status == "error" {
			errCount++
			if it.Score != 0 {
				t.Errorf("%s 查询失败不得计分: %+v", it.Tag, it)
			}
		}
	}
	if errCount == 0 {
		t.Fatalf("至少一项应标 error, report=%+v", report)
	}
}
