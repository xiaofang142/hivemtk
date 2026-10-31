package llm

import (
	"sync"

	"hivemtk-user/internal/secrets"

	"hivemtk-user/internal/pkg/utils/logger"
)

func encryptAPIKeyForStorage(plain string) string {
	if plain == "" || secrets.IsCiphertextFormat(plain) || !secrets.Ready() {
		return plain
	}
	enc, err := secrets.EncryptString(plain)
	if err != nil {
		logger.Warnf("[LLM] api key encrypt failed (store plaintext as fallback): %v", err)
		return plain
	}
	return enc
}

// keyInvalidProviders 记录「密文存在但解不开」的 provider 名字。
//
// 这一类失败与「没配 Key」是两回事：本地模型（local-mlx 等）本来就可以没有 Key，
// 而密文解不开意味着**曾经配过、现在用不了**——主密钥被换过，或者多实例各拿一份
// 不同的 MASTER_KEY。两者的处置方式完全不同（后者重新保存一次密钥即可恢复），
// 所以必须在 provider 上留一个可观测的标记，让运维能从页面直接看出来，
// 而不是对着上游 401 猜。
var (
	keyInvalidMu        sync.RWMutex
	keyInvalidProviders = map[string]string{} // provider name -> 失败原因
)

func markKeyInvalid(name, reason string) {
	keyInvalidMu.Lock()
	defer keyInvalidMu.Unlock()
	if keyInvalidProviders == nil {
		keyInvalidProviders = map[string]string{}
	}
	keyInvalidProviders[name] = reason
}

func clearKeyInvalid(name string) {
	keyInvalidMu.Lock()
	defer keyInvalidMu.Unlock()
	delete(keyInvalidProviders, name)
}

// KeyInvalidReason 返回 provider 的密钥解密失败原因（未失败时 ok=false）。
func KeyInvalidReason(name string) (string, bool) {
	keyInvalidMu.RLock()
	defer keyInvalidMu.RUnlock()
	reason, ok := keyInvalidProviders[name]
	return reason, ok
}

// KeyInvalidProviders 返回当前所有密钥解不开的 provider（快照，供启动汇总日志/观测使用）。
func KeyInvalidProviders() map[string]string {
	keyInvalidMu.RLock()
	defer keyInvalidMu.RUnlock()
	out := make(map[string]string, len(keyInvalidProviders))
	for k, v := range keyInvalidProviders {
		out[k] = v
	}
	return out
}

func decryptAPIKeyForUse(stored string) string {
	plain, _ := decryptAPIKeyForUseNamed("", stored)
	return plain
}

// decryptAPIKeyForUseNamed 解密并顺带记录失败所属的 provider。
// name 为空时只解密不记账（embedding 等没有 provider 名字的读路径）。
func decryptAPIKeyForUseNamed(name, stored string) (string, bool) {
	if stored == "" || !secrets.IsCiphertextFormat(stored) {
		if name != "" {
			clearKeyInvalid(name)
		}
		return stored, false
	}
	plain, err := secrets.DecryptString(stored)
	if err != nil {
		logger.Errorf("[LLM] api key decrypt failed (master key rotation/多实例 key 不一致?), provider=%s 将以空 key 显式失败，需在「模型提供商」重新保存密钥: %v", name, err)
		if name != "" {
			markKeyInvalid(name, "API Key 无法用当前 MASTER_KEY 解密，请重新保存该提供商的密钥")
		}
		return "", true
	}
	if name != "" {
		clearKeyInvalid(name)
	}
	return plain, false
}
