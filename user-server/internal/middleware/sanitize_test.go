package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// sanitizeEchoEngine 只挂一道脱敏中间件，末段 handler 把整段 body 读完后回报原始字节，
// 用于把「脱敏改写了什么」与「脱敏在读到哪一刻停手」两件事分开断言。
func sanitizeEchoEngine(t *testing.T) (r *gin.Engine, contentLen *int, raw *[]byte, readErr *string) {
	t.Helper()
	r = gin.New()
	contentLen = new(int)
	raw = new([]byte)
	readErr = new(string)
	r.Use(SanitizeMiddleware())
	r.POST("/echo", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			*readErr = err.Error()
		}
		*raw = body
		var payload struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(body, &payload) == nil {
			*contentLen = len(payload.Content)
		}
		c.JSON(http.StatusOK, payload.Content)
	})
	return r, contentLen, raw, readErr
}

// filler 铺一段指定量级的正文：每 3 字节一组 "ab "，任意连续字母数字段都只有 2 字符，
// 不会被 tokenRe（30 字符以上）命中 ⇒ 脱敏前后该值字节数一致，才能拿长度做判据。
func filler(repeat int, tail string) string {
	return strings.Repeat("ab ", repeat) + tail
}

const tailMarker = "TAIL-MARKER-KEEP-ME"

