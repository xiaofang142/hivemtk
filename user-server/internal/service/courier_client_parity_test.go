package service

// courier_client_parity_test.go §3.12 刀2：CourierClient 收编 generic 方言——新旧双跑对拍。
//
// 判据（对拍一致才切换；对拍不等即回退不硬切）：同输入下双实现各跑一遍比对——
//   (a) 请求形态（method/path/query/鉴权头/静态头）一致；
//   (b) 成功路径输出（[]*LogisticsTrackView）逐节点逐字段一致；
//   (c) 失败面双方 err!=nil 且 tracks=nil 一致（错误文本允许信息增量，见
//       courier_client.go 头注 C2-3——调用方只判 err==nil）。
//
// legacyCourierClient 是切换前旧实现的**逐字拷贝**（基准），测试期间禁止改写；
// 新实现经 newCourierClientWithExecutor 注入放行环回的 Executor（测试接缝），
// 生产构造 NewCourierClientFromConfig 恒为严格 SSRF 口径。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/connector"
)

// ---------- 旧实现基准（逐字拷贝自切换前的 CourierClient.Query，勿改） ----------

type legacyCourierClient struct {
	baseURL string
	key     string
	secret  string
	http    *http.Client
}

func (c *legacyCourierClient) Configured() bool { return c.baseURL != "" }

