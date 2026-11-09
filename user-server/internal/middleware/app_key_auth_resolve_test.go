package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
)

// fakeChannelResolver 记录每次解析调用并按两名列分别应答。
// 语义对齐 service.ChatChannelService：查不到返回 error（不是 nil, nil）。
type fakeChannelResolver struct {
	byChannelID map[string]*ChatChannelView
	byAppKey    map[string]*ChatChannelView
	channelHits []string
	appKeyHits  []string
}

func (f *fakeChannelResolver) ResolveByChannelID(_ context.Context, channelID string) (*ChatChannelView, error) {
	f.channelHits = append(f.channelHits, channelID)
	if ch, ok := f.byChannelID[channelID]; ok {
		return ch, nil
	}
	return nil, fmt.Errorf("渠道不存在: %s", channelID)
}

func (f *fakeChannelResolver) ResolveByAppKey(_ context.Context, appKey string) (*ChatChannelView, error) {
	f.appKeyHits = append(f.appKeyHits, appKey)
	if ch, ok := f.byAppKey[appKey]; ok {
		return ch, nil
	}
	return nil, fmt.Errorf("AppKey 无效: %s", appKey)
}

// resolveEchoRouter 挂 AppKeyResolve 的最小路由：把中间件注入的上下文原样回读，
// 供断言 resolved channel / tenant 绑定（不碰 DB、不碰装配树）。
func resolveEchoRouter(res ChatChannelResolver) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/chat/public/open", AppKeyResolve(res), func(c *gin.Context) {
		chID, _ := c.Get("chat_channel_id")
		appKey, hasAppKey := c.Get("chat_app_key")
		tid, bound := tenant.FromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{
			"channel_id":   chID,
			"app_key":      appKey,
			"has_app_key":  hasAppKey,
			"tenant_id":    tid,
			"tenant_bound": bound,
		})
	})
	return r
}

type resolveEcho struct {
	Code        int    `json:"code"`
	ChannelID   string `json:"channel_id"`
	AppKey      string `json:"app_key"`
	HasAppKey   bool   `json:"has_app_key"`
	TenantID    uint   `json:"tenant_id"`
	TenantBound bool   `json:"tenant_bound"`
}

