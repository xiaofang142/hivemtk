// kb_release.go 知识库发布制的存储层（新规划 T-P9-02 / G-5，五层架构 L2）。
//
// 本层的边界要说清：**发布是一个事务**，而 service 层按架构门不得持有任何 GORM 句柄
// （check-architecture.sh 2.2 那条 `[a-zA-Z_]+\.db\.(Transaction|Where|...)` 判红）。
// 所以"锁行 → 分配待发布桶 → 逐条落库 → 移指针 → 记审计"这五步整体放在这里，
// service 只负责**决定哪些变更有资格进这个事务**（判审批结论、判 op 约束），
// 把裁决完的意图（PublishIntent）交进来。同 repository.CreateWithBinding 的既有折中。
//
// 与 internal/pkg/kbrelease 的分工按**表**劈：kb_releases 的读闸门片段与导入侧打戳
// 在叶子包（aiagent 不得 import 本层），其余三张表的读写都在本层。两边文件头互指。
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
)

// KBReleaseRepository 发布制仓储
type KBReleaseRepository struct {
	db *gorm.DB
}

// NewKBReleaseRepository 构造（db 由装配层给，本层不自己取全局句柄）
func NewKBReleaseRepository(db *gorm.DB) *KBReleaseRepository {
	return &KBReleaseRepository{db: db}
}

// Available 底座可用。nil 接收者要能答 false 而不是 panic（口径同 ApprovalRequestRepository）。
func (r *KBReleaseRepository) Available() bool { return r != nil && r.db != nil }

// ErrKBReleaseNotFound 库没有发布行（与"读故障"分开：前者是事实，后者要上抛）
var ErrKBReleaseNotFound = errors.New("kb_release: 该库没有发布记录")

// ErrKBReleaseNotGoverned 该库还没进发布制，任何写路径都拒在此处。
//
// 单列一个 error 而不是返回 fmt.Errorf：service 要能把"你没启用"翻译成运营端能照着做
// 的提示（去启用治理），而"底座的其它错误"不能一起被这样说。
var ErrKBReleaseNotGoverned = errors.New("kb_release: 该库未启用发布制")

// 下面四条都是**业务结论**而不是故障：它们回答"当前状态不允许这次动作"，
// 控制层据此回 409（前端该刷新指针视图），而没登记的错误一律落 500（前端该报装配）。
// 合起来会漏的是另一头：把"库里没上一版"和"连接断了"都报成 500，运营就会在
// 一个本来就没什么可回滚的库上无限重试。
var (
	// ErrKBReleaseRollbackUnavailable 没有可回退的上一版（从没发布过，或已经回到底）。
	ErrKBReleaseRollbackUnavailable = errors.New("kb_release: 该库没有可回滚的上一版")
	// ErrKBReleaseRestoreUnavailable 没有被回滚掉的版本可放回。
	ErrKBReleaseRestoreUnavailable = errors.New("kb_release: 该库没有被撤下的版本可放回")
	// ErrKBReleaseRecallBan 有已被回滚但尚未处置的版本 ⇒ 禁止跨号发布（防静默复活）。
	ErrKBReleaseRecallBan = errors.New("kb_release: 该库有被回滚掉的版本未处置，禁止发布")
	// ErrKBReleaseNothingToPublish 既没有已批准的变更，待发布桶也是空的。
	ErrKBReleaseNothingToPublish = errors.New("kb_release: 该库没有待发布内容")
	// ErrKBReleaseTargetConflict 变更指向的分段与当前语料对不上（不存在 / 已退役 / 属于别库）。
	ErrKBReleaseTargetConflict = errors.New("kb_release: 变更目标分段与当前语料不符")
	// ErrKBReleaseChangeNotPending 变更行在"读待办 → 进事务"之间被撤回或被别次发布带走。
	//
	// 单列一条（而不是并入 TargetConflict）：两者的运营动作完全不同 —— 前者是"这条改动
	// 已经不算数了，刷新待办再看"，后者是"你指的分段不对，重新提一条"。
	ErrKBReleaseChangeNotPending = errors.New("kb_release: 变更已不在待处理态，本次发布回滚")
)

// PublishChange 一条**已判定有资格**进本次发布的变更（合法性由 service 判完）。
type PublishChange struct {
	ID            string
	Op            string
	ProductID     string
	DocumentID    uint64
	Content       string
	TargetChunkID uint64
}

