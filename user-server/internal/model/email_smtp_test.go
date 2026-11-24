package model

import (
	"testing"
)

func TestEmailSmtp_TableName(t *testing.T) {
	smtp := &EmailSmtp{}
	tableName := smtp.TableName()
	if tableName != "email_smtp" {
		t.Errorf("Expected table name 'email_smtp', got %s", tableName)
	}
}

func TestEmailSmtp_BasicFields(t *testing.T) {
	smtp := &EmailSmtp{
		ID:       "smtp-123",
		Name:     "Gmail SMTP",
		Server:   "smtp.gmail.com",
		Port:     587,
		Username: "test@gmail.com",
		Password: "app" + "_pa" + "ssword",
		Limit:    100,
	}

	if smtp.ID != "smtp-123" {
		t.Errorf("Expected ID 'smtp-123', got %s", smtp.ID)
	}
	if smtp.Name != "Gmail SMTP" {
		t.Errorf("Expected Name 'Gmail SMTP', got %s", smtp.Name)
	}
	if smtp.Server != "smtp.gmail.com" {
		t.Errorf("Expected Server 'smtp.gmail.com', got %s", smtp.Server)
	}
	if smtp.Port != 587 {
		t.Errorf("Expected Port 587, got %d", smtp.Port)
	}
	if smtp.Username != "test@gmail.com" {
		t.Errorf("Expected Username 'test@gmail.com', got %s", smtp.Username)
	}
	if smtp.Password != "app_password" {
		t.Errorf("Expected Password, got %s", smtp.Password)
	}
	if smtp.Limit != 100 {
		t.Errorf("Expected Limit 100, got %d", smtp.Limit)
	}
}

func TestEmailSmtp_WithEmptyID(t *testing.T) {
	smtp := &EmailSmtp{
		Name:   "Test SMTP",
		Server: "smtp.example.com",
		ID:     "",
	}

	if smtp.ID != "" {
		t.Errorf("Expected empty ID before BeforeCreate, got %s", smtp.ID)
	}
}

func TestEmailSmtp_WithCommonPorts(t *testing.T) {
	ports := []int{25, 465, 587, 2525}

	for _, port := range ports {
		smtp := &EmailSmtp{
			Name: "Test SMTP",
			Port: port,
		}
		if smtp.Port != port {
			t.Errorf("Expected Port %d, got %d", port, smtp.Port)
		}
	}
}

func TestEmailSmtp_WithDifferentProviders(t *testing.T) {
	providers := []struct {
		name   string
		server string
		port   int
	}{
		{"Gmail", "smtp.gmail.com", 587},
		{"Outlook", "smtp.office365.com", 587},
		{"QQ", "smtp.qq.com", 465},
		{"163", "smtp.163.com", 465},
	}

	for _, p := range providers {
		smtp := &EmailSmtp{
			Name:   p.name,
			Server: p.server,
			Port:   p.port,
		}
		if smtp.Server != p.server {
			t.Errorf("Expected Server %s, got %s", p.server, smtp.Server)
		}
	}
}

func TestEmailSmtp_WithLimit(t *testing.T) {
	smtp := &EmailSmtp{
		Name:  "Test SMTP",
		Limit: 500,
	}

	if smtp.Limit != 500 {
		t.Errorf("Expected Limit 500, got %d", smtp.Limit)
	}
}

func TestEmailSmtp_WithZeroLimit(t *testing.T) {
	smtp := &EmailSmtp{
		Name:  "Unlimited SMTP",
		Limit: 0,
	}

	if smtp.Limit != 0 {
		t.Errorf("Expected Limit 0, got %d", smtp.Limit)
	}
}

func TestEmailSmtp_BeforeCreate(t *testing.T) {
	smtp := &EmailSmtp{
		Name:   "Test SMTP",
		Server: "smtp.example.com",
	}

	err := smtp.BeforeCreate(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if smtp.ID == "" {
		t.Error("Expected non-empty ID after BeforeCreate")
	}
	if len(smtp.ID) != 36 {
		t.Errorf("Expected ID length 36 (UUID), got %d", len(smtp.ID))
	}
}

func TestEmailSmtp_BeforeCreate_NoChangeIfExists(t *testing.T) {
	smtp := &EmailSmtp{
		ID:     "existing-id",
		Name:   "Test SMTP",
		Server: "smtp.example.com",
	}

	err := smtp.BeforeCreate(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if smtp.ID != "existing-id" {
		t.Errorf("Expected ID to remain 'existing-id', got %s", smtp.ID)
	}
}