func doResolve(r *gin.Engine, headerChannelID, queryChannelID string) (*httptest.ResponseRecorder, resolveEcho) {
	path := "/api/chat/public/open"
	if queryChannelID != "" {
		path += "?channel_id=" + queryChannelID
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	if headerChannelID != "" {
		req.Header.Set("X-Chat-Channel-Id", headerChannelID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var echo resolveEcho
	echo.Code = w.Code
	_ = json.Unmarshal(w.Body.Bytes(), &echo)
	return w, echo
}

// TestAppKeyResolveRefSlotFallsBackToAppKey 钉住 ref 槽位（X-Chat-Channel-Id）
// 里放 app_key 时的回退解析：
//
//   - 嵌入代码与 embed-sdk 都以 appKey 作 channel_ref 传该槽位（bug 现场：
//     修复前该槽位只按 channel_id 列解析 ⇒ appKey 形 ref 直接 401
//     "渠道未配置或不可用: ak_..."，客服 widget 打不开）；
//   - 回退命中后上下文注入的必须是渠道行自身的 channel_id（canonical）与
//     chat_app_key（保留溯源），且绑定渠道行租户（on 模式下游 GORM fail-closed 的修复面）。
func TestAppKeyResolveRefSlotFallsBackToAppKey(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	const (
		canonical = "9885e22d-e5a1-4291-9a33-86709616d61a"
		appKeyRef = "ak_TestRefSlotFallback0000000000"
	)
	res := &fakeChannelResolver{
		byChannelID: map[string]*ChatChannelView{canonical: {ChannelID: canonical, Active: true, TenantID: 7}},
		byAppKey:    map[string]*ChatChannelView{appKeyRef: {ChannelID: canonical, Active: true, TenantID: 7}},
	}
	r := resolveEchoRouter(res)

	// header 槽位
	w, echo := doResolve(r, appKeyRef, "")
	if w.Code != http.StatusOK {
		t.Fatalf("app_key 形 ref 走 header 槽位必须回退命中放行，实际 %d %s", w.Code, w.Body.String())
	}
	if echo.ChannelID != canonical {
		t.Errorf("上下文的 channel_id 必须是渠道行 canonical 值，实际 %q", echo.ChannelID)
	}
	if !echo.HasAppKey || echo.AppKey != appKeyRef {
		t.Errorf("回退命中时应注入 chat_app_key=%q，实际 has=%v val=%q", appKeyRef, echo.HasAppKey, echo.AppKey)
	}
	if !echo.TenantBound || echo.TenantID != 7 {
		t.Errorf("必须绑定渠道行租户 7，实际 bound=%v tenant=%d", echo.TenantBound, echo.TenantID)
	}
	if len(res.channelHits) != 1 || res.channelHits[0] != appKeyRef {
		t.Errorf("必须先按 channel_id 列尝试一次（命中前不应多次查询），实际 %v", res.channelHits)
	}
	if len(res.appKeyHits) != 1 || res.appKeyHits[0] != appKeyRef {
		t.Errorf("channel_id 列未命中后应恰按 app_key 列回退一次，实际 %v", res.appKeyHits)
	}

	// query 槽位同语义（两条槽位共用同一解析分支，各自过一遍防回归）
	res2 := &fakeChannelResolver{
		byChannelID: map[string]*ChatChannelView{canonical: {ChannelID: canonical, Active: true, TenantID: 7}},
		byAppKey:    map[string]*ChatChannelView{appKeyRef: {ChannelID: canonical, Active: true, TenantID: 7}},
	}
	w2, echo2 := doResolve(resolveEchoRouter(res2), "", appKeyRef)
	if w2.Code != http.StatusOK || echo2.ChannelID != canonical || !echo2.TenantBound || echo2.TenantID != 7 {
		t.Fatalf("query 槽位回退必须同语义：%d %s", w2.Code, w2.Body.String())
	}
}

// TestAppKeyResolveCanonicalChannelIDBindsTenant 钉住 canonical channel_id 命中
// 路径的租户绑定（bug 现场：修复前只有 app_key 分支绑租户，channel_id 槽位解析
// 成功的请求在 on 模式于下游 GORM 隔离 callback 处 500 fail-closed）。
// 同时钉住"首查命中即短路"：不得再做 app_key 回退查询。
func TestAppKeyResolveCanonicalChannelIDBindsTenant(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	const canonical = "ak_ChannelIDColumnDirectHit0000"
	res := &fakeChannelResolver{
		byChannelID: map[string]*ChatChannelView{canonical: {ChannelID: canonical, Active: true, TenantID: 7}},
	}
	w, echo := doResolve(resolveEchoRouter(res), canonical, "")
	if w.Code != http.StatusOK {
		t.Fatalf("channel_id 列直接命中必须放行，实际 %d %s", w.Code, w.Body.String())
	}
	if echo.ChannelID != canonical {
		t.Errorf("channel_id 应为命中值 %q，实际 %q", canonical, echo.ChannelID)
	}
	if !echo.TenantBound || echo.TenantID != 7 {
		t.Errorf("channel_id 命中路径必须绑定租户 7，实际 bound=%v tenant=%d", echo.TenantBound, echo.TenantID)
	}
	if echo.HasAppKey {
		t.Errorf("非回退命中不应注入 chat_app_key，实际 %q", echo.AppKey)
	}
	if len(res.appKeyHits) != 0 {
		t.Errorf("首查命中即短路，不应发生 app_key 回退查询，实际 %v", res.appKeyHits)
	}
}

// TestAppKeyResolveUnknownRefStill401 两列都查不到时必须维持 401 fail-closed
// （回退是解析语义扩展，不是放行面扩大）。
func TestAppKeyResolveUnknownRefStill401(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	res := &fakeChannelResolver{}
	w, echo := doResolve(resolveEchoRouter(res), "ak_NotExistAnywhere", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未知 ref 必须 401，实际 %d %s", w.Code, w.Body.String())
	}
	if echo.Code != 401 {
		t.Errorf("信封 code 应为 401，实际 %d", echo.Code)
	}
	if len(res.channelHits) != 1 || len(res.appKeyHits) != 1 {
		t.Errorf("两列各应恰查一次，实际 channel=%v appKey=%v", res.channelHits, res.appKeyHits)
	}
}

// TestAppKeyResolveInactiveAppKeyRefStill403 经 app_key 回退命中的渠道被禁用时
// 必须 403（与 app_key 分支同语义）。
func TestAppKeyResolveInactiveAppKeyRefStill403(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	const appKeyRef = "ak_InactiveChannel000000000000"
	res := &fakeChannelResolver{
		byAppKey: map[string]*ChatChannelView{appKeyRef: {ChannelID: "uuid-inactive", Active: false, TenantID: 7}},
	}
	w, _ := doResolve(resolveEchoRouter(res), appKeyRef, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("回退命中但渠道禁用必须 403，实际 %d %s", w.Code, w.Body.String())
	}
}

// pathParamEchoRouter 挂 :app_key 路径参数形态的公开口（对齐
// chat_routes.go 的 GET /api/chat/public/channel/:app_key/info——安装引导
// 「测试 API 连通性」的真实路由形状），把中间件注入的上下文原样回读。
func pathParamEchoRouter(res ChatChannelResolver) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/chat/public/channel/:app_key/info", AppKeyResolve(res), func(c *gin.Context) {
		chID, _ := c.Get("chat_channel_id")
		appKey, hasAppKey := c.Get("chat_app_key")
		tid, bound := tenant.FromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{
			"channel_id":   chID,
			"app_key":      appKey,
			"has_app_key":  hasAppKey,
			"tenant_id":    tid,
			"tenant_bound": bound,
		})
	})
	return r
}

