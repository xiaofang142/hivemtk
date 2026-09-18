// Package repository - message_hub 裸奔方法批量补测
//
// 目标：把 repository 层覆盖率从 23.5% 拉到 50%+
// 覆盖模块：
//   - message_hub_inbox_ctx.go       11 方法全 0%  →  真 DB
//   - message_hub_inbox_stats.go      1 方法全 0%  →  真 DB
//   - message_hub_inbox_syncgap.go    1 方法全 0%  →  真 DB
//   - InboxConversationRepository     8 方法全 0%  →  真 DB
//   - InboxAssignmentRepository      12 方法全 0%  →  真 DB
//   - outbound 补漏                   3 方法全 0%  →  真 DB
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupHubFullTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.InboxAssignment{},
		&model.MessageTrace{},
		&model.AgentStatus{},
	)

	for _, tbl := range []string{
		"message_trace",
		"inbox_assignments",
		"inbox_conversations",
		"message_hub",
		"agent_statuses",
	} {
		if err := db.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("TRUNCATE %s: %v", tbl, err)
		}
	}
	return db
}

func newHubWithConv(platform, accountID, customer, msgID, direction, status string, sentAt time.Time) *model.MessageHub {
	return &model.MessageHub{
		Platform:       platform,
		AccountID:      accountID,
		MsgID:          msgID,
		ConversationID: "conv:" + customer,
		Direction:      direction,
		Status:         status,
		MsgType:        "text",
		Content:        "hello-" + msgID,
		SenderID: func() string {
			if direction == "inbound" {
				return customer
			}
			return "agent-001"
		}(),
		ReceiverID: func() string {
			if direction == "inbound" {
				return "agent-001"
			}
			return customer
		}(),
		TraceID:   "trace-" + msgID,
		DedupHash: "hash-" + msgID,
		SentAt:    sentAt,
	}
}

func newConv(platform, accountID, customer, status string, now time.Time) *model.InboxConversation {
	ct := now.Add(-5 * time.Minute)
	cm := "hi from " + customer
	return &model.InboxConversation{
		Platform:           platform,
		AccountID:          accountID,
		CustomerID:         customer,
		CustomerName:       "客户-" + customer,
		ConversationID:     "conv:" + customer,
		Status:             status,
		LastMessageFrom:    "customer",
		LastMessageAt:      &ct,
		LastMessagePreview: cm,
		UnreadCount:        0,
		TotalCount:         1,
	}
}

func TestMessageHubRepository_GetLastByPlatformAccount(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	now := time.Now()

	for i := 1; i <= 3; i++ {
		h := newHubWithConv("wechat", "acc_gpa", "c1", fmt.Sprintf("m_gpa_%d", i), "inbound", "received", now.Add(-time.Duration(4-i)*time.Minute))
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.GetLastByPlatformAccount(ctx, "wechat", "acc_gpa")
	if err != nil {
		t.Fatalf("GetLastByPlatformAccount 失败: %v", err)
	}
	if got.MsgID != "m_gpa_3" {
		t.Errorf("期望最新 m_gpa_3, 得 %s", got.MsgID)
	}
}

func TestMessageHubRepository_GetLastByPlatformAccount_NilRepo(t *testing.T) {
	var repo *MessageHubRepository
	_, err := repo.GetLastByPlatformAccount(context.Background(), "wechat", "acc")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("nil repo 应返回 ErrRecordNotFound, 得 %v", err)
	}
}

func TestMessageHubRepository_GetLastByPlatformAccount_NoMatch(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	_, err := repo.GetLastByPlatformAccount(context.Background(), "unknown", "unknown")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("无匹配应返回 ErrRecordNotFound, 得 %v", err)
	}
}

