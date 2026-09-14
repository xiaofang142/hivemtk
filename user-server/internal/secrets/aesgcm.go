// Package secrets 提供基于 AES-256-GCM 的对称加密原语，落地 TL-1 决策：
//
//	启动时若未配置 MASTER_KEY（>=32 字节），InitFromEnv 返回 error；
//	cmd/api 装配层在生产环境（config.IsDevelopmentEnv()==false）据此 fail-fast 拒绝启动，
//	仅开发环境允许降级。Ready() 返回 false 时 Encrypt/Decrypt 返回 ErrMasterKeyMissing，
//	调用方需自行判断 Ready()，降级路径为明文读写（WARN 日志）。
//
// 设计要点：
//   - 使用 AEAD (GCM) 而非裸 AES，提供完整性校验。
//   - nonce 12 字节随机，前缀拼接到密文，方便持久化。
//   - EncryptString 输出格式 `enc:v1:{base64(nonce|ciphertext)}`，DecryptString 识别该前缀
//     同时兼容存量无前缀纯密文（双轨双格式）。
//   - 任意路径泄露 MASTER_KEY 即整盘作废；建议通过 secret manager 注入。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	envMasterKey = "MASTER_KEY"
	keyLen       = 32
	nonceLen     = 12

	stringEncPrefix = "enc:v1:"
)

// ErrMasterKeyMissing 在 AEAD 未初始化时返回。调用方据此降级为明文 + WARN 日志。
var ErrMasterKeyMissing = errors.New("secrets: MASTER_KEY is missing or shorter than 32 bytes")

var (
	gcmOnce sync.Once
	gcmAEAD cipher.AEAD
)

// InitFromEnv 从环境变量读取主密钥并初始化全局 AEAD。
// 空/过短/初始化失败均返回 error 但不阻止进程运行，Ready() 将返回 false。
// 进程内多次调用幂等。
func InitFromEnv() error {
	var initErr error
	gcmOnce.Do(func() {
		raw := os.Getenv(envMasterKey)
		if len(raw) < keyLen {
			initErr = fmt.Errorf("%w (got len=%d)", ErrMasterKeyMissing, len(raw))
			return
		}
		key := []byte(raw)[:keyLen]
		block, err := aes.NewCipher(key)
		if err != nil {
			initErr = fmt.Errorf("secrets: aes.NewCipher: %w", err)
			return
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			initErr = fmt.Errorf("secrets: cipher.NewGCM: %w", err)
			return
		}
		gcmAEAD = aead
	})
	return initErr
}

// Ready 报告全局 AEAD 是否已就绪（MASTER_KEY 可用且初始化成功）。
func Ready() bool { return gcmAEAD != nil }

// ResetForTest 重置全局 AEAD 状态和 sync.Once，仅供单测切换不同密钥使用。
// 生产代码禁止调用。
func ResetForTest() {
	gcmOnce = sync.Once{}
	gcmAEAD = nil
}

// Encrypt 加密任意明文。未初始化时返回 ErrMasterKeyMissing。
func Encrypt(plaintext []byte) ([]byte, error) {
	if gcmAEAD == nil {
		return nil, ErrMasterKeyMissing
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	out := gcmAEAD.Seal(nil, nonce, plaintext, nil)
	full := make([]byte, 0, nonceLen+len(out))
	full = append(full, nonce...)
	full = append(full, out...)
	return full, nil
}

// Decrypt 解密；密文损坏或密钥不匹配会返回 error。
func Decrypt(ciphertext []byte) ([]byte, error) {
	if gcmAEAD == nil {
		return nil, ErrMasterKeyMissing
	}
	if len(ciphertext) < nonceLen {
		return nil, errors.New("secrets: ciphertext too short")
	}
	nonce := ciphertext[:nonceLen]
	body := ciphertext[nonceLen:]
	pt, err := gcmAEAD.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm.Open: %w", err)
	}
	return pt, nil
}

// EncryptString 加密封装为持久化字符串格式 `enc:v1:{base64(nonce|ciphertext)}`。
// Ready() 为 false 时返回 ("", ErrMasterKeyMissing)。
func EncryptString(s string) (string, error) {
	if !Ready() {
		return "", ErrMasterKeyMissing
	}
	out, err := Encrypt([]byte(s))
	if err != nil {
		return "", err
	}
	return stringEncPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// DecryptString 识别两种格式并解密：
//   - `enc:v1:base64(...)` 前缀格式（本模块 EncryptString 输出）
//   - 无前缀但 IsCiphertextFormat 判定为密文（兼容存量无前缀）
//   - 明文（IsCiphertextFormat 判定为否）直接原样返回
//
// Ready() 为 false 时返回 ("", ErrMasterKeyMissing)，调用方降级为明文。
func DecryptString(s string) (string, error) {
	if !Ready() {
		return "", ErrMasterKeyMissing
	}
	var raw []byte
	var err error
	if strings.HasPrefix(s, stringEncPrefix) {
		raw, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(s, stringEncPrefix))
		if err != nil {
			return "", fmt.Errorf("secrets: decode string prefix: %w", err)
		}
	} else if IsCiphertextFormat(s) {
		raw = []byte(s)
	} else {
		return s, nil
	}
	out, err := Decrypt(raw)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func IsCiphertextFormat(s string) bool {
	return strings.HasPrefix(s, stringEncPrefix)
}