// AppliedChange 落库结果：新插入那条 chunk 的 id（retire 为 0），供 service 补向量化。
//
// 下面三个类型带 json 标签而上面的 PublishChange/PublishIntent 不带，分界线是
// "**会不会原样序列化进响应体**"：这三个会（Publish 回 verdicts+result，回滚/放回直接回
// 这三个号），而没标签时 Go 的字段名会以 PascalCase 出现在响应里 —— 本 API 的其余出口
// 全是 snake_case，前端照着 to_version 取数会拿到 undefined，且拿到的是 200 不是报错。
// PublishIntent/PublishChange 只进不出，给它们加标签等于给一个不上线的形状许承诺。
type AppliedChange struct {
	ChangeID       string `json:"change_id"`
	Op             string `json:"op"`
	AppliedChunkID uint64 `json:"applied_chunk_id"`
	Content        string `json:"content"`
}

// PublishIntent 一次发布的入参
type PublishIntent struct {
	ProductID string
	Actor     string
	Changes   []PublishChange
}

// PublishResult 一次发布的产出
type PublishResult struct {
	FromVersion int             `json:"from_version"`
	ToVersion   int             `json:"to_version"`
	Applied     []AppliedChange `json:"applied"`
}

// RollbackResult 回滚/放回的产出（三个号都回给调用方，管理端要能原样展示"从哪来回哪去"）
type RollbackResult struct {
	FromVersion int `json:"from_version"`
	ToVersion   int `json:"to_version"`
	Recalled    int `json:"recalled_version"`
}

// ensureRowSQL 建行幂等。governed 一律从 false 起：**建一行不等于开启治理** ——
// 让"有人查了一次这个库的状态"就把库推进发布制，等于用一个读操作改了写语义。
//
// created_at/updated_at 写死 now()：这两列是 AutoMigrate 建的，本句走的是裸 SQL，
// 少写一列就得到 NULL，而 model 上是 time.Time（非指针）—— 读回来那一下会直接报错。
// 与其把结论交给"AutoMigrate 到底给没给这两列加 NOT NULL"这种会随版本变的事实，
// 不如由写侧自己说清。
const ensureRowSQL = `
INSERT INTO kb_releases (product_id, governed, effective_version, previous_version,
                         recalled_version, draft_version, allocated_version, changed_by,
                         created_at, updated_at)
VALUES (?, false, 0, 0, 0, 0, 0, ?, now(), now())
ON CONFLICT (product_id) DO NOTHING
`

// releaseColumns 读一行的列名单（写死而不是 SELECT *：加列时本层要显式跟上，
// 别让一个 Scan 悄悄少读一列）。
const releaseColumns = "id, product_id, governed, effective_version, previous_version, " +
	"recalled_version, draft_version, allocated_version, changed_by, created_at, updated_at"

// LockRelease 取该行并在事务里加写锁；行不存在则先建再锁（幂等）。
//
// 必须在事务里调用才有序：锁的存活期就是事务的存活期（见 Publish/movePointer）。
func (r *KBReleaseRepository) LockRelease(ctx context.Context, tx *gorm.DB, productID, actor string) (*model.KBRelease, error) {
	if tx == nil {
		return nil, errors.New("kb_release repository: LockRelease 必须在事务内调用")
	}
	if err := tx.WithContext(ctx).Exec(ensureRowSQL, productID, actor).Error; err != nil {
		return nil, fmt.Errorf("kb_releases 建行失败: %w", err)
	}
	var rel model.KBRelease
	err := tx.WithContext(ctx).Table("kb_releases").
		Select(releaseColumns).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("product_id = ?", productID).
		Scan(&rel).Error
	if err != nil {
		return nil, fmt.Errorf("kb_releases 加锁读取失败: %w", err)
	}
	if rel.ID == 0 {
		return nil, ErrKBReleaseNotFound
	}
	return &rel, nil
}

// GetRelease 无锁读一行（管理端列表/详情用；不用于任何写路径）
func (r *KBReleaseRepository) GetRelease(ctx context.Context, productID string) (*model.KBRelease, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	var rel model.KBRelease
	err := r.db.WithContext(ctx).Table("kb_releases").
		Select(releaseColumns).
		Where("product_id = ?", productID).
		Scan(&rel).Error
	if err != nil {
		return nil, err
	}
	if rel.ID == 0 {
		return nil, ErrKBReleaseNotFound
	}
	return &rel, nil
}

