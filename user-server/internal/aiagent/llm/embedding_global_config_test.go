package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/secrets"
)

// embedding.global_override 的两条缝（读/写）。
//
// 这一组用例重点钉三件在这个文件里改过的事：
//   - 本层**不再自建缓存**：每次读都回源注入的读取口。上一版在这里另有一份 60s 缓存，
//     叠在参数中心那份之上，管理台点"保存"后最坏要等两分钟才生效。
//   - 写口的来路（actor id）必须一路传到参数中心，否则"是谁把向量服务指到外部端点"查不到。
//   - 没装配时读回 nil、写回错误，而不是回落到"直接写库"。

func resetEmbeddingOverrideSeams(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetEmbeddingOverrideProvider(nil)
		SetEmbeddingOverrideWriter(nil)
	})
}

func TestGetGlobalEmbeddingOverrideUnwiredIsNil(t *testing.T) {
	resetEmbeddingOverrideSeams(t)
	if got := GetGlobalEmbeddingOverride(); got != nil {
		t.Errorf("未注入读取口时应回 nil，got %+v", got)
	}
	err := SetGlobalEmbeddingOverride(&GlobalEmbeddingOverride{Enabled: true}, 1)
	if !errors.Is(err, ErrEmbeddingOverrideUnwired) {
		t.Errorf("未注入写口应回 ErrEmbeddingOverrideUnwired，got %v", err)
	}
}

func TestGetGlobalEmbeddingOverrideEmptyAndBadJSON(t *testing.T) {
	resetEmbeddingOverrideSeams(t)

	SetEmbeddingOverrideProvider(func(context.Context) string { return "" })
	if got := GetGlobalEmbeddingOverride(); got != nil {
		t.Errorf("参数值为空串（未配置）应回 nil，got %+v", got)
	}

	SetEmbeddingOverrideProvider(func(context.Context) string { return "{" })
	if got := GetGlobalEmbeddingOverride(); got != nil {
		t.Errorf("JSON 读不出时应回 nil，got %+v", got)
	}
}

// 每次调用都要回源读取口：这一格锁的是"本层不再自建第二份缓存"。
// 允许一份 60s 的本地副本就能把"在线改、免重启"这句话变成两句 60s 的叠加。
func TestGetGlobalEmbeddingOverrideRereadsProviderEachCall(t *testing.T) {
	resetEmbeddingOverrideSeams(t)

	calls := 0
	value := `{"enabled":true,"base_url":"https://a.example.com/v1","api_key":"","model":"m1"}`
	SetEmbeddingOverrideProvider(func(context.Context) string {
		calls++
		return value
	})

	first := GetGlobalEmbeddingOverride()
	if first == nil || first.Model != "m1" {
		t.Fatalf("第一次读 = %+v，期望 model=m1", first)
	}
	value = `{"enabled":true,"base_url":"https://b.example.com/v1","api_key":"","model":"m2"}`
	second := GetGlobalEmbeddingOverride()
	if second == nil || second.Model != "m2" {
		t.Errorf("改动后立刻读 = %+v，期望 model=m2（读到 m1 说明本层又攒了一份缓存）", second)
	}
	if calls != 2 {
		t.Errorf("读取口被调用 %d 次，期望 2 次", calls)
	}
}

// 关掉配置只写 enabled=false 就够：读侧回的那份必须是 Enabled=false，
// 而不是把上一次的 base_url 留在结构里让调用侧误判。
func TestGetGlobalEmbeddingOverrideDisabledKeepsFields(t *testing.T) {
	resetEmbeddingOverrideSeams(t)
	SetEmbeddingOverrideProvider(func(context.Context) string {
		return `{"enabled":false,"base_url":"https://a.example.com/v1","api_key":"","model":"m1"}`
	})
	got := GetGlobalEmbeddingOverride()
	if got == nil {
		t.Fatal("读到 nil，期望一份 Enabled=false 的配置")
	}
	if got.Enabled {
		t.Errorf("Enabled = true，期望 false")
	}
}

// Set → 参数中心 → Get 的闭环：写进去的那份必须原样读回来，
// 且密文不许以明文形态留在落库的串里（secrets 就绪时）。
func TestEmbeddingOverrideSetThenGetRoundTrip(t *testing.T) {
	resetEmbeddingOverrideSeams(t)

	var stored string
	var gotActor uint
	SetEmbeddingOverrideProvider(func(context.Context) string { return stored })
	SetEmbeddingOverrideWriter(func(_ context.Context, raw string, actorID uint) error {
		stored, gotActor = raw, actorID
		return nil
	})

	const plainKey = "sk-unit-test-key"
	in := &GlobalEmbeddingOverride{Enabled: true, BaseURL: "https://api.example.com/v1", APIKey: plainKey, Model: "BAAI/bge-m3"}
	if err := SetGlobalEmbeddingOverride(in, 4242); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	if gotActor != 4242 {
		t.Errorf("写口收到的 actor = %d，期望 4242（审计来路丢了）", gotActor)
	}
	if stored == "" {
		t.Fatal("写口没有收到任何内容")
	}
	if secrets.Ready() && strings.Contains(stored, plainKey) {
		t.Errorf("落库串里出现明文 api_key：%s", stored)
	}

	back := GetGlobalEmbeddingOverride()
	if back == nil {
		t.Fatalf("Set 之后读回 nil，stored=%s", stored)
	}
	if back.Enabled != in.Enabled || back.BaseURL != in.BaseURL || back.Model != in.Model {
		t.Errorf("读回的字段与写入不一致：got %+v want %+v", back, in)
	}
	if back.APIKey != plainKey {
		t.Errorf("读回的 api_key = %q，期望解密后回到原文（加密-解密必须对称）", back.APIKey)
	}
}

func TestSetGlobalEmbeddingOverrideNilIsNoop(t *testing.T) {
	resetEmbeddingOverrideSeams(t)
	called := false
	SetEmbeddingOverrideWriter(func(context.Context, string, uint) error {
		called = true
		return nil
	})
	if err := SetGlobalEmbeddingOverride(nil, 1); err != nil {
		t.Errorf("Set(nil) 应回 nil，got %v", err)
	}
	if called {
		t.Error("Set(nil) 不该走到写口")
	}
}

// 写口报错必须原样上抛：管理台那句"保存失败"是运维唯一的线索，
// 吞掉它会让页面显示已保存而库里没动。
func TestSetGlobalEmbeddingOverridePropagatesWriterError(t *testing.T) {
	resetEmbeddingOverrideSeams(t)
	boom := errors.New("value type validation failed")
	SetEmbeddingOverrideWriter(func(context.Context, string, uint) error { return boom })
	err := SetGlobalEmbeddingOverride(&GlobalEmbeddingOverride{Enabled: true, BaseURL: "u", Model: "m"}, 7)
	if !errors.Is(err, boom) {
		t.Errorf("got %v，期望原样上抛 %v", err, boom)
	}
}
