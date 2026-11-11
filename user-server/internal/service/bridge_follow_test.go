package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// M-6（2026-11-11）：新用户（非关注好友）登记 + 自动关注策略闸门。
// 交付口径：识别信号登记恒生效（观测面）；关注动作默认关，双闸 + 信号可动作判定
// 全绿才入队动作行；全程不影响消息入库/AI 触发（本文件只测策略与登记本身）。

// fakeFollowRepo 进程内假仓储（记录插入行，供断言）。
type fakeFollowRepo struct {
	mu    sync.Mutex
	rows  []*model.BridgeFollowEvent
	dupes int
}

func (f *fakeFollowRepo) InsertIdempotent(_ context.Context, event *model.BridgeFollowEvent) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.DedupKey == event.DedupKey {
			f.dupes++
			return false, nil
		}
	}
	f.rows = append(f.rows, event)
	return true, nil
}

func (f *fakeFollowRepo) GetByDedupKey(_ context.Context, dedupKey string) (*model.BridgeFollowEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.DedupKey == dedupKey {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeFollowRepo) ListRecent(_ context.Context, limit int) ([]*model.BridgeFollowEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit > len(f.rows) {
		limit = len(f.rows)
	}
	return f.rows[:limit], nil
}

func (f *fakeFollowRepo) CountByEventTypeSince(_ context.Context, eventType string, _ time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, r := range f.rows {
		if r.EventType == eventType {
			n++
		}
	}
	return n, nil
}

func (f *fakeFollowRepo) byType(eventType string) []*model.BridgeFollowEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.BridgeFollowEvent
	for _, r := range f.rows {
		if r.EventType == eventType {
			out = append(out, r)
		}
	}
	return out
}

// swapDeliverFollowAction 注入动作行入队探针（记录调用并按需返回成功）。
func swapDeliverFollowAction(t *testing.T, record *[]string) {
	t.Helper()
	prev := deliverFollowActionFn
	deliverFollowActionFn = func(_ context.Context, channel, _accountID, conversationID, msgType, _content, _mediaURL, _eventID string) (uint64, string, error) {
		if record != nil {
			*record = append(*record, channel+"|"+conversationID+"|"+msgType)
		}
		return 1, "mh:fake_follow", nil
	}
	t.Cleanup(func() { deliverFollowActionFn = prev })
}

// setFollowUserParamStub 注入参数中心 stub（开关值可指定）。
func setFollowUserParamStub(t *testing.T, enabled bool, channels string) {
	t.Helper()
	svc := &ConfigParamService{
		cache:  make(map[string]paramEntry, 8),
		loaded: make(map[string]bool, 8),
		nowFn:  time.Now,
	}
	svc.cache["bridge."+FollowUserEnabledParam] = paramEntry{value: map[bool]string{true: "true", false: "false"}[enabled], expiresAt: time.Now().Add(time.Minute)}
	if channels != "" {
		svc.cache["bridge."+FollowUserChannelsParam] = paramEntry{value: channels, expiresAt: time.Now().Add(time.Minute)}
	}
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
}

func TestFollowSignalActionable(t *testing.T) {
	if FollowSignalActionable(model.FollowSignalMutual) {
		t.Errorf("mutual_follow 不应触发关注动作（互相关注已建立）")
	}
	for _, s := range []string{model.FollowSignalPeerFollow, model.FollowSignalNotMutual, model.FollowSignalUnfollowedTab, model.FollowSignalFriendRequest} {
		if !FollowSignalActionable(s) {
			t.Errorf("信号 %q 应可动作", s)
		}
	}
	if FollowSignalActionable("") || FollowSignalActionable("unknown_source") {
		t.Errorf("未知信号来源应保守判 false（不打动作）")
	}
}

func TestBridgeFollowService_RegisterFirstTouch_Idempotent(t *testing.T) {
	repo := &fakeFollowRepo{}
	svc := NewBridgeFollowService(repo)
	ctx := context.Background()
	ev := &model.MessageEvent{
		EventID:        "evt-m6-1",
		Channel:        "douyin",
		ConversationID: "conv-m6-1",
		SenderType:     "system",
		SenderName:     "新用户甲",
		Extra: map[string]any{
			"account_id":          "acc-1",
			"new_user_unfollowed": true,
			"new_user_signal":     model.FollowSignalPeerFollow,
		},
	}
	inserted, err := svc.RegisterUnfollowedFirstTouch(ctx, ev, model.FollowSignalPeerFollow)
	if err != nil || !inserted {
		t.Fatalf("首次登记失败: inserted=%v err=%v", inserted, err)
	}
	inserted2, _ := svc.RegisterUnfollowedFirstTouch(ctx, ev, model.FollowSignalPeerFollow)
	if inserted2 {
		t.Errorf("同会话二次登记不应新插入（dedup_key 幂等）")
	}
	rows := repo.byType(model.BridgeFollowEventFirstTouch)
	if len(rows) != 1 {
		t.Fatalf("首触行数 = %d，期望 1", len(rows))
	}
	row := rows[0]
	if row.DedupKey != firstTouchDedupKey("douyin", "conv-m6-1") {
		t.Errorf("DedupKey = %q，期望 uft:douyin:conv-m6-1 形状", row.DedupKey)
	}
	if row.Signal != model.FollowSignalPeerFollow || row.Result != "registered" {
		t.Errorf("Signal/Result = %q/%q，形状不符", row.Signal, row.Result)
	}
	if row.AccountID != "acc-1" {
		t.Errorf("AccountID = %q，期望 acc-1（Extra 提取）", row.AccountID)
	}
}

