package ragcustomerservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newSessionForTest(t *testing.T, cfg SessionConfig) (*InMemoryDialogManager, *Session) {
	t.Helper()
	dm := NewInMemoryDialogManager(&DialogManagerConfig{
		DefaultMaxHistoryLength: 3,
		DefaultSessionTimeout:   time.Hour,
		SessionCleanupInterval:  time.Hour, // 拉长后台清理周期，避免与本用例竞态
	})
	s, err := dm.CreateSession(context.Background(), "u-1", "wecom", "kb-1", cfg)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return dm, s
}

func TestCreateSessionAppliesDefaultsAndValidates(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	if s.Config.MaxHistoryLength != 3 {
		t.Errorf("MaxHistoryLength 应回填默认 3, got %d", s.Config.MaxHistoryLength)
	}
	if s.Config.Timeout != 3600 {
		t.Errorf("Timeout 应回填默认 3600s, got %d", s.Config.Timeout)
	}
	if s.Status != SessionActive || s.Conversation == nil || s.Metadata["last_activity"] == nil {
		t.Errorf("新建会话形态错: %+v", s)
	}

	// 显式配置不得被默认值覆盖
	s2, err := dm.CreateSession(context.Background(), "u-2", "tg", "kb-2", SessionConfig{MaxHistoryLength: 9, Timeout: 30})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s2.Config.MaxHistoryLength != 9 || s2.Config.Timeout != 30 {
		t.Errorf("显式配置被改写: %+v", s2.Config)
	}

	for _, tc := range []struct{ name, user, platform, kb string }{
		{"空 user", "", "wx", "kb"}, {"空 platform", "u", "", "kb"}, {"空 kb", "u", "wx", ""},
	} {
		if _, err := dm.CreateSession(context.Background(), tc.user, tc.platform, tc.kb, SessionConfig{}); err == nil {
			t.Errorf("%s 应返回错误", tc.name)
		}
	}
}

