package service

import (
	"bytes"
	"context"

	"crypto/aes"

	"crypto/cipher"

	"crypto/subtle"

	"encoding/base64"

	"encoding/binary"

	"encoding/json"

	"encoding/xml"

	"errors"

	"fmt"

	"strconv"

	"strings"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// wecomCallbackWindow 回调 timestamp 的可接受偏差。
// 本地策略：官方只声明 nonce 在两小时内唯一，未直接给出时间戳校验规则，
// 这里按同一口径收口，避免旧回调被无限期重放（审计 S-05）。
const wecomCallbackWindow = 2 * time.Hour

// wecomXMLMaxDepth 是 <xml> → map 的递归上限。
// 必须有上限的理由不是"整洁"：外壳解析发生在**验签之前**（要先取出 encrypt 才算得出
// 签名第四段），输入完全由请求方控制；body 有 2 MiB 硬上限（MaxWebhookBody），
// 但 encoding/xml 自身不设深度限制 —— 全用 `<a>` 嵌套就是几十万层递归，
// 一个未验签的请求即可把进程栈打爆（预认证 DoS）。
// 取值只要远小于"2 MiB 能塞下的层数"就起到防护作用，所以这里不贴着已知最浅形状取值：
// 外壳只用得到一层子元素（<xml><Encrypt/>），解密明文的已知形状是两层
// （<xml><Image><MediaId/>），16 层是给更深的业务结构留余量，多深的构造都不必递归到底。
const wecomXMLMaxDepth = 16

// wecomFlatXMLMap 把官方 <xml> 回调壳/解密明文解成与 JSON 同构的 map，
// 键取官方 tag 名（ToUserName / Encrypt / MsgId / Content …），CDATA 自动展开。
//
// 存在这个函数的理由（审计 N-08）：企微入站此前在**四处独立环节**各自写死 JSON
// （验签取 encrypt、Receive 的 ParsePayload、officialEventID 取 MsgId、解密后取明文），
// 带 <Encrypt/> 外壳的回调形态在第一步就 400。企微页原文未取到可引用出处（文档站是
// SPA，只有同族的微信开放平台《消息加解密》给出该外壳，见 §6），所以修复不声明
// "哪一种才是官方形态"，而是两种都收 —— 任一边判断错都不会失联。
//
// 解析失败、或超过深度上限，一律返回 nil（调用方按"不是 XML"处理），绝不 panic。
func wecomFlatXMLMap(raw []byte) map[string]any {
	if !bytes.Contains(raw, []byte("<xml")) {
		return nil
	}
	p := &wecomXMLParser{dec: xml.NewDecoder(bytes.NewReader(raw))}
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "xml" {
			m, _ := p.element(1).(map[string]any)
			if p.tooDeep || len(m) == 0 {
				return nil
			}
			return m
		}
	}
}

// wecomXMLParser 是一个带深度上限的递归下降。
type wecomXMLParser struct {
	dec     *xml.Decoder
	tooDeep bool
}

// element 从当前游标读到所属元素的闭合标签为止（调用方已经消费掉开标签）。
// 纯文本元素 → string；含子元素的 → 子 map（企微图片消息就是
// <Img><MediaId>…</MediaId><PicUrl>…</PicUrl></Img> 这种两层结构，
// 子元素的**文本**必须一起解出来，否则 MediaId 会解成空 map 而丢掉）。
func (p *wecomXMLParser) element(depth int) any {
	if depth > wecomXMLMaxDepth {
		p.tooDeep = true
		return nil
	}
	var text strings.Builder
	var child map[string]any
	for {
		tok, err := p.dec.Token()
		if err != nil {
			if child != nil {
				return child
			}
			return strings.TrimSpace(text.String())
		}
		switch t := tok.(type) {
		case xml.CharData:
			text.Write(t)
		case xml.StartElement:
			if child == nil {
				child = map[string]any{}
			}
			child[t.Name.Local] = p.element(depth + 1)
			if p.tooDeep {
				return nil
			}
		case xml.EndElement:
			if child != nil {
				return child
			}
			return strings.TrimSpace(text.String())
		}
	}
}