// ListReleases 列出已进发布制的库（运营先看这张：哪些库被管着、各攒了多少待发布）
func (r *KBReleaseRepository) ListReleases(ctx context.Context) ([]model.KBRelease, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	var rows []model.KBRelease
	err := r.db.WithContext(ctx).Table("kb_releases").
		Select(releaseColumns).
		Where("governed").
		Order("product_id ASC").
		Scan(&rows).Error
	return rows, err
}

// SetGoverned 翻转发治开关（启用/停用发布制）。
//
// 停用只翻这一列，**不回滚任何已生效的版本号**：governed=false 让闸门对该库整体失效，
// 语料回到"全可见"，而历史版本号还在 —— 重新开启时不需要重建任何状态。
func (r *KBReleaseRepository) SetGoverned(ctx context.Context, productID string, governed bool, actor string) (*model.KBRelease, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	var before bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rel, err := r.LockRelease(ctx, tx, productID, actor)
		if err != nil {
			return err
		}
		before = rel.Governed
		if before == governed {
			return nil // 幂等：重复启用不该再记一条审计，也不该空转一次指针写
		}
		// UpdateColumns + 显式 updated_at：见 Publish 里同形状的注释。
		return tx.WithContext(ctx).Table("kb_releases").
			Where("id = ?", rel.ID).
			UpdateColumns(map[string]any{
				"governed":   governed,
				"changed_by": actor,
				"updated_at": time.Now(),
			}).Error
	})
	if err != nil {
		return nil, err
	}
	if before != governed {
		// 改前改后直接取这两个 bool：任何"由目标值反推旧值"的写法都会在一次翻转判断
		// 改动时把留痕写反（本函数上一版就是这么错的）。
		if err := r.AppendAudit(ctx, nil, model.KBChangeAuditLog{
			SubjectKey: "release:" + productID,
			Action:     model.KBAuditGoverned,
			OldValue:   fmt.Sprintf("%t", before),
			NewValue:   fmt.Sprintf("%t", governed),
			Actor:      actor,
		}); err != nil {
			return nil, err
		}
	}
	return r.GetRelease(ctx, productID)
}

// InsertChange 落一条变更请求
func (r *KBReleaseRepository) InsertChange(ctx context.Context, ch *model.KBChangeRequest) error {
	if !r.Available() {
		return errors.New("kb_release repository: db 未初始化")
	}
	return r.db.WithContext(ctx).Create(ch).Error
}

