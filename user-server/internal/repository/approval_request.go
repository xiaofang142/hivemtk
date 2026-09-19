// approval_request.go 审批检查点仓储（T-P3-01 / N-4）
//
// 五层架构里本文件是 service.ApprovalRequestService 与 approval_requests 表之间唯一的一层。
//
// **本卡刻意没有内存版底座**（与 T-P2-01 的草稿不同，那里内存/DB 一副接口两副实现）：
// 审批记录重启即空 = 下一次入队查不到 pending = 要么重复建待办、要么在最坏的一种
// 实现里"查不到就当作没拦过"直接放行。闸门忘掉自己批过什么，比闸门报错严重得多，
// 所以这里只有一条路：句柄不可用 ⇒ 明确报错，由调用方 fail-closed。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// ErrApprovalPendingConflict 同一 subject+policy 已有他人先建的 pending 审批
// （部分唯一索引 uq_approval_request_open）。
//
// 单列成 sentinel 而不是让调用方去匹配 PG 约束名：判据要写在代码里才能测，
// "23505" 这种字符串比较散在 service 里等于没有判据（同 ErrOrderDraftPendingConflict）。
var ErrApprovalPendingConflict = errors.New("approval_request: 该对象同策略已有 pending 审批")

// ErrApprovalTokenEmpty 空恢复凭证。
//
// 这是一道**必须存在**的守卫而不是输入洁癖：resume_token 的唯一索引是部分的
// （谓词排除空串），所以 auto-approve 的记录在库里就是 token 为空串，且可以有很多条。
// 少了这道守卫，"按凭证查挂起点"会拿空串去命中其中任意一条（First 取哪条由排序决定），
// 于是任何一次 token 漏传的恢复调用都能"恢复"到一条与它毫无关系的已批准记录上。
//
// 注：这段注释刻意不出现"不等于空串"的 SQL 字面量写法（两个 ASCII 单引号相邻）。实测
// gofmt 会把**函数声明的文档注释**里相邻的两个单引号重写成排版引号，连反引号包起来的代码段
// 也照改，写完就被静默改掉 —— 本卡实测踩过两次。
var ErrApprovalTokenEmpty = errors.New("approval_request: 恢复凭证为空")

// approvalWriteColumns 裁决时可写的列。
//
// 这是一份**白名单**，写在这里的意义是"名单之外的一切改不动"：
//   - id/created_at：主键与入队时刻不可改（改了等于把一条审批挪到另一次入队上）；
//   - subject_type/subject_id/policy_key：**被审对象的身份不可改**。若允许在裁决的同一次
//     写入里改掉 subject，一条"已批准 (quote,q1)"的记录可以被顺手改成 (quote,q2)，
//     等于用一次合法裁决给另一张报价开了门 —— 这是本表唯一能被用来绕过闸门的路径；
//   - resume_token：凭证一旦发出不许换（流程手里那份会当场失效，且换 token 不是裁决动作）；
//   - expires_at：裁决后它是惰性的（清扫只挑 pending），不改它就不必进写集合。
var approvalWriteColumns = []string{
	"status", "decided_by", "decided_at", "decision_note", "updated_at",
}