// maybeRegisterNewUserUnfollowed 策略闸门：默认关 → 只登记不触发；开关开 + 渠道在
// 清单 + 信号可动作 → 异步入队动作行；互关信号 → 登记但不触发。
func TestMaybeRegisterNewUserUnfollowed_PolicyGate(t *testing.T) {
	ctx := context.Background()
	newEvent := func(signal string, channel string) *model.MessageEvent {
		return &model.MessageEvent{
			EventID:        "evt-gate",
			Channel:        channel,
			ConversationID: "conv-gate",
			SenderType:     "system",
			SenderName:     "新用户乙",
			Extra: map[string]any{
				"account_id":          "acc-2",
				"new_user_unfollowed": true,
				"new_user_signal":     signal,
			},
		}
	}

	t.Run("默认关：只登记首触，不触发动作行", func(t *testing.T) {
		setFollowUserParamStub(t, false, "")
		repo := &fakeFollowRepo{}
		svc := NewInboxIngressService()
		svc.SetBridgeFollowService(NewBridgeFollowService(repo))
		var delivered []string
		swapDeliverFollowAction(t, &delivered)
		svc.maybeRegisterNewUserUnfollowed(ctx, newEvent(model.FollowSignalPeerFollow, "douyin"))
		if len(repo.byType(model.BridgeFollowEventFirstTouch)) != 1 {
			t.Fatalf("首触应登记 1 条")
		}
		if len(repo.byType(model.BridgeFollowEventFollowAction)) != 0 || len(delivered) != 0 {
			t.Fatalf("开关默认关时不得触发动作行：delivered=%v actions=%d", delivered, len(repo.byType(model.BridgeFollowEventFollowAction)))
		}
	})

	t.Run("开关开+渠道在清单+可动作：登记并异步入队动作行", func(t *testing.T) {
		setFollowUserParamStub(t, true, `["douyin","tiktok","bilibili"]`)
		repo := &fakeFollowRepo{}
		svc := NewInboxIngressService()
		svc.SetBridgeFollowService(NewBridgeFollowService(repo))
		var delivered []string
		swapDeliverFollowAction(t, &delivered)
		svc.maybeRegisterNewUserUnfollowed(ctx, newEvent(model.FollowSignalNotMutual, "douyin"))
		// 动作经 utils.SafeGo 异步旁路：轮询等待（上限 2s），失败即主链路被阻塞的证据
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if len(delivered) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(delivered) != 1 {
			t.Fatalf("动作行应入队 1 次，实际 %d（次）", len(delivered))
		}
		if got := delivered[0]; got != "douyin|conv-gate|"+FollowActionMsgType {
			t.Errorf("动作行形状 = %q，期望 douyin|conv-gate|%s", got, FollowActionMsgType)
		}
		if len(repo.byType(model.BridgeFollowEventFollowAction)) != 1 {
			t.Fatalf("动作审计行应登记 1 条")
		}
	})

	t.Run("互关信号：登记但不触发动作", func(t *testing.T) {
		setFollowUserParamStub(t, true, `["douyin","tiktok","bilibili"]`)
		repo := &fakeFollowRepo{}
		svc := NewInboxIngressService()
		svc.SetBridgeFollowService(NewBridgeFollowService(repo))
		var delivered []string
		swapDeliverFollowAction(t, &delivered)
		svc.maybeRegisterNewUserUnfollowed(ctx, newEvent(model.FollowSignalMutual, "douyin"))
		time.Sleep(150 * time.Millisecond)
		if len(repo.byType(model.BridgeFollowEventFirstTouch)) != 1 {
			t.Fatalf("互关信号同样登记首触")
		}
		if len(delivered) != 0 {
			t.Fatalf("互关已建立不应触发关注动作")
		}
	})

	t.Run("渠道不在清单：不触发动作", func(t *testing.T) {
		setFollowUserParamStub(t, true, `["bilibili"]`)
		repo := &fakeFollowRepo{}
		svc := NewInboxIngressService()
		svc.SetBridgeFollowService(NewBridgeFollowService(repo))
		var delivered []string
		swapDeliverFollowAction(t, &delivered)
		svc.maybeRegisterNewUserUnfollowed(ctx, newEvent(model.FollowSignalNotMutual, "xianyu"))
		time.Sleep(150 * time.Millisecond)
		if len(delivered) != 0 {
			t.Fatalf("渠道不在 follow_user_channels 清单不应触发")
		}
	})

	t.Run("未标记消息：完全 no-op", func(t *testing.T) {
		setFollowUserParamStub(t, true, `["douyin"]`)
		repo := &fakeFollowRepo{}
		svc := NewInboxIngressService()
		svc.SetBridgeFollowService(NewBridgeFollowService(repo))
		var delivered []string
		swapDeliverFollowAction(t, &delivered)
		svc.maybeRegisterNewUserUnfollowed(ctx, &model.MessageEvent{
			EventID: "evt-plain", Channel: "douyin", ConversationID: "conv-plain",
			Extra: map[string]any{"account_id": "acc-3"},
		})
		time.Sleep(50 * time.Millisecond)
		if len(repo.rows) != 0 || len(delivered) != 0 {
			t.Fatalf("未标记消息不得登记/触发：rows=%d delivered=%d", len(repo.rows), len(delivered))
		}
	})

	t.Run("未装配 bridgeFollow：no-op（nil 安全）", func(t *testing.T) {
		svc := NewInboxIngressService()
		svc.maybeRegisterNewUserUnfollowed(ctx, newEvent(model.FollowSignalPeerFollow, "douyin")) // 不应 panic
	})
}
