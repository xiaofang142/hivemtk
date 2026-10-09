// kb_release.go 知识库变更流程的业务层（新规划 T-P9-02 / G-5，五层架构 L3）。
//
// 这一层只回答两个问题：**这条变更有没有资格进下一次发布**，以及**发布之后还要补什么**。
// 落库本身（锁行 → 分桶 → 写 chunk → 移指针 → 记审计）在 repository.Publish 那一个事务里，
// 因为架构门 2.2 不许 service 持 GORM 句柄，也不许在这里做多语句事务 ——
// 于是本文件里既没有 db 也没有 tx，只有一个 store 接口。
//
// 与审批的关系按"读结论、不复制结论"处理（同 C2 的口径）：
//   - 提交时入队一条 approval_requests（subject_type=kb_change，policy=kb.change.apply）；
//   - 发布时**逐条回读**那条审批的当前状态，只有 approved 才进事务；
//   - kb_change_requests 里刻意不记 rejected/expired —— 批没批的唯一事实源是审批表，
//     这里再记一份就会出现"审批说批了、变更说没批"这种永远查不出来的分歧。
//     代价是"被拒的变更行仍留在 pending"，这是有意取舍：它只能被发起人撤回，
//     而它永远不会进语料（本函数不认它），AC① 不靠清理来保证。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 审批侧的命名键。两者都要过 ApprovalSubmitInput.normalize() 的字符集
// （小写字母、数字、_ . -），且 subject_type <=32、policy_key <=64。
const (
	// KBChangeApprovalSubjectType 被审对象类型。
	KBChangeApprovalSubjectType = "kb_change"
	// KBChangeApprovalPolicyKey 谁要求审的：知识内容上线这一格。
	KBChangeApprovalPolicyKey = "kb.change.apply"
)

// 入参上限。长度都是**列宽**决定的，不是审美：
// product_id 是 varchar(64)（与 KBRelease 的唯一索引同形），超长的写入会在库里被截断或报错，
// 而那一次失败发生在审批已经入队**之后** —— 会留下一条没有变更行的审批，所以必须在这里挡掉。
const (
	kbChangeProductMaxLen   = 64
	kbChangeReasonMaxLen    = 4000
	kbChangeActorMaxLen     = 256
	kbChangeContentMaxRunes = 16384

	// kbChangeIDMaxLen 与 knowledge_chunks.change_id 的 varchar(40) 同宽：
	// 落库那条 chunk 要带得上这个号，号被截断就等于分段与变更对不上账。
	kbChangeIDMaxLen = 40
)

// KBReleaseProductMaxLen 是 kb_releases.product_id / kb_change_requests.product_id 的列宽。
//
// 导出给控制层做路径参数闸：超长的 product_id 不会得到一句 400，而是走到 PG 的
// "value too long" ⇒ 同一个原因在两个端点上分别报 400 和 500，比数字抄两份更容易漂。
const KBReleaseProductMaxLen = kbChangeProductMaxLen

// kbWriteArgs 三个写动作（发布 / 回滚 / 启停治理）共用的入参整理。
//
// 收成一处的理由与 normalize() 同源：这三条都要"去空白 + 非空 + 不超列宽"，
// 少一项的后果不是提示难看，是一条超长 product_id 走到仓储拿到一句数据库报错。
func kbWriteArgs(productID, actor string) (string, string, error) {
	productID = strings.TrimSpace(productID)
	actor = strings.TrimSpace(actor)
	if productID == "" {
		return "", "", fmt.Errorf("%w: product_id 为空", ErrKBChangeInputInvalid)
	}
	if len(productID) > kbChangeProductMaxLen {
		return "", "", fmt.Errorf("%w: product_id 长 %d 超列宽 %d",
			ErrKBChangeInputInvalid, len(productID), kbChangeProductMaxLen)
	}
	if actor == "" {
		return "", "", fmt.Errorf("%w: actor 为空（发布/回滚/启停必须落到具体的人，留痕才有出处）",
			ErrKBChangeInputInvalid)
	}
	if len(actor) > kbChangeActorMaxLen {
		return "", "", fmt.Errorf("%w: actor 长 %d 超 %d",
			ErrKBChangeInputInvalid, len(actor), kbChangeActorMaxLen)
	}
	return productID, actor, nil
}

