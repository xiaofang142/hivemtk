package router

// 装配锁：任务服务对「回收触发器」是**可选注入**（SetTriggerRemover，未注入时 removeTriggers
// 直接 return）。可选注入的好处是服务能单独起、单独测，代价是路由里漏掉那一行既不报错也不红：
// 单元用例自己接线、自己绿，线上却留着「任务删了、定时器还每分钟醒来」的幽灵条目，
// 而那个条目每次醒来都查不到行、只写一条 Warn 日志，用户端看不出任何异常。
// 所以这里锁装配位：行为用例覆盖不到这一层，只有源码能证明它接上了。

import (
	"os"
	"strings"
	"testing"
)

const triggerRemoverCall = "taskSvc.SetTriggerRemover(cronSvc)"

func TestBrowserTriggerRemoverIsWired(t *testing.T) {
	data, err := os.ReadFile("browser_automation_routes.go")
	if err != nil {
		t.Fatalf("读路由装配文件失败（锁失去依据）: %v", err)
	}
	src := string(data)

	wireAt := strings.Index(src, triggerRemoverCall)
	if wireAt < 0 {
		t.Fatalf("%s 不在装配里：删任务/改类型后触发器不会被回收", triggerRemoverCall)
	}
	// 注入必须写在 cronSvc 构造之后。顺序错了编译不过，但「挪进某个 if 分支」这种形态
	// 静态检查最容易漏，于是同时按行取原文判：接线行必须是无条件单句。
	buildAt := strings.Index(src, "basvc.NewCronService(")
	if buildAt < 0 {
		t.Fatal("找不到 cronSvc 的构造点，顺序锁失去依据")
	}
	if wireAt < buildAt {
		t.Error("SetTriggerRemover 写在 NewCronService 之前：接的是还没构造出来的 cronSvc")
	}

	lineStart := strings.LastIndex(src[:wireAt], "\n") + 1
	lineEnd := strings.Index(src[wireAt:], "\n")
	if lineEnd < 0 {
		lineEnd = len(src)
	} else {
		lineEnd += wireAt
	}
	line := src[lineStart:lineEnd]
	if strings.TrimLeft(line, "\t") != triggerRemoverCall {
		t.Errorf("接线行不是无条件单句，got %q（落在 if/else 里就等于某条路径上仍没接线）", strings.TrimSpace(line))
	}
	if tabs := len(line) - len(strings.TrimLeft(line, "\t")); tabs != 1 {
		t.Errorf("接线行缩进 %d 层（期望函数体一层的 1 个制表符）——层级变深说明它被包进了某个块里", tabs)
	}
}
