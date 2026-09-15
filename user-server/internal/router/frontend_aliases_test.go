package router

import (
	"strings"
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// TestFrontendAliases_NoFailedRegistration 守护前端兼容别名层的注册成功率。
//
// 背景（2026-09-16 审计发现）：`setupFrontendAliases` 的 doReg/doRegAdmin 用
// recover 吞掉 gin 的重复注册 panic，其中 doReg 更是 `_ = r` **完全静默**。
// 实测每次启动有 **66 处**别名注册失败而无人知晓，其中 5 处是真实的 path 冲突
// （`/llm/models/:id*` 与权威路由 `/llm/models/:name` 通配符冲突），
// 说明那 5 个别名**从未生效**。
//
// 冗余注册本身无害，但「注册失败」与「注册成功」必须可区分，否则别名层
// 一旦因为权威路由改名而失效，不会有任何信号。本测试即断言该信号为零：
// 若失败，说明新增了冗余别名（应删除）或存在真实冲突（应修 path）。
func TestFrontendAliases_NoFailedRegistration(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	if len(aliasRegFailures) == 0 {
		return
	}
	t.Errorf("有 %d 处前端兼容别名注册失败：\n    %s\n"+
		"处置：冗余注册请删除该行；path 冲突/拼写错误请修正路径。",
		len(aliasRegFailures), strings.Join(aliasRegFailures, "\n    "))
}
