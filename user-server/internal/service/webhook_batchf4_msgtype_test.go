package service

// 批F-4d/e/f（审计 N-17）：各渠道官方入站消息类型名 ≠ 中台类型词表，且入站媒体读取
// 缺"超限拒收"。三件事各有一处独立的失效方式，都在这里钉住。
//
// 修复前的三种失效（按后果从重到轻）：
//  1. 企微走 hub.Push → Normalize 硬校验词表：官方 voice 不在词表（中台叫 audio）
//     ⇒ ErrMessageHubInvalidMsgType ⇒ dispatch 上抛 ⇒ 客户发的语音**连一行 hub 都没有**。
//     这不是"类型标错"，是整条消息蒸发。
//  2. WA / 飞书 / 公众号直接 repo.Create（绕过 Normalize）：行落得下，但落的是词表外的
//     "document"/"media"/"post"/"shortvideo" ⇒ 工作台按类型筛选（repository 里 msg_type = ?）
//     与 by_msg_type 统计**永远**筛不到这些行。
//  3. 飞书富文本（官方 post）顶层没有 text 键，正文只在 content[][]{tag,text} 里
//     ⇒ 修复前整段丢掉，AI 收到的是字面量 "[post]"，客户写的一屏图文不见了。
//
// 另有六处入站媒体读取用 io.LimitReader(r, max) 读到正好 max 字节：
// 文件大小 == limit 与 > limit 从读取结果上**无法区分** ⇒ 半截文件当完整原件转存、
// 回填、并在日志里写"媒体已转存"。readInboundMedia 用 limit+1 的读法把它变成可判定。
// （六 = 当前调用点数；其中 git diff 可见的"修复前就存在"的只有 4 处，另 2 处随本批
//  新文件引入、在本批内一并收口 —— 数字写死成 4 会在下次读到时对不上现场。）
//
// 官方出处（本仓取证口径：A 档连 URL 一起记）：
//   飞书 im.message.receive_v1 与各 message_type 的 content 结构
//     https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json
//   企业微信 接收消息与事件（voice/shortvideo 的 MsgType 与 MediaId）
//     https://developer.work.weixin.qq.com/document/path/90239  —— 该站是 SPA，
//     服务端渲染取不到正文（见审计 §6），故本文件**不**把企微类型表当 A 档引用：
//     未知/未核实的官方名一律只可能落到 text（见 TestN17_UnknownOfficialType...），
//     不可能落到"被拒"，取证缺口因此不会变成客户消息的损失。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// ---------------------------------------------------------------- 词表收口

// TestN17_InboundHubMsgTypeMapsOfficialNames 逐个官方名 → 中台名。
// 期望值写死而不是回读表：回读等于用被测代码测被测代码。
func TestN17_InboundHubMsgTypeMapsOfficialNames(t *testing.T) {
	cases := []struct {
		official, channel, want string
	}{
		{"voice", "企微/公众号", model.MsgTypeAudio},
		{"picture", "钉钉", model.MsgTypeImage},
		{"sticker", "飞书表情/WA 贴纸", model.MsgTypeImage},
		{"document", "WA", model.MsgTypeFile},
		{"folder", "飞书", model.MsgTypeFile},
		{"media", "飞书视频", model.MsgTypeVideo},
		{"shortvideo", "公众号/企微", model.MsgTypeVideo},
		{"post", "飞书富文本", model.MsgTypeText},
		{"richtext", "钉钉", model.MsgTypeText},
		{"mixed", "微信客服", model.MsgTypeText},
		{"merge_forward", "飞书", model.MsgTypeText},
		{"system", "飞书", model.MsgTypeText},
		{"interactive", "飞书卡片", model.MsgTypeCard},
		{"hongbao", "飞书红包", model.MsgTypeCard},
		{"order", "WA 订单", model.MsgTypeCard},
		// 恰好已在词表里的名字必须原样通过（映射表不该把它们改窄）：
		{"text", "通用", model.MsgTypeText},
		{"image", "通用", model.MsgTypeImage},
		{"audio", "飞书/钉钉/WA", model.MsgTypeAudio},
		{"video", "通用", model.MsgTypeVideo},
		{"file", "通用", model.MsgTypeFile},
		{"location", "通用", model.MsgTypeLocation},
	}
	seen := map[string]int{}
	for _, tc := range cases {
		t.Run(tc.official+"/"+tc.channel, func(t *testing.T) {
			got := InboundHubMsgType(tc.official)
			if got != tc.want {
				t.Errorf("InboundHubMsgType(%q) = %q, want %q", tc.official, got, tc.want)
			}
			if !messageHubMsgTypes[got] {
				t.Errorf("映射结果 %q 不在中台词表里 —— 走 hub.Push 的渠道会整条拒收", got)
			}
			seen[got]++
		})
	}
	// 反向闸：若映射表退化成"一律 text"，上面每条断言仍会绿（因为期望值也是 text
	// 的话就没测了）—— 这里用不同的期望值数量证明这张表确实在区分。
	if len(seen) < 5 {
		t.Errorf("期望输出只覆盖 %d 种中台类型，映射表可能已被压平（应至少区分 audio/image/file/video/text/card）：%v",
			len(seen), seen)
	}
	// 反向闸 2：全为小写、去空格、大小写不敏感（官方 JSON 里出现过 "Image" 的形态）。
	if InboundHubMsgType(" VOICE ") != model.MsgTypeAudio || InboundHubMsgType("Picture") != model.MsgTypeImage {
		t.Error("官方名须先 TrimSpace+ToLower 再查表")
	}
}

