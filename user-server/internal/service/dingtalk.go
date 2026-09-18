package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"context"
	"hivemtk-user/internal/pkg/httpclient"
)

var dingtalkRobotBase = "https://oapi.dingtalk.com/robot/send"

// DingTalkService 钉钉群机器人出站服务
type DingTalkService struct {
	client *http.Client
}

// NewDingTalkService 创建钉钉服务（client 带超时 + 连接池，复用全局 httpclient）
func NewDingTalkService() *DingTalkService {
	return &DingTalkService{client: httpclient.NewWithTimeout(15 * time.Second)}
}

// SendRobot 通过钉钉自定义机器人 webhook 发送消息。
//
//	webhookOrToken: 完整 webhook URL（含 access_token），或仅 access_token（自动拼接 base）
//	secret:         机器人「加签」密钥（可选；为空不签名）。允许通过 `webhook|secret` 形式内联
//	msgType:        text / markdown / link / action_card（默认 text）
//	content:        消息内容（text/markdown/action_card 为文本；link 为 JSON 字符串）
func (s *DingTalkService) SendRobot(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error) {
	if webhookOrToken == "" {
		return "", fmt.Errorf("dingtalk: webhook/access_token required")
	}
	webhook := webhookOrToken
	if i := strings.Index(webhookOrToken, "|"); i >= 0 {
		webhook = webhookOrToken[:i]
		secret = webhookOrToken[i+1:]
	}

	u := webhook
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = dingtalkRobotBase + "?access_token=" + url.QueryEscape(webhook)
	}
	if secret != "" {
		ts := time.Now().UnixMilli()
		sign := dingtalkSign(secret, ts)
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u = fmt.Sprintf("%s%sts=%d&sign=%s", u, sep, ts, url.QueryEscape(sign))
	}

	mt := msgType
	if mt == "" {
		mt = "text"
	}

	var payload []byte
	var err error
	switch mt {
	case "text":
		payload, err = json.Marshal(map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": content},
		})
	case "markdown":
		payload, err = json.Marshal(map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]string{"title": "消息", "text": content},
		})
	default:
		payload, err = json.Marshal(map[string]any{
			"msgtype": mt,
			mt:        json.RawMessage(content),
		})
	}
	if err != nil {
		return "", fmt.Errorf("dingtalk: marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("dingtalk: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("dingtalk: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("dingtalk: decode response: %w", err)
	}
	if out.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk: api errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
	}
	return fmt.Sprintf("dingtalk-%d", time.Now().UnixNano()), nil
}

func dingtalkSign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
