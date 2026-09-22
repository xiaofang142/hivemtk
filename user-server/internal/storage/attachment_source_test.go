package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearStorageEnv 把三个环境派生值恢复成"什么都没配"。
//
// 必须逐个显式 Unset：本包其余用例（以及整包并发跑时）可能已经把它们设上了，
// 只设自己想断的那一个键会得到一份依赖跑批顺序的"默认值"。
func clearStorageEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"STORAGE_LOCAL_BASE_DIR", "UPLOAD_DIR", "UPLOAD_FOLDER", "STORAGE_LOCAL_PUBLIC_URL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("清 %s 失败: %v", key, err)
		}
	}
}

func TestLocalSourceDefaults(t *testing.T) {
	clearStorageEnv(t)

	baseDir, publicURL, folder := LocalSource()
	if baseDir != "./uploads" {
		t.Errorf("默认磁盘根 = %q", baseDir)
	}
	if publicURL != "/files" {
		t.Errorf("默认公开 URL 前缀 = %q", publicURL)
	}
	if folder != "attachments" {
		t.Errorf("默认附件目录名 = %q", folder)
	}
	// 默认值必须与驱动自己的默认值同值：两处各写一份 "./uploads" 时，
	// 改一处就是"上传成功、外发时附件静静消失"。
	if d := NewLocalDriver("", ""); d.baseDir != filepath.Clean(baseDir) || d.publicBaseURL != publicURL {
		t.Errorf("LocalSource 默认 (%q,%q) 与 NewLocalDriver 默认 (%q,%q) 不一致",
			baseDir, publicURL, d.baseDir, d.publicBaseURL)
	}
}

func TestLocalSourceEnvPrecedence(t *testing.T) {
	clearStorageEnv(t)
	t.Setenv("STORAGE_LOCAL_BASE_DIR", "/data/storage")
	t.Setenv("UPLOAD_DIR", "/ignored")
	t.Setenv("UPLOAD_FOLDER", "files-attach")
	t.Setenv("STORAGE_LOCAL_PUBLIC_URL", "https://cdn.example.com/assets/")

	baseDir, publicURL, folder := LocalSource()
	if baseDir != "/data/storage" {
		t.Errorf("STORAGE_LOCAL_BASE_DIR 没压过 UPLOAD_DIR：%q", baseDir)
	}
	if folder != "files-attach" {
		t.Errorf("UPLOAD_FOLDER 没被采纳：%q", folder)
	}
	if publicURL != "https://cdn.example.com/assets" {
		t.Errorf("公开 URL 前缀没去掉尾斜杠：%q", publicURL)
	}

	// UPLOAD_DIR 自第三十八轮起不再参与解析：它从没进过 .env-example / docker-compose / deploy/，
	// 而除本函数外的四个本地盘读取点（storage/factory.go、service/init_storage.go、
	// service/channel_media.go、browser_automation/service/storage_util.go）从来只认
	// STORAGE_LOCAL_BASE_DIR → ./uploads。留着这一档就是"同一台机器两套根"，
	// 于是"controller 上传的文件公开链接 404"与"渠道媒体公开链接 404"必然二选一地发生。
	// 只配它 ⇒ 落到默认根，而不是悄悄换一棵树。
	clearStorageEnv(t)
	t.Setenv("UPLOAD_DIR", "/mnt/upload")
	if baseDir, _, _ := LocalSource(); baseDir != "./uploads" {
		t.Errorf("只配已退役的 UPLOAD_DIR 时磁盘根 = %q，期望仍为默认 ./uploads", baseDir)
	}
}

func TestLocalAttachmentSourceMatchesUploadLayout(t *testing.T) {
	clearStorageEnv(t)
	t.Setenv("STORAGE_LOCAL_BASE_DIR", "/data/storage")

	dir, urlPrefix := LocalAttachmentSource()
	if want := filepath.Join("/data/storage", "attachments"); dir != want {
		t.Errorf("附件根 = %q，期望 %q（上传侧写的是 {baseDir}/{folder}/{yyyy}/{mm}/…）", dir, want)
	}
	if urlPrefix != "/files" {
		t.Errorf("公开 URL 前缀 = %q", urlPrefix)
	}

	// 目录名改了就必须跟着改：外发侧认的前缀段来自同一个值，各写一份就会漂移到"永远挂不上"。
	clearStorageEnv(t)
	t.Setenv("UPLOAD_FOLDER", "mail-attach")
	if dir, _ := LocalAttachmentSource(); filepath.Base(dir) != "mail-attach" {
		t.Errorf("UPLOAD_FOLDER 没进到附件根：%q", dir)
	}
}

// TestUploadHandlerReadsEnvThroughLocalSource 静态锁：上传侧不再自己读那三个键。
//
// 外发侧的附件根必须由上传侧同一份派生逻辑得到，否则 STORAGE_LOCAL_BASE_DIR 一改
// 就是"上传成功、发信时附件静静消失"。这一格与排水循环是同一个病灶的形状：
// 值在、两处各算各的。
func TestUploadHandlerReadsEnvThroughLocalSource(t *testing.T) {
	src := readNonCommentLines(t, "../controller/upload.go")
	if got := countLinesContaining(src, "LocalSource()"); got != 1 {
		t.Errorf("upload.go 里 LocalSource() 命中 %d 次，期望恰好 1 次 ⇒ 环境派生值又有了第二份实现", got)
	}
	for _, key := range []string{`os.Getenv("UPLOAD_FOLDER")`, `os.Getenv("STORAGE_LOCAL_BASE_DIR")`, `os.Getenv("UPLOAD_DIR")`, `os.Getenv("STORAGE_LOCAL_PUBLIC_URL")`} {
		if got := countLinesContaining(src, key); got != 0 {
			t.Errorf("upload.go 仍自己读 %s（%d 次）⇒ 与外发侧的根不再同源", key, got)
		}
	}
}

// TestFilesRouteReadsEnvThroughLocalSource 静态锁：/files 托管侧与上传侧同源。
//
// 与上一条同形状，只是被锁的文件换成路由侧。路由侧的行为测试
// （router/files_guard_test.go）只能断"挂的这棵树取得到文件"，
// 在"路由自己抄一份解析、抄的键恰好等价"时不红 —— 那一格由这里负责。
func TestFilesRouteReadsEnvThroughLocalSource(t *testing.T) {
	src := readNonCommentLines(t, "../router/files_guard.go")
	if got := countLinesContaining(src, "LocalSource()"); got != 1 {
		t.Errorf("files_guard.go 里 LocalSource() 命中 %d 次，期望恰好 1 次 ⇒ 托管根又有了第二份实现", got)
	}
	for _, key := range []string{`os.Getenv("STORAGE_LOCAL_BASE_DIR")`, `os.Getenv("UPLOAD_DIR")`, `os.Getenv("UPLOAD_FOLDER")`, `os.Getenv("STORAGE_LOCAL_PUBLIC_URL")`} {
		if got := countLinesContaining(src, key); got != 0 {
			t.Errorf("files_guard.go 仍自己读 %s（%d 次）⇒ 与上传侧的根不再同源", key, got)
		}
	}
}

func readNonCommentLines(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func countLinesContaining(lines []string, needle string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(strings.ReplaceAll(line, " ", ""), needle) {
			n++
		}
	}
	return n
}
