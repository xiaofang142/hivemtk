package channelcaps

import "testing"

// TestManifestRenderFieldsNonEmpty 每个渠道都必须声明出站格式（specs/024 T401）。
// 缺字段的渠道在渲染层会被当成「未知格式」，只能靠默认值猜。
func TestManifestRenderFieldsNonEmpty(t *testing.T) {
	for _, m := range All() {
		if m.OutboundFormat == "" {
			t.Fatalf("%s 未声明 OutboundFormat", m.Channel)
		}
		if !IsKnownOutboundFormat(m.OutboundFormat) {
			t.Fatalf("%s 的 OutboundFormat=%q 不在词表里", m.Channel, m.OutboundFormat)
		}
		if m.MaxTextLen < 0 {
			t.Fatalf("%s 的 MaxTextLen 不能为负", m.Channel)
		}
	}
}

// TestManifestRenderFormatHasEvidence 格式声明必须有证据锚（specs/024 T401）：
// 没有证据的声明等于猜的——出站行为一变就没人知道该改哪一行。
func TestManifestRenderFormatHasEvidence(t *testing.T) {
	for _, m := range All() {
		if m.FormatEvidence == "" {
			t.Fatalf("%s 的 OutboundFormat 没有证据锚（FormatEvidence 为空）", m.Channel)
		}
	}
}

// TestManifestRichCardOnlyWhereImplemented 只有真正带 FeatureRichCard 的渠道才算有卡片载体。
func TestManifestRichCardOnlyWhereImplemented(t *testing.T) {
	want := map[string]bool{"telegram": true, "feishu": true}
	for _, m := range All() {
		if got := SupportsRichCards(m.Channel); got != want[m.Channel] {
			t.Fatalf("%s SupportsRichCards=%v，实际实现=%v", m.Channel, got, want[m.Channel])
		}
	}
	if SupportsRichCards("not-a-channel") {
		t.Fatal("未知渠道不得宣称有卡片载体")
	}
}