// wecomMediaContainers 是携带 media_id 的消息在 XML 形态下的子对象名。
// 官方回调页原文未取到（§6），所以这里和 N-08 同口径：不声明哪一种是官方形态，
// 顶层摊平（本仓历史 JSON 形态）与子对象嵌套（同族公众号/企微媒体消息的 XML 形态）都读。
var wecomMediaContainers = []string{"Image", "image", "Voice", "voice", "Video", "video", "File", "file", "ShortVideo", "shortvideo"}

// wecomLinkContainers 是链接消息的子对象名（同上一条的理由：官方页未取到，两种形态都读）。
var wecomLinkContainers = []string{"Link", "link"}

// wecomSubString 在「顶层 + 指定子对象」里找第一个非空键。
// 必须查两层的理由（审计 N-09）：深度上限修好之后 <Image> 确实解成了子 map，
// 但取值仍写在顶层 —— 嵌套形态的 media_id 解得出来却没人去取，
// 结果和丢掉一样（图片/语音/文件消息永远拿不到媒体，M-01 的企微那条）。
func wecomSubString(plain map[string]any, containers []string, keys ...string) string {
	if v := getString(plain, keys...); v != "" {
		return v
	}
	for _, name := range containers {
		if sub, ok := plain[name].(map[string]any); ok {
			if v := getString(sub, keys...); v != "" {
				return v
			}
		}
	}
	return ""
}

// wecomEnvelopeMap 统一两种回调外壳：JSON 直接用，XML 解一层。
// 返回 nil 表示两种都不成，调用方按解析失败处理。
func wecomEnvelopeMap(body []byte) map[string]any {
	var j map[string]any
	if err := json.Unmarshal(body, &j); err == nil {
		return j
	}
	return wecomFlatXMLMap(body)
}

func verifyWeCom(token, aesKey string, body []byte, query map[string]string) (bool, error) {
	if token == "" {
		return false, errors.New("missing token")
	}

	// N-08：外壳不再限定 JSON —— 官方回调是 <xml>，本仓历史用例是 JSON，两种都取得出四段。
	env := wecomEnvelopeMap(body)
	msgSignature := getString(env, "msg_signature", "MsgSignature")
	timestamp := getString(env, "timestamp", "TimeStamp")
	nonce := getString(env, "nonce", "Nonce")
	if query != nil {
		if msgSignature == "" {
			msgSignature = query["msg_signature"]
			timestamp = query["timestamp"]
			nonce = query["nonce"]
		}
		if timestamp == "" {
			timestamp = query["timestamp"]
		}
		if nonce == "" {
			nonce = query["nonce"]
		}
	}
	if msgSignature == "" || timestamp == "" || nonce == "" {
		return false, errors.New("missing msg_signature/timestamp/nonce")
	}
	secs, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid timestamp %q", timestamp)
	}
	if drift := time.Since(time.Unix(secs, 0)); drift > wecomCallbackWindow || drift < -wecomCallbackWindow {
		return false, fmt.Errorf("timestamp %q outside %s window", timestamp, wecomCallbackWindow)
	}

	fourth := getString(env, "encrypt", "Encrypt")
	if fourth == "" && query != nil {
		fourth = query["echostr"]
	}
	parts := []string{token, timestamp, nonce, fourth}
	sortStrings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	if subtle.ConstantTimeCompare([]byte(h), []byte(msgSignature)) != 1 {
		return false, errors.New("signature mismatch")
	}
	return true, nil
}

func verifyWechat(token string, body []byte, headers map[string]string) bool {
	if token == "" {
		return false
	}
	ts := headers["X-Wechat-Timestamp"]
	nonce := headers["X-Wechat-Nonce"]
	sig := headers["X-Wechat-Signature"]
	if sig == "" {
		sig = headers["signature"]
	}
	if ts == "" || nonce == "" || sig == "" {
		return false
	}

	parts := []string{token, ts, nonce}
	sortStrings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	return subtle.ConstantTimeCompare([]byte(h), []byte(sig)) == 1
}