// 本层错误。分开定义是因为控制层按它们挑 HTTP 码：
// 400（入参本身不合法）/404（查无此条）/409（状态不允许这次动作）/503（底座未装配）。
var (
	ErrKBReleaseUnavailable     = errors.New("知识库变更底座未装配（无 DB 句柄）")
	ErrKBChangeInputInvalid     = errors.New("知识库变更入参不合法")
	ErrKBChangeNotFound         = errors.New("知识库变更不存在")
	ErrKBChangeNotWithdrawable  = errors.New("该变更当前状态不允许撤回")
	ErrKBReleaseApprovalMissing = errors.New("变更没有可用的审批结论")
)

// 仓储那五条**业务结论**在本层同名转授（同一个 error 变量的第二个名字，不是复制品）：
// 控制层按 depguard 只许 import service（T-P8-03 的 ErrBadCaseInputInvalid 同形处理），
// 而 errors.Is 判的是变量身份 ⇒ 别名不会变成"两个错要分别处置"的死分支。
//
// 与之相对，仓储里那些没包装的裸错误（连接断、SQL 报错、账目自相矛盾）**不**在这一组里：
// 它们到控制层就是 500。把"库里没有上一版"和"库里读不出来"并成一个码，
// 运营就会在一个本来无事可做的库上无限重试。
var (
	ErrKBReleaseNotGoverned         = repository.ErrKBReleaseNotGoverned
	ErrKBReleaseRollbackUnavailable = repository.ErrKBReleaseRollbackUnavailable
	ErrKBReleaseRestoreUnavailable  = repository.ErrKBReleaseRestoreUnavailable
	ErrKBReleaseRecallBan           = repository.ErrKBReleaseRecallBan
	ErrKBReleaseNothingToPublish    = repository.ErrKBReleaseNothingToPublish
	ErrKBReleaseTargetConflict      = repository.ErrKBReleaseTargetConflict
	// ErrKBReleaseChangeNotPending 也是**业务结论**：它答的是"这条改动此刻不算数"，
	// 不是"底座读不出来"，所以必须与上面一组同进 409，否则运营看到 500 会重试一次
	// 同样注定回滚的动作。
	ErrKBReleaseChangeNotPending = repository.ErrKBReleaseChangeNotPending
)

// KBChangeListQuery 变更列表的过滤条件（别名而不是照抄：照抄会在两层之间
// 留下一个需要手工同步的形状，同 BadCaseListQuery）。
type KBChangeListQuery = repository.KBChangeFilter

// KBChangeSubmitInput 一次变更提交。
type KBChangeSubmitInput struct {
	ProductID     string
	DocumentID    uint64
	Op            string
	TargetChunkID uint64
	Content       string
	Reason        string
	RequestedBy   string
}

// KBChangeVerdicts 发布时按审批结论分好的一组账。
//
// 五格并列而不是"通过的 + 一个 skipped 计数"：运营要能逐号知道**为什么这次没带上**，
// 而"pending"与"rejected"的下一步动作完全不同（前者等审批、后者要重提）。
type KBChangeVerdicts struct {
	Included       []string `json:"included"`
	SkippedPending []string `json:"skipped_pending"`
	SkippedRefused []string `json:"skipped_refused"`
	SkippedExpired []string `json:"skipped_expired"`
	SkippedUnknown []string `json:"skipped_unknown"`
}

// KBReleaseStats 一个库的发布视图（分条数 + 在服数 + 待办数）。
type KBReleaseStats struct {
	Release     *model.KBRelease `json:"release"`
	InForce     int64            `json:"in_force"`
	Total       int64            `json:"total"`
	Pending     int64            `json:"pending_changes"`
	GateMode    string           `json:"gate_mode"`
	ShadowStats bool             `json:"shadow_stats"`
}

// kbReleaseStore 本层用到的仓储面（窄接口：能被测试替身完整覆盖，且不会随仓储加方法而漂）。
type kbReleaseStore interface {
	Available() bool
	GetRelease(ctx context.Context, productID string) (*model.KBRelease, error)
	ListReleases(ctx context.Context) ([]model.KBRelease, error)
	SetGoverned(ctx context.Context, productID string, governed bool, actor string) (*model.KBRelease, error)
	InsertChange(ctx context.Context, ch *model.KBChangeRequest) error
	GetChange(ctx context.Context, id string) (*model.KBChangeRequest, error)
	ListChanges(ctx context.Context, f repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error)
	PendingChanges(ctx context.Context, productID string) ([]model.KBChangeRequest, error)
	WithdrawChange(ctx context.Context, id string) (bool, error)
	RecordAudit(ctx context.Context, log model.KBChangeAuditLog) error
	ListAuditBySubject(ctx context.Context, subjectKey string, limit int) ([]model.KBChangeAuditLog, error)
	Publish(ctx context.Context, in repository.PublishIntent) (*repository.PublishResult, error)
	Rollback(ctx context.Context, productID, actor string) (*repository.RollbackResult, error)
	Restore(ctx context.Context, productID, actor string) (*repository.RollbackResult, error)
	CountChunksInForce(ctx context.Context, productID string) (int64, int64, error)
}

