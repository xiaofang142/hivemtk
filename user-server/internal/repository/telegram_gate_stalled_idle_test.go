// ListStalledRestricted 的"请求内路径已经不在这一行上"这一条取数口径。
//
// 守的不变量：同一次入群只许播报一次。入群请求内路径（HandleNewMembers）从写台账到
// 收尾（登记送达或计一次失败）之间夹两次 TG 往返，那段时间这一行同样满足
// "join_status=restricted + 未验证 + welcome_sent_at IS NULL + 窗口开着"，
// 补偿循环照旧口径捞取就会给同一个人再禁言 + 再播报一次（线上实测同一次入群三条提示，
// 两次 restrictChatMember 相隔 8 秒）。
//
// 这条判据必须是**延迟**而不是"年轻就永久跳过"：请求内路径真的发失败时（TG 抖动、
// Bot 被移出群），它在最后一笔写入里把 welcome_resends 顶到 >0，补偿当轮就该接手。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 安静窗口内的行不捞；跨过窗口、已交棒、或压根没有写入时刻的行照捞。
func TestTelegramMember_ListStalledRestrictedIdleWindow(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupMember{})
	repo := NewTelegramGroupMemberRepositoryWithDB(db)
	ctx := context.Background()

	now := time.Now()
	idle := 2 * time.Minute // 与 service 侧的"请求内安静窗口"同量级；本枚只按参数判定
	exp := now.Add(10 * time.Minute)

	seed := func(userID string, resends int, updatedAt *time.Time) *model.TelegramGroupMember {
		t.Helper()
		m := &model.TelegramGroupMember{
			AccountID: 1, ChatID: "idle-window", UserID: userID, FullName: userID,
			JoinStatus: model.TGMemberRestricted, JoinMode: "mute_unlock",
			Authorized: false, VerifyToken: "tok_" + userID, ExpiresAt: &exp,
			WelcomeResends: resends,
		}
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("seed %s 失败: %v", userID, err)
		}
		// UpdateColumn 才躲得开 autoUpdateTime：Update 会把这一列顶回 now，
		// 于是"变老了"这个前提根本没被种进去。
		res := db.Model(&model.TelegramGroupMember{}).Where("id = ?", m.ID)
		var r *gorm.DB
		if updatedAt == nil {
			r = res.UpdateColumn("updated_at", gorm.Expr("NULL"))
		} else {
			r = res.UpdateColumn("updated_at", *updatedAt)
		}
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatalf("种 updated_at 失败: rows=%d err=%v", r.RowsAffected, r.Error)
		}
		return m
	}

	seed("in-flight", 0, ptr(now))                             // 刚被请求内路径写下 ⇒ 不该捞
	seed("in-flight-old", 0, ptr(now.Add(-time.Second)))       // 同上，窗口内一秒也不放
	seed("idle", 0, ptr(now.Add(-idle-time.Second)))           // 跨过安静窗口 ⇒ 捞
	seed("handed-off", 1, ptr(now))                            // 请求内路径计了失败并交棒 ⇒ 捞
	seed("no-stamp", 0, nil)                                   // 无写入时刻（手工/迁移）⇒ 捞
	seed("delivered-idle", 0, ptr(now.Add(-idle-time.Minute))) // 已送达 ⇒ 依旧不捞（必要条件没松）
	if err := db.Model(&model.TelegramGroupMember{}).
		Where("chat_id = ? AND user_id = ?", "idle-window", "delivered-idle").
		UpdateColumn("welcome_sent_at", now.Add(-idle)).Error; err != nil {
		t.Fatalf("补记送达失败: %v", err)
	}

	got, err := repo.ListStalledRestricted(ctx, now, now.Add(-idle), 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range got {
		seen[m.UserID] = true
	}
	want := map[string]bool{"idle": true, "handed-off": true, "no-stamp": true}
	if len(got) != len(want) {
		t.Fatalf("命中集合应=%v, 实际=%v", want, seen)
	}
	for u := range want {
		if !seen[u] {
			t.Fatalf("%s 应被捞出来补发, 实际命中=%v", u, seen)
		}
	}
	for _, u := range []string{"in-flight", "in-flight-old", "delivered-idle"} {
		if seen[u] {
			t.Fatalf("%s 不该被捞（安静窗口内 / 已送达）, 实际命中=%v", u, seen)
		}
	}
}

func ptr[T any](v T) *T { return &v }
