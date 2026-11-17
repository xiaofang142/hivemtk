// Package channelcaps 渠道「消息级」能力清单（借鉴 LangBot manifest.yaml 的声明式能力契约）。
//
// 与 internal/capregistry 的分工：
//   - capregistry 回答「这个渠道能不能接入 / 能不能发」（渠道 × 粗粒度能力，服务端拓扑）；
//   - channelcaps 回答「这条消息能不能以某种形态收/发」（渠道 × 消息类型 / 交互特征，
//     出站渲染、富卡降级、能力提示词都从这里取事实）。
//
// 口径（重要）：
//  1. 只登记**代码里已落地**的路径，未实现或未验证的一律不登记——能力表宁可窄，不可宽；
//     宽表会让 AI 提示词和降级逻辑误以为载体存在（例如把卡片发给只能收文字的渠道）。
//  2. 新增渠道 / 新增消息形态：在 manifests.go 登记一行，生成文档由测试钉死
//     （docs/channel-capabilities.md），文档漂移即红。
package channelcaps

import (
	"fmt"
	"sort"
	"strings"

	"hivemtk-user/internal/model"
)

// Feature 渠道的交互特征（非消息类型维度的能力）。
type Feature string

const (
	// FeatureRichCard 该渠道有真正的卡片载体（而不是把卡片压成文字）。
	FeatureRichCard Feature = "rich_card"
	// FeatureGroup 支持群聊会话。
	FeatureGroup Feature = "group"
	// FeatureMention 支持群里 @ 指定成员。
	FeatureMention Feature = "mention"
	// FeatureStream 支持流式分片下发（当前无渠道落地）。
	FeatureStream Feature = "stream"
	// FeatureRecall 支持撤回已发送消息。
	FeatureRecall Feature = "recall"
	// FeatureInteraction 支持可交互卡片回执（点击后能回 ack，不让客户端转圈）。
	// 只有真正实现了 channelbot/core.InteractionAnswerer 的渠道才有此位（specs/024 T210）。
	FeatureInteraction Feature = "interaction"
)

// Manifest 一个渠道的能力manifest。Inbound/Outbound 用 model.MsgType* 词表。
type Manifest struct {
	Channel  string
	Label    string
	Via      string // webhook / bridge / widget / api
	Inbound  []string
	Outbound []string
	// Events 该渠道解析器能产出的平台事件类型（specs/024 T206）。
	// 取值必须来自 model.EventType*；nil = 只有 message.received（纯消息渠道）。
	Events   []string
	Features []Feature
	// OutboundFormat 出站正文的目标格式（specs/024 T401）：plain | markdown | html。
	// 出站渲染层按这个值决定要不要剥 Markdown 语法；不认识的值一律视为配置错误。
	OutboundFormat string
	// MaxTextLen 平台单条正文的字符上限（0 = 不设上限）。
	// 只用于溢出计数（outbound_text_overflow），**不做切分**：
	// 切分各渠道自己负责（QQ 在 channelbot/qq 内 splitQQMessage），两处各切一次会把一句话劈成两半。
	MaxTextLen int
	// FormatEvidence 格式取值的证据锚（代码位置或协议事实），空 = 未验证。
	// 没有证据的格式声明等于猜的，出站行为一变就没人知道该改哪。
	FormatEvidence string
	Note           string
}

// 出站正文格式词表。
const (
	OutboundFormatPlain    = "plain"
	OutboundFormatMarkdown = "markdown"
	OutboundFormatHTML     = "html"
)

// IsKnownOutboundFormat 判定格式取值是否登记过。
func IsKnownOutboundFormat(f string) bool {
	switch f {
	case OutboundFormatPlain, OutboundFormatMarkdown, OutboundFormatHTML:
		return true
	}
	return false
}

var manifests = map[string]Manifest{}

// Register 登记一个渠道的能力（重复登记 panic：能力表是契约，不是配置）。
func Register(m Manifest) {
	if m.Channel == "" {
		panic("channelcaps: 渠道名不能为空")
	}
	if _, ok := manifests[m.Channel]; ok {
		panic(fmt.Sprintf("channelcaps: %s 重复登记", m.Channel))
	}
	if len(m.Events) == 0 {
		// 默认值写在注册口而不是每个 manifest：13 个渠道里多数只产消息，
		// 让「纯消息渠道」零声明，事件渠道才需要显式登记。
		m.Events = []string{model.EventTypeMessageReceived}
	}
	if m.OutboundFormat == "" {
		// 纯触达渠道（sms/custom 之外）默认按纯文本：宁可少剥(format=plain 会剥 Markdown)，
		// 也不能把 HTML 标签原样投进不认 HTML 的输入框。
		m.OutboundFormat = OutboundFormatPlain
	}
	if m.MaxTextLen < 0 {
		m.MaxTextLen = 0
	}
	m.Inbound = dedupSorted(m.Inbound)
	m.Events = dedupSorted(m.Events)
	m.Outbound = dedupSorted(m.Outbound)
	m.Features = dedupFeatures(m.Features)
	manifests[m.Channel] = m
}

func dedupSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func dedupFeatures(in []Feature) []Feature {
	seen := make(map[Feature]struct{}, len(in))
	out := make([]Feature, 0, len(in))
	for _, f := range in {
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// All 返回全部 manifest（按渠道名稳定排序）。
func All() []Manifest {
	out := make([]Manifest, 0, len(manifests))
	for _, m := range manifests {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out
}

// Get 返回渠道 manifest（未知渠道 ok=false）。
func Get(channel string) (Manifest, bool) {
	m, ok := manifests[channel]
	return m, ok
}

// Known 报告渠道是否已登记。
func Known(channel string) bool {
	_, ok := manifests[channel]
	return ok
}

// SupportsInbound 该渠道是否能把该消息类型交进来。
func SupportsInbound(channel, msgType string) bool {
	m, ok := manifests[channel]
	return ok && contains(m.Inbound, msgType)
}

// SupportsOutbound 该渠道是否能以该消息类型发出去。
func SupportsOutbound(channel, msgType string) bool {
	m, ok := manifests[channel]
	return ok && contains(m.Outbound, msgType)
}

// SupportsRichCards 该渠道是否有真正的卡片载体（未知渠道恒 false）。
//
// 这是「发得出去吗」的唯一事实源：出站渲染与卡片降级都从这里取，
// 不再各自维护渠道白名单（白名单多一处就多一处漂移）。
func SupportsRichCards(channel string) bool {
	return Has(channel, FeatureRichCard)
}

// OutboundFormatOf 该渠道出站正文的目标格式；未知渠道返回空串（调用方必须报错）。
func OutboundFormatOf(channel string) string {
	if m, ok := manifests[channel]; ok {
		return m.OutboundFormat
	}
	return ""
}

// MaxTextLenOf 该渠道单条正文上限；未知渠道或未设上限返回 0。
func MaxTextLenOf(channel string) int {
	if m, ok := manifests[channel]; ok {
		return m.MaxTextLen
	}
	return 0
}

// Has 该渠道是否具备某交互特征（未知渠道恒 false：不认识的渠道不许宣称能力）。
func Has(channel string, f Feature) bool {
	m, ok := manifests[channel]
	if !ok {
		return false
	}
	for _, got := range m.Features {
		if got == f {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, got := range list {
		if got == v {
			return true
		}
	}
	return false
}

// Markdown 生成能力文档（文档 = 注册表渲染，禁止手改）。
func Markdown() string {
	var b strings.Builder
	b.WriteString("# 渠道消息级能力表\n\n")
	b.WriteString("> 本文件由 `internal/channelcaps` 生成（channelcaps.Markdown），测试钉死与注册表一致，**请勿手改**。\n")
	b.WriteString("> 口径：只登记代码已落地的收发路径；未登记 = 未实现/未验证，出站降级与 AI 能力提示词一律按「不支持」处理。\n\n")
	b.WriteString("| 渠道 | 名称 | 接入 | 入站消息 | 出站消息 | 平台事件 | 出站格式 | 文本上限 | 交互特征 | 说明 |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, m := range All() {
		features := make([]string, 0, len(m.Features))
		for _, f := range m.Features {
			features = append(features, "`"+string(f)+"`")
		}
		if len(features) == 0 {
			features = []string{"—"}
		}
		maxLen := "—"
		if m.MaxTextLen > 0 {
			maxLen = fmt.Sprintf("%d", m.MaxTextLen)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			m.Channel, m.Label, m.Via, codeList(m.Inbound), codeList(m.Outbound),
			codeList(m.Events), "`"+m.OutboundFormat+"`", maxLen,
			strings.Join(features, " "), strings.ReplaceAll(m.Note, "|", "\\|"))
	}
	return b.String()
}

func codeList(list []string) string {
	if len(list) == 0 {
		return "—"
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, "`"+v+"`")
	}
	return strings.Join(out, " ")
}