// kbChangeApprovalGateway 审批服务的窄面（同 quote_send 的 quoteSendApproval）。
type kbChangeApprovalGateway interface {
	Submit(ctx context.Context, in ApprovalSubmitInput) (*model.ApprovalRequest, bool, error)
	Get(ctx context.Context, id string) (*model.ApprovalRequest, error)
}

// kbChunkEmbedder 向量化通路。实现者是 aiagent 侧的 KnowledgeService.EmbedAndPersistChunks。
//
// 为什么发布之后还要补一次向量：**没有任何后台通路扫 embed_status='pending' 回填向量**
// （实测：全仓对这一列的读点只有 vector_retriever.go:153 的告警计数与文档级筛选），
// 落库那条新分段的 embedding 是 NULL ⇒ 词面检索（content_tsv 由触发器维护）当场可检，
// 语义检索要等这一次显式补算。所以它是"补强"而不是"生效"，失败只出声、不让发布回滚。
type kbChunkEmbedder interface {
	EmbedAndPersistChunks(ctx context.Context, numericProductID string, chunks []model.KnowledgeChunk) error
}

// KBReleaseService 知识库变更与发布服务
type KBReleaseService struct {
	store     kbReleaseStore
	approvals kbChangeApprovalGateway
	embedder  kbChunkEmbedder

	// now/seq 是**实例字段**而不是包级变量：本包有一条硬规矩（seam-guard 门 + 上界枚举），
	// 任何"测试可写、且可能被异步链读到的包级全局"都要上锁登记并补一条 -race 腿。
	// 时钟与序号只在同步的请求链路里用，放在实例上既没有竞态（atomic）也不需要登记。
	now func() time.Time
	seq atomic.Uint64
}

// NewKBReleaseService 构造。approvals 必填（变更不经审批就没有"有资格"这个东西）；
// embedder 可为 nil（未接向量通路时发布照常生效，只是不做补算，并出声）。
func NewKBReleaseService(store kbReleaseStore, approvals kbChangeApprovalGateway, embedder kbChunkEmbedder) *KBReleaseService {
	return &KBReleaseService{store: store, approvals: approvals, embedder: embedder, now: time.Now}
}

// Available 底座可用（口径同 BadCaseService）
func (s *KBReleaseService) Available() bool {
	return s != nil && s.store != nil && s.store.Available() && s.approvals != nil
}

// globalKBReleaseSvc 全局登记处（形状同 globalBadCaseSvc：路由现取全局，未装配 ⇒ 503）。
var globalKBReleaseSvc atomic.Pointer[KBReleaseService]

// SetGlobalKBReleaseService 登记全局实例；传 nil 等于撤掉（端点回 503）。
func SetGlobalKBReleaseService(s *KBReleaseService) { globalKBReleaseSvc.Store(s) }

// GlobalKBReleaseService 取全局实例（可能为 nil，调用方必须判空）。
func GlobalKBReleaseService() *KBReleaseService { return globalKBReleaseSvc.Load() }

// GateMode 当前闸门档位（off|shadow|on），供端点回显"现在改了会不会立刻影响线上"。
func GateMode() string { return kbrelease.ModeForLog() }

// changeID 生成变更主键。
//
// 号里带纳秒与实例自增两位：纳秒保证跨进程不撞（多副本同机同纳秒的概率极低，且撞了是
// 主键冲突报错、不是静默覆盖），自增保证同一次调用序列里必然不同。
// 长度受 knowledge_chunks.change_id 的 varchar(40) 约束，超了就直接报错而不是截断 ——
// 截断后的号写进分段，变更与分段就对不上账，而那正是 AC③ 要的答案。
func (s *KBReleaseService) changeID() (string, error) {
	id := fmt.Sprintf("kbc_%d_%d", s.now().UnixNano(), s.seq.Add(1))
	if len(id) > kbChangeIDMaxLen {
		return "", fmt.Errorf("%w: 变更号 %s 长 %d 超 %d", ErrKBChangeInputInvalid, id, len(id), kbChangeIDMaxLen)
	}
	return id, nil
}

