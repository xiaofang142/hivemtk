package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserCommandLogRepository append-only 命令-事件日志仓储。
// 不可变性契约：永不 UPDATE 单行、执行路径无 Delete；PruneBefore 是治理级时间窗整体裁剪
// （G19：无界增长不可接受），与"篡改审计记录"是两回事——保留期内审计/重放事实完整。
type BrowserCommandLogRepository interface {
	Append(ctx context.Context, entry *model.BrowserCommandLog) error
	ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserCommandLog, error)

	// PruneBefore 的 cutoffSource 必填：写明这个界的**来源**（配置项名=值 / 调用方标识），
	// 空串即拒绝（ErrPruneCutoffSourceRequired）。界本身落在 prune_runs.cutoff 上，
	// 但"改界"这件事只有带着来源才可查——见 model.BrowserAuditPruneRun.CutoffSource。
	PruneBefore(ctx context.Context, cutoff time.Time, cutoffSource string) (int64, error)
}

// ErrPruneCutoffSourceRequired 裁剪界没有来源时拒绝裁剪。写成 sentinel 而不是自由文本：
// 调用方（`service.StartAuditRetention`）与本包测试都按它判定「是前置条件没满足」还是
// 「库坏了」，两者处置动作相反——前者改调用方，后者根本不该删任何一行。
var ErrPruneCutoffSourceRequired = errors.New("browser command log prune requires a cutoff source")

type browserCommandLogRepo struct {
	db *gorm.DB
}

func NewBrowserCommandLogRepository() BrowserCommandLogRepository {
	return &browserCommandLogRepo{db: _db.GetDB()}
}

func NewBrowserCommandLogRepositoryWithDB(db *gorm.DB) BrowserCommandLogRepository {
	return &browserCommandLogRepo{db: db}
}

func (r *browserCommandLogRepo) Append(ctx context.Context, entry *model.BrowserCommandLog) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *browserCommandLogRepo) ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserCommandLog, error) {
	var list []*model.BrowserCommandLog
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("seq ASC").Find(&list).Error
	return list, err
}

// pruneBatchRows 治理裁剪的单批行数（G19）：command_log 与 llm_plans 共用同一口径，
// 两处各自写死一个字面量的话，改一处就等于「一半分批、一半整表」。
// 分批的理由是锁持有时间：首次上线时超期行数等于历史全量，一条语句吃完就是长事务。
// 改这个值要连 repository/retention_b19g_test.go 的种子（一批 + 1 行）一起看，
// 以及 repository/retention_a6_test.go 里「一批删除对应一条摘要」的那格。
const pruneBatchRows = 5000

// auditRowHashSQL 把一行的审计事实折成一个十六进制 sha256。
//
// 字段清单刻意**不含** id：id 由序列生成，摘要要证的是「这一行内容是不是它」，
// 而不是「它是第几行入库的」。
//
// created_at 一律先 `AT TIME ZONE 'UTC'` 再转文本。直接 ::text 会按**会话时区**渲染，
// 于是同一行数据在主库和任何一颗 UTC 副本/不同 TimeZone 的连接上会折出不同哈希——
// 摘要便不再是「内容的指纹」，而是「内容的指纹 + 当时那条连接的环境」。
// （本仓已有过一次同类事故：日期分桶因 Go 按宿主机时区、PG 按会话时区而裂脑。）
//
// 可空列一律 COALESCE：concat_ws 遇到 NULL 是**跳过该参数**而不是写入空串，
// 少一个参数会让后面的字段整体左移，「step_id 为空、seq=5」与「step_id=5、seq 为空」
// 就可能折成同一个哈希。
const auditRowHashSQL = `encode(sha256(convert_to(concat_ws('|',
	session_id::text, task_id::text,
	COALESCE(step_id::text, ''), seq::text,
	direction, action,
	COALESCE(payload::text, '{}'),
	duration_ms::text,
	CASE WHEN ok IS NULL THEN 'n' WHEN ok THEN 't' ELSE 'f' END,
	(created_at AT TIME ZONE 'UTC')::text), 'utf8')), 'hex')`

// pruneWindowSQL 取「接下来该裁的一批」（按 id 升序 ≤ pruneBatchRows 行），
// 并在**库内**按 session 聚合出批摘要：payload 单行可到 64 KiB，一批 5000 行
// 拉到 Go 里再逐行哈希，等于把治理任务变成内存炸弹。
// 参数依次是 cutoff / id 高水位 / 单批行数。
const pruneWindowSQL = `SELECT session_id,
	       count(*)   AS row_count,
	       min(seq)   AS first_seq,
	       max(seq)   AS last_seq,
	       max(id)    AS max_id,
	       encode(sha256(convert_to(string_agg(row_hash, '' ORDER BY id), 'utf8')), 'hex') AS batch_digest
	FROM (
		SELECT id, session_id, seq, ` + auditRowHashSQL + ` AS row_hash
		FROM browser_command_log
		WHERE created_at < ? AND id <= ?
		ORDER BY id
		LIMIT ?
	) AS rows_to_prune
	GROUP BY session_id
	ORDER BY session_id`

// pruneWindow 是一批裁剪里属于单个 session 的那一段。
type pruneWindow struct {
	SessionID   uint
	RowCount    int64
	FirstSeq    int
	LastSeq     int
	MaxID       uint
	BatchDigest string
}

