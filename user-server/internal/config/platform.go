package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlatformConfig 平台上报配置（开源版精简版）：仅保留"商户上报"用到的配置
// （APIURL / Secret）。兼容老字段（LogReportInterval / LicenseSyncInterval 与
// Admin*）仅作占位，读不到也允许（给前端 / 监控保持 schema 兼容）。
type PlatformConfig struct {
	APIURL string `yaml:"api_url" json:"api_url"`
	Secret string `yaml:"secret" json:"secret"`
	// AdminUsername / AdminPassword 原为 /platform/* JWT 代理通道的登录凭据，
	// 该通道已整体删除（方案A，2026-11-11 L-1）：全仓再无消费者，仅作占位
	// 保持旧 yaml / schema 兼容，不参与任何必填校验。
	AdminUsername       string `yaml:"admin_username" json:"admin_username"`
	AdminPassword       string `yaml:"admin_password" json:"admin_password"`
	LogReportInterval   int    `yaml:"log_report_interval" json:"log_report_interval"`
	LicenseSyncInterval int    `yaml:"license_sync_interval" json:"license_sync_interval"`
	// CloudURL 云端版对外地址（可选）。留空回落 DefaultCloudURL；
	// 不参与 LoadPlatform 的必填校验 —— 它只影响 public config 的展示字段，
	// 缺配置不该阻断启动。
	CloudURL string `yaml:"cloud_url" json:"cloud_url"`
}

// DefaultCloudURL 云端版对外地址回落常量 —— **厂商自己的**生产域名。
//
// ⚠️ 它是"最后一级回落"，不是"推荐值"：一套自建 SaaS / 私有化部署如果没配
// `urls.cloud_url`、`PLATFORM_CLOUD_URL` 与平台配置的 cloud_url，前端"云端版入口"
// 就会指向厂商的站点 —— 用户点过去就是别人的生产环境（托管方自己的实例反倒没人访问）。
//
// 因此有两条约束：
//  1. 全仓只此一处字面量（改域名只改这里）；
//  2. **回落到它就等于没配置**，启动期必须打一行 WARN（见 CloudURLSource 与
//     app.WarnIfCloudURLIsBuiltinDefault），不允许"安静地指错地方"。
const DefaultCloudURL = "https://work.xapptool.cn"

// 云端版地址的四个来源标签（启动期提示与排障用；顺序即优先级）。
const (
	CloudURLSourcePlatformCfg = "platform_config"
	CloudURLSourceEnv         = "env:PLATFORM_CLOUD_URL"
	CloudURLSourceParamCenter = "param_center:urls.cloud_url"
	CloudURLSourceBuiltin     = "builtin_default(vendor)"
)

// cloudURLProvider 是 urls.cloud_url 参数中心取值口（按 ctx 取租户覆盖）。
// 默认返回空串 = 未装配，CloudURLFor() 会继续回落到 env/常量。
//
// 做成 provider 而不是在这里调 config_param service：config 不能反向依赖 service
// （service 层 import 了 config，反向会成环），装配点在 internal/app/url_params_wiring.go。
var cloudURLProvider = func(ctx context.Context) string { return "" }

// SetCloudURLProvider 注入 urls.cloud_url 参数中心取值口。传 nil 是空操作而不是复位
// （与本仓其余 seam 一致，防止装配顺序出错把兜底顶掉）。
func SetCloudURLProvider(fn func(ctx context.Context) string) {
	if fn != nil {
		cloudURLProvider = fn
	}
}

// CloudURLFor 云端版地址：已装配平台配置 > 环境变量 PLATFORM_CLOUD_URL > 参数中心 urls.cloud_url > 占位常量。
//
// ctx 用于参数中心的租户覆盖解析：SaaS 模式绑定租户后读到本租户覆盖值，未绑定
// （启动期 / 平台级调用）回落平台默认值——不会借用别的租户的配置。
//
// 刻意与 PlatformURL 不同：PlatformURL 关闭态恒空（"没有平台可连"是合法值），
// 而云端地址是 public config 的展示字段，任何形态都要给前端一个可渲染的值
// —— 离线（PLATFORM_ENABLED=false）回落占位常量而非空串。
func CloudURLFor(ctx context.Context) string {
	u, _ := CloudURLSource(ctx)
	return u
}

// CloudURLSource 返回生效的云端版地址与它的来源标签。
//
// 抽出 source 的理由：`CloudURLFor` 只回答"是什么"，还需要回答"**这个值是谁给的**"
// ——没配置时它会安静地等于厂商域名，只比较字符串无法区分
// "运维显式填了同一个域名"和"什么都没填"。启动期据此打响声。
func CloudURLSource(ctx context.Context) (url, source string) {
	if PlatformCfg != nil && strings.TrimSpace(PlatformCfg.CloudURL) != "" {
		return strings.TrimSpace(PlatformCfg.CloudURL), CloudURLSourcePlatformCfg
	}
	if v := strings.TrimSpace(os.Getenv("PLATFORM_CLOUD_URL")); v != "" {
		return v, CloudURLSourceEnv
	}
	if v := strings.TrimSpace(cloudURLProvider(ctx)); v != "" {
		return v, CloudURLSourceParamCenter
	}
	return DefaultCloudURL, CloudURLSourceBuiltin
}

