package controller

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// ChatPublicController 公开 chat API 控制器（访客端，无 JWT）
//
// 通过软解析 AppKeyResolve 鉴权后提供：
//   - 会话打开/续接
//   - 消息发送
//   - 历史消息查询
//   - 转人工 / 关闭 / 评分
//   - 离线消息拉取
//   - 拉取离线期间坐席/AI 回复（连接 WebSocket 时）
//
// 路由前缀：/api/chat/public
//
// 私域部署模式：不再强制要求 X-Chat-App-Key Header。
// 渠道 ID 软解析顺序：ctx.chat_channel_id > body.channel_id > X-Chat-Channel-Id > 默认 "default"。
//
// 安全：所有会话级操作要求 visitor_token（HMAC-SHA256 签名），
// token 由 OpenSession 返回，绑定 (channelID, visitorID, sessionID)，
// 防止 IDOR 越权访问他人会话历史。
type ChatPublicController struct {
	visitorSvc *service.VisitorChatService
	channelSvc *service.ChatChannelService
}

func visitorTokenFromRequest(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-Chat-Visitor-Token")); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.Query("visitor_token")); v != "" {
		return v
	}
	return ""
}

func validateVisitorTokenOrAbort(c *gin.Context, channelID, visitorID, sessionID string) bool {
	token := visitorTokenFromRequest(c)
	if token == "" {
		response.Error(c, http.StatusForbidden, "缺少 visitor_token，请先调用 OpenSession 获取")
		return false
	}
	if err := service.ValidateVisitorToken(token, channelID, visitorID, sessionID); err != nil {
		response.Error(c, http.StatusForbidden, "visitor_token 无效或已过期")
		return false
	}
	return true
}

// NewChatPublicController 构造控制器
func NewChatPublicController(visitorSvc *service.VisitorChatService, channelSvc *service.ChatChannelService) *ChatPublicController {
	return &ChatPublicController{visitorSvc: visitorSvc, channelSvc: channelSvc}
}

// GetChannelInfoByAppKey 根据 app_key 查询渠道公开信息（用于 widget 安装引导的连通性测试）
// GET /api/chat/public/channel/:app_key/info
func (ctrl *ChatPublicController) GetChannelInfoByAppKey(c *gin.Context) {
	appKey := c.Param("app_key")
	if appKey == "" {
		response.Error(c, http.StatusBadRequest, "app_key 不能为空")
		return
	}
	channel, err := ctrl.channelSvc.GetByAppKey(c.Request.Context(), appKey)
	if err != nil {
		response.NotFoundError(c, "渠道")
		return
	}
	response.Success(c, gin.H{
		"channel_id":           channel.ChannelID,
		"channel_name":         channel.ChannelName,
		"status":               channel.Status,
		"widget_title":         channel.WidgetTitle,
		"widget_color":         channel.WidgetColor,
		"widget_position":      channel.WidgetPosition,
		"welcome_message":      channel.WelcomeMessage,
		"auto_assign":          channel.AutoAssign,
		"confidence_threshold": channel.ConfidenceThreshold,
	}, "ok")
}

func resolveChannelID(c *gin.Context, reqBody *service.VisitorOpenSessionRequest) string {
	if v, ok := c.Get("chat_channel_id"); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return middleware.GetChatChannelID(c)
}

// OpenSession 打开（或续接）会话
// POST /api/chat/public/sessions
func (ctrl *ChatPublicController) OpenSession(c *gin.Context) {
	var req service.VisitorOpenSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if req.ChannelID == "" {
		req.ChannelID = resolveChannelID(c, &req)
	}
	if req.VisitorID == "" {
		req.VisitorID = c.GetHeader("X-Chat-Visitor-Id")
		if req.VisitorID == "" {
			req.VisitorID = c.Query("visitor_id")
		}
	}

	result, err := ctrl.visitorSvc.OpenSession(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, safeError(c, err, "打开会话失败，请稍后重试"))
		return
	}
	response.Success(c, result, "会话已打开")
}

// GetMessages 获取历史消息
// GET /api/chat/public/sessions/:session_id/messages
//
// 安全：需要有效的 visitor_token，防止 IDOR 越权读取他人会话历史
func (ctrl *ChatPublicController) GetMessages(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, "session_id 必填")
		return
	}
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" {
		response.Error(c, http.StatusBadRequest, "X-Chat-Visitor-Id 必填")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	messages, total, err := ctrl.visitorSvc.GetMessages(c.Request.Context(), channelID, visitorID, sessionID, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusBadRequest, safeError(c, err, "获取消息失败，请稍后重试"))
		return
	}
	response.SuccessWithList(c, messages, total)
}

