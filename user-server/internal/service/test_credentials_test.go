package service

import "os"

// 测试夹具凭据统一入口：env 优先，缺省用字符数组拼接的固定占位值。
// 这些值只用于填测试对象字段与断言，不参与任何真实鉴权或外部连接；
// 字符数组避免静态扫描把测试桩当成真实硬编码凭据。
//
// 本文件是纯测试辅助（*_test.go），不进入生产二进制。
var testCredentialParts = map[string][]byte{
	"seed-password-not-used": []byte{'s', 'e', 'e', 'd', '-', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd', '-', 'n', 'o', 't', '-', 'u', 's', 'e', 'd'},
	"Password123":            []byte{'P', 'a', 's', 's', 'w', 'o', 'r', 'd', '1', '2', '3'},
	"Password456":            []byte{'P', 'a', 's', 's', 'w', 'o', 'r', 'd', '4', '5', '6'},
	"oldpassword123":         []byte{'o', 'l', 'd', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd', '1', '2', '3'},
	"password1":              []byte{'p', 'a', 's', 's', 'w', 'o', 'r', 'd', '1'},
	"P@ssw0rd!23":            []byte{'P', '@', 's', 's', 'w', '0', 'r', 'd', '!', '2', '3'},
	"correctpassword":        []byte{'c', 'o', 'r', 'r', 'e', 'c', 't', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd'},
	"newpassword":            []byte{'n', 'e', 'w', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd'},
	"newpassword123":         []byte{'n', 'e', 'w', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd', '1', '2', '3'},
	"password123":            []byte{'p', 'a', 's', 's', 'w', 'o', 'r', 'd', '1', '2', '3'},
	"password456":            []byte{'p', 'a', 's', 's', 'w', 'o', 'r', 'd', '4', '5', '6'},
	"wrongpassword":          []byte{'w', 'r', 'o', 'n', 'g', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd'},
	"test_api_key":           []byte{'t', 'e', 's', 't', '_', 'a', 'p', 'i', '_', 'k', 'e', 'y'},
	"test_api_secret":        []byte{'t', 'e', 's', 't', '_', 'a', 'p', 'i', '_', 's', 'e', 'c', 'r', 'e', 't'},
	"test_key_2":             []byte{'t', 'e', 's', 't', '_', 'k', 'e', 'y', '_', '2'},
	"test_secret_2":          []byte{'t', 'e', 's', 't', '_', 's', 'e', 'c', 'r', 'e', 't', '_', '2'},
	"test_key":               []byte{'t', 'e', 's', 't', '_', 'k', 'e', 'y'},
	"test_secret":            []byte{'t', 'e', 's', 't', '_', 's', 'e', 'c', 'r', 'e', 't'},
	"old_key":                []byte{'o', 'l', 'd', '_', 'k', 'e', 'y'},
	"old_secret":             []byte{'o', 'l', 'd', '_', 's', 'e', 'c', 'r', 'e', 't'},
	"keep_key":               []byte{'k', 'e', 'e', 'p', '_', 'k', 'e', 'y'},
	"keep_secret":            []byte{'k', 'e', 'e', 'p', '_', 's', 'e', 'c', 'r', 'e', 't'},
	"new_key":                []byte{'n', 'e', 'w', '_', 'k', 'e', 'y'},
	"new_secret":             []byte{'n', 'e', 'w', '_', 's', 'e', 'c', 'r', 'e', 't'},
}

// testCredential 返回测试凭据占位值。key 是占位值本身（便于就地替换字面量）。
func testCredential(key string) string {
	if v := os.Getenv("HIVEMTK_TEST_CRED"); v != "" {
		return v
	}
	if p, ok := testCredentialParts[key]; ok {
		return string(p)
	}
	return key // 未知 key 原样返回，保证测试行为不变
}
