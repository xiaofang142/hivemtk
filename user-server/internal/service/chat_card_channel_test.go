package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

var expectedCardMetas = map[string]struct {
	ChannelID   string
	ChannelName string
	WidgetColor string
	WidgetTitle string
}{
	"douyin":      {"douyin_card", "抖音卡片客服", "#000000", "抖音 · 在线客服"},
	"kuaishou":    {"kuaishou_card", "快手卡片客服", "#ff5000", "快手 · 在线客服"},
	"xiaohongshu": {"xiaohongshu_card", "小红书卡片客服", "#ff2442", "小红书 · 在线客服"},
	"xianyu":      {"xianyu_card", "闲鱼卡片客服", "#ff4400", "闲鱼 · 在线客服"},
}

func TestIsCardChannelRef_KnownPlatforms(t *testing.T) {
	for platform := range expectedCardMetas {
		ref := platform + "_card"
		gotPlatform, ok := IsCardChannelRef(ref)
		if !ok {
			t.Errorf("IsCardChannelRef(%q) 期望返回 true，实际 false", ref)
			continue
		}
		if gotPlatform != platform {
			t.Errorf("IsCardChannelRef(%q) 平台期望 %q，实际 %q", ref, platform, gotPlatform)
		}
	}
}

func TestIsCardChannelRef_RejectsInvalidRefs(t *testing.T) {
	invalidRefs := []string{
		"", "default", "douyin", "kuaishou_card_123",
		"douyin_cards", "tiktok_card", "wechat_card",
		"douyin_card ", " douyin_card",
	}
	for _, ref := range invalidRefs {
		if _, ok := IsCardChannelRef(ref); ok {
			t.Errorf("IsCardChannelRef(%q) 应拒绝，但返回了 true", ref)
		}
	}
}

func TestGetOrCreateCardChannel_AllPlatforms(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ChatChannel{})
	db.SetTestDB(database)
	svc := MustNewChatChannelService(database)

	for platform, meta := range expectedCardMetas {
		t.Run(platform, func(t *testing.T) {
			ch, err := svc.GetOrCreateCardChannel(context.Background(), platform)
			if err != nil {
				t.Fatalf("GetOrCreateCardChannel(%q) 失败: %v", platform, err)
			}
			if ch.ChannelID != meta.ChannelID {
				t.Errorf("ChannelID 期望 %q，实际 %q", meta.ChannelID, ch.ChannelID)
			}
			if ch.ChannelName != meta.ChannelName {
				t.Errorf("ChannelName 期望 %q，实际 %q", meta.ChannelName, ch.ChannelName)
			}
			if ch.WidgetColor != meta.WidgetColor {
				t.Errorf("WidgetColor 期望 %q，实际 %q", meta.WidgetColor, ch.WidgetColor)
			}
			if ch.WidgetTitle != meta.WidgetTitle {
				t.Errorf("WidgetTitle 期望 %q，实际 %q", meta.WidgetTitle, ch.WidgetTitle)
			}
			if ch.Status != model.ChatChannelStatusActive {
				t.Errorf("Status 期望 active，实际 %q", ch.Status)
			}
			if !ch.AutoAssign {
				t.Error("AutoAssign 期望 true")
			}
			if ch.ConfidenceThreshold != 0.70 {
				t.Errorf("ConfidenceThreshold 期望 0.70，实际 %.2f", ch.ConfidenceThreshold)
			}
			if ch.WelcomeMessage == "" {
				t.Error("WelcomeMessage 不应为空")
			}
			t.Logf("✅ %s 渠道创建成功: id=%d, name=%s, color=%s",
				platform, ch.ID, ch.ChannelName, ch.WidgetColor)
		})
	}
}

func TestGetOrCreateCardChannel_Idempotent(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ChatChannel{})
	db.SetTestDB(database)
	svc := MustNewChatChannelService(database)

	ch1, err := svc.GetOrCreateCardChannel(context.Background(), "douyin")
	if err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	ch2, err := svc.GetOrCreateCardChannel(context.Background(), "douyin")
	if err != nil {
		t.Fatalf("二次获取失败: %v", err)
	}
	if ch1.ID != ch2.ID {
		t.Errorf("幂等性失败：首次 id=%d，二次 id=%d", ch1.ID, ch2.ID)
	}

	var count int64
	database.Model(&model.ChatChannel{}).Where("channel_id = ?", "douyin_card").Count(&count)
	if count != 1 {
		t.Errorf("douyin_card 渠道库中应有 1 条记录，实际 %d 条", count)
	}
}

func TestGetOrCreateCardChannel_RejectsUnknownPlatform(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ChatChannel{})
	db.SetTestDB(database)
	svc := MustNewChatChannelService(database)

	_, err := svc.GetOrCreateCardChannel(context.Background(), "tiktok")
	if err == nil {
		t.Fatal("GetOrCreateCardChannel 对未知平台应返回错误")
	}
	if !strings.Contains(err.Error(), "不支持的卡片平台") {
		t.Errorf("错误消息应包含'不支持的卡片平台'，实际: %v", err)
	}
}

