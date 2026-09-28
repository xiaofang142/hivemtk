package main

// 这两条锁守的是「新机器能不能照着仓库把 Host 装起来」：
//  1. 扩展 ID 必须从 manifest 写死的 key 推导，而不是让人手抄一个参数——
//     抄错一位就是一条 allowed_origins 不匹配的 Chrome 拒绝，而 Chrome 只回一句
//     "Specified native messaging host not found"，看不出是 ID 错了；
//  2. Host 连哪台服务端必须能在 ~/.hivemtk/nm_host.conf 里改。Chrome 启动 NM Host 时
//     不继承终端环境变量，只认 env 的话等于「服务端口不是 8204 就连不上，且无法配置」。
// 判据用两个独立实现互相核对：Go 这边算一遍 ID，install.sh --check 那边算一遍，
// 逐字相同才算数（只测脚本自己的输出等于让脚本给自己打分）。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// manifestKeyRE 取 manifest.json 里的 "key"（单行 JSON，值是无换行的 base64）
var manifestKeyRE = regexp.MustCompile(`"key":\s*"([^"]+)"`)

// deriveExtIDFromKey 按 Chrome 的规则从 SPKI 公钥推导扩展 ID：
// sha256(DER) 的前 32 个十六进制位，每位的数值 0..15 映射成 'a'+值。
func deriveExtIDFromKey(t *testing.T, keyB64 string) string {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		t.Fatalf("manifest key 不是合法 base64: %v", err)
	}
	sum := sha256.Sum256(der)
	frag := hex.EncodeToString(sum[:])[:32]
	out := make([]byte, len(frag))
	for i := 0; i < len(frag); i++ {
		// Chrome 是按**十六进制位**映射（32 位 → 32 个字母），不是按字节
		c := frag[i]
		switch {
		case c >= '0' && c <= '9':
			out[i] = byte('a' + (c - '0'))
		case c >= 'a' && c <= 'f':
			out[i] = byte('a' + (c - 'a') + 10)
		default:
			t.Fatalf("sha256 前缀含非十六进制字符 %q", c)
		}
	}
	return string(out)
}

func extensionManifestPath(t *testing.T) string {
	t.Helper()
	// 测试的工作目录是 cmd/nm-host，扩展源码在仓库同级的 user-web 下
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "user-web", "browser_automation", "manifest.json"))
	if err != nil {
		t.Fatalf("定位扩展 manifest 失败: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("扩展 manifest 不在预期位置（%s），本用例失去依据: %v", p, err)
	}
	return p
}

func TestInstallScriptDerivesExtensionID(t *testing.T) {
	manifestPath := extensionManifestPath(t)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("读扩展 manifest 失败: %v", err)
	}
	m := manifestKeyRE.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("manifest %s 里没有 key 字段——ID 推导失去依据（key 被删则每次加载换一个 ID）", manifestPath)
	}
	want := deriveExtIDFromKey(t, string(m[1]))

	script := filepath.Join(".", "install.sh")
	out, err := exec.Command("bash", script, "--check").CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh --check 跑不起来（判据失去依据）: %v\n%s", err, out)
	}
	got := ""
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ext_id="); ok {
			got = v
		}
	}
	if got == "" {
		t.Fatalf("install.sh --check 没打印 ext_id=<id>，脚本没把 ID 当成可核对的输出:\n%s", out)
	}
	if got != want {
		t.Errorf("install.sh 推导的扩展 ID = %q，Go 独立推导 = %q，两者必须逐字相同", got, want)
	}

	// 安装态核对：本机若已注册过 NM Host，脚本算出的 ID 必须和已注册的 allowed_origins 一致，
	// 否则「脚本自洽但和真实 Chrome 加载的那份不是一回事」。
	installed := filepath.Join(os.Getenv("HOME"),
		"Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", "com.hivemtk.browser.json")
	if data, err := os.ReadFile(installed); err == nil {
		if !strings.Contains(string(data), "chrome-extension://"+want+"/") {
			t.Errorf("已注册的 manifest allowed_origins 里没有推导出的 ID %s（脚本与本机安装态脱节）", want)
		}
	} else if !os.IsNotExist(err) {
		t.Errorf("读已注册的 NM Host manifest 失败: %v", err)
	}

	src, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("读 install.sh 失败: %v", err)
	}
	text := string(src)
	// 只判**命令位**上的 sudo：注释里解释「为什么不用 sudo」是必要的，
	// 拿 strings.Contains 扫全文会把说明文字判成违规（守卫该读的是执行位）。
	if sudoCall := regexp.MustCompile(`(?m)^[^\S\n]*sudo[^\S\n]`).FindString(text); sudoCall != "" {
		t.Errorf("install.sh 仍在调用 sudo（%q）：NM Host 装在用户可写目录即可，sudo 会在无 tty 的 CI/远程执行里卡死", strings.TrimSpace(sudoCall))
	}
	if strings.Contains(text, "YOUR_EXT_ID_HERE") {
		t.Error("install.sh 还留着「扩展 ID 待填」的占位分支——ID 应由 key 推导，占位分支会把错的 ID 写进 allowed_origins")
	}
}

