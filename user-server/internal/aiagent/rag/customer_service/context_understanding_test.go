package ragcustomerservice

import (
	"context"
	"strings"
	"testing"
)

func newCUSSvc() *ContextUnderstandingServiceImpl {
	return NewContextUnderstandingService(nil)
}

func TestNewContextUnderstandingServiceDefaults(t *testing.T) {
	d := newCUSSvc()
	if d.config.IntentRecognitionThreshold != 0.6 || !d.config.EntityExtractionEnabled ||
		!d.config.SentimentAnalysisEnabled || !d.config.TopicDetectionEnabled {
		t.Errorf("nil 配置应填默认值: %+v", d.config)
	}
	custom := NewContextUnderstandingService(&ContextUnderstandingConfig{EntityExtractionEnabled: false})
	if custom.config.EntityExtractionEnabled {
		t.Error("显式 false 不得被默认值覆盖")
	}
}

func TestAnalyzeIntentCategories(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		msg      string
		wantTop  string
		wantCat0 string
		conf     float64
	}{
		{"你好呀", "greeting", "social", 0.8},
		{"HELLO there", "greeting", "social", 0.8},
		{"这个产品多少钱", "product_inquiry", "sales", 0.8},
		{"我的订单还没发货", "order_inquiry", "support", 0.8},
		{"我要投诉", "complaint_support", "support", 0.8},
		{"谢谢你的帮助", "positive_feedback", "social", 0.8},
		{"随便看看天气", "general_inquiry", "general", 0.6},
	}
	for _, tc := range cases {
		got, err := newCUSSvc().AnalyzeIntent(ctx, tc.msg, nil)
		if err != nil {
			t.Fatalf("AnalyzeIntent(%q): %v", tc.msg, err)
		}
		if got.PrimaryIntent != tc.wantTop || got.Confidence != tc.conf {
			t.Errorf("msg=%q intent=%q conf=%v want %q/%v",
				tc.msg, got.PrimaryIntent, got.Confidence, tc.wantTop, tc.conf)
		}
		if len(got.Categories) == 0 || got.Categories[0] != tc.wantCat0 {
			t.Errorf("msg=%q categories=%v want 首位 %q", tc.msg, got.Categories, tc.wantCat0)
		}
	}
	if _, err := newCUSSvc().AnalyzeIntent(ctx, "", nil); err == nil {
		t.Error("空消息应报错")
	}
	withParams, _ := newCUSSvc().AnalyzeIntent(ctx, "订单号 12345 有货吗", nil)
	if withParams.Parameters["order_number"] != "ORDER123456" {
		t.Errorf("参数抽取应命中订单号（当前为固定样例值）, got %v", withParams.Parameters)
	}
}