// TestAppKeyResolvePathParamSlot 钉住路径参数槽位（bug 现场：AppKeyResolve 此前
// 只认 header/query/body 三槽位，GET /channel/:app_key/info 在库里没有名为
// default 的渠道时永远 401 "渠道未配置或不可用: default"，安装引导连通性测试
// 对任何合法渠道都报「连接失败」——2026-11-10 全量回归实测）。修复后路径参数
// 与 ref 槽位同语义：channel_id 列先试、app_key 列回退、命中注入 canonical
// channel_id + chat_app_key + 渠道行租户。
func TestAppKeyResolvePathParamSlot(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	const (
		canonical = "2c781d29-5457-4169-b967-b0040f2b2c5c"
		appKey    = "ak_YvcfQRsqdz9eF9YwnE5PjmT2"
	)
	res := &fakeChannelResolver{
		byAppKey: map[string]*ChatChannelView{appKey: {ChannelID: canonical, Active: true, TenantID: 9}},
	}
	r := pathParamEchoRouter(res)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/public/channel/"+appKey+"/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("路径参数槽位必须回退命中放行，实际 %d %s", w.Code, w.Body.String())
	}
	var echo resolveEcho
	_ = json.Unmarshal(w.Body.Bytes(), &echo)
	if echo.ChannelID != canonical {
		t.Errorf("上下文 channel_id 必须是渠道行 canonical 值 %q，实际 %q", canonical, echo.ChannelID)
	}
	if !echo.HasAppKey || echo.AppKey != appKey {
		t.Errorf("路径 app_key 命中应注入 chat_app_key=%q，实际 has=%v val=%q", appKey, echo.HasAppKey, echo.AppKey)
	}
	if !echo.TenantBound || echo.TenantID != 9 {
		t.Errorf("必须绑定渠道行租户 9，实际 bound=%v tenant=%d", echo.TenantBound, echo.TenantID)
	}
	if len(res.appKeyHits) != 1 || res.appKeyHits[0] != appKey {
		t.Errorf("应恰按 app_key 列回退一次，实际 %v", res.appKeyHits)
	}

	// 无该参数的路由不受影响：默认槽位语义保持（不产生 app_key 查询）
	res2 := &fakeChannelResolver{}
	r2 := resolveEchoRouter(res2)
	req2 := httptest.NewRequest(http.MethodPost, "/api/chat/public/open", strings.NewReader(`{}`))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("无槽位请求仍应走 default 兜底 401，实际 %d", w2.Code)
	}
	if len(res2.appKeyHits) != 1 || res2.appKeyHits[0] != "default" {
		t.Errorf("default 兜底语义保持：channel_id 列失败后按 app_key 列回退恰一次，实际 %v", res2.appKeyHits)
	}
}
