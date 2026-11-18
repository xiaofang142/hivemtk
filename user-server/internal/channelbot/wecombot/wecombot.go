// Package wecombot 封装企业微信智能机器人回调：签名校验、AES-CBC 解密、webhook 推送。
//
// 与 wecom 自建应用的区别：智能机器人走独立回调协议（同一套 EncodingAESKey
// 加解密算法，但事件体与推送地址不同）。
package wecombot

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"hivemtk-user/internal/channelbot/core"
)

// VerifySignature 校验回调签名：sha1(sort(token,timestamp,nonce,encrypt) 拼接)。
func VerifySignature(token, signature, timestamp, nonce, encrypt string) bool {
	if token == "" || signature == "" {
		return false
	}
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return core.SecureEqual(strings.ToLower(signature), hex.EncodeToString(sum[:]))
}

// aesKey 把 43 位 EncodingAESKey 还原为 32 字节密钥。
func aesKey(encodingAESKey string) ([]byte, error) {
	if len(encodingAESKey) != 43 {
		return nil, fmt.Errorf("wecombot: EncodingAESKey 长度必须为 43，实际 %d", len(encodingAESKey))
	}
	raw, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("wecombot: EncodingAESKey base64 解码失败: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("wecombot: AES key 长度必须为 32，实际 %d", len(raw))
	}
	return raw, nil
}

// Decrypt 解密回调密文（AES-256-CBC，PKCS#7）。
//
// 明文结构：16 字节随机 + 4 字节网络序 msg_len + msg + receiveid。
func Decrypt(encodingAESKey, ciphertext string) ([]byte, error) {
	key, err := aesKey(encodingAESKey)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("wecombot: 密文 base64 解码失败: %w", err)
	}
	if len(data) < aes.BlockSize || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("wecombot: 密文长度非法 %d", len(data))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("wecombot: aes.NewCipher: %w", err)
	}
	iv := key[:aes.BlockSize]
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, data)

	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, err
	}
	if len(plain) < 20 {
		return nil, fmt.Errorf("wecombot: 明文太短 %d", len(plain))
	}
	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if msgLen < 0 || 20+msgLen > len(plain) {
		return nil, fmt.Errorf("wecombot: msg_len 越界 %d (plain=%d)", msgLen, len(plain))
	}
	return plain[20 : 20+msgLen], nil
}

// Encrypt 加密（与 Decrypt 互逆，供测试与本地联调构造固定向量）。
func Encrypt(encodingAESKey, receiveID string, msg []byte) (string, error) {
	key, err := aesKey(encodingAESKey)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	buf.Write(iv)
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(msg))); err != nil {
		return "", err
	}
	buf.Write(msg)
	buf.WriteString(receiveID)
	plain := pkcs7Pad(buf.Bytes(), aes.BlockSize)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, plain)
	return base64.StdEncoding.EncodeToString(out), nil
}

func pkcs7Pad(b []byte, size int) []byte {
	pad := size - len(b)%size
	return append(b, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

func pkcs7Unpad(b []byte, size int) ([]byte, error) {
	if len(b) == 0 || len(b)%size != 0 {
		return nil, fmt.Errorf("wecombot: 明文长度非法 %d", len(b))
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > size || pad > len(b) {
		return nil, fmt.Errorf("wecombot: PKCS#7 填充非法 %d", pad)
	}
	for _, c := range b[len(b)-pad:] {
		if int(c) != pad {
			return nil, fmt.Errorf("wecombot: PKCS#7 填充不一致")
		}
	}
	return b[:len(b)-pad], nil
}

// Message 解密后的机器人事件（只取本项目消费的字段）。
type Message struct {
	MsgType string `json:"msgtype"`
	MsgID   string `json:"msgid"`
	ChatID  string `json:"chatid"`
	From    struct {
		UserID string `json:"userid"`
		Name   string `json:"name"`
	} `json:"from"`
	Text *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Image *struct {
		MediaID string `json:"media_id"`
	} `json:"image,omitempty"`
	Markdown *struct {
		Content string `json:"content"`
	} `json:"markdown,omitempty"`
	Event *struct {
		EventType         string `json:"eventtype"`
		TemplateCardEvent struct {
			EventKey string `json:"event_key"`
			TaskID   string `json:"task_id"`
		} `json:"template_card_event"`
	} `json:"event,omitempty"`
	CreateTime int64 `json:"create_time"`
}

// ParseMessage 解析解密后的 JSON。
func ParseMessage(plain []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(plain, &m); err != nil {
		return nil, fmt.Errorf("wecombot parse message: %w", err)
	}
	return &m, nil
}

// Client 智能机器人推送客户端（webhook 地址按账号配置）。
type Client struct {
	core.BaseClient
}

func NewClient(opts ...core.ClientOption) *Client {
	return &Client{BaseClient: core.NewBaseClient(opts...)}
}

// PushMessage 向机器人 webhook 推送消息。msgType 支持 markdown / text。
func (c *Client) PushMessage(ctx context.Context, webhookURL, msgType, content string) error {
	if webhookURL == "" {
		return fmt.Errorf("wecombot: webhook 地址为空")
	}
	if msgType == "" {
		msgType = "markdown"
	}
	payload := map[string]any{"msgtype": msgType, msgType: map[string]any{"content": content}}
	body, _ := json.Marshal(payload)
	raw, status, err := c.DoJSONBytes(ctx, "POST", webhookURL, body, nil)
	if err != nil {
		return err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("wecombot push decode: %w (status=%d)", err, status)
	}
	if r.ErrCode != 0 {
		return &core.APIError{Channel: "wecombot", StatusCode: status, Code: fmt.Sprint(r.ErrCode),
			Raw: fmt.Sprintf("wecombot push: errcode=%d errmsg=%s", r.ErrCode, r.ErrMsg)}
	}
	return nil
}
