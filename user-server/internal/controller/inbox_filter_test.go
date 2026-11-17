package controller

// 「待首响」是查询参数不是新接口：控制器若把 filter 丢掉，仓储层再正确也不会生效。
// 这里从 HTTP 入口到列表结果走一遍，钉住"参数确实透传"。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestInboxAPI_FilterAwaitingFirstReply(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.InboxConversation{})
	base := time.Now().Add(-time.Hour)

	seed := func(customer string, waiting, firstReply, closedAt *time.Time, status string) {
		t.Helper()
		row := &model.InboxConversation{
			TenantID: 1, Platform: "wechat", AccountID: "acc-api-filter", CustomerID: customer,
			Status: status, WaitingSince: waiting, FirstReplyAt: firstReply, ClosedAt: closedAt,
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", customer, err)
		}
	}
	t1 := base
	t2 := base.Add(30 * time.Minute)
	first := base.Add(10 * time.Minute)
	seed("waiting-old", &t1, nil, nil, model.InboxStatusOpen)
	seed("waiting-new", &t2, nil, nil, model.InboxStatusUnread)
	seed("replied", &t1, &first, nil, model.InboxStatusOpen)
	seed("closed", &t1, nil, &first, model.InboxStatusClosed)
	seed("snoozed", &t1, nil, nil, model.InboxStatusSnoozed)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	ctrl := NewInboxController(service.NewInboxService())
	engine.GET("/api/inbox/conversations", ctrl.List)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/inbox/conversations?filter=awaiting_first_reply&page_size=50", nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			List []struct {
				CustomerID string `json:"customer_id"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if resp.Data.Total != 2 || len(resp.Data.List) != 2 {
		t.Fatalf("应命中 2 条待首响，实际 total=%d len=%d：%s",
			resp.Data.Total, len(resp.Data.List), w.Body.String())
	}
	if resp.Data.List[0].CustomerID != "waiting-old" ||
		resp.Data.List[1].CustomerID != "waiting-new" {
		t.Fatalf("应按等待时长升序，实际顺序 %q → %q",
			resp.Data.List[0].CustomerID, resp.Data.List[1].CustomerID)
	}
}
