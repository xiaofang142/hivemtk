package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// 公开客服接口的请求体是**业务数据**，不是日志。曾经在 chatPublic 组上挂过
// SanitizeMiddleware，它把处理器实际读取的 body 重写成脱敏文本：
//   - 36 位 UUID 形式的 channel_id 命中 tokenRe，被换成 `7d35...6225`，
//     合法渠道查询直接落空（真机表现：客服挂件「渠道不存在」）；
//   - 访客消息正文里的 30+ 字符连续串（URL、单号、英文长词）被截成 `Toke...6789`
//     后入库，原始内容永久丢失。
//
// 这两格钉住的正是「请求体必须原样到达处理器/落库」这一条，防止脱敏中间件
// 被再次挂回 public 组（脱敏属于日志出口，不属于入参）。
func TestChatPublicRequestBodyKeepsUUIDChannelID(t *testing.T) {
	r, database, _ := newChatTokenTestEngine(t)

	uuidChannelID := "7d35c8ca-c3fe-4e2f-9edae5576225"
	channel := &model.ChatChannel{
		ChannelID:     uuidChannelID,
		ChannelName:   "UUID 渠道",
		AppKey:        "ak_body_integrity_uuid",
		AppSecretHash: "test-hash",
		Status:        model.ChatChannelStatusActive,
	}
	if err := database.Create(channel).Error; err != nil {
		t.Fatalf("铺 UUID 渠道失败: %v", err)
	}
	t.Cleanup(func() { database.Where("id = ?", channel.ID).Delete(&model.ChatChannel{}) })

	body := fmt.Sprintf(`{"channel_id":%q,"visitor_id":"v_body_integrity"}`, uuidChannelID)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/public/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("带 UUID channel_id 开会话回 HTTP %d，期望 200（body=%s）", w.Code, truncateBody(w.Body.String()))
	}

	var env struct {
		Data struct {
			Session struct {
				AccountID string `json:"account_id"`
			} `json:"session"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if env.Data.Session.AccountID != uuidChannelID {
		t.Errorf("会话落在渠道 %q，期望原样保存 %q（说明请求体在处理器之前被改写）",
			env.Data.Session.AccountID, uuidChannelID)
	}
}

func TestChatPublicMessageContentNotMasked(t *testing.T) {
	r, database, _ := newChatTokenTestEngine(t)

	openBody := `{"channel_id":"default","visitor_id":"v_body_integrity_msg"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/public/sessions", strings.NewReader(openBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("开会话回 HTTP %d: %s", w.Code, truncateBody(w.Body.String()))
	}
	var openEnv struct {
		Data struct {
			Session struct {
				SessionID string `json:"session_id"`
			} `json:"session"`
			VisitorToken string `json:"visitor_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &openEnv); err != nil {
		t.Fatalf("开会话响应解析失败: %v", err)
	}

	content := "TokenTest_abcdefghijklmnopqrstuvwxyz0123456789 你好"
	msgBody := fmt.Sprintf(`{"content":%q,"content_type":"text"}`, content)
	req = httptest.NewRequest(http.MethodPost,
		"/api/chat/public/sessions/"+openEnv.Data.Session.SessionID+"/messages", strings.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Chat-Visitor-Id", "v_body_integrity_msg")
	req.Header.Set("X-Chat-Visitor-Token", openEnv.Data.VisitorToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("发消息回 HTTP %d: %s", w.Code, truncateBody(w.Body.String()))
	}

	var stored string
	if err := database.Model(&model.SessionMessage{}).
		Where("session_id = ? AND sender_type = ?", openEnv.Data.Session.SessionID, "user").
		Order("id DESC").Limit(1).Pluck("content", &stored).Error; err != nil {
		t.Fatalf("查落库消息失败: %v", err)
	}
	if stored != content {
		t.Errorf("消息正文被改写：落库=%q 原文=%q", stored, content)
	}
}
