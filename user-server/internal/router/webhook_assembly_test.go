package router

import (
	"os"
	"strings"
	"testing"
)

// WebhookService 的构造函数体自己起后台循环（worker 池、限速 janitor、恢复扫描、
// 免打扰到期派发），退出只认实例自己的 stopCh。装配里因此只容得下一个构造点：
// 多一次 New 就多一套永不退出的 worker，且恢复扫描会和主实例并行重扫同一张表。
// 这条用源码形状守，是因为"多起来的第二个实例"在运行时没有任何可观测差异。
func TestRouterAssemblesExactlyOneWebhookService(t *testing.T) {
	src, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("读 router.go 失败（门扫空就等于没测）: %v", err)
	}
	const ctor = "service." + "NewWebhookService("
	n := strings.Count(string(src), ctor)
	if n != 1 {
		t.Errorf("router.go 里 %s 出现 %d 次，期望 1 次：装配期唯一实例", ctor, n)
	}
}
