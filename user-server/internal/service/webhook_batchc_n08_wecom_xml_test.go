package service

// 审计 N-08（本会话读码发现，§5 追加）：企微入站整条链路只认 JSON 外壳。
//
// 三处独立环节各自要求 JSON：
//   1. `verifyWeCom` 用 `json.Unmarshal(body)` 取 `encrypt`；取不到就用
//      `query["echostr"]` 当第四段 —— POST 消息回调没有 echostr，于是 sha1 的
//      第四段是空串，签名必然对不上 → 第一步就 400。
//   2. `WebhookService.ParsePayload` 对 body 做 `json.Unmarshal`，失败即
//      `Receive` 返回 "parse error" → 400。
//   3. `decryptWeComPayload` 把解密出的明文再 `json.Unmarshal`。
//
// 也就是说：企微「智能机器人」那种 JSON 回调能走通，而带 `<xml><Encrypt/>` 外壳的
// 回调形态一条都进不来。官方原文本次仍未取到可引用出处（developer.work.weixin.qq.com
// 是 SPA，见 §6），所以修复口径刻意**不声明哪一种是官方形态**：两种都收
// （envelope 与解密明文各自 JSON/XML 双解），这样任何一边都不需要猜对。
//
// 本文件先把"XML 形态现在进不来"钉成红测，再实现双形态；同时钉住 JSON 形态
// 不得被换掉，以及 §8.2 一直欠着的企微 hub 层 MsgId 去重用例。
//
// 实跑（先红后绿）时发现的两处连带问题也一并钉在这里：
//   - 第 4 个 JSON-only 环节：`officialEventID` 的企微分支 —— 明文 XML 回调会取不到
//     MsgId，退化成整包哈希兜底。
//   - hub 层按 MsgId 收敛重投时返回的是 `ErrMessageHubIdempotent` **错误**，
//     `dispatchWeCom` 原样上抛会让 handleJob 继续往下走、用取不到正文的外壳
//     落一条空内容 unified_message。修法：幂等就地转成 (nil, nil)，
//     与飞书/抖音/Telegram 的 dispatch 同一口径。

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// n08MsgXML 解密后的明文（消息体形态：官方 tag 名，与 公众号 同族）。
func n08MsgXML(msgID, content string) string {
	return fmt.Sprintf(`<xml><ToUserName><![CDATA[ww_n08_corp]]></ToUserName>`+
		`<FromUserName><![CDATA[ext_n08_user]]></FromUserName>`+
		`<CreateTime>1700000001</CreateTime><MsgType><![CDATA[text]]></MsgType>`+
		`<Content><![CDATA[%s]]></Content><MsgId>%s</MsgId>`+
		`<AgentID>1000002</AgentID></xml>`, content, msgID)
}

// n08EnvelopeXML 外发回调外壳：官方把整条明文塞进 <Encrypt>，AgentID 也在壳里。
func n08EnvelopeXML(encrypt string) []byte {
	return []byte(`<xml><ToUserName><![CDATA[ww_n08_corp]]></ToUserName>` +
		`<Encrypt><![CDATA[` + encrypt + `]]></Encrypt>` +
		`<AgentID><![CDATA[1000002]]></AgentID></xml>`)
}

// n08Seed 建一个开了回调的企微账号（显式大主键：本包多用例共享库，自增 id 会撞）。
func n08Seed(t *testing.T, db *gorm.DB, id uint) (*model.WeComAccount, string) {
	t.Helper()
	aesKey := newWecomEncodingAESKey(t)
	acc := &model.WeComAccount{
		ID: id, CorpID: "ww_n08_corp", CorpSecret: "s",
		AgentID: 1000002, CallbackToken: "n08_token", EncodingAESKey: aesKey,
		WebhookEnabled: true, AIAgentEnabled: false, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed wecom account: %v", err)
	}
	return acc, aesKey
}