func TestMessageHubRepository_HasUnrepliedCustomerMessage(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	convID := "conv:hu1"
	now := time.Now()
	h := newHubWithConv("wechat", "acc_hur", "hu1", "m_hur_1", "inbound", "received", now.Add(-50*time.Second))
	h.ConversationID = convID
	if err := db.Create(h).Error; err != nil {
		t.Fatal(err)
	}

	unreplied, within, err := repo.HasUnrepliedCustomerMessage(ctx, convID, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !unreplied {
		t.Error("inbound 最后一条应 unreplied=true")
	}
	if !within {
		t.Error("50s 前应在 2min 窗口内")
	}

	_, within, _ = repo.HasUnrepliedCustomerMessage(ctx, convID, 10*time.Second)
	if within {
		t.Error("50s 前应在 10s 窗口外")
	}
}

func TestMessageHubRepository_HasUnrepliedCustomerMessage_OutboundLast(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	convID := "conv:hu2"
	now := time.Now()
	out := newHubWithConv("wechat", "acc", "hu2", "m_hu2_1", "outbound", "delivered", now.Add(-5*time.Second))
	out.ConversationID = convID
	if err := db.Create(out).Error; err != nil {
		t.Fatal(err)
	}
	_, _, err := repo.HasUnrepliedCustomerMessage(ctx, convID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMessageHubRepository_HasUnrepliedCustomerMessage_EmptyConvID(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	unreplied, within, err := repo.HasUnrepliedCustomerMessage(context.Background(), "", time.Minute)
	if err != nil || unreplied || within {
		t.Errorf("空 convID 应返回 (false,false,nil), 得 (%v,%v,%v)", unreplied, within, err)
	}
}

func TestMessageHubRepository_GetLastOutboundByConversation(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	convID := "conv:gl1"
	now := time.Now()

	rec := newHubWithConv("wechat", "acc", "gl1", "m_gl1_1", "outbound", "delivered", now.Add(-2*time.Minute))
	rec.ConversationID = convID
	if err := db.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetLastOutboundByConversation(ctx, convID)
	if err != nil {
		t.Fatalf("GetLastOutbound: %v", err)
	}
	if got.MsgID != "m_gl1_1" {
		t.Errorf("期望 m_gl1_1, 得 %s", got.MsgID)
	}

	old := newHubWithConv("wechat", "acc", "gl1", "m_gl1_old", "outbound", "delivered", now.Add(-6*time.Minute))
	old.ConversationID = convID
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}

	got2, _ := repo.GetLastOutboundByConversation(ctx, convID)
	if got2.MsgID != "m_gl1_1" {
		t.Errorf("cutoff 5min 内仍应取到新的, 得 %s", got2.MsgID)
	}
}

func TestMessageHubRepository_GetLastInboundByConversation(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	convID := "conv:gi1"
	now := time.Now()

	h1 := newHubWithConv("wechat", "acc", "gi1", "m_gi_1", "inbound", "received", now.Add(-2*time.Minute))
	h1.ConversationID = convID
	h2 := newHubWithConv("wechat", "acc", "gi1", "m_gi_2", "inbound", "received", now.Add(-time.Minute))
	h2.ConversationID = convID

	out := newHubWithConv("wechat", "acc", "gi1", "m_gi_out", "outbound", "delivered", now.Add(-30*time.Second))
	out.ConversationID = convID

	for _, h := range []*model.MessageHub{h1, h2, out} {
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.GetLastInboundByConversation(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MsgID != "m_gi_2" {
		t.Errorf("期望最新 inbound m_gi_2, 得 %s", got.MsgID)
	}
}

func TestMessageHubRepository_ListByConversationContext(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	now := time.Now()

	s1 := newHubWithConv("wechat", "acc_ctx", "c1", "m_ctx_1", "inbound", "received", now)
	r1 := newHubWithConv("wechat", "acc_ctx", "c1", "m_ctx_2", "outbound", "delivered", now)
	r2 := newHubWithConv("wechat", "acc_ctx", "c1", "m_ctx_3", "outbound", "delivered", now)
	r2.ReceiverID = "other-c"

	for _, h := range []*model.MessageHub{s1, r1, r2} {
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	hubs, err := repo.ListByConversationContext(context.Background(), "wechat", "acc_ctx", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hubs) != 2 {
		t.Errorf("期望 2 条(sender 或 receiver 是 c1), 得 %d", len(hubs))
	}
}

func TestMessageHubRepository_FindNullConversationIDRows(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	now := time.Now()

	n1 := newHubWithConv("wechat", "acc", "c1", "m_null_1", "inbound", "received", now)
	n1.ConversationID = ""
	n2 := newHubWithConv("wechat", "acc", "c1", "m_null_2", "inbound", "received", now)
	n2.ConversationID = ""
	good := newHubWithConv("wechat", "acc", "c1", "m_good", "inbound", "received", now)

	for _, h := range []*model.MessageHub{n1, n2, good} {
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	rows, err := repo.FindNullConversationIDRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("期望 2 条空 conv_id, 得 %d", len(rows))
	}
}

func TestMessageHubRepository_UpdateConversationID(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	now := time.Now()

	h := newHubWithConv("wechat", "acc", "c1", "m_update_conv", "inbound", "received", now)
	h.ConversationID = ""
	if err := db.Create(h).Error; err != nil {
		t.Fatal(err)
	}

	err := repo.UpdateConversationID(context.Background(), h.ID, "conv:updated")
	if err != nil {
		t.Fatal(err)
	}

	var got model.MessageHub
	if err := db.First(&got, h.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ConversationID != "conv:updated" {
		t.Errorf("期望 conv:updated, 得 %q", got.ConversationID)
	}
}

func TestMessageHubRepository_FindConversationIDsMissingInbox(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	now := time.Now()

	for _, c := range []string{"conv_a", "conv_b"} {
		h := newHubWithConv("wechat", "acc", c, "m_"+c, "inbound", "received", now)
		h.ConversationID = c
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}
	conv := newConv("wechat", "acc", "conv_a", "unread", now)
	conv.ConversationID = "conv_a"
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	missing, err := repo.FindConversationIDsMissingInbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "conv_b" {
		t.Errorf("期望 missing=[conv_b], 得 %v", missing)
	}
}

func TestMessageHubRepository_FindLatestByConversation(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	convID := "conv:fl1"
	now := time.Now()

	for i := 1; i <= 2; i++ {
		h := newHubWithConv("wechat", "acc", "fl1", fmt.Sprintf("m_fl_%d", i), "inbound", "received", now)
		h.ConversationID = convID
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.FindLatestByConversation(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MsgID != "m_fl_2" {
		t.Errorf("期望最后插入 m_fl_2, 得 %s", got.MsgID)
	}

	_, err = repo.FindLatestByConversation(ctx, "")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("空 convID 应 ErrRecordNotFound, 得 %v", err)
	}
}

func TestMessageHubRepository_NormalizePollutedConversationIDs(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	now := time.Now()

	polluted := newHubWithConv("wechat", "acc_np", "np1", "m_np1", "inbound", "received", now)
	polluted.ConversationID = "conv:np1 今天 14:30"
	polluted.SenderID = "conv:np1"
	polluted.ReceiverID = "agent-001"
	if err := db.Create(polluted).Error; err != nil {
		t.Fatal(err)
	}

	affected, err := repo.NormalizePollutedConversationIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Errorf("期望影响 1 行, 得 %d", affected)
	}

	var got model.MessageHub
	if err := db.First(&got, polluted.ID).Error; err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got.ConversationID, "今天") || strings.Contains(got.ConversationID, "14:30") {
		t.Errorf("时间戳后缀未清洗: %s", got.ConversationID)
	}
}

func TestMessageHubRepository_NormalizePollutedTraceConversationIDs(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}

	trace := model.MessageTrace{
		TraceID:        "t1",
		ConversationID: "conv:np2 昨天 09:15",
		AccountID:      "acc_np2",
		Channel:        "wechat",
		Node:           "bridge",
		Direction:      "inbound",
		MsgID:          "m_np2",
		Status:         "ok",
		CreatedAt:      time.Now(),
	}
	if err := db.Create(&trace).Error; err != nil {
		t.Fatal(err)
	}

	affected, err := repo.NormalizePollutedTraceConversationIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Errorf("期望影响 1 行, 得 %d", affected)
	}

	var got model.MessageTrace
	if err := db.First(&got, trace.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.ConversationID, "昨天") {
		t.Errorf("时间戳后缀未清洗: %s", got.ConversationID)
	}
}

func TestMessageHubRepository_GetHubStats(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	now := time.Now()

	for i := 1; i <= 5; i++ {
		dir := "inbound"
		if i%2 == 0 {
			dir = "outbound"
		}
		h := newHubWithConv("wechat", "acc_stats", "c1", fmt.Sprintf("m_stats_%d", i), dir, "received", now.Add(-time.Duration(i)*time.Minute))

		h.IsRead = (i == 2 || i == 4)
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	stats, err := repo.GetHubStats(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 5 {
		t.Errorf("期望 total=5, 得 %d", stats.Total)
	}
	if stats.Inbound != 3 {
		t.Errorf("期望 inbound=3, 得 %d", stats.Inbound)
	}
	if stats.Outbound != 2 {
		t.Errorf("期望 outbound=2, 得 %d", stats.Outbound)
	}
	if stats.Unread != 3 {
		t.Errorf("期望 unread=3, 得 %d", stats.Unread)
	}
	if stats.ByPlatform["wechat"] != 5 {
		t.Errorf("ByPlatform.wechat 期望 5, 得 %d", stats.ByPlatform["wechat"])
	}
	if stats.Recent24h != 5 {
		t.Errorf("Recent24h 期望 5 (全部在 24h 内), 得 %d", stats.Recent24h)
	}

	start := now.Add(-4 * time.Minute)
	end := now.Add(-2 * time.Minute)
	statsWin, err := repo.GetHubStats(ctx, &start, &end)
	if err != nil {
		t.Fatal(err)
	}
	if statsWin.Total != 3 {
		t.Errorf("时间窗口期望 3, 得 %d", statsWin.Total)
	}
}

func TestMessageHubRepository_FindSyncGapConversations(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}

	now := time.Now()

	for _, pair := range []struct {
		customer string
		since    time.Time
	}{
		{"ok", now.Add(-time.Minute)},
		{"gap", now.Add(-time.Minute)},
		{"old", now.Add(-2 * time.Hour)},
	} {
		h := newHubWithConv("wechat", "acc_sgap", pair.customer, "m_"+pair.customer, "inbound", "received", now)
		h.ConversationID = "conv:" + pair.customer
		h.CreatedAt = pair.since
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	conv := newConv("wechat", "acc_sgap", "ok", "unread", now)
	conv.ConversationID = "conv:ok"
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	rows, err := repo.FindSyncGapConversations(context.Background(), now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 || rows[0].CustomerID != "gap" {
		t.Errorf("期望 gap customer, 得 %v", rows)
	}
}

func TestInboxConversationRepository_CreateAndGet(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	conv := newConv("wechat", "acc_ir", "c1_ir", "unread", now)
	if err := r.Create(ctx, conv); err != nil {
		t.Fatal(err)
	}

	got, err := r.GetByID(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomerID != "c1_ir" {
		t.Errorf("期望 c1_ir, 得 %s", got.CustomerID)
	}

	var nilr *InboxConversationRepository
	if err := nilr.Create(ctx, conv); err != nil {
		t.Error("nil repo Create 应返回 nil")
	}
}

func TestInboxConversationRepository_FindByPlatformAccountCustomer(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	now := time.Now()

	conv := newConv("wechat", "acc_fpac", "c_fpac", "unread", now)
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	got, err := r.FindByPlatformAccountCustomer(context.Background(), "wechat", "acc_fpac", "c_fpac")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != conv.ID {
		t.Errorf("期望 %d, 得 %d", conv.ID, got.ID)
	}
}

func TestInboxConversationRepository_UpdateLastMessage(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	now := time.Now()

	conv := newConv("wechat", "acc_ulm", "c_ulm", "unread", now)
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	newAt := now.Add(-30 * time.Second)
	newPreview := "新消息预览-" + strings.Repeat("a", 550)
	err := r.UpdateLastMessage(context.Background(), conv.ID, newPreview, newAt, 2)
	if err != nil {
		t.Fatal(err)
	}

	var got model.InboxConversation
	if err := db.Session(&gorm.Session{NewDB: true}).First(&got, conv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 2 {
		t.Errorf("期望 unread_count=2, 得 %d", got.UnreadCount)
	}
	if len(got.LastMessagePreview) != 500 {
		t.Errorf("期望截断到 500, 得 %d", len(got.LastMessagePreview))
	}
}

func TestInboxConversationRepository_ListByAccount(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	now := time.Now()

	for i := 1; i <= 5; i++ {
		conv := newConv("wechat", "acc_lba", fmt.Sprintf("c%d", i), "unread", now)
		if err := db.Create(conv).Error; err != nil {
			t.Fatal(err)
		}
	}

	items, total, err := r.ListByAccount(context.Background(), "wechat", "acc_lba", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("期望 total=5, 得 %d", total)
	}
	if len(items) != 3 {
		t.Errorf("pageSize=3 期望 3 条, 得 %d", len(items))
	}

	items, _, _ = r.ListByAccount(context.Background(), "wechat", "acc_lba", 0, 0)
	if len(items) != 5 {
		t.Errorf("默认分页期望 5 条, 得 %d", len(items))
	}
}

func TestInboxConversationRepository_UpsertFromMessage_NewConversation(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	in := UpsertFromMessageInput{
		Platform:           "wechat",
		AccountID:          "acc_up",
		CustomerID:         "c_up_new",
		CustomerName:       "新客户",
		ConversationID:     "conv:c_up_new",
		LastMessageID:      1,
		LastMessagePreview: "首条消息",
		LastMessageAt:      now,
		LastMessageFrom:    "customer",
	}
	if err := r.UpsertFromMessage(ctx, in); err != nil {
		t.Fatal(err)
	}

	got, err := r.FindByPlatformAccountCustomer(ctx, "wechat", "acc_up", "c_up_new")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "unread" {
		t.Errorf("首条 customer 消息期望 unread, 得 %s", got.Status)
	}
	if got.UnreadCount != 1 {
		t.Errorf("首条 customer 消息期望 unread=1, 得 %d", got.UnreadCount)
	}
	if got.TotalCount != 1 {
		t.Errorf("期望 total_count=1, 得 %d", got.TotalCount)
	}
}

func TestInboxConversationRepository_UpsertFromMessage_UpdateExisting(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	conv := newConv("wechat", "acc_ue", "c_upd", "open", now)
	conv.UnreadCount = 0
	conv.TotalCount = 1
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	in := UpsertFromMessageInput{
		Platform:           "wechat",
		AccountID:          "acc_ue",
		CustomerID:         "c_upd",
		ConversationID:     "conv:c_upd",
		LastMessagePreview: "第二条 customer 消息",
		LastMessageAt:      now.Add(-10 * time.Second),
		LastMessageFrom:    "customer",
	}
	if err := r.UpsertFromMessage(ctx, in); err != nil {
		t.Fatal(err)
	}

	got, err := r.FindByPlatformAccountCustomer(ctx, "wechat", "acc_ue", "c_upd")
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 1 {
		t.Errorf("第二条 customer 消息期望 unread=1, 得 %d", got.UnreadCount)
	}
	if got.TotalCount != 2 {
		t.Errorf("期望 total_count=2, 得 %d", got.TotalCount)
	}

	in.LastMessageFrom = "agent"
	in.LastMessagePreview = "agent 回复"
	if err := r.UpsertFromMessage(ctx, in); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.FindByPlatformAccountCustomer(ctx, "wechat", "acc_ue", "c_upd")
	if got2.UnreadCount != 0 {
		t.Errorf("agent 消息后 unread 应清零, 得 %d", got2.UnreadCount)
	}
	if got2.Status != "open" {
		t.Errorf("agent 回复 + unread 状态应转 open, 得 %s", got2.Status)
	}
}

func TestInboxConversationRepository_UpsertFromMessageTx_NilTx(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	err := r.UpsertFromMessageTx(context.Background(), nil, UpsertFromMessageInput{Platform: "x"})
	if err != nil {
		t.Errorf("nil tx 应返回 nil, 得 %v", err)
	}
}

func TestInboxConversationRepository_DeletePollutedInboxRows(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	now := time.Now()

	poll := newConv("wechat", "acc_dpr", "poll-c", "unread", now)
	poll.ConversationID = "conv:poll 今天 14:30"
	poll.CustomerID = "conv:poll 今天 14:30"
	good := newConv("wechat", "acc_dpr", "good-c", "unread", now)
	good.ConversationID = "conv:good"

	if err := db.Create(poll).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(good).Error; err != nil {
		t.Fatal(err)
	}

	affected, err := r.DeletePollutedInboxRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Errorf("期望删 1 条污染行, 得 %d", affected)
	}

	var n int64
	db.Model(&model.InboxConversation{}).Count(&n)
	if n != 1 {
		t.Errorf("剩余应 1 条, 得 %d", n)
	}
}

func TestInboxConversationRepository_DeleteOrphanConvInboxRows(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	convLive := newConv("wechat", "acc_doci", "c_live", "unread", now)
	convLive.ConversationID = "conv:live"
	convDead := newConv("wechat", "acc_doci", "c_dead", "unread", now)
	convDead.ConversationID = "conv:dead"

	for _, c := range []*model.InboxConversation{convLive, convDead} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	h := newHubWithConv("wechat", "acc_doci", "c_live", "m_live", "inbound", "received", now)
	h.ConversationID = "conv:live"
	if err := db.Create(h).Error; err != nil {
		t.Fatal(err)
	}

	affected, err := r.DeleteOrphanConvInboxRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Errorf("期望删 1 条孤儿 conv_dead, 得 %d", affected)
	}
}

func TestInboxConversationRepository_DeleteOrphanInboxByConversation(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	c1 := newConv("wechat", "acc_dobi", "keep-c", "unread", now)
	c1.ConversationID = "conv:dobi"
	c2 := newConv("wechat", "acc_dobi", "orphan-c", "unread", now)
	c2.ConversationID = "conv:dobi"

	for _, c := range []*model.InboxConversation{c1, c2} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	affected, err := r.DeleteOrphanInboxByConversation(ctx, "wechat", "acc_dobi", "conv:dobi", "keep-c")
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Errorf("期望删 orphan-c 1 条, 得 %d", affected)
	}
}

func TestInboxConversationRepository_ReconcileUnread(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	convA := newConv("wechat", "acc_ru", "cA", "open", now)
	convA.ConversationID = "conv:ru_A"
	convB := newConv("wechat", "acc_ru", "cB", "unread", now)
	convB.ConversationID = "conv:ru_B"

	for _, c := range []*model.InboxConversation{convA, convB} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	inA := newHubWithConv("wechat", "acc_ru", "cA", "m_A_in", "inbound", "received", now)
	inA.ConversationID = "conv:ru_A"

	outB := newHubWithConv("wechat", "acc_ru", "cB", "m_B_out", "outbound", "delivered", now.Add(2*time.Second))
	outB.ConversationID = "conv:ru_B"
	for _, h := range []*model.MessageHub{inA, outB} {
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	affected, err := r.ReconcileUnread(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 2 {
		t.Errorf("期望影响 2 行, 得 %d", affected)
	}

	var gotA model.InboxConversation
	db.First(&gotA, convA.ID)
	if gotA.UnreadCount < 1 {
		t.Errorf("convA 最后 inbound 期望 unread>=1, 得 %d", gotA.UnreadCount)
	}
	if gotA.Status != "unread" {
		t.Errorf("convA 最后 inbound + 之前 open 应转 unread, 得 %s", gotA.Status)
	}

	var gotB model.InboxConversation
	db.First(&gotB, convB.ID)
	if gotB.UnreadCount != 0 {
		t.Errorf("convB 最后 outbound 期望 unread=0, 得 %d", gotB.UnreadCount)
	}
	if gotB.Status != "open" {
		t.Errorf("convB 最后 outbound + 之前 unread 应转 open, 得 %s", gotB.Status)
	}
}

func TestInboxConversationRepository_FindOverdueConversations(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	thresh := now.Add(-10 * time.Minute)
	for _, pair := range []struct {
		customer, from string
		lastAt         time.Time
		status         string
	}{
		{"old", "customer", now.Add(-20 * time.Minute), "unread"},
		{"recent", "customer", now.Add(-30 * time.Second), "unread"},
		{"agent", "agent", now.Add(-20 * time.Minute), "unread"},
	} {
		conv := newConv("wechat", "acc_foc", pair.customer, pair.status, now)
		conv.LastMessageFrom = pair.from
		conv.LastMessageAt = &pair.lastAt
		if err := db.Create(conv).Error; err != nil {
			t.Fatal(err)
		}
	}

	list, err := r.FindOverdueConversations(ctx, thresh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].CustomerID != "old" {
		t.Errorf("期望 overdue=[old], 得 %v", func() []string {
			var ids []string
			for _, c := range list {
				ids = append(ids, c.CustomerID)
			}
			return ids
		}())
	}

	list2, _ := r.FindOverdueConversations(ctx, thresh, 0)
	if len(list2) != 1 {
		t.Errorf("limit=0 期望 1 条, 得 %d", len(list2))
	}
}

func TestInboxConversationRepository_ListOnlineAgentIDs(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		st := "offline"
		if i <= 2 {
			st = "online"
		}
		if err := db.Create(&model.AgentStatus{
			AgentID: uint(i), Status: st, AgentName: fmt.Sprintf("agent-%d", i),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	ids, err := r.ListOnlineAgentIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("期望 2 个 online agent, 得 %v", ids)
	}
}

func TestInboxConversationRepository_MarkRead_UpdateField(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	conv := newConv("wechat", "acc_mrf", "c_mrf", "unread", now)
	conv.UnreadCount = 5
	conv.Pinned = false
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	if err := r.MarkRead(ctx, conv.ID); err != nil {
		t.Fatal(err)
	}
	var got model.InboxConversation
	db.Session(&gorm.Session{NewDB: true}).First(&got, conv.ID)
	if got.UnreadCount != 0 || got.Status != "open" {
		t.Errorf("MarkRead 后 (unread=%d,status=%s), 期望 (0,open)", got.UnreadCount, got.Status)
	}

	if err := r.UpdateField(ctx, conv.ID, "pinned", true); err != nil {
		t.Fatal(err)
	}
	db.Session(&gorm.Session{NewDB: true}).First(&got, conv.ID)
	if !got.Pinned {
		t.Error("UpdateField pinned=true 未生效")
	}
}

func TestInboxConversationRepository_CountByAssignedToStatus(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	for i := 1; i <= 3; i++ {
		st := "open"
		if i <= 2 {
			st = "assigned"
		}
		conv := newConv("wechat", "acc_cbats", fmt.Sprintf("c%d", i), st, now)
		conv.AssignedTo = "agent-a"
		if err := db.Create(conv).Error; err != nil {
			t.Fatal(err)
		}
	}

	n, err := r.CountByAssignedToStatus(ctx, "agent-a", []string{"assigned"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("期望 2 条 assigned, 得 %d", n)
	}
}

func TestInboxConversationRepository_AssignTx_AllActions(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	conv := newConv("wechat", "acc_atx", "c_atx", "open", now)
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	out, err := r.AssignTx(ctx, AssignTxInput{
		ConversationID: conv.ID, Action: "assign",
		ToType: "human", ToUserID: "agent-01", OperatorID: "op1", Remark: "assign test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.NewAssignedTo != "agent-01" {
		t.Errorf("NewAssignedTo 期望 agent-01, 得 %s", out.NewAssignedTo)
	}
	var gotAssign model.InboxConversation
	db.First(&gotAssign, conv.ID)
	if gotAssign.Status != "assigned" || gotAssign.AssignedTo != "agent-01" {
		t.Errorf("assign 后 status=%s assigned_to=%s", gotAssign.Status, gotAssign.AssignedTo)
	}
	if out.History == nil {
		t.Error("assign 应写入 history")
	} else if out.History.Action != "assign" {
		t.Errorf("history.action 期望 assign, 得 %s", out.History.Action)
	}

	_, err = r.AssignTx(ctx, AssignTxInput{
		ConversationID: conv.ID, Action: "release", OperatorID: "op2",
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotRelease model.InboxConversation
	db.First(&gotRelease, conv.ID)
	if gotRelease.Status != "open" || gotRelease.AssignedTo != "" {
		t.Errorf("release 后 status=%s assigned_to=%q", gotRelease.Status, gotRelease.AssignedTo)
	}

	r.AssignTx(ctx, AssignTxInput{ConversationID: conv.ID, Action: "close", OperatorID: "op3"})
	var gotClose model.InboxConversation
	db.First(&gotClose, conv.ID)
	if gotClose.Status != "closed" {
		t.Errorf("close 后期望 closed, 得 %s", gotClose.Status)
	}
	if gotClose.ClosedAt == nil {
		t.Error("close 后 closed_at 应非空")
	}

	r.AssignTx(ctx, AssignTxInput{ConversationID: conv.ID, Action: "reopen", OperatorID: "op4"})
	var gotReopen model.InboxConversation
	db.First(&gotReopen, conv.ID)
	if gotReopen.Status != "unread" || gotReopen.ClosedAt != nil {
		t.Errorf("reopen 后 status=%s closed_at=%v", gotReopen.Status, gotReopen.ClosedAt)
	}

	_, err = r.AssignTx(ctx, AssignTxInput{ConversationID: 99999, Action: "assign"})
	if err == nil || !strings.Contains(err.Error(), "conversation not found") {
		t.Errorf("不存在的 conv 应返回 conversation not found, 得 %v", err)
	}
}

func TestInboxConversationRepository_GetStats(t *testing.T) {
	db := setupHubFullTestDB(t)
	r := NewInboxConversationRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()
	thresh := now.Add(-10 * time.Minute)

	for _, pair := range []struct {
		customer, status string
		at               time.Time
		assignedTo       string
	}{
		{"cu1", "unread", now.Add(-20 * time.Minute), ""},
		{"cu2", "unread", now.Add(-20 * time.Minute), ""},
		{"cu3", "unread", now.Add(-30 * time.Second), ""},
		{"co1", "open", now.Add(-20 * time.Minute), ""},
		{"co2", "open", now.Add(-20 * time.Minute), ""},
		{"ca1", "assigned", now.Add(-20 * time.Minute), "agent-X"},
		{"cc1", "closed", now.Add(-20 * time.Minute), ""},
	} {
		conv := newConv("wechat", "acc_gs", pair.customer, pair.status, now)
		conv.LastMessageAt = &pair.at
		conv.AssignedTo = pair.assignedTo
		if err := db.Create(conv).Error; err != nil {
			t.Fatal(err)
		}
	}

	stats, err := r.GetStats(ctx,
		[]string{"unread", "open", "assigned", "closed"},
		[]string{"assigned"},
		"customer", thresh,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Unread != 3 || stats.Open != 2 || stats.Assigned != 1 || stats.Closed != 1 {
		t.Errorf("状态分布期望 U=3 O=2 A=1 C=1, 得 U=%d O=%d A=%d C=%d",
			stats.Unread, stats.Open, stats.Assigned, stats.Closed)
	}
	if stats.OverdueCount != 5 {
		t.Errorf("overdue 期望 5, 得 %d", stats.OverdueCount)
	}
	if stats.ByAssignedTo["agent-X"] != 1 {
		t.Errorf("ByAssignedTo[agent-X] 期望 1, 得 %d", stats.ByAssignedTo["agent-X"])
	}
}

func TestInboxAssignmentRepository_ListAndGroup(t *testing.T) {
	db := setupHubFullTestDB(t)
	cr := NewInboxConversationRepositoryWithDB(db)
	ar := NewInboxAssignmentRepositoryWithDB(db)
	ctx := context.Background()
	now := time.Now()

	conv := newConv("wechat", "assign_repo", "c_ar", "open", now)
	if err := db.Create(conv).Error; err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		cr.AssignTx(ctx, AssignTxInput{
			ConversationID: conv.ID, Action: "assign",
			ToType: "human", ToUserID: fmt.Sprintf("agent-%d", (i%2)+1),
		})
	}

	items, total, err := ar.ListByConversationID(ctx, conv.ID, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Errorf("期望 total=3 items=3, 得 total=%d items=%d", total, len(items))
	}

	counts, err := ar.GroupCountByToUserID(ctx, []string{"agent-1", "agent-2"}, "assign")
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 2 {
		t.Errorf("期望 2 个 agent, 得 %v", counts)
	}

	var nilar *InboxAssignmentRepository
	items, total, _ = nilar.ListByConversationID(ctx, 0, 1, 10)
	if total != 0 || len(items) != 0 {
		t.Errorf("nil repo 应返回空, 得 total=%d items=%d", total, len(items))
	}
}

func TestMessageHubRepository_GetByMsgIDsInScopeWithConv(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	now := time.Now()

	for i := 1; i <= 3; i++ {
		h := newHubWithConv("wechat", "acc_gmiisc", "c1", fmt.Sprintf("m_gmiisc_%d", i), "outbound", "pending", now)
		h.MsgID = fmt.Sprintf("m_gmiisc_%d", i)
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	list, err := repo.GetByMsgIDsInScopeWithConv(context.Background(), "wechat", "acc_gmiisc", "conv:c1", []string{"m_gmiisc_1", "m_gmiisc_2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("期望 2 条, 得 %d", len(list))
	}
}

func TestMessageHubRepository_ListRecentOutboundInConv(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	convID := "conv:lroic"
	now := time.Now()

	for i := 1; i <= 4; i++ {
		h := newHubWithConv("wechat", "acc", "c_lroic", fmt.Sprintf("m_lroic_%d", i), "outbound", "delivered", now.Add(-time.Duration(5-i)*time.Minute))
		h.ConversationID = convID
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
	}

	list, err := repo.ListRecentOutboundInConv(ctx, "wechat", "acc", convID, now.Add(-10*time.Minute), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("limit=2 期望 2 条, 得 %d", len(list))
	}

	if len(list) > 0 && list[0].MsgID != "m_lroic_4" {
		t.Errorf("按 sent_at DESC 第一条应 m_lroic_4, 得 %s", list[0].MsgID)
	}
}

func TestMessageHubRepository_AckOutboundDeliveredBatchReturningWithStatus(t *testing.T) {
	db := setupHubFullTestDB(t)
	repo := &MessageHubRepository{db: db}
	now := time.Now()

	var msgIDs []string
	for i := 1; i <= 3; i++ {
		h := newHubWithConv("wechat", "acc_ackwsc", "c1", fmt.Sprintf("m_ackwsc_%d", i), "outbound", "pending", now)
		if err := db.Create(h).Error; err != nil {
			t.Fatal(err)
		}
		msgIDs = append(msgIDs, h.MsgID)
	}

	updatedIDs, affectedRows, err := repo.AckOutboundDeliveredBatchReturningWithStatus(context.Background(), "wechat", "acc_ackwsc", "", "delivered", msgIDs)
	if err != nil {
		t.Fatal(err)
	}
	if affectedRows != 3 {
		t.Errorf("期望 affected=3, 得 %d", affectedRows)
	}
	if len(updatedIDs) != 3 {
		t.Errorf("期望 3 条 updatedIDs, 得 %d", len(updatedIDs))
	}
	for _, r := range updatedIDs {
		if !strings.Contains(r, "m_ackwsc_") {
			t.Errorf("非法 MsgID: %s", r)
		}
	}
}
