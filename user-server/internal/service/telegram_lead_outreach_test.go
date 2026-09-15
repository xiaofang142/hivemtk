package service

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/cache"
)

func dmOutreachTestCleanup(t *testing.T, accountID string, userID int64, groupID string) {
	t.Helper()
	ctx := context.Background()
	keys := []string{
		fmt.Sprintf("mtk:tg:dm_outreach:%s:%d:%s", accountID, userID, groupID),
		fmt.Sprintf("mtk:tg:outreach:%s:%s:%d", accountID, groupID, userID),
		fmt.Sprintf("mtk:tg:outreach:%s:dm:%d", accountID, userID),
	}
	for _, k := range keys {
		_ = cache.GetGlobalCache().Delete(ctx, k)
	}
}

// TestDMOutreachCooldown_FirstAllowed_SecondBlocked
// R-3b: DM 触达双层冷却之第二层——首次放行，冷却窗口内第二次被拦截
func TestDMOutreachCooldown_FirstAllowed_SecondBlocked(t *testing.T) {
	ctx := context.Background()
	svc := &TelegramDMOutreachService{}
	const (
		acc = "1"
		uid = int64(778100)
		gid = "-100cooldown"
	)
	dmOutreachTestCleanup(t, acc, uid, gid)
	dmOutreachTestCleanup(t, acc, uid+1, gid)
	defer dmOutreachTestCleanup(t, acc, uid, gid)
	defer dmOutreachTestCleanup(t, acc, uid+1, gid)

	if !svc.dmOutreachCooldownAllowed(ctx, acc, uid, gid) {
		t.Fatal("首次调用应被允许")
	}
	if svc.dmOutreachCooldownAllowed(ctx, acc, uid, gid) {
		t.Fatal("冷却期内第二次调用应被拦截")
	}
	if !svc.dmOutreachCooldownAllowed(ctx, acc, uid+1, gid) {
		t.Fatal("不同用户不应被连带拦截")
	}
}

// TestTriggerDMOutreach_DMKeySetOnFirstCall
// R-3b: 完整调用链验证——首次 TriggerDMOutreach 落下 DM 冷却 key，
// 第二次同 (账号,用户,群) 调用在冷却层被拦截
func TestTriggerDMOutreach_DMKeySetOnFirstCall(t *testing.T) {
	ctx := context.Background()
	ws := &WebhookService{}
	svc := NewTelegramDMOutreachService(ws)
	const (
		acc = "1"
		uid = int64(778101)
		gid = "-100fullpath"
	)
	dmOutreachTestCleanup(t, acc, uid, gid)
	defer dmOutreachTestCleanup(t, acc, uid, gid)

	svc.TriggerDMOutreach(ctx, acc, uid, gid, "群", 80, true, "你好")

	dmKey := fmt.Sprintf("mtk:tg:dm_outreach:%s:%d:%s", acc, uid, gid)
	if _, err := cache.GetGlobalCache().Get(ctx, dmKey); err != nil {
		t.Fatalf("首次触发后应存在 DM 冷却 key %s: %v", dmKey, err)
	}

	svc.TriggerDMOutreach(ctx, acc, uid, gid, "群", 95, true, "再来一条")

	set, err := cache.GetGlobalCache().SetNX(ctx, dmKey, "1", tgDMOutreachCooldown)
	if err != nil {
		t.Fatalf("探针 SetNX 失败: %v", err)
	}
	if set {
		t.Fatal("第二次触发未经过冷却（key 不应仍存活）")
	}
}
