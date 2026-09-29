package service

import (
	"encoding/json"
	"strings"
	"sync"

	"hivemtk-user/internal/geo/model"
)

// IntentStrategy 单一意图的内容与信源策略
type IntentStrategy struct {
	Intent        string
	Stage         string
	SourceTypes   []string
	ContentFormat string
	PromptHint    string
}

// DefaultIntentMatrix 默认意图-策略矩阵
var DefaultIntentMatrix = map[string]IntentStrategy{
	"疑问": {
		Intent: "疑问", Stage: "认知",
		SourceTypes:   []string{"问答平台", "官方FAQ", "百科词条", "行业科普"},
		ContentFormat: "FAQ 清单 + 术语白话解释",
		PromptHint:    "以常见问题问答形式组织；优先回答'是什么/为什么'；给出可验证的事实数据；避免推销语气",
	},
	"对比": {
		Intent: "对比", Stage: "比较",
		SourceTypes:   []string{"第三方评测", "参数对照页", "用户口碑聚合"},
		ContentFormat: "维度对照表 + 客观优劣分析",
		PromptHint:    "必须包含结构化对照表；客观列出来源可查的优劣势；明确适用人群画像而非贬低竞品",
	},
	"推荐": {
		Intent: "推荐", Stage: "决策",
		SourceTypes:   []string{"排行榜文章", "权威机构背书", "真实案例"},
		ContentFormat: "Top-N 推荐 + 场景化选择建议",
		PromptHint:    "按使用场景分层推荐；每条推荐附具体理由与证据；突出售后与服务承诺",
	},
	"教程": {
		Intent: "教程", Stage: "认知",
		SourceTypes:   []string{"官方文档", "实操指南", "视频教程索引"},
		ContentFormat: "步骤化教程 + 常见问题排查",
		PromptHint:    "分步编号、可直接照做；包含前置条件与预期结果；结尾引导进阶阅读",
	},
	"评测": {
		Intent: "评测", Stage: "比较",
		SourceTypes:   []string{"实测报告", "数据基准", "第三方实验室"},
		ContentFormat: "评测报告 + 量化评分",
		PromptHint:    "以实测数据和量化指标为主；说明测试环境与方法；结论限定在测得范围内",
	},
	"信息": {
		Intent: "信息", Stage: "认知",
		SourceTypes:   []string{"行业媒体", "官方博客", "白皮书"},
		ContentFormat: "科普文章 + 行业背景",
		PromptHint:    "提供领域背景与全局视角；建立品牌专业形象；自然埋设后续意图(对比/教程)的钩子",
	},
}

// intentMatrixDefaultJSON 缺省 6 意图矩阵 JSON（DB 为空/故障时兜底）
var intentMatrixDefaultOnce sync.Once
var intentMatrixDefaultJSON string

func defaultIntentMatrixJSON() string {
	intentMatrixDefaultOnce.Do(func() {
		b, err := json.Marshal(DefaultIntentMatrix)
		if err != nil {
			b = []byte("{}")
		}
		intentMatrixDefaultJSON = string(b)
	})
	return intentMatrixDefaultJSON
}

// activeIntentMatrix DB 优先读取意图矩阵（geo_dicts/intent_matrix.matrix），
// 缺行自动播种；DB 故障或空表时 fail-open 回内置 DefaultIntentMatrix。
func activeIntentMatrix() map[string]IntentStrategy {
	var m map[string]IntentStrategy
	if err := DictJSON(model.DictCategoryIntentMatrix, "matrix", defaultIntentMatrixJSON(), &m); err != nil || len(m) == 0 {
		return DefaultIntentMatrix
	}
	return m
}

// GetIntentStrategy 获取意图策略；未知意图回退到"信息"基础策略
func GetIntentStrategy(intent string) IntentStrategy {
	m := activeIntentMatrix()
	if st, ok := m[intent]; ok {
		return st
	}
	if st, ok := m["信息"]; ok {
		return st
	}
	return DefaultIntentMatrix["信息"]
}

// EnhancePromptWithIntent 按关键词的决策意图增强生成 prompt（思维链布控核心注入点）：
// 在基础 prompt 之后追加该意图下的信源类型、内容形态与策略指令，
// 使产出内容的信源布局与用户所处决策阶段匹配。
func EnhancePromptWithIntent(basePrompt, keyword string) string {
	intent := classifyIntent(keyword)
	st := GetIntentStrategy(intent)
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString("\n6. 决策意图适配（用户处于「")
	b.WriteString(st.Stage)
	b.WriteString("」阶段的「")
	b.WriteString(intent)
	b.WriteString("」意图）：\n   - 建议信源类型：")
	b.WriteString(strings.Join(st.SourceTypes, "、"))
	b.WriteString("\n   - 内容形态：")
	b.WriteString(st.ContentFormat)
	b.WriteString("\n   - 策略要求：")
	b.WriteString(st.PromptHint)
	return b.String()
}
