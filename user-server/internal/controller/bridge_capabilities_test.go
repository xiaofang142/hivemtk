package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// M-5（2026-11-11）：capabilities 必须把「巡检未读优先渠道清单」下发给扩展。
// 全局参数未装配时 GlobalConfigParam 为 nil-safe stub → 字段回落默认清单；
// 既有三字段（sse_enabled/poll_interval_ms/sse_heartbeat_ms）形状不变（向后兼容只增不改）。
func TestBridgeCapabilitiesController_IncludesPatrolUnreadFirstChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := NewBridgeCapabilitiesController()
	router := gin.New()
	router.GET("/api/bridge/capabilities", ctrl.GetCapabilities)

	req, _ := http.NewRequest("GET", "/api/bridge/capabilities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
	var body struct {
		SseEnabled      bool     `json:"sse_enabled"`
		PollIntervalMs  int      `json:"poll_interval_ms"`
		SseHeartbeatMs  int      `json:"sse_heartbeat_ms"`
		UnreadFirstList []string `json:"patrol_unread_first_channels"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体不是合法 JSON：%v body=%s", err, w.Body.String())
	}
	// 既有字段形状不回归（Envelope 直写顶层 payload，无 code/data 包装）
	if body.PollIntervalMs != 1500 || body.SseHeartbeatMs != 15000 {
		t.Errorf("既有字段形状漂移：poll_interval_ms=%d sse_heartbeat_ms=%d", body.PollIntervalMs, body.SseHeartbeatMs)
	}
	// 未装配 → 默认清单（douyin/tiktok/xianyu/kuaishou）；非 nil 才说明字段真的下发了
	if body.UnreadFirstList == nil {
		t.Fatalf("patrol_unread_first_channels 字段缺失：扩展侧未读优先开关将恒为关")
	}
	want := map[string]bool{"douyin": true, "tiktok": true, "xianyu": true, "kuaishou": true}
	if len(body.UnreadFirstList) != len(want) {
		t.Errorf("默认清单 = %v，期望 4 条（douyin/tiktok/xianyu/kuaishou）", body.UnreadFirstList)
	}
	for _, ch := range body.UnreadFirstList {
		if !want[ch] {
			t.Errorf("默认清单含意外渠道 %q（xiaohongshu 徽章不稳定、jd/bilibili/weibo/pdd 未校准，均不应默认开）", ch)
		}
	}
}
