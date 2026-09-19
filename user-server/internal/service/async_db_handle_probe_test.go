// async_db_handle_probe_test.go —— fire-and-forget 的异步体不得读 pkg/db 的包级全局句柄。
//
// 为什么要有这条：异步投递一旦被编译成"在 goroutine 里读包级全局句柄"，那次读就可能与
// 别人换库的 db.SetTestDB() 撞在同一地址上。本包有 100 处 SetTestDB 调用（用例各自换库）、
// 28 处 `go func()`，两者一旦重叠就是数据竞争 —— 而重叠与否取决于机器快慢：-race 在
// TestE2E_WebChat_* 之间红过（写方 web_chat_e2e_test.go:160，读方经 session_chain.go 的异步体
// 落到 repository/csat.go:23），本地按那两条用例单跑两遍却全绿。偶发红当不了回归门，
// 这两条探针把窗口撑成"必然重叠"：每派一个异步任务就立刻从本用例的 goroutine 回写一次全局句柄。
//
// 回写的是本用例自己刚设进去的那个句柄（cur），语义零变化：本包无 t.Parallel()、用例顺序
// 执行，写回自己读到的值不影响别的人；被断言的从来不是值，是"有没有并发读写同一地址"。
package service

import (
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSessionChainCSATResolvesDBHandleSynchronously(t *testing.T) {
	cur := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.CSATSurvey{},
	)
	db.SetTestDB(cur)
	if got := db.GetDB(); got != cur {
		t.Fatalf("全局句柄未生效：got=%p want=%p", got, cur)
	}
	svc := NewSessionChainService()
	const rounds = 300
	for i := 0; i < rounds; i++ {
		// 不存在的 sessionID：异步体第一步 GetBySessionID 即失败返回，只读不写，
		// 不会给测试库留下行，也就不会污染别的用例的计数断言。
		sess := &model.CustomerSession{SessionID: fmt.Sprintf("csat-handle-probe-%d-%d", time.Now().UnixNano(), i)}
		svc.TriggerCSATOnClose(sess)
		db.SetTestDB(cur)
	}
}

// 同一形状的第二个站点：fallbackVersionOf 的异步版本刷新也在 goroutine 里读全局句柄。
// 它没在 CI 红过（窗口比 CSAT 那条窄），但形状逐字相同，一起钉住比逐条追红便宜。
func TestFallbackVersionResolvesDBHandleSynchronously(t *testing.T) {
	cur := testutil.NewTestDB(t, &model.ScriptLibrary{})
	db.SetTestDB(cur)
	const rounds = 300
	for i := 0; i < rounds; i++ {
		// 不存在的模板 id（大主键，避开别的用例的自增段）：MaxScriptVersion 查不到 ⇒
		// versionCache 不被写 ⇒ 每一轮都真的派出一次异步刷新。
		fallbackVersionOf(uint(9_000_000 + i))
		db.SetTestDB(cur)
	}
}
