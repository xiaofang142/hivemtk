package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 批I：媒体转存"活路径"（channelMediaPersist → storeInboundMedia → 存储驱动）的首批用例。
//
// 起点是 N-25 死注册表的删除（channelMediaFollowers 一整套，见审计文档 §20.1）。删完之后按
// 「删除前先证明未使用」的规矩补了一刀反向探针：把 channelMediaPersist 整体短路成
// `return "", nil`（等价于"转存静默不干活"），`-run 'Media|Backfill'` 28 条用例 **全绿**
// （证据 batchI/batchI-I1-survived.log）。原因很具体：八家渠道各自的媒体用例都把
// `xxMediaStoreFn` 这个缝换成了假的（只记参数、不落盘），于是装配语句
// `xxMediaStoreFn = channelMediaPersist` 右半边那条真链路 —— obs 配置查询、
// 本地/云驱动选择、目录与文件名组装、落盘失败上报 —— 一条断言都没挨过。
// 本文件钉的就是这四格，每格各配一刀变异（§20.3）。
//
// ⚠️ 本文件的用例会写包级全局 DB 句柄（db.SetTestDB），因为 storeInboundMedia 走的是
// dbForStorage→dbutil.GetDB()（它没有 ctx-carried db 可读）。每条都在 cleanup 里成对还原，
// 见 [[platform-test-global-db-handle-leak]] 那条"复用全局句柄会串到后续用例"的教训。

// batchIGlobalDB 铺一个只含 obs_config 表的测试库并接管全局句柄（用例结束自动还原）。
func batchIGlobalDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.ObsConfig{})
	prev := db.GetDB()
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(prev) })
	return database
}

// batchIObjectKey 把公开 URL 折回存储相对路径（本地驱动 = 去掉 publicBase 前缀）。
func batchIObjectKey(t *testing.T, url, publicBase string) string {
	t.Helper()
	if !strings.HasPrefix(url, publicBase+"/") {
		t.Fatalf("URL %q 不在公开前缀 %q 之下", url, publicBase)
	}
	return strings.TrimPrefix(url, publicBase+"/")
}

// TestBatchI_ChannelMediaPersistWritesUnderChannelFolder 兜底存储那一格：
// 没有 is_default 配置时按 STORAGE_LOCAL_BASE_DIR 落本地盘，URL/目录/字节都要对。
func TestBatchI_ChannelMediaPersistWritesUnderChannelFolder(t *testing.T) {
	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)
	batchIGlobalDB(t)

	payload := []byte("\x89PNG\r\n\x1a\nbatchI-payload")
	url, err := channelMediaPersist(context.Background(), string(ChannelQQ), "media-a", payload, "image/png", "")
	if err != nil {
		t.Fatalf("channelMediaPersist 失败: %v", err)
	}
	if url == "" {
		t.Fatal("返回空 URL：媒体没落盘，调用方会保留占位符（客户侧永远看不到这张图）")
	}
	const publicBase = "/files"
	if !strings.HasPrefix(url, publicBase+"/channels/"+string(ChannelQQ)+"/") {
		t.Fatalf("URL 前缀错: %q，期望 %q", url, publicBase+"/channels/"+string(ChannelQQ)+"/")
	}
	// 驱动契约（internal/storage/local.go 文件头）：{baseDir}/{folder}/{yyyy}/{mm}/{uuid}.{ext}
	// 日期目录由驱动自己加，调用方只能给 channels/<渠道>。
	if segs := strings.Split(strings.TrimPrefix(url, publicBase+"/channels/"+string(ChannelQQ)+"/"), "/"); len(segs) != 3 {
		t.Fatalf("channels/%s 之后应是 3 段（yyyy/mm/文件名），实得 %d 段 %q —— 日期目录被拼了两次", string(ChannelQQ), len(segs), segs)
	}
	disk := filepath.Join(base, filepath.FromSlash(batchIObjectKey(t, url, publicBase)))
	got, readErr := os.ReadFile(disk)
	if readErr != nil {
		t.Fatalf("URL 指向的文件不在盘上: %v", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("落盘字节与入参不等：%d vs %d 字节", len(got), len(payload))
	}
	if ext := filepath.Ext(disk); ext != ".png" {
		t.Fatalf("文件名应带由 contentType 推断的扩展名，实得 %q（无扩展名的文件在浏览器里不按图片渲染）", ext)
	}

	// 落库/回给上层的载荷走的是 mediaPersistRecord（channelMediaPersist 只透传 URL），
	// 所以记录字段要在这一格里各断一次，否则"存对了文件、报错了类型/大小"没人拦。
	rec, recErr := storeInboundMedia(context.Background(), payload, "image/png", string(ChannelQQ), "media-a-rec", "")
	if recErr != nil {
		t.Fatalf("storeInboundMedia 失败: %v", recErr)
	}
	if rec.MimeType != "image/png" {
		t.Fatalf("mediaPersistRecord.MimeType = %q，期望原样带回 image/png（上层据此判定能否按图片渲染）", rec.MimeType)
	}
	if rec.Size != int64(len(payload)) {
		t.Fatalf("mediaPersistRecord.Size = %d，期望 %d", rec.Size, len(payload))
	}
	if rec.PublicURL == "" {
		t.Fatal("mediaPersistRecord.PublicURL 为空")
	}
}

