package service

// site_ma04_test.go —— MA-04：deploy 失败文案/stdout 的 git 凭证剥离。
// 红转绿锚点：修前 repo URL（含 https://user:token@ 形态）原样进错误文案
// 与 os.Stdout（git 报错回显远端 URL），服务器日志面可见 = 跨租户凭证泄漏。

import (
	"errors"
	"strings"
	"testing"
)

func TestScrubGitCredentialURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"user:pass 形态", "https://user:ghp_secret@github.com/x/y.git", "https://user:***@github.com/x/y.git"},
		{"仅用户名即凭证", "https://ghp_secret@github.com/x/y.git", "https://***@github.com/x/y.git"},
		{"ssh 无 userinfo", "git@github.com:x/y.git", "git@github.com:x/y.git"},
		{"公网无凭证", "https://github.com/x/y.git", "https://github.com/x/y.git"},
		{"多 URL 混合", "clone https://u:p@a.com/r.git and https://t@b.com/r.git", "clone https://u:***@a.com/r.git and https://***@b.com/r.git"},
		{"空串", "", ""},
	}
	for _, tc := range cases {
		if got := scrubGitCredentialURL(tc.in); got != tc.want {
			t.Errorf("%s: scrubGitCredentialURL(%q) = %q want %q", tc.name, tc.in, got, tc.want)
		}
	}
	// 幂等：已脱敏串再剥不变（避免日志管道重复清洗时越剥越短）。
	once := scrubGitCredentialURL("https://user:ghp_secret@github.com/x/y.git")
	if twice := scrubGitCredentialURL(once); twice != once {
		t.Errorf("二次剥离不幂等: %q -> %q", once, twice)
	}
}

func TestGitDeployError_ScrubsRepoURL(t *testing.T) {
	args := []string{"git", "-C", "/srv/geo", "remote", "set-url", "origin", "https://u:ghp_secret@github.com/x/y.git"}
	err := gitDeployError(args, errors.New("boom"))
	if err == nil {
		t.Fatal("错误不应为 nil")
	}
	if strings.Contains(err.Error(), "ghp_secret") {
		t.Errorf("凭证泄漏进错误文案: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "u:***@github.com") {
		t.Errorf("剥离后应保留用户名便于定位: %q", err.Error())
	}
}
