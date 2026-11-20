package repository

import (
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

type crawlBatchBeforeLease struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	TenantID  uint   `gorm:"not null;default:1"`
	SourceID  uint   `gorm:"not null"`
	BatchNo   string `gorm:"type:varchar(64);not null"`
	Trigger   string `gorm:"type:varchar(16);not null"`
	Status    string `gorm:"type:varchar(16);not null;default:'pending'"`
	StartedAt *time.Time
	CreatedAt time.Time
}

func (crawlBatchBeforeLease) TableName() string { return "kb_crawl_batches" }

func TestCrawlerJobs_MigrationPreservesLegacyAndTenantUniqueness(t *testing.T) {
	db := testutil.NewTestDB(t, &crawlBatchBeforeLease{})
	started := time.Now().Add(-time.Hour)
	legacy := crawlBatchBeforeLease{TenantID: 31, SourceID: 9, BatchNo: "legacy-running", Trigger: "manual", Status: "running", StartedAt: &started}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	for migration := 1; migration <= 2; migration++ {
		if err := db.AutoMigrate(&model.KBCrawlBatch{}, &model.KBCrawlPage{}); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", migration, err)
		}
	}
	var preserved model.KBCrawlBatch
	if err := db.First(&preserved, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.Status != "running" || preserved.TenantID != 31 || preserved.LeaseToken != "" || preserved.LeaseUntil != nil || preserved.StartedAt == nil {
		t.Fatalf("旧批次迁移后改变或虚构租约: %+v", preserved)
	}
	for _, column := range []string{"lease_token", "lease_until"} {
		if !db.Migrator().HasColumn(&model.KBCrawlBatch{}, column) {
			t.Fatalf("缺少租约列 %s", column)
		}
	}
	page := func(tenantID, sourceID uint) *model.KBCrawlPage {
		return &model.KBCrawlPage{TenantID: tenantID, SourceID: sourceID, URLHash: "same-url", URL: "https://example.com/synthetic", KBID: 1, DocumentID: 1, ContentHash: "same-content"}
	}
	if err := db.Create(page(31, 9)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(page(31, 9)).Error; err == nil {
		t.Fatal("同租户同来源同 URL 必须由唯一索引拒绝重复")
	}
	if err := db.Create(page(32, 9)).Error; err != nil {
		t.Fatalf("不同租户不得被全局唯一索引阻止: %v", err)
	}
	if err := db.Create(page(31, 10)).Error; err != nil {
		t.Fatalf("不同来源应保留独立文档关联: %v", err)
	}
}