// TestBatchI_ChannelMediaPersistUsesConfiguredDefaultStorage 配置命中那一格：
// 有 is_default=local 记录时必须按记录的 Endpoint/Domain 走，而不是退回环境变量兜底。
func TestBatchI_ChannelMediaPersistUsesConfiguredDefaultStorage(t *testing.T) {
	configured := t.TempDir()
	envFallback := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", envFallback)
	database := batchIGlobalDB(t)

	const domain = "https://cdn.example/media"
	cfg := &model.ObsConfig{
		Name: "批I 用例默认存储", Provider: model.ObsProviderLocal,
		Endpoint: configured, Domain: domain, IsDefault: true, Status: model.ObsStatusActive,
	}
	if err := repository.NewObsConfigRepositoryWithDB(database).Create(context.Background(), cfg); err != nil {
		t.Fatalf("铺 obs_config 失败: %v", err)
	}

	url, err := channelMediaPersist(context.Background(), string(ChannelFeishu), "media-b", []byte("body-b"), "text/plain", "note.txt")
	if err != nil {
		t.Fatalf("channelMediaPersist 失败: %v", err)
	}
	if !strings.HasPrefix(url, domain+"/channels/"+string(ChannelFeishu)+"/") {
		t.Fatalf("URL 用了错误的公开域: %q（配置里的 Domain 被忽略 ⇒ 兜底环境变量在替它说话）", url)
	}
	if _, statErr := os.Stat(filepath.Join(configured, filepath.FromSlash(batchIObjectKey(t, url, domain)))); statErr != nil {
		t.Fatalf("文件没落在配置的 Endpoint 下: %v", statErr)
	}
	if entries, _ := os.ReadDir(envFallback); len(entries) != 0 {
		t.Fatal("兜底目录被写入：说明查询到的默认配置没生效")
	}
}

// TestBatchI_ChannelMediaPersistReportsUploadFailure 两条失败臂各自上报那一格：
//   - 驱动选不出来（provider 不认识）→ 包装成 "storage factory: ..."
//   - 驱动选了但传不上去（云 SDK 尚未接通）→ 包装成 "upload media: ..."
//
// 两臂都必须把 error 交回去，让调用方保留占位符并告警；返回 ("", nil) 会让上层把
// "没存上"当成"存成了空 URL"，客户侧就是一条永久没有附件的消息。
func TestBatchI_ChannelMediaPersistReportsUploadFailure(t *testing.T) {
	cases := []struct {
		name     string
		provider model.ObsProvider
		wantWrap string
	}{
		{"云驱动未接通", model.ObsProviderAliyun, "upload media"},
		{"provider 不认识", model.ObsProvider("nonsense"), "storage factory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database := batchIGlobalDB(t)
			cfg := &model.ObsConfig{
				Name: "批I 用例失败存储", Provider: tc.provider,
				Bucket: "b", Region: "r", Endpoint: "e", IsDefault: true, Status: model.ObsStatusActive,
			}
			if err := repository.NewObsConfigRepositoryWithDB(database).Create(context.Background(), cfg); err != nil {
				t.Fatalf("铺 obs_config 失败: %v", err)
			}

			url, err := channelMediaPersist(context.Background(), string(ChannelTelegram), "media-c", []byte("body-c"), "image/jpeg", "")
			if err == nil {
				t.Fatalf("转存失败却返回 nil error（url=%q）：失败被吞，上层无从降级", url)
			}
			if url != "" {
				t.Fatalf("失败时不应给出 URL，实得 %q", url)
			}
			if !strings.Contains(err.Error(), tc.wantWrap) {
				t.Fatalf("错误应带 %q 这一层上下文包装，实得 %v", tc.wantWrap, err)
			}
		})
	}
}

// TestBatchI_FilenameHintOutranksMediaType 文件名臂那一格：hint 带扩展名时用 hint 的，
// 而不是由 contentType 反推。两条臂的差别只在扩展名（落盘主体是 uuid），而扩展名恰恰是
// 播放器/浏览器判渲染方式的唯一线索 —— 语音 .amr 被改写成 .ogg 就是"存下来了但放不出来"。
func TestBatchI_FilenameHintOutranksMediaType(t *testing.T) {
	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)
	batchIGlobalDB(t)

	url, err := channelMediaPersist(context.Background(), string(ChannelWechat), "3cbbbdeaf0cdec1d",
		[]byte("amr-body"), "audio/ogg", "voice.amr")
	if err != nil {
		t.Fatalf("channelMediaPersist 失败: %v", err)
	}
	key := batchIObjectKey(t, url, "/files")
	if ext := filepath.Ext(key); ext != ".amr" {
		t.Fatalf("落盘扩展名 = %q（URL %q）：期望沿用文件名 hint 的 .amr，说明 hint 被 contentType 顶掉了", ext, url)
	}
	if _, statErr := os.Stat(filepath.Join(base, filepath.FromSlash(key))); statErr != nil {
		t.Fatalf("文件不在盘上: %v", statErr)
	}
}

// TestBatchI_ExtFromContentType 扩展名推断那一格（storeInboundMedia 里唯一影响文件名的分支）。
func TestBatchI_ExtFromContentType(t *testing.T) {
	cases := []struct {
		name string
		ct   string
		want string
	}{
		{"已知图片", "image/png", ".png"},
		{"已知文档", "application/pdf", ".pdf"},
		{"未知类型退回 bin", "application/octet-stream", ".bin"},
		{"空类型退回 bin", "", ".bin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extFromContentType(tc.ct); got != tc.want {
				t.Fatalf("extFromContentType(%q) = %q，期望 %q", tc.ct, got, tc.want)
			}
		})
	}
}
