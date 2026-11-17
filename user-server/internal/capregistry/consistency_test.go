package capregistry

import (
	"testing"

	"hivemtk-user/internal/channelcaps"
)

// specs/024 T1202：capregistry ↔ channelcaps 一致性。
// 粗粒度能力矩阵（本包）里**IM 接入类能力**（收私信/发私信/官方回调）声明的平台，
// 必须在细粒度 manifest（channelcaps）中存在——对外宣传「能收发消息」而细粒度
// 查无此渠道，就是口径自相矛盾。反向不要求（channelcaps 更细，桥接/预留渠道
// 可以只有细粒度声明）；CapSMS/CapEmail/CapKBSync 是触达与知识库同步能力，
// 本就没有消息 manifest，不纳入判定。两套注册表并存是历史现实，短期不合并，
// 先用测试钉住不自相矛盾。

func TestEveryCapregistryPlatformExistsInChannelcaps(t *testing.T) {
	entries := All()
	checked := 0
	for _, e := range entries {
		if e.Cap != CapDMReceive && e.Cap != CapDMSend && e.Cap != CapWebhook {
			continue
		}
		checked++
		if e.Platform == "" {
			t.Errorf("capregistry 存在空平台的 IM 能力登记: %+v", e)
			continue
		}
		if !channelcaps.Known(e.Platform) {
			t.Errorf("capregistry 声明的 IM 能力平台 %q (%s) 在 channelcaps 细粒度 manifest 中不存在（粗细两套口径自相矛盾）",
				e.Platform, e.Cap)
		}
	}
	if checked == 0 {
		t.Fatal("未校验到任何 IM 能力条目：capregistry/channels.go 可能被清空，一致性门失效")
	}
	t.Logf("已校验 %d/%d 条粗粒度登记的 IM 能力平台全部存在于 channelcaps", checked, len(entries))
}
