package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// DingTalkAppService 钉钉企业内部应用账号服务（CRUD + 回调验签 + 入站收消息）
type DingTalkAppService struct {
	repo       repository.DingTalkAppRepository
	webhookSvc *WebhookService
	// hubRepo 只用于入站媒体的回填（message_hub 的 media_url + Extra.media_urls 一次写全）。
	hubRepo *repository.MessageHubRepository
}

// NewDingTalkAppService 创建钉钉应用账号服务。
// webhookSvc 复用 WebhookService（其 ingressSvc 已在 NewWebhookService 中注入），
// 以便入站消息经 HandleIngressMessage 进入统一 AI 派发管线。
//
// 注：保留 db *gorm.DB 入参以维持向后兼容（router 装配不改动），
// 内部在构造函数中实例化 repository，service struct 不直接持有 *gorm.DB。
func NewDingTalkAppService(db *gorm.DB, webhookSvc *WebhookService) *DingTalkAppService {
	return &DingTalkAppService{
		repo:       repository.NewDingTalkAppRepository(db),
		webhookSvc: webhookSvc,
		hubRepo:    repository.NewMessageHubRepositoryWithDB(db),
	}
}

// CreateAccount 创建账号
func (s *DingTalkAppService) CreateAccount(ctx context.Context, acc *model.DingTalkAppAccount) error {
	return s.repo.Create(ctx, acc)
}

// GetAccount 查询账号
func (s *DingTalkAppService) GetAccount(ctx context.Context, id uint) (*model.DingTalkAppAccount, error) {
	return s.repo.FindByID(ctx, id)
}

// UpdateAccount 更新账号
func (s *DingTalkAppService) UpdateAccount(ctx context.Context, acc *model.DingTalkAppAccount) error {
	return s.repo.Update(ctx, acc)
}

// ListAccounts 列出账号
func (s *DingTalkAppService) ListAccounts(ctx context.Context) ([]model.DingTalkAppAccount, error) {
	return s.repo.ListAll(ctx)
}

// DeleteAccount 删除账号
func (s *DingTalkAppService) DeleteAccount(ctx context.Context, id uint) error {
	return s.repo.DeleteByID(ctx, id)
}

// VerifyCallback 钉钉回调 URL 验证（GET）。
// 钉钉对配置的回调地址发起 GET，携带 signature/timestamp/nonce/echostr。
// 本地用 token 计算 signature 比对；一致则回显 echostr（配置了 AESKey 时先解密）。
func (s *DingTalkAppService) VerifyCallback(ctx context.Context, accountID uint, signature, timestamp, nonce, echostr string) (string, error) {
	acc, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", errors.New("account not found")
	}
	if acc.Token == "" {
		return "", errors.New("callback token not configured")
	}

	mac := hmac.New(sha256.New, []byte(acc.Token))
	mac.Write([]byte(timestamp + "\n" + nonce))
	expect := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expect), []byte(signature)) != 1 {
		return "", errors.New("signature verify failed")
	}
	if acc.AESKey == "" {
		return echostr, nil
	}
	plain, err := dingTalkDecrypt(acc.AESKey, echostr)
	if err != nil {
		return "", fmt.Errorf("decrypt echostr: %w", err)
	}
	return plain, nil
}

// 明文机器人回调的官方时间窗：timestamp 与本地时间差须小于 1 小时。
const dingTalkRobotCallbackWindow = time.Hour

