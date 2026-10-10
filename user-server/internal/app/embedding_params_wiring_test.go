package app

import (
	"context"
	"strings"
	"testing"

	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// embedding 组只有一条，但它是**双向**的一条：读口给 DefaultConfig、写口给管理端 PUT。
// 因此这里的用例把两条腿分别钉死，少一条的表现都不一样：
//   - 只接写口：管理台保存成功、库里也有值，而运行期永远读内置默认 —— "改了不生效"。
//   - 只接读口：PUT 直接报"写口未注入"，比不生效更早暴露，但同样是一处漏接。

var wantEmbeddingWired = []string{"embedding.global_override"}

func resetEmbeddingProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		llmpkg.SetEmbeddingOverrideProvider(nil)
		llmpkg.SetEmbeddingOverrideWriter(nil)
	})
}

func newEmbeddingParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetEmbeddingProviders(t)
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no DB")
	}
	if err := service.SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := service.NewConfigParamService(database)
	service.SetGlobalForTest(svc)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	return svc
}

func TestWireEmbeddingConfigParamsKeys(t *testing.T) {
	resetEmbeddingProviders(t)
	got := WireEmbeddingConfigParams()
	if len(got) != len(wantEmbeddingWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot = %v", len(got), len(wantEmbeddingWired), got)
	}
	for i, k := range got {
		if k != wantEmbeddingWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantEmbeddingWired[i])
		}
	}
}

func TestWiredEmbeddingKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantEmbeddingWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在：接了线但没有参数行，改了也没人看得到", k)
		}
	}
}

// 读腿：参数中心改了值，DefaultConfig 那一条读取路径（GetGlobalEmbeddingOverride）必须跟着变。
func TestWireEmbeddingConfigParamsReadLegReachesReader(t *testing.T) {
	svc := newEmbeddingParamFixture(t)
	ctx := context.Background()

	if got := llmpkg.GetGlobalEmbeddingOverride(); got != nil {
		t.Fatalf("接线前读到 %+v，期望 nil（未注入读取口不该回一份配置）", got)
	}

	WireEmbeddingConfigParams()

	// 种子默认是"关"：接线后读到的应该是那份关掉的配置，而不是 nil。
	off := llmpkg.GetGlobalEmbeddingOverride()
	if off == nil {
		t.Fatal("接线后读到 nil，期望种子默认那份")
	}
	if off.Enabled {
		t.Errorf("种子默认应当是 Enabled=false，got %+v", off)
	}

	const on = `{"enabled":true,"base_url":"https://api.siliconflow.cn/v1","api_key":"","model":"BAAI/bge-m3"}`
	if err := svc.UpdateValue(ctx, "embedding", "global_override", on, 1); err != nil {
		t.Fatalf("改 embedding.global_override 失败：%v", err)
	}
	got := llmpkg.GetGlobalEmbeddingOverride()
	if got == nil {
		t.Fatal("改了参数后读到 nil")
	}
	if !got.Enabled || got.BaseURL != "https://api.siliconflow.cn/v1" || got.Model != "BAAI/bge-m3" {
		t.Errorf("参数中心的值未被读走：got %+v", got)
	}
}

// 写腿：SetGlobalEmbeddingOverride 必须真的落到参数中心那一行，并且留下一条带操作者的审计。
// 这一格是本次收敛相对旧裸 SQL 的全部增量：旧写法两张都没有（值在另一张表里，且没人知道是谁改的）。
func TestWireEmbeddingConfigParamsWriteLegReachesParamAndAudit(t *testing.T) {
	svc := newEmbeddingParamFixture(t)
	ctx := context.Background()
	const actorID = uint(9001)

	WireEmbeddingConfigParams()

	if err := llmpkg.SetGlobalEmbeddingOverride(&llmpkg.GlobalEmbeddingOverride{
		Enabled: true, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "text-embedding-v3",
	}, actorID); err != nil {
		t.Fatalf("SetGlobalEmbeddingOverride 失败：%v", err)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List 失败：%v", err)
	}
	var stored string
	for _, p := range list {
		if p.Group == "embedding" && p.Key == "global_override" {
			stored = p.Value
		}
	}
	if !strings.Contains(stored, "text-embedding-v3") {
		t.Fatalf("参数中心的值里没有写入的 model：param_value = %q", stored)
	}
	if !strings.Contains(stored, `"enabled":true`) {
		t.Errorf("参数中心的值里没有写入的 enabled：param_value = %q", stored)
	}

	logs, err := svc.AuditLogs(ctx, 20)
	if err != nil {
		t.Fatalf("AuditLogs 失败：%v", err)
	}
	var found bool
	for _, l := range logs {
		if l.ParamGroup == "embedding" && l.ParamKey == "global_override" && l.ActorID == actorID {
			found = true
		}
	}
	if !found {
		t.Errorf("写入没有留下带操作者 %d 的审计记录（取到的是 %d 条）", actorID, len(logs))
	}
}

// 接线之后写腿必须立即可读：读口与写口共用参数中心那一份缓存，
// 写完的下一次读不许还看到旧值（这一格锁的是"本层不再自建第二份缓存"）。
func TestWireEmbeddingConfigParamsWriteThenReadIsImmediatelyVisible(t *testing.T) {
	svc := newEmbeddingParamFixture(t)
	ctx := context.Background()
	WireEmbeddingConfigParams()

	if err := llmpkg.SetGlobalEmbeddingOverride(&llmpkg.GlobalEmbeddingOverride{
		Enabled: true, BaseURL: "https://first.example.com/v1", Model: "m-first",
	}, 1); err != nil {
		t.Fatalf("第一次写失败：%v", err)
	}
	afterFirst := llmpkg.GetGlobalEmbeddingOverride()
	if afterFirst == nil || afterFirst.Model != "m-first" {
		t.Fatalf("写完立刻读不到：got %+v", afterFirst)
	}

	if err := svc.UpdateValue(ctx, "embedding", "global_override",
		`{"enabled":true,"base_url":"https://second.example.com/v1","api_key":"","model":"m-second"}`, 1); err != nil {
		t.Fatalf("第二次写失败：%v", err)
	}
	afterSecond := llmpkg.GetGlobalEmbeddingOverride()
	if afterSecond == nil || afterSecond.Model != "m-second" {
		t.Errorf("第二次写之后仍读到上一份：got %+v", afterSecond)
	}
}

// 无库时（没跑迁移、装配早于建表）两条缝都要仍可用：读回种子默认那份、写回一个错误，
// 而不是在第一次请求时 panic 或"看起来保存成功了"。
func TestWireEmbeddingConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetEmbeddingProviders(t)

	if got := WireEmbeddingConfigParams(); len(got) != len(wantEmbeddingWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d（接线不该依赖库是否可用）", len(got), len(wantEmbeddingWired))
	}
	// Get* 走 nil-safe stub 的 fallback：空串 = 未配置 = nil。
	if got := llmpkg.GetGlobalEmbeddingOverride(); got != nil {
		t.Errorf("无库时读到 %+v，期望 nil", got)
	}
	if err := llmpkg.SetGlobalEmbeddingOverride(&llmpkg.GlobalEmbeddingOverride{Enabled: true}, 1); err == nil {
		t.Error("无库时写入应报错，而不是静默成功")
	}
}
