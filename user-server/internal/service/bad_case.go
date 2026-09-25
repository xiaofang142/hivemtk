// bad_case.go G-2 Bad Case 闭环服务层（新规划任务清单 T-P8-03）。
//
// 三件事在这一层，且只在这一层：
//  1. **判"要不要记"**：自动标记的门槛（低于阈值 / 零命中）与幂等键的形态。
//     门槛不写在编排器里 —— 编排器只负责"把这一轮的事实交出来"，
//     否则阈值口径一变就要动对话主链路，而这条链路是本仓最不敢碰的那条。
//  2. **状态机**：pending → labeled → exported，旁路 dismiss。迁移表在 model 层
//     （BadCaseTransitions），这里照表判，不写散落的 if。
//  3. **导出评测集**：把库里的判定结论翻译成 T-P8-04 吃得下的行，并保证同一份样本
//     不会进两个评测集（这件事仓储用一行 SQL 挡，见 ClaimForExport）。
//
// HTTP 层只做身份、形状、状态码；仓储只做读写。校验不放仓储，
// 因为"未打标的行不许导出"是业务判断而不是 SQL 判断。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ErrBadCaseInputInvalid 入参非法（与仓储同一 sentinel：控制器只需认一个错）。
var ErrBadCaseInputInvalid = repository.ErrBadCaseInputInvalid

// ErrBadCaseNotFound 这一条不存在。
var ErrBadCaseNotFound = errors.New("bad_case: 记录不存在")

// ErrBadCaseTransition 状态不允许这个动作（重复打标、导出未打标的行、改已导出的行）。
//
// 这一句里带着"当前状态"，前端据此决定是刷新队列还是提示用户别重复提交 ——
// 曾经还打算再分一个"刚被别人抢走"的 sentinel，但那是同一个可观察结果（行已不在期望态）
// 的两种叙事，代码无法把它们区分开；开两个错会诱使调用方对其中一个做自动重试，
// 而重试的正是"已经有人判过了"这件事。
var ErrBadCaseTransition = errors.New("bad_case: 当前状态不允许该操作")

// ErrBadCaseNothingToExport 这一批没有可导出的行。
//
// 单列成 sentinel（而不是把仓储的 ErrBadCaseAlreadyClaimed 原样抛给控制器）是为了
// 一句 409 与一句 200 空集分得开：前者是"有人跟你同时点了导出"，
// 后者才是"库里确实没有已打标未导出的样本"。
var ErrBadCaseNothingToExport = errors.New("bad_case: 没有可导出的已打标样本")

// BadCaseListQuery 列表的查询条件（对仓储同类型的别名，与 HumanTaskListQuery 同一取舍）：
// 架构门禁止 controller 引用 repository，而 HTTP 层必须能构造这个查询。
// 别名而不是照抄一份结构体 —— 照抄会在两层之间留下一个需要手工同步的形状。
type BadCaseListQuery = repository.BadCaseListQuery

// BadCaseDefaultPageSize HTTP 侧未给分页时的页大小（同 human_tasks 的取舍：
// 列表页永远必须分页，一次读全表那条路只服务进程内调用）。
const BadCaseDefaultPageSize = 20

// badCaseFallbackThreshold 阈值缺位时用的档。
//
// 取编排器同款默认档而不是自己写一个数：本卡有两处会读阈值（自动标记的门槛、
// 落库那一列的 0 值兜底），写死两处就会在下次改默认档时漂成两个口径。
// 也不能取 0：阈值读不到时 "confidence < 0" 永不成立 ⇒ 整条自动标记静默停摆，
// 而它是本卡唯一的生产者。
func badCaseFallbackThreshold() float64 { return DefaultOrchestratorConfig().ConfidenceThreshold }

var badCaseIDSeq int64

func newBadCaseID() string {
	n := atomic.AddInt64(&badCaseIDSeq, 1)
	return fmt.Sprintf("bc_%d_%d", time.Now().UnixNano(), n)
}

func newBadCaseEvalSetID() string {
	n := atomic.AddInt64(&badCaseIDSeq, 1)
	return fmt.Sprintf("bce_%d_%d", time.Now().UnixNano(), n)
}

// BadCaseMarkInput 一轮回答交出来的现场。
type BadCaseMarkInput struct {
	SessionID      string
	MessageID      string
	SignalID       string
	IntentType     string
	QueryText      string
	AnswerText     string
	Confidence     float64
	Threshold      float64 // <=0 时按 badCaseFallbackThreshold() 兜底，见该函数注释
	RetrievedCount int
	MarkReason     string
}

