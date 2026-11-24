// contract_header_test.go 跨端契约版本在**租户端**的两个判据（SaaS 审计 A4）：
//
//	①每一次出站请求都真的带上了 `X-Contract-Version`；
//	②平台回 426 时，这个事实被记进管理端可见的状态，且**只有**受闸路径的 200 才能清掉它。
//
// # 为什么每条负例都要配正对照
//
// ②单独看是"某个 bool 变 true 了"，很容易被写成永远为真的假断言。
// 所以每条记录断言后面都跟一条"平台重新接受 ⇒ 告警必须消失"的正对照 ——
// 否则一个只会 `RecordUpgradeRequired` 而不 `Clear` 的实现（= 告警永远挂着，
// 运维从此不看它了）也能让负例全绿。
package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/contract"
)

// contractStub 一个可切换行为的平台桩：status==0 时回 200。
type contractStub struct {
	status atomic.Int32
	path   atomic.Value // string：最近一次收到的路径
	header atomic.Value // string：最近一次收到的契约版本头
}

func newContractStub(t *testing.T) (*httptest.Server, *contractStub) {
	t.Helper()
	st := &contractStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.path.Store(r.URL.Path)
		st.header.Store(r.Header.Get(contract.HeaderName))
		code := int(st.status.Load())
		if code == 0 {
			code = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		if code == http.StatusUpgradeRequired {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"code":426,"msg":"契约版本不兼容：平台最低支持 v1，本请求声明 v0"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
	}))
	t.Cleanup(srv.Close)
	contract.ClearUpgradeRequired()
	t.Cleanup(contract.ClearUpgradeRequired)
	return srv, st
}

// TestEveryOutboundRequestCarriesContractHeader 钉住"没有一条出站通道漏带版本头"。
//
// 逐个走**真实的入口函数**（不是直接调 constructor）：A4 的失效形态就是
// "新增了一个客户端，忘了带这个头"，而那种漏只可能在具体入口里出现。
func TestEveryOutboundRequestCarriesContractHeader(t *testing.T) {
	srv, st := newContractStub(t)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "ret"})

	c := NewPlatformClient("mk-1")
	c.httpClient = srv.Client()

	assertHeader := func(what string) {
		t.Helper()
		got, _ := st.header.Load().(string)
		if got != contract.HeaderValue() {
			t.Errorf("%s 的出站请求没带 %s=%q（实际 %q）—— 平台侧该组挂着 "+
				"ContractHandshake()，漏带的后果是这条功能恒 426",
				what, contract.HeaderName, contract.HeaderValue(), got)
		}
	}

	// ① 商户面主通道（doRetry）
	if err := c.do("GET", "/merchant-api/orders", nil, nil); err != nil {
		t.Fatalf("do(/merchant-api/orders): %v", err)
	}
	assertHeader("商户面主通道")

	// ② 安装上报
	if err := c.ReportInstall(&ReportInstallReq{InstallID: "i-1"}); err != nil {
		t.Fatalf("ReportInstall: %v", err)
	}
	assertHeader("ReportInstall")

	// ③ 心跳上报
	if err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "i-1"}); err != nil {
		t.Fatalf("ReportHeartbeat: %v", err)
	}
	assertHeader("ReportHeartbeat")

	// ④ 连通性探针（平台侧没挂闸，但走同一个构造入口 —— 不留"要不要带头"的第二份清单）
	if err := c.CheckConnection(); err != nil {
		t.Fatalf("CheckConnection: %v", err)
	}
	assertHeader("CheckConnection")

	// ⑤ LLM 代理（最先被平台升级打到的那条）
	pc := SetLLMProxyClientForTest(srv.URL, "mk-1", srv.Client())
	if _, err := pc.ChatCompletions(t.Context(), "plat-p", []byte(`{"model":"m"}`), LLMCallMeta{}); err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	assertHeader("LLM 代理")
}