// TestN17_UnknownOfficialTypeLandsOnTextNotRejection 词表外、别名表外的名字（官方还在加新类型，
// 企微那张表本仓也还没取到可引用的 A 档出处）必须落到 text：
// 落到词表外 = 走 Push 的渠道整条拒收，落到"丢弃" = 消息不见，落到 text = 只是类型粗。
func TestN17_UnknownOfficialTypeLandsOnTextNotRejection(t *testing.T) {
	for _, unknown := range []string{"", "   ", "a_brand_new_type", "stream", "notify", "reaction", "声音"} {
		if got := InboundHubMsgType(unknown); got != model.MsgTypeText {
			t.Errorf("未知类型 %q 映射为 %q，want text（未核实的名只能落在最保守的一档）", unknown, got)
		}
	}
	// 反向闸：本用例不是"什么都不测"——词表内的名字必须原样通过，
	// 否则上面那条"兜到 text"会连 image 一起兜掉还看不出来。
	if InboundHubMsgType("image") == model.MsgTypeText {
		t.Error("image 被兜底成了 text：兜底分支吞掉了本该原样通过的类型")
	}
}

// TestN17_AliasTableSelfConsistency 别名表的值域必须落在词表内。
// 手抄一张映射表最容易错的就是把目标名写成员表名（"voice"→"voice"），
// 那种行在逐条用例里也可能因为期望值同源地抄错，这里独立查一遍。
func TestN17_AliasTableSelfConsistency(t *testing.T) {
	if len(inboundHubMsgTypeAliases) < 15 {
		t.Fatalf("别名表只剩 %d 条，覆盖各渠道官方名至少需要 15+ 条", len(inboundHubMsgTypeAliases))
	}
	for official, hubType := range inboundHubMsgTypeAliases {
		if !messageHubMsgTypes[hubType] {
			t.Errorf("别名 %q → %q，%q 不在中台词表里", official, hubType, hubType)
		}
		if official != strings.ToLower(strings.TrimSpace(official)) {
			t.Errorf("别名键 %q 必须是已小写去空格的形态，否则查不到", official)
		}
		if official == hubType {
			t.Errorf("别名 %q → 自身：这条要么删掉（词表已含）要么写错", official)
		}
	}
	// 飞书的占位符表与别名表是两处维护的（一个管正文一个管类型），
	// 官方新增类型时最容易只补一张 —— 逐个占位符键查一遍映射结果。
	for official := range feishuInboundPlaceholders {
		if !messageHubMsgTypes[InboundHubMsgType(official)] {
			t.Errorf("飞书官方类型 %q 的映射结果越出词表", official)
		}
	}
}

// ---------------------------------------------------------------- 媒体读取上限

// TestN17_ReadInboundMediaRejectsOverLimit limit+1 的读法是唯一能区分
// "文件正好等于上限"与"文件超过上限"的办法。
func TestN17_ReadInboundMediaRejectsOverLimit(t *testing.T) {
	const limit = 8
	cases := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"小文件", 3, false},
		// 恰好等于上限必须**收**：否则"上限"变成"上限减一"，客户 64MB 的文件永远存不进。
		{"恰好等于上限", limit, false},
		{"超一字节", limit + 1, true},
		{"远超上限", limit * 50, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := readInboundMedia(bytes.NewReader(bytes.Repeat([]byte("x"), tc.size)), limit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%d 字节超过上限 %d 却成功返回 %d 字节：半截文件会被当完整件转存",
						tc.size, limit, len(data))
				}
				if data != nil {
					t.Errorf("超限须整体拒收，got %d 字节残留", len(data))
				}
				return
			}
			if err != nil {
				t.Fatalf("%d 字节未超限却报错：%v", tc.size, err)
			}
			if len(data) != tc.size {
				t.Errorf("got %d bytes, want %d", len(data), tc.size)
			}
		})
	}

	// 读取中途出错必须原样上抛（不能被"超限"判断吃掉，也不能返回半截数据当成功）。
	wantErr := errors.New("boom")
	if _, err := readInboundMedia(f4tErrReader{wantErr}, limit); !errors.Is(err, wantErr) {
		t.Errorf("底层错误应上抛，got %v", err)
	}

	// 反向闸：真常量必须仍是 64MB（改成 1 字节会让所有渠道"媒体超限"，而用例照样绿）。
	if maxInboundMediaBytes < 1<<20 {
		t.Errorf("maxInboundMediaBytes = %d，入站媒体上限不该小于 1MB", maxInboundMediaBytes)
	}
}

type f4tErrReader struct{ err error }

func (r f4tErrReader) Read([]byte) (int, error) { return 0, r.err }

