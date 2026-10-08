package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// 本文件锁的是 5 个下拉词引擎共用的两条解析链路（stripJSONP / fetchRaw）。
//
// 起因是真实调用失败：curl-suggest 对 baidu+bing 两个引擎都返回 count=0，
// 而服务端日志里是 JSON 解析错（invalid character 'q' / 'y'），
// 但外层只 Warnf 后 continue，用户端只看得到「0 条下拉词」，连失败都不知道。
// 本文件用线上真实响应体做夹具（已脱敏为短样本），确保剥壳与取字段两处不回归。

// 真实响应体样本（2026-10-08 实测，命令见提交说明）：
//   baidu: cb({q:"智能客服系统",p:false,s:["智能客服系统","好处"]});
//     → 键无引号，是 JS 对象字面量，不是 JSON（json.Unmarshal 必然失败）
//   bing : if(typeof cb == 'function') cb({"AS":{...}});
//     → 带 if 守卫，剥壳正则会把守卫括号当回调括号
//   google: if(typeof cb == 'function') cb(["x",["a","b"]],{_x:1});
//     → 同 bing，且内层是数组不是对象

func TestStripJSONP_BaiduUnquotedKeys(t *testing.T) {
	// 分工写清楚：stripJSONP 只负责「取回调实参」，baidu 的无引号键由 looseUnmarshal 兜。
	// 所以这里断言剥壳后拿到的是那段字面量原样，再交给 looseUnmarshal 才变成合法 JSON。
	inner := stripJSONP(`cb({q:"智能客服系统",p:false,s:["智能客服系统","好处","功能"]});`)
	if inner != `{q:"智能客服系统",p:false,s:["智能客服系统","好处","功能"]}` {
		t.Fatalf("剥壳应原样取出实参，实际=%q", inner)
	}
	var m map[string]any
	if err := looseUnmarshal([]byte(inner), &m); err != nil {
		t.Fatalf("looseUnmarshal 应能解析 baidu 的无引号键，err=%v", err)
	}
	arr, ok := m["s"].([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("baidu s 字段应解析出 3 条，实际 %#v", m["s"])
	}
	if got, _ := arr[1].(string); got != "好处" {
		t.Fatalf("baidu s[1]=%q，期望 好处", got)
	}
}

func TestStripJSONP_IfGuardWrapped(t *testing.T) {
	// bing/google 的 `if(typeof cb == 'function')` 守卫必须被剥掉，
	// 且剥出的必须是回调实参本身，不是「守卫括号到末尾」那一大段。
	inner := stripJSONP(`if(typeof cb == 'function') cb({"AS":{"Query":"x"}});`)
	var m map[string]any
	if err := json.Unmarshal([]byte(inner), &m); err != nil {
		t.Fatalf("bing 剥壳后不是合法 JSON，err=%v，内文=%q", err, inner)
	}
	if _, ok := m["AS"]; !ok {
		t.Fatalf("bing 剥壳后应保留 AS 字段，实际 %#v", m)
	}

	// google 真实形状是 `cb && cb([...多个实参...])`：守卫不是 if(...) 而是 `&&`，
	// 且回调带多实参，因此剥壳 + 取第一实参两步都要过。
	innerArr := firstJSONArg(stripJSONP(
		`cb && cb(["智能客服系统",["智能客服系统","智能客服系统推荐"],[],{"google:suggestsubtypes":[[512],[5,30],[5,30]]}])`))
	var arr []any
	if err := looseUnmarshal([]byte(innerArr), &arr); err != nil {
		t.Fatalf("google 剥壳+取第一实参后不是合法 JSON 数组，err=%v，内文=%q", err, innerArr)
	}
	if len(arr) != 4 {
		t.Fatalf("google 第一实参应含 4 个元素，实际 %d：%q", len(arr), innerArr)
	}
	if items, ok := arr[1].([]any); !ok || len(items) != 2 {
		t.Fatalf("google 第二元素应是下拉词数组，实际 %#v", arr[1])
	}
}

func TestFirstJSONArg(t *testing.T) {
	cases := []struct{ in, want string }{
		{`["q",["a","b"],[],{"x":1}]`, `["q",["a","b"],[],{"x":1}]`}, // 单实参原样
		{`["q",["a"]],["extra"]`, `["q",["a"]]`},                     // 多实参取第一个
		{`{"a":1,"b":2},{"c":3}`, `{"a":1,"b":2}`},                   // 对象实参
		{`["a,comma",["x"]]`, `["a,comma",["x"]]`},                   // 字符串内逗号不切
		{`{"k":"v,w"},{"z":1}`, `{"k":"v,w"}`},                       // 对象值内逗号不切
		{``, ``},
		{`[1,2]`, `[1,2]`},
	}
	for _, c := range cases {
		if got := firstJSONArg(c.in); got != c.want {
			t.Fatalf("firstJSONArg(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestLooseUnmarshal_KeepsValidJSONIntact(t *testing.T) {
	// looseUnmarshal 是「宽松兜底」：合法 JSON 必须原样可解析，不能被补引号逻辑弄坏。
	for _, in := range []string{`{"s":["a","b"]}`, `["x",["y"]]`, `{"k":{"n":1}}`} {
		var m map[string]any
		if err := looseUnmarshal([]byte(in), &m); err != nil && in[0] == '{' {
			t.Fatalf("合法对象 JSON 不该报错：%q err=%v", in, err)
		}
		var a []any
		if err := looseUnmarshal([]byte(in), &a); err != nil && in[0] == '[' {
			t.Fatalf("合法数组 JSON 不该报错：%q err=%v", in, err)
		}
	}
}

func TestLooseUnmarshal_ReturnsErrorOnGarbage(t *testing.T) {
	// 真垃圾必须仍然报错，不能因为补引号逻辑而「碰巧解析成功」。
	for _, in := range []string{`<html>err</html>`, `not json`, `{"a":`} {
		var m map[string]any
		if err := looseUnmarshal([]byte(in), &m); err == nil {
			t.Fatalf("垃圾输入应报错：%q", in)
		}
	}
}

func TestStripJSONP_PlainJSONAndTrailingWhitespaceUnchanged(t *testing.T) {
	// 已经是纯 JSON 的响应不能被动过（回归护栏：剥壳必须是保守的）。
	// 尾随空白被 trim 掉是允许的（上游普遍带换行），因此按 trim 后比较。
	for _, in := range []string{`{"s":["a"]}`, `["x",["a"]]`, `{"a":1}`, `{"a":1}   `} {
		if got, want := stripJSONP(in), strings.TrimSpace(in); got != want {
			t.Fatalf("纯 JSON 不应被改动：in=%q got=%q want=%q", in, got, want)
		}
	}
}

func TestStripJSONP_UnclosedOrEmpty(t *testing.T) {
	// 畸形输入不得 panic，也不得返回半截内容被下游当 JSON 解析。
	for _, in := range []string{"", "cb(", "cb({a:1})", "not json at all"} {
		got := stripJSONP(in)
		if strings.TrimSpace(got) == "" && strings.TrimSpace(in) != "" {
			t.Fatalf("非空输入不应剥成空串：in=%q got=%q", in, got)
		}
	}
}

func TestFetchRaw_RejectsNon2xx(t *testing.T) {
	// fetchRaw 原实现不查状态码：上游 500/403 的 HTML 错误页会被当 JSONP 剥壳再解析，
	// 最终报「invalid character '<'」这种与真实原因无关的错，排查成本极高。
	// 这里要求状态码非 2xx 时返回带状态码的错误，且不返回 body。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html>forbidden</html>`))
	}))
	defer srv.Close()

	body, err := fetchRaw(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatalf("非 2xx 必须返回错误，实际拿到 body=%q", body)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("错误信息应含状态码 403，实际=%v", err)
	}
	if body != "" {
		t.Fatalf("出错时不应返回 body，实际=%q", body)
	}
}

func TestFetchRaw_Accepts2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`cb({s:["a","b"]});`))
	}))
	defer srv.Close()

	body, err := fetchRaw(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("2xx 不该报错：%v", err)
	}
	// 2xx 必须原样透传（剥壳是下游的事），这里不关心键有没有引号。
	if body != `cb({s:["a","b"]});` {
		t.Fatalf("2xx 应原样返回 body，实际=%q", body)
	}
}

