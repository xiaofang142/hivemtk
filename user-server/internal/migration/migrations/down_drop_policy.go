package migrations

import (
	"log"
	"strings"
)

// declineDrop 是三类「被主动放弃的降级删除」的共同出口。
//
// 本项目的建表事实源是 GORM AutoMigrate（internal/pkg/db/migrate.go），版本化迁移只是其上的增量层：
// 同一张表、同一批列与索引既被模型声明、又被若干迁移用 IF NOT EXISTS 语句"创建"过。
// 于是迁移的 Down 无法在写代码时区分"这是我 Up 建的"和"这是模型在用的"——
// 而 `POST /api/migration/rollback` 一次调用就会把后者一并销毁。
//
// 两类代价不对称：多留一个对象不破坏任何功能（升级路径靠 IF NOT EXISTS 幂等，
// 闲置的可空列与索引只是占位），删掉一个在用对象则当前进程永不补回（AutoMigrate 只在启动时跑），
// 表与列是数据丢失、索引是长期 seq scan。故 Down 一律只撤销自己 Up 新建的对象。
//
// 判据在 a_full_chain_migration_test.go：逐迁移比对「Down 删掉的表/列/索引 ⊆ 该迁移 Up 新建的」，
// 并核一轮 Up→Down 后基线对象不得净丢失。
func declineDrop(version, kind string, objects ...string) {
	if len(objects) == 0 {
		return
	}
	log.Printf("[migration %s] 放弃删%s %s：由模型/AutoMigrate 持有，降级不销毁在用对象",
		version, kind, strings.Join(objects, ","))
}

func declineTableDrop(version string, tables ...string)   { declineDrop(version, "表", tables...) }
func declineColumnDrop(version string, columns ...string) { declineDrop(version, "列", columns...) }
func declineIndexDrop(version string, indexes ...string)  { declineDrop(version, "索引", indexes...) }
