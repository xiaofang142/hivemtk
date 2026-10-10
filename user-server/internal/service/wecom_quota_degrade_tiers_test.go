package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/repository"
)

// TestComputeHealthScoreQuotaDegradeBothTiers 钉住 wecom.quota_degrade 参数
// **同时挪动两档**这件事。
//
// 背景：computeHealthScore 里配额扣分有三档阈值，其中 -25 档原来写死 0.95、-15 档
// 读参数 quotaDegrade（默认 0.9）。两者独立 ⇒ 运维把参数调到 0.95 以上时，>0.95
// 那一档先命中，参数根本轮不到生效——"配额降级"于是成了半接线：改得动中间档、
// 改不动最高档。修复是取 max(0.95, quotaDegrade)（见 wecom_account_health.go:392）。
//
// 三个子用例合起来才够：①证明默认值下行为没变（0.9<0.95，逐档与写死一致）；
// ②证明参数调大真的能抬高最高档；③证明参数调小不会把最高档一起拖下去
// （否则一个 0.9 的号子会被从 -15 抬到 -25，那是倒退）。
func TestComputeHealthScoreQuotaDegradeBothTiers(t *testing.T) {
	_, db := newSSERuntimeParamFixture(t)
	ctx := context.Background()

	// 只让配额维度起作用：成功率满分、错误数为 0、登录态正常。
	scoreOf := func(quotaRate float64) int {
		return computeHealthScore(WeComLoginOnline, quotaRate, 100, 0)
	}
	setQuotaDegrade := func(val string) {
		t.Helper()
		repo := repository.NewConfigParamRepository(db)
		if err := repo.UpdateValue(ctx, "wecom", "quota_degrade", val, 1); err != nil {
			t.Fatalf("写 quota_degrade=%s 失败：%v", val, err)
		}
		// 冷缓存 service：参数服务按 group.key 缓存 60s（configParamTTL），
		// 否则读到的还是上一个 service 留下的旧值。
		SetGlobalForTest(NewConfigParamService(db))
	}

	t.Run("默认 0.9 时行为与写死 0.95 逐档一致", func(t *testing.T) {
		for _, tc := range []struct {
			quota float64
			want  int
			why   string
		}{
			{0.96, 75, ">0.95 命中最高档，扣 25"},
			{0.951, 75, "刚过 0.95 仍扣 25"},
			{0.95, 85, "0.95 本身不 >0.95，落 -15 档"},
			{0.92, 85, "0.9<q<=0.95 落 -15 档"},
			{0.9, 95, "等于阈值不 >，落 -5 档"},
			{0.8, 95, "0.7<q<=0.9 落 -5 档"},
			{0.71, 95, "刚过 0.7 仍落 -5 档"},
			{0.7, 100, "0.7 本身不 >0.7，不扣"},
			{0.5, 100, "低配额本就不该扣分"},
		} {
			if got := scoreOf(tc.quota); got != tc.want {
				t.Errorf("quotaRate=%v 应得 %d（%s），got %d", tc.quota, tc.want, tc.why, got)
			}
		}
	})

	t.Run("参数调到 0.97 时最高档随之抬到 0.97", func(t *testing.T) {
		setQuotaDegrade("0.97")
		// max(0.95, 0.97)=0.97：两档同时抬高，0.96 从 -25 掉到 -5
		// （它既不超过 0.97 的最高档，也不超过 0.97 的中间档）
		if got := scoreOf(0.96); got != 95 {
			t.Errorf("quotaRate=0.96 应落 -5 档得 95, got %d", got)
		}
		if got := scoreOf(0.975); got != 75 {
			t.Errorf("quotaRate=0.975 应扣 25 得 75（最高档确实被抬起来了）, got %d", got)
		}
		if got := scoreOf(0.97); got != 95 {
			t.Errorf("quotaRate=0.97 应落 -5 档得 95, got %d", got)
		}
	})

	t.Run("参数调到 0.99 时三档全部让位给参数", func(t *testing.T) {
		setQuotaDegrade("0.99")
		// 0.98 > 0.99 不成立 → 落 -5；0.995 > 0.99 → 扣 25
		if got := scoreOf(0.98); got != 95 {
			t.Errorf("quotaRate=0.98 应落 -5 档得 95, got %d", got)
		}
		if got := scoreOf(0.995); got != 75 {
			t.Errorf("quotaRate=0.995 应扣 25 得 75, got %d", got)
		}
	})

	t.Run("参数调小到 0.5 时最高档仍是 0.95 不下沉", func(t *testing.T) {
		setQuotaDegrade("0.5")
		// max(0.95, 0.5)=0.95：最高档不动，否则 0.9 的号子会被从 -15 抬到 -25
		if got := scoreOf(0.96); got != 75 {
			t.Errorf("quotaRate=0.96 应仍扣 25 得 75（max(0.95,0.5)=0.95）, got %d", got)
		}
		if got := scoreOf(0.95); got != 85 {
			t.Errorf("quotaRate=0.95 应扣 15 得 85, got %d", got)
		}
		if got := scoreOf(0.8); got != 85 {
			t.Errorf("quotaRate=0.8 应落 -15 档得 85（中间档随参数降到 0.5）, got %d", got)
		}
		if got := scoreOf(0.5); got != 100 {
			t.Errorf("quotaRate=0.5 等于阈值不 >，不应扣分得 100, got %d", got)
		}
	})
}