// ReceiveMessage 处理钉钉两条官方入站通道（二者验签算法完全不同）：
//
//   - body 含非空 encrypt → 事件订阅 HTTP 回调：query 的 signature 须等于
//     sha1(sort(token, timestamp, nonce, encrypt))，明文按官方布局解密。
//   - 否则 → 企业内部机器人 HTTP 模式回调：body 为明文 JSON，header 的 sign 须等于
//     base64(HMAC-SHA256("<timestamp>\n<appSecret>", key=appSecret))，且 timestamp 在 1 小时窗内。
//
// 两种模式都 fail-closed：对应密钥未配置时拒收（仅 ALLOW_INSECURE_WEBHOOK=true 可跳过，受启动护栏约束）。
func (s *DingTalkAppService) ReceiveMessage(ctx context.Context, accountID uint, raw []byte, query, headers map[string]string) error {
	acc, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return errors.New("account not found")
	}
	if !acc.InboundEnabled {
		return errors.New("inbound not enabled")
	}
	var env struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("parse envelope: %w", err)
	}
	payload := raw
	if env.Encrypt != "" {
		if err := verifyDingTalkEventSignature(acc.Token, env.Encrypt, query); err != nil {
			return err
		}
		plain, derr := dingTalkDecrypt(acc.AESKey, env.Encrypt)
		if derr != nil {
			return fmt.Errorf("decrypt message: %w", derr)
		}
		payload = []byte(plain)
	} else if err := verifyDingTalkRobotSign(acc.AppSecret, headers); err != nil {
		return err
	}
	var msg dingTalkInboundMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return fmt.Errorf("parse message: %w", err)
	}
	msgType, content, downloadCodes := dingTalkInboundBody(msg.MsgType, &msg.Content, msg.Text.Content)

	sender := msg.SenderStaffID
	if sender == "" {
		sender = msg.SenderID
	}
	if sender == "" {
		sender = msg.ConversationID
	}
	agent := acc.AIAgentID
	if agent == "" {
		agent = "sales_engine"
	}
	// 官方 conversationType：1=单聊、2=群聊。群聊里平台只在「@机器人」时才回调
	// （单聊则直接回调），所以中台不需要再判 @：进到这里的就是该回的。
	isGroup := msg.ConversationType == "2"
	event := &model.MessageEvent{
		EventID:        fmt.Sprintf("dt-%d-%s", accountID, dingTalkDedupKey(payload, msg.MsgID)),
		SessionID:      fmt.Sprintf("dt-%d-%s", accountID, msg.ConversationID),
		Channel:        model.ChannelDingTalk,
		SenderID:       sender,
		SenderName:     msg.SenderNick,
		MsgType:        msgType,
		Content:        content,
		ConversationID: msg.ConversationID,
		IsGroup:        isGroup,
		Timestamp:      dingTalkMessageTime(msg.CreateAt),
		AIAgent:        agent,
		Extra: map[string]any{
			"account_id": fmt.Sprintf("%d", accountID),
			// 官方 msgId 是这条消息的稳定主键：中台据此跳过内容窗口去重（否则同一客户
			// 五分钟内连发的第二条相同文本会被判重复丢掉，N-14），并参与出站回环识别。
			"channel_msg_id":             msg.MsgID,
			"session_webhook":            msg.SessionWebhook,
			"session_webhook_expired_at": msg.SessionWebhookExpiredTime,
			// 原样留一份官方值：IsGroup 把「2」和「字段没下发」压成同一个 false，
			// 排障时要能分辨是平台没升应用还是我们读错了。
			"conversation_type": string(msg.ConversationType),
		},
	}
	if isGroup {
		// 官方没有独立的群 id 字段，conversationId 即群会话 id（飞书 GroupID=ChatID 同口径）。
		// GroupID 非空才能让 AI 侧收敛成「一个群一个会话」而不是每个发言人一个会话。
		event.GroupID = msg.ConversationID
		if msg.ConversationTitle != "" {
			event.Extra["group_name"] = msg.ConversationTitle
		}
	}
	if msg.IsAdmin.Set {
		event.Extra["is_admin"] = msg.IsAdmin.Val
	}
	if msg.IsInAtList.Set {
		event.Extra["is_in_at_list"] = msg.IsInAtList.Val
	}
	// M-01：downloadCode 会过期（官方未公布时限，只在错误码里给「下载码有误或者已经过期」），
	// 所以入站当场必须把引用落库并立刻换成长期 URL；robotCode 是换取接口的必填项，
	// 只有回调带下来才有（自定义机器人无 robotCode，那条路径本就收不到媒体）。
	if msg.RobotCode != "" {
		event.Extra["robot_code"] = msg.RobotCode
	}
	if len(downloadCodes) > 0 {
		event.Extra["media_download_code"] = downloadCodes[0]
	}
	if msg.Content.FileName != "" {
		event.Extra["file_name"] = msg.Content.FileName
	}
	if msg.Content.Duration > 0 {
		event.Extra["media_duration"] = strconv.FormatInt(int64(msg.Content.Duration), 10)
	}
	if s.webhookSvc == nil {
		return errors.New("webhook service not configured")
	}

	if err := s.webhookSvc.ingressHandler(ctx).HandleIngressMessage(ctx, event); err != nil {
		logger.Errorf("[dingtalk] 入站消息处理失败 accountID=%d eventID=%s: %v", accountID, event.EventID, err)
		return err
	}
	if len(downloadCodes) > 0 {
		s.persistDingTalkMediaAsync(ctx, accountID, event.EventID, event.ConversationID,
			acc.AppKey, acc.AppSecret, msg.RobotCode, downloadCodes, msg.Content.FileName)
	}
	return nil
}

