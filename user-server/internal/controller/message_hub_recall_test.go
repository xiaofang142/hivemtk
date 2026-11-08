package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// #757 回归：POST /api/message-hub/{id}/recall 的出网失败（ErrMessageHubRecallFailed，
// 哨兵注释即写明 ⇒ 502）原样落到 controller 的 default 分支 ErrorFromDB → 500
// 「操作失败，请稍后重试」——把"上游渠道现在给不了"报成"我们服务坏了"。
// 修复后必须回 502 UPSTREAM_FAILURE_5005。
func TestMessageHubController_Recall_ChannelErrorReports502(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)

	row := &model.MessageHub{
		Platform:       "wecom",
		AccountID:      "acc1",
		MsgID:          "wecom-1",
		ConversationID: "conv-1",
		Direction:      "outbound",
		Status:         "sent",
		MsgType:        "text",
		Content:        "hello",
		SenderID:       "s1",
		ReceiverID:     "r1",
		SentAt:         time.Now(),
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 出网必失败（wecom extra 缺 msgid 的确定性业务态也走这条路）。
	// NewMessageHubService 经 repository.NewMessageHubRepository() 取全局句柄，
	// 上面的 db.SetTestDB 已把它指到测试库。
	svc := service.NewMessageHubService()
	svc = svc.WithReachRecaller(recallAlwaysFail{})

	router := gin.New()
	ctrl := NewMessageHubController(svc)
	router.POST("/api/message-hub/:id/recall", ctrl.Recall)

	req := httptest.NewRequest(http.MethodPost, "/api/message-hub/1/recall", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("出网失败应 502（修复前 default 兜成 500），实际 %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "UPSTREAM_FAILURE_5005") {
		t.Errorf("响应业务码应为 UPSTREAM_FAILURE_5005，body=%s", w.Body.String())
	}
}

type recallAlwaysFail struct{}

func (recallAlwaysFail) RecallHub(context.Context, *model.MessageHub) error {
	return errors.New("wecom hub extra missing msgid")
}
