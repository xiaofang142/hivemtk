// password_reset_token_plaintext_startup_test.go 钉住"改哈希后，存量明文行必须真的从库里消失"。
//
// 为什么必须有这一道：校验改成"先哈希再查"之后，旧那批 72 字符明文行**永远匹配不上**，
// 留着它们既没功能价值、又是可读的凭证（备份文件、SQL 日志、只读副本都还在照抄这张表）。
// 版本化迁移链在当前启动口径下永不执行（建表真值是 AutoMigrate），所以作废动作挂在
// AutoMigrate 的 post-migrate 钩子上，与 obs_config / opportunities 那两道同形状。
//
// 三条腿各管一件事：
//  1. 明文行被**硬删**（模型带 DeletedAt，走 GORM Delete 只是打软删标记 ⇒ 原文仍在表里，等于没修）；
//  2. 合规行（64 字符十六进制）不受影响，且钩子可重跑；
//  3. 装配点只调用一次（钩子写了没人调＝本文件前两条腿全部落空）。
package db

import (
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

const (
	// 旧实现 BeforeCreate 的形状：uuid+uuid＝36+36＝72 字符，原样落库
	legacyPlainToken = "11111111-2222-3333-4444-555555555555" + "66666666-7777-8888-9999-000000000000"
	compliantToken   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func TestStartupDropsLegacyPlaintextResetTokens(t *testing.T) {
	gdb := testutil.NewTestDB(t, &model.PasswordResetToken{})
	if gdb == nil {
		t.Skip("测试库不可达")
	}
	expires := time.Now().Add(time.Hour).Format("2006-01-02 15:04:05")

	if err := gdb.Exec("INSERT INTO password_reset_tokens (id,user_id,token,expires_at,created_at) VALUES (?,?,?,?,?)",
		"legacy-1", "4242", legacyPlainToken, expires, time.Now()).Error; err != nil {
		t.Fatalf("植入存量明文行失败: %v", err)
	}
	if len(legacyPlainToken) != 72 {
		t.Fatalf("夹具形状不对：存量明文应为 72 字符，实际 %d", len(legacyPlainToken))
	}
	if err := gdb.Exec("INSERT INTO password_reset_tokens (id,user_id,token,expires_at,created_at) VALUES (?,?,?,?,?)",
		"hashed-1", "4242", compliantToken, expires, time.Now()).Error; err != nil {
		t.Fatalf("植入合规行失败: %v", err)
	}

	postMigrateDropLegacyPlaintextResetTokens(gdb)

	var legacyLeft, hashedLeft int64
	if err := gdb.Table("password_reset_tokens").Where("id = ?", "legacy-1").Count(&legacyLeft).Error; err != nil {
		t.Fatalf("统计存量行失败: %v", err)
	}
	if legacyLeft != 0 {
		t.Errorf("存量明文行还在（Count 走软删过滤）=%d ⇒ 期望 0", legacyLeft)
	}
	// 绕开 GORM 的软删过滤，直接问表本身：原文必须物理不在
	var rawRows int64
	if err := gdb.Raw("SELECT count(*) FROM password_reset_tokens WHERE id = 'legacy-1'").Scan(&rawRows).Error; err != nil {
		t.Fatalf("裸查存量行失败: %v", err)
	}
	if rawRows != 0 {
		t.Errorf("存量明文行只是被打了软删标记（表内仍有 %d 行）⇒ 凭证仍可被读出，等于没修", rawRows)
	}

	if err := gdb.Table("password_reset_tokens").Where("id = ?", "hashed-1").Count(&hashedLeft).Error; err != nil {
		t.Fatalf("统计合规行失败: %v", err)
	}
	if hashedLeft != 1 {
		t.Errorf("合规行被误删：剩 %d 行，期望 1", hashedLeft)
	}

	// 可重跑：第二次不得再动任何东西
	postMigrateDropLegacyPlaintextResetTokens(gdb)
	if err := gdb.Table("password_reset_tokens").Where("id = ?", "hashed-1").Count(&hashedLeft).Error; err != nil {
		t.Fatalf("复跑后统计失败: %v", err)
	}
	if hashedLeft != 1 {
		t.Errorf("复跑后合规行剩 %d 行，期望 1 ⇒ 钩子不幂等", hashedLeft)
	}
}

// 装配点锁：钩子写了没人调，上面两条腿就只是"一个没接的函数"。
// 按整行匹配计数（不是子串）——定义那一行也含同名子串。
// 位置判据：必须排在 AutoMigrate 终校验与其余 post-migrate 钩子之后，
// 否则表还没建出来就 DELETE，只会得到一句被吞掉的告警。
func TestAutoMigrateWiresLegacyPlaintextResetTokenGuard(t *testing.T) {
	src, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("读 migrate.go 失败: %v", err)
	}
	const call = "\tpostMigrateDropLegacyPlaintextResetTokens(DB)"
	const afterLine = "\tpostMigrateObsDefaultUniqueIndex(DB)"
	calls, define, obsAt, callAt := 0, 0, -1, -1
	for i, line := range strings.Split(string(src), "\n") {
		switch line {
		case call:
			calls++
			callAt = i
		case "func postMigrateDropLegacyPlaintextResetTokens(db *gorm.DB) {":
			define++
		case afterLine:
			obsAt = i
		}
	}
	if define != 1 {
		t.Errorf("钩子定义出现 %d 次，期望 1", define)
	}
	if calls != 1 {
		t.Errorf("AutoMigrate 里那句调用出现 %d 次，期望 1 ⇒ 存量明文作废没接线（或重复接线）", calls)
	}
	if obsAt < 0 {
		t.Fatal("找不到 obs_config 那道钩子的调用行 ⇒ 本腿的位置参照消失，需人工复核 migrate.go")
	}
	if callAt < obsAt {
		t.Errorf("调用在第 %d 行、obs_config 钩子在第 %d 行 ⇒ 作废动作必须排在终校验之后，否则表还没建出来", callAt+1, obsAt+1)
	}
}
