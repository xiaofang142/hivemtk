// knowledge_base_owner_validation_test.go 把 POST 与 PUT /api/knowledge-bases 的
// owner_type 入参校验钉在 **HTTP 层**：校验本身在 service 里早有测（
// test/integration/knowledge_base_crud_test.go 断的是 err != nil），但"service 返了错"
// 与"调用方看到 4xx"之间还隔着 response.ErrorFromDB 的分档 —— 它按 errors.Is 与消息子串判，
// 裸 errors.New 落不进名单就是 500。2026-09-22 在开发实例上实测这一格返
// 500 INTERNAL_ERROR_6002（消息 "owner_type=shared 时 owner_agent_id 必为空"），
// 而它是纯粹的入参错 ⇒ 缺陷在 HTTP 契约上，只有 HTTP 层的用例拦得住。
package controller

import (
	"fmt"
	"net/http"
	"testing"
)

func TestKBCreate_OwnerTypeValidationIsClientError(t *testing.T) {
	router, _, _ := newKBHandlerRouter(t)

	cases := []struct {
		name string
		body string
	}{
		{"shared 却带 owner_agent_id", `{"kb_code":"kb-owner-400-shared","type":"faq","name":"x","owner_type":"shared","owner_agent_id":1}`},
		{"private 却缺 owner_agent_id", `{"kb_code":"kb-owner-400-private","type":"faq","name":"x","owner_type":"private"}`},
		{"owner_type 取了枚举外的值", `{"kb_code":"kb-owner-400-bogus","type":"faq","name":"x","owner_type":"public"}`},
	}
	for _, c := range cases {
		code, envCode, _ := kbHandlerCall(t, router, http.MethodPost, "/api/knowledge-bases", c.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s: 应为 400，实际 %d（body.code=%v）—— 500 意味着错误没包 utils.ErrInvalidInput，"+
				"而 ErrorFromDB 的子串名单里没有这条消息", c.name, code, envCode)
			continue
		}
		if got := fmt.Sprint(envCode); got != "INVALID_PARAM_1001" {
			t.Errorf("%s: 400 了但信封 code 是 %q，期望 INVALID_PARAM_1001", c.name, got)
		}
	}
}

// 控制组：同一入口的合法形态必须仍然 200，否则上一条用例的 400 可能来自"整个接口都坏了"。
func TestKBCreate_SharedWithoutOwnerStillSucceeds(t *testing.T) {
	router, _, _ := newKBHandlerRouter(t)
	code, envCode, data := kbHandlerCall(t, router, http.MethodPost, "/api/knowledge-bases",
		`{"kb_code":"kb-owner-200-control","type":"faq","name":"control","owner_type":"shared"}`)
	if code != http.StatusOK {
		t.Fatalf("合法建档应为 200，实际 %d（body.code=%v）", code, envCode)
	}
	if data["id"] == nil || data["id"] == float64(0) {
		t.Errorf("200 的响应里应有 data.id，实际 %v", data)
	}
}

// PUT 这一族是同一条缺陷的另一半：UpdateKB 里的 owner_type/owner_agent_id 组合校验
// 与 CreateKB 是**两份**代码（service/knowledge_base.go:266-273），措辞一模一样，
// 但改前没包 ErrInvalidInput ⇒ 改了 POST 不等于改了 PUT。2026-09-22 是本批补测时发现的，
// 两格在开发形态下实测都是 500。
func TestKBUpdate_OwnerTypeValidationIsClientError(t *testing.T) {
	router, _, kbID := newKBHandlerRouter(t) // 夹具给的这条是 shared 且 owner_agent_id 为空

	cases := []struct {
		name string
		body string
	}{
		// merged.OwnerType=private 而 OwnerAgentID 仍是夹具的空 ⇒ "owner_type=private 时 owner_agent_id 必填"
		{"改成 private 却不给 owner_agent_id", `{"owner_type":"private"}`},
		// 请求不动 owner_type（保持 shared），只塞 owner_agent_id ⇒ "owner_type=shared 时 owner_agent_id 必为空"
		{"shared 库上单独塞 owner_agent_id", `{"owner_agent_id":9}`},
	}
	for _, c := range cases {
		code, envCode, _ := kbHandlerCall(t, router, http.MethodPut, fmt.Sprintf("/api/knowledge-bases/%d", kbID), c.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s: 应为 400，实际 %d（body.code=%v）—— 500 意味着 UpdateKB 里这条错误没包 utils.ErrInvalidInput"+
				"（与 CreateKB 是两份代码，别以为改过一边就都改了）", c.name, code, envCode)
		}
	}
}

// 控制组：同一 PUT 入口的合法改动必须仍然 200。
func TestKBUpdate_LegalRenameStillSucceeds(t *testing.T) {
	router, _, kbID := newKBHandlerRouter(t)
	code, envCode, data := kbHandlerCall(t, router, http.MethodPut, fmt.Sprintf("/api/knowledge-bases/%d", kbID),
		`{"name":"改名后的库"}`)
	if code != http.StatusOK {
		t.Fatalf("合法改名应为 200，实际 %d（body.code=%v）", code, envCode)
	}
	if fmt.Sprint(data["id"]) != fmt.Sprint(kbID) {
		t.Errorf("200 的响应里 data.id 应是 %d，实际 %v", kbID, data)
	}
}