func DecryptWeComMessage(aesKey, encrypted string) ([]byte, error) {
	if len(aesKey) != 43 {
		return nil, fmt.Errorf("invalid EncodingAESKey length: %d", len(aesKey))
	}

	key := aesKey + "="
	keyB, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	cipherB, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decode cipher: %w", err)
	}
	if len(cipherB) < 32 || len(cipherB)%16 != 0 {
		return nil, fmt.Errorf("invalid cipher length: %d", len(cipherB))
	}
	block, err := aes.NewCipher(keyB)
	if err != nil {
		return nil, err
	}
	// 官方方案：IV 取 AESKey 前 16 字节，密文整体解密（密文首块不是 IV，
	// 而是 random(16) 那段明文的密文，跳过它会连带把长度域读偏 16 字节）。
	iv := keyB[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(cipherB))
	mode.CryptBlocks(plain, cipherB)

	plen := int(plain[len(plain)-1])
	if plen < 1 || plen > 32 {
		return nil, fmt.Errorf("invalid padding: %d", plen)
	}
	plain = plain[:len(plain)-plen]

	if len(plain) < 20 {
		return nil, fmt.Errorf("plain too short: %d", len(plain))
	}
	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if 20+msgLen > len(plain) {
		return nil, fmt.Errorf("msg_len overflow: %d", msgLen)
	}
	return plain[20 : 20+msgLen], nil
}

func VerifyURL(token, aesKey, msgSignature, timestamp, nonce, echostr string) (string, error) {
	if len(aesKey) != 43 {
		return "", errors.New("invalid EncodingAESKey")
	}

	plain, err := DecryptWeComMessage(aesKey, echostr)
	if err != nil {
		return "", fmt.Errorf("decrypt echostr: %w", err)
	}
	// DecryptWeComMessage 已按官方布局剥掉 random(16)+msg_len(4) 头，
	// 这里拿到的就是 echostr 明文，不能再剥一次偏移。
	plainStr := strings.TrimRight(string(plain), "\x00")

	parts := []string{token, timestamp, nonce, plainStr}
	sortStrings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	if subtle.ConstantTimeCompare([]byte(h), []byte(msgSignature)) != 1 {
		return "", errors.New("signature mismatch")
	}

	return plainStr, nil
}