// BadCaseService Bad Case 闭环服务。
type BadCaseService struct {
	repo repository.BadCaseRepository
}

// NewBadCaseService 构造。repo 可为 nil（Available() 回 false，所有写口回错误）。
func NewBadCaseService(repo repository.BadCaseRepository) *BadCaseService {
	return &BadCaseService{repo: repo}
}

// Available 报告底座是否可用（装配回显与控制器的 503 判据）。
func (s *BadCaseService) Available() bool { return s != nil && s.repo != nil && s.repo.Available() }

// GlobalBadCaseService / SetGlobalBadCaseService 全局登记处。
//
// 与待办同一形状：路由与编排器各自从全局取一份，避免"装配层传一份、路由又 new 一份"。
var globalBadCaseSvc atomic.Pointer[BadCaseService]

// SetGlobalBadCaseService 登记全局实例；传 nil 等于撤掉（端点回 503、编排器不挂标记器）。
func SetGlobalBadCaseService(s *BadCaseService) { globalBadCaseSvc.Store(s) }

// GlobalBadCaseService 取全局实例（可能为 nil，调用方必须判空并给出"未装配"的答复）。
func GlobalBadCaseService() *BadCaseService { return globalBadCaseSvc.Load() }

// ShouldMark 报告这一轮是否该记一条坏例，并给出来源与理由。
//
// 两条判据并列而不是一条带 else，因为它们的修法不同（前者找算法、后者找知识库）：
// 零命中的回答哪怕置信度再高也要记 —— 无本之木的答案正是评测集里最缺的那类负样本
// （AC③ 的"零命中项"就是这么来的，不是从 low_confidence 里挑出来的）。
//
// 但"检回数 0"在这里只被读成"答了而没凭据"：转人工、追问澄清、生成失败那三类根本没
// 回答的轮次同样带着 0（串行引擎在检索之前就 return 了），把它们记成知识缺口就是拿
// 系统公告去派知识库的活。那道口径在 service.markBadCase 的 badCaseTurnNotAnAnswer 里，
// 门槛本身不管——它只见数不见语境，这是分工不是漏项。
func ShouldMark(in BadCaseMarkInput) (source, reason string, ok bool) {
	threshold := in.Threshold
	if threshold <= 0 {
		threshold = badCaseFallbackThreshold()
	}
	if in.RetrievedCount <= 0 {
		return model.BadCaseSourceZeroHit,
			fmt.Sprintf("本轮检回 %d 条知识片段（阈值 %.4f，置信度 %.4f）", in.RetrievedCount, threshold, in.Confidence),
			true
	}
	if in.Confidence < threshold {
		return model.BadCaseSourceLowConfidence,
			fmt.Sprintf("置信度 %.4f 低于本轮生效阈值 %.4f", in.Confidence, threshold),
			true
	}
	return "", "", false
}

