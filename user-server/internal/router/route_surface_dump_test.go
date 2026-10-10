package router

// TestDumpLiveRouteTable 把生产装配入口 Setup() 跑出来的那张路由表原样导成 TSV，供离线取证复算。
//
// 为什么要有它：同目录的 route_surface_consistency_test.go 是**门**，它只在比对失败时出声，
// 不会把"哪些路由没有任何消费方"这份名单交出来。而僵尸接口分诊要的不是一个通过率，
// 是一张可复核的名册（判"某条口能不能下线/要不要补前端接线"得逐条点名）。
// 2026-10-11 的那轮分诊用一份一次性脚本算出四档计数，脚本没入库 ⇒ 今天既复算不出那四个数，
// 也拿不到名册。这里补的就是那条"事实源出口"：表还是同一张表（同一个 liveRoutes），
// 只是多一个导出口，不新增判据、不动门。
//
// 为什么不挂进 CI：没设 ROUTE_DUMP_FILE 时它直接 Skip ⇒ 门禁与 CI 都不为它付建库成本。
// 它是取证件，不是门；把它当门用会退化成"每次 CI 都导一遍没人读的快照"。

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"
)

// routeDumpFileEnv 是导出路径的环境变量名：设为某个文件路径才导出，未设则跳过本用例。
const routeDumpFileEnv = "ROUTE_DUMP_FILE"

func TestDumpLiveRouteTable(t *testing.T) {
	target := strings.TrimSpace(os.Getenv(routeDumpFileEnv))
	if target == "" {
		t.Skipf("未设置 %s：本用例只在需要复算路由消费面时导出路由表，不作为门禁", routeDumpFileEnv)
	}
	exact, _ := liveRoutes(t)

	rows := make([]string, 0, len(exact))
	for key, handler := range exact {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			t.Fatalf("路由表键的形状不是 \"METHOD /path\"：%q", key)
		}
		rows = append(rows, parts[0]+"\t"+parts[1]+"\t"+handler)
	}
	sort.Strings(rows)

	f, err := os.Create(target)
	if err != nil {
		t.Fatalf("写路由表导出件 %s 失败：%v", target, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	if _, err := w.WriteString("METHOD\tPATH\tHANDLER\n"); err != nil {
		t.Fatalf("写表头失败：%v", err)
	}
	for _, r := range rows {
		if _, err := w.WriteString(r + "\n"); err != nil {
			t.Fatalf("写行失败：%v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush 失败：%v", err)
	}

	// 自证：导出件非空且含一条本仓一定在的口，否则"复算"会建立在一个空文件上。
	// 挑 GET /health：Setup()（router.go:165）在第 200 行无条件注册它，
	// 它不在鉴权组里、不随前端构建产物或特性位变化，因此这条缺席只可能是"导出的不是完整路由表"。
	if len(rows) == 0 {
		t.Fatalf("Setup() 跑出的路由表是空的：装配入口坏了，导出无意义")
	}
	var sawHealth bool
	for _, r := range rows {
		if strings.HasPrefix(r, "GET\t/health\t") {
			sawHealth = true
			break
		}
	}
	if !sawHealth {
		t.Fatalf("导出的 %d 行里没有 GET /health ⇒ 事实源不是完整的路由表，读数不可用", len(rows))
	}
	t.Logf("导出 %d 行路由表到 %s", len(rows), target)
}