func (s *WebhookService) dispatchWeCom(ctx context.Context, accountID string, p *ParsedPayload, raw []byte, headers map[string]string) (*model.MessageHub, error) {
	if s.integration == nil {
		return nil, nil
	}

	plain := s.parseWeComPlain(ctx, accountID, raw)
	if plain == nil {

		plain = p.Extra
	}
	if plain == nil {
		return nil, fmt.Errorf("wecom plain nil")
	}

	fromUser := getString(plain, "FromUserName", "from")
	fromName := getString(plain, "FromUserName")
	msgType := strings.ToLower(getString(plain, "MsgType", "msg_type"))
	content := getString(plain, "Content", "content", "Text", "text")
	if content == "" {

		switch msgType {
		case "image":
			content = "[图片]"
		case "voice":
			content = "[语音]"
		case "video":
			content = "[视频]"
		case "shortvideo":
			content = "[小视频]"
		case "file":
			content = "[文件]"
		case "location":
			content = "[位置]"
		case "link":
			content = strings.TrimSpace(wecomSubString(plain, wecomLinkContainers, "Title", "title") + " " +
				wecomSubString(plain, wecomLinkContainers, "Url", "url"))
		default:
			content = getString(plain, "Content", "content", "Text", "text")
		}
		if content == "" {
			// 落到这里说明正文在**嵌套子对象**里而顶层没有（或该类型本就没正文）。
			// 不能留空串：D-03/N-07 定性过，空正文照样会落一条"看得见、永远没人回复"的收件箱消息。
			content = "[" + msgType + "]"
		}
	}
	mediaID := wecomSubString(plain, wecomMediaContainers, "MediaId", "media_id")
	chatID := getString(plain, "ChatId", "chat_id")
	chatType := getString(plain, "ChatType", "chat_type")
	event := getString(plain, "Event", "event")
	msgID := getString(plain, "MsgId", "msg_id")

	if msgType == "event" {
		logger.Infof("[Webhook] wecom event=%s from=%s", event, fromUser)
		return nil, nil
	}

	var accID uint64
	if v, err := strconv.ParseUint(accountID, 10, 64); err == nil && v > 0 {
		accID = v
	} else {

		acc, gerr := s.wecomRepo.GetByMerchant(ctx)
		if gerr != nil || len(acc) == 0 {
			return nil, fmt.Errorf("invalid account_id")
		}
		accID = uint64(acc[0].ID)
	}

	hubMsg, _, err := s.integration.ReceiveCallback(ctx, &ReceiveCallbackRequest{
		AccountID: uint(accID),
		FromUser:  fromUser,
		FromName:  fromName,
		// 这条路径经 hub.Push → Normalize 硬校验词表：官方类型 voice 不在词表里
		// （中台叫 audio），传原值等于让整条语音消息被 ErrMessageHubInvalidMsgType 拒掉、
		// dispatch 上抛 —— 客户的话不是类型标错，是整条蒸发。媒体判断仍用官方 msgType。
		MsgType:  InboundHubMsgType(msgType),
		Content:  content,
		MsgID:    msgID,
		MediaID:  mediaID,
		ChatID:   chatID,
		ChatType: chatType,
	})

	if err != nil {
		// 审计 N-08 全路径用例实测到的：同一官方 MsgId 的重投在 hub 层收敛时，
		// Push 返回的是 ErrMessageHubIdempotent **错误**而不是已存在的那一行。
		// 这里必须就地吞掉并返回 (nil, nil)：handleJob 只有在
		// hubMsg==nil && dispatchErr==nil 时才走 markProcessed；错误继续上抛会
		// 落到下面那条 unified_message —— 而密文外壳里取不到 Content，
		// 等于写一条空内容消息进收件箱，正是 D-03 定性的「看得到、永远没人回复」。
		// 与飞书/抖音/Telegram 的 dispatch 同一口径（那三处也按 duplicate 不报错处理）。
		if errors.Is(err, ErrMessageHubIdempotent) {
			logger.Infof("[Webhook] wecom 重投已在 hub 层收敛 msg_id=%s account=%s", msgID, accountID)
			return nil, nil
		}
		return nil, err
	}

	p.Content = content
	p.Sender = fromUser
	p.ChatID = chatID

	if hubMsg != nil && content != "" && fromUser != "" && msgType != "event" {
		MineUnifiedLead(ctx, s, hubMsg, WeComLeadAdapter{}, accountID, chatID, "", fromUser, fromName, "", content)
	}
	// 媒体消息转存（best-effort）：企微 media_id 仅 3 天有效，异步下载回填长期 URL
	if mediaID != "" && hubMsg != nil && isWeComMediaMsgType(msgType) {
		s.persistWeComMediaAsync(ctx, accountID, hubMsg.MsgID, mediaID, msgType)
	}
	return hubMsg, nil
}

// isWeComMediaMsgType 企微携带 MediaId 的消息类型。
func isWeComMediaMsgType(msgType string) bool {
	switch msgType {
	case "image", "voice", "video", "shortvideo", "file":
		return true
	}
	return false
}

// wecomMediaFetchFn / wecomMediaStoreFn / wecomTokenFn 是企微媒体链路的三条外部 IO 腿，
// 以函数变量注入（与飞书 /  WhatsApp / QQ 同一手法）：取 access_token 与下载素材都要访问
// qyapi.weixin.qq.com，转存要经存储驱动，测试环境三者全不可达 ⇒ 没有替身就只能测到"类型标对了"
// 这一半，测不到"客户发的语音真的落库并带上原件"这一半。
var (
	wecomTokenFn = func(ctx context.Context, s *WebhookService, accID uint) (string, error) {
		return s.wecomAccessToken(ctx, accID)
	}
	wecomMediaFetchFn = FetchWeComMedia
	wecomMediaStoreFn = channelMediaPersist
)

