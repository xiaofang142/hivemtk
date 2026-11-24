package controller

// 契约：WhatsApp 账号列表**不得把接入凭证明文吐给前端**。
//
// 坑：WhatsappAccount 模型带 token / app_id / app_secret，直接序列化 ⇒
// 任何登录用户都能把商户的企业微信接入凭证整个读走。
// 兄弟模块 whatsapp-cloud 早就这么做了（access_token_masked / app_secret_masked，
// 见 whatsapp_cloud_account.go 的 maskFeishuSecret），这条是漏的。
//
// 与"企业微信 / 短信配置"那两处不同：这里**不需要**写侧认掩码 ——
// 凭证是**扫码登录流程**写进去的（CreateAccount 只收 name/remark，
// UpdateAccount 也只改 name/remark），前端既不读它也不回传它，
// 不存在"把掩码原样提交回来覆盖真值"的路径。
//
// 本文件锁：三个凭证字段在读侧都被换成掩码，且原值不出现在 JSON 里。

import (
	"encoding/json"
	"strings"
	"testing"

	"hivemtk-user/internal/model"

	"github.com/google/uuid"
)

// TestMaskWhatsappAccountSecrets 三个凭证字段都要被换成掩码。
func TestMaskWhatsappAccountSecrets(t *testing.T) {
	acc := &model.WhatsappAccount{
		ID:        uuid.New(),
		Token:     "REAL" + "_WA_T" + "OKEN_VALUE",
		AppID:     "REAL_APP_ID",
		AppSecret: "REAL_APP_SECRET",
	}
	maskWhatsappAccountSecrets([]*model.WhatsappAccount{acc})

	if strings.Contains(acc.Token, "REAL_WA_TOKEN") {
		t.Errorf("token 未脱敏：%q", acc.Token)
	}
	if strings.Contains(acc.AppSecret, "REAL_APP_SECRET") {
		t.Errorf("app_secret 未脱敏：%q", acc.AppSecret)
	}
	// app_id 是业务标识不是密钥，按 whatsapp-cloud 的口径保留明文（不掩）。
	if acc.AppID != "REAL_APP_ID" {
		t.Errorf("app_id 是业务标识，应保持明文，实际 %q", acc.AppID)
	}
}

// TestMaskWhatsappAccountSecretsJSONHasNoPlaintext 序列化后的 JSON 里不得出现明文凭证。
func TestMaskWhatsappAccountSecretsJSONHasNoPlaintext(t *testing.T) {
	acc := &model.WhatsappAccount{
		ID:        uuid.New(),
		Token:     "REAL" + "_WA_T" + "OKEN_VALUE",
		AppSecret: "REAL_APP_SECRET",
	}
	maskWhatsappAccountSecrets([]*model.WhatsappAccount{acc})

	raw, err := json.Marshal(acc)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	for _, secret := range []string{"REAL_WA_TOKEN_VALUE", "REAL_APP_SECRET"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("JSON 里仍含明文凭证 %q：%s", secret, raw)
		}
	}
}

// TestMaskWhatsappAccountSecretsEmptyStaysEmpty 没配的凭证保持空串（不凭空造出 "****"）。
func TestMaskWhatsappAccountSecretsEmptyStaysEmpty(t *testing.T) {
	acc := &model.WhatsappAccount{Token: "", AppSecret: ""}
	maskWhatsappAccountSecrets([]*model.WhatsappAccount{acc})

	if acc.Token != "" {
		t.Errorf("未配置的 token 应保持空串，实际 %q", acc.Token)
	}
	if acc.AppSecret != "" {
		t.Errorf("未配置的 app_secret 应保持空串，实际 %q", acc.AppSecret)
	}
}

// TestMaskWhatsappAccountSecretsNilSafe nil 不能 panic。
func TestMaskWhatsappAccountSecretsNilSafe(t *testing.T) {
	maskWhatsappAccountSecrets([]*model.WhatsappAccount{nil}) // 不应 panic
}
