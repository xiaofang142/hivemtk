package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.86.0（B2 撤回状态格）验收判据：
//
//	① 两列都能被 Up 补出来（先在测试库里把列删掉，模拟"存量库"）；
//	② 幂等重入：第二次 Up 不报错；
//	③ 双路同形：迁移补出来的列形状与模型 tag（AutoMigrate）一致——
//	   recall_error 非空默认 ''，recalled_at 可空；
//	④ 无回填：存量行的 recalled_at 保持 NULL（"不知道撤没撤过"不许写成别的值）；
//	⑤ Down 拒绝删列（撤回事实降级不销毁）。
func TestMessageHubRecallMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	ctx := context.Background()

	// 存量库模拟：AutoMigrate 已按新模型建了列，先删掉，让 Up 走 ADD COLUMN 那条路
	for _, col := range []string{"recalled_at", "recall_error"} {
		if err := database.Migrator().DropColumn(&model.MessageHub{}, col); err != nil {
			t.Fatalf("预置删除 %s 列失败: %v", col, err)
		}
		if database.Migrator().HasColumn(&model.MessageHub{}, col) {
			t.Fatalf("预置失败：%s 列仍在", col)
		}
	}

	m := NewMessageHubRecallMigration(database)
	// ① 补列
	if err := m.Up(ctx); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// ② 幂等重入
	if err := m.Up(ctx); err != nil {
		t.Fatalf("幂等重入失败: %v", err)
	}
	for _, col := range []string{"recalled_at", "recall_error"} {
		if !database.Migrator().HasColumn(&model.MessageHub{}, col) {
			t.Fatalf("① message_hub.%s 列缺失", col)
		}
	}

	// ③ 形状：recall_error 非空默认空串（读侧按 = '' 判"没有失败原因"）
	var notNull string
	var colDefault *string
	if err := database.Raw(`
		SELECT is_nullable, column_default FROM information_schema.columns
		WHERE table_name = 'message_hub' AND column_name = 'recall_error'`).
		Row().Scan(&notNull, &colDefault); err != nil {
		t.Fatalf("读 recall_error 列元数据: %v", err)
	}
	if notNull != "NO" {
		t.Fatalf("③ recall_error 应为 NOT NULL，got is_nullable=%s", notNull)
	}

	// ④ 无回填 + 默认值生效：插入一行不带这两列数据，recalled_at 必须是 NULL
	if err := database.Exec(`DELETE FROM message_hub`).Error; err != nil {
		t.Fatalf("清空: %v", err)
	}
	if err := database.Create(&model.MessageHub{
		Platform: "telegram", AccountID: "acc1", MsgID: "mh-mig-1",
		ConversationID: "conv-mig-1", Direction: "outbound", Status: "sent",
		MsgType: "text", Content: "x",
	}).Error; err != nil {
		t.Fatalf("插入存量形态行: %v", err)
	}
	var recalledAt *string
	var recallErr string
	if err := database.Raw(`SELECT recalled_at::text, recall_error FROM message_hub WHERE msg_id = 'mh-mig-1'`).
		Row().Scan(&recalledAt, &recallErr); err != nil {
		t.Fatalf("回读存量行: %v", err)
	}
	if recalledAt != nil {
		t.Fatalf("④ 存量行 recalled_at 被回填成 %q：撤回状态本应保持未知", *recalledAt)
	}
	if recallErr != "" {
		t.Fatalf("③ recall_error 默认值不是空串: %q", recallErr)
	}

	// ⑤ Down 不删列
	if err := m.Down(ctx); err != nil {
		t.Fatalf("⑤ Down 应拒绝删列而不报错: %v", err)
	}
	for _, col := range []string{"recalled_at", "recall_error"} {
		if !database.Migrator().HasColumn(&model.MessageHub{}, col) {
			t.Fatalf("⑤ Down 不得销毁在用的 message_hub.%s 列", col)
		}
	}
}
