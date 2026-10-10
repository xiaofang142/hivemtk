package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 访客从 embed 窗口"查看历史会话"列表点开某一条会话时，必须能换回**那一条**的
// visitor_token。
//
// 为什么要测这个：visitor_token 是签在 (channel, visitor, session) 三元组上的无状态
// HMAC，而原先只有 OpenSession 会签发，OpenSession 又只会命中"最近活跃的那一条"。
// 前端 resumeSession 只换了 sessionId、没换 token ⇒ 列表里凡是更早的会话，它的
// messages / offline-messages / close / rate 全部会被 validateVisitorTokenOrAbort 挡成
// 403。真机走查 /chat/embed/default 抓到过这一腿的形态：点历史项 → 窗口是空的；
// 点右上角 × → POST /sessions/{id}/close 回 403。
//
// 用例先立"旧 token 确实打不开这条会话"这一格（否则整份用例只是在测一条本来就通的
// 路径，换证这件事没有任何判据），再验 ExchangeVisitorToken 把它打开。
// 归属那几格是安全判据：这个端点不能变成"知道 session_id 就能领证"的越权入口。

func newVisitorTokenTestService(t *testing.T) (*VisitorChatService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.ChatChannel{},
		&model.QuickReply{},
		&model.SessionTag{},
		// OpenSession 有黑名单前置校验，缺这张表会直接报 relation 不存在
		&model.UserBlacklist{},
		&model.CSATSurvey{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	channelSvc := MustNewChatChannelService(database)
	svc := NewVisitorChatService(context.Background(), database, channelSvc, nil, NewChannelAgentBindingService())
	return svc, database
}

// openFreshSession 建一条属于该访客的会话，返回它的 session_id 与随发的 token。
// Resume=false：否则第二次 open 会把第一条续回来，两条会话会撞成同一个 id。
func openFreshSession(t *testing.T, svc *VisitorChatService, channelID, visitorID string) (string, string) {
	t.Helper()
	res, err := svc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID:   channelID,
		VisitorID:   visitorID,
		VisitorName: "换证用例访客",
		Resume:      false,
	})
	if err != nil {
		t.Fatalf("OpenSession(visitor=%s) 失败: %v", visitorID, err)
	}
	if res.Session == nil || res.VisitorToken == "" {
		t.Fatalf("OpenSession 未返回会话或 token：%+v", res)
	}
	return res.Session.SessionID, res.VisitorToken
}

func TestVisitorChatService_ExchangeVisitorToken(t *testing.T) {
	svc, database := newVisitorTokenTestService(t)
	ctx := context.Background()
	const channelID = "default"

	stamp := time.Now().Format("150405.000")
	visitorA := "v_exchange_a_" + stamp
	visitorB := "v_exchange_b_" + stamp

	olderID, _ := openFreshSession(t, svc, channelID, visitorA)
	newerID, newerToken := openFreshSession(t, svc, channelID, visitorA)
	if olderID == newerID {
		t.Fatalf("两次 OpenSession 拿到同一条会话，用例前提不成立（id=%s）", olderID)
	}

	// 把早的那条置成已结束：recent-closed 列表只认 resolved/closed，
	// 而"访客从列表里点它"正是这条链路唯一的入口。
	if e := database.Model(&model.CustomerSession{}).
		Where("session_id = ?", olderID).
		Update("status", model.SessionStatusClosed).Error; e != nil {
		t.Fatalf("置会话为 closed 失败: %v", e)
	}
	list, err := svc.GetRecentClosedSessions(ctx, channelID, visitorA, 10)
	if err != nil {
		t.Fatalf("GetRecentClosedSessions 失败: %v", err)
	}
	inList := false
	for _, s := range list {
		if s.SessionID == olderID {
			inList = true
		}
	}
	if !inList {
		t.Fatalf("早先那条会话没出现在 recent-closed 列表里，换证入口对本用例不成立（列表 %d 条）", len(list))
	}

	// 判据的前置：手上带着"最近一条"的 token 去打早先那条，必须先证明确实打不开。
	// 这一格红了说明 token 根本不是会话绑定的，那么缺陷与修复都不成立。
	if verr := ValidateVisitorToken(newerToken, channelID, visitorA, olderID); verr == nil {
		t.Fatalf("前置不成立：另一条会话的 token 竟能通过 (visitor=%s, session=%s) 的校验", visitorA, olderID)
	}

	t.Run("换回的 token 能通过该会话的校验", func(t *testing.T) {
		token, err := svc.ExchangeVisitorToken(ctx, channelID, visitorA, olderID)
		if err != nil {
			t.Fatalf("ExchangeVisitorToken 失败: %v", err)
		}
		if token == "" {
			t.Fatal("ExchangeVisitorToken 返回空 token")
		}
		if token == newerToken {
			t.Error("换回的 token 与手上那条会话的 token 相同，说明签名没带上 session_id")
		}
		if verr := ValidateVisitorToken(token, channelID, visitorA, olderID); verr != nil {
			t.Errorf("换回的 token 对本条会话仍校验不过: %v", verr)
		}
	})

	t.Run("换回的 token 打不开别的会话", func(t *testing.T) {
		// 若这个端点签出的是"访客级"而非"会话级"凭证，等于把 IDOR 防护拆了。
		token, err := svc.ExchangeVisitorToken(ctx, channelID, visitorA, olderID)
		if err != nil {
			t.Fatalf("ExchangeVisitorToken 失败: %v", err)
		}
		if verr := ValidateVisitorToken(token, channelID, visitorA, newerID); verr == nil {
			t.Errorf("换到的 token 能开该访客的另一条会话（%s），token 不再是会话级", newerID)
		}
	})

	t.Run("别人的会话换不到证", func(t *testing.T) {
		token, err := svc.ExchangeVisitorToken(ctx, channelID, visitorB, olderID)
		if err == nil {
			t.Errorf("访客 B 换到了访客 A 会话的 token（前 8 位 %s），这是越权领证", token[:8])
		}
		if token != "" {
			t.Errorf("换证失败时仍返回了非空 token: %q", token)
		}
	})

	t.Run("不存在的会话换不到证，且错误可被判为归属拒绝", func(t *testing.T) {
		// 控制器按 errors.Is(err, ErrVisitorSessionNotOwned) 决定回 403 还是 5xx：
		// 归属错误若被包成不可比对的新错误，服务端故障与"这不是你的会话"会一起被报成
		// 403，而前端对 403 是静默 catch ⇒ 故障被读成"你没权限"。
		_, err := svc.ExchangeVisitorToken(ctx, channelID, visitorA, "sess_not_created_by_this_case")
		if err == nil {
			t.Fatal("不存在的 session_id 竟然换到了 token")
		}
		if !errors.Is(err, ErrVisitorSessionNotOwned) {
			t.Errorf("归属拒绝没带上哨兵错误，实际: %v", err)
		}
		if msg := err.Error(); msg != "会话不存在或无权访问" {
			t.Errorf("对外文案变了（会把存在性与权限区分开）: %q", msg)
		}
	})

	t.Run("空 session_id 直接拒", func(t *testing.T) {
		if _, err := svc.ExchangeVisitorToken(ctx, channelID, visitorA, ""); err == nil {
			t.Error("空 session_id 应当换不到证")
		}
	})
}