// ApprovalRequestRepository 审批请求读写接口
type ApprovalRequestRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Insert 落一条新记录。同一 (subject_type, subject_id, policy_key) 已有 pending 行时
	// 返回 ErrApprovalPendingConflict，其余冲突原样返回。
	Insert(ctx context.Context, r *model.ApprovalRequest) error

	// GetByID 按主键读取；不存在返回 (nil, nil)。**读失败返回 error**，不返回 (nil, nil)
	// —— 把故障读成"没有这条审批"，调用方就会当成"还没批"而放行等待，或者直接放弃挂起流程。
	GetByID(ctx context.Context, id string) (*model.ApprovalRequest, error)

	// GetByResumeToken 按恢复凭证读取；查不到返回 (nil, nil)；空凭证返回 ErrApprovalTokenEmpty。
	GetByResumeToken(ctx context.Context, token string) (*model.ApprovalRequest, error)

	// GetPendingBySubject 取该 (subject, policy) 当前处于 pending 的那条（幂等复用用）；
	// 没有返回 (nil, nil)，读失败返回 error —— 两者的差别就是本卡 AC③ 能不能兑现的地方。
	GetPendingBySubject(ctx context.Context, subjectType, subjectID, policyKey string) (*model.ApprovalRequest, error)

	// MutatePending 在一条事务里锁住某行、确认仍是 pending 后交 fn 就地改，再按
	// approvalWriteColumns 写回。返回 false = 行不存在或已不是 pending（fn 未被调用）。
	//
	// 不写成"GetByID → 判 pending → Save"三步：两名审批人同时点同一行时，两边都读到
	// pending、各写一次自己的结论，**后写的把先写的覆盖掉**，而两边都以为自己批成功了 ——
	// 于是一件事带着两个相反裁决继续往下走。挡住这件事的是写回时那句
	// `WHERE id = ? AND status = 'pending'`（比较并交换）；`FOR UPDATE` 买的是另一样东西：
	// **fn 执行期间这一行不许被别人改**。两者各自被一条测试钉住
	// （`_ConcurrentMutateSingleWinner` / `_MutateHoldsRowLockWhileFnRuns`）——
	// 变异实测：摘掉 FOR UPDATE 只有后者红，前者仍绿，别把单赢家的功劳记到行锁头上。
	MutatePending(ctx context.Context, id string, fn func(*model.ApprovalRequest)) (bool, error)

	// ExpirePendingBatch 在一条事务里锁定一批"已到期且仍 pending"的记录并翻成 expired，
	// 返回**实际被翻转**的行。limit<=0 = 本轮不限。
	//
	// FOR UPDATE SKIP LOCKED 与草稿侧同理：多副本 worker 各拿一份不相交的集合，
	// 不会把同一批审批各过期一次（也不会在清扫与人工裁决之间抢同一行、把刚批完的行刷成过期）。
	ExpirePendingBatch(ctx context.Context, now time.Time, limit int) ([]*model.ApprovalRequest, error)
}

type approvalRequestRepo struct {
	db *gorm.DB
}

// NewApprovalRequestRepository 创建实例（用全局 DB）
func NewApprovalRequestRepository() ApprovalRequestRepository {
	return &approvalRequestRepo{db: _db.GetDB()}
}

// NewApprovalRequestRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）
func NewApprovalRequestRepositoryWithDB(db *gorm.DB) ApprovalRequestRepository {
	return &approvalRequestRepo{db: db}
}

func (r *approvalRequestRepo) Available() bool { return r != nil && r.db != nil }

func (r *approvalRequestRepo) require() error {
	if !r.Available() {
		return errors.New("approval_request repository: db handle is nil")
	}
	return nil
}

func (r *approvalRequestRepo) Insert(ctx context.Context, a *model.ApprovalRequest) error {
	if err := r.require(); err != nil {
		return err
	}
	if a == nil || a.ID == "" {
		return errors.New("approval_request repository: 空记录或空 ID")
	}
	if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
		if isApprovalPendingConflict(err) {
			return ErrApprovalPendingConflict
		}
		return err
	}
	return nil
}

// isApprovalPendingConflict 判定"命中的是 uq_approval_request_open 还是别的约束"。
//
// SQLSTATE 与约束名**两个条件都要命中**：把别的唯一约束（例如将来给 resume_token 加的
// 全表唯一）也读成"已有 pending"，service 会去做一次毫无意义的重查、然后把一次真冲突
// 汇报成幂等复用。
//
// 不认 gorm.ErrDuplicatedKey：那个类型只在 Open 时设 TranslateError:true 才产出，
// 本仓连接配置没开，这里拿到的是底层 *pgconn.PgError（错误串含 23505 与约束名）。
func isApprovalPendingConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, "uq_approval_request_open")
}

