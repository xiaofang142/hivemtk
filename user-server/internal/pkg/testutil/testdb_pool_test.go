// testdb_pool_test.go 钉住本辅助开出的每个句柄**必须带连接池上界**。
//
// 为什么这条腿必须存在：上界是 CI 侧 `FATAL: sorry, too many clients already
// (SQLSTATE 53300)` 的唯一缓解，而它不改变任何业务断言的通过与否 —— 三枚 async_db_handle
// 探针在有无上界两格下都照旧 PASS（实测见 testdb.go 的那段注释）。也就是说，删掉那两行
// `SetMaxOpenConns/SetMaxIdleConns` 之后，全套用例仍然绿，只有 CI 会在下一次整片跑时
// 重新红成一条与代码无关的连接数错误。这正是"只有注释在守"的形状。
//
// 判据取运行时的 `sql.DB.Stats().MaxOpenConnections` 而不是 grep 源码：去掉上界时
// database/sql 的取值是 **0**（0＝不限），"没设上界"与"上界被改成别的数"在同一条断言上红，
// 而 grep 只能看见其中一种。
//
// 期望值这里刻意写**字面量**而不引用 `testDBMaxOpenConns`：腿若与被测常量同源，那两行
// `SetMaxOpenConns(...)` 被摘掉时用例会连包一起编译不过（`undefined`），拿到的是"编译红"
// 而不是"这条性质没了"。字面量换来的是可判的红因，代价是改上界时要两处同步——那是有意为之的
// 一次对话（32 这个数的论证写在 testdb.go，改它的人得先读完那段）。
package testutil

import (
	"testing"
)

const wantTestDBMaxOpenConns = 32

func TestNewTestDBBoundsPool(t *testing.T) {
	sqlDB, err := NewTestDB(t).DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != wantTestDBMaxOpenConns {
		t.Errorf("测试库句柄的 MaxOpenConns = %d，期望 %d（0 表示根本没设上界，database/sql 里 0＝不限）",
			got, wantTestDBMaxOpenConns)
	}
}