// persistWeComMediaAsync 异步下载企微媒体并转存，按 msg_id 回填 message_hub.media_url。
func (s *WebhookService) persistWeComMediaAsync(ctx context.Context, accountID, msgID, mediaID, msgType string) {
	if s.wecomRepo == nil {
		return
	}
	utils.SafeGo(ctx, "wecom.media_persist", func(gctx context.Context) {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID == 0 {
			if accs, gerr := s.wecomRepo.GetByMerchant(gctx); gerr == nil && len(accs) > 0 {
				accID = uint64(accs[0].ID)
			}
		}
		if accID == 0 {
			return
		}
		token, terr := wecomTokenFn(gctx, s, uint(accID))
		if terr != nil || token == "" {
			logger.Ctx(gctx).Warn().Err(terr).Str("account_id", accountID).Msg("[WeCom] 媒体转存跳过：access_token 获取失败")
			return
		}
		rc, contentType, derr := wecomMediaFetchFn(gctx, token, mediaID)
		if derr != nil {
			logger.Ctx(gctx).Warn().Err(derr).Str("media_id", mediaID).Msg("[WeCom] 媒体下载失败（占位符保留）")
			return
		}
		defer func() { _ = rc.Close() }()
		data, rerr := readInboundMedia(rc, maxInboundMediaBytes)
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("media_id", mediaID).Msg("[WeCom] 媒体读取失败（占位符保留）")
			return
		}
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = wecomDefaultContentType(msgType)
		}
		publicURL, serr := wecomMediaStoreFn(gctx, "wecom", mediaID, data, contentType, "")
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[WeCom] 媒体转存失败")
			return
		}
		if s.messageHubRepo == nil {
			s.ensureReposFromDB(gctx)
		}
		EnrichHubMediaURLByMsgID(gctx, s.messageHubRepo, "wecom", accountID, msgID, publicURL)
		logger.Ctx(gctx).Info().Str("media_id", mediaID).Str("url", publicURL).Msg("[WeCom] 媒体已转存")
	})
}