// normalize 去空白 + 按列宽与 op 约束校验。返回就地整理后的入参副本。
//
// 校验顺序是"先看能不能救（去空白）、再看形状、最后看动作约束"：动作约束放最后是因为
// 它的报错要能同时给出 op，而 op 的合法性在前一步才定下来。
func (in KBChangeSubmitInput) normalize() (KBChangeSubmitInput, error) {
	out := in
	out.ProductID = strings.TrimSpace(out.ProductID)
	out.Op = strings.ToLower(strings.TrimSpace(out.Op))
	out.Reason = strings.TrimSpace(out.Reason)
	out.RequestedBy = strings.TrimSpace(out.RequestedBy)
	out.Content = strings.TrimSpace(out.Content)

	if out.ProductID == "" {
		return out, fmt.Errorf("%w: product_id 为空", ErrKBChangeInputInvalid)
	}
	if len(out.ProductID) > kbChangeProductMaxLen {
		return out, fmt.Errorf("%w: product_id 长 %d 超列宽 %d",
			ErrKBChangeInputInvalid, len(out.ProductID), kbChangeProductMaxLen)
	}
	if out.Reason == "" {
		// 理由非空是 AC③ 的落点之一：没有"为什么改"的批准记录，事后无法判断这一版凭什么上线。
		return out, fmt.Errorf("%w: reason 为空（变更必须写明理由）", ErrKBChangeInputInvalid)
	}
	if len(out.Reason) > kbChangeReasonMaxLen {
		return out, fmt.Errorf("%w: reason 长 %d 超 %d", ErrKBChangeInputInvalid, len(out.Reason), kbChangeReasonMaxLen)
	}
	if out.RequestedBy == "" {
		return out, fmt.Errorf("%w: requested_by 为空", ErrKBChangeInputInvalid)
	}
	if len(out.RequestedBy) > kbChangeActorMaxLen {
		return out, fmt.Errorf("%w: requested_by 长 %d 超 %d", ErrKBChangeInputInvalid, len(out.RequestedBy), kbChangeActorMaxLen)
	}
	if !model.IsValidKBChangeOp(out.Op) {
		return out, fmt.Errorf("%w: op=%q 不在 %s 之内",
			ErrKBChangeInputInvalid, out.Op, strings.Join(model.KBChangeOps, "/"))
	}
	if n := len([]rune(out.Content)); n > kbChangeContentMaxRunes {
		return out, fmt.Errorf("%w: content %d 字超单条分段上限 %d（请拆成多条变更）",
			ErrKBChangeInputInvalid, n, kbChangeContentMaxRunes)
	}
	switch out.Op {
	case model.KBChangeOpAdd:
		if out.Content == "" {
			return out, fmt.Errorf("%w: add 必须有内容", ErrKBChangeInputInvalid)
		}
		if out.DocumentID == 0 {
			return out, fmt.Errorf("%w: add 必须指定 document_id（分段归属的文档）", ErrKBChangeInputInvalid)
		}
		if out.TargetChunkID != 0 {
			return out, fmt.Errorf("%w: add 不该带 target_chunk_id", ErrKBChangeInputInvalid)
		}
	case model.KBChangeOpRevise:
		if out.Content == "" {
			return out, fmt.Errorf("%w: revise 必须有新内容", ErrKBChangeInputInvalid)
		}
		if out.TargetChunkID == 0 {
			return out, fmt.Errorf("%w: revise 必须指定被替换的 target_chunk_id", ErrKBChangeInputInvalid)
		}
	case model.KBChangeOpRetire:
		if out.TargetChunkID == 0 {
			return out, fmt.Errorf("%w: retire 必须指定 target_chunk_id", ErrKBChangeInputInvalid)
		}
		if out.Content != "" {
			return out, fmt.Errorf("%w: retire 不该带 content（要改内容请用 revise）", ErrKBChangeInputInvalid)
		}
	}
	return out, nil
}

