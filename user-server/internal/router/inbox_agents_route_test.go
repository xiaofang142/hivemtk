package router

// 收件箱坐席名单口（GET /api/inbox/agents）的常驻门。
//
// 这条口是"分配下拉不再打仅超管的用户清单口"那一步的替代件，两头都要钉：
//   - 非超管读得到名单：否则收件箱页面能打开、下拉永远是空的（原来就是这个形状）；
//   - 读到的只有名单要用的四列：否则替代件自己变成新的账号资料泄露面。
//
// 判据读的是**响应体字符串**，不是 handler 返回值——投影写错列名时 gorm 会静默回零值列，
// 只有把 body 摊开比才看得见。

import (
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
)

func TestInboxAgentsRosterReadableAndDesensitized(t *testing.T) {
	engine, database := gateEngineAndDB(t)
	cs := gateRoleToken(t, "customer_service")

	// 哨兵值只出现在夹具行里：它们出现在响应体＝整行账号资料被回了出来。
	const sentinelEmail = "agent-one@sentinel.invalid"
	const sentinelPhone = "13900001111"
	rows := []*model.SystemUser{
		{Username: "agent-one", Password: "x", RealName: "一线坐席", Role: "customer_service",
			Status: 1, Enabled: true, Email: sentinelEmail, Phone: sentinelPhone},
		{Username: "agent-two", Password: "x", RealName: "二线坐席", Role: "staff",
			Status: 1, Enabled: true},
		// 停用账号不该再进候选：把会话分给它等于没人接。停用位必须在创建后用 Update 落，
		// 因为列上挂着 `default:true`——Insert 时 GORM 会把零值 false 让位给库默认，
		// 写进去仍是 true（生产侧 SetEnabled 也是走 Update 的）。
		{Username: "agent-off", Password: "x", RealName: "已停用", Role: "customer_service",
			Status: 1, Enabled: true},
		// 无坐席档案的账号域角色不是坐席候选（名单是"谁能接手会话"=有档案的在职账号，
		// 不是"系统里有谁"；admin 老板有档案时同样会出现在名单里——SaaS 新租户常态）。
		{Username: "gate-boss", Password: "x", RealName: "超管", Role: "admin",
			Status: 1, Enabled: true},
		{Username: "gate-manager", Password: "x", RealName: "经理", Role: "manager",
			Status: 1, Enabled: true},
	}
	for _, r := range rows {
		if err := database.Create(r).Error; err != nil {
			t.Fatalf("写入坐席夹具 %s 失败: %v", r.Username, err)
		}
	}
	// 坐席档案（2026-11-04 BUG-2 口径修正后名单的判定源）：名单= 有坐席档案的
	// 在职账号。三个在职账号都建档；gate-boss / gate-manager 刻意不建档——
	// 它们代表"账号域里存在但不是坐席"的行，正是不该混进脱敏投影的形状。
	if err := database.AutoMigrate(&model.AgentStatus{}); err != nil {
		t.Fatalf("迁移 agent_statuses 失败: %v", err)
	}
	for _, idx := range []int{0, 1, 2} {
		r := rows[idx]
		if err := database.Create(&model.AgentStatus{
			AgentID: r.ID, AgentName: r.Username, Status: "offline", MaxSessions: 5,
		}).Error; err != nil {
			t.Fatalf("写入坐席档案 %s 失败: %v", r.Username, err)
		}
	}
	off := rows[2]
	if err := database.Model(&model.SystemUser{}).Where("id = ?", off.ID).
		Update("enabled", false).Error; err != nil {
		t.Fatalf("把 %s 置为停用失败: %v", off.Username, err)
	}
	// 夹具自证：库里这行确实停了。少了这一步，"名单里没有停用坐席"可能被一个
	// 根本没停用的夹具喂成假绿。
	var stillEnabled bool
	if err := database.Model(&model.SystemUser{}).Where("id = ?", off.ID).
		Select("enabled").Scan(&stillEnabled).Error; err != nil {
		t.Fatalf("读回停用位失败: %v", err)
	}
	if stillEnabled {
		t.Fatal("夹具里 agent-off 的停用位没落进库：这条判据测不到停用面")
	}

	// 前置一：这条口不许被升进仅超管档——升上去等于分配下拉又回到永远空。
	if middleware.IsAdminOnlyRoute("GET", "/api/inbox/agents") {
		t.Fatal("GET /api/inbox/agents 在仅超管档里：坐席读不到自己的候选人名单")
	}
	// 前置（行为向）：这条口替代的是仅超管的用户清单口——先证明那条口对坐席确实是关着的，
	// "改走脱敏口"才算减少了面；判据读文案不读状态码，因为 handler 自己也可能回 403。
	code, body := gateCall(t, engine, "GET", "/api/system/users", cs)
	if code != http.StatusForbidden ||
		!(strings.Contains(body, adminAuthDenyMsg) || strings.Contains(body, policyDeniedMsg)) {
		t.Fatalf("坐席读 GET /api/system/users 应被超管档拦成 403+%q/%q，实得 %d / %s",
			adminAuthDenyMsg, policyDeniedMsg, code, strings.TrimSpace(body))
	}

	code, body = gateCall(t, engine, "GET", "/api/inbox/agents", cs)
	if code != http.StatusOK {
		t.Fatalf("customer_service 读坐席名单应 200，实得 %d / %s", code, strings.TrimSpace(body))
	}
	for _, want := range []string{"agent-one", "agent-two", "一线坐席"} {
		if !strings.Contains(body, want) {
			t.Errorf("名单里没有 %s（可分配坐席读漏了）：%s", want, strings.TrimSpace(body))
		}
	}
	for _, banned := range []string{"agent-off", "已停用", "gate-boss", "超管", "gate-manager",
		sentinelEmail, sentinelPhone, `"email"`, `"phone"`, `"enabled"`, `"status"`, `"last_login"`,
		`"data_scope"`, `"password"`} {
		if strings.Contains(body, banned) {
			t.Errorf("坐席名单回出了 %s：脱敏投影没生效", banned)
		}
	}
}
