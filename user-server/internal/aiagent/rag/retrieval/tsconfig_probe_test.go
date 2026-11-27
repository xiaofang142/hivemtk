// tsconfig_probe_test.go 钉住「文本搜索配置按库里真实存在的挑」这条口径。
//
// 回归背景（2026-10-22 线上）：`zh_rag` 只在装了 zhparser 扩展的库上存在
// （建它的迁移判据就是"zhparser 装了才建"），而线上镜像 pgvector/pgvector:pg15 不含该扩展。
// 读取侧却无条件先试 zh_rag，于是每次词法召回先撞一次
//
//	ERROR: text search configuration "zh_rag" does not exist (SQLSTATE 42704)
//
// 才降级到 simple 继续（功能结果对，日志是假的）。
package ragretrieval

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// captureSQLErrors 把 GORM 执行期间的错误 SQL 收进切片。
//
// 判据是**错误 SQL**而不是日志级别：GORM 的 logger 默认会把每条失败查询按 ERROR 打出来，
// 我们要锁的正是"不该再出现那条注定失败的查询"，而不是某条日志的措辞。
func captureSQLErrors(t *testing.T, db *gorm.DB, fn func()) []string {
	t.Helper()
	var captured []string
	orig := db.Logger
	db.Logger = &errCapturingLogger{Interface: orig, sink: &captured}
	t.Cleanup(func() { db.Logger = orig })
	fn()
	return captured
}

// errCapturingLogger 把执行失败时的 SQL 收进 sink，其余原样透传。
type errCapturingLogger struct {
	gormlogger.Interface
	sink *[]string
}

func (l *errCapturingLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err != nil {
		sql, _ := fc()
		*l.sink = append(*l.sink, sql+" | "+err.Error())
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

// resetTSConfigProbe 清空探测缓存（测试用）。
//
// 探测缓存是进程级的：同一个包里既有"有 zh_rag 的库"又有"没有 zh_rag 的库"时，
// 先探测的库会把答案带给后面的用例 ⇒ 用例之间互相污染。生产面不用这个函数
// （golangci-lint 的 run.tests=false 不看测试文件，所以它必须待在测试文件里）。
func resetTSConfigProbe() {
	tsConfigProbe.Range(func(k, _ any) bool {
		tsConfigProbe.Delete(k)
		return true
	})
}

// TestAvailableTSConfigs_ExcludesMissingConfig 库里没有 zh_rag 时，可用配置集合里不许有它。
//
// 这条是整个修复的核心断言：调用方按这个集合去查，就再也不会撞 42704。
func TestAvailableTSConfigs_ExcludesMissingConfig(t *testing.T) {
	db := setupHybridTestDB(t)
	resetTSConfigProbe()
	t.Cleanup(resetTSConfigProbe)

	// 测试库与生产同镜像：没有 zhparser ⇒ 没有 zh_rag。
	// 显式确认一次前提，避免"哪天测试库装了 zhparser"导致本用例失去意义。
	var hasZhRag int
	if err := db.Raw("SELECT count(*) FROM pg_ts_config WHERE cfgname = 'zh_rag'").Scan(&hasZhRag).Error; err != nil {
		t.Fatalf("查 pg_ts_config 失败: %v", err)
	}
	if hasZhRag != 0 {
		t.Skip("测试库已存在 zh_rag，本用例的前提不成立（改走 TestAvailableTSConfigs_PrefersZhRag）")
	}

	cfgs := availableTSConfigs(context.Background(), db)
	for _, c := range cfgs {
		if c == "zh_rag" {
			t.Fatalf("可用配置集合里出现了库里不存在的 zh_rag：%v", cfgs)
		}
	}
	if len(cfgs) == 0 {
		t.Fatalf("可用配置集合为空：simple 是 PG 内置配置，永远存在，探不出它说明探测逻辑坏了（cfgs=%v）", cfgs)
	}
	if cfgs[0] != "simple" {
		t.Errorf("缺 zh_rag 时首选应为 simple，实际 %v", cfgs)
	}
}

// TestAvailableTSConfigs_PrefersZhRag 库里有 zh_rag 时必须选它（不降级到 simple）。
func TestAvailableTSConfigs_PrefersZhRag(t *testing.T) {
	db := setupHybridTestDB(t)
	resetTSConfigProbe()
	t.Cleanup(resetTSConfigProbe)

	// 用内置 default 解析器建一个 zh_rag：只为验证"存在即被选中"这条口径，
	// 不验证分词质量（default 解析器对 CJK 的处理另有专门结论，见 tsconfig_probe.go 注释）。
	if err := db.Exec(`CREATE TEXT SEARCH CONFIGURATION zh_rag (PARSER = pg_catalog."default")`).Error; err != nil {
		t.Fatalf("建 zh_rag 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec(`DROP TEXT SEARCH CONFIGURATION IF EXISTS zh_rag`).Error })

	cfgs := availableTSConfigs(context.Background(), db)
	if len(cfgs) == 0 || cfgs[0] != "zh_rag" {
		t.Fatalf("库里有 zh_rag 时首选必须是它，实际 %v", cfgs)
	}
}

// TestLexicalRetrieve_NoMissingConfigError 缺 zh_rag 的库上做一次词法召回：
//  1. 召回结果不为空（走 simple 兜底，行为与修复前一致）；
//  2. **执行期间没有任何错误 SQL**（修复前会有一条 42704）。
func TestLexicalRetrieve_NoMissingConfigError(t *testing.T) {
	db := setupHybridTestDB(t)
	resetTSConfigProbe()
	t.Cleanup(resetTSConfigProbe)

	var hasZhRag int
	if err := db.Raw("SELECT count(*) FROM pg_ts_config WHERE cfgname = 'zh_rag'").Scan(&hasZhRag).Error; err != nil {
		t.Fatalf("查 pg_ts_config 失败: %v", err)
	}
	if hasZhRag != 0 {
		t.Skip("测试库已存在 zh_rag，本用例针对的是缺配置形态")
	}

	// 造一条中文 chunk，tsvector 用 simple（与生产触发器在缺 zh_rag 时的写法一致）。
	const content = "示例科技提供私域 AI 营销系统"
	if err := db.Exec(
		`INSERT INTO knowledge_chunks (document_id, product_id, content, embed_status, content_tsv)
		 VALUES ($1, $2, $3, 'pending', to_tsvector('simple', $3))`,
		1, "", content,
	).Error; err != nil {
		t.Fatalf("插入 chunk 失败: %v", err)
	}

	r := NewLexicalRetriever(db)
	var rows []Chunk
	errs := captureSQLErrors(t, db, func() {
		got, err := r.Retrieve(context.Background(), "", "私域", 10)
		if err != nil {
			t.Fatalf("词法召回返回 error: %v", err)
		}
		rows = got
	})

	if len(rows) == 0 {
		t.Fatal("词法召回结果为空：缺 zh_rag 时应降级到 simple 兜底并召回得到")
	}
	for _, e := range errs {
		if strings.Contains(e, "zh_rag") || strings.Contains(e, "42704") {
			t.Fatalf("执行期间仍出现针对不存在配置的失败查询：%s", e)
		}
	}
}
