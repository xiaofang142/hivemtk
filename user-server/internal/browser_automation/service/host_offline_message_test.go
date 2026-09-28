package service

// 契约锁：Host 离线文案里「被逻辑读的那一段」和「被用户读的那一段」都在库里锁死。
//
// 两半各有来历：
//   - 前半句 `browser host 未连接` 不只是文案——executor 的中止归因用它做子串匹配
//     （见 executor.go 里对 abort 的判断），改掉头一句话就会静默改掉重试判据；
//   - 后半句是这轮真机腿踩到的误导：Host 装好且在线过，服务端热重启的那 1 分钟里
//     它正按退避重连，用户拿到的却是「请在本机 Chrome 加载扩展并运行 install.sh」，
//     于是被推去重装一个本来就装好的东西（我这轮真的按这句话去核了一遍安装态）。
import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestHostOfflineMessageContract(t *testing.T) {
	if ErrHostOffline == nil {
		t.Fatal("ErrHostOffline 必须存在")
	}
	msg := ErrHostOffline.Error()
	if !strings.Contains(msg, "browser host 未连接") {
		t.Errorf("执行侧按这一句做中止归因，文案改动不得去掉它：%s", msg)
	}
	for _, want := range []string{"自动重连", "稍后重试"} {
		if !strings.Contains(msg, want) {
			t.Errorf("离线文案须告知用户「Host 可能正在自动重连」（缺 %q），否则只能读成「没装」：%s", want, msg)
		}
	}
	// 离线判据本身可被 errors.Is 识别（controller 的 409 转换与「未上线」归因都走这条），
	// 这里只补一层包装自证文案与哨兵同源。
	if !errors.Is(fmt.Errorf("run: %w", ErrHostOffline), ErrHostOffline) {
		t.Error("ErrHostOffline 必须可被 errors.Is 识别")
	}
}
