package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 访客"换回某条历史会话的 visitor_token"这一口的装配与归属，必须在**路由层**验一次。
//
// 为什么不是只在 service 层测：service 用例证明不了三件事 —— 路由有没有挂上（少写一行
// chatPublic.POST 的表现是 404，而不是报错）、public 组那几个中间件（AppKeyResolve /
// LangResolver / VisitorRateLimit / Sanitize）会不会把这一口拦在控制器之前、以及
// 访客 ID 与 token 的传递方式（X-Chat-Visitor-Id 头 vs query）在控制器里接得不接得住。
// 这一口的存在意义就是把 403 变成 200，所以还要拿它真去打一次会话级调用。
//
// 归属那一格是安全判据：这个口不能变成"知道 session_id 就能替别人领证"。

const chatPublicTokenPath = "/api/chat/public/sessions/"

func newChatTokenTestEngine(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.ChatChannel{},
		&model.QuickReply{},
		&model.SessionTag{},
		&model.UserBlacklist{},
		&model.CSATSurvey{},
	)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	channel, err := service.MustNewChatChannelService(database).GetOrCreateDefaultChannel(context.Background())
	if err != nil {
		t.Fatalf("取默认渠道失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)
	return r, database, channel.ChannelID
}

// seedVisitorSession 直接落一条属于该访客的已结束会话（不绕 OpenSession：
// 那条链路会触发黑名单/坐席计数/CSAT 回流，与本题无关且要铺更多表）。
func seedVisitorSession(t *testing.T, database *gorm.DB, channelID, visitorID string) string {
	t.Helper()
	sessionID := "sess_r67_" + time.Now().Format("150405.000") + "_" + visitorID
	row := &model.CustomerSession{
		SessionID:   sessionID,
		Platform:    model.PlatformWebEmbed,
		AccountID:   channelID,
		UserID:      visitorID,
		UserName:    "换证路由用例访客",
		Status:      model.SessionStatusClosed,
		HandlerType: model.HandlerTypeAI,
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("插入测试会话失败: %v", err)
	}
	t.Cleanup(func() {
		// 清掉本用例铺的数据，免得下一次跑（或同包的 recent-closed 用例）读到两条
		database.Where("session_id = ?", sessionID).Delete(&model.CustomerSession{})
	})
	return sessionID
}

func exchangeToken(t *testing.T, r *gin.Engine, sessionID, visitorID string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, chatPublicTokenPath+sessionID+"/token", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Chat-Visitor-Id", visitorID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Logf("换证非 200：%d body=%s", w.Code, truncateBody(body))
		return w.Code, ""
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			VisitorToken string `json:"visitor_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("换证响应不是合法 JSON（%d）: %v body=%s", w.Code, err, truncateBody(body))
	}
	return w.Code, env.Data.VisitorToken
}

func getMessages(t *testing.T, r *gin.Engine, sessionID, visitorID, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, chatPublicTokenPath+sessionID+"/messages?page=1", nil)
	req.Header.Set("X-Chat-Visitor-Id", visitorID)
	if token != "" {
		req.Header.Set("X-Chat-Visitor-Token", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestChatPublicSessionTokenRoute(t *testing.T) {
	r, database, channelID := newChatTokenTestEngine(t)

	// 前置：路由真的挂在 public 组上了（少注册一行的表现是 404，谁都看不见）
	registered := false
	for _, ri := range r.Routes() {
		if ri.Method == http.MethodPost && ri.Path == chatPublicTokenPath+":session_id/token" {
			registered = true
		}
	}
	if !registered {
		t.Fatal("POST /api/chat/public/sessions/:session_id/token 未注册")
	}

	owner := "v_r67_owner_" + time.Now().Format("150405.000")
	stranger := "v_r67_stranger_" + time.Now().Format("150405.000")
	sessionID := seedVisitorSession(t, database, channelID, owner)

	t.Run("本人的会话换到证", func(t *testing.T) {
		code, token := exchangeToken(t, r, sessionID, owner)
		if code != http.StatusOK {
			t.Fatalf("换证 HTTP %d，期望 200", code)
		}
		if token == "" {
			t.Fatal("200 但 data.visitor_token 为空")
		}
	})

	t.Run("换到的证能把这条会话的历史消息打开", func(t *testing.T) {
		// 这一格是整个修复的落点：缺陷的表现就是这条会话级调用回 403。
		_, token := exchangeToken(t, r, sessionID, owner)
		if code := getMessages(t, r, sessionID, owner, token); code != http.StatusOK {
			t.Errorf("带换回 token 拉历史消息回 HTTP %d，期望 200", code)
		}
	})

	t.Run("不带证仍是被拦的（换证不是把门拆了）", func(t *testing.T) {
		code := getMessages(t, r, sessionID, owner, "")
		if code != http.StatusForbidden {
			t.Errorf("无 token 拉历史消息回 HTTP %d，期望 403（IDOR 防护不能被本次修复绕过）", code)
		}
	})

	t.Run("别人的会话换不到证", func(t *testing.T) {
		code, token := exchangeToken(t, r, sessionID, stranger)
		if code != http.StatusForbidden {
			t.Errorf("访客 stranger 换他人会话的证回 HTTP %d，期望 403", code)
		}
		if token != "" {
			t.Error("403 却仍带回了 token")
		}
	})

	t.Run("不存在的会话回 403 且不区分存在性", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, chatPublicTokenPath+"sess_never_seeded"+owner+"/token", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Chat-Visitor-Id", owner)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("不存在的会话换证回 HTTP %d，期望 403", w.Code)
		}
		if !strings.Contains(w.Body.String(), "无权") {
			t.Errorf("回应的文案不是统一的那句: %s", truncateBody(w.Body.String()))
		}
	})

	t.Run("缺访客标识回 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, chatPublicTokenPath+sessionID+"/token", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("不带 X-Chat-Visitor-Id 回 HTTP %d，期望 400", w.Code)
		}
	})
}
