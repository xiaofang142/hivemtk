package model

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// scanModelTables 从 model 目录实测出全部 TableName 取值。
//
// 为什么要扫源码而不是维护一张清单：清单会被"新增模型时忘了登记"绕过，
// 而漏登记的后果正是这张表要解决的问题（孤儿表 → 没人知道它随谁开关）。
func scanModelTables(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读 model 目录失败: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			continue
		}
		s := string(src)
		// 形如: func (x) TableName() string { return "tbl" ... }
		idx := 0
		for {
			pos := strings.Index(s[idx:], "TableName() string")
			if pos < 0 {
				break
			}
			pos += idx
			end := strings.Index(s[pos:], "}")
			if end < 0 {
				break
			}
			seg := s[pos : pos+end]
			if q1 := strings.Index(seg, `return "`); q1 >= 0 {
				rest := seg[q1+len(`return "`):]
				if q2 := strings.Index(rest, `"`); q2 > 0 {
					out[rest[:q2]] = name[:len(name)-3]
				}
			}
			idx = pos + end
		}
	}
	return out
}

// TestEveryModelTableHasModuleOwner 钉死「一张表都不许无主」。
//
// 事故形态（任务一实测）：270 张表里 32 张没有归属，包含
// wechat_accounts / webhook_events / notifications / bridge_accounts 这类
// 真实业务表。文档少写几行不致命，**表无主才致命**：它意味着
// 关掉某个模块之后，这张表可能仍被定时任务写入，数据继续增长 ——
// 也就是"关了模块不省钱也不干净"。
func TestEveryModelTableHasModuleOwner(t *testing.T) {
	tables := scanModelTables(t)
	if len(tables) == 0 {
		t.Fatal("未扫到任何表：扫描规则失效，这条例程等于不存在")
	}

	var orphans []string
	for tbl := range tables {
		if OwnerOfTable(tbl) == "" {
			orphans = append(orphans, tbl)
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		t.Errorf("有 %d 张表未归属任何模块（它们不会随模块开关而停用）：\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}

// TestTableOwnersPointToRealModules 表归属的模块必须真实存在
func TestTableOwnersPointToRealModules(t *testing.T) {
	for tbl, mk := range TableOwners {
		if OwnerModuleCategory(mk) == "" {
			t.Errorf("表 %s 归属了不存在的模块 %q", tbl, mk)
		}
	}
}

// TestNoTableBelongsToTwoModules 一张表只能有一个归属模块。
//
// 重复归属在"关掉任一个都让它消失"的语义下是错的，而它极其容易发生：
// 平台续费模块常常同时动 core 与自己的表。
func TestNoTableBelongsToTwoModules(t *testing.T) {
	seen := map[string][]string{}
	for mk, list := range moduleTableRegistry {
		for _, tbl := range list {
			seen[tbl] = append(seen[tbl], mk)
		}
	}
	var bad []string
	for tbl, mks := range seen {
		if len(mks) > 1 {
			bad = append(bad, tbl+" -> "+strings.Join(mks, ","))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("有 %d 张表被多个模块登记（关掉任一个都会让它消失）：\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// TestPaidModuleHasTables 收费模块必须声明它会动哪些表
//
// 判据：收费模块空表表 = "关掉它什么都不会停"，那它就不该单独定价，
// 应该并进别的模块。这类空表表是最容易被漏审的地方。
func TestPaidModuleHasTables(t *testing.T) {
	for _, mk := range PaidModuleKeys() {
		if len(TablesOfModule(mk)) == 0 {
			t.Errorf("收费模块 %s 没有登记任何表：关掉它不会有任何数据停用，"+
				"说明它不该作为独立商品存在，应并入其它模块", mk)
		}
	}
}

// TestBrowserAutomationTablesAllOwned 浏览器自动化的表一张都不许漏登、也不许登幽灵。
//
// 为什么单开一条：`TestEveryModelTableHasModuleOwner` 扫的是 `internal/model` 目录，
// 而浏览器自动化的十二张表住在 `internal/browser_automation/model`——**不在那个扫描面里**。
// 于是漏登不会让任何一条用例红（事实形态：browser_sessions / browser_steps /
// browser_command_log / browser_cron_triggers / browser_llm_plans / browser_audit_digests /
// browser_write_claims 七张长期无主，而 browser_profiles 这张**根本不存在**的表反倒登着）。
//
// 漏登的后果不是「读不出来」——本表目前还没有生产消费方——而是**归属这件事没被说清**：
// 以后真按「关模块 ⇒ 这张表跟着停」做开关时，漏登的那张表会继续被后台任务写。
//
// 判据取自模型源码（TableName 取值）而不是手抄清单：手抄清单会被「新增模型时忘了登记」
// 绕过，那正是这张表要解决的问题。
func TestBrowserAutomationTablesAllOwned(t *testing.T) {
	dir := filepath.Join("..", "browser_automation", "model")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", dir, err)
	}
	owned := map[string]bool{}
	for _, tbl := range TablesOfModule(ModuleBrowserAutomation) {
		owned[tbl] = true
	}
	// 实际存在的表：从模型源码里扫 TableName 取值。
	real := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		s := string(src)
		idx := 0
		for {
			pos := strings.Index(s[idx:], "TableName() string")
			if pos < 0 {
				break
			}
			pos += idx
			end := strings.Index(s[pos:], "}")
			if end < 0 {
				break
			}
			seg := s[pos : pos+end]
			if q1 := strings.Index(seg, `return "`); q1 >= 0 {
				rest := seg[q1+len(`return "`):]
				if q2 := strings.Index(rest, `"`); q2 > 0 {
					real[rest[:q2]] = true
				}
			}
			idx = pos + end
		}
	}

	var missing, ghost []string
	for tbl := range real {
		if !owned[tbl] {
			missing = append(missing, tbl)
		}
	}
	// ego-lite 腿的四张物理表住在 internal/model/egolite.go（物理独立是设计文档 §一
	// 的定案），模块开关上随 browser-automation——模块归属合法，不算 ghost。
	isEgo := func(t string) bool {
		return strings.HasPrefix(t, "egolite_") || strings.HasPrefix(t, "ego_")
	}
	for _, tbl := range TablesOfModule(ModuleBrowserAutomation) {
		if isEgo(tbl) {
			continue // ego 表在另一个扫描面（internal/model），不与本目录 real 集比对
		}
		if !real[tbl] {
			ghost = append(ghost, tbl)
		}
	}
	_ = owned
	sort.Strings(missing)
	sort.Strings(ghost)
	if len(missing) > 0 {
		t.Errorf("浏览器自动化有 %d 张表未登记模块归属（关掉模块它们不会跟着停）：\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(ghost) > 0 {
		t.Errorf("浏览器自动化登记了 %d 张不存在的表（登记一张没有的表 = 归属这件事说假话）：\n  %s",
			len(ghost), strings.Join(ghost, "\n  "))
	}
}
