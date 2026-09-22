// attachment_source.go 本地存储的三个环境派生值 —— 上传侧、外发侧与 /files 托管侧必须同源。
//
// 单独成文件的理由：外发侧要按"上传接口落盘的形状"把粘贴进来的 URL 换回磁盘路径，
// 而这三件事（磁盘根、公开 URL 前缀、目录名）此前只在 controller/upload.go 里内联算过一遍。
// 外发侧再抄一遍就成了两个事实源 —— STORAGE_LOCAL_BASE_DIR 一改，表现不是报错，
// 是"上传成功、发信时附件静静消失，而邮件状态写着已发送"。
// 第三十八轮把 /files 的托管根（router.RegisterFilesRoute）也收进这同一份解析：
// 当时它自己抄了一遍，抄漏的那一档让"写到 $UPLOAD_DIR、从 ./uploads 公开"成为 404。
package storage

import (
	"os"
	"path/filepath"
	"strings"
)

// LocalSource 按上传侧的既有优先级给出本地存储的三个派生值：
// 磁盘根（STORAGE_LOCAL_BASE_DIR → ./uploads）、
// 公开 URL 前缀（STORAGE_LOCAL_PUBLIC_URL → /files）、
// 附件目录名（UPLOAD_FOLDER → attachments）。
//
// 磁盘根这里只留两档。历史上中间还夹着一档 `UPLOAD_DIR`，而全仓另外四个本地盘读取点
// （storage/factory.go、service/init_storage.go、service/channel_media.go、
// browser_automation/service/storage_util.go）从来只认 STORAGE_LOCAL_BASE_DIR → ./uploads：
// 两档链与三档链并存时，"公开链接 404"必然在 controller 上传与渠道媒体之间二选一地发生
// （谁跟另一侧不同源，谁的文件就没人托管）。该键既不在 .env-example、也不在
// docker-compose.yml 与 deploy/，退役它不影响任何在本仓里能复现的部署形态。
// 要换本地盘根，配 STORAGE_LOCAL_BASE_DIR。
func LocalSource() (baseDir, publicURLPrefix, attachmentFolder string) {
	baseDir = os.Getenv("STORAGE_LOCAL_BASE_DIR")
	if baseDir == "" {
		baseDir = "./uploads"
	}

	attachmentFolder = os.Getenv("UPLOAD_FOLDER")
	if attachmentFolder == "" {
		attachmentFolder = "attachments"
	}

	publicURLPrefix = os.Getenv("STORAGE_LOCAL_PUBLIC_URL")
	if publicURLPrefix == "" {
		publicURLPrefix = "/files"
	}
	return baseDir, strings.TrimRight(publicURLPrefix, "/"), attachmentFolder
}

// LocalAttachmentSource 给出邮件外发可挂载附件的磁盘根，以及它在公网上的 URL 前缀。
//
// 返回的是上传侧真正写入的那棵树（{baseDir}/{folder}），不是 /files 路由托管的那棵：
// 附件要挂的是文件本身，而文件由本函数同源的那组环境变量决定落在哪里。
func LocalAttachmentSource() (dir, urlPrefix string) {
	baseDir, publicURLPrefix, folder := LocalSource()
	return filepath.Join(baseDir, folder), publicURLPrefix
}
