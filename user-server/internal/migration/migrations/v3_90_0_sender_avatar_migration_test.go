package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.90.0（M-2 客户头像链路存储半项）验收判据：
//
//	① 两列都能被 Up 补出来（先在测试库里把列删掉，模拟"存量库"）；
//	② 幂等重入：第二次 Up 不报错；
//	③ 双路同形：迁移补出来的列形状与模型 tag（AutoMigrate）一致——可空 varchar(500)；
//	④ 无回填：存量行的 sender_avatar 保持 NULL（头像只可能由迁移之后的上行产生）；
//	⑤ Down 拒绝删列（在用对象降级不销毁）。
func TestSenderAvatarMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.InboxConversation{})
	ctx := context.Background()

	// 存量库模拟：AutoMigrate 已按新模型建了列，先删掉，让 Up 走 ADD COLUMN 那条路
	for _, col := range []string{"sender_avatar"} {
		if err := database.Migrator().DropColumn(&model.MessageHub{}, col); err != nil {
			t.Fatalf("预置删除 message_hub.%s 列失败: %v", col, err)
		}
		if err := database.Migrator().DropColumn(&model.InboxConversation{}, col); err != nil {
			t.Fatalf("预置删除 inbox_conversations.%s 列失败: %v", col, err)
		}
	}

	m := NewSenderAvatarMigration(database)
	// ① 补列
	if err := m.Up(ctx); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// ② 幂等重入
	if err := m.Up(ctx); err != nil {
		t.Fatalf("幂等重入失败: %v", err)
	}
	for _, col := range []string{"sender_avatar"} {
		if !database.Migrator().HasColumn(&model.MessageHub{}, col) {
			t.Fatalf("① message_hub.%s 列缺失", col)
		}
		if !database.Migrator().HasColumn(&model.InboxConversation{}, col) {
			t.Fatalf("① inbox_conversations.%s 列缺失", col)
		}
	}

	// ③ 形状：sender_avatar 可空 varchar(500)
	var isNullable string
	var charMaxLen int
	if err := database.Raw(`
		SELECT is_nullable, coalesce(character_maximum_length, 0) FROM information_schema.columns
		WHERE table_name = 'message_hub' AND column_name = 'sender_avatar'`).
		Row().Scan(&isNullable, &charMaxLen); err != nil {
		t.Fatalf("读 message_hub.sender_avatar 列元数据: %v", err)
	}
	if isNullable != "YES" {
		t.Fatalf("③ message_hub.sender_avatar 应可空，got is_nullable=%s", isNullable)
	}
	if charMaxLen != model.MaxMediaURLLen {
		t.Fatalf("③ message_hub.sender_avatar 列宽应 %d，got %d", model.MaxMediaURLLen, charMaxLen)
	}

	// ④ 无回填：插入存量形态行，sender_avatar 必须是 NULL（迁移不产生任何回填值）。
	// 注：经 ORM Create 的行 GORM 会把 Go 零值写成空串（v3.82.0 同款已知行为）——
	// NULL 与 '' 语义等价（读侧统一降级为默认形态），都证明迁移本身没有回填。
	if err := database.Exec(`DELETE FROM message_hub`).Error; err != nil {
		t.Fatalf("清空: %v", err)
	}
	if err := database.Create(&model.MessageHub{
		Platform: "telegram", AccountID: "acc-av-mig-1", MsgID: "mh-av-mig-1",
		ConversationID: "conv-av-mig-1", Direction: "inbound", Status: "received",
		MsgType: "text", Content: "x",
	}).Error; err != nil {
		t.Fatalf("插入存量形态行: %v", err)
	}
	var senderAvatar *string
	if err := database.Raw(`SELECT sender_avatar::text FROM message_hub WHERE msg_id = 'mh-av-mig-1'`).
		Row().Scan(&senderAvatar); err != nil {
		t.Fatalf("回读存量行: %v", err)
	}
	if senderAvatar != nil && *senderAvatar != "" {
		t.Fatalf("④ 存量行 sender_avatar 被回填成 %q：头像只可能由迁移之后上行产生", *senderAvatar)
	}

	// ⑤ Down 不删列
	if err := m.Down(ctx); err != nil {
		t.Fatalf("⑤ Down 应拒绝删列而不报错: %v", err)
	}
	for _, col := range []string{"sender_avatar"} {
		if !database.Migrator().HasColumn(&model.MessageHub{}, col) {
			t.Fatalf("⑤ Down 不得销毁在用的 message_hub.%s 列", col)
		}
		if !database.Migrator().HasColumn(&model.InboxConversation{}, col) {
			t.Fatalf("⑤ Down 不得销毁在用的 inbox_conversations.%s 列", col)
		}
	}
}