// dingTalkInboundMessage 机器人回调消息体（官方 open.dingtalk.com/document/development/robot-message-type）。
type dingTalkInboundMessage struct {
	MsgType        string          `json:"msgtype"`
	SenderID       string          `json:"senderId"`
	SenderStaffID  string          `json:"senderStaffId"`
	SenderNick     string          `json:"senderNick"`
	ConversationID string          `json:"conversationId"`
	MsgID          string          `json:"msgId"`
	CreateAt       dingTalkFlexInt `json:"createAt"`

	SessionWebhook            string `json:"sessionWebhook"`
	SessionWebhookExpiredTime int64  `json:"sessionWebhookExpiredTime"`
	RobotCode                 string `json:"robotCode"`

	// 会话与身份面（官方参数表；此前整表没读，钉钉回调里所有群聊都退化成单聊，N-22）。
	// 标量一律走宽松类型：整包 Unmarshal 是原子的，一个形态不符就让整条客户消息 400 丢掉且不重投。
	ConversationType  dingTalkFlexString `json:"conversationType"`
	ConversationTitle string             `json:"conversationTitle"` // 官方：群聊时才有的会话标题
	IsAdmin           dingTalkFlexBool   `json:"isAdmin"`
	IsInAtList        dingTalkFlexBool   `json:"isInAtList"`
	// 官方把四类媒体的字段统一挂在**顶层 content 对象**里（picture/audio/video/file 的
	// downloadCode、file 的 fileName、audio 的 recognition、richText 的逐项数组），
	// 不存在与 msgtype 同名的子对象——官方 python stream SDK 的
	// `ImageContent.from_dict(d['content'])` 是同一口径。M-01 以此为准。
	Content dingTalkInboundContent `json:"content"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

// dingTalkFlexInt 数字标量的两种官方写法都收：钉钉文档里 createAt/duration 一会儿是
// String（"1700000000000"）一会儿是 Long（1700000000000），真实回调两种都出现过。
// 这里必须兼容——整包 json.Unmarshal 是原子的，一个字段形态不符就让整条消息返回 400，
// 等于把客户消息丢掉，而丢掉的那条永远不会重投。
type dingTalkFlexInt int64

func (f *dingTalkFlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse numeric field %q: %w", s, err)
	}
	*f = dingTalkFlexInt(v)
	return nil
}

// dingTalkFlexString 与 dingTalkFlexInt 同一条教训的字符串侧：官方类型栏与实际下发对不上时，
// 两种写法都收，别让客户消息因为一次类型漂移被整包丢掉。
type dingTalkFlexString string

func (s *dingTalkFlexString) UnmarshalJSON(b []byte) error {
	v := strings.Trim(string(b), `"`)
	if v == "null" {
		v = ""
	}
	*s = dingTalkFlexString(v)
	return nil
}

// dingTalkFlexBool 三态布尔：未下发（Set=false）、下发 false、下发 true 三者可分辨
// —— 官方明写「机器人发布上线后生效，否则不返回」，把缺席当 false 会让 ops 误判成非管理员。
// 与 createAt 不同，这里解析不出形态时**不报错**：isAdmin/isInAtList 目前只做存档，
// 没有任何判定消费它们，为一枚旁证字段让整条客户消息 400 且不重投是反向的取舍。
type dingTalkFlexBool struct {
	Set bool
	Val bool
}

func (b *dingTalkFlexBool) UnmarshalJSON(raw []byte) error {
	s := strings.Trim(string(raw), `"`)
	switch s {
	case "true":
		*b = dingTalkFlexBool{Set: true, Val: true}
	case "false":
		*b = dingTalkFlexBool{Set: true, Val: false}
	default:
		*b = dingTalkFlexBool{}
	}
	return nil
}

type dingTalkInboundContent struct {
	Content      string          `json:"content"`
	DownloadCode string          `json:"downloadCode"`
	Recognition  string          `json:"recognition"`
	Duration     dingTalkFlexInt `json:"duration"`
	VideoType    string          `json:"videoType"`
	FileName     string          `json:"fileName"`
	FileType     string          `json:"fileType"`
	RichText     []struct {
		Text         string `json:"text"`
		DownloadCode string `json:"downloadCode"`
		Type         string `json:"type"`
	} `json:"richText"`
}

