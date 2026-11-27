package llm

import (
	"errors"
	"fmt"
	"strings"
)

const (
	RouteModeDirect = "direct"
	RouteModeProxy  = "proxy"
)

// ResolveRouteMode 只决定请求经本地直连还是平台代理；Source=platform 无本地 Key，强制代理。
// RouteMode 是部署显式选择，不是 Source 的别名。
func ResolveRouteMode(provider *ProviderConfig) (string, error) {
	if provider == nil {
		return "", errors.New("provider 配置为空")
	}
	mode := strings.TrimSpace(provider.RouteMode)
	if mode == "" {
		mode = RouteModeDirect
	}
	if mode != RouteModeDirect && mode != RouteModeProxy {
		return "", fmt.Errorf("非法 RouteMode %q", mode)
	}
	if strings.TrimSpace(provider.Source) == "platform" {
		return RouteModeProxy, nil
	}
	return mode, nil
}
