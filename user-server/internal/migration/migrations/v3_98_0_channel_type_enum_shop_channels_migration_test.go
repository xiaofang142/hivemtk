package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.98.0（电商店铺五渠道枚举扩容）验收判据（与 v3.92.0 测试同形）：
//
//	① 存量形态漂移现场：v3.22.2+v3.92.0 形态下五值确认缺失；
//	② Up 补齐五值；
//	③ 幂等重入不重复；
//	④ 收口效果：绑定行落 ChannelType='taobao'/'shopee' 不再被 ENUM 拒写。
func TestShopChannelTypeEnumMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDB(t, &model.ChannelAgentBinding{})
	if database == nil {
		t.Fatal("测试库不可达：枚举扩容是否收口无法判定")
	}
	ctx := context.Background()

	if err := database.Exec(`DROP TABLE IF EXISTS channel_agent_bindings`).Error; err != nil {
		t.Fatalf("重置 channel_agent_bindings 失败: %v", err)
	}
	if err := database.Exec(`DROP TYPE IF EXISTS channel_type_enum`).Error; err != nil {
		t.Fatalf("重置 channel_type_enum 失败（存在未预期依赖，需人工核查）: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Exec(`DELETE FROM channel_agent_bindings WHERE account_id LIKE 'acc-enum98-%'`).Error
	})

	if err := database.AutoMigrate(&model.ChannelAgentBinding{}); err != nil {
		t.Fatalf("重建 channel_agent_bindings 失败: %v", err)
	}
	// 重建 v3.22.2（12 值 ENUM）+ v3.92.0（桥接五值）形态，再钉本轮漂移现场。
	if err := NewEnumOptimizeMigration(database).Up(ctx); err != nil {
		t.Fatalf("重建 v3.22.2 形态失败: %v", err)
	}
	if err := NewChannelTypeEnumBridgeValuesMigration(database).Up(ctx); err != nil {
		t.Fatalf("重建 v3.92.0 形态失败: %v", err)
	}
	m := NewShopChannelTypeEnumMigration(database)
	for _, v := range shopChannelEnumValues {
		var present bool
		if err := database.Raw(
			`SELECT EXISTS (SELECT 1 FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
			 WHERE t.typname = 'channel_type_enum' AND e.enumlabel = ?)`,
			v,
		).Scan(&present).Error; err != nil {
			t.Fatalf("① 查枚举值 %s 失败: %v", v, err)
		}
		if present {
			t.Fatalf("① 漂移现场不成立：%s 不应已存在", v)
		}
	}

	// ② 补五值
	if err := m.Up(ctx); err != nil {
		t.Fatalf("② 枚举扩容失败: %v", err)
	}
	labels := enumLabelsOrFatal(t, database)
	for _, v := range shopChannelEnumValues {
		if !labels[v] {
			t.Fatalf("② 枚举缺 %s：%v", v, labels)
		}
	}

	// ③ 幂等重入
	if err := m.Up(ctx); err != nil {
		t.Fatalf("③ 幂等重入失败: %v", err)
	}
	labels = enumLabelsOrFatal(t, database)
	if len(labels) != len(v3222ChannelTypeEnumValues)+len(bridgeChannelEnumValues)+len(shopChannelEnumValues) {
		t.Fatalf("③ 重入不得产生重复值：%v", labels)
	}

	// ④ 收口效果：店铺渠道绑定落库不再被 ENUM 拒写
	isPrimary := true
	for i, ch := range []string{"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us"} {
		row := &model.ChannelAgentBinding{
			TenantID:    1,
			ChannelType: ch,
			AccountID:   "acc-enum98-" + ch,
			AgentID:     uint(i + 1), //nolint:gosec // 测试序号无溢出面
			IsPrimary:   &isPrimary,
		}
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("④ 绑定行落库 ChannelType=%s 被 ENUM 拒写: %v", ch, err)
		}
	}
}
