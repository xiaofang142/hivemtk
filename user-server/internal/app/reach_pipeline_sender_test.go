package app

import (
	"context"
	"sort"
	"strings"
	"testing"

	"hivemtk-user/internal/service"
)

// 这一组用例守的是调度器那条出站口的诚实性：ReachPipelineService 在没有真实发送器时
// 会直接判失败（判据见 service 包 reach_pipeline_dispatch.go），而它装的是这里构造的
// 发送器。所以这里的不变量是：任何一条渠道要么报错、要么交回一个真发出去过的消息号，
// 绝不允许 ("", nil)——那等于一条没出网的作业被记成"已投递，回执为空"。

// TestPipelineReachSender_NoChannelReturnsSilently 逐条走 service.ReachChannels 的枚举集合。
// 用集合本身而不是抄一份渠道清单，是为了让"管道新接受一条渠道、发送器却没接"当场红。
//
// 这里只判"不许悄悄成功"这一条，不判错误文案里有没有渠道名：渠道本来就是 reach_jobs 的一列，
// 运营台读到的失败原因和渠道来自同一行的两个字段。而且 douyin/kuaishou/xiaohongshu/tiktok/xianyu
// 这几条的失败文案由 bridge 出站口的共用哨兵产出（service/bridge_outbound.go 的
// errBridgeOutboundNotReady，另有用例断它的原文），在这里要求渠道名只会把那条共用文案改成
// 每个渠道一份。本类型自己写出的两句文案（card 与未知渠道）由下面两条用例分别点名。
func TestPipelineReachSender_NoChannelReturnsSilently(t *testing.T) {
	sender := NewPipelineReachSender(nil)
	if sender == nil {
		t.Fatal("NewPipelineReachSender(nil) 不应返回 nil：返回 nil 会让装配点以为可以降级")
	}
	channels := make([]string, 0, len(service.ReachChannels))
	for ch := range service.ReachChannels {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	for _, ch := range channels {
		msgID, err := sender.SendReach(context.Background(), ch, "acc-1", "u-1", "内容")
		if err == nil && msgID == "" {
			t.Errorf("渠道 %s 交回 (空消息号, nil)：调度器会把它当投递成功写进 _tracking", ch)
		}
	}
}

// TestPipelineReachSender_CardRefusesWithReason 卡片缺的是 card_id 的来源，不是发送实现：
// 错误文案必须同时说出"没有 card_id 来源"和"单条外发走哪条路"，否则下一个人会以为
// IntegrationReachAdapter.SendCard 那条也还没写。
func TestPipelineReachSender_CardRefusesWithReason(t *testing.T) {
	_, err := NewPipelineReachSender(nil).SendReach(context.Background(), "card", "acc-1", "u-1", "内容")
	if err == nil {
		t.Fatal("卡片在批量管道里没有 card_id 来源，必须报错而不是返回空号当成功")
	}
	for _, want := range []string{"card_id", "reach.card.send"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误文案少了 %q，读不出这是缺参数而不是缺实现: %v", want, err)
		}
	}
}

// TestPipelineReachSender_UnknownChannelRefuses 管道外的渠道不能悄悄通过。
func TestPipelineReachSender_UnknownChannelRefuses(t *testing.T) {
	_, err := NewPipelineReachSender(nil).SendReach(context.Background(), "msn", "acc-1", "u-1", "内容")
	if err == nil {
		t.Fatal("未知渠道应报错")
	}
	if !strings.Contains(err.Error(), "msn") {
		t.Errorf("错误里要点名是哪条渠道: %v", err)
	}
}
