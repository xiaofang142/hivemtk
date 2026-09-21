package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newIngressRouter 构造只挂 IngressSecretAuth 的最小路由：守卫放行时打进 sentinel handler。
// 不碰 DB、不碰 config，避免把这条用例变成"依赖整棵装配树"的集成测试。
func newIngressRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/chat/ingress", IngressSecretAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": "handler"})
	})
	return r
}

func doIngress(r *gin.Engine, headerValue string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/chat/ingress", strings.NewReader(`{}`))
	if headerValue != "" {
		req.Header.Set("X-Ingress-Secret", headerValue)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func ingressMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体不是预期信封：%v / %s", err, w.Body.String())
	}
	return body.Message
}

// TestIngressSecretAuthMessageNamesEnvKeyItReads 未配置时的 503 文案必须点名代码真正读取的那个变量。
//
// 反例（本用例首跑即红）：文案印 `INGRESS_SECRET`，而 `os.Getenv` 读的是 `INGRESS_API_KEY`
// ⇒ 运维照提示配好 `INGRESS_SECRET` 后仍然 503，且全仓没有任何代码读前者，排查是死路。
// 这里断言的是"文案里的键名 == 读取用的键名"，不是某个字面量：修法把键名收敛成常量、
// 文案由同一符号生成，之后任一侧再改都会把这条打红。
func TestIngressSecretAuthMessageNamesEnvKeyItReads(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	// t.Setenv 负责用例结束后还原原值；空串与未设置在本中间件里同义（TrimSpace 后判空）
	t.Setenv("INGRESS_API_KEY", "")

	w := doIngress(newIngressRouter(), "whatever")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("密钥未配置必须 fail-closed 返回 503，实际 %d %s", w.Code, w.Body.String())
	}
	msg := ingressMessage(t, w)
	if !strings.Contains(msg, "INGRESS_API_KEY") {
		t.Fatalf("503 文案必须点名代码读取的变量名 INGRESS_API_KEY，实际：%q", msg)
	}
}

// TestIngressSecretAuthRejectsWhenKeyUnsetAndPassesOnMatch 钉住守卫本身的三条腿：
// 未配置 ⇒ 503、缺头/错值 ⇒ 401、匹配 ⇒ 放行。
func TestIngressSecretAuthRejectsWhenKeyUnsetAndPassesOnMatch(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	const configured = "ingress-key-for-tests-0123456789"
	t.Setenv("INGRESS_API_KEY", configured)

	r := newIngressRouter()
	if w := doIngress(r, configured); w.Code != http.StatusOK {
		t.Fatalf("带正确 X-Ingress-Secret 必须放行，实际 %d %s", w.Code, w.Body.String())
	}
	if w := doIngress(r, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("缺 X-Ingress-Secret 必须 401，实际 %d %s", w.Code, w.Body.String())
	}
	if w := doIngress(r, configured+"-but-wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("错值必须 401，实际 %d %s", w.Code, w.Body.String())
	}

	// 对照：换成未配置，同一棵树必须从"放行"翻成 503（证明前面那个 200 来自密钥而不是漏挂守卫）
	t.Setenv("INGRESS_API_KEY", "")
	if w := doIngress(newIngressRouter(), configured); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("密钥清空后同一请求必须 503，实际 %d %s", w.Code, w.Body.String())
	}
}