// SendMessage 发送访客消息
// POST /api/chat/public/sessions/:session_id/messages
//
// 安全：需要有效的 visitor_token，防止 IDOR 越权向他人会话发消息
func (ctrl *ChatPublicController) SendMessage(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, "session_id 必填")
		return
	}
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" {
		response.Error(c, http.StatusBadRequest, "X-Chat-Visitor-Id 必填")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	var body struct {
		Content     string `json:"content" binding:"required"`
		ContentType string `json:"content_type"`
		MediaURL    string `json:"media_url"`
		MediaType   string `json:"media_type"`
		MediaName   string `json:"media_name"`
		MediaSize   int64  `json:"media_size"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if len([]rune(body.Content)) > 5000 {
		response.Error(c, http.StatusBadRequest, "消息内容过长，最多 5000 字符")
		return
	}

	body.Content = escapeHTML(body.Content)

	req := &service.VisitorSendMessageRequest{
		ChannelID:   channelID,
		VisitorID:   visitorID,
		SessionID:   sessionID,
		Content:     body.Content,
		ContentType: body.ContentType,
		MediaURL:    body.MediaURL,
		MediaType:   body.MediaType,
		MediaName:   body.MediaName,
		MediaSize:   body.MediaSize,
	}
	result, err := ctrl.visitorSvc.SendMessage(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, safeError(c, err, "发送消息失败，请稍后重试"))
		return
	}
	response.Success(c, result, "消息已发送")
}

// GetActiveSession 获取访客最近活跃会话（用于离线消息续接）
// GET /api/chat/public/sessions/active
func (ctrl *ChatPublicController) GetActiveSession(c *gin.Context) {
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" {
		response.Error(c, http.StatusBadRequest, "X-Chat-Visitor-Id 必填")
		return
	}

	session, err := ctrl.visitorSvc.GetLatestActiveSession(c.Request.Context(), channelID, visitorID)
	if err != nil {
		response.ErrorFromDB(c, err, safeError(c, err, "获取会话失败，请稍后重试"))
		return
	}
	if session == nil {
		response.Success(c, nil, "无活跃会话")
		return
	}
	response.Success(c, session, "ok")
}

// GetRecentClosedSessions 获取最近 7 天已结束会话
// GET /api/chat/public/sessions/recent-closed
func (ctrl *ChatPublicController) GetRecentClosedSessions(c *gin.Context) {
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" {
		response.Error(c, http.StatusBadRequest, "X-Chat-Visitor-Id 必填")
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	sessions, err := ctrl.visitorSvc.GetRecentClosedSessions(c.Request.Context(), channelID, visitorID, limit)
	if err != nil {
		response.ErrorFromDB(c, err, safeError(c, err, "获取会话列表失败，请稍后重试"))
		return
	}
	response.SuccessWithList(c, sessions, int64(len(sessions)))
}

// ExchangeSessionToken 访客为历史会话列表里的某一条会话换回 visitor_token
// POST /api/chat/public/sessions/:session_id/token
//
// 为什么要有这个端点：visitor_token 只随 OpenSession 返回，而 OpenSession 只会命中
// "最近活跃的那一条"。访客从 recent-closed 列表点开更早的会话时，前端带的是别的会话的
// token ⇒ 该会话的历史消息/关闭/评价全部 403（访客看到的是"点进去是空的"）。
//
// 安全：归属校验按 (platform=web_embed, account_id=channel, user_id=visitor) 三列全等，
// 换不到他人会话的 token；不属于当前访客时回**同一个** 403 文案，不区分"不存在"与"无权"，
// 免得这个端点变成会话 ID 探活器。
//
// 403 只给"归属不成立"：密钥缺失/库出错这类服务端故障必须回 5xx，不能伪装成权限问题——
// 前端对 403 是静默 catch，故障被报成 403 时访客只会看到"点开的会话是空的"。
func (ctrl *ChatPublicController) ExchangeSessionToken(c *gin.Context) {
	sessionID := c.Param("session_id")
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" || sessionID == "" {
		response.Error(c, http.StatusBadRequest, "参数不完整")
		return
	}

	token, err := ctrl.visitorSvc.ExchangeVisitorToken(c.Request.Context(), channelID, visitorID, sessionID)
	if err != nil {
		if errors.Is(err, service.ErrVisitorSessionNotOwned) {
			response.Error(c, http.StatusForbidden, "会话不存在或无权访问")
			return
		}
		logger.Ctx(c.Request.Context()).Err(err).
			Str("session_id", sessionID).
			Msg("[chat/public] 访客换回会话 visitor_token 失败（服务端故障，非归属问题）")
		response.Error(c, http.StatusInternalServerError, "服务暂时不可用，请稍后重试")
		return
	}
	response.Success(c, gin.H{"visitor_token": token}, "ok")
}

// GetOfflineMessages 拉取访客离线期间的坐席/AI 回复消息
// GET /api/chat/public/sessions/:session_id/offline-messages
// 用于访客重新连接 WebSocket 时，一次性拉取离线期间的所有回复。
// 标记消息为已投递（通过 message.delivered_at 字段）。
//
// 安全：需要有效的 visitor_token，防止 IDOR 越权拉取他人离线消息
func (ctrl *ChatPublicController) GetOfflineMessages(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, "session_id 必填")
		return
	}
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" {
		response.Error(c, http.StatusBadRequest, "X-Chat-Visitor-Id 必填")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	messages, err := ctrl.visitorSvc.GetOfflineMessages(c.Request.Context(), channelID, visitorID, sessionID)
	if err != nil {
		response.ErrorFromDB(c, err, safeError(c, err, "获取离线消息失败，请稍后重试"))
		return
	}
	response.SuccessWithList(c, messages, int64(len(messages)))
}

// RequestHumanTransfer 访客主动转人工
// POST /api/chat/public/sessions/:session_id/transfer
//
// 安全：需要有效的 visitor_token
func (ctrl *ChatPublicController) RequestHumanTransfer(c *gin.Context) {
	sessionID := c.Param("session_id")
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" || sessionID == "" {
		response.Error(c, http.StatusBadRequest, "参数不完整")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)

	body.Reason = escapeHTML(body.Reason)
	if len([]rune(body.Reason)) > 500 {
		body.Reason = body.Reason[:500]
	}

	if err := ctrl.visitorSvc.RequestHumanTransfer(c.Request.Context(), channelID, visitorID, sessionID, body.Reason); err != nil {
		response.Error(c, http.StatusBadRequest, safeError(c, err, "转人工失败，请稍后重试"))
		return
	}
	response.Success(c, nil, "已为您转接人工客服")
}

// CloseSession 访客关闭会话
// POST /api/chat/public/sessions/:session_id/close
//
// 安全：需要有效的 visitor_token
func (ctrl *ChatPublicController) CloseSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" || sessionID == "" {
		response.Error(c, http.StatusBadRequest, "参数不完整")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	if err := ctrl.visitorSvc.CloseSession(c.Request.Context(), channelID, visitorID, sessionID); err != nil {
		response.Error(c, http.StatusBadRequest, safeError(c, err, "关闭会话失败，请稍后重试"))
		return
	}
	response.Success(c, nil, "会话已关闭")
}

// RateSession 访客评分
// POST /api/chat/public/sessions/:session_id/rate
//
// 安全：需要有效的 visitor_token
func (ctrl *ChatPublicController) RateSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	channelID := resolveChannelID(c, nil)
	visitorID := c.GetHeader("X-Chat-Visitor-Id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" || sessionID == "" {
		response.Error(c, http.StatusBadRequest, "参数不完整")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	var body struct {
		Rating  int    `json:"rating" binding:"required,min=1,max=5"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "评分必须在 1-5 之间")
		return
	}

	body.Comment = escapeHTML(body.Comment)
	if len([]rune(body.Comment)) > 1000 {
		body.Comment = body.Comment[:1000]
	}

	if err := ctrl.visitorSvc.RateSession(c.Request.Context(), channelID, visitorID, sessionID, body.Rating, body.Comment); err != nil {
		response.Error(c, http.StatusBadRequest, safeError(c, err, "评分失败，请稍后重试"))
		return
	}
	response.Success(c, nil, "感谢您的评价")
}

