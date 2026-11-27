package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"hivemtk-user/internal/secrets"
)

const (
	llmSessionBlindDomain    = "hivemtk:llm-session-blind:v1"
	llmSessionBlindKeyMinLen = 32
	llmSessionKeyMaxLen      = 512
)

var (
	ErrInvalidLLMSessionKey   = errors.New("LLM canonical session key 非法")
	ErrLLMSessionBlindKey     = errors.New("LLM session blind index 密钥非法")
	ErrLLMSessionCryptoNoKeys = errors.New("LLM session blind index 密钥集合为空")
)

// ProtectedLLMSessionKey 只包含可持久化材料；BlindIndexes 按当前→历史密钥顺序返回，供轮换期双写/双查。
type ProtectedLLMSessionKey struct {
	Encrypted    []byte
	BlindIndexes []string
}

// LLMSessionCrypto 将 canonical key 加密为 AEAD 密文，并用独立密钥派生产生稳定的 HMAC blind index。
type LLMSessionCrypto struct {
	blindKeys [][]byte
}

func NewLLMSessionCrypto(blindKeys ...[]byte) (*LLMSessionCrypto, error) {
	if len(blindKeys) == 0 {
		return nil, ErrLLMSessionCryptoNoKeys
	}
	keys := make([][]byte, 0, len(blindKeys))
	seen := make(map[string]bool, len(blindKeys))
	for _, key := range blindKeys {
		if len(key) < llmSessionBlindKeyMinLen {
			return nil, ErrLLMSessionBlindKey
		}
		copyKey := append([]byte{}, key...)
		fingerprint := hex.EncodeToString(copyKey)
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		keys = append(keys, copyKey)
	}
	return &LLMSessionCrypto{blindKeys: keys}, nil
}

func validateLLMSessionCanonicalKey(key string) error {
	if key == "" || strings.TrimSpace(key) != key || strings.ContainsRune(key, '\x00') || len(key) > llmSessionKeyMaxLen {
		return ErrInvalidLLMSessionKey
	}
	return nil
}

func (c *LLMSessionCrypto) Protect(key string) (ProtectedLLMSessionKey, error) {
	if err := validateLLMSessionCanonicalKey(key); err != nil {
		return ProtectedLLMSessionKey{}, err
	}
	if c == nil || len(c.blindKeys) == 0 {
		return ProtectedLLMSessionKey{}, ErrLLMSessionCryptoNoKeys
	}
	encrypted, err := secrets.EncryptString(key)
	if err != nil {
		return ProtectedLLMSessionKey{}, err
	}
	indexes := make([]string, 0, len(c.blindKeys))
	for _, blindKey := range c.blindKeys {
		indexes = append(indexes, c.blindIndex(key, blindKey))
	}
	return ProtectedLLMSessionKey{Encrypted: []byte(encrypted), BlindIndexes: indexes}, nil
}

func (c *LLMSessionCrypto) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", ErrInvalidLLMSessionKey
	}
	plain, err := secrets.DecryptString(string(ciphertext))
	if err != nil {
		return "", err
	}
	if err := validateLLMSessionCanonicalKey(plain); err != nil {
		return "", err
	}
	return plain, nil
}

func (c *LLMSessionCrypto) Rotate(newBlindKey []byte) (*LLMSessionCrypto, error) {
	if len(newBlindKey) < llmSessionBlindKeyMinLen {
		return nil, ErrLLMSessionBlindKey
	}
	if c == nil || len(c.blindKeys) == 0 {
		return nil, ErrLLMSessionCryptoNoKeys
	}
	keys := make([][]byte, 0, len(c.blindKeys)+1)
	keys = append(keys, newBlindKey)
	keys = append(keys, c.blindKeys...)
	return NewLLMSessionCrypto(keys...)
}

func (c *LLMSessionCrypto) VerifyBlindIndex(key, index string) bool {
	if err := validateLLMSessionCanonicalKey(key); err != nil || c == nil || index == "" {
		return false
	}
	for _, blindKey := range c.blindKeys {
		candidate := c.blindIndex(key, blindKey)
		if hmac.Equal([]byte(candidate), []byte(index)) {
			return true
		}
	}
	return false
}

func (c *LLMSessionCrypto) Redact(key string) string {
	if err := validateLLMSessionCanonicalKey(key); err != nil || c == nil || len(c.blindKeys) == 0 {
		return "llmsess:redacted:invalid"
	}
	index := c.blindIndex(key, c.blindKeys[0])
	if len(index) < 12 {
		return "llmsess:redacted:invalid"
	}
	return "llmsess:redacted:" + index[:12]
}

func (c *LLMSessionCrypto) blindIndex(key string, blindKey []byte) string {
	mac := hmac.New(sha256.New, blindKey)
	_, _ = mac.Write([]byte(llmSessionBlindDomain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(key))
	return hex.EncodeToString(mac.Sum(nil))
}
