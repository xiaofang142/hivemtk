package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
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

// tgAPIStub 假的 Telegram Bot API 服务端。
//
// 存在的意义：AuthorizeMember 的放行动作（restrictChatMember 解禁 /
// approveChatJoinRequest）必须打到 TG 侧。此前没有接缝，测试只能用假 token
// 打真 API → 必然 401 → "授权成功"主路径无法验证（该用例因此长期失败）。
// 现在经 SetAPIBase 指向本 stub，即可覆盖 happy path 与失败回滚两条路径。
//
// callMethod 只校验 HTTP 状态码（不解析 body 的 ok 字段），故 200 即视为成功。
type tgAPIStub struct {
	mu     sync.Mutex
	calls  []string
	status int
}

func (s *tgAPIStub) handle(w http.ResponseWriter, r *http.Request) {
	// 路径形如 /bot<token>/<method>
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	method := parts[len(parts)-1]
	s.mu.Lock()
	s.calls = append(s.calls, method)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if s.status != http.StatusOK {
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
		return
	}
	_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
}

func (s *tgAPIStub) called(method string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if c == method {
			return true
		}
	}
	return false
}

func (s *tgAPIStub) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

// seedTGMember 播种一条待验证台账 + 对应 TG 账号。
func seedTGMember(t *testing.T, db *gorm.DB, token string) {
	t.Helper()
	m := &model.TelegramGroupMember{
		AccountID: 1, ChatID: "-100", UserID: "42",
		JoinStatus: model.TGMemberRestricted, JoinMode: TGGateModeMuteUnlock,
		VerifyToken: token,
		ExpiresAt:   timePtr(time.Now().Add(5 * time.Minute)),
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}
	acc := &model.TelegramAccount{
		AccountName: "gate-test", BotToken: "1:fake", BotUsername: "gate_test_bot",
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
}

// 冒用与过期：他人 token 不得放行；过期 token 不得放行。
func TestStartTokenOwnershipAndExpiry(t *testing.T) {
	stub := &tgAPIStub{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	defer srv.Close()

	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{}, &model.TelegramAccount{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)
	svc.SetAPIBase(srv.URL)
	seedTGMember(t, db, "tok_owner")

	// 冒用：user 99 拿 user 42 的 token → 命中流程但拒绝放行
	svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 99}, "/start tok_owner", 99)
	m, _ := svc.memberRepo.GetByToken(context.Background(), "tok_owner")
	if m == nil || m.Authorized {
		t.Fatal("他人 token 不得放行")
	}
	if stub.called("restrictChatMember") {
		t.Errorf("冒用被拒时不得调用 TG 解禁接口，实际调用序列: %v", stub.snapshot())
	}
}

// 过期 token：不触发放行，且不得调用 TG 接口。
func TestStartTokenExpiredRejected(t *testing.T) {
	stub := &tgAPIStub{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	defer srv.Close()

	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{}, &model.TelegramAccount{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)
	svc.SetAPIBase(srv.URL)
	seedTGMember(t, db, "tok_expired")

	// 把过期时间改到过去
	past := time.Now().Add(-time.Minute)
	if err := db.Model(&model.TelegramGroupMember{}).Where("verify_token = ?", "tok_expired").
		Update("expires_at", past).Error; err != nil {
		t.Fatalf("update expires_at: %v", err)
	}

	svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 42}, "/start tok_expired", 42)
	m, _ := svc.memberRepo.GetByToken(context.Background(), "tok_expired")
	if m == nil || m.Authorized {
		t.Fatal("过期 token 不得放行")
	}
	if stub.called("restrictChatMember") {
		t.Errorf("过期 token 不得调用 TG 解禁接口，实际调用序列: %v", stub.snapshot())
	}
}

