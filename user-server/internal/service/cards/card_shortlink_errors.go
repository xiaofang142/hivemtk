package cards

import "errors"

// ErrCardRedirectURLRequired 表示卡片没有配置跳转地址（redirect_url），因此无法生成短链。
//
// 短链的唯一用途就是把访问者转到 redirect_url：没有目标地址时生成出来的短链是死链。
// 这个错误被显式返回，而不是静默跳过，原因有二：
//  1. 历史实现（douyin/kuaishou/xianyu/tiktok）是「先删旧短链，再判空返回 nil」，
//     于是调用「重新生成短链」时会先把已经分发出去的 /s/xxxxxx 删掉、再空手而归——
//     用户看到 200，老链接却已经 404（D-44）。
//  2. 接口返回 200 但 short_link_url/short_code 为空，前端无法区分「成功但没链接」
//     和「失败」，只能把空串渲染到分享面板上。
//
// controller 层把它映射成 400 + 填写引导。
var ErrCardRedirectURLRequired = errors.New("卡片未配置跳转地址（redirect_url），无法生成短链")
