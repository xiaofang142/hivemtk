package migrations

import (
	"log"
	"strings"
)

// declineTableDrop 记录一次「被主动放弃的删表」。
//
// 本项目的建表事实源是 GORM AutoMigrate（internal/pkg/db/migrate.go），版本化迁移只是其上的增量层，
// 于是这些表同时被当前代码的模型声明：降级后应用照样读写它们。实测一轮 Up→Down 会净删 53 张基线表，
// 而 `POST /api/migration/rollback` 一次调用就能触发其中任意一张——那不是回退，是销毁在用数据。
// 多留一张表不破坏任何功能（升级路径靠 IF NOT EXISTS 幂等），删掉一张在用表则不可逆。
//
// 因此 Down 只撤销自己 Up 新建的对象；其余删表一律改成这里的日志。
// 判据在 a_full_chain_migration_test.go：逐迁移比对「Down 删掉的表 ⊆ 该迁移 Up 新建的表」。
func declineTableDrop(version string, tables ...string) {
	if len(tables) == 0 {
		return
	}
	log.Printf("[migration %s] 放弃删表 %s：由模型/AutoMigrate 持有，降级不销毁在用数据",
		version, strings.Join(tables, ","))
}