// SubmitChange 提交一条变更：入队审批 → 落变更行 → 记留痕。
//
// 顺序是**先审批后入库**（与 model 里那条注释同一条理由）：反过来会留下"一条 pending 变更、
// 没有对应审批"的孤儿，而它是 AC① 的反例 —— 发布时只能按"没批"处理，但它在待办列表里
// 长得像一条正常待办。审批入队失败则一行都不留。
//
// 入库失败**不**回滚那条审批：审批行会停在 pending，到期由 ExpireOverdue 收掉，
// 期间即使被人批了也没有变更行可发布（发布读的是 kb_change_requests）。
// 反过来才危险：有变更行而查不到结论。
func (s *KBReleaseService) SubmitChange(ctx context.Context, in KBChangeSubmitInput) (*model.KBChangeRequest, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	in, err := in.normalize()
	if err != nil {
		return nil, err
	}
	id, err := s.changeID()
	if err != nil {
		return nil, err
	}

	appr, created, err := s.approvals.Submit(ctx, ApprovalSubmitInput{
		SubjectType: KBChangeApprovalSubjectType,
		SubjectID:   id,
		PolicyKey:   KBChangeApprovalPolicyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("变更 %s 入队审批失败: %w", id, err)
	}
	if appr == nil {
		// Submit 要么给行要么给错；nil 且无错是实现漂了的形态，按"没拿到结论"处理：不落库。
		return nil, fmt.Errorf("%w: 变更 %s 的审批入队返回空记录", ErrKBReleaseApprovalMissing, id)
	}

	ch := &model.KBChangeRequest{
		ID:            id,
		ProductID:     in.ProductID,
		DocumentID:    in.DocumentID,
		Op:            in.Op,
		TargetChunkID: in.TargetChunkID,
		Content:       in.Content,
		Reason:        in.Reason,
		Status:        model.KBChangeStatusPending,
		ApprovalID:    appr.ID,
		RequestedBy:   in.RequestedBy,
	}
	if err := s.store.InsertChange(ctx, ch); err != nil {
		return nil, fmt.Errorf("变更 %s 落库失败（审批 %s 已入队，等它到期即可，不会有孤儿变更行）: %w", id, appr.ID, err)
	}
	if err := s.store.RecordAudit(ctx, model.KBChangeAuditLog{
		SubjectKey: "change:" + id,
		Action:     model.KBAuditSubmitted,
		OldValue:   "",
		NewValue:   fmt.Sprintf("%s op=%s approval=%s", model.KBChangeStatusPending, in.Op, appr.ID),
		Actor:      in.RequestedBy,
	}); err != nil {
		// 留痕失败不回滚变更行（审批与变更都已落库，回滚反而制造孤儿），但必须出声：
		// AC③ 少一条记录是可补的，静默少一条才是问题。
		logger.Warnf("[kb-release] 变更 %s 的 submitted 留痕写入失败: %v", id, err)
	}
	logger.Infof("[kb-release] 变更已提交：%s op=%s product=%s approval=%s（新建=%t）",
		id, in.Op, in.ProductID, appr.ID, created)
	// I8：变更发布前进不了 knowledge_chunks，这条铃铛就是"队列里压了一单"
	// 的唯一即时提醒；写失败只出声，提交本身已经成功。
	NotifyBusiness(ctx, KBChangePendingNotification(ch))
	return ch, nil
}

// WithdrawChange 发起人撤回一条还没发布的变更。
//
// 判据只有两处：跃迁表答"pending 能不能去 withdrawn"（唯一事实源在 model），
// 仓储的 CAS 答"它现在还是不是 pending"。发布事务已把状态推到 applied 的那条撤不掉 ——
// 想撤下已上线的内容，提一条新的 retire 再走一次审批，而不是在这里开一条特权路径。
func (s *KBReleaseService) WithdrawChange(ctx context.Context, id, actor string) (*model.KBChangeRequest, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(actor) == "" {
		return nil, fmt.Errorf("%w: id 或 actor 为空", ErrKBChangeInputInvalid)
	}
	ch, err := s.store.GetChange(ctx, id)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, fmt.Errorf("%w: %s", ErrKBChangeNotFound, id)
	}
	if !model.KBChangeTransitionAllowed(ch.Status, model.KBChangeStatusWithdrawn) {
		return nil, fmt.Errorf("%w: %s 当前是 %s（只有 pending 能撤；已发布的请改提一条 retire 走审批）",
			ErrKBChangeNotWithdrawable, id, ch.Status)
	}
	ok, err := s.store.WithdrawChange(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		// 上面刚判过能撤 —— 这里 CAS 没中，就是被人抢先把这条发布了。
		// 判据留在两处不是为了两次读，而是为了让"读到的状态"与"改到的行"之间的窗口
		// 由 CAS 兜住：宁可报"撤不了"，也不能把一个已上线的号写回 withdrawn。
		return nil, fmt.Errorf("%w: %s 在本次撤回之间状态已变（可能刚被发布）", ErrKBChangeNotWithdrawable, id)
	}
	if err := s.store.RecordAudit(ctx, model.KBChangeAuditLog{
		SubjectKey: "change:" + id,
		Action:     model.KBAuditWithdrawn,
		OldValue:   model.KBChangeStatusPending,
		NewValue:   model.KBChangeStatusWithdrawn,
		Actor:      strings.TrimSpace(actor),
	}); err != nil {
		return nil, err
	}
	return s.store.GetChange(ctx, id)
}

