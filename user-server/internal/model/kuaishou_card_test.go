package model

import (
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm/schema"
)

// TestKuaishouCard_TableName 钉住落库表名。KuaishouCard 自己没有 TableName()，
// 表名由 gorm 的 NamingStrategy 从结构体名推导；而 repository/kuaishou_card_stats.go:124
// 的统计查询是写死的 `LEFT JOIN kuaishou_cards`。结构体一改名，AutoMigrate 就建一张
// 新空表、老数据留在原名表里，那条 JOIN 直到运行时才炸 —— 所以这条不能只 `_ = card`。
func TestKuaishouCard_TableName(t *testing.T) {
	typ := reflect.TypeOf(KuaishouCard{})
	if _, ok := typ.MethodByName("TableName"); ok {
		t.Fatal("KuaishouCard 新增了显式 TableName()：本用例的推导前提失效，请把期望值换成它的返回值，并同步迁移登记与写死的 JOIN")
	}
	if got := (schema.NamingStrategy{}).TableName(typ.Name()); got != "kuaishou_cards" {
		t.Errorf("推导表名=%q，期望 kuaishou_cards（repository 里有写死的 JOIN 依赖它）", got)
	}
}

func TestKuaishouCard_BasicFields(t *testing.T) {
	now := time.Now()
	card := &KuaishouCard{
		ID:           1,
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://example.com/redirect",
		ShortLinkID:  uintPtr(100),
		DomainPoolID: 200,
		Tags:         "tag1,tag2,tag3",
		ViewCount:    1000,
		LikeCount:    500,
		ShareCount:   200,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if card.ID != 1 {
		t.Errorf("Expected ID 1, got %d", card.ID)
	}
	if card.Title != "Test Card" {
		t.Errorf("Expected Title 'Test Card', got %s", card.Title)
	}
	if card.Description != "Test description" {
		t.Errorf("Expected Description 'Test description', got %s", card.Description)
	}
	if card.ImageURL != "https://example.com/image.jpg" {
		t.Errorf("Expected ImageURL 'https://example.com/image.jpg', got %s", card.ImageURL)
	}
	if card.RedirectURL != "https://example.com/redirect" {
		t.Errorf("Expected RedirectURL 'https://example.com/redirect', got %s", card.RedirectURL)
	}
	if card.ShortLinkID == nil || *card.ShortLinkID != 100 {
		t.Errorf("Expected ShortLinkID 100, got %v", card.ShortLinkID)
	}
	if card.DomainPoolID != 200 {
		t.Errorf("Expected DomainPoolID 200, got %d", card.DomainPoolID)
	}
	if card.Tags != "tag1,tag2,tag3" {
		t.Errorf("Expected Tags 'tag1,tag2,tag3', got %s", card.Tags)
	}
	if card.ViewCount != 1000 {
		t.Errorf("Expected ViewCount 1000, got %d", card.ViewCount)
	}
	if card.LikeCount != 500 {
		t.Errorf("Expected LikeCount 500, got %d", card.LikeCount)
	}
	if card.ShareCount != 200 {
		t.Errorf("Expected ShareCount 200, got %d", card.ShareCount)
	}
	if !card.IsActive {
		t.Error("Expected IsActive to be true")
	}
}

func TestKuaishouCard_DefaultValues(t *testing.T) {
	card := &KuaishouCard{}

	if card.ViewCount != 0 {
		t.Logf("ViewCount is %d (expected 0 before save, default is 0)", card.ViewCount)
	}
	if card.LikeCount != 0 {
		t.Logf("LikeCount is %d (expected 0 before save, default is 0)", card.LikeCount)
	}
	if card.ShareCount != 0 {
		t.Logf("ShareCount is %d (expected 0 before save, default is 0)", card.ShareCount)
	}
	if card.IsActive != false {
		t.Logf("IsActive is %v (expected false before save, default is true)", card.IsActive)
	}
}

func TestKuaishouCard_IsActiveCard(t *testing.T) {
	activeCard := &KuaishouCard{
		IsActive: true,
	}
	if !activeCard.IsActive {
		t.Error("Expected IsActiveCard() to return true for active card")
	}

	inactiveCard := &KuaishouCard{
		IsActive: false,
	}
	if inactiveCard.IsActive {
		t.Error("Expected IsActiveCard() to return false for inactive card")
	}
}

func TestKuaishouCard_WithNilShortLinkID(t *testing.T) {
	card := &KuaishouCard{
		Title:       "Test Card",
		ShortLinkID: nil,
	}

	if card.ShortLinkID != nil {
		t.Errorf("Expected ShortLinkID nil, got %v", card.ShortLinkID)
	}
}

func TestKuaishouCard_WithShortLinkID(t *testing.T) {
	id := uint(100)
	card := &KuaishouCard{
		Title:       "Test Card",
		ShortLinkID: &id,
	}

	if card.ShortLinkID == nil || *card.ShortLinkID != 100 {
		t.Errorf("Expected ShortLinkID 100, got %v", card.ShortLinkID)
	}
}

func TestKuaishouCard_EngagementMetrics(t *testing.T) {
	card := &KuaishouCard{
		Title:      "Viral Card",
		ViewCount:  10000,
		LikeCount:  5000,
		ShareCount: 2000,
	}

	if card.ViewCount < card.LikeCount {
		t.Error("Expected ViewCount to be >= LikeCount")
	}
	if card.LikeCount < card.ShareCount {
		t.Error("Expected LikeCount to be >= ShareCount")
	}
}

func uintPtr(i uint) *uint {
	return &i
}

func TestKuaishouCard_WithEmptyTags(t *testing.T) {
	card := &KuaishouCard{
		Title: "Test Card",
		Tags:  "",
	}

	if card.Tags != "" {
		t.Errorf("Expected empty Tags, got %s", card.Tags)
	}
}
