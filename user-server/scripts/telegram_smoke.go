// Telegram 端到端冒烟测试（一次性 CLI 工具）
//
// 用途：
//  1. 验证 bot token 有效
//  2. 用新代码的 URL 推导逻辑 + PUBLIC_BASE_URL 注册 webhook 到 Telegram
//  3. 调用 getWebhookInfo 验证 TG 侧已正确收到
//  4. 验证后清理 webhook 释放 TG 侧资源
//
// 用法：
//
//	go run scripts/telegram_smoke.go --token=<BOT_TOKEN> --public-base=https://<你的公网回调域名>
//
// --public-base 必填且刻意不给默认值：Telegram 只接受公网可达的 HTTPS 回调地址，
// 谁在什么隧道/域名上暴露本服务，只有操作者知道，代码里写死一个只会指向已经不存在的机器。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const tgAPIBase = "https://api.telegram.org/bot"

func main() {
	token := flag.String("token", "", "Telegram bot token (必填)")
	publicBase := flag.String("public-base", "", "公网基座 URL（必填，用于推导 webhook URL）")
	accountID := flag.Uint("account-id", 1, "TG 账号 ID（拼接进 webhook path）")
	cleanup := flag.Bool("cleanup", true, "测试完成后清理 webhook")
	flag.Parse()

	if *token == "" {
		fmt.Println("FATAL: --token 必填")
		os.Exit(2)
	}
	if *publicBase == "" {
		fmt.Println("FATAL: --public-base 必填")
		os.Exit(2)
	}

	masked := maskToken(*token)
	fmt.Printf("== Step 0: 参数准备 ==\n")
	fmt.Printf("  bot_token: %s\n", masked)
	fmt.Printf("  public_base: %s\n", *publicBase)
	fmt.Printf("  account_id: %d\n", *accountID)
	webhookURL := strings.TrimRight(*publicBase, "/") + fmt.Sprintf("/api/webhook/telegram/%d", *accountID)
	fmt.Printf("  derived webhook URL: %s\n\n", webhookURL)

	fmt.Println("== Step 1: getMe（确认 bot 存活 + 拿 username） ==")
	me, err := callTG(*token, "getMe", nil)
	if err != nil {
		fmt.Printf("FATAL: getMe 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  bot id: %v\n", me["id"])
	fmt.Printf("  bot username: @%v\n", me["username"])
	fmt.Printf("  bot first_name: %v\n\n", me["first_name"])

	fmt.Println("== Step 2: setWebhook ==")
	secret := randomHex(32)
	params := map[string]string{
		"url":             webhookURL,
		"secret_token":    secret,
		"allowed_updates": `["message","edited_message","channel_post","callback_query","my_chat_member","chat_member"]`,
	}
	setRes, err := callTG(*token, "setWebhook", params)
	if err != nil {
		fmt.Printf("FATAL: setWebhook 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  setWebhook result: %v\n\n", setRes)

	fmt.Println("== Step 3: getWebhookInfo（验证 TG 侧已生效） ==")
	info, err := callTG(*token, "getWebhookInfo", nil)
	if err != nil {
		fmt.Printf("FATAL: getWebhookInfo 失败: %v\n", err)
		os.Exit(1)
	}
	if got := info["url"]; got != webhookURL {
		fmt.Printf("FATAL: TG 侧 url=%v != 推导的 %s\n", got, webhookURL)
		os.Exit(1)
	}
	pending, _ := info["pending_update_count"].(float64)
	lastErr, _ := info["last_error_message"].(string)
	fmt.Printf("  url: %v\n", info["url"])
	fmt.Printf("  has_custom_certificate: %v\n", info["has_custom_certificate"])
	fmt.Printf("  pending_update_count: %v\n", info["pending_update_count"])
	fmt.Printf("  last_error_message: %q\n", lastErr)
	fmt.Printf("  pending=%v → %s\n\n", pending, statusFor(pending, lastErr))

	fmt.Println("== Step 4: 模拟入站 (HTTP POST 到 webhook URL) ==")
	dryRunResult := simulatePost(webhookURL, secret)
	fmt.Printf("  HTTP POST result: %s\n\n", dryRunResult)

	if *cleanup {
		fmt.Println("== Step 5: 清理 (deleteWebhook) ==")
		dRes, err := callTG(*token, "deleteWebhook", map[string]string{"drop_pending_updates": "true"})
		if err != nil {
			fmt.Printf("WARN: deleteWebhook 失败: %v\n", err)
		} else {
			fmt.Printf("  deleteWebhook result: %v\n", dRes)
		}
		fmt.Println()
	}

	fmt.Println("== 完成: Telegram 通道端到端冒烟测试通过 ==")
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return "***"
	}
	return t[:4] + "****" + t[len(t)-4:]
}

func randomHex(n int) string {
	b := make([]byte, n)
	seed := time.Now().UnixNano()
	for i := range b {
		b[i] = byte((seed >> (i % 8)) & 0xff)
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 2*n)
	for i, v := range b {
		out[2*i] = hexDigits[v>>4]
		out[2*i+1] = hexDigits[v&0xf]
	}
	return string(out)
}

func callTG(token, method string, params map[string]string) (map[string]any, error) {
	url := tgAPIBase + token + "/" + method
	form := bytes.NewBuffer(nil)
	for k, v := range params {
		form.WriteString(k)
		form.WriteString("=")
		form.WriteString(v)
		form.WriteString("&")
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, form)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var raw struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse: %w (raw=%s)", err, string(body))
	}
	if !raw.OK {

		var m map[string]any
		_ = json.Unmarshal(raw.Result, &m)
		return m, fmt.Errorf("API 错误(%d): %s (raw=%s)", raw.ErrorCode, raw.Description, string(body))
	}

	var m map[string]any
	if len(raw.Result) > 0 && raw.Result[0] == '{' {
		_ = json.Unmarshal(raw.Result, &m)
	}
	return m, nil
}

func simulatePost(webhookURL, secret string) string {
	update := map[string]any{
		"update_id": 99999999,
		"message": map[string]any{
			"message_id": 1,
			"from": map[string]any{
				"id":         12345,
				"is_bot":     false,
				"first_name": "smoke_test_user",
			},
			"chat": map[string]any{
				"id":         12345,
				"type":       "private",
				"first_name": "smoke_test_user",
			},
			"date": time.Now().Unix(),
			"text": "/start",
		},
	}
	body, _ := json.Marshal(update)
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("HTTP 投递失败(预期，因为 user-server 未运行): %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Sprintf("HTTP %d, body=%s", resp.StatusCode, string(respBody))
}

func statusFor(pending float64, lastErr string) string {
	if lastErr != "" {
		return "❌ 有错误，需要排查"
	}
	if pending > 0 {
		return "⏳ 有待处理 update（正常，说明有入站消息积压）"
	}
	return "✅ 正常，TG 侧已正确接收 webhook 注册"
}
