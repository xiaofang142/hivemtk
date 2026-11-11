package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// chat_public 上传凭证发放的 file_type 白名单 fail-closed 用例。
//
// 【改前行为（fail-open，本刀钉死）】GetUploadToken 里
// `if list, ok := allowedExts[fileType]; ok` —— file_type 不在 image/file/audio/video
// 四个已知值时 ok=false，整段白名单被**静默跳过**：`?file_type=x&ext=exe` 一路走到
// 凭证签发，拿到 exe 扩展名的 ≤20MB 上传凭证（token+key+policy 全套）。改前本用例
// 200（fail-open 复现，负例先红）；改后未知 file_type 必须 400 fail-closed。
//
// 已知类型×非法扩展名的 400 与已知类型×合法扩展名的 200 是防改坏守卫：
// fail-closed 只许收窄未知 file_type，不许动白名单本身。
func setupUploadTokenRouter(t *testing.T) (*gin.Engine, *service.VisitorChatService) {
	t.Helper()

	const secret = "chat-public-upload-token-test-secret-0123456789"
	prevCfg := config.GetAppConfig()
	config.SetAppConfig(&config.AppConfig{
		Security: config.SecurityConfig{VisitorTokenSecret: secret},
		Storage: config.StorageConfig{
			Type: "qiniu",
			Qiniu: config.QiniuConfig{
				AccessKey: "test-ak",
				SecretKey: "test-sk",
				Bucket:    "test-bucket",
				Domain:    "cdn.example.com",
			},
		},
	})
	t.Cleanup(func() { config.SetAppConfig(&prevCfg) })

	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.ChatChannel{},
		&model.QuickReply{},
		&model.SessionTag{},
		// OpenSession 有黑名单前置校验，缺这张表会直接报 relation 不存在
		&model.UserBlacklist{},
		&model.CSATSurvey{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	channelSvc := service.MustNewChatChannelService(repository.NewChatChannelRepositoryWithDB(database))
	visitorSvc := service.NewVisitorChatService(channelSvc, nil, service.NewChannelAgentBindingService())

	ctrl := NewChatPublicController(visitorSvc, channelSvc)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/chat/public/upload-token", ctrl.GetUploadToken)
	return r, visitorSvc
}

// openUploadTokenSession 建一条会话拿到 (sessionID, visitorID, token) 三元组，
// token 与会话归属同源，保证用例卡在 file_type 这一层而不是鉴权层。
func openUploadTokenSession(t *testing.T, svc *service.VisitorChatService) (sessionID, visitorID, token string) {
	t.Helper()
	stamp := time.Now().Format("150405.000000")
	visitorID = "v_upload_" + stamp
	res, err := svc.OpenSession(context.Background(), &service.VisitorOpenSessionRequest{
		ChannelID:   "default",
		VisitorID:   visitorID,
		VisitorName: "上传凭证用例访客",
		Resume:      false,
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}
	if res.Session == nil || res.VisitorToken == "" {
		t.Fatalf("OpenSession 未返回会话或 token：%+v", res)
	}
	return res.Session.SessionID, visitorID, res.VisitorToken
}

func getUploadTokenHTTP(t *testing.T, r *gin.Engine, sessionID, visitorID, token, fileType, ext string) *httptest.ResponseRecorder {
	t.Helper()
	q := "?session_id=" + sessionID + "&visitor_id=" + visitorID +
		"&channel_id=default&visitor_token=" + token + "&size=102400"
	if fileType != "" {
		q += "&file_type=" + fileType
	}
	if ext != "" {
		q += "&ext=" + ext
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat/public/upload-token"+q, nil)
	r.ServeHTTP(w, req)
	return w
}

// 负例①：未知 file_type（x）+ 任意扩展名（exe）必须拒绝。
// 改前：200 + 发出 exe 凭证（fail-open，见文件头注释）；改后：400。
func TestGetUploadToken_UnknownFileType_FailClosed(t *testing.T) {
	r, svc := setupUploadTokenRouter(t)
	sessionID, visitorID, token := openUploadTokenSession(t, svc)

	w := getUploadTokenHTTP(t, r, sessionID, visitorID, token, "x", "exe")
	if w.Code != http.StatusBadRequest {
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		t.Fatalf("未知 file_type=x&ext=exe 必须 400（fail-closed），实际 %d body=%v", w.Code, body)
	}
}

// 防改坏守卫：已知类型 + 白名单内扩展名照常发凭证。
func TestGetUploadToken_KnownFileType_AllowedExt(t *testing.T) {
	r, svc := setupUploadTokenRouter(t)
	sessionID, visitorID, token := openUploadTokenSession(t, svc)

	w := getUploadTokenHTTP(t, r, sessionID, visitorID, token, "image", "jpg")
	if w.Code != http.StatusOK {
		t.Fatalf("image+jpg 在白名单内应 200 发凭证，实际 %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Key       string `json:"key"`
			Token     string `json:"token"`
			ExpiresIn int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体解析失败: %v body=%s", err, w.Body.String())
	}
	if body.Code != 0 || body.Data.Token == "" || body.Data.ExpiresIn != 3600 {
		t.Fatalf("凭证字段不全：code=%d token 空=%v expires_in=%d", body.Code, body.Data.Token == "", body.Data.ExpiresIn)
	}
	if len(body.Data.Key) < 4 || body.Data.Key[len(body.Data.Key)-4:] != ".jpg" {
		t.Fatalf("key 应以 .jpg 结尾，实际 %q", body.Data.Key)
	}
}

// 防改坏守卫：已知类型 + 白名单外扩展名维持既有 400（白名单本身不许松动）。
func TestGetUploadToken_KnownFileType_DisallowedExt(t *testing.T) {
	r, svc := setupUploadTokenRouter(t)
	sessionID, visitorID, token := openUploadTokenSession(t, svc)

	w := getUploadTokenHTTP(t, r, sessionID, visitorID, token, "image", "exe")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("image+exe 应 400，实际 %d body=%s", w.Code, w.Body.String())
	}
}

// 防改坏守卫：缺 ext 维持既有 400。
func TestGetUploadToken_MissingExt(t *testing.T) {
	r, svc := setupUploadTokenRouter(t)
	sessionID, visitorID, token := openUploadTokenSession(t, svc)

	w := getUploadTokenHTTP(t, r, sessionID, visitorID, token, "image", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺 ext 应 400，实际 %d body=%s", w.Code, w.Body.String())
	}
}
