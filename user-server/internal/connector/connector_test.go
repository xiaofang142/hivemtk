package connector

// connector_test.go TB-06b：连接器方言层契约。
//
// 判据（对应清单行「虚构平台零代码接入」）：
//  1. generic 方言：虚构平台（从未写码支持过）经 preset 数据（端点+参数映射+能力标签）
//     注册→Call→归一化信封——全程零平台专属业务代码（G2 判据）；
//  2. kuaidi100 方言：签名向量（MD5(param+key+customer) 大写）+form 形态+非 200 错误面；
//  3. SSRF：生产口径默认拒绝环回/私有网段（复用 tooluse 单一事实源）；
//  4. 注册表：重复注册 panic/未注册方言 fail-closed/List 升序；
//  5. 归一化：JSON 对象→data / JSON 数组→items / 非 JSON→raw.string。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

// ---------- 夹具 ----------

func tb06bServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func tb06bCfg(baseURL, provider string) ConnectorConfig {
	return ConnectorConfig{
		BaseURL:  baseURL,
		Provider: provider,
		Auth:     ConnectorAuth{Type: "bearer", Key: "test-key"},
	}
}

// ---------- G2 判据：虚构平台零代码接入 ----------

func TestConnector_FictionalPlatformZeroCode(t *testing.T) {
	// 虚构平台「fakeMart」：从未有专用代码；preset 数据描述其端点+参数映射+能力标签。
	srv, hits := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/orders" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("platform_order_id") != "FM-1" { // ParamMap: order_id→platform_order_id
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"order_id":"FM-1","status":"shipped","tracking":"SF123"}`))
	})
	ex := NewExecutorWithValidator(func(string) error { return nil }) // 测试 seam
	ep := EndpointConfig{
		Name:         "query_order",
		Method:       "GET",
		Path:         "/api/v1/orders",
		Capabilities: []string{"query_order"},
		ParamMap:     map[string]string{"order_id": "platform_order_id"},
	}
	out, err := ex.Call(context.Background(), tb06bCfg(srv.URL, "generic"), ep,
		map[string]any{"order_id": "FM-1"})
	if err != nil {
		t.Fatalf("虚构平台调用失败: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("fake 服务器应恰被调 1 次, got %d", hits.Load())
	}
	if out["ok"] != true {
		t.Fatalf("归一化信封 ok 应 true, got %v", out)
	}
	data, _ := out["data"].(map[string]any)
	if data == nil || data["status"] != "shipped" {
		t.Fatalf("data 应含平台原始字段, got %v", out["data"])
	}
}

// ---------- 归一化三形态 ----------

func TestConnector_NormalizeThreeShapes(t *testing.T) {
	ex := NewExecutorWithValidator(func(string) error { return nil })
	cases := []struct {
		name  string
		body  string
		check func(map[string]any) bool
	}{
		{"对象→data", `{"a":1}`, func(m map[string]any) bool { _, ok := m["data"]; return ok }},
		{"数组→items", `[{"x":1}]`, func(m map[string]any) bool { _, ok := m["items"]; return ok }},
		{"非 JSON→raw.string", `plain text`, func(m map[string]any) bool {
			raw, ok := m["raw"].(map[string]any)
			return ok && raw["text"] == "plain text"
		}},
	}
	for i, tc := range cases {
		srv, _ := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		})
		ep := EndpointConfig{Name: fmt.Sprintf("ep%d", i), Method: "GET", Path: "/"}
		out, err := ex.Call(context.Background(), tb06bCfg(srv.URL, "generic"), ep, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !tc.check(out) {
			t.Fatalf("%s: 信封形态不符, got %v", tc.name, out)
		}
	}
}

// ---------- kuaidi100 方言：签名向量+form 形态 ----------

func TestConnector_Kuaidi100SignVector(t *testing.T) {
	var gotForm url.Values
	var gotCT string
	srv, _ := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.Form
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":200,"message":"ok","data":[{"time":"09:00","status":"运输中"}]}`))
	})
	cfg := ConnectorConfig{
		BaseURL:  srv.URL,
		Provider: "kuaidi100",
		Auth:     ConnectorAuth{Type: "api_key", Key: "SECRETKEY", Header: "CUSTOMER100"},
	}
	ep := EndpointConfig{Name: "query_express", Method: "POST", Path: "/poll/query.do"}
	ex := NewExecutorWithValidator(func(string) error { return nil })
	out, err := ex.Call(context.Background(), cfg, ep, map[string]any{"carrier": "SF", "tracking_no": "SF123"})
	if err != nil {
		t.Fatalf("kuaidi100 调用失败: %v", err)
	}
	// form 形态
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("应为 form 编码, got %q", gotCT)
	}
	// param JSON：key 排序稳定
	var param map[string]any
	if err := json.Unmarshal([]byte(gotForm.Get("param")), &param); err != nil {
		t.Fatalf("param 应为 JSON: %v", err)
	}
	if param["carrier"] != "SF" || param["tracking_no"] != "SF123" {
		t.Fatalf("参数映射不符: %v", param)
	}
	// 签名向量：MD5(param+key+customer) 大写
	expectSign := strings.ToUpper(md5Sum(gotForm.Get("param") + "SECRETKEY" + "CUSTOMER100"))
	if gotForm.Get("sign") != expectSign {
		t.Fatalf("签名不符: got %s want %s", gotForm.Get("sign"), expectSign)
	}
	if gotForm.Get("customer") != "CUSTOMER100" {
		t.Fatalf("customer 应为凭证侧 Header 值, got %q", gotForm.Get("customer"))
	}
	// 归一化：data 数组→items
	items, ok := out["items"].([]map[string]any)
	if !ok || len(items) != 1 || items[0]["status"] != "运输中" {
		t.Fatalf("轨迹节点应进 items, got %v", out)
	}
}

