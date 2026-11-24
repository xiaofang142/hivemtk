package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 出站账号解析必须经 service/repository seam；webhook 与 registry 文件都不能重新
// 长出直接 DB/GORM 查询。constructor 只负责装配仓储，不属于数据访问。
func TestOutboundAccountValidation_NoDirectDataAccess(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位架构回归测试文件")
	}
	root := filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
	files := []string{"webhook_outbound.go", "outbound_registry.go"}
	for _, name := range files {
		path := filepath.Join(root, "user-server", "internal", "service", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		text := string(raw)
		for i, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if name == "webhook_outbound.go" && strings.Contains(trimmed, "bridgeOutboxCapable(") && !strings.HasPrefix(trimmed, "func bridgeOutboxCapable(") {
				t.Errorf("webhook_outbound.go:%d 不得直接查询 bridge 能力位；该判断应由 outbound_registry 的 registry-owned helper 收口", i+1)
			}
			if strings.Contains(trimmed, `"gorm.io/gorm"`) {
				t.Errorf("%s:%d 不得直接引入 GORM", name, i+1)
			}
			if strings.Contains(trimmed, "dbstate.Get()") && !strings.Contains(trimmed, "NewChannelDispatchService(") {
				t.Errorf("%s:%d 不得直接获取 DB 句柄；仅允许传给 ChannelDispatchService constructor", name, i+1)
			}
			if strings.Contains(trimmed, ".WithContext(") {
				t.Errorf("%s:%d 不得直接执行 DB WithContext 查询", name, i+1)
			}
			for _, token := range []string{".Where(", ".First(", ".Find(", ".Create(", ".Updates(", ".Delete("} {
				if strings.Contains(trimmed, token) {
					t.Errorf("%s:%d 不得直接调用 GORM 查询/写入方法 %s", name, i+1, token)
				}
			}
			if strings.Contains(trimmed, "model.ChannelAccount") || strings.Contains(trimmed, "model.Channel") {
				t.Errorf("%s:%d 不得直接声明/查询 ChannelAccount 或 Channel model", name, i+1)
			}
		}
	}
}