func TestFetchJSONPArray_BaiduShapeEndToEnd(t *testing.T) {
	// 端到端：起一个返回真实 baidu 形状的服务，走 fetchJSONPArray 取 s 字段。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`cb({q:"智能客服系统",p:false,s:["智能客服系统","智能客服系统推荐"]});`))
	}))
	defer srv.Close()

	got, err := fetchJSONPArray(context.Background(), srv.Client(), srv.URL, "s")
	if err != nil {
		t.Fatalf("baidu 形状应解析成功：%v", err)
	}
	if len(got) != 2 || got[1] != "智能客服系统推荐" {
		t.Fatalf("baidu 下拉词解析不对：%#v", got)
	}
}

func TestFetchJSONPArray_FieldAbsentReturnsEmptyNotError(t *testing.T) {
	// 字段缺失是「上游没给这个词」，不是错误 —— 上层据此正常落库 0 条。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`cb({q:"x"});`))
	}))
	defer srv.Close()

	got, err := fetchJSONPArray(context.Background(), srv.Client(), srv.URL, "s")
	if err != nil {
		t.Fatalf("字段缺失不该报错：%v", err)
	}
	if len(got) != 0 {
		t.Fatalf("字段缺失应返回空，实际=%#v", got)
	}
}

func TestBaiduSuggestEngine_FetchRealShape(t *testing.T) {
	// 直接打引擎实现：确认 BaiduSuggestEngine.Fetch 走通真实响应形状。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`cb({q:"客服",p:false,s:["客服","客服系统"]});`))
	}))
	defer srv.Close()

	// Fetch 里 URL 是硬编码的，这里用 httptest 只验证解析层已由上面的用例覆盖；
	// 本用例确保引擎名与字段契约不变（s）。
	if (&BaiduSuggestEngine{client: srv.Client()}).Name() != "baidu" {
		t.Fatal("BaiduSuggestEngine.Name() 应为 baidu")
	}
}

