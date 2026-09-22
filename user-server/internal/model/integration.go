package model

import (
	"strings"
	"time"
)

// IntegrationAccount 第三方对接账号
type IntegrationAccount struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform     string     `gorm:"type:varchar(50);index;not null" json:"platform"`
	AccountName  string     `gorm:"type:varchar(100)" json:"account_name"`
	APIKey       string     `gorm:"type:varchar(200)" json:"api_key"`
	APISecret    string     `gorm:"type:varchar(200)" json:"api_secret"`
	RefreshToken string     `gorm:"type:text" json:"refresh_token"`
	AccessToken  string     `gorm:"type:text" json:"access_token"`
	TokenExpires *time.Time `json:"token_expires"`
	WebhookURL   string     `gorm:"type:varchar(500)" json:"webhook_url"`
	Config       string     `gorm:"type:text" json:"config"`
	Status       int        `gorm:"default:1" json:"status"`
	LastSyncAt   *time.Time `json:"last_sync_at"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (IntegrationAccount) TableName() string {
	return "integration_accounts"
}

// SyncLog 同步日志
type SyncLog struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform     string     `gorm:"type:varchar(50);index" json:"platform"`
	SyncType     string     `gorm:"type:varchar(50)" json:"sync_type"`
	Status       int        `gorm:"default:0" json:"status"`
	RecordCount  int        `gorm:"default:0" json:"record_count"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (SyncLog) TableName() string {
	return "sync_logs"
}

