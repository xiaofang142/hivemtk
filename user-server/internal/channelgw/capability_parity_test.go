package channelgw_test

import (
	"testing"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/channelcaps"
	"hivemtk-user/internal/channelgw"
)

// G-08：三套能力登记表（channelgw 线路注册表 / capregistry 粗粒度能力 /
// channelcaps 消息级 manifest）暂时不合并为一个包，但必须双向钉死：
//   - 桥接渠道在 channelgw 注册 ⇒ capregistry 有收+发能力 ⇒ channelcaps 有 manifest；
//   - capregistry 声明 Via=bridge 的收/发渠道 ⇒ channelgw 必须注册，否则能力宣传无线路支撑。
//
// 这是「能力描述单源」的可执行前置门：任何一边偷偷加渠道/删能力都会红，
// 防止再出现「文档说支持、网关拒收」这类口径漂移。
func TestBridgeCapabilityThreeRegistriesParity(t *testing.T) {
	caps := map[string]map[capregistry.Capability]bool{}
	for _, e := range capregistry.All() {
		if e.Via != "bridge" {
			continue
		}
		if e.Cap != capregistry.CapDMReceive && e.Cap != capregistry.CapDMSend {
			continue
		}
		name := channelgw.NormalizeChannel(e.Platform)
		if name == "" {
			t.Errorf("capregistry 存在空平台桥接能力登记: %+v", e)
			continue
		}
		if caps[name] == nil {
			caps[name] = map[capregistry.Capability]bool{}
		}
		caps[name][e.Cap] = true
		if !channelgw.Default.IsChannel(name) {
			t.Errorf("capregistry 桥接能力 %s/%s 在 channelgw.Registry 无线路注册", name, e.Cap)
		}
		if !channelcaps.Known(name) {
			t.Errorf("capregistry 桥接能力 %s/%s 在 channelcaps 无消息级 manifest", name, e.Cap)
		}
	}

	for _, name := range channelgw.Default.Names() {
		if !caps[name][capregistry.CapDMReceive] || !caps[name][capregistry.CapDMSend] {
			t.Errorf("channelgw 桥接渠道 %s 在 capregistry 缺少收/发能力登记（%v）", name, caps[name])
		}
		if !channelcaps.Known(name) {
			t.Errorf("channelgw 桥接渠道 %s 在 channelcaps 无消息级 manifest", name)
		}
		spec, _ := channelgw.Default.Spec(name)
		if spec.OutboundPath == channelgw.OutboundPathBridgeOutbox && !caps[name][capregistry.CapDMSend] {
			t.Errorf("channelgw 渠道 %s 声明 bridge_outbox 但 capregistry 无 CapDMSend", name)
		}
	}
	if len(caps) == 0 {
		t.Fatal("未校验到任何桥接能力登记：parity 门失效")
	}
	t.Logf("已双向校验 %d 个桥接渠道的三套能力登记一致", len(channelgw.Default.Names()))
}
