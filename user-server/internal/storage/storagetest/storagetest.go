// storagetest.go 测试期的存储根注入与收尾清扫。
//
// go test 的 CWD 是被测包目录，而上传/备份/还原暂存三棵树历史上都是相对 CWD 的字面量，
// 于是每跑一轮测试就往源码树里留一份（internal/service/backups 一度积到 490 个目录，
// internal/aiagent/knowledge/controller/uploads 积到 1550 个文件）。根因侧已由
// storage.LocalSource / storage.BackupSource 收口成可配置，本包负责测试侧的两件事：
//
//  1. Install：把三个根指进同一个临时目录，让测试默认根本不碰源码树；
//  2. Sweep：m.Run() 之后扫掉本轮仍落在包目录里的残留。
//
// Sweep 是兜底不是主力 —— 个别用例会自己 Setenv/Unsetenv 环境变量，中途把注入值摘掉，
// 摘掉之后就回退到相对路径、又开始写包目录。Sweep 跑在所有断言之后，不影响任何断言。
//
// 仅被 _test.go 引用。
package storagetest

import (
	"os"
	"path/filepath"
	"strings"
)

// rootDirs 与 envKeys 按下标一一对应：每棵树有自己的档，互不混用。
// 目录名与 storage.LocalSource / storage.BackupSource 的三个默认值同名，
// 只是被挪到了同一个临时根之下。
var (
	rootDirs = []string{"uploads", "backups", "restore_tmp"}
	envKeys  = []string{"STORAGE_LOCAL_BASE_DIR", "BACKUP_BASE_DIR", "RESTORE_TMP_DIR"}
)

// Install 建一个临时根，把三个存储环境变量分别指到根下的三棵子树，返回还原函数。
//
// TestMain 里在 m.Run() 之前调用拿到 restore，在 os.Exit 前调用 restore —— 还原原值
// 并删掉临时根。原本未设置的环境变量还原成"未设置"而不是空串，免得空串与未设置
// 在后续判空之外的逻辑里产生分歧。
//
// 三棵树各自成子目录而不是共用根：restore_tmp 的用例会断言目录权限为 0700
// （MkdirAll 新建出来的），共用根会被 MkdirTemp 的 0700 之外的东西污染；
// 共用也会让 uploads 的文件与 backups 的备份混在一层，排查时看不清是谁写的。
func Install() (restore func()) {
	root, err := os.MkdirTemp("", "hivemtk-storagetest-")
	if err != nil {
		// 建不出临时根就什么都不注入：让测试按原样跑，Sweep 仍会兜住残留。
		return func() {}
	}

	prev := make([]string, len(envKeys))
	had := make([]bool, len(envKeys))
	for i, k := range envKeys {
		prev[i], had[i] = os.LookupEnv(k)
		_ = os.Setenv(k, filepath.Join(root, rootDirs[i]))
	}

	return func() {
		for i, k := range envKeys {
			if !had[i] {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, prev[i])
			}
		}
		_ = os.RemoveAll(root)
	}
}

// Sweep 删掉 pkgDir 里本轮测试落下的三棵树（uploads / backups / restore_tmp）。
//
// 删之前按绝对路径校验目标必须严格位于 pkgDir 之内，另挡一道 pkgDir 本身是
// user-server 根目录的情况 —— 那里的 backups/ 是运行时真实备份，误删不可恢复。
// 校验不过就整轮跳过，宁可少删也不冒险。
func Sweep(pkgDir string) {
	abs, err := filepath.Abs(pkgDir)
	if err != nil || abs == "" {
		return
	}
	if filepath.Base(abs) == "user-server" {
		return
	}

	for _, name := range rootDirs {
		target := filepath.Join(abs, name)
		if !strictlyWithin(target, abs) {
			continue
		}
		if _, err := os.Lstat(target); err != nil {
			continue
		}
		_ = os.RemoveAll(target)
	}
}

// strictlyWithin 报告 target 是否严格位于 dir 之下（dir 本身不算）。
func strictlyWithin(target, dir string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