// n08Query 按官方 `msg_signature=sha1(字典序 sort(token,timestamp,nonce,encrypt))` 造参。
func n08Query(token, encrypt string) map[string]string {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "n08nonce"
	return map[string]string{
		"timestamp":     ts,
		"nonce":         nonce,
		"msg_signature": wecomCallbackSignature(token, ts, nonce, encrypt),
	}
}

// TestN08_VerifyWeCom_XMLEnvelopeSignature 外壳是 XML 时也必须取得到 encrypt。
// 修复前：验签用的第四段是空串 → 恒不匹配。
func TestN08_VerifyWeCom_XMLEnvelopeSignature(t *testing.T) {
	acc, aesKey := n08SeedNoDB(t)
	enc := wecomOfficialEncrypt(t, aesKey, n08MsgXML("wamid_n08_v", "第一条"), acc.CorpID)
	body := n08EnvelopeXML(enc)
	q := n08Query(acc.CallbackToken, enc)

	ok, err := verifyWeCom(acc.CallbackToken, aesKey, body, q)
	if err != nil {
		t.Fatalf("XML 外壳验签报错：%v", err)
	}
	if !ok {
		t.Error("XML 外壳 + query 签名的企微回调必须验签通过，got false")
	}

	// 反向：改一个字节必须拒（否则本用例是恒真守卫）。
	bad := map[string]string{"timestamp": q["timestamp"], "nonce": q["nonce"], "msg_signature": q["msg_signature"][:38] + "00"}
	if ok2, _ := verifyWeCom(acc.CallbackToken, aesKey, body, bad); ok2 {
		t.Error("被篡改的 msg_signature 不得通过")
	}
	// 反向：encrypt 被换掉而签名没跟着改，必须拒。
	other := wecomOfficialEncrypt(t, aesKey, n08MsgXML("wamid_n08_other", "另一条"), acc.CorpID)
	if ok3, _ := verifyWeCom(acc.CallbackToken, aesKey, n08EnvelopeXML(other), q); ok3 {
		t.Error("密文与签名不匹配不得通过")
	}
}

// n08SeedNoDB 只用到账号的 token/AESKey，不碰库（验签与解密是纯函数）。
func n08SeedNoDB(t *testing.T) (*model.WeComAccount, string) {
	t.Helper()
	aesKey := newWecomEncodingAESKey(t)
	return &model.WeComAccount{
		ID: 90413002, CorpID: "ww_n08_corp", AgentID: 1000002,
		CallbackToken: "n08_token", EncodingAESKey: aesKey, WebhookEnabled: true,
	}, aesKey
}

// TestN08_ParsePayload_XMLEnvelopeParses Receive 在验签后立刻 ParsePayload，
// 那里对 body 做 json.Unmarshal —— XML 外壳会在与验签无关的第二道关卡上再死一次。
func TestN08_ParsePayload_XMLEnvelopeParses(t *testing.T) {
	_, aesKey := n08SeedNoDB(t)
	enc := wecomOfficialEncrypt(t, aesKey, n08MsgXML("wamid_n08_p", "正文"), "ww_n08_corp")
	svc := &WebhookService{}
	p, err := svc.ParsePayload(context.Background(), ChannelWeCom, n08EnvelopeXML(enc))
	if err != nil {
		t.Fatalf("XML 回调不得在 ParsePayload 处报错（此处报错=整条请求被拒）：%v", err)
	}
	if p == nil || p.Extra == nil {
		t.Fatalf("必须解析出载荷，got %+v", p)
	}
	// 密文形态下正文当然取不到，但外壳字段必须在。
	if got, _ := p.Extra["Encrypt"].(string); got == "" {
		if got2, _ := p.Extra["encrypt"].(string); got2 == "" {
			t.Errorf("Extra 里要保留外壳的 Encrypt 字段，got %+v", p.Extra)
		}
	}
}

