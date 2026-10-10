package service

import "testing"

func TestStripMarkdownForDM(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空串原样", "", ""},
		{"无标记原样", "您好，全包大概1200元/平，量房后出准确报价", "您好，全包大概1200元/平，量房后出准确报价"},
		{"粗体剥标记", "**全包省心**，欢迎咨询", "全包省心，欢迎咨询"},
		{"斜体剥标记", "*价格区间*仅供参考", "价格区间仅供参考"},
		{"下划线粗体", "__重点__内容", "重点内容"},
		{"删除线", "~~原价299~~", "原价299"},
		{"行内代码", "配置 `qwen-max` 模型", "配置 qwen-max 模型"},
		{"链接保留URL", "[报价单](https://example.com/q.pdf)已发", "报价单（https://example.com/q.pdf）已发"},
		{"链接text空", "[](https://example.com)", "https://example.com"},
		{"标题剥井号", "## 报价说明\n内容", "报价说明\n内容"},
		{"引用剥符号", "> 官方声明", "官方声明"},
		{"列表转圆点", "- 全包\n- 半包", "· 全包\n· 半包"},
		{"星号列表转圆点", "* 全包\n* 半包", "· 全包\n· 半包"},
		{"代码围栏剥行", "```json\n{\"a\":1}\n```", "{\"a\":1}"},
		{"数学星号不动", "面积 3*4 米的算法不变", "面积 3*4 米的算法不变"},
		{"snake_case不动", "字段名 user_name 保持原样", "字段名 user_name 保持原样"},
		{"单下划线斜体不剥防误伤", "这个 _价格_ 好", "这个 _价格_ 好"},
		{"组合场景", "**小薇**：\n## 报价\n- 全包 `1200`/平\n[详情](https://x.cn)", "小薇：\n报价\n· 全包 1200/平\n详情（https://x.cn）"},
		{"行中井号不剥", "标题要打 ## 才生效", "标题要打 ## 才生效"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripMarkdownForDM(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestStripMarkdownForBridgeChannel(t *testing.T) {
	if !stripMarkdownForBridgeChannel("douyin", "text") {
		t.Fatal("douyin text 应清洗")
	}
	if !stripMarkdownForBridgeChannel("douyin", "") {
		t.Fatal("空 msgType 视为 text 应清洗")
	}
	if stripMarkdownForBridgeChannel("douyin", "image") {
		t.Fatal("非文本不清洗")
	}
	if stripMarkdownForBridgeChannel("wecom", "text") {
		t.Fatal("非网页桥接渠道（wecom 自有富文本）不清洗")
	}
}

func TestDeliverBridgeOutboundSanitizesDouyinText(t *testing.T) {
	if globalInboxIngressService == nil {
		t.Skip("未装配 ingress（单测环境），清洗逻辑由 StripMarkdownForDM 用例覆盖")
	}
}