// TestN17_NoUnguardedMediaReadAtAnyChannel 源码闸：入站媒体一律走 readInboundMedia。
//
// 为什么要有这条：六处读取是同一个 io.LimitReader 惯用法的六次复制，修好五处漏一处
// 在测试上是完全看不出来的（各渠道的媒体用例只验"能存下来"，不验"超限拒收"）。
// 口径只卡"带 maxInboundMediaBytes 的 LimitReader 必须 +1"：
// 别处的 io.LimitReader（读错误响应体等）不受本规则约束。
//
// 这道闸的两个已知覆盖面（写成注释，免得后来人把它当"全仓媒体读取都已收口"）：
//   - 只走 service 包：TG 的下载腿在 internal/channelbot/telegram（自己 `maxBytes+1` +
//     「读到正好 max 不算超限」的判定），由 F-4b 电池的 T15/T16 钉，不在这里；
//   - 认的是**常量名**：QQ 的 `qq_media.go` 用自己的 `qqMaxMediaBytes+1`，形态正确但
//     不经过 readInboundMedia，因此这条闸既不会判它红、也不会把它算进 guardedCalls。
func TestN17_NoUnguardedMediaReadAtAnyChannel(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var offenders []string
	guardedCalls := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		guardedCalls += strings.Count(string(src), "= readInboundMedia(")
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, "io.LimitReader(") || !strings.Contains(line, "maxInboundMediaBytes") {
				continue
			}
			if !strings.Contains(line, "+1") {
				offenders = append(offenders, fmt.Sprintf("%s:%d", name, i+1))
			}
		}
	}
	if guardedCalls < 6 {
		t.Errorf("readInboundMedia 只有 %d 处调用点（各渠道入站媒体至少 6 处）：闸变成空转", guardedCalls)
	}
	if len(offenders) > 0 {
		t.Errorf("以下入站媒体读取没有超限拒收（LimitReader 未读 limit+1）：%v", offenders)
	}
}

// TestN17_NoOfficialTypeNameHardcodedAsHubType 源码闸：任何非测试代码里
// `MsgType: "字面量"` 都必须是中台词表内的名字。
//
// 为什么只卡字面量：入站类型的来源有 4 类（渠道官方名 / 适配器归一后的中台名 /
// model.MsgType* 常量 / InboundHubMsgType 映射），前两类的正确性已经在上面的各渠道
// 端到端用例里按**落库那一行**验过；这一条只补一个将来会发生的形态 ——
// 新渠道接入时照着官方文档把 "voice"/"picture" 这样的原值硬编码进 hub 字面量。
// 表达式形态（X.MsgType）不卡：那需要一张按文件维护的白名单，而白名单正是门的盲区
// （审计「门禁口径盲区」条）。
func TestN17_NoOfficialTypeNameHardcodedAsHubType(t *testing.T) {
	var offenders []string
	sites := 0
	err := filepath.Walk(".", func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		name := info.Name()
		if info.IsDir() {
			if name == "humanize" || name == "translation" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// 只卡"这个文件会直接构造 hub 行"的那些：MessageEvent.MsgType 本就该是官方名
		// （归一发生在落库点），不按文件收窄会把这层区分抹掉、变成常驻噪声。
		if !strings.Contains(string(src), "model.MessageHub{") {
			return nil
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "MsgType:") {
				continue
			}
			lit := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "MsgType:")), ",")
			if !strings.HasPrefix(lit, `"`) || !strings.HasSuffix(lit, `"`) {
				continue // 非常量表达式：由用例按落库结果验
			}
			value := strings.Trim(lit, `"`)
			sites++
			if !messageHubMsgTypes[value] {
				offenders = append(offenders, fmt.Sprintf("%s:%d → %q", path, i+1, value))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk service package: %v", err)
	}
	// 反向闸：没扫到任何字面量说明闸空转（例如目录名变了、或 MsgType 都改成了常量）。
	if sites < 3 {
		t.Errorf("只扫到 %d 处 MsgType 字面量，本用例可能已经空转", sites)
	}
	if len(offenders) > 0 {
		t.Errorf("以下 hub 类型字面量越出中台词表（官方名当类型写进去，工作台按类型筛选会永久筛不到）：%v", offenders)
	}
}

// ---------------------------------------------------------------- 飞书入站

// f4tFeishuBody 按官方 im.message.receive_v1 外壳造报文：content 是**字符串化的 JSON**。
// 用 json.Marshal 逐层拼，不手抄转义 —— 富文本正文里有中文与引号，手抄必然错在夹具而不是被测码。
func f4tFeishuBody(t *testing.T, msgID, msgType string, content any) []byte {
	t.Helper()
	inner, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal feishu content: %v", err)
	}
	const ts = "1693637833687199921"
	payload := map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id": "ev-" + msgID, "event_type": "im.message.receive_v1",
			"create_time": ts, "token": "v", "app_id": "a", "tenant_key": "tk",
		},
		"event": map[string]any{
			"sender":   map[string]any{"sender_id": map[string]any{"open_id": "ou_f4"}, "sender_type": "user"},
			"receiver": map[string]any{"chat_id": "oc_f4", "open_id": "ou_bot_f4", "user_id": "u_bot_f4"},
			"message": map[string]any{
				"message_id": msgID, "chat_id": "oc_f4", "chat_type": "p2p",
				"message_type": msgType, "create_time": ts, "content": string(inner),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal feishu event: %v", err)
	}
	return body
}

