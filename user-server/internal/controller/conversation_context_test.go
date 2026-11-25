package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func TestConversationContext_EmployeeAccountScope(t *testing.T) {
	cases := []struct {
		name        string
		grant       string
		actor       string
		target      string
		contextMode string
		forged      bool
		want        int
	}{
		{name: "新授权允许本账号", grant: "phase2-a", target: "a", want: http.StatusOK},
		{name: "同租户其他账号拒绝", grant: "phase2-a", target: "b", want: http.StatusForbidden},
		{name: "空授权拒绝", target: "a", want: http.StatusForbidden},
		{name: "跨租户账号拒绝", grant: "foreign", target: "foreign", want: http.StatusForbidden},
		{name: "管理员可读本租户", actor: "admin", target: "b", want: http.StatusOK},
		{name: "全量作用域可读本租户", actor: "all", target: "b", want: http.StatusOK},
		{name: "管理员不能跨租户", actor: "admin", target: "foreign", want: http.StatusForbidden},
		{name: "查询不能伪造员工角色租户", grant: "phase2-a", target: "b", forged: true, want: http.StatusForbidden},
		{name: "缺登录身份拒绝", actor: "missing", target: "a", want: http.StatusUnauthorized},
		{name: "负数员工身份拒绝", actor: "negative", target: "a", want: http.StatusUnauthorized},
		{name: "未知员工拒绝", actor: "unknown", target: "a", want: http.StatusForbidden},
		{name: "停用员工拒绝", actor: "disabled", grant: "disabled", target: "a", want: http.StatusForbidden},
		{name: "缺租户拒绝", actor: "admin", target: "a", contextMode: "missing", want: http.StatusForbidden},
		{name: "系统上下文不能冒充员工", actor: "admin", target: "a", contextMode: "system", want: http.StatusForbidden},
		{name: "旧授权数值账号允许", grant: "legacy-id", target: "a", want: http.StatusOK},
		{name: "旧授权真实账号名称兼容", grant: "legacy-name", target: "a", want: http.StatusOK},
		{name: "旧授权未指定渠道的唯一名称兼容", grant: "legacy-name-no-channel", target: "a", want: http.StatusOK},
		{name: "旧授权同平台重名账号拒绝", grant: "legacy-ambiguous-name", target: "a", want: http.StatusForbidden},
		{name: "旧授权未指定渠道的跨平台重名拒绝", grant: "legacy-wildcard-name", target: "a", want: http.StatusForbidden},
		{name: "旧授权指定渠道消歧账号名", grant: "legacy-exact-channel", target: "a", want: http.StatusOK},
		{name: "旧授权非法范围拒绝", grant: "legacy-invalid-scope", target: "a", want: http.StatusForbidden},
		{name: "新授权非法角色拒绝", grant: "phase2-invalid-role", target: "a", want: http.StatusForbidden},
		{name: "旧授权渠道不匹配拒绝", grant: "legacy-wrong-channel", target: "a", want: http.StatusForbidden},
		{name: "识别码不是员工授权", grant: "legacy-code", target: "a", want: http.StatusForbidden},
		{name: "旧授权已撤销拒绝", grant: "legacy-deleted", target: "a", want: http.StatusForbidden},
		{name: "旧新授权并集保留旧授权", grant: "union", target: "b", want: http.StatusOK},
		{name: "旧新授权并集保留新授权", grant: "union", target: "a", want: http.StatusOK},
		{name: "读取时重验撤权", grant: "revoked", target: "a", want: http.StatusForbidden},
		{name: "授权存储故障闭合", grant: "store-error", target: "a", want: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldMode := tenant.CurrentMode()
			tenant.SetMode(tenant.ModeOn)
			t.Cleanup(func() { tenant.SetMode(oldMode) })
			database := testutil.NewTestDB(t, &model.SystemUser{}, &model.BusinessAccountAuthorization{},
				&model.ChannelAccountAuthorization{}, &model.Channel{}, &model.ChannelAccount{},
				&model.ChannelIdentifyCode{}, &model.MessageHub{}, &model.ConversationContext{})
			if err := database.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_conv_ctx_tenant_account_conv ON conversation_contexts (tenant_id, channel_account_id, external_conversation_id)").Error; err != nil {
				t.Fatal(err)
			}
			ctx := tenant.WithTenantID(context.Background(), 8501)
			create := func(value any, tid uint) {
				t.Helper()
				if err := database.WithContext(tenant.WithTenantID(context.Background(), tid)).Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			users := map[string]*model.SystemUser{}
			for _, name := range []string{"staff", "admin", "all", "disabled"} {
				user := &model.SystemUser{TenantID: 8501, Username: "context-" + name, Password: "fixture-only", Role: model.SystemUserRoleUser, DataScope: model.DataScopeSelf}
				if name == "admin" {
					user.Role = model.SystemUserRoleAdmin
				}
				if name == "all" {
					user.DataScope = model.DataScopeAll
				}
				create(user, 8501)
				users[name] = user
			}
			if err := database.WithContext(ctx).Model(users["disabled"]).Update("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
			accounts := map[string]*model.ChannelAccount{}
			for _, name := range []string{"a", "b", "foreign"} {
				tid := uint(8501)
				if name == "foreign" {
					tid = 8502
				}
				channel := &model.Channel{TenantID: tid, Code: "telegram-" + name, Type: "telegram", Name: "TG " + name}
				create(channel, tid)
				account := &model.ChannelAccount{TenantID: tid, ChannelID: channel.ID, Name: "account-" + name, IdentifyCode: "cid_context_" + name, DataScope: "{}", Status: 1}
				create(account, tid)
				accounts[name] = account
			}
			svc := service.NewConversationContextService()
			for name, account := range accounts {
				accountCtx := tenant.WithTenantID(context.Background(), account.TenantID)
				scope := service.ConversationContextScope{Platform: "telegram"}
				secret := "private-" + name
				for _, err := range []error{
					svc.SetSummary(accountCtx, account.ID, "same-conversation", secret+"-summary", scope),
					svc.AppendMessage(accountCtx, account.ID, "same-conversation", model.ConversationMessage{MessageID: "m-1", Content: secret + "-message"}, scope),
					svc.AddTag(accountCtx, account.ID, "same-conversation", secret+"-tag", scope),
					svc.SetMemberRole(accountCtx, account.ID, "same-conversation", secret+"-member", "demand", scope),
					svc.AddNote(accountCtx, account.ID, "same-conversation", service.ContextNoteNamespaceAutoReply, secret+"-reply-note", scope),
					svc.AddNote(accountCtx, account.ID, "same-conversation", service.ContextNoteNamespaceLeadMining, secret+"-mining-note", scope),
				} {
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			phase2 := func(account *model.ChannelAccount, userID uint) {
				t.Helper()
				create(&model.ChannelAccountAuthorization{TenantID: 8501, UserID: userID, ChannelAccountID: account.ID, Role: "viewer", VisibleIndustryScope: "[]"}, 8501)
			}
			legacy := func(channel, ref string, deleted bool) {
				t.Helper()
				grant := &model.BusinessAccountAuthorization{TenantID: 8501, UserID: users["staff"].ID, Channel: channel, AccountID: ref, Scope: "read"}
				if deleted {
					now := time.Now()
					grant.DeletedAt = &now
				}
				create(grant, 8501)
			}
			switch tc.grant {
			case "phase2-a", "union":
				phase2(accounts["a"], users["staff"].ID)
				if tc.grant == "union" {
					legacy("telegram", strconv.FormatUint(uint64(accounts["b"].ID), 10), false)
				}
			case "foreign":
				phase2(accounts["foreign"], users["staff"].ID)
			case "disabled":
				phase2(accounts["a"], users["disabled"].ID)
			case "legacy-id", "legacy-wrong-channel", "legacy-deleted":
				channel := "telegram"
				if tc.grant == "legacy-wrong-channel" {
					channel = "feishu"
				}
				legacy(channel, strconv.FormatUint(uint64(accounts["a"].ID), 10), tc.grant == "legacy-deleted")
			case "legacy-name", "legacy-name-no-channel":
				channel := "telegram"
				if tc.grant == "legacy-name-no-channel" {
					channel = ""
				}
				legacy(channel, accounts["a"].Name, false)
			case "legacy-ambiguous-name", "legacy-exact-channel", "legacy-wildcard-name":
				if err := database.WithContext(ctx).Model(accounts["b"]).Update("name", accounts["a"].Name).Error; err != nil {
					t.Fatal(err)
				}
				channel := "telegram"
				if tc.grant == "legacy-exact-channel" {
					channel = "telegram-a"
				}
				if tc.grant == "legacy-wildcard-name" {
					channel = ""
					if err := database.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", accounts["b"].ChannelID).Update("type", "feishu").Error; err != nil {
						t.Fatal(err)
					}
				}
				legacy(channel, accounts["a"].Name, false)
			case "legacy-invalid-scope":
				legacy("telegram", strconv.FormatUint(uint64(accounts["a"].ID), 10), false)
				if err := database.WithContext(ctx).Model(&model.BusinessAccountAuthorization{}).Where("user_id = ?", users["staff"].ID).Update("scope", "invalid").Error; err != nil {
					t.Fatal(err)
				}
			case "phase2-invalid-role":
				phase2(accounts["a"], users["staff"].ID)
				if err := database.WithContext(ctx).Model(&model.ChannelAccountAuthorization{}).Where("user_id = ?", users["staff"].ID).Update("role", "invalid").Error; err != nil {
					t.Fatal(err)
				}
			case "legacy-code":
				legacy("telegram", accounts["a"].IdentifyCode, false)
			case "revoked":
				phase2(accounts["a"], users["staff"].ID)
				if _, err := svc.SnapshotForUser(ctx, users["staff"].ID, accounts["a"].ID, "same-conversation", service.ConversationContextScope{Platform: "telegram"}); err != nil {
					t.Fatalf("撤权前应允许读取：%v", err)
				}
				if err := database.WithContext(ctx).Where("user_id = ?", users["staff"].ID).Delete(&model.ChannelAccountAuthorization{}).Error; err != nil {
					t.Fatal(err)
				}
			case "store-error":
				phase2(accounts["a"], users["staff"].ID)
				callback := "test:conversation-permission-error"
				if err := database.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "channel_account_authorizations" {
						tx.AddError(errors.New("模拟授权存储不可用"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = database.Callback().Query().Remove(callback) })
			}
			contextReads := 0
			readCallback := "test:conversation-context-read"
			if err := database.Callback().Query().Before("gorm:query").Register(readCallback, func(tx *gorm.DB) {
				if tx.Statement.Table == "conversation_contexts" {
					contextReads++
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Callback().Query().Remove(readCallback) })
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				switch tc.actor {
				case "missing":
				case "negative":
					c.Set("user_id", -1)
				case "unknown":
					c.Set("user_id", uint(999999))
				default:
					name := tc.actor
					if name == "" {
						name = "staff"
					}
					c.Set("user_id", users[name].ID)
				}
				c.Next()
			})
			engine.GET("/conversation-context", NewConversationContextController(svc).Get)
			url := fmt.Sprintf("/conversation-context?channel_account_id=%d&conversation_id=same-conversation&platform=telegram&is_group=false", accounts[tc.target].ID)
			if tc.forged {
				url += fmt.Sprintf("&user_id=%d&role=admin&tenant_id=8502", users["admin"].ID)
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			switch tc.contextMode {
			case "missing":
			case "system":
				req = req.WithContext(tenant.WithSystemScope(ctx))
			default:
				req = req.WithContext(ctx)
			}
			out := httptest.NewRecorder()
			engine.ServeHTTP(out, req)
			if out.Code != tc.want {
				t.Fatalf("HTTP状态=%d，期望%d；响应=%s", out.Code, tc.want, out.Body.String())
			}
			var body struct {
				Code json.RawMessage `json:"code"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.want != http.StatusOK {
				if contextReads != 0 {
					t.Fatalf("未授权请求不得先读取上下文：读取次数=%d", contextReads)
				}
				if string(body.Code) == "0" || (len(body.Data) > 0 && string(body.Data) != "null") || strings.Contains(out.Body.String(), "private-") {
					t.Fatalf("拒绝响应不得成功或带私有上下文：%s", out.Body.String())
				}
				return
			}
			if string(body.Code) != "0" {
				t.Fatalf("合法读取应成功：%s", out.Body.String())
			}
			if contextReads == 0 {
				t.Fatal("合法请求必须实际读取共享上下文")
			}
			for _, field := range []string{"summary", "message", "tag", "member", "reply-note", "mining-note"} {
				if !strings.Contains(string(body.Data), "private-"+tc.target+"-"+field) {
					t.Fatalf("合法读取缺少%s：%s", field, out.Body.String())
				}
			}
		})
	}
}