// ExternalCustomer 外部客户（CRM 对接）
type ExternalCustomer struct {
	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform      string     `gorm:"type:varchar(50);index:idx_extcust_platform_external,priority:1" json:"platform"`
	ExternalID    string     `gorm:"type:varchar(100);index:idx_extcust_platform_external,priority:2" json:"external_id"`
	Name          string     `gorm:"type:varchar(100)" json:"name"`
	Phone         string     `gorm:"type:varchar(50);index" json:"phone"`
	Email         string     `gorm:"type:varchar(100)" json:"email"`
	Company       string     `gorm:"type:varchar(200)" json:"company"`
	Position      string     `gorm:"type:varchar(100)" json:"position"`
	Industry      string     `gorm:"type:varchar(100)" json:"industry"`
	Level         string     `gorm:"type:varchar(50)" json:"level"`
	Source        string     `gorm:"type:varchar(100)" json:"source"`
	OwnerID       string     `gorm:"type:varchar(100)" json:"owner_id"`
	OwnerName     string     `gorm:"type:varchar(100)" json:"owner_name"`
	Status        string     `gorm:"type:varchar(50)" json:"status"`
	Tags          string     `gorm:"type:text" json:"tags"`
	LastContactAt *time.Time `json:"last_contact_at"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ExternalCustomer) TableName() string {
	return "external_customers"
}

// ExternalOrderScopedUniqueIndex 是 (platform, order_id) 那把复合唯一索引的名字。
//
// 导出的理由与 billQuoteRowConstraint 同一条判据的反面：这一把索引名要同时被
// 三处读到 —— 模型标签、启动时删旧索引的 postMigrate、以及真库形状用例。
// 三处各写一遍字面量，删旧索引那一步就会去删一个不存在的名字而**静默成功**
// （DROP INDEX IF EXISTS 对拼错的名字也是"执行成功"），旧约束继续拦着跨平台订单号。
const ExternalOrderScopedUniqueIndex = "uq_external_orders_platform_order_id"

// ExternalOrderLegacyOrderIDIndex 是**改键之前**那把单列唯一索引的名字（G15 的成因）。
//
// AutoMigrate 只加不删：把标签改成复合键之后，旧索引仍留在库里并继续生效，
// 所以启动时必须显式 DROP 一次。它由 GORM 的默认命名规则生成（uni_<表>_<列>），
// 这里把字面值钉成常量而不是再算一遍：命名规则一改，旧索引就删不掉了，
// 而"删不掉"的形状是"改键改了个寂寞"。
const ExternalOrderLegacyOrderIDIndex = "uni_external_orders_order_id"

// ExternalOrder 外部订单（电商对接）
//
// 唯一键是 (platform, order_id) 而**不是** order_id：订单号由平台自己编，
// 两家平台撞号是常态而不是意外。T-P2-02 用真库探针实测过坏的形状 ——
// B 平台沿用 A 平台的订单号 ⇒ duplicate key、表里只 1 行、接口回 500，
// 也就是一条**签名完全合法**的回调被静默丢掉（G15 第①条）。
type ExternalOrder struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// priority:1 显式写：复合键哪一列打头决定的是"按平台捞列表"能不能用上这把索引。
	Platform string `gorm:"type:varchar(50);uniqueIndex:uq_external_orders_platform_order_id,priority:1;index" json:"platform"`
	// OrderID 上**不再带** unique（见上面那段），只保留 not null 与复合键的第二列。
	OrderID string `gorm:"type:varchar(100);uniqueIndex:uq_external_orders_platform_order_id,priority:2;not null" json:"order_id"`
	OrderNo string `gorm:"type:varchar(100);index" json:"order_no"`
	UserID  string `gorm:"type:varchar(100);index" json:"user_id"`

	// BillID 这张订单替哪张应收付的钱（抄回调载荷里的 bill_id，可空 = 与账单无关）。
	//
	// 可空是主形态而不是偷懒：绝大多数外部订单与我们的报价链无关（客户自己下的单），
	// 只有载荷里带了账单号的那些才有值。**不建唯一**：分期与拆单都是一单对一账/一账对多单。
	//
	// 这一格是 G15 第③条的落点：改之前"钱到了"与"这是哪张应收"之间没有任何一行数据连着，
	// external_orders 又不带报价引用（见 model/bill.go 那段），于是回款域无从对齐。
	// 它与 payments.bill_id 的分工：本列是**镜像上的线索**（哪条订单付过账），
	// payments 才是**资金凭据**（哪笔钱算进结清）。结清判据只读后者，见 service/payment.go。
	BillID    string `gorm:"type:varchar(64);index" json:"bill_id"`
	UserName  string `gorm:"type:varchar(100)" json:"user_name"`
	UserPhone string `gorm:"type:varchar(50)" json:"user_phone"`

	// TotalAmount / PayAmount / DiscountAmount 的单位是**元取整**（bigint 列，历史形状）。
	//
	// 镜像侧的钱从来不是财务精度：列型是 bigint，改它要在存量表上 ALTER 列型，
	// 而这一张表上没有任何一处按它做对账（对账读 bills/payments）。
	// 本卡修的是**读法**：载荷里的 369.99 从前被 int64() 截成 369（系统性少记），
	// 现在按四舍五入落 370，且金额文本的解析集中到 webhookMoney（见 service 层）。
	TotalAmount    int64 `gorm:"type:bigint;default:0" json:"total_amount"`
	PayAmount      int64 `gorm:"type:bigint;default:0" json:"pay_amount"`
	DiscountAmount int64 `gorm:"type:bigint;default:0" json:"discount_amount"`

	// Status 平台自述的订单状态：值域由平台决定，本系统不规范化存储值（镜像就该是镜像），
	// 但**乱序回退**由 ExternalOrderStatusRegresses 拦（G15 第②条），见那个函数。
	Status string `gorm:"type:varchar(50)" json:"status"`

	OrderTime    *time.Time `json:"order_time"`
	PayTime      *time.Time `json:"pay_time"`
	ShipTime     *time.Time `json:"ship_time"`
	CompleteTime *time.Time `json:"complete_time"`
	Items        string     `gorm:"type:text" json:"items"`
	ShippingAddr string     `gorm:"type:text" json:"shipping_addr"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// externalOrderStatusRank 是订单状态"走到哪一步了"的先后刻度，只用于判回退。
//
// 三点口径，逐条对应一种坏法：
//   - **不认识的字面值不在表里**，且两侧任一不认识时一律按"不是回退"放行。
//     状态词由平台给（淘宝 TRADE_CLOSED、微信 SUCCESS…，大小写与用词各家不同），
//     把不认识的值冻成"不许覆盖"的后果是镜像永远停在第一条收到的状态上 ——
//     那比偶尔被回退一次更坏。本判据只在**两侧都认识**时开火。
//   - 刻度是"钱与货的先后"这一条线，不是平台状态机的全图：退款可以在发货之后，
//     也可以在不发货时直接退，所以 cancelled/refunded/closed 一律排在最前段之后。
//   - unknown = 0 且**认识**：载荷缺 status 时控制器兜成它（既有行为），
//     而它绝不该把一次真实的已付擦成"不知道"。
var externalOrderStatusRank = map[string]int{
	"unknown":   0,
	"created":   10,
	"paid":      20,
	"shipped":   30,
	"completed": 40,
	"cancelled": 50,
	"canceled":  50, // 两种拼法都收：这是平台的字符串，不是我们的枚举
	"closed":    50,
	"refunded":  60,
}

// NormalizeExternalOrderStatus 把平台状态词归一到判据用的形态：去前后空白 + 小写。
//
// 只用于比较，**不写回库里**：external_orders.status 存的仍是平台原样给的那个词
// （镜像的可读性优先于列值整齐，且历史行里已经是混杂的）。
func NormalizeExternalOrderStatus(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ExternalOrderStatusRegresses 报告"库里是 from、新推送是 to"算不算倒退。
//
// 两侧任一不认识 ⇒ false（放行，理由见刻度表）。两侧都认识且 to 的刻度**严格小于** from ⇒ true。
// 相等（同一条状态被重投）算放行：那是幂等重放，交给 upsert 自己吃掉，
// 在这里拦下来会让"更新别的字段"这条正常路径也一起停掉。
func ExternalOrderStatusRegresses(from, to string) bool {
	f, ok := externalOrderStatusRank[NormalizeExternalOrderStatus(from)]
	if !ok {
		return false
	}
	t, ok := externalOrderStatusRank[NormalizeExternalOrderStatus(to)]
	if !ok {
		return false
	}
	return t < f
}

// TableName 指定表名
func (ExternalOrder) TableName() string {
	return "external_orders"
}

// ExternalProduct 外部商品（电商对接）
type ExternalProduct struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform      string    `gorm:"type:varchar(50);index" json:"platform"`
	ProductID     string    `gorm:"type:varchar(100);index" json:"product_id"`
	Name          string    `gorm:"type:varchar(200)" json:"name"`
	CategoryID    string    `gorm:"type:varchar(100)" json:"category_id"`
	CategoryName  string    `gorm:"type:varchar(100)" json:"category_name"`
	Price         int64     `gorm:"type:bigint;default:0" json:"price"`
	OriginalPrice int64     `gorm:"type:bigint;default:0" json:"original_price"`
	Stock         int       `gorm:"default:0" json:"stock"`
	Sales         int       `gorm:"default:0" json:"sales"`
	Images        string    `gorm:"type:text" json:"images"`
	Status        int       `gorm:"default:1" json:"status"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ExternalProduct) TableName() string {
	return "external_products"
}

// WebhookEvent Webhook 事件
type WebhookEvent struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform    string     `gorm:"type:varchar(50);index" json:"platform"`
	EventID     string     `gorm:"type:varchar(100);unique" json:"event_id"`
	EventType   string     `gorm:"type:varchar(50)" json:"event_type"`
	AccountID   string     `gorm:"type:varchar(100);default:''" json:"account_id"`
	RawData     string     `gorm:"type:text" json:"raw_data"`
	Processed   bool       `gorm:"default:false;index:idx_webhook_events_processed_created,priority:1" json:"processed"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index:idx_webhook_events_processed_created,priority:2" json:"created_at"`
}

// TableName 指定表名
func (WebhookEvent) TableName() string {
	return "webhook_events"
}
