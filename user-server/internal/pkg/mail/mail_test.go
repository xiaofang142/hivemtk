package mail

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAutoConfig(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		wantHost string
		wantPort int
		wantSSL  bool
	}{
		{"QQ 邮箱", "test@qq.com", "smtp.qq.com", 465, true},
		{"163 邮箱", "test@163.com", "smtp.163.com", 465, true},
		{"126 邮箱", "test@126.com", "smtp.126.com", 465, true},
		{"yeah 邮箱", "test@yeah.net", "smtp.yeah.net", 465, true},
		{"sina 邮箱", "test@sina.com", "smtp.sina.com", 465, true},
		{"139 邮箱", "test@139.com", "smtp.139.com", 465, true},
		{"Gmail", "test@gmail.com", "smtp.gmail.com", 587, false},
		{"Outlook", "test@outlook.com", "smtp.live.com", 587, false},
		{"Hotmail", "test@hotmail.com", "smtp.live.com", 587, false},
		{"Yahoo", "test@yahoo.com", "smtp.mail.yahoo.com", 465, true},
		{"AOL", "test@aol.com", "smtp.aol.com", 465, true},
		{"GMX", "test@gmx.com", "smtp.gmx.com", 465, true},
		{"未知域名", "test@unknown.com", "", 587, false},
		{"空域名", "test@", "", 587, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				From: tt.from,
			}
			autoConfig(cfg)

			if cfg.Host != tt.wantHost {
				t.Errorf("autoConfig() Host = %v, want %v", cfg.Host, tt.wantHost)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("autoConfig() Port = %v, want %v", cfg.Port, tt.wantPort)
			}
			if cfg.SSL != tt.wantSSL {
				t.Errorf("autoConfig() SSL = %v, want %v", cfg.SSL, tt.wantSSL)
			}
		})
	}
}

func TestSendMail(t *testing.T) {
	cfg := Config{
		From:     "test@qq.com",
		Password: "test_password",
	}

	err := SendMail(cfg, []string{"recipient@example.com"}, "Test Subject", "Test Body", false)
	if err == nil {
		t.Log("Expected SMTP connection to fail with fake credentials")
	}

	err = SendMail(cfg, []string{"recipient@example.com"}, "Test Subject", "<h1>Test Body</h1>", true)
	if err == nil {
		t.Log("Expected SMTP connection to fail with fake credentials")
	}
}

func TestSendMailWithProvidedConfig(t *testing.T) {
	cfg := Config{
		Host:     "smtp.example.com",
		Port:     587,
		From:     "test@example.com",
		Password: "test_password",
		SSL:      false,
	}

	err := SendMail(cfg, []string{"recipient@example.com"}, "Test Subject", "Test Body", false)
	if err == nil {
		t.Log("Expected SMTP connection to fail with fake server")
	}
}

func TestSendMailEmptyTo(t *testing.T) {
	cfg := Config{
		Host:     "smtp.example.com",
		Port:     587,
		From:     "test@example.com",
		Password: "test_password",
	}

	err := SendMail(cfg, []string{}, "Test Subject", "Test Body", false)
	if err == nil {
		t.Log("Expected SendMail to fail with empty recipients")
	}
}

func TestConfigStruct(t *testing.T) {
	cfg := Config{
		Host:     "smtp.test.com",
		Port:     587,
		From:     "sender@test.com",
		Password: "secret",
		SSL:      true,
	}

	if cfg.Host != "smtp.test.com" {
		t.Error("Host field not set correctly")
	}
	if cfg.Port != 587 {
		t.Error("Port field not set correctly")
	}
	if cfg.From != "sender@test.com" {
		t.Error("From field not set correctly")
	}
	if cfg.Password != "secret" {
		t.Error("Password field not set correctly")
	}
	if cfg.SSL != true {
		t.Error("SSL field not set correctly")
	}
}

// TestBuildMessageAppliesOptions 可变参数必须真的作用到消息上。
//
// SendMail 本体连的是真 SMTP，测试碰不到它，于是"opts 收了没人用"这种形状不会有任何症状 ——
// 而这里被"用"掉的正是 List-Unsubscribe：头没了不会报错，只会让收件人找不到退订入口，
// 最后以举报率的形式打在发信域信誉上。所以把组装步单独抽出来，让头能被断言。
func TestBuildMessageAppliesOptions(t *testing.T) {
	link := "https://crm.example.com/u?t=1"
	m := buildMessage(Config{From: "ops@example.com"}, []string{"lead@example.com"}, "本月新品", "<p>正文</p>", true,
		[]Option{Unsubscribe(link)})

	if got := m.GetHeader("List-Unsubscribe"); len(got) != 1 || got[0] != "<"+link+">" {
		t.Errorf("选项没被作用到消息上，List-Unsubscribe = %v", got)
	}
	if got := m.GetHeader("List-Unsubscribe-Post"); len(got) != 1 || got[0] != "List-Unsubscribe=One-Click" {
		t.Errorf("一键退订声明缺失：%v", got)
	}
	if got := m.GetHeader("To"); len(got) != 1 || got[0] != "lead@example.com" {
		t.Errorf("收件人 = %v", got)
	}
	if got := m.GetHeader("From"); len(got) != 1 || got[0] != "ops@example.com" {
		t.Errorf("发信人 = %v，期望取 Config.From", got)
	}
}

