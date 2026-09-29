// Package controller 的测试统一入口（TestMain）
//
// 知识库导入用例（知识文本/URL/文件导入）走的是 service 里的相对路径 uploads/,
// 而 go test 的 CWD 就是本包目录 —— 于是每跑一轮就往
// internal/aiagent/knowledge/controller/uploads/ 里堆一批（历史峰值 1550 个文件，
// .gitignore 里甚至有人专门为它加过一条规则）。这里在所有用例前把三棵存储根
// 指进临时目录，收尾再 Sweep 一次兜底。
package controller

import (
	"os"
	"testing"

	"hivemtk-user/internal/storage/storagetest"
)

// TestMain knowledge/controller 包测试统一入口
func TestMain(m *testing.M) {
	restoreStorageRoots := storagetest.Install()

	code := m.Run()
	storagetest.Sweep(".")
	restoreStorageRoots()
	os.Exit(code)
}