// TestContract426RecordedAndCleared 426 ⇒ 记进可见状态；随后 200 ⇒ 清掉（正对照）。
func TestContract426RecordedAndCleared(t *testing.T) {
	srv, st := newContractStub(t)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "ret"})

	c := NewPlatformClient("mk-2")
	c.httpClient = srv.Client()

	// 第一档：平台回 426 ⇒ 状态里必须有它，且原因来自平台文案（运维据此知道升哪一端）
	st.status.Store(http.StatusUpgradeRequired)
	if err := c.do("GET", "/merchant-api/orders", nil, nil); err == nil {
		t.Fatal("平台回 426 时 do() 必须返回错误，实际 nil")
	}
	got, ok := contract.UpgradeRequired()
	if !ok {
		t.Fatal("平台已回 426，但租户端没记录 —— 管理端就永远看不到「平台要求本机升级」，" +
			"A4 的原始症状（失败被日志吞掉）原样保留")
	}
	if !strings.Contains(got.Message, "契约版本不兼容") {
		t.Errorf("记录的原因应取自平台 426 的信封 msg，实际 %q —— 一句通用的「平台返回 426」"+
			"会让运维不知道要升哪一端", got.Message)
	}
	if got.Declared != contract.Version {
		t.Errorf("状态里应带上本机声明的版本 %d，实际 %d", contract.Version, got.Declared)
	}

	// 第二档（正对照）：平台重新接受 ⇒ 告警必须消失。
	// 少了这一条，一个"只记不清"的实现也能让上面全绿 —— 而那种实现的后果是
	// 告警一旦亮起就永远亮着，两次之后没人再看它。
	st.status.Store(0)
	if err := c.do("GET", "/merchant-api/orders", nil, nil); err != nil {
		t.Fatalf("平台回 200 时 do() 不该报错: %v", err)
	}
	if st, ok := contract.UpgradeRequired(); ok {
		t.Fatalf("平台已恢复正常，告警却没清（仍是 %q）—— 告警会变成永久噪音", st.Message)
	}
}

// TestNonGuardedSuccessDoesNotClearWarning 未挂闸端点的 200 **不许**清告警。
//
// 这是最容易写错的一条：`/health`、登录、`/platform/**`（平台自身 Web 前端的 API）、
// 贡献者接口都没有契约闸，它们回 200 只说明"平台活着"，完全不代表"平台接受我们的
// 契约版本"。一次周期性探针把告警清掉 ⇒ 真正的 426 被静默掩盖，而且掩得很有规律，
// 排查时几乎不可能想到是探针干的好事。
//
// 判据直接打在**决策点**上：`noteContractVerdict` 是全包唯一决定"这次响应算不算裁决"
// 的函数，所以这里既能表达语义（受闸 vs 豁免），也不会因为"某个入口忘了调它"而变绿。
// 真实入口的覆盖由上面的 TestEveryOutboundRequestCarriesContractHeader 与下一条负责。
func TestNonGuardedSuccessDoesNotClearWarning(t *testing.T) {
	srv, st := newContractStub(t)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "ret"})

	c := NewPlatformClient("mk-3")
	c.httpClient = srv.Client()

	// 先用一条受闸路径把告警立起来（真实 HTTP，不走白盒）。
	st.status.Store(http.StatusUpgradeRequired)
	if err := c.do("GET", "/merchant-api/orders", nil, nil); err == nil {
		t.Fatal("前置条件不成立：426 应让 do() 报错")
	}
	if _, ok := contract.UpgradeRequired(); !ok {
		t.Fatal("前置条件不成立：426 应被记录")
	}

	// 豁免路径的 200 ⇒ 没有裁决权，告警必须还在。
	for _, p := range []string{"/health", "/api/auth/login", "/platform/dashboard", "/contributor-api/v1/assets"} {
		noteContractVerdict(p, http.StatusOK, []byte(`{"code":200,"msg":"ok"}`))
		if _, ok := contract.UpgradeRequired(); !ok {
			t.Fatalf("%q 的 200 把 426 告警清掉了 —— 它没有契约闸，回 200 只说明平台活着，"+
				"不代表平台接受我们的契约版本。探针/登录是周期性的，这种掩盖几乎无法排查", p)
		}
	}

	// 正对照：受闸路径的 200 **必须**清掉（否则上面那段只是在测"永远不清"）。
	noteContractVerdict("/merchant-api/orders", http.StatusOK, []byte(`{"code":200,"msg":"ok"}`))
	if st, ok := contract.UpgradeRequired(); ok {
		t.Fatalf("受闸路径恢复 200 后告警应清除（仍是 %q）—— 告警会变成永久噪音，"+
			"之后没人再看它", st.Message)
	}
}

