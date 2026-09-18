package bridge

import (
	"context"
	"net/url"

	gw "hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
)

const maxReplyContentBytes = 4 * 1024 //nolint:unused //// 仅被 *_test.go 引用，生产路径未用

// OwnershipChecker 账号归属校验回调：
//   - 输入：uid (JWT 解析的 user_id), channel, accountID
//   - 输出：true 表示当前 user 拥有该 bridge 账号
//   - 由 service 层在初始化时注入（避免 bridge → service 的反向依赖）
type OwnershipChecker func(ctx context.Context, uid uint, channel, accountID string) (bool, error)

// GlobalOwnershipChecker 全局归属校验（service 层在 init 时注入）
var GlobalOwnershipChecker OwnershipChecker

// RegisterOwnershipChecker 注册账号归属校验回调
func RegisterOwnershipChecker(fn OwnershipChecker) {
	GlobalOwnershipChecker = fn
}

func maskTokenBridge(token string) string {
	if token == "" {
		return ""
	}
	runes := []rune(token)
	if len(runes) <= 4 {
		return token + "***(" + itoa(len(token)) + " chars)"
	}
	return string(runes[:4]) + "***(" + itoa(len(token)) + " chars)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func describeUpstreamQuery(values url.Values) map[string]string {
	out := make(map[string]string, len(values))
	for k, vs := range values {
		v := ""
		if len(vs) > 0 {
			v = vs[0]
		}
		if k == "token" {
			out[k] = maskTokenBridge(v)
		} else {
			out[k] = v
		}
	}
	return out
}

func historyItemToEvent(m *UnifiedMessage, it *HistoryItem) *model.MessageEvent {
	return gw.HistoryToEvent(m, it)
}
