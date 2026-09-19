package channelgw

import (
	"testing"

	"hivemtk-user/internal/model"
)

// TestRegistry_RegisterAndQuery 注册/查询/覆盖/并发读基础行为。
func TestRegistry_RegisterAndQuery(t *testing.T) {
	r := NewRegistry()
	r.Register(ChannelSpec{Name: "demo", Transports: []Transport{TransportHTTP}, Label: "演示"})

	if !r.IsChannel("demo") {
		t.Error("IsChannel(demo) = false, want true")
	}
	if r.IsChannel("unknown") {
		t.Error("未注册渠道不应命中")
	}
	if !r.Supports("demo", TransportHTTP) {
		t.Error("demo 应支持 http")
	}
	if r.Supports("demo", TransportWebSocket) {
		t.Error("demo 未声明 websocket，不应支持")
	}
	if r.Supports("unknown", TransportHTTP) {
		t.Error("未注册渠道 Supports 应为 false")
	}

	spec, ok := r.Spec("demo")
	if !ok || spec.Label != "演示" {
		t.Errorf("Spec 查询错误: %+v ok=%v", spec, ok)
	}
	if names := r.Names(); len(names) != 1 || names[0] != "demo" {
		t.Errorf("Names = %v, want [demo]", names)
	}

	r.Register(ChannelSpec{Name: "demo", Transports: []Transport{TransportHTTP, TransportWebSocket}})
	if !r.Supports("demo", TransportWebSocket) {
		t.Error("同名覆盖后应支持 websocket")
	}

	r.Register(ChannelSpec{Name: ""})
	if r.IsChannel("") {
		t.Error("空名注册应被忽略")
	}
}

// TestKuaishouExperimentalDeclared B8（2026-09-19）：快手是唯一实验渠道——选择器
// 纯模式猜测、零真机校准，注册处如实降级；其余四渠道保持生产声明。
// 若快手完成真机校准，翻转 spec 的 Experimental 并同步 bridge constants.js。
func TestKuaishouExperimentalDeclared(t *testing.T) {
	if !Default.IsExperimental(model.ChannelKuaishou) {
		t.Error("kuaishou 应登记为 experimental")
	}
	for _, name := range []string{model.ChannelDouyin, model.ChannelXHS, model.ChannelXianyu, model.ChannelTikTok} {
		if Default.IsExperimental(name) {
			t.Errorf("%s 不应标 experimental", name)
		}
	}
	// 降级不删链路：五渠道仍全量可查
	for _, name := range Default.Names() {
		if !Default.IsChannel(name) {
			t.Errorf("Names/IsChannel 不一致: %s", name)
		}
	}
	if Default.IsExperimental("unknown-channel") {
		t.Error("未注册渠道不应命中 IsExperimental")
	}
}

// TestDefaultRegistry 默认注册表覆盖 5 大社交渠道，且均支持 HTTP + WebSocket 双传输。
func TestDefaultRegistry(t *testing.T) {
	want := []string{
		model.ChannelDouyin,
		model.ChannelXHS,
		model.ChannelKuaishou,
		model.ChannelXianyu,
		model.ChannelTikTok,
	}
	for _, ch := range want {
		if !Default.IsChannel(ch) {
			t.Errorf("Default 缺渠道 %s", ch)
			continue
		}
		if !Default.Supports(ch, TransportHTTP) {
			t.Errorf("渠道 %s 应支持 http", ch)
		}
		if !Default.Supports(ch, TransportWebSocket) {
			t.Errorf("渠道 %s 应支持 websocket", ch)
		}
	}
	if len(Default.Names()) != len(want) {
		t.Errorf("Default 渠道数 = %d, want %d", len(Default.Names()), len(want))
	}
}
