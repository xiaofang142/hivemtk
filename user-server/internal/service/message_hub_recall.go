// message_hub_recall.go B2：平台撤回的**本地收口**（状态 + 幂等 + 分类错误）。
//
// 这个文件补的不是"撤回怎么调平台"——那件事在 internal/app/integration_reach_adapter.go
// 早就接通了（TG deleteMessage / 飞书 DELETE messages / 企微 recall，各有窗口）。
// 缺的是三样东西：
//
//  1. **回执**：撤回成功后 hub 行上什么都不留，运维问"这条撤过吗"只能去翻平台日志；
//  2. **幂等**：同一行点两次会**第二次出网**。平台对重复撤回的回应并不一致
//     （TG 报 message to delete not found、飞书报已撤回、企微报消息不存在），
//     把"平台那句各有各的说法"当成成功/失败来判，等于把幂等交给对方的心情；
//  3. **分类**：窗口过期和"这个渠道根本不支持主动撤回"必须能分开报给前端，
//     否则运营看到同一句"撤回失败"没法判断是"等一会儿再试"还是"这件事做不到"。
//
// 出网动作经 ReachRecaller 端口注入，不在这里 import internal/app：
// app 依赖 service（装配层在 app、被依赖方是 service），反向 import 会立刻成环。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

var (
	// ErrMessageHubRowNotFound 撤回目标行不存在（HTTP 404）。
	ErrMessageHubRowNotFound = errors.New("message hub row not found")
	// ErrMessageHubRecallInbound 入站消息不属于"可撤回"（撤回只对出站行成立：
	// 入站行是客户发来的，我们没有任何平台凭据去删它）。
	ErrMessageHubRecallInbound = errors.New("only outbound messages can be recalled")
	// ErrMessageHubRecallUnavailable 撤回端口未装配（db 为空/装配缺失）⇒ 503。
	ErrMessageHubRecallUnavailable = errors.New("reach recaller not wired")
	// ErrMessageHubRecallNotSupported 该渠道没有平台主动撤回通道（WA / 桥接渠道）⇒ 409。
	ErrMessageHubRecallNotSupported = errors.New("recall not supported for channel")
	// ErrMessageHubRecallWindowExpired 已超过平台撤回窗口 ⇒ 409（等也没用）。
	ErrMessageHubRecallWindowExpired = errors.New("recall window expired")
	// ErrMessageHubRecallFailed 出网失败（网络/平台业务码拒绝/凭据缺失）⇒ 502。
	ErrMessageHubRecallFailed = errors.New("recall failed")
)

// RecallFailureKind 撤回失败的分类，供 controller 映射 HTTP 码。
//
// 不是"多此一举的枚举"：这三类对运营的下一步动作完全不同 ——
// not_supported 是"这个渠道做不到（换人工去平台里删）"，
// window_expired 是"做不了了（窗口过了）"，
// channel_error 才是"现在不行，可能再试一次就行"。
type RecallFailureKind string

const (
	RecallKindNotSupported  RecallFailureKind = "not_supported"
	RecallKindWindowExpired RecallFailureKind = "window_expired"
	RecallKindChannelError  RecallFailureKind = "channel_error"
)

// RecallFailure 端口实现方（internal/app）交回的**分类**错误。
//
// 为什么不在 service 里 errors.Is 判 app 的哨兵错：那要求 service import app（成环）。
// 反过来让 app 用 service 的类型（app 本来就依赖 service）是单向的。
type RecallFailure struct {
	Kind RecallFailureKind
	Err  error
}

func (e *RecallFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Kind)
	}
	return string(e.Kind) + ": " + e.Err.Error()
}