// ---------- 错误面 ----------

func TestConnector_ErrorSurfaces(t *testing.T) {
	// 未注册方言
	ex := NewExecutorWithValidator(func(string) error { return nil })
	if _, err := ex.Call(context.Background(), tb06bCfg("https://example.com", "no_such_dialect"), EndpointConfig{Name: "x"}, nil); err == nil {
		t.Fatal("未注册方言应 fail-closed")
	}
	// base_url 空
	if _, err := ex.Call(context.Background(), tb06bCfg("", "generic"), EndpointConfig{Name: "x"}, nil); err == nil {
		t.Fatal("空 base_url 应拒绝")
	}
	// 平台非 200
	srv, _ := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := ex.Call(context.Background(), tb06bCfg(srv.URL, "generic"), EndpointConfig{Name: "x", Method: "GET", Path: "/"}, nil)
	if err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("平台非 200 应原样进 error, got %v", err)
	}
	// kuaidi100 业务失败（status!=200 信封）
	srv2, _ := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":400,"message":"参数错误"}`))
	})
	cfg := ConnectorConfig{BaseURL: srv2.URL, Provider: "kuaidi100", Auth: ConnectorAuth{Type: "api_key", Key: "k", Header: "c"}}
	_, err = ex.Call(context.Background(), cfg, EndpointConfig{Name: "x", Method: "POST", Path: "/"}, nil)
	if err == nil || !strings.Contains(err.Error(), "kuaidi100 status=400") {
		t.Fatalf("kuaidi100 业务失败应进 error, got %v", err)
	}
}

// ---------- SSRF：生产口径默认严格 ----------

func TestConnector_SSRFDefaultStrict(t *testing.T) {
	ex := NewExecutor() // 生产构造
	if err := ex.baseURLValidator("http://127.0.0.1:9999"); err == nil {
		t.Fatal("环回地址应被默认口径拒绝")
	}
	if err := ex.baseURLValidator("http://169.254.169.254/latest"); err == nil {
		t.Fatal("元数据地址应被默认口径拒绝")
	}
	// 与 tooluse 单一事实源同口径（同一函数）
	if tooluse.ValidateOpenAPIBaseURL("http://10.0.0.1/x") == nil {
		t.Fatal("私有网段应拒绝（与 tooluse 同口径）")
	}
}

// ---------- 注册表 ----------

func TestConnector_Registry(t *testing.T) {
	if Get("generic") == nil || Get("kuaidi100") == nil {
		t.Fatal("内置方言应已注册（init）")
	}
	if Get("no_such") != nil {
		t.Fatal("未注册方言应 nil")
	}
	list := List()
	if len(list) < 2 || list[0] > list[1] {
		t.Fatalf("List 应升序, got %v", list)
	}
	// 重复注册 panic
	defer func() {
		if recover() == nil {
			t.Fatal("重复注册应 panic（配置错误即暴露）")
		}
	}()
	Register(GenericDialect{})
}

// ---------- 管家婆方言 + Ping ----------

func TestConnector_GraspDialect_FormAndSign(t *testing.T) {
	var gotSign, gotServerNo, gotMethod string
	srv, _ := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotSign = r.Form.Get("sign")
		gotServerNo = r.Form.Get("server_no")
		gotMethod = r.Form.Get("sign_method")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	})
	cfg := ConnectorConfig{
		BaseURL:  srv.URL,
		Provider: "grasp_hmac",
		Auth:     ConnectorAuth{Type: "hmac", Key: "SERVERKEY", Header: "SERVERNO"},
	}
	ep := EndpointConfig{Name: "saleorder_save", Method: "POST", Path: "/api/saleorder/save"}
	ex := NewExecutorWithValidator(func(string) error { return nil })
	out, err := ex.Call(context.Background(), cfg, ep, map[string]any{"custCode": "C1"})
	if err != nil {
		t.Fatalf("grasp 调用失败: %v", err)
	}
	if gotServerNo != "SERVERNO" || gotMethod != "MD5" {
		t.Fatalf("server_no/sign_method 形态不符: %s/%s", gotServerNo, gotMethod)
	}
	// 签名拼接口径（V 级证据 preset 形态）：MD5(server_no+server_key+path) 大写
	expect := strings.ToUpper(md5Sum("SERVERNO" + "SERVERKEY" + "/api/saleorder/save"))
	if gotSign != expect {
		t.Fatalf("签名不符: got %s want %s", gotSign, expect)
	}
	if out["ok"] != true {
		t.Fatalf("归一化信封 ok 应 true, got %v", out)
	}
}

func TestConnector_Ping(t *testing.T) {
	srv, hits := tb06bServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // 401=连通（鉴权是另一面）
	})
	ex := NewExecutorWithValidator(func(string) error { return nil })
	reachable, code, _, err := ex.Ping(context.Background(), tb06bCfg(srv.URL, "generic"))
	if err != nil || !reachable || code != http.StatusUnauthorized || hits.Load() != 1 {
		t.Fatalf("401 应判可达（连通性与鉴权分离）, got reachable=%v code=%d hits=%d err=%v", reachable, code, hits.Load(), err)
	}
	// 不可达（坏 host）
	reachable2, _, _, err2 := ex.Ping(context.Background(), tb06bCfg("http://127.0.0.1:1/", "generic"))
	if err2 != nil || reachable2 {
		t.Fatalf("不可达应 false, got %v %v", reachable2, err2)
	}
	// 生产口径：环回基址被 SSRF 拒绝
	exProd := NewExecutor()
	reachable3, _, _, _ := exProd.Ping(context.Background(), tb06bCfg("http://127.0.0.1:8217/", "generic"))
	if reachable3 {
		t.Fatal("生产口径环回应不可达")
	}
}
