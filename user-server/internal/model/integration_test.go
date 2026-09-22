package model

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntegrationAccount_TableName(t *testing.T) {
	account := &IntegrationAccount{}
	tableName := account.TableName()
	if tableName != "integration_accounts" {
		t.Errorf("Expected table name 'integration_accounts', got %s", tableName)
	}
}

func TestIntegrationAccount_BasicFields(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)

	account := &IntegrationAccount{
		ID:           1,
		Platform:     "crm_xiaoshouyi",
		AccountName:  "Test Account",
		APIKey:       "test_api_key",
		APISecret:    "test_api_secret",
		RefreshToken: "refresh_token_123",
		AccessToken:  "access_token_123",
		TokenExpires: &expiresAt,
		WebhookURL:   "https://example.com/webhook",
		Config:       `{"region": "cn-north-1"}`,
		Status:       1,
		LastSyncAt:   &now,
	}

	if account.ID != 1 {
		t.Errorf("Expected ID 1, got %d", account.ID)
	}
	if account.Platform != "crm_xiaoshouyi" {
		t.Errorf("Expected Platform 'crm_xiaoshouyi', got %s", account.Platform)
	}
	if account.AccountName != "Test Account" {
		t.Errorf("Expected AccountName 'Test Account', got %s", account.AccountName)
	}
	if account.Status != 1 {
		t.Errorf("Expected Status 1, got %d", account.Status)
	}
}

func TestIntegrationAccount_PlatformValues(t *testing.T) {
	platforms := []string{"crm_xiaoshouyi", "crm_fenxiangxiao", "ecommerce_taobao", "ecommerce_jd"}

	for _, platform := range platforms {
		account := &IntegrationAccount{
			Platform: platform,
		}
		if account.Platform != platform {
			t.Errorf("Expected Platform %s, got %s", platform, account.Platform)
		}
	}
}

func TestSyncLog_TableName(t *testing.T) {
	log := &SyncLog{}
	tableName := log.TableName()
	if tableName != "sync_logs" {
		t.Errorf("Expected table name 'sync_logs', got %s", tableName)
	}
}

func TestSyncLog_BasicFields(t *testing.T) {
	now := time.Now()
	endTime := now.Add(time.Minute)

	log := &SyncLog{
		ID:           1,
		Platform:     "crm_xiaoshouyi",
		SyncType:     "customer",
		Status:       1,
		RecordCount:  100,
		ErrorMessage: "",
		StartTime:    now,
		EndTime:      &endTime,
	}

	if log.ID != 1 {
		t.Errorf("Expected ID 1, got %d", log.ID)
	}
	if log.SyncType != "customer" {
		t.Errorf("Expected SyncType 'customer', got %s", log.SyncType)
	}
	if log.RecordCount != 100 {
		t.Errorf("Expected RecordCount 100, got %d", log.RecordCount)
	}
}

func TestSyncLog_StatusValues(t *testing.T) {
	statuses := map[int]string{
		0: "进行中",
		1: "成功",
		2: "失败",
	}

	for status, desc := range statuses {
		log := &SyncLog{
			Status: status,
		}
		if log.Status != status {
			t.Errorf("Expected Status %d (%s), got %d", status, desc, log.Status)
		}
	}
}

func TestExternalCustomer_TableName(t *testing.T) {
	customer := &ExternalCustomer{}
	tableName := customer.TableName()
	if tableName != "external_customers" {
		t.Errorf("Expected table name 'external_customers', got %s", tableName)
	}
}

func TestExternalCustomer_BasicFields(t *testing.T) {
	now := time.Now()

	customer := &ExternalCustomer{
		ID:            1,
		Platform:      "crm_xiaoshouyi",
		ExternalID:    "ext-001",
		Name:          "John Doe",
		Phone:         "13800138000",
		Email:         "john@example.com",
		Company:       "Test Company",
		Position:      "Manager",
		Industry:      "Technology",
		Level:         "VIP",
		Source:        "Website",
		OwnerID:       "owner-001",
		OwnerName:     "Sales Rep",
		Status:        "potential",
		Tags:          `["vip", "hot"]`,
		LastContactAt: &now,
	}

	if customer.ID != 1 {
		t.Errorf("Expected ID 1, got %d", customer.ID)
	}
	if customer.Name != "John Doe" {
		t.Errorf("Expected Name 'John Doe', got %s", customer.Name)
	}
	if customer.Phone != "13800138000" {
		t.Errorf("Expected Phone '13800138000', got %s", customer.Phone)
	}
	if customer.Level != "VIP" {
		t.Errorf("Expected Level 'VIP', got %s", customer.Level)
	}
}

func TestExternalOrder_TableName(t *testing.T) {
	order := &ExternalOrder{}
	tableName := order.TableName()
	if tableName != "external_orders" {
		t.Errorf("Expected table name 'external_orders', got %s", tableName)
	}
}

