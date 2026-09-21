package mail

import (
	"crypto/tls"
	"fmt"
	"strings"

	"gopkg.in/gomail.v2"
)

type Config struct {
	Host     string
	Port     int
	From     string `json:"from"`
	Password string `json:"password"`
	SSL      bool   `json:"ssl"`
}

// ImplicitTLSPort 隐式 TLS（SMTPS）的标准端口。其余端口走 STARTTLS —— gomail.NewDialer
// 用的也是这一条判据，两处一致才不会让同一条 SMTP 记录在两条外发路径上表现不同。
const ImplicitTLSPort = 465

// SendMail 发一封信。opts 是给合规头部（List-Unsubscribe 等）留的接缝：
// 可变参数让既有调用方一行不改，新调用方能拿到"这封信真发出去时头会长什么样"的控制点。
func SendMail(cfg Config, to []string, subject, body string, isHTML bool, opts ...Option) error {
	cfg = resolveSendConfig(cfg)

	m := buildMessage(cfg, to, subject, body, isHTML, opts)

	d := &gomail.Dialer{
		Host:      cfg.Host,
		Port:      cfg.Port,
		Username:  cfg.From,
		Password:  cfg.Password,
		SSL:       cfg.SSL,
		TLSConfig: &tls.Config{ServerName: cfg.Host},
	}

	if conn, err := d.Dial(); err == nil {
		_ = conn.Close()
	} else {
		return fmt.Errorf("SMTP连接测试失败: %v", err)
	}

	return d.DialAndSend(m)
}

// resolveSendConfig 记录里给了地址就照用，缺哪一项才按发信域名兜底。
func resolveSendConfig(cfg Config) Config {
	if cfg.Host == "" || cfg.Port == 0 {
		autoConfig(&cfg)
	}
	return cfg
}

// buildMessage 组装一封信的头与正文，最后才作用 opts —— 顺序让选项能覆盖前面任何一项。
func buildMessage(cfg Config, to []string, subject, body string, isHTML bool, opts []Option) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", cfg.From)
	m.SetHeader("To", to...)
	m.SetHeader("Subject", subject)

	contentType := "text/plain"
	if isHTML {
		contentType = "text/html"
	}
	m.SetBody(contentType, body)
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func autoConfig(cfg *Config) {
	domain := strings.Split(cfg.From, "@")

	switch {
	case len(domain) > 1 && strings.Contains(domain[1], "qq.com"):
		cfg.Host = "smtp.qq.com"
	case len(domain) > 1 && strings.Contains(domain[1], "163.com"):
		cfg.Host = "smtp.163.com"
	case len(domain) > 1 && strings.Contains(domain[1], "126.com"):
		cfg.Host = "smtp.126.com"
	case len(domain) > 1 && strings.Contains(domain[1], "yeah.net"):
		cfg.Host = "smtp.yeah.net"
	case len(domain) > 1 && strings.Contains(domain[1], "sina.com"):
		cfg.Host = "smtp.sina.com"
	case len(domain) > 1 && strings.Contains(domain[1], "139.com"):
		cfg.Host = "smtp.139.com"
	case len(domain) > 1 && strings.Contains(domain[1], "gmail.com"):
		cfg.Host = "smtp.gmail.com"
	case len(domain) > 1 && (strings.Contains(domain[1], "outlook.com") || strings.Contains(domain[1], "hotmail.com")):
		cfg.Host = "smtp.live.com"
	case len(domain) > 1 && strings.Contains(domain[1], "yahoo.com"):
		cfg.Host = "smtp.mail.yahoo.com"
	case len(domain) > 1 && strings.Contains(domain[1], "aol.com"):
		cfg.Host = "smtp.aol.com"
	case len(domain) > 1 && strings.Contains(domain[1], "gmx.com"):
		cfg.Host = "smtp.gmx.com"
	default:
		cfg.Host = ""
	}

	switch cfg.Host {
	case "smtp.gmail.com", "smtp.live.com":
		cfg.Port = 587
		cfg.SSL = false
	case "":
		cfg.Port = 587
	default:
		cfg.Port = ImplicitTLSPort
		cfg.SSL = true
	}
}
