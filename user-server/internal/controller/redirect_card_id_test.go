package controller

import "testing"

// TestExtractCardID 覆盖 redirect.go 里 extractCardID 的解析边界。
//
// 这条闸门是被真实事故逼出来的：短链 original_url 按铁律#24 必须是 https://，
// 而修复前的实现直接在整条 URL 上 strings.Replace(pathPrefix, "", 1)，把前缀从
// URL 中间挖掉（"https://h/douyin/card/60" → "https://h60"），随后
// IndexAny(idStr, "?#/") 又从 https: 的第一个 '/' 截断成 "https:"，Sscanf 必失败。
// 结果：卡片分支永不命中 → renderCardChatPage 不执行 → onSuccess 里的
// RecordActivity 永不触发 → 卡片浏览量永久不落库（4 张 *_card_activities 表全 0 行）。
// 所以「带 scheme 的真实短链」必须逐个断言 ok=true，别只测相对路径。
func TestExtractCardID(t *testing.T) {
	cases := []struct {
		name     string
		original string
		prefix   string
		wantID   uint
		wantOK   bool
	}{
		// 铁律#24 允许的真实形态：https 目标
		{"https 基础域名", "https://example.com/douyin/card/60", "/douyin/card/", 60, true},
		{"https 业务域名", "https://hivemtk.cn/douyin/card/60", "/douyin/card/", 60, true},
		{"https 带 query", "https://hivemtk.cn/douyin/card/60?utm=1", "/douyin/card/", 60, true},
		{"https 带 fragment", "https://hivemtk.cn/douyin/card/60#top", "/douyin/card/", 60, true},
		{"https 多余路径段", "https://hivemtk.cn/douyin/card/60/extra", "/douyin/card/", 60, true},
		{"https 其它平台", "https://hivemtk.cn/tiktok/card/7", "/tiktok/card/", 7, true},
		{"https 小红书", "https://hivemtk.cn/xiaohongshu/card/12", "/xiaohongshu/card/", 12, true},
		{"https 快手", "https://hivemtk.cn/kuaishou/card/9", "/kuaishou/card/", 9, true},
		{"https 闲鱼", "https://hivemtk.cn/xianyu/card/5", "/xianyu/card/", 5, true},
		{"https 端口非默认", "https://127.0.0.1:8211/douyin/card/88", "/douyin/card/", 88, true},
		{"http 仍能解析", "http://hivemtk.cn/douyin/card/60", "/douyin/card/", 60, true},
		// 相对路径：redirect.go 的兄弟路径 renderTiktokCard 之类可能走到，仍要兼容
		{"相对路径", "/douyin/card/60", "/douyin/card/", 60, true},
		{"相对路径带 query", "/douyin/card/60?a=b", "/douyin/card/", 60, true},

		// 负例：一律不得解析出 id
		{"前缀不匹配", "https://hivemtk.cn/other/card/60", "/douyin/card/", 0, false},
		{"缺 id", "https://hivemtk.cn/douyin/card/", "/douyin/card/", 0, false},
		{"id 非数字", "https://hivemtk.cn/douyin/card/abc", "/douyin/card/", 0, false},
		{"id 为 0", "https://hivemtk.cn/douyin/card/0", "/douyin/card/", 0, false},
		{"id 非正数", "https://hivemtk.cn/douyin/card/-3", "/douyin/card/", 0, false},
		{"query 里带 id 不算", "https://hivemtk.cn/douyin/card/?id=60", "/douyin/card/", 0, false},
		{"空串", "", "/douyin/card/", 0, false},
		{"只有前缀", "/douyin/card/", "/douyin/card/", 0, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, ok := extractCardID(c.original, c.prefix)
			if id != c.wantID || ok != c.wantOK {
				t.Fatalf("extractCardID(%q, %q) = (%d, %v)，期望 (%d, %v)",
					c.original, c.prefix, id, ok, c.wantID, c.wantOK)
			}
		})
	}
}

// TestExtractCardID_AllRedirectBranches 钉住 5 个平台分支的 pathPrefix 形态，
// 防止以后有人改某个平台的路径却忘了同步这里的期望值。
func TestExtractCardID_AllRedirectBranches(t *testing.T) {
	branches := []struct {
		platform string
		prefix   string
	}{
		{"douyin", "/douyin/card/"},
		{"kuaishou", "/kuaishou/card/"},
		{"xiaohongshu", "/xiaohongshu/card/"},
		{"xianyu", "/xianyu/card/"},
		{"tiktok", "/tiktok/card/"},
	}
	for _, b := range branches {
		t.Run(b.platform, func(t *testing.T) {
			// 修复前这些 https 目标全部返回 ok=false（真实事故场景）
			id, ok := extractCardID("https://hivemtk.cn"+b.prefix+"4242", b.prefix)
			if !ok || id != 4242 {
				t.Fatalf("%s 平台短链解析失败: got (%d, %v)，期望 (4242, true)", b.platform, id, ok)
			}
		})
	}
}