func TestExternalOrder_BasicFields(t *testing.T) {
	now := time.Now()

	order := &ExternalOrder{
		ID:             1,
		Platform:       "ecommerce_taobao",
		OrderID:        "tb-123456",
		OrderNo:        "internal-001",
		UserID:         "user-001",
		UserName:       "Test User",
		UserPhone:      "13800138000",
		TotalAmount:    19999,
		PayAmount:      17999,
		DiscountAmount: 2000,
		Status:         "paid",
		PayTime:        &now,
		Items:          `[{"id": "item1", "qty": 2}]`,
		ShippingAddr:   "Beijing, China",
	}

	if order.ID != 1 {
		t.Errorf("Expected ID 1, got %d", order.ID)
	}
	if order.OrderID != "tb-123456" {
		t.Errorf("Expected OrderID 'tb-123456', got %s", order.OrderID)
	}
	if order.TotalAmount != 19999 {
		t.Errorf("Expected TotalAmount 19999, got %d", order.TotalAmount)
	}
	if order.PayAmount != 17999 {
		t.Errorf("Expected PayAmount 17999, got %d", order.PayAmount)
	}
}

func TestExternalProduct_TableName(t *testing.T) {
	product := &ExternalProduct{}
	tableName := product.TableName()
	if tableName != "external_products" {
		t.Errorf("Expected table name 'external_products', got %s", tableName)
	}
}

func TestExternalProduct_BasicFields(t *testing.T) {
	product := &ExternalProduct{
		ID:            1,
		Platform:      "ecommerce_taobao",
		ProductID:     "prod-001",
		Name:          "Test Product",
		CategoryID:    "cat-001",
		CategoryName:  "Electronics",
		Price:         9999,
		OriginalPrice: 12999,
		Stock:         100,
		Sales:         500,
		Images:        `["img1.jpg", "img2.jpg"]`,
		Status:        1,
	}

	if product.ID != 1 {
		t.Errorf("Expected ID 1, got %d", product.ID)
	}
	if product.Name != "Test Product" {
		t.Errorf("Expected Name 'Test Product', got %s", product.Name)
	}
	if product.Price != 9999 {
		t.Errorf("Expected Price 9999, got %d", product.Price)
	}
	if product.Stock != 100 {
		t.Errorf("Expected Stock 100, got %d", product.Stock)
	}
}

func TestWebhookEvent_TableName(t *testing.T) {
	event := &WebhookEvent{}
	tableName := event.TableName()
	if tableName != "webhook_events" {
		t.Errorf("Expected table name 'webhook_events', got %s", tableName)
	}
}

func TestWebhookEvent_BasicFields(t *testing.T) {
	now := time.Now()
	processedAt := now.Add(time.Second)

	event := &WebhookEvent{
		ID:          1,
		Platform:    "crm_xiaoshouyi",
		EventID:     "evt-001",
		EventType:   "customer.created",
		RawData:     `{"event": "customer.created"}`,
		Processed:   true,
		ProcessedAt: &processedAt,
	}

	if event.ID != 1 {
		t.Errorf("Expected ID 1, got %d", event.ID)
	}
	if event.EventID != "evt-001" {
		t.Errorf("Expected EventID 'evt-001', got %s", event.EventID)
	}
	if event.EventType != "customer.created" {
		t.Errorf("Expected EventType 'customer.created', got %s", event.EventType)
	}
	if !event.Processed {
		t.Error("Expected Processed to be true")
	}
}

// —— T-P7-02 / G15：外部订单镜像的三处形状修正 ————————————————
//
// 这一族判据拦的是 T-P2-02 取证出来、登记为 G15 结转本卡的三条实测缺陷
// （三条都是"一次签名完全合法的回调会静默丢单/写坏数据"的形状）：
//   ① order_id 全局唯一 ⇒ B 平台沿用 A 平台的订单号时，第二条插不进去、接口回 500；
//   ② 状态无条件覆盖 ⇒ 后到的 created 能把已落库的 paid 抹回去；
//   ③ 镜像不带账单引用 ⇒ 钱到了也追不回"冲的是哪张应收"。