// dingTalkInboundBody 把官方 msgtype 归一化成 (中台 MsgType, 展示与 AI 可读的正文, 媒体下载码列表)。
// 占位符口径与 WhatsApp 一致（[图片]/[语音]/[视频]/[文件]），工作台和 AI 两侧不必各认一套。
// 语音例外：官方直接给 recognition（语音识别文本），有正文就用正文，别让 AI 只看到一个占位符。
// 第一个返回值恒在中台 msg_type 词表里（含未知类型的兜底），这条不变量由
// TestN17_DingTalkInboundBodyTypesInHubVocabulary 逐官方类型钉住。
func dingTalkInboundBody(msgtype string, c *dingTalkInboundContent, textBody string) (string, string, []string) {
	switch msgtype {
	case "", "text":
		content := c.Content
		if content == "" {
			content = textBody
		}
		return model.MsgTypeText, content, nil
	case "picture":
		return model.MsgTypeImage, "[图片]", c.downloadCodes()
	case "audio":
		content := c.Recognition
		if content == "" {
			content = "[语音]"
		}
		return model.MsgTypeAudio, content, c.downloadCodes()
	case "video":
		return model.MsgTypeVideo, "[视频]", c.downloadCodes()
	case "file":
		content := "[文件]"
		if c.FileName != "" {
			content = "[文件] " + c.FileName
		}
		return model.MsgTypeFile, content, c.downloadCodes()
	case "richText":
		var sb strings.Builder
		for _, item := range c.RichText {
			if item.Text != "" {
				sb.WriteString(item.Text)
				continue
			}
			if item.DownloadCode != "" {
				sb.WriteString("[图片]")
			}
		}
		content := sb.String()
		if content == "" {
			content = "[富文本]"
		}
		// 富文本的正文已逐项拼好（文字 + 每张图片一个占位符），类型按文本走 ——
		// 与飞书 post、中台词表同一口径。把官方名 "richText" 原样写进 msg_type，
		// 工作台按类型筛选（msg_type = ?）与 by_msg_type 统计永远看不到这一行。
		return model.MsgTypeText, content, c.downloadCodes()
	default:
		// 官方类型还会新增（未知的那个必然不在中台词表里）⇒ 宁缺不错：类型兜到 text，
		// 占位符仍回显官方名，让客服看得见"这里有条我们没承载的消息"。
		return InboundHubMsgType(msgtype), "[" + msgtype + "]", c.downloadCodes()
	}
}

// downloadCodes 汇总本条消息携带的全部下载码：富文本可以一次带多张图片，
// 只取首条会让其余几张过期作废（与 N-10 的"逐条媒体各转存一次"同一教训）。
// 按出现顺序去重：顶层与 content 各带一份、或同一张图被拆进 richText 两段时，
// 重复的码会让同一个文件转存两次，Extra.media_urls 里也就多出两条一样的 URL。
func (c *dingTalkInboundContent) downloadCodes() []string {
	codes := make([]string, 0, len(c.RichText)+1)
	seen := make(map[string]bool, len(c.RichText)+1)
	appendCode := func(code string) {
		if code == "" || seen[code] {
			return
		}
		seen[code] = true
		codes = append(codes, code)
	}
	appendCode(c.DownloadCode)
	for _, item := range c.RichText {
		appendCode(item.DownloadCode)
	}
	return codes
}

// dingTalkMessageTime 官方 createAt 是毫秒时间戳；缺失或不可解析才退回当前时间。
// 用 time.Now() 顶替会让延迟投递的消息排到会话最前面，也绕过中台的"历史消息"窗口判定。
func dingTalkMessageTime(createAt dingTalkFlexInt) time.Time {
	ms := int64(createAt)
	if ms <= 0 {
		return time.Now()
	}
	if ms < 1_000_000_000_000 {
		ms *= 1000
	}
	return time.UnixMilli(ms)
}

// dingTalkDedupKey 入站幂等键：官方 msgId 只在机器人明文回调里有；事件订阅
// （加密）载荷没带，直接拼空串会让 EventID 退化成常量 "dt-<account>-"，
// 后续所有事件被 message_hub 判重复并静默丢弃。缺官方键时退回载荷内容哈希。
func dingTalkDedupKey(payload []byte, msgID string) string {
	if msgID != "" {
		return msgID
	}
	sum := sha256.Sum256(payload)
	return "h-" + hex.EncodeToString(sum[:])[:16]
}