func (r *approvalRequestRepo) GetByID(ctx context.Context, id string) (*model.ApprovalRequest, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var a model.ApprovalRequest
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *approvalRequestRepo) GetByResumeToken(ctx context.Context, token string) (*model.ApprovalRequest, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, ErrApprovalTokenEmpty
	}
	var a model.ApprovalRequest
	err := r.db.WithContext(ctx).Where("resume_token = ?", token).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *approvalRequestRepo) GetPendingBySubject(ctx context.Context, subjectType, subjectID, policyKey string) (*model.ApprovalRequest, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var a model.ApprovalRequest
	err := r.db.WithContext(ctx).
		Where("subject_type = ? AND subject_id = ? AND policy_key = ? AND status = ?",
			subjectType, subjectID, policyKey, model.ApprovalStatusPending).
		Order("created_at ASC, id ASC").
		First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// MutatePending 一次加锁读 + 判态 + 按列清单写回，返回 fn 是否真的落库。
//
// 排序上有讲究：先 FOR UPDATE 读到快照，再判 status，最后带 status 条件写回。
// 两道判据各管一件事，实测可分离（见 approval_request_test.go 的两条用例）：
//   - 带 `status='pending'` 的写回 = 单赢家的来源。摘掉它才会退化成乐观覆盖。
//   - FOR UPDATE = 让 fn 看到并锁定这一行的**当前**版本（fn 执行期间别人改不动它，
//     含非白名单列）。摘掉它，8 协程用例仍绿 —— 因为 UPDATE 自己会排队、重判时行已不是
//     pending；但 fn 里"读到的就是将要写回的那一行"这个前提没了。
//
// 注释只描述这两件事，不再声称"少了行锁就会双写成功"（那是本卡实测推翻的一句）。
func (r *approvalRequestRepo) MutatePending(ctx context.Context, id string, fn func(*model.ApprovalRequest)) (bool, error) {
	if err := r.require(); err != nil {
		return false, err
	}
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a model.ApprovalRequest
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&a).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if a.Status != model.ApprovalStatusPending {
			return nil
		}
		if fn != nil {
			fn(&a)
		}
		// Model 传的是**空壳**而不是 &a：GORM 会从 Model 的主键值再拼一条 `id = ?` 进
		// WHERE，而 a 是刚被 fn 改过的那个实例 —— 一旦 fn 动了 m.ID，两条 id 条件互斥，
		// 更新静默命中 0 行，applied=false，service 便会对一条**仍然 pending** 的记录
		// 报出"已由他人裁决"。写回的目标只认传进来的 id，fn 改什么都不算数。
		res := tx.Model(&model.ApprovalRequest{}).
			Where("id = ? AND status = ?", id, model.ApprovalStatusPending).
			Select(approvalWriteColumns).
			Updates(&a)
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (r *approvalRequestRepo) ExpirePendingBatch(ctx context.Context, now time.Time, limit int) ([]*model.ApprovalRequest, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var flipped []*model.ApprovalRequest
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			// expires_at IS NOT NULL 在 SQL 三值逻辑下是冗余的（NULL <= now 不为真），
			// 显式写出来是为了让"已裁决行不会被清扫碰到"这句话能被读见。
			Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?",
				model.ApprovalStatusPending, now).
			Order("expires_at ASC, id ASC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		var rows []*model.ApprovalRequest
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, a := range rows {
			ids = append(ids, a.ID)
		}
		if err := tx.Model(&model.ApprovalRequest{}).
			Where("id IN ? AND status = ?", ids, model.ApprovalStatusPending).
			Updates(map[string]any{
				"status":        model.ApprovalStatusExpired,
				"decided_by":    model.ApprovalDecidedByTTL,
				"decided_at":    now,
				"decision_note": "ttl_expired",
				"updated_at":    now,
			}).Error; err != nil {
			return err
		}
		for _, a := range rows {
			a.Status = model.ApprovalStatusExpired
			a.DecidedBy = model.ApprovalDecidedByTTL
			at := now
			a.DecidedAt = &at
			a.DecisionNote = "ttl_expired"
			a.UpdatedAt = now
		}
		flipped = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return flipped, nil
}
