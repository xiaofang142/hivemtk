package service

import (
	"context"

	"crypto/subtle"

	"encoding/json"

	"errors"

	"fmt"

	"strconv"

	"strings"

	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

func (s *WebhookService) getFeishuEncryptKey(ctx context.Context, accountID string) string {
	if s.feishuRepo == nil {
		return ""
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	acc, err := s.feishuRepo.GetByID(ctx, uint(id))
	if err != nil || acc == nil {
		return ""
	}
	return acc.EncryptKey
}

// getFeishuVerificationToken 取账号的 Verification Token（明文事件模式下唯一的来源凭证）。
func (s *WebhookService) getFeishuVerificationToken(ctx context.Context, accountID string) string {
	if s.feishuRepo == nil {
		return ""
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	acc, err := s.feishuRepo.GetByID(ctx, uint(id))
	if err != nil || acc == nil {
		return ""
	}
	return acc.VerificationToken
}

// HandleFeishuURLVerification 处理飞书事件订阅的 POST url_verification 挑战。
// 官方流程：配置回调 URL 时飞书 POST {"challenge":"...","token":"...","type":"url_verification"}
// （开启 Encrypt Key 时为 {"encrypt":"..."} 信封），服务端必须原样回显 {"challenge": "..."}。
// 返回 (challenge, true, nil) 表示已处理；(…, false, nil) 表示非验证请求（调用方继续常规处理）；
// 校验失败返回 err（token 与账号 VerificationToken 不匹配等，防任意第三方伪造绑定）。
func (s *WebhookService) HandleFeishuURLVerification(ctx context.Context, accountID string, raw []byte) (string, bool, error) {
	payload := raw
	var probe struct {
		Encrypt   string `json:"encrypt"`
		Challenge string `json:"challenge"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", false, nil
	}
	isVerification := probe.Type == "url_verification"
	if !isVerification && probe.Encrypt == "" {
		return "", false, nil
	}
	if probe.Encrypt != "" {
		key := s.getFeishuEncryptKey(ctx, accountID)
		if key == "" {
			return "", true, errors.New("feishu encrypt_key not configured")
		}
		plain, derr := DecryptFeishuEvent(key, probe.Encrypt)
		if derr != nil {
			return "", true, fmt.Errorf("decrypt url_verification: %w", derr)
		}
		payload = plain
	}
	var req struct {
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return "", true, fmt.Errorf("parse url_verification: %w", err)
	}
	if req.Type != "url_verification" || req.Challenge == "" {
		if isVerification {

			return "", true, fmt.Errorf("url_verification missing challenge")
		}
		return "", false, nil
	}
	id, perr := strconv.ParseUint(accountID, 10, 64)
	if perr != nil || id == 0 || s.feishuRepo == nil {
		return "", true, errors.New("invalid feishu account_id")
	}
	acc, gerr := s.feishuRepo.GetByID(ctx, uint(id))
	if gerr != nil || acc == nil {
		return "", true, errors.New("feishu account not found")
	}

	if acc.VerificationToken == "" || subtle.ConstantTimeCompare([]byte(req.Token), []byte(acc.VerificationToken)) != 1 {
		return "", true, errors.New("feishu verification token mismatch")
	}
	return req.Challenge, true, nil
}

// feishuTimestamp 飞书官方把 create_time 以**字符串**下发（文档示例
// "create_time": "1609073151345"，header 层是纳秒、event.message 层是毫秒）。
// 结构体原样写成 int64 会让整个回调在 json.Unmarshal 阶段报错，
// 结果是一条真实入站消息都落不了库；这里对两种 JSON 形态都容错。
type feishuTimestamp string

func (t *feishuTimestamp) UnmarshalJSON(b []byte) error {
	*t = feishuTimestamp(strings.Trim(string(b), `"`))
	return nil
}

func (s *WebhookService) dispatchFeishu(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if s.lazyDB() == nil {
		return nil, nil
	}
	s.ensureReposFromDB(ctx)

	var envProbe struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &envProbe); err == nil && envProbe.Encrypt != "" {
		key := s.getFeishuEncryptKey(ctx, accountID)
		if key == "" {
			return nil, fmt.Errorf("feishu encrypted event but encrypt_key not configured (account=%s)", accountID)
		}
		plain, derr := DecryptFeishuEvent(key, envProbe.Encrypt)
		if derr != nil {
			return nil, fmt.Errorf("feishu decrypt event: %w", derr)
		}
		raw = plain
	}

	var fsPayload struct {
		Challenge string `json:"challenge"`
		Type      string `json:"type"`
		Header    *struct {
			EventType    string          `json:"event_type"`
			AppID        string          `json:"app_id"`
			TenantKey    string          `json:"tenant_key"`
			EventID      string          `json:"event_id"`
			Token        string          `json:"token"`
			CreateTime   feishuTimestamp `json:"create_time"`
			AppSecretVer int             `json:"app_secret_ver"`
		} `json:"header,omitempty"`
		Event *struct {
			Sender *struct {
				SenderID *struct {
					UnionID string `json:"union_id"`
					UserID  string `json:"user_id"`
					OpenID  string `json:"open_id"`
				} `json:"sender_id"`
				SenderType string `json:"sender_type"`
				TenantKey  string `json:"tenant_key"`
			} `json:"sender"`
			Message *struct {
				MessageID   string          `json:"message_id"`
				ChatID      string          `json:"chat_id"`
				ChatType    string          `json:"chat_type"`
				MessageType string          `json:"message_type"`
				Content     string          `json:"content"`
				CreateTime  feishuTimestamp `json:"create_time"`
			} `json:"message"`
		} `json:"event,omitempty"`
	}
	if err := json.Unmarshal(raw, &fsPayload); err != nil {
		return nil, fmt.Errorf("feishu parse: %w", err)
	}

	if fsPayload.Challenge != "" && (fsPayload.Type == "url_verification" || fsPayload.Header == nil) {

		return nil, nil
	}
	if fsPayload.Event == nil || fsPayload.Event.Message == nil {
		return nil, nil
	}
	m := fsPayload.Event.Message

	hubType := InboundHubMsgType(m.MessageType)
	placeholder := feishuInboundPlaceholder(m.MessageType)
	content := feishuInboundText(m.MessageType, m.Content)
	if strings.TrimSpace(content) == "" {
		content = placeholder
	}
	mediaKey, mediaResType, mediaName := feishuInboundMedia(m.MessageType, m.Content)
	senderID := ""
	if fsPayload.Event.Sender != nil && fsPayload.Event.Sender.SenderID != nil {
		senderID = fsPayload.Event.Sender.SenderID.OpenID
		if senderID == "" {
			senderID = fsPayload.Event.Sender.SenderID.UserID
		}
		if senderID == "" {
			senderID = fsPayload.Event.Sender.SenderID.UnionID
		}
	}
	hub := &model.MessageHub{
		Platform:       "feishu",
		AccountID:      accountID,
		MsgID:          m.MessageID,
		Direction:      "inbound",
		SenderID:       senderID,
		ConversationID: m.ChatID,
		MsgType:        hubType,
		Content:        content,
		SentAt:         time.Now(),
		IsGroup:        m.ChatType == "group",
		GroupID:        m.ChatID,
	}
	if mediaKey != "" {
		hub.Extra = model.JSONMap{"file_key": mediaKey, "message_id": m.MessageID}
		if mediaName != "" {
			hub.Extra["file_name"] = mediaName
		}
	}
	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
			return nil, err
		}
	}
	// 媒体转存（best-effort）：异步下载飞书资源并回填长期 URL
	if mediaKey != "" {
		s.persistFeishuMediaAsync(ctx, accountID, m.MessageID, mediaKey, mediaResType, mediaName)
	}
	s.upsertInboxFromHub(ctx, hub, "")

	MineUnifiedLead(ctx, s, hub, FeishuLeadAdapter{}, accountID, m.ChatID, "", senderID, "", "", content)

	p.Content = content
	p.Sender = senderID
	p.ChatID = m.ChatID
	return hub, nil
}

// feishuInboundPlaceholders 官方入站消息类型 → 工作台/AI 可读的中文占位符。
//
// 类型名本身不进中台词表（那一层由 InboundHubMsgType 收口），这里只管一件事：
// 客户发了什么，屏幕上就写得出来。修复前占位符直接拼官方原值，于是
// "[media]""[post]""[sticker]" 成了客服和 AI 看到的全文。
var feishuInboundPlaceholders = map[string]string{
	"text":                 "[文本]",
	"post":                 "[图文]",
	"image":                "[图片]",
	"file":                 "[文件]",
	"folder":               "[文件夹]",
	"audio":                "[语音]",
	"media":                "[视频]",
	"sticker":              "[表情]",
	"location":             "[位置]",
	"interactive":          "[卡片]",
	"hongbao":              "[红包]",
	"share_chat":           "[群名片]",
	"share_user":           "[个人名片]",
	"calendar":             "[日程]",
	"general_calendar":     "[日程]",
	"share_calendar_event": "[日程]",
	"video_chat":           "[视频会议]",
	"todo":                 "[待办]",
	"vote":                 "[投票]",
	"system":               "[系统消息]",
	"merge_forward":        "[合并转发]",
}

// feishuInboundPlaceholder 未知类型留官方原值：官方类型表还会新增，
// 新类型上线时这条消息仍然「看得见、认得出是哪种」，而不是静默变成一个中文词。
func feishuInboundPlaceholder(rawType string) string {
	if ph, ok := feishuInboundPlaceholders[rawType]; ok {
		return ph
	}
	return "[" + rawType + "]"
}

// feishuInboundText 取入站正文。
//
// text 型顶层就有 {"text":...}；post（富文本）没有 —— 官方结构是
// {"title":"我是一个标题","content":[[{"tag":"text","text":"第一行:"}]]}
// （**不带**发送侧的 zh_cn/en_us 语言壳），修复前整段丢掉，客户写了一屏图文，
// AI 收到的是三个字母 "[post]"。
func feishuInboundText(rawType, contentJSON string) string {
	if strings.TrimSpace(contentJSON) == "" {
		return ""
	}
	if rawType == "post" {
		return feishuPostText(contentJSON)
	}
	var obj struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(contentJSON), &obj)
	return obj.Text
}

