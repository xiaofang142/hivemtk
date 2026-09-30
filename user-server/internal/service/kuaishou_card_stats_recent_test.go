package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// setupKuaishouCardStatsTestDB 复用仓库既有的 testutil 建库手法（见 kuaishou_card_test.go）。
func setupKuaishouCardStatsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.KuaishouCard{},
		&model.KuaishouCardActivity{},
	)
	db.SetTestDB(database)
	return database
}

func seedKuaishouCard(t *testing.T, database *gorm.DB, title string) uint {
	t.Helper()
	card := model.KuaishouCard{Title: title, Description: "audit", IsActive: true}
	if err := database.Create(&card).Error; err != nil {
		t.Fatalf("播种快手卡片失败: %v", err)
	}
	return card.ID
}

// TestKuaishouCardStats_GetCardStats_RecentActivityPresent 是本次修复的防回归闸门。
//
// 事故：仓储层只有 GetRecentActivitiesWithJoin（全局、无 cardID 过滤），
// 缺 GetRecentActivitiesByCard，而 dto.KuaishouCardStatsResponse（单卡）也没有
// RecentActivity 字段 —— 于是「快手单卡统计」页的最近活动表格永远空。
// 前端 CardStats.vue 把表格绑在 recentActivity 上且 el-tag 渲染依赖它，
// 于是那张表连同 12 处 el-tag 全是死代码。
func TestKuaishouCardStats_GetCardStats_RecentActivityPresent(t *testing.T) {
	database := setupKuaishouCardStatsTestDB(t)
	svc := NewKuaishouCardStatsService(database)
	cardID := seedKuaishouCard(t, database, "audit-card")

	// 播一条 view 活动（activity_type='view' 才是仓储认的类型）
	if err := database.Create(&model.KuaishouCardActivity{
		CardID:       cardID,
		UserID:       0,
		ActivityType: "view",
		IPAddress:    "127.0.0.1",
		CreatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("播种快手卡片活动失败: %v", err)
	}
	// 再播一条非 view 活动，确认过滤生效（仓储 Where 里带了 activity_type='view'）
	if err := database.Create(&model.KuaishouCardActivity{
		CardID:       cardID,
		ActivityType: "like",
		IPAddress:    "127.0.0.1",
		CreatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("播种非 view 活动失败: %v", err)
	}

	resp, err := svc.GetCardStats(context.Background(), &dto.KuaishouCardStatsRequest{CardID: cardID})
	if err != nil {
		t.Fatalf("GetCardStats 报错: %v", err)
	}

	// 修前这个字段根本不存在，前端拿到的是 undefined ⇒ 表格永远空
	if resp.RecentActivity == nil {
		t.Fatal("RecentActivity 为 nil：单卡统计响应必须带 recentActivity 数组（nil 会被前端 Array.isArray 判空）")
	}
	if len(resp.RecentActivity) != 1 {
		t.Fatalf("RecentActivity 长度 = %d，期望 1（只统计 activity_type='view'）", len(resp.RecentActivity))
	}
	got := resp.RecentActivity[0]
	if got.CardID != cardID {
		t.Errorf("RecentActivity[0].CardID = %d，期望 %d", got.CardID, cardID)
	}
	if got.Action != "view" {
		t.Errorf("RecentActivity[0].Action = %q，期望 \"view\"", got.Action)
	}
	if got.IPAddress != "127.0.0.1" {
		t.Errorf("RecentActivity[0].IPAddress = %q，期望 \"127.0.0.1\"（仓储把 ip_address 映射到 user_ip）", got.IPAddress)
	}
	// dto.Activity.CreatedAt 是格式化后的字符串（不是 time.Time），
	// 所以判「非零」要看它是不是空串 / 零值串。
	if got.CreatedAt == "" || got.CreatedAt == "0001-01-01T00:00:00Z" {
		t.Errorf("RecentActivity[0].CreatedAt = %q，期望非零值", got.CreatedAt)
	}

	// 关键：字段必须真的序列化出去（修前 json 里压根没有 recentActivity 这个 key）
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("序列化响应失败: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("反序列化响应失败: %v", err)
	}
	if _, ok := m["recentActivity"]; !ok {
		t.Fatalf("响应 JSON 里没有 recentActivity 这个 key: %s", raw)
	}
}

// TestKuaishouCardStats_GetCardStats_RecentActivityEmptyIsArray 无活动时必须是空数组而不是 null，
// 与抖音/小红书单卡响应的形状保持一致（前端 el-table 绑 null 会直接抛错）。
func TestKuaishouCardStats_GetCardStats_RecentActivityEmptyIsArray(t *testing.T) {
	database := setupKuaishouCardStatsTestDB(t)
	svc := NewKuaishouCardStatsService(database)
	cardID := seedKuaishouCard(t, database, "audit-card-no-activity")

	resp, err := svc.GetCardStats(context.Background(), &dto.KuaishouCardStatsRequest{CardID: cardID})
	if err != nil {
		t.Fatalf("GetCardStats 报错: %v", err)
	}
	if resp.RecentActivity == nil {
		t.Fatal("无活动时 RecentActivity 为 nil，期望空数组（make([]dto.Activity,0,0)）")
	}
	if len(resp.RecentActivity) != 0 {
		t.Errorf("无活动时 RecentActivity 长度 = %d，期望 0", len(resp.RecentActivity))
	}
	raw, _ := json.Marshal(resp)
	if want := `"recentActivity":[]`; !strings.Contains(string(raw), want) {
		t.Errorf("响应 JSON 里 %s 序列化成了非空数组形式: %s", want, raw)
	}
}

// TestKuaishouCardStats_GetCardStats_RecentActivityScopedToCard 确认按 cardID 过滤：
// A 卡的 view 活动不应出现在 B 卡的响应里（这正是修复前的语义缺口）。
func TestKuaishouCardStats_GetCardStats_RecentActivityScopedToCard(t *testing.T) {
	database := setupKuaishouCardStatsTestDB(t)
	svc := NewKuaishouCardStatsService(database)
	cardA := seedKuaishouCard(t, database, "audit-card-A")
	cardB := seedKuaishouCard(t, database, "audit-card-B")

	for _, id := range []uint{cardA, cardB} {
		if err := database.Create(&model.KuaishouCardActivity{
			CardID: id, ActivityType: "view", IPAddress: "10.0.0.1", CreatedAt: time.Now(),
		}).Error; err != nil {
			t.Fatalf("播种活动失败(card=%d): %v", id, err)
		}
	}

	respA, err := svc.GetCardStats(context.Background(), &dto.KuaishouCardStatsRequest{CardID: cardA})
	if err != nil {
		t.Fatalf("GetCardStats(cardA) 报错: %v", err)
	}
	if len(respA.RecentActivity) != 1 {
		t.Fatalf("cardA 的 RecentActivity 长度 = %d，期望 1", len(respA.RecentActivity))
	}
	if respA.RecentActivity[0].CardID != cardA {
		t.Errorf("cardA 的响应里混进了 cardID=%d 的活动", respA.RecentActivity[0].CardID)
	}
}
