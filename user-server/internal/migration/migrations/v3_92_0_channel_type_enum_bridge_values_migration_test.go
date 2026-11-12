package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.92.0（桥接渠道枚举扩容）验收判据：
//
//	① 空操作分支：channel_type_enum 不存在（v3.22.2 未跑）时 Up 不报错、不抢先建 ENUM；
//	② 存量库形态重建：按 v3.22.2 建 12 值 ENUM 并把 channel_agent_bindings.channel_type
//	   转成 ENUM 后，五值（jd/bilibili/weibo/personal_wx/pdd）确认缺失——这是漂移现场；
//	③ Up 补齐五值（pg_enum 可查，12+5=17）；
//	④ 幂等重入：第二次 Up 不报错不重复；
//	⑤ 收口效果：绑定行落 ChannelType='bilibili'/'weibo' 不再被 ENUM 拒写；
//	⑥ Down 不销毁（PG 无 DROP VALUE，降级保留）。
//
// 测试库是持久实例：先 DROP TABLE + DROP TYPE 取得确定起点（同包无用例 t.Parallel，
// 销毁 channel_agent_bindings 不影响并行用例；v3.70.0 键位测试同表但串行执行）。
func TestChannelTypeEnumBridgeValuesMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDB(t, &model.ChannelAgentBinding{})
	if database == nil {
		t.Fatal("测试库不可达：枚举扩容是否收口无法判定（不 Skip，跳过等于没锁）")
	}
	ctx := context.Background()

	// 确定起点：删表（释放类型依赖）→ 删类型（有意外依赖则在此响亮失败，不 CASCADE）
	if err := database.Exec(`DROP TABLE IF EXISTS channel_agent_bindings`).Error; err != nil {
		t.Fatalf("重置 channel_agent_bindings 失败: %v", err)
	}
	if err := database.Exec(`DROP TYPE IF EXISTS channel_type_enum`).Error; err != nil {
		t.Fatalf("重置 channel_type_enum 失败（存在未预期依赖，需人工核查）: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Exec(`DELETE FROM channel_agent_bindings WHERE account_id LIKE 'acc-enum92-%'`).Error
	})

	// ① 空操作分支
	m := NewChannelTypeEnumBridgeValuesMigration(database)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("① ENUM 不存在时 Up 应空操作不报错: %v", err)
	}
	var enumExists bool
	if err := database.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'channel_type_enum')`,
	).Scan(&enumExists).Error; err != nil {
		t.Fatalf("① 查 ENUM 存在性失败: %v", err)
	}
	if enumExists {
		t.Fatal("① ENUM 不存在时 Up 不得抢先创建 channel_type_enum")
	}

	// ② 重建 v3.22.2 存量形态：建表（varchar 列）→ 跑 v3.22.2（建 12 值 ENUM + 转列）
	if err := database.AutoMigrate(&model.ChannelAgentBinding{}); err != nil {
		t.Fatalf("② 重建 channel_agent_bindings 失败: %v", err)
	}
	if err := NewEnumOptimizeMigration(database).Up(ctx); err != nil {
		t.Fatalf("② 重建 v3.22.2 存量形态失败: %v", err)
	}
	for _, v := range bridgeChannelEnumValues {
		var present bool
		if err := database.Raw(
			`SELECT EXISTS (SELECT 1 FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
			 WHERE t.typname = 'channel_type_enum' AND e.enumlabel = ?)`,
			v,
		).Scan(&present).Error; err != nil {
			t.Fatalf("② 查枚举值 %s 失败: %v", v, err)
		}
		if present {
			t.Fatalf("② 前置漂移现场不成立：v3.22.2 形态下 %s 不应存在", v)
		}
	}

	// 列已是 ENUM（后续 ⑤ 才能证明「ENUM 拒写」这一风险确实被收口）
	var dataType string
	if err := database.Raw(
		`SELECT data_type FROM information_schema.columns
		 WHERE table_name = 'channel_agent_bindings' AND column_name = 'channel_type'`,
	).Scan(&dataType).Error; err != nil {
		t.Fatalf("② 查列类型失败: %v", err)
	}
	if dataType != "USER-DEFINED" {
		t.Fatalf("② channel_agent_bindings.channel_type 应已转 ENUM，got data_type=%s", dataType)
	}

	// ③ 补五值
	if err := m.Up(ctx); err != nil {
		t.Fatalf("③ 枚举扩容失败: %v", err)
	}
	labels := enumLabelsOrFatal(t, database)
	if len(labels) != len(v3222ChannelTypeEnumValues)+len(bridgeChannelEnumValues) {
		t.Fatalf("③ 枚举值总数应 %d，got %d：%v",
			len(v3222ChannelTypeEnumValues)+len(bridgeChannelEnumValues), len(labels), labels)
	}
	for _, v := range bridgeChannelEnumValues {
		if !labels[v] {
			t.Fatalf("③ 枚举缺 %s：%v", v, labels)
		}
	}

	// ④ 幂等重入
	if err := m.Up(ctx); err != nil {
		t.Fatalf("④ 幂等重入失败: %v", err)
	}
	labels = enumLabelsOrFatal(t, database)
	if len(labels) != len(v3222ChannelTypeEnumValues)+len(bridgeChannelEnumValues) {
		t.Fatalf("④ 重入不得产生重复值：%v", labels)
	}

	// ⑤ 收口效果：桥接渠道绑定落库不再被 ENUM 拒写
	isPrimary := true
	for i, ch := range []string{"bilibili", "weibo", "jd", "personal_wx", "pdd"} {
		row := &model.ChannelAgentBinding{
			TenantID:    1,
			ChannelType: ch,
			AccountID:   "acc-enum92-" + ch,
			AgentID:     uint(i + 1), //nolint:gosec // 测试序号无溢出面
			IsPrimary:   &isPrimary,
		}
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("⑤ 绑定行落库 ChannelType=%s 被 ENUM 拒写: %v", ch, err)
		}
	}

	// ⑥ Down 降级保留（PG 无 DROP VALUE，不销毁在用枚举值）
	if err := m.Down(ctx); err != nil {
		t.Fatalf("⑥ Down 不应报错: %v", err)
	}
	labels = enumLabelsOrFatal(t, database)
	if len(labels) != len(v3222ChannelTypeEnumValues)+len(bridgeChannelEnumValues) {
		t.Fatalf("⑥ Down 后枚举值应保留：%v", labels)
	}
}

// enumLabelsOrFatal 读 channel_type_enum 现值集合。
func enumLabelsOrFatal(t *testing.T, db *gorm.DB) map[string]bool {
	t.Helper()
	var labels []string
	if err := db.Raw(
		`SELECT e.enumlabel FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
		 WHERE t.typname = 'channel_type_enum' ORDER BY e.enumsortorder`,
	).Scan(&labels).Error; err != nil {
		t.Fatalf("读 channel_type_enum 现值失败: %v", err)
	}
	set := make(map[string]bool, len(labels))
	for _, l := range labels {
		set[l] = true
	}
	return set
}

// v3222ChannelTypeEnumValues 是 v3.22.2 建枚举时的 12 值快照（与该迁移
// enums map 保持字面一致；此处复制而非引用私有变量，避免为测试改生产代码签名）。
var v3222ChannelTypeEnumValues = []string{
	"telegram", "wecom", "feishu", "whatsapp", "dingtalk",
	"douyin", "xiaohongshu", "kuaishou", "xianyu", "tiktok", "web", "web_embed",
}