// PublishPending 发布某库当前**已获批准**的变更：一个批次、一个事务、一次指针移动。
//
// 三条口径值得说明：
//  1. "有没有资格"逐条问审批，不问变更行自己（后者只有 pending/applied/withdrawn 三态）；
//     审批行与对象的三列（subject_type / subject_id / policy_key）必须逐一对上，
//     少比一列就是给别的对象开门（同 quote_send 的 carriedApproval）。
//  2. 一条都没批**且待发布桶为空**时才不调用事务：那趟空指针移动会留下一条误导性的发布留痕。
//     桶不为空时必须进事务 —— 见下面分支里的理由（导入也走发布制）。
//  3. 向量补算在事务之后，失败只出声：语料已经生效，词面检索当场就能命中它，
//     把"补强失败"报成"发布失败"会让人以为要重放一次已经成功的发布。
func (s *KBReleaseService) PublishPending(ctx context.Context, productID, actor string) (*KBChangeVerdicts, *repository.PublishResult, error) {
	outcome := &KBChangeVerdicts{
		Included:       []string{},
		SkippedPending: []string{},
		SkippedRefused: []string{},
		SkippedExpired: []string{},
		SkippedUnknown: []string{},
	}
	if !s.Available() {
		return outcome, nil, ErrKBReleaseUnavailable
	}
	productID, actor, argErr := kbWriteArgs(productID, actor)
	if argErr != nil {
		return outcome, nil, argErr
	}

	// 先问"这个库进没进发布制"，再问"有没有东西可发"。事务里还有同一道判据
	// （repository.Publish 的第 1 条内建判据），这里提前问是为了让答案是 409
	// "这个库没进发布制"，而不是 200"这次没什么可发的"—— 后者把"你没开门"
	// 说成了"屋里是空的"，运营照着这句只会再点一次发布按钮。
	//
	// NotFound 归一成 nil 行再判，不看仓储是否同时回了行：真库回的是 (nil, NotFound)，
	// 但这一路的判据只认"有没有一行 governed=true"，不该依赖错误与行的配对形状。
	rel, err := s.store.GetRelease(ctx, productID)
	if errors.Is(err, repository.ErrKBReleaseNotFound) {
		rel, err = nil, nil
	}
	if err != nil {
		return outcome, nil, err
	}
	if rel == nil || !rel.Governed {
		return outcome, nil, fmt.Errorf("%w（product=%s）", ErrKBReleaseNotGoverned, productID)
	}

	pending, err := s.store.PendingChanges(ctx, productID)
	if err != nil {
		return outcome, nil, err
	}
	intent := repository.PublishIntent{ProductID: productID, Actor: actor}
	for i := range pending {
		ch := pending[i]
		if ch.ApprovalID == "" {
			// 无号的行按"没批"处理（不猜）：它要么是半路写入的历史遗留，要么是入队失败后的残留。
			outcome.SkippedUnknown = append(outcome.SkippedUnknown, ch.ID)
			continue
		}
		appr, gerr := s.approvals.Get(ctx, ch.ApprovalID)
		if gerr != nil {
			// 读结论失败 ⇒ 整次发布停下。"跳过这一条继续发"会把"读不到"变成"没批"，
			// 而这两件事对运营的含义完全不同（一次故障 vs 一条决定）。
			return outcome, nil, fmt.Errorf("读变更 %s 的审批 %s 失败: %w", ch.ID, ch.ApprovalID, gerr)
		}
		if appr == nil {
			outcome.SkippedUnknown = append(outcome.SkippedUnknown, ch.ID)
			continue
		}
		if appr.SubjectType != KBChangeApprovalSubjectType || appr.SubjectID != ch.ID ||
			appr.PolicyKey != KBChangeApprovalPolicyKey {
			outcome.SkippedUnknown = append(outcome.SkippedUnknown, ch.ID)
			logger.Warnf("[kb-release] 变更 %s 挂着审批 %s，但三列是 (%s,%s,%s) ⇒ 不是这条变更的结论，本次不带",
				ch.ID, appr.ID, appr.SubjectType, appr.SubjectID, appr.PolicyKey)
			continue
		}
		switch appr.Status {
		case model.ApprovalStatusApproved:
			intent.Changes = append(intent.Changes, repository.PublishChange{
				ID: ch.ID, Op: ch.Op, ProductID: ch.ProductID,
				DocumentID: ch.DocumentID, Content: ch.Content, TargetChunkID: ch.TargetChunkID,
			})
			outcome.Included = append(outcome.Included, ch.ID)
		case model.ApprovalStatusPending:
			outcome.SkippedPending = append(outcome.SkippedPending, ch.ID)
		case model.ApprovalStatusRejected:
			outcome.SkippedRefused = append(outcome.SkippedRefused, ch.ID)
		case model.ApprovalStatusExpired:
			outcome.SkippedExpired = append(outcome.SkippedExpired, ch.ID)
		default:
			// 未知状态一律不当"批了"（口径同 quote_send 的 verdict unknown ⇒ 不发）
			outcome.SkippedUnknown = append(outcome.SkippedUnknown, ch.ID)
			logger.Warnf("[kb-release] 变更 %s 的审批 %s status=%q 无法识别 ⇒ 本次不带", ch.ID, appr.ID, appr.Status)
		}
	}
	if len(intent.Changes) == 0 && rel.DraftVersion == 0 {
		logger.Infof("[kb-release] 库 %s 本次没有可发布的变更：待审 %d 条 / 被拒 %d 条 / 过期 %d 条 / 结论不明 %d 条",
			productID, len(outcome.SkippedPending), len(outcome.SkippedRefused),
			len(outcome.SkippedExpired), len(outcome.SkippedUnknown))
		return outcome, nil, nil
	}
	// 到这里才进事务。注意上面那条判据是 `变更为空 **且** 桶为空`：桶是导入链路预先
	// 打好的戳（kbrelease.EnsureDraftStamp），只导入了内容、一条变更都没提的库，
	// 桶里是有货的 —— 那种发布必须真的发生，否则"导入也走发布制"这条路是死的：
	// 导进来的内容永远攒在待发布桶里，而每次点发布都只回一句"没有可发布的变更"。
	// 桶号不高于在服版本那种账目异常留给仓储判（它回的是拒绝，不是空跑）。

	res, err := s.store.Publish(ctx, intent)
	if err != nil {
		return outcome, nil, err
	}
	s.embedApplied(ctx, productID, res)
	logger.Infof("[kb-release] 库 %s 发布完成：v%d → v%d，落库 %d 条变更",
		productID, res.FromVersion, res.ToVersion, len(res.Applied))
	return outcome, res, nil
}