// TestN17_FeishuPostBodyIsExtracted 富文本正文必须拼回可读文本（修复前是字面量 "[post]"）。
// 官方结构：{"title":"...","content":[[{"tag":"text","text":"..."},{"tag":"img","image_key":"..."}]]}
// 且**不带**发送侧的 zh_cn/en_us 语言壳。
func TestN17_FeishuPostBodyIsExtracted(t *testing.T) {
	content := map[string]any{
		"title": "报价说明",
		"content": []any{
			// 同一行内的多个节点直接相连（官方：一行 = 内层数组，节点 = 数组元素）
			[]any{map[string]any{"tag": "text", "text": "第一行：总价 "},
				map[string]any{"tag": "a", "text": "12800 元", "href": "https://example.com"}},
			[]any{map[string]any{"tag": "img", "image_key": "img_v3_f4"}},
			[]any{map[string]any{"tag": "text", "text": "   "}},
			[]any{map[string]any{"tag": "hr"}},
			[]any{map[string]any{"tag": "text", "text": "第二行"}},
		},
	}
	raw, _ := json.Marshal(content)
	got := feishuInboundText("post", string(raw))
	// 行内相连、行间换行：img/hr/空白行没有 text，不该换来一个空行（否则 AI 历史里全是空行）。
	want := "报价说明\n第一行：总价 12800 元\n第二行"
	if got != want {
		t.Errorf("富文本拼接 = %q, want %q", got, want)
	}
	// 反向闸：只回标题（正文行没拼）或只回正文（标题丢了）都必须是红。
	if strings.TrimSpace(got) == "" || !strings.Contains(got, "12800") || !strings.Contains(got, "报价说明") {
		t.Errorf("标题或正文行丢了：%q", got)
	}
	// 畸形 JSON 不得 panic，也不得把类型名当正文返回。
	if bad := feishuPostText(`{"content":"not-an-array"}`); bad != "" {
		t.Errorf("畸形富文本应返回空串由占位符兜住，got %q", bad)
	}
	// text 型仍走顶层 text 键（改动不能把最简单的那条弄坏）。
	if tx := feishuInboundText("text", `{"text":"在吗"}`); tx != "在吗" {
		t.Errorf("text 型正文 = %q, want 在吗", tx)
	}
}

// TestN17_FeishuResourceKeysAndResTypes 各类型的资源键与官方 resources 接口的 type 参数。
// 官方 GET /messages/{mid}/resources/{file_key} 必带 ?type=image|file，用错直接 2340069；
// file_key 与 image_key 也各在不同类型里（修复前 sticker/folder 连键都没取到）。
func TestN17_FeishuResourceKeysAndResTypes(t *testing.T) {
	cases := []struct {
		msgType, content           string
		wantKey, wantRes, wantName string
		why                        string
	}{
		{"image", `{"image_key":"img_v3_a"}`, "img_v3_a", "image", "", "图片键在 image_key"},
		{"file", `{"file_key":"file_v3_b","file_name":"合同.pdf"}`, "file_v3_b", "file", "合同.pdf", "文件名是官方字段，丢了这个键就没扩展名线索"},
		{"folder", `{"file_key":"file_v3_c"}`, "file_v3_c", "file", "", "文件夹也带 file_key（修复前完全没取）"},
		{"audio", `{"file_key":"audio_v3_d","duration":3000}`, "audio_v3_d", "file", "", "语音的键是 file_key 不是 audio_key"},
		{"media", `{"file_key":"file_v3_e","image_key":"img_cover_f","duration":5000}`, "file_v3_e", "file", "", "视频正文在 file_key；image_key 是封面，不单独转存"},
		{"sticker", `{"file_key":"sticker_v3_g"}`, "sticker_v3_g", "file", "", "表情包也带 file_key（修复前没取）"},
		{"post", `{"content":[[{"tag":"text","text":"看这个"},{"tag":"img","image_key":"img_v3_h"}]]}`, "img_v3_h", "image", "", "图文混排里嵌的图"},
		{"post", `{"content":[[{"tag":"media","file_key":"file_v3_i"}]]}`, "file_v3_i", "file", "", "图文混排里嵌的视频"},
		{"text", `{"text":"在吗"}`, "", "", "", "纯文本没有资源"},
		{"image", `{"no_such_key":"x"}`, "", "", "", "畸形 JSON 不猜键"},
	}
	for _, tc := range cases {
		t.Run(tc.msgType+"::"+tc.why, func(t *testing.T) {
			key, resType, name := feishuInboundMedia(tc.msgType, tc.content)
			if key != tc.wantKey || resType != tc.wantRes || name != tc.wantName {
				t.Errorf("feishuInboundMedia(%q,%q) = (%q,%q,%q), want (%q,%q,%q)",
					tc.msgType, tc.content, key, resType, name, tc.wantKey, tc.wantRes, tc.wantName)
			}
			if tc.wantKey != "" && resType != "image" && resType != "file" {
				t.Errorf("type 参数只能是官方两种，got %q", resType)
			}
		})
	}
	// 反向闸：官方 21 个类型的占位符必须都在（漏一个就把该类型的正文变成一个英文类型名）。
	placeholders := make([]string, 0, len(feishuInboundPlaceholders))
	for k := range feishuInboundPlaceholders {
		placeholders = append(placeholders, k)
	}
	sort.Strings(placeholders)
	if len(placeholders) < 20 {
		t.Errorf("飞书官方类型占位符只覆盖 %d 种：%v", len(placeholders), placeholders)
	}
	for _, official := range placeholders {
		if ph := feishuInboundPlaceholder(official); ph == "["+official+"]" || ph == "" {
			t.Errorf("类型 %q 的正文占位符没配，客户/工作台看到的是 %q", official, ph)
		}
	}
	// 未知类型保留官方名：新增类型要"看得见是哪种"，不是静默变成一个中文词。
	if got := feishuInboundPlaceholder("brand_new_type"); got != "[brand_new_type]" {
		t.Errorf("未知类型占位符 = %q, want [brand_new_type]", got)
	}
}