func postJSON(t *testing.T, r *gin.Engine, content string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"content":"`+content+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// 脱敏自己的读体上限必须与全局封顶同源，不能各写一份。
//
// 背景：本中间件曾在 io.LimitReader 上写死 1MB，而全局 BodyLimit 默认 8MB
// （MAX_JSON_BODY_MB）。两档并存时的表现不是报错，是 1~8MB 这一段的 JSON 请求被静默
// 截断成半份再交给处理器 —— 唯一的活装配点 internal/router/chat_routes.go:27 挂在
// 匿名公聊口 /chat/public/* 上，于是"访客发一条超长消息"是一个看不出原因的 400。
func TestSanitizeMiddlewareDoesNotTruncateBelowGlobalCap(t *testing.T) {
	r, contentLen, raw, readErr := sanitizeEchoEngine(t)

	content := filler(600_000, tailMarker) // ≈1.8MB，> 旧的写死 1MB、< 全局默认 8MB
	if len(content) < 1<<20 || len(content) > 8<<20 {
		t.Fatalf("夹具没落进 1~8MB 窗口: len=%d", len(content))
	}
	postJSON(t, r, content)

	if *readErr != "" {
		t.Fatalf("handler 读体报错: %v", *readErr)
	}
	if *contentLen != len(content) {
		t.Errorf("处理器看到的内容长度=%d，期望 %d ⇒ 请求体在中间件里被截断（原始 body %d 字节，末尾 %q）",
			*contentLen, len(content), len(*raw), tailOf(*raw))
	}
}

// 上一格只证明"默认档够用"；有人把上限改成写死的 8MB 时它照样绿。
// 这一格把全局值抬到 8MB 以上，钉的是"跟着 MAX_JSON_BODY_MB 走"而不是"跟着某个常量走"。
func TestSanitizeCapFollowsGlobalCapEnv(t *testing.T) {
	t.Setenv("MAX_JSON_BODY_MB", "12")

	r, contentLen, raw, readErr := sanitizeEchoEngine(t)
	content := filler(3_200_000, tailMarker) // ≈9.6MB，> 全局默认 8MB、< 本格的 12MB
	if len(content) < 8<<20 || len(content) > 12<<20 {
		t.Fatalf("夹具没落进 8~12MB 窗口: len=%d", len(content))
	}
	postJSON(t, r, content)

	if *readErr != "" {
		t.Fatalf("handler 读体报错: %v", *readErr)
	}
	if *contentLen != len(content) {
		t.Errorf("MAX_JSON_BODY_MB=12 时处理器看到的内容长度=%d，期望 %d ⇒ 脱敏上限没跟着全局值走（body %d 字节）",
			*contentLen, len(content), len(*raw))
	}
}

func tailOf(b []byte) string {
	if len(b) < 12 {
		return string(b)
	}
	return string(b[len(b)-12:])
}

// 上限跟着全局值**向下**走才算同源，只测向上会把"压根不设上限"也判成合格。
// 注意这一格锁的是事实源，不是用户可见行为：真实链路上 BodyLimit 排在脱敏之前，
// 超限请求在那里就 413 了（见 TestOversizedJSONIsRejectedBeforeSanitization）。
func TestSanitizeCapFollowsGlobalCapEnvDownward(t *testing.T) {
	t.Setenv("MAX_JSON_BODY_MB", "1")

	r, _, raw, readErr := sanitizeEchoEngine(t)
	content := filler(600_000, tailMarker) // ≈1.8MB > 本格的 1MB
	postJSON(t, r, content)

	if *readErr != "" {
		t.Fatalf("handler 读体报错: %v", *readErr)
	}
	if len(*raw) >= 1<<20+len(tailMarker) {
		t.Errorf("MAX_JSON_BODY_MB=1 时中间件仍读完了 %d 字节 ⇒ 读体上限没跟随全局值收窄（变成了不设限）", len(*raw))
	}
}

// 用户可见的那一格：超全局上限的 JSON 请求在链上就该被明确拒掉，
// 而不是带着半份 body 进到处理器（那是一个看不出原因的 400）。
func TestOversizedJSONIsRejectedBeforeSanitization(t *testing.T) {
	t.Setenv("MAX_JSON_BODY_MB", "1")

	engine := gin.New()
	ran := new(int)
	engine.Use(BodyLimit(BodyLimitFromEnv()))
	engine.Use(SanitizeMiddleware())
	engine.POST("/echo", func(c *gin.Context) {
		*ran++
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"read": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/echo",
		strings.NewReader(`{"content":"`+filler(600_000, tailMarker)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("2MB 请求在 1MB 全局上限下的状态码=%d，期望 413", rec.Code)
	}
	if *ran != 0 {
		t.Errorf("超限请求仍进了 handler（跑了 %d 次）⇒ 静默截断的老路还开着", *ran)
	}
}

func TestSanitizeMiddlewareMasksPIIInJSONBody(t *testing.T) {
	r, _, raw, readErr := sanitizeEchoEngine(t)

	postJSON(t, r, `我的手机是 13800138000","password":"hunter2`)
	if *readErr != "" {
		t.Fatalf("handler 读体报错: %v", *readErr)
	}
	body := string(*raw)
	if strings.Contains(body, "13800138000") {
		t.Errorf("手机号没被脱敏: %s", body)
	}
	if !strings.Contains(body, "138****8000") {
		t.Errorf("手机号脱敏形状不符（期望保留首 3 末 4）: %s", body)
	}
	if strings.Contains(body, "hunter2") {
		t.Errorf("password 字段没被掩掉: %s", body)
	}
}

// 中间件只声明对 application/json 生效；multipart 的内存占用由 gin 的 MaxMultipartMemory
// 收口，这里若把它当 JSON 读会把整份上传流吃进内存并改写字节流。
func TestSanitizeMiddlewareSkipsNonJSONBody(t *testing.T) {
	r, _, raw, readErr := sanitizeEchoEngine(t)

	const rawBody = `{"content":"手机号 13800138000"}`
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if *readErr != "" {
		t.Fatalf("handler 读体报错: %v", *readErr)
	}
	if string(*raw) != rawBody {
		t.Errorf("非 JSON 请求体被改写了：%q，期望原样 %q", *raw, rawBody)
	}
}

func TestSanitizeStringMasksPhoneEmailBankcard(t *testing.T) {
	cases := []struct{ in, want string }{
		{"联系 13800138000 谢谢", "联系 138****8000 谢谢"},
		{"邮箱 zhangsan@example.com", "邮箱 z***@example.com"},
		{"卡号 6222021234567890128", "卡号 6222 **** **** 0128"},
		{"", ""},
		{"没有敏感信息", "没有敏感信息"},
	}
	for _, tc := range cases {
		if got := SanitizeString(tc.in); got != tc.want {
			t.Errorf("SanitizeString(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// 与 audit/jwt 两个各自的私有 sanitizeMap 无关：这里锁的是导出的那份 PII 规则表。
func TestSanitizeMapPIIRulesOnNestedValues(t *testing.T) {
	out := SanitizeMap(map[string]any{
		"api_key": "plaintext-secret",
		"note":    "身份证 11010519491231002X",
		"nested":  map[string]any{"owner_phone": "13800138000"},
		"list":    []any{"手机 13800138000"},
	})
	if out["api_key"] != "********" {
		t.Errorf("字段级规则没命中 api_key: %#v", out["api_key"])
	}
	if strings.Contains(out["note"].(string), "11010519491231002X") {
		t.Errorf("身份证没被脱敏: %v", out["note"])
	}
	nested := out["nested"].(map[string]any)
	if nested["owner_phone"] != "138****8000" {
		t.Errorf("嵌套 map 没递归脱敏: %#v", nested["owner_phone"])
	}
	list := out["list"].([]any)
	if list[0] != "手机 138****8000" {
		t.Errorf("数组元素没递归脱敏: %#v", list[0])
	}
	if SanitizeMap(nil) != nil {
		t.Errorf("SanitizeMap(nil) 应返回 nil")
	}
}
