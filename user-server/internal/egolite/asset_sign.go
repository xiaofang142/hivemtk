package egolite

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// 资产签名链（决策 8：ed25519；设计文档 A2 资产包格式 + R7 供应链防线）。
//
// 链路：发布者私钥签名 manifest.json 规范化字节 → manifest.hivemtk.signature 携带
// （sig 对 manifest 的 SHA-256；公钥指纹登记在平台上架单）。runner 安装/执行前验签：
// 验签失败 = 包被篡改或来源不明 → 拒绝执行（fail-close）。
//
// 实现纪律：本文件只做**验证侧**（服务端审核工作台与 runner 共用同一实现——双端同源
// 纪律的第二次应用）；签发侧在平台端（发布者本地），不入 user-server 仓库。

// SignedManifest 带 hivemtk 扩展面的 manifest（解析后）
type SignedManifest struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Domains  []string `json:"domains"`
	Hivemtk  struct {
		Entry       string `json:"entry"`
		CompatMin   string `json:"compat_min"`
		Writes      []string `json:"writes"`
		SigAlg      string `json:"sig_alg"`   // 固定 ed25519
		Signature   string `json:"signature"` // base64(std) of SHA-256(canonical manifest body)
		PubKeyFpr   string `json:"pubkey_fingerprint"` // sha256(pubkey) 前 16 hex（平台登记核对）
	} `json:"hivemtk"`
}

// VerifyAssetSignature 验证资产 manifest 的 ed25519 签名。
//
// canonicalBytes：**去掉 hivemtk.signature 字段后**的 manifest JSON 原始字节。
// 规范化约定：发布者以「除 signature 外的完整 JSON（保持原键序）」计算 SHA-256——
// 这要求发布侧在签名前把 signature 置空串写盘一次取字节；验签侧读原始文件字节、
// 反序列化后把 signature 字段重置为空串再序列化会破坏键序。
// 因此定案：**签名对象是 manifest 的 rawBytes 但 signature 字段值替换为空串**的精确字节——
// 本函数用字节级定位替换（不重序列化），发布侧以同一规则产出。这样新增/改序字段
// 不会破坏验签（除 signature 值本身外一切字节保持发布时的样子）。
func VerifyAssetSignature(rawManifest []byte, pubKey ed25519.PublicKey) error {
	if len(pubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("公钥长度非法（%d ≠ %d）", len(pubKey), ed25519.PublicKeySize)
	}
	var m SignedManifest
	if err := json.Unmarshal(rawManifest, &m); err != nil {
		return fmt.Errorf("manifest 解析失败: %w", err)
	}
	if m.Hivemtk.SigAlg != "" && m.Hivemtk.SigAlg != "ed25519" {
		return fmt.Errorf("不支持的签名算法: %q", m.Hivemtk.SigAlg)
	}
	sig, err := base64.StdEncoding.DecodeString(m.Hivemtk.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature 缺失或长度非法")
	}
	canonical, err := blankSignatureField(rawManifest, m.Hivemtk.Signature)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	if !ed25519.Verify(pubKey, sum[:], sig) {
		return fmt.Errorf("签名验证失败（包被篡改或公钥不匹配）")
	}
	return ValidateManifestShape(ManifestShape{
		ID:               m.ID,
		Domains:          m.Domains,
		HivemtkEntry:     m.Hivemtk.Entry,
		HivemtkCompatMin: m.Hivemtk.CompatMin,
	})
}

// blankSignatureField 字节级把 signature 字段值替换为空串（保持其余字节不动）。
// 多次出现时全部替换无害（值唯一性由签名值本身保证）。
func blankSignatureField(raw []byte, sigValue string) ([]byte, error) {
	if sigValue == "" {
		return raw, nil
	}
	needle := []byte("\"" + sigValue + "\"")
	repl := []byte(`""`)
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if i+len(needle) <= len(raw) && string(raw[i:i+len(needle)]) == string(needle) {
			out = append(out, repl...)
			i += len(needle)
			continue
		}
		out = append(out, raw[i])
		i++
	}
	return out, nil
}

// PubKeyFingerprint 公钥指纹（平台上架单登记核对用）
func PubKeyFingerprint(pubKey ed25519.PublicKey) string {
	sum := sha256.Sum256(pubKey)
	return fmt.Sprintf("%x", sum[:8])
}
