package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlatformConfig 平台上报配置（开源版精简版）
//
// 背景：hivemtk 全面开源后不再有 License / 授权同步，
// 这里只保留"商户上报"用到的配置（APIURL / Secret / Admin*）。
// 兼容老字段（LogReportInterval / LicenseSyncInterval）仅作占位，
// 读不到也允许（给前端 / 监控保持 schema 兼容）。
type PlatformConfig struct {
	APIURL              string `yaml:"api_url" json:"api_url"`
	Secret              string `yaml:"secret" json:"secret"`
	AdminUsername       string `yaml:"admin_username" json:"admin_username"`
	AdminPassword       string `yaml:"admin_password" json:"admin_password"`
	LogReportInterval   int    `yaml:"log_report_interval" json:"log_report_interval"`
	LicenseSyncInterval int    `yaml:"license_sync_interval" json:"license_sync_interval"`
}

// PlatformCfg 全局平台配置（运行时唯一实例）
//
// 开源版：仅承载"上报地址 + 签名密钥 + 平台管理员账号"，
// 不再做 License 授权同步 / 续期 / 校验。
var PlatformCfg *PlatformConfig

// PlatformEnabled 平台集成总开关。默认关闭 = 纯本地模式：
// 不加载平台配置、不注册商户、不心跳、不拉市场，且这条链路上一次网络请求都不发。
//
// 判真的方向刻意保守：只有明确真值才开，拼错/空/陌生值一律关。
// 因为"意外开启"= 朝一个可能已下线的地址发请求，"意外关闭"= 少一个可选功能。
func PlatformEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_ENABLED"))) {
	case "true", "1", "on", "yes":
		return true
	}
	return false
}

// PlatformURL 解析平台对外地址：关闭态恒为空串；开启态取 已装配配置 > 环境变量 > 空串。
//
// 空串是合法值，含义是"没有平台可连"，所以这里刻意不设任何默认域名：
// 出站路径本来就各自判 config.PlatformCfg == nil 快速失败，
// 回落一个具体域名只会让请求打向黑洞、把日志刷成 Error，换不来任何功能。
func PlatformURL() string {
	if !PlatformEnabled() {
		return ""
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

// LoadPlatform 从 YAML 文件加载平台配置
//
// 开关：PLATFORM_ENABLED 未开启时直接早退并把 PlatformCfg 置 nil，
// 不读文件、不校验字段——nil 就是下游每条出站路径共用的"快速失败"信号。
//
// 失败策略（仅开启态）：
//   - 必填字段缺失（api_url / secret / admin_password）→ 返回错误，且不装配半截配置
//   - 可选字段缺失 → 使用 0 值
//
// 路径：
//   - 默认 config/platform.yaml
//   - 可通过环境变量 PLATFORM_CONFIG_PATH 覆盖
func LoadPlatform(path string) error {
	if !PlatformEnabled() {
		PlatformCfg = nil
		return nil
	}
	if p := os.Getenv("PLATFORM_CONFIG_PATH"); p != "" {
		path = p
	}
	data, err := os.ReadFile(path)
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
	if cfg.APIURL == "" {
		return fmt.Errorf("平台配置缺少必填字段 api_url（请通过环境变量 PLATFORM_API_HOST/PLATFORM_API_URL 或 config/platform.yaml 的 api_url 指定，容器内禁止留空）")
	}
	if cfg.Secret == "" {
		return fmt.Errorf("平台配置缺少必填字段 secret（HMAC 签名密钥）")
	}
	if cfg.AdminPassword == "" {
		return fmt.Errorf("平台配置缺少必填字段 admin_password（用于 /platform/* JWT 登录）")
	}
	PlatformCfg = &cfg
	return nil
}