func (c *legacyCourierClient) Query(ctx context.Context, carrier, trackingNo string) ([]*portcontract.LogisticsTrackView, error) {
	if !c.Configured() || trackingNo == "" {
		return nil, nil
	}
	reqURL := fmt.Sprintf("%s/api/track?carrier=%s&no=%s", c.baseURL, url.QueryEscape(carrier), url.QueryEscape(trackingNo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.secret != "" {
		req.Header.Set("X-Api-Secret", c.secret)
	}
	resp, err := c.http.Do(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("courier api status %d", resp.StatusCode)
	}
	var out struct {
		Tracks []struct {
			Time        string `json:"time"`
			Status      string `json:"status"`
			Location    string `json:"location"`
			Description string `json:"description"`
		} `json:"tracks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	tracks := make([]*portcontract.LogisticsTrackView, 0, len(out.Tracks))
	for _, t := range out.Tracks {
		tracks = append(tracks, &portcontract.LogisticsTrackView{
			Time:        t.Time,
			Status:      t.Status,
			Location:    t.Location,
			Description: t.Description,
		})
	}
	return tracks, nil
}

// ---------- 对拍夹具 ----------

type capturedRequest struct {
	method      string
	path        string
	rawQuery    string
	authHeader  string
	secretFound bool
}

// parityUpstream 同一响应行为 + 请求录制（新旧各起一台同行为实例，录到的请求形态互比）。
type parityUpstream struct {
	srv  *httptest.Server
	hits *atomic.Int32
	req  capturedRequest
}

func newParityUpstream(t *testing.T, body string, status int) *parityUpstream {
	t.Helper()
	up := &parityUpstream{hits: &atomic.Int32{}}
	up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		_ = r.ParseForm()
		up.req = capturedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			rawQuery:    r.URL.RawQuery,
			authHeader:  r.Header.Get("Authorization"),
			secretFound: r.Header.Get("X-Api-Secret") != "",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(up.srv.Close)
	return up
}

type parityPair struct {
	legacy *legacyCourierClient
	modern *CourierClient
	up     *parityUpstream
}

func newParityPair(t *testing.T, body string, status int, key, secret string) *parityPair {
	t.Helper()
	up := newParityUpstream(t, body, status)
	baseURL := up.srv.URL
	legacy := &legacyCourierClient{baseURL: baseURL, key: key, secret: secret, http: &http.Client{}}
	modern := newCourierClientWithExecutor(
		connector.NewExecutorWithValidator(func(string) error { return nil }), // 测试 seam：放行环回
		LogisticsIntegration{Enabled: true, BaseURL: baseURL, Key: key, Secret: secret},
	)
	return &parityPair{legacy: legacy, modern: modern, up: up}
}

func assertTracksEqual(t *testing.T, tag string, a, b []*portcontract.LogisticsTrackView) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("[%s] 轨迹条数不等: legacy=%d modern=%d", tag, len(a), len(b))
	}
	for i := range a {
		if *a[i] != *b[i] {
			t.Fatalf("[%s] 第 %d 节点不等: legacy=%+v modern=%+v", tag, i, *a[i], *b[i])
		}
	}
}

// ---------- 对拍主样本 ----------

func TestCourierParity_SuccessMultiTracks(t *testing.T) {
	body := `{"tracks":[{"time":"09:00","status":"运输中","location":"上海转运中心","description":"包裹已到达上海转运中心"},{"time":"14:30","status":"派送中","location":"浦东新区","description":"快递员正在派送"}]}`
	p := newParityPair(t, body, http.StatusOK, "K1", "S1")

	lt, lerr := p.legacy.Query(context.Background(), "SF", "SF123")
	mt, merr := p.modern.Query(context.Background(), "SF", "SF123")
	if lerr != nil || merr != nil {
		t.Fatalf("成功样本不应报错: legacy=%v modern=%v", lerr, merr)
	}
	assertTracksEqual(t, "multi", lt, mt)
	if len(lt) != 2 {
		t.Fatalf("应解析出 2 个节点, got %d", len(lt))
	}
	// (a) 请求形态一致
	if p.up.req.method != http.MethodGet || p.up.req.path != "/api/track" {
		t.Fatalf("请求形态不符: %+v", p.up.req)
	}
	if p.up.req.authHeader != "Bearer K1" || !p.up.req.secretFound {
		t.Fatalf("鉴权/静态头不符: %+v", p.up.req)
	}
	q := p.up.req.rawQuery
	if !courierParityContains(q, "carrier=SF") || !courierParityContains(q, "no=SF123") {
		t.Fatalf("query 参数映射不符: %s", q)
	}
	if p.up.hits.Load() != 2 {
		t.Fatalf("上游应被双实现各调 1 次, got %d", p.up.hits.Load())
	}
}

func TestCourierParity_SecretOmittedWhenEmpty(t *testing.T) {
	p := newParityPair(t, `{"tracks":[]}`, http.StatusOK, "K1", "")
	_, lerr := p.legacy.Query(context.Background(), "YT", "YT9")
	_, merr := p.modern.Query(context.Background(), "YT", "YT9")
	if lerr != nil || merr != nil {
		t.Fatalf("不应报错: %v %v", lerr, merr)
	}
	if p.up.req.secretFound {
		t.Fatal("secret 为空时双方都不得带 X-Api-Secret")
	}
	// key 为空：双方都不带 Authorization
	p2 := newParityPair(t, `{"tracks":[]}`, http.StatusOK, "", "")
	_, lerr2 := p2.legacy.Query(context.Background(), "YT", "YT9")
	_, merr2 := p2.modern.Query(context.Background(), "YT", "YT9")
	if lerr2 != nil || merr2 != nil {
		t.Fatalf("不应报错: %v %v", lerr2, merr2)
	}
	if p2.up.req.authHeader != "" {
		t.Fatalf("key 为空时双方都不得带 Authorization, got %q", p2.up.req.authHeader)
	}
}

func TestCourierParity_ShortCircuitNilNil(t *testing.T) {
	p := newParityPair(t, `{"tracks":[]}`, http.StatusOK, "K", "S")
	// 未配置 base_url
	uncfgLegacy := &legacyCourierClient{http: p.legacy.http}
	uncfgModern := newCourierClientWithExecutor(nil, LogisticsIntegration{})
	lt, lerr := uncfgLegacy.Query(context.Background(), "SF", "SF123")
	mt, merr := uncfgModern.Query(context.Background(), "SF", "SF123")
	if lerr != nil || merr != nil || lt != nil || mt != nil {
		t.Fatalf("未配置应双 (nil,nil): legacy=%v,%v modern=%v,%v", lt, lerr, mt, merr)
	}
	// 空运单号
	lt2, lerr2 := p.legacy.Query(context.Background(), "SF", "")
	mt2, merr2 := p.modern.Query(context.Background(), "SF", "")
	if lerr2 != nil || merr2 != nil || lt2 != nil || mt2 != nil {
		t.Fatalf("空运单号应双 (nil,nil): legacy=%v,%v modern=%v,%v", lt2, lerr2, mt2, merr2)
	}
	if p.up.hits.Load() != 0 {
		t.Fatalf("短路路径不得外发: hits=%d", p.up.hits.Load())
	}
}

// ---------- 对拍失败面 ----------

func TestCourierParity_FailureSurfaces(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"非200", `{"error":"upstream down"}`, http.StatusBadGateway},
		{"非JSON", `plain text`, http.StatusOK},
		{"JSON数组", `[{"tracks":[]}]`, http.StatusOK},
		{"tracks字段类型不符", `{"tracks":"not-an-array"}`, http.StatusOK},
		{"空body", ``, http.StatusOK},
	}
	for _, tc := range cases {
		p := newParityPair(t, tc.body, tc.status, "K", "S")
		lt, lerr := p.legacy.Query(context.Background(), "SF", "SF123")
		mt, merr := p.modern.Query(context.Background(), "SF", "SF123")
		if lerr == nil || merr == nil {
			t.Fatalf("[%s] 失败面应对拍一致（双方都报错）: legacyErr=%v modernErr=%v", tc.name, lerr, merr)
		}
		if lt != nil || mt != nil {
			t.Fatalf("[%s] 失败面 tracks 应双 nil: legacy=%v modern=%v", tc.name, lt, mt)
		}
		if p.up.hits.Load() != 2 {
			t.Fatalf("[%s] 双实现各应打到上游 1 次, got %d", tc.name, p.up.hits.Load())
		}
	}
}

func TestCourierParity_ObjectWithoutTracksIsEmptySuccess(t *testing.T) {
	// 对象无 tracks 字段：旧实现 Decode 成功 → 空轨迹 nil err；新实现同语义。
	p := newParityPair(t, `{"other":"field"}`, http.StatusOK, "K", "S")
	lt, lerr := p.legacy.Query(context.Background(), "SF", "SF123")
	mt, merr := p.modern.Query(context.Background(), "SF", "SF123")
	if lerr != nil || merr != nil {
		t.Fatalf("缺 tracks 字段应对拍为成功: legacy=%v modern=%v", lerr, merr)
	}
	assertTracksEqual(t, "no-tracks-field", lt, mt)
	if len(lt) != 0 {
		t.Fatalf("应为空轨迹, got %d", len(lt))
	}
}

func TestCourierParity_NetworkUnreachableBothFail(t *testing.T) {
	legacy := &legacyCourierClient{baseURL: "http://127.0.0.1:1", key: "K", http: &http.Client{}}
	modern := newCourierClientWithExecutor(
		connector.NewExecutorWithValidator(func(string) error { return nil }),
		LogisticsIntegration{Enabled: true, BaseURL: "http://127.0.0.1:1", Key: "K"},
	)
	_, lerr := legacy.Query(context.Background(), "SF", "SF1")
	_, merr := modern.Query(context.Background(), "SF", "SF1")
	if lerr == nil || merr == nil {
		t.Fatalf("网络不可达应双报错: legacy=%v modern=%v", lerr, merr)
	}
}

func courierParityContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
