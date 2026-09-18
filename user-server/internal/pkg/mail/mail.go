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

func SendMail(cfg Config, to []string, subject, body string, isHTML bool) error {
	if cfg.Host == "" || cfg.Port == 0 {
		autoConfig(&cfg)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", cfg.From)
	m.SetHeader("To", to...)
	m.SetHeader("Subject", subject)

	contentType := "text/plain"
	if isHTML {
		contentType = "text/html"
	}
	m.SetBody(contentType, body)

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
		cfg.Port = 465
		cfg.SSL = true
	}
}