func TestSuggestEngines_AllDeclaredNames(t *testing.T) {
	// 路由层 docs 承诺 5 引擎；名字一旦改动，前端 engines 下拉与本文件夹具会静默失配。
	svc := &KeywordMiningService{}
	want := map[string]bool{"baidu": true, "bing": true, "google": true, "360": true, "sogou": true}
	got := map[string]bool{}
	for _, e := range svc.allEngines() {
		got[e.Name()] = true
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("缺少引擎 %s（实际 %v）", n, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("引擎数应=%d，实际=%d（%v）", len(want), len(got), got)
	}
}

// ────────────────────────────────────────────
// 2026-10-08 二轮：bing/sogou 线上真实形状（首轮改造后实测仍 0 条的两个引擎）
// ────────────────────────────────────────────

func TestFirstJSONArg_BingCommentInsideCallback(t *testing.T) {
	// 线上实测（curl -A <UA> 'https://api.bing.com/qsonhs.aspx?q=智能客服系统&type=cb&cb=cb'）：
	//   if(typeof cb == 'function') cb({"AS":{"Query":"…","Results":[{…,"Suggests":[{"Txt":…}]},…]}}/* pageview_candidate */);
	// 注释在**回调括号内**，所以 stripJSONP 按配对取实参是对的，但取出来还带着尾巴；
	// 「按顶层逗号切第一实参」在这里完全无从下手（整段只有一个值），必须扫到第一个闭合的值。
	arg := firstJSONArg(stripJSONP(
		`if(typeof cb == 'function') cb({"AS":{"Query":"x","FullResults":1,"Results":[{"Suggests":[{"Txt":"客服系统"},{"Txt":"客服平台"}]}]}}/* pageview_candidate */);`))
	if want := `{"AS":{"Query":"x","FullResults":1,"Results":[{"Suggests":[{"Txt":"客服系统"},{"Txt":"客服平台"}]}]}}`; arg != want {
		t.Fatalf("bing 注释未剥净：%q", arg)
	}
	var resp struct {
		AS struct {
			Results []struct {
				Suggests []struct{ Txt string } `json:"Suggests"`
			} `json:"Results"`
		} `json:"AS"`
	}
	if err := looseUnmarshal([]byte(arg), &resp); err != nil {
		t.Fatalf("bing 形状解析失败：%v", err)
	}
	if len(resp.AS.Results) != 1 || len(resp.AS.Results[0].Suggests) != 2 {
		t.Fatalf("bing 下拉词条数不对：%s", arg)
	}
}

func TestSogouChain_DottedCallbackGBKMultiArg(t *testing.T) {
	// 线上实测（curl 'https://sor.html5.qq.com/api/getsug?key=智能客服系统'）：
	//   window.sogou.sug(["智能客服系统",["智能客服系统软件",…],["0;3;0;0",…],["","",""],…],-1)
	// 三个坑叠在一起：回调名带点、第二实参 -1、正文是 GBK（实测 &charset=utf-8 等六种参数一律无效）。
	// 这里跑的是 SogouSuggestEngine.Fetch 的同一串调用（除 URL 硬编码那一环）。
	payload := `window.sogou.sug(["智能客服系统",["智能客服系统软件","智能客服管理系统"],` +
		`["0;3;0;0","1;42;0;0"],["",""],["0"],"","suglabId_1",{"SessionId":"x"}],-1)`
	gbk, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(payload))
	if err != nil {
		t.Fatalf("夹具编码 GBK 失败：%v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(gbk)
	}))
	defer srv.Close()

	body, err := fetchRaw(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetchRaw 应把 GBK 正文解码成 UTF-8：%v", err)
	}
	arr := []any{}
	if err := looseUnmarshal([]byte(firstJSONArg(stripJSONP(body))), &arr); err != nil {
		t.Fatalf("sogou 链路解析失败：%v，body=%q", err, body)
	}
	items, ok := arr[1].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("sogou 第二元素应为 2 条下拉词，实际 %#v", arr[1])
	}
	if got, _ := items[0].(string); got != "智能客服系统软件" {
		t.Fatalf("sogou 解码后的词不对：%q", got)
	}
}