func TestAddMessageTrimsToMaxHistory(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		msg := Message{ID: "m", Role: MessageRoleUser, Content: strings.Repeat("x", i+1)}
		if err := dm.AddMessage(ctx, s.ID, msg); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	conv, err := dm.GetConversationHistory(ctx, s.ID, 0)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(conv.Messages) != 3 {
		t.Fatalf("应裁剪到 MaxHistoryLength=3, got %d", len(conv.Messages))
	}
	if conv.Messages[0].Content != "xxx" || conv.Messages[2].Content != "xxxxx" {
		t.Errorf("保留的应是最后 3 条: %q", []string{conv.Messages[0].Content, conv.Messages[2].Content})
	}
	if conv.Metadata["last_message_time"] == nil {
		t.Error("AddMessage 应写入 last_message_time")
	}
	if conv.Messages[0].Timestamp.IsZero() {
		t.Error("零值 Timestamp 应被补全为 now")
	}

	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := dm.AddMessage(ctx, s.ID, Message{Content: "带时间戳", Timestamp: fixed}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	conv2, _ := dm.GetConversationHistory(ctx, s.ID, 0)
	if got := conv2.Metadata["last_message_time"]; got != fixed {
		t.Errorf("显式 Timestamp 应原样写入 metadata, got %v", got)
	}
}

func TestAddMessageAndHistoryErrorBranches(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	if err := dm.AddMessage(ctx, "", Message{}); err == nil {
		t.Error("空 sessionID 应报错")
	}
	if err := dm.AddMessage(ctx, "nope", Message{}); err == nil {
		t.Error("不存在的会话应报错")
	}
	if _, err := dm.GetConversationHistory(ctx, "", 5); err == nil {
		t.Error("空 sessionID 查询历史应报错")
	}
	if _, err := dm.GetConversationHistory(ctx, "nope", 5); err == nil {
		t.Error("不存在会话查询历史应报错")
	}

	if err := dm.CloseSession(ctx, s.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if err := dm.AddMessage(ctx, s.ID, Message{Content: "迟到"}); err == nil {
		t.Error("已关闭会话不应再接收消息")
	}
}

func TestGetConversationHistoryLimitTakesTail(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{MaxHistoryLength: 10})
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if err := dm.AddMessage(ctx, s.ID, Message{Role: MessageRoleAssistant, Content: string(rune('a' + i))}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	conv, err := dm.GetConversationHistory(ctx, s.ID, 2)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(conv.Messages) != 2 || conv.Messages[0].Content != "e" || conv.Messages[1].Content != "f" {
		t.Errorf("limit=2 应取最后两条, got %d 条 %q", len(conv.Messages), contents(conv.Messages))
	}
	if all, _ := dm.GetConversationHistory(ctx, s.ID, 0); len(all.Messages) != 6 {
		t.Errorf("limit<=0 应返回全量, got %d", len(all.Messages))
	}
	if over, _ := dm.GetConversationHistory(ctx, s.ID, 99); len(over.Messages) != 6 {
		t.Errorf("limit 超过长度应返回全量, got %d", len(over.Messages))
	}
	if cur, _ := dm.GetSession(ctx, s.ID); len(cur.Conversation.Messages) != 6 {
		t.Error("读取历史不得截断会话内存储")
	}
}

func contents(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func TestUpdateSessionMetadataMerges(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	before := s.UpdatedAt
	if err := dm.UpdateSessionMetadata(ctx, s.ID, map[string]any{"stage": "quote"}); err != nil {
		t.Fatalf("UpdateSessionMetadata: %v", err)
	}
	if err := dm.UpdateSessionMetadata(ctx, s.ID, map[string]any{"owner": "agent-1", "last_activity": before}); err != nil {
		t.Fatalf("UpdateSessionMetadata: %v", err)
	}
	got, _ := dm.GetSession(ctx, s.ID)
	if got.Metadata["stage"] != "quote" || got.Metadata["owner"] != "agent-1" {
		t.Errorf("元数据未合并: %v", got.Metadata)
	}
	if !got.UpdatedAt.After(before) {
		t.Error("更新元数据应刷新 UpdatedAt")
	}

	if err := dm.UpdateSessionMetadata(ctx, "", map[string]any{"k": 1}); err == nil {
		t.Error("空 sessionID 应报错")
	}
	if err := dm.UpdateSessionMetadata(ctx, s.ID, map[string]any{}); err == nil {
		t.Error("空 metadata 应报错")
	}
	if err := dm.UpdateSessionMetadata(ctx, "nope", map[string]any{"k": 1}); err == nil {
		t.Error("不存在会话应报错")
	}

	orphan := &Session{ID: "manual", Metadata: nil}
	dm.sessions["manual"] = orphan
	if err := dm.UpdateSessionMetadata(ctx, "manual", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("nil Metadata 会话应先建容器再写: %v", err)
	}
	if orphan.Metadata["k"] != "v" {
		t.Errorf("nil Metadata 分支失效: %v", orphan.Metadata)
	}
}

func TestCleanupExpiredSessionsRemovesOnlyStale(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{Timeout: 1}) // 1 秒超时
	ctx := context.Background()

	fresh, err := dm.CreateSession(ctx, "u-2", "wecom", "kb-1", SessionConfig{Timeout: 3600})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// 手工把 s 的最后活跃时间推到 1 小时前
	dm.sessions[s.ID].Metadata["last_activity"] = time.Now().Add(-time.Hour)
	// 无 last_activity 的会话应被忽略（不属过期判定范围）
	noActivity := &Session{ID: "no-activity", UserID: "u-3", Config: SessionConfig{Timeout: 1}, Metadata: map[string]any{}}
	dm.sessions["no-activity"] = noActivity

	if err := dm.CleanupExpiredSessions(ctx); err != nil {
		t.Fatalf("CleanupExpiredSessions: %v", err)
	}
	if _, ok := dm.sessions[s.ID]; ok {
		t.Error("过期会话应被删除")
	}
	if _, ok := dm.sessions[fresh.ID]; !ok {
		t.Error("活跃会话不应被删除")
	}
	if _, ok := dm.sessions[noActivity.ID]; !ok {
		t.Error("缺 last_activity 的会话不应被删除")
	}
}

func TestListUserSessionsFilters(t *testing.T) {
	dm, _ := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	// 平台互不相同：同 (user,platform) 连建两个会话会因 generateSessionID 时钟粒度相撞而互相覆盖
	for _, spec := range []struct{ user, platform string }{
		{"u-1", "wecom2"}, {"u-1", "telegram"}, {"u-2", "wecom2"},
	} {
		if _, err := dm.CreateSession(ctx, spec.user, spec.platform, "kb-x", SessionConfig{}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	if _, err := dm.ListUserSessions(ctx, "", "wecom", SessionActive); err == nil {
		t.Error("空 userID 应报错")
	}

	all, err := dm.ListUserSessions(ctx, "u-1", "", "")
	if err != nil {
		t.Fatalf("ListUserSessions: %v", err)
	}
	if len(all) != 3 { // 本用例 u-1 共 1(初始)+2 个
		t.Fatalf("按用户过滤结果=%d want 3", len(all))
	}
	byPlatform, _ := dm.ListUserSessions(ctx, "u-1", "telegram", "")
	if len(byPlatform) != 1 || byPlatform[0].Platform != "telegram" {
		t.Errorf("按平台过滤错: %d 条", len(byPlatform))
	}
	closed, _ := dm.ListUserSessions(ctx, "u-1", "telegram", SessionClosed)
	if len(closed) != 0 {
		t.Errorf("状态过滤错: %d", len(closed))
	}
	if err := dm.CloseSession(ctx, byPlatform[0].ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	nowClosed, _ := dm.ListUserSessions(ctx, "u-1", "telegram", SessionClosed)
	if len(nowClosed) != 1 || nowClosed[0].Metadata["closed_at"] == nil {
		t.Errorf("关闭后应按状态查到且写入 closed_at: %+v", nowClosed)
	}
	if none, _ := dm.ListUserSessions(ctx, "ghost", "", ""); len(none) != 0 {
		t.Errorf("无匹配用户应返回空, got %d", len(none))
	}
}

func TestCloseSessionErrorBranchesAndActivity(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	if err := dm.CloseSession(ctx, ""); err == nil {
		t.Error("空 sessionID 应报错")
	}
	if err := dm.CloseSession(ctx, "nope"); err == nil {
		t.Error("不存在会话应报错")
	}

	old := s.UpdatedAt
	time.Sleep(2 * time.Millisecond)
	if _, err := dm.GetSession(ctx, s.ID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	after, _ := dm.GetSession(ctx, s.ID)
	if !after.UpdatedAt.After(old) {
		t.Error("GetSession 应刷新活跃时间")
	}
	dm.updateLastActivity("不存在的会话") // 覆盖 exists=false 分支，不应 panic
}

func TestGenerateSessionIDShape(t *testing.T) {
	a := generateSessionID("u1", "wx")
	b := generateSessionID("u1", "wx")
	if !strings.HasPrefix(a, "u1_wx_") {
		t.Errorf("ID 前缀错: %q", a)
	}
	if a == b {
		t.Error("纳秒后缀应保证同参不同 ID")
	}
}
