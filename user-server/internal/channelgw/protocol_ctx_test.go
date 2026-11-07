package channelgw

import (
	"strings"
	"testing"
)

// TestIngestCtxKeys 渠道上下文键约定（借鉴 ChatGPT-On-CS CTX_* 模型）：
// ① 键必须带 ctx. 前缀（与扩展端 extra 命名空间隔离，防撞键）；
// ② 值必须全小写 snake_case（直接进 message_hub.extra / LLM variables）；
// ③ 关键电商键齐全（商品/订单/物流/人群标签）。
// 证据源：docs/research/CHATGPT-ON-CS-IMPLEMENTATION-DETAILS-2026-11.md §4.1/§7。
func TestIngestCtxKeys(t *testing.T) {
	keys := []string{
		IngestCtxGoods,
		IngestCtxGoodsID,
		IngestCtxGoodsSpec,
		IngestCtxOrderID,
		IngestCtxOrderStatus,
		IngestCtxOrderAmount,
		IngestCtxLogistics,
		IngestCtxMemberTag,
		IngestCtxFanTag,
		IngestCtxNewCustomerTag,
		IngestCtxSenderName,
		// CH-37b 身份/会话扩展键
		IngestCtxAssignedStatus,
		IngestCtxShopID,
		IngestCtxRegion,
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if !strings.HasPrefix(k, "ctx.") {
			t.Errorf("ctx 键 %q 缺少 ctx. 前缀", k)
		}
		if seen[k] {
			t.Errorf("ctx 键 %q 重复", k)
		}
		seen[k] = true
		body := strings.TrimPrefix(k, "ctx.")
		if body == "" || strings.ContainsAny(body, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			t.Errorf("ctx 键 %q 主体 %q 应为非空 snake_case", k, body)
		}
	}

	// 电商核心键不得缺失（缺了等于 AI 拿不到订单上下文）
	for _, must := range []string{IngestCtxGoods, IngestCtxGoodsID, IngestCtxOrderID, IngestCtxOrderStatus} {
		if !seen[must] {
			t.Errorf("缺少电商核心上下文键 %q", must)
		}
	}
	// CH-37b 身份/会话扩展键不得缺失（店铺归属/地域/接单状态）
	for _, must := range []string{IngestCtxAssignedStatus, IngestCtxShopID, IngestCtxRegion} {
		if !seen[must] {
			t.Errorf("缺少身份/会话扩展上下文键 %q", must)
		}
	}
}
