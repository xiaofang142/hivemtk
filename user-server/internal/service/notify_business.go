// notify_business.go 业务通知生产者（I8）：给通知中心补三类运营类生产者 ——
// 新订单草稿待确认、新坏例待归因、知识库变更待审核。
//
// 与待办 SLA（I2）的关系：两路共用 NotificationService 与 notifications 表，
// 但**装配彼此独立** —— SLA 那台挂在三态旗 FF_LTC_HANDOFF_SLA_JOB 上（off 时
// 根本不构造），这一口不受任何旗子控制：底座拿到 DB 句柄就装配，三类生产者
// 即刻生效。理由是两件事的风险不同：SLA 会按节拍反复外发（必须有档位），
// 这三类是"新实体首建"的一次性事件，铃铛响不响不影响任何业务正确性。
//
// 去重口径也不同：SLA 的节拍器每轮扫到同一批逾期行，靠 HasRecentByTypeLink
// 挡重复；这三类的链接指向**刚创建的那一个实体**（草稿 ID/坏例 ID/变更 ID），
// 天然唯一 —— 实体不重复建，通知就不会重复发，所以这里一次查询都不做。
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 三类生产者的通知 type。与 human_task_overdue 同一口径：自由字符串、不是
// model 里那五个枚举的子集 —— 通知中心按 type 过滤时认的就是这些值，
// 新增一类就在这里登记，不要散在各调用点拼字面量。
const (
	// OrderDraftCreatedNotifyType 新订单草稿待确认。
	OrderDraftCreatedNotifyType = "order_draft_created"
	// BadCasePendingNotifyType 新坏例待归因。
	BadCasePendingNotifyType = "bad_case_pending"
	// KBChangePendingNotifyType 知识库变更待审核。
	KBChangePendingNotifyType = "kb_change_pending"
)

// BusinessNotifier 生产者需要的最小通知口（*NotificationService 天然满足）。
//
// 抽成接口只为测试：本包的单测没有 DB，注入一个记录仪/失败桩就能断言
// "建了草稿就该发出一条什么通知"，不用为铃铛接一条真库。
type BusinessNotifier interface {
	Create(ctx context.Context, n *model.Notification) error
}

// businessNotifierHolder atomic.Value 的载荷壳：atomic.Value 不收裸 nil
// 接口，把接口装进结构体，nil 槽就变成 holder{n: nil} 一个正常可存的值。
type businessNotifierHolder struct {
	n BusinessNotifier
}

var globalBusinessNotifier atomic.Value // businessNotifierHolder

// SetGlobalNotifier 登记全局通知口；传 nil 等于撤掉（生产者全部 no-op 并出声）。
// 与 SetGlobalBadCaseService 同一形状：装配层写一次，请求期只读。
func SetGlobalNotifier(n BusinessNotifier) {
	globalBusinessNotifier.Store(businessNotifierHolder{n: n})
}

// GlobalNotifier 取全局通知口（未装配为 nil，调用方必须判空）。
func GlobalNotifier() BusinessNotifier {
	v, ok := globalBusinessNotifier.Load().(businessNotifierHolder)
	if !ok {
		return nil
	}
	return v.n
}

// NotifyBusiness 写一条业务通知：槽缺席或写失败都只出声、不回错 ——
// 通知是业务动作成功之后的旁路，绝不因为铃铛没响就把已成功的动作读成失败。
// 返回是否真写了。
func NotifyBusiness(ctx context.Context, n *model.Notification) bool {
	if n == nil {
		return false
	}
	ntf := GlobalNotifier()
	if ntf == nil {
		logger.Warnf("[business-notify] 通知口未装配 ⇒ %q 未发出（type=%s link=%s）",
			n.Title, n.Type, n.Link)
		return false
	}
	if err := ntf.Create(ctx, n); err != nil {
		logger.Errorf("[business-notify] 写通知失败（type=%s link=%s title=%q）：%v",
			n.Type, n.Link, n.Title, err)
		return false
	}
	return true
}

// notifyUserID 把业务侧字符串 owner 解析成通知的 uint 用户 id。
// 解析不出（空串 / 非数字）一律回 0 = 全体可见 —— 与待办 SLA 的
// humanTaskAssigneeUserID 同一条口径：一条提醒发错人比没人看到更坏。
func notifyUserID(raw string) uint {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}

// OrderDraftCreatedNotification 新订单草稿待确认：发给草稿 owner（解析不出回 0 广播）。
//
// Link 直指这条草稿的详情页（/dashboard/drafts/:id，与工作台聚合待办生成的深链同一条，
// 由 I10 落点）—— 点铃铛即落到确认/取消/改价三动作所在的页面，不用回列表自己找。
// 正文仍带全草稿 ID 与金额：通知列表不跳转的人不点开也知道发生了什么。
func OrderDraftCreatedNotification(d *OrderDraft) *model.Notification {
	if d == nil {
		return nil
	}
	return &model.Notification{
		UserID: notifyUserID(d.OwnerID),
		Type:   OrderDraftCreatedNotifyType,
		Title:  "新订单草稿待确认",
		Content: fmt.Sprintf("订单草稿 %s：%s ×%d，金额 %.2f，状态待确认。",
			d.ID, d.ProductName, d.Quantity, d.TotalAmount),
		Link: "/dashboard/drafts/" + d.ID,
	}
}

// BadCasePendingNotification 新坏例待归因：广播（UserID=0）。
//
// 受众是做归因的运营/管理员，不是报坏例的坐席 —— 人报的那一条他自己知道，
// 要催的是"还没人来 Label 它"，所以走全体可见而不是发给 labeler。
func BadCasePendingNotification(row *model.BadCase) *model.Notification {
	if row == nil {
		return nil
	}
	return &model.Notification{
		UserID:  0,
		Type:    BadCasePendingNotifyType,
		Title:   "新增待归因坏例",
		Content: fmt.Sprintf("坏例 %s 已入库待归因（来源 %s，会话 %s）。", row.ID, row.Source, row.SessionID),
		Link:    "/badCase/list",
	}
}

// KBChangePendingNotification 知识库变更待审核：广播（UserID=0）。
// 受众是审批中心（/approvalTask/list）里的审核人；变更行发布前不进
// knowledge_chunks，这条铃铛就是"有一单压在队列里"的唯一即时提醒。
func KBChangePendingNotification(ch *model.KBChangeRequest) *model.Notification {
	if ch == nil {
		return nil
	}
	return &model.Notification{
		UserID: 0,
		Type:   KBChangePendingNotifyType,
		Title:  "知识库变更待审核",
		Content: fmt.Sprintf("变更 %s（op=%s，产品 %s，申请人 %s）已提交，等待审核。",
			ch.ID, ch.Op, ch.ProductID, ch.RequestedBy),
		Link: "/approvalTask/list",
	}
}