// CloudURL 无 ctx 形态：平台级调用（无租户归属）等价于未绑定租户读取。
func CloudURL() string { return CloudURLFor(context.Background()) }

// PlatformCfg 全局平台配置（运行时唯一实例）：仅承载"上报地址 + 签名密钥"。
var PlatformCfg *PlatformConfig

// PlatformEnabled 平台集成总开关。默认关闭 = 纯本地模式：
// 不加载平台配置、不注册商户、不心跳、不拉市场，且这条链路上一次网络请求都不发。
//
// 判真的方向刻意保守：只有明确真值才开，拼错/空/陌生值一律关。
// 因为"意外开启"= 朝一个可能已下线的地址发请求，"意外关闭"= 少一个可选功能。
//
// 拼错**不报错**（与 TENANT_MODE 不同，这是有意的）：
//   - TENANT_MODE 拼错 ⇒ SaaS 部署的所有租户数据被写进默认租户 1 ⇒ 数据损坏，
//     所以那边是"拒绝启动"（tenant.InitFromEnv）。
//   - PLATFORM_ENABLED 拼错 ⇒ 只是少一个可选功能（资产市场/模块下发/心跳），
//     业务数据完好 ⇒ 那边是"保守关闭"。
//
// 但**沉默关闭**有个真实危害：运维看到日志写"PLATFORM_ENABLED 未设置"就以为
// 平台没接、开始查别的方向，而真相是"你拼错了开关"。
// ⇒ 这里只回 bool（判真方向不变），把"是拼错还是没设"的区分交给
// ResolvePlatformEnabled，由调用方在启动日志里如实说。
func PlatformEnabled() bool {
	enabled, _ := ResolvePlatformEnabled(os.Getenv("PLATFORM_ENABLED"))
	return enabled
}

// ResolvePlatformEnabled 解析 PLATFORM_ENABLED 环境变量值。
// 返回 (是否开启, 取值是否已知)：
//   - 空串/未设置 → false+true（明确关闭）
//   - true/1/on/yes → true+true
//   - false/0/off/no → false+true
//   - 其他值 → false+**false**（拼错：保守关闭，但让调用方能说清真相）
//
// 大小写不敏感，容忍首尾空白。
func ResolvePlatformEnabled(env string) (bool, bool) {
	v := strings.ToLower(strings.TrimSpace(env))
	switch v {
	case "":
		return false, true
	case "true", "1", "on", "yes":
		return true, true
	case "false", "0", "off", "no":
		return false, true
	default:
		return false, false
	}
}

// platformHostProvider 是 platform.api_host 参数中心的取值口。
//
// 做成 provider 而不是在这里调 config_param service：config 不能反向依赖 service
// （service 层 import 了 config，反向会成环），装配点在 internal/app/url_params_wiring.go。
// 与 cloudURLProvider 同一形状，刻意不注入启动瞬间的值：每次调用现读参数中心，
// 值改完 60s TTL 缓存过期后即生效，不需要重启。
var platformHostProvider = func(ctx context.Context) string { return "" }

// SetPlatformHostProvider 注入 platform.api_host 参数中心取值口。传 nil 是空操作而不是复位
// （与本仓其余 seam 一致，防止装配顺序出错把兜底顶掉）。
func SetPlatformHostProvider(fn func(ctx context.Context) string) {
	if fn != nil {
		platformHostProvider = fn
	}
}

// PlatformURL 解析平台对外地址：关闭态恒为空串；开启态取
// 参数中心（platform.api_host）> 已装配配置 > 环境变量 > 空串。
//
// **参数中心优先于 env 是刻意的**，与 urls.* 组那几条（env 优先）方向相反：
// urls.* 面向的是"部署契约"（env 是运维的最终解释权所在），而平台地址面向的是
// "平台迁移应对"——用户端改地址必须能压住部署时留在 .env 里的旧值，否则这条参数
// 改了不生效，等于没做。
//
// 空串是合法值，含义是"没有平台可连"，所以这里刻意不设任何默认域名：
// 出站路径本来就各自判 config.PlatformCfg == nil 快速失败，
// 回落一个具体域名只会让请求打向黑洞、把日志刷成 Error，换不来任何功能。
func PlatformURL() string {
	return PlatformURLFor(context.Background())
}

