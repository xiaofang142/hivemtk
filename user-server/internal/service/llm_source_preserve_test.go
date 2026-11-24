package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
)

// 回归：前端"启用/停用"开关只发 {enabled}，绝不能把平台下发的 provider
// 降级成"自带 Key"。
//
// 事故链（第一版实现）：
//   UpdateModel 构造新 ProviderConfig 时没有带 Source → 落库 source 为空串
//   → ProviderSource() 判成 self
//   → 平台垫资 provider 的 Key 根本不在商户侧（平台不下发，ADR-M7）
//   → 此后每次调用必然 401，而用户做的事只是点了一下"启用"开关。
//
// 判据放在**真实的 UpdateModel** 上，而不是只测取值语义：
// 上一版的用例只验了 ProviderConfig 的字段语义，UpdateModel 里少赋一个字段
// 它照样绿 —— 而事故恰恰发生在赋值那一行。

func TestUpdateModelPreservesPlatformSource(t *testing.T) {
	d := llm.NewDispatcher(llm.NewLLMService())
	d.AddProvider(llm.ProviderConfig{
		Name: "plat", Model: "gpt-4o", DisplayName: "平台模型",
		Source: "platform", PlanCode: "P1",
		CostPer1kInput: 0.5, CostPer1kOutput: 3,
		Enabled: true, QualityScore: 0.9,
	})
	svc := &LLMRoutingService{dispatcher: d}

	// 前端只发 enabled（其余字段全是零值）
	err := svc.UpdateModel(context.Background(), "plat", LLMProviderInfo{
		Name:    "plat",
		Enabled: false,
	})
	if err != nil {
		// 落库失败（测试库未初始化）是可接受的，只要更新后的内存态已正确 ——
		// 出问题的那一行就在内存态的构造里。
		t.Logf("UpdateModel 返回（测试环境无库）：%v", err)
	}

	got := d.GetAllProviders()
	var p llm.ProviderConfig
	for _, x := range got {
		if x.Name == "plat" {
			p = x
		}
	}
	if p.Name == "" {
		t.Fatal("更新后 provider 不存在")
	}
	if !p.IsPlatformProvider() {
		t.Error("仅切 enabled 后 provider 被降级成了 self：此后每次调用必 401")
	}
	if p.PlanCode != "P1" {
		t.Errorf("套餐号丢失：%q", p.PlanCode)
	}
	if p.EffectiveInputCostPer1k() != 0.5 || p.EffectiveOutputCostPer1k() != 3 {
		t.Errorf("分列单价丢失：in=%v out=%v",
			p.EffectiveInputCostPer1k(), p.EffectiveOutputCostPer1k())
	}
	if p.Enabled {
		t.Error("enabled 未生效")
	}
}

func TestCreateModelAlwaysSelf(t *testing.T) {
	d := llm.NewDispatcher(llm.NewLLMService())
	svc := &LLMRoutingService{dispatcher: d}

	// 商户伪造 source=platform 想让调用走代理 —— 必须被强制成 self
	err := svc.CreateModel(context.Background(), LLMProviderInfo{
		Name:         "fake",
		Model:        "x",
		BaseURL:      "https://api.example.com",
		APIKey:       "sk-" + "fake",
		Source:       "platform",
		Enabled:      true,
		QualityScore: 0.5,
		MaxRPM:       10,
	})
	if err != nil {
		t.Logf("CreateModel 返回（测试环境无库）：%v", err)
	}
	var p llm.ProviderConfig
	for _, x := range d.GetAllProviders() {
		if x.Name == "fake" {
			p = x
		}
	}
	if p.Name == "" {
		t.Fatalf("CreateModel 未落到内存，err=%v", err)
	}
	if p.IsPlatformProvider() {
		t.Fatal("商户不得通过接口声明自己是 platform provider：平台上并没有它，" +
			"每次调用都会 404，而商户从界面上看不出是自己声明错了")
	}
}
