package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// /start 命令识别：token 提取、无 token、非 /start 文本不拦截
func TestHandleStartCommandRecognition(t *testing.T) {
	cases := []struct {
		text    string
		wantTok string
		wantHit bool
	}{
		{"/start", "", true},
		{"/start abc123", "abc123", true},
		{" /start  token  ", "token", true},
		{"/star", "", false},
		{"/help", "", false},
		{"hello", "", false},
		{"/startx", "", false},
	}
	for _, c := range cases {
		tok, hit := parseStartCommand(c.text)
		if hit != c.wantHit {
			t.Errorf("text=%q hit=%v want=%v", c.text, hit, c.wantHit)
		}
		if hit && tok != c.wantTok {
			t.Errorf("text=%q token=%q want=%q", c.text, tok, c.wantTok)
		}
	}
	// nil guard：from 为空 / db 为空直接跳过（不 panic）
	svc := &TelegramGateService{}
	if svc.HandleStartCommand(context.Background(), 1, nil, "/start", 42) {
		t.Error("nil from should not hit")
	}
	if svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 1}, "/start", 42) {
		t.Error("nil db should not hit")
	}
}

// token 归属与过期校验：别人的 token 不能放行（防冒用），正主 token 放行落库 approved
func TestStartTokenOwnershipAndExpiry(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{}, &model.TelegramAccount{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)

	seed := &model.TelegramGroupMember{
		AccountID: 1, ChatID: "-100", UserID: "42",
		JoinStatus: model.TGMemberRestricted, JoinMode: TGGateModeMuteUnlock,
		VerifyToken: "tok_owner",
		ExpiresAt:   timePtr(time.Now().Add(5 * time.Minute)),
	}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 账号记录：AuthorizeMember 会加载 Bot 客户端（SendMessage 走真实 TG API 会失败，
	// 但失败仅告警不阻断授权落库，与生产行为一致）
	if err := db.Create(&model.TelegramAccount{AccountName: "gate-test", BotToken: "1:fake", BotUsername: "gate_test_bot"}).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}

	// 冒用：user 99 拿 user 42 的 token → 命中流程但拒绝放行
	svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 99}, "/start tok_owner", 99)
	m, _ := svc.memberRepo.GetByToken(context.Background(), "tok_owner")
	if m == nil || m.Authorized {
		t.Fatal("他人 token 不得放行")
	}

	// 正主：放行成功、状态 approved（Bot API 调用失败仅告警，不阻断授权落库）
	svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 42}, "/start tok_owner", 42)
	m, _ = svc.memberRepo.Get(context.Background(), 1, "-100", "42")
	if m == nil || !m.Authorized || m.JoinStatus != model.TGMemberApproved {
		t.Fatalf("正主 token 应放行: %+v", m)
	}
}

// TTL 清扫：只挑 authorized=false 且过期的 pending/restricted 记录
func TestSweepExpiredFilter(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)
	past := timePtr(time.Now().Add(-time.Hour))
	future := timePtr(time.Now().Add(time.Hour))

	seeds := []*model.TelegramGroupMember{
		{AccountID: 1, ChatID: "-1", UserID: "1", JoinStatus: model.TGMemberRestricted, VerifyToken: "e1", ExpiresAt: past},
		{AccountID: 1, ChatID: "-2", UserID: "2", JoinStatus: model.TGMemberPending, VerifyToken: "e2", ExpiresAt: past},
		{AccountID: 1, ChatID: "-3", UserID: "3", JoinStatus: model.TGMemberRestricted, VerifyToken: "e3", ExpiresAt: future},
		{AccountID: 1, ChatID: "-4", UserID: "4", JoinStatus: model.TGMemberApproved, Authorized: true, VerifyToken: "e4", ExpiresAt: past},
	}
	for _, m := range seeds {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	expired, err := svc.memberRepo.ListExpired(context.Background(), time.Now(), 100)
	if err != nil {
		t.Fatalf("ListExpired: %v", err)
	}
	if len(expired) != 2 {
		t.Fatalf("应命中 2 条超时未验证，实际 %d: %+v", len(expired), expired)
	}
	for _, m := range expired {
		if m.VerifyToken != "e1" && m.VerifyToken != "e2" {
			t.Errorf("误命中 %s", m.VerifyToken)
		}
	}
}

func TestGateModeConstants(t *testing.T) {
	if TGGateModeJoinRequest != "join_request" || TGGateModeMuteUnlock != "mute_unlock" {
		t.Fatal("mode 常量与前端约定不一致")
	}
	if !strings.HasPrefix(botDeepLink("mybot", "tok"), "https://t.me/mybot?start=tok") {
		t.Fatal("深链格式错误")
	}
}

func TestGenVerifyToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok := genVerifyToken()
		if len(tok) != 40 {
			t.Fatalf("token 长度应为 40 hex，实际 %d: %s", len(tok), tok)
		}
		if seen[tok] {
			t.Fatalf("token 重复: %s", tok)
		}
		seen[tok] = true
	}
}
