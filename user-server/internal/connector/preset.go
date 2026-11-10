package connector

// preset.go §3.12 刀1：连接器 preset 注册表。
//
// TB-06b 只交付了方言层（Dialect 注册表）与手动 Ping；端点要以「数据」形态成为
// 工具，还需要一份 preset 注册面：一个 preset = 一台三方平台（ConnectorConfig）
// + 它的端点清单（[]EndpointConfig，含参数映射与能力标签）。
//
// 契约冻结（§3.12 刀1，与源码既有契约分开标注）：
//   - preset 名全局唯一，重复注册 panic（配置错误即暴露，沿方言 Register 同款）；
//   - ListPresets 升序（清单面/vocab 消费）；
//   - 桥接语义（tool_name/能力标签→装饰器映射/负例 fail-closed）见 bridge.go。

import (
	"fmt"
	"sort"
	"sync"
)

// ConnectorPreset 连接器 preset（一台三方平台的接入数据包）。
// Name 即 tool_name 第二段（connector.<preset>.<endpoint>）；Config 携带基址/
// 方言 ID/鉴权形态；Endpoints 描述可暴露端点（能力标签必填，缺省不桥接）。
type ConnectorPreset struct {
	Name      string           `json:"name"`
	Config    ConnectorConfig  `json:"config"`
	Endpoints []EndpointConfig `json:"endpoints"`
}

var (
	presetMu     sync.RWMutex
	presetByName = map[string]ConnectorPreset{}
)

// RegisterPreset 注册 preset（重名 panic=配置错误即暴露，沿方言 Register 同款）。
func RegisterPreset(p ConnectorPreset) {
	if p.Name == "" {
		panic("connector preset name 不能为空")
	}
	presetMu.Lock()
	defer presetMu.Unlock()
	if _, dup := presetByName[p.Name]; dup {
		panic(fmt.Sprintf("connector preset already registered: %s", p.Name))
	}
	presetByName[p.Name] = p
}

// GetPreset 取 preset（未注册返回 false，调用方 fail-closed）。
func GetPreset(name string) (ConnectorPreset, bool) {
	presetMu.RLock()
	defer presetMu.RUnlock()
	p, ok := presetByName[name]
	return p, ok
}

// ListPresets preset 清单（按名升序；vocab/桥接遍历消费）。
func ListPresets() []ConnectorPreset {
	presetMu.RLock()
	defer presetMu.RUnlock()
	out := make([]ConnectorPreset, 0, len(presetByName))
	for _, p := range presetByName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PresetNames preset 名清单（升序）。
func PresetNames() []string {
	presetMu.RLock()
	defer presetMu.RUnlock()
	out := make([]string, 0, len(presetByName))
	for name := range presetByName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
