package middleware

import (
	"context"
	"strings"
	"sync"

	"hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// 订阅闸
//
// 三层判定：
//  ① 显式小白名单（续费必需面，逐路径列全 + 对账测试）→ 放行；
//  ② 客户面显式不锁清单（产品裁决：商户存量客户的 AI 应答不断线）→ 放行；
//  ③ 其余路径走 ModuleOwnerOf(path)（复用模块路由归属表）：
//     有归属模块 → 4305 拦；无归属 → 放行 + WARN（与 4301 同风险等级取舍）。
//
// 锁判定本地化：判定在 service.SubscriptionMirrorService 内完成：镜像的
// current_period_end / trial_expires_at 与 now 惰性比较，**与平台可达性无关**；
// fetched_at 只 Warn 不参与锁；fail-open 仅对「镜像无 period_end 数据」。
//
// 执行位置：与 GuardModulePath 同位置挂接（鉴权之后、角色权限之前），仅 on 模式装配。
// expired 时登录态仍有效 —— 锁的是业务面，不是身份（否则续费墙自己都打不开）。

// subscriptionLockChecker 判定接口（装配期由 service.SubscriptionMirrorService 注入）
type subscriptionLockChecker interface {
	// IsLocked 请求上下文；true = 订阅已到期，业务面应拦 4305
	IsLocked(ctx context.Context) bool
}

var (
	subGateMu       sync.RWMutex
	subGateCheckerV subscriptionLockChecker
)

// SetSubscriptionLockChecker 注入订阅闸判定（router 装配期调用）
func SetSubscriptionLockChecker(c subscriptionLockChecker) {
	subGateMu.Lock()
	subGateCheckerV = c
	subGateMu.Unlock()
}

func currentSubscriptionLockChecker() subscriptionLockChecker {
	subGateMu.RLock()
	defer subGateMu.RUnlock()
	return subGateCheckerV
}

// subscriptionAllowPrefixes 显式白名单前缀（①，对账测试 TestSubscriptionWhitelistCoversRenewalSurface 钉住）。
//
// 续费必需面，逐路径列全：
//   - /api/auth/*          登录/登出/改密（含 logout）
//   - /api/public/*        公开配置（登录页/注册页在锁态也要渲染）
//   - /api/module-market/* 浏览/下单/pay-status（修死循环：市场页依赖集不得 4305）
//   - /api/subscription/*  订阅状态 + 续费下单（墙页依赖集）
//   - /api/modules/sync    续费成功后刷新镜像的恢复通道
//   - /api/modules/my      我的模块（购买/续费引导面）
var subscriptionAllowPrefixes = []string{
	"/api/auth/",
	"/api/public/",
	"/api/module-market/",
	"/api/subscription/",
}

// subscriptionAllowExact 显式白名单精确路径（①）
var subscriptionAllowExact = []string{
	"/api/modules/sync",
	"/api/modules/my",
}

// subscriptionCustomerFacing 客户面显式不锁（②，产品裁决写入 spec）。
//
// widget/chat public、chat WebSocket、总线 ingress/outbox、wechat webhook
// 在 expired 时保持可用 —— 商户存量客户的 AI 应答不断线；锁的是员工工作台业务面。
// 模式语义与 module_gate 相同：`*` 吃掉恰好一段；前缀以 `/*` 结尾表示整棵子树。
var subscriptionCustomerFacing = []string{
	"/api/chat/public/*",     // widget 对话面
	"/api/ws/visitor",        // 访客 WebSocket
	"/api/channelbus/ingest", // 消息总线 ingress（扩展端上报）
	"/api/channelbus/outbox", // 消息总线 outbox（含 /ack /sse 子路径见下）
	"/api/channelbus/outbox/*",
	"/api/webhook/wechat/*", // wechat webhook
}

// subscriptionAllowPath ①+② 路径放行判定
func subscriptionAllowPath(path string) bool {
	for _, p := range subscriptionAllowExact {
		if path == p {
			return true
		}
	}
	for _, p := range subscriptionAllowPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, pat := range subscriptionCustomerFacing {
		if pat == path {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "*") // 保留末尾斜杠：/api/chat/public/
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}
	return false
}

// GuardSubscriptionPath 订阅闸中间件（expired → 白名单外 4305）。
//
// 直接挂在 auth 组上（与 GuardModulePath 同位置），装配仅 on 模式。
func GuardSubscriptionPath() gin.HandlerFunc {
	return func(c *gin.Context) {
		// off 模式（离线单机）没有订阅概念，恒放行：装配层已按 on 条件挂接，
		// 这里再判一次是纵深 —— 万一 off 部署误挂了也不会把离线用户锁死。
		if !tenant.IsMultiTenant() {
			c.Next()
			return
		}
		checker := currentSubscriptionLockChecker()
		if checker == nil {
			// 未注入 = 装配缺失。放行（与模块闸同口径：拦住会让整个系统不可用）。
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if subscriptionAllowPath(path) {
			c.Next()
			return
		}
		if !checker.IsLocked(c.Request.Context()) {
			c.Next()
			return
		}
		// ③ 三层判定：复用模块路由归属表
		owner := ModuleOwnerOf(path)
		if owner == "" {
			// 无归属模块的路径：放行 + WARN（与 4301 未覆盖路由同风险等级取舍：
			// 新接口忘了归模块就全站 403 是灾难，漏拦最多让某个接口多暴露）。
			logger.Warnf("[subscription-gate] 路径 %s 无模块归属，expired 态下放行（记入 uncovered 巡检）", path)
			noteUncovered(path)
			c.Next()
			return
		}
		// 有归属模块 → expired 一律拦
		response.ErrorWithBusinessCode(c, errors.CodeSubscriptionExpired,
			"订阅已到期，业务功能已暂停，请续费后继续使用", gin.H{"module_key": owner})
		c.Abort()
	}
}
