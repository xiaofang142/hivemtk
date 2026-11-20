// kb_crawler_register.go T4 三张表的建表登记。
//
// 为什么在这里而不是 internal/model：model 包不能 import internal/pkg/db
// （db.migrate.go 反向 import model 成环——本仓既有判例 KBDocumentChunkRow
// 就因 import 环用 RegisterExtraModels，见 repository/kb_document_chunk_repo.go）。
// 生产建表只跑 AutoMigrate（kb_release.go 文件头 T-P9-02 实测口径）。
package service

import (
	internalmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

func init() {
	db.RegisterExtraModels(
		&internalmodel.KBCrawlerSource{},
		&internalmodel.KBCrawlBatch{},
		&internalmodel.KBCrawlPage{},
		&internalmodel.QACandidate{},
	)
}
