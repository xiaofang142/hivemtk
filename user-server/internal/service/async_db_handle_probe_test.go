// async_db_handle_probe_test.go —— fire-and-forget 的异步体不得读 pkg/db 的包级全局句柄。
//
// 为什么要有这条：异步投递一旦被编译成"在 goroutine 里读包级全局句柄"，那次读就可能与
// 别人换库的 db.SetTestDB() 撞在同一地址上。本包有 100+ 处 `db.SetTestDB(`、40 处 `go func(`
// （`grep -rho 'X' internal/service/*_test.go | wc -l` 现算，含注释里的提及，只会随用例增加而涨），
// 两者一旦重叠就是数据竞争 —— 而重叠与否
// 取决于机器快慢：-race 在 TestE2E_WebChat_* 之间红过（写方 web_chat_e2e_test.go:160，读方经
// session_chain.go 的异步体落到 repository/csat.go:23），本地按那两条用例单跑两遍却全绿。
// 偶发红当不了回归门，这三条探针把窗口撑成"必然重叠"：每派一个异步任务就立刻从本用例的
// goroutine 回写一次全局句柄。
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

// 同一形状的第三个站点：DispatchSessionEventAsync 的异步体在 goroutine 里 new 出规则引擎服务，
// 而那次构造把全局句柄读走十余次（NewAutomationRuleRepository ＋ NewCustomerServicePlusService
// 名下 8 个子仓储 ＋ NewEmailServiceAuto 里的账号仓储与 NewMessageHubService）。
// 这一枚与前两枚不同：它不是"照形状顺手钉住"，而是整包 -race 在**已提交字节**的干净克隆上
// 实测到的红——TestCreateSession_AllowDifferentPlatform 与 TestCreateSession_AnonymousUser
// 各红一次（红因 testing.go:1712: race detected during execution of test，即" victim 用例"，
// 真正的读方在异步体里）；写方是 customer_session_blacklist_test.go 的 db.SetTestDB。
// 同两条用例单跑 -count=4（16 次）与三连发 -count=25（75 次）都全绿 ⇒ 重叠与否仍取决于调度，
// 故按前两枚的形状把窗口撑成必然重叠。
//
// 同一趟整包里还照出**第四个**站点、本文件没有为它补腿：MaybeSendAwayReply
// （customer_service_plus.go:479 起协程，异步体经 OfficeHoursService.GetConfig 在 office_hours.go:45
// 逐次 new 仓储）。它挪不动——GetOfficeHoursService 是 sync.Once 单例，把构造挪到 spawning 之前
// 只是把首次读全局的那一刻提前，测试里还会把首个句柄冻进单例（比竞争更糟）。它的守卫是 accessor
// 那把锁，回归腿见 pkg/db 的 TestConcurrentGetDBAndSetTestDBAreRaceFree。
func TestSessionEventDispatchResolvesDBHandleSynchronously(t *testing.T) {
	cur := testutil.NewTestDB(t, &model.AutomationRule{})
	db.SetTestDB(cur)
	if got := db.GetDB(); got != cur {
		t.Fatalf("全局句柄未生效：got=%p want=%p", got, cur)
	}
	const rounds = 300
	for i := 0; i < rounds; i++ {
		// 本探针只迁移 automation_rules 且它是空的：异步体第一步 ListEnabledRulesByEvent
		// 恒回零条 ⇒ 一条规则都不执行 ⇒ 只读不写，不给测试库留行、不污染别家的计数断言。
		sessionID := fmt.Sprintf("dispatch-handle-probe-%d-%d", time.Now().UnixNano(), i)
		DispatchSessionEventAsync(RuleEventConversationCreated, sessionID,
			&model.CustomerSession{SessionID: sessionID})
		db.SetTestDB(cur)
	}
}
