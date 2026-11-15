package service

import "strings"

// platformNoticeMarkers 是各平台（抖音/小红书/微博/B站/闲鱼等）系统公告的文案特征。
// 2026-11-15 小红书 xapptool 会话实证：平台公告被 DOM 采集当消息，outbound 方向
// 回推浪费未互关 24h 仅 1 条的私信额度，inbound 方向触发 AI 答非所问（5048/5068/
// 5011-5014）。此公告识别属「输入卫生」层（与自回声/去重同层），不是回复策略——
// 客户真实发言是否应答由 LLM 基于上下文判断（见 system_prompt NO_REPLY 指令）。
var platformNoticeMarkers = []string{
	"对方关注或回复你之前",
	"最多只能发1条",
	"已连续聊天",
	"获得聊天状态",
	"暂不支持该消息类型",
	"请到手机端查看",
	"请到抖音手机端查看",
	"朋友验证",
	"消息已发出，但被对方拒收",
	"发送失败，请稍后重试",
}

// IsPlatformSystemNotice 判断文本是否为平台系统公告（非客户发言）。
// 归一化（去空白）后子串匹配，变形（空格/换行插入）也能命中。
func IsPlatformSystemNotice(content string) bool {
	norm := strings.Join(strings.Fields(content), "")
	if norm == "" {
		return false
	}
	for _, m := range platformNoticeMarkers {
		if strings.Contains(norm, m) {
			return true
		}
	}
	return false
}