// TestN17_FeishuVoiceAndMediaLandInHubWithRightType 端到端（到 dispatch 为止）：
// 飞书直接 repo.Create 绕过 Normalize ⇒ 越词表的类型不是被拒，是落成筛不到的行。
// 这里按官方 message_type 派三条，断言 hub 行类型全在词表内且能按类型筛出来。
func TestN17_FeishuVoiceAndMediaLandInHubWithRightType(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.FeishuAccount{
		ID: 90417101, AccountName: "FS-n17", AppID: "a", AppSecret: "b",
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed feishu account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	accountID := fmt.Sprintf("%d", acc.ID)

	cases := []struct {
		msgID, msgType string
		content        any
		wantType       string
		wantContent    string
	}{
		{"om_n17_media", "media", map[string]any{"file_key": "file_v3_n17_media", "file_name": "产品介绍.mp4"}, model.MsgTypeVideo, "[视频]"},
		{"om_n17_audio", "audio", map[string]any{"file_key": "file_v3_n17_audio", "duration": 4000}, model.MsgTypeAudio, "[语音]"},
		{"om_n17_sticker", "sticker", map[string]any{"file_key": "sticker_v3_n17"}, model.MsgTypeImage, "[表情]"},
		{"om_n17_folder", "folder", map[string]any{"file_key": "file_v3_n17_folder"}, model.MsgTypeFile, "[文件夹]"},
		{"om_n17_post", "post", map[string]any{"title": "图文标题", "content": []any{
			[]any{map[string]any{"tag": "text", "text": "这是富文本正文"}}}}, model.MsgTypeText, "图文标题\n这是富文本正文"},
		{"om_n17_new", "stream", map[string]any{"x": 1}, model.MsgTypeText, "[stream]"},
	}
	for _, tc := range cases {
		hub, err := svc.dispatchFeishu(context.Background(), accountID, &ParsedPayload{},
			f4tFeishuBody(t, tc.msgID, tc.msgType, tc.content))
		if err != nil {
			t.Fatalf("dispatch %s: %v", tc.msgType, err)
		}
		if hub == nil {
			t.Fatalf("dispatch %s 没有落 hub 行", tc.msgType)
		}
		if hub.MsgType != tc.wantType {
			t.Errorf("%s 的 hub 类型 = %q, want %q", tc.msgType, hub.MsgType, tc.wantType)
		}
		if !messageHubMsgTypes[hub.MsgType] {
			t.Errorf("%s 落到了词表外类型 %q：工作台按类型筛不到、by_msg_type 统计也没有", tc.msgType, hub.MsgType)
		}
		if hub.Content != tc.wantContent {
			t.Errorf("%s 的正文 = %q, want %q", tc.msgType, hub.Content, tc.wantContent)
		}
	}

	// 反向闸：按类型筛选必须真能筛到（repository 的 msg_type = ? 是工作台唯一入口）。
	var cnt int64
	if err := db.Model(&model.MessageHub{}).Where("platform = ? AND msg_type = ?", "feishu", model.MsgTypeVideo).
		Count(&cnt).Error; err != nil {
		t.Fatalf("count by msg_type: %v", err)
	}
	if cnt != 1 {
		t.Errorf("按 video 筛选到 %d 行，want 1（media 型那条）", cnt)
	}
}

// TestN17_FeishuMediaFullChainBackfillsHubRow 取凭证 → 下载 → 转存 → 回填全链。
// 三条外部 IO 腿都用替身，断言的是"传给官方 resources 接口的 type 参数"与
// "转存时的文件名提示"——修复前这两处分别是"从消息类型反推"和"丢弃官方 file_name"。
func TestN17_FeishuMediaFullChainBackfillsHubRow(t *testing.T) {
	db := setupChannelFullDB(t)
	acc := &model.FeishuAccount{
		ID: 90417102, AccountName: "FS-chain", AppID: "a", AppSecret: "b",
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed feishu account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	type fetchCall struct {
		token, messageID, fileKey, resType string
	}
	type storeCall struct {
		mediaID, data, contentType, hint string
	}
	fetched := make(chan fetchCall, 4)
	stored := make(chan storeCall, 4)

	pT, pF, pS := feishuTenantTokenFn, feishuMediaFetchFn, feishuMediaStoreFn
	t.Cleanup(func() { feishuTenantTokenFn, feishuMediaFetchFn, feishuMediaStoreFn = pT, pF, pS })
	feishuTenantTokenFn = func(_ context.Context, _ *FeishuIntegrationService, a *model.FeishuAccount) (string, error) {
		if a.AppID != "a" {
			t.Errorf("替身收到的账号不对：%+v", a)
		}
		return "t-f4-token", nil
	}
	feishuMediaFetchFn = func(_ context.Context, token, messageID, fileKey, resType string) (io.ReadCloser, string, error) {
		fetched <- fetchCall{token, messageID, fileKey, resType}
		return io.NopCloser(strings.NewReader("f4-bytes-of-" + fileKey)), "video/mp4", nil
	}
	feishuMediaStoreFn = func(_ context.Context, channel, mediaID string, data []byte, contentType, hint string) (string, error) {
		stored <- storeCall{mediaID: mediaID, data: string(data), contentType: contentType, hint: hint}
		if !strings.Contains(string(data), mediaID) {
			return "", fmt.Errorf("stub: bytes do not belong to %s", mediaID)
		}
		return "/files/" + channel + "/" + mediaID, nil
	}

	const msgID = "om_n17_chain_media"
	if _, err := svc.dispatchFeishu(context.Background(), fmt.Sprintf("%d", acc.ID), &ParsedPayload{},
		f4tFeishuBody(t, msgID, "media", map[string]any{
			"file_key": "file_v3_chain", "image_key": "img_cover_chain", "file_name": "产品介绍.mp4",
		})); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	select {
	case f := <-fetched:
		if f.token != "t-f4-token" || f.messageID != msgID || f.fileKey != "file_v3_chain" {
			t.Errorf("下载腿入参 = %+v", f)
		}
		// 官方：视频/文件类资源的 type 参数是 file，只有 image_key 才是 image。
		// 反推的话 media 会被推成 video —— 官方没有这个取值，直接 2340069。
		if f.resType != "file" {
			t.Errorf("resources 的 type = %q, want file（官方取值只有 image|file，media 推成 video 会报 2340069）", f.resType)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("15s 内没发起下载：媒体转存没被排队")
	}
	select {
	case s := <-stored:
		if s.hint != "产品介绍.mp4" {
			t.Errorf("转存文件名提示 = %q, want 产品介绍.mp4（官方 file_name 丢了就存成无扩展名对象）", s.hint)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("15s 内没完成转存")
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		var hub model.MessageHub
		err := db.Where("platform = ? AND msg_id = ?", "feishu", msgID).First(&hub).Error
		if err == nil && hub.MediaURL == "/files/feishu/file_v3_chain" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub 行未回填长期 URL：%v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------------------------------------------------------------- 企微入站

// TestN17_WeComVoiceReachesHubInsteadOfBeingRejected 企微是唯一经 hub.Push → Normalize
// 的渠道 ⇒ 官方 voice 越词表时整条消息被拒（修复前实测：dispatch 上抛，一行都不落）。
// 走的是 N-08 那套密文外壳（XML envelope → 验签 → 解密 → 明文 XML）。
func TestN17_WeComVoiceReachesHubInsteadOfBeingRejected(t *testing.T) {
	db := setupChannelFullDB(t)
	aesKey := newWecomEncodingAESKey(t)
	acc := &model.WeComAccount{
		ID: 90417201, CorpID: "ww_n17_corp", CorpSecret: "s", AgentID: 1000017,
		CallbackToken: "n17_token", EncodingAESKey: aesKey,
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed wecom account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	accountID := fmt.Sprintf("%d", acc.ID)

	// 三条：官方 voice（越词表 + 带 MediaId）、shortvideo（小视频）、text（不得被换掉）。
	// 字段名按官方接收消息文档的 tag：voice 带 MediaId/Format/Recognition，
	// shortvideo 带 MediaId/ThumbMediaId，且正文里没有 Content。
	plainVoice := `<xml><ToUserName><![CDATA[ww_n17_corp]]></ToUserName>` +
		`<FromUserName><![CDATA[ext_n17_user]]></FromUserName><CreateTime>1700000017</CreateTime>` +
		`<MsgType><![CDATA[voice]]></MsgType><MediaId><![CDATA[n17_media_voice]]></MediaId>` +
		`<Format>amr</Format><Recognition>帮我发个报价</Recognition>` +
		`<MsgId>n17_voice_1</MsgId><AgentID>1000017</AgentID></xml>`
	plainShort := `<xml><ToUserName><![CDATA[ww_n17_corp]]></ToUserName>` +
		`<FromUserName><![CDATA[ext_n17_user]]></FromUserName><CreateTime>1700000019</CreateTime>` +
		`<MsgType><![CDATA[shortvideo]]></MsgType><MediaId><![CDATA[n17_media_short]]></MediaId>` +
		`<ThumbMediaId><![CDATA[n17_media_thumb]]></ThumbMediaId>` +
		`<MsgId>n17_short_1</MsgId><AgentID>1000017</AgentID></xml>`
	plainText := `<xml><ToUserName><![CDATA[ww_n17_corp]]></ToUserName>` +
		`<FromUserName><![CDATA[ext_n17_user]]></FromUserName><CreateTime>1700000018</CreateTime>` +
		`<MsgType><![CDATA[text]]></MsgType><Content><![CDATA[企微文本不得回归]]></Content>` +
		`<MsgId>n17_txt_1</MsgId><AgentID>1000017</AgentID></xml>`

	// 企微媒体的三条 IO 腿用替身：测试环境取不到 access_token，也不该去真取。
	fetched := make(chan string, 4)
	pT, pF, pS := wecomTokenFn, wecomMediaFetchFn, wecomMediaStoreFn
	t.Cleanup(func() { wecomTokenFn, wecomMediaFetchFn, wecomMediaStoreFn = pT, pF, pS })
	wecomTokenFn = func(context.Context, *WebhookService, uint) (string, error) { return "n17-token", nil }
	wecomMediaFetchFn = func(_ context.Context, token, mediaID string) (io.ReadCloser, string, error) {
		fetched <- token + "|" + mediaID
		return io.NopCloser(strings.NewReader("n17-bytes")), "application/octet-stream", nil
	}
	wecomMediaStoreFn = func(_ context.Context, channel, mediaID string, data []byte, contentType, hint string) (string, error) {
		if contentType == "application/octet-stream" || contentType == "" {
			return "", fmt.Errorf("stub: 类型推断没生效，got %q", contentType)
		}
		return "/files/" + channel + "/" + mediaID, nil
	}

	cases := []struct {
		plain                  string
		wantType, wantBodyName string
	}{
		{plainVoice, model.MsgTypeAudio, "[语音]"},
		{plainShort, model.MsgTypeVideo, "[小视频]"},
		{plainText, model.MsgTypeText, "企微文本不得回归"},
	}
	for _, tc := range cases {
		enc := wecomOfficialEncrypt(t, aesKey, tc.plain, acc.CorpID)
		raw := n08EnvelopeXML(enc)
		p, err := svc.ParsePayload(context.Background(), ChannelWeCom, raw)
		if err != nil {
			t.Fatalf("ParsePayload: %v", err)
		}
		hub, err := svc.dispatchWeCom(context.Background(), accountID, p, raw, nil)
		if err != nil {
			t.Fatalf("dispatchWeCom（类型 %s）: %v —— 整条消息被拒就是这里的后果", tc.wantType, err)
		}
		if hub == nil {
			t.Fatalf("类型 %s 没落 hub 行", tc.wantType)
		}
		if hub.MsgType != tc.wantType {
			t.Errorf("hub 类型 = %q, want %q", hub.MsgType, tc.wantType)
		}
		if !messageHubMsgTypes[hub.MsgType] {
			t.Errorf("hub 类型 %q 越词表", hub.MsgType)
		}
		if hub.Content != tc.wantBodyName {
			t.Errorf("hub 正文 = %q, want %q", hub.Content, tc.wantBodyName)
		}
	}

	// 语音/小视频两条媒体的转存腿必须真跑起来（MediaId 只有 3 天有效）。
	seen := map[string]bool{}
	timeout := time.After(15 * time.Second)
	for len(seen) < 2 {
		select {
		case f := <-fetched:
			seen[f] = true
		case <-timeout:
			t.Fatalf("企微媒体只发起 %d 次下载（want 2）：%v", len(seen), seen)
		}
	}
	for _, want := range []string{"n17-token|n17_media_voice", "n17-token|n17_media_short"} {
		if !seen[want] {
			t.Errorf("缺少一次针对 %s 的下载", want)
		}
	}
}

// ---------------------------------------------------------------- WhatsApp / 公众号

// TestN17_WhatsAppTypesMapIntoVocabulary WA 直接 repo.Create，越词表的类型不是被拒，
// 是在工作台"按类型筛选"里凭空消失（document 落了一行，但筛 file 筛不到）。
func TestN17_WhatsAppTypesMapIntoVocabulary(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	const waAccountID = "90119"
	if err := db.Create(&model.WhatsAppCloudAccount{
		ID: 90119, AccountName: "f4t-wa", PhoneNumberID: "pn-f4t",
		WhatsAppBusinessID: "wb-f4t", AccessToken: "tok-f4t", Status: 1,
	}).Error; err != nil {
		t.Fatalf("seed wa account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	f4tInstallWAStubs(t)

	const waUser = "+8613900000017"
	msgs := []string{
		`{"from":"` + waUser + `","id":"wamid-f4t-doc","timestamp":"1700000021","type":"document","document":{"id":"wa-media-f4t-doc","mime_type":"application/pdf","filename":"报价.pdf"}}`,
		`{"from":"` + waUser + `","id":"wamid-f4t-stk","timestamp":"1700000022","type":"sticker","sticker":{"id":"wa-media-f4t-stk","mime_type":"image/webp"}}`,
		`{"from":"` + waUser + `","id":"wamid-f4t-aud","timestamp":"1700000023","type":"audio","audio":{"id":"wa-media-f4t-aud","mime_type":"audio/ogg"}}`,
		`{"from":"` + waUser + `","id":"wamid-f4t-txt","timestamp":"1700000024","type":"text","text":{"body":"在吗"}}`,
	}
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[` +
		strings.Join(msgs, ",") + `],"contacts":[{"profile":{"name":"Bob"},"wa_id":"` + waUser + `"}]},"field":"messages"}]}]}`)

	hub, err := svc.dispatchWhatsApp(context.Background(), waAccountID, &ParsedPayload{EventID: "evt-f4t-wa"}, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("WA 推送没有落 hub 行")
	}
	want := map[string]struct{ msgType, body string }{
		"wamid-f4t-doc": {model.MsgTypeFile, "[文件]"},
		"wamid-f4t-stk": {model.MsgTypeImage, "[表情]"},
		"wamid-f4t-aud": {model.MsgTypeAudio, "[语音]"},
		"wamid-f4t-txt": {model.MsgTypeText, "在吗"},
	}
	for msgID, wantRow := range want {
		var row model.MessageHub
		if err := db.Where("platform = ? AND msg_id = ?", "whatsapp", msgID).First(&row).Error; err != nil {
			t.Errorf("%s 没有 hub 行：%v", msgID, err)
			continue
		}
		if row.MsgType != wantRow.msgType {
			t.Errorf("%s 类型 = %q, want %q", msgID, row.MsgType, wantRow.msgType)
		}
		if !messageHubMsgTypes[row.MsgType] {
			t.Errorf("%s 类型 %q 越词表：工作台筛 file/image 都筛不到它", msgID, row.MsgType)
		}
		// 正文由**适配器**写（Ingress 才是落库的那条路），服务侧的 waMessageContent 只影响
		// 内存视图 —— 所以这条断言测的是适配器那张表，改错地方会在这里露出来。
		if row.Content != wantRow.body {
			t.Errorf("%s 正文 = %q, want %q", msgID, row.Content, wantRow.body)
		}
	}
	// 反向闸：一次推送的 4 条都要落库（M-02 的口径），少一条就是 dispatch 只处理首条。
	var cnt int64
	if err := db.Model(&model.MessageHub{}).Where("platform = ? AND account_id = ?", "whatsapp", waAccountID).
		Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 4 {
		t.Errorf("落了 %d 行，want 4", cnt)
	}
}

// f4tInstallWAStubs 把 WA 的下载/转存两条腿换掉（媒体消息会在 dispatch 里排队转存）。
func f4tInstallWAStubs(t *testing.T) {
	t.Helper()
	fetchPrev, storePrev := waMediaFetchFn, waMediaStoreFn
	t.Cleanup(func() { waMediaFetchFn, waMediaStoreFn = fetchPrev, storePrev })
	waMediaFetchFn = func(_ context.Context, accessToken, phoneID, mediaID string) (io.ReadCloser, string, error) {
		if accessToken == "" || phoneID == "" {
			return nil, "", fmt.Errorf("stub: credentials empty")
		}
		return io.NopCloser(strings.NewReader("bytes-of-" + mediaID)), "application/octet-stream", nil
	}
	waMediaStoreFn = func(_ context.Context, channel, mediaID string, data []byte, contentType, hint string) (string, error) {
		return "/files/" + channel + "/" + mediaID, nil
	}
}

// TestN17_WeChatShortvideoIsVideoTypeWithOwnBody 公众号 shortvideo（小视频）没有对应中台类型：
// 类型归 video（否则 hub 落一个词表外的 shortvideo，筛选与统计都找不到），区分靠正文占位符。
func TestN17_WeChatShortvideoIsVideoTypeWithOwnBody(t *testing.T) {
	cases := []struct {
		msgType            string
		wantType, wantBody string
	}{
		{"shortvideo", model.MsgTypeVideo, "[小视频]"},
		{"video", model.MsgTypeVideo, "[视频]"},
		{"voice", model.MsgTypeAudio, "[语音]"},
		{"image", model.MsgTypeImage, "[图片]"},
	}
	for _, tc := range cases {
		t.Run(tc.msgType, func(t *testing.T) {
			m := &WechatIncomingMessage{MsgType: tc.msgType, MediaID: "wx_media_f4t", ToUserName: "gh_x", FromUserName: "o_x"}
			hubType, body, extra, ok := m.NormalizeInbound()
			if !ok {
				t.Fatalf("%s 被判为不进收件箱", tc.msgType)
			}
			if hubType != tc.wantType {
				t.Errorf("类型 = %q, want %q", hubType, tc.wantType)
			}
			if body != tc.wantBody {
				t.Errorf("正文 = %q, want %q", body, tc.wantBody)
			}
			if !messageHubMsgTypes[hubType] {
				t.Errorf("类型 %q 越词表", hubType)
			}
			if got, _ := extra["media_id"].(string); got != "wx_media_f4t" {
				t.Errorf("media_id 没留痕（3 天后取不到原件，但至少要能看出曾经有过）：%v", extra)
			}
		})
	}
}

// TestN17_DingTalkInboundBodyTypesInHubVocabulary 钉住钉钉归一层的输出不变量：
// 第一个返回值恒在中台 msg_type 词表内。
//
// 为什么单独钉这一条而不是靠"读代码看它写对了"：钉钉的官方类型名（richText / 未来的新类型）
// 是直接进 msg_type 的，而钉钉落库走的是不带 Normalize 校验的入站腿 —— 越词表的值不会报错，
// 只会安静地写成一行人台筛不到、by_msg_type 统计数不到的行（N-17 的第三种现场）。
// 批F-4c 那版本把官方名原样保留（"richText"），正是这条不变量的违例。
func TestN17_DingTalkInboundBodyTypesInHubVocabulary(t *testing.T) {
	cases := []struct {
		msgtype  string
		wantType string
	}{
		{"", model.MsgTypeText},
		{"text", model.MsgTypeText},
		{"picture", model.MsgTypeImage},
		{"audio", model.MsgTypeAudio},
		{"video", model.MsgTypeVideo},
		{"file", model.MsgTypeFile},
		{"richText", model.MsgTypeText},
		// 官方词表之外的（新增类型、或端上别的形态）一律兜到 text，不得原样入库：
		{"actionCard", model.MsgTypeText},
		{"stream", model.MsgTypeText},
		{"WHATEVER_NEW", model.MsgTypeText},
	}
	for _, tc := range cases {
		content := &dingTalkInboundContent{Content: "正文", FileName: "报价单.pdf", Recognition: "识别文本"}
		if tc.msgtype == "richText" {
			content.RichText = []struct {
				Text         string `json:"text"`
				DownloadCode string `json:"downloadCode"`
				Type         string `json:"type"`
			}{{Text: "这款有货"}, {DownloadCode: "dc-rt", Type: "picture"}}
		}
		gotType, body, _ := dingTalkInboundBody(tc.msgtype, content, "文本正文")
		if gotType != tc.wantType {
			t.Errorf("dingTalkInboundBody(%q) 类型 = %q, want %q", tc.msgtype, gotType, tc.wantType)
		}
		if !messageHubMsgTypes[gotType] {
			t.Errorf("dingTalkInboundBody(%q) 类型 %q 越出中台词表 ⇒ 这一行工作台筛不到", tc.msgtype, gotType)
		}
		if body == "" {
			t.Errorf("dingTalkInboundBody(%q) 正文为空 ⇒ AI 与客户都看不见这条消息", tc.msgtype)
		}
	}
}