// feishuPostText 把富文本按「标题 + 逐行节点文本」拼回可读正文。
// 不做按 tag 白名单过滤：img/emotion/hr 本就没有 text，多一道 switch 只是多个漏拼的地方。
func feishuPostText(contentJSON string) string {
	var post struct {
		Title   string `json:"title"`
		Content [][]struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(contentJSON), &post); err != nil {
		return ""
	}
	lines := make([]string, 0, len(post.Content)+1)
	if t := strings.TrimSpace(post.Title); t != "" {
		lines = append(lines, t)
	}
	for _, row := range post.Content {
		var b strings.Builder
		for _, node := range row {
			b.WriteString(node.Text)
		}
		if line := strings.TrimSpace(b.String()); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// feishuInboundMedia 取该条消息要转存的资源：键、官方 resources 接口的 type 参数、原始文件名。
//
// 官方键位很散：image→image_key；file/audio/media/folder/sticker→file_key
// （file/media/folder 另带 file_name）；media 的 image_key 是视频封面，不单独转存。
// 修复前 switch 只认 image/file/audio/media，表情包与文件夹消息连资源键都没取到。
// post 里嵌的 img/media 节点也带键，一并取出（图文混排是客户常见发法）。
// 取不到键时三个返回值全为 0：调用方按 key 非空决定是否起转存，
// 不留"有 type 没 key"的半截返回（FetchFeishuMedia 对空 resType 是直接报错的）。
func feishuInboundMedia(rawType, contentJSON string) (key, resType, fileName string) {
	if strings.TrimSpace(contentJSON) == "" {
		return "", "", ""
	}
	switch rawType {
	case "image":
		var obj struct {
			ImageKey string `json:"image_key"`
		}
		if json.Unmarshal([]byte(contentJSON), &obj) != nil {
			return "", "", ""
		}
		if obj.ImageKey == "" {
			return "", "", ""
		}
		return obj.ImageKey, "image", ""
	case "file", "folder", "audio", "media", "sticker":
		var obj struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if json.Unmarshal([]byte(contentJSON), &obj) != nil {
			return "", "", ""
		}
		if obj.FileKey == "" {
			return "", "", ""
		}
		return obj.FileKey, "file", obj.FileName
	case "post":
		var post struct {
			Content [][]struct {
				Tag      string `json:"tag"`
				ImageKey string `json:"image_key"`
				FileKey  string `json:"file_key"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(contentJSON), &post) != nil {
			return "", "", ""
		}
		for _, row := range post.Content {
			for _, node := range row {
				if node.Tag == "img" && node.ImageKey != "" {
					return node.ImageKey, "image", ""
				}
				if node.Tag == "media" && node.FileKey != "" {
					return node.FileKey, "file", ""
				}
			}
		}
	}
	return "", "", ""
}

// 飞书媒体链路的三条外部 IO 腿以函数变量注入（与 WhatsApp / QQ 同一手法）：
// 取 tenant_access_token 与下载资源都要访问 open.feishu.cn，转存要经 obs_config/存储驱动，
// 测试环境三者全不可达，用替身才能跑通「取凭证 → 下载 → 转存 → 回填」全链（生产指向实现本身）。
var (
	feishuTenantTokenFn = func(ctx context.Context, integration *FeishuIntegrationService, acc *model.FeishuAccount) (string, error) {
		return integration.getAccessToken(ctx, acc)
	}
	feishuMediaFetchFn = FetchFeishuMedia
	feishuMediaStoreFn = channelMediaPersist
)

// persistFeishuMediaAsync 异步下载飞书消息资源并转存，按 msg_id 回填 message_hub.media_url。
// resType 由调用方按资源种类给定（官方 GET /messages/{mid}/resources/{key} 必带 ?type=image|file，
// 用错 type 官方直接报 2340069），这里不再从消息类型反推。
func (s *WebhookService) persistFeishuMediaAsync(ctx context.Context, accountID, messageID, fileKey, resType, fileName string) {
	utils.SafeGo(ctx, "feishu.media_persist", func(gctx context.Context) {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID == 0 {
			return
		}
		if s.feishuRepo == nil {
			return
		}
		acc, gerr := s.feishuRepo.GetByID(gctx, uint(accID))
		if gerr != nil || acc == nil {
			logger.Ctx(gctx).Warn().Str("account_id", accountID).Msg("[Feishu] 媒体转存跳过：账号不存在")
			return
		}
		integration := s.feishuIntegration
		if integration == nil {
			integration = NewFeishuIntegrationService(s.lazyDB())
		}
		tenantToken, tkerr := feishuTenantTokenFn(gctx, integration, acc)
		if tkerr != nil || tenantToken == "" {
			logger.Ctx(gctx).Warn().Err(tkerr).Str("account_id", accountID).Msg("[Feishu] 媒体转存跳过：tenant_access_token 获取失败")
			return
		}
		rc, contentType, derr := feishuMediaFetchFn(gctx, tenantToken, messageID, fileKey, resType)
		if derr != nil {
			logger.Ctx(gctx).Warn().Err(derr).Str("file_key", fileKey).Msg("[Feishu] 媒体下载失败（占位符保留）")
			return
		}
		defer func() { _ = rc.Close() }()
		data, rerr := readInboundMedia(rc, maxInboundMediaBytes)
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("file_key", fileKey).Msg("[Feishu] 媒体读取失败（占位符保留）")
			return
		}
		publicURL, serr := feishuMediaStoreFn(gctx, "feishu", fileKey, data, contentType, fileName)
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("file_key", fileKey).Msg("[Feishu] 媒体转存失败")
			return
		}
		EnrichHubMediaURLByMsgID(gctx, s.messageHubRepo, "feishu", accountID, messageID, publicURL)
		logger.Ctx(gctx).Info().Str("file_key", fileKey).Str("url", publicURL).Msg("[Feishu] 媒体已转存")
	})
}