func TestExternalOrderIsScopedToPlatform(t *testing.T) {
	st := reflect.TypeOf(ExternalOrder{})

	// 复合唯一键：(platform, order_id)。命名 + 两列都挂同一个索引名 + priority 定序，
	// 缺任何一样都组不成复合键（匿名 uniqueIndex 会被 GORM 建成两个单列索引）。
	platformTag := gormTagOf(t, st, "Platform")
	orderTag := gormTagOf(t, st, "OrderID")
	if got := compositeUniqueName(platformTag, 1); got != ExternalOrderScopedUniqueIndex {
		t.Errorf("platform 应挂在 %s 的 priority:1 上，实际标签 %q", ExternalOrderScopedUniqueIndex, platformTag)
	}
	if got := compositeUniqueName(orderTag, 2); got != ExternalOrderScopedUniqueIndex {
		t.Errorf("order_id 应挂在 %s 的 priority:2 上，实际标签 %q", ExternalOrderScopedUniqueIndex, orderTag)
	}
	// 单列 unique 必须**摘掉**：留着它，复合索引只是多出来的一条装饰，
	// 跨平台复用订单号照样撞（而且撞在哪个索引上取决于 PG 先检查哪一个）。
	// 判据按**标签分段**取词，不用 Contains("uniqueIndex:")：复合键那一格本身就含这个前缀。
	for _, part := range strings.Split(orderTag, ";") {
		if part == "unique" || part == "uniqueIndex" {
			t.Errorf("order_id 上仍带单列唯一标签 %q ⇒ 跨平台复用订单号会静默丢单：%s", part, orderTag)
		}
	}
	// 两列都得留普通可读性：按平台捞订单列表的既有路径不能因为改索引而退化
	if !strings.Contains(platformTag, "index") {
		t.Errorf("platform 缺索引（GetByPlatform 是既有读路径）：%s", platformTag)
	}
}

func TestExternalOrderCarriesBillLink(t *testing.T) {
	st := reflect.TypeOf(ExternalOrder{})
	f, ok := st.FieldByName("BillID")
	if !ok {
		t.Fatal("ExternalOrder 没有 BillID：回调带来的钱与账单之间没有任何一行数据连着（G15 的第③条）")
	}
	tag := string(f.Tag.Get("gorm"))
	if !strings.Contains(tag, "varchar(64)") {
		t.Errorf("bill_id 应为 varchar(64)（与 bills.id 生成上界同宽）：%s", tag)
	}
	if !strings.Contains(tag, "index") {
		t.Errorf("bill_id 缺索引：对账要按「这张应收被哪几笔订单付的」捞镜像：%s", tag)
	}
	if strings.Contains(tag, "uniqueIndex") || strings.Contains(tag, "unique") {
		t.Errorf("bill_id 不许唯一：一笔付一张单可以，多笔付同一张单与拆单也是真实场景：%s", tag)
	}
	if f.Type.Kind() != reflect.String {
		t.Errorf("BillID 应为非指针字符串（空串=这张订单与账单无关），实际 %v", f.Type.Kind())
	}
	if got := strings.Split(f.Tag.Get("json"), ",")[0]; got != "bill_id" {
		t.Errorf("json 键 %q ≠ 列名 bill_id", got)
	}
}

func TestExternalOrderStatusRegressionIsDirectional(t *testing.T) {
	// from, to, 是不是"退步"（退步 ⇒ 镜像保留原状态）
	cases := []struct {
		from, to string
		blocked  bool
	}{
		{"paid", "created", true}, // G15 实测的那一条：乱序的 created 抹掉已付
		{"paid", "unknown", true}, // 缺字段的推送不该把真实状态擦成 unknown
		{"shipped", "paid", true},
		{"completed", "shipped", true},
		{"created", "paid", false},                // 正常前进
		{"paid", "refunded", false},               // 退款在钱之后
		{"paid", "cancelled", false},              // 取消在钱之后
		{"unknown", "created", false},             // 首条占位被真实状态替掉
		{"created", "unknown", true},              // unknown 永远不该覆盖已知词
		{"PAID", "created", true},                 // 平台词大小写不一，判据要按同一口径归一
		{" paid", "created", true},                // 同上：前后空白
		{"weird_platform_word", "created", false}, // 两侧都不认识 ⇒ 放行（见函数注释）
		{"paid", "weird_platform_word", false},    // 未知新词不许被冻住
		{"", "created", false},                    // 库里那格是空的 ⇒ 任何词都算前进
		{"paid", "", false},                       // 载荷给空串由控制器先兜成 unknown，这里按放行
	}
	for _, tc := range cases {
		if got := ExternalOrderStatusRegresses(tc.from, tc.to); got != tc.blocked {
			t.Errorf("ExternalOrderStatusRegresses(%q, %q) = %v，期望 %v", tc.from, tc.to, got, tc.blocked)
		}
	}
}

// compositeUniqueName 从 gorm 标签里取出「挂在指定 priority 上的复合唯一索引名」。
// priority 必须显式写：不写时 GORM 按结构体字段序定序，而字段序日后一动，
// 复合键的前缀就换了列（列集合相同而 (platform) 与 (order_id) 谁打头差别在索引能不能用）。
func compositeUniqueName(tag string, priority int) string {
	want := "uniqueIndex:" + ExternalOrderScopedUniqueIndex
	for _, part := range strings.Split(tag, ";") {
		if !strings.HasPrefix(part, want) {
			continue
		}
		for _, opt := range strings.Split(strings.TrimPrefix(part, want+","), ",") {
			if opt == "priority:"+strconv.Itoa(priority) {
				return ExternalOrderScopedUniqueIndex
			}
		}
	}
	return ""
}