// Mark 落一条自动标记的坏例。返回 (true, nil) = 新落一行；(false, nil) = 这一轮已有行。
//
// 幂等键由 (source, session, message) 决定（见 model.BadCaseDedupKey）。message_id
// 缺失时**不落库**并回一句错误：热路径上拿不到消息 id 的轮次无法指回现场，
// 而空 message_id 会让所有这类轮次挤进同一条键 —— 第一条之后的真坏例全被"已存在"吃掉。
func (s *BadCaseService) Mark(ctx context.Context, in BadCaseMarkInput) (bool, error) {
	if !s.Available() {
		return false, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	source, reason, ok := ShouldMark(in)
	if !ok {
		return false, nil
	}
	dedupKey, keyOK := model.BadCaseDedupKey(source, in.SessionID, in.MessageID)
	if !keyOK {
		return false, fmt.Errorf("%w: 自动标记必须有 message_id（session=%s source=%s），否则同键互吞",
			ErrBadCaseInputInvalid, in.SessionID, source)
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return false, fmt.Errorf("%w: session_id 为空", ErrBadCaseInputInvalid)
	}
	threshold := in.Threshold
	if threshold <= 0 {
		threshold = badCaseFallbackThreshold()
	}
	now := time.Now()
	row := &model.BadCase{
		ID:             newBadCaseID(),
		Source:         source,
		Status:         model.BadCaseStatusPending,
		SessionID:      in.SessionID,
		MessageID:      in.MessageID,
		SignalID:       in.SignalID,
		IntentType:     in.IntentType,
		QueryText:      in.QueryText,
		AnswerText:     in.AnswerText,
		Confidence:     in.Confidence,
		Threshold:      threshold,
		RetrievedCount: in.RetrievedCount,
		MarkReason:     reason + in.MarkReason,
		DedupKey:       dedupKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.repo.InsertIfAbsent(ctx, row)
	if err != nil {
		return false, err
	}
	if created {
		logger.Ctx(ctx).Info().
			Str("case_id", row.ID).Str("source", source).Str("session_id", in.SessionID).
			Float64("confidence", in.Confidence).Float64("threshold", threshold).
			Msg("[BadCase] 自动标记一条低质回答")
	}
	return created, nil
}

// BadCaseMarkerFn 编排器上挂的标记器签名（与 humanTaskProducer 同一形状：
// 挂 nil 就等于不挂，对话主链路的行为与交付前逐字一致）。
type BadCaseMarkerFn func(ctx context.Context, in BadCaseMarkInput)

// BadCaseMarker 造出要挂到编排器上的自动标记器；底座为 nil 时回 nil（不挂）。
//
// 这里刻意**吞掉**错误只留一行日志：标记是对话的旁路，它挂了不该让回答发不出去。
// 但"吞"的边界要写清楚：吞的是 Mark 的 error，不吞 panic（panic 由编排器自身的
// recover 兜），也不吞"底座没装配"这件事 —— 那件事在装配点有一句 Warn。
func BadCaseMarker(svc *BadCaseService) BadCaseMarkerFn {
	if svc == nil || !svc.Available() {
		return nil
	}
	return func(ctx context.Context, in BadCaseMarkInput) {
		if _, err := svc.Mark(ctx, in); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("session_id", in.SessionID).Str("message_id", in.MessageID).
				Msg("[BadCase] 自动标记失败（不影响本轮回答）")
		}
	}
}

// MarkManual 人工补录一条坏例（坐席觉得答得烂，而置信度没觉得）。
//
// 与 Mark 分开写，因为两件事不同：自动那一路要过 ShouldMark 的门槛、并吃幂等键；
// 这一路是人的判断，既不该被门槛挡回去，也不该因为"同一个会话已经录过一条"就录不进。
// 所以 DedupKey 就是本行 id（唯一键恒成立），而 source 固定 manual —— 队列里
// "人报的"与"系统报的"必须分得开，否则自动标记的准确率会被人工那几条一起带偏。
func (s *BadCaseService) MarkManual(ctx context.Context, in BadCaseMarkInput, labelerID string) (*model.BadCase, error) {
	if !s.Available() {
		return nil, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	if strings.TrimSpace(labelerID) == "" {
		return nil, fmt.Errorf("%w: 补录必须落到具体的人", ErrBadCaseInputInvalid)
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, fmt.Errorf("%w: session_id 为空", ErrBadCaseInputInvalid)
	}
	if strings.TrimSpace(in.QueryText) == "" || strings.TrimSpace(in.AnswerText) == "" {
		return nil, fmt.Errorf("%w: 补录必须同时给出用户问题与被判的回答", ErrBadCaseInputInvalid)
	}
	threshold := in.Threshold
	if threshold <= 0 {
		threshold = badCaseFallbackThreshold()
	}
	now := time.Now()
	id := newBadCaseID()
	reason := strings.TrimSpace(in.MarkReason)
	if reason == "" {
		reason = "人工补录"
	}
	row := &model.BadCase{
		ID:             id,
		Source:         model.BadCaseSourceManual,
		Status:         model.BadCaseStatusPending,
		SessionID:      in.SessionID,
		MessageID:      in.MessageID,
		SignalID:       in.SignalID,
		IntentType:     in.IntentType,
		QueryText:      in.QueryText,
		AnswerText:     in.AnswerText,
		Confidence:     in.Confidence,
		Threshold:      threshold,
		RetrievedCount: in.RetrievedCount,
		MarkReason:     reason,
		DedupKey:       id,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := s.repo.InsertIfAbsent(ctx, row); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

// Label 给一条坏例下归因结论：pending → labeled。
func (s *BadCaseService) Label(ctx context.Context, id, labelerID, label, note string) (*model.BadCase, error) {
	if !s.Available() {
		return nil, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	if strings.TrimSpace(labelerID) == "" {
		return nil, fmt.Errorf("%w: 打标必须落到具体的人", ErrBadCaseInputInvalid)
	}
	if !model.IsKnownBadCaseLabel(label) {
		return nil, fmt.Errorf("%w: 未知归因类目 %q", ErrBadCaseInputInvalid, label)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		// 一句依据都不给打标，等于把这条结论变成"某人说它不对"。评测集会带上它，
		// 而 T-P8-04 拿这份集子算分时就再也无法判断结论是否成立。
		return nil, fmt.Errorf("%w: 打标必须写明判定依据", ErrBadCaseInputInvalid)
	}
	fixLayer := model.FixLayerOfLabel(label)
	if fixLayer == "" { // 上一行已挡未知类目；这条是防类目表与层表被人改歪
		return nil, fmt.Errorf("%w: 类目 %q 没有对应责任层", ErrBadCaseInputInvalid, label)
	}
	now := time.Now()
	applied, err := s.repo.ApplyAction(ctx, id, model.BadCaseStatusPending, func(row *model.BadCase) error {
		row.Status = model.BadCaseStatusLabeled
		row.Label = label
		row.LabelNote = note
		row.LabelerID = labelerID
		row.LabeledAt = &now
		row.FixLayer = fixLayer
		row.UpdatedAt = now
		return nil
	})
	return s.finishAction(ctx, id, applied, err)
}

// Dismiss 判定"这不是坏例"：pending|labeled → dismissed，必须给理由。
//
// 已打标也允许撤销成 dismissed（判完发现是误报是常态），但已导出的不行 ——
// 那份评测集已经交到 T-P8-04 手上了，这里悄悄改会让分数对不上任何一份字节。
func (s *BadCaseService) Dismiss(ctx context.Context, id, operatorID, reason string) (*model.BadCase, error) {
	if !s.Available() {
		return nil, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	if strings.TrimSpace(operatorID) == "" {
		return nil, fmt.Errorf("%w: 撤销必须落到具体的人", ErrBadCaseInputInvalid)
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("%w: 撤销必须写明理由", ErrBadCaseInputInvalid)
	}
	now := time.Now()
	for _, from := range []string{model.BadCaseStatusPending, model.BadCaseStatusLabeled} {
		if !model.CanBadCaseTransition(from, model.BadCaseStatusDismissed) {
			continue
		}
		applied, err := s.repo.ApplyAction(ctx, id, from, func(row *model.BadCase) error {
			row.Status = model.BadCaseStatusDismissed
			row.LabelerID = operatorID
			row.CancelReason = strings.TrimSpace(reason)
			row.LabeledAt = &now
			row.UpdatedAt = now
			return nil
		})
		if err != nil {
			return nil, err
		}
		if applied {
			return s.repo.GetByID(ctx, id)
		}
	}
	// 两个开放态都没抢到：要么行不存在，要么它已在 exported/dismissed。
	// 分清这两件事要一次读，宁可多一次读也不回一句含糊的 409。
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrBadCaseNotFound
	}
	return nil, fmt.Errorf("%w（当前状态 %s）", ErrBadCaseTransition, row.Status)
}

// Get 读一条。
func (s *BadCaseService) Get(ctx context.Context, id string) (*model.BadCase, error) {
	if !s.Available() {
		return nil, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// List 按条件取一页。
func (s *BadCaseService) List(ctx context.Context, q repository.BadCaseListQuery) ([]*model.BadCase, int64, error) {
	if !s.Available() {
		return nil, 0, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	if q.PageSize == 0 && q.Page == 0 {
		q.Page, q.PageSize = 1, BadCaseDefaultPageSize
	}
	return s.repo.List(ctx, q)
}

// BadCaseEvalRow 导出给评测集的一行（T-P8-04 的直接输入）。
//
// 字段刻意少于库里那一行：expected_* 一类"标准答案"列本卡**给不出**，
// 打标的人判的是"这句回答错在哪"，不是"正确回答是什么"。这里留一个空的
// expected_answer 字段会被下游当成"已有人写过标准答案"而直接用，
// 所以宁可不给，让 T-P8-04 自己决定要不要在此之上补标注。
type BadCaseEvalRow struct {
	CaseID     string    `json:"case_id"`
	Source     string    `json:"source"`
	QueryText  string    `json:"query_text"`
	AnswerText string    `json:"answer_text"`
	Label      string    `json:"label"`
	FixLayer   string    `json:"fix_layer"`
	LabelNote  string    `json:"label_note"`
	IntentType string    `json:"intent_type,omitempty"`
	Confidence float64   `json:"confidence"`
	Threshold  float64   `json:"threshold"`
	LabeledAt  time.Time `json:"labeled_at"`
	LabelerID  string    `json:"labeler_id"`
}

// BadCaseEvalExport 一次导出的产物。
type BadCaseEvalExport struct {
	EvalSetID  string           `json:"eval_set_id"`
	ExportedAt time.Time        `json:"exported_at"`
	Count      int              `json:"count"`
	Rows       []BadCaseEvalRow `json:"rows"`
}

// ExportEvalSet 取走一批"已打标且未导出"的样本，组成评测集。
func (s *BadCaseService) ExportEvalSet(ctx context.Context, labels []string, limit int) (*BadCaseEvalExport, error) {
	if !s.Available() {
		return nil, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	if limit == 0 {
		// 只有"没给"才兜默认档。负数是句无效请求，不是"没给"：
		// 把它一起兜掉，进程内调用方（将来的定时导出）写错符号就会静默拿到 200 行。
		limit = BadCaseDefaultExportLimit
	}
	evalSetID := newBadCaseEvalSetID()
	now := time.Now()
	rows, err := s.repo.ClaimForExport(ctx, labels, limit, evalSetID, now)
	if err != nil {
		if errors.Is(err, repository.ErrBadCaseAlreadyClaimed) {
			return nil, ErrBadCaseNothingToExport
		}
		return nil, err
	}
	out := &BadCaseEvalExport{EvalSetID: evalSetID, ExportedAt: now, Count: len(rows), Rows: make([]BadCaseEvalRow, 0, len(rows))}
	for _, row := range rows {
		labeled := time.Time{}
		if row.LabeledAt != nil {
			labeled = *row.LabeledAt
		}
		out.Rows = append(out.Rows, BadCaseEvalRow{
			CaseID:     row.ID,
			Source:     row.Source,
			QueryText:  row.QueryText,
			AnswerText: row.AnswerText,
			Label:      row.Label,
			FixLayer:   row.FixLayer,
			LabelNote:  row.LabelNote,
			IntentType: row.IntentType,
			Confidence: row.Confidence,
			Threshold:  row.Threshold,
			LabeledAt:  labeled,
			LabelerID:  row.LabelerID,
		})
	}
	logger.Ctx(ctx).Info().Str("eval_set_id", evalSetID).Int("count", out.Count).
		Msg("[BadCase] 导出一批评测集样本")
	return out, nil
}

// BadCaseDefaultExportLimit 未显式给上限时单次导出的行数。
//
// 故意是个小数：导出这一步的下游是评测基线，一次灌几千条未逐条复核的样本
// 会把基线变成噪声，而基线是要拿来比较两次改动好坏的。
const BadCaseDefaultExportLimit = 200

// BadCaseStats 队列看板读数。
type BadCaseStats struct {
	ByStatus   map[string]int64 `json:"by_status"`
	ByFixLayer map[string]int64 `json:"by_fix_layer"`
	// LabeledRatio 已判率（分母 = 四态总数）。单独给这一个数是因为它是本卡的
	// 北极星：闭环"转起来了没有"看的是有多少行走完了判定，而不是总共记了多少行。
	LabeledRatio float64 `json:"labeled_ratio"`
}

// Stats 汇总队列读数。
func (s *BadCaseService) Stats(ctx context.Context) (*BadCaseStats, error) {
	if !s.Available() {
		return nil, errors.New("bad_case: 底座不可用（未装配或缺少 DB 句柄）")
	}
	byStatus, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	byLayer, err := s.repo.CountByFixLayer(ctx)
	if err != nil {
		return nil, err
	}
	var total, judged int64
	for status, n := range byStatus {
		total += n
		if status != model.BadCaseStatusPending {
			judged += n
		}
	}
	out := &BadCaseStats{ByStatus: byStatus, ByFixLayer: byLayer}
	if total > 0 {
		out.LabeledRatio = float64(judged) / float64(total)
	}
	return out, nil
}

// finishAction 把仓储的 (applied, err) 翻译成三个 sentinel。
func (s *BadCaseService) finishAction(ctx context.Context, id string, applied bool, err error) (*model.BadCase, error) {
	if err != nil {
		return nil, err
	}
	if !applied {
		row, gerr := s.repo.GetByID(ctx, id)
		if gerr != nil {
			return nil, gerr
		}
		if row == nil {
			return nil, ErrBadCaseNotFound
		}
		return nil, fmt.Errorf("%w（当前状态 %s）", ErrBadCaseTransition, row.Status)
	}
	return s.repo.GetByID(ctx, id)
}
