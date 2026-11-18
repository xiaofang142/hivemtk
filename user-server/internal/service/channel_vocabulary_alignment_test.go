package service

import (
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// TestChannelVocabularyAlignedWithRegistry 词表对齐总闸（2026-11-19 二次审核新增）。
//
// 背景：渠道注册一次要在多处留声——ai_tagger channel 词表、NormalizeChannelType 别名
// 映射、markdownDMChannels 纯文本白名单、extractSessionSkills 技能分组、
// capability_runtime bridge 分组。pdd（2026-11-09 二次审核补齐）与本轮五渠道
// （2026-11-19 二次审核补齐）两轮都在同一批面上漏登记，症状各异（标签落不了/绑定漂移/
// MD 原样暴露/技能缺失），既有门禁全拦不住。本闸把「channelgw.Default 注册表」作为
// 单一事实源，锁死消费面；与 channelgw/registry_parity_test.go 闸 5
// （channelcaps/capregistry 面）共同构成「一次注册、处处生效」的全链锁。
func TestChannelVocabularyAlignedWithRegistry(t *testing.T) {
	// registryNames 与 channelgw Default 注册表字面对齐（service 包测试不能 import
	// channelgw——依赖方向 channelgw→service，本闸用字面快照锁定，模式同
	// registry_parity_test.go 闸 4 的 wantOutbox；注册表变更必须同步本清单）。
	registryNames := []string{
		"douyin", "xiaohongshu", "kuaishou", "xianyu", "tiktok",
		"jd", "bilibili", "weibo", "personal_wx", "pdd",
		"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us",
	}
	names := registryNames

	// ① ai_tagger channel 词表 ⊇ 注册表全部渠道（personal_wx 例外：伴生外部工程，
	//    AI 谈单标签不在伴生渠道采集——与既有词表口径一致）。
	tagger := NewAITagger()
	taggerChannels := map[string]bool{}
	for _, ch := range tagger.tagTaxonomy["channel"] {
		taggerChannels[ch] = true
	}
	for _, n := range names {
		if !taggerChannels[n] && n != string(model.ChannelPersonalWX) {
			t.Errorf("① ai_tagger channel 词表缺注册渠道 %q（AI 谈单标签在该渠道无法落库）", n)
		}
	}

	// ② NormalizeChannelType：注册渠道名归一后必须自辨识（不得漂移成别的渠道）。
	for _, n := range names {
		switch n {
		case "web", "web_embed":
			continue // web 族不在 Default 注册表，防御性跳过
		}
		if got := NormalizeChannelType(n); got != n {
			t.Errorf("② NormalizeChannelType(%q) = %q：注册渠道名经映射后漂移", n, got)
		}
	}

	// ③ markdownDMChannels ⊇ 全部桥接渠道（网页私信输入框都是纯文本，MD 会原样暴露）。
	for _, n := range BridgeOutboxChannels() {
		if !markdownDMChannels[n] {
			t.Errorf("③ markdownDMChannels 缺桥接渠道 %q", n)
		}
	}

	// ④ extractSessionSkills：桥接渠道（personal_wx 除外——技能面走 wechat 分组）应得 social_media。
	for _, n := range BridgeOutboxChannels() {
		sess := &model.CustomerSession{Platform: model.Platform(n)}
		skills := extractSessionSkills(sess)
		if !strings.Contains(strings.Join(skills, ","), "social_media") && n != string(model.ChannelPersonalWX) {
			t.Errorf("④ 渠道 %s 技能分组缺 social_media: %v", n, skills)
		}
	}

	// ⑤ capability_runtime 的 bridge 分组：以五渠道在词表与 capregistry 双面在场替代
	//    直锁未导出 case 清单；case 漏项由 capregistry 一致性测试与词表对齐共同压制。
	for _, want := range []string{"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us"} {
		if !taggerChannels[want] {
			t.Errorf("⑤ 二次审核补齐面 %q 未在词表（capability_runtime case 同批核对）", want)
		}
	}
}