func TestHostConfServerURLEnvAndFilePrecedence(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "nm_host.conf")
	writeConf := func(body string) {
		t.Helper()
		if err := os.WriteFile(confPath, []byte(body), 0o600); err != nil {
			t.Fatalf("写配置失败: %v", err)
		}
	}

	// 只有配置文件：server_url= 与 token= 都要读出来
	writeConf("token=file-token\nserver_url=ws://10.0.0.2:9001/api/browser/host-ws\n")
	conf, err := readConfFile(confPath)
	if err != nil {
		t.Fatalf("读配置失败: %v", err)
	}
	if got := conf["token"]; got != "file-token" {
		t.Errorf("conf token 读取失败，got %q", got)
	}
	if got := conf["server_url"]; got != "ws://10.0.0.2:9001/api/browser/host-ws" {
		t.Errorf("conf server_url 读取失败，got %q", got)
	}

	// 优先级：环境变量压过配置文件（临时换服务端不该要求改文件）
	t.Setenv("HIVE_MTK_WS_URL", "ws://env-host:1/api/browser/host-ws")
	if got := resolveServerURL(conf, defaultWsURL); got != "ws://env-host:1/api/browser/host-ws" {
		t.Errorf("HIVE_MTK_WS_URL 应当压过配置文件，got %q", got)
	}
	t.Setenv("HIVE_MTK_WS_URL", "")
	if got := resolveServerURL(conf, defaultWsURL); got != "ws://10.0.0.2:9001/api/browser/host-ws" {
		t.Errorf("没有环境变量时应当用配置文件里的 server_url，got %q", got)
	}

	// 配置文件里没写 server_url：退回编译期默认值，而不是空串（空串会让 dialAddr 报错并静默退出）
	writeConf("token=only-token\n")
	conf, err = readConfFile(confPath)
	if err != nil {
		t.Fatalf("读配置失败: %v", err)
	}
	if got := resolveServerURL(conf, defaultWsURL); got != defaultWsURL {
		t.Errorf("缺 server_url 时应当退回默认值 %q，got %q", defaultWsURL, got)
	}

	// 配置文件不存在不能把 Host 判死（首次安装就是没有这个文件）
	if conf, err := readConfFile(filepath.Join(dir, "missing.conf")); err != nil {
		t.Errorf("配置文件缺失应当返回空表而不是报错: %v", err)
	} else if len(conf) != 0 {
		t.Errorf("配置文件缺失应当返回空表，got %v", conf)
	}

	// 主链路必须真的用上这个解析结果，否则解析函数只是摆在那儿的死代码
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读 main.go 失败: %v", err)
	}
	if !strings.Contains(string(src), "addr := resolveServerURL(") {
		t.Error(`main() 没有走 resolveServerURL(conf…)：配置文件里的 server_url 读回来没人用`)
	}
}
