package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/config"
)

// ErrQQAccountNotFound QQ 账号不存在（verify-callback 自检入口）
var ErrQQAccountNotFound = errors.New("qq account not found")

// QQWebhookURLPathPrefix 是本系统接收 QQ 平台推送的标准路径前缀。
// 所有合法的 QQ webhook URL 必须形如：https://host/api/webhook/qq/{id}
// → 前缀校验可拦截误填（如填了 /webhook/qq-bot 等），避免平台侧验证永远失败。
const QQWebhookURLPathPrefix = "/api/webhook/qq/"

// QQWebhookAllowedPorts QQ 官方回调地址端口白名单（官方文档：80、443、8080、8443）。
var QQWebhookAllowedPorts = map[int]bool{80: true, 443: true, 8080: true, 8443: true}

// ValidateQQWebhookURL 校验 webhook URL 是否符合 QQ 官方 + 本系统要求
//
// 校验规则（对照官方 event-emit 文档，比 Telegram 多端口白名单）：
//  1. URL 必须可被 net/url 解析
//  2. scheme 必须为 https（官方要求 HTTPS 回调地址）
//  3. host 必须非空（含端口时端口必须在 QQWebhookAllowedPorts 白名单内）
//  4. path 必须以 /api/webhook/qq/ 开头（确保平台推送能被本系统路由到对应 controller）
//
// 仅本地静态校验，不发起网络请求；secret 正确性由平台真实 op13 验证确认。
func ValidateQQWebhookURL(raw string) error {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fmt.Errorf("webhook URL 为空")
	}
	u, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("webhook URL 解析失败: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("webhook URL scheme 必须是 https，当前=%q（QQ 官方要求 HTTPS 回调地址）", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook URL 缺少 host（如 https://chat.example.com）")
	}
	if port := u.Port(); port != "" {
		p, perr := strconv.Atoi(port)
		if perr != nil || !QQWebhookAllowedPorts[p] {
			return fmt.Errorf("webhook URL 端口 %q 不在 QQ 官方白名单（80/443/8080/8443）内", port)
		}
	}
	if !strings.HasPrefix(u.Path, QQWebhookURLPathPrefix) {
		return fmt.Errorf("webhook URL path 必须以 %q 开头，当前=%q", QQWebhookURLPathPrefix, u.Path)
	}
	return nil
}

// SuggestQQWebhookURL 从 PUBLIC_BASE_URL 推导 QQ webhook 回调地址。
// 未配置公网 base 时返回空串（调用方降级为手填 + 校验兜底）。
func SuggestQQWebhookURL(accountID uint) string {
	return SuggestQQWebhookURLFromBase(config.GetPublicBaseURL(), accountID)
}

// SuggestQQWebhookURLFromBase 指定 base 的推导（便于测试与 controller 复用）。
func SuggestQQWebhookURLFromBase(publicBase string, accountID uint) string {
	if strings.TrimSpace(publicBase) == "" {
		return ""
	}
	return strings.TrimRight(publicBase, "/") + QQWebhookURLPathPrefix + strconv.FormatUint(uint64(accountID), 10)
}

// QQCallbackSelfCheckResult 本地 op13 自检结果。
// plain_token/signature 与平台 op13 应答体字段名一致，可与官方测试工具对照。
type QQCallbackSelfCheckResult struct {
	PlainToken string `json:"plain_token"`
	EventTS    string `json:"event_ts"`
	Signature  string `json:"signature"`
}

// VerifyCallbackSelfCheck 本地 op13 验签自检。
//
// 能力边界：本地自检只能证明「签名链路连通 + secret 非空」——DerivePrivateKey
// 对任意非空 secret 都能成功签名，secret 是否填对只能由 q.qq.com 保存回调时
// 平台发起的真实 op13 验证确认。调用方文案不得宣称"自检通过 = secret 正确"。
func (s *QQService) VerifyCallbackSelfCheck(ctx context.Context, accountID uint) (*QQCallbackSelfCheckResult, error) {
	acc, err := s.accRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get qq account: %w", err)
	}
	if acc == nil {
		return nil, ErrQQAccountNotFound
	}
	if acc.WebhookSecret == "" {
		return nil, errors.New("webhook_secret 未配置：请先填写 q.qq.com 开放平台下发的 BotSecret")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("gen plain_token: %w", err)
	}
	plainToken := hex.EncodeToString(b)
	eventTS := strconv.FormatInt(time.Now().Unix(), 10)
	sig, err := qq.GenerateCallbackTestSignature(acc.WebhookSecret, eventTS, plainToken)
	if err != nil {
		return nil, fmt.Errorf("sign op13 challenge: %w", err)
	}
	return &QQCallbackSelfCheckResult{PlainToken: plainToken, EventTS: eventTS, Signature: sig}, nil
}