// embedApplied 事务提交后补算向量（best-effort，理由见 PublishPending 第 3 条）。
func (s *KBReleaseService) embedApplied(ctx context.Context, productID string, res *repository.PublishResult) {
	if s.embedder == nil || res == nil {
		return
	}
	chunks := make([]model.KnowledgeChunk, 0, len(res.Applied))
	for _, ap := range res.Applied {
		if ap.AppliedChunkID == 0 || ap.Content == "" {
			continue // retire 没有新行；空内容不必占一次 embedding 调用
		}
		chunks = append(chunks, model.KnowledgeChunk{ID: ap.AppliedChunkID, Content: ap.Content})
	}
	if len(chunks) == 0 {
		return
	}
	if err := s.embedder.EmbedAndPersistChunks(ctx, productID, chunks); err != nil {
		logger.Errorf("[kb-release] 库 %s 版本 v%d 的向量补算失败（%d 条分段，词面检索不受影响，可重放 reindex）: %v",
			productID, res.ToVersion, len(chunks), err)
	}
}

// Rollback 回滚一格（纯指针移动）。
func (s *KBReleaseService) Rollback(ctx context.Context, productID, actor string) (*repository.RollbackResult, error) {
	return s.movePointer(ctx, productID, actor, false)
}

// Restore 放回被回滚掉的那一版。
func (s *KBReleaseService) Restore(ctx context.Context, productID, actor string) (*repository.RollbackResult, error) {
	return s.movePointer(ctx, productID, actor, true)
}