// CountAvailableAgents 公开可用坐席数（用于显示"当前 X 位客服在线"）
// GET /api/chat/public/agents/available
func (ctrl *ChatPublicController) CountAvailableAgents(c *gin.Context) {
	count, err := ctrl.visitorSvc.CountAvailableAgents(c.Request.Context())
	if err != nil {
		response.ErrorFromDB(c, err, safeError(c, err, "获取坐席数量失败，请稍后重试"))
		return
	}
	response.Success(c, gin.H{"available": count}, "ok")
}

// GetUploadToken 生成七牛上传凭证
// GET /api/chat/public/upload-token?file_type=image&ext=jpg&size=102400
//
// 返回：
//   - upload_url: 七牛华东上传入口
//   - token: 上传凭证
//   - key: 预生成的文件 key（chat/yyyy/MM/<uuid>.ext）
//   - public_url: 上传后可直接访问的 CDN URL
//   - expires_in: token 有效期（秒）
//
// 安全：需要有效的 visitor_token + 有效会话
func (ctrl *ChatPublicController) GetUploadToken(c *gin.Context) {

	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, "缺少 session_id 参数")
		return
	}
	channelID := middleware.GetChatChannelID(c)
	visitorID := strings.TrimSpace(c.Query("visitor_id"))
	if visitorID == "" {
		response.Error(c, http.StatusBadRequest, "缺少 visitor_id 参数")
		return
	}

	if !validateVisitorTokenOrAbort(c, channelID, visitorID, sessionID) {
		return
	}

	if _, err := ctrl.visitorSvc.GetSessionByVisitorSessionID(c.Request.Context(), channelID, visitorID, sessionID); err != nil {
		response.Error(c, http.StatusForbidden, "无效的会话凭证")
		return
	}

	cfg := config.GetAppConfig()
	if cfg.Storage.Type != "qiniu" || cfg.Storage.Qiniu.AccessKey == "" {
		response.Error(c, http.StatusServiceUnavailable, "对象存储未配置")
		return
	}

	fileType := c.DefaultQuery("file_type", "file")
	ext := c.DefaultQuery("ext", "")
	sizeStr := c.DefaultQuery("size", "0")
	size, err := utils.ParseInt64Strict("chat_public.size", sizeStr)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "size 参数非法")
		return
	}

	const maxSize = 20 * 1024 * 1024
	if size > maxSize {
		response.Error(c, http.StatusRequestEntityTooLarge, "文件大小超出 20MB 限制")
		return
	}

	allowedExts := map[string][]string{
		"image": {"jpg", "jpeg", "png", "gif", "webp"},
		"file":  {"pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "zip", "rar"},
		"audio": {"mp3", "wav", "ogg", "m4a"},
		"video": {"mp4", "webm"},
	}
	extLower := strings.ToLower(ext)
	if ext == "" {
		response.Error(c, http.StatusBadRequest, "缺少 ext 参数")
		return
	}
	if list, ok := allowedExts[fileType]; ok {
		valid := false
		for _, e := range list {
			if e == extLower {
				valid = true
				break
			}
		}
		if !valid {
			response.Error(c, http.StatusBadRequest, "文件类型不被允许: "+ext)
			return
		}
	}

	now := time.Now()
	uid := uuid.NewString()
	key := fmt.Sprintf("chat/%04d/%02d/%s.%s", now.Year(), now.Month(), uid, extLower)

	expires := now.Add(1 * time.Hour).Unix()
	policy := map[string]any{
		"scope":      cfg.Storage.Qiniu.Bucket + ":" + key,
		"deadline":   expires,
		"fsizeLimit": maxSize,
		"returnBody": `{"key":"$(key)","hash":"$(etag)","fsize":$(fsize),"fname":"$(fname)"}`,
	}
	policyBytes, _ := json.Marshal(policy)
	policyEncoded := base64.URLEncoding.EncodeToString(policyBytes)

	mac := hmac.New(sha1.New, []byte(cfg.Storage.Qiniu.SecretKey))
	mac.Write([]byte(policyEncoded))
	signature := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	token := cfg.Storage.Qiniu.AccessKey + ":" + signature + ":" + policyEncoded

	uploadDomain := cfg.Storage.Qiniu.UploadDomain
	if uploadDomain == "" {
		uploadDomain = "up-z2.qiniup.com"
	}
	uploadURL := fmt.Sprintf("https://%s", uploadDomain)
	publicURL := "https://" + strings.TrimSuffix(cfg.Storage.Qiniu.Domain, "/") + "/" + key

	response.Success(c, gin.H{
		"upload_url": uploadURL,
		"token":      token,
		"key":        key,
		"public_url": publicURL,
		"expires_in": 3600,
	}, "ok")
}

func escapeHTML(s string) string {
	if s == "" {
		return s
	}
	replacer := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		"&", "&amp;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(s)
}

var (
	internalErrorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)sql|database|mysql|postgres|connection|timeout|deadlock`),
		regexp.MustCompile(`(?i)stack|panic|runtime|goroutine`),
		regexp.MustCompile(`(?i)/[\w./-]+\.go:\d+`),
		regexp.MustCompile(`(?i)dial|tcp|udp|network`),
		regexp.MustCompile(`(?i)permission denied|access denied|forbidden`),
	}
)

func safeError(c *gin.Context, err error, defaultMsg string) string {
	if err == nil {
		return defaultMsg
	}
	msg := err.Error()
	for _, re := range internalErrorPatterns {
		if re.MatchString(msg) {
			logger.Ctx(c.Request.Context()).Error().Err(err).Str("original_error", msg).Msg("内部错误已脱敏处理")
			return defaultMsg
		}
	}
	return msg
}