func dingTalkDecrypt(aesKey, cipherText string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(aesKey)
		if err != nil {
			return "", fmt.Errorf("decode aes key: %w", err)
		}
	}
	if len(key) > 32 {
		key = key[:32]
	} else if len(key) < 32 {
		padded := make([]byte, 32)
		copy(padded, key)
		key = padded
	}
	ct, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(ct)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext is not a multiple of block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:aes.BlockSize]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ct))
	mode.CryptBlocks(plain, ct)
	n := len(plain)
	if n == 0 {
		return "", errors.New("empty plaintext")
	}
	pad := int(plain[n-1])
	if pad < 1 || pad > aes.BlockSize || pad > n {
		return "", errors.New("invalid padding")
	}
	for _, b := range plain[n-pad:] {
		if int(b) != pad {
			return "", errors.New("invalid padding")
		}
	}
	body := plain[:n-pad]
	// 官方明文布局：random(16) + msg_len(4, 大端) + msg + receiveId。
	// 直接把整段交给 json 解析必然失败（真实事件订阅回调从未走通过）。
	if len(body) < 20 {
		return "", fmt.Errorf("dingtalk plaintext too short: %d", len(body))
	}
	msgLen := int(binary.BigEndian.Uint32(body[16:20]))
	if msgLen < 0 || 20+msgLen > len(body) {
		return "", fmt.Errorf("dingtalk plaintext msg_len %d out of range (payload %d bytes)", msgLen, len(body))
	}
	return string(body[20 : 20+msgLen]), nil
}

// verifyDingTalkRobotSign 校验企业内部机器人明文回调（header: timestamp + sign）。
func verifyDingTalkRobotSign(secret string, headers map[string]string) error {
	if secret == "" {
		if insecureWebhookAllowed() {
			logger.Warnf("[DingTalk] app_secret 未配置，ALLOW_INSECURE_WEBHOOK=true 已启用，跳过机器人回调验签")
			return nil
		}
		return errors.New("dingtalk app_secret 未配置，无法验签明文机器人回调；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true")
	}
	tsValue := dingTalkHeader(headers, "timestamp")
	sign := dingTalkHeader(headers, "sign")
	if tsValue == "" || sign == "" {
		return errors.New("missing timestamp/sign header")
	}
	ms, err := strconv.ParseInt(tsValue, 10, 64)
	if err != nil {
		return fmt.Errorf("dingtalk timestamp not epoch-millis: %w", err)
	}
	drift := time.Since(time.UnixMilli(ms))
	if drift < 0 {
		drift = -drift
	}
	if drift > dingTalkRobotCallbackWindow {
		return fmt.Errorf("dingtalk timestamp stale by %s（官方窗口 1 小时）", drift.Round(time.Second))
	}
	if subtle.ConstantTimeCompare([]byte(dingtalkSign(secret, ms)), []byte(sign)) != 1 {
		return errors.New("dingtalk robot callback signature mismatch")
	}
	return nil
}

// verifyDingTalkEventSignature 校验事件订阅加密回调（query: signature + timestamp + nonce）。
func verifyDingTalkEventSignature(token, encrypt string, query map[string]string) error {
	if token == "" {
		if insecureWebhookAllowed() {
			logger.Warnf("[DingTalk] token 未配置，ALLOW_INSECURE_WEBHOOK=true 已启用，跳过事件订阅回调验签")
			return nil
		}
		return errors.New("dingtalk 事件订阅 token 未配置，无法验签；开发放行需显式 ALLOW_INSECURE_WEBHOOK=true")
	}
	sig := dingTalkQueryValue(query, "signature", "msg_signature")
	ts := dingTalkQueryValue(query, "timestamp")
	nonce := dingTalkQueryValue(query, "nonce")
	if sig == "" || ts == "" || nonce == "" {
		return errors.New("missing signature/timestamp/nonce query params")
	}
	parts := []string{token, ts, nonce, encrypt}
	sortStrings(parts)
	if subtle.ConstantTimeCompare([]byte(sha1Hex([]byte(strings.Join(parts, "")))), []byte(sig)) != 1 {
		return errors.New("dingtalk event signature mismatch")
	}
	return nil
}

func dingTalkHeader(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func dingTalkQueryValue(query map[string]string, names ...string) string {
	for _, name := range names {
		for k, v := range query {
			if strings.EqualFold(k, name) && v != "" {
				return v
			}
		}
	}
	return ""
}