func (s *KBReleaseService) movePointer(ctx context.Context, productID, actor string, forward bool) (*repository.RollbackResult, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	productID, actor, argErr := kbWriteArgs(productID, actor)
	if argErr != nil {
		return nil, argErr
	}
	if forward {
		return s.store.Restore(ctx, productID, actor)
	}
	return s.store.Rollback(ctx, productID, actor)
}

// SetGoverned 启用/停用某库的发布制（库级那道锁；全局旗子是另一道）。
//
// 这里补一条"闸门当前档位"的告警：governed=true 而旗子是 off 时，用户会看到
// "启用了但什么都没被挡住"，那正是最需要说出来的组合。
func (s *KBReleaseService) SetGoverned(ctx context.Context, productID string, governed bool, actor string) (*model.KBRelease, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	productID, actor, argErr := kbWriteArgs(productID, actor)
	if argErr != nil {
		return nil, argErr
	}
	rel, err := s.store.SetGoverned(ctx, productID, governed, actor)
	if err != nil {
		return nil, err
	}
	mode := kbrelease.ModeForLog()
	if governed && mode == "off" {
		logger.Warnf("[kb-release] 库 %s 已进发布制，但 %s=off ⇒ 闸门不加进召回 SQL，待发布内容照常可见；要真拦需置 on",
			productID, kbrelease.FlagEnv)
	} else {
		logger.Infof("[kb-release] 库 %s governed=%t（闸门档位 %s）", productID, governed, mode)
	}
	return rel, nil
}

// GetRelease 读一个库的发布指针（没有行 ⇒ (nil, nil)，让调用方把"未启用"当状态展示）
func (s *KBReleaseService) GetRelease(ctx context.Context, productID string) (*model.KBRelease, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	rel, err := s.store.GetRelease(ctx, strings.TrimSpace(productID))
	if errors.Is(err, repository.ErrKBReleaseNotFound) {
		return nil, nil
	}
	return rel, err
}

// ListReleases 已启用发布制的库列表
func (s *KBReleaseService) ListReleases(ctx context.Context) ([]model.KBRelease, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	return s.store.ListReleases(ctx)
}

// ListChanges 变更列表（管理端待办视图）
func (s *KBReleaseService) ListChanges(ctx context.Context, f repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error) {
	if !s.Available() {
		return nil, 0, ErrKBReleaseUnavailable
	}
	f.ProductID = strings.TrimSpace(f.ProductID)
	return s.store.ListChanges(ctx, f)
}

// GetChange 读一条变更（不存在 ⇒ (nil, nil)，同仓储口径）
func (s *KBReleaseService) GetChange(ctx context.Context, id string) (*model.KBChangeRequest, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	return s.store.GetChange(ctx, strings.TrimSpace(id))
}

// ListAudit 读某个对象的留痕（change:<id> 或 release:<product_id>）
func (s *KBReleaseService) ListAudit(ctx context.Context, subjectKey string, limit int) ([]model.KBChangeAuditLog, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	if strings.TrimSpace(subjectKey) == "" {
		return nil, fmt.Errorf("%w: subject_key 为空", ErrKBChangeInputInvalid)
	}
	return s.store.ListAuditBySubject(ctx, strings.TrimSpace(subjectKey), limit)
}

// Stats 一个库的发布视图。
//
// InForce 走的是叶子包那一份可见性谓词（与召回同一个判据），所以这个数回答的是
// "线上现在真能不能检到"，不是"表里有几条"。两者的差就是闸门拦掉的那部分。
// ShadowStats 为真表示当前在观察档：下面的 InForce 是**全量**（闸门没参与过滤），
// 读数含义与 on 档不同，界面必须带这个标志位展示，否则会把观察期的数字当成"没拦住东西"。
func (s *KBReleaseService) Stats(ctx context.Context, productID string) (*KBReleaseStats, error) {
	if !s.Available() {
		return nil, ErrKBReleaseUnavailable
	}
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, fmt.Errorf("%w: product_id 为空", ErrKBChangeInputInvalid)
	}
	rel, err := s.GetRelease(ctx, productID)
	if err != nil {
		return nil, err
	}
	inForce, total, err := s.store.CountChunksInForce(ctx, productID)
	if err != nil {
		return nil, err
	}
	pending, err := s.store.PendingChanges(ctx, productID)
	if err != nil {
		return nil, err
	}
	mode := kbrelease.ModeForLog()
	return &KBReleaseStats{
		Release:     rel,
		InForce:     inForce,
		Total:       total,
		Pending:     int64(len(pending)),
		GateMode:    mode,
		ShadowStats: mode != "on",
	}, nil
}
