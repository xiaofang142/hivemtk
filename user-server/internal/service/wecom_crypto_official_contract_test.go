package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

// 企业微信回调加解密的官方契约夹具（无需真凭据）。
// 契约出处 https://developer.work.weixin.qq.com/document/path/90968 ：
//   AESKey   = Base64_Decode(EncodingAESKey + "=")            → 32 字节
//   IV       = AESKey 的前 16 字节                            ← 不是密文首块
//   明文     = random(16) + msg_len(4, 网络字节序) + msg + receiveid
//   填充     = PKCS#7 补到 32 的整数倍
//   密文     = AES-256-CBC(全量明文) 再 Base64
// 回调验签：msg_signature = sha1(字典序 sort(token, timestamp, nonce, encrypt))。

func newWecomEncodingAESKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("生成 EncodingAESKey: %v", err)
	}
	return strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
}

// wecomOfficialEncrypt 完全按官方说明实现，作为「上游长什么样」的裁判，
// 不与被测代码共用任何加解密辅助函数。
func wecomOfficialEncrypt(t *testing.T, encodingAESKey, msg, receiveid string) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		t.Fatalf("decode AESKey: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes cipher: %v", err)
	}
	plain := make([]byte, 16)
	if _, err := rand.Read(plain); err != nil {
		t.Fatalf("random prefix: %v", err)
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(msg)))
	plain = append(plain, lenBuf...)
	plain = append(plain, msg...)
	plain = append(plain, receiveid...)

	pad := 32 - len(plain)%32
	for i := 0; i < pad; i++ {
		plain = append(plain, byte(pad))
	}
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(ct, plain)
	return base64.StdEncoding.EncodeToString(ct)
}

func wecomCallbackSignature(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

const wecomFixtureXML = "<xml><ToUserName><![CDATA[ww_corpid]]></ToUserName>" +
	"<FromUserName><![CDATA[zhangsan]]></FromUserName>" +
	"<CreateTime>1726732800</CreateTime><MsgType><![CDATA[text]]></MsgType>" +
	"<Content><![CDATA[这个套餐多少钱]]></Content><MsgId>6234567890123456</MsgId>" +
	"<AgentID>1000002</AgentID></xml>"

func TestDecryptWeComMessage_OfficialLayout(t *testing.T) {
	aesKey := newWecomEncodingAESKey(t)
	cipherB64 := wecomOfficialEncrypt(t, aesKey, wecomFixtureXML, "ww_corpid")

	got, err := DecryptWeComMessage(aesKey, cipherB64)
	if err != nil {
		t.Fatalf("官方布局的密文解不出来: %v", err)
	}
	if string(got) != wecomFixtureXML {
		t.Errorf("解出的消息体与原文不符\ngot  = %q\nwant = %q", got, wecomFixtureXML)
	}
}

func TestDecryptWeComMessage_ShortAndASCIIContent(t *testing.T) {
	aesKey := newWecomEncodingAESKey(t)
	msg := "hi"
	cipherB64 := wecomOfficialEncrypt(t, aesKey, msg, "ww_corpid")

	got, err := DecryptWeComMessage(aesKey, cipherB64)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != msg {
		t.Errorf("got %q, want %q", got, msg)
	}
}

func TestVerifyURL_OfficialEchostrHandshake(t *testing.T) {
	token := "the_token"
	aesKey := newWecomEncodingAESKey(t)
	echostr := wecomOfficialEncrypt(t, aesKey, "call_back_echostr", "ww_corpid")
	timestamp, nonce := "1726732800", "1372623149"
	sig := wecomCallbackSignature(token, timestamp, nonce, "call_back_echostr")

	got, err := VerifyURL(token, aesKey, sig, timestamp, nonce, echostr)
	if err != nil {
		t.Fatalf("URL 验证失败: %v", err)
	}
	if got != "call_back_echostr" {
		t.Errorf("应原样回显 echostr 明文（官方要求不加引号/BOM/换行），got %q", got)
	}
}

func TestDecryptWeComMessage_RejectsForeignAESKey(t *testing.T) {
	aesKey := newWecomEncodingAESKey(t)
	other := newWecomEncodingAESKey(t)
	cipherB64 := wecomOfficialEncrypt(t, aesKey, wecomFixtureXML, "ww_corpid")

	if _, err := DecryptWeComMessage(other, cipherB64); err == nil {
		t.Error("换一把密钥还能解开，说明这条路径根本没按官方方案解密")
	}
}

func TestDecryptWeComMessage_BadKeyLengthRejected(t *testing.T) {
	if _, err := DecryptWeComMessage("short", "whatever"); err == nil {
		t.Fatal("EncodingAESKey 非 43 字符应直接拒绝")
	}
}
