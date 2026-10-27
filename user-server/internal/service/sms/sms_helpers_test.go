package sms

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestEscapeJSON(t *testing.T) {
	cases := map[string]string{
		`hello`:  `"hello"`,
		`a"b`:    `"a\"b"`,
		"a\nb":   `"a\nb"`,
		"a\tb":   `"a\tb"`,
		"中文":     `"中文"`,
		"a\x00b": `"a\u0000b"`,
	}
	for in, want := range cases {
		if got := escapeJSON(in); got != want {
			t.Errorf("escapeJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJsonMarshalString_Empty(t *testing.T) {
	got, _ := jsonMarshalString("")
	if got != `""` {
		t.Errorf("expected empty quoted, got %q", got)
	}
}

func TestRandomNonce(t *testing.T) {
	n1 := randomNonce()
	n2 := randomNonce()
	if n1 == n2 {
		t.Error("randomNonce should be different")
	}
	if len(n1) != 32 {
		t.Errorf("expected 32 chars, got %d", len(n1))
	}
}

func TestSpecialURLEncode(t *testing.T) {
	cases := map[string]string{
		"abc":       "abc",
		"a-b_c.d~e": "a-b_c.d~e",
		"a b":       "a+b",
		"a+b":       "a%2Bb",
		"中文":        "%C3%A4%C2%B8%C2%AD%C3%A6%C2%96%C2%87",
	}
	for in, want := range cases {
		if got := specialURLEncode(in); got != want {
			t.Errorf("specialURLEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPercentEncode(t *testing.T) {
	if got := percentEncode("a-b_c.d~e"); got != "a-b_c.d~e" {
		t.Errorf("expected unchanged, got %q", got)
	}
	got := percentEncode("a b")
	if !strings.Contains(got, "%20") {
		t.Errorf("expected %%20 for space, got %q", got)
	}
}

func TestSignAliyun(t *testing.T) {
	params := url.Values{}
	params.Set("AccessKeyId", "testid")
	params.Set("Action", "SendSms")
	params.Set("Format", "JSON")

	sig := signAliyun(params, "testsecret")
	if sig == "" {
		t.Error("expected non-empty signature")
	}
	sig2 := signAliyun(params, "testsecret")
	if sig != sig2 {
		t.Error("signature should be deterministic")
	}
	sig3 := signAliyun(params, "different")
	if sig == sig3 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestSignAliyun_SkipSignature(t *testing.T) {
	params := url.Values{}
	params.Set("AccessKeyId", "testid")
	params.Set("Signature", "should-be-ignored")
	sig := signAliyun(params, "testsecret")
	if sig == "" {
		t.Error("expected non-empty signature")
	}
}

func TestSha256Hex(t *testing.T) {
	got := sha256Hex("")
	if got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("unexpected sha256 for empty: %s", got)
	}
}

func TestHmacSHA256(t *testing.T) {
	mac := hmacSHA256([]byte("key"), "data")
	if len(mac) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(mac))
	}
}

func TestBuildWSSE(t *testing.T) {
	wsse := buildWSSE("appKey123", "appSecret456")
	if !strings.Contains(wsse, `UsernameToken`) {
		t.Error("expected UsernameToken in WSSE")
	}
	if !strings.Contains(wsse, `Username="appKey123"`) {
		t.Error("expected username")
	}
	if !strings.Contains(wsse, `PasswordDigest="`) {
		t.Error("expected PasswordDigest")
	}
	if !strings.Contains(wsse, `Nonce="`) {
		t.Error("expected Nonce")
	}
	if !strings.Contains(wsse, `Created="`) {
		t.Error("expected Created")
	}
	if !strings.Contains(wsse, time.Now().UTC().Format("2006")) {
		t.Error("expected current year in timestamp")
	}
}
