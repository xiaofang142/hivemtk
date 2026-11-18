package channelcaps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// 能力文档 = 注册表渲染，漂移即红（同 capregistry 的文档新鲜度口径）。
// 更新方式：UPDATE_GOLDEN=1 go test ./internal/channelcaps/ 后 diff 入库 docs/channel-capabilities.md。
func TestCapabilityDocFreshness(t *testing.T) {
	got := Markdown()
	path := filepath.Join("..", "..", "docs", "channel-capabilities.md")

	want, err := os.ReadFile(path)
	if err != nil || os.Getenv("UPDATE_GOLDEN") == "1" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if werr := os.WriteFile(path, []byte(got), 0o644); werr != nil {
			t.Fatalf("写渠道能力表失败: %v", werr)
		}
		if err != nil {
			t.Skipf("channel-capabilities.md 不存在，已生成基线: %s（请 diff 后入库）", path)
		}
		t.Skipf("UPDATE_GOLDEN=1：channel-capabilities.md 已刷新，请 diff 后入库")
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(got) {
		t.Fatalf("docs/channel-capabilities.md 与注册表不一致：改能力请先改 internal/channelcaps/manifests.go，再 UPDATE_GOLDEN=1 重新生成")
	}
}

// 每个 model.Channel* 常量都必须有 manifest：新增渠道不许静默缺失能力声明，
// 否则出站渲染与 AI 能力提示词会按「未知渠道 = 无能力」误判。
func TestEveryChannelConstantHasManifest(t *testing.T) {
	for _, channel := range []string{
		model.ChannelWeb, model.ChannelTelegram, model.ChannelWhatsApp, model.ChannelXHS,
		model.ChannelWeCom, model.ChannelXianyu, model.ChannelDouyin, model.ChannelKuaishou,
		model.ChannelTikTok, model.ChannelJD, model.ChannelBilibili, model.ChannelWeibo,
		model.ChannelPersonalWX, model.ChannelPDD, model.ChannelEmail, model.ChannelFeishu,
		model.ChannelDingTalk, model.ChannelQQ,
	} {
		if !Known(channel) {
			t.Errorf("渠道 %s 缺少 manifest（internal/channelcaps/manifests.go）", channel)
		}
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("重复登记应 panic")
		}
	}()
	Register(Manifest{Channel: model.ChannelTelegram, Label: "重复"})
}

func TestUnknownChannelClaimsNothing(t *testing.T) {
	const ghost = "not_a_channel"
	if Known(ghost) {
		t.Fatal("未登记渠道不应 Known")
	}
	if Has(ghost, FeatureRichCard) || SupportsInbound(ghost, model.MsgTypeText) || SupportsOutbound(ghost, model.MsgTypeText) {
		t.Fatal("未登记渠道不得宣称任何能力")
	}
}

func TestRichCardOnlyWhereSenderRendersCards(t *testing.T) {
	// 与 channel_card_delivery.go 的事实对齐：只有飞书/Telegram 是真卡片载体。
	for _, channel := range []string{model.ChannelFeishu, model.ChannelTelegram} {
		if !Has(channel, FeatureRichCard) || !SupportsOutbound(channel, model.MsgTypeCard) {
			t.Fatalf("%s 必须声明富卡能力", channel)
		}
	}
	for _, channel := range []string{model.ChannelDouyin, model.ChannelXHS, model.ChannelWeCom, model.ChannelWhatsApp, model.ChannelQQ, model.ChannelDingTalk, model.ChannelWeb, model.ChannelEmail} {
		if Has(channel, FeatureRichCard) || SupportsOutbound(channel, model.MsgTypeCard) {
			t.Fatalf("%s 没有卡片载体，不得声明富卡", channel)
		}
	}
}

func TestBridgeChannelsAllDeclareText(t *testing.T) {
	for _, channel := range []string{
		model.ChannelDouyin, model.ChannelXHS, model.ChannelKuaishou, model.ChannelTikTok,
		model.ChannelXianyu, model.ChannelJD, model.ChannelBilibili, model.ChannelWeibo,
		model.ChannelPersonalWX, model.ChannelPDD,
	} {
		m, ok := Get(channel)
		if !ok {
			t.Fatalf("桥接渠道 %s 未登记", channel)
		}
		if m.Via != "bridge" {
			t.Errorf("%s 接入方式应为 bridge，得 %s", channel, m.Via)
		}
		if !SupportsInbound(channel, model.MsgTypeText) || !SupportsOutbound(channel, model.MsgTypeText) {
			t.Errorf("%s 至少要有文字收发", channel)
		}
	}
}

func TestAllSortedAndStable(t *testing.T) {
	a, b := All(), All()
	if len(a) == 0 {
		t.Fatal("注册表为空")
	}
	for i := range a {
		if a[i].Channel != b[i].Channel {
			t.Fatal("All() 顺序不稳定")
		}
		if i > 0 && a[i-1].Channel > a[i].Channel {
			t.Fatal("All() 未按渠道名排序")
		}
	}
}

// TestEveryManifestDeclaresAttachmentLimit 附件上限必须逐条有声明与证据锚（specs/025 M-2）：
// 空证据 = 猜的，校验落点在渠道客户端时没人知道该按哪个数拒收。
func TestEveryManifestDeclaresAttachmentLimit(t *testing.T) {
	for _, m := range All() {
		if m.MaxAttachments < 0 {
			t.Errorf("%s: MaxAttachments 不能为负（%d）", m.Channel, m.MaxAttachments)
		}
		if m.MaxAttachmentBytes < 0 {
			t.Errorf("%s: MaxAttachmentBytes 不能为负（%d）", m.Channel, m.MaxAttachmentBytes)
		}
		if strings.TrimSpace(m.AttachmentEvidence) == "" {
			t.Errorf("%s: 缺少 AttachmentEvidence（0 上限也必须写明「无附件通道」或「未校验」的依据）", m.Channel)
		}
		if mediaInbound(m.Inbound) && m.MaxAttachments == 0 && !strings.Contains(m.AttachmentEvidence, "无") && !strings.Contains(m.AttachmentEvidence, "未校验") && !strings.Contains(m.AttachmentEvidence, "不校验") {
			t.Errorf("%s: 入站支持媒体但附件数为 0，证据未说明原因：%q", m.Channel, m.AttachmentEvidence)
		}
	}
}

func mediaInbound(types []string) bool {
	for _, mt := range types {
		switch mt {
		case model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo:
			return true
		}
	}
	return false
}

func TestAttachmentLimitOf(t *testing.T) {
	got := AttachmentLimitOf(model.ChannelTelegram)
	if got.MaxCount != 10 || got.MaxBytes != 20*1024*1024 {
		t.Fatalf("telegram 附件上限读数错误：%+v", got)
	}
	if got := AttachmentLimitOf("not_a_channel"); got != (AttachmentLimit{}) {
		t.Fatalf("未知渠道应返回零值，实际 %+v", got)
	}
}