// 正主 token + TG API 正常：放行成功、落库 approved，且**确实**调用了 TG 解禁接口。
func TestAuthorizeMember_MuteUnlock_HappyPath(t *testing.T) {
	stub := &tgAPIStub{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	defer srv.Close()

	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{}, &model.TelegramAccount{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)
	svc.SetAPIBase(srv.URL)
	seedTGMember(t, db, "tok_owner")

	svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 42}, "/start tok_owner", 42)

	m, _ := svc.memberRepo.Get(context.Background(), 1, "-100", "42")
	if m == nil || !m.Authorized || m.JoinStatus != model.TGMemberApproved {
		t.Fatalf("正主 token 应放行: %+v", m)
	}
	if !stub.called("restrictChatMember") {
		t.Errorf("放行必须调用 TG 解禁接口（restrictChatMember 放开权限），实际调用序列: %v", stub.snapshot())
	}
}

// 失败回滚（fail-closed 回归）：TG 解禁接口失败时必须**回滚** approved。
//
// 设计依据（telegram_gate.go AuthorizeMember 内注释）：
//
//	解禁失败必须回滚 approved：否则台账显示已放行而 TG 侧仍受限
//	（用户被告知"验证通过"却发不了言）。回滚后清扫器补偿循环会再试。
//
// 历史沿革（勿回退）：本用例原先断言"Bot API 调用失败仅告警，不阻断授权落库"，
// 即 fail-open —— 那是早期行为。生产代码后来改为 fail-closed 并加了回滚，
// 用例注释却未同步，导致长期失败。此处的断言即当前正确语义。
func TestAuthorizeMember_UnrestrictFailureRollsBack(t *testing.T) {
	stub := &tgAPIStub{status: http.StatusUnauthorized}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	defer srv.Close()

	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{}, &model.TelegramAccount{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)
	svc.SetAPIBase(srv.URL)
	seedTGMember(t, db, "tok_owner")

	svc.HandleStartCommand(context.Background(), 1, &telegram.TGUser{ID: 42}, "/start tok_owner", 42)

	m, _ := svc.memberRepo.Get(context.Background(), 1, "-100", "42")
	if m == nil {
		t.Fatal("台账记录不应消失")
	}
	if m.Authorized {
		t.Error("解禁失败时不得标记为已授权（fail-closed）—— 否则台账显示已放行而 TG 侧仍受限")
	}
	if m.JoinStatus != model.TGMemberRestricted {
		t.Errorf("解禁失败后 join_status 应回滚为 restricted，实际 %s", m.JoinStatus)
	}
	if m.AuthorizedAt != nil {
		t.Error("解禁失败后 authorized_at 应清空，否则补偿/清扫器会跳过该成员")
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

// MemberUnverified：未验证（pending/restricted/kicked 且未激活）→ true；
// approved / authorized / 无台账 → false（非门控群放行给原业务）
func TestMemberUnverified(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.TelegramGroupMember{})
	if db == nil {
		t.Skip("no test db")
	}
	svc := NewTelegramGateService(db)
	past := timePtr(time.Now().Add(-time.Minute))

	seeds := []struct {
		user   string
		status string
		auth   bool
		want   bool
	}{
		{"1", model.TGMemberPending, false, true},
		{"2", model.TGMemberRestricted, false, true},
		{"3", model.TGMemberKicked, false, true},
		{"4", model.TGMemberApproved, true, false},
		{"5", model.TGMemberRestricted, true, false}, // authorized 但状态没刷新（异常兜底）
	}
	for _, s := range seeds {
		if err := db.Create(&model.TelegramGroupMember{
			AccountID: 7, ChatID: "-77", UserID: s.user,
			JoinStatus: s.status, Authorized: s.auth,
		}).Error; err != nil {
			t.Fatalf("seed %s: %v", s.user, err)
		}
	}
	for _, s := range seeds {
		got := svc.MemberUnverified(context.Background(), 7, "-77", s.user)
		if got != s.want {
			t.Errorf("user=%s status=%s auth=%v: got=%v want=%v", s.user, s.status, s.auth, got, s.want)
		}
	}
	// 非门控群 / 无记录 → 放行
	if svc.MemberUnverified(context.Background(), 7, "-88", "1") {
		t.Error("无台账应返回 false")
	}
	if svc.MemberUnverified(context.Background(), 9, "-77", "1") {
		t.Error("其他账号无台账应返回 false")
	}
	_ = past
}
