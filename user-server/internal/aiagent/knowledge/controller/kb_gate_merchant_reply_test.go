// kb_gate_merchant_reply_test.go T-P9-02 写闸门在商家侧"就地改语料"入口上的对外形状。
//
// replyChunkWriteError 是改 / 删 / 拆三个入口共用的错误出口，它只为本卡新增的那一条判据
// 开一个 409 分支。两码在这里不是粗细之别而是**下一步动作之别**：400 是"请求体写错了，
// 改完再点"，409 是"这个库已进发布制，这条路不提供，请改提一条变更走审批"。
// 把 409 一起报成 400，运营会反复改正文再点保存，而改多少次结果都一样。
package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/kbrelease"
)

func TestKM_ChunkWriteErrorMapsGovernedTo409(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name     string
		err      error
		wantCode int
		wantFrag string
	}{
		{
			name:     "哨兵本身 ⇒ 409",
			err:      kbrelease.ErrGovernedDirectWrite,
			wantCode: http.StatusConflict,
			wantFrag: "发布制",
		},
		{
			name:     "仓储包装过一层仍认得 ⇒ 409",
			err:      fmt.Errorf("%w（chunk=7 product=p-1）", kbrelease.ErrGovernedDirectWrite),
			wantCode: http.StatusConflict,
			wantFrag: "变更流程",
		},
		{
			name:     "其余错误维持既有 400 口径",
			err:      errors.New("正文不能为空"),
			wantCode: http.StatusBadRequest,
			wantFrag: "正文不能为空",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			// response.Error 里的 localeOf 会读 c.GetHeader("Accept-Language")，
			// 而 gin.CreateTestContext 造出的 ctx 不带 Request —— 不给它就是 nil 解引用 panic。
			c.Request = httptest.NewRequest(http.MethodPut, "/api/knowledge-merchant/chunks/7", nil)
			replyChunkWriteError(c, tc.err)
			if w.Code != tc.wantCode {
				t.Errorf("%s 应回 %d，实得 %d（响应体：%s）", tc.name, tc.wantCode, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantFrag) {
				t.Errorf("响应体缺少 %q：%s", tc.wantFrag, w.Body.String())
			}
			// 409 必须把错误原文带到响应体：运营照着这句话去提变更，
			// 吞掉正文的 409 只剩"操作失败"，等于没告知下一步动作。
			if !strings.Contains(w.Body.String(), tc.err.Error()) {
				t.Errorf("响应体没回显错误原文 %q：%s", tc.err.Error(), w.Body.String())
			}
		})
	}
}
