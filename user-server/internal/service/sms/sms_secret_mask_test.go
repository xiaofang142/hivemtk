package sms

// 契约：短信通道密钥**读出去是掩码，写回来时掩码被当成"没改"**。
//
// 坑：读侧（controller/sms.go 的 maskSMSConfig）把 AccessKeyId / AccessKeySecret
// 脱敏后再给前端，而前端配置页把**整份 GET 结果直接塞进表单**、保存时原样提交掩码串
//（user-web/src/views/sms/Config.vue：`smsConfig.value = response`，
//  表单项 `v-model="smsConfig.aliyun.accessKeyId"`）。
// 写侧不认掩码 ⇒ 用户打开配置页点一次「保存」，真凭证就被覆盖成 "****ALUE"，
// 之后所有短信发送都回 "Specified access key is not found."，
// 而页面显示"保存成功"，没有任何异常提示 —— 通道就此废掉。
//
// 本文件锁三条腿：
//  1. 掩码串进来 ⇒ 库里原值保留（缺陷本体）；
//  2. 真值进来 ⇒ 仍然能覆盖（脱敏不能把"改密钥"这个功能焊死）；
//  3. 三家通道（aliyun/tencent/huawei）都要认掩码 —— 漏一家就漏一条通道。

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// seedSmsSecrets 写一条带真凭证的通道配置。
func seedSmsSecrets(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&model.SmsAliyunConfig{
		AccessKeyID: "REAL_AK_VALUE", AccessKeySecret: "REAL_SECRET_VALUE", SignName: "签名",
	}).Error; err != nil {
		t.Fatalf("阿里云通道配置落库失败：%v", err)
	}
	if err := db.Create(&model.SmsTencentConfig{
		SecretID: "REAL_TENCENT_ID", SecretKey: "REAL_TENCENT_KEY", AppID: "app1", SignName: "签名",
	}).Error; err != nil {
		t.Fatalf("腾讯通道配置落库失败：%v", err)
	}
	if err := db.Create(&model.SmsHuaweiConfig{
		AppKey: "REAL_HUAWEI_KEY", AppSecret: "REAL_HUAWEI_SECRET", Sender: "sender", Signature: "签名",
	}).Error; err != nil {
		t.Fatalf("华为通道配置落库失败：%v", err)
	}
}

// TestSmsSaveConfigMaskedSecretsKeepOriginal 掩码串进来 ⇒ 三家通道的真凭证都不动。
func TestSmsSaveConfigMaskedSecretsKeepOriginal(t *testing.T) {
	db := setupSmsServiceTestDB(t)
	svc := NewSmsService(newTestSmsRepository(db))
	seedSmsSecrets(t, db)

	// 模拟前端：把 GET 回来的掩码串原样提交。
	err := svc.SaveConfig(context.Background(), &dto.SmsConfigRequest{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      1000,
		RetryTimes:      1,
		Aliyun: dto.SmsAliyunConfig{
			AccessKeyId:     SmsSecretMask + "ALUE",
			AccessKeySecret: SmsSecretMask + "ALUE",
			SignName:        "签名",
		},
		Tencent: dto.SmsTencentConfig{
			SecretId:  SmsSecretMask + "_ID",
			SecretKey: SmsSecretMask + "_KEY",
			AppId:     "app1",
			SignName:  "签名",
		},
		Huawei: dto.SmsHuaweiConfig{
			AppKey:    SmsSecretMask + "_KEY",
			AppSecret: SmsSecretMask + "CRET",
			Sender:    "sender",
			Signature: "签名",
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig 失败：%v", err)
	}

	type chk struct {
		name string
		got  string
		want string
	}
	var aliyun model.SmsAliyunConfig
	var tencent model.SmsTencentConfig
	var huawei model.SmsHuaweiConfig
	if err := db.First(&aliyun).Error; err != nil {
		t.Fatalf("读回阿里云配置失败：%v", err)
	}
	if err := db.First(&tencent).Error; err != nil {
		t.Fatalf("读回腾讯配置失败：%v", err)
	}
	if err := db.First(&huawei).Error; err != nil {
		t.Fatalf("读回华为配置失败：%v", err)
	}
	for _, c := range []chk{
		{"aliyun.access_key_id", aliyun.AccessKeyID, "REAL_AK_VALUE"},
		{"aliyun.access_key_secret", aliyun.AccessKeySecret, "REAL_SECRET_VALUE"},
		{"tencent.secret_id", tencent.SecretID, "REAL_TENCENT_ID"},
		{"tencent.secret_key", tencent.SecretKey, "REAL_TENCENT_KEY"},
		{"huawei.app_key", huawei.AppKey, "REAL_HUAWEI_KEY"},
		{"huawei.app_secret", huawei.AppSecret, "REAL_HUAWEI_SECRET"},
	} {
		if c.got != c.want {
			t.Errorf("%s 期望保持 %q，实际 %q —— 掩码串覆盖了真凭证，通道就此废掉", c.name, c.want, c.got)
		}
	}
}

// TestSmsSaveConfigRealSecretStillOverwrites 真值进来仍然要能覆盖（脱敏不能把功能焊死）。
func TestSmsSaveConfigRealSecretStillOverwrites(t *testing.T) {
	db := setupSmsServiceTestDB(t)
	svc := NewSmsService(newTestSmsRepository(db))
	seedSmsSecrets(t, db)

	err := svc.SaveConfig(context.Background(), &dto.SmsConfigRequest{
		DefaultProvider: "aliyun",
		RateLimit:       100,
		DailyLimit:      1000,
		RetryTimes:      1,
		Aliyun: dto.SmsAliyunConfig{
			AccessKeyId:     "BRAND_NEW_AK",
			AccessKeySecret: "BRAND_NEW_SECRET",
			SignName:        "签名",
		},
	})
	if err != nil {
		t.Fatalf("SaveConfig 失败：%v", err)
	}

	var aliyun model.SmsAliyunConfig
	if err := db.First(&aliyun).Error; err != nil {
		t.Fatalf("读回阿里云配置失败：%v", err)
	}
	if aliyun.AccessKeyID != "BRAND_NEW_AK" {
		t.Errorf("真值必须能覆盖旧密钥：期望 %q，实际 %q", "BRAND_NEW_AK", aliyun.AccessKeyID)
	}
	if aliyun.AccessKeySecret != "BRAND_NEW_SECRET" {
		t.Errorf("真值必须能覆盖旧密钥：期望 %q，实际 %q", "BRAND_NEW_SECRET", aliyun.AccessKeySecret)
	}
}

// TestIsMaskedSmsSecret 掩码判据本身：按前缀认，不按全等。
func TestIsMaskedSmsSecret(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"****ALUE", true},
		{"****", true},
		{"****CRET", true},
		{"", false},
		{"REAL_AK_VALUE", false},
		{"AK_****", false}, // 掩码不在开头 ⇒ 不是掩码
	}
	for _, c := range cases {
		if got := isMaskedSmsSecret(c.in); got != c.want {
			t.Errorf("isMaskedSmsSecret(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
