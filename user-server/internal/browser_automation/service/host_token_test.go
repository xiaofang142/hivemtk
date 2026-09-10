package service

import (
	"context"
	"errors"
	"testing"

	hrepo "hivemtk-user/internal/repository"
)

func TestGenerateHostTokenFormat(t *testing.T) {
	tok, err := GenerateHostToken(42)
	if err != nil {
		t.Fatalf("GenerateHostToken err: %v", err)
	}
	userID, ok := parseHostTokenUser(tok)
	if !ok || userID != 42 {
		t.Errorf("parseHostTokenUser(%q) = %d, %v; want 42, true", tok, userID, ok)
	}
}

func TestParseHostTokenUser(t *testing.T) {
	valid := map[string]uint{"bh_1_abcdef": 1, "bh_999_x": 999}
	for tok, want := range valid {
		id, ok := parseHostTokenUser(tok)
		if !ok || id != want {
			t.Errorf("parseHostTokenUser(%q) = %d,%v want %d", tok, id, ok, want)
		}
	}
	invalid := []string{"", "bh_1", "xx_1_abc", "bh_0_abc", "bh_abc_def"}
	for _, tok := range invalid {
		if _, ok := parseHostTokenUser(tok); ok {
			t.Errorf("parseHostTokenUser(%q) should fail", tok)
		}
	}
}

// fakeKV 空候选 KV（Get 恒报错 ⇒ candidates 空 ⇒ fail-closed）
type errKV struct{}

func (errKV) Get(_ context.Context, _ string) (string, error) { return "", errors.New("empty") }
func (errKV) Upsert(_ context.Context, _, _ string) (string, error) {
	return "", errors.New("empty")
}
func (errKV) EnsureTable(_ context.Context) error { return nil }

var _ hrepo.SystemConfigKVRepository = errKV{}

func TestValidateHostTokenFailClosed(t *testing.T) {
	// KV 无 token 时必须拒绝（fail-closed）
	userID, ok := ValidateHostToken(context.Background(), errKV{}, "bh_1_x")
	if ok || userID != 0 {
		t.Errorf("空候选应拒绝，got %d,%v", userID, ok)
	}
}

// okKV 返回固定 token 的 KV
type okKV struct{ token string }

func (k okKV) Get(_ context.Context, key string) (string, error) {
	if key == hostTokenKey {
		return k.token, nil
	}
	return "", errors.New("empty")
}
func (k okKV) Upsert(_ context.Context, _, _ string) (string, error) { return "", nil }
func (k okKV) EnsureTable(_ context.Context) error                   { return nil }

func TestValidateHostTokenMatch(t *testing.T) {
	tok, _ := GenerateHostToken(7)
	kv := okKV{token: tok}
	userID, ok := ValidateHostToken(context.Background(), kv, tok)
	if !ok || userID != 7 {
		t.Errorf("合法 token 应通过且解出 user=7，got %d,%v", userID, ok)
	}
	userID, ok = ValidateHostToken(context.Background(), kv, "bh_7_wrong")
	if ok {
		t.Error("错误 token 应拒绝")
	}
}