// TestHeartbeatSuccessClearsWarning 心跳的 200 也必须能清告警。
//
// 这条单独钉一次，因为它是"只在主通道清"最容易漏掉的地方：心跳是周期性调用，
// 运维开完兼容窗口后，平台不再拒我们了，但告警只会在**商户面有流量时**才消失。
// 表现是管理员盯着一个永远不更新的 426 —— 于是没人再信这个字段。
func TestHeartbeatSuccessClearsWarning(t *testing.T) {
	srv, st := newContractStub(t)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s3c" + "ret"})

	c := NewPlatformClient("mk-4")
	c.httpClient = srv.Client()

	st.status.Store(http.StatusUpgradeRequired)
	if err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "i-4"}); err == nil {
		t.Fatal("前置条件不成立：426 应让 ReportHeartbeat 报错")
	}
	if _, ok := contract.UpgradeRequired(); !ok {
		t.Fatal("前置条件不成立：心跳的 426 应被记录（这正是 A4 里「失败被日志吞掉」的那条通道）")
	}

	st.status.Store(0)
	if err := c.ReportHeartbeat(&ReportHeartbeatReq{InstallID: "i-4"}); err != nil {
		t.Fatalf("心跳 200 时不该报错: %v", err)
	}
	if st, ok := contract.UpgradeRequired(); ok {
		t.Fatalf("心跳恢复 200 后告警应清除（仍是 %q）—— 运维开完窗口也看不到告警消失",
			st.Message)
	}
}

// TestContractGuardedPrefixesMatchPlatformList 钉住"哪些前缀受闸"这份对账单。
//
// 它不是装饰：少登记 ⇒ 那条通道的 426 不会被记录（管理端看不见）；
// 多登记 ⇒ 一次普通 200 会把别处的 426 清掉。两种都静默，所以用一条用例钉住，
// 改这里的人必须同时看平台端的 CONTRACT_GUARDED。
func TestContractGuardedPrefixesMatchPlatformList(t *testing.T) {
	want := []string{"/merchant-api/", "/llm-api/v1/", "/api/platform/"}
	if len(contractGuardedPrefixes) != len(want) {
		t.Fatalf("受闸前缀清单长度变了（%v）—— 必须与平台端 checkpoint 清单 CONTRACT_GUARDED 同步",
			contractGuardedPrefixes)
	}
	for i, p := range want {
		if contractGuardedPrefixes[i] != p {
			t.Errorf("受闸前缀第 %d 项应为 %q，实际 %q（与平台端 CONTRACT_GUARDED 脱钩）",
				i, p, contractGuardedPrefixes[i])
		}
	}
	// 正对照：清单里的必须认，清单外的必须不认 —— 否则 isContractGuardedPath 恒真/恒假也能过。
	for _, p := range want {
		if !isContractGuardedPath(p + "x") {
			t.Errorf("%q 应被判为受闸路径", p+"x")
		}
	}
	for _, p := range []string{"/health", "/api/auth/login", "/platform/dashboard", "/contributor-api/v1/x"} {
		if isContractGuardedPath(p) {
			t.Errorf("%q 不该被判为受闸路径（它没有契约闸，它的 200 不能清告警）", p)
		}
	}
}
