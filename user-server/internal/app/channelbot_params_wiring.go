package app

import (
	"context"
	"time"

	corebot "hivemtk-user/internal/channelbot/core"
	qqbot "hivemtk-user/internal/channelbot/qq"
	tgbot "hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/service"
)

// WireChannelBotConfigParams 把渠道侧的长度/条数上限与外部依赖超时接进参数中心。
//
// 为什么这几个点需要装配层而不能像 bridge/sse.go 那样自己读 config_params：
// internal/service 反过来 import 了 channelbot/{qq,telegram,core}
// （webhook.go:14-15、qq_account.go:14-15、telegram_gate.go:14、
// channel_error.go:12、feishu.go:24-25），子包一旦 import service 就成了环。
// 所以底层包只暴露「provider + 读取函数」的缝，注入统一放在这里做。
//
// 返回本条接线的参数键名，供启动日志与装配层测试核对。
func WireChannelBotConfigParams() []string {
	bg := context.Background()
	cp := service.GlobalConfigParam()
	wired := make([]string, 0, 5)

	qqbot.SetQQMessageMaxLenProvider(func() int {
		return cp.GetInt(bg, "channelbot", "qq_message_max_len", qqbot.QQMessageMaxLen)
	})
	wired = append(wired, "channelbot.qq_message_max_len")

	tgbot.SetTGMessageMaxLenProvider(func() int {
		return cp.GetInt(bg, "channelbot", "tg_message_max_length", tgbot.TGMessageMaxLength)
	})
	wired = append(wired, "channelbot.tg_message_max_length")

	tgbot.SetTGInlineRowsMaxProvider(func() int {
		return cp.GetInt(bg, "channelbot", "tg_inline_rows_max", tgbot.TGInlineRowsMax)
	})
	wired = append(wired, "channelbot.tg_inline_rows_max")

	tgbot.SetTGInlineButtonsPerRowMaxProvider(func() int {
		return cp.GetInt(bg, "channelbot", "tg_inline_buttons_per_row_max", tgbot.TGInlineButtonsPerRowMax)
	})
	wired = append(wired, "channelbot.tg_inline_buttons_per_row_max")

	corebot.SetHTTPTimeoutProvider(func() time.Duration {
		return cp.GetDuration(bg, "channelbot", "http_timeout", corebot.DefaultHTTPTimeout)
	})
	wired = append(wired, "channelbot.http_timeout")

	return wired
}