// PlatformURLFor 带 ctx 形态的地址解析。
//
// ctx 目前**不参与**优先级判定（平台地址是部署级事实，不是租户级事实，见
// platformScopedParams 白名单），但保留 ctx 是为了：
//  1. 将来若出现"私有平台 + 公有平台"并存的多平台场景，不必再改全部调用点；
//  2. 与 CloudURLFor / WebsiteBaseURLFor 的形状保持一致，读代码时不用问"这个为什么没有 ctx"。
//
// 无 ctx 形态等价于未绑定租户读取。
func PlatformURLFor(ctx context.Context) string {
	if !PlatformEnabled() {
		return ""
	}
	if v := strings.TrimSpace(platformHostProvider(ctx)); v != "" {
		return v
	}
	if PlatformCfg != nil && PlatformCfg.APIURL != "" {
		return PlatformCfg.APIURL
	}
	for _, k := range []string{"PLATFORM_API_HOST", "PLATFORM_API_URL"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// defaultPlatformConfigPath 是平台配置的默认相对路径（相对 CWD，找不到再按二进制目录回落）。
const defaultPlatformConfigPath = "config/platform.yaml"

// platformConfigRootDir 是"二进制所在目录"的取值出口，单独成变量是为了让
// 回落用例能把该目录指到临时目录（生产恒为 config.GetRootDir）。
var platformConfigRootDir = GetRootDir

// LoadPlatform 从 YAML 文件加载平台配置
//
// 开关：PLATFORM_ENABLED 未开启时直接早退并把 PlatformCfg 置 nil，
// 不读文件、不校验字段——nil 就是下游每条出站路径共用的"快速失败"信号。
//
// 失败策略（仅开启态）：
//   - 必填字段缺失（secret）→ 返回错误，且不装配半截配置
//   - 可选字段缺失 → 使用 0 值
//
// api_url 的必填判定是**解析后的最终地址**而不是 yaml 字段本身：
// 地址可能来自参数中心 platform.api_host（管理端可改），那时装配期 yaml 的
// api_url 本来就允许为空。只判 yaml 会让"在数据库里配地址"这条路径在启动期
// 直接报错——功能还没上线就先把门堵死。
//
// 路径：
//   - 默认 config/platform.yaml（按 CWD 解析；CWD 下没有时按**二进制所在目录**再找一次，
//     因为部署时 CWD 常与二进制目录不同，见 LoadPlatform 里的回落注释）
//   - 可通过环境变量 PLATFORM_CONFIG_PATH 覆盖（覆盖后不做任何回落）
func LoadPlatform(path string) error {
	if !PlatformEnabled() {
		PlatformCfg = nil
		return nil
	}
	if p := os.Getenv("PLATFORM_CONFIG_PATH"); p != "" {
		path = p
	}
	data, err := os.ReadFile(path)
	if err != nil && os.IsNotExist(err) && os.Getenv("PLATFORM_CONFIG_PATH") == "" &&
		path == defaultPlatformConfigPath {
		// 兜底：只对"默认相对路径"生效。config/ 是随二进制分发的目录，但进程的 CWD
		// 不一定是 user-server/（systemd 的 WorkingDirectory、从仓库根起、桌面端双击
		// 二进制、容器里换个 WORKDIR 都不同）。不兜底时这里会"读失败→PlatformCfg=nil"，
		// 而后果是**静默降级**：商户注册、心跳、资产市场、模块下发全哑，日志只有一行
		// error。显式传进来的路径（含 PLATFORM_CONFIG_PATH）找不到就是找不到，不做猜测。
		if alt := filepath.Join(platformConfigRootDir(), defaultPlatformConfigPath); alt != path {
			if b, altErr := os.ReadFile(alt); altErr == nil {
				data, err, path = b, nil, alt
			}
		}
	}
	if err != nil {
		return fmt.Errorf("读取平台配置失败 (%s): %w", path, err)
	}
	expanded := os.ExpandEnv(string(data))
	var cfg PlatformConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return fmt.Errorf("解析平台配置失败: %w", err)
	}
	if v := os.Getenv("PLATFORM_API_HOST"); v != "" {
		cfg.APIURL = v
	} else if v := os.Getenv("PLATFORM_API_URL"); v != "" {
		cfg.APIURL = v
	}
	if cfg.Secret == "" {
		return fmt.Errorf("平台配置缺少必填字段 secret（HMAC 签名密钥）")
	}
	PlatformCfg = &cfg
	// 装配后再判地址：此刻 PlatformCfg 已就位，PlatformURL() 才能按
	// 参数中心 > yaml/env 的真实优先级解析出最终值。
	if PlatformURL() == "" {
		return fmt.Errorf("平台地址未配置（请在管理端参数中心设置 platform.api_host，" +
			"或通过环境变量 PLATFORM_API_HOST/PLATFORM_API_URL 或 config/platform.yaml 的 api_url 指定；容器内禁止留空）")
	}
	return nil
}