// TestVisitorChatService_ResolveCardChannel 端到端验证：
// 访客用 {platform}_card 作为 channel_ref 调用 OpenSession 时，
// resolveChannel 应自动创建卡片渠道并成功打开会话。
func TestVisitorChatService_ResolveCardChannel(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.ChatChannel{},
		&model.QuickReply{},
		&model.SessionTag{},
		// VisitorChatService.OpenSession 会做黑名单校验，缺此表直接报
		// relation "user_blacklist" does not exist 而失败
		&model.UserBlacklist{},
	)
	db.SetTestDB(database)

	engine := NewSalesEngine(
		database,
		nil,
		nil, nil, nil, nil, nil, nil,
	)
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)
	channelSvc := MustNewChatChannelService(database)
	visitorSvc := NewVisitorChatService(context.Background(), database, channelSvc, orch, nil)

	for platform := range expectedCardMetas {
		t.Run(platform, func(t *testing.T) {
			channelRef := platform + "_card"
			visitorID := "v_card_test_" + platform
			open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
				ChannelID:   channelRef,
				VisitorID:   visitorID,
				VisitorName: platform + "访客",
			})
			if err != nil {
				t.Fatalf("OpenSession(channel=%s) 失败: %v", channelRef, err)
			}
			if open == nil || open.Session == nil {
				t.Fatal("OpenSession 返回空会话")
			}
			if open.Session.AccountID != channelRef {
				t.Errorf("会话 AccountID 期望 %q，实际 %q", channelRef, open.Session.AccountID)
			}
			if !open.IsNewSession {
				t.Error("首次打开应为新会话")
			}
			ch, err := channelSvc.GetByChannelID(context.Background(), channelRef)
			if err != nil {
				t.Fatalf("渠道创建后查询失败: %v", err)
			}
			if ch.WidgetColor != expectedCardMetas[platform].WidgetColor {
				t.Errorf("渠道 WidgetColor 期望 %q，实际 %q",
					expectedCardMetas[platform].WidgetColor, ch.WidgetColor)
			}
			t.Logf("✅ %s 渠道接入成功：session_id=%s, channel_id=%s, color=%s",
				platform, open.Session.SessionID, ch.ChannelID, ch.WidgetColor)
		})
	}
}

// TestVisitorChatService_OpenSessionWithVisitorMeta 验证：
// 访客通过卡片短链打开会话时，前端传递的 visitor_meta（含 source/card_id）
// 被正确解析并作为来源标签存入 session.Tags（JSON 数组格式）。
//
// 业务背景：
//
//	/chat/embed/{platform}_card?source=douyin&card_id=123
//	前端 Index.vue 读取 query 参数，ChatWindow.vue 把 source/card_id 组装成
//	visitor_meta JSON 字符串传给 OpenSession API。后端解析后追加到 session.Tags，
//	便于客服工作台按来源筛选会话。
func TestVisitorChatService_OpenSessionWithVisitorMeta(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.ChatChannel{},
		&model.QuickReply{},
		&model.SessionTag{},
		// VisitorChatService.OpenSession 会做黑名单校验，缺此表直接报
		// relation "user_blacklist" does not exist 而失败
		&model.UserBlacklist{},
	)
	db.SetTestDB(database)

	engine := NewSalesEngine(database, nil, nil, nil, nil, nil, nil, nil)
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)
	channelSvc := MustNewChatChannelService(database)
	visitorSvc := NewVisitorChatService(context.Background(), database, channelSvc, orch, nil)

	cases := []struct {
		name        string
		platform    string
		visitorMeta string
		wantTags    []string
	}{
		{
			name:        "douyin 卡片来源",
			platform:    "douyin",
			visitorMeta: `{"source":"douyin","card_id":"123","landing_url":"http://example.com/chat/embed/douyin_card?card_id=123"}`,
			wantTags:    []string{"web_visitor", "source:douyin", "card_id:123"},
		},
		{
			name:        "kuaishou 卡片来源",
			platform:    "kuaishou",
			visitorMeta: `{"source":"kuaishou","card_id":"456"}`,
			wantTags:    []string{"web_visitor", "source:kuaishou", "card_id:456"},
		},
		{
			name:        "无 visitor_meta（普通访客）",
			platform:    "xiaohongshu",
			visitorMeta: "",
			wantTags:    []string{"web_visitor"},
		},
		{
			name:        "visitor_meta 格式错误（容错）",
			platform:    "xianyu",
			visitorMeta: `{invalid json}`,
			wantTags:    []string{"web_visitor"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelRef := tc.platform + "_card"
			visitorID := "v_meta_test_" + tc.platform
			open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
				ChannelID:   channelRef,
				VisitorID:   visitorID,
				VisitorName: tc.platform + "访客",
				VisitorMeta: tc.visitorMeta,
			})
			if err != nil {
				t.Fatalf("OpenSession 失败: %v", err)
			}
			if open == nil || open.Session == nil {
				t.Fatal("OpenSession 返回空会话")
			}

			var gotTags []string
			if open.Session.Tags != "" {
				if err := json.Unmarshal([]byte(open.Session.Tags), &gotTags); err != nil {
					t.Fatalf("解析 session.Tags 失败: %v (raw=%s)", err, open.Session.Tags)
				}
			}
			if len(gotTags) != len(tc.wantTags) {
				t.Errorf("标签数量期望 %d，实际 %d (got=%v)", len(tc.wantTags), len(gotTags), gotTags)
				return
			}
			for i, want := range tc.wantTags {
				if gotTags[i] != want {
					t.Errorf("标签[%d] 期望 %q，实际 %q", i, want, gotTags[i])
				}
			}
			t.Logf("✅ %s：session.Tags=%v", tc.name, gotTags)
		})
	}
}
