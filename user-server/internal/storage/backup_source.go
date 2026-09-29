// backup_source.go 备份根与还原暂存根的两个环境派生值 —— 创建侧与还原侧必须同源。
//
// 单独成文件的理由与 attachment_source.go 相同：这两条路径此前把 "backups"、
// "restore_tmp" 相对字面量内联写死在 service/backup.go 里，全仓没有任何一档可以
// 覆盖它。于是 go test（CWD = 包目录）每跑一轮，就往源码树里留 490 个备份目录与
// 一份还原暂存 data.json —— 只能靠人工删，删完下一轮又长回来。
//
// 两档链与存储侧同构：env → 默认值。默认值与历史内联字面量逐字节等价
// （filepath.Join("./backups", x) 与 filepath.Join("backups", x) 同解），
// 因此不配任何环境变量时生产行为不变。
package storage

import "os"

// BackupSource 按既有优先级给出备份根与还原暂存根：
// 备份根（BACKUP_BASE_DIR → ./backups）、
// 还原暂存根（RESTORE_TMP_DIR → ./restore_tmp）。
//
// 还原暂存根要单独一档而不是复用备份根：备份是长期保留的产物，暂存是解压过程中
// 含明文数据的短命目录（权限 0600/0700），两者生命周期与安全要求都不同，
// 混成一棵树会让"清临时目录"误伤历史备份。
//
// 要换落盘位置，配 BACKUP_BASE_DIR / RESTORE_TMP_DIR。
func BackupSource() (backupDir, restoreTmp string) {
	backupDir = os.Getenv("BACKUP_BASE_DIR")
	if backupDir == "" {
		backupDir = "./backups"
	}
	restoreTmp = os.Getenv("RESTORE_TMP_DIR")
	if restoreTmp == "" {
		restoreTmp = "./restore_tmp"
	}
	return backupDir, restoreTmp
}