// TestResolveSendConfig 服务器地址的兜底只在记录本身不全时才生效。
//
// 群发路径把 SMTP 记录里的 Server/Port 原样传进来，靠的就是"给了就别猜"这一条；
// 反过来（记录不全时按域名猜）是历史行为，两边都要能被断言 —— 否则改一句判空条件，
// 表现是"自建域名的信悄悄发到猜测的公共服务器上"或"记录齐全的连接被重写成 587"。
func TestResolveSendConfig(t *testing.T) {
	got := resolveSendConfig(Config{
		Host: "smtp.custom.example",
		Port: 2525,
		From: "ops@example.com",
	})
	if got.Host != "smtp.custom.example" || got.Port != 2525 {
		t.Errorf("记录齐全却被重写成了 %s:%d", got.Host, got.Port)
	}

	fallback := resolveSendConfig(Config{From: "ops@qq.com"})
	if fallback.Host != "smtp.qq.com" || fallback.Port != 465 || !fallback.SSL {
		t.Errorf("记录不全时应按域名兜底，实际得到 %s:%d ssl=%v", fallback.Host, fallback.Port, fallback.SSL)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		htmlstr  string
		clue     TemplateParseMap
		expected string
	}{
		{
			name:    "替换所有字段",
			htmlstr: "Hello {name}, welcome to {city}. Address: {address}, Account: {account}",
			clue: TemplateParseMap{
				Name:    "John",
				City:    "Beijing",
				Address: "Main St 123",
				Account: "john@example.com",
			},
			expected: "Hello John, welcome to Beijing. Address: Main St 123, Account: john@example.com",
		},
		{
			name:    "替换部分字段",
			htmlstr: "Hello {name}, welcome to {city}",
			clue: TemplateParseMap{
				Name:    "Jane",
				City:    "Shanghai",
				Address: "",
				Account: "",
			},
			expected: "Hello Jane, welcome to Shanghai",
		},
		{
			name:    "不包含占位符",
			htmlstr: "Hello World",
			clue: TemplateParseMap{
				Name:    "John",
				City:    "Beijing",
				Address: "Main St",
				Account: "john@example.com",
			},
			expected: "Hello World",
		},
		{
			name:    "空字符串",
			htmlstr: "",
			clue: TemplateParseMap{
				Name:    "John",
				City:    "Beijing",
				Address: "Main St",
				Account: "john@example.com",
			},
			expected: "",
		},
		{
			name:    "字段值为空",
			htmlstr: "Name: {name}, City: {city}",
			clue: TemplateParseMap{
				Name:    "",
				City:    "",
				Address: "",
				Account: "",
			},
			expected: "Name: , City: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.htmlstr, tt.clue)
			if result != tt.expected {
				t.Errorf("Parse() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildTrace(t *testing.T) {
	traceID := uuid.New()
	websiteURL := "https://example.com"
	htmlstr := "<html><body>Hello</body></html>"

	result := BuildTrace(htmlstr, traceID, websiteURL)

	expectedImage := `<img src="https://example.com/email/trace/` + traceID.String() + `" style="width:1px; height: 1px;" />`
	expected := htmlstr + expectedImage

	if result != expected {
		t.Errorf("BuildTrace() = %v, want %v", result, expected)
	}

	if !strings.Contains(result, "img src") {
		t.Error("BuildTrace() should contain tracking image")
	}
	if !strings.Contains(result, traceID.String()) {
		t.Error("BuildTrace() should contain trace ID")
	}
}

func TestBuildTraceEmptyHTML(t *testing.T) {
	traceID := uuid.New()
	websiteURL := "https://test.com"

	result := BuildTrace("", traceID, websiteURL)

	expectedImage := `<img src="https://test.com/email/trace/` + traceID.String() + `" style="width:1px; height: 1px;" />`

	if result != expectedImage {
		t.Errorf("BuildTrace() with empty HTML = %v, want %v", result, expectedImage)
	}
}