// GetChange 读一条变更；不存在返回 (nil, nil)（与 knowledge_base repo 的 GetByID 同口径，
// service 靠"nil 且无错"区分"查无此条"与"查不了"）
func (r *KBReleaseRepository) GetChange(ctx context.Context, id string) (*model.KBChangeRequest, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	var ch model.KBChangeRequest
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&ch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// KBChangeFilter 列表过滤
type KBChangeFilter struct {
	ProductID string
	Status    string
	Op        string
	Limit     int
	Offset    int
}

// ListChanges 变更列表（管理端待办视图）
func (r *KBReleaseRepository) ListChanges(ctx context.Context, f KBChangeFilter) ([]model.KBChangeRequest, int64, error) {
	if !r.Available() {
		return nil, 0, errors.New("kb_release repository: db 未初始化")
	}
	q := r.db.WithContext(ctx).Model(&model.KBChangeRequest{})
	if f.ProductID != "" {
		q = q.Where("product_id = ?", f.ProductID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Op != "" {
		q = q.Where("op = ?", f.Op)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.KBChangeRequest
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(f.Offset).Find(&rows).Error
	return rows, total, err
}

// PendingChanges 某库全部待处理变更（发布前的预览与"这版要带哪些改动"的判据来源）
func (r *KBReleaseRepository) PendingChanges(ctx context.Context, productID string) ([]model.KBChangeRequest, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	var rows []model.KBChangeRequest
	err := r.db.WithContext(ctx).
		Where("product_id = ? AND status = ?", productID, model.KBChangeStatusPending).
		Order("id ASC").
		Find(&rows).Error
	return rows, err
}

// WithdrawChange 发起人撤回。CAS 条件就是"仍是 pending"——
// 已经发布出去的撤不掉（跃迁表里没有 applied→withdrawn 这条路），
// 并发下被人先发布了也撤不掉：两种情况都返回 applied=false，由 service 报"撤不了"。
func (r *KBReleaseRepository) WithdrawChange(ctx context.Context, id string) (bool, error) {
	if !r.Available() {
		return false, errors.New("kb_release repository: db 未初始化")
	}
	res := r.db.WithContext(ctx).Model(&model.KBChangeRequest{}).
		Where("id = ? AND status = ?", id, model.KBChangeStatusPending).
		Update("status", model.KBChangeStatusWithdrawn)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// AppendAudit 记一条留痕（AC③）。与业务写同事务调用时把 tx 传进来，
// 传 nil 用本层句柄（独立记录，例如启用治理这类"事务已经 commit 完"的事件）。
func (r *KBReleaseRepository) AppendAudit(ctx context.Context, tx *gorm.DB, log model.KBChangeAuditLog) error {
	if r == nil {
		return errors.New("kb_release repository: 仓储未初始化")
	}
	db := r.db
	if tx != nil {
		db = tx
	}
	if db == nil {
		return errors.New("kb_release repository: db 未初始化")
	}
	if log.SubjectKey == "" || log.Action == "" || log.Actor == "" {
		return fmt.Errorf("kb_release repository: 审计行字段不全 subject=%q action=%q actor=%q",
			log.SubjectKey, log.Action, log.Actor)
	}
	return db.WithContext(ctx).Create(&log).Error
}

// RecordAudit 独立记一条留痕（不绑事务）。
//
// 单列一扇而不是让 service 传 nil：service 层不得出现 *gorm.DB（架构门 2.2 的口径），
// 而"提交之后再记"这一格本来就没有 tx 可给。
func (r *KBReleaseRepository) RecordAudit(ctx context.Context, log model.KBChangeAuditLog) error {
	return r.AppendAudit(ctx, nil, log)
}

// ListAuditBySubject 按对象读事件流（只增，不改不删）
func (r *KBReleaseRepository) ListAuditBySubject(ctx context.Context, subjectKey string, limit int) ([]model.KBChangeAuditLog, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.KBChangeAuditLog
	err := r.db.WithContext(ctx).
		Where("subject_key = ?", subjectKey).
		Order("id DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// chunkInsertSQL 新插入一条分段。
//
// 三列版本戳由本句显式写死（不靠 model 的 default 标签）：这三列是闸门的判据，
// 落库这一句必须自己说清"我把它放进第几版"，不能让一次重构悄悄把值交给标签。
// content_hash / content_tsv / contextual_tsv / embed_status 都不写：
// 前三个由 knowledge_chunks_tsv_trigger 维护
// （internal/migration/migrations/rag_hybrid_migration.go:151-171，含 content_hash 那段），
// embed_status 走库级 DEFAULT 'pending' —— 而**没有任何后台通路扫这一列回填向量**
// （实测：全仓读点只有 vector_retriever.go:153 的告警计数与文档级筛选），
// 所以向量侧的补齐由 service 在事务提交后显式做一次，见 service 侧 kbEmbedApplied。
const chunkInsertSQL = `
INSERT INTO knowledge_chunks
	(document_id, product_id, chunk_index, content, char_count, weight,
	 source_language, embedding_source, kb_version, retired_version, change_id)
VALUES (?, ?, ?, ?, ?, 1, 'zh', 'tei', ?, 0, ?)
RETURNING id
`

// chunkIndexSQL 同一文档内当前最大分段号的下一格（revise 复用旧行的号，让替换段落留在原位）
const chunkIndexSQL = `SELECT COALESCE(MAX(chunk_index), -1) + 1 FROM knowledge_chunks WHERE document_id = ?`

// chunkTarget 被改/被撤的目标行。
//
// 字段名与列名一一对应（RetiredVersion↔retired_version）：这里靠 model 标签做映射，
// 一个"看着像"的名字（比如 Retired）会让 Scan 悄悄填 0，而 0 正是"还没退役"——
// 于是"不能修订已退役分段"这条判据会无声失效，且失效方向是放行。
type chunkTarget struct {
	ID             uint64 `gorm:"column:id"`
	DocumentID     uint64 `gorm:"column:document_id"`
	ProductID      string `gorm:"column:product_id"`
	ChunkIndex     int    `gorm:"column:chunk_index"`
	KBVersion      int    `gorm:"column:kb_version"`
	RetiredVersion int    `gorm:"column:retired_version"`
	Content        string `gorm:"column:content"`
}

// loadChunk 读目标行；不存在返回 (nil, nil)
func (r *KBReleaseRepository) loadChunk(ctx context.Context, tx *gorm.DB, id uint64) (*chunkTarget, error) {
	var row chunkTarget
	err := tx.WithContext(ctx).Table("knowledge_chunks").
		Select("id, document_id, product_id, chunk_index, kb_version, retired_version, content").
		Where("id = ?", id).
		Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("knowledge_chunks 目标行读取失败: %w", err)
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &row, nil
}

// insertChunk 落一条新分段并回其 id。
//
// docID 与 c.DocumentID 分开传：revise 时变更行可能没带文档号（只指了 chunk），
// 那时归属要从被替换的那条老行继承，见 Publish 的 revise 分支。
func (r *KBReleaseRepository) insertChunk(ctx context.Context, tx *gorm.DB, c PublishChange, docID uint64, version, chunkIndex int) (uint64, error) {
	var id uint64
	err := tx.WithContext(ctx).Raw(chunkInsertSQL,
		docID, c.ProductID, chunkIndex, c.Content,
		len([]rune(c.Content)), version, c.ID,
	).Scan(&id).Error
	if err != nil {
		return 0, fmt.Errorf("knowledge_chunks 插入失败（change=%s）: %w", c.ID, err)
	}
	if id == 0 {
		return 0, fmt.Errorf("knowledge_chunks 插入未回 id（change=%s）", c.ID)
	}
	return id, nil
}

// retireChunk 把一条老分段标为在第 version 版退役。
//
// RowsAffected==0 只有两种可能：行已被并发删掉，或它**已经是退役态**。后者必须报错：
// 静默放过等于"同一条被下线两次"，第二次那条变更会在下一次发布里凭空消失，
// 而它的审批记录写着"已批准"——那是 AC① 的反面。
func (r *KBReleaseRepository) retireChunk(ctx context.Context, tx *gorm.DB, chunkID uint64, version int) error {
	res := tx.WithContext(ctx).Table("knowledge_chunks").
		Where("id = ? AND retired_version = 0", chunkID).
		Update("retired_version", version)
	if res.Error != nil {
		return fmt.Errorf("knowledge_chunks 退役写入失败（chunk=%d）: %w", chunkID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("chunk %d 不存在或已被退役，无法再次下线", chunkID)
	}
	return nil
}

// Publish 一次发布：把 service 判定过的变更落进库，并把生效指针移到待发布桶。
//
// 五步全在同一个事务里，任何一步失败整体回滚 —— 于是"发布失败"这件事的唯一表现就是
// 调用方拿到一个 error，库里既没有半套语料、也没有半个指针（这是上一版设计里
// apply_failed 那个态被删掉的原因）。
//
// 五条内建判据（都在本函数里，不靠调用方自觉）：
//  1. 该库必须 governed=true（否则报错；发布**不**顺带开启治理，见 ensureRowSQL 那条注释）；
//  2. recalled_version > effective_version 时**拒绝发布**：那一格是被人回滚掉的，指针跨过去
//     就会把它静默放回线上（见 model.KBRelease.RecalledVersion）；
//  3. 待发布桶为 0 时从 allocated 高水位往后取一格，绝不复用旧号；
//  4. 桶号不高于在服版本就停下（账目被外部改过的形态，宁可拒绝，不能绕过审批上线）；
//  5. 既没有变更又没有待发布桶 ⇒ 拒绝：空移一次指针会让"上一版"变成一个内容与当前
//     完全相同的号，回滚按钮按下去等于什么都没发生，而审计里写了一次发布。
//
// 关于桶的共享：draft_version 可能已被导入链路预先分配并写进过快照（见
// kbrelease.EnsureDraftStamp），本函数只是把指针推过它 —— 所以一次发布会把
// "这段时间导入的内容 + 这批已批准的变更"整体上线，这是"导入也走发布制"的题中之义。
func (r *KBReleaseRepository) Publish(ctx context.Context, in PublishIntent) (*PublishResult, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	if in.ProductID == "" || in.Actor == "" {
		return nil, errors.New("kb_release repository: Publish 缺 product_id 或 actor")
	}
	out := &PublishResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rel, err := r.LockRelease(ctx, tx, in.ProductID, in.Actor)
		if err != nil {
			return err
		}
		if !rel.Governed {
			return fmt.Errorf("%w（product=%s）", ErrKBReleaseNotGoverned, in.ProductID)
		}
		if rel.RecalledVersion > rel.EffectiveVersion {
			return fmt.Errorf("%w（版本 %d 已被回滚，先处置它的变更（撤回或重新提交）再发布）",
				ErrKBReleaseRecallBan, rel.RecalledVersion)
		}
		out.FromVersion = rel.EffectiveVersion

		bucket := rel.DraftVersion
		if bucket <= 0 {
			bucket = rel.AllocatedVersion + 1
		}
		if bucket <= rel.EffectiveVersion {
			// 正常走不到（高水位只增），走到就是账目被外部改过：宁可停下，
			// 也不能把新一批内容打上一个"已经在服"的号 —— 那等于绕过审批直接上线。
			return fmt.Errorf("待发布桶 %d 不高于在服版本 %d，拒绝落库", bucket, rel.EffectiveVersion)
		}
		if len(in.Changes) == 0 && rel.DraftVersion == 0 {
			return fmt.Errorf("%w（product=%s：无已批准的变更、待发布桶为空）",
				ErrKBReleaseNothingToPublish, in.ProductID)
		}

		nextIdx := make(map[uint64]int) // 同一文档在本次事务里的**下一个**可用分段号
		for _, c := range in.Changes {
			var appliedID uint64
			switch c.Op {
			case model.KBChangeOpAdd:
				if c.Content == "" {
					return fmt.Errorf("变更 %s：add 必须有内容", c.ID)
				}
				if c.DocumentID == 0 {
					return fmt.Errorf("变更 %s：add 必须指定 document_id", c.ID)
				}
				idx, err := r.chunkIndexFor(ctx, tx, c.DocumentID, nextIdx)
				if err != nil {
					return err
				}
				newID, err := r.insertChunk(ctx, tx, c, c.DocumentID, bucket, idx)
				if err != nil {
					return err
				}
				appliedID = newID
			case model.KBChangeOpRevise:
				old, err := r.loadChunk(ctx, tx, c.TargetChunkID)
				if err != nil {
					return err
				}
				if old == nil {
					return fmt.Errorf("%w：要修订的 chunk %d 不存在", ErrKBReleaseTargetConflict, c.TargetChunkID)
				}
				if old.RetiredVersion != 0 {
					return fmt.Errorf("%w：chunk %d 已在版本 %d 退役，不能再次修订",
						ErrKBReleaseTargetConflict, c.TargetChunkID, old.RetiredVersion)
				}
				if old.ProductID != c.ProductID {
					return fmt.Errorf("%w：chunk %d 属于库 %s，与本变更的库 %s 不符",
						ErrKBReleaseTargetConflict, c.TargetChunkID, old.ProductID, c.ProductID)
				}
				docID := c.DocumentID
				if docID == 0 {
					docID = old.DocumentID
				}
				// 新行沿用旧行的分段号：替换要落在原位，检索结果的排序与上下文邻接才不变。
				newID, err := r.insertChunk(ctx, tx, c, docID, bucket, old.ChunkIndex)
				if err != nil {
					return err
				}
				if err := r.retireChunk(ctx, tx, c.TargetChunkID, bucket); err != nil {
					return err
				}
				appliedID = newID
			case model.KBChangeOpRetire:
				old, err := r.loadChunk(ctx, tx, c.TargetChunkID)
				if err != nil {
					return err
				}
				if old == nil {
					return fmt.Errorf("%w：要下线的 chunk %d 不存在", ErrKBReleaseTargetConflict, c.TargetChunkID)
				}
				if old.ProductID != c.ProductID {
					return fmt.Errorf("%w：chunk %d 属于库 %s，与本变更的库 %s 不符",
						ErrKBReleaseTargetConflict, c.TargetChunkID, old.ProductID, c.ProductID)
				}
				if err := r.retireChunk(ctx, tx, c.TargetChunkID, bucket); err != nil {
					return err
				}
			default:
				return fmt.Errorf("未知变更动作 %q", c.Op)
			}
			out.Applied = append(out.Applied, AppliedChange{
				ChangeID: c.ID, Op: c.Op, AppliedChunkID: appliedID, Content: c.Content,
			})
			// 状态回写带 CAS 条件并**必须吃掉影响行数**：本事务只锁了 kb_releases 那一行，
			// 变更行没锁 —— service 读待办与进事务之间如果有人撤回（WithdrawChange 是独立
			// 通路），这里就是 0 行。放过它的表现是"正文已经进语料、变更行却写着 withdrawn"，
			// 而审计里那次撤回根本没发生。0 行即整次发布回滚（同上面"没有半套语料"的口径）。
			upd := tx.WithContext(ctx).Model(&model.KBChangeRequest{}).
				Where("id = ? AND status = ?", c.ID, model.KBChangeStatusPending).
				Updates(map[string]any{
					"status":           model.KBChangeStatusApplied,
					"release_version":  bucket,
					"applied_chunk_id": appliedID,
				})
			if upd.Error != nil {
				return fmt.Errorf("变更状态回写失败（change=%s）: %w", c.ID, upd.Error)
			}
			if upd.RowsAffected == 0 {
				return fmt.Errorf("%w（change=%s：读待办与落库之间被撤回，或被别次发布带走）",
					ErrKBReleaseChangeNotPending, c.ID)
			}
			if err := r.AppendAudit(ctx, tx, model.KBChangeAuditLog{
				SubjectKey: "change:" + c.ID,
				Action:     model.KBAuditPublished,
				OldValue:   model.KBChangeStatusPending,
				NewValue:   fmt.Sprintf("%s@v%d", model.KBChangeStatusApplied, bucket),
				Actor:      in.Actor,
			}); err != nil {
				return err
			}
		}

		// 指针移动。UpdateColumns 而非 Updates：本行有 updated_at，而 Table() 方式没有
		// schema，GORM 不会替我们补自动时间戳 —— 所以 updated_at 显式进 map。
		// 更重要的是**不能**让任何钩子把 governed 之类没列进 map 的字段按零值写回去。
		upd := map[string]any{
			"effective_version": bucket,
			"previous_version":  rel.EffectiveVersion,
			"draft_version":     0,
			"allocated_version": bucket,
			"recalled_version":  0,
			"changed_by":        in.Actor,
			"updated_at":        time.Now(),
		}
		if err := tx.WithContext(ctx).Table("kb_releases").
			Where("id = ?", rel.ID).
			UpdateColumns(upd).Error; err != nil {
			return fmt.Errorf("kb_releases 指针移动失败: %w", err)
		}
		out.ToVersion = bucket
		return r.AppendAudit(ctx, tx, model.KBChangeAuditLog{
			SubjectKey: "release:" + in.ProductID,
			Action:     model.KBAuditPublished,
			OldValue:   fmt.Sprintf("effective=%d draft=%d", rel.EffectiveVersion, rel.DraftVersion),
			NewValue:   fmt.Sprintf("effective=%d previous=%d applied=%d", bucket, rel.EffectiveVersion, len(in.Changes)),
			Actor:      in.Actor,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// chunkIndexFor 取该文档本次可用的分段号，并把 map 推进到下一格。
//
// 推进是必须的：只记不推的话，同一次发布里对同一文档的两条 add 会拿到同一个
// chunk_index（(document_id,chunk_index) 上没有唯一约束兜着，于是静默得到两条同号分段，
// 重建与展示都按号取第一条 —— 第二条永远检索不到）。
func (r *KBReleaseRepository) chunkIndexFor(ctx context.Context, tx *gorm.DB, docID uint64, nextIdx map[uint64]int) (int, error) {
	if n, ok := nextIdx[docID]; ok {
		nextIdx[docID] = n + 1
		return n, nil
	}
	var idx int
	if err := tx.WithContext(ctx).Raw(chunkIndexSQL, docID).Scan(&idx).Error; err != nil {
		return 0, fmt.Errorf("knowledge_chunks 分段号读取失败（document=%d）: %w", docID, err)
	}
	nextIdx[docID] = idx + 1
	return idx, nil
}

// Rollback 生效指针回拨一格：previous → effective，previous 清零，并记下刚被撤下的号。
//
// **一个字节都不碰 knowledge_chunks**（AC②）。回拨之后：
//   - 被撤下那一版引入的行 kb_version > effective ⇒ 不可见（它们还在表里，随时能回来）；
//   - 被撤下那一版退役的行 retired_version > effective ⇒ 老内容重新可见。
//     两条都只由 effective 这一个数决定，所以"回滚"没有任何数据搬运。
//
// previous 清掉是刻意的（实现里把它写成"当前在服号"，于是回滚后 previous>=effective，
// movePointer 的回滚判据随即拒绝第二次回滚）：连点两下不会穿到更老的历史。
// 想回到刚撤下的那一版，用 Restore —— 它是本函数的精确逆操作。
func (r *KBReleaseRepository) Rollback(ctx context.Context, productID, actor string) (*RollbackResult, error) {
	return r.movePointer(ctx, productID, actor, false)
}

// Restore 回滚的逆操作：把被撤下的那一版放回去。
func (r *KBReleaseRepository) Restore(ctx context.Context, productID, actor string) (*RollbackResult, error) {
	return r.movePointer(ctx, productID, actor, true)
}

// movePointer 回滚/放回共用一条通路：两者都只是同一行上四个号的换法。
//
// forward=false：E=previous，recalled=当前在服号（禁令由此升起，挡住下一次跨号发布）。
// forward=true ：E=recalled，recalled=0（禁令解除），previous=刚在服的号。
// 两个方向的 previous 都是"刚离开的那个号"，所以每次移动后只保留一步可退。
func (r *KBReleaseRepository) movePointer(ctx context.Context, productID, actor string, forward bool) (*RollbackResult, error) {
	if !r.Available() {
		return nil, errors.New("kb_release repository: db 未初始化")
	}
	out := &RollbackResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rel, err := r.LockRelease(ctx, tx, productID, actor)
		if err != nil {
			return err
		}
		action := model.KBAuditRollback
		var to, recalled int
		if forward {
			action = model.KBAuditRestored
			if rel.RecalledVersion <= rel.EffectiveVersion {
				return fmt.Errorf("%w（当前在服 v%d，撤下号 v%d）",
					ErrKBReleaseRestoreUnavailable, rel.EffectiveVersion, rel.RecalledVersion)
			}
			to = rel.RecalledVersion
			recalled = 0
		} else {
			if rel.PreviousVersion <= 0 || rel.PreviousVersion >= rel.EffectiveVersion {
				return fmt.Errorf("%w（当前在服 v%d，上一版 v%d）",
					ErrKBReleaseRollbackUnavailable, rel.EffectiveVersion, rel.PreviousVersion)
			}
			to = rel.PreviousVersion
			recalled = rel.EffectiveVersion
		}
		out.FromVersion, out.ToVersion, out.Recalled = rel.EffectiveVersion, to, recalled
		if err := tx.WithContext(ctx).Table("kb_releases").
			Where("id = ?", rel.ID).
			UpdateColumns(map[string]any{
				"effective_version": to,
				"previous_version":  rel.EffectiveVersion,
				"recalled_version":  recalled,
				"changed_by":        actor,
				"updated_at":        time.Now(),
			}).Error; err != nil {
			return fmt.Errorf("kb_releases 指针回拨失败: %w", err)
		}
		return r.AppendAudit(ctx, tx, model.KBChangeAuditLog{
			SubjectKey: "release:" + productID,
			Action:     action,
			OldValue:   fmt.Sprintf("effective=%d previous=%d", rel.EffectiveVersion, rel.PreviousVersion),
			NewValue:   fmt.Sprintf("effective=%d recalled=%d", out.ToVersion, out.Recalled),
			Actor:      actor,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CountChunksInForce 某库**在服**的分条数与总条数（运营视图与"闸门拦掉了多少"的读数）。
//
// 关键取舍：可见性判据**只有叶子包那一份**（kbrelease.VisiblePredicate），这里不另写
// 一遍规则 —— 两套口径迟早会分叉，而分叉的表现是"页面说有待发布、实际搜不到"。
// 谓词为空（闸门 off）时在服数就等于总数，这也正是 off 档应有的读数。
//
// 两次计数分开跑：第二条走的是谓词原样（"NOT EXISTS(...)"= 可见），
// 任何"取反再减"的字符串改写都会把 inForce 与 hidden 算反，且红不出来。
func (r *KBReleaseRepository) CountChunksInForce(ctx context.Context, productID string) (int64, int64, error) {
	if !r.Available() {
		return 0, 0, errors.New("kb_release repository: db 未初始化")
	}
	var total, inForce int64
	if err := r.db.WithContext(ctx).Table("knowledge_chunks").
		Where("product_id = ?", productID).
		Count(&total).Error; err != nil {
		return 0, 0, err
	}
	q := r.db.WithContext(ctx).Table("knowledge_chunks").Where("product_id = ?", productID)
	if pred := kbrelease.VisiblePredicate(); pred != "" {
		q = q.Where(pred)
	}
	if err := q.Count(&inForce).Error; err != nil {
		return 0, 0, err
	}
	return inForce, total, nil
}
