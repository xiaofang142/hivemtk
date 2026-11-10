// 租户域 Redis 键构造（specs/001-saas-multi-tenant T010）
//
// 铁律：
//   - off 模式：键形与旧版本逐字节一致（存量吊销名单/会话键必须继续命中，
//     否则升级窗口内已吊销 token 复活——安全隐患）
//   - on 模式：ctx 携带租户 ⇒ "t{tenant}:" 前缀，配额/状态按租户分域；
//     无租户 ctx（系统操作/全局语义键）保持裸键，调用方自证其全局语义
package tenant

import (
	"context"
	"fmt"
	"hivemtk-user/internal/pkg/utils/logger"
	"sync"
)

var bareKeyWarnOnce sync.Once

// RedisKey 构造租户分域 Redis 键。
//
//	ctx 携带租户且 on 模式	→ "t{id}:{key}"
//	其余（off 模式/无租户）	→ 原样返回
func RedisKey(ctx context.Context, key string) string {
	if key == "" || !IsMultiTenant() {
		return key
	}
	id, ok := FromContext(ctx)
	if !ok {
		// on 模式下无租户 ctx：保持裸键（全局语义），但只告警一次——
		// 请求路径的键都应经中间件绑定租户，走到这里的多是系统级键
		bareKeyWarnOnce.Do(func() {
			logger.Warnf("[tenant] on 模式下 Redis 键 %q 无租户上下文，按全局键处理（请确认是全局语义）", key)
		})
		return key
	}
	return fmt.Sprintf("t%d:%s", id, key)
}

// RedisKeyWithTenant 显式以租户维度构造键（off 模式仍返回裸键——键形铁律优先）
func RedisKeyWithTenant(tenantID uint, key string) string {
	if key == "" || !IsMultiTenant() || tenantID == 0 {
		return key
	}
	return fmt.Sprintf("t%d:%s", tenantID, key)
}
