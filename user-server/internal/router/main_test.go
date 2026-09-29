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

	"hivemtk-user/internal/storage/storagetest"
)

// TestMain router 包测试统一入口
func TestMain(m *testing.M) {
	restoreStorageRoots := storagetest.Install()

	code := m.Run()
	storagetest.Sweep(".")
	restoreStorageRoots()
	os.Exit(code)
}
