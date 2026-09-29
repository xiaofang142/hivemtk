package service

import (
	"context"
	"fmt"
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
//
// chatMemberStatus 是给 getChatMember 用的返回值覆盖：那个方法要读 body 里的
// result.status（对象），而默认回包 `"result":true` 根本解不进 map ⇒ 不设它时
// 任何走 getChatMember 的用例只会拿到解析错误。空串＝沿用默认回包。
type tgAPIStub struct {
	mu               sync.Mutex
	calls            []string
	status           int
	chatMemberStatus string
}

func (s *tgAPIStub) handle(w http.ResponseWriter, r *http.Request) {
	// 路径形如 /bot<token>/<method>
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	method := parts[len(parts)-1]
	s.mu.Lock()
	s.calls = append(s.calls, method)
	memberStatus := s.chatMemberStatus
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if s.status != http.StatusOK {
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
		return
	}
	if method == "getChatMember" && memberStatus != "" {
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"user":{"id":4242,"is_bot":false}}}`, memberStatus)
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
		// 授权位与 join_status 是两列，判据也得各管各的：这种行确实会出现（/start 放行写到
		// 一半、人工核对时的手工置位、迁移遗留）。少了 authorized=false 这条谓词，e5 会被
		// join_status 的过滤网漏下来当成"超时未验证"，人已经解禁却在下轮被移出。
		{AccountID: 1, ChatID: "-5", UserID: "5", JoinStatus: model.TGMemberRestricted, Authorized: true, VerifyToken: "e5", ExpiresAt: past},
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

// 群主/管理员的状态字面量判定（getChatMember 的 result.status 全集里只有这两个能免验证）。
func TestTGPrivilegedStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"creator", true},
		{"administrator", true},
		{"member", false},
		{"left", false},
		{"kicked", false},
		{"", false},
		{"Creator", false},   // TG 侧是小写枚举，大小写不一律不豁免
		{"anonymous", false}, // 匿名管理员身份不可核对 ⇒ 不豁免
		{"in_member", false}, // 含子串也不得命中
	}
	for _, c := range cases {
		if got := tgPrivilegedStatus(c.status); got != c.want {
			t.Errorf("status=%q got=%v want=%v", c.status, got, c.want)
		}
	}
}

// 门控装群前就在群里的群主：他没有台账行，而"补发验证邀请"那条路要先禁言——
// Bot 对群主的 restrictChatMember 必然被 TG 拒 ⇒ 走到 continue ⇒ 邀请永远发不出去，
// 互锁对他终身成立，症状就是"群里老板说话，外面一声不响"。
// 这里验的是：先问一次 TG 侧真实身份，特权就地授权放行；非特权/查不到一律维持互锁。
func TestMemberUnverifiedPrivilegedSpeaker(t *testing.T) {
	cases := []struct {
		name         string
		user         string // 空串＝"4242"
		memberStatus string // 空串＝沿用 stub 默认回包（result:true，解不进 map ⇒ 模拟 TG 返回异常）
		apiStatus    int
		wantUnverif  bool
		wantAuthRow  bool
		wantNoAPI    bool // id 不合法时连 TG 都不该问
	}{
		{"群主就地授权", "", "creator", http.StatusOK, false, true, false},
		{"管理员就地授权", "", "administrator", http.StatusOK, false, true, false},
		{"普通成员维持互锁", "", "member", http.StatusOK, true, false, false},
		{"已退群者维持互锁", "", "left", http.StatusOK, true, false, false},
		{"TG 回包异常维持互锁", "", "", http.StatusOK, true, false, false},
		{"API 打不通维持互锁", "", "creator", http.StatusUnauthorized, true, false, false},
		{"id 非数字维持互锁", "not-a-number", "creator", http.StatusOK, true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			user := c.user
			if user == "" {
				user = "4242"
			}
			db := testutil.NewTestDB(t, &model.TelegramGroupGate{}, &model.TelegramGroupMember{}, &model.TelegramAccount{})
			acc := &model.TelegramAccount{AccountName: "特权判定号", BotToken: "1:fake", BotUsername: "priv_bot", Status: 1}
			if err := db.Create(acc).Error; err != nil {
				t.Fatalf("种 TG 账号: %v", err)
			}
			if err := db.Create(&model.TelegramGroupGate{
				AccountID: acc.ID, ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true, VerifyTTLMin: 10,
			}).Error; err != nil {
				t.Fatalf("种门控: %v", err)
			}
			stub := &tgAPIStub{status: c.apiStatus, chatMemberStatus: c.memberStatus}
			srv := httptest.NewServer(http.HandlerFunc(stub.handle))
			t.Cleanup(srv.Close)
			svc := NewTelegramGateService(db)
			svc.SetAPIBase(srv.URL)
			ctx := context.Background()

			if got := svc.MemberUnverified(ctx, acc.ID, "-100", user); got != c.wantUnverif {
				t.Errorf("MemberUnverified = %v，want %v（TG 调用：%v）", got, c.wantUnverif, stub.snapshot())
			}

			var m model.TelegramGroupMember
			getErr := db.Where("account_id = ? AND chat_id = ? AND user_id = ?", acc.ID, "-100", user).First(&m).Error
			if !c.wantAuthRow {
				if getErr == nil {
					t.Errorf("非特权/身份查不到时不得写授权台账（宁可多拦不误放），实际 %+v", m)
				}
				if stub.called("sendMessage") {
					t.Errorf("判定失败时不得在群里发东西，实际调用：%v", stub.snapshot())
				}
				if c.wantNoAPI && stub.called("getChatMember") {
					t.Errorf("id 不合法时不该打 TG，实际调用：%v", stub.snapshot())
				}
				return
			}
			if getErr != nil {
				t.Fatalf("特权发言人没落下授权台账 ⇒ 互锁对他仍在，下一条消息还是没回: %v", getErr)
			}
			if !m.Authorized || m.JoinStatus != model.TGMemberApproved {
				t.Errorf("授权台账不对：authorized=%v join_status=%s", m.Authorized, m.JoinStatus)
			}
			if m.AuthorizedAt == nil {
				t.Error("authorized_at 未写 ⇒ 清扫/补偿口径会把他当未验证成员")
			}
			if m.VerifyToken != "" {
				t.Error("群主无需验证链接，不该有 verify_token")
			}
			if !stub.called("getChatMember") {
				t.Errorf("特权判定必须真去 TG 侧核身份，实际调用：%v", stub.snapshot())
			}
			// Bot 无权禁言群主，发邀请也不是这条路该做的事：调了就说明豁免没生效
			if stub.called("restrictChatMember") || stub.called("sendMessage") {
				t.Errorf("特权豁免不应触发禁言/发邀请，实际调用：%v", stub.snapshot())
			}

			// 判定结果必须落在台账上：第二条消息不该再打 TG，否则每条群发言都要一次往返
			before := len(stub.snapshot())
			if svc.MemberUnverified(ctx, acc.ID, "-100", user) {
				t.Error("已授权后仍判未验证")
			}
			if delta := len(stub.snapshot()) - before; delta != 0 {
				t.Errorf("授权后又调了 %d 次 TG，判定应落在台账上（调用：%v）", delta, stub.snapshot())
			}
		})
	}
}

// MemberVerified 是「门控群里这名发言人是已验证成员」的正面判据。
// 它不能由 MemberUnverified 取反代替：后者返回 false 的情形还包括群没装门控、
// 台账里是已退群的人、以及服务根本没装配 ⇒ 取反会把宽松触发策略泼到这些人身上。
func TestMemberVerifiedScopes(t *testing.T) {
	cases := []struct {
		name   string
		gate   *model.TelegramGroupGate
		member *model.TelegramGroupMember
		want   bool
	}{
		{
			name: "没装门控的群不在本策略内",
			gate: nil,
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberApproved, Authorized: true,
			},
			want: false,
		},
		{
			name: "门控已关闭",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: false},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberApproved, Authorized: true,
			},
			want: false,
		},
		{
			name: "入群申请制群沿用原判据",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeJoinRequest, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberApproved, Authorized: true,
			},
			want: false,
		},
		{
			name:   "无台账不算已验证",
			gate:   &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: nil,
			want:   false,
		},
		{
			name: "待激活",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberPending,
			},
			want: false,
		},
		{
			name: "禁言中",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberRestricted,
			},
			want: false,
		},
		{
			name: "超时被清理",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberKicked,
			},
			want: false,
		},
		{
			name: "已退群（MemberUnverified 对他也是放行，两句判据不互为取反）",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: "left",
			},
			want: false,
		},
		{
			name: "已放行",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberApproved,
			},
			want: true,
		},
		{
			name: "已授权但群内状态还停在禁言中",
			gate: &model.TelegramGroupGate{ChatID: "-100", Mode: TGGateModeMuteUnlock, Enabled: true},
			member: &model.TelegramGroupMember{
				ChatID: "-100", UserID: "4242", JoinStatus: model.TGMemberRestricted, Authorized: true,
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.TelegramGroupGate{}, &model.TelegramGroupMember{})
			if c.gate != nil {
				g := *c.gate
				g.AccountID = 1
				if err := db.Create(&g).Error; err != nil {
					t.Fatalf("种门控: %v", err)
				}
			}
			if c.member != nil {
				m := *c.member
				m.AccountID = 1
				if err := db.Create(&m).Error; err != nil {
					t.Fatalf("种台账: %v", err)
				}
			}
			svc := NewTelegramGateService(db)
			if got := svc.MemberVerified(context.Background(), 1, "-100", "4242"); got != c.want {
				t.Errorf("MemberVerified = %v，want %v", got, c.want)
			}
		})
	}
}

// 依赖没装配（db 为 nil ⇒ 服务未 wired）时必须判"未验证"，否则宽松触发策略会在
// 装配失败的服务上变成默认放行。
// 入群提示文案里「你正在通过 @xxx 加入群组」这段被去掉后，位置占位符 %s 的个数变了。
// 若仍按位置序渲染，剩下的 https://t.me/%s?start=%s 会整体左移一位（domain 位拿到
// bot 名、token 位拿到 domain）⇒ 验证链接坏掉，而且坏得没有任何报错。
// 库里两个群的 welcome_msg 是人配过的老 %s 模板，不能因为这次改文案就变样 ⇒
// 两条渲染路径都得有用例。
func TestRenderTGGateWelcomeNamedAndLegacy(t *testing.T) {
	const (
		display = "三人行用户"
		bot     = "@lovesanrenxing_bot"
		token   = "abc123token"
		link    = "https://t.me/lovesanrenxing_bot?start=abc123token"
	)

	t.Run("新模板用命名占位符", func(t *testing.T) {
		out := renderTGGateWelcome(tgGateDefaultWelcome(), display, bot, token)
		if strings.Contains(out, "你正在通过") {
			t.Errorf("该删的那段还在：%q", out)
		}
		if !strings.Contains(out, link) {
			t.Errorf("验证链接没渲染出来：%q，want 含 %q", out, link)
		}
		if !strings.Contains(out, display) {
			t.Errorf("昵称没替换：%q", out)
		}
		// 占位符没被消费干净＝模板改了但渲染分支没跟上（会原样发到群里）
		if strings.Contains(out, "{{") {
			t.Errorf("命名占位符没被替换：%q", out)
		}
		if strings.Contains(out, "%!") {
			t.Errorf("fmt 报了参数错配：%q", out)
		}
	})

	t.Run("老 %s 模板照旧可用", func(t *testing.T) {
		legacy := "👋 欢迎 %s！你正在通过 %s 加入「三人行」技术交流群。\n\n请在 10 分钟内点击这里完成验证：https://t.me/%s?start=%s\n\n验证后即可正常发言~"
		out := renderTGGateWelcome(legacy, display, bot, token)
		if !strings.Contains(out, link) {
			t.Errorf("老模板的链接被改坏了：%q", out)
		}
		if !strings.Contains(out, "你正在通过 @lovesanrenxing_bot") {
			t.Errorf("老模板不该被这次改动改写：%q", out)
		}
		if strings.Contains(out, "%!") || strings.Contains(out, "%s") {
			t.Errorf("老模板参数没吃满：%q", out)
		}
	})

	// 三个渲染点（方案 A 私聊、方案 B 群内、清扫器补发）共用这个函数：
	// 少接一处就等于那个入口还在发老文案。
	t.Run("兜底模板自身不含被删那段", func(t *testing.T) {
		if strings.Contains(tgGateDefaultWelcome(), "你正在通过") {
			t.Error("兜底模板里还留着要去掉的那段")
		}
	})

	// 线上事故：2 动词老模板 + 4 实参 Sprintf ⇒ 尾部挂 %!(EXTRA …) 发到群里。
	// 现在按位置逐个填，多余实参直接丢弃，绝不外发 EXTRA。
	t.Run("动词比实参少时不外发EXTRA", func(t *testing.T) {
		short := "👋 欢迎 %s 加入「三人行」技术交流群！\n\n请在 10 分钟内点击这里完成验证：https://t.me/lovesanrenxing_bot?start=abc123token\n\n验证后即可正常发言~"
		out := renderTGGateWelcome(short, display, bot, token)
		if strings.Contains(out, "%!") || strings.Contains(out, "%s") || strings.Contains(out, "{{") {
			t.Errorf("短模板渲染泄漏了占位符/EXTRA：%q", out)
		}
		if !strings.Contains(out, display) {
			t.Errorf("昵称没填上：%q", out)
		}
	})

	// 未知命名占位符（如 {{foo}}）会原文外发：必须退回默认模板而不是原样发。
	t.Run("未知命名占位符退回默认模板", func(t *testing.T) {
		out := renderTGGateWelcome("👋 欢迎 {{display}}！暗号：{{foo}}", display, bot, token)
		if strings.Contains(out, "{{") {
			t.Errorf("未知占位符外发出去了：%q", out)
		}
		if !strings.Contains(out, link) {
			t.Errorf("退回默认模板后链接应可用：%q", out)
		}
	})

	// 非 %s 动词（如 %d）填不上：同样退回默认模板。
	t.Run("非s动词退回默认模板", func(t *testing.T) {
		out := renderTGGateWelcome("👋 欢迎 %s！剩余 %d 分钟", display, bot, token)
		if strings.Contains(out, "%") && out != "100%" {
			// 默认模板里没有 %，出现即泄漏
			t.Errorf("未填充动词外发出去了：%q", out)
		}
		if !strings.Contains(out, link) {
			t.Errorf("退回默认模板后链接应可用：%q", out)
		}
	})
}

func TestMemberVerifiedUnwiredService(t *testing.T) {
	if (NewTelegramGateService(nil)).MemberVerified(context.Background(), 1, "-100", "4242") {
		t.Error("未装配的门控服务不得判已验证")
	}
}
