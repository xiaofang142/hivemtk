package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FA-GEO-01 判据：按规则组解析 robots.txt，逐 bot 给出可执行证据。
// 覆盖明确禁止、明确允许、通配回退、多组、局部路径；不把“出现 bot 名称”或 HTTP 200 当放行。
func TestRobotsRuleGroups(t *testing.T) {
	cases := []struct {
		name   string
		robots string
		bot    string
		want   bool
	}{
		{
			name:   "名称出现但Disallow全站仍禁止",
			robots: "# GPTBot is mentioned\nUser-agent: GPTBot\nDisallow: /\n",
			bot:    "gptbot", want: false,
		},
		{
			name:   "明确允许全站",
			robots: "User-agent: GPTBot\nAllow: /\n",
			bot:    "gptbot", want: true,
		},
		{
			name:   "通配回退禁止",
			robots: "User-agent: *\nDisallow: /\n",
			bot:    "gptbot", want: false,
		},
		{
			name:   "通配仅局部路径根可达",
			robots: "User-agent: *\nDisallow: /private\n",
			bot:    "gptbot", want: true,
		},
		{
			name:   "多组特定bot覆盖通配",
			robots: "User-agent: *\nDisallow: /\n\nUser-agent: GPTBot\nAllow: /\n",
			bot:    "gptbot", want: true,
		},
		{
			name:   "多组非目标bot仍受通配禁止",
			robots: "User-agent: *\nDisallow: /\n\nUser-agent: GPTBot\nAllow: /\n",
			bot:    "claudebot", want: false,
		},
		{
			name:   "等长Allow优先于Disallow",
			robots: "User-agent: GPTBot\nDisallow: /\nAllow: /\n",
			bot:    "gptbot", want: true,
		},
		{
			name:   "根路径不受子路径Allow影响",
			robots: "User-agent: GPTBot\nDisallow: /\nAllow: /public\n",
			bot:    "gptbot", want: false,
		},
		{
			name:   "多个User-agent共享一组",
			robots: "User-agent: GPTBot\nUser-agent: ClaudeBot\nDisallow: /\n",
			bot:    "claudebot", want: false,
		},
		{
			name:   "无规则组默认放行",
			robots: "# nothing here\n",
			bot:    "gptbot", want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			groups := parseLiveRobots(tc.robots)
			got, rule := liveRobotsBotVerdict(groups, tc.bot)
			if got != tc.want {
				t.Fatalf("bot=%s got=%t want=%t, rule=%s", tc.bot, got, tc.want, rule)
			}
		})
	}
}

// 合成 HTTP 抓取：证明是“抓取后解析”而非只看响应码/名称。
func TestCheckRobotsAllowsAIBots_HTTPContract(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		want       bool
		wantDetail string
	}{
		{
			name: "名称出现不再等于放行",
			body: "User-agent: GPTBot\nDisallow: /\n",
			want: true, wantDetail: "gptbot=false(Disallow: /)",
		},
		{
			name: "通配禁止逐bot证据",
			body: "User-agent: *\nDisallow: /\n",
			want: false, wantDetail: "逐 bot:",
		},
		{
			name: "显式放行",
			body: "User-agent: GPTBot\nAllow: /\n",
			want: true, wantDetail: "规则组判定 AI 爬虫可达",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					_, _ = w.Write([]byte(tc.body))
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			ok, detail := checkRobotsAllowsAIBots(context.Background(), srv.Client(), srv.URL)
			if ok != tc.want {
				t.Fatalf("got=%t want=%t, detail=%s", ok, tc.want, detail)
			}
			if !strings.Contains(detail, tc.wantDetail) {
				t.Fatalf("detail 缺少 %q: %s", tc.wantDetail, detail)
			}
		})
	}
}

// 路径优先级：最长匹配胜出，等长 Allow 优先；无匹配时默认放行。
func TestRobotsPathPriority(t *testing.T) {
	g := liveRobotsGroup{
		disallows: []string{"/", "/private"},
		allows:    []string{"/public"},
	}
	cases := []struct {
		path string
		want bool
		rule string
	}{
		{"/", false, "Disallow: /"},
		{"/public", true, "Allow: /public"},
		{"/private", false, "Disallow: /private"},
		{"/public/x", true, "Allow: /public"},
		{"/other", false, "Disallow: /"},
	}
	for _, tc := range cases {
		got, rule := liveRobotsPathAllowed(g, tc.path)
		if got != tc.want || rule != tc.rule {
			t.Fatalf("path=%s got=(%t,%s) want=(%t,%s)", tc.path, got, rule, tc.want, tc.rule)
		}
	}
}
