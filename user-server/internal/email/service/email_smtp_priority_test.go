package email

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/crypto"
	"hivemtk-user/internal/repository"
)

// TestResolveSmtpFromDB_PicksFirstUsable 表里第一条 Server/Username 非空的行应被选中，
// 且密码经 crypto.Decrypt 解回明文（落库是加密的）。
func TestResolveSmtpFromDB_PicksFirstUsable(t *testing.T) {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	setupEmailSendServiceTestDB(t)
	repo := repository.NewEmailSmtpRepository()
	enc, err := crypto.Encrypt("decrypted-pw")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "db-smtp", Server: "smtp.db.example", Port: 465,
		Username: "db@example.com", Password: enc, Limit: 100,
	}); err != nil {
		t.Fatalf("建 SMTP 失败: %v", err)
	}

	svc := NewEmailSendService()
	got := svc.resolveSmtpFromDB(context.Background())
	if got == nil {
		t.Fatal("resolveSmtpFromDB 应返回表里的账号，实际 nil")
	}
	if got.Server != "smtp.db.example" || got.Username != "db@example.com" {
		t.Errorf("选中账号 = %+v, 期望 smtp.db.example / db@example.com", got)
	}
	if got.Password != "decrypted-pw" {
		t.Errorf("密码 = %q, 期望解密后的明文 decrypted-pw", got.Password)
	}
}

// TestResolveSmtpFromDB_EmptyTableReturnsNil 表空时返回 nil（让调用方回落报错，
// 与接线前「无 env 即报错」一致）。
func TestResolveSmtpFromDB_EmptyTableReturnsNil(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	svc := NewEmailSendService()
	if got := svc.resolveSmtpFromDB(context.Background()); got != nil {
		t.Errorf("空表 resolveSmtpFromDB 应返回 nil, 实际 %+v", got)
	}
}

// TestResolveSmtpFromDB_SkipsIncomplete 行的 Server 或 Username 为空时跳过，选后面那条完整的。
func TestResolveSmtpFromDB_SkipsIncomplete(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	repo := repository.NewEmailSmtpRepository()
	// FA-EMAIL-03 起解密失败=行不可用，测试种子必须放可解密的密文。
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	enc, encErr := crypto.Encrypt("pw")
	if encErr != nil {
		t.Fatalf("加密失败: %v", encErr)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "bad", Server: "", Username: "", Password: "x", Limit: 10,
	}); err != nil {
		t.Fatalf("建坏行失败: %v", err)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "good", Server: "smtp.good.example", Username: "good@example.com",
		Password: enc, Limit: 10,
	}); err != nil {
		t.Fatalf("建好行失败: %v", err)
	}
	svc := NewEmailSendService()
	got := svc.resolveSmtpFromDB(context.Background())
	if got == nil || got.Server != "smtp.good.example" {
		t.Errorf("应跳过不完整行并选中 good, 实际 %+v", got)
	}
}

// ── F3：备用通道（163/QQ）DB 化 ──────────────────────────────────────────────

// TestResolveSmtpFromDB_NamedBackupChannelFirst 表里有 Name="env:163" 的行时，
// 它优先于任意首条可用行被选中（与 env 链 163 → QQ 同序），且密码解回明文。
func TestResolveSmtpFromDB_NamedBackupChannelFirst(t *testing.T) {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	setupEmailSendServiceTestDB(t)
	repo := repository.NewEmailSmtpRepository()
	// 先插一条「普通」行：没有命名通道时它会被选中；有了 env:163 就该让位。
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "plain", Server: "smtp.plain.example", Port: 465,
		Username: "plain@example.com", Password: "plainpw", Limit: 100,
	}); err != nil {
		t.Fatalf("建普通行失败: %v", err)
	}
	enc, err := crypto.Encrypt("backup-163-pw")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "env:163", Server: "smtp.163.com", Port: 465,
		Username: "backup163@163.com", Password: enc, Limit: 100,
	}); err != nil {
		t.Fatalf("建 env:163 行失败: %v", err)
	}

	svc := NewEmailSendService()
	got := svc.resolveSmtpFromDB(context.Background())
	if got == nil {
		t.Fatal("resolveSmtpFromDB 应选中 env:163 行，实际 nil")
	}
	if got.Name != "env:163" || got.Server != "smtp.163.com" {
		t.Errorf("选中行 = %s/%s, 期望 env:163/smtp.163.com（命名备用通道应压过首条可用行）", got.Name, got.Server)
	}
	if got.Password != "backup-163-pw" {
		t.Errorf("密码 = %q, 期望解密后的明文 backup-163-pw", got.Password)
	}
}

// TestResolveSmtpFromDB_163BeatsQQ 同时有 env:163 与 env:qq 行时按 env 链同序选 163。
func TestResolveSmtpFromDB_163BeatsQQ(t *testing.T) {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	setupEmailSendServiceTestDB(t)
	repo := repository.NewEmailSmtpRepository()
	encQQ, encErr := crypto.Encrypt("qqpw")
	if encErr != nil {
		t.Fatalf("加密失败: %v", encErr)
	}
	enc163, encErr2 := crypto.Encrypt("163pw")
	if encErr2 != nil {
		t.Fatalf("加密失败: %v", encErr2)
	}
	// qq 先插入（物理顺序在前），163 后插入：选取顺序不应跟着插入顺序走。
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "env:qq", Server: "smtp.qq.com", Port: 465,
		Username: "backup@qq.com", Password: encQQ, Limit: 100,
	}); err != nil {
		t.Fatalf("建 env:qq 行失败: %v", err)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "env:163", Server: "smtp.163.com", Port: 465,
		Username: "backup163@163.com", Password: enc163, Limit: 100,
	}); err != nil {
		t.Fatalf("建 env:163 行失败: %v", err)
	}

	svc := NewEmailSendService()
	got := svc.resolveSmtpFromDB(context.Background())
	if got == nil || got.Name != "env:163" {
		t.Errorf("应选 env:163（与 env 链同序），实际 %+v", got)
	}
}

// TestResolveSmtpFromDB_NamedRowIncompleteFallsThrough env:163 行缺 Username 时
// 跳过它，继续找 qq 命名行——单条坏数据不应把备用通道整条打死。
func TestResolveSmtpFromDB_NamedRowIncompleteFallsThrough(t *testing.T) {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-field-encryption-key-0123456789abcdef")
	setupEmailSendServiceTestDB(t)
	repo := repository.NewEmailSmtpRepository()
	encQQ, encErr := crypto.Encrypt("qqpw")
	if encErr != nil {
		t.Fatalf("加密失败: %v", encErr)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "env:163", Server: "smtp.163.com", Port: 465,
		Username: "", Password: "pw", Limit: 100,
	}); err != nil {
		t.Fatalf("建残缺 env:163 行失败: %v", err)
	}
	if err := repo.Create(context.Background(), &model.EmailSmtp{
		Name: "env:qq", Server: "smtp.qq.com", Port: 465,
		Username: "backup@qq.com", Password: encQQ, Limit: 100,
	}); err != nil {
		t.Fatalf("建 env:qq 行失败: %v", err)
	}

	svc := NewEmailSendService()
	got := svc.resolveSmtpFromDB(context.Background())
	if got == nil || got.Name != "env:qq" {
		t.Errorf("残缺 163 行应跳过并选 env:qq，实际 %+v", got)
	}
}
