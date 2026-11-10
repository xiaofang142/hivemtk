package controller

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/model"

	"github.com/stretchr/testify/assert"
)

// TestCustomer360Controller_GetCustomerMessages_LimitApplied 钉死 limit 语义。
//
// 回归背景（2026-11-10 双模式 API 验证 #224）：前端
// views/customer360/List.vue:432 调 getCustomerMessages(row.id, { limit: 50 })，
// 而 handler 早前**完全没有解析 limit**——恒返回 DTO 全量 message_history。
// 客户消息一多就是几百条一次性回给浏览器；且同页 /events、/orders 都认 limit，
// 只有 /messages 不认，属于同一页面上三套口径。
//
// 判据：
//   - limit=2 → 只回 2 条，total 仍是全量（前端可据此显示「共 N 条」）
//   - limit 缺省 → 默认 50
//   - limit 非法/负数 → 回默认 50
//   - limit>200 → 上限 200
func TestCustomer360Controller_GetCustomerMessages_LimitApplied(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	sess := &model.CustomerSession{
		SessionID: "sess-limit-001",
		UserID:    "user-limit",
		UserName:  "Limit User",
		AccountID: "acct-limit",
		Platform:  model.PlatformDouyin,
		Status:    model.SessionStatusResolved,
	}
	if err := db.Create(sess).Error; err != nil {
		t.Fatalf("seed session 失败: %v", err)
	}

	// 造 5 条消息，确保「截断」与「全量」可区分
	for i := 0; i < 5; i++ {
		msg := &model.SessionMessage{
			SessionID:   sess.SessionID,
			Content:     "m",
			SenderType:  "user",
			ContentType: model.MessageTypeText,
		}
		if err := db.Create(msg).Error; err != nil {
			t.Fatalf("seed message 失败: %v", err)
		}
	}

	cases := []struct {
		name      string
		url       string
		wantLen   int
		wantTotal float64
	}{
		{name: "limit=2 回 2 条，total 为全量", url: "/api/customer/messages?user_id=user-limit&limit=2", wantLen: 2, wantTotal: 5},
		{name: "缺省 limit 小于全量则全量返回", url: "/api/customer/messages?user_id=user-limit", wantLen: 5, wantTotal: 5},
		{name: "非法 limit 回默认", url: "/api/customer/messages?user_id=user-limit&limit=abc", wantLen: 5, wantTotal: 5},
		{name: "负数 limit 回默认", url: "/api/customer/messages?user_id=user-limit&limit=-3", wantLen: 5, wantTotal: 5},
		{name: "limit 超过上限 200 时按 200 截断", url: "/api/customer/messages?user_id=user-limit&limit=9999", wantLen: 5, wantTotal: 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tc.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, 200, w.Code)

			var resp struct {
				Code float64 `json:"code"`
				Data struct {
					MessageHistory []map[string]any `json:"message_history"`
					Total          float64          `json:"total"`
				} `json:"data"`
			}
			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, float64(0), resp.Code)
			assert.Len(t, resp.Data.MessageHistory, tc.wantLen,
				"message_history 条数应按 limit 截断（回归：limit 曾被静默忽略）")
			assert.Equal(t, tc.wantTotal, resp.Data.Total,
				"total 应是截断前的全量条数")
		})
	}
}