func TestExtractEntitiesRespectsSwitch(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()
	got, err := svc.ExtractEntities(ctx, "明天想买一件苹果的裙子，下周也要")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if strings.Join(got["time"], ",") != "明天,下周" {
		t.Errorf("time 实体=%v want 明天,下周", got["time"])
	}
	if strings.Join(got["product"], ",") != "裙子" {
		t.Errorf("product 实体=%v", got["product"])
	}
	if strings.Join(got["brand"], ",") != "苹果" {
		t.Errorf("brand 实体=%v", got["brand"])
	}

	off := NewContextUnderstandingService(&ContextUnderstandingConfig{}) // EntityExtractionEnabled=false
	empty, err := off.ExtractEntities(ctx, "明天想买裙子")
	if err != nil {
		t.Fatalf("关闭开关仍应无错误: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("关闭开关应返回空 map, got %v", empty)
	}

	none, _ := svc.ExtractEntities(ctx, "毫无关键词的一句话")
	if len(none) != 0 {
		t.Errorf("无关键词应为空 map, got %v", none)
	}
	if _, err := svc.ExtractEntities(ctx, ""); err == nil {
		t.Error("空消息应报错")
	}
}

func TestAnalyzeSentimentScoring(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()

	pos, err := svc.AnalyzeSentiment(ctx, "这个方案真不错，我很喜欢，推荐给大家")
	if err != nil {
		t.Fatalf("AnalyzeSentiment: %v", err)
	}
	if pos.Label != "positive" || pos.Score <= 0.1 {
		t.Errorf("正面语料 score=%v label=%q", pos.Score, pos.Label)
	}

	neg, _ := svc.AnalyzeSentiment(ctx, "太失望了，质量差，还很贵")
	if neg.Label != "negative" || neg.Score >= -0.1 {
		t.Errorf("负面语料 score=%v label=%q", neg.Score, neg.Label)
	}

	mixed, _ := svc.AnalyzeSentiment(ctx, "还好，就是有点贵") // 命中 1 正（好）1 负（贵）
	if mixed.Score != 0 || mixed.Label != "neutral" {
		t.Errorf("正负词各一次应抵消为中性, score=%v label=%q", mixed.Score, mixed.Label)
	}

	neu, _ := svc.AnalyzeSentiment(ctx, "今天周三")
	if neu.Label != "neutral" || neu.Score != 0.0 {
		t.Errorf("中性语料 score=%v label=%q", neu.Score, neu.Label)
	}

	angry, _ := svc.AnalyzeSentiment(ctx, "我很生气，也很开心你们处理了")
	types := map[string]bool{}
	for _, e := range angry.Emotions {
		types[e.Type] = true
	}
	if !types["anger"] || !types["joy"] {
		t.Errorf("情绪识别应同时命中 anger 与 joy, got %+v", angry.Emotions)
	}

	scare, _ := svc.AnalyzeSentiment(ctx, "很难过，也很害怕")
	types = map[string]bool{}
	for _, e := range scare.Emotions {
		types[e.Type] = true
	}
	if !types["sadness"] || !types["fear"] {
		t.Errorf("情绪识别应同时命中 sadness 与 fear, got %+v", scare.Emotions)
	}

	off := NewContextUnderstandingService(&ContextUnderstandingConfig{})
	neutral, _ := off.AnalyzeSentiment(ctx, "太失望了")
	if neutral.Score != 0.0 || neutral.Label != "neutral" || neutral.Emotions != nil {
		t.Errorf("关闭情感分析应直接返回中性: %+v", neutral)
	}
	if _, err := svc.AnalyzeSentiment(ctx, ""); err == nil {
		t.Error("空消息应报错")
	}
}

func TestUpdateContextAndTopicDetection(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()

	base := Context{Topic: "product_inquiry"}
	msg := Message{Content: "你好"} // greeting 与 product_inquiry 跨域 → 应判话题变更
	intent, _ := svc.AnalyzeIntent(ctx, msg.Content, nil)

	updated, err := svc.UpdateContext(ctx, base, msg, intent)
	if err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}
	if updated.Topic != "greeting" {
		t.Errorf("话题应切换为 greeting, got %q", updated.Topic)
	}
	if len(updated.PreviousTopics) != 1 || updated.PreviousTopics[0] != "product_inquiry" {
		t.Errorf("历史话题未记录: %v", updated.PreviousTopics)
	}
	if updated.Intent != intent.PrimaryIntent {
		t.Errorf("intent 未写入: %q", updated.Intent)
	}
	if updated.Sentiment.Label == "" || updated.LastInteraction.IsZero() {
		t.Error("情感与最后交互时间应被写入")
	}

	// 同域消息不切换话题，但参数应写进 Entities
	orderMsg := Message{Content: "订单号 9527 什么时候发货"}
	orderIntent, _ := svc.AnalyzeIntent(ctx, orderMsg.Content, nil)
	same, _ := svc.UpdateContext(ctx, Context{Topic: "product_inquiry"}, orderMsg, orderIntent)
	if same.Topic != "product_inquiry" || len(same.PreviousTopics) != 0 {
		t.Errorf("同域不应切换话题: topic=%q prev=%v", same.Topic, same.PreviousTopics)
	}
	if len(same.Entities["order_number"]) != 1 {
		t.Errorf("order_number 参数应写入 Entities: %v", same.Entities)
	}

	// 空 intent 不覆盖既有 intent
	kept, _ := svc.UpdateContext(ctx, Context{Topic: "x", Intent: "keep"}, Message{Content: "随便"}, IntentAnalysis{})
	if kept.Intent != "keep" {
		t.Errorf("空 PrimaryIntent 不应覆盖, got %q", kept.Intent)
	}

	// 已有 Entities 时原地合并；浅拷贝共享底层 map 属既有行为（见 Findings 3），在此钉住
	seeded := Context{Topic: "greeting", Entities: map[string][]string{"a": {"1"}}}
	merged, _ := svc.UpdateContext(ctx, seeded, orderMsg, orderIntent)
	if len(merged.Entities["order_number"]) != 1 || merged.Entities["a"] == nil {
		t.Errorf("应合并进已有 Entities 且不丢既有键: %v", merged.Entities)
	}
	if _, aliased := seeded.Entities["order_number"]; !aliased {
		t.Error("UpdateContext 与调用方共享同一 Entities map（既有行为），若改为深拷贝需同步更新本断言")
	}

	if changed, topic, err := svc.DetectTopicChange(ctx, "complaint_support", Message{Content: "我要投诉"}); changed || topic != "complaint_support" || err != nil {
		t.Errorf("同域话题不应变更: changed=%v topic=%q err=%v", changed, topic, err)
	}
	if changed, topic, _ := svc.DetectTopicChange(ctx, "greeting", Message{Content: "你好"}); !changed || topic != "greeting" {
		t.Errorf("greeting 不在 sales/support 话题表内，同词也应判变更（既有行为）: changed=%v topic=%q", changed, topic)
	}
	if changed, topic, _ := svc.DetectTopicChange(ctx, "product_inquiry", Message{Content: "你好"}); !changed || topic != "greeting" {
		t.Errorf("跨域应判为话题变更: changed=%v topic=%q", changed, topic)
	}
	if _, _, err := svc.DetectTopicChange(ctx, "product_inquiry", Message{Content: ""}); err == nil {
		t.Error("空消息应透传 AnalyzeIntent 错误")
	}

	off := NewContextUnderstandingService(&ContextUnderstandingConfig{})
	if changed, topic, _ := off.DetectTopicChange(ctx, "old", Message{Content: "你好"}); changed || topic != "old" {
		t.Error("关闭话题检测时应原样返回")
	}
	sentimentless, err := off.UpdateContext(ctx, Context{}, Message{Content: ""}, IntentAnalysis{})
	if err != nil {
		t.Errorf("UpdateContext 应吞掉内部子调用错误: %v", err)
	}
	if sentimentless.Sentiment.Label != "" {
		t.Errorf("子调用报错时不应写入情感: %+v", sentimentless.Sentiment)
	}
	// 开启话题检测但子调用报错：既不改话题也不返回错误
	topicKept, err := svc.UpdateContext(ctx, Context{Topic: "t"}, Message{Content: ""}, IntentAnalysis{})
	if err != nil || topicKept.Topic != "t" || len(topicKept.PreviousTopics) != 0 {
		t.Errorf("DetectTopicChange 报错时 UpdateContext 应原样保留话题: %+v %v", topicKept, err)
	}

	prefs, err := svc.GetUserPreferences(ctx, "u", "wx")
	if err != nil || prefs == nil || len(prefs) != 0 {
		t.Errorf("GetUserPreferences 应返回空 map: %v %v", prefs, err)
	}
	if err := svc.UpdateUserPreferences(ctx, "u", "wx", map[string]any{"k": "v"}); err != nil {
		t.Errorf("UpdateUserPreferences 应为 no-op: %v", err)
	}
}