// TestN08_DispatchWeCom_XMLFullPathLandsInHub 端到端（到 dispatch 为止）：
// 密文外壳 → 验签取得到 encrypt → 解密 → 明文 XML → 落 message_hub。
// 这一条同时补掉 §8.2 欠着的「企微官方 MsgId 在 hub 层收敛」用例。
func TestN08_DispatchWeCom_XMLFullPathLandsInHub(t *testing.T) {
	db := setupChannelFullDB(t)
	acc, aesKey := n08Seed(t, db, 90413001)
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	accountID := fmt.Sprintf("%d", acc.ID)

	// 同一条消息（同 MsgId）两次投递：官方会重投，且第二次的外壳密文不同
	// （random(16) 每次不同），整包哈希兜底挡不住，只能靠 MsgId 收敛。
	enc1 := wecomOfficialEncrypt(t, aesKey, n08MsgXML("wecdn08_dup_1", "重复投递"), acc.CorpID)
	enc2 := wecomOfficialEncrypt(t, aesKey, n08MsgXML("wecdn08_dup_1", "重复投递"), acc.CorpID)
	if enc1 == enc2 {
		t.Fatal("夹具失效：同一明文的两次加密密文相同，这条用例就成了假绿")
	}

	for i, enc := range []string{enc1, enc2} {
		p, err := svc.ParsePayload(context.Background(), ChannelWeCom, n08EnvelopeXML(enc))
		if err != nil {
			t.Fatalf("第 %d 次 ParsePayload: %v", i+1, err)
		}
		hub, err := svc.dispatchWeCom(context.Background(), accountID, p, n08EnvelopeXML(enc), nil)
		if i == 0 {
			// 首次投递必须产出 hub 行。
			if err != nil {
				t.Fatalf("首次 dispatchWeCom: %v", err)
			}
			if hub == nil {
				t.Fatal("首次投递没有产出 hub 行（消息被当成非消息事件丢了）")
			}
			continue
		}
		// 重投：hub 层按官方 MsgId 收敛成 (nil, nil)。返回 error 不行 ——
		// handleJob 见 err!=nil 会继续往下走，用**外壳**落一条空内容的
		// unified_message（密文里取不到 Content），就是 D-03 那个缺陷的形状。
		if err != nil {
			t.Fatalf("重投不得把 hub 层幂等上抛为错误（会漏出空内容 unified_message）：%v", err)
		}
		if hub != nil {
			t.Fatalf("重投不该再产出第二行，got %+v", hub)
		}
	}

	var rows []model.MessageHub
	if err := db.Where("platform = ? AND msg_id = ?", "wecom", "wecdn08_dup_1").Find(&rows).Error; err != nil {
		t.Fatalf("查询 hub: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("同一官方 MsgId 的两次投递必须在 hub 层收敛成 1 行，实际 %d 行", len(rows))
	}
	if rows[0].SenderID != "ext_n08_user" {
		t.Errorf("hub.SenderID 要取明文里的 FromUserName，got %q", rows[0].SenderID)
	}
	if rows[0].Content != "重复投递" {
		t.Errorf("hub.Content 要取明文里的 Content，got %q", rows[0].Content)
	}
}

// TestN08_DispatchWeCom_JSONEnvelopeStillWorks 反向守卫：修 XML 不得把
// 现在能跑的 JSON 形态换掉（那才是本仓库改造前唯一被测过的形状）。
func TestN08_DispatchWeCom_JSONEnvelopeStillWorks(t *testing.T) {
	db := setupChannelFullDB(t)
	acc, aesKey := n08Seed(t, db, 90413003)
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	plain := `{"ToUserName":"ww_n08_corp","FromUserName":"ext_n08_json","CreateTime":1700000002,` +
		`"MsgType":"text","Content":"JSON 形态","MsgId":"wecdn08_json_1","AgentID":1000002}`
	enc := wecomOfficialEncrypt(t, aesKey, plain, acc.CorpID)
	body := []byte(`{"encrypt":"` + enc + `"}`)

	q := n08Query(acc.CallbackToken, enc)
	ok, err := verifyWeCom(acc.CallbackToken, aesKey, body, q)
	if err != nil || !ok {
		t.Fatalf("JSON 外壳验签必须仍然通过，got ok=%v err=%v", ok, err)
	}

	p, err := svc.ParsePayload(context.Background(), ChannelWeCom, body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	hub, err := svc.dispatchWeCom(context.Background(), fmt.Sprintf("%d", acc.ID), p, body, nil)
	if err != nil {
		t.Fatalf("dispatchWeCom: %v", err)
	}
	if hub == nil || hub.MsgID != "wecdn08_json_1" {
		t.Fatalf("JSON 形态要照常落库并带上官方 MsgId，got %+v", hub)
	}
}

// TestN08_OfficialEventID_PlainXMLKeepsOfficialMsgID 明文（未加密）XML 回调：
// Receive 的第一道去重键必须仍是官方 MsgId，不能因为外壳是 XML 就退化成整包哈希
// （整包哈希对「同一消息、重投时字节不同」无效，那是 S-04 已定性的口径）。
func TestN08_OfficialEventID_PlainXMLKeepsOfficialMsgID(t *testing.T) {
	body := []byte(n08MsgXML("wecdn08_evt_1", "明文"))
	if got, want := officialEventID(ChannelWeCom, "1", body), "wecom:1:wecdn08_evt_1"; got != want {
		t.Fatalf("明文 XML 回调的官方键要是 %q，got %q", want, got)
	}
	// 反向：JSON 形态的官方键不得被换掉。
	jsonBody := []byte(`{"MsgId":"wecdn08_evt_2","MsgType":"text","Content":"x"}`)
	if got, want := officialEventID(ChannelWeCom, "1", jsonBody), "wecom:1:wecdn08_evt_2"; got != want {
		t.Fatalf("JSON 形态官方键回归，want %q got %q", want, got)
	}
	// 反向：XML 兜底只作用于企微，别的渠道不该从企微外壳里取出键。
	if got := officialEventID(ChannelWhatsapp, "1", body); got != "" {
		t.Fatalf("whatsapp 不该认企微的 XML 外壳，got %q", got)
	}
}

// TestN08_XMLDepthIsBounded 外壳解析发生在**验签之前**（要从 body 里取 encrypt 才能算
// 签名第四段），所以输入完全由请求方控制：body 硬上限是 MaxWebhookBody=2 MiB，
// 而 encoding/xml 没有深度限制 —— 全用 `<a>` 嵌套就是几十万层递归，
// 一个未验签的请求就能把进程栈打爆（预认证 DoS）。故 XML→map 的递归必须有显式上限。
func TestN08_XMLDepthIsBounded(t *testing.T) {
	// 深嵌套必须在到达上限时就放弃：30 层远超官方形状（最深处是
	// <xml><Image><MediaId/> 这种两层），解析结果必须是 nil 而不是"硬解出来"。
	deep := []byte("<xml>" + strings.Repeat("<a>", 30) + strings.Repeat("</a>", 30) + "</xml>")
	if got := wecomFlatXMLMap(deep); got != nil {
		t.Fatalf("30 层嵌套的 XML 必须因超出深度上限而解析失败，got %d 个字段", len(got))
	}
	// 反向：官方形状（外壳一层 + 媒体消息的两层子结构）不能被深度上限误伤。
	media := []byte(`<xml><ToUserName><![CDATA[ww_n08_corp]]></ToUserName>` +
		`<MsgType><![CDATA[image]]></MsgType><Image><MediaId><![CDATA[media_1]]></MediaId></Image></xml>`)
	got := wecomFlatXMLMap(media)
	if got == nil {
		t.Fatal("两层子结构的官方形状必须解析成功")
	}
	img, ok := got["Image"].(map[string]any)
	if !ok || img["MediaId"] != "media_1" {
		t.Fatalf("嵌套子元素要解成子 map，got %+v", got["Image"])
	}
	// 上限之外还得"扛得住最大合法 body"：2 MiB 全是 <a> 也必须立刻返回，不是慢慢爬栈。
	huge := []byte("<xml>" + strings.Repeat("<a>", int(MaxWebhookBody)/3) + "</xml>")
	start := time.Now()
	if wecomFlatXMLMap(huge) != nil {
		t.Fatal("2 MiB 深嵌套必须解析失败而不是递归到底")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("深度上限生效后应当是常数级耗时，实际 %s", elapsed)
	}
}

// TestN08_DispatchWeCom_NestedXMLFieldsReachHubContent 钉住 N-09 的第二半：
// 深度上限修好之后 <Link> 确实解成了子 map，但**取值写在顶层**——嵌套形态的
// Title/Url 解得出来却没人去取，结果和丢掉一样。
// 选 link 做全路径取证的理由：它落进 hub.Content，是这条链路上唯一"嵌套字段
// 会变成可查证据"的消息类型（媒体的 MediaId 只喂异步转存，测试里不可观测）。
func TestN08_DispatchWeCom_NestedXMLFieldsReachHubContent(t *testing.T) {
	db := setupChannelFullDB(t)
	acc, aesKey := n08Seed(t, db, 90413005)
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	plain := `<xml><ToUserName><![CDATA[ww_n08_corp]]></ToUserName>` +
		`<FromUserName><![CDATA[ext_n08_user]]></FromUserName>` +
		`<CreateTime>1700000009</CreateTime><MsgType><![CDATA[link]]></MsgType>` +
		`<Link><Title><![CDATA[n08 商品链接]]></Title>` +
		`<Url><![CDATA[https://example.com/n08]]></Url></Link>` +
		`<MsgId>wecdn09_link_1</MsgId><AgentID>1000002</AgentID></xml>`
	enc := wecomOfficialEncrypt(t, aesKey, plain, acc.CorpID)
	body := n08EnvelopeXML(enc)

	p, err := svc.ParsePayload(context.Background(), ChannelWeCom, body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	hub, err := svc.dispatchWeCom(context.Background(), fmt.Sprintf("%d", acc.ID), p, body, nil)
	if err != nil {
		t.Fatalf("dispatchWeCom: %v", err)
	}
	if hub == nil {
		t.Fatal("link 消息没有产出 hub 行")
	}
	var rows []model.MessageHub
	if err := db.Where("platform = ? AND msg_id = ?", "wecom", "wecdn09_link_1").Find(&rows).Error; err != nil {
		t.Fatalf("查询 hub: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("期望 1 行 hub，实际 %d 行", len(rows))
	}
	if rows[0].Content != "n08 商品链接 https://example.com/n08" {
		t.Fatalf("嵌套在 <Link> 里的 Title/Url 必须被取到，got %q", rows[0].Content)
	}
}

// TestN08_NestedMediaIDIsPickedUp 媒体消息的 MediaId 挂在 <Image>/<Voice>/… 子对象下，
// 两层查找必须拿得到；反向：本仓历史 JSON 形态把它摊平在顶层，也不能被改坏。
// （企微回调页原文未取到，§6 —— 所以两种形态都要读，不声明哪种是官方的。）
func TestN08_NestedMediaIDIsPickedUp(t *testing.T) {
	nested := []byte(`<xml><ToUserName><![CDATA[ww_n08_corp]]></ToUserName>` +
		`<FromUserName><![CDATA[ext_n08_user]]></FromUserName>` +
		`<MsgType><![CDATA[image]]></MsgType>` +
		`<Image><MediaId><![CDATA[media_nested_1]]></MediaId></Image>` +
		`<MsgId>wecdn09_img_2</MsgId></xml>`)
	plain := wecomEnvelopeMap(nested)
	if plain == nil {
		t.Fatal("图片回调明文必须解析成功")
	}
	// 先证明"只查顶层"确实取不到 —— 否则这条用例是在防一个不存在的问题。
	if got := getString(plain, "MediaId", "media_id"); got != "" {
		t.Fatalf("夹具失效：顶层本不该有 MediaId，got %q（那这条用例证不了两层查找）", got)
	}
	if got := wecomSubString(plain, wecomMediaContainers, "MediaId", "media_id"); got != "media_nested_1" {
		t.Fatalf("嵌套子对象里的 MediaId 必须取到，got %q", got)
	}
	// 反向守卫：摊平形态不能被两层查找改坏。
	flat := map[string]any{"MsgType": "image", "media_id": "media_flat_1"}
	if got := wecomSubString(flat, wecomMediaContainers, "MediaId", "media_id"); got != "media_flat_1" {
		t.Fatalf("顶层摊平形态仍要取得到，got %q", got)
	}
	// 既不在顶层也不在已知子对象里 → 空串，不能误取到别的子对象的值。
	elsewhere := map[string]any{"MsgType": "image", "Other": map[string]any{"MediaId": "nope"}}
	if got := wecomSubString(elsewhere, wecomMediaContainers, "MediaId", "media_id"); got != "" {
		t.Fatalf("未知子对象不该被当成媒体容器，got %q", got)
	}
}

// TestN08_DispatchWeCom_NestedMediaIDReachesHubRow 把「嵌套 MediaId 的取值」钉在**调用点**上：
// 企微收进来的 media_id 会先落进 hub.media_url（wecom_integration.go 里 MediaURL: req.MediaID），
// 长期 URL 才由异步转存回填 —— 所以 media_url 是这条取值链路上唯一可查的证据。
// 只测 helper 不测调用点不够：调用点退回「只查顶层」时 helper 依旧正确，缺陷却原样复发。
//
// 走**明文回调**形态（不加密）的理由：这条链路上要可观测就得让 dispatch 拿到明文，
// 而明文分支在 parseWeComPlain 取到 enc=="" 时就返回，不碰 wecomRepo；随后把 wecomRepo
// 摘成 nil，媒体转存协程（persistWeComMediaAsync 的第一道门禁就是它）就不会起 ——
// 单测不打企微外网，media_url 也就停在"刚收进来"的状态。
func TestN08_DispatchWeCom_NestedMediaIDReachesHubRow(t *testing.T) {
	db := setupChannelFullDB(t)
	acc, _ := n08Seed(t, db, 90413009)
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	body := []byte(`<xml><ToUserName><![CDATA[ww_n08_corp]]></ToUserName>` +
		`<FromUserName><![CDATA[ext_n08_user]]></FromUserName>` +
		`<CreateTime>1700000010</CreateTime><MsgType><![CDATA[image]]></MsgType>` +
		`<Image><MediaId><![CDATA[media_nested_1]]></MediaId></Image>` +
		`<MsgId>wecdn09_img_2</MsgId><AgentID>1000002</AgentID></xml>`)

	p, err := svc.ParsePayload(context.Background(), ChannelWeCom, body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	svc.wecomRepo = nil

	hub, err := svc.dispatchWeCom(context.Background(), fmt.Sprintf("%d", acc.ID), p, body, nil)
	if err != nil {
		t.Fatalf("dispatchWeCom: %v", err)
	}
	if hub == nil {
		t.Fatal("图片消息没有产出 hub 行")
	}
	var rows []model.MessageHub
	if err := db.Where("platform = ? AND msg_id = ?", "wecom", "wecdn09_img_2").Find(&rows).Error; err != nil {
		t.Fatalf("查询 hub: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("期望 1 行 hub，实际 %d 行", len(rows))
	}
	if rows[0].MediaURL != "media_nested_1" {
		t.Fatalf("嵌套在 <Image> 里的 MediaId 必须被调用点取到并落进 hub.media_url，got %q", rows[0].MediaURL)
	}
	if rows[0].Content != "[图片]" {
		t.Errorf("图片消息的正文占位符不该依赖 MediaId，got %q", rows[0].Content)
	}
	if rows[0].SenderID != "ext_n08_user" {
		t.Errorf("明文回调的 SenderID 仍要取 FromUserName，got %q", rows[0].SenderID)
	}
}
