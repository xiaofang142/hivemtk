package app

import (
	"context"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/metrics"
	"hivemtk-user/internal/pkg/utils/logger"
	appsvc "hivemtk-user/internal/service"
)

// visitorTokenWeakSecretTotal 访客会话 HMAC 密钥启动审计计数（F-6-5①）：
// 密钥缺失或长度 <32 时 +1，供 /metrics 面观测弱密钥部署面。
var visitorTokenWeakSecretTotal = metrics.NewCounter(
	"app_visitor_token_secret_weak_total",
	"Startup audits where VISITOR_TOKEN_SECRET is missing or shorter than 32 chars",
	[]string{},
)

// WireURLsConfigParams 注入 urls 组的 config 侧参数中心取值口
// （cloud_url / geo_site_base_url / public_base_url）与 platform 组的
// platform.api_host。
//
// 收录标准：读取点在 internal/config（config 不能反向依赖 service），只能由装配层
// 注入 provider。urls.frontend_url 不在这里——它的读取点在 service 本体
// （password_reset.go 的 getFrontendBaseURL），走「读取函数自己调 GlobalConfigParam()」
// 的直连范式，不需要注入。
//
// 每条都写成显式闭包，且 GetString 与键名字面量必须在同一行：
// scripts/check-config-param-readpoints.py 认的是这个形状。
//
// 闭包 provider（不注入启动瞬间的值）：每次调用都现读参数中心，值改完 60s TTL 缓存
// 过期后即生效，不需要重启。ctx 原样透传，SaaS 模式绑定租户后读到本租户覆盖值；
// 平台级调用（无租户 ctx）回落平台默认值。
//
// platform.api_host 的 ctx 目前不参与判定（平台地址是部署级事实，见
// config_param_tenant_scope.go 的 platformScopedParams），但闭包仍照传 ctx：
// 与同组其余三条保持同一形状，将来若出现多平台并存的场景不必再改装配点。
func WireURLsConfigParams() []string {
	cp := appsvc.GlobalConfigParam()
	wired := make([]string, 0, 4)

	// 这里的"缺省"必须是空串而不是 DefaultCloudURL：同一个值当默认值回填，
	// 就等于把"没配"伪装成"配了"——A11 的启动期提示就无法区分两者（见 CloudURLSource）。
	config.SetCloudURLProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "urls", "cloud_url", "")
	})
	wired = append(wired, "urls.cloud_url")

	config.SetWebsiteBaseURLProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "urls", "geo_site_base_url", config.DefaultWebsiteBaseURL)
	})
	wired = append(wired, "urls.geo_site_base_url")

	config.SetPublicBaseURLProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "urls", "public_base_url", "")
	})
	wired = append(wired, "urls.public_base_url")

	config.SetPlatformHostProvider(func(ctx context.Context) string {
		return cp.GetString(ctx, "platform", "api_host", "")
	})
	wired = append(wired, "platform.api_host")

	return wired
}

// WarnIfCloudURLIsBuiltinDefault 启动期提示：云端版地址是不是还指着厂商的默认域名（A11）。
//
// 为什么需要它：`urls.cloud_url` 的种子默认值与代码常量**都是**厂商生产域名，所以
// "运维从没配过"和"运维配了一个一模一样的域名"在参数表里长得完全相同。仅比较字符串
// 无法区分，只能靠来源标签（CloudURLSource）：
//   - 来源是 env / 平台配置 ⇒ 运维显式配过，不提示；
//   - 来源是参数中心（含种子默认值）或代码常量 ⇒ 提示：一套自建 SaaS / 私有化部署此时
//     会把用户引导到厂商站点。
//
// 只打一行，且写明消音办法（把来源改成 env 或平台配置即可），不做任何自动改写：
// 值本身对厂商自己的云端版是**正确**的，猜错方向比不猜更糟。
func WarnIfCloudURLIsBuiltinDefault() {
	url, source := config.CloudURLSource(context.Background())
	if url != config.DefaultCloudURL {
		return
	}
	if source == config.CloudURLSourceEnv || source == config.CloudURLSourcePlatformCfg {
		return
	}
	logger.Warnf("[urls] 云端版地址仍是内置默认域名 %s（来源 %s）。若本实例是**自建 SaaS / 私有化部署**，"+
		"前端「云端版入口」会把用户引到厂商生产环境：请显式配置 urls.cloud_url 参数、"+
		"env PLATFORM_CLOUD_URL 或平台配置的 cloud_url（任一处显式配置后本行不再出现）",
		url, source)
}

// AuditVisitorTokenSecretAtStartup 启动期审计访客会话 HMAC 密钥（缺失/强度不足两档）。
//
// 背景（2026-11-10 embed 全链回归实测）：config.yaml 的
// security.visitor_token_secret 是 `${VISITOR_TOKEN_SECRET:}` 空默认展开——
// 漏配不会阻断启动，而是让 **每一次** 访客客服请求都在运行期 fail-closed
// （POST /api/chat/public/sessions 稳定 400「生成 visitor_token 失败:
// secret 不能为空」），症状离根因隔了三层， fresh 部署极易当成"渠道坏了"排查。
//
// 定位与 A11 同款：运行期 fail-closed 是正确的安全默认（绝不为漏配放行），
// 但**沉默失败**必须配一行响亮的启动期告警，写明影响面与消音办法。
//
// F-6-5①（2026-11-11 裁决：轻量强校验）：强度不足（非空但 <32 字符）从「文案建议」
// 升级为 Error 级启动告警 + metrics 计数（visitorTokenWeakSecretTotal），文案附生成
// 命令；**不硬拒启动**（既有部署可能已用弱密钥，硬拒=断服），既有密钥平滑轮换。
// 管理台提示随 N-7 env 闸一并评估（不在本处展开）。
func AuditVisitorTokenSecretAtStartup() {
	secret := config.GetAppConfig().Security.VisitorTokenSecret
	switch {
	case secret == "":
		visitorTokenWeakSecretTotal.WithLabel().Inc()
		logger.Errorf("[chat] VISITOR_TOKEN_SECRET 未配置：访客客服（embed widget）打开会话将全部失败" +
			"（fail-closed，报「生成 visitor_token 失败: secret 不能为空」）。" +
			"请设置 env VISITOR_TOKEN_SECRET（建议 32+ 字符随机串）后重启；不使用客服挂件可忽略本行")
	case len(secret) < 32:
		visitorTokenWeakSecretTotal.WithLabel().Inc()
		logger.Errorf("[chat] VISITOR_TOKEN_SECRET 强度不足（当前 %d 字符 < 32）：短密钥显著降低访客 token 的 HMAC 伪造成本。"+
			"建议轮换：openssl rand -hex 32 生成新密钥写入 env VISITOR_TOKEN_SECRET 后重启"+
			"（不硬拒启动，既有部署可平滑迁移；不使用客服挂件可忽略本行）", len(secret))
	}
}
