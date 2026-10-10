package scope

import (
	"context"
	"testing"
)

// team_test.go 租户过滤三态判定的纯单元用例（不连库，本地即跑）：
//   - 未注入身份（系统上下文）→ 直通（ingest/cron 等平台管线不能被误伤）；
//   - 注入平台管理员 → 直通（需求 3：超管跨团队全量）；
//   - 注入普通身份 → 锁定本团队（fail-closed：漏注入等于少看，不是多看）；
//   - TENANT_ISOLATION=off → TeamScoped 全直通（回滚口径，TeamScoped 内部判开关）。
func TestTeamFilterDecision_ThreeStates(t *testing.T) {
	// ① 系统上下文（无身份）→ 直通
	if passthrough, teamID := teamFilterDecision(nil); !passthrough || teamID != 0 {
		t.Errorf("nil ctx 应直通，got passthrough=%v teamID=%d", passthrough, teamID)
	}
	if passthrough, _ := teamFilterDecision(context.Background()); !passthrough {
		t.Error("空 ctx 应视为系统上下文（直通）")
	}

	// ② 平台管理员 → 直通
	ctx := WithTeamInfo(context.Background(), TeamInfo{TeamID: 3, Platform: true})
	if passthrough, _ := teamFilterDecision(ctx); !passthrough {
		t.Error("平台管理员应直通（跨团队全量，需求 3）")
	}

	// ③ 普通身份 → 锁定本团队
	ctx = WithTeamInfo(context.Background(), TeamInfo{TeamID: 7})
	if passthrough, teamID := teamFilterDecision(ctx); passthrough || teamID != 7 {
		t.Errorf("普通身份应锁定团队 7，got passthrough=%v teamID=%d", passthrough, teamID)
	}

	// ④ 老调用方（只有 user_id/role 键，SetGinValuesToCtx 兼容链路）→ 有身份但无团队
	ctx = WithRole(WithUID(context.Background(), 9), "user")
	if passthrough, teamID := teamFilterDecision(ctx); passthrough || teamID != 0 {
		t.Errorf("有身份无团队应锁定 team 0（平台侧行），got passthrough=%v teamID=%d", passthrough, teamID)
	}
	// role=admin 走 isAdmin 兼容路径同样直通
	ctx = WithRole(WithUID(context.Background(), 9), "admin")
	if passthrough, _ := teamFilterDecision(ctx); !passthrough {
		t.Error("role=admin（兼容链路）应直通")
	}
}

// TestTeamInfoFromCtx_Presence 语义钉：ctx 里有没有"身份"必须可判——
// 系统上下文（无键）与"已注入但团队为 0 的平台侧身份"是两件事。
func TestTeamInfoFromCtx_Presence(t *testing.T) {
	if _, present := TeamInfoFromCtx(nil); present {
		t.Error("nil ctx 应视为无身份（系统上下文）")
	}
	if _, present := TeamInfoFromCtx(context.Background()); present {
		t.Error("空 ctx 应视为无身份（系统上下文）")
	}
	info, present := TeamInfoFromCtx(WithTeamInfo(context.Background(), TeamInfo{TeamID: 0, Platform: true}))
	if !present || !info.Platform || info.TeamID != 0 {
		t.Errorf("注入的平台侧身份应可读回，got %+v present=%v", info, present)
	}
}

// TestTenantIsolationSwitch 回滚口径：TENANT_ISOLATION=off 时 TeamScoped 全直通。
func TestTenantIsolationSwitch(t *testing.T) {
	restore := SetTenantIsolationForTest(false)
	defer restore()
	// TeamScoped 内部先判开关：关 → 无论什么身份都直通。
	// 这里不构建 gorm 会话，直接验证开关 seam 生效（判定函数不读开关，
	// 开关在 TeamScoped 层——语义见 team.go 注释）。
	if tenantIsolationEnabled() {
		t.Error("开关关闭后 tenantIsolationEnabled 应为 false")
	}
}
