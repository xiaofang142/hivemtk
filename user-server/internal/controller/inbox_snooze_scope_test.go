package controller

// 「稍后处理」是按行寻址的写口：非超管只能动自己名下的会话。
// 守卫名册把这一条记成 SnoozeScoped/UnsnoozeScoped，这里从 HTTP 入口证明确实拦得住。

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestInboxAPI_SnoozeRespectsScope(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.InboxConversation{})
	conv := &model.InboxConversation{
		TenantID: 1, Platform: "wechat", AccountID: "acc-scope", CustomerID: "c-scope",
		Status: model.InboxStatusOpen, AssignedTo: "alice",
	}
	if err := db.Create(conv).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	ctrl := NewInboxController(service.NewInboxService())
	engine.POST("/api/inbox/conversations/:id/snooze", func(c *gin.Context) {
		c.Set("role", "customer_service")
		c.Set("username", c.GetHeader("X-Test-User"))
		c.Next()
	}, ctrl.Snooze)
	engine.POST("/api/inbox/conversations/:id/unsnooze", func(c *gin.Context) {
		c.Set("role", "customer_service")
		c.Set("username", c.GetHeader("X-Test-User"))
		c.Next()
	}, ctrl.Unsnooze)

	call := func(method, path, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", user)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	id := strconv.FormatUint(uint64(conv.ID), 10)

	// 别人的会话：403，且库里状态不变。
	w := call(http.MethodPost, "/api/inbox/conversations/"+id+"/snooze", "bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("非归属坐席应 403，实际 %d：%s", w.Code, w.Body.String())
	}
	var after model.InboxConversation
	if err := db.First(&after, conv.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if after.Status != model.InboxStatusOpen {
		t.Fatalf("越权请求不应改状态，实际 %q", after.Status)
	}

	// 自己的会话：200 且进入 snoozed。
	w = call(http.MethodPost, "/api/inbox/conversations/"+id+"/snooze", "alice")
	if w.Code != http.StatusOK {
		t.Fatalf("归属坐席应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if err := db.First(&after, conv.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if after.Status != model.InboxStatusSnoozed || after.SnoozedUntil == nil {
		t.Fatalf("应为 snoozed 且有到期时间，实际 status=%q until=%v", after.Status, after.SnoozedUntil)
	}

	// 取消稍后处理同样受归属约束。
	w = call(http.MethodPost, "/api/inbox/conversations/"+id+"/unsnooze", "bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("非归属坐席取消应 403，实际 %d：%s", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, "/api/inbox/conversations/"+id+"/unsnooze", "alice")
	if w.Code != http.StatusOK {
		t.Fatalf("归属坐席取消应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var reloaded model.InboxConversation
	if err := db.First(&reloaded, conv.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if reloaded.Status != model.InboxStatusOpen || reloaded.SnoozedUntil != nil {
		t.Fatalf("取消后应为 open 且清空到期时间，实际 status=%q until=%v", reloaded.Status, reloaded.SnoozedUntil)
	}
}
