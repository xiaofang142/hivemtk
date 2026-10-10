// Package router 的测试统一入口（TestMain）
//
// RegisterFilesRoute 里有一句 os.MkdirAll(storage.LocalSource())（files_guard.go），
// 而 go test 的 CWD 就是本包目录 —— 只要装配腿跑到它、又没人先注入
// STORAGE_LOCAL_BASE_DIR，就会在 internal/router/uploads/ 下凭空长出一棵空树。
// 这里在所有用例前把三棵存储根指进临时目录，收尾再 Sweep 一次兜底。
package router

import (
	"os"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/storage/storagetest"
)

// visitorTokenTestSecret 测试用访客 token HMAC 密钥（仅测试，非生产密钥）。
// 与 internal/service/main_test.go 用的是同一枚：装配腿里的控制器会真的签发
// visitor_token，密钥为空时那些口只会得到一句"生成 visitor_token 失败"，
// 读起来像归属校验坏了。
const visitorTokenTestSecret = "hivemtk-test-visitor-token-secret-0123456789"

// TestMain router 包测试统一入口
func TestMain(m *testing.M) {
	if cfg := config.GetAppConfig(); cfg.Security.VisitorTokenSecret == "" {
		cfg.Security.VisitorTokenSecret = visitorTokenTestSecret
		config.SetAppConfig(&cfg)
	}

	restoreStorageRoots := storagetest.Install()

	code := m.Run()
	storagetest.Sweep(".")
	restoreStorageRoots()
	os.Exit(code)
}