func (e *RecallFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ReachRecaller 撤回端口：按 hub 行把消息从平台上撤掉。
//
// 入参是**整行**而不是 (channel, msgID)：撤回要判 platform / direction / sent_at
// （窗口）/ platform_msg_id（平台号），按 msgID 回查会多一次读、而且 msg_id 不是
// 全局唯一（唯一索引是四元组），回查可能挑到另一租户/另一会话的同名行。
type ReachRecaller interface {
	RecallHub(ctx context.Context, hub *model.MessageHub) error
}

// RecallResult 一次撤回调用的结论（既有成功幂等命中，也有失败分类）。
type RecallResult struct {
	MessageID uint   `json:"id"`
	Channel   string `json:"channel"`
	Recalled  bool   `json:"recalled"`
	// AlreadyRecalled 本次没有出网：这一行在本请求之前就已经是终态。
	AlreadyRecalled bool       `json:"already_recalled"`
	RecalledAt      *time.Time `json:"recalled_at,omitempty"`
	RecallError     string     `json:"recall_error,omitempty"`
}

// WithReachRecaller 注入撤回端口（nil = 不装配，端点回 503 而不是假装撤回成功）。
func (s *MessageHubService) WithReachRecaller(r ReachRecaller) *MessageHubService {
	if s != nil {
		s.recaller = r
	}
	return s
}

// Recall 撤回一条出站消息，并把结论落到 hub 行。
//
// 幂等判据是**数据**不是请求：recalled_at 非空即终态，直接返回已撤回，
// 不再出网。并发两次请求由 MarkRecalled 的条件更新裁决，输的那一方拿到
// RowsAffected=0，按"别人已经撤过"回成功——两边对同一行的结论一致。
//
// 失败时把原因写进 recall_error 再返回分类错误：行上的 recall_error 是
// "点过但没成，为什么"的唯一出处（平台日志里查不到我们这一侧的判定过程）。
func (s *MessageHubService) Recall(ctx context.Context, id uint) (*RecallResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrMessageHubRecallUnavailable
	}
	row, err := s.repo.GetByIDForRecall(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMessageHubRowNotFound
		}
		return nil, err
	}

	res := &RecallResult{MessageID: row.ID, Channel: row.Platform}
	if row.Direction != "outbound" {
		return res, ErrMessageHubRecallInbound
	}

	// 终态：已经撤过了。回 200 + already_recalled，不回 409 —— 对调用方来说
	// "我要的结果在"就是成功（平台 API 的重复撤回正是靠这一条兜住的）。
	if row.RecalledAt != nil {
		res.Recalled = true
		res.AlreadyRecalled = true
		res.RecalledAt = row.RecalledAt
		return res, nil
	}

	if s.recaller == nil {
		return res, ErrMessageHubRecallUnavailable
	}

	recallErr := s.recaller.RecallHub(ctx, row)
	if recallErr != nil {
		reason := recallErr.Error()
		if len(reason) > 200 {
			reason = reason[:200]
		}
		// 写失败不覆盖出网错误：真正要报给前端的是"为什么没撤掉"，
		// 而不是"连记录都写不进去"——后者只进日志（下一句）。
		if err := s.repo.MarkRecallFailed(ctx, id, reason); err != nil {
			return res, fmt.Errorf("%w（且回写 recall_error 失败: %v）", classifyRecallFailure(recallErr), err)
		}
		res.RecallError = reason
		return res, classifyRecallFailure(recallErr)
	}

	now := time.Now()
	wrote, err := s.repo.MarkRecalled(ctx, id, now)
	if err != nil {
		// 平台侧已经撤掉了，但本地没记上：如实报错（宁可让运营看到红灯，
		// 也不能回一个"成功"而把终态丢了——丢了就会有人再点一次，再出网一次）。
		return res, fmt.Errorf("%w: 平台已撤回但回写 recalled_at 失败: %v", ErrMessageHubRecallFailed, err)
	}
	res.Recalled = true
	res.RecalledAt = &now
	if !wrote {
		// 并发：另一笔请求先把 recalled_at 写掉了。本次出网动作是重复的，
		// 但结论与对方一致 —— 标记成 already_recalled，让前端知道不用再点。
		res.AlreadyRecalled = true
	}
	return res, nil
}

// classifyRecallFailure 把端口交回的分类错误翻成 service 的哨兵错
// （controller 只认本包的哨兵，不认 app 的）。
func classifyRecallFailure(err error) error {
	var f *RecallFailure
	if errors.As(err, &f) {
		switch f.Kind {
		case RecallKindNotSupported:
			return fmt.Errorf("%w%s", ErrMessageHubRecallNotSupported, reasonSuffix(f.Err, ErrMessageHubRecallNotSupported))
		case RecallKindWindowExpired:
			return fmt.Errorf("%w%s", ErrMessageHubRecallWindowExpired, reasonSuffix(f.Err, ErrMessageHubRecallWindowExpired))
		}
	}
	return fmt.Errorf("%w: %v", ErrMessageHubRecallFailed, err)
}

// reasonSuffix 给哨兵错拼"具体原因"时去掉重复的哨兵文案。
//
// 端口实现方（internal/app）的习惯写法是 `fmt.Errorf("%w（具体原因）", ErrRecallNotSupported)`，
// 而两个包的哨兵文案逐字相同（都叫 "recall not supported for channel"）。
// 直接 `%w: %v` 会把这句话印两遍：
//
//	recall not supported for channel: recall not supported for channel（桥接渠道 …）
//
// 前端拿到的就是这句复读。这里对齐前缀后只保留一次，同时仍用 %w 保住 errors.Is。
func reasonSuffix(err error, sentinel error) string {
	if err == nil || sentinel == nil {
		return ""
	}
	msg := err.Error()
	if strings.HasPrefix(msg, sentinel.Error()) {
		return strings.TrimPrefix(msg, sentinel.Error())
	}
	return ": " + msg
}