// wecomDefaultContentType 企微 media/get 不回 Content-Type 时按 MsgType 推断。
func wecomDefaultContentType(msgType string) string {
	switch msgType {
	case "image":
		return "image/jpeg"
	case "voice":
		return "audio/amr"
	case "video", "shortvideo":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// wecomAccessToken 取企微 access_token（复用 WeComService 缓存逻辑）。
func (s *WebhookService) wecomAccessToken(ctx context.Context, accountID uint) (string, error) {
	acc, err := s.wecomRepo.GetByID(ctx, accountID)
	if err != nil || acc == nil {
		return "", fmt.Errorf("wecom account %d not found", accountID)
	}
	svc := NewWeComServiceWithDB(s.lazyDB())
	return svc.GetAccessToken(ctx, acc)
}

func (s *WebhookService) parseWeComPlain(ctx context.Context, accountID string, raw []byte) map[string]any {
	// N-08：外壳 JSON / <xml> 都认，只取外层 encrypt；两种都没有 encrypt 时按明文外壳直接用。
	p := wecomEnvelopeMap(raw)
	if p == nil {
		return nil
	}
	enc := getString(p, "encrypt", "Encrypt")
	if enc == "" {
		return p
	}

	if s.wecomRepo == nil {
		return nil
	}

	if id, err := strconv.ParseUint(accountID, 10, 64); err == nil && id > 0 {
		if acc, gerr := s.wecomRepo.GetByID(ctx, uint(id)); gerr == nil && acc != nil && acc.EncodingAESKey != "" {
			if out := decryptWeComPayload(acc.EncodingAESKey, enc); out != nil {
				if validateWeComAgentID(out, acc.AgentID) {
					return out
				}
				logger.Warnf("[Webhook] wecom AgentID 校验失败(精确路由) account=%d payload_agent=%v expected=%d，回退全量遍历",
					id, out["AgentID"], acc.AgentID)
			} else {
				logger.Warnf("[Webhook] wecom 解密失败(精确路由) account=%d，回退全量遍历", id)
			}
		}
	} else {
		logger.Warnf("[Webhook] wecom 解密缺少有效 account 标识 account=%q，回退全量遍历", accountID)
	}

	accs, err := s.wecomRepo.GetByMerchant(ctx)
	if err != nil || len(accs) == 0 {
		return nil
	}
	for _, a := range accs {
		if a.EncodingAESKey == "" {
			continue
		}
		if out := decryptWeComPayload(a.EncodingAESKey, enc); out != nil {
			if validateWeComAgentID(out, a.AgentID) {
				return out
			}
		}
	}
	logger.Warnf("[Webhook] wecom 全量遍历未能匹配到有效账号 account_id=%s", accountID)
	return nil
}

func decryptWeComPayload(aesKey, enc string) map[string]any {
	if aesKey == "" {
		return nil
	}
	plain, err := DecryptWeComMessage(aesKey, enc)
	if err != nil {
		return nil
	}
	// N-08：解密后的明文消息体官方形态是 <xml>，本仓历史形态是 JSON，两种都收。
	var out map[string]any
	if err := json.Unmarshal(plain, &out); err == nil {
		return out
	}
	return wecomFlatXMLMap(plain)
}

func validateWeComAgentID(payload map[string]any, expected int) bool {
	if payload == nil || expected == 0 {
		return true
	}
	raw, ok := payload["AgentID"]
	if !ok {
		return true
	}
	var got int
	switch v := raw.(type) {
	case float64:
		got = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			got = n
		} else {
			return true
		}
	default:
		return true
	}
	return got == expected
}

func (s *WebhookService) getWechatSecrets(ctx context.Context, accountID string) (string, string) {
	if s.wechatIntegration == nil {
		if s.lazyDB() == nil {
			return "", ""
		}
		s.wechatIntegration = NewWechatService(s.lazyDB())
	}
	var acc *model.WechatAccount
	if id, err := strconv.ParseUint(accountID, 10, 64); err == nil && id > 0 {
		if a, gerr := s.wechatIntegration.GetAccount(ctx, uint(id)); gerr == nil {
			acc = a
		}
	}
	if acc == nil || acc.Token == "" {
		if a, gerr := s.wechatIntegration.GetFirstActiveAccount(ctx); gerr == nil {
			acc = a
		}
	}
	if acc == nil {
		return "", ""
	}
	return acc.Token, acc.EncodingAESKey
}

// GetWeComSecrets 公开方法：供 controller 层 URL 验证使用
func (s *WebhookService) GetWeComSecrets(ctx context.Context, accountID string) (string, string, error) {
	return s.getWeComSecrets(ctx, accountID)
}

func (s *WebhookService) getWeComSecrets(ctx context.Context, accountID string) (string, string, error) {
	if s.wecomRepo == nil {
		return "", "", errors.New("wecomRepo nil")
	}
	if s.lazyDB() == nil {
		return "", "", nil
	}

	if id, err := strconv.ParseUint(accountID, 10, 64); err == nil && id > 0 {
		acc, err := s.wecomRepo.GetByID(ctx, uint(id))
		if err == nil && acc != nil {
			return acc.CallbackToken, acc.EncodingAESKey, nil
		}
	}

	accs, err := s.wecomRepo.GetByMerchant(ctx)
	if err != nil {
		return "", "", err
	}
	for _, a := range accs {
		if a.WebhookEnabled {
			return a.CallbackToken, a.EncodingAESKey, nil
		}
	}
	if len(accs) > 0 {
		return accs[0].CallbackToken, accs[0].EncodingAESKey, nil
	}
	return "", "", errors.New("wecom account not found")
}