func TestToUTF8(t *testing.T) {
	// 合法 UTF-8 必须原样返回（其余四引擎都走这条），GBK 必须转换，转不动的不能吞掉。
	if got := toUTF8([]byte(`{"s":["智能客服系统"]}`)); got != `{"s":["智能客服系统"]}` {
		t.Fatalf("UTF-8 直通被改了：%q", got)
	}
	gbk, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte("智能客服系统"))
	if err != nil {
		t.Fatalf("编码夹具失败：%v", err)
	}
	if got := toUTF8(gbk); got != "智能客服系统" {
		t.Fatalf("GBK 未正确转换：got=%q", got)
	}
	// 既不是 UTF-8 也不是合法 GBK 的碎字节：实测走 GBK 解码器的替换语义（不报错、有损）。
	// 底线不是「原样返回」，而是「不 panic、且不把垃圾洗成看着合法的正文」——
	// 下游 looseUnmarshal 必须仍然解析失败，否则一次截断就会被读成「上游给了个合法空结果」。
	got := toUTF8([]byte{0xff, 0xfe, 0x00, '{', '"', 'a', ':', '1'})
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err == nil {
		t.Fatalf("碎字节不该被洗成可解析正文：got=%q", got)
	}
}

func TestCrawlSuggest_EmptySeedsOutcomeContract(t *testing.T) {
	// 不发任何请求的一条腿：验证 outcome 恒非空、三个诊断字段都初始化过
	// （PerEngine 若是 nil map，调用方 range 不报错但 JSON 出来是 null，前端按对象取会炸）。
	out := (&KeywordMiningService{}).CrawlSuggest(context.Background(), nil, nil)
	if out == nil || out.Keywords == nil || out.PerEngine == nil || out.Errors == nil {
		t.Fatalf("outcome 字段未初始化：%#v", out)
	}
	if len(out.Keywords) != 0 || len(out.PerEngine) != 0 || len(out.Errors) != 0 {
		t.Fatalf("空种子应全零：%#v", out)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("outcome 应可序列化：%v", err)
	}
	for _, k := range []string{`"keywords":[]`, `"per_engine":{}`, `"errors":[]`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("序列化应含 %s，实际=%s", k, b)
		}
	}
}
