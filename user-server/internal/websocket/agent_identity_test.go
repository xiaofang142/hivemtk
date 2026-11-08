package websocket

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// A-5 复检钉子：agent_id 与令牌 user_id 不符必须 403（admin 例外）。
// 用例钉住三件事：
//   1. 坐席角色（staff）自报他人 agent_id → 403，且在 Upgrade 之前拒绝（不产生半开连接）；
//   2. admin 自报任意 agent_id → 放行（管理台代看任意坐席队列是设计行为）；
//   3. WS_AGENT_ALLOW_ALL_USERS=true 的调试后门不受本轮校验约束（保持原语义）。
func wsIdentityContext(agentIDStr string, tokenUID uint, role string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/ws?agent_id="+agentIDStr, nil)
	c.Set("user_id", tokenUID)
	c.Set("role", role)
	return c, w
}

func TestHandleWebSocket_AgentIDMismatchRejected(t *testing.T) {
	t.Setenv("WS_AGENT_ALLOW_ALL_USERS", "")
	handler := NewWSHandler()

	// staff(UID=7) 冒名订阅 agent_id=9 → 必须拒绝
	c, w := wsIdentityContext("9", 7, "staff")
	handler.HandleWebSocket(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("冒名 agent_id 应 403，实际 %d", w.Code)
	}

	// token user_id 缺失（未过 auth 中间件的裸请求）也必须拒绝
	c2, w2 := wsIdentityContext("9", 0, "staff")
	handler.HandleWebSocket(c2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("无令牌身份应 403，实际 %d", w2.Code)
	}
}

func TestHandleWebSocket_AgentIDSelfOK(t *testing.T) {
	t.Setenv("WS_AGENT_ALLOW_ALL_USERS", "")
	handler := NewWSHandler()

	// UID=7 订阅自己的 agent_id=7：校验通过（后续 Upgrade 因非 ws 握手失败，
	// 但绝不能是 403 —— 403 意味着身份门误伤了自己人）。
	c, w := wsIdentityContext("7", 7, "staff")
	handler.HandleWebSocket(c)
	if w.Code == http.StatusForbidden {
		t.Fatalf("本人订阅自己的 agent_id 不应被身份门拒绝（got 403）")
	}
}

func TestHandleWebSocket_AdminMaySubscribeOthers(t *testing.T) {
	t.Setenv("WS_AGENT_ALLOW_ALL_USERS", "")
	handler := NewWSHandler()

	c, w := wsIdentityContext("9", 1, "admin")
	handler.HandleWebSocket(c)
	if w.Code == http.StatusForbidden {
		t.Fatalf("admin 代看坐席队列不应被身份门拒绝（got 403）")
	}
}

func TestHandleWebSocket_DebugBypassUnaffected(t *testing.T) {
	t.Setenv("WS_AGENT_ALLOW_ALL_USERS", "true")
	handler := NewWSHandler()

	// 调试后门：无令牌身份也不做 agent_id 比对（原语义保持）
	c, w := wsIdentityContext("9", 0, "")
	handler.HandleWebSocket(c)
	if w.Code == http.StatusForbidden {
		t.Fatalf("WS_AGENT_ALLOW_ALL_USERS=true 时不应走身份门（got 403）")
	}
}