func TestPureHelpers(t *testing.T) {
	if got := toLower("AbC中文Z"); got != "abc中文z" {
		t.Errorf("toLower=%q", got)
	}
	if findSubstring("abcdef", "cd") != 2 {
		t.Error("findSubstring 起始下标错")
	}
	if findSubstring("ab", "abc") != -1 {
		t.Error("text 短于 substr 应返回 -1")
	}
	if findSubstring("abc", "") != 0 {
		t.Error("空 substr 应返回 0")
	}
	if contains("abc", "") != true || contains("ab", "abc") != false {
		t.Error("contains 边界错")
	}
	if !containsAny("xx订单yy", []string{"订单", "退款"}) || containsAny("xx", []string{}) {
		t.Error("containsAny 边界错")
	}
	if hasAnyWords("今天很开心", []string{"开心"}) != true || hasAnyWords("今天", []string{"开心"}) != false {
		t.Error("hasAnyWords 边界错")
	}
	if getSentimentLabel(0.9) != "positive" || getSentimentLabel(-0.9) != "negative" || getSentimentLabel(0.0) != "neutral" {
		t.Error("getSentimentLabel 阈值边界错")
	}
	// 否定语义：命中负面词的区间不再参与正面词计数，否则「不好」里的「好」把分数抵消成 0 判成中性。
	if got := calculateSentimentScore("不好"); got >= 0 {
		t.Errorf("calculateSentimentScore(\"不好\")=%v，否定表述应判负", got)
	}
	// 同一类的其余形态：整词表里没有「不喜欢/不满意/不推荐/不值得」，靠否定前缀规则收口。
	for _, s := range []string{"不喜欢", "不满意", "不推荐", "不值得"} {
		if got := calculateSentimentScore(s); got >= 0 {
			t.Errorf("calculateSentimentScore(%q)=%v，「否定词+正面词」应判负", s, got)
		}
	}
	// 表内固定正面词不得被否定前缀误伤：「不错」整体是褒义，拆成「不+错」就把褒义判成贬义。
	if got := calculateSentimentScore("不错"); got <= 0 {
		t.Errorf("calculateSentimentScore(\"不错\")=%v，应为正", got)
	}
	if got := calculateSentimentScore("很好"); got <= 0 {
		t.Errorf("calculateSentimentScore(\"很好\")=%v，应为正", got)
	}
	if got := calculateSentimentScore("太差了"); got >= 0 {
		t.Errorf("calculateSentimentScore(\"太差了\")=%v 应为负", got)
	}
	if calculateSentimentScore("还行吧") != 0 {
		t.Error("无情感词应为 0")
	}

	if got := extractTimeEntities("昨天和前天下单"); len(got) != 2 || got[0] != "昨天" || got[1] != "前天" {
		t.Errorf("extractTimeEntities=%v", got)
	}
	if got := extractProductEntities("手机和电脑还有书籍"); len(got) != 3 || got[0] != "手机" {
		t.Errorf("extractProductEntities=%v", got)
	}
	if got := extractBrandEntities("阿迪达斯和优衣库"); len(got) != 2 || got[1] != "优衣库" {
		t.Errorf("extractBrandEntities=%v", got)
	}
	if extractOrderNumber("号") != "ORDER123456" || extractOrderNumber("无") != "" {
		t.Error("extractOrderNumber 分支错（当前实现返回固定样例号）")
	}
	if extractProductName("我想买裙子") != "裙子" || extractProductName("买个手机") != "手机" || extractProductName("无") != "" {
		t.Error("extractProductName 分支错")
	}

	params := extractParameters("订单号 123 商品 裙子 到货")
	if params["order_number"] != "ORDER123456" || params["product_name"] != "裙子" {
		t.Errorf("extractParameters=%v", params)
	}
	if len(extractParameters("无关文本")) != 0 {
		t.Error("无关键词时参数应为空")
	}

	if isRelatedTopic("", "greeting") {
		t.Error("空话题不应判相关")
	}
	if !isRelatedTopic("product_inquiry", "order_inquiry") {
		t.Error("sales 域内应判相关")
	}
	if !isRelatedTopic("complaint_support", "troubleshooting") {
		t.Error("support 域内应判相关")
	}
	if isRelatedTopic("product_inquiry", "complaint_support") {
		t.Error("跨域应判不相关")
	}
}