// lastAuditDigest 取该 session 最新一条摘要（无则 nil）。
// 每次都用全新的 dest struct：复用一个已填充的 struct 再 First() 会把旧字段并进 WHERE，
// 表现为一声「记录不存在」的假红。
func lastAuditDigest(tx *gorm.DB, sessionID uint) (*model.BrowserAuditDigest, error) {
	var d model.BrowserAuditDigest
	err := tx.Where("session_id = ?", sessionID).Order("ordinal DESC").First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// auditChainHash 链哈希 = sha256(prev_chain_hash || batch_digest) 的十六进制。
// 算法写在两处（实现 + 测试）并互相核对：链的意义是「任何人补一段假历史都要重算全链」，
// 所以它必须是可复述的一条式子，而不是一个只有实现自己知道的秘密。
func auditChainHash(prevChainHash, batchDigest string) string {
	sum := sha256.Sum256([]byte(prevChainHash + batchDigest))
	return hex.EncodeToString(sum[:])
}

// PruneBefore G19：删除 cutoff 之前的全部命令日志（分批 pruneBatchRows 防长事务）。
//
// A6给这条路径加了一条前置条件：**每一批先落摘要、再删行，两者同一个事务**。
// 摘要写不进去（表不在、约束冲突、库不可达）就整批回滚，一行都不许消失——
// 否则「裁剪」就退化成「有一段历史只有 90 天的寿命，且没人能证明它存在过」。
//
// 加第二条前置条件：`cutoffSource` 非空。它和 A6 那条同一条路子——
// 「按什么界删的」与「删掉的那些行存在过」都是事后无法反推的事实，只能在删之前落库。
// 界本身进 `prune_runs.cutoff`，来源进 `cutoff_source`；缺来源时改小保留期与一次正常
// 裁剪在库里同形，所以这里**拒绝执行**而不是补一个 `"unknown"`：一个所有人都能填的默认值
// 等于没有这一列（同 A12「缺表即功能全废」的 fail-close 方向选择）。
//
// id 高水位在扫描入口取一次：新写入的行 created_at=now 本来就不满足 `created_at < cutoff`，
// 但回填/迁移可以把老时间戳塞进更大的 id，没有这个界时循环就会去追别人正在写的那一段。
//
// 返回值的口径不变（实删行数），新增的是库里多出来的两类留痕。
func (r *browserCommandLogRepo) PruneBefore(ctx context.Context, cutoff time.Time, cutoffSource string) (int64, error) {
	if cutoffSource == "" {
		return 0, ErrPruneCutoffSourceRequired
	}
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.BrowserCommandLog{})
	}
	var highwater uint
	if err := base().Where("created_at < ?", cutoff).
		Select("COALESCE(max(id), 0)").Scan(&highwater).Error; err != nil {
		return 0, err
	}
	var rowsBefore int64
	if err := base().Where("created_at < ? AND id <= ?", cutoff, highwater).
		Count(&rowsBefore).Error; err != nil {
		return 0, err
	}

	var total int64
	var batches, digests int
	for {
		var window []pruneWindow
		if err := r.db.WithContext(ctx).Raw(pruneWindowSQL, cutoff, highwater, pruneBatchRows).
			Scan(&window).Error; err != nil {
			return total, err
		}
		if len(window) == 0 {
			break
		}
		var pruned int64
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var n int64
			for _, w := range window {
				prev, err := lastAuditDigest(tx, w.SessionID)
				if err != nil {
					return err
				}
				prevChain, prevSeq, ordinal := "", 0, 1
				if prev != nil {
					prevChain, prevSeq, ordinal = prev.ChainHash, prev.LastSeq, prev.Ordinal+1
				}
				d := model.BrowserAuditDigest{
					SessionID:     w.SessionID,
					RowCount:      w.RowCount,
					FirstSeq:      w.FirstSeq,
					LastSeq:       w.LastSeq,
					PrevSeq:       prevSeq,
					Ordinal:       ordinal,
					BatchDigest:   w.BatchDigest,
					PrevChainHash: prevChain,
					ChainHash:     auditChainHash(prevChain, w.BatchDigest),
					Cutoff:        cutoff,
				}
				if err := tx.Create(&d).Error; err != nil {
					return err
				}
				// 删除谓词与摘要谓词是同一套（cutoff + 高水位 + 本段 max_id），
				// 差别只在多了 session_id：摘要覆盖哪些行，被删的就是哪些行。
				res := tx.Where("created_at < ? AND id <= ? AND id <= ? AND session_id = ?",
					cutoff, highwater, w.MaxID, w.SessionID).Delete(&model.BrowserCommandLog{})
				if res.Error != nil {
					return res.Error
				}
				// 行数不符说明摘要写完到删除之间有别的手伸进来（回填、另一个裁剪进程）。
				// 宁可回滚重来，也不留一条描述不实的摘要——摘要一旦可以说谎，它就不是凭据。
				if res.RowsAffected != w.RowCount {
					return fmt.Errorf("摘要声明 %d 行、实删 %d 行（session=%d 批摘要 %s）：撤销本批",
						w.RowCount, res.RowsAffected, w.SessionID, w.BatchDigest)
				}
				n += res.RowsAffected
			}
			pruned = n
			return nil
		})
		if err != nil {
			return total, err
		}
		total += pruned
		batches++
		digests += len(window)
	}

	// 扫描留痕单独一事务：它汇总的正是上面那几个已提交的小事务，把它们一起裹进长事务
	// 等于收回分批的承诺。因此「有摘要行却没有对应留痕」是可查的中断信号，而不是静默状态。
	run := model.BrowserAuditPruneRun{
		Cutoff:       cutoff,
		CutoffSource: cutoffSource,
		Batches:      batches,
		Digests:      digests,
		RowsPruned:   total,
		RowsBefore:   rowsBefore,
	}
	if err := r.db.WithContext(ctx).Create(&run).Error; err != nil {
		return total, err
	}
	return total, nil
}
