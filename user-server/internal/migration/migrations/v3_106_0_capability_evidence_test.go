package migrations

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 实际登记的升级重跑不产生授权，降级保留既有证据。
func TestCapabilityEvidenceMigration_RegisteredIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.106.0")
	if !ok || m.Name() == "" || m.Description() == "" {
		t.Fatal("缺少登记的能力证据迁移元数据")
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := m.Up(ctx); err != nil {
			t.Fatalf("第 %d 次升级失败：%v", i+1, err)
		}
	}
	for _, table := range []any{&model.CapabilitySourceBinding{}, &model.CapabilityEvidence{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("升级只应建空表，实际 %d 行，错误 %v", count, err)
		}
	}
	evidence := model.CapabilityEvidence{TenantID: 71, ShopID: "test-shop", Product: "douyin", Region: "CN", Action: "message.text.send", Carrier: "bridge", ExpiresAt: time.Now(), Simulated: true}
	if err := db.Create(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model.CapabilityEvidence{}, evidence.ID).Error; err != nil {
		t.Fatalf("降级不得删除证据：%v", err)
	}
}
